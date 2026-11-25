package check

import "testing"

// probe check tells, on the line of each output of the workflow, one that
// does not parse, one that reads what no step publishes, and one that reads
// a name the outputs of a workflow cannot read. The outputs of a step of
// any job are read once every job is over, so none is read too early.
func TestCheck_WorkflowOutputs(t *testing.T) {
	path := writeWorkflow(t, t.TempDir(), "workflow.yml", `name: outputs
vars:
  queue: orders
jobs:
- name: Receive
  id: receive
  steps:
  - id: take
    uses: http
    with:
      url: http://localhost/{{ vars.queue }}
    test: res.code == 200
    outputs:
      messages: res.body.messages
      cursor: res.body.next
- name: Later
  steps:
  - uses: http
    with:
      url: http://localhost
    test: res.code == 200
outputs:
  messages: outputs.take.messages
  cursor: outputs.cursor ?? vars.cursor
  queue: vars.queue
  broken: outputs.take.
  missing: outputs.take.rows
  nobody: outputs.nobody
  code: res.code
  undefined: vars.nope
`)
	findings, err := Check(path, checkActions)
	if err != nil {
		t.Fatal(err)
	}
	checkFindings(t, findings, []want{
		{SeverityWarning, 24, "outputs.cursor: vars.cursor is not defined by the workflow, so it is nil unless --var gives it"},
		{SeverityError, 26, "outputs.broken:"},
		{SeverityError, 27, `outputs.missing: outputs.take.rows is not published: step "take" publishes cursor, messages`},
		{SeverityError, 28, "outputs.nobody: outputs.nobody is not published by any step"},
		{SeverityError, 29, "outputs.code: res is not defined; it can read outputs and vars"},
		{SeverityWarning, 30, "outputs.undefined: vars.nope is not defined by the workflow"},
	})
}

// An output of the workflow with no expression is refused, as nothing
// could be given for it.
func TestCheck_WorkflowOutputEmpty(t *testing.T) {
	path := writeWorkflow(t, t.TempDir(), "workflow.yml", `name: outputs
jobs:
- name: J
  steps:
  - uses: hello
outputs:
  nothing: ""
`)
	findings, err := Check(path, checkActions)
	if err != nil {
		t.Fatal(err)
	}
	checkFindings(t, findings, []want{
		{SeverityError, 7, "outputs.nothing: the expression is empty"},
	})
}
