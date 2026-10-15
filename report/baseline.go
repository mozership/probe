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

	baseJobs := make(map[string]*Job, len(base.Jobs))
	for i := range base.Jobs {
		if _, ok := baseJobs[base.Jobs[i].ID]; !ok {
			baseJobs[base.Jobs[i].ID] = &base.Jobs[i]
		}
	}
	for i := range r.Jobs {
		job := &r.Jobs[i]
		baseJob, ok := baseJobs[job.ID]
		if !ok {
			continue
		}
		baseSteps := make(map[string][]*Step)
		for j := range baseJob.Steps {
			st := &baseJob.Steps[j]
			if st.ID != "" && st.Repeat != nil {
				baseSteps[st.ID] = append(baseSteps[st.ID], st)
			}
		}
		for j := range job.Steps {
			st := &job.Steps[j]
			if st.ID == "" || st.Repeat == nil {
				continue
			}
			matches := baseSteps[st.ID]
			if len(matches) == 0 {
				continue
			}
			baseSteps[st.ID] = matches[1:]
			if st.Repeat.Latency == nil {
				continue
			}
			rp := matches[0].Repeat
			st.Repeat.Baseline = &RepeatBaseline{Total: rp.Total, Success: rp.Success, Latency: rp.Latency}
		}
	}
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
	s := FormatMs(before) + " → " + FormatMs(now)
	if before > 0 {
		s += fmt.Sprintf(" (%+.1f%%)", (now-before)/before*100)
	}
	return s
}

// successRate writes the share of runs that succeeded, or - when there were
// none.
func successRate(success, total int) string {
	if total == 0 {
		return "-"
	}
	return fmt.Sprintf("%.1f%%", float64(success)/float64(total)*100)
}
