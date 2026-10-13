package http

import (
	"encoding/xml"
	"fmt"
	"time"

	"media-stream-bridge/internal/domain"
)

type ServerCaps struct {
	XMLName   xml.Name `xml:"caps"`
	Server    CapsServer `xml:"server"`
	Limits    Limits     `xml:"limits"`
	Searching Search     `xml:"searching"`
	Categories Categories `xml:"categories"`
}

type CapsServer struct {
	Version   string `xml:"version,attr"`
	Title     string `xml:"title,attr"`
	Strapline string `xml:"strapline,attr"`
	Email     string `xml:"email,attr"`
}

type Limits struct {
	Max     int `xml:"max,attr"`
	Default int `xml:"default,attr"`
}

type Search struct {
	Search      SearchItem `xml:"search"`
	TvSearch    SearchItem `xml:"tv-search"`
	MovieSearch SearchItem `xml:"movie-search"`
}

type SearchItem struct {
	Available       string `xml:"available,attr"`
	SupportedParams string `xml:"supportedParams,attr"`
}

type Categories struct {
	Category []Category `xml:"category"`
}

type Category struct {
	ID      int      `xml:"id,attr"`
	Name    string   `xml:"name,attr"`
	Subcats []Subcat `xml:"subcat,omitempty"`
}

type Subcat struct {
	ID   int    `xml:"id,attr"`
	Name string `xml:"name,attr"`
}

func GetCapsXML() ([]byte, error) {
	caps := ServerCaps{
		Server: CapsServer{
			Version:   "2.0",
			Title:     "Media Stream Bridge Indexer",
			Strapline: "Multi-Provider Streaming & Donghua Hub",
			Email:     "bridge@local",
		},
		Limits: Limits{Max: 100, Default: 50},
		Searching: Search{
			Search:      SearchItem{Available: "yes", SupportedParams: "q"},
			TvSearch:    SearchItem{Available: "yes", SupportedParams: "q,season,ep"},
			MovieSearch: SearchItem{Available: "yes", SupportedParams: "q,imdbid"},
		},
		Categories: Categories{
			Category: []Category{
				{
					ID:   2000,
					Name: "Movies",
					Subcats: []Subcat{
						{ID: 2040, Name: "HD"},
						{ID: 2045, Name: "UHD"},
					},
				},
				{
					ID:   5000,
					Name: "TV",
					Subcats: []Subcat{
						{ID: 5040, Name: "HD"},
						{ID: 5070, Name: "Anime"},
					},
				},
			},
		},
	}
	return xml.MarshalIndent(caps, "", "  ")
}

// RSS XML emission structures
type RSSFeed struct {
	XMLName      xml.Name   `xml:"rss"`
	Version      string     `xml:"version,attr"`
	XMLNSTorznab string     `xml:"xmlns:torznab,attr"`
	Channel      RSSChannel `xml:"channel"`
}

type RSSChannel struct {
	Title       string    `xml:"title"`
	Description string    `xml:"description"`
	Link        string    `xml:"link"`
	Items       []RSSItem `xml:"item"`
}

type RSSItem struct {
	Title       string        `xml:"title"`
	GUID        string        `xml:"guid"`
	PubDate     string        `xml:"pubDate"`
	Enclosure   RSSEnclosure  `xml:"enclosure"`
	TorznabAttrs []TorznabAttr `xml:"torznab:attr"`
}

type RSSEnclosure struct {
	URL    string `xml:"url,attr"`
	Length int64  `xml:"length,attr"`
	Type   string `xml:"type,attr"`
}

type TorznabAttr struct {
	Name  string `xml:"name,attr"`
	Value string `xml:"value,attr"`
}

func GenerateRSSXML(results []domain.SearchResult, hostURL string) ([]byte, error) {
	feed := RSSFeed{
		Version:      "2.0",
		XMLNSTorznab: "http://torznab.com/schemas/2015/feed",
		Channel: RSSChannel{
			Title:       "Media Stream Bridge RSS Feed",
			Description: "Automated Vietnamese & Donghua Stream Indexer for *arr",
			Link:        hostURL,
		},
	}

	for _, res := range results {
		downloadURL := fmt.Sprintf("%s/torznab/download?url=%s&title=%s&cat=%s",
			hostURL, res.DownloadURL, res.Title, res.Category)

		catID := "5000"
		if res.Category == "movie" {
			catID = "2000"
		}

		item := RSSItem{
			Title:   res.Title,
			GUID:    res.DownloadURL,
			PubDate: res.PublishDate.Format(time.RFC1123Z),
			Enclosure: RSSEnclosure{
				URL:    downloadURL,
				Length: res.SizeBytes,
				Type:   "application/x-bittorrent",
			},
			TorznabAttrs: []TorznabAttr{
				{Name: "category", Value: catID},
				{Name: "seeders", Value: "50"},
				{Name: "peers", Value: "10"},
				{Name: "provider", Value: res.Provider},
			},
		}
		feed.Channel.Items = append(feed.Channel.Items, item)
	}

	output, err := xml.MarshalIndent(feed, "", "  ")
	if err != nil {
		return nil, err
	}
	return append([]byte(xml.Header), output...), nil
}
