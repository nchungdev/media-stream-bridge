package kkphim

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
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
		baseURL: "https://phimapi.com",
	}
}

func (p *Provider) Name() string {
	return "KKPhim"
}

type searchResponse struct {
	Status bool `json:"status"`
	Data   struct {
		Items []struct {
			ID            string `json:"_id"`
			Name          string `json:"name"`
			Slug          string `json:"slug"`
			OriginalName  string `json:"origin_name"`
			Year          int    `json:"year"`
			PosterURL     string `json:"poster_url"`
			ThumbURL      string `json:"thumb_url"`
			Type          string `json:"type"`
		} `json:"items"`
	} `json:"data"`
}

func (p *Provider) Search(ctx context.Context, query string) ([]domain.SearchResult, error) {
	reqURL := fmt.Sprintf("%s/v1/api/tim-kiem?keyword=%s", p.baseURL, url.QueryEscape(query))
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
		return nil, fmt.Errorf("kkphim search status: %d", resp.StatusCode)
	}

	var data searchResponse
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		return nil, err
	}

	var results []domain.SearchResult
	for _, it := range data.Data.Items {
		cat := "movie"
		if it.Type == "series" || it.Type == "hoathinh" || strings.Contains(strings.ToLower(it.Name), "phần") {
			cat = "tv"
		}
		title := formatter.BuildReleaseTitle(it.Name, it.OriginalName, query, it.Year, 0, "1080p WEB-DL", p.Name())
		results = append(results, domain.SearchResult{
			Title:       title,
			DownloadURL: fmt.Sprintf("bridge://kkphim/%s", it.Slug),
			InfoURL:     fmt.Sprintf("%s/phim/%s", p.baseURL, it.Slug),
			Category:    cat,
			SizeBytes:   1073741824, // 1GB baseline
			PublishDate: time.Now(),
			Provider:    p.Name(),
		})
	}
	return results, nil
}

type detailResponse struct {
	Movie struct {
		Name         string `json:"name"`
		OriginName   string `json:"origin_name"`
		Slug         string `json:"slug"`
		Year         int    `json:"year"`
		EpisodeTotal any    `json:"episode_total"`
		Type         string `json:"type"`
	} `json:"movie"`
	Episodes []struct {
		ServerName string `json:"server_name"`
		ServerData []struct {
			Name      string `json:"name"`
			Slug      string `json:"slug"`
			LinkM3u8  string `json:"link_m3u8"`
			LinkEmbed string `json:"link_embed"`
		} `json:"server_data"`
	} `json:"episodes"`
}

func (p *Provider) GetMedia(ctx context.Context, slug string) (*domain.MediaItem, error) {
	reqURL := fmt.Sprintf("%s/phim/%s", p.baseURL, slug)
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
		return nil, fmt.Errorf("kkphim detail status: %d", resp.StatusCode)
	}

	var detail detailResponse
	if err := json.NewDecoder(resp.Body).Decode(&detail); err != nil {
		return nil, err
	}

	cat := "movie"
	if detail.Movie.Type == "series" || detail.Movie.Type == "hoathinh" {
		cat = "tv"
	}

	totalEps := 0
	switch v := detail.Movie.EpisodeTotal.(type) {
	case float64:
		totalEps = int(v)
	case int:
		totalEps = v
	case string:
		totalEps, _ = strconv.Atoi(v)
	}
	item := &domain.MediaItem{
		ID:            detail.Movie.Slug,
		Title:         detail.Movie.Name,
		OriginalTitle: detail.Movie.OriginName,
		Slug:          detail.Movie.Slug,
		Year:          detail.Movie.Year,
		Category:      cat,
		Provider:      p.Name(),
		TotalEpisodes: totalEps,
		CreatedAt:     time.Now(),
	}

	for _, sv := range detail.Episodes {
		for _, ep := range sv.ServerData {
			item.Episodes = append(item.Episodes, domain.Episode{
				Slug:      ep.Slug,
				Name:      ep.Name,
				StreamURL: ep.LinkM3u8,
				EmbedURL:  ep.LinkEmbed,
				Quality:   "1080p",
			})
		}
		if len(item.Episodes) > 0 {
			break // First complete server is preferred
		}
	}

	return item, nil
}
