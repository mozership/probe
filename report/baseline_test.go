package report

import (
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"
)

// repeated returns a repeated step whose action took p50Ms at the median,
// and twice and three times that at p95 and p99.
func repeated(index int, id string, success, total int, p50Ms float64) Step {
	return Step{
		Index: index, ID: id, Name: id, Status: Passed,
		Repeat: &Repeat{
			Total: total, Success: success, Failure: total - success,
			Latency: &Latency{Count: total, P50Ms: p50Ms, P95Ms: p50Ms * 2, P99Ms: p50Ms * 3},
		},
	}
}

func TestReport_Compare(t *testing.T) {
	start := time.Date(2026, 10, 15, 9, 0, 0, 0, time.UTC)
	base := &Report{
		RunID: "run-1", StartedAt: start,
		Jobs: []Job{
			{ID: "other", Steps: []Step{repeated(0, "search", 10, 10, 1)}},
			{ID: "api", Steps: []Step{
				repeated(0, "search", 9, 10, 100),
				repeated(1, "each", 10, 10, 10),
				repeated(2, "each", 10, 10, 20),
				{Index: 3, ID: "once", Status: Passed},
			}},
		},
	}
	r := &Report{
		Jobs: []Job{
			{ID: "api", Steps: []Step{
				{Index: 0, ID: "login", Status: Passed},
				repeated(1, "search", 10, 10, 120),
				repeated(2, "each", 10, 10, 11),
				repeated(3, "each", 10, 10, 22),
				repeated(4, "each", 10, 10, 33),
				repeated(5, "added", 10, 10, 5),
				repeated(6, "once", 10, 10, 5),
				{Index: 7, ID: "failed", Status: Failed, Repeat: &Repeat{Total: 3, Failure: 3}},
			}},
			{ID: "new", Steps: []Step{repeated(0, "search", 10, 10, 1)}},
		},
	}

	r.Compare(base)

	if want := (&BaselineRun{RunID: "run-1", StartedAt: start}); !reflect.DeepEqual(r.Baseline, want) {
		t.Errorf("Baseline = %+v, want %+v", r.Baseline, want)
	}
	steps := r.Jobs[0].Steps
	tests := []struct {
		name  string
		step  Step
		p50Ms float64 // of the baseline; 0 for none
	}{
		{"a step of the same id in the job of the same id", steps[1], 100},
		{"the first step of an id shared by several", steps[2], 10},
		{"the second step of an id shared by several", steps[3], 20},
		{"a step of an id the baseline has fewer of", steps[4], 0},
		{"a step the baseline has none of", steps[5], 0},
		{"a step that ran once in the baseline", steps[6], 0},
		{"a step whose action returned no time", steps[7], 0},
		{"a step of a job the baseline has none of", r.Jobs[1].Steps[0], 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			b := tt.step.Repeat.Baseline
			if tt.p50Ms == 0 {
				if b != nil {
					t.Errorf("Baseline = %+v, want none", b)
				}
				return
			}
			if b == nil || b.Latency == nil || b.Latency.P50Ms != tt.p50Ms {
				t.Fatalf("Baseline = %+v, want one with p50 %vms", b, tt.p50Ms)
			}
		})
	}
	if b := steps[1].Repeat.Baseline; b.Total != 10 || b.Success != 9 {
		t.Errorf("Baseline runs = %d of %d, want 9 of 10", b.Success, b.Total)
	}
}

func TestReport_CompareNone(t *testing.T) {
	r := &Report{Jobs: []Job{{ID: "api", Steps: []Step{repeated(0, "search", 10, 10, 1)}}}}
	r.Compare(nil)
	if r.Baseline != nil || r.Jobs[0].Steps[0].Repeat.Baseline != nil {
		t.Errorf("a report compared to none should have no baseline, got %+v", r)
	}
}

func TestRead(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "report.json")
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	want := &Report{Name: "Demo", Status: Passed, Jobs: []Job{{ID: "api", Steps: []Step{repeated(0, "search", 10, 10, 1)}}}}
	if err := want.WriteJSON(f); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}

	got, err := Read(path)
	if err != nil {
		t.Fatalf("Read() error = %v", err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Read() = %+v, want %+v", got, want)
	}

	broken := filepath.Join(dir, "broken.json")
	if err := os.WriteFile(broken, []byte("{"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Read(broken); err == nil || !strings.Contains(err.Error(), "failed to parse report") {
		t.Errorf("Read() of a broken file error = %v, want a parse error", err)
	}
	if _, err := Read(filepath.Join(dir, "none.json")); err == nil || !strings.Contains(err.Error(), "failed to read report") {
		t.Errorf("Read() of a missing file error = %v, want a read error", err)
	}
}

func TestReport_WriteMarkdown_Baseline(t *testing.T) {
	r := &Report{
		Name: "Latency", Status: Passed,
		Jobs: []Job{{
			ID: "search", Name: "Search | API", Status: Passed,
			Steps: []Step{
				{Index: 0, Name: "Login", Status: Passed},
				repeated(1, "query", 49, 50, 135),
				repeated(2, "added", 10, 10, 5),
				{Index: 3, Name: "Errors only", Status: Failed, Repeat: &Repeat{Total: 3, Failure: 3}},
			},
		}},
	}
	r.Jobs[0].Steps[1].Repeat.Baseline = &RepeatBaseline{
		Total: 40, Success: 40,
		Latency: &Latency{Count: 40, P50Ms: 120, P95Ms: 270, P99Ms: 270},
	}
	r.Baseline = &BaselineRun{RunID: "run-1", StartedAt: time.Date(2026, 10, 15, 18, 0, 0, 0, time.FixedZone("JST", 9*60*60))}

	out := r.markdown(true)
	want := "## Compared to the Baseline\n\n" +
		"Compared to the run started at 2026-10-15T09:00:00Z (run-1), before → now.\n\n" +
		"| Job | Step | Runs | p50 | p95 | p99 | Success |\n" +
		"|---|---|---|---|---|---|---|\n" +
		"| Search \\| API | 1. query | 40 → 50 | 120ms → 135ms (+12.5%) | 270ms → 270ms | 270ms → 405ms (+50.0%) | 100.0% → 98.0% |\n" +
		"| Search \\| API | 2. added | 10 (no baseline) | 5.0ms | 10.0ms | 15.0ms | 100.0% |\n"
	if !strings.Contains(out, want) {
		t.Errorf("Markdown should contain\n%s\ngot:\n%s", want, out)
	}
	if strings.Contains(out, "Errors only |") {
		t.Error("a step whose action returned no time should have no row")
	}

	_, cuts := r.markdownWithCuts(true)
	at := strings.Index(out, "\n## Compared to the Baseline")
	if !slices.Contains(cuts, at) {
		t.Errorf("cuts = %v, want one before the comparison at %d", cuts, at)
	}
}

func TestReport_WriteMarkdown_NoBaseline(t *testing.T) {
	r := &Report{Name: "Latency", Status: Passed, Jobs: []Job{{ID: "api", Steps: []Step{repeated(0, "search", 10, 10, 1)}}}}
	if out := r.markdown(true); strings.Contains(out, "## Compared to the Baseline") {
		t.Errorf("a report compared to none should have no comparison, got:\n%s", out)
	}
}

func TestComparison_BaselineWithoutTimes(t *testing.T) {
	rp := repeated(0, "search", 10, 10, 100).Repeat
	rp.Baseline = &RepeatBaseline{Total: 5, Success: 0}
	want := []string{"0 → 10", "- → 100ms", "- → 200ms", "- → 300ms", "0.0% → 100.0%"}
	if got := comparison(rp); !reflect.DeepEqual(got, want) {
		t.Errorf("comparison() = %q, want %q", got, want)
	}
}

func TestMsChange(t *testing.T) {
	tests := []struct {
		before, now float64
		want        string
	}{
		{120, 135, "120ms → 135ms (+12.5%)"},
		{1.1, 1.14, "1.1ms → 1.1ms"},
		{0.004, 0.006, "0.0ms → 0.0ms"},
		{0, 5, "0.0ms → 5.0ms"},
	}
	for _, tt := range tests {
		if got := msChange(tt.before, tt.now); got != tt.want {
			t.Errorf("msChange(%v, %v) = %q, want %q", tt.before, tt.now, got, tt.want)
		}
	}
}

func TestRepeatBaseline_Summary(t *testing.T) {
	now := &Latency{Count: 10, P50Ms: 135, P95Ms: 270, P99Ms: 5}
	tests := []struct {
		name     string
		baseline *RepeatBaseline
		want     string
	}{
		{"times", &RepeatBaseline{Latency: &Latency{P50Ms: 120, P95Ms: 300, P99Ms: 0}}, "baseline p50 120ms (+12.5%), p95 300ms (-10.0%), p99 0.0ms"},
		{"no times", &RepeatBaseline{Total: 3}, "baseline has no times"},
		{"no step", nil, "no baseline"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.baseline.Summary(now); got != tt.want {
				t.Errorf("Summary() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestBaselineSteps_Next(t *testing.T) {
	base := &Report{Jobs: []Job{{ID: "api", Steps: []Step{
		repeated(0, "each", 10, 10, 10),
		repeated(1, "each", 10, 10, 20),
		{Index: 2, ID: "once", Status: Passed},
	}}}}

	steps := base.BaselineSteps("api")
	for i, want := range []float64{10, 20} {
		if b := steps.Next("each"); b == nil || b.Latency.P50Ms != want {
			t.Errorf("Next() %d = %+v, want p50 %vms", i, b, want)
		}
	}
	for _, id := range []string{"each", "once", ""} {
		if b := steps.Next(id); b != nil {
			t.Errorf("Next(%q) = %+v, want none", id, b)
		}
	}

	if all := base.BaselineSteps("api").All(); len(all["each"]) != 2 || all["each"][1].Latency.P50Ms != 20 || len(all) != 1 {
		t.Errorf("All() = %+v, want the two steps of each in order", all)
	}
	if all := steps.All(); len(all["each"]) != 0 {
		t.Errorf("All() after Next = %+v, want none of each left", all)
	}

	var none *Report
	if all := none.BaselineSteps("api").All(); all != nil {
		t.Errorf("All() of no baseline = %+v, want none", all)
	}
	if b := none.BaselineSteps("api").Next("each"); b != nil {
		t.Errorf("Next() of no baseline = %+v, want none", b)
	}
	if b := base.BaselineSteps("other").Next("each"); b != nil {
		t.Errorf("Next() of a job the baseline has none of = %+v, want none", b)
	}
}
