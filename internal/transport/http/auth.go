package http

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"
)

const (
	sessionCookie   = "SID"
	sessionTTL      = 24 * time.Hour
	maxLoginFails   = 5
	lockoutDuration = 15 * time.Minute
)

type failRecord struct {
	count       int
	lockedUntil time.Time
}

// authStore holds qBittorrent-style sessions and per-IP failure counters.
type authStore struct {
	username, password, apiKey string

	mu       sync.Mutex
	sessions map[string]time.Time // SID -> expiry (sliding)
	fails    map[string]*failRecord
	now      func() time.Time
}

func newAuthStore(username, password, apiKey string) *authStore {
	return &authStore{
		username: username,
		password: password,
		apiKey:   apiKey,
		sessions: make(map[string]time.Time),
		fails:    make(map[string]*failRecord),
		now:      time.Now,
	}
}

func secureEqual(a, b string) bool {
	return subtle.ConstantTimeCompare([]byte(a), []byte(b)) == 1
}

func (a *authStore) checkCredentials(user, pass string) bool {
	// Evaluate both comparisons so timing does not reveal which one failed.
	u := secureEqual(user, a.username)
	p := secureEqual(pass, a.password)
	return u && p
}

func clientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

func (a *authStore) locked(ip string) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	rec := a.fails[ip]
	if rec == nil {
		return false
	}
	if !rec.lockedUntil.IsZero() && a.now().After(rec.lockedUntil) {
		delete(a.fails, ip)
		return false
	}
	return !rec.lockedUntil.IsZero()
}

func (a *authStore) recordFailure(ip string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	rec := a.fails[ip]
	if rec == nil {
		rec = &failRecord{}
		a.fails[ip] = rec
	}
	rec.count++
	if rec.count >= maxLoginFails {
		rec.lockedUntil = a.now().Add(lockoutDuration)
	}
}

func (a *authStore) clearFailures(ip string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	delete(a.fails, ip)
}

func (a *authStore) newSession() (string, error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	sid := hex.EncodeToString(buf)
	a.mu.Lock()
	defer a.mu.Unlock()
	now := a.now()
	for k, exp := range a.sessions {
		if now.After(exp) {
			delete(a.sessions, k)
		}
	}
	a.sessions[sid] = now.Add(sessionTTL)
	return sid, nil
}

// validSession checks the SID and slides its expiry forward.
func (a *authStore) validSession(sid string) bool {
	if sid == "" {
		return false
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	exp, ok := a.sessions[sid]
	if !ok || a.now().After(exp) {
		delete(a.sessions, sid)
		return false
	}
	a.sessions[sid] = a.now().Add(sessionTTL)
	return true
}

func (a *authStore) deleteSession(sid string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	delete(a.sessions, sid)
}

func (s *Server) handleQBitLogin(w http.ResponseWriter, r *http.Request) {
	ip := clientIP(r)
	if s.auth.locked(ip) {
		http.Error(w, "Forbidden", http.StatusForbidden)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "Bad Request", http.StatusBadRequest)
		return
	}
	if !s.auth.checkCredentials(r.PostForm.Get("username"), r.PostForm.Get("password")) {
		s.auth.recordFailure(ip)
		_, _ = w.Write([]byte("Fails."))
		return
	}
	sid, err := s.auth.newSession()
	if err != nil {
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}
	s.auth.clearFailures(ip)
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookie,
		Value:    sid,
		Path:     "/",
		HttpOnly: true,
		Secure:   r.TLS != nil,
		SameSite: http.SameSiteStrictMode,
		MaxAge:   int(sessionTTL.Seconds()),
	})
	_, _ = w.Write([]byte("Ok."))
}

func (s *Server) handleQBitLogout(w http.ResponseWriter, r *http.Request) {
	if c, err := r.Cookie(sessionCookie); err == nil {
		s.auth.deleteSession(c.Value)
	}
	http.SetCookie(w, &http.Cookie{Name: sessionCookie, Path: "/", MaxAge: -1, HttpOnly: true})
	_, _ = w.Write([]byte("Ok."))
}

// authenticate reports whether the request carries valid credentials, and
// whether it carried any credentials that turned out to be wrong.
func (s *Server) authenticate(r *http.Request) (ok, attempted bool) {
	if c, err := r.Cookie(sessionCookie); err == nil && s.auth.validSession(c.Value) {
		return true, false
	}
	if user, pass, has := r.BasicAuth(); has {
		if s.auth.checkCredentials(user, pass) {
			return true, false
		}
		attempted = true
	}
	key := r.URL.Query().Get("apikey")
	if key == "" {
		key = r.Header.Get("X-Api-Key")
	}
	if key != "" {
		if secureEqual(key, s.auth.apiKey) {
			return true, false
		}
		attempted = true
	}
	return false, attempted
}

func isTorznab(r *http.Request) bool {
	return strings.HasPrefix(r.URL.Path, "/torznab")
}

func isHTMLRoute(r *http.Request) bool {
	return r.URL.Path == "/" || r.URL.Path == "/dashboard"
}

// requireAuth admits requests with a valid session, Basic credentials or API key.
func (s *Server) requireAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ip := clientIP(r)
		if s.auth.locked(ip) {
			http.Error(w, "Forbidden", http.StatusForbidden)
			return
		}
		ok, attempted := s.authenticate(r)
		if ok {
			next.ServeHTTP(w, r)
			return
		}
		if attempted {
			s.auth.recordFailure(ip)
		}
		switch {
		case isTorznab(r):
			writeTorznabError(w, 100, "Invalid API Key")
		case isHTMLRoute(r):
			w.Header().Set("WWW-Authenticate", `Basic realm="Media Stream Bridge", charset="UTF-8"`)
			http.Error(w, "Forbidden", http.StatusForbidden)
		default:
			http.Error(w, "Forbidden", http.StatusForbidden)
		}
	})
}
