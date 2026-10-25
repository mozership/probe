/*
Package dag provides DAG ASCII rendering tests.

# Golden Test Cases

The golden tests cover comprehensive DAG patterns for ASCII rendering:

	Category              | Case Name                | Structure
	----------------------|--------------------------|------------------------------------------
	Basic                 | single                   | Single job
	                      | embedded_action          | Jobs with embedded actions (↗ bullet)
	                      | embedded_expanded        | Embedded action with path expands steps
	                      | truncated_long_names     | Long names truncated with ellipsis (…)
	                      | wide_names               | Names of wide characters, padded by columns
	                      | linear_two               | A → B
	                      | linear_three             | A → B → C
	Divergence            | divergence_two           | A → [B, C]
	                      | divergence_three         | A → [B, C, D]
	                      | divergence_uneven_steps  | A → [B(3 steps), C] (uneven heights)
	Convergence           | convergence_two          | [A, B] → C
	                      | convergence_three        | [A, B, C] → D
	                      | convergence_uneven_steps | [A(3 steps), B] → C (uneven heights)
	                      | multi_child_convergence  | [A, B] → [C(both), D(A only)]
	Complex               | diamond                  | A → [B, C] → D
	                      | hourglass                | [A, B] → C → [D, E]
	Parallel              | parallel_roots           | [A], [B] (independent)
	                      | parallel_chains          | [A→B], [C→D] (independent chains)
	Mixed                 | mixed_standalone         | A → B, C (standalone)
	                      | sorted_children_first    | Jobs with children sorted before others
	                      | wide_divergence          | A → [B, C, D, E]

# Usage

Run golden tests:

	go test -run TestASCII_Golden -v

Update golden files when output format changes:

	UPDATE_GOLDEN=1 go test -run TestASCII_Golden

Golden files are stored in testdata/dag_ascii/*.golden.txt
*/
package dag

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mozership/probe/internal/textwidth"
)

func TestASCII_Render_EmptyWorkflow(t *testing.T) {
	g := Graph{
		Jobs: []Job{},
	}

	renderer := newASCIIRenderer(g)
	result := renderer.render()

	if result != "" {
		t.Errorf("expected empty string for empty workflow, got %q", result)
	}
}

func TestASCII_Render_SingleJob(t *testing.T) {
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

	renderer := newASCIIRenderer(g)
	result := renderer.render()

	// Check that the output contains the job name
	if !strings.Contains(result, "Single Job") {
		t.Errorf("expected output to contain 'Single Job', got:\n%s", result)
	}

	// Check that the output contains the step name
	if !strings.Contains(result, "Step 1") {
		t.Errorf("expected output to contain 'Step 1', got:\n%s", result)
	}

	// Check box characters
	if !strings.Contains(result, "╭") || !strings.Contains(result, "╯") {
		t.Errorf("expected output to contain box characters, got:\n%s", result)
	}
}

func TestASCII_Render_TwoJobsWithDependency(t *testing.T) {
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

	renderer := newASCIIRenderer(g)
	result := renderer.render()

	// Check that the output contains both job names
	if !strings.Contains(result, "First Job") {
		t.Errorf("expected output to contain 'First Job', got:\n%s", result)
	}
	if !strings.Contains(result, "Second Job") {
		t.Errorf("expected output to contain 'Second Job', got:\n%s", result)
	}

	// Check for connection arrow
	if !strings.Contains(result, "↓") {
		t.Errorf("expected output to contain arrow '↓', got:\n%s", result)
	}
}

func TestASCII_Render_ParallelJobs(t *testing.T) {
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

	renderer := newASCIIRenderer(g)
	result := renderer.render()

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
}

func TestASCII_Render_MultipleSteps(t *testing.T) {
	g := Graph{
		Jobs: []Job{
			{
				Name: "Multi Step Job",
				ID:   "multi",
				Steps: []Step{
					{Name: "Step One"},
					{Name: "Step Two"},
					{Name: "Step Three"},
				},
			},
		},
	}

	renderer := newASCIIRenderer(g)
	result := renderer.render()

	// Check all steps are present
	if !strings.Contains(result, "Step One") {
		t.Errorf("expected output to contain 'Step One', got:\n%s", result)
	}
	if !strings.Contains(result, "Step Two") {
		t.Errorf("expected output to contain 'Step Two', got:\n%s", result)
	}
	if !strings.Contains(result, "Step Three") {
		t.Errorf("expected output to contain 'Step Three', got:\n%s", result)
	}

	// Check step bullet points
	if strings.Count(result, "○") != 3 {
		t.Errorf("expected 3 step bullets, got %d", strings.Count(result, "○"))
	}
}

func TestASCII_CalculateLevels(t *testing.T) {
	g := Graph{
		Jobs: []Job{
			{Name: "Job A", ID: "a"},
			{Name: "Job B", ID: "b", Needs: []string{"a"}},
			{Name: "Job C", ID: "c", Needs: []string{"b"}},
		},
	}

	renderer := newASCIIRenderer(g)
	renderer.calculateLevels()

	if len(renderer.levels) != 3 {
		t.Errorf("expected 3 levels, got %d", len(renderer.levels))
	}

	// Level 0 should have Job A
	if len(renderer.levels[0]) != 1 {
		t.Errorf("expected 1 job at level 0, got %d", len(renderer.levels[0]))
	}

	// Level 1 should have Job B
	if len(renderer.levels[1]) != 1 {
		t.Errorf("expected 1 job at level 1, got %d", len(renderer.levels[1]))
	}

	// Level 2 should have Job C
	if len(renderer.levels[2]) != 1 {
		t.Errorf("expected 1 job at level 2, got %d", len(renderer.levels[2]))
	}
}

func TestASCII_CalculateLevels_Parallel(t *testing.T) {
	g := Graph{
		Jobs: []Job{
			{Name: "Root", ID: "root"},
			{Name: "Left", ID: "left", Needs: []string{"root"}},
			{Name: "Right", ID: "right", Needs: []string{"root"}},
			{Name: "Merge", ID: "merge", Needs: []string{"left", "right"}},
		},
	}

	renderer := newASCIIRenderer(g)
	renderer.calculateLevels()

	if len(renderer.levels) != 3 {
		t.Errorf("expected 3 levels, got %d", len(renderer.levels))
	}

	// Level 0 should have Root
	if len(renderer.levels[0]) != 1 {
		t.Errorf("expected 1 job at level 0, got %d", len(renderer.levels[0]))
	}

	// Level 1 should have Left and Right (parallel)
	if len(renderer.levels[1]) != 2 {
		t.Errorf("expected 2 jobs at level 1, got %d", len(renderer.levels[1]))
	}

	// Level 2 should have Merge
	if len(renderer.levels[2]) != 1 {
		t.Errorf("expected 1 job at level 2, got %d", len(renderer.levels[2]))
	}
}

func TestASCII_Render(t *testing.T) {
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

	result := ASCII{}.Render(g)

	if result == "" {
		t.Error("expected non-empty result")
	}

	if !strings.Contains(result, "Test Job") {
		t.Errorf("expected output to contain 'Test Job', got:\n%s", result)
	}
}

func TestASCII_Render_Empty(t *testing.T) {
	g := Graph{
		Jobs: []Job{},
	}

	result := ASCII{}.Render(g)

	if result != "" {
		t.Errorf("expected empty result for empty workflow, got:\n%s", result)
	}
}

func TestASCII_FixedBoxWidth(t *testing.T) {
	// Test that all boxes have the same fixed width
	g := Graph{
		Jobs: []Job{
			{Name: "Short", ID: "short", Steps: []Step{{Name: "Step"}}},
			{Name: "This is a very long job name", ID: "long", Steps: []Step{{Name: "Step"}}},
		},
	}

	renderer := newASCIIRenderer(g)
	renderer.calculateLevels()
	renderer.createNodes()

	// Both boxes should have the same fixed width
	if renderer.nodes[0].Width != fixedNodeWidth {
		t.Errorf("expected box width %d, got %d", fixedNodeWidth, renderer.nodes[0].Width)
	}
	if renderer.nodes[1].Width != fixedNodeWidth {
		t.Errorf("expected box width %d, got %d", fixedNodeWidth, renderer.nodes[1].Width)
	}
}

func TestASCII_RenderNode(t *testing.T) {
	job := &Job{
		Name: "Test",
		Steps: []Step{
			{Name: "Step 1"},
		},
	}

	g := Graph{Jobs: []Job{*job}}
	renderer := newASCIIRenderer(g)

	box := &asciiNode{
		Job:   job,
		JobID: "test",
		Width: fixedNodeWidth,
	}

	lines := renderer.renderNode(box)

	// Should have: top border, name, separator, step, bottom border
	if len(lines) != 5 {
		t.Errorf("expected 5 lines, got %d", len(lines))
	}

	// Check top border
	if !strings.HasPrefix(lines[0], "╭") || !strings.HasSuffix(lines[0], "╮") {
		t.Errorf("invalid top border: %s", lines[0])
	}

	// Check bottom border
	if !strings.HasPrefix(lines[4], "╰") || !strings.HasSuffix(lines[4], "╯") {
		t.Errorf("invalid bottom border: %s", lines[4])
	}

	// Check separator
	if !strings.HasPrefix(lines[2], "├") || !strings.HasSuffix(lines[2], "┤") {
		t.Errorf("invalid separator: %s", lines[2])
	}

	// Check all lines have same width
	for i, line := range lines {
		if textwidth.Width(line) != fixedNodeWidth {
			t.Errorf("line %d has width %d, expected %d: %s", i, textwidth.Width(line), fixedNodeWidth, line)
		}
	}
}

func TestASCII_RenderNode_EmbeddedAction(t *testing.T) {
	job := &Job{
		Name: "Test",
		Steps: []Step{
			{Name: "Normal Step"},
			{Name: "Embedded Step", Embedded: true},
		},
	}

	g := Graph{Jobs: []Job{*job}}
	renderer := newASCIIRenderer(g)

	box := &asciiNode{
		Job:   job,
		JobID: "test",
		Width: fixedNodeWidth,
	}

	lines := renderer.renderNode(box)

	// Should have: top border, name, separator, step1, step2, bottom border
	if len(lines) != 6 {
		t.Errorf("expected 6 lines, got %d", len(lines))
	}

	// Check normal step uses ○
	if !strings.Contains(lines[3], "○") {
		t.Errorf("expected normal step to have ○ bullet: %s", lines[3])
	}

	// Check embedded step uses ↗
	if !strings.Contains(lines[4], "↗") {
		t.Errorf("expected embedded step to have ↗ bullet: %s", lines[4])
	}
}

// TestASCII_RenderNode_WideNames pads and cuts names of wide characters by
// the columns they take, so that the box is as wide as one of narrow names.
func TestASCII_RenderNode_WideNames(t *testing.T) {
	job := &Job{
		Name: "結合テスト",
		Steps: []Step{
			{Name: "ログイン"},
			{Name: "ステージングの API を順に呼んで確かめる"},
			{Name: "埋め込み", Embedded: true, EmbeddedFile: "共通ジョブ定義ファイル.yml", EmbeddedSteps: []string{"準備", "とても長い名前の埋め込まれたステップ"}},
		},
	}
	renderer := newASCIIRenderer(Graph{Jobs: []Job{*job}})
	lines := renderer.renderNode(&asciiNode{Job: job, JobID: "test", Width: fixedNodeWidth})
	for i, line := range lines {
		line = strings.NewReplacer(ansiDim, "", ansiReset, "").Replace(line)
		if w := textwidth.Width(line); w != fixedNodeWidth {
			t.Errorf("line %d is %d columns wide, want %d: %s", i, w, fixedNodeWidth, line)
		}
	}
	if !strings.Contains(lines[4], "ステージングの API …│") {
		t.Errorf("a long wide step name should be cut by columns: %s", lines[4])
	}
}

// asciiGoldenCase is a golden test case: a graph and the name of the file holding
// its expected rendering.
type asciiGoldenCase struct {
	name  string // Test case name, used as golden file name (e.g., "diamond" -> "diamond.golden.txt")
	graph Graph
}

// getDagAsciiGoldenTestCases returns all golden test cases covering various DAG patterns:
//   - Basic: single job, embedded actions (↗ bullet), truncation (…), linear chains (2-3 levels)
//   - Divergence: one parent to multiple children (2-4 branches), including uneven step counts
//   - Convergence: multiple parents to one child (2-3 parents), including uneven step counts
//   - Complex: diamond (diverge then converge), hourglass (converge then diverge)
//   - Parallel: independent roots, independent chains
//   - Mixed: combinations with standalone jobs, sorting verification (children-first)
func asciiGoldenCases() []asciiGoldenCase {
	return []asciiGoldenCase{
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
			name: "truncated_long_names",
			graph: Graph{Jobs: []Job{
				{ID: "build", Name: "Build Application Server", Steps: []Step{
					{Name: "Initialize build environment"},
					{Name: "Compile source code"},
				}},
				{ID: "test", Name: "Run Integration Tests", Needs: []string{"build"}, Steps: []Step{
					{Name: "Setup test database connection"},
					{Name: "Execute integration test suite"},
				}},
			}},
		},
		{
			name: "wide_names",
			graph: Graph{Jobs: []Job{
				{ID: "build", Name: "ビルド", Steps: []Step{
					{Name: "ソースをコンパイルする"},
				}},
				{ID: "test", Name: "結合テストを全部実行する", Needs: []string{"build"}, Steps: []Step{
					{Name: "ステージングの API を順に呼んで確かめる"},
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
			name: "divergence_uneven_steps",
			graph: Graph{Jobs: []Job{
				{ID: "build", Name: "Build", Steps: []Step{
					{Name: "Compile"},
				}},
				{ID: "unit-test", Name: "Unit Test", Needs: []string{"build"}, Steps: []Step{
					{Name: "Setup"},
					{Name: "Run unit"},
					{Name: "Teardown"},
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
			name: "convergence_uneven_steps",
			graph: Graph{Jobs: []Job{
				{ID: "build-linux", Name: "Build Linux", Steps: []Step{
					{Name: "Setup env"},
					{Name: "Compile"},
					{Name: "Package"},
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
			name: "multi_child_convergence",
			graph: Graph{Jobs: []Job{
				{ID: "build", Name: "Build", Steps: []Step{
					{Name: "Compile"},
				}},
				{ID: "lint", Name: "Lint", Steps: []Step{
					{Name: "Run lint"},
				}},
				{ID: "unit-test", Name: "Unit Test", Needs: []string{"build", "lint"}, Steps: []Step{
					{Name: "Run unit"},
				}},
				{ID: "deploy", Name: "Deploy", Needs: []string{"build"}, Steps: []Step{
					{Name: "Deploy app"},
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
			name: "sorted_children_first",
			graph: Graph{Jobs: []Job{
				{ID: "docs", Name: "Docs", Steps: []Step{
					{Name: "Build docs"},
				}},
				{ID: "notify", Name: "Notify", Steps: []Step{
					{Name: "Send notification"},
				}},
				{ID: "build", Name: "Build", Steps: []Step{
					{Name: "Compile"},
				}},
				{ID: "lint", Name: "Lint", Steps: []Step{
					{Name: "Run lint"},
				}},
				{ID: "test", Name: "Test", Needs: []string{"build"}, Steps: []Step{
					{Name: "Run tests"},
				}},
			}},
		},
		{
			name: "wide_divergence",
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
				{ID: "e2e", Name: "E2E Test", Needs: []string{"build"}, Steps: []Step{
					{Name: "Run e2e"},
				}},
				{ID: "lint", Name: "Lint", Needs: []string{"build"}, Steps: []Step{
					{Name: "Run lint"},
				}},
			}},
		},
	}
}

// TestASCII_Golden compares the rendering of every golden case against the file in
// testdata/dag_ascii.
//
// To update golden files when the output format intentionally changes:
//
//	UPDATE_GOLDEN=1 go test -run TestASCII_Golden
func TestASCII_Golden(t *testing.T) {
	for _, tc := range asciiGoldenCases() {
		t.Run(tc.name, func(t *testing.T) {
			actual := ASCII{}.Render(tc.graph)

			goldenPath := filepath.Join("testdata", "dag_ascii", tc.name+".golden.txt")

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
