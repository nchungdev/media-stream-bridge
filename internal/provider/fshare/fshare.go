package fshare

import (
	"context"
	"fmt"
	"strings"
	"time"

	"media-stream-bridge/internal/domain"
)

type Provider struct{}

func New() *Provider {
	return &Provider{}
}

func (p *Provider) Name() string {
	return "Fshare"
}

func (p *Provider) Search(ctx context.Context, query string) ([]domain.SearchResult, error) {
	// If the user pastes an fshare URL or requests Fshare
	if strings.Contains(query, "fshare.vn") {
		return []domain.SearchResult{
			{
				Title:       fmt.Sprintf("Fshare Direct Release: %s", query),
				DownloadURL: query,
				Category:    "tv",
				SizeBytes:   2147483648,
				PublishDate: time.Now(),
				Provider:    p.Name(),
			},
		}, nil
	}
	return nil, nil
}

func (p *Provider) GetMedia(ctx context.Context, slug string) (*domain.MediaItem, error) {
	return nil, fmt.Errorf("fshare media resolution handled via aria2/webhelper")
}
