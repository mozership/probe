package probe

import (
	"errors"
	"fmt"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/linyows/probe/actionref"
	"github.com/linyows/probe/actionrpc"
)

const aliasSHA = "0123456789abcdef0123456789abcdef01234567"

var builtinNames = []string{"embedded", "http", "shell"}

func loadWorkflow(t *testing.T, content string, more ...string) (*Probe, error) {
	t.Helper()
	dir := t.TempDir()
	paths := []string{writeWorkflow(t, dir, "workflow.yml", content)}
	for i, c := range more {
		paths = append(paths, writeWorkflow(t, dir, fmt.Sprintf("more%d.yml", i), c))
	}
	p := &Probe{FilePath: strings.Join(paths, ","), Config: Config{Log: os.Stdout, Actions: builtinNames}}
	return p, p.Load()
}

func TestLoad_ActionNames(t *testing.T) {
	redis := "github.com/mozership/probe-redis@" + aliasSHA
	p, err := loadWorkflow(t, `name: names
actions:
  redis: `+redis+`
  greet: ./greet
jobs:
- name: J
  defaults:
    redis:
      url: redis://localhost
      nested:
        a: 1
    http:
      url: http://localhost
  steps:
  - uses: redis
    with:
      commands: [PING]
      nested:
        b: 2
  - uses: greet
  - uses: http
  - uses: `+redis+`
- name: K
  steps:
  - uses: redis
`)
	if err != nil {
		t.Fatal(err)
	}
	job := p.workflow.Jobs[0]
	for i, want := range []string{redis, "./greet", "http", redis} {
		if got := job.Steps[i].Uses; got != want {
			t.Errorf("steps[%d].uses = %q, want %q", i, got, want)
		}
	}
	// The defaults written by the name reach the steps that use the action,
	// by the name or in full.
	for _, i := range []int{0, 3} {
		if got := job.Steps[i].With["url"]; got != "redis://localhost" {
			t.Errorf("steps[%d].with.url = %v, want the default", i, got)
		}
	}
	nested, _ := job.Steps[0].With["nested"].(map[string]any)
	if len(nested) != 2 {
		t.Errorf("steps[0].with.nested = %v, want the keys of the step and of the defaults", nested)
	}
	if got := job.Steps[2].With["url"]; got != "http://localhost" {
		t.Errorf("steps[2].with.url = %v, want the default of http", got)
	}
	if _, ok := job.Steps[1].With["url"]; ok {
		t.Errorf("steps[1].with = %v, want none of the defaults of another action", job.Steps[1].With)
	}
	defaults, _ := job.Defaults.(map[string]any)
	if _, ok := defaults[redis]; !ok || len(defaults) != 2 {
		t.Errorf("defaults = %v, want them keyed by the action", defaults)
	}
	// A job without defaults has its steps resolved too.
	if got := p.workflow.Jobs[1].Steps[0].Uses; got != redis {
		t.Errorf("jobs[1].steps[0].uses = %q, want %q", got, redis)
	}
}

func TestLoad_ActionNamesRefused(t *testing.T) {
	tests := []struct {
		name    string
		actions string
		job     string
		want    string
	}{
		{"the name of an action of Probe", "  http: ./greet\n", "", `the name "http" is that of an action of Probe`},
		{"embedded", "  embedded: ./greet\n", "", `the name "embedded" is that of an action of Probe`},
		{"a name with a slash", "  \"my/redis\": ./greet\n", "", `the name "my/redis" must be letters`},
		{"a name with a dot", "  my.redis: ./greet\n", "", `the name "my.redis" must be letters`},
		{"a name that holds a template", "  \"{{vars.x}}\": ./greet\n", "", `must be letters`},
		{"an empty name", "  \"\": ./greet\n", "", `must be letters`},
		{"another name", "  a: ./greet\n  b: a\n", "", `b must name an external action`},
		{"an action of Probe", "  web: http\n", "", `web must name an external action`},
		{"nothing", "  redis: \"\"\n", "", `redis must name an external action`},
		{"a tag", "  redis: github.com/mozership/probe-redis@v1.0.0\n", "", `must be pinned to a full 40-character commit SHA`},
		{"no commit", "  redis: github.com/mozership/probe-redis\n", "", `must be pinned to a commit`},
		{"another host", "  redis: gitlab.com/mozership/probe-redis@" + aliasSHA + "\n", "", `unsupported action`},
		{"a template in the commit", "  redis: github.com/mozership/probe-redis@{{vars.sha}}\n", "", `redis must name an external action as it is, not by a template`},
		{"a template in a local path", "  greet: \"./{{vars.action}}\"\n", "", `greet must name an external action as it is, not by a template`},
		{"a template for the whole of it", "  greet: \"{{vars.action}}/greet\"\n", "", `greet must name an external action as it is, not by a template`},
		{
			"defaults written twice",
			"  greet: ./greet\n",
			"  defaults:\n    greet: {name: a}\n    ./greet: {name: b}\n",
			`greet and ./greet are the same action`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := loadWorkflow(t, "name: refused\nactions:\n"+tt.actions+"jobs:\n- name: J\n"+tt.job+"  steps:\n  - uses: http\n")
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Errorf("Load() error = %v, want one saying %q", err, tt.want)
			}
		})
	}
}

// Without the names of the actions of Probe, as a caller of the package may
// leave them, a name is not checked against them.
func TestLoad_ActionNamesWithoutBuiltin(t *testing.T) {
	dir := t.TempDir()
	path := writeWorkflow(t, dir, "workflow.yml", "name: n\nactions:\n  http: ./greet\njobs:\n- name: J\n  steps:\n  - uses: http\n")
	p := &Probe{FilePath: path, Config: Config{Log: os.Stdout}}
	if err := p.Load(); err != nil {
		t.Fatal(err)
	}
	if got := p.workflow.Jobs[0].Steps[0].Uses; got != "./greet" {
		t.Errorf("uses = %q, want ./greet", got)
	}
}

// The actions of the last file given replace those of the files before it,
// as any top-level key does.
func TestLoad_ActionNamesOfSeveralFiles(t *testing.T) {
	p, err := loadWorkflow(t,
		"actions:\n  greet: ./old\n  gone: ./gone\n",
		"name: n\nactions:\n  greet: ./new\njobs:\n- name: J\n  steps:\n  - uses: greet\n  - uses: gone\n")
	if err != nil {
		t.Fatal(err)
	}
	steps := p.workflow.Jobs[0].Steps
	if steps[0].Uses != "./new" || steps[1].Uses != "gone" {
		t.Errorf("uses = %q and %q, want ./new and gone", steps[0].Uses, steps[1].Uses)
	}
}

func TestCheck_ActionNames(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "declared"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeWorkflow(t, filepath.Join(dir, "declared"), "action.yml", "runs:\n  using: binary\n  path: x\nparams: [url, calls]\n")
	path := writeWorkflow(t, dir, "workflow.yml", `name: names
actions:
  cache: ./declared
  http: ./declared
  store: github.com/mozership/probe-s3@v1
jobs:
- name: J
  defaults:
    cache:
      url: redis://localhost
      timout: 5s
  steps:
  - uses: cache
    with:
      calls: []
      cals: []
    test: res.code == 0
  - uses: cash
    test: res.code == 0
  - uses: store
    test: res.code == 0
- name: K
  defaults:
    cache: {url: a}
    ./declared: {url: b}
  steps:
  - uses: cache
    test: res.code == 0
`)
	opts := CheckOptions{Actions: []string{"http"}, Params: map[string][]string{"http": {"url"}}, Manifest: actionref.ReadManifest}
	findings, err := Check(path, opts)
	if err != nil {
		t.Fatal(err)
	}
	checkFindings(t, findings, []want{
		{SeverityError, 4, `actions: the name "http" is that of an action of Probe`},
		{SeverityError, 5, `actions: store: action "github.com/mozership/probe-s3@v1" must be pinned to a full 40-character commit SHA`},
		{SeverityError, 11, `defaults.cache: unknown key "timout" for the ./declared action`},
		{SeverityError, 16, `with: unknown key "cals" for the ./declared action; did you mean "calls"?`},
		{SeverityError, 18, `unknown action "cash"`},
		{SeverityError, 24, `defaults: cache and ./declared are the same action`},
	})
}

// TestEndToEndActionNames runs a workflow that names its external action
// under actions, and is refused, under a guard, by the action in full.
func TestLoad_NamesForEmbeddedJobs(t *testing.T) {
	redis := "github.com/mozership/probe-redis@" + aliasSHA
	p := &Probe{Config: Config{Log: os.Stdout, Actions: builtinNames}}
	// Problems are collected, as probe check does, so that the wrong name
	// does not end the load.
	p.aliasProblems = &[]aliasProblem{}
	dir := t.TempDir()
	p.FilePath = writeWorkflow(t, dir, "workflow.yml", `name: names
actions:
  redis: `+redis+`
  greet: ./greet
  up: ../shared/up
  abs: /opt/actions/abs
  http: ./greet
jobs:
- name: J
  steps:
  - uses: embedded
    with:
      path: job.yml
`)
	if err := p.Load(); err != nil {
		t.Fatal(err)
	}
	// t.TempDir is a symbolic link on macOS, and the workflow is found by
	// the path it was given.
	want := map[string]string{
		"redis": redis,
		"greet": filepath.Join(dir, "greet"),
		"up":    filepath.Join(filepath.Dir(dir), "shared", "up"),
		"abs":   "/opt/actions/abs",
	}
	if got := p.workflow.named; !maps.Equal(got, want) {
		t.Errorf("named = %v, want %v", got, want)
	}

	t.Run("a workflow that names none", func(t *testing.T) {
		p, err := loadWorkflow(t, "name: none\njobs:\n- name: J\n  steps:\n  - uses: http\n")
		if err != nil {
			t.Fatal(err)
		}
		if len(p.workflow.named) != 0 {
			t.Errorf("named = %v, want none", p.workflow.named)
		}
	})
}

func TestJob_UseActions(t *testing.T) {
	redis := "github.com/mozership/probe-redis@" + aliasSHA
	names := map[string]string{"redis": redis}

	job := &Job{
		Defaults: map[string]any{"redis": map[string]any{"url": "a"}, "http": map[string]any{"url": "b"}},
		Steps:    []*Step{{Uses: "redis"}, nil, {Uses: "http"}, {Uses: "store"}},
	}
	if err := job.UseActions(names); err != nil {
		t.Fatal(err)
	}
	for i, want := range map[int]string{0: redis, 2: "http", 3: "store"} {
		if job.Steps[i].Uses != want {
			t.Errorf("step %d: uses = %q, want %q", i, job.Steps[i].Uses, want)
		}
	}
	defaults := job.Defaults.(map[string]any)
	if _, ok := defaults[redis]; !ok || len(defaults) != 2 {
		t.Errorf("defaults = %v, want those of redis keyed by the action", defaults)
	}

	// No names, and defaults that are not a map, change nothing.
	plain := &Job{Defaults: "x", Steps: []*Step{{Uses: "redis"}}}
	if err := plain.UseActions(nil); err != nil || plain.Steps[0].Uses != "redis" {
		t.Errorf("UseActions(nil) = %v with uses %q, want the job as it was", err, plain.Steps[0].Uses)
	}
	if err := plain.UseActions(names); err != nil || plain.Steps[0].Uses != redis {
		t.Errorf("UseActions() = %v with uses %q, want %q", err, plain.Steps[0].Uses, redis)
	}

	twice := &Job{Defaults: map[string]any{"redis": map[string]any{}, redis: map[string]any{}}}
	if err := twice.UseActions(names); err == nil || !strings.Contains(err.Error(), "defaults: redis and "+redis+" are the same action") {
		t.Errorf("UseActions() = %v, want the defaults refused", err)
	}
}

func TestCheck_EmbeddedJobNames(t *testing.T) {
	dir := t.TempDir()
	// The job embeds another, which embeds the first again: each file is
	// read once.
	jobPath := filepath.Join(dir, "job.yml")
	nested := writeWorkflow(t, dir, "nested.yml", `name: nested
steps:
- uses: cache
- uses: cach
- uses: embedded
  with:
    path: `+jobPath+`
- uses: embedded
  with:
    path: "{{vars.other}}"
`)
	job := writeWorkflow(t, dir, "job.yml", `name: job
steps:
- uses: cache
- uses: http
- uses: ./local
- uses: cash
- uses: wrong
- uses: nothing
- uses: github.com/mozership/probe-s3@v1
- uses: embedded
  with:
    path: `+nested+`
`)
	path := writeWorkflow(t, dir, "workflow.yml", `name: names
actions:
  cache: github.com/mozership/probe-redis@`+aliasSHA+`
  wrong: github.com/mozership/probe-s3@v1
vars:
  job: `+job+`
jobs:
- name: J
  steps:
  - name: static
    uses: embedded
    with:
      path: `+job+`
    test: res.code == 0
  - name: a template is not known before the run
    uses: embedded
    with:
      path: "{{vars.job}}"
    test: res.code == 0
  - name: a file that is not there is told by the run
    uses: embedded
    with:
      path: `+filepath.Join(dir, "none.yml")+`
    test: res.code == 0
`)
	opts := CheckOptions{Actions: []string{"embedded", "http"}}
	findings, err := Check(path, opts)
	if err != nil {
		t.Fatal(err)
	}
	checkFindings(t, findings, []want{
		{SeverityError, 4, "actions: wrong:"},
		{SeverityError, 13, `job.yml: step 3 uses "cash", which is not an action of Probe or a name under actions; did you mean "cache"?`},
		{SeverityError, 13, `job.yml: step 5 uses "nothing", which is not an action of Probe or a name under actions`},
		{SeverityError, 13, `job.yml: step 6: uses: action "github.com/mozership/probe-s3@v1" must be pinned to a full 40-character commit SHA`},
		{SeverityError, 13, `nested.yml: step 1 uses "cach", which is not an action of Probe or a name under actions; did you mean "cache"?`},
	})

	// Without the names of the actions of Probe, no uses can be told wrong.
	findings, err = Check(path, CheckOptions{})
	if err != nil {
		t.Fatal(err)
	}
	checkFindings(t, findings, []want{{SeverityError, 4, "actions: wrong:"}})
}

func TestResolveExternalActions_UnknownAction(t *testing.T) {
	jobs := []*Job{{Name: "J", Steps: []*Step{{Uses: "http"}, {Uses: "htp"}}}}

	_, err := resolveExternalActions(jobs, "", actionrpc.Guard{}, builtinNames)
	if err == nil || !strings.Contains(err.Error(), `job "J", step 1: unknown action "htp"`) || !strings.Contains(err.Error(), `did you mean "http"?`) {
		t.Errorf("err = %v, want the unknown action told with its job and step", err)
	}
	var pe *ProbeError
	if !errors.As(err, &pe) || pe.Type != ErrorTypeConfiguration {
		t.Errorf("err = %v, want a configuration error", err)
	}

	// Without the names of the actions of Probe, nothing can be told unknown.
	if _, err := resolveExternalActions(jobs, "", actionrpc.Guard{}, nil); err != nil {
		t.Errorf("err = %v, want none without the names", err)
	}
}

func TestWorkflow_CheckEmbeddedJobs(t *testing.T) {
	dir := t.TempDir()
	good := writeWorkflow(t, dir, "good.yml", "name: good\nsteps:\n- uses: cache\n- uses: http\n- uses: ./local\n")
	bad := writeWorkflow(t, dir, "bad.yml", "name: bad\nsteps:\n- uses: http\n- uses: cash\n")
	// A job file that embeds the bad one, and itself, which ends.
	outerPath := filepath.Join(dir, "outer.yml")
	outer := writeWorkflow(t, dir, "outer.yml", "name: outer\nsteps:\n- uses: embedded\n  with:\n    path: "+outerPath+"\n- uses: embedded\n  with:\n    path: "+bad+"\n")
	embed := func(path string) string {
		return `name: w
actions:
  cache: github.com/mozership/probe-redis@` + aliasSHA + `
vars:
  bad: ` + bad + `
jobs:
- name: J
  steps:
  - uses: http
  - uses: embedded
    with:
      path: "` + path + `"
`
	}

	tests := []struct {
		name    string
		path    string
		wantErr string
	}{
		{"every uses is an action", good, ""},
		{"a uses that is none", bad, bad + `, step 1: unknown action "cash"`},
		{"in a file a var names", "{{vars.bad}}", bad + `, step 1: unknown action "cash"`},
		{"in a file that file embeds", outer, bad + `, step 1: unknown action "cash"`},
		{"a path only the run knows", "{{outputs.find.job}}", ""},
		{"a file that is not there", filepath.Join(dir, "none.yml"), ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p, err := loadWorkflow(t, embed(tt.path))
			if err != nil {
				t.Fatal(err)
			}
			vars := map[string]any{"bad": bad}
			err = p.workflow.checkEmbeddedJobs(builtinNames, vars)
			switch {
			case tt.wantErr == "" && err != nil:
				t.Errorf("err = %v, want none", err)
			case tt.wantErr != "" && (err == nil || !strings.Contains(err.Error(), tt.wantErr)):
				t.Errorf("err = %v, want one containing %q", err, tt.wantErr)
			}
			// The vars are those it is given, which the run evaluated: with
			// another file under the same var, that file is the one read.
			if tt.path == "{{vars.bad}}" {
				if err := p.workflow.checkEmbeddedJobs(builtinNames, map[string]any{"bad": good}); err != nil {
					t.Errorf("err = %v, want the file the vars name read", err)
				}
			}
			if err := p.workflow.checkEmbeddedJobs(nil, vars); err != nil {
				t.Errorf("err = %v, want none without the names of the actions of Probe", err)
			}
		})
	}
}

func TestEndToEndActionNames(t *testing.T) {
	dir := t.TempDir()
	probeBin := filepath.Join(dir, "probe")
	actionDir := filepath.Join(dir, "greet")
	bin := filepath.Join(actionDir, "greet")
	for _, b := range [][]string{{probeBin, "./cmd/probe"}, {bin, "./testdata/external-action"}} {
		if out, err := exec.Command("go", "build", "-o", b[0], b[1]).CombinedOutput(); err != nil {
			t.Fatalf("go build %s: %v\n%s", b[1], err, out)
		}
	}
	writeWorkflow(t, actionDir, "action.yml", "runs:\n  using: binary\n  path: greet\n")
	named := writeWorkflow(t, dir, "workflow.yml", `name: named
actions:
  greet: ./greet
jobs:
- name: greet
  defaults:
    greet:
      name: probe
  steps:
  - name: greet probe
    uses: greet
    test: res.greeting == "hello probe"
`)
	// A job file in another directory than the workflow, which embeds one
	// more: both use the name the workflow gives, and its ./greet is the
	// directory next to the workflow, not one next to either job file.
	jobs := filepath.Join(dir, "jobs")
	if err := os.MkdirAll(filepath.Join(jobs, "deep"), 0o755); err != nil {
		t.Fatal(err)
	}
	inner := writeWorkflow(t, filepath.Join(jobs, "deep"), "inner.yml", `name: inner
steps:
- name: greet from the inner job
  uses: greet
  with:
    name: inner
  test: res.greeting == "hello inner"
`)
	outer := writeWorkflow(t, jobs, "outer.yml", `name: outer
defaults:
  greet:
    name: outer
steps:
- name: greet from the job
  uses: greet
  test: res.greeting == "hello outer"
- name: embed one more
  uses: embedded
  with:
    path: `+inner+`
  test: res.code == 0
`)
	embedding := writeWorkflow(t, dir, "embedding.yml", `name: embedding
actions:
  greet: ./greet
jobs:
- name: embed
  steps:
  - name: embed a job
    uses: embedded
    with:
      path: `+outer+`
    test: res.code == 0
`)
	unnamed := writeWorkflow(t, dir, "unnamed.yml", `name: unnamed
actions:
  great: ./greet
jobs:
- name: embed
  steps:
  - name: embed a job
    uses: embedded
    with:
      path: `+outer+`
    test: res.code == 0
`)
	// An action that does not exist is told before any step runs: in the
	// workflow, and in a job file whose path is known by then.
	unknown := writeWorkflow(t, dir, "unknown.yml", `name: unknown
jobs:
- name: first
  steps:
  - name: runs before
    uses: shell
    with:
      cmd: "true"
    test: res.code == 0
  - name: never reached
    uses: shel
    skipif: "true"
`)
	byVar := writeWorkflow(t, dir, "by-var.yml", `name: by var
vars:
  job: `+outer+`
jobs:
- name: embed
  steps:
  - name: runs before
    uses: shell
    with:
      cmd: "true"
    test: res.code == 0
  - name: embed a job
    uses: embedded
    with:
      path: "{{vars.job}}"
    test: res.code == 0
`)
	// The path of the job file is made from a declared secret, which the
	// error that names the file must not show.
	bySecret := writeWorkflow(t, dir, "by-secret.yml", `name: by secret
secrets:
  - JOBS_DIR
vars:
  job: "{{JOBS_DIR}}/outer.yml"
jobs:
- name: embed
  steps:
  - name: embed a job
    uses: embedded
    with:
      path: "{{vars.job}}"
    test: res.code == 0
`)
	// A path only the run knows is checked as the job is started.
	byOutput := writeWorkflow(t, dir, "by-output.yml", `name: by output
jobs:
- name: embed
  steps:
  - name: runs before
    id: find
    uses: shell
    with:
      cmd: echo `+outer+`
    test: res.code == 0
    outputs:
      job: replace(res.stdout, '\n', '')
  - name: embed a job
    uses: embedded
    with:
      path: "{{outputs.find.job}}"
    test: res.code == 0
`)
	clash := writeWorkflow(t, dir, "clash.yml", `name: clash
actions:
  shell: ./greet
jobs:
- name: greet
  steps:
  - uses: shell
    with:
      name: probe
`)

	tests := []struct {
		name     string
		args     []string
		wantCode int
		wantOut  string
	}{
		{"a named action runs", []string{named}, ExitOK, ""},
		{"the guard names the action in full", []string{"--read-only", named}, ExitConfigError, "--allow-action ./greet"},
		{"and lets it run by that", []string{"--read-only", "--allow-action", "./greet", named}, ExitOK, ""},
		{"but not by the name", []string{"--read-only", "--allow-action", "greet", named}, ExitConfigError, "--allow-action ./greet"},
		{"the name of an action of Probe", []string{clash}, ExitConfigError, `the name "shell" is that of an action of Probe`},
		{"check passes a named action", []string{"check", named}, ExitOK, ""},
		{"a job a step embeds uses the name", []string{embedding}, ExitOK, ""},
		{"the guard names its action in full", []string{"--read-only", embedding}, ExitConfigError, "--allow-action " + actionDir},
		{"and lets it run by that", []string{"--read-only", "--allow-action", actionDir, embedding}, ExitOK, ""},
		{"check passes a job that uses a name", []string{"check", embedding}, ExitOK, ""},
		{"an action that does not exist stops the run", []string{unknown}, ExitConfigError, `job "first", step 1: unknown action "shel": it is not an action of Probe, an external action, or a name given under actions; did you mean "shell"?`},
		{"and so does one in a job file", []string{unnamed}, ExitConfigError, outer + `, step 0: unknown action "greet"`},
		{"and in a job file a var names", []string{byVar}, ExitConfigError, outer + `, step 0: unknown action "greet"`},
		{"and in a job file a secret names, which stays hidden", []string{bySecret}, ExitConfigError, `<secret:JOBS_DIR>/outer.yml, step 0: unknown action "greet"`},
		{"one in a job file only the run knows fails its step", []string{byOutput}, ExitActionError, `job "outer", step 0: unknown action "greet"`},
		{"check tells a name the workflow does not give", []string{"check", unnamed}, ExitConfigError, `step 0 uses "greet", which is not an action of Probe or a name under actions; did you mean "great"?`},
		{"check tells the name of an action of Probe", []string{"check", clash}, ExitConfigError, `the name "shell" is that of an action of Probe`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cmd := exec.Command(probeBin, tt.args...)
			cmd.Env = append(os.Environ(), "JOBS_DIR="+jobs)
			out, err := cmd.CombinedOutput()
			code := 0
			if err != nil {
				exitErr, ok := err.(*exec.ExitError)
				if !ok {
					t.Fatalf("unexpected error: %v", err)
				}
				code = exitErr.ExitCode()
			}
			if code != tt.wantCode {
				t.Errorf("exit code = %d, want %d\n%s", code, tt.wantCode, out)
			}
			if !strings.Contains(string(out), tt.wantOut) {
				t.Errorf("output does not contain %q\n%s", tt.wantOut, out)
			}
			if tt.args[0] == bySecret && strings.Contains(string(out), jobs) {
				t.Errorf("the output shows the secret %s\n%s", jobs, out)
			}
			// A run stopped for its configuration ran no step.
			if tt.wantCode == ExitConfigError && tt.args[0] != "check" && strings.Contains(string(out), "runs before") {
				t.Errorf("a step ran before the run was stopped\n%s", out)
			}
		})
	}
}

// The defaults of the workflow reach the steps of every job, after those of
// the step's own job, and are written by the names of the actions too.
func TestLoad_WorkflowDefaults(t *testing.T) {
	redis := "github.com/mozership/probe-redis@" + aliasSHA
	p, err := loadWorkflow(t, `name: defaults
actions:
  redis: `+redis+`
defaults:
  http:
    url: http://shared
    headers:
      accept: application/json
      x-from: workflow
  redis:
    url: redis://shared
jobs:
- name: J
  defaults:
    http:
      headers:
        x-from: job
        x-job: "1"
  steps:
  - uses: http
    with:
      get: /a
      headers:
        x-step: "1"
  - uses: http
    with:
      url: http://own
  - uses: redis
- name: K
  steps:
  - uses: http
  - uses: hello
`)
	if err != nil {
		t.Fatal(err)
	}
	steps := p.workflow.Jobs[0].Steps
	if got := steps[0].With["url"]; got != "http://shared" {
		t.Errorf("jobs[0].steps[0].with.url = %v, want that of the workflow", got)
	}
	want := map[string]any{"accept": "application/json", "x-from": "job", "x-job": "1", "x-step": "1"}
	if got := steps[0].With["headers"]; !reflect.DeepEqual(got, want) {
		t.Errorf("jobs[0].steps[0].with.headers = %v, want %v", got, want)
	}
	if got := steps[1].With["url"]; got != "http://own" {
		t.Errorf("jobs[0].steps[1].with.url = %v, want the step's own", got)
	}
	if got := steps[2].With["url"]; got != "redis://shared" {
		t.Errorf("jobs[0].steps[2].with.url = %v, want that of the workflow, written by the name", got)
	}

	// A job without defaults is given those of the workflow as they are
	// written: what another job merged into its own is not among them.
	other := p.workflow.Jobs[1].Steps
	want = map[string]any{"accept": "application/json", "x-from": "workflow"}
	if got := other[0].With["headers"]; !reflect.DeepEqual(got, want) {
		t.Errorf("jobs[1].steps[0].with.headers = %v, want %v", got, want)
	}
	if len(other[1].With) != 0 {
		t.Errorf("jobs[1].steps[1].with = %v, want none of the defaults of another action", other[1].With)
	}
	shared, _ := p.workflow.Defaults.(map[string]any)
	if _, ok := shared[redis]; !ok || len(shared) != 2 {
		t.Errorf("defaults = %v, want them keyed by the action", shared)
	}
}

func TestLoad_WorkflowDefaultsWrittenTwice(t *testing.T) {
	_, err := loadWorkflow(t, `name: defaults
actions:
  greet: ./greet
defaults:
  greet: {name: a}
  ./greet: {name: b}
jobs:
- name: J
  steps:
  - uses: greet
`)
	if err == nil || !strings.Contains(err.Error(), "defaults: greet and ./greet are the same action") {
		t.Errorf("Load() = %v, want the defaults refused", err)
	}
}

// A step that is given the same default as another keeps a copy of its own.
func TestApplyDefaults_CopiesWhatItGives(t *testing.T) {
	job := &Job{
		Defaults: map[string]any{"http": map[string]any{"headers": map[string]any{"a": "1"}}},
		Steps:    []*Step{{Uses: "http"}, {Uses: "http"}},
	}
	job.ApplyDefaults()
	job.Steps[0].With["headers"].(map[string]any)["b"] = "2"
	if got := job.Steps[1].With["headers"].(map[string]any); len(got) != 1 {
		t.Errorf("steps[1].with.headers = %v, want it apart from that of steps[0]", got)
	}
	if got := job.Defaults.(map[string]any)["http"].(map[string]any)["headers"].(map[string]any); len(got) != 1 {
		t.Errorf("defaults.http.headers = %v, want it as it was written", got)
	}
}
