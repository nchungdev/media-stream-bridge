package http

import (
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"
	"net/url"
	"path/filepath"
	"strings"

	"github.com/go-chi/chi/v5"

	"media-stream-bridge/internal/queue"
)

// setupQBittorrentRoutes registers qBittorrent WebAPI v2 compatible routes for Sonarr/Radarr.
// Login itself is public and registered separately.
func (s *Server) setupQBittorrentRoutes(r chi.Router) {
	r.Post("/api/v2/auth/logout", s.handleQBitLogout)

	// App information
	r.Get("/api/v2/app/version", s.handleQBitVersion)
	r.Get("/api/v2/app/webapiVersion", s.handleQBitWebAPIVersion)
	r.Get("/api/v2/app/preferences", s.handleQBitPreferences)

	// Torrents management
	r.Get("/api/v2/torrents/info", s.handleQBitTorrentsInfo)
	r.Post("/api/v2/torrents/add", s.handleQBitTorrentsAdd)
	r.Post("/api/v2/torrents/delete", s.handleQBitTorrentsDelete)
	r.Post("/api/v2/torrents/pause", s.handleQBitTorrentsPause)
	r.Post("/api/v2/torrents/resume", s.handleQBitTorrentsResume)
	r.Get("/api/v2/torrents/categories", s.handleQBitCategories)
}

func (s *Server) handleQBitVersion(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("v4.6.0"))
}

func (s *Server) handleQBitWebAPIVersion(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("2.9.0"))
}

func (s *Server) handleQBitPreferences(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"save_path":                      s.cfg.Storage.DefaultDownloadDir,
		"temp_path":                      s.cfg.Storage.DefaultDownloadDir,
		"queueing_enabled":               true,
		"max_active_downloads":           2,
		"max_active_torrents":            5,
		"max_active_uploads":             5,
		"slow_torrent_dl_rate_threshold": 0,
	})
}

func (s *Server) handleQBitCategories(w http.ResponseWriter, r *http.Request) {
	tvDownloadDir := filepath.Join(s.cfg.Storage.DefaultDownloadDir, "series")
	movieDownloadDir := filepath.Join(s.cfg.Storage.DefaultDownloadDir, "movies")
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"tv-sonarr": map[string]string{"savePath": tvDownloadDir},
		"radarr":    map[string]string{"savePath": movieDownloadDir},
	})
}

// qBitTorrentItem matches qBittorrent's JSON representation for Sonarr / Radarr
type qBitTorrentItem struct {
	AddedOn          int64   `json:"added_on"`
	AmountLeft       int64   `json:"amount_left"`
	AutoTmm          bool    `json:"auto_tmm"`
	Category         string  `json:"category"`
	Completed        int64   `json:"completed"`
	CompletionOn     int64   `json:"completion_on"`
	ContentPath      string  `json:"content_path"`
	DlSpeed          int64   `json:"dlspeed"`
	DownloadPath     string  `json:"download_path"`
	Downloaded       int64   `json:"downloaded"`
	ETA              int64   `json:"eta"`
	FLPiecePrio      bool    `json:"f_l_piece_prio"`
	ForceStart       bool    `json:"force_start"`
	Hash             string  `json:"hash"`
	LastActivity     int64   `json:"last_activity"`
	MagnetURI        string  `json:"magnet_uri"`
	MaxRatio         float64 `json:"max_ratio"`
	MaxSeedingTime   int64   `json:"max_seeding_time"`
	Name             string  `json:"name"`
	NumComplete      int     `json:"num_complete"`
	NumIncomplete    int     `json:"num_incomplete"`
	NumLeechs        int     `json:"num_leechs"`
	NumSeeds         int     `json:"num_seeds"`
	Priority         int     `json:"priority"`
	Progress         float64 `json:"progress"`
	Ratio            float64 `json:"ratio"`
	RatioLimit       float64 `json:"ratio_limit"`
	SavePath         string  `json:"save_path"`
	SeedingTime      int64   `json:"seeding_time"`
	SeedingTimeLimit int64   `json:"seeding_time_limit"`
	SeenComplete     int64   `json:"seen_complete"`
	SeqDl            bool    `json:"seq_dl"`
	Size             int64   `json:"size"`
	State            string  `json:"state"`
	SuperSeeding     bool    `json:"super_seeding"`
	Tags             string  `json:"tags"`
	TimeActive       int64   `json:"time_active"`
	TotalSize        int64   `json:"total_size"`
	Tracker          string  `json:"tracker"`
	UpLimit          int64   `json:"up_limit"`
	Uploaded         int64   `json:"uploaded"`
	UpSpeed          int64   `json:"upspeed"`
}

func (s *Server) handleQBitTorrentsInfo(w http.ResponseWriter, r *http.Request) {
	hashesFilter := r.URL.Query().Get("hashes")
	var filterMap map[string]bool
	if hashesFilter != "" {
		filterMap = make(map[string]bool)
		for _, h := range strings.Split(hashesFilter, "|") {
			filterMap[strings.ToLower(strings.TrimSpace(h))] = true
		}
	}

	jobs := s.queueManager.GetAllJobs()
	items := make([]qBitTorrentItem, 0, len(jobs))

	for _, j := range jobs {
		if filterMap != nil && !filterMap[j.Hash] {
			continue
		}

		// State mapping to qBittorrent:
		// "uploading" or "pausedUP" means torrent download is complete.
		// "downloading" means downloading.
		// "pausedDL" means paused.
		// "error" means error.
		stateStr := "downloading"
		switch j.State {
		case queue.StateUploading:
			stateStr = "uploading"
		case queue.StatePaused:
			stateStr = "pausedDL"
		case queue.StateError:
			stateStr = "error"
		case queue.StateQueued:
			stateStr = "queuedDL"
		default:
			stateStr = "downloading"
		}

		amountLeft := j.TotalBytes - j.DoneBytes
		if amountLeft < 0 {
			amountLeft = 0
		}

		item := qBitTorrentItem{
			AddedOn:      j.CreatedAt.Unix(),
			AmountLeft:   amountLeft,
			AutoTmm:      false,
			Category:     j.Category,
			Completed:    j.DoneBytes,
			CompletionOn: j.CompletedAt.Unix(),
			ContentPath:  j.ContentPath,
			DlSpeed:      j.SpeedBytes,
			DownloadPath: j.SavePath,
			Downloaded:   j.DoneBytes,
			ETA:          j.ETA,
			Hash:         j.Hash,
			LastActivity: j.UpdatedAt.Unix(),
			Name:         j.Title,
			NumComplete:  50,
			NumSeeds:     50,
			Progress:     j.Progress,
			Ratio:        1.0,
			SavePath:     j.SavePath,
			Size:         j.TotalBytes,
			State:        stateStr,
			TotalSize:    j.TotalBytes,
		}
		items = append(items, item)
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(items)
}

const (
	maxAddURLs       = 50
	maxMultipartMem  = 1 << 20
	maxTorrentUpload = 4096
)

func (s *Server) handleQBitTorrentsAdd(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseMultipartForm(maxMultipartMem); err != nil && !errors.Is(err, http.ErrNotMultipart) {
		http.Error(w, "Bad Request", http.StatusBadRequest)
		return
	}

	urlsRaw := r.FormValue("urls")
	category := r.FormValue("category")
	savepath := r.FormValue("savepath")

	log.Printf("[qBit Mock Add] Received add request. urls: %s, category: %s, savepath: %s",
		redactAPIKey(urlsRaw), category, savepath)

	var lines []string
	for _, l := range strings.Split(urlsRaw, "\n") {
		if l = strings.TrimSpace(l); l != "" {
			lines = append(lines, l)
		}
	}
	if len(lines) > maxAddURLs {
		http.Error(w, "Fails.", http.StatusRequestEntityTooLarge)
		return
	}

	accepted := 0
	for _, line := range lines {
		target, title, cat, ok := parseBridgeLine(line)
		if !ok {
			continue // magnets and .torrent URLs are never fetched
		}
		if cat == "" {
			cat = category
		}
		if err := s.dispatchJob(r.Context(), target, title, cat, savepath); err != nil {
			log.Printf("[qBit Mock Add] Failed to queue %q: %v", title, err)
			_, _ = w.Write([]byte("Fails."))
			return
		}
		accepted++
	}

	// Sonarr/Radarr upload the placeholder torrent served by /torznab/download
	// after the real job was queued there; accept exactly that payload.
	if accepted == 0 && !hasPlaceholderTorrent(r) {
		w.WriteHeader(http.StatusUnsupportedMediaType)
		_, _ = w.Write([]byte("Fails."))
		return
	}

	_, _ = w.Write([]byte("Ok."))
}

// parseBridgeLine extracts the bridge:// target from either a bare bridge URI
// or a /torznab/download link.
func parseBridgeLine(line string) (target, title, cat string, ok bool) {
	if strings.HasPrefix(line, "bridge://") {
		return line, "Media Stream Grab", "", true
	}
	u, err := url.Parse(line)
	if err != nil || !strings.Contains(u.Path, "/torznab/download") {
		return "", "", "", false
	}
	q := u.Query()
	target = q.Get("url")
	if !strings.HasPrefix(target, "bridge://") {
		return "", "", "", false
	}
	return target, q.Get("title"), q.Get("cat"), true
}

func hasPlaceholderTorrent(r *http.Request) bool {
	if r.MultipartForm == nil {
		return false
	}
	for _, fh := range r.MultipartForm.File["torrents"] {
		if fh.Size > maxTorrentUpload {
			continue
		}
		f, err := fh.Open()
		if err != nil {
			continue
		}
		data, _ := io.ReadAll(io.LimitReader(f, maxTorrentUpload))
		_ = f.Close()
		if string(data) == dummyTorrent {
			return true
		}
	}
	return false
}

func (s *Server) handleQBitTorrentsDelete(w http.ResponseWriter, r *http.Request) {
	_ = r.ParseForm()
	hashes := r.FormValue("hashes")
	deleteFiles := r.FormValue("deleteFiles") == "true"

	for _, h := range strings.Split(hashes, "|") {
		cleanH := strings.TrimSpace(h)
		if cleanH != "" {
			s.queueManager.DeleteJob(cleanH, deleteFiles)
		}
	}

	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("Ok."))
}

func (s *Server) handleQBitTorrentsPause(w http.ResponseWriter, r *http.Request) {
	_ = r.ParseForm()
	hashes := r.FormValue("hashes")
	for _, h := range strings.Split(hashes, "|") {
		cleanH := strings.TrimSpace(h)
		if cleanH != "" {
			s.queueManager.PauseJob(cleanH)
		}
	}
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("Ok."))
}

func (s *Server) handleQBitTorrentsResume(w http.ResponseWriter, r *http.Request) {
	_ = r.ParseForm()
	hashes := r.FormValue("hashes")
	for _, h := range strings.Split(hashes, "|") {
		cleanH := strings.TrimSpace(h)
		if cleanH != "" {
			s.queueManager.ResumeJob(cleanH)
		}
	}
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("Ok."))
}
