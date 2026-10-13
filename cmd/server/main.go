package main

import (
	"fmt"
	"log"
	"net/http"
	"os"
	"time"

	"media-stream-bridge/internal/config"
	"media-stream-bridge/internal/downloader"
	"media-stream-bridge/internal/provider/donghuaweb"
	"media-stream-bridge/internal/provider/fshare"
	"media-stream-bridge/internal/provider/kkphim"
	"media-stream-bridge/internal/provider/nguonc"
	"media-stream-bridge/internal/queue"
	"media-stream-bridge/internal/service"
	transporthttp "media-stream-bridge/internal/transport/http"
)

func main() {
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
		log.Printf("[Warn] Could not load config from %v: %v. Using defaults.", configPath, err)
		cfg = &config.Config{}
		cfg.Server.Host = "0.0.0.0"
		cfg.Server.Port = 6890
		cfg.Storage.TVBaseDir = "/srv/mergerfs/MainPool/Phim/TV Shows"
		cfg.Storage.MovieBaseDir = "/srv/mergerfs/MainPool/Phim/Movies"
		cfg.Storage.DefaultDownloadDir = "/srv/mergerfs/MainPool/downloads"
	}

	// Initialize Providers
	kkProvider := kkphim.New()
	nguonCProvider := nguonc.New()
	donghuaProvider := donghuaweb.New()
	fshareProvider := fshare.New()

	// Initialize Aggregator with 6s timeout
	agg := service.NewAggregator(6*time.Second, kkProvider, nguonCProvider, donghuaProvider, fshareProvider)

	// Initialize Downloader
	dl := downloader.NewEngine(cfg.Storage.DefaultDownloadDir)

	// Initialize Central Queue Manager (2 concurrent workers)
	sonarrNotify := func(seriesID int) {
		dl.NotifySonarrRefresh(cfg.Arr.SonarrURL, cfg.Arr.SonarrAPIKey, seriesID)
	}
	radarrNotify := func(movieID int) {
		dl.NotifySonarrRefresh(cfg.Arr.RadarrURL, cfg.Arr.RadarrAPIKey, movieID)
	}
	qm := queue.NewManager(2, sonarrNotify, radarrNotify)

	// Initialize HTTP Server
	server := transporthttp.NewServer(cfg, agg, dl, qm)

	addr := fmt.Sprintf("%s:%d", cfg.Server.Host, cfg.Server.Port)
	log.Printf("[Media Stream Bridge] Starting high-performance Go daemon on %s ...", addr)
	log.Printf("[Media Stream Bridge] Registered Providers: %s, %s, %s",
		kkProvider.Name(), nguonCProvider.Name(), donghuaProvider.Name())

	if err := http.ListenAndServe(addr, server.Router()); err != nil {
		log.Fatalf("[Media Stream Bridge] Fatal error: %v", err)
	}
}
