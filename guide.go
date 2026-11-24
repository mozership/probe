package probe

import (
	"embed"
	"io/fs"
	"strings"

	"github.com/mozership/probe/internal/engine"
	"github.com/mozership/probe/internal/guide"
)

// The reference and concept pages of the documentation site are built into
// the binary, so that probe guide works offline and always matches the
// version that runs. A coding agent reads them before writing a workflow.
// They are embedded here, as go:embed reads only the directory of the
// package and those under it, and printed by internal/guide.
//
// Of the pages of actions, only those of the built-in actions are: an
// external action is described by its own repository, at the commit a
// workflow pins, and its page in the docs is left out, so that Probe needs
// no release to follow one of the action.
//
//go:embed docs/content/reference/*.md docs/content/guide/concepts/*.md
//go:embed docs/content/reference/actions/db.md docs/content/reference/actions/dns.md
//go:embed docs/content/reference/actions/embedded.md docs/content/reference/actions/file.md
//go:embed docs/content/reference/actions/grpc.md
//go:embed docs/content/reference/actions/hello.md docs/content/reference/actions/http.md
//go:embed docs/content/reference/actions/imap.md docs/content/reference/actions/shell.md
//go:embed docs/content/reference/actions/smtp.md docs/content/reference/actions/ssh.md
var guideFS embed.FS

// guideRoot is the directory of guideFS that the book is laid out in.
const guideRoot = "docs/content"

// guideBook is the book of the pages built into the binary.
var guideBook = guide.New(mustSub(guideFS, guideRoot))

func mustSub(fsys fs.FS, dir string) fs.FS {
	sub, err := fs.Sub(fsys, dir)
	if err != nil {
		panic(err)
	}
	return sub
}

// GuideTopic is one page probe guide can print.
type GuideTopic struct {
	Name  string // what to pass to probe guide
	Group string // the heading it is listed under
	Title string // the page's first heading
}

// GuideTopics lists every topic, references first, then one per action, then
// the concepts.
func GuideTopics() []GuideTopic {
	topics := guideBook.Topics()
	out := make([]GuideTopic, len(topics))
	for i, t := range topics {
		out[i] = GuideTopic{Name: t.Name, Group: t.Group, Title: t.Title}
	}
	return out
}

// Guide returns the Markdown page for a topic. An action's page can also be
// asked for as actions/<name>.
func Guide(topic string) (string, error) {
	return guideBook.Page(topic)
}

// GuideIndex renders the list of topics probe guide prints without one.
func GuideIndex() string {
	return guideBook.Index()
}

// OfficialAction is an external action published alongside Probe.
type OfficialAction = guide.OfficialAction

// OfficialActions lists the external actions published alongside Probe.
func OfficialActions() []OfficialAction {
	return guide.OfficialActions()
}

// DocReader reads the description of an external action, as probe guide
// does for an action that is not one of Probe's.
type DocReader = guide.DocReader

// GuideAction returns the page probe guide prints for arg, a topic or an
// external action, with workflow, when it is not empty, the files of a
// workflow whose names under actions arg may be. It is looked for in this
// order:
//
//  1. A name the workflow gives under actions, so that the page is that of
//     the action the workflow runs. A path there is taken from the
//     directory of the workflow, as a run takes it.
//  2. A local path, starting with ./ or ../ or absolute, taken from the
//     working directory, or github.com/<owner>/<repo>[/<dir>][@<commit>].
//  3. A topic of the pages built into Probe.
//  4. An official action, read from github.com/mozership/probe-<name>.
//
// An external action is described by what read returns, with the lines
// that tell where it comes from before it.
func GuideAction(arg, workflow string, read DocReader) (string, error) {
	var w guide.Workflow
	if workflow != "" && !guide.IsActionPath(strings.TrimSpace(arg)) {
		var err error
		if w, err = engine.WorkflowActions(workflow); err != nil {
			return "", err
		}
	}
	return guideBook.Action(arg, w, read)
}
