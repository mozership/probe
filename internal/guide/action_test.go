package guide

import (
	"errors"
	"strings"
	"testing"

	"github.com/mozership/probe/internal/actionref"
)

const testSHA = "0123456789abcdef0123456789abcdef01234567"

// docCall is what Action asked a DocReader for.
type docCall struct{ uses, baseDir string }

func fakeDocs(calls *[]docCall) DocReader {
	return func(uses, baseDir string) (*actionref.Doc, error) {
		*calls = append(*calls, docCall{uses, baseDir})
		return &actionref.Doc{Uses: uses + "@" + testSHA, File: actionref.DocFile, Text: "# " + uses + "\n"}, nil
	}
}

// testWorkflow names two actions, one of them by the name of an official
// action.
var testWorkflow = Workflow{
	Uses: map[string]string{"my": "./tools/my", "slack": "github.com/someone/slack@" + testSHA},
	Dir:  "/w",
}

func TestAction_Resolves(t *testing.T) {
	tests := []struct {
		name string
		arg  string
		w    Workflow
		want docCall
	}{
		{"a name of the workflow, from its directory", "my", testWorkflow, docCall{"./tools/my", "/w"}},
		{"a name of the workflow, trimmed", " my ", testWorkflow, docCall{"./tools/my", "/w"}},
		{"a name of the workflow over an official one", "slack", testWorkflow, docCall{"github.com/someone/slack@" + testSHA, "/w"}},
		{"a path, from the working directory", "./tools/my", testWorkflow, docCall{"./tools/my", ""}},
		{"a path without a workflow", "../x", Workflow{}, docCall{"../x", ""}},
		{"an absolute path", "/a/x", Workflow{}, docCall{"/a/x", ""}},
		{"a repository", "github.com/o/r", Workflow{}, docCall{"github.com/o/r", ""}},
		{"a pinned repository", "github.com/o/r@" + testSHA, Workflow{}, docCall{"github.com/o/r@" + testSHA, ""}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var calls []docCall
			if _, err := testBook().Action(tt.arg, tt.w, fakeDocs(&calls)); err != nil {
				t.Fatal(err)
			}
			if len(calls) != 1 || calls[0] != tt.want {
				t.Errorf("read %+v, want %+v", calls, tt.want)
			}
		})
	}
}

func TestAction_Topics(t *testing.T) {
	b := testBook()
	var calls []docCall
	for _, w := range []Workflow{{}, testWorkflow} {
		page, err := b.Action("yaml", w, fakeDocs(&calls))
		if err != nil {
			t.Fatal(err)
		}
		if want, _ := b.Page("yaml"); page != want {
			t.Errorf("Action(yaml, %+v) is not the page of yaml", w)
		}
	}
	if len(calls) != 0 {
		t.Errorf("read %+v for a topic", calls)
	}
}

// TestAction_Official checks that an official action that is no topic is
// read from its repository.
func TestAction_Official(t *testing.T) {
	saved := officialActions
	t.Cleanup(func() { officialActions = saved })
	officialActions = append(OfficialActions(), OfficialAction{Name: "zeta", Description: "Do zeta"})

	b := testBook()
	for _, arg := range []string{"zeta", "actions/zeta", "Zeta"} {
		var calls []docCall
		if _, err := b.Action(arg, Workflow{}, fakeDocs(&calls)); err != nil {
			t.Fatal(err)
		}
		if want := (docCall{"github.com/mozership/probe-zeta", ""}); len(calls) != 1 || calls[0] != want {
			t.Errorf("Action(%q) read %+v, want %+v", arg, calls, want)
		}
	}

	// The pages of the official actions are not in the book, so a name of
	// one is read from its repository rather than found among the topics.
	for _, a := range saved {
		var calls []docCall
		if _, err := b.Action(a.Name, Workflow{}, fakeDocs(&calls)); err != nil {
			t.Fatal(err)
		}
		if want := (docCall{a.Repository(), ""}); len(calls) != 1 || calls[0] != want {
			t.Errorf("Action(%q) read %+v, want %+v", a.Name, calls, want)
		}
	}

	_, err := b.Action("zeat", Workflow{}, fakeDocs(new([]docCall)))
	if err == nil || err.Error() != `unknown guide topic: zeat; did you mean "zeta"?` {
		t.Errorf("error = %v", err)
	}
}

func TestAction_Errors(t *testing.T) {
	b := testBook()
	var calls []docCall
	if _, err := b.Action("nope", Workflow{}, fakeDocs(&calls)); err == nil || err.Error() != "unknown guide topic: nope (run probe guide to list the topics)" {
		t.Errorf("error = %v", err)
	}
	if _, err := b.Action("actions/htpp", Workflow{}, fakeDocs(&calls)); err == nil || err.Error() != `unknown guide topic: htpp; did you mean "http"?` {
		t.Errorf("error = %v", err)
	}
	failing := func(string, string) (*actionref.Doc, error) { return nil, errors.New("boom") }
	if _, err := b.Action("github.com/o/r", Workflow{}, failing); err == nil || err.Error() != "boom" {
		t.Errorf("error = %v, want that of the reader", err)
	}
	if len(calls) != 0 {
		t.Errorf("read %+v", calls)
	}
}

func TestDocPage(t *testing.T) {
	tests := []struct {
		name string
		doc  actionref.Doc
		want string
	}{
		{
			"the latest release",
			actionref.Doc{Uses: "github.com/o/r@" + testSHA, Version: "v1.2.0", Latest: true, File: actionref.DocFile, Text: "# x\n"},
			"> The action github.com/o/r at its latest release. What follows is its README.md at that commit, written by the authors of the action, not by Probe. Pin it as:\n\n```yaml\nuses: github.com/o/r@" + testSHA + " # v1.2.0\n```\n\n---\n\n# x\n",
		},
		{
			"a commit given, with a warning and no version",
			actionref.Doc{Uses: "github.com/o/r@" + testSHA, File: actionref.ManifestFile, Text: "# x", Warning: "w"},
			"> The action github.com/o/r at the commit given. What follows is what its action.yml declares, as it has no README.md at that commit, written by the authors of the action, not by Probe. Pin it as:\n\n```yaml\nuses: github.com/o/r@" + testSHA + "\n```\n\n> Warning: w.\n\n---\n\n# x\n",
		},
		{
			"a local action",
			actionref.Doc{Uses: "/a/x", Local: true, File: actionref.DocFile, Text: "# x\n"},
			"> The local action in /a/x. What follows is its README.md, written by the authors of the action, not by Probe.\n\n---\n\n# x\n",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := docPage(&tt.doc, nil)
			if err != nil {
				t.Fatal(err)
			}
			if got != tt.want {
				t.Errorf("docPage() =\n%s\nwant\n%s", got, tt.want)
			}
		})
	}
}

func TestOfficialActions(t *testing.T) {
	for _, a := range OfficialActions() {
		if !strings.HasPrefix(a.Repository(), "github.com/mozership/probe-") || a.Name == "" || a.Description == "" {
			t.Errorf("official action %+v", a)
		}
	}

	// The list is a copy, which changes nothing of the official actions.
	OfficialActions()[0].Name = "changed"
	if officialActions[0].Name == "changed" {
		t.Error("OfficialActions() returned the list itself")
	}
}
