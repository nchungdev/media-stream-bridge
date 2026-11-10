package http

import (
	"encoding/xml"
	"log"
	"net/http"
	"strings"
)

// dummyTorrent is the placeholder payload returned for grabs; the real work
// is queued when /torznab/download is hit, so *arr only needs a 200 response.
const dummyTorrent = "d8:announce0:e"

type torznabError struct {
	XMLName     xml.Name `xml:"error"`
	Code        int      `xml:"code,attr"`
	Description string   `xml:"description,attr"`
}

func writeTorznabError(w http.ResponseWriter, code int, desc string) {
	out, _ := xml.Marshal(torznabError{Code: code, Description: desc})
	w.Header().Set("Content-Type", "application/xml; charset=utf-8")
	_, _ = w.Write(append([]byte(xml.Header), out...))
}

func (s *Server) handleTorznab(w http.ResponseWriter, r *http.Request) {
	tMode := strings.ToLower(r.URL.Query().Get("t"))
	if tMode == "" {
		tMode = "caps"
	}

	if tMode == "caps" {
		capsBytes, err := GetCapsXML()
		if err != nil {
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/xml; charset=utf-8")
		_, _ = w.Write(capsBytes)
		return
	}

	query := r.URL.Query().Get("q")
	// If Sonarr queries by episode/season, check query string
	if query == "" {
		query = r.URL.Query().Get("title")
	}

	results := s.aggregator.SearchAll(r.Context(), query)
	rssBytes, err := GenerateRSSXML(results, strings.TrimRight(s.cfg.PublicURL, "/"), s.cfg.APIKey)
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/xml; charset=utf-8")
	_, _ = w.Write(rssBytes)
}

func (s *Server) handleTorznabDownload(w http.ResponseWriter, r *http.Request) {
	rawURL := r.URL.Query().Get("url")
	cat := r.URL.Query().Get("cat")
	title := r.URL.Query().Get("title")

	if rawURL == "" {
		http.Error(w, "Missing URL", http.StatusBadRequest)
		return
	}

	log.Printf("[Torznab Grab] Grabbed item: %s, url: %s, cat: %s", title, redactAPIKey(rawURL), cat)

	if err := s.dispatchJob(r.Context(), rawURL, title, cat, ""); err != nil {
		log.Printf("[Torznab Grab] Failed to queue %q: %v", title, err)
		http.Error(w, "could not queue download", http.StatusBadGateway)
		return
	}

	w.Header().Set("Content-Type", "application/x-bittorrent")
	_, _ = w.Write([]byte(dummyTorrent))
}
