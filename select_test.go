package probe

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/mozership/probe/report"
)

// selectionJobs is a workflow in which tests need setup, which needs db,
// cleanup always runs after setup and tests, notify always runs after
// cleanup, and lint and its own cleanup stand apart.
func selectionJobs() []Job {
	return []Job{
		{ID: "db", Name: "Database"},
		{ID: "setup", Name: "Setup", Needs: []string{"db"}},
		{ID: "tests", Name: "Tests", Needs: []string{"setup"}},
		{ID: "lint", Name: "Lint"},
		{ID: "cleanup", Name: "Cleanup", Needs: []string{"setup", "tests"}, Always: true},
		{ID: "notify", Name: "Notify", Needs: []string{"cleanup"}, Always: true},
		{ID: "lint_cleanup", Name: "Lint cleanup", Needs: []string{"lint"}, Always: true},
		{ID: "job_7", Name: "Tests"},
	}
}

func TestSelectJobs(t *testing.T) {
	tests := []struct {
		name  string
		names []string
		// want are the ids of the jobs that run, and needs those of the
		// jobs each of them that always runs waits for.
		want  []string
		needs map[string][]string
		line  string
	}{
		{
			name:  "a job by its id runs with what it needs, however far down, and what always follows",
			names: []string{"tests"},
			want:  []string{"db", "setup", "tests", "cleanup", "notify"},
			line:  "Running 5 of 8 jobs: Tests, Database (needed), Setup (needed), Cleanup (always), Notify (always)",
		},
		{
			name:  "a job by its name",
			names: []string{"Lint"},
			want:  []string{"lint", "lint_cleanup"},
			line:  "Running 2 of 8 jobs: Lint, Lint cleanup (always)",
		},
		{
			name:  "a job that always runs waits only for the jobs it needs that run",
			names: []string{"setup"},
			want:  []string{"db", "setup", "cleanup", "notify"},
			needs: map[string][]string{"cleanup": {"setup"}, "notify": {"cleanup"}},
		},
		{
			name:  "a job that always runs, selected, brings what it needs",
			names: []string{"Cleanup"},
			want:  []string{"db", "setup", "tests", "cleanup", "notify"},
			needs: map[string][]string{"cleanup": {"setup", "tests"}},
		},
		{
			name:  "a job with no need runs alone",
			names: []string{"db"},
			want:  []string{"db"},
			line:  "Running 1 of 8 jobs: Database",
		},
		{
			name:  "several jobs, in the order they are written, a job given twice once",
			names: []string{"lint", "db", "lint"},
			want:  []string{"db", "lint", "lint_cleanup"},
			line:  "Running 3 of 8 jobs: Database, Lint, Lint cleanup (always)",
		},
		{
			name:  "an id wins over a name",
			names: []string{"job_7"},
			want:  []string{"job_7"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			all := selectionJobs()
			jobs, sel, err := selectJobs(all, tt.names)
			if err != nil {
				t.Fatal(err)
			}
			var got []string
			for _, j := range jobs {
				got = append(got, j.ID)
				if want, ok := tt.needs[j.ID]; ok && !reflect.DeepEqual(j.Needs, want) {
					t.Errorf("job %s needs %q, want %q", j.ID, j.Needs, want)
				}
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("selectJobs(%q) runs %q, want %q", tt.names, got, tt.want)
			}
			name := func(id string) string {
				for _, j := range all {
					if j.ID == id {
						return j.Name
					}
				}
				return id
			}
			if got := sel.describe(name); tt.line != "" && got != tt.line {
				t.Errorf("describe() = %q, want %q", got, tt.line)
			}
			// The jobs of the workflow are left as they are written.
			if !reflect.DeepEqual(all, selectionJobs()) {
				t.Errorf("selectJobs changed the jobs it was given: %+v", all)
			}
		})
	}
}

func TestSelectJobs_Errors(t *testing.T) {
	tests := []struct {
		name  string
		names []string
		want  string
	}{
		{"a name no job has", []string{"lnit"}, `no job has the id or name "lnit"; did you mean "lint"?`},
		{"a name too far from any", []string{"deploy"}, `no job has the id or name "deploy"`},
		{"a name of another case", []string{"setup", "notify", "TESTS"}, `no job has the id or name "TESTS"; did you mean "tests"?`},
		{"a name several jobs have", []string{"Tests"}, `2 jobs are named "Tests"; select one by its id: tests, job_7`},
		{"a name a template makes", []string{"Lint staging"}, `no job has the id or name "Lint staging"; a job whose name has a template is selected by its id`},
		{"a name with an unclosed opener", []string{"Lint staging"}, `no job has the id or name "Lint staging"`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			jobs := selectionJobs()
			switch {
			case strings.Contains(tt.name, "template"):
				jobs[3].Name = "Lint {{vars.env}}"
			case strings.Contains(tt.name, "unclosed"):
				// Not a template: it is the name as written.
				jobs[3].Name = "Lint {{ vars.env"
			}
			_, _, err := selectJobs(jobs, tt.names)
			if err == nil || err.Error() != tt.want {
				t.Errorf("selectJobs(%q) = %v, want %q", tt.names, err, tt.want)
			}
		})
	}
}

// A run of some jobs is the run of those alone: the others do not run and
// are not in the report, the exit status is theirs, and a job that cleans
// up after a selected one runs.
func TestWorkflow_Start_SelectedJobs(t *testing.T) {
	failing := NewMockActionRunner()
	failing.SetError("hello", errors.New("boom"))
	setup := NewMockActionRunner()
	cleanup := NewMockActionRunner()
	other := NewMockActionRunner()

	dir := t.TempDir()
	path := filepath.Join(dir, "report.json")
	md := filepath.Join(dir, "report.md")
	w := &Workflow{
		Name: "Selected",
		Vars: map[string]any{"env": "stg"},
		Jobs: []Job{
			{ID: "setup", Name: "Setup {{vars.env}}", Steps: []*Step{{Name: "Prepares", Uses: "hello", actionRunner: setup}}},
			{ID: "tests", Name: "Tests", Needs: []string{"setup"}, Steps: []*Step{{Name: "Fails", Uses: "hello", actionRunner: failing}}},
			{Name: "Other", Steps: []*Step{{Name: "Not run", Uses: "hello", actionRunner: other}}},
			{Name: "Cleanup", Needs: []string{"setup", "tests"}, Always: true, Steps: []*Step{{Name: "Removes", Uses: "hello", actionRunner: cleanup}}},
		},
	}
	out := startCapturingWith(t, w, Config{
		Output:  OutputModeSpinner,
		Jobs:    []string{"Tests"},
		Reports: []report.Target{{Format: report.JSON, Path: path}, {Format: report.Markdown, Path: md}},
	})

	if len(setup.Calls["hello"]) != 1 || len(cleanup.Calls["hello"]) != 1 {
		t.Errorf("the job needed and the one that always follows should run once each:\n%s", out)
	}
	if len(other.Calls["hello"]) != 0 || strings.Contains(out, "Other") {
		t.Errorf("the job not selected ran or is reported:\n%s", out)
	}
	if w.exitStatus != ExitActionError {
		t.Errorf("exit status = %d, want %d of the failed job selected", w.exitStatus, ExitActionError)
	}
	errOut := w.printer.errWriter.(interface{ String() string }).String()
	// A name is said as the report says it, with the vars.
	if want := "Running 3 of 4 jobs: Tests, Setup stg (needed), Cleanup (always)"; !strings.Contains(errOut, want) {
		t.Errorf("error output should contain %q, got: %s", want, errOut)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var r report.Report
	if err := json.Unmarshal(data, &r); err != nil {
		t.Fatal(err)
	}
	var ids []string
	for _, j := range r.Jobs {
		ids = append(ids, j.ID)
	}
	// The job without an ID keeps the one its place in the workflow gives.
	if want := []string{"setup", "tests", "job_3"}; !reflect.DeepEqual(ids, want) {
		t.Errorf("the report has the jobs %q, want %q", ids, want)
	}
	if r.Summary.Jobs.Total != 3 || r.Summary.Jobs.Failed != 1 || r.Summary.Jobs.Skipped != 0 {
		t.Errorf("the report counts %+v, want 3 jobs, 1 failed and none skipped", r.Summary.Jobs)
	}
	if want := (&report.Selection{Jobs: []string{"tests"}, Total: 4}); !reflect.DeepEqual(r.Selection, want) {
		t.Errorf("the report records the selection %+v, want %+v", r.Selection, want)
	}
	page, err := os.ReadFile(md)
	if err != nil {
		t.Fatal(err)
	}
	if want := "3 of the 4 jobs of the workflow ran, selected with --job: tests.\n"; !strings.Contains(string(page), want) {
		t.Errorf("the markdown report should say %q, got:\n%s", want, page)
	}
}

// A run of every job records no selection.
func TestWorkflow_Start_NoSelection(t *testing.T) {
	path := filepath.Join(t.TempDir(), "report.json")
	w := &Workflow{Name: "All", Jobs: []Job{{ID: "a", Name: "A", Steps: []*Step{{Name: "Runs", Uses: "hello", actionRunner: NewMockActionRunner()}}}}}
	startCapturingWith(t, w, Config{Output: OutputModeSpinner, Reports: []report.Target{{Format: report.JSON, Path: path}}})
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "selection") {
		t.Errorf("the report of a run of every job records a selection:\n%s", data)
	}
	if errOut := w.printer.errWriter.(interface{ String() string }).String(); strings.Contains(errOut, "Running") {
		t.Errorf("a run of every job says which run: %s", errOut)
	}
}

// A job that cannot be found fails the run before any job runs.
func TestWorkflow_Start_UnknownJob(t *testing.T) {
	runner := NewMockActionRunner()
	w := &Workflow{
		Name: "Unknown",
		Jobs: []Job{{ID: "tests", Name: "Tests", Steps: []*Step{{Name: "Runs", Uses: "hello", actionRunner: runner}}}},
	}
	err := w.Start(Config{Jobs: []string{"test"}})
	if want := `no job has the id or name "test"; did you mean "tests"?`; err == nil || err.Error() != want {
		t.Errorf("Start() = %v, want %q", err, want)
	}
	if len(runner.Calls["hello"]) != 0 {
		t.Error("a job ran")
	}
}
