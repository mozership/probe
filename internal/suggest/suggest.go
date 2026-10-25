// Package suggest finds the name that was likely meant by one that is not
// known, so that an error can ask "did you mean" it.
package suggest

import (
	"fmt"
	"strings"
)

// Closest returns the name of allowed closest to name, when one is close
// enough to be what was meant: two edits from it at most, and no more than
// half its length. One that differs from name only in case, as api_url does
// from API_URL, is closest.
func Closest(name string, allowed []string) string {
	// A short name is close to too many others: x is two edits from rt.
	return Within(name, allowed, min(2, len(name)/2))
}

// Hint returns the question that follows an error about name, which is not
// among allowed: "; did you mean" the name Closest finds, or an empty string
// when none is close enough.
func Hint(name string, allowed []string) string {
	if s := Closest(name, allowed); s != "" {
		return fmt.Sprintf("; did you mean %q?", s)
	}
	return ""
}

// Within returns the name of allowed closest to name, when it is edits
// edits from it at most, or differs from it only in case. It is for names
// among which one two edits away is as likely another as the one meant, as
// body is two from code.
func Within(name string, allowed []string, edits int) string {
	for _, a := range allowed {
		if strings.EqualFold(name, a) {
			return a
		}
	}
	best, bestDist := "", edits+1
	for _, a := range allowed {
		if d := distance(name, a); d < bestDist {
			best, bestDist = a, d
		}
	}
	return best
}

// distance is the Levenshtein distance between a and b.
func distance(a, b string) int {
	prev := make([]int, len(b)+1)
	for j := range prev {
		prev[j] = j
	}
	for i := 1; i <= len(a); i++ {
		cur := make([]int, len(b)+1)
		cur[0] = i
		for j := 1; j <= len(b); j++ {
			cost := 1
			if a[i-1] == b[j-1] {
				cost = 0
			}
			cur[j] = min(prev[j]+1, cur[j-1]+1, prev[j-1]+cost)
		}
		prev = cur
	}
	return prev[len(b)]
}
