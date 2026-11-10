package http

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"path/filepath"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	"media-stream-bridge/internal/config"
	"media-stream-bridge/internal/formatter"
	"media-stream-bridge/internal/pathsafe"
	"media-stream-bridge/internal/queue"
	"media-stream-bridge/internal/service"
)

const (
	dispatchConcurrency = 4
	dispatchTimeout     = 30 * time.Second
)

type Server struct {
	cfg          *config.Config
	aggregator   *service.Aggregator
	queueManager *queue.Manager
	auth         *authStore
	router       *chi.Mux
	sem          chan struct{} // bounds concurrent provider lookups
}

func NewServer(cfg *config.Config, agg *service.Aggregator, qm *queue.Manager) *Server {
	s := &Server{
		cfg:          cfg,
		aggregator:   agg,
		queueManager: qm,
		auth:         newAuthStore(cfg.QBitUsername, cfg.QBitPassword, cfg.APIKey),
		router:       chi.NewRouter(),
		sem:          make(chan struct{}, dispatchConcurrency),
	}
	s.setupRoutes()
	return s
}

func (s *Server) setupRoutes() {
	s.router.Use(middleware.RequestID)
	s.router.Use(accessLog)
	s.router.Use(middleware.Recoverer)
	s.router.Use(limitBody)
	s.router.Use(sameOrigin)

	// Public: qBittorrent login and a minimal health probe.
	s.router.Post("/api/v2/auth/login", s.handleQBitLogin)
	s.router.Get("/api/status", s.handleStatus)

	s.router.Group(func(r chi.Router) {
		r.Use(s.requireAuth)
		r.Get("/", s.handleRoot)
		s.setupQBittorrentRoutes(r)
		s.setupDashboardRoutes(r)

		// Torznab endpoints (compat with Prowlarr / Sonarr / Radarr)
		r.Get("/torznab", s.handleTorznab)
		r.Get("/torznab/api", s.handleTorznab)
		r.Get("/torznab/download", s.handleTorznabDownload)
	})
}

func (s *Server) handleRoot(w http.ResponseWriter, r *http.Request) {
	if strings.Contains(r.Header.Get("Accept"), "text/html") {
		s.handleDashboardHTML(w, r)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"service":     "Media Stream Bridge",
		"status":      "running",
		"dashboard":   "/dashboard",
		"torznab":     "/torznab/api",
		"qbittorrent": "/api/v2",
	})
}

func (s *Server) handleStatus(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

// dispatchJob resolves a bridge:// URI and queues it. Provider lookups are
// bounded by a small semaphore so a burst of URLs cannot fan out unchecked.
func (s *Server) dispatchJob(ctx context.Context, rawURL, title, cat, customSavePath string) error {
	if !strings.HasPrefix(rawURL, "bridge://") {
		return fmt.Errorf("not a bridge URI")
	}
	select {
	case s.sem <- struct{}{}:
		defer func() { <-s.sem }()
	case <-ctx.Done():
		return ctx.Err()
	}

	ctx, cancel := context.WithTimeout(ctx, dispatchTimeout)
	defer cancel()
	media, err := s.aggregator.GetMediaByURI(ctx, rawURL)
	if err != nil {
		return fmt.Errorf("resolve %s: %w", rawURL, err)
	}

	destDir := s.resolveDestDir(media.Category == "tv" || cat == "tv", media.Title, media.OriginalTitle, title, customSavePath)
	_, err = s.queueManager.AddJob(title, rawURL, cat, destDir, media)
	return err
}

// resolveDestDir picks the download directory. A client supplied save path is
// honoured only when it lies inside the download root.
func (s *Server) resolveDestDir(isTV bool, mediaTitle, origTitle, jobTitle, customSavePath string) string {
	root := s.cfg.Storage.DefaultDownloadDir
	if customSavePath != "" {
		if pathsafe.Within(root, customSavePath) {
			return filepath.Clean(customSavePath)
		}
		log.Printf("[Queue Dispatch] Ignoring save path outside download dir: %q", customSavePath)
	}

	name := formatter.PickBestEnglishTitle(origTitle, "", mediaTitle)
	if name == "" {
		name = mediaTitle
	}
	if isTV {
		series := pathsafe.SafeComponent(formatter.CleanSeriesName(name))
		season := formatter.DetectSeason(mediaTitle, origTitle, jobTitle)
		if season <= 0 {
			season = 1
		}
		return filepath.Join(root, "series", series, fmt.Sprintf("Season %02d", season))
	}
	return filepath.Join(root, "movies", pathsafe.SafeComponent(name))
}

func (s *Server) Router() *chi.Mux {
	return s.router
}
