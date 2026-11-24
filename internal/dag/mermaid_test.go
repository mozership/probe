/*
Package dag provides DAG Mermaid rendering tests.

# Golden Test Cases

The golden tests cover comprehensive DAG patterns for Mermaid rendering:

	Category              | Case Name                | Structure
	----------------------|--------------------------|------------------------------------------
	Basic                 | single                   | Single job
	                      | linear_two               | A → B
	                      | linear_three             | A → B → C
	Divergence            | divergence_two           | A → [B, C]
	                      | divergence_three         | A → [B, C, D]
	Convergence           | convergence_two          | [A, B] → C
	                      | convergence_three        | [A, B, C] → D
	Complex               | diamond                  | A → [B, C] → D
	                      | hourglass                | [A, B] → C → [D, E]
	Parallel              | parallel_roots           | [A], [B] (independent)
	                      | parallel_chains          | [A→B], [C→D] (independent chains)
	Mixed                 | mixed_standalone         | A → B, C (standalone)
	Node IDs              | sanitized_id_collision   | "unit-test" → "unit.test", both "unit_test"
	                      | step_id_collision        | A → "A_step0", a job named like A's step

# Usage

Run golden tests:

	go test -run TestMermaid_Golden -v

Update golden files when output format changes:

	UPDATE_GOLDEN=1 go test -run TestMermaid_Golden

Golden files are stored in testdata/dag_mermaid/*.golden.txt
*/
package dag

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestMermaid_Render_EmptyWorkflow(t *testing.T) {
	g := Graph{
		Jobs: []Job{},
	}

	result := Mermaid{}.Render(g)

	if result != "" {
		t.Errorf("expected empty string for empty workflow, got %q", result)
	}
}

func TestMermaid_Render_SingleJob(t *testing.T) {
	g := Graph{
		Jobs: []Job{
			{
				Name: "Single Job",
				ID:   "single",
				Steps: []Step{
					{Name: "Step 1"},
				},
			},
		},
	}

	result := Mermaid{}.Render(g)

	// Check that output starts with flowchart header
	if !strings.HasPrefix(result, "flowchart LR") {
		t.Errorf("expected output to start with 'flowchart LR', got:\n%s", result)
	}

	// Check that the output contains the job name
	if !strings.Contains(result, "Single Job") {
		t.Errorf("expected output to contain 'Single Job', got:\n%s", result)
	}

	// Check that the output contains the step name
	if !strings.Contains(result, "Step 1") {
		t.Errorf("expected output to contain 'Step 1', got:\n%s", result)
	}
}

func TestMermaid_Render_TwoJobsWithDependency(t *testing.T) {
	g := Graph{
		Jobs: []Job{
			{
				Name: "First Job",
				ID:   "first",
				Steps: []Step{
					{Name: "First Step"},
				},
			},
			{
				Name:  "Second Job",
				ID:    "second",
				Needs: []string{"first"},
				Steps: []Step{
					{Name: "Second Step"},
				},
			},
		},
	}

	result := Mermaid{}.Render(g)

	// Check that the output contains both job names
	if !strings.Contains(result, "First Job") {
		t.Errorf("expected output to contain 'First Job', got:\n%s", result)
	}
	if !strings.Contains(result, "Second Job") {
		t.Errorf("expected output to contain 'Second Job', got:\n%s", result)
	}

	// Check for connection arrow (Mermaid uses -->)
	if !strings.Contains(result, "-->") {
		t.Errorf("expected output to contain '-->', got:\n%s", result)
	}
}

func TestMermaid_Render_ParallelJobs(t *testing.T) {
	g := Graph{
		Jobs: []Job{
			{
				Name: "Root Job",
				ID:   "root",
				Steps: []Step{
					{Name: "Root Step"},
				},
			},
			{
				Name:  "Branch A",
				ID:    "branch-a",
				Needs: []string{"root"},
				Steps: []Step{
					{Name: "Branch A Step"},
				},
			},
			{
				Name:  "Branch B",
				ID:    "branch-b",
				Needs: []string{"root"},
				Steps: []Step{
					{Name: "Branch B Step"},
				},
			},
		},
	}

	result := Mermaid{}.Render(g)

	// Check that the output contains all job names
	if !strings.Contains(result, "Root Job") {
		t.Errorf("expected output to contain 'Root Job', got:\n%s", result)
	}
	if !strings.Contains(result, "Branch A") {
		t.Errorf("expected output to contain 'Branch A', got:\n%s", result)
	}
	if !strings.Contains(result, "Branch B") {
		t.Errorf("expected output to contain 'Branch B', got:\n%s", result)
	}

	// Check for edges
	if !strings.Contains(result, "root --> branch_a") {
		t.Errorf("expected output to contain 'root --> branch_a', got:\n%s", result)
	}
	if !strings.Contains(result, "root --> branch_b") {
		t.Errorf("expected output to contain 'root --> branch_b', got:\n%s", result)
	}
}

func TestMermaid_SanitizeID(t *testing.T) {
	renderer := Mermaid{}

	tests := []struct {
		input    string
		expected string
	}{
		{"simple", "simple"},
		{"with-dash", "with_dash"},
		{"with space", "with_space"},
		{"123numeric", "n123numeric"},
		{"special@#$chars", "special___chars"},
		{"", "node"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result := renderer.sanitizeID(tt.input)
			if result != tt.expected {
				t.Errorf("sanitizeID(%q) = %q, want %q", tt.input, result, tt.expected)
			}
		})
	}
}

func TestMermaid_EscapeLabel(t *testing.T) {
	renderer := Mermaid{}

	tests := []struct {
		input    string
		expected string
	}{
		{"simple", "simple"},
		{`with "quotes"`, `with #quot;quotes#quot;`},
		{"no special", "no special"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result := renderer.escapeLabel(tt.input)
			if result != tt.expected {
				t.Errorf("escapeLabel(%q) = %q, want %q", tt.input, result, tt.expected)
			}
		})
	}
}

func TestMermaid_Render(t *testing.T) {
	g := Graph{
		Jobs: []Job{
			{
				Name: "Test Job",
				ID:   "test",
				Steps: []Step{
					{Name: "Test Step"},
				},
			},
		},
	}

	result := Mermaid{}.Render(g)

	if result == "" {
		t.Error("expected non-empty result")
	}

	if !strings.Contains(result, "Test Job") {
		t.Errorf("expected output to contain 'Test Job', got:\n%s", result)
	}
}

func TestMermaid_Render_Empty(t *testing.T) {
	g := Graph{
		Jobs: []Job{},
	}

	result := Mermaid{}.Render(g)

	if result != "" {
		t.Errorf("expected empty result for empty workflow, got:\n%s", result)
	}
}

func TestMermaid_EmbeddedAction(t *testing.T) {
	g := Graph{
		Jobs: []Job{
			{
				Name: "Test",
				ID:   "test",
				Steps: []Step{
					{Name: "Normal Step"},
					{Name: "Embedded Step", Embedded: true},
				},
			},
		},
	}

	result := Mermaid{}.Render(g)

	// Check that output contains both steps
	if !strings.Contains(result, "Normal Step") {
		t.Errorf("expected output to contain 'Normal Step', got:\n%s", result)
	}
	if !strings.Contains(result, "Embedded Step") {
		t.Errorf("expected output to contain 'Embedded Step', got:\n%s", result)
	}
}

// mermaidGoldenCase is a golden test case: a graph and the name of the file holding
// its expected rendering.
type mermaidGoldenCase struct {
	name  string // Test case name, used as golden file name (e.g., "diamond" -> "diamond.golden.txt")
	graph Graph
}

// getDagMermaidGoldenTestCases returns all golden test cases covering various DAG patterns.
func mermaidGoldenCases() []mermaidGoldenCase {
	return []mermaidGoldenCase{
		{
			name: "single",
			graph: Graph{Jobs: []Job{
				{ID: "build", Name: "Build", Steps: []Step{
					{Name: "Compile"},
				}},
			}},
		},
		{
			name: "embedded_action",
			graph: Graph{Jobs: []Job{
				{ID: "setup", Name: "Setup", Steps: []Step{
					{Name: "Checkout"},
					{Name: "Set env vars", Embedded: true},
					{Name: "Validate config", Embedded: true},
				}},
				{ID: "test", Name: "Test", Needs: []string{"setup"}, Steps: []Step{
					{Name: "Run tests"},
					{Name: "Upload coverage", Embedded: true},
				}},
			}},
		},
		{
			name: "embedded_expanded",
			graph: Graph{Jobs: []Job{
				{ID: "deploy", Name: "Deploy", Steps: []Step{
					{Name: "Run job", Embedded: true, EmbeddedFile: "embedded-success-job.yml", EmbeddedSteps: []string{"Simple success step", "Another success step"}},
				}},
			}},
		},
		{
			name: "linear_two",
			graph: Graph{Jobs: []Job{
				{ID: "build", Name: "Build", Steps: []Step{
					{Name: "Compile"},
				}},
				{ID: "test", Name: "Test", Needs: []string{"build"}, Steps: []Step{
					{Name: "Run tests"},
				}},
			}},
		},
		{
			name: "linear_three",
			graph: Graph{Jobs: []Job{
				{ID: "build", Name: "Build", Steps: []Step{
					{Name: "Compile"},
				}},
				{ID: "test", Name: "Test", Needs: []string{"build"}, Steps: []Step{
					{Name: "Run tests"},
				}},
				{ID: "deploy", Name: "Deploy", Needs: []string{"test"}, Steps: []Step{
					{Name: "Deploy app"},
				}},
			}},
		},
		{
			name: "divergence_two",
			graph: Graph{Jobs: []Job{
				{ID: "build", Name: "Build", Steps: []Step{
					{Name: "Compile"},
				}},
				{ID: "unit-test", Name: "Unit Test", Needs: []string{"build"}, Steps: []Step{
					{Name: "Run unit"},
				}},
				{ID: "lint", Name: "Lint", Needs: []string{"build"}, Steps: []Step{
					{Name: "Run lint"},
				}},
			}},
		},
		{
			name: "divergence_three",
			graph: Graph{Jobs: []Job{
				{ID: "build", Name: "Build", Steps: []Step{
					{Name: "Compile"},
				}},
				{ID: "unit-test", Name: "Unit Test", Needs: []string{"build"}, Steps: []Step{
					{Name: "Run unit"},
				}},
				{ID: "integration", Name: "Integration", Needs: []string{"build"}, Steps: []Step{
					{Name: "Run integ"},
				}},
				{ID: "lint", Name: "Lint", Needs: []string{"build"}, Steps: []Step{
					{Name: "Run lint"},
				}},
			}},
		},
		{
			name: "convergence_two",
			graph: Graph{Jobs: []Job{
				{ID: "build-linux", Name: "Build Linux", Steps: []Step{
					{Name: "Compile"},
				}},
				{ID: "build-mac", Name: "Build Mac", Steps: []Step{
					{Name: "Compile"},
				}},
				{ID: "release", Name: "Release", Needs: []string{"build-linux", "build-mac"}, Steps: []Step{
					{Name: "Upload"},
				}},
			}},
		},
		{
			name: "convergence_three",
			graph: Graph{Jobs: []Job{
				{ID: "build-linux", Name: "Build Linux", Steps: []Step{
					{Name: "Compile"},
				}},
				{ID: "build-mac", Name: "Build Mac", Steps: []Step{
					{Name: "Compile"},
				}},
				{ID: "build-win", Name: "Build Win", Steps: []Step{
					{Name: "Compile"},
				}},
				{ID: "release", Name: "Release", Needs: []string{"build-linux", "build-mac", "build-win"}, Steps: []Step{
					{Name: "Upload"},
				}},
			}},
		},
		{
			name: "diamond",
			graph: Graph{Jobs: []Job{
				{ID: "build", Name: "Build", Steps: []Step{
					{Name: "Compile"},
				}},
				{ID: "unit-test", Name: "Unit Test", Needs: []string{"build"}, Steps: []Step{
					{Name: "Run unit"},
				}},
				{ID: "lint", Name: "Lint", Needs: []string{"build"}, Steps: []Step{
					{Name: "Run lint"},
				}},
				{ID: "deploy", Name: "Deploy", Needs: []string{"unit-test", "lint"}, Steps: []Step{
					{Name: "Deploy app"},
				}},
			}},
		},
		{
			name: "hourglass",
			graph: Graph{Jobs: []Job{
				{ID: "build-linux", Name: "Build Linux", Steps: []Step{
					{Name: "Compile"},
				}},
				{ID: "build-mac", Name: "Build Mac", Steps: []Step{
					{Name: "Compile"},
				}},
				{ID: "integration", Name: "Integration", Needs: []string{"build-linux", "build-mac"}, Steps: []Step{
					{Name: "Test all"},
				}},
				{ID: "deploy-prod", Name: "Deploy Prod", Needs: []string{"integration"}, Steps: []Step{
					{Name: "Deploy"},
				}},
				{ID: "deploy-stage", Name: "Deploy Stage", Needs: []string{"integration"}, Steps: []Step{
					{Name: "Deploy"},
				}},
			}},
		},
		{
			name: "parallel_roots",
			graph: Graph{Jobs: []Job{
				{ID: "build-app", Name: "Build App", Steps: []Step{
					{Name: "Compile app"},
				}},
				{ID: "build-lib", Name: "Build Lib", Steps: []Step{
					{Name: "Compile lib"},
				}},
			}},
		},
		{
			name: "parallel_chains",
			graph: Graph{Jobs: []Job{
				{ID: "build-app", Name: "Build App", Steps: []Step{
					{Name: "Compile app"},
				}},
				{ID: "test-app", Name: "Test App", Needs: []string{"build-app"}, Steps: []Step{
					{Name: "Test app"},
				}},
				{ID: "build-lib", Name: "Build Lib", Steps: []Step{
					{Name: "Compile lib"},
				}},
				{ID: "test-lib", Name: "Test Lib", Needs: []string{"build-lib"}, Steps: []Step{
					{Name: "Test lib"},
				}},
			}},
		},
		{
			name: "mixed_standalone",
			graph: Graph{Jobs: []Job{
				{ID: "build", Name: "Build", Steps: []Step{
					{Name: "Compile"},
				}},
				{ID: "test", Name: "Test", Needs: []string{"build"}, Steps: []Step{
					{Name: "Run tests"},
				}},
				{ID: "docs", Name: "Docs", Steps: []Step{
					{Name: "Build docs"},
				}},
			}},
		},
		{
			// Job IDs that sanitize to the same node ID stay separate nodes
			name: "sanitized_id_collision",
			graph: Graph{Jobs: []Job{
				{ID: "unit-test", Name: "Unit (dash)", Steps: []Step{
					{Name: "Run"},
				}},
				{ID: "unit.test", Name: "Unit (dot)", Needs: []string{"unit-test"}, Steps: []Step{
					{Name: "Run"},
				}},
				{ID: "report", Name: "Report", Needs: []string{"unit.test"}},
			}},
		},
		{
			// A job whose ID is the node ID of another job's step stays apart
			// from that step
			name: "step_id_collision",
			graph: Graph{Jobs: []Job{
				{ID: "build", Name: "Build", Steps: []Step{
					{Name: "Compile"},
				}},
				{ID: "build_step0", Name: "Package", Needs: []string{"build"}, Steps: []Step{
					{Name: "Archive"},
				}},
			}},
		},
	}
}

func TestNodeIDs_Allocate(t *testing.T) {
	ids := nodeIDs{}
	for _, tt := range []struct{ base, want string }{
		{"unit_test", "unit_test"},
		{"unit_test", "unit_test_2"},
		{"unit_test_2", "unit_test_2_2"},
		{"unit_test", "unit_test_3"},
		{"build", "build"},
	} {
		if got := ids.allocate(tt.base); got != tt.want {
			t.Errorf("allocate(%q) = %q, want %q", tt.base, got, tt.want)
		}
	}
}

// TestMermaid_Golden compares the rendering of every golden case against the file in
// testdata/dag_mermaid.
//
// To update golden files when the output format intentionally changes:
//
//	UPDATE_GOLDEN=1 go test -run TestMermaid_Golden
func TestMermaid_Golden(t *testing.T) {
	for _, tc := range mermaidGoldenCases() {
		t.Run(tc.name, func(t *testing.T) {
			actual := Mermaid{}.Render(tc.graph)

			goldenPath := filepath.Join("testdata", "dag_mermaid", tc.name+".golden.txt")

			if os.Getenv("UPDATE_GOLDEN") == "1" {
				if err := os.MkdirAll(filepath.Dir(goldenPath), 0o755); err != nil {
					t.Fatalf("failed to create golden directory: %v", err)
				}
				if err := os.WriteFile(goldenPath, []byte(actual), 0o600); err != nil {
					t.Fatalf("failed to write golden file: %v", err)
				}
				return
			}

			expected, err := os.ReadFile(goldenPath)
			if err != nil {
				t.Fatalf("failed to read golden file %s: %v\nRun with UPDATE_GOLDEN=1 to create it", goldenPath, err)
			}

			if actual != string(expected) {
				t.Errorf("output does not match golden file %s\n\nExpected:\n%s\n\nActual:\n%s\n\nRun with UPDATE_GOLDEN=1 to update", goldenPath, string(expected), actual)
			}
		})
	}
}

// TestMermaid_LinksStepsInOrder pins the step edges inside a job.
func TestMermaid_LinksStepsInOrder(t *testing.T) {
	g := Graph{Jobs: []Job{{
		ID:   "build",
		Name: "Build",
		Steps: []Step{
			{Name: "Checkout"},
			{Name: "Run job", Embedded: true, EmbeddedFile: "job.yml", EmbeddedSteps: []string{"Compile"}},
			{Name: "Package"},
		},
	}}}

	out := Mermaid{}.Render(g)
	for _, want := range []string{
		"        direction TB\n",
		"        build_step0 --> build_step1\n",
		"        build_step1 --> build_step2\n",
		"        build_step2 --> build_step3\n",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("output should contain %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "build_step3 -->") {
		t.Errorf("the last step should not link onward:\n%s", out)
	}
}

// TestMermaid_UnknownNeed checks that a need naming no job of the graph,
// which the dag command does not reject, gets a node of its own: one that is
// not a job or step drawn above, shared by every job that needs it.
func TestMermaid_UnknownNeed(t *testing.T) {
	g := Graph{Jobs: []Job{
		{ID: "build", Name: "Build", Steps: []Step{{Name: "Compile"}}},
		{ID: "test", Name: "Test", Needs: []string{"build_step0", "missing-job"}},
		{ID: "lint", Name: "Lint", Needs: []string{"build_step0"}},
	}}

	out := Mermaid{}.Render(g)
	for _, want := range []string{
		"    build_step0_2 --> test\n",
		"    missing_job --> test\n",
		"    build_step0_2 --> lint\n",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("output should contain %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "    build_step0 --> ") {
		t.Errorf("an unknown need should not link from the step build_step0:\n%s", out)
	}
}
