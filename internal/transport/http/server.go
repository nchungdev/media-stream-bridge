package http

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"path/filepath"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/go-chi/cors"

	"media-stream-bridge/internal/config"
	"media-stream-bridge/internal/downloader"
	"media-stream-bridge/internal/formatter"
	"media-stream-bridge/internal/queue"
	"media-stream-bridge/internal/service"
)

type Server struct {
	cfg          *config.Config
	aggregator   *service.Aggregator
	downloader   *downloader.Engine
	queueManager *queue.Manager
	router       *chi.Mux
}

func NewServer(cfg *config.Config, agg *service.Aggregator, dl *downloader.Engine, qm *queue.Manager) *Server {
	s := &Server{
		cfg:          cfg,
		aggregator:   agg,
		downloader:   dl,
		queueManager: qm,
		router:       chi.NewRouter(),
	}
	s.setupRoutes()
	return s
}

func (s *Server) setupRoutes() {
	s.router.Use(middleware.RequestID)
	s.router.Use(middleware.RealIP)
	s.router.Use(middleware.Logger)
	s.router.Use(middleware.Recoverer)

	s.router.Use(cors.Handler(cors.Options{
		AllowedOrigins: []string{"*"},
		AllowedMethods: []string{"GET", "POST", "OPTIONS"},
		AllowedHeaders: []string{"*"},
	}))

	// Dashboard & Root
	s.router.Get("/", s.handleRoot)
	s.router.Get("/api/status", s.handleStatus)

	// Setup qBittorrent mock endpoints for Sonarr/Radarr
	s.setupQBittorrentRoutes()

	// Setup Web Dashboard endpoints
	s.setupDashboardRoutes()

	// Torznab endpoints (compat with Prowlarr / Sonarr / Radarr)
	s.router.Get("/torznab", s.handleTorznab)
	s.router.Get("/torznab/api", s.handleTorznab)
	s.router.Get("/torznab/download", s.handleTorznabDownload)

	// Manual download trigger API
	s.router.Post("/api/download", s.handleManualDownload)
}

func (s *Server) handleRoot(w http.ResponseWriter, r *http.Request) {
	if strings.Contains(r.Header.Get("Accept"), "text/html") {
		s.handleDashboardHTML(w, r)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"service":   "Media Stream Bridge",
		"version":   "2.1 (Golang + Queue Manager)",
		"status":    "running",
		"dashboard": "/dashboard",
		"torznab":   "/torznab/api",
		"qbittorrent": "/api/v2",
	})
}

func (s *Server) handleStatus(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"status":      "ok",
		"version":     "2.1.0",
		"queue_count": len(s.queueManager.GetAllJobs()),
	})
}

func (s *Server) handleTorznab(w http.ResponseWriter, r *http.Request) {
	tMode := strings.ToLower(r.URL.Query().Get("t"))
	if tMode == "" {
		tMode = "caps"
	}

	scheme := "http"
	if r.TLS != nil || r.Header.Get("X-Forwarded-Proto") == "https" {
		scheme = "https"
	}
	hostURL := fmt.Sprintf("%s://%s", scheme, r.Host)

	if tMode == "caps" {
		capsBytes, err := GetCapsXML()
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/xml; charset=utf-8")
		_, _ = w.Write(capsBytes)
		return
	}

	query := r.URL.Query().Get("q")
	// If Sonarr queries by episode/season, check query string
	if query == "" {
		query = r.URL.Query().Get("title")
	}

	results := s.aggregator.SearchAll(r.Context(), query)
	rssBytes, err := GenerateRSSXML(results, hostURL)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/xml; charset=utf-8")
	_, _ = w.Write(rssBytes)
}

func (s *Server) handleTorznabDownload(w http.ResponseWriter, r *http.Request) {
	rawURL := r.URL.Query().Get("url")
	cat := r.URL.Query().Get("cat")
	title := r.URL.Query().Get("title")

	if rawURL == "" {
		http.Error(w, "Missing URL", http.StatusBadRequest)
		return
	}

	log.Printf("[Torznab Grab] Grabbed item: %s, url: %s, cat: %s", title, rawURL, cat)

	// Dispatch to central queue
	s.dispatchJobToQueue(rawURL, title, cat, "")

	// Return dummy 200 OK torrent content so *arr marks grab as success
	w.Header().Set("Content-Type", "application/x-bittorrent")
	_, _ = w.Write([]byte("d8:announce0:e"))
}

func (s *Server) dispatchJobToQueue(rawURL, title, cat, customSavePath string) {
	if !strings.HasPrefix(rawURL, "bridge://") {
		return
	}

	go func() {
		bgCtx := context.Background()
		media, err := s.aggregator.GetMediaByURI(bgCtx, rawURL)
		if err != nil {
			log.Printf("[Queue Dispatch] Failed to resolve media %s: %v", rawURL, err)
			return
		}

		destDir := customSavePath
		if destDir == "" {
			if media.Category == "tv" || cat == "tv" {
				seriesClean := formatter.PickBestEnglishTitle(media.OriginalTitle, "", media.Title)
				if seriesClean == "" {
					seriesClean = media.Title
				}
				seriesClean = formatter.CleanSeriesName(seriesClean)
				season := formatter.DetectSeason(media.Title, media.OriginalTitle, title)
				if season <= 0 {
					season = 1
				}
				destDir = filepath.Join(s.cfg.Storage.DefaultDownloadDir, "series", seriesClean, fmt.Sprintf("Season %02d", season))
			} else {
				movieName := formatter.PickBestEnglishTitle(media.OriginalTitle, "", media.Title)
				if movieName == "" {
					movieName = media.Title
				}
				destDir = filepath.Join(s.cfg.Storage.DefaultDownloadDir, "movies", movieName)
			}
		}

		s.queueManager.AddJob(title, rawURL, cat, destDir, media)
	}()
}

func (s *Server) handleManualDownload(w http.ResponseWriter, r *http.Request) {
	var body struct {
		URI      string `json:"uri"`
		Category string `json:"category"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	go func() {
		media, err := s.aggregator.GetMediaByURI(r.Context(), body.URI)
		if err != nil {
			log.Printf("[Manual DL] Failed: %v", err)
			return
		}
		targetBase := s.cfg.Storage.TVBaseDir
		if body.Category == "movie" {
			targetBase = s.cfg.Storage.MovieBaseDir
		}
		destDir := filepath.Join(targetBase, media.Title)
		for _, ep := range media.Episodes {
			_ = s.downloader.DownloadEpisode(r.Context(), media, &ep, destDir)
		}
	}()

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "queued"})
}

func (s *Server) Router() *chi.Mux {
	return s.router
}
