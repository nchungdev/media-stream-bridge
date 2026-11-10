package service

import (
	"context"
	"testing"
	"time"

	"media-stream-bridge/internal/domain"
)

type fakeProvider struct{ got string }

func (f *fakeProvider) Name() string { return "Fake" }
func (f *fakeProvider) Search(context.Context, string) ([]domain.SearchResult, error) {
	return nil, nil
}
func (f *fakeProvider) GetMedia(_ context.Context, slug string) (*domain.MediaItem, error) {
	f.got = slug
	return &domain.MediaItem{Slug: slug}, nil
}

func TestGetMediaByURISlugValidation(t *testing.T) {
	p := &fakeProvider{}
	agg := NewAggregator(time.Second, p)
	for _, uri := range []string{
		"bridge://fake/../x", "bridge://fake/..", "bridge://fake/a/b",
		"bridge://fake/.hidden", "bridge://fake/a%2Fb", "bridge://fake/",
	} {
		if _, err := agg.GetMediaByURI(context.Background(), uri); err == nil {
			t.Errorf("%q should be rejected", uri)
		}
	}
	if _, err := agg.GetMediaByURI(context.Background(), "bridge://fake/some-show_2.0"); err != nil || p.got != "some-show_2.0" {
		t.Fatalf("valid slug rejected: %v", err)
	}
}
