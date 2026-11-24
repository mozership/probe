package engine

import (
	"os"
	"path/filepath"
	"regexp"
	"testing"

	"github.com/mozership/probe/internal/yamlfmt"
)

func skillText(t *testing.T) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("..", "..", "skills", "probe", "SKILL.md"))
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// TestSkill_ExampleLoads keeps the workflow in SKILL.md valid: an agent
// copies it, so it has to load as Probe reads workflows.
func TestSkill_ExampleLoads(t *testing.T) {
	m := regexp.MustCompile("(?s)## A workflow\n\n```yaml\n(.*?)```").FindStringSubmatch(skillText(t))
	if m == nil {
		t.Fatal("SKILL.md has no example workflow under \"## A workflow\"")
	}
	path := filepath.Join(t.TempDir(), "example.yml")
	if err := os.WriteFile(path, []byte(m[1]), 0o600); err != nil {
		t.Fatal(err)
	}

	// It is also laid out as probe fmt lays a workflow out.
	formatted, err := yamlfmt.Format([]byte(m[1]))
	if err != nil {
		t.Fatalf("the example is not formatted: %v", err)
	}
	if string(formatted) != m[1] {
		t.Errorf("the example is not formatted as probe fmt formats it, which gives:\n%s", formatted)
	}

	p := New(path, false)
	if err := p.Load(); err != nil {
		t.Fatalf("the example does not load: %v", err)
	}
	if got := p.workflow.Secrets; len(got) != 1 || got[0] != "API_TOKEN" {
		t.Errorf("Secrets = %v", got)
	}
	if n := len(p.workflow.Jobs); n != 2 {
		t.Errorf("jobs = %d, want 2", n)
	}
}
