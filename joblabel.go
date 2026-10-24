package probe

import (
	"strings"
	"unicode"

	"golang.org/x/text/width"
)

// maxJobLabelWidth is how many columns the label of a job takes at most in
// a progress line. The lines of jobs that run at once are told apart by it,
// so a long name is cut short rather than pushing the step out of sight.
const maxJobLabelWidth = 24

// jobLabels returns the label of each job of order in a progress line: its
// name, as name gives it for its id, or its id when it has no name. A label
// is at most maxJobLabelWidth columns wide. Jobs whose labels would be the
// same, two jobs named the same, have their ids added, so that a line is of
// one job and the id is that which --job takes.
func jobLabels(order []string, name func(id string) string) map[string]string {
	full := make(map[string]string, len(order))
	labels := make(map[string]string, len(order))
	count := make(map[string]int, len(order))
	for _, id := range order {
		n := ""
		if name != nil {
			n = oneLine(name(id))
		}
		if n == "" {
			n = id
		}
		full[id] = n
		labels[id] = truncateWidth(n, maxJobLabelWidth)
		count[labels[id]]++
	}

	for _, id := range order {
		if count[labels[id]] < 2 || full[id] == id {
			continue
		}
		suffix := " (" + id + ")"
		room := maxJobLabelWidth - displayWidth(suffix)
		if room < 4 {
			labels[id] = truncateWidth(id, maxJobLabelWidth)
			continue
		}
		labels[id] = truncateWidth(full[id], room) + suffix
	}
	return labels
}

// oneLine returns s with each run of white space and control characters
// made one space, so that a name keeps to the line it labels.
func oneLine(s string) string {
	return strings.Join(strings.FieldsFunc(s, func(r rune) bool {
		return unicode.IsSpace(r) || unicode.IsControl(r)
	}), " ")
}

// displayWidth returns how many columns s takes in a terminal: two for a
// wide character, such as one of Japanese, none for a mark that combines
// with the one before it, and one for any other.
func displayWidth(s string) int {
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

// truncateWidth returns s cut to at most maxWidth columns, ending in an
// ellipsis when it was cut.
func truncateWidth(s string, maxWidth int) string {
	if displayWidth(s) <= maxWidth {
		return s
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
