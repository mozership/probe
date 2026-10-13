package probe

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/mozership/probe/internal/yamlfmt"
)

func TestSkill_Frontmatter(t *testing.T) {
	s := Skill()
	if !strings.HasPrefix(s, "---\nname: probe\ndescription: ") {
		t.Fatalf("SKILL.md should open with frontmatter naming the skill, got %.60q", s)
	}
	end := strings.Index(s[4:], "\n---\n")
	if end < 0 {
		t.Fatal("SKILL.md frontmatter is not closed")
	}
	for line := range strings.SplitSeq(s[4:4+end], "\n") {
		if desc, ok := strings.CutPrefix(line, "description: "); ok && len(desc) > 1024 {
			t.Errorf("description is %d characters; skill loaders cap it at 1024", len(desc))
		}
	}
}

// TestSkill_ExampleLoads keeps the workflow in SKILL.md valid: an agent
// copies it, so it has to load as Probe reads workflows.
func TestSkill_ExampleLoads(t *testing.T) {
	m := regexp.MustCompile("(?s)## A workflow\n\n```yaml\n(.*?)```").FindStringSubmatch(Skill())
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

// TestSkill_GuideTopicsExist checks every probe guide command in SKILL.md
// names a topic that exists, so a renamed page cannot leave the skill
// pointing at nothing.
func TestSkill_GuideTopicsExist(t *testing.T) {
	matches := regexp.MustCompile(`probe guide ([a-z][a-z/-]*)`).FindAllStringSubmatch(Skill(), -1)
	if len(matches) == 0 {
		t.Fatal("SKILL.md should point at probe guide")
	}
	for _, m := range matches {
		if _, err := Guide(m[1]); err != nil {
			t.Errorf("SKILL.md mentions probe guide %s: %v", m[1], err)
		}
	}
}

func TestInstallSkill(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "nested", "skills", "probe")

	path, err := InstallSkill(dir)
	if err != nil {
		t.Fatal(err)
	}
	if path != filepath.Join(dir, "SKILL.md") {
		t.Errorf("path = %s", path)
	}
	data, err := os.ReadFile(path)
	if err != nil || string(data) != Skill() {
		t.Fatalf("installed skill differs from the embedded one: %v", err)
	}

	// Running it again replaces an outdated copy.
	if err := os.WriteFile(path, []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := InstallSkill(dir); err != nil {
		t.Fatal(err)
	}
	if data, _ := os.ReadFile(path); string(data) != Skill() {
		t.Error("an existing SKILL.md should be replaced")
	}
}

func TestInstallSkill_DefaultDir(t *testing.T) {
	t.Chdir(t.TempDir())

	path, err := InstallSkill("")
	if err != nil {
		t.Fatal(err)
	}
	if path != filepath.Join(".claude", "skills", "probe", "SKILL.md") {
		t.Errorf("path = %s, want the default directory", path)
	}
}

func TestInstallSkill_Unwritable(t *testing.T) {
	file := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(file, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := InstallSkill(filepath.Join(file, "skills")); err == nil {
		t.Error("expected an error when the directory cannot be created")
	}
}

// TestInstallSkill_DoesNotFollowSymlink covers a project that ships SKILL.md
// as a symlink to a file outside it: installing must replace the link, not
// write through it.
func TestInstallSkill_DoesNotFollowSymlink(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "outside.txt")
	if err := os.WriteFile(target, []byte("keep me"), 0o600); err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(root, "project", ".claude", "skills", "probe")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "SKILL.md")
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}

	if _, err := InstallSkill(dir); err != nil {
		t.Fatal(err)
	}

	if data, _ := os.ReadFile(target); string(data) != "keep me" {
		t.Errorf("the file the link pointed at was changed to %.40q", data)
	}
	info, err := os.Lstat(link)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		t.Error("SKILL.md should now be a regular file, not the link")
	}
	if data, _ := os.ReadFile(link); string(data) != Skill() {
		t.Error("SKILL.md should hold the skill")
	}
	if info.Mode().Perm() != umaskMode(t) {
		t.Errorf("mode = %v, want %v as the umask allows", info.Mode().Perm(), umaskMode(t))
	}

	leftovers, _ := filepath.Glob(filepath.Join(dir, ".SKILL.md.*"))
	if len(leftovers) != 0 {
		t.Errorf("temporary files left behind: %v", leftovers)
	}
}

// TestInstallSkill_RefusesLinkedDirectory covers a project whose .claude is
// a symlink to a directory elsewhere.
func TestInstallSkill_RefusesLinkedDirectory(t *testing.T) {
	victim := checkout(t)
	if err := os.MkdirAll(filepath.Join(filepath.Dir(victim), "skills", "probe"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Dir(victim), ".claude"); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}

	if _, err := InstallSkill(""); err == nil {
		t.Error("installing through a linked .claude should be refused")
	}
	if _, err := os.Stat(filepath.Join(filepath.Dir(victim), "skills", "probe", "SKILL.md")); err == nil {
		t.Error("SKILL.md was written outside the project")
	}
}

// checkout makes a working directory with a file outside it and a symlink
// "out" inside it pointing at the outside directory, as a checked-out
// project could carry. It returns the outside file.
func checkout(t *testing.T) (outsideFile string) {
	t.Helper()
	base := t.TempDir()
	outside := filepath.Join(base, "outside")
	project := filepath.Join(base, "project")
	for _, d := range []string{outside, project} {
		if err := os.Mkdir(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	outsideFile = filepath.Join(outside, "victim")
	if err := os.WriteFile(outsideFile, []byte("keep me"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(project, "out")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	t.Chdir(project)
	return outsideFile
}

// umaskMode is the mode os.Create gives a new file under the current umask.
func umaskMode(t *testing.T) os.FileMode {
	t.Helper()
	ref := filepath.Join(t.TempDir(), "ref")
	f, err := os.Create(ref)
	if err != nil {
		t.Fatal(err)
	}
	_ = f.Close()
	info, err := os.Stat(ref)
	if err != nil {
		t.Fatal(err)
	}
	return info.Mode().Perm()
}
