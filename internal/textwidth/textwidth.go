// Package textwidth measures text by the columns it takes in a terminal,
// and cuts it to them, so that what is written beside it lines up whatever
// characters it has.
package textwidth

import (
	"strings"
	"unicode"
	"unicode/utf8"

	"golang.org/x/text/width"
)

// Width returns how many columns s takes in a terminal: two for a wide
// character, such as one of Japanese, or one drawn as an emoji, none for a
// mark that combines with the one before it or a format character, and one
// for any other.
func Width(s string) int {
	w := 0
	for len(s) > 0 {
		n, cw := next(s)
		w += cw
		s = s[n:]
	}
	return w
}

// The variation selector that asks for the character before it to be drawn
// as an emoji, and the skin tones that change the emoji before them.
const (
	emojiPresentation = '\uFE0F'
	skinToneFirst     = '\U0001F3FB'
	skinToneLast      = '\U0001F3FF'
)

// next returns the length of the first character of s, with the marks,
// format characters, emoji variation selector and skin tones that follow
// it, and how many columns they take together. A character given the
// emoji variation selector, as the warning sign of ⚠️ is, is drawn two
// columns wide, and a skin tone takes no column of its own after the emoji
// it changes. An emoji joined to the next by a zero width joiner, as those
// of a family are, is measured as each of them would be alone, which is
// wider than a terminal that joins them draws it.
func next(s string) (int, int) {
	r, n := utf8.DecodeRuneInString(s)
	w := runeWidth(r)
	for n < len(s) {
		r, size := utf8.DecodeRuneInString(s[n:])
		switch {
		case r == emojiPresentation:
			w = max(w, 2)
		case r >= skinToneFirst && r <= skinToneLast:
		case runeWidth(r) == 0:
		default:
			return n, w
		}
		n += size
	}
	return n, w
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
// ellipsis when it was cut. A character is kept or left out with the marks
// and the emoji modifiers that follow it, and a wide one that would go past
// maxWidth is left out whole, so what is returned may be a column
// narrower. Nothing is left of s when maxWidth is less than one.
func Truncate(s string, maxWidth int) string {
	if Width(s) <= maxWidth {
		return s
	}
	if maxWidth < 1 {
		return ""
	}
	w, end := 0, 0
	for end < len(s) {
		n, cw := next(s[end:])
		if w+cw > maxWidth-1 {
			break
		}
		w += cw
		end += n
	}
	return s[:end] + "…"
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
