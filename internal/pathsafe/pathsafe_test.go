package pathsafe

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSafeComponent(t *testing.T) {
	cases := map[string]string{
		"../../etc":     "-.-etc",
		"a/b\\c:d":      "a-b-c-d",
		"  ..hidden":    "hidden",
		"":              "untitled",
		"...":           "untitled",
		"tab\x00\x07ok": "tabok",
		"Normal Title":  "Normal Title",
		"a..b":          "a.b",
	}
	for in, want := range cases {
		if got := SafeComponent(in); got != want {
			t.Errorf("SafeComponent(%q) = %q, want %q", in, got, want)
		}
	}
	if got := SafeComponent(strings.Repeat("é", 200)); len(got) > 150 {
		t.Errorf("not truncated: %d bytes", len(got))
	}
}

func TestWithin(t *testing.T) {
	base := t.TempDir()
	outside := t.TempDir()
	if err := os.Symlink(outside, filepath.Join(base, "link")); err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		p    string
		want bool
	}{
		{base, true},
		{filepath.Join(base, "series", "x"), true},
		{filepath.Join(base, "..", "other"), false},
		{outside, false},
		{filepath.Join(base, "link", "x"), false},
		{"relative/path", false},
	}
	for _, c := range cases {
		if got := Within(base, c.p); got != c.want {
			t.Errorf("Within(%q) = %v, want %v", c.p, got, c.want)
		}
	}
}
