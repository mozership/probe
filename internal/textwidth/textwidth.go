// Package textwidth measures text by the columns it takes in a terminal,
// and cuts it to them, so that what is written beside it lines up whatever
// characters it has.
package textwidth

import (
	"strings"
	"unicode"

	"golang.org/x/text/width"
)

// Width returns how many columns s takes in a terminal: two for a wide
// character, such as one of Japanese, none for a mark that combines with
// the one before it or a format character, and one for any other.
func Width(s string) int {
	w := 0
	for _, r := range s {
		w += runeWidth(r)
	}
	return w
}

func runeWidth(r rune) int {
	if unicode.In(r, unicode.Mn, unicode.Me, unicode.Cf) {
		return 0
	}
	switch width.LookupRune(r).Kind() {
	case width.EastAsianWide, width.EastAsianFullwidth:
		return 2
	}
	return 1
}

// Truncate returns s cut to at most maxWidth columns, ending in an
// ellipsis when it was cut. A wide character that would go past maxWidth
// is left out whole, so what is returned may be a column narrower. Nothing
// is left of s when maxWidth is less than one.
func Truncate(s string, maxWidth int) string {
	if Width(s) <= maxWidth {
		return s
	}
	if maxWidth < 1 {
		return ""
	}
	var b strings.Builder
	w := 0
	for _, r := range s {
		rw := runeWidth(r)
		if w+rw > maxWidth-1 {
			break
		}
		b.WriteRune(r)
		w += rw
	}
	return b.String() + "…"
}

// OneLine returns s with each run of white space and control characters
// made one space, and those at its ends left out, so that it keeps to the
// line it is written on, and without format characters, such as one that
// turns the text after it right to left, which would change how the rest
// of the line reads.
func OneLine(s string) string {
	s = strings.Map(func(r rune) rune {
		if unicode.Is(unicode.Cf, r) {
			return -1
		}
		return r
	}, s)
	return strings.Join(strings.FieldsFunc(s, func(r rune) bool {
		return unicode.IsSpace(r) || unicode.IsControl(r)
	}), " ")
}
