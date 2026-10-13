package nguonc

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"time"

	"media-stream-bridge/internal/domain"
	"media-stream-bridge/internal/formatter"
)

type Provider struct {
	client  *http.Client
	baseURL string
}

func New() *Provider {
	return &Provider{
		client:  &http.Client{Timeout: 6 * time.Second},
		baseURL: "https://phim.nguonc.com",
	}
}

func (p *Provider) Name() string {
	return "NguonC"
}

type searchResponse struct {
	Status string `json:"status"`
	Items  []struct {
		Name         string `json:"name"`
		Slug         string `json:"slug"`
		OriginalName string `json:"original_name"`
		Year         int    `json:"year"`
		CurrentEp    string `json:"current_episode"`
	} `json:"items"`
}

func (p *Provider) Search(ctx context.Context, query string) ([]domain.SearchResult, error) {
	reqURL := fmt.Sprintf("%s/api/films/search?keyword=%s", p.baseURL, url.QueryEscape(query))
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0")

	resp, err := p.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("nguonc search status: %d", resp.StatusCode)
	}

	var data searchResponse
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		return nil, err
	}

	var results []domain.SearchResult
	for _, it := range data.Items {
		cat := "tv"
		if it.CurrentEp == "FULL" || it.CurrentEp == "Full" {
			cat = "movie"
		}
		title := formatter.BuildReleaseTitle(it.Name, it.OriginalName, query, it.Year, 0, "1080p WEB-DL", p.Name())
		results = append(results, domain.SearchResult{
			Title:       title,
			DownloadURL: fmt.Sprintf("bridge://nguonc/%s", it.Slug),
			InfoURL:     fmt.Sprintf("%s/phim/%s", p.baseURL, it.Slug),
			Category:    cat,
			SizeBytes:   1073741824,
			PublishDate: time.Now(),
			Provider:    p.Name(),
		})
	}
	return results, nil
}

type filmResponse struct {
	Status string `json:"status"`
	Movie  struct {
		Name          string `json:"name"`
		OriginalName  string `json:"original_name"`
		Slug          string `json:"slug"`
		Year          int    `json:"year"`
		TotalEpisodes int    `json:"total_episodes"`
		CurrentEp     string `json:"current_episode"`
		Episodes      []struct {
			ServerName string `json:"server_name"`
			Items      []struct {
				Name  string `json:"name"`
				Slug  string `json:"slug"`
				Embed string `json:"embed"`
				M3u8  string `json:"m3u8"`
			} `json:"items"`
		} `json:"episodes"`
	} `json:"movie"`
}

func (p *Provider) GetMedia(ctx context.Context, slug string) (*domain.MediaItem, error) {
	reqURL := fmt.Sprintf("%s/api/film/%s", p.baseURL, slug)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0")

	resp, err := p.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("nguonc detail status: %d", resp.StatusCode)
	}

	var data filmResponse
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		return nil, err
	}

	item := &domain.MediaItem{
		ID:            data.Movie.Slug,
		Title:         data.Movie.Name,
		OriginalTitle: data.Movie.OriginalName,
		Slug:          data.Movie.Slug,
		Year:          data.Movie.Year,
		Category:      "tv",
		Provider:      p.Name(),
		TotalEpisodes: data.Movie.TotalEpisodes,
		CreatedAt:     time.Now(),
	}

	for _, sv := range data.Movie.Episodes {
		for _, ep := range sv.Items {
			item.Episodes = append(item.Episodes, domain.Episode{
				Slug:      ep.Slug,
				Name:      ep.Name,
				StreamURL: ep.M3u8,
				EmbedURL:  ep.Embed,
				Quality:   "1080p",
			})
		}
		if len(item.Episodes) > 0 {
			break
		}
	}

	return item, nil
}
