package report

import (
	"encoding/json"
	"fmt"
	"os"
	"time"
)

// BaselineRun is the run a report's response times were compared to.
type BaselineRun struct {
	RunID     string    `json:"run_id,omitempty"`
	StartedAt time.Time `json:"started_at"`
}

// RepeatBaseline is the outcome of a repeated step in the run compared to.
type RepeatBaseline struct {
	Total   int      `json:"total"`
	Success int      `json:"success"`
	Latency *Latency `json:"latency,omitempty"`
}

// Read reads a JSON report, as WriteJSON writes it.
func Read(path string) (*Report, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read report: %w", err)
	}
	var r Report
	if err := json.Unmarshal(data, &r); err != nil {
		return nil, fmt.Errorf("failed to parse report %s: %w", path, err)
	}
	return &r, nil
}

// Compare gives each repeated step whose response times were measured the
// outcome of the step of the same id, in the job of the same id, in base,
// the report of an earlier run. When a job has several steps of an id, as
// the entries of an iteration have, they are matched in order. A step that
// base has no step for, as a step added since has not, is given none.
func (r *Report) Compare(base *Report) {
	if base == nil {
		return
	}
	r.Baseline = &BaselineRun{RunID: base.RunID, StartedAt: base.StartedAt}

	for i := range r.Jobs {
		job := &r.Jobs[i]
		steps := base.BaselineSteps(job.ID)
		for j := range job.Steps {
			st := &job.Steps[j]
			if st.Repeat == nil {
				continue
			}
			b := steps.Next(st.ID)
			if st.Repeat.Latency != nil {
				st.Repeat.Baseline = b
			}
		}
	}
}

// BaselineSteps are the repeated steps of a job of a baseline report, which
// Next gives out by their ids in order.
type BaselineSteps struct {
	steps map[string][]*Step
}

// BaselineSteps returns the repeated steps of the first job of id in r; nil,
// which has none, when r is nil or has no job of the id.
func (r *Report) BaselineSteps(jobID string) *BaselineSteps {
	if r == nil {
		return nil
	}
	for i := range r.Jobs {
		job := &r.Jobs[i]
		if job.ID != jobID {
			continue
		}
		s := &BaselineSteps{steps: make(map[string][]*Step)}
		for j := range job.Steps {
			st := &job.Steps[j]
			if st.ID != "" && st.Repeat != nil {
				s.steps[st.ID] = append(s.steps[st.ID], st)
			}
		}
		return s
	}
	return nil
}

// Next returns the outcome of the next repeated step of id, or nil when
// there is none left.
func (s *BaselineSteps) Next(id string) *RepeatBaseline {
	if s == nil || id == "" {
		return nil
	}
	matches := s.steps[id]
	if len(matches) == 0 {
		return nil
	}
	s.steps[id] = matches[1:]
	rp := matches[0].Repeat
	return &RepeatBaseline{Total: rp.Total, Success: rp.Success, Latency: rp.Latency}
}

// baselinePhrase names the run compared to, by when it started and its id
// when it has one.
func baselinePhrase(b *BaselineRun) string {
	s := "Compared to the run started at " + b.StartedAt.UTC().Format(time.RFC3339)
	if b.RunID != "" {
		s += " (" + b.RunID + ")"
	}
	return s
}

// comparison returns the cells of a repeated step in the table of how it
// changed from the baseline: the runs, p50, p95, p99 and the share of the
// runs that succeeded, each as before → now. A step with no baseline has
// only now.
func comparison(rp *Repeat) []string {
	l := rp.Latency
	b := rp.Baseline
	if b == nil {
		return []string{
			fmt.Sprintf("%d (no baseline)", l.Count),
			FormatMs(l.P50Ms), FormatMs(l.P95Ms), FormatMs(l.P99Ms),
			successRate(rp.Success, rp.Total),
		}
	}
	runs := fmt.Sprintf("%d → %d", 0, l.Count)
	if b.Latency != nil {
		runs = fmt.Sprintf("%d → %d", b.Latency.Count, l.Count)
	}
	ms := func(now float64, before func(*Latency) float64) string {
		if b.Latency == nil {
			return "- → " + FormatMs(now)
		}
		return msChange(before(b.Latency), now)
	}
	return []string{
		runs,
		ms(l.P50Ms, func(l *Latency) float64 { return l.P50Ms }),
		ms(l.P95Ms, func(l *Latency) float64 { return l.P95Ms }),
		ms(l.P99Ms, func(l *Latency) float64 { return l.P99Ms }),
		successRate(b.Success, b.Total) + " → " + successRate(rp.Success, rp.Total),
	}
}

// msChange writes a time before and now, and how much it changed relative
// to before.
func msChange(before, now float64) string {
	return FormatMs(before) + " → " + FormatMs(now) + relChange(before, now)
}

// relChange writes how much now changed relative to before, as " (+12.5%)",
// or nothing when the two are written the same: a change finer than the
// times are written to would be a share of what does not show, which for
// times under 0.1ms is any share at all.
func relChange(before, now float64) string {
	if before <= 0 || FormatMs(before) == FormatMs(now) {
		return ""
	}
	return fmt.Sprintf(" (%+.1f%%)", (now-before)/before*100)
}

// Summary writes on one line the times of the step in the baseline, and how
// much now, the times of this run, changed from them, as the terminal shows
// it under now. A nil baseline is a step the baseline has none of.
func (b *RepeatBaseline) Summary(now *Latency) string {
	if b == nil {
		return "no baseline"
	}
	if b.Latency == nil {
		return "baseline has no times"
	}
	return fmt.Sprintf("baseline p50 %s%s, p95 %s%s, p99 %s%s",
		FormatMs(b.Latency.P50Ms), relChange(b.Latency.P50Ms, now.P50Ms),
		FormatMs(b.Latency.P95Ms), relChange(b.Latency.P95Ms, now.P95Ms),
		FormatMs(b.Latency.P99Ms), relChange(b.Latency.P99Ms, now.P99Ms))
}

// successRate writes the share of runs that succeeded, or - when there were
// none.
func successRate(success, total int) string {
	if total == 0 {
		return "-"
	}
	return fmt.Sprintf("%.1f%%", float64(success)/float64(total)*100)
}
