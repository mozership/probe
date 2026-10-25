package oas

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/mozership/probe/report"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const coverageSpec = `openapi: 3.0.3
info: {title: users, version: "1"}
paths:
  /users/{id}:
    get:
      responses:
        "200": {description: a user}
        "404": {description: no such user}
    delete:
      responses:
        "204": {description: deleted}
  /items:
    get:
      responses:
        "2XX": {description: items}
        default: {description: an error}
`

func writeCoverageSpec(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "openapi.yml")
	require.NoError(t, os.WriteFile(path, []byte(coverageSpec), 0o600))
	return path
}

// coverageReport is a report whose steps were matched to the contracts given.
func coverageReport(contracts ...*report.Contract) *report.Report {
	var steps []report.Step
	for i, c := range contracts {
		steps = append(steps, report.Step{Index: i, Contract: c})
	}
	// A step without a contract, as one of another action, counts for none.
	steps = append(steps, report.Step{Index: len(steps)})
	return &report.Report{Jobs: []report.Job{{Steps: steps}}}
}

func TestNewCoverage(t *testing.T) {
	spec := writeCoverageSpec(t)
	r := coverageReport(
		&report.Contract{Spec: spec, Operation: "GET /users/{id}", Response: "200"},
		&report.Contract{Spec: spec, Operation: "GET /users/{id}", Response: "200"},
		&report.Contract{Spec: spec, Operation: "GET /items", Response: "default"},
		// A status the operation does not declare counts for the operation alone.
		&report.Contract{Spec: spec, Operation: "DELETE /users/{id}"},
		// Another document counts for none of this one.
		&report.Contract{Spec: "other.yml", Operation: "GET /users/{id}", Response: "404"},
	)

	cov, err := NewCoverage(spec, r)
	require.NoError(t, err)

	want := []OperationCoverage{
		{Operation: "GET /users/{id}", Steps: 2, Responses: []ResponseCoverage{{"200", 2}, {"404", 0}}},
		{Operation: "DELETE /users/{id}", Steps: 1, Responses: []ResponseCoverage{{"204", 0}}},
		{Operation: "GET /items", Steps: 1, Responses: []ResponseCoverage{{"2XX", 0}, {"default", 1}}},
	}
	assert.Equal(t, want, cov.Operations)

	ops, checkedOps, ress, checkedRess := cov.Counts()
	assert.Equal(t, []int{3, 3, 5, 2}, []int{ops, checkedOps, ress, checkedRess})
}

func TestNewCoverage_SpecWrittenAnotherWay(t *testing.T) {
	spec := writeCoverageSpec(t)
	r := coverageReport(&report.Contract{Spec: filepath.Join(filepath.Dir(spec), ".", "openapi.yml"), Operation: "GET /items", Response: "2XX"})

	cov, err := NewCoverage(spec, r)
	require.NoError(t, err)
	assert.Equal(t, 1, cov.Operations[2].Responses[0].Steps)
}

func TestNewCoverage_Errors(t *testing.T) {
	spec := writeCoverageSpec(t)
	tests := []struct {
		name    string
		spec    string
		report  *report.Report
		wantErr string
	}{
		{
			name:    "no step checked against a document",
			spec:    spec,
			report:  coverageReport(),
			wantErr: "no step of the report was checked against a contract",
		},
		{
			name:    "steps checked against another document",
			spec:    spec,
			report:  coverageReport(&report.Contract{Spec: "other.yml", Operation: "GET /items"}),
			wantErr: "they were checked against other.yml",
		},
		{
			name:    "a document that cannot be read",
			spec:    filepath.Join(t.TempDir(), "none.yml"),
			report:  coverageReport(),
			wantErr: "failed to read OpenAPI spec",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := NewCoverage(tt.spec, tt.report)
			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.wantErr)
		})
	}
}

func TestCoverage_Write(t *testing.T) {
	cov := &Coverage{
		Spec: "openapi.yml",
		Operations: []OperationCoverage{
			{Operation: "GET /users/{id}", Steps: 2, Responses: []ResponseCoverage{{"200", 2}, {"404", 0}}},
			{Operation: "DELETE /users/{id}", Steps: 0, Responses: []ResponseCoverage{{"204", 0}}},
			{Operation: "GET /items", Steps: 1, Responses: []ResponseCoverage{{"default", 1}}},
		},
	}
	var buf bytes.Buffer
	require.NoError(t, cov.Write(&buf))
	want := `Coverage of openapi.yml

✓ GET /users/{id} (2 steps)
    ✓ 200 (2 steps)
    - 404
- DELETE /users/{id}
    - 204
✓ GET /items (1 step)
    ✓ default (1 step)

Operations: 2 of 3 checked (66.7%)
Responses:  2 of 4 checked (50.0%)
`
	assert.Equal(t, want, buf.String())
}

func TestCoverage_WriteNothingDeclared(t *testing.T) {
	var buf bytes.Buffer
	require.NoError(t, (&Coverage{Spec: "empty.yml"}).Write(&buf))
	assert.Contains(t, buf.String(), "Operations: none declared\nResponses:  none declared\n")
}

func TestReadReport(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "probe-report.json")
	r := coverageReport(&report.Contract{Spec: "openapi.yml", Operation: "GET /items", Response: "2XX"})
	var buf bytes.Buffer
	require.NoError(t, r.WriteJSON(&buf))
	require.NoError(t, os.WriteFile(path, buf.Bytes(), 0o600))

	got, err := ReadReport(path)
	require.NoError(t, err)
	assert.Equal(t, r.Jobs[0].Steps[0].Contract, got.Jobs[0].Steps[0].Contract)

	broken := filepath.Join(dir, "broken.json")
	require.NoError(t, os.WriteFile(broken, []byte("{"), 0o600))
	_, err = ReadReport(broken)
	assert.ErrorContains(t, err, "failed to parse report")

	_, err = ReadReport(filepath.Join(dir, "none.json"))
	assert.ErrorContains(t, err, "failed to read report")
}

func TestCountCoverage(t *testing.T) {
	r := coverageReport(
		&report.Contract{Spec: "users.proto", Operation: "users.v1.UserService/GetUser"},
		&report.Contract{Spec: "users.proto", Operation: "users.v1.UserService/GetUser"},
	)
	declared := []Declared{
		{Operation: "users.v1.UserService/GetUser"},
		{Operation: "users.v1.UserService/DeleteUser"},
	}
	cov, err := CountCoverage("./users.proto", declared, r)
	require.NoError(t, err)
	assert.Equal(t, []OperationCoverage{
		{Operation: "users.v1.UserService/GetUser", Steps: 2},
		{Operation: "users.v1.UserService/DeleteUser", Steps: 0},
	}, cov.Operations)
}

func TestCoverage_WriteOperationsOnly(t *testing.T) {
	cov := &Coverage{
		Spec:           "users.proto",
		OperationsOnly: true,
		Operations: []OperationCoverage{
			{Operation: "UserService/GetUser", Steps: 1},
			{Operation: "UserService/DeleteUser"},
		},
	}
	var buf bytes.Buffer
	require.NoError(t, cov.Write(&buf))
	assert.Equal(t, "Coverage of users.proto\n\n\u2713 UserService/GetUser (1 step)\n- UserService/DeleteUser\n\nOperations: 1 of 2 checked (50.0%)\n", buf.String())
}
