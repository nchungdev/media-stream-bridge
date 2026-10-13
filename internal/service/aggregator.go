package service

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"media-stream-bridge/internal/domain"
	"media-stream-bridge/internal/provider"
)

type Aggregator struct {
	providers []provider.Provider
	timeout   time.Duration
}

func NewAggregator(timeout time.Duration, providers ...provider.Provider) *Aggregator {
	return &Aggregator{
		providers: providers,
		timeout:   timeout,
	}
}

// SearchAll executes concurrent fan-out searches across all registered providers.
func (a *Aggregator) SearchAll(ctx context.Context, query string) []domain.SearchResult {
	ctx, cancel := context.WithTimeout(ctx, a.timeout)
	defer cancel()

	var wg sync.WaitGroup
	resultChan := make(chan []domain.SearchResult, len(a.providers))

	for _, p := range a.providers {
		wg.Add(1)
		go func(prov provider.Provider) {
			defer wg.Done()
			res, err := prov.Search(ctx, query)
			if err == nil && len(res) > 0 {
				resultChan <- res
			}
		}(p)
	}

	wg.Wait()
	close(resultChan)

	var combined []domain.SearchResult
	seen := make(map[string]bool)

	for list := range resultChan {
		for _, item := range list {
			key := fmt.Sprintf("%s|%s", item.Provider, item.Title)
			if !seen[key] {
				seen[key] = true
				combined = append(combined, item)
			}
		}
	}

	// Always provide at least a sample release if query is empty (for Torznab caps/sync)
	if len(combined) == 0 && (query == "" || strings.ToLower(query) == "sample") {
		combined = append(combined, domain.SearchResult{
			Title:       "Media.Stream.Bridge.VIP.Direct.Sample.Release",
			DownloadURL: "https://www.fshare.vn/file/SAMPLE",
			Category:    "tv",
			SizeBytes:   1073741824,
			PublishDate: time.Now(),
			Provider:    "Bridge",
		})
	}

	return combined
}

// GetMediaByURI parses bridge://<provider>/<slug> and fetches full media details.
func (a *Aggregator) GetMediaByURI(ctx context.Context, uri string) (*domain.MediaItem, error) {
	// Format: bridge://<provider>/<slug>
	if !strings.HasPrefix(uri, "bridge://") {
		return nil, fmt.Errorf("invalid bridge URI: %s", uri)
	}

	parts := strings.Split(strings.TrimPrefix(uri, "bridge://"), "/")
	if len(parts) < 2 {
		return nil, fmt.Errorf("malformed bridge URI: %s", uri)
	}

	provName := strings.ToLower(parts[0])
	slug := strings.Join(parts[1:], "/")

	for _, p := range a.providers {
		if strings.ToLower(p.Name()) == provName {
			return p.GetMedia(ctx, slug)
		}
	}

	return nil, fmt.Errorf("provider not found for: %s", provName)
}
