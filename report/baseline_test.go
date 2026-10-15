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
		"| Search \\| API | 1. query | 40 → 50 | 120ms → 135ms (+12.5%) | 270ms → 270ms (+0.0%) | 270ms → 405ms (+50.0%) | 100.0% → 98.0% |\n" +
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
