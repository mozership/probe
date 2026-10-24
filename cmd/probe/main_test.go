package main

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/mozership/probe"
	"github.com/mozership/probe/actionrpc"
	"github.com/mozership/probe/actions"
)

func TestCmd_isValid(t *testing.T) {
	c := newBufferCmd()

	tests := []struct {
		name     string
		flag     string
		expected bool
	}{
		{
			name:     "valid short flag",
			flag:     "-help",
			expected: true,
		},
		{
			name:     "valid long flag",
			flag:     "--help",
			expected: true,
		},
		{
			name:     "invalid flag",
			flag:     "--invalid",
			expected: false,
		},
		{
			name:     "invalid short flag",
			flag:     "-invalid",
			expected: false,
		},
		{
			name:     "valid verbose flag",
			flag:     "--verbose",
			expected: true,
		},
		{
			name:     "valid timing flag",
			flag:     "--timing",
			expected: true,
		},
		{
			name:     "valid v flag (shorthand)",
			flag:     "-v",
			expected: true,
		},
		{
			name:     "valid h flag (shorthand)",
			flag:     "-h",
			expected: true,
		},
		{
			name:     "valid version flag",
			flag:     "--version",
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := c.isValid(tt.flag)
			if result != tt.expected {
				t.Errorf("isValid(%q) = %v, want %v", tt.flag, result, tt.expected)
			}
		})
	}
}

func TestCmd_usage(t *testing.T) {
	c := newBufferCmd()
	c.ver = "test-version"
	c.rev = "test-commit"
	c.usage()
	output := fmt.Sprintf("%s", c.errWriter)

	// Check that output contains expected elements
	expectedElements := []string{
		"Probe - A YAML-based workflow automation tool",
		"https://github.com/mozership/probe",
		"test-version",
		"test-commit",
		"Usage: probe [options] <workflow-file>",
		"Options:",
	}

	for _, element := range expectedElements {
		if !strings.Contains(output, element) {
			t.Errorf("usage() output should contain %q, but it doesn't. Output: %s", element, output)
		}
	}

	// Check ASCII art is present (note: colors may not be in output during testing)
	if !strings.Contains(output, "__  __  __  __  __") {
		t.Errorf("usage() output should contain ASCII art")
	}
}

func TestCmd_start(t *testing.T) {
	help := " __  __  __  __  __\n|  ||  ||  ||  || _|\n|  ||  /| |||  /|  |\n| | |  \\| |||  \\| _|\n|_| |_\\_|__||__||__|\n\nProbe - A YAML-based workflow automation tool.\nhttps://github.com/mozership/probe (ver: dev, rev: unknown)\n\nUsage: probe [options] <workflow-file>...\n       probe gen <openapi-file>\n       probe dag [--mermaid] [--job <job>] <workflow-file>...\n       probe check <workflow-file>...\n       probe fmt [--check] <workflow-file>...\n       probe coverage <openapi-file|proto-file> <report-file>\n       probe guide [topic]\n       probe skill [install [dir]]\n       probe manifest init <owner>/<repo>\n       probe manifest <tag> <checksums-file> [action-file]\n\nArguments:\n  workflow-file    Path to a YAML workflow file, a directory or a glob.\n                   Several are merged in their order into one workflow,\n                   given apart or with commas: \"base.yml override.yml\" or\n                   \"base.yml,override.yml\".\n\nSubcommands:\n  gen <file>       Generate probe workflow YAML from OpenAPI specification\n  dag <file>...    Show job dependency graph as ASCII art (default)\n                   Use --mermaid to output in Mermaid format, and --job to\n                   show only the jobs a run with it runs\n  check <file>...  Find what is wrong or weak in a workflow without running it\n  fmt <file>...    Format workflow files in place, and print those it changed\n                   A directory is every YAML file under it, and - is the\n                   standard input, formatted to the standard output\n                   Use --check to change nothing, and exit with 1 when a\n                   file is not formatted\n  coverage <openapi-file|proto-file> <report-file>\n                   Show which operations of an OpenAPI document, or methods of\n                   a .proto file, the steps of a run checked, from its\n                   --report json file\n  guide [topic]    Print a page of the documentation as Markdown\n                   Without a topic, list the topics\n  skill            Print the skill that teaches coding agents to use Probe\n                   install [dir] writes it to dir (.claude/skills/probe)\n  manifest init <owner>/<repo>\n                   Print an action.yml to start a new external action from,\n                   published in that GitHub repository\n  manifest <tag> <checksums-file> [action-file]\n                   Print the action.yml of a release of an external action:\n                   action-file (action.yml) with the tag in runs.url and the\n                   digests of checksums-file in runs.checksums\n\nOptions:\n  -h, --help         Show command usage\n      --version      Show version information\n      --timing       Show timing (start time, response time)\n  -v, --verbose      Show verbose log\n      --output       Report output: auto, spinner or stream (env: PROBE_OUTPUT)\n      --report       Write reports: json, junit, markdown, github-summary as format[=path],... (env: PROBE_REPORT)\n      --baseline     Compare the response times to those of a --report json file (env: PROBE_BASELINE)\n      --read-only    Refuse what writes, such as an HTTP POST or an UPDATE (env: PROBE_READ_ONLY)\n      --allow-host   Refuse connecting to hosts but these, as host[:port] or *.domain,... (env: PROBE_ALLOW_HOSTS)\n      --allow-action Run these actions under --read-only or --allow-host although they do not keep to them (env: PROBE_ALLOW_ACTIONS)\n      --var          Give a var of the workflow as name=value, in place of what it writes; can be repeated\n      --env-file     Read variables for the vars from files of NAME=value lines, as file,...; can be repeated\n      --job          Run only these jobs, by id or name, as job,..., with the jobs they need and the always jobs after them; can be repeated\n"

	tests := []struct {
		name           string
		args           []string
		expectCode     int
		expectWorkflow string
		expectVerbose  bool
		expectTiming   bool
		expectHelp     bool
		expectOutput   string
	}{
		{
			name:           "help flag",
			args:           []string{"probe", "--help"},
			expectCode:     1,
			expectHelp:     true,
			expectWorkflow: "",
			expectVerbose:  false,
			expectTiming:   false,
			expectOutput:   help,
		},
		{
			name:           "version command",
			args:           []string{"probe", "--version"},
			expectCode:     0,
			expectHelp:     false,
			expectWorkflow: "",
			expectVerbose:  false,
			expectTiming:   false,
			expectOutput:   "",
		},
		{
			name:           "no workflow specified",
			args:           []string{"probe"},
			expectCode:     probe.ExitConfigError,
			expectHelp:     false,
			expectWorkflow: "",
			expectVerbose:  false,
			expectTiming:   false,
			expectOutput:   "[ERROR] workflow is required\n",
		},
		{
			name:           "workflow argument",
			args:           []string{"probe", "test.yml"},
			expectCode:     0,
			expectHelp:     false,
			expectWorkflow: "test.yml",
			expectVerbose:  false,
			expectTiming:   false,
			expectOutput:   "",
		},
		{
			name:           "verbose flag without workflow",
			args:           []string{"probe", "--verbose"},
			expectCode:     probe.ExitConfigError,
			expectHelp:     false,
			expectWorkflow: "",
			expectVerbose:  true,
			expectTiming:   false,
			expectOutput:   "[ERROR] workflow is required\n",
		},
		{
			name:           "timing flag without workflow",
			args:           []string{"probe", "--timing"},
			expectCode:     probe.ExitConfigError,
			expectHelp:     false,
			expectWorkflow: "",
			expectVerbose:  false,
			expectTiming:   true,
			expectOutput:   "[ERROR] workflow is required\n",
		},
		{
			name:           "multiple flags with workflow argument",
			args:           []string{"probe", "--verbose", "--timing", "test.yml"},
			expectCode:     0,
			expectHelp:     false,
			expectWorkflow: "test.yml",
			expectVerbose:  true,
			expectTiming:   true,
			expectOutput:   "",
		},
		{
			name:           "v shorthand flag with workflow argument",
			args:           []string{"probe", "-v", "test.yml"},
			expectCode:     0,
			expectHelp:     false,
			expectWorkflow: "test.yml",
			expectVerbose:  true,
			expectTiming:   false,
			expectOutput:   "",
		},
		{
			name:           "h shorthand flag",
			args:           []string{"probe", "-h"},
			expectCode:     1,
			expectHelp:     true,
			expectWorkflow: "",
			expectVerbose:  false,
			expectTiming:   false,
			expectOutput:   help,
		},
		{
			name:           "options after argument",
			args:           []string{"probe", "test.yml", "--verbose"},
			expectCode:     0,
			expectHelp:     false,
			expectWorkflow: "test.yml",
			expectVerbose:  true,
			expectTiming:   false,
			expectOutput:   "",
		},
		{
			name:           "options after argument with multiple flags",
			args:           []string{"probe", "test.yml", "--verbose", "--timing"},
			expectCode:     0,
			expectHelp:     false,
			expectWorkflow: "test.yml",
			expectVerbose:  true,
			expectTiming:   true,
			expectOutput:   "",
		},
		{
			name:           "mixed options before and after argument",
			args:           []string{"probe", "--verbose", "test.yml", "--timing"},
			expectCode:     0,
			expectHelp:     false,
			expectWorkflow: "test.yml",
			expectVerbose:  true,
			expectTiming:   true,
			expectOutput:   "",
		},
		{
			name:           "shorthand option after argument",
			args:           []string{"probe", "test.yml", "-v"},
			expectCode:     0,
			expectHelp:     false,
			expectWorkflow: "test.yml",
			expectVerbose:  true,
			expectTiming:   false,
			expectOutput:   "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := newBufferCmd()
			code := c.start(tt.args)
			output := fmt.Sprintf("%s", c.errWriter)

			if code != tt.expectCode {
				t.Errorf("start(%v) = %d, want %d", tt.args, code, tt.expectCode)
			}

			if c.WorkflowPath != tt.expectWorkflow {
				t.Errorf("start(%v).WorkflowPath = %q, want %q", tt.args, c.WorkflowPath, tt.expectWorkflow)
			}

			if c.Verbose != tt.expectVerbose {
				t.Errorf("start(%v).Verbose = %v, want %v", tt.args, c.Verbose, tt.expectVerbose)
			}

			if c.Timing != tt.expectTiming {
				t.Errorf("start(%v).Timing = %v, want %v", tt.args, c.Timing, tt.expectTiming)
			}

			if c.Help != tt.expectHelp {
				t.Errorf("start(%v).Help = %v, want %v", tt.args, c.Help, tt.expectHelp)
			}

			// Check that version and commit are set
			if c.ver != version {
				t.Errorf("start(%v).ver = %q, want %q", tt.args, c.ver, version)
			}

			if c.rev != commit {
				t.Errorf("start(%v).rev = %q, want %q", tt.args, c.rev, commit)
			}

			if output != tt.expectOutput {
				t.Errorf("start(%v) output to %q, want %q", tt.args, output, tt.expectOutput)
			}

			// Check validFlags
			expectedFlags := []string{"help", "h", "version", "timing", "verbose", "v", "mermaid", "check", "output", "report", "baseline", "read-only", "allow-host", "allow-action", "var", "env-file", "job"}
			if len(c.validFlags) != len(expectedFlags) {
				t.Errorf("start(%v) validFlags length = %d, want %d", tt.args, len(c.validFlags), len(expectedFlags))
			}
		})
	}
}

func TestCmd_gen(t *testing.T) {
	tests := []struct {
		name       string
		args       []string
		expectCode int
		contains   []string
		errContain string
	}{
		{
			name:       "gen with petstore spec",
			args:       []string{"probe", "gen", "../../testdata/petstore.yml"},
			expectCode: 0,
			contains: []string{
				"Pet Store API Tests",
				"id: pets",
				"id: list-pets",
				"uses: http",
			},
		},
		{
			name:       "gen without file argument",
			args:       []string{"probe", "gen"},
			expectCode: probe.ExitConfigError,
			errContain: "OpenAPI spec file is required",
		},
		{
			name:       "gen with two files",
			args:       []string{"probe", "gen", "../../testdata/petstore.yml", "other.yml"},
			expectCode: probe.ExitConfigError,
			errContain: "gen takes one OpenAPI spec file, not 2",
		},
		{
			name:       "gen with nonexistent file",
			args:       []string{"probe", "gen", "nonexistent.yml"},
			expectCode: probe.ExitConfigError,
			errContain: "failed to read OpenAPI spec",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := newBufferCmd()
			code := c.start(tt.args)
			if code != tt.expectCode {
				t.Errorf("start(%v) = %d, want %d", tt.args, code, tt.expectCode)
			}
			if len(tt.contains) > 0 {
				output := fmt.Sprintf("%s", c.outWriter)
				for _, s := range tt.contains {
					if !strings.Contains(output, s) {
						t.Errorf("output should contain %q, got: %s", s, output)
					}
				}
			}
			if tt.errContain != "" {
				errOutput := fmt.Sprintf("%s", c.errWriter)
				if !strings.Contains(errOutput, tt.errContain) {
					t.Errorf("error output should contain %q, got: %s", tt.errContain, errOutput)
				}
			}
		})
	}
}

func TestCmd_check(t *testing.T) {
	dir := t.TempDir()
	write := func(name, content string) string {
		path := filepath.Join(dir, name)
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
		return path
	}
	clean := write("clean.yml", "name: clean\njobs:\n- name: J\n  steps:\n  - uses: hello\n    test: res.code == 0\n")
	weak := write("weak.yml", "name: weak\njobs:\n- name: J\n  steps:\n  - uses: hello\n")
	broken := write("broken.yml", "name: broken\njobs:\n- name: J\n  steps:\n  - uses: htp\n    test: res.code == 0\n")
	// A workflow split into three files, which only read together have
	// what a workflow needs.
	head := write("head.yml", "name: split\n")
	vars := write("vars.yml", "vars:\n  who: x\n")
	jobs := write("jobs.yml", "jobs:\n- name: J\n  steps:\n  - uses: hello\n    with:\n      name: \"{{vars.who}}\"\n    test: res.code == 0\n")
	notes := write("notes.txt", "name: notes\n")

	tests := []struct {
		name       string
		args       []string
		expectCode int
		contains   string
		errContain string
	}{
		{"a workflow with nothing wrong", []string{"probe", "check", clean}, 0, "No problems found", ""},
		{"a workflow with warnings only", []string{"probe", "check", weak}, 0, "0 errors, 1 warning", ""},
		{"a workflow with an error", []string{"probe", "check", broken}, probe.ExitConfigError, `unknown action "htp"; did you mean "http"?`, ""},
		{"no workflow", []string{"probe", "check"}, probe.ExitConfigError, "", "workflow file is required"},
		{"files given apart", []string{"probe", "check", head, vars, jobs}, 0, "No problems found", ""},
		{"files given apart and with commas", []string{"probe", "check", head + "," + vars, jobs}, 0, "No problems found", ""},
		{"one of the files alone", []string{"probe", "check", head}, probe.ExitConfigError, head + ": error: configuration error in decode_yaml", ""},
		{"files that both have jobs", []string{"probe", "check", weak, clean}, 0, clean + ":2: warning: jobs replaces the jobs of " + weak + ":2, which do not run", ""},
		{"a file named that is not YAML", []string{"probe", "check", clean, notes}, probe.ExitConfigError, "", "notes.txt: not a YAML file"},
		{"a second file that does not exist", []string{"probe", "check", clean, filepath.Join(dir, "none.yml")}, probe.ExitConfigError, "", "none.yml: no such file"},
		{"a workflow that does not exist", []string{"probe", "check", filepath.Join(dir, "none.yml")}, probe.ExitConfigError, "", "no such file"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := newBufferCmd()
			if code := c.start(tt.args); code != tt.expectCode {
				t.Errorf("start(%v) = %d, want %d; stdout: %s; stderr: %s", tt.args, code, tt.expectCode, c.outWriter, c.errWriter)
			}
			if out := fmt.Sprintf("%s", c.outWriter); !strings.Contains(out, tt.contains) {
				t.Errorf("output should contain %q, got: %s", tt.contains, out)
			}
			if errOut := fmt.Sprintf("%s", c.errWriter); !strings.Contains(errOut, tt.errContain) {
				t.Errorf("error output should contain %q, got: %s", tt.errContain, errOut)
			}
		})
	}
}

func TestCmd_coverage(t *testing.T) {
	dir := t.TempDir()
	spec := filepath.Join(dir, "openapi.yml")
	if err := os.WriteFile(spec, []byte(`openapi: 3.0.3
info: {title: users, version: "1"}
paths:
  /users:
    get:
      responses:
        "200": {description: users}
`), 0o600); err != nil {
		t.Fatal(err)
	}
	proto := filepath.Join(dir, "users.proto")
	if err := os.WriteFile(proto, []byte("syntax = \"proto3\";\npackage users.v1;\nimport \"not/at/hand.proto\";\nservice UserService {\n  rpc GetUser(Req) returns (Res);\n  rpc DeleteUser(Req) returns (Res);\n}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	reportFile := filepath.Join(dir, "probe-report.json")
	if err := os.WriteFile(reportFile, []byte(`{"jobs":[{"steps":[{"index":0,"contract":{"spec":"`+spec+`","operation":"GET /users","response":"200"}},{"index":1,"contract":{"spec":"`+proto+`","operation":"users.v1.UserService/GetUser"}}]}]}`), 0o600); err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name       string
		args       []string
		expectCode int
		contains   string
		errContain string
	}{
		{
			name:       "coverage of a document by a report",
			args:       []string{"probe", "coverage", spec, reportFile},
			expectCode: 0,
			contains:   "Operations: 1 of 1 checked (100.0%)",
		},
		{
			name:       "coverage without a report",
			args:       []string{"probe", "coverage", spec},
			expectCode: probe.ExitConfigError,
			errContain: "an OpenAPI spec file or a .proto file, and a report file, are required",
		},
		{
			name:       "coverage with a report that cannot be read",
			args:       []string{"probe", "coverage", spec, filepath.Join(dir, "none.json")},
			expectCode: probe.ExitConfigError,
			errContain: "failed to read report",
		},
		{
			name:       "coverage of a .proto file",
			args:       []string{"probe", "coverage", proto, reportFile},
			expectCode: 0,
			contains:   "Operations: 1 of 2 checked (50.0%)",
		},
		{
			name:       "coverage with a document the report did not check",
			args:       []string{"probe", "coverage", "../../testdata/petstore.yml", reportFile},
			expectCode: probe.ExitConfigError,
			errContain: "no step of the report was checked against ../../testdata/petstore.yml",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := newBufferCmd()
			if code := c.start(tt.args); code != tt.expectCode {
				t.Errorf("start(%v) = %d, want %d; stderr: %s", tt.args, code, tt.expectCode, c.errWriter)
			}
			if out := fmt.Sprintf("%s", c.outWriter); !strings.Contains(out, tt.contains) {
				t.Errorf("output should contain %q, got: %s", tt.contains, out)
			}
			if errOut := fmt.Sprintf("%s", c.errWriter); !strings.Contains(errOut, tt.errContain) {
				t.Errorf("error output should contain %q, got: %s", tt.errContain, errOut)
			}
		})
	}
}

func TestCmd_parseArgs_dag(t *testing.T) {
	tests := []struct {
		name             string
		args             []string
		expectSubCommand string
		expectSubArgs    []string
		expectDagMermaid bool
	}{
		{
			name:             "dag with workflow file",
			args:             []string{"dag", "workflow.yml"},
			expectSubCommand: "dag",
			expectSubArgs:    []string{"workflow.yml"},
			expectDagMermaid: false,
		},
		{
			name:             "dag with --mermaid flag before file",
			args:             []string{"dag", "--mermaid", "workflow.yml"},
			expectSubCommand: "dag",
			expectSubArgs:    []string{"workflow.yml"},
			expectDagMermaid: true,
		},
		{
			name:             "dag with --mermaid flag after file",
			args:             []string{"dag", "workflow.yml", "--mermaid"},
			expectSubCommand: "dag",
			expectSubArgs:    []string{"workflow.yml"},
			expectDagMermaid: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := newBufferCmd()
			err := c.parseArgs(tt.args)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if c.SubCommand != tt.expectSubCommand {
				t.Errorf("SubCommand = %q, want %q", c.SubCommand, tt.expectSubCommand)
			}
			if len(c.SubCommandArgs) != len(tt.expectSubArgs) {
				t.Errorf("SubCommandArgs = %v, want %v", c.SubCommandArgs, tt.expectSubArgs)
			} else {
				for i, arg := range tt.expectSubArgs {
					if c.SubCommandArgs[i] != arg {
						t.Errorf("SubCommandArgs[%d] = %q, want %q", i, c.SubCommandArgs[i], arg)
					}
				}
			}
			if c.DagMermaid != tt.expectDagMermaid {
				t.Errorf("DagMermaid = %v, want %v", c.DagMermaid, tt.expectDagMermaid)
			}
			if c.WorkflowPath != "" {
				t.Errorf("WorkflowPath = %q, want empty", c.WorkflowPath)
			}
		})
	}
}

func TestCmd_dag(t *testing.T) {
	tests := []struct {
		name       string
		args       []string
		expectCode int
		errContain string
	}{
		{
			name:       "dag without file argument",
			args:       []string{"probe", "dag"},
			expectCode: probe.ExitConfigError,
			errContain: "workflow file is required",
		},
		{
			name:       "mermaid flag without dag subcommand",
			args:       []string{"probe", "--mermaid", "test.yml"},
			expectCode: probe.ExitConfigError,
			errContain: "--mermaid can only be used with the dag subcommand",
		},
		{
			name:       "mermaid flag without dag subcommand and without file",
			args:       []string{"probe", "--mermaid"},
			expectCode: probe.ExitConfigError,
			errContain: "--mermaid can only be used with the dag subcommand",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := newBufferCmd()
			code := c.start(tt.args)
			if code != tt.expectCode {
				t.Errorf("start(%v) = %d, want %d", tt.args, code, tt.expectCode)
			}
			if tt.errContain != "" {
				errOutput := fmt.Sprintf("%s", c.errWriter)
				if !strings.Contains(errOutput, tt.errContain) {
					t.Errorf("error output should contain %q, got: %s", tt.errContain, errOutput)
				}
			}
		})
	}
}

// TestCmd_parseArgs_workflowFiles keeps the files given apart: they are
// merged in their order as those given with commas are.
func TestCmd_parseArgs_workflowFiles(t *testing.T) {
	tests := []struct {
		name   string
		args   []string
		expect string
	}{
		{"one file", []string{"a.yml"}, "a.yml"},
		{"files with commas", []string{"a.yml,b.yml"}, "a.yml,b.yml"},
		{"files apart", []string{"a.yml", "b.yml"}, "a.yml,b.yml"},
		{"files apart and with commas", []string{"a.yml,b.yml", "c.yml", "d.yml,e.yml"}, "a.yml,b.yml,c.yml,d.yml,e.yml"},
		{"flags between the files", []string{"a.yml", "-v", "b.yml", "--var", "x=y", "c.yml"}, "a.yml,b.yml,c.yml"},
		{"files after --", []string{"-v", "--", "-a.yml", "--b.yml"}, "-a.yml,--b.yml"},
		{"a subcommand's name after --", []string{"--", "check", "a.yml"}, "check,a.yml"},
		{"a file before --", []string{"a.yml", "--", "check"}, "a.yml,check"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := newBufferCmd()
			if err := c.parseArgs(tt.args); err != nil {
				t.Fatalf("parseArgs(%v) unexpected error: %v", tt.args, err)
			}
			if c.WorkflowPath != tt.expect {
				t.Errorf("WorkflowPath = %q, want %q", c.WorkflowPath, tt.expect)
			}
			if c.SubCommand != "" {
				t.Errorf("SubCommand = %q, want empty", c.SubCommand)
			}
		})
	}
}

// TestCmd_dagOfFilesGivenApart reads each of the files given apart, which
// a run of them reads in the same way.
func TestCmd_dagOfFilesGivenApart(t *testing.T) {
	dir := t.TempDir()
	write := func(name, content string) string {
		path := filepath.Join(dir, name)
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
		return path
	}
	head := write("head.yml", "name: split\n")
	jobs := write("jobs.yml", "jobs:\n- name: First job\n  steps:\n  - uses: hello\n- name: Second job\n  needs: [job-0]\n  steps:\n  - uses: hello\n")
	more := write("more.yml", "vars:\n  who: x\n")

	tests := []struct {
		name string
		args []string
	}{
		{"apart", []string{"probe", "dag", head, jobs, more}},
		{"apart and with commas", []string{"probe", "dag", head + "," + jobs, more}},
		{"mermaid between the files", []string{"probe", "dag", head, "--mermaid", jobs, more}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := newBufferCmd()
			c.mocking = false
			if code := c.start(tt.args); code != 0 {
				t.Fatalf("start(%v) = %d, want 0; stderr: %s", tt.args, code, c.errWriter)
			}
			out := fmt.Sprintf("%s", c.outWriter)
			for _, want := range []string{"First job", "Second job"} {
				if !strings.Contains(out, want) {
					t.Errorf("output should contain %q, got: %s", want, out)
				}
			}
			if mermaid := strings.HasPrefix(out, "flowchart"); mermaid != c.DagMermaid {
				t.Errorf("output in Mermaid = %v, want %v: %s", mermaid, c.DagMermaid, out)
			}
			if errOut := fmt.Sprintf("%s", c.errWriter); errOut != "" {
				t.Errorf("error output should be empty, got: %s", errOut)
			}
		})
	}

	t.Run("files that both have jobs", func(t *testing.T) {
		other := write("other.yml", "jobs:\n- name: Other job\n  steps:\n  - uses: hello\n")
		c := newBufferCmd()
		c.mocking = false
		if code := c.start([]string{"probe", "dag", head, jobs, other}); code != 0 {
			t.Fatalf("start() = %d, want 0; stderr: %s", code, c.errWriter)
		}
		want := "[WARN] " + other + ":1: jobs replaces the jobs of " + jobs + ":1, which do not run"
		if errOut := fmt.Sprintf("%s", c.errWriter); !strings.Contains(errOut, want) {
			t.Errorf("error output should contain %q, got: %s", want, errOut)
		}
		if out := fmt.Sprintf("%s", c.outWriter); strings.Contains(out, "First job") || !strings.Contains(out, "Other job") {
			t.Errorf("output should have the jobs of other.yml alone, got: %s", out)
		}
	})
}

// TestCmd_runProbe_filesGivenApart runs a workflow split into files given
// apart, each of which has a part of what the run reads.
func TestCmd_runProbe_filesGivenApart(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	write := func(name, content string) {
		if err := os.WriteFile(name, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	// The steps are skipped, so that no action is started.
	write("head.yml", "name: Split\n")
	write("vars.yml", "vars:\n  who: merged\n")
	write("jobs.yml", "jobs:\n- name: \"Job of {{vars.who}}\"\n  steps:\n  - uses: hello\n    skipif: \"true\"\n")
	write("other.yml", "jobs:\n- name: Other job\n  steps:\n  - uses: hello\n    skipif: \"true\"\n")

	tests := []struct {
		name     string
		args     []string
		contains []string
		absent   string
		warn     string
	}{
		{
			name:     "each file has a part",
			args:     []string{"head.yml", "vars.yml", "--report", "json=r.json", "jobs.yml"},
			contains: []string{`"name": "Split"`, `"name": "Job of merged"`},
		},
		{
			name:     "files that both have jobs",
			args:     []string{"head.yml,vars.yml", "jobs.yml", "other.yml", "--report", "json=r.json"},
			contains: []string{`"name": "Other job"`},
			absent:   "Job of merged",
			warn:     "[WARN] other.yml:1: jobs replaces the jobs of jobs.yml:1, which do not run",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := newBufferCmd()
			if err := c.parseArgs(tt.args); err != nil {
				t.Fatal(err)
			}
			if code := c.runProbe(); code != 0 {
				t.Fatalf("runProbe() = %d, want 0; stderr: %s", code, c.errWriter)
			}
			data, err := os.ReadFile("r.json")
			if err != nil {
				t.Fatal(err)
			}
			for _, want := range tt.contains {
				if !strings.Contains(string(data), want) {
					t.Errorf("report should contain %q, got: %s", want, data)
				}
			}
			if tt.absent != "" && strings.Contains(string(data), tt.absent) {
				t.Errorf("report should not contain %q, got: %s", tt.absent, data)
			}
			errOut := fmt.Sprintf("%s", c.errWriter)
			if tt.warn == "" && errOut != "" {
				t.Errorf("error output should be empty, got: %s", errOut)
			}
			if !strings.Contains(errOut, tt.warn) {
				t.Errorf("error output should contain %q, got: %s", tt.warn, errOut)
			}
		})
	}
}

func TestCmd_parseArgs_subcommandBeforeEndOfOptions(t *testing.T) {
	c := newBufferCmd()
	if err := c.parseArgs([]string{"check", "--", "-w.yml"}); err != nil {
		t.Fatalf("parseArgs() unexpected error: %v", err)
	}
	if c.SubCommand != "check" || len(c.SubCommandArgs) != 1 || c.SubCommandArgs[0] != "-w.yml" {
		t.Errorf("SubCommand = %q, SubCommandArgs = %v, want check [-w.yml]", c.SubCommand, c.SubCommandArgs)
	}
}

func TestCmd_parseArgs_gen(t *testing.T) {
	c := newBufferCmd()
	err := c.parseArgs([]string{"gen", "openapi.yml"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if c.SubCommand != "gen" {
		t.Errorf("SubCommand = %q, want %q", c.SubCommand, "gen")
	}
	if len(c.SubCommandArgs) != 1 || c.SubCommandArgs[0] != "openapi.yml" {
		t.Errorf("SubCommandArgs = %v, want [openapi.yml]", c.SubCommandArgs)
	}
	if c.WorkflowPath != "" {
		t.Errorf("WorkflowPath = %q, want empty", c.WorkflowPath)
	}
}

func TestCmd_InvalidFlags(t *testing.T) {
	args := []string{"probe", "--invalid-flag"}
	cmd := newBufferCmd()
	n := cmd.start(args)
	output := fmt.Sprintf("%s", cmd.errWriter)
	expected := strings.TrimLeft(`
[ERROR] unknown flag: --invalid-flag
try --help to know more
`, "\n")

	if n != probe.ExitConfigError {
		t.Errorf("Cmd.start(%v) should return %d for invalid flags", args, probe.ExitConfigError)
	}

	if expected != output {
		t.Errorf("Expected error message about unknown flag, got: %s", output)
	}
}

func TestCmd_runBuiltinActions(t *testing.T) {
	tests := []struct {
		name     string
		expected string
	}{
		{name: "hello", expected: ""},
		{name: "http", expected: ""},
		{name: "smtp", expected: ""},
		{name: "db", expected: ""},
		{name: "shell", expected: ""},
		{name: "embedded", expected: ""},
		{name: "unknown", expected: "[ERROR] not supported plugin: unknown\n"},
	}

	cmd := newBufferCmd()

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cmd.runBuiltinActions(tt.name)
			got := fmt.Sprintf("%s", cmd.errWriter)
			if got != tt.expected {
				t.Errorf("runBuiltinActions got: %s, expected: %s", got, tt.expected)
			}
		})
	}
}

func TestCmd_printVersion(t *testing.T) {
	c := newBufferCmd()
	c.ver = "1.0.0"
	c.rev = "abc123"
	c.printVersion()
	got := fmt.Sprintf("%s", c.outWriter)
	expected := "Probe Version 1.0.0 (commit: abc123)\n"

	if got != expected {
		t.Errorf("printVersion() output = %q, want %q", got, expected)
	}
}

func TestCmd_parseArgs_report(t *testing.T) {
	tests := []struct {
		name         string
		args         []string
		expectReport string
		expectPath   string
		expectErr    string
	}{
		{
			name:         "value after equals",
			args:         []string{"--report=junit=out/junit.xml", "workflow.yml"},
			expectReport: "junit=out/junit.xml",
			expectPath:   "workflow.yml",
		},
		{
			name:         "value as next argument",
			args:         []string{"workflow.yml", "--report", "json,markdown=summary.md"},
			expectReport: "json,markdown=summary.md",
			expectPath:   "workflow.yml",
		},
		{
			name:      "missing value",
			args:      []string{"workflow.yml", "--report"},
			expectErr: "flag needs an argument: --report",
		},
		{
			name:      "value that is a flag",
			args:      []string{"--report", "--verbose", "workflow.yml"},
			expectErr: "flag needs an argument: --report",
		},
		{
			name:      "unknown format",
			args:      []string{"--report=html", "workflow.yml"},
			expectErr: "unknown report format: html",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := newBufferCmd()
			err := c.parseArgs(tt.args)

			if tt.expectErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.expectErr) {
					t.Fatalf("parseArgs(%v) error = %v, want it to contain %q", tt.args, err, tt.expectErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("parseArgs(%v) unexpected error: %v", tt.args, err)
			}
			if c.Report != tt.expectReport {
				t.Errorf("Report = %q, want %q", c.Report, tt.expectReport)
			}
			if c.WorkflowPath != tt.expectPath {
				t.Errorf("WorkflowPath = %q, want %q", c.WorkflowPath, tt.expectPath)
			}
		})
	}
}

func TestCmd_runProbe_reportEnv(t *testing.T) {
	t.Setenv("PROBE_REPORT", "pdf")

	c := newBufferCmd()
	c.WorkflowPath = "workflow.yml"
	code := c.runProbe()

	if code != probe.ExitConfigError {
		t.Errorf("runProbe() = %d, want %d for an invalid PROBE_REPORT", code, probe.ExitConfigError)
	}
	errOutput := fmt.Sprintf("%s", c.errWriter)
	if !strings.Contains(errOutput, "unknown report format: pdf") {
		t.Errorf("error output should name the bad format, got: %s", errOutput)
	}
}

func TestCmd_parseArgs_baseline(t *testing.T) {
	tests := []struct {
		name           string
		args           []string
		expectBaseline string
		expectErr      string
	}{
		{
			name:           "value after equals",
			args:           []string{"--baseline=main.json", "workflow.yml"},
			expectBaseline: "main.json",
		},
		{
			name:           "value as next argument",
			args:           []string{"workflow.yml", "--baseline", "main.json"},
			expectBaseline: "main.json",
		},
		{
			name:      "missing value",
			args:      []string{"workflow.yml", "--baseline"},
			expectErr: "flag needs an argument: --baseline",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := newBufferCmd()
			err := c.parseArgs(tt.args)

			if tt.expectErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.expectErr) {
					t.Fatalf("parseArgs(%v) error = %v, want it to contain %q", tt.args, err, tt.expectErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("parseArgs(%v) unexpected error: %v", tt.args, err)
			}
			if c.Baseline != tt.expectBaseline {
				t.Errorf("Baseline = %q, want %q", c.Baseline, tt.expectBaseline)
			}
		})
	}
}

func TestCmd_runProbe_baseline(t *testing.T) {
	dir := t.TempDir()
	broken := filepath.Join(dir, "broken.json")
	if err := os.WriteFile(broken, []byte("{"), 0o644); err != nil {
		t.Fatal(err)
	}
	empty := filepath.Join(dir, "empty.json")
	if err := os.WriteFile(empty, []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name     string
		flag     string
		env      string
		expected string
	}{
		{"missing file from the flag", filepath.Join(dir, "none.json"), "", "--baseline: failed to read report"},
		{"broken file from the environment", "", broken, "--baseline: failed to parse report"},
		{"JSON that is no report", empty, "", "--baseline: " + empty + " is not a report of a run"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("PROBE_BASELINE", tt.env)
			c := newBufferCmd()
			c.WorkflowPath = "workflow.yml"
			c.Baseline = tt.flag

			if code := c.runProbe(); code != probe.ExitConfigError {
				t.Errorf("runProbe() = %d, want %d", code, probe.ExitConfigError)
			}
			if errOutput := fmt.Sprintf("%s", c.errWriter); !strings.Contains(errOutput, tt.expected) {
				t.Errorf("error output should contain %q, got: %s", tt.expected, errOutput)
			}
		})
	}
}

func TestCmd_mistypedSubCommand(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "gide"), []byte("jobs: []\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name       string
		args       []string
		expectCode int
		errOutput  string
	}{
		{"close to a subcommand", []string{"probe", "chek", "w.yml"}, probe.ExitConfigError, "[ERROR] unknown command \"chek\"; did you mean \"check\"?\ntry --help to know more\n"},
		{"in another case", []string{"probe", "Guide"}, probe.ExitConfigError, "[ERROR] unknown command \"Guide\"; did you mean \"guide\"?\ntry --help to know more\n"},
		{"after a flag", []string{"probe", "--verbose", "fmtt", "w.yml"}, probe.ExitConfigError, "[ERROR] unknown command \"fmtt\"; did you mean \"fmt\"?\ntry --help to know more\n"},
		{"a file of that name", []string{"probe", "gide"}, 0, ""},
		{"a path", []string{"probe", "./chek"}, 0, ""},
		{"a YAML file", []string{"probe", "check.yml"}, 0, ""},
		{"after --", []string{"probe", "--", "chek"}, 0, ""},
		{"a --job named as a subcommand", []string{"probe", "--job", "check", "w.yml"}, 0, ""},
		{"after a flag with a value", []string{"probe", "--report", "json", "chek"}, probe.ExitConfigError, "[ERROR] unknown command \"chek\"; did you mean \"check\"?\ntry --help to know more\n"},
		{"close to no subcommand", []string{"probe", "workflow"}, 0, ""},
	}

	t.Chdir(dir)
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := newBufferCmd()
			if code := c.start(tt.args); code != tt.expectCode {
				t.Errorf("start(%v) = %d, want %d", tt.args, code, tt.expectCode)
			}
			if errOut := fmt.Sprintf("%s", c.errWriter); errOut != tt.errOutput {
				t.Errorf("error output = %q, want %q", errOut, tt.errOutput)
			}
		})
	}
}

func TestCmd_guide(t *testing.T) {
	tests := []struct {
		name       string
		args       []string
		expectCode int
		contains   string
		errContain string
	}{
		{"list topics", []string{"probe", "guide"}, 0, "Usage: probe guide <topic>", ""},
		{"print a page", []string{"probe", "guide", "yaml"}, 0, "# YAML Configuration Reference", ""},
		{"print an action page", []string{"probe", "guide", "actions/shell"}, 0, "# Shell Action", ""},
		{"unknown topic", []string{"probe", "guide", "nope"}, probe.ExitConfigError, "", "unknown guide topic: nope"},
		{"mistyped topic", []string{"probe", "guide", "htpp"}, probe.ExitConfigError, "", `unknown guide topic: htpp; did you mean "http"?`},
		{"two topics", []string{"probe", "guide", "yaml", "cli"}, probe.ExitConfigError, "", "guide takes one topic, not 2"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := newBufferCmd()
			if code := c.start(tt.args); code != tt.expectCode {
				t.Errorf("start(%v) = %d, want %d", tt.args, code, tt.expectCode)
			}
			if out := fmt.Sprintf("%s", c.outWriter); tt.contains != "" && !strings.Contains(out, tt.contains) {
				t.Errorf("output should contain %q, got: %.200s", tt.contains, out)
			}
			if errOut := fmt.Sprintf("%s", c.errWriter); tt.errContain != "" && !strings.Contains(errOut, tt.errContain) {
				t.Errorf("error output should contain %q, got: %s", tt.errContain, errOut)
			}
		})
	}
}

// TestGuide_EveryActionHasAPage keeps the built-in actions and the guide in
// step: an action added without a reference page fails here.
func TestGuide_EveryActionHasAPage(t *testing.T) {
	for _, name := range actions.Names() {
		if _, err := probe.Guide(name); err != nil {
			t.Errorf("built-in action %q has no guide page: %v", name, err)
		}
	}
}

func TestCmd_skill(t *testing.T) {
	dir := t.TempDir()

	tests := []struct {
		name       string
		args       []string
		expectCode int
		contains   string
		errContain string
	}{
		{"print", []string{"probe", "skill"}, 0, "name: probe", ""},
		{"install into a directory", []string{"probe", "skill", "install", dir}, 0, "Wrote " + dir, ""},
		{"unknown argument", []string{"probe", "skill", "bogus"}, probe.ExitConfigError, "", "unknown skill arguments: bogus"},
		{"mistyped install", []string{"probe", "skill", "instal", dir}, probe.ExitConfigError, "", `unknown skill argument "instal"; did you mean "install"?`},
		{"too many arguments", []string{"probe", "skill", "install", "a", "b"}, probe.ExitConfigError, "", "Usage: probe skill"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := newBufferCmd()
			if code := c.start(tt.args); code != tt.expectCode {
				t.Errorf("start(%v) = %d, want %d", tt.args, code, tt.expectCode)
			}
			if out := fmt.Sprintf("%s", c.outWriter); tt.contains != "" && !strings.Contains(out, tt.contains) {
				t.Errorf("output should contain %q, got: %.200s", tt.contains, out)
			}
			if errOut := fmt.Sprintf("%s", c.errWriter); tt.errContain != "" && !strings.Contains(errOut, tt.errContain) {
				t.Errorf("error output should contain %q, got: %s", tt.errContain, errOut)
			}
		})
	}

	if _, err := os.Stat(filepath.Join(dir, "SKILL.md")); err != nil {
		t.Errorf("install should have written SKILL.md: %v", err)
	}
}

func TestCmd_manifest(t *testing.T) {
	dir := t.TempDir()
	sum := strings.Repeat("a", 64)
	write := func(name, content string) string {
		t.Helper()
		path := filepath.Join(dir, name)
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
		return path
	}
	action := write("action.yml", "name: greet\nguard: [read-only]\nruns:\n  using: binary\n  url: https://github.com/example/probe-greet/releases/download/v0.1.0/probe-greet_{os}_{arch}\n")
	local := write("local.yml", "runs:\n  using: binary\n  path: probe-greet\n")
	checksums := write("checksums.txt", sum+"  probe-greet_linux_amd64\n")
	released := "name: greet\nguard: [read-only]\nruns:\n  using: binary\n  url: https://github.com/example/probe-greet/releases/download/v0.2.0/probe-greet_{os}_{arch}\n  checksums:\n    linux_amd64: \"" + sum + "\"\n"

	tests := []struct {
		name       string
		args       []string
		expectCode int
		expectOut  string
		errContain string
	}{
		{"action file given", []string{"probe", "manifest", "v0.2.0", checksums, action}, 0, released, ""},
		{"action.yml of the working directory", []string{"probe", "manifest", "v0.2.0", checksums}, 0, released, ""},
		{"no checksums file", []string{"probe", "manifest", "v0.2.0"}, probe.ExitConfigError, "", "       probe manifest <tag>"},
		{"init without a repository", []string{"probe", "manifest", "init"}, probe.ExitConfigError, "", "Usage: probe manifest init"},
		{"mistyped init", []string{"probe", "manifest", "ini", "example/probe-greet"}, probe.ExitConfigError, "", `unknown manifest argument "ini"; did you mean "init"?`},
		{"tag close to init", []string{"probe", "manifest", "lint", checksums, action}, 0, strings.Replace(released, "v0.2.0", "lint", 1), ""},
		{"tag in capitals", []string{"probe", "manifest", "INIT", checksums, action}, 0, strings.Replace(released, "v0.2.0", "INIT", 1), ""},
		{"init with too many arguments", []string{"probe", "manifest", "init", "example/probe-greet", "more"}, probe.ExitConfigError, "", "Usage: probe manifest init"},
		{"init with a bad repository", []string{"probe", "manifest", "init", "probe-greet"}, probe.ExitConfigError, "", "must be <owner>/<repo>"},
		{"too many arguments", []string{"probe", "manifest", "v0.2.0", checksums, action, "more"}, probe.ExitConfigError, "", "Usage: probe manifest"},
		{"missing checksums file", []string{"probe", "manifest", "v0.2.0", filepath.Join(dir, "none.txt"), action}, probe.ExitConfigError, "", "none.txt"},
		{"missing action file", []string{"probe", "manifest", "v0.2.0", checksums, filepath.Join(dir, "none.yml")}, probe.ExitConfigError, "", "none.yml"},
		{"action that cannot be released", []string{"probe", "manifest", "v0.2.0", checksums, local}, probe.ExitConfigError, "", "runs.url is needed"},
	}

	t.Chdir(dir)
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := newBufferCmd()
			if code := c.start(tt.args); code != tt.expectCode {
				t.Errorf("start(%v) = %d, want %d", tt.args, code, tt.expectCode)
			}
			if out := fmt.Sprintf("%s", c.outWriter); out != tt.expectOut {
				t.Errorf("output = %q, want %q", out, tt.expectOut)
			}
			if errOut := fmt.Sprintf("%s", c.errWriter); !strings.Contains(errOut, tt.errContain) {
				t.Errorf("error output should contain %q, got: %s", tt.errContain, errOut)
			}
		})
	}
}

func TestCmd_manifestInit(t *testing.T) {
	c := newBufferCmd()
	if code := c.start([]string{"probe", "manifest", "init", "example/probe-greet"}); code != 0 {
		t.Fatalf("start() = %d, want 0: %s", code, c.errWriter)
	}
	out := fmt.Sprintf("%s", c.outWriter)
	for _, want := range []string{
		"name: greet\n",
		"# guard: [read-only, allow-host]\n",
		"  url: https://github.com/example/probe-greet/releases/download/v0.0.0/probe-greet_{os}_{arch}\n",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("output should contain %q, got:\n%s", want, out)
		}
	}
}

func TestCmd_guard(t *testing.T) {
	tests := []struct {
		name    string
		args    []string
		env     map[string]string
		want    actionrpc.Guard
		wantErr string
	}{
		{name: "no guard", args: []string{"w.yml"}},
		{
			name: "flags",
			args: []string{"--read-only", "--allow-host", "api.example.com, *.internal", "--allow-action=shell", "w.yml"},
			want: actionrpc.Guard{ReadOnly: true, AllowHosts: []string{"api.example.com", "*.internal"}, AllowActions: []string{"shell"}},
		},
		{
			name: "environment variables",
			args: []string{"w.yml"},
			env:  map[string]string{"PROBE_READ_ONLY": "true", "PROBE_ALLOW_HOSTS": "localhost:8080", "PROBE_ALLOW_ACTIONS": "shell,ssh"},
			want: actionrpc.Guard{ReadOnly: true, AllowHosts: []string{"localhost:8080"}, AllowActions: []string{"shell", "ssh"}},
		},
		{
			name: "flags win over environment variables",
			args: []string{"--allow-host", "a.example.com", "w.yml"},
			env:  map[string]string{"PROBE_ALLOW_HOSTS": "b.example.com", "PROBE_READ_ONLY": "0"},
			want: actionrpc.Guard{AllowHosts: []string{"a.example.com"}},
		},
		{name: "PROBE_READ_ONLY that is not a boolean", args: []string{"w.yml"}, env: map[string]string{"PROBE_READ_ONLY": "yes"}, wantErr: "PROBE_READ_ONLY must be true or false"},
		{
			name: "flags given false or empty win over environment variables",
			args: []string{"--read-only=false", "--allow-host=", "--allow-action=", "w.yml"},
			env:  map[string]string{"PROBE_READ_ONLY": "true", "PROBE_ALLOW_HOSTS": "a.example.com", "PROBE_ALLOW_ACTIONS": "shell"},
			want: actionrpc.Guard{},
		},
		{
			name: "--read-only=true",
			args: []string{"--read-only=true", "--allow-action=", "w.yml"},
			env:  map[string]string{"PROBE_ALLOW_ACTIONS": "shell"},
			want: actionrpc.Guard{ReadOnly: true},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			for _, k := range []string{"PROBE_READ_ONLY", "PROBE_ALLOW_HOSTS", "PROBE_ALLOW_ACTIONS"} {
				t.Setenv(k, tt.env[k])
			}
			c := newBufferCmd()
			if err := c.parseArgs(tt.args); err != nil {
				t.Fatal(err)
			}
			got, err := c.guard()
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("err = %v, want %q", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			// The kinds of guard the built-in actions keep to are what they
			// declare.
			if !reflect.DeepEqual(got.Keeps, actions.Keeps()) {
				t.Errorf("Keeps = %v, want %v", got.Keeps, actions.Keeps())
			}
			got.Keeps = nil
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("guard = %+v, want %+v", got, tt.want)
			}
		})
	}
}

func TestCmd_readOnlyTakesABoolean(t *testing.T) {
	c := newBufferCmd()
	if err := c.parseArgs([]string{"--read-only=maybe", "w.yml"}); err == nil || !strings.Contains(err.Error(), "must be true or false") {
		t.Errorf("parseArgs(--read-only=maybe) = %v, want an error", err)
	}
}

func TestCmd_guardFlagsNeedAnArgument(t *testing.T) {
	for _, flag := range []string{"--allow-host", "--allow-action"} {
		c := newBufferCmd()
		if err := c.parseArgs([]string{"w.yml", flag}); err == nil || !strings.Contains(err.Error(), "flag needs an argument") {
			t.Errorf("parseArgs(%s) = %v, want an error", flag, err)
		}
	}
}

func TestCmd_parseArgs_var(t *testing.T) {
	c := newBufferCmd()
	err := c.parseArgs([]string{"--var", "url=http://localhost:8080", "w.yml", "--var=token=a=b", "--var", "empty=", "--var", "url=http://other"})
	if err != nil {
		t.Fatal(err)
	}
	if c.WorkflowPath != "w.yml" {
		t.Errorf("WorkflowPath = %q, want w.yml", c.WorkflowPath)
	}
	want := map[string]string{"url": "http://other", "token": "a=b", "empty": ""}
	if got := c.vars(); !reflect.DeepEqual(got, want) {
		t.Errorf("vars() = %#v, want %#v", got, want)
	}
	if got := newBufferCmd().vars(); got != nil {
		t.Errorf("vars() = %#v, want nil when no --var is given", got)
	}

	for _, args := range [][]string{
		{"--var", "novalue", "w.yml"},
		{"--var", "=value", "w.yml"},
		{"--var= =value", "w.yml"},
		{"w.yml", "--var"},
		{"--var", "--verbose", "w.yml"},
	} {
		if err := newBufferCmd().parseArgs(args); err == nil {
			t.Errorf("parseArgs(%q) = nil, want an error", args)
		}
	}
}

func TestCmd_parseArgs_job(t *testing.T) {
	c := newBufferCmd()
	if err := c.parseArgs([]string{"--job", "tests, Lint", "w.yml", "--job=setup"}); err != nil {
		t.Fatal(err)
	}
	if want := []string{"tests", "Lint", "setup"}; !reflect.DeepEqual(c.Jobs, want) {
		t.Errorf("Jobs = %q, want %q", c.Jobs, want)
	}
	for _, args := range [][]string{
		{"w.yml", "--job"},
		{"--job=", "w.yml"},
		{"--job", " , ", "w.yml"},
	} {
		if err := newBufferCmd().parseArgs(args); err == nil {
			t.Errorf("parseArgs(%q) = nil, want --job to need a job", args)
		}
	}
}

// --job narrows a run, or the graph of dag, to some jobs; it narrows
// nothing else, and says so.
func TestCmd_job(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	// The steps are skipped, so that no action is started.
	wf := "name: Jobs\njobs:\n" +
		"- id: setup\n  name: Setup\n  steps:\n  - uses: hello\n    skipif: \"true\"\n" +
		"- name: Tests\n  needs: [setup]\n  steps:\n  - uses: hello\n    skipif: \"true\"\n" +
		"- name: Other job\n  steps:\n  - uses: hello\n    skipif: \"true\"\n"
	if err := os.WriteFile("w.yml", []byte(wf), 0o600); err != nil {
		t.Fatal(err)
	}

	t.Run("run", func(t *testing.T) {
		c := newBufferCmd()
		if err := c.parseArgs([]string{"--job", "Tests", "--report", "json=r.json", "w.yml"}); err != nil {
			t.Fatal(err)
		}
		if code := c.runProbe(); code != 0 {
			t.Fatalf("runProbe() = %d, want 0; stderr: %s", code, c.errWriter)
		}
		data, err := os.ReadFile("r.json")
		if err != nil {
			t.Fatal(err)
		}
		for _, want := range []string{`"id": "setup"`, `"id": "job_1"`, `"total": 2`} {
			if !strings.Contains(string(data), want) {
				t.Errorf("report should contain %q, got: %s", want, data)
			}
		}
		if strings.Contains(string(data), "Other job") {
			t.Errorf("report should not contain the job not selected, got: %s", data)
		}
	})

	// What a run of every job is refused for, in a job a run of some
	// leaves out, refuses the run of some as well; dag refuses a cycle,
	// which it has no order to draw in.
	t.Run("refused in a job left out", func(t *testing.T) {
		bad := map[string]string{
			"missing.yml": "name: Bad\njobs:\n- id: a\n  name: A\n  steps:\n  - uses: hello\n- id: d\n  name: D\n  needs: [nosuch]\n  steps:\n  - uses: hello\n",
			"cycle.yml":   "name: Bad\njobs:\n- id: a\n  name: A\n  steps:\n  - uses: hello\n- id: b\n  name: B\n  needs: [c]\n  steps:\n  - uses: hello\n- id: c\n  name: C\n  needs: [b]\n  steps:\n  - uses: hello\n",
			"uses.yml":    "name: Bad\njobs:\n- id: a\n  name: A\n  steps:\n  - uses: hello\n- id: b\n  name: B\n  steps:\n  - uses: helo\n",
		}
		want := map[string]string{
			"missing.yml": "job 'd' depends on non-existent job 'nosuch'",
			"cycle.yml":   "circular dependency detected",
			"uses.yml":    `unknown action "helo"`,
		}
		for name, content := range bad {
			if err := os.WriteFile(name, []byte(content), 0o600); err != nil {
				t.Fatal(err)
			}
			c := newBufferCmd()
			if err := c.parseArgs([]string{"--job", "a", name}); err != nil {
				t.Fatal(err)
			}
			if code := c.runProbe(); code != probe.ExitConfigError {
				t.Errorf("%s: runProbe() = %d, want %d", name, code, probe.ExitConfigError)
			}
			if errOut := fmt.Sprintf("%s", c.errWriter); !strings.Contains(errOut, want[name]) {
				t.Errorf("%s: error output should contain %q, got: %s", name, want[name], errOut)
			}
		}
		for _, args := range [][]string{{"probe", "dag", "cycle.yml"}, {"probe", "dag", "--job", "a", "cycle.yml"}} {
			c := newBufferCmd()
			c.mocking = false
			if code := c.start(args); code != probe.ExitConfigError {
				t.Errorf("start(%q) = %d, want %d", args, code, probe.ExitConfigError)
			}
			if errOut := fmt.Sprintf("%s", c.errWriter); !strings.Contains(errOut, "circular dependency detected: ") {
				t.Errorf("start(%q): error output should tell the cycle, got: %s", args, errOut)
			}
		}
	})

	t.Run("coverage of a run of some jobs", func(t *testing.T) {
		spec := "openapi: 3.0.0\ninfo: {title: t, version: \"1\"}\npaths: {}\n"
		rep := `{"name": "Jobs", "jobs": [{"id": "setup", "name": "Setup", "steps": [{"name": "s", "status": "passed", "contract": {"spec": "openapi.yml"}}]}], "selection": {"jobs": ["setup"], "total": 3}}`
		if err := os.WriteFile("openapi.yml", []byte(spec), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile("partial.json", []byte(rep), 0o600); err != nil {
			t.Fatal(err)
		}
		c := newBufferCmd()
		c.start([]string{"probe", "coverage", "openapi.yml", "partial.json"})
		want := "[WARN] 1 of the 3 jobs of the workflow ran, selected with --job: setup; the steps of the others are not counted"
		if errOut := fmt.Sprintf("%s", c.errWriter); !strings.Contains(errOut, want) {
			t.Errorf("error output should contain %q, got: %s", want, errOut)
		}
	})

	t.Run("unknown job", func(t *testing.T) {
		c := newBufferCmd()
		if err := c.parseArgs([]string{"--job", "Test", "w.yml"}); err != nil {
			t.Fatal(err)
		}
		if code := c.runProbe(); code != probe.ExitConfigError {
			t.Errorf("runProbe() = %d, want %d", code, probe.ExitConfigError)
		}
		want := `[ERROR] no job has the id or name "Test"; did you mean "Tests"?`
		if errOut := fmt.Sprintf("%s", c.errWriter); !strings.Contains(errOut, want) {
			t.Errorf("error output should contain %q, got: %s", want, errOut)
		}
	})

	t.Run("dag", func(t *testing.T) {
		c := newBufferCmd()
		c.mocking = false
		if code := c.start([]string{"probe", "dag", "--job", "setup", "w.yml"}); code != 0 {
			t.Fatalf("start() = %d, want 0; stderr: %s", code, c.errWriter)
		}
		out := fmt.Sprintf("%s", c.outWriter)
		if !strings.Contains(out, "Setup") || strings.Contains(out, "Tests") || strings.Contains(out, "Other job") {
			t.Errorf("output should have the job selected alone, got: %s", out)
		}
	})

	for _, sub := range []string{"check", "fmt"} {
		t.Run(sub, func(t *testing.T) {
			c := newBufferCmd()
			if code := c.start([]string{"probe", sub, "--job", "setup", "w.yml"}); code != probe.ExitConfigError {
				t.Errorf("start() = %d, want %d", code, probe.ExitConfigError)
			}
			errOut := fmt.Sprintf("%s", c.errWriter)
			for _, want := range []string{"--job can only be used to run a workflow or with the dag subcommand", "Usage: probe --job <job> <workflow-file>..."} {
				if !strings.Contains(errOut, want) {
					t.Errorf("error output should contain %q, got: %s", want, errOut)
				}
			}
		})
	}
}

func TestCmd_parseArgs_envFile(t *testing.T) {
	c := newBufferCmd()
	if err := c.parseArgs([]string{"--env-file", "a.env, b.env", "w.yml", "--env-file=c.env"}); err != nil {
		t.Fatal(err)
	}
	if want := []string{"a.env", "b.env", "c.env"}; !reflect.DeepEqual(c.EnvFiles, want) {
		t.Errorf("EnvFiles = %q, want %q", c.EnvFiles, want)
	}
	for _, args := range [][]string{
		{"w.yml", "--env-file"},
		{"--env-file=", "w.yml"},
		{"--env-file", " , ", "w.yml"},
	} {
		if err := newBufferCmd().parseArgs(args); err == nil {
			t.Errorf("parseArgs(%q) = nil, want --env-file to need a file", args)
		}
	}
}

func TestLoadEnvFiles(t *testing.T) {
	dir := t.TempDir()
	write := func(name, content string) string {
		t.Helper()
		path := filepath.Join(dir, name)
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
		return path
	}

	base := write("base.env", "ENVFILE_TEST_A=base\nENVFILE_TEST_B=base\nPROBE_ALLOW_ACTIONS=shell\n")
	local := write("local.env", "ENVFILE_TEST_B=local\n")
	env, err := loadEnvFiles([]string{base, local})
	if err != nil {
		t.Fatal(err)
	}
	// The file read last wins among the files.
	want := map[string]string{"ENVFILE_TEST_A": "base", "ENVFILE_TEST_B": "local", "PROBE_ALLOW_ACTIONS": "shell"}
	if !reflect.DeepEqual(env, want) {
		t.Errorf("loadEnvFiles() = %#v, want %#v", env, want)
	}
	// Nothing is set in the environment of the process, so that a setting
	// of Probe, or what an action runs with, is not what a file says.
	for name := range want {
		if _, set := os.LookupEnv(name); set {
			t.Errorf("%s was set in the environment", name)
		}
	}

	if env, err := loadEnvFiles(nil); err != nil || env != nil {
		t.Errorf("loadEnvFiles(nil) = %v, %v", env, err)
	}
	if _, err := loadEnvFiles([]string{filepath.Join(dir, "missing.env")}); err == nil {
		t.Error("loadEnvFiles() = nil, want an error for a file that does not exist")
	}
	bad := write("bad.env", "not a variable\n")
	if _, err := loadEnvFiles([]string{bad}); err == nil || !strings.Contains(err.Error(), bad+":1: ") {
		t.Errorf("loadEnvFiles() = %v, want the file and the line", err)
	}
}
