package probe

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/linyows/probe/actionref"
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

var checkActions = CheckOptions{Actions: []string{"embedded", "grpc", "hello", "http", "shell"}}

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
			name: "an id a step without one is given in another job",
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
		},
		{
			name: "one id given to publishing steps of two jobs",
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
				{SeverityWarning, 19, `with: outputs.step_0.a may not be published yet: job "C" does not need job "A"`},
				{SeverityWarning, 19, `with: outputs.step_0.b may not be published yet: job "C" does not need job "B"`},
				{SeverityError, 19, `with: outputs.step_0.c is not published: step "step_0" publishes a, b`},
				{SeverityWarning, 23, `with: outputs.step_0 may not be published yet: job "C" does not need the jobs whose steps publish it`},
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
