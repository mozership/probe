package check

import "testing"

// probe check reads the template of an iteration as it reads any other: one
// that does not parse, and an output it reads before the step that publishes
// it has run, are told on its line.
func TestCheck_IterationTemplate(t *testing.T) {
	path := writeWorkflow(t, t.TempDir(), "workflow.yml", `name: iteration
jobs:
- name: J
  steps:
  - uses: http
    with:
      url: http://localhost
    iteration: "{{ parse_json(file('cases.json') }}"
    test: res.code == 200
  - uses: http
    with:
      url: http://localhost
    iteration: "{{ outputs.later.rows }}"
    test: res.code == 200
  - id: later
    uses: http
    with:
      url: http://localhost
    test: res.code == 200
    outputs:
      rows: res.body.rows
`)
	findings, err := Check(path, CheckOptions{})
	if err != nil {
		t.Fatal(err)
	}
	checkFindings(t, findings, []want{
		{SeverityError, 8, "iteration: template {{ parse_json(file('cases.json') }}"},
		{SeverityError, 13, "iteration: outputs.later.rows"},
	})
}
