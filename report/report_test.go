package report

import (
	"bytes"
	"encoding/json"
	"encoding/xml"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/mozership/probe/mask"
)

// newTestReport returns the report of a run with a job of every outcome: a
// failed job whose steps pass, fail an assertion, fail in the action, are
// skipped or have no test; a skipped job; a repeated step that failed one
// iteration; and a job that failed before any step ran.
func newTestReport() *Report {
	start := time.Date(2026, 10, 2, 12, 0, 0, 0, time.UTC)
	return &Report{
		Name:        "Demo",
		Description: "A demo run",
		Status:      Failed,
		StartedAt:   start,
		FinishedAt:  start.Add(3 * time.Second),
		DurationMs:  3000,
		Summary: Summary{
			Jobs:  Count{Total: 4, Failed: 3, Skipped: 1},
			Steps: Count{Total: 6, Passed: 1, Failed: 3, Skipped: 1, Untested: 1},
		},
		Jobs: []Job{
			{
				ID: "api", Name: "API", Status: Failed, StartedAt: start, DurationMs: 1500,
				Steps: []Step{
					{Index: 0, Name: "Health", Status: Passed, Test: "res.code == 200", DurationMs: 120, Echo: "line 1\nline 2"},
					{Index: 1, Name: "Create", Status: Failed, Test: "res.code == 201", DurationMs: 80,
						Retry: &Retry{Attempts: 3, Max: 3},
						Failure: &Failure{
							Kind:     "assertion",
							Message:  "test evaluated to false",
							Request:  map[string]any{"url": "http://example.test/items"},
							Response: map[string]any{"code": 500, "body": "boom ``` ]]>"},
						}},
					{Index: 2, Name: "Connect", Status: Failed, Failure: &Failure{Kind: "action", Message: "connection refused"}},
					{Index: 3, Name: "Cleanup", Status: Skipped},
					{Index: 4, Name: "Note", Status: Untested},
				},
			},
			{ID: "after", Name: "After | API", Status: Skipped, StartedAt: start, Steps: []Step{}},
			{
				ID: "poll", Name: "Poll", Status: Failed, StartedAt: start, DurationMs: 3000,
				Steps: []Step{
					{Index: 0, Name: "Tick", Status: Failed, Test: "res.code == 0", Repeat: &Repeat{Total: 3, Success: 2, Failure: 1}},
				},
			},
			{ID: "setup", Name: "Setup", Status: Failed, StartedAt: start, DurationMs: 10, Steps: []Step{}},
		},
	}
}

func TestParseTargets(t *testing.T) {
	tests := []struct {
		name      string
		input     string
		want      []Target
		expectErr string
	}{
		{
			name:  "empty",
			input: "",
			want:  nil,
		},
		{
			name:  "format without a path uses the default file",
			input: "json",
			want:  []Target{{Format: JSON, Path: "probe-report.json"}},
		},
		{
			name:  "every format with and without paths",
			input: "junit=out/junit.xml, JSON ,markdown=summary.md",
			want: []Target{
				{Format: JUnit, Path: "out/junit.xml"},
				{Format: JSON, Path: "probe-report.json"},
				{Format: Markdown, Path: "summary.md"},
			},
		},
		{
			name:  "empty entries are ignored",
			input: ",junit,,",
			want:  []Target{{Format: JUnit, Path: "probe-junit.xml"}},
		},
		{
			name:  "empty path falls back to the default",
			input: "markdown=",
			want:  []Target{{Format: Markdown, Path: "probe-report.md"}},
		},
		{
			name:      "unknown format",
			input:     "json,html=out.html",
			expectErr: "unknown report format: html",
		},
		{
			name:      "same format twice",
			input:     "json=a.json,json=b.json",
			expectErr: "report format given more than once: json",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseTargets(tt.input)
			if tt.expectErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.expectErr) {
					t.Fatalf("error = %v, want it to contain %q", err, tt.expectErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("got %#v, want %#v", got, tt.want)
			}
		})
	}
}

func TestReport_WriteJSON(t *testing.T) {
	r := newTestReport()

	var buf bytes.Buffer
	if err := r.WriteJSON(&buf); err != nil {
		t.Fatal(err)
	}

	var decoded Report
	if err := json.Unmarshal(buf.Bytes(), &decoded); err != nil {
		t.Fatalf("output is not valid JSON: %v", err)
	}
	if decoded.Summary != r.Summary {
		t.Errorf("summary = %+v, want %+v", decoded.Summary, r.Summary)
	}

	out := buf.String()
	for _, want := range []string{`"status": "failed"`, `"kind": "assertion"`, `"duration_ms": 3000`} {
		if !strings.Contains(out, want) {
			t.Errorf("JSON should contain %s", want)
		}
	}
	if strings.Contains(out, `"failure": null`) || strings.Contains(out, `"retry": null`) {
		t.Error("absent optional fields should be omitted, not null")
	}
}

func TestReport_WriteJUnit(t *testing.T) {
	r := newTestReport()

	var buf bytes.Buffer
	if err := r.WriteJUnit(&buf); err != nil {
		t.Fatal(err)
	}
	out := buf.String()

	if !strings.HasPrefix(out, `<?xml version="1.0" encoding="UTF-8"?>`) {
		t.Error("JUnit output should start with the XML header")
	}

	var doc junitTestSuites
	if err := xml.Unmarshal(buf.Bytes(), &doc); err != nil {
		t.Fatalf("output is not valid XML: %v\n%s", err, out)
	}

	// api: 5 steps; after: 1 synthetic skipped case; poll: 1 step; setup: 1 synthetic error case
	if doc.Tests != 8 || doc.Failures != 2 || doc.Errors != 2 || doc.Skipped != 2 {
		t.Errorf("totals = tests %d failures %d errors %d skipped %d, want 8/2/2/2",
			doc.Tests, doc.Failures, doc.Errors, doc.Skipped)
	}
	if len(doc.Suites) != 4 {
		t.Fatalf("suites = %d, want 4", len(doc.Suites))
	}

	api := doc.Suites[0]
	if api.Name != "API" || api.Timestamp != "2026-10-02T12:00:00Z" || api.Time != "1.500" {
		t.Errorf("suite = %s %s %s, want API 2026-10-02T12:00:00Z 1.500", api.Name, api.Timestamp, api.Time)
	}
	create := api.Cases[1]
	if create.Failure == nil || create.Failure.Type != FailureAssertion {
		t.Fatalf("a false test should be a <failure>, got %+v", create)
	}
	if !strings.Contains(create.Failure.Body, "boom ``` ]]>") {
		t.Errorf("failure body should survive CDATA intact, got %q", create.Failure.Body)
	}
	if !strings.Contains(create.Failure.Body, "test: res.code == 201") {
		t.Errorf("failure body should name the test, got %q", create.Failure.Body)
	}
	if connect := api.Cases[2]; connect.Error == nil || connect.Error.Type != FailureAction {
		t.Errorf("an action error should be an <error>, got %+v", connect)
	}
	if api.Cases[3].Skipped == nil {
		t.Error("a skipped step should carry <skipped>")
	}
	if out := api.Cases[0].SystemOut; out == nil || out.Text != "line 1\nline 2" {
		t.Errorf("echo should land in system-out, got %+v", out)
	}

	if after := doc.Suites[1]; len(after.Cases) != 1 || after.Cases[0].Skipped == nil {
		t.Errorf("a job skipped before any step should get one skipped case, got %+v", after.Cases)
	}
	if poll := doc.Suites[2].Cases[0]; poll.Failure == nil || poll.Failure.Message != "1 of 3 iterations failed" {
		t.Errorf("a failing repeat should report its iterations, got %+v", poll.Failure)
	}
	if setup := doc.Suites[3]; len(setup.Cases) != 1 || setup.Cases[0].Error == nil {
		t.Errorf("a job that failed without a failed step should get one error case, got %+v", setup.Cases)
	}
}

func TestReport_WriteMarkdown(t *testing.T) {
	r := newTestReport()

	var buf bytes.Buffer
	if err := r.WriteMarkdown(&buf); err != nil {
		t.Fatal(err)
	}
	out := buf.String()

	for _, want := range []string{
		"# Demo\n\nA demo run\n\n",
		"**Failed** in 3.00s. Jobs: 4 total, 3 failed, 1 skipped. Steps: 6 total, 1 passed, 3 failed, 1 skipped, 1 untested.\n",
		"| API | failed | 1/5 | 1.50s |\n",
		"| After \\| API | skipped | 0/0 | 0.00s |\n",
		"## Failures\n\n### API / 1. Create\n\nassertion: test evaluated to false\n\n```\nres.code == 201\n```\n",
		"<details><summary>Response</summary>\n\n````json\n",
		"### API / 2. Connect\n\naction: connection refused\n",
		"### Poll / 0. Tick\n\n2 of 3 iterations succeeded.\n",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("Markdown should contain %q, got:\n%s", want, out)
		}
	}
	if strings.Contains(out, "Request</summary>") && !strings.Contains(out, `"url": "http://example.test/items"`) {
		t.Error("request block should hold the request")
	}
	if strings.Contains(out, "### API / 2. Connect\n\naction: connection refused\n\n<details>") {
		t.Error("a failure without a request or response should not render empty blocks")
	}
	if !strings.HasSuffix(out, "\n") || strings.HasSuffix(out, "\n\n") {
		t.Error("the page should end in exactly one newline")
	}
}

func TestReport_WriteMarkdown_NoFailures(t *testing.T) {
	r := &Report{Name: "Green", Status: Passed, Summary: Summary{Jobs: Count{Total: 1, Passed: 1}}}

	var buf bytes.Buffer
	if err := r.WriteMarkdown(&buf); err != nil {
		t.Fatal(err)
	}
	out := buf.String()

	if strings.Contains(out, "## Failures") {
		t.Errorf("a passing run should have no failures section, got:\n%s", out)
	}
	if !strings.Contains(out, "**Passed** in 0.00s. Jobs: 1 total, 1 passed. Steps: 0 total.") {
		t.Errorf("summary line missing, got:\n%s", out)
	}
}

func TestReport_WriteMarkdown_ResponseTimes(t *testing.T) {
	r := &Report{
		Name: "Latency", Status: Passed,
		Jobs: []Job{{
			ID: "search", Name: "Search | API", Status: Passed,
			Steps: []Step{
				{Index: 0, Name: "Login", Status: Passed},
				{Index: 1, Name: "Query", Status: Passed, Repeat: &Repeat{
					Total: 50, Success: 50,
					Latency: &Latency{Count: 50, MinMs: 8.2, MeanMs: 15, P50Ms: 12.5, P90Ms: 30, P95Ms: 120, P99Ms: 900, MaxMs: 1500},
				}},
				{Index: 2, Name: "Errors only", Status: Failed, Repeat: &Repeat{Total: 3, Failure: 3}},
			},
		}},
	}

	out := r.markdown(true)
	want := "## Response Times\n\n" +
		"| Job | Step | Runs | Min | p50 | p90 | p95 | p99 | Max |\n" +
		"|---|---|---|---|---|---|---|---|---|\n" +
		"| Search \\| API | 1. Query | 50 | 8.2ms | 12.5ms | 30.0ms | 120ms | 900ms | 1.50s |\n"
	if !strings.Contains(out, want) {
		t.Errorf("Markdown should contain\n%s\ngot:\n%s", want, out)
	}
	if strings.Contains(out, "Errors only |") {
		t.Error("a step whose action returned no time should have no row")
	}

	_, cuts := r.markdownWithCuts(true)
	at := strings.Index(out, "\n## Response Times")
	if !slices.Contains(cuts, at) {
		t.Errorf("cuts = %v, want one before the response times at %d", cuts, at)
	}
}

func TestReport_WriteMarkdown_NoResponseTimes(t *testing.T) {
	if out := newTestReport().markdown(true); strings.Contains(out, "## Response Times") {
		t.Errorf("a run without times should have no response times section, got:\n%s", out)
	}
}

func TestWriteFence(t *testing.T) {
	var b strings.Builder
	writeFence(&b, "json", "a ```` b")
	if want := "`````json\na ```` b\n`````\n\n"; b.String() != want {
		t.Errorf("got %q, want %q", b.String(), want)
	}
}

func TestReport_Write(t *testing.T) {
	r := newTestReport()
	dir := t.TempDir()

	for _, format := range []Format{JSON, JUnit, Markdown} {
		path := filepath.Join(dir, "nested", "dir", string(format))
		if err := r.Write(Target{Format: format, Path: path}); err != nil {
			t.Fatalf("%s: %v", format, err)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("%s: %v", format, err)
		}
		if len(data) == 0 {
			t.Errorf("%s: file is empty", format)
		}
	}

	// A directory where the file should go cannot be created over.
	blocked := filepath.Join(dir, "blocked")
	if err := os.Mkdir(blocked, 0o755); err != nil {
		t.Fatal(err)
	}
	err := r.Write(Target{Format: JSON, Path: blocked})
	if err == nil || !strings.Contains(err.Error(), "failed to create json report") {
		t.Errorf("error = %v, want a create failure", err)
	}
}

// TestReport_WriteDoesNotFollowSymlink covers a project that ships a report
// path as a symlink to a file elsewhere: writing the report must replace the
// link, not overwrite the file it points to.
func TestReport_WriteDoesNotFollowSymlink(t *testing.T) {
	r := newTestReport()

	for _, format := range []Format{JSON, JUnit, Markdown} {
		t.Run(string(format), func(t *testing.T) {
			root := t.TempDir()
			target := filepath.Join(root, "outside.txt")
			if err := os.WriteFile(target, []byte("keep me"), 0o600); err != nil {
				t.Fatal(err)
			}
			dir := filepath.Join(root, "project")
			if err := os.Mkdir(dir, 0o755); err != nil {
				t.Fatal(err)
			}
			link := filepath.Join(dir, "report")
			if err := os.Symlink(target, link); err != nil {
				t.Skipf("symlinks unavailable: %v", err)
			}

			if err := r.Write(Target{Format: format, Path: link}); err != nil {
				t.Fatal(err)
			}

			if data, _ := os.ReadFile(target); string(data) != "keep me" {
				t.Errorf("the file the link pointed at was changed to %.40q", data)
			}
			info, err := os.Lstat(link)
			if err != nil {
				t.Fatal(err)
			}
			if info.Mode()&os.ModeSymlink != 0 {
				t.Error("the report should now be a regular file, not the link")
			}
			if info.Mode().Perm() != umaskMode(t) {
				t.Errorf("mode = %v, want %v as the umask allows", info.Mode().Perm(), umaskMode(t))
			}
			if data, _ := os.ReadFile(link); !strings.Contains(string(data), "Demo") {
				t.Errorf("the report was not written: %.60q", data)
			}
			leftovers, _ := filepath.Glob(filepath.Join(dir, ".report.*"))
			if len(leftovers) != 0 {
				t.Errorf("temporary files left behind: %v", leftovers)
			}
		})
	}
}

func TestReport_WriteReplacesExistingFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "report.md")
	if err := os.WriteFile(path, []byte("an older, much longer report than the new one\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	r := &Report{Name: "New", Status: Passed}
	if err := r.Write(Target{Format: Markdown, Path: path}); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(path)
	if !strings.HasPrefix(string(data), "# New\n") || strings.Contains(string(data), "older") {
		t.Errorf("the old report should be replaced entirely, got:\n%s", data)
	}
}

// TestReport_WriteRefusesLinkedDirectory covers a report path whose directory
// is a symlink out of the project, as in --report json=out/report.json with a
// checked-out "out" link.
func TestReport_WriteRefusesLinkedDirectory(t *testing.T) {
	victim := checkout(t)

	err := newTestReport().Write(Target{Format: JSON, Path: "out/victim"})
	if err == nil || !strings.Contains(err.Error(), "failed to create json report") {
		t.Errorf("error = %v, want the report refused", err)
	}
	unchanged(t, victim)
}

// checkout makes a working directory with a file outside it and a symlink
// "out" inside it pointing at the outside directory, as a checked-out
// project could carry. It returns the outside file.
func checkout(t *testing.T) (outsideFile string) {
	t.Helper()
	base := t.TempDir()
	outside := filepath.Join(base, "outside")
	project := filepath.Join(base, "project")
	for _, d := range []string{outside, project} {
		if err := os.Mkdir(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	outsideFile = filepath.Join(outside, "victim")
	if err := os.WriteFile(outsideFile, []byte("keep me"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(project, "out")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	t.Chdir(project)
	return outsideFile
}

func unchanged(t *testing.T, path string) {
	t.Helper()
	if data, _ := os.ReadFile(path); string(data) != "keep me" {
		t.Errorf("%s was changed to %.40q", path, data)
	}
}

// umaskMode is the mode os.Create gives a new file under the current umask.
func umaskMode(t *testing.T) os.FileMode {
	t.Helper()
	ref := filepath.Join(t.TempDir(), "ref")
	f, err := os.Create(ref)
	if err != nil {
		t.Fatal(err)
	}
	_ = f.Close()
	info, err := os.Stat(ref)
	if err != nil {
		t.Fatal(err)
	}
	return info.Mode().Perm()
}

func TestReport_Mask(t *testing.T) {
	m := mask.New([]string{"S"}, map[string]string{"S": "sekret"})
	r := &Report{
		Name:        "run sekret",
		Description: "desc sekret",
		Jobs: []Job{{
			Name: "job sekret",
			Steps: []Step{{
				Name: "step sekret",
				Test: `res.body == "sekret"`,
				Echo: "echo sekret",
				Failure: &Failure{
					Kind:     FailureAssertion,
					Message:  "msg sekret",
					Request:  map[string]any{"authorization": "Bearer z", "q": "sekret"},
					Response: map[string]any{"body": "sekret"},
					Violations: []Violation{{
						In:      "response",
						Field:   "$.sekret",
						Reason:  "got sekret",
						Message: "body sekret",
					}},
				},
			}},
		}},
	}

	r.Mask(m)

	var buf bytes.Buffer
	if err := r.WriteJSON(&buf); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(buf.String(), "sekret") || strings.Contains(buf.String(), "Bearer z") {
		t.Errorf("report still shows a secret:\n%s", buf.String())
	}
	if got := strings.Count(buf.String(), "<secret:S>"); got != 12 {
		t.Errorf("masked %d values, want 12:\n%s", got, buf.String())
	}
}

func TestViolation_String(t *testing.T) {
	tests := []struct {
		v    Violation
		want string
	}{
		{Violation{In: "response", Message: "path not found"}, "response: path not found"},
		{Violation{In: "response", Message: "body failed", Reason: "missing name"}, "response: body failed: missing name"},
		{Violation{In: "response", Field: "$.id", Message: "body failed", Reason: "want integer"}, "response: $.id: body failed: want integer"},
	}
	for _, tt := range tests {
		if got := tt.v.String(); got != tt.want {
			t.Errorf("String() = %q, want %q", got, tt.want)
		}
	}
}

func TestFailureDetail_Violations(t *testing.T) {
	got := failureDetail(Step{Failure: &Failure{
		Kind:    FailureContractResponse,
		Message: "the response breaks its contract in 1 place",
		Violations: []Violation{
			{In: "response", Field: "$.id", Message: "body failed", Reason: "want integer"},
		},
	}})
	want := "contract_response: the response breaks its contract in 1 place\n- response: $.id: body failed: want integer\n"
	if got != want {
		t.Errorf("failureDetail = %q, want %q", got, want)
	}
}

// newContractReport returns the report of a run whose one step broke its
// contract.
func newContractReport() *Report {
	return &Report{
		Name:   "Contract",
		Status: Failed,
		Jobs: []Job{{
			ID: "users", Name: "Users", Status: Failed,
			Steps: []Step{{
				Index: 0, Name: "Get a user", Status: Failed,
				Failure: &Failure{
					Kind:     FailureContractResponse,
					Message:  "the response breaks its contract in 1 place",
					Response: map[string]any{"code": 200},
					Violations: []Violation{
						{In: "response", Field: "$.id", Message: "body failed", Reason: "want integer"},
					},
				},
			}},
		}},
	}
}

func TestReport_WriteJUnit_Contract(t *testing.T) {
	var buf bytes.Buffer
	if err := newContractReport().WriteJUnit(&buf); err != nil {
		t.Fatal(err)
	}
	var doc junitTestSuites
	if err := xml.Unmarshal(buf.Bytes(), &doc); err != nil {
		t.Fatalf("output is not valid XML: %v\n%s", err, buf.String())
	}
	c := doc.Suites[0].Cases[0]
	if c.Failure == nil || c.Failure.Type != FailureContractResponse {
		t.Fatalf("a broken contract should be a <failure>, got %+v", c)
	}
	if !strings.Contains(c.Failure.Body, "- response: $.id: body failed: want integer") {
		t.Errorf("failure body should list the violation, got %q", c.Failure.Body)
	}
}

func TestReport_WriteMarkdown_Contract(t *testing.T) {
	var buf bytes.Buffer
	if err := newContractReport().WriteMarkdown(&buf); err != nil {
		t.Fatal(err)
	}
	want := "contract_response: the response breaks its contract in 1 place\n\n- response: $.id: body failed: want integer\n\n"
	if !strings.Contains(buf.String(), want) {
		t.Errorf("markdown should list the violation after the kind, got:\n%s", buf.String())
	}
}

func TestReport_WriteJUnit_ContractRequest(t *testing.T) {
	r := newContractReport()
	f := r.Jobs[0].Steps[0].Failure
	f.Kind = FailureContractRequest
	f.Violations[0].In = "request"

	var buf bytes.Buffer
	if err := r.WriteJUnit(&buf); err != nil {
		t.Fatal(err)
	}
	var doc junitTestSuites
	if err := xml.Unmarshal(buf.Bytes(), &doc); err != nil {
		t.Fatalf("output is not valid XML: %v\n%s", err, buf.String())
	}
	// A request the contract does not allow is a mistake in the test, as a
	// test that cannot be evaluated is.
	if c := doc.Suites[0].Cases[0]; c.Error == nil || c.Error.Type != FailureContractRequest {
		t.Errorf("a request that breaks its contract should be an <error>, got %+v", c)
	}
}
