package provider

import (
	"context"
	"media-stream-bridge/internal/domain"
)

// Provider is the common interface implemented by all media sources.
type Provider interface {
	Name() string
	Search(ctx context.Context, query string) ([]domain.SearchResult, error)
	GetMedia(ctx context.Context, slug string) (*domain.MediaItem, error)
}
