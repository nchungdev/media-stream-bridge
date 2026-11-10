package queue

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"media-stream-bridge/internal/domain"
	"media-stream-bridge/internal/formatter"
	"media-stream-bridge/internal/pathsafe"
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

	// Files lists every file this job created, so deletion never touches anything else.
	Files []string `json:"-"`

	cancel context.CancelFunc `json:"-"`
}

// Options configures a Manager.
type Options struct {
	MaxConcurrent  int
	MaxJobs        int
	EpisodeTimeout time.Duration
	BaseDir        string // download root; jobs may only write below it
}

type Manager struct {
	mu            sync.RWMutex
	jobs          map[string]*Job // key: Hash
	queueOrder    []string        // list of Hashes in order
	maxConcurrent int
	activeWorkers int
	maxJobs       int
	episodeTO     time.Duration
	baseDir       string
}

func NewManager(opts Options) *Manager {
	if opts.MaxConcurrent <= 0 {
		opts.MaxConcurrent = 2
	}
	if opts.MaxJobs <= 0 {
		opts.MaxJobs = 200
	}
	if opts.EpisodeTimeout <= 0 {
		opts.EpisodeTimeout = 3 * time.Hour
	}
	m := &Manager{
		jobs:          make(map[string]*Job),
		queueOrder:    make([]string, 0),
		maxConcurrent: opts.MaxConcurrent,
		maxJobs:       opts.MaxJobs,
		episodeTO:     opts.EpisodeTimeout,
		baseDir:       opts.BaseDir,
	}
	go m.workerLoop()
	return m
}

func GenerateHash(text string) string {
	h := sha1.New()
	h.Write([]byte(text))
	return hex.EncodeToString(h.Sum(nil))
}

// ErrQueueFull is returned by AddJob when MaxJobs is reached.
var ErrQueueFull = errors.New("queue is full")

func (m *Manager) AddJob(title, uri, category, savePath string, media *domain.MediaItem) (*Job, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	hash := GenerateHash(fmt.Sprintf("%s|%s", title, uri))
	if existing, found := m.jobs[hash]; found {
		return existing, nil
	}
	if len(m.jobs) >= m.maxJobs {
		return nil, ErrQueueFull
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
	return job, nil
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

	if deleteFiles {
		m.removeJobFiles(job)
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
	if !pathsafe.Within(m.baseDir, job.SavePath) {
		m.failJob(job, "save path outside the download directory")
		return
	}
	_ = os.MkdirAll(job.SavePath, 0o775)

	if job.Media == nil || len(job.Media.Episodes) == 0 {
		m.failJob(job, "No streamable episodes found")
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

		fileName := m.episodeFileName(job, ep)
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

		m.trackFile(job, outFilePath)

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
}

func (m *Manager) trackFile(job *Job, path string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	job.Files = append(job.Files, path)
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

func (m *Manager) failJob(job *Job, msg string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	job.State = StateError
	job.ErrorMsg = msg
}

// episodeFileName builds a sanitized "<name>.mp4" for one episode.
func (m *Manager) episodeFileName(job *Job, ep domain.Episode) string {
	title := formatter.PickBestEnglishTitle(job.Media.OriginalTitle, "", job.Media.Title)
	if title == "" {
		title = job.Media.Title
	}
	var stem string
	if job.Category == "tv" {
		season := formatter.DetectSeason(job.Media.Title, job.Media.OriginalTitle, job.Title)
		epNum := formatter.DetectEpisodeNum(ep.Name, ep.Slug)
		stem = formatter.BuildEpisodeFileName(formatter.CleanSeriesName(title), season, epNum)
		stem = strings.TrimSuffix(stem, ".mp4")
	} else if job.Media.Year > 0 {
		stem = fmt.Sprintf("%s (%d)", title, job.Media.Year)
	} else {
		stem = title
	}
	return pathsafe.SafeComponent(stem) + ".mp4"
}

// removeJobFiles deletes only the files the job created, then prunes the
// now-empty job directories. It never removes a non-empty directory.
func (m *Manager) removeJobFiles(job *Job) {
	for _, f := range job.Files {
		if !pathsafe.Within(m.baseDir, f) {
			continue
		}
		_ = os.Remove(f)
		_ = os.Remove(f + ".part")
	}
	m.removeIfEmpty(job.SavePath)
	m.removeIfEmpty(filepath.Dir(job.SavePath))
}

func (m *Manager) removeIfEmpty(dir string) {
	dir = filepath.Clean(dir)
	for _, protected := range []string{m.baseDir, filepath.Join(m.baseDir, "series"), filepath.Join(m.baseDir, "movies")} {
		if dir == filepath.Clean(protected) {
			return
		}
	}
	if dir == "" || !pathsafe.Within(m.baseDir, dir) {
		return
	}
	_ = os.Remove(dir) // fails harmlessly when the directory is not empty
}
