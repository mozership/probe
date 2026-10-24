package probe

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mozership/probe/actionref"
)

// writeWorkflow writes content to a file named name in dir and returns its
// path.
func writeWorkflow(t *testing.T, dir, name, content string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

// want is a finding expected: its severity, line and a part of its message.
type want struct {
	severity string
	line     int
	message  string
}

func checkFindings(t *testing.T, findings []Finding, wants []want) {
	t.Helper()
	if len(findings) != len(wants) {
		var b strings.Builder
		for _, f := range findings {
			b.WriteString("\n  " + f.String())
		}
		t.Fatalf("findings = %d, want %d:%s", len(findings), len(wants), b.String())
	}
	for i, w := range wants {
		f := findings[i]
		if f.Severity != w.severity || f.Line != w.line || !strings.Contains(f.Message, w.message) {
			t.Errorf("findings[%d] = %s, want %s on line %d saying %q", i, f, w.severity, w.line, w.message)
		}
	}
}

var checkActions = CheckOptions{
	Actions:   []string{"embedded", "grpc", "hello", "http", "shell"},
	Contracts: map[string][]string{"grpc": {"proto"}, "http": {"openapi"}},
}

func TestCheck(t *testing.T) {
	tests := []struct {
		name     string
		workflow string
		want     []want
	}{
		{
			name: "a workflow with nothing wrong",
			workflow: `name: ok
vars:
  url: http://localhost
jobs:
- name: Users
  id: users
  steps:
  - name: Log in
    id: login
    uses: http
    with:
      post: "{{vars.url}}/login"
    test: res.code == 200 && outputs.login.token != ""
    outputs:
      token: res.body.token
    echo: "{{outputs.login.token}}"
  - name: Me
    uses: http
    with:
      get: "{{vars.url}}/me"
      headers:
        authorization: "Bearer {{outputs.token}}"
    test: res.code == 200
- name: After
  needs: [users]
  steps:
  - name: Reads users
    uses: shell
    with:
      cmd: "echo {{outputs.login.token}}"
    test: res.code == 0
`,
		},
		{
			name: "keys a workflow does not take",
			workflow: `name: keys
job:
- name: Typo
jobs:
- name: Users
  need: [x]
  repeat:
    count: 2
    intervall: 1s
  steps:
  - name: Get
    uses: http
    tset: res.code == 200
    retry:
      max_attempt: 3
      max_attempts: 3
`,
			want: []want{
				{SeverityError, 2, `unknown key "job"; did you mean "jobs"?`},
				{SeverityError, 6, `unknown key "need"; did you mean "needs"?`},
				{SeverityError, 9, `unknown key "intervall"; did you mean "interval"?`},
				{SeverityWarning, 11, "nothing checks this step"},
				{SeverityError, 13, `unknown key "tset"; did you mean "test"?`},
				{SeverityError, 15, `unknown key "max_attempt"; did you mean "max_attempts"?`},
			},
		},
		{
			name: "a key that holds an anchor for the files read after it",
			workflow: `name: anchors
shared:
  url: &url http://localhost
base: &base
  uses: hello
jobs:
- name: Hello
  steps:
  - <<: *base
    name: Hello
    with:
      to: *url
    test: res.code == 0
`,
		},
		{
			name:     "YAML that does not parse",
			workflow: "name: [broken\n",
			want:     []want{{SeverityError, 0, "YAML:"}},
		},
		{
			name:     "a workflow a run refuses to load",
			workflow: "jobs:\n- name: No workflow name\n  steps:\n  - uses: hello\n    test: res.code == 0\n",
			want:     []want{{SeverityError, 0, "Name"}},
		},
		{
			name: "actions that do not exist",
			workflow: `name: actions
jobs:
- name: Steps
  steps:
  - name: Typo
    uses: htp
    test: res.code == 200
  - name: External
    uses: ./actions/mine
    test: res.code == 0
`,
			want: []want{{SeverityError, 6, `unknown action "htp"; did you mean "http"?`}},
		},
		{
			name: "needs that cannot be met",
			workflow: `name: needs
jobs:
- name: Users
  id: users
  steps:
  - uses: hello
    test: res.code == 0
- name: After
  needs: [userz]
  steps:
  - uses: hello
    test: res.code == 0
`,
			want: []want{{SeverityError, 9, `needs: no job has the id "userz"; did you mean "users"?`}},
		},
		{
			name: "needs that go round",
			workflow: `name: cycle
jobs:
- name: A
  id: a
  needs: [b]
  steps:
  - uses: hello
    test: res.code == 0
- name: B
  id: b
  needs: [a]
  steps:
  - uses: hello
    test: res.code == 0
`,
			want: []want{{SeverityError, 0, "circular"}},
		},
		{
			name: "a step id a run refuses",
			workflow: `name: ids
jobs:
- name: Steps
  steps:
  - name: Bad id
    id: Bad.Id
    uses: hello
    test: res.code == 0
`,
			want: []want{{SeverityError, 4, "invalid step ID 'Bad.Id'"}},
		},
		{
			name: "expressions and templates that do not parse",
			workflow: `name: syntax
vars:
  broken: "{{ vars.x + }}"
jobs:
- name: "{{ vars.( }}"
  skipif: vars.x ==
  steps:
  - name: Step
    id: step
    uses: http
    with:
      get: "/users/{{ vars.id ) }}"
    vars:
      v: "{{ ! }}"
    test: res.code = 200
    skipif: "("
    outputs:
      id: res.body.
    echo: "{{ res. }}"
`,
			want: []want{
				{SeverityError, 3, "vars.broken: template {{ vars.x + }}"},
				{SeverityError, 5, "name: template {{ vars.( }}"},
				{SeverityError, 6, "skipif:"},
				{SeverityError, 12, "with.get: template {{ vars.id ) }}"},
				{SeverityError, 14, "vars.v: template {{ ! }}"},
				{SeverityError, 15, "test:"},
				{SeverityError, 16, "skipif:"},
				{SeverityError, 18, "outputs.id:"},
				{SeverityError, 19, "echo: template {{ res. }}"},
			},
		},
		{
			name: "a wait that is not a time",
			workflow: `name: wait
jobs:
- name: Waits
  steps:
  - name: Bad
    uses: hello
    wait: soon
    test: res.code == 0
  - name: Templated
    uses: hello
    wait: "{{ vars.wait }}"
    test: res.code == 0
  - name: Broken template
    uses: hello
    wait: "{{ vars. }}"
    test: res.code == 0
vars:
  wait: 1s
`,
			want: []want{
				{SeverityError, 7, "wait: invalid wait format: soon"},
				{SeverityError, 15, "wait: template {{ vars. }}"},
			},
		},
		{
			name: "outputs read before or without being published",
			workflow: `name: outputs
jobs:
- name: Users
  id: users
  steps:
  - name: Early
    uses: http
    with:
      get: "/{{ outputs.later.id }}"
      headers:
        x-a: "{{ outputs.nobody }}"
        x-b: "{{ outputs.later.idd }}"
        x-c: "{{ outputs.later_id }}"
    test: res.code == 200
  - name: Later
    id: later
    uses: http
    with:
      get: "/{{ outputs.later.id }}"
    test: res.code == 200 && outputs.later.id > 0
    outputs:
      id: res.body.id
      later_id: res.body.id
- name: Other
  steps:
  - name: Without needs
    uses: shell
    with:
      cmd: "echo {{ outputs.later.id }} {{ outputs.later_id }}"
    test: res.code == 0
`,
			want: []want{
				{SeverityError, 9, "with: outputs.later.id is read before its step publishes it"},
				{SeverityError, 11, `with: outputs.nobody is not published by any step`},
				{SeverityError, 12, `with: outputs.later.idd is not published: step "later" publishes id, later_id`},
				{SeverityError, 13, "with: outputs.later_id is read before its step publishes it"},
				{SeverityError, 19, "with: outputs.later.id is read before its step publishes it"},
				{SeverityWarning, 29, `with: outputs.later.id may not be published yet: job "Other" does not need job "Users"`},
				{SeverityWarning, 29, `with: outputs.later_id may not be published yet: job "Other" does not need job "Users"`},
			},
		},
		{
			// A step without an id publishes its outputs by their names
			// alone, and is not read by where it is written.
			name: "outputs of a step without an id",
			workflow: `name: given ids
jobs:
- name: A
  id: a
  steps:
  - name: Explicit
    id: step_0
    uses: hello
    test: res.code == 0
    outputs:
      v: res.code
  - name: Without an id
    uses: hello
    test: res.code == 0
    outputs:
      w: res.code
- name: B
  needs: [a]
  steps:
  - name: Without an id and outputs
    uses: hello
    test: res.code == 0
  - name: Reads
    uses: hello
    with:
      v: "{{ outputs.step_0.v }} {{ outputs.step_1.w }}"
    test: res.code == 0
`,
			want: []want{
				{SeverityError, 26, `with: outputs.step_1.w is not published by any step; did you mean "step_0"?`},
			},
		},
		{
			name: "publishing steps of two jobs without an id",
			workflow: `name: shared ids
jobs:
- name: A
  steps:
  - uses: hello
    test: res.code == 0
    outputs:
      a: res.code
- name: B
  steps:
  - uses: hello
    test: res.code == 0
    outputs:
      b: res.code
- name: C
  steps:
  - uses: hello
    with:
      v: "{{ outputs.step_0.a }} {{ outputs.step_0.b }} {{ outputs.step_0.c }}"
    test: res.code == 0
  - uses: hello
    with:
      v: "{{ outputs.step_0 }}"
    test: res.code == 0
`,
			want: []want{
				{SeverityError, 19, `with: outputs.step_0.a is not published by any step`},
				{SeverityError, 19, `with: outputs.step_0.b is not published by any step`},
				{SeverityError, 19, `with: outputs.step_0.c is not published by any step`},
				{SeverityError, 23, `with: outputs.step_0 is not published by any step`},
			},
		},
		{
			name: "outputs of a job needed through another",
			workflow: `name: transitive
jobs:
- name: A
  id: a
  steps:
  - id: one
    uses: hello
    test: res.code == 0
    outputs:
      v: res.code
- name: B
  id: b
  needs: [a]
  steps:
  - uses: hello
    test: res.code == 0
- name: C
  needs: [b]
  steps:
  - uses: hello
    with:
      v: "{{ outputs.one.v }}"
    test: res.code == 0
`,
		},
		{
			name: "steps nothing checks and tests that read nothing",
			workflow: `name: tests
jobs:
- name: Steps
  steps:
  - name: No test
    uses: shell
  - name: Contract
    uses: http
    with:
      get: /users
      openapi:
        spec: ./openapi.yml
  - name: Contract left out
    uses: http
    with:
      get: /health
      openapi: false
  - name: Proto
    uses: grpc
    with:
      addr: localhost:50051
      service: UserService
      method: GetUser
      proto:
        files: [./users.proto]
  - name: A proto that is not the grpc action's
    uses: shell
    with:
      cmd: echo
      proto:
        files: [./users.proto]
  - name: Always
    uses: hello
    test: 1 == 1
  - name: Function only
    uses: hello
    test: len("abc") == 3
`,
			want: []want{
				{SeverityWarning, 5, "nothing checks this step: it has no test"},
				{SeverityWarning, 13, "nothing checks this step: it has no test"},
				{SeverityWarning, 26, "nothing checks this step: it has no test"},
				{SeverityWarning, 34, "test reads nothing from the step"},
				{SeverityWarning, 37, "test reads nothing from the step"},
			},
		},
		{
			name: "a test over the runs of a repeated job",
			workflow: `name: repeat test
jobs:
- name: Search
  repeat:
    count: 10
    test: steps.serch.rt.p95 < 0.3 && steps.search.rate > 0.9 && steps.search.rt.p999 < 1 && steps.step_1.runs == 10 && steps.search.runs.n > 0 && steps.search.rt.p95.s < 1
  steps:
  - name: Search
    id: search
    uses: http
    with:
      get: /search
  - name: Other
    uses: http
    with:
      get: /other
- name: Broken
  repeat:
    count: 2
    test: steps.a.runs ==
  steps:
  - name: A
    id: a
    uses: http
    with:
      get: /a
    test: res.code == 200
`,
			want: []want{
				{SeverityError, 6, "repeat.test: steps.serch is not a step of the job; its steps are search, step_1"},
				{SeverityError, 6, "repeat.test: steps.search has no rate"},
				{SeverityError, 6, "repeat.test: steps.search.rt has no p999"},
				{SeverityError, 6, "repeat.test: steps.search.runs is a number, which has no n"},
				{SeverityError, 6, "repeat.test: steps.search.rt.p95 is a number, which has no s"},
				{SeverityError, 20, "repeat.test: unexpected token"},
			},
		},
		{
			name: "a test of a repeated job against the run compared to",
			workflow: `name: baseline test
jobs:
- name: Search
  repeat:
    count: 10
    test: steps.search.success_rate == 1 && baseline.steps.search.runs > 0
    baseline_test: steps.search.rt.p95 < baseline.steps.serch.rt.p95 && baseline.steps.search.failure == 0 && baseline.steps.search.rt.p999 < 1 && baseline.run_id != ""
  steps:
  - name: Search
    id: search
    uses: http
    with:
      get: /search
- name: Plain
  repeat:
    count: 2
    baseline_test: steps.a.success_rate == 1
  steps:
  - name: A
    id: a
    uses: http
    with:
      get: /a
- name: By key
  repeat:
    count: 2
    test: baseline[vars.k] != nil
    baseline_test: steps.b.rt.p95 < baseline.steps.b.rt[vars.p]
  steps:
  - name: B
    id: b
    uses: http
    with:
      get: /b
vars:
  k: steps
  p: p95
`,
			want: []want{
				{SeverityError, 6, "repeat.test: baseline is read only by repeat.baseline_test"},
				{SeverityError, 7, "repeat.baseline_test: baseline.steps.serch is not a step of the job; its steps are search"},
				{SeverityError, 7, "repeat.baseline_test: baseline.steps.search has no failure; it has runs, success, success_rate, rt"},
				{SeverityError, 7, "repeat.baseline_test: baseline.steps.search.rt has no p999"},
				{SeverityError, 7, "repeat.baseline_test: baseline has no run_id; it has steps"},
				{SeverityWarning, 17, "repeat.baseline_test: it reads nothing under baseline, so it can go in repeat.test"},
				{SeverityError, 27, "repeat.test: baseline is read only by repeat.baseline_test"},
			},
		},
		{
			name: "a test against the run compared to that reads a step by a key known when it runs",
			workflow: `name: baseline by key
jobs:
- name: By key
  repeat:
    count: 2
    baseline_test: steps.b.rt.p95 < baseline.steps[vars.id].rt.p95
  steps:
  - name: B
    id: b
    uses: http
    with:
      get: /b
`,
			want: []want{
				{SeverityError, 0, "repeat.baseline_test: baseline.steps is read by a key known only when the test runs"},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := writeWorkflow(t, t.TempDir(), "workflow.yml", tt.workflow)
			findings, err := Check(path, checkActions)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			checkFindings(t, findings, tt.want)
			for _, f := range findings {
				if f.File != path {
					t.Errorf("File = %q, want %q", f.File, path)
				}
			}
		})
	}
}

// A key asks an action to check a step against a contract only when the
// action says so: the checker knows of no such key by itself.
func TestCheck_ContractsFromTheActions(t *testing.T) {
	path := writeWorkflow(t, t.TempDir(), "workflow.yml", "name: a\njobs:\n- name: J\n  steps:\n  - name: Contract\n    uses: http\n    with:\n      get: /users\n      openapi:\n        spec: ./openapi.yml\n")

	findings, err := Check(path, CheckOptions{Actions: []string{"http"}, Contracts: map[string][]string{"http": {"openapi"}}})
	if err != nil {
		t.Fatal(err)
	}
	checkFindings(t, findings, nil)

	findings, err = Check(path, CheckOptions{Actions: []string{"http"}})
	if err != nil {
		t.Fatal(err)
	}
	checkFindings(t, findings, []want{{SeverityWarning, 5, "nothing checks this step: it has no test"}})
}

func TestCheck_WithoutActions(t *testing.T) {
	path := writeWorkflow(t, t.TempDir(), "workflow.yml", "name: a\njobs:\n- name: J\n  steps:\n  - uses: anything\n    test: res.code == 0\n")
	findings, err := Check(path, CheckOptions{})
	if err != nil {
		t.Fatal(err)
	}
	checkFindings(t, findings, nil)
}

func TestCheck_FilesReadTogether(t *testing.T) {
	dir := t.TempDir()
	base := writeWorkflow(t, dir, "base.yml", "name: merged\nvars:\n  url: http://localhost") // no newline at the end
	jobs := writeWorkflow(t, dir, "jobs.yml", "jobs:\n- name: J\n  steps:\n  - uses: hello\n    tset: res.code == 0\n")

	findings, err := Check(base+","+jobs, checkActions)
	if err != nil {
		t.Fatal(err)
	}
	checkFindings(t, findings, []want{
		{SeverityWarning, 4, "nothing checks this step"},
		{SeverityError, 5, `unknown key "tset"`},
	})
	for _, f := range findings {
		if f.File != jobs {
			t.Errorf("File = %q, want %q", f.File, jobs)
		}
	}
}

// TestCheck_JobsReplaced warns of a file whose jobs replace those of an
// earlier file, which a run of them does not run, and not of other keys.
func TestCheck_JobsReplaced(t *testing.T) {
	dir := t.TempDir()
	first := writeWorkflow(t, dir, "first.yml", "name: first\njobs:\n- name: A\n  steps:\n  - uses: hello\n    test: res.code == 0\n")
	vars := writeWorkflow(t, dir, "vars.yml", "vars:\n  who: x\n")
	second := writeWorkflow(t, dir, "second.yml", "vars:\n  who: y\njobs:\n- name: B\n  steps:\n  - uses: hello\n    test: res.code == 0\n")

	findings, err := Check(first+","+vars+","+second, checkActions)
	if err != nil {
		t.Fatal(err)
	}
	checkFindings(t, findings, []want{
		{SeverityWarning, 3, "jobs replaces the jobs of " + first + ":2, which do not run"},
	})
	if findings[0].File != second {
		t.Errorf("File = %q, want %q", findings[0].File, second)
	}
}

// TestCheck_Names checks the names the expressions and templates read: a
// name nothing is evaluated with is an error, and a var nothing defines a
// warning, as --var may give it.
func TestCheck_Names(t *testing.T) {
	tests := []struct {
		name     string
		workflow string
		want     []want
	}{
		{
			name: "a var nothing defines and steps which a step does not read",
			workflow: `name: names
vars:
  base: http://localhost
jobs:
- name: C
  steps:
  - name: h
    uses: http
    with:
      url: "{{ vars.base }}"
      get: /
    test: res.status == 200 && vars.nope == 1
  - name: h2
    uses: hello
    with: {name: x}
    test: steps.missing.code == 1 && res.cod == 0
`,
			want: []want{
				{SeverityWarning, 12, "test: vars.nope is not defined by the workflow, so it is nil unless --var gives it"},
				{SeverityError, 16, "test: steps is not defined; it can read outputs, repeat_index, report, req, res, rt, status and vars; what an earlier step publishes is read under outputs"},
			},
		},
		{
			name: "names misspelled or written as another language writes them",
			workflow: `name: names
vars:
  base: http://localhost
  url: "{{ vars.bsae }}/api"
jobs:
- name: C
  skipif: var.skip
  steps:
  - name: h
    id: h
    uses: hello
    with:
      url: "{{ vars.bse }}/{{ API_TOKEN }}"
    test: res.code != null
    outputs:
      region: us-east
    echo: "{{ output.h.region }}"
`,
			want: []want{
				{SeverityWarning, 4, `vars: vars.bsae is not defined by the workflow, so it is nil unless --var gives it; did you mean "base"?`},
				{SeverityError, 7, `skipif: var is not defined; it can read outputs, repeat_index, report, req, res, rt, status and vars; did you mean "vars"?`},
				{SeverityWarning, 13, `with: vars.bse is not defined by the workflow, so it is nil unless --var gives it; did you mean "base"?`},
				{SeverityError, 13, `with: API_TOKEN is not defined; it can read outputs, repeat_index, report, req, res, rt, status and vars; the environment is read only by the workflow's vars, as api_token: "{{API_TOKEN}}", which a step reads as vars.api_token`},
				{SeverityError, 14, "test: null is not defined; it can read outputs, repeat_index, report, req, res, rt, status and vars; did you mean nil?"},
				{SeverityError, 16, "outputs.region: us is not defined"},
				{SeverityError, 16, "outputs.region: east is not defined"},
				{SeverityError, 17, `echo: output is not defined; it can read outputs, repeat_index, report, req, res, rt, status and vars; did you mean "outputs"?`},
			},
		},
		{
			name: "the fields of a step and what each reads",
			workflow: `name: fields
vars:
  base: http://localhost
jobs:
- name: C
  steps:
  - name: "Step {{ vars.nme }} {{ undefined }}"
    uses: hello
    vars:
      a: 1
      b: "{{ vars.a }}"
      c: "{{ vars.id }} {{ vars.base }}"
    iteration:
    - {id: 7}
    wait: "{{ vars.delay }}"
    with: {b: "{{ vars.b }}", c: "{{ vars.c }}", id: "{{ vars.id }}"}
    test: res.b == "1"
  - name: Iteration a template gives
    uses: hello
    vars:
      s: 3
    iteration: "{{ [{'x': vars.s, 'y': vars.t}] }}"
    with: {x: "{{ vars.x }}"}
    test: res.x == 3
`,
			want: []want{
				{SeverityWarning, 7, "name: vars.nme is not defined by the workflow"},
				{SeverityError, 7, "name: undefined is not defined; it can read outputs, repeat_index, report, req, res, rt, status and vars; did you mean nil?"},
				{SeverityWarning, 11, "vars: vars.a is not defined yet: a step's vars read only those of the workflow, so it is nil unless --var gives it"},
				{SeverityWarning, 12, "vars: vars.id is not defined yet: a step's vars read only those of the workflow"},
				{SeverityWarning, 15, "wait: vars.delay is not defined by the workflow"},
				{SeverityWarning, 22, "iteration: vars.t is not defined by the workflow"},
			},
		},
		{
			name: "the tests of a repeated job",
			workflow: `name: repeated
jobs:
- name: R
  repeat:
    count: 2
    test: steps.a.success_rate == 1 && res.code == 200 && vars.limit > 0
    baseline_test: steps.a.rt.p95 < baseline.steps.a.rt.p95 * vars.factor && outputs.a.x == nil
  steps:
  - name: A
    id: a
    uses: hello
    with: {name: x}
`,
			want: []want{
				{SeverityError, 6, "repeat.test: res is not defined; it can read baseline, steps and vars"},
				{SeverityWarning, 6, "repeat.test: vars.limit is not defined by the workflow"},
				{SeverityError, 7, "repeat.baseline_test: outputs is not defined; it can read baseline, steps and vars"},
				{SeverityWarning, 7, "repeat.baseline_test: vars.factor is not defined by the workflow"},
			},
		},
		{
			name: "what defines a name and what reads one in a way check cannot follow",
			workflow: `name: defined
vars:
  base: "{{ API_URL }}"
  all: "{{ vars }}"
jobs:
- name: "Job {{ vars.base }} {{ CurrentJobID }}"
  skipif: vars.base == ""
  steps:
  - name: Step vars
    uses: hello
    vars:
      user: alice
    with:
      name: "{{ vars.user }} {{ vars.base }}"
    test: res.name != nil && vars[res.name] == nil && $env.vars.base != ""
  - name: Written iteration
    uses: hello
    iteration:
    - {id: 1, label: one}
    - {id: 2}
    with:
      name: "{{ vars.id }} {{ vars.label }}"
    test: 'let n = len(res.name); n > 0 && all([res.name], # != "") && now() != nil'
  - name: Iteration a template gives
    uses: hello
    iteration: "{{ parse_json(file('cases.json')) }}"
    with:
      name: "{{ vars.anything }}"
    test: res.name == vars.anything
`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := writeWorkflow(t, t.TempDir(), "workflow.yml", tt.workflow)
			findings, err := Check(path, checkActions)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			checkFindings(t, findings, tt.want)
		})
	}
}

// TestCheck_NamesOfFilesReadTogether reads the vars that one file defines
// and another reads, and the templates of defaults as the steps they apply
// to read them.
func TestCheck_NamesOfFilesReadTogether(t *testing.T) {
	dir := t.TempDir()
	base := writeWorkflow(t, dir, "base.yml", "name: merged\nvars:\n  url: http://localhost\ndefaults:\n  http:\n    headers:\n      x-user: \"{{ vars.usr }}\"\n")
	jobs := writeWorkflow(t, dir, "jobs.yml", "jobs:\n- name: J\n  steps:\n  - uses: http\n    vars:\n      user: bob\n    with:\n      get: \"{{ vars.url }}/\"\n    test: res.code == 200\n")

	findings, err := Check(base+","+jobs, checkActions)
	if err != nil {
		t.Fatal(err)
	}
	// A key of with the defaults give is told on the line of the with it
	// is given to.
	checkFindings(t, findings, []want{
		{SeverityWarning, 7, `with: vars.usr is not defined by the workflow, so it is nil unless --var gives it; did you mean "user"?`},
	})
	if findings[0].File != jobs {
		t.Errorf("File = %q, want %q", findings[0].File, jobs)
	}
}

func TestCheck_Unreadable(t *testing.T) {
	if _, err := Check(filepath.Join(t.TempDir(), "none.yml"), checkActions); err == nil {
		t.Error("Check of a file that does not exist should fail")
	}
}

func TestFinding_String(t *testing.T) {
	tests := []struct {
		f    Finding
		want string
	}{
		{Finding{SeverityError, "w.yml", 3, `job 0 "J"`, "bad"}, `w.yml:3: error: job 0 "J": bad`},
		{Finding{SeverityWarning, "w.yml", 0, "", "weak"}, "w.yml: warning: weak"},
		{Finding{SeverityError, "", 0, "", "circular"}, "error: circular"},
	}
	for _, tt := range tests {
		if got := tt.f.String(); got != tt.want {
			t.Errorf("String() = %q, want %q", got, tt.want)
		}
	}
}

func TestSuggest(t *testing.T) {
	tests := []struct {
		key     string
		allowed []string
		want    string
	}{
		{"tset", []string{"name", "test"}, "test"},
		{"api_base_url", []string{"API_BASE_URL", "api_url"}, "API_BASE_URL"},
		{"steps", []string{"outputs", "status", "vars"}, ""},
		{"x", []string{"res", "rt"}, ""},
		{"us", []string{"res", "rt"}, ""},
		{"rs", []string{"res", "rt"}, "res"},
		{"var", []string{"outputs", "vars"}, "vars"},
	}
	for _, tt := range tests {
		if got := suggest(tt.key, tt.allowed); got != tt.want {
			t.Errorf("suggest(%q, %q) = %q, want %q", tt.key, tt.allowed, got, tt.want)
		}
	}
}

func TestDistance(t *testing.T) {
	tests := []struct {
		a, b string
		want int
	}{
		{"test", "test", 0},
		{"tset", "test", 2},
		{"need", "needs", 1},
		{"", "abc", 3},
	}
	for _, tt := range tests {
		if got := distance(tt.a, tt.b); got != tt.want {
			t.Errorf("distance(%q, %q) = %d, want %d", tt.a, tt.b, got, tt.want)
		}
	}
}

func TestCheck_WithKeys(t *testing.T) {
	opts := CheckOptions{
		Actions: []string{"hello", "http", "shell"},
		Params: map[string][]string{
			"http":  {"url", "method", "headers", "body", "get", "post"},
			"shell": {"cmd", "env"},
		},
	}
	tests := []struct {
		name     string
		workflow string
		want     []want
	}{
		{
			name: "keys the actions take",
			workflow: `name: ok
jobs:
- name: J
  defaults:
    http:
      url: http://localhost
  steps:
  - uses: http
    with:
      post: /users
      headers:
        whatever-header: x
      body: {any: thing}
    test: res.code == 201
  - uses: hello
    with:
      anything: goes
    test: res.code == 0
  - uses: ./actions/mine
    with:
      own: keys
    test: res.code == 0
  - uses: http
    with:
      get: /x
      "{{ vars.key }}": templated
    test: res.code == 200
vars:
  key: x-key
`,
		},
		{
			name: "keys a step's action does not take",
			workflow: `name: typos
jobs:
- name: J
  steps:
  - name: Create
    uses: http
    with:
      url: http://localhost
      post: /users
      heders:
        content-type: application/json
      bdoy: {name: probe}
    test: res.code == 201
  - name: Run
    uses: shell
    with:
      command: echo
    test: res.code == 0
`,
			want: []want{
				{SeverityError, 10, `with: unknown key "heders" for the http action; did you mean "headers"?`},
				{SeverityError, 12, `with: unknown key "bdoy" for the http action; did you mean "body"?`},
				{SeverityError, 17, `with: unknown key "command" for the shell action`},
			},
		},
		{
			name: "keys from a YAML alias or merge key",
			workflow: `name: aliases
shared:
  params: &params
    url: http://localhost
    bdoy: {name: probe}
jobs:
- name: J
  steps:
  - name: Alias
    uses: http
    with: *params
    test: res.code == 200
  - name: Merge
    uses: http
    with:
      <<: *params
      post: /users
    test: res.code == 201
  - name: Unclosed
    uses: http
    with:
      url: http://localhost
      "bdoy{{": x
    test: res.code == 200
`,
			want: []want{
				// Told on the step's with, which the alias is written on.
				{SeverityError, 11, `with: unknown key "bdoy" for the http action`},
				{SeverityError, 15, `with: unknown key "bdoy" for the http action`},
				{SeverityError, 23, `with: unknown key "bdoy{{" for the http action`},
			},
		},
		{
			name: "keys of defaults told once on their own line",
			workflow: `name: defaults
jobs:
- name: J
  defaults:
    http:
      url: http://localhost
      heders:
        accept: application/json
    htp:
      url: http://localhost
  steps:
  - uses: http
    with:
      get: /a
    test: res.code == 200
  - uses: http
    with:
      get: /b
    test: res.code == 200
`,
			want: []want{
				{SeverityError, 7, `defaults.http: unknown key "heders" for the http action; did you mean "headers"?`},
				{SeverityError, 9, `defaults: no action is named "htp", so its defaults apply to no step; did you mean "http"?`},
			},
		},
		{
			name: "keys of the defaults of the workflow",
			workflow: `name: defaults
defaults:
  http:
    url: http://localhost
    heders:
      accept: application/json
  htp:
    url: http://localhost
jobs:
- name: J
  defaults:
    http:
      timout: 5s
  steps:
  - uses: http
    with:
      get: /a
      bdoy: x
    test: res.code == 200
`,
			want: []want{
				{SeverityError, 5, `defaults.http: unknown key "heders" for the http action; did you mean "headers"?`},
				{SeverityError, 7, `defaults: no action is named "htp", so its defaults apply to no step; did you mean "http"?`},
				{SeverityError, 13, `defaults.http: unknown key "timout" for the http action`},
				{SeverityError, 18, `with: unknown key "bdoy" for the http action; did you mean "body"?`},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := writeWorkflow(t, t.TempDir(), "workflow.yml", tt.workflow)
			findings, err := Check(path, opts)
			if err != nil {
				t.Fatal(err)
			}
			checkFindings(t, findings, tt.want)
		})
	}
}

// TestCheck_ExternalParams checks the with of an external action against
// the params its action.yml declares, read through CheckOptions.Manifest.
func TestCheck_ExternalParams(t *testing.T) {
	dir := t.TempDir()
	for name, manifest := range map[string]string{
		"declared": "runs:\n  using: binary\n  path: x\nparams: [url, calls]\n",
		"silent":   "runs:\n  using: binary\n  path: x\n",
	} {
		if err := os.MkdirAll(filepath.Join(dir, name), 0o755); err != nil {
			t.Fatal(err)
		}
		writeWorkflow(t, filepath.Join(dir, name), "action.yml", manifest)
	}
	path := writeWorkflow(t, dir, "workflow.yml", `name: external
jobs:
- name: J
  defaults:
    ./declared:
      url: http://localhost
      timout: 5s
  steps:
  - uses: ./declared
    with:
      calls: []
      cals: []
    test: res.code == 0
  - uses: ./silent
    with:
      anything: goes
    test: res.code == 0
  - uses: ./missing
    with:
      anything: goes
    test: res.code == 0
`)
	params := map[string][]string{"http": {"url"}}
	opts := CheckOptions{Actions: []string{"http"}, Params: params, Manifest: actionref.ReadManifest}
	findings, err := Check(path, opts)
	if err != nil {
		t.Fatal(err)
	}
	checkFindings(t, findings, []want{
		{SeverityError, 7, `defaults../declared: unknown key "timout" for the ./declared action`},
		{SeverityError, 12, `with: unknown key "cals" for the ./declared action; did you mean "calls"?`},
		{SeverityWarning, 18, `uses: the keys of with are not checked, as action.yml could not be read`},
	})
	if len(params) != 1 {
		t.Errorf("Check changed the Params it was given: %v", params)
	}

	// Without Manifest, nothing is read and the external actions are not
	// checked.
	findings, err = Check(path, CheckOptions{Actions: []string{"http"}, Params: params})
	if err != nil {
		t.Fatal(err)
	}
	checkFindings(t, findings, nil)
}
