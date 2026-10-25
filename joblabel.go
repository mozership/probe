package probe

import "github.com/mozership/probe/internal/textwidth"

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
			n = textwidth.OneLine(name(id))
		}
		if n == "" {
			n = id
		}
		full[id] = n
		labels[id] = textwidth.Truncate(n, maxJobLabelWidth)
		count[labels[id]]++
	}

	for _, id := range order {
		if count[labels[id]] < 2 || full[id] == id {
			continue
		}
		suffix := " (" + id + ")"
		room := maxJobLabelWidth - textwidth.Width(suffix)
		if room < 4 {
			labels[id] = textwidth.Truncate(id, maxJobLabelWidth)
			continue
		}
		labels[id] = textwidth.Truncate(full[id], room) + suffix
	}
	return labels
}
