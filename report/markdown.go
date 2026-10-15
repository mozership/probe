package report

import (
	"fmt"
	"io"
	"strings"
)

// WriteMarkdown writes the report as a Markdown page: a summary line, a table
// of jobs, a table of the response times of the repeated steps, and a section
// for every failed step with its test, the reason it failed, and the request
// and response. The page is meant to be read as is, for example as a GitHub
// job summary or by a coding agent.
func (r *Report) WriteMarkdown(w io.Writer) error {
	_, err := io.WriteString(w, r.markdown(true))
	return err
}

// markdown renders the page. Without payloads, failed steps leave out their
// request and response, which is what keeps a page with large responses
// within a size limit.
func (r *Report) markdown(payloads bool) string {
	page, _ := r.markdownWithCuts(payloads)
	return page
}

// markdownWithCuts renders the page and also returns the offsets where it
// can be cut short without leaving a block half written: before the job
// table, before the response times, before the failures heading, and before
// each failure section. They are recorded while rendering rather than
// searched for afterwards, because a test or a message can itself contain a
// line that looks like a heading.
func (r *Report) markdownWithCuts(payloads bool) (string, []int) {
	var b strings.Builder
	var cuts []int

	fmt.Fprintf(&b, "# %s\n\n", r.Name)
	if r.Description != "" {
		fmt.Fprintf(&b, "%s\n\n", strings.TrimSpace(r.Description))
	}

	fmt.Fprintf(&b, "**%s** in %.2fs. Jobs: %s. Steps: %s.\n\n",
		markdownStatus(r.Status), msToSec(r.DurationMs),
		countPhrase(r.Summary.Jobs), countPhrase(r.Summary.Steps))

	cuts = append(cuts, b.Len())
	b.WriteString("| Job | Status | Steps | Duration |\n")
	b.WriteString("|---|---|---|---|\n")
	for _, job := range r.Jobs {
		var passed int
		for _, st := range job.Steps {
			if st.Status == Passed {
				passed++
			}
		}
		fmt.Fprintf(&b, "| %s | %s | %d/%d | %.2fs |\n",
			markdownCell(job.Name), job.Status, passed, len(job.Steps), msToSec(job.DurationMs))
	}

	latencies := false
	for _, job := range r.Jobs {
		for _, st := range job.Steps {
			if st.Repeat == nil || st.Repeat.Latency == nil {
				continue
			}
			if !latencies {
				cuts = append(cuts, b.Len())
				b.WriteString("\n## Response Times\n\n")
				b.WriteString("| Job | Step | Runs | Min | p50 | p90 | p95 | p99 | Max |\n")
				b.WriteString("|---|---|---|---|---|---|---|---|---|\n")
				latencies = true
			}
			l := st.Repeat.Latency
			fmt.Fprintf(&b, "| %s | %d. %s | %d | %s | %s | %s | %s | %s | %s |\n",
				markdownCell(job.Name), st.Index, markdownCell(st.Name), l.Count,
				FormatMs(l.MinMs), FormatMs(l.P50Ms), FormatMs(l.P90Ms),
				FormatMs(l.P95Ms), FormatMs(l.P99Ms), FormatMs(l.MaxMs))
		}
	}

	failures := false
	failure := func() {
		if !failures {
			cuts = append(cuts, b.Len())
			b.WriteString("\n## Failures\n\n")
			failures = true
		}
		cuts = append(cuts, b.Len())
	}
	for _, job := range r.Jobs {
		for _, st := range job.Steps {
			if st.Status != Failed {
				continue
			}
			failure()
			writeMarkdownFailure(&b, job, st, payloads)
		}
		if rt := job.RepeatTest; rt != nil && rt.Status == Failed {
			failure()
			writeMarkdownRepeatTestFailure(&b, job, rt)
		}
	}

	// Every block ends in a blank line; the page itself ends in one newline.
	return strings.TrimRight(b.String(), "\n") + "\n", cuts
}

func writeMarkdownFailure(b *strings.Builder, job Job, st Step, payloads bool) {
	fmt.Fprintf(b, "### %s / %d. %s\n\n", job.Name, st.Index, st.Name)

	if st.Failure != nil {
		fmt.Fprintf(b, "%s: %s\n\n", st.Failure.Kind, st.Failure.Message)
		for _, v := range st.Failure.Violations {
			fmt.Fprintf(b, "- %s\n", v)
		}
		if len(st.Failure.Violations) > 0 {
			b.WriteString("\n")
		}
	}
	if st.Repeat != nil {
		fmt.Fprintf(b, "%d of %d iterations succeeded.\n\n", st.Repeat.Success, st.Repeat.Total)
	}
	if st.Test != "" {
		writeFence(b, "", st.Test)
	}
	if st.Failure == nil || !payloads {
		return
	}
	for _, part := range []struct {
		label string
		data  map[string]any
	}{
		{"Request", st.Failure.Request},
		{"Response", st.Failure.Response},
	} {
		if part.data == nil {
			continue
		}
		fmt.Fprintf(b, "<details><summary>%s</summary>\n\n", part.label)
		writeFence(b, "json", indentJSON(part.data))
		b.WriteString("</details>\n\n")
	}
}

// writeMarkdownRepeatTestFailure writes the section of a job whose test over
// its runs failed: the reason, then the test.
func writeMarkdownRepeatTestFailure(b *strings.Builder, job Job, rt *RepeatTest) {
	fmt.Fprintf(b, "### %s / repeat test\n\n", job.Name)
	if rt.Failure != nil {
		fmt.Fprintf(b, "%s: %s\n\n", rt.Failure.Kind, rt.Failure.Message)
	}
	writeFence(b, "", rt.Test)
}

// writeFence writes content in a fenced code block whose fence is longer than
// any backtick run inside it, so the content cannot close the block early.
func writeFence(b *strings.Builder, lang, content string) {
	fence := "```"
	for strings.Contains(content, fence) {
		fence += "`"
	}
	fmt.Fprintf(b, "%s%s\n%s\n%s\n\n", fence, lang, strings.TrimRight(content, "\n"), fence)
}

func markdownStatus(status string) string {
	if status == Failed {
		return "Failed"
	}
	return "Passed"
}

// countPhrase renders a tally such as "3 total, 2 passed, 1 failed", leaving
// out the statuses nothing has.
func countPhrase(c Count) string {
	parts := []string{fmt.Sprintf("%d total", c.Total)}
	for _, p := range []struct {
		n     int
		label string
	}{
		{c.Passed, "passed"},
		{c.Failed, "failed"},
		{c.Skipped, "skipped"},
		{c.Untested, "untested"},
	} {
		if p.n > 0 {
			parts = append(parts, fmt.Sprintf("%d %s", p.n, p.label))
		}
	}
	return strings.Join(parts, ", ")
}

// markdownCell keeps a value on one table row and out of the column syntax.
func markdownCell(s string) string {
	s = strings.ReplaceAll(s, "\n", " ")
	return strings.ReplaceAll(s, "|", `\|`)
}
