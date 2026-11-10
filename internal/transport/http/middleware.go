package http

import (
	"log"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/go-chi/chi/v5/middleware"
)

const maxBodyBytes = 1 << 20

var reAPIKey = regexp.MustCompile(`(?i)(apikey=)[^&\s"]*`)

// redactAPIKey masks any apikey=... value so credentials never reach the logs.
func redactAPIKey(s string) string {
	return reAPIKey.ReplaceAllString(s, "${1}REDACTED")
}

// accessLog logs each request without the apikey query value.
func accessLog(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		ww := middleware.NewWrapResponseWriter(w, r.ProtoMajor)
		next.ServeHTTP(ww, r)
		status := ww.Status()
		if status == 0 {
			status = http.StatusOK
		}
		log.Printf("%s %s %d %dB %s", r.Method, redactAPIKey(r.URL.RequestURI()), status, ww.BytesWritten(), time.Since(start).Round(time.Millisecond))
	})
}

// limitBody caps request bodies on state-changing methods.
func limitBody(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			r.Body = http.MaxBytesReader(w, r.Body, maxBodyBytes)
		}
		next.ServeHTTP(w, r)
	})
}

// sameOrigin rejects POSTs whose Origin or Referer names another host.
// Sonarr and Radarr send neither header, so they are unaffected.
func sameOrigin(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			for _, h := range []string{"Origin", "Referer"} {
				v := r.Header.Get(h)
				if v == "" {
					continue
				}
				u, err := url.Parse(v)
				if err != nil || !strings.EqualFold(u.Host, r.Host) {
					http.Error(w, "Forbidden", http.StatusForbidden)
					return
				}
			}
		}
		next.ServeHTTP(w, r)
	})
}

const dashboardCSP = "default-src 'self'; script-src 'self' 'unsafe-inline' https://cdn.tailwindcss.com; " +
	"style-src 'self' 'unsafe-inline'; img-src 'self' data:; connect-src 'self'; " +
	"frame-ancestors 'none'; base-uri 'none'; form-action 'self'"

func setDashboardHeaders(w http.ResponseWriter) {
	w.Header().Set("X-Frame-Options", "DENY")
	w.Header().Set("Content-Security-Policy", dashboardCSP)
	w.Header().Set("X-Content-Type-Options", "nosniff")
}
