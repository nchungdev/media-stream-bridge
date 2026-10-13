package downloader

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"media-stream-bridge/internal/domain"
	"media-stream-bridge/internal/formatter"
)

type Engine struct {
	downloadDir string
	client      *http.Client
	mu          sync.Mutex
	activeJobs  map[string]bool
}

func NewEngine(downloadDir string) *Engine {
	_ = os.MkdirAll(downloadDir, 0755)
	return &Engine{
		downloadDir: downloadDir,
		client:      &http.Client{Timeout: 10 * time.Second},
		activeJobs:  make(map[string]bool),
	}
}

// DownloadEpisode downloads an episode stream (HLS m3u8 or MP4) using native ffmpeg or yt-dlp.
func (e *Engine) DownloadEpisode(ctx context.Context, item *domain.MediaItem, ep *domain.Episode, targetDir string) error {
	_ = os.MkdirAll(targetDir, 0755)

	var cleanName string
	if item.Category == "tv" {
		season := formatter.DetectSeason(item.Title, item.OriginalTitle, "")
		epNum := formatter.DetectEpisodeNum(ep.Name, ep.Slug)
		seriesClean := formatter.PickBestEnglishTitle(item.OriginalTitle, "", item.Title)
		if seriesClean == "" {
			seriesClean = item.Title
		}
		cleanName = formatter.BuildEpisodeFileName(seriesClean, season, epNum)
	} else {
		movieName := formatter.PickBestEnglishTitle(item.OriginalTitle, "", item.Title)
		if movieName == "" {
			movieName = item.Title
		}
		if item.Year > 0 {
			cleanName = fmt.Sprintf("%s (%d).mp4", movieName, item.Year)
		} else {
			cleanName = fmt.Sprintf("%s.mp4", movieName)
		}
		cleanName = strings.ReplaceAll(cleanName, "/", "-")
	}

	outPath := filepath.Join(targetDir, cleanName)

	if _, err := os.Stat(outPath); err == nil {
		log.Printf("[Downloader] File already exists: %s, skipping", outPath)
		return nil
	}

	streamURL := ep.StreamURL
	if streamURL == "" && ep.EmbedURL != "" {
		streamURL = ep.EmbedURL
	}

	if streamURL == "" {
		return fmt.Errorf("no stream or embed URL found for %s", ep.Name)
	}

	log.Printf("[Downloader] Starting download for %s -> %s", cleanName, outPath)

	// Build ffmpeg / yt-dlp command
	// We use yt-dlp as primary, fallback to ffmpeg
	var cmd *exec.Cmd
	if strings.HasSuffix(streamURL, ".m3u8") || strings.Contains(streamURL, ".m3u8?") {
		// Use ffmpeg with headers for direct m3u8 streams
		cmd = exec.CommandContext(ctx, "ffmpeg",
			"-headers", "User-Agent: Mozilla/5.0\r\nReferer: https://phimapi.com/\r\n",
			"-i", streamURL,
			"-c", "copy",
			"-bsf:a", "aac_adtstoasc",
			"-y", outPath,
		)
	} else {
		// General video URL or embed
		cmd = exec.CommandContext(ctx, "yt-dlp",
			"--no-check-certificates",
			"-o", outPath,
			streamURL,
		)
	}

	output, err := cmd.CombinedOutput()
	if err != nil {
		log.Printf("[Downloader] Error downloading %s: %v, output: %s", cleanName, err, string(output))
		return err
	}

	log.Printf("[Downloader] Successfully downloaded: %s", outPath)
	return nil
}

// NotifySonarrRefresh calls Sonarr/Radarr rescan command after download finishes.
func (e *Engine) NotifySonarrRefresh(sonarrURL, apiKey string, seriesID int) {
	if sonarrURL == "" || apiKey == "" || seriesID <= 0 {
		return
	}
	payload := fmt.Sprintf(`{"name": "RescanSeries", "seriesId": %d}`, seriesID)
	reqURL := fmt.Sprintf("%s/api/v3/command", strings.TrimRight(sonarrURL, "/"))
	req, err := http.NewRequest(http.MethodPost, reqURL, strings.NewReader(payload))
	if err != nil {
		return
	}
	req.Header.Set("X-Api-Key", apiKey)
	req.Header.Set("Content-Type", "application/json")
	resp, err := e.client.Do(req)
	if err == nil {
		resp.Body.Close()
	}
}

// ResolveAria2Download adds link to Aria2 RPC if applicable.
func (e *Engine) AddToAria2(rpcURL, rpcSecret, downloadURL, dir string) error {
	// Simple JSON-RPC call
	data := fmt.Sprintf(`{"jsonrpc":"2.0","id":"bridge","method":"aria2.addUri","params":["token:%s",["%s"],{"dir":"%s"}]}`,
		rpcSecret, downloadURL, dir)
	req, err := http.NewRequest(http.MethodPost, rpcURL, strings.NewReader(data))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := e.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	return nil
}
