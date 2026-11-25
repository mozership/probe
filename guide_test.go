package probe

import (
	"errors"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mozership/probe/internal/actionref"
)

func TestGuideTopics_CoverEveryEmbeddedPage(t *testing.T) {
	reachable := make(map[string]bool)
	names := make(map[string]bool)
	for _, topic := range guideBook.Topics() {
		if names[topic.Name] {
			t.Errorf("topic name %q is used twice", topic.Name)
		}
		names[topic.Name] = true
		reachable[path.Join(guideRoot, topic.File)] = true

		if topic.Title == "" {
			t.Errorf("topic %q has no title; its page should start with a level-one heading", topic.Name)
		}
	}

	err := fs.WalkDir(guideFS, ".", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() && !reachable[path] {
			t.Errorf("%s is embedded but no topic prints it", path)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

// TestGuide_ActionPages checks that every page of an action in the docs is
// either built in, as that of a built-in action, or that of an official
// action, which is read from its repository and must not be built in.
func TestGuide_ActionPages(t *testing.T) {
	dir := "docs/content/reference/actions"
	official := make(map[string]bool)
	for _, a := range OfficialActions() {
		official[a.Name] = true
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		name, ok := strings.CutSuffix(e.Name(), ".md")
		if !ok {
			continue
		}
		_, err := guideFS.ReadFile(path.Join(dir, e.Name()))
		switch embedded := err == nil; {
		case official[name] && embedded:
			t.Errorf("the page of the official action %q is built in; it is read from its repository", name)
		case !official[name] && !embedded:
			t.Errorf("%s/%s is neither built in nor the page of an official action; add it to the go:embed of guideFS", dir, e.Name())
		}
	}
}

func TestGuide(t *testing.T) {
	for _, topic := range GuideTopics() {
		page, err := Guide(topic.Name)
		if err != nil {
			t.Errorf("Guide(%q): %v", topic.Name, err)
			continue
		}
		if !strings.Contains(page, "# "+topic.Title+"\n") {
			t.Errorf("Guide(%q) should return the page titled %q", topic.Name, topic.Title)
		}
	}
}

func TestGuide_Aliases(t *testing.T) {
	http, err := Guide("http")
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"actions/http", "HTTP", " http "} {
		got, err := Guide(name)
		if err != nil {
			t.Errorf("Guide(%q): %v", name, err)
		} else if got != http {
			t.Errorf("Guide(%q) should print the same page as Guide(\"http\")", name)
		}
	}

	expr, err := Guide("concepts/expressions")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(expr, "# Expressions and Templates") {
		t.Errorf("concepts/expressions should be the expressions page, got %.40q", expr)
	}

	// The reference overview and the concept page share the word "actions".
	ref, _ := Guide("actions")
	concept, _ := Guide("concepts/actions")
	if !strings.HasPrefix(ref, "# Actions Reference") || !strings.HasPrefix(concept, "# Actions\n") {
		t.Error("actions and concepts/actions should be different pages")
	}
}

func TestGuideIndex(t *testing.T) {
	index := GuideIndex()
	if !strings.HasPrefix(index, "Usage: probe guide <topic>\n") {
		t.Errorf("index should start with the usage line, got %.40q", index)
	}
	for _, group := range []string{"\nReference:\n", "\nActions:\n", "\nConcepts:\n"} {
		if !strings.Contains(index, group) {
			t.Errorf("index is missing the %q group", strings.TrimSpace(group))
		}
	}
	for _, topic := range GuideTopics() {
		// Every name is followed by at least two spaces before its title.
		if !strings.Contains(index, "  "+topic.Name+"  ") {
			t.Errorf("index should list %q with room before its title", topic.Name)
		}
	}
	_, external, ok := strings.Cut(index, "\nExternal actions, read from GitHub:\n")
	if !ok {
		t.Fatal("index is missing the external actions")
	}
	external, _, _ = strings.Cut(external, "\n\n")
	external += "\n"
	for _, a := range OfficialActions() {
		if !strings.Contains(external, "  "+a.Name+"  ") || !strings.Contains(external, "  "+a.Description+"\n") {
			t.Errorf("index should list the official action %q with its description", a.Name)
		}
	}
}

const testSHA = "0123456789abcdef0123456789abcdef01234567"

// docCall is what GuideAction asked a DocReader for.
type docCall struct{ uses, baseDir string }

func fakeDocs(calls *[]docCall) DocReader {
	return func(uses, baseDir string) (*actionref.Doc, error) {
		*calls = append(*calls, docCall{uses, baseDir})
		return &actionref.Doc{Uses: uses + "@" + testSHA, File: actionref.DocFile, Text: "# " + uses + "\n"}, nil
	}
}

func writeGuideWorkflow(t *testing.T) (string, string) {
	t.Helper()
	dir := t.TempDir()
	wf := filepath.Join(dir, "workflow.yml")
	content := "name: w\nactions:\n  my: ./tools/my\n  slack: github.com/someone/slack@" + testSHA + "\njobs:\n  - name: j\n    steps:\n      - uses: my\n"
	if err := os.WriteFile(wf, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return wf, dir
}

func TestGuideAction_Resolves(t *testing.T) {
	wf, dir := writeGuideWorkflow(t)
	tests := []struct {
		name     string
		arg      string
		workflow string
		want     docCall
	}{
		{"a name of the workflow, from its directory", "my", wf, docCall{"./tools/my", dir}},
		{"a name of the workflow over an official one", "slack", wf, docCall{"github.com/someone/slack@" + testSHA, dir}},
		{"a path, from the working directory", "./tools/my", wf, docCall{"./tools/my", ""}},
		{"a path without a workflow", "../x", "", docCall{"../x", ""}},
		{"a repository", "github.com/o/r", "", docCall{"github.com/o/r", ""}},
		{"a pinned repository", "github.com/o/r@" + testSHA, "", docCall{"github.com/o/r@" + testSHA, ""}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var calls []docCall
			if _, err := GuideAction(tt.arg, tt.workflow, fakeDocs(&calls)); err != nil {
				t.Fatal(err)
			}
			if len(calls) != 1 || calls[0] != tt.want {
				t.Errorf("read %+v, want %+v", calls, tt.want)
			}
		})
	}
}

// TestGuideAction_MergedWorkflow checks that the names of a workflow given
// as files that override one another are those a run takes.
func TestGuideAction_MergedWorkflow(t *testing.T) {
	dir := t.TempDir()
	base := filepath.Join(dir, "base.yml")
	over := filepath.Join(dir, "over.yml")
	if err := os.WriteFile(base, []byte("name: w\nactions:\n  my: ./old\njobs:\n  - name: j\n    steps:\n      - uses: my\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(over, []byte("actions:\n  my: ./new\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	var calls []docCall
	if _, err := GuideAction("my", base+","+over, fakeDocs(&calls)); err != nil {
		t.Fatal(err)
	}
	if want := (docCall{"./new", dir}); len(calls) != 1 || calls[0] != want {
		t.Errorf("read %+v, want %+v", calls, want)
	}
}

func TestGuideAction_Topics(t *testing.T) {
	wf, _ := writeGuideWorkflow(t)
	var calls []docCall
	for _, wfile := range []string{"", wf} {
		page, err := GuideAction("yaml", wfile, fakeDocs(&calls))
		if err != nil {
			t.Fatal(err)
		}
		if want, _ := Guide("yaml"); page != want {
			t.Errorf("GuideAction(yaml, %q) is not the page of yaml", wfile)
		}
	}
	if len(calls) != 0 {
		t.Errorf("read %+v for a topic", calls)
	}
}

// TestGuideAction_Official checks that the pages of the official actions are
// not built in, so that a name of one is read from its repository rather
// than found among the topics.
func TestGuideAction_Official(t *testing.T) {
	for _, a := range OfficialActions() {
		var calls []docCall
		if _, err := GuideAction(a.Name, "", fakeDocs(&calls)); err != nil {
			t.Fatal(err)
		}
		if want := (docCall{a.Repository(), ""}); len(calls) != 1 || calls[0] != want {
			t.Errorf("GuideAction(%q) read %+v, want %+v", a.Name, calls, want)
		}
	}
}

func TestGuideAction_Errors(t *testing.T) {
	var calls []docCall
	if _, err := GuideAction("nope", "", fakeDocs(&calls)); err == nil || err.Error() != "unknown guide topic: nope (run probe guide to list the topics)" {
		t.Errorf("error = %v", err)
	}
	if _, err := GuideAction("my", filepath.Join(t.TempDir(), "none.yml"), fakeDocs(&calls)); err == nil {
		t.Error("a workflow that is not there should fail")
	}
	// A path, with spaces around it, is not looked for among the names of
	// the workflow, which is then not read.
	if _, err := GuideAction(" ./x ", filepath.Join(t.TempDir(), "none.yml"), fakeDocs(new([]docCall))); err != nil {
		t.Errorf("a path should not read the workflow: %v", err)
	}
	failing := func(string, string) (*actionref.Doc, error) { return nil, errors.New("boom") }
	if _, err := GuideAction("github.com/o/r", "", failing); err == nil || err.Error() != "boom" {
		t.Errorf("error = %v, want that of the reader", err)
	}
	if len(calls) != 0 {
		t.Errorf("read %+v", calls)
	}
}

// TestOfficialActions_HaveDocsPages keeps the list in step with the pages
// of the docs, each official action having one by its name.
func TestOfficialActions_HaveDocsPages(t *testing.T) {
	for _, a := range OfficialActions() {
		if !strings.HasPrefix(a.Repository(), "github.com/mozership/probe-") || a.Description == "" {
			t.Errorf("official action %+v", a)
		}
		if _, err := os.Stat(filepath.Join("docs", "content", "reference", "actions", a.Name+".md")); err != nil {
			t.Errorf("official action %q has no page in the docs: %v", a.Name, err)
		}
	}
}
