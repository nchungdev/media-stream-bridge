package domain

import "time"

// Episode represents an individual media episode/stream item.
type Episode struct {
	Slug      string `json:"slug"`
	Name      string `json:"name"`
	StreamURL string `json:"stream_url"`
	EmbedURL  string `json:"embed_url,omitempty"`
	Quality   string `json:"quality,omitempty"`
}

// MediaItem represents a parsed movie or show from any provider.
type MediaItem struct {
	ID            string    `json:"id"`
	Title         string    `json:"title"`
	OriginalTitle string    `json:"original_title,omitempty"`
	Slug          string    `json:"slug"`
	Year          int       `json:"year,omitempty"`
	Category      string    `json:"category"` // "tv" or "movie"
	Provider      string    `json:"provider"` // "kkphim", "nguonc", "fshare", etc.
	PosterURL     string    `json:"poster_url,omitempty"`
	Episodes      []Episode `json:"episodes,omitempty"`
	TotalEpisodes int       `json:"total_episodes,omitempty"`
	SizeBytes     int64     `json:"size_bytes,omitempty"`
	CreatedAt     time.Time `json:"created_at"`
}

// SearchResult is a normalized item ready for Torznab XML emission.
type SearchResult struct {
	Title       string    `json:"title"`
	DownloadURL string    `json:"download_url"`
	InfoURL     string    `json:"info_url,omitempty"`
	Category    string    `json:"category"` // "tv" or "movie"
	SizeBytes   int64     `json:"size_bytes"`
	PublishDate time.Time `json:"publish_date"`
	Provider    string    `json:"provider"`
}
