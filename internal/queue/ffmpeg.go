package queue

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/url"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

// validateStreamURL accepts only plain http(s) URLs that do not point at
// loopback, private or link-local literal addresses.
func validateStreamURL(raw string) error {
	if strings.HasPrefix(strings.TrimSpace(raw), "-") {
		return errors.New("stream URL must not start with '-'")
	}
	u, err := url.Parse(raw)
	if err != nil {
		return fmt.Errorf("invalid stream URL: %w", err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return fmt.Errorf("unsupported stream URL scheme %q", u.Scheme)
	}
	host := u.Hostname()
	if host == "" {
		return errors.New("stream URL has no host")
	}
	if strings.EqualFold(host, "localhost") {
		return errors.New("stream URL host is not allowed")
	}
	if ip := net.ParseIP(host); ip != nil &&
		(ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() ||
			ip.IsLinkLocalMulticast() || ip.IsUnspecified() || ip.IsMulticast()) {
		return errors.New("stream URL host is not allowed")
	}
	return nil
}

func ffmpegArgs(streamURL, tmpFile string) []string {
	return []string{
		"-nostdin",
		"-protocol_whitelist", "http,https,tls,tcp,crypto",
		"-rw_timeout", "30000000",
		"-headers", "User-Agent: Mozilla/5.0 (Windows NT 10.0; Win64; x64)\r\nReferer: https://phimapi.com/\r\n",
		"-i", streamURL,
		"-c", "copy",
		"-bsf:a", "aac_adtstoasc",
		"-f", "mp4",
		"-progress", "pipe:1",
		"-y", tmpFile,
	}
}

func (m *Manager) downloadEpisodeWithFFmpeg(ctx context.Context, job *Job, streamURL, outPath string, epIdx, totalCount, completedCount int) error {
	if err := validateStreamURL(streamURL); err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, m.episodeTO)
	defer cancel()

	tmpFile := outPath + ".part"
	_ = os.Remove(tmpFile)

	cmd := exec.CommandContext(ctx, "ffmpeg", ffmpegArgs(streamURL, tmpFile)...)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	if err := cmd.Start(); err != nil {
		return err
	}

	done := make(chan struct{})
	go func() {
		defer close(done)
		m.trackProgress(stdout, job, epIdx, totalCount, completedCount)
	}()
	<-done // the pipe must be drained before Wait closes it

	if err := cmd.Wait(); err != nil {
		_ = os.Remove(tmpFile)
		if ctx.Err() == context.DeadlineExceeded {
			return fmt.Errorf("episode timed out after %s", m.episodeTO)
		}
		return err
	}
	return os.Rename(tmpFile, outPath)
}

// trackProgress parses ffmpeg's "-progress pipe:1" output into job progress.
func (m *Manager) trackProgress(r io.Reader, job *Job, epIdx, totalCount, completedCount int) {
	scanner := bufio.NewScanner(r)
	lastUpdate := time.Now()
	var lastOutBytes int64

	for scanner.Scan() {
		k, v, ok := strings.Cut(scanner.Text(), "=")
		if !ok || strings.TrimSpace(k) != "total_size" {
			continue
		}
		curBytes, err := strconv.ParseInt(strings.TrimSpace(v), 10, 64)
		now := time.Now()
		dur := now.Sub(lastUpdate).Seconds()
		if err != nil || curBytes <= 0 || dur < 1.0 {
			continue
		}
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
