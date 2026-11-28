package main

import (
	"os"
	"regexp"
	"strings"
	"testing"
)

func readLogo(t *testing.T) wordmark {
	t.Helper()
	logo, err := os.ReadFile(logoFile)
	if err != nil {
		t.Fatal(err)
	}
	m, err := readWordmark(string(logo))
	if err != nil {
		t.Fatal(err)
	}
	return m
}

// readFonts skips the test where the font is not, as off a Mac.
func readFonts(t *testing.T) fonts {
	t.Helper()
	if _, err := os.Stat(fontFile); err != nil {
		t.Skipf("no %s: %v", fontFile, err)
	}
	fs, err := loadFonts(fontFile)
	if err != nil {
		t.Fatal(err)
	}
	return fs
}

func TestOutputsAreUpToDate(t *testing.T) {
	mark, fs := readLogo(t), readFonts(t)
	for _, o := range outputs {
		got, err := os.ReadFile(o.path)
		if err != nil {
			t.Fatal(err)
		}
		want, err := render(o.theme, mark, fs)
		if err != nil {
			t.Fatal(err)
		}
		if string(got) != want {
			t.Errorf("%s is not what go run . draws; run it in misc/_architecture", o.path)
		}
	}
}

func TestReadWordmark(t *testing.T) {
	m := readLogo(t)
	if len(m.paths) != 5 {
		t.Fatalf("got %d paths, want the 5 letters of PROBE", len(m.paths))
	}
	if m.minX >= m.maxX || m.minY >= m.maxY {
		t.Fatalf("empty bounds: %+v", m)
	}
	if ratio := m.width(100) / 100; ratio < 2 || ratio > 4 {
		t.Errorf("the letters are %.2f times as wide as high, not the logo's proportions", ratio)
	}
}

func TestReadWordmarkErrors(t *testing.T) {
	tests := []struct {
		name, svg, want string
	}{
		{"no letters", `<svg><path d="M0,0L1,1Z"/></svg>`, "no group of five paths"},
		{
			"relative command",
			`<g transform="matrix(1,0,0,1,0,0)">` + strings.Repeat(`<path d="M0,0l1,1Z"/>`, 5) + `</g>`,
			"other than M, L, C and Z",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := readWordmark(tt.svg)
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("got %v, want an error containing %q", err, tt.want)
			}
		})
	}
}

func TestStyleFollowsTheme(t *testing.T) {
	if !strings.Contains(style(auto), "prefers-color-scheme: dark") {
		t.Error("the README copy does not follow the system setting")
	}
	if strings.Contains(style(light), darkRules) || strings.Contains(style(light), "prefers-color-scheme") {
		t.Error("the light copy draws white")
	}
	if !strings.HasSuffix(style(dark), darkRules) {
		t.Error("the dark copy does not end with the white rules")
	}
}

func TestGlyphsAreDefinedOnce(t *testing.T) {
	svg, err := render(light, readLogo(t), readFonts(t))
	if err != nil {
		t.Fatal(err)
	}
	defined := map[string]int{}
	for _, m := range regexp.MustCompile(`<path id="([^"]+)"`).FindAllStringSubmatch(svg, -1) {
		defined[m[1]]++
	}
	for id, n := range defined {
		if n > 1 {
			t.Errorf("%s is defined %d times", id, n)
		}
	}
	uses := regexp.MustCompile(`xlink:href="#([^"]+)"`).FindAllStringSubmatch(svg, -1)
	if len(uses) <= len(defined) {
		t.Errorf("%d uses of %d glyphs: the glyphs are not shared", len(uses), len(defined))
	}
	for _, m := range uses {
		if defined[m[1]] == 0 {
			t.Errorf("%s is used but not defined", m[1])
		}
	}
}
