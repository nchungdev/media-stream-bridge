package donghuaweb

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"media-stream-bridge/internal/domain"
)

type Provider struct {
	client  *http.Client
	baseURL string
}

func New() *Provider {
	return &Provider{
		client:  &http.Client{Timeout: 8 * time.Second},
		baseURL: "https://yanhh3d.ee",
	}
}

func (p *Provider) Name() string {
	return "YanHH3D"
}

var (
	reSearchResult = regexp.MustCompile(`<a[^>]+href=["'](https://yanhh3d\.ee/[^"']+)["'][^>]*title=["']([^"']+)["']`)
	reEpisodeLink  = regexp.MustCompile(`https://yanhh3d\.ee/co-trang/[^/]+/tap-(\d+)\.html`)
	rePlayerIframe = regexp.MustCompile(`src=["'](https://streamfree\.vip/embed/v[t]?/[^"']+)["']`)
)

func (p *Provider) Search(ctx context.Context, query string) ([]domain.SearchResult, error) {
	reqURL := fmt.Sprintf("%s/?s=%s", p.baseURL, url.QueryEscape(query))
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64)")

	resp, err := p.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	html := string(bodyBytes)

	matches := reSearchResult.FindAllStringSubmatch(html, -1)
	seen := make(map[string]bool)
	var results []domain.SearchResult

	for _, m := range matches {
		link := m[1]
		title := m[2]
		if seen[link] || !strings.Contains(link, "/co-trang/") && !strings.Contains(link, "/tu-tien/") && !strings.Contains(link, "/tien-hiep/") {
			continue
		}
		seen[link] = true

		slug := link[strings.LastIndex(link, "/")+1:]
		cleanTitle := fmt.Sprintf("%s [YanHH3D Vietsub]", title)

		results = append(results, domain.SearchResult{
			Title:       cleanTitle,
			DownloadURL: fmt.Sprintf("bridge://yanhh3d/%s", slug),
			InfoURL:     link,
			Category:    "tv",
			SizeBytes:   1073741824,
			PublishDate: time.Now(),
			Provider:    p.Name(),
		})
	}
	return results, nil
}

func (p *Provider) GetMedia(ctx context.Context, slug string) (*domain.MediaItem, error) {
	// Try finding the category path
	for _, cat := range []string{"co-trang", "tu-tien", "tien-hiep"} {
		pageURL := fmt.Sprintf("%s/%s/%s", p.baseURL, cat, slug)
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, pageURL, nil)
		if err != nil {
			continue
		}
		req.Header.Set("User-Agent", "Mozilla/5.0")

		resp, err := p.client.Do(req)
		if err != nil || resp.StatusCode != http.StatusOK {
			if resp != nil {
				resp.Body.Close()
			}
			continue
		}

		bodyBytes, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		if err != nil {
			continue
		}
		html := string(bodyBytes)

		// Parse episodes
		epMatches := reEpisodeLink.FindAllStringSubmatch(html, -1)
		epMap := make(map[int]string)
		for _, em := range epMatches {
			num, _ := strconv.Atoi(em[1])
			epMap[num] = em[0]
		}

		var epNums []int
		for n := range epMap {
			epNums = append(epNums, n)
		}
		sort.Ints(epNums)

		item := &domain.MediaItem{
			ID:            slug,
			Title:         slug,
			Slug:          slug,
			Category:      "tv",
			Provider:      p.Name(),
			TotalEpisodes: len(epNums),
			CreatedAt:     time.Now(),
		}

		for _, n := range epNums {
			epURL := epMap[n]
			item.Episodes = append(item.Episodes, domain.Episode{
				Slug:      fmt.Sprintf("tap-%d", n),
				Name:      fmt.Sprintf("Tập %d", n),
				EmbedURL:  epURL,
				Quality:   "1080p",
			})
		}

		return item, nil
	}

	return nil, fmt.Errorf("media not found on YanHH3D: %s", slug)
}
