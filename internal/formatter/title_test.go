package formatter

import (
	"testing"
)

func TestBuildReleaseTitle(t *testing.T) {
	tests := []struct {
		name       string
		vietName   string
		origName   string
		query      string
		year       int
		season     int
		quality    string
		provider   string
		wantPrefix string
		wantSeason string
	}{
		{
			name:       "The Westward Season 4 KKPhim",
			vietName:   "Tây Hành Kỷ (Phần 4)",
			origName:   "The Westward IV",
			query:      "Westward",
			year:       2022,
			season:     0,
			quality:    "1080p WEB-DL",
			provider:   "KKPhim",
			wantPrefix: "The Westward S04",
			wantSeason: "S04",
		},
		{
			name:       "The Westward Season 5 NguonC",
			vietName:   "Tây Hành Kỷ 5",
			origName:   "Xi Xing Ji: Nian Fan, The Westward 5th Season",
			query:      "Westward",
			year:       2023,
			season:     0,
			quality:    "1080p WEB-DL",
			provider:   "NguonC",
			wantPrefix: "The Westward S05",
			wantSeason: "S05",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := BuildReleaseTitle(tt.vietName, tt.origName, tt.query, tt.year, tt.season, tt.quality, tt.provider)
			t.Logf("Generated Title: %s", got)
			if tt.wantPrefix != "" && !hasPrefix(got, tt.wantPrefix) {
				t.Errorf("BuildReleaseTitle() got = %v, want prefix %v", got, tt.wantPrefix)
			}
		})
	}
}

func hasPrefix(s, prefix string) bool {
	return len(s) >= len(prefix) && s[:len(prefix)] == prefix
}
