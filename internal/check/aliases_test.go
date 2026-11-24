package check

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/mozership/probe/internal/actionref"
)

const aliasSHA = "0123456789abcdef0123456789abcdef01234567"

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
