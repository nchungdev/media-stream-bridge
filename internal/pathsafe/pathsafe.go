// Package pathsafe builds file system paths from untrusted strings.
package pathsafe

import (
	"path/filepath"
	"strings"
	"unicode"
	"unicode/utf8"

	"golang.org/x/text/unicode/norm"
)

const (
	maxComponentBytes = 150
	fallbackName      = "untitled"
)

var separatorReplacer = strings.NewReplacer("/", "-", "\\", "-", ":", "-")

// SafeComponent turns an arbitrary string into a single, harmless path component.
func SafeComponent(s string) string {
	s = norm.NFC.String(s)
	s = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return -1
		}
		return r
	}, s)
	s = separatorReplacer.Replace(s)
	for strings.Contains(s, "..") {
		s = strings.ReplaceAll(s, "..", ".")
	}
	s = strings.TrimLeft(s, ". ")
	s = truncate(s, maxComponentBytes)
	s = strings.TrimRight(s, ". ")
	if s == "" {
		return fallbackName
	}
	return s
}

func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	s = s[:max]
	for !utf8.ValidString(s) {
		s = s[:len(s)-1]
	}
	return s
}

// Within reports whether p is base itself or lies below it, resolving symlinks
// of the deepest existing ancestor so a link cannot be used to escape.
func Within(base, p string) bool {
	if base == "" || p == "" || !filepath.IsAbs(base) || !filepath.IsAbs(p) {
		return false
	}
	return lexicallyWithin(resolve(base), resolve(p))
}

func lexicallyWithin(base, p string) bool {
	rel, err := filepath.Rel(filepath.Clean(base), filepath.Clean(p))
	if err != nil {
		return false
	}
	return rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) && !filepath.IsAbs(rel)
}

// resolve evaluates symlinks on the longest existing prefix of p.
func resolve(p string) string {
	p = filepath.Clean(p)
	rest := ""
	for cur := p; ; cur = filepath.Dir(cur) {
		if real, err := filepath.EvalSymlinks(cur); err == nil {
			return filepath.Join(real, rest)
		}
		if filepath.Dir(cur) == cur {
			return p
		}
		rest = filepath.Join(filepath.Base(cur), rest)
	}
}
