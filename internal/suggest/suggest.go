// Package suggest finds the name that was likely meant by one that is not
// known, so that an error can ask "did you mean" it.
package suggest

import "strings"

// Closest returns the name of allowed closest to name, when one is close
// enough to be what was meant: two edits from it at most, and no more than
// half its length. One that differs from name only in case, as api_url does
// from API_URL, is closest.
func Closest(name string, allowed []string) string {
	for _, a := range allowed {
		if strings.EqualFold(name, a) {
			return a
		}
	}
	// A short name is close to too many others: x is two edits from rt.
	best, bestDist := "", min(3, len(name)/2+1)
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
