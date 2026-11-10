package queue

import (
	"os"
	"path/filepath"
	"testing"

	"media-stream-bridge/internal/domain"
)

func TestValidateStreamURL(t *testing.T) {
	bad := []string{
		"-i http://x/y.m3u8",
		"--help",
		"file:///etc/passwd",
		"concat:http://a|file:///etc/passwd",
		"ftp://example.com/x",
		"http://",
		"http://127.0.0.1/x.m3u8",
		"http://10.0.0.5/x.m3u8",
		"http://192.168.1.37:8989/x",
		"http://169.254.169.254/latest",
		"http://[::1]/x",
		"http://localhost/x",
	}
	for _, u := range bad {
		if validateStreamURL(u) == nil {
			t.Errorf("expected %q to be rejected", u)
		}
	}
	for _, u := range []string{"https://cdn.example.com/a/b.m3u8?x=1", "http://example.org/v.mp4"} {
		if err := validateStreamURL(u); err != nil {
			t.Errorf("%q rejected: %v", u, err)
		}
	}
}

func TestFFmpegArgsHardening(t *testing.T) {
	args := ffmpegArgs("https://x/y.m3u8", "/tmp/o.part")
	want := map[string]bool{"-nostdin": false, "-protocol_whitelist": false, "-rw_timeout": false}
	for _, a := range args {
		if _, ok := want[a]; ok {
			want[a] = true
		}
	}
	for k, v := range want {
		if !v {
			t.Errorf("missing %s", k)
		}
	}
}

func TestDeleteJobOnlyRemovesTrackedFiles(t *testing.T) {
	base := t.TempDir()
	sibling := filepath.Join(t.TempDir(), "library") // outside base
	if err := os.MkdirAll(sibling, 0o755); err != nil {
		t.Fatal(err)
	}
	precious := filepath.Join(sibling, "keep.mp4")
	_ = os.WriteFile(precious, []byte("x"), 0o644)

	m := &Manager{jobs: map[string]*Job{}, baseDir: base}
	m.jobs["h1"] = &Job{Hash: "h1", Title: "evil", SavePath: sibling}
	m.queueOrder = []string{"h1"}
	m.DeleteJob("h1", true)
	if _, err := os.Stat(precious); err != nil {
		t.Fatalf("file outside base was removed: %v", err)
	}

	// A legitimate job: tracked file and .part go away, empty dirs are pruned,
	// protected roots stay.
	dir := filepath.Join(base, "series", "Show", "Season 01")
	_ = os.MkdirAll(dir, 0o755)
	f := filepath.Join(dir, "Show - S01E01.mp4")
	_ = os.WriteFile(f, []byte("x"), 0o644)
	_ = os.WriteFile(f+".part", []byte("x"), 0o644)
	other := filepath.Join(dir, "unrelated.nfo")
	_ = os.WriteFile(other, []byte("x"), 0o644)
	m.jobs["h2"] = &Job{Hash: "h2", SavePath: dir, Files: []string{f}}
	m.queueOrder = []string{"h2"}
	m.DeleteJob("h2", true)
	if _, err := os.Stat(f); !os.IsNotExist(err) {
		t.Error("tracked file not deleted")
	}
	if _, err := os.Stat(other); err != nil {
		t.Error("untracked file was deleted")
	}

	_ = os.Remove(other)
	m.jobs["h3"] = &Job{Hash: "h3", SavePath: dir}
	m.queueOrder = []string{"h3"}
	m.DeleteJob("h3", true)
	if _, err := os.Stat(filepath.Join(base, "series")); err != nil {
		t.Error("series root must never be removed")
	}
	if _, err := os.Stat(base); err != nil {
		t.Error("base dir must never be removed")
	}
	if _, err := os.Stat(filepath.Join(base, "series", "Show")); !os.IsNotExist(err) {
		t.Error("empty series dir should be pruned")
	}
}

func TestAddJobLimit(t *testing.T) {
	m := &Manager{jobs: map[string]*Job{}, maxJobs: 1}
	if _, err := m.AddJob("a", "bridge://x/a", "tv", "/tmp/a", &domain.MediaItem{}); err != nil {
		t.Fatal(err)
	}
	if _, err := m.AddJob("b", "bridge://x/b", "tv", "/tmp/b", nil); err != ErrQueueFull {
		t.Fatalf("want ErrQueueFull, got %v", err)
	}
	if _, err := m.AddJob("a", "bridge://x/a", "tv", "/tmp/a", nil); err != nil {
		t.Fatalf("duplicate should return existing: %v", err)
	}
}

func TestEpisodeFileNameSanitized(t *testing.T) {
	m := &Manager{}
	job := &Job{Category: "movie", Media: &domain.MediaItem{Title: "../../etc/passwd", Year: 2020}}
	name := m.episodeFileName(job, domain.Episode{})
	if filepath.Base(name) != name || filepath.Ext(name) != ".mp4" {
		t.Fatalf("unsafe file name %q", name)
	}
}
