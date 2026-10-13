package queue

import (
	"bufio"
	"context"
	"crypto/sha1"
	"encoding/hex"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"media-stream-bridge/internal/domain"
	"media-stream-bridge/internal/formatter"
)

type JobState string

const (
	StateQueued      JobState = "queued"
	StateDownloading JobState = "downloading"
	StateUploading   JobState = "uploading" // Completed in qBittorrent semantics
	StatePaused      JobState = "paused"
	StateError       JobState = "error"
)

type EpisodeProgress struct {
	Index    int     `json:"index"`
	Name     string  `json:"name"`
	Progress float64 `json:"progress"`
	Done     bool    `json:"done"`
}

type Job struct {
	ID          string            `json:"id"`
	Hash        string            `json:"hash"` // 40-character hex string for qBittorrent protocol
	Title       string            `json:"title"`
	URI         string            `json:"uri"`
	Category    string            `json:"category"` // "tv" or "movie"
	SavePath    string            `json:"save_path"`
	ContentPath string            `json:"content_path"`
	State       JobState          `json:"state"`
	TotalBytes  int64             `json:"total_bytes"`
	DoneBytes   int64             `json:"done_bytes"`
	Progress    float64           `json:"progress"` // 0.0 -> 1.0
	SpeedBytes  int64             `json:"speed_bytes"`
	ETA         int64             `json:"eta"`
	ErrorMsg    string            `json:"error_msg,omitempty"`
	CreatedAt   time.Time         `json:"created_at"`
	UpdatedAt   time.Time         `json:"updated_at"`
	CompletedAt time.Time         `json:"completed_at,omitempty"`
	Media       *domain.MediaItem `json:"media,omitempty"`
	Episodes    []EpisodeProgress `json:"episodes,omitempty"`

	cancel context.CancelFunc `json:"-"`
}

type Manager struct {
	mu            sync.RWMutex
	jobs          map[string]*Job // key: Hash
	queueOrder    []string        // list of Hashes in order
	maxConcurrent int
	activeWorkers int
	notifySonarr  func(seriesID int)
	notifyRadarr  func(movieID int)
}

func NewManager(maxConcurrent int, sonarrNotifier, radarrNotifier func(id int)) *Manager {
	if maxConcurrent <= 0 {
		maxConcurrent = 2
	}
	m := &Manager{
		jobs:          make(map[string]*Job),
		queueOrder:    make([]string, 0),
		maxConcurrent: maxConcurrent,
		notifySonarr:  sonarrNotifier,
		notifyRadarr:  radarrNotifier,
	}
	go m.workerLoop()
	return m
}

func GenerateHash(text string) string {
	h := sha1.New()
	h.Write([]byte(text))
	return hex.EncodeToString(h.Sum(nil))
}

func (m *Manager) AddJob(title, uri, category, savePath string, media *domain.MediaItem) *Job {
	m.mu.Lock()
	defer m.mu.Unlock()

	hash := GenerateHash(fmt.Sprintf("%s|%s", title, uri))
	if existing, found := m.jobs[hash]; found {
		return existing
	}

	job := &Job{
		ID:         fmt.Sprintf("job-%d", time.Now().UnixNano()),
		Hash:       hash,
		Title:      title,
		URI:        uri,
		Category:   category,
		SavePath:   savePath,
		State:      StateQueued,
		TotalBytes: 1073741824, // 1GB default
		Progress:   0.0,
		CreatedAt:  time.Now(),
		UpdatedAt:  time.Now(),
		Media:      media,
	}

	if media != nil && len(media.Episodes) > 0 {
		job.TotalBytes = int64(len(media.Episodes)) * 524288000 // estimate ~500MB per episode
		for i, ep := range media.Episodes {
			job.Episodes = append(job.Episodes, EpisodeProgress{
				Index: i + 1,
				Name:  ep.Name,
				Done:  false,
			})
		}
	}

	m.jobs[hash] = job
	m.queueOrder = append(m.queueOrder, hash)
	log.Printf("[Queue] Added job %s: '%s' (hash: %s)", job.ID, job.Title, job.Hash)
	return job
}

func (m *Manager) GetAllJobs() []*Job {
	m.mu.RLock()
	defer m.mu.RUnlock()

	result := make([]*Job, 0, len(m.queueOrder))
	for _, h := range m.queueOrder {
		if j, ok := m.jobs[h]; ok {
			result = append(result, j)
		}
	}
	return result
}

func (m *Manager) GetJob(hash string) *Job {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.jobs[strings.ToLower(hash)]
}

func (m *Manager) DeleteJob(hash string, deleteFiles bool) {
	m.mu.Lock()
	defer m.mu.Unlock()

	hash = strings.ToLower(hash)
	job, exists := m.jobs[hash]
	if !exists {
		return
	}

	if job.cancel != nil {
		job.cancel()
	}

	if deleteFiles && job.SavePath != "" {
		_ = os.RemoveAll(job.SavePath)
	}

	delete(m.jobs, hash)
	newOrder := make([]string, 0, len(m.queueOrder))
	for _, h := range m.queueOrder {
		if h != hash {
			newOrder = append(newOrder, h)
		}
	}
	m.queueOrder = newOrder
	log.Printf("[Queue] Deleted job: %s (hash: %s)", job.Title, hash)
}

func (m *Manager) PauseJob(hash string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	job := m.jobs[strings.ToLower(hash)]
	if job != nil && job.State == StateDownloading {
		if job.cancel != nil {
			job.cancel()
		}
		job.State = StatePaused
		job.UpdatedAt = time.Now()
	}
}

func (m *Manager) ResumeJob(hash string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	job := m.jobs[strings.ToLower(hash)]
	if job != nil && job.State == StatePaused {
		job.State = StateQueued
		job.UpdatedAt = time.Now()
	}
}

func (m *Manager) workerLoop() {
	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()

	for range ticker.C {
		m.mu.Lock()
		if m.activeWorkers >= m.maxConcurrent {
			m.mu.Unlock()
			continue
		}

		var nextJob *Job
		for _, h := range m.queueOrder {
			j := m.jobs[h]
			if j != nil && j.State == StateQueued {
				nextJob = j
				break
			}
		}

		if nextJob == nil {
			m.mu.Unlock()
			continue
		}

		m.activeWorkers++
		nextJob.State = StateDownloading
		nextJob.UpdatedAt = time.Now()
		ctx, cancel := context.WithCancel(context.Background())
		nextJob.cancel = cancel
		m.mu.Unlock()

		go m.executeJob(ctx, nextJob)
	}
}

func (m *Manager) executeJob(ctx context.Context, job *Job) {
	defer func() {
		m.mu.Lock()
		m.activeWorkers--
		m.mu.Unlock()
	}()

	log.Printf("[Worker] Starting processing job: '%s' into '%s'", job.Title, job.SavePath)
	_ = os.MkdirAll(job.SavePath, 0755)

	if job.Media == nil || len(job.Media.Episodes) == 0 {
		m.mu.Lock()
		job.State = StateError
		job.ErrorMsg = "No streamable episodes found"
		m.mu.Unlock()
		return
	}

	totalEps := len(job.Media.Episodes)
	completedEps := 0

	for i, ep := range job.Media.Episodes {
		select {
		case <-ctx.Done():
			log.Printf("[Worker] Job cancelled/paused: %s", job.Title)
			return
		default:
		}

		// Calculate target episode file name
		var fileName string
		if job.Category == "tv" {
			season := formatter.DetectSeason(job.Media.Title, job.Media.OriginalTitle, job.Title)
			epNum := formatter.DetectEpisodeNum(ep.Name, ep.Slug)
			seriesClean := formatter.PickBestEnglishTitle(job.Media.OriginalTitle, "", job.Media.Title)
			if seriesClean == "" {
				seriesClean = job.Media.Title
			}
			seriesClean = formatter.CleanSeriesName(seriesClean)
			fileName = formatter.BuildEpisodeFileName(seriesClean, season, epNum)
		} else {
			movieClean := formatter.PickBestEnglishTitle(job.Media.OriginalTitle, "", job.Media.Title)
			if movieClean == "" {
				movieClean = job.Media.Title
			}
			if job.Media.Year > 0 {
				fileName = fmt.Sprintf("%s (%d).mp4", movieClean, job.Media.Year)
			} else {
				fileName = fmt.Sprintf("%s.mp4", movieClean)
			}
		}

		outFilePath := filepath.Join(job.SavePath, fileName)
		if job.ContentPath == "" {
			job.ContentPath = outFilePath
		}

		// Check if file already downloaded
		if fi, err := os.Stat(outFilePath); err == nil && fi.Size() > 1024*1024 {
			log.Printf("[Worker] Episode %s already exists, skipping.", fileName)
			completedEps++
			m.updateJobProgress(job, completedEps, totalEps, i, true, 0)
			continue
		}

		streamURL := ep.StreamURL
		if streamURL == "" && ep.EmbedURL != "" {
			streamURL = ep.EmbedURL
		}

		if streamURL == "" {
			log.Printf("[Worker] Episode %s has no URL, skipping", ep.Name)
			continue
		}

		// Download episode via ffmpeg stream copy with progress pipe
		err := m.downloadEpisodeWithFFmpeg(ctx, job, streamURL, outFilePath, i, totalEps, completedEps)
		if err != nil {
			log.Printf("[Worker] Failed downloading %s: %v", fileName, err)
			m.mu.Lock()
			job.State = StateError
			job.ErrorMsg = fmt.Sprintf("Failed at %s: %v", ep.Name, err)
			m.mu.Unlock()
			return
		}

		completedEps++
		m.updateJobProgress(job, completedEps, totalEps, i, true, 0)
	}

	// Job completed successfully
	m.mu.Lock()
	job.State = StateUploading // In qBittorrent, uploading means 100% complete and ready for Sonarr import
	job.Progress = 1.0
	job.DoneBytes = job.TotalBytes
	job.SpeedBytes = 0
	job.ETA = 0
	job.CompletedAt = time.Now()
	job.UpdatedAt = time.Now()
	m.mu.Unlock()

	log.Printf("[Worker] Job successfully completed: %s", job.Title)

	// Notify Sonarr/Radarr
	if m.notifySonarr != nil && job.Category == "tv" {
		m.notifySonarr(0)
	}
	if m.notifyRadarr != nil && job.Category == "movie" {
		m.notifyRadarr(0)
	}
}

func (m *Manager) updateJobProgress(job *Job, completedCount, totalCount, epIdx int, epDone bool, speed int64) {
	m.mu.Lock()
	defer m.mu.Unlock()

	job.Progress = float64(completedCount) / float64(totalCount)
	job.DoneBytes = int64(float64(job.TotalBytes) * job.Progress)
	job.SpeedBytes = speed
	job.UpdatedAt = time.Now()
	if epIdx < len(job.Episodes) {
		job.Episodes[epIdx].Done = epDone
		if epDone {
			job.Episodes[epIdx].Progress = 1.0
		}
	}
}

func (m *Manager) downloadEpisodeWithFFmpeg(ctx context.Context, job *Job, streamURL, outPath string, epIdx, totalCount, completedCount int) error {
	tmpFile := outPath + ".part"
	_ = os.Remove(tmpFile)

	args := []string{
		"-headers", "User-Agent: Mozilla/5.0 (Windows NT 10.0; Win64; x64)\r\nReferer: https://phimapi.com/\r\n",
		"-i", streamURL,
		"-c", "copy",
		"-bsf:a", "aac_adtstoasc",
		"-progress", "pipe:1",
		"-y", tmpFile,
	}

	cmd := exec.CommandContext(ctx, "ffmpeg", args...)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}

	if err := cmd.Start(); err != nil {
		return err
	}

	// Read progress stream from ffmpeg
	scanner := bufio.NewScanner(stdout)
	lastUpdate := time.Now()
	var lastOutBytes int64

	go func() {
		for scanner.Scan() {
			line := scanner.Text()
			parts := strings.SplitN(line, "=", 2)
			if len(parts) == 2 {
				k := strings.TrimSpace(parts[0])
				v := strings.TrimSpace(parts[1])
				if k == "total_size" {
					if curBytes, err := strconv.ParseInt(v, 10, 64); err == nil && curBytes > 0 {
						now := time.Now()
						dur := now.Sub(lastUpdate).Seconds()
						if dur >= 1.0 {
							speed := int64(float64(curBytes-lastOutBytes) / dur)
							lastOutBytes = curBytes
							lastUpdate = now

							m.mu.Lock()
							epWeight := 1.0 / float64(totalCount)
							curEpRatio := float64(curBytes) / 524288000.0 // estimate 500MB per ep
							if curEpRatio > 0.95 {
								curEpRatio = 0.95
							}
							job.Progress = (float64(completedCount) * epWeight) + (curEpRatio * epWeight)
							job.DoneBytes = int64(float64(job.TotalBytes) * job.Progress)
							job.SpeedBytes = speed
							if speed > 0 {
								job.ETA = (job.TotalBytes - job.DoneBytes) / speed
							}
							job.UpdatedAt = now
							if epIdx < len(job.Episodes) {
								job.Episodes[epIdx].Progress = curEpRatio
							}
							m.mu.Unlock()
						}
					}
				}
			}
		}
	}()

	if err := cmd.Wait(); err != nil {
		_ = os.Remove(tmpFile)
		return err
	}

	// Rename .part to final file
	return os.Rename(tmpFile, outPath)
}

// Ensure interface compatibility
var _ io.Reader = (*bufio.Reader)(nil)
