package formatter

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"unicode"
)

var (
	reSeasonVi    = regexp.MustCompile(`(?i)(?:phần|mùa)\s*(\d+)`)
	reSeasonEn    = regexp.MustCompile(`(?i)\b(?:season|ss)\s*(\d+)\b|\b(\d+)(?:st|nd|rd|th)?\s+season\b|\bS(\d{1,2})\b`)
	reRomanSeason = regexp.MustCompile(`(?i)\b(?:phần|mùa|season|ss)?\s*(VI|VII|VIII|IX|X|IV|V|III|II|I)\b`)
	reNumberEnd   = regexp.MustCompile(`\s+(\d+)$`)
	reEpisodeNum  = regexp.MustCompile(`(?i)(?:tập|tap|ep|episode)\s*[-_]?\s*(\d+)|\b(\d+)\b`)
	reSeasonNoise = regexp.MustCompile(`(?i)\s*(?:\(?\s*(?:phần|mùa|season|ss)?\s*\d+\s*\)?|\b\d+(?:st|nd|rd|th)?\s+season\b|\bS\d{1,2}\b|\b(?:VI|VII|VIII|IX|X|IV|V|III|II|I)\b).*$`)
)

// DetectSeason extracts season number from Vietnamese title, English/original title or query.
func DetectSeason(vietName, origName, query string) int {
	candidates := []string{vietName, origName, query}

	for _, text := range candidates {
		if text == "" {
			continue
		}
		if m := reSeasonVi.FindStringSubmatch(text); len(m) > 1 {
			if n, err := strconv.Atoi(m[1]); err == nil && n > 0 && n < 100 {
				return n
			}
		}
		if m := reSeasonEn.FindStringSubmatch(text); len(m) > 1 {
			for i := 1; i < len(m); i++ {
				if m[i] != "" {
					if n, err := strconv.Atoi(m[i]); err == nil && n > 0 && n < 100 {
						return n
					}
				}
			}
		}
		if m := reRomanSeason.FindStringSubmatch(text); len(m) > 1 {
			roman := strings.ToUpper(m[1])
			switch roman {
			case "I":
				return 1
			case "II":
				return 2
			case "III":
				return 3
			case "IV":
				return 4
			case "V":
				return 5
			case "VI":
				return 6
			case "VII":
				return 7
			case "VIII":
				return 8
			case "IX":
				return 9
			case "X":
				return 10
			}
		}
		if m := reNumberEnd.FindStringSubmatch(text); len(m) > 1 {
			if n, err := strconv.Atoi(m[1]); err == nil && n > 0 && n < 30 {
				return n
			}
		}
	}
	return 0
}

// DetectEpisodeNum parses episode index from episode name or slug (e.g., "Tập 01", "tap-5").
func DetectEpisodeNum(name, slug string) int {
	for _, text := range []string{name, slug} {
		if m := reEpisodeNum.FindStringSubmatch(text); len(m) > 1 {
			for i := 1; i < len(m); i++ {
				if m[i] != "" {
					if n, err := strconv.Atoi(m[i]); err == nil && n > 0 {
						return n
					}
				}
			}
		}
	}
	return 1
}

// PickBestEnglishTitle finds the best Latin/English title from original name or query.
func PickBestEnglishTitle(origName, query, vietName string) string {
	if origName == "" {
		return ""
	}

	parts := strings.Split(origName, ",")
	if len(parts) == 1 {
		parts = strings.Split(origName, "/")
	}

	trimmedQuery := strings.TrimSpace(strings.ToLower(query))

	var fallback string
	for _, part := range parts {
		clean := strings.TrimSpace(part)
		if clean == "" {
			continue
		}

		// Check if it matches user/arr query
		if trimmedQuery != "" && strings.Contains(strings.ToLower(clean), trimmedQuery) {
			return CleanSeriesName(clean)
		}

		// Ensure it has ascii latin characters
		if isLatinString(clean) && fallback == "" {
			fallback = clean
		}
	}

	if fallback != "" {
		return CleanSeriesName(fallback)
	}
	return ""
}

// CleanSeriesName removes trailing season noise from a series title.
func CleanSeriesName(name string) string {
	cleaned := reSeasonNoise.ReplaceAllString(name, "")
	cleaned = strings.TrimSpace(cleaned)
	cleaned = strings.Trim(cleaned, "()[]-:")
	return strings.TrimSpace(cleaned)
}

func isLatinString(s string) bool {
	latinCount := 0
	for _, r := range s {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') {
			latinCount++
		}
		if unicode.Is(unicode.Han, r) {
			return false
		}
	}
	return latinCount >= 3
}

// BuildReleaseTitle generates a Scene/Torznab-compliant title for Sonarr/Radarr.
func BuildReleaseTitle(vietName, origName, query string, year, season int, quality, provider string) string {
	if season == 0 {
		season = DetectSeason(vietName, origName, query)
	}

	enName := PickBestEnglishTitle(origName, query, vietName)
	cleanViet := CleanSeriesName(vietName)

	var sb strings.Builder

	if enName != "" && !strings.EqualFold(enName, cleanViet) {
		sb.WriteString(enName)
		if season > 0 {
			sb.WriteString(fmt.Sprintf(" S%02d", season))
		}
		sb.WriteString(" (" + cleanViet + ")")
	} else {
		sb.WriteString(cleanViet)
		if season > 0 {
			sb.WriteString(fmt.Sprintf(" S%02d", season))
		}
	}

	if year > 0 {
		sb.WriteString(fmt.Sprintf(" (%d)", year))
	}

	if quality == "" {
		quality = "1080p WEB-DL"
	}
	sb.WriteString(" [" + quality + "]")
	sb.WriteString(" [" + provider + "]")

	return sb.String()
}

// BuildEpisodeFileName formats a file name that Sonarr automatically parses:
// "The Westward - S04E01.mp4"
func BuildEpisodeFileName(seriesTitle string, season, epNum int) string {
	if season <= 0 {
		season = 1
	}
	cleanSeries := strings.ReplaceAll(seriesTitle, "/", "-")
	cleanSeries = strings.ReplaceAll(cleanSeries, ":", " -")
	cleanSeries = strings.TrimSpace(cleanSeries)
	return fmt.Sprintf("%s - S%02dE%02d.mp4", cleanSeries, season, epNum)
}
