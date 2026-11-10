package http

import (
	"bytes"
	"context"
	"io"
	"log"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"media-stream-bridge/internal/config"
	"media-stream-bridge/internal/domain"
	"media-stream-bridge/internal/queue"
	"media-stream-bridge/internal/service"
)

const (
	testUser   = "sonarr"
	testPass   = "correct-horse-battery"
	testAPIKey = "0123456789abcdef01234567"
)

type fakeProvider struct{}

func (fakeProvider) Name() string { return "Fake" }
func (fakeProvider) Search(context.Context, string) ([]domain.SearchResult, error) {
	return []domain.SearchResult{{
		Title: "Show <b>&", DownloadURL: "bridge://fake/show", Category: "tv", Provider: "Fake",
		PublishDate: time.Unix(0, 0),
	}}, nil
}
func (fakeProvider) GetMedia(_ context.Context, slug string) (*domain.MediaItem, error) {
	return &domain.MediaItem{Title: "Show", Slug: slug, Category: "tv"}, nil
}

func newTestServer(t *testing.T) (*Server, string) {
	t.Helper()
	root := t.TempDir()
	cfg := config.Default()
	cfg.Storage.DefaultDownloadDir = root
	cfg.QBitUsername, cfg.QBitPassword, cfg.APIKey = testUser, testPass, testAPIKey
	cfg.PublicURL = "http://127.0.0.1:6890"
	qm := queue.NewManager(queue.Options{MaxJobs: 10, BaseDir: root})
	agg := service.NewAggregator(time.Second, fakeProvider{})
	return NewServer(cfg, agg, qm), root
}

func do(s *Server, req *http.Request) *httptest.ResponseRecorder {
	if req.RemoteAddr == "" {
		req.RemoteAddr = "203.0.113.9:1234"
	}
	rec := httptest.NewRecorder()
	s.Router().ServeHTTP(rec, req)
	return rec
}

func postForm(path string, v url.Values) *http.Request {
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(v.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	return req
}

func login(t *testing.T, s *Server) *http.Cookie {
	t.Helper()
	rec := do(s, postForm("/api/v2/auth/login", url.Values{"username": {testUser}, "password": {testPass}}))
	if rec.Body.String() != "Ok." {
		t.Fatalf("login body = %q", rec.Body.String())
	}
	for _, c := range rec.Result().Cookies() {
		if c.Name == "SID" {
			if !c.HttpOnly || c.SameSite != http.SameSiteStrictMode || len(c.Value) != 64 {
				t.Fatalf("weak cookie: %+v", c)
			}
			return c
		}
	}
	t.Fatal("no SID cookie")
	return nil
}

func TestLoginOKBadAndLockout(t *testing.T) {
	s, _ := newTestServer(t)
	login(t, s)

	bad := url.Values{"username": {testUser}, "password": {"nope"}}
	for i := 0; i < 5; i++ {
		rec := do(s, postForm("/api/v2/auth/login", bad))
		if rec.Code != http.StatusOK || rec.Body.String() != "Fails." {
			t.Fatalf("attempt %d: %d %q", i, rec.Code, rec.Body.String())
		}
	}
	rec := do(s, postForm("/api/v2/auth/login", url.Values{"username": {testUser}, "password": {testPass}}))
	if rec.Code != http.StatusForbidden {
		t.Fatalf("locked IP should get 403, got %d", rec.Code)
	}
	// A different IP is unaffected.
	req := postForm("/api/v2/auth/login", url.Values{"username": {testUser}, "password": {testPass}})
	req.RemoteAddr = "198.51.100.1:9"
	if do(s, req).Body.String() != "Ok." {
		t.Fatal("other IP should still log in")
	}
	// Lockout expires.
	s.auth.now = func() time.Time { return time.Now().Add(16 * time.Minute) }
	if do(s, postForm("/api/v2/auth/login", url.Values{"username": {testUser}, "password": {testPass}})).Body.String() != "Ok." {
		t.Fatal("lockout should expire")
	}
}

func TestSessionsDifferPerLogin(t *testing.T) {
	s, _ := newTestServer(t)
	if login(t, s).Value == login(t, s).Value {
		t.Fatal("SIDs must differ")
	}
}

func TestProtectedRoutesRequireAuth(t *testing.T) {
	s, _ := newTestServer(t)
	cases := []struct{ method, path string }{
		{"GET", "/api/v2/torrents/info"},
		{"POST", "/api/v2/torrents/add"},
		{"POST", "/api/v2/torrents/delete"},
		{"GET", "/api/queue"},
		{"GET", "/dashboard"},
		{"GET", "/"},
	}
	for _, c := range cases {
		rec := do(s, httptest.NewRequest(c.method, c.path, nil))
		if rec.Code != http.StatusForbidden {
			t.Errorf("%s %s = %d, want 403", c.method, c.path, rec.Code)
		}
	}
	if rec := do(s, httptest.NewRequest("GET", "/dashboard", nil)); rec.Header().Get("WWW-Authenticate") == "" {
		t.Error("HTML routes need WWW-Authenticate")
	}
	if rec := do(s, httptest.NewRequest("GET", "/api/status", nil)); rec.Code != http.StatusOK {
		t.Errorf("status must be public, got %d", rec.Code)
	}
}

func TestValidSessionAndBasicAuthPass(t *testing.T) {
	s, _ := newTestServer(t)
	req := httptest.NewRequest("GET", "/api/v2/torrents/info", nil)
	req.AddCookie(login(t, s))
	if rec := do(s, req); rec.Code != http.StatusOK {
		t.Fatalf("SID: %d", rec.Code)
	}
	req = httptest.NewRequest("GET", "/dashboard", nil)
	req.SetBasicAuth(testUser, testPass)
	rec := do(s, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("basic: %d", rec.Code)
	}
	if rec.Header().Get("X-Frame-Options") != "DENY" || !strings.Contains(rec.Header().Get("Content-Security-Policy"), "frame-ancestors 'none'") {
		t.Error("dashboard security headers missing")
	}
	req = httptest.NewRequest("GET", "/dashboard", nil)
	req.SetBasicAuth(testUser, "wrong")
	if do(s, req).Code != http.StatusForbidden {
		t.Error("wrong basic auth must fail")
	}
}

func TestTorznabAuth(t *testing.T) {
	s, _ := newTestServer(t)
	rec := do(s, httptest.NewRequest("GET", "/torznab/api?t=caps", nil))
	if !strings.Contains(rec.Body.String(), `<error code="100" description="Invalid API Key">`) {
		t.Fatalf("want torznab error, got %q", rec.Body.String())
	}
	rec = do(s, httptest.NewRequest("GET", "/torznab/api?t=caps&apikey=wrong-key", nil))
	if !strings.Contains(rec.Body.String(), "<error") {
		t.Fatal("wrong key must fail")
	}
	rec = do(s, httptest.NewRequest("GET", "/torznab/api?t=search&q=x&apikey="+testAPIKey, nil))
	body := rec.Body.String()
	if !strings.Contains(body, "<rss") || strings.Contains(body, "<error") {
		t.Fatalf("valid key failed: %q", body)
	}
	if !strings.Contains(body, "http://127.0.0.1:6890/torznab/download?apikey="+testAPIKey) {
		t.Errorf("enclosure must use PublicURL and carry apikey: %q", body)
	}
	if !strings.Contains(body, "title=Show+%3Cb%3E%26") {
		t.Errorf("enclosure title must be url-encoded: %q", body)
	}
	req := httptest.NewRequest("GET", "/torznab/api?t=caps", nil)
	req.Header.Set("X-Api-Key", testAPIKey)
	if strings.Contains(do(s, req).Body.String(), "<error") {
		t.Error("X-Api-Key header should authenticate")
	}
}

func TestCrossOriginPostRejected(t *testing.T) {
	s, _ := newTestServer(t)
	cookie := login(t, s)
	for _, hdr := range []struct{ k, v string }{
		{"Origin", "https://evil.example"},
		{"Referer", "https://evil.example/page"},
	} {
		req := postForm("/api/v2/torrents/delete", url.Values{"hashes": {"x"}})
		req.Host = "bridge.local:6890"
		req.Header.Set(hdr.k, hdr.v)
		req.AddCookie(cookie)
		if rec := do(s, req); rec.Code != http.StatusForbidden {
			t.Errorf("%s: want 403, got %d", hdr.k, rec.Code)
		}
	}
	req := postForm("/api/v2/torrents/delete", url.Values{"hashes": {"x"}})
	req.Host = "bridge.local:6890"
	req.Header.Set("Origin", "http://bridge.local:6890")
	req.AddCookie(cookie)
	if rec := do(s, req); rec.Code != http.StatusOK {
		t.Errorf("same-origin POST: got %d", rec.Code)
	}
}

func TestAddSemantics(t *testing.T) {
	s, root := newTestServer(t)
	cookie := login(t, s)
	add := func(v url.Values) *httptest.ResponseRecorder {
		req := postForm("/api/v2/torrents/add", v)
		req.AddCookie(cookie)
		return do(s, req)
	}

	if rec := add(url.Values{"urls": {"magnet:?xt=urn:btih:abc"}}); rec.Code != http.StatusUnsupportedMediaType || rec.Body.String() != "Fails." {
		t.Errorf("magnet: %d %q", rec.Code, rec.Body.String())
	}
	if rec := add(url.Values{"urls": {"http://evil.example/a.torrent"}}); rec.Code != http.StatusUnsupportedMediaType {
		t.Errorf(".torrent url: %d", rec.Code)
	}
	if rec := add(url.Values{"urls": {strings.Repeat("bridge://fake/a\n", 51)}}); rec.Code != http.StatusRequestEntityTooLarge {
		t.Errorf("too many urls: %d", rec.Code)
	}
	if rec := add(url.Values{"urls": {"bridge://fake/show"}, "savepath": {"/srv/mergerfs/MainPool/Phim"}}); rec.Body.String() != "Ok." {
		t.Fatalf("bridge add: %q", rec.Body.String())
	}
	for _, j := range s.queueManager.GetAllJobs() {
		if !strings.HasPrefix(j.SavePath, root) {
			t.Errorf("save path escaped download dir: %s", j.SavePath)
		}
	}

	// Placeholder torrent upload is accepted, any other upload is not.
	upload := func(content string) *httptest.ResponseRecorder {
		var buf bytes.Buffer
		mw := multipart.NewWriter(&buf)
		fw, _ := mw.CreateFormFile("torrents", "x.torrent")
		_, _ = io.WriteString(fw, content)
		_ = mw.Close()
		req := httptest.NewRequest(http.MethodPost, "/api/v2/torrents/add", &buf)
		req.Header.Set("Content-Type", mw.FormDataContentType())
		req.AddCookie(cookie)
		return do(s, req)
	}
	if rec := upload(dummyTorrent); rec.Body.String() != "Ok." {
		t.Errorf("placeholder: %q", rec.Body.String())
	}
	if rec := upload("d4:infod4:name1:aee"); rec.Code != http.StatusUnsupportedMediaType {
		t.Errorf("real torrent: %d", rec.Code)
	}
}

func TestQueueFullAnswersFails(t *testing.T) {
	s, _ := newTestServer(t)
	s.queueManager = queue.NewManager(queue.Options{MaxJobs: 1, BaseDir: t.TempDir()})
	cookie := login(t, s)
	for i, want := range []string{"Ok.", "Fails."} {
		req := postForm("/api/v2/torrents/add", url.Values{"urls": {"bridge://fake/show" + string(rune('a'+i))}})
		req.AddCookie(cookie)
		if got := do(s, req).Body.String(); got != want {
			t.Errorf("add %d = %q, want %q", i, got, want)
		}
	}
}

func TestResolveDestDirStaysInside(t *testing.T) {
	s, root := newTestServer(t)
	for _, title := range []string{"../../etc", "..", "/abs/path", ""} {
		tv := s.resolveDestDir(true, title, "", "", "")
		mv := s.resolveDestDir(false, title, "", "", "")
		for _, p := range []string{tv, mv} {
			if rel, err := filepath.Rel(root, p); err != nil || strings.HasPrefix(rel, "..") {
				t.Errorf("title %q escaped: %s", title, p)
			}
		}
	}
	if got := s.resolveDestDir(true, "Show", "", "", "/srv/mergerfs/MainPool/Phim"); !strings.HasPrefix(got, root) {
		t.Errorf("outside savepath honoured: %s", got)
	}
	inside := filepath.Join(root, "series")
	if got := s.resolveDestDir(true, "Show", "", "", inside); got != inside {
		t.Errorf("inside savepath ignored: %s", got)
	}
}

func TestDashboardEscapesAndHasNoInlineHandlers(t *testing.T) {
	for _, want := range []string{"function esc(", "esc(j.title)", "esc(j.save_path)", "esc(j.hash)", "data-hash", "addEventListener"} {
		if !strings.Contains(dashboardHTML, want) {
			t.Errorf("dashboard missing %q", want)
		}
	}
	if strings.Contains(dashboardHTML, "onclick=") {
		t.Error("inline onclick handlers must be gone")
	}
}

func TestLogsNeverContainAPIKey(t *testing.T) {
	s, _ := newTestServer(t)
	var buf bytes.Buffer
	log.SetOutput(&buf)
	t.Cleanup(func() { log.SetOutput(os.Stderr) })

	do(s, httptest.NewRequest("GET", "/torznab/api?t=caps&apikey="+testAPIKey, nil))
	req := postForm("/api/v2/torrents/add", url.Values{"urls": {"http://h/torznab/download?url=bridge://fake/s&apikey=" + testAPIKey}})
	req.SetBasicAuth(testUser, testPass)
	do(s, req)
	if strings.Contains(buf.String(), testAPIKey) {
		t.Fatalf("api key leaked into logs:\n%s", buf.String())
	}
	if !strings.Contains(buf.String(), "REDACTED") {
		t.Fatal("expected redaction marker in logs")
	}
}

func TestBodyLimit(t *testing.T) {
	s, _ := newTestServer(t)
	big := url.Values{"username": {testUser}, "password": {strings.Repeat("a", 2<<20)}}
	if rec := do(s, postForm("/api/v2/auth/login", big)); rec.Code != http.StatusBadRequest {
		t.Fatalf("oversized body: %d", rec.Code)
	}
}
