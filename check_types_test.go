package probe

import (
	"maps"
	"testing"

	"github.com/mozership/probe/internal/kind"
)

var (
	reqTypes = map[string]map[string]string{
		"http":  {"url": kind.String, "method": kind.String, "headers": kind.Map, "body": kind.String},
		"shell": {"cmd": kind.String},
		"hello": {},
	}
	resTypes = map[string]map[string]string{
		"http":  {"status": kind.String, "code": kind.Number, "headers": kind.Map, "body": kind.String},
		"shell": {"code": kind.Number, "stdout": kind.String, "stderr": kind.String},
		"hello": {"dump": kind.Bool, "status": kind.Number},
	}
)

// TestCheck_Types checks the expressions and templates by the kinds of what
// they read: what is written, what a function returns, the fields of the
// step's context and those an action declares of its res and req.
func TestCheck_Types(t *testing.T) {
	path := writeWorkflow(t, t.TempDir(), "workflow.yml", `name: types
vars:
  id: "{{ 'id-' + 1 }}"
jobs:
- name: "Users {{ 1 + 'a' }}"
  skipif: repeat_index + 1
  steps:
  - name: Get
    uses: http
    with:
      get: "/users/{{ 'id-' + 1 }}"
    test: res.code == "200 OK"
    outputs:
      size: res.headers + 1
    echo: "{{ res.status * 2 }}"
  - name: Not a test
    uses: http
    with:
      get: /
    skipif: status
    test: res.code
  - name: Command
    uses: shell
    with:
      cmd: echo
    test: res.stdout contains 1 || req.cmd != 1
  - name: Greet
    uses: hello
    with:
      status: ok
    test: res.status == "ok"
  - name: Template
    uses: http
    with:
      get: /
    test: "{{ res.code == 200 }}"
- name: Load
  repeat:
    count: 2
    test: steps.search.success_rate > "0.9"
    baseline_test: baseline.steps.search.rt.p95
  steps:
  - name: Search
    id: search
    uses: http
    with:
      get: /search
    test: res.code == 200
`)
	opts := checkActions
	opts.ReqFields, opts.ResFields, opts.WithFields = reqFields, resFields, []string{"hello"}
	opts.ReqTypes, opts.ResTypes = reqTypes, resTypes
	findings, err := Check(path, opts)
	if err != nil {
		t.Fatal(err)
	}
	checkFindings(t, findings, []want{
		{SeverityError, 3, `vars.id: template {{ 'id-' + 1 }}: "id-" + 1 fails: "id-" is a string and 1 a number, which + does not take together; to join them as text, convert the number with string(): "id-" + string(1)`},
		{SeverityError, 5, `name: template {{ 1 + 'a' }}: 1 + "a" fails`},
		{SeverityError, 6, `skipif: repeat_index + 1 is a number, not true or false, so the job is not skipped, whatever it comes to; compare it with what it should be`},
		{SeverityError, 11, `with.get: template {{ 'id-' + 1 }}: "id-" + 1 fails`},
		{SeverityError, 12, `test: res.code == "200 OK" is never true: res.code is a number and "200 OK" a string, which are never equal; a number is never equal to a string: compare res.code with a number`},
		{SeverityError, 14, `outputs.size: res.headers + 1 fails: res.headers is a map and 1 a number`},
		{SeverityError, 15, `echo: template {{ res.status * 2 }}: res.status * 2 fails`},
		{SeverityError, 20, `skipif: status is a number, not true or false, so the step is not skipped, whatever it comes to`},
		{SeverityError, 21, `test: res.code is a number, not true or false, so the step fails`},
		{SeverityError, 26, `test: res.stdout contains 1 fails: res.stdout is a string and 1 a number, which contains does not take together; contains takes strings`},
		{SeverityError, 31, `test: res.status == "ok" is never true: res.status is a number`},
		{SeverityError, 36, `test: a map key must be a quoted string`},
		{SeverityError, 40, `repeat.test: steps.search.success_rate > "0.9" fails: steps.search.success_rate is a number and "0.9" a string`},
		{SeverityError, 41, `repeat.baseline_test: baseline.steps.search.rt.p95 is a number, not true or false, so the job fails`},
	})
}

// TestCheck_TypesNotKnown checks that nothing is told of what may hold
// anything: vars, which --var gives as text, outputs, what is under a
// field, such as res.body, the fields of an action that declares no kinds,
// and the keys of with a step of the hello action returns.
func TestCheck_TypesNotKnown(t *testing.T) {
	path := writeWorkflow(t, t.TempDir(), "workflow.yml", `name: types
vars:
  port: 8080
  name: "{{ USER ?? 'probe' }}"
jobs:
- name: Users
  skipif: vars.port == "8080"
  steps:
  - name: Get
    id: get
    uses: http
    with:
      get: "/users/{{ vars.port + 1 }}?name={{ vars.name }}&n={{ len(vars.name) + 1 }}"
    test: res.code == 200 && res.body.id == "1" && res.rawbody contains "id" && res.body + 1 > 0 && res.anything == "x"
    outputs:
      id: res.body.id + 1
      code: string(res.code) + " OK"
    echo: "{{ res.code }} {{ res.status }} {{ res.code + 1 }} {{ outputs.get.id + 'x' }}"
  - name: External
    uses: ./actions/mine
    test: res.code == "200 OK"
  - name: Greet
    uses: hello
    with:
      name: probe
      count: 2
    test: res.name == 1 && res.count == "2" && req.name == 2 && res.status == 0 && !res.dump
  - name: Command
    uses: shell
    with:
      cmd: echo
    test: 'res.json.ok == "yes" && res.stdout != "" && (res.code == 0 ? true : false) && res.stdout[0:1] == "{"'
`)
	// The fields are not checked, for the test to read a field the action
	// does not declare.
	opts := checkActions
	opts.ReqTypes, opts.ResTypes = reqTypes, resTypes
	findings, err := Check(path, opts)
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range findings {
		if f.Severity == SeverityError {
			t.Errorf("finding %s, want none for what may hold anything", f)
		}
	}
}

// What readActionResult reads of res is of the kinds readResTypes tells
// check of.
func TestReadResTypes(t *testing.T) {
	declared := map[string]string{"body": kind.String, "code": kind.Number, "stdout": kind.String}
	got := readResTypes(declared)
	want := map[string]string{"rawbody": kind.String, "code": kind.Number, "stdout": kind.String}
	if !maps.Equal(got, want) {
		t.Errorf("readResTypes(%v) = %v, want %v", declared, got, want)
	}
	if _, ok := declared["rawbody"]; ok {
		t.Error("readResTypes changed the kinds it was given")
	}
	for _, res := range []map[string]any{
		{"body": `{"id": 1}`, "code": int64(200), "stdout": `{"ok": true}`},
		{"body": "text", "code": int64(200), "stdout": "text"},
	} {
		r := readActionResult(map[string]any{"res": res, "status": 0})
		for field, k := range got {
			if v, ok := r.Res[field]; ok {
				if vk, _ := kind.Of(v); vk != k {
					t.Errorf("res.%s is %v (%T), want a %s", field, v, v, k)
				}
			}
		}
	}
}

// TestCheck_TypesAfterRefused checks that a part of an expression the
// checker refuses and an evaluation takes, such as an index of a string,
// hides nothing after it, that a not in and a not contains are told as
// written, and that an external action is checked by no kind, even one
// given kinds by its name.
func TestCheck_TypesAfterRefused(t *testing.T) {
	path := writeWorkflow(t, t.TempDir(), "workflow.yml", `name: types
jobs:
- name: Commands
  steps:
  - name: Index
    uses: shell
    with:
      cmd: echo 4
    test: res.stdout[0] == "4" && res.code == "0"
  - name: Not contains
    uses: shell
    with:
      cmd: echo
    test: res.stdout not contains 1
  - name: Not in
    uses: shell
    with:
      cmd: echo
    test: res.code not in [res.stdout] && res.code not in ["0"]
  - name: External
    uses: ./actions/mine
    test: res.code == "200 OK"
`)
	opts := checkActions
	opts.ReqTypes = reqTypes
	opts.ResTypes = maps.Clone(resTypes)
	opts.ResTypes["./actions/mine"] = map[string]string{"code": kind.Number}
	findings, err := Check(path, opts)
	if err != nil {
		t.Fatal(err)
	}
	checkFindings(t, findings, []want{
		{SeverityError, 9, `test: res.code == "0" is never true: res.code is a number and "0" a string`},
		{SeverityError, 14, `test: res.stdout not contains 1 fails: res.stdout is a string and 1 a number`},
		{SeverityError, 19, `test: res.code not in [res.stdout] is always true`},
	})
}
