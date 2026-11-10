package main

import (
	"log"
	"net"
	"net/http"
	"os"
	"strconv"
	"syscall"
	"time"

	"media-stream-bridge/internal/config"
	"media-stream-bridge/internal/provider/donghuaweb"
	"media-stream-bridge/internal/provider/fshare"
	"media-stream-bridge/internal/provider/kkphim"
	"media-stream-bridge/internal/queue"
	"media-stream-bridge/internal/service"
	transporthttp "media-stream-bridge/internal/transport/http"
)

func main() {
	syscall.Umask(0o002)

	var configPath string
	candidates := []string{
		os.Getenv("MEDIA_STREAM_CONFIG"),
		"/config/config.json",
		"/etc/fshare-bridge/config.json",
		"./config.json",
	}

	for _, p := range candidates {
		if p != "" {
			if _, err := os.Stat(p); err == nil {
				configPath = p
				break
			}
		}
	}

	cfg, err := config.LoadConfig(configPath)
	if err != nil {
		log.Printf("[Warn] Could not load config from %q: %v. Using defaults.", configPath, err)
		cfg = config.Default()
	}
	if err := cfg.ApplyEnv(); err != nil {
		log.Fatalf("[Media Stream Bridge] Invalid configuration: %v", err)
	}
	if err := os.MkdirAll(cfg.Storage.DefaultDownloadDir, 0o775); err != nil {
		log.Printf("[Warn] Cannot create download dir %s: %v", cfg.Storage.DefaultDownloadDir, err)
	}

	// Initialize Providers (KKPhim direct HLS CDN, YanHH3D, Fshare)
	kkProvider := kkphim.New()
	donghuaProvider := donghuaweb.New()
	fshareProvider := fshare.New()

	// Initialize Aggregator with 6s timeout
	agg := service.NewAggregator(6*time.Second, kkProvider, donghuaProvider, fshareProvider)

	// Initialize Central Queue Manager (2 concurrent workers)
	qm := queue.NewManager(queue.Options{
		MaxConcurrent:  2,
		MaxJobs:        cfg.MaxJobs,
		EpisodeTimeout: cfg.EpisodeTimeout,
		BaseDir:        cfg.Storage.DefaultDownloadDir,
	})

	// Initialize HTTP Server
	server := transporthttp.NewServer(cfg, agg, qm)

	addr := net.JoinHostPort(cfg.Server.Host, strconv.Itoa(cfg.Server.Port))
	log.Printf("[Media Stream Bridge] Starting high-performance Go daemon on %s ...", addr)
	log.Printf("[Media Stream Bridge] Registered Providers: %s, %s, %s",
		kkProvider.Name(), donghuaProvider.Name(), fshareProvider.Name())

	srv := &http.Server{
		Addr:              addr,
		Handler:           server.Router(),
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      60 * time.Second,
		IdleTimeout:       120 * time.Second,
		MaxHeaderBytes:    1 << 20,
	}
	if err := srv.ListenAndServe(); err != nil {
		log.Fatalf("[Media Stream Bridge] Fatal error: %v", err)
	}
}
