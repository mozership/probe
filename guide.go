package probe

import (
	"embed"
	"fmt"
	"io/fs"
	"path"
	"sort"
	"strings"

	"github.com/mozership/probe/internal/suggest"
)

// The reference and concept pages of the documentation site are built into
// the binary, so that probe guide works offline and always matches the
// version that runs. A coding agent reads them before writing a workflow.
//
// Of the pages of actions, only those of the built-in actions are: an
// external action is described by its own repository, at the commit a
// workflow pins, and its page in the docs is left out, so that Probe needs
// no release to follow one of the action.
//
//go:embed docs/content/reference/*.md docs/content/guide/concepts/*.md
//go:embed docs/content/reference/actions/db.md docs/content/reference/actions/dns.md
//go:embed docs/content/reference/actions/embedded.md docs/content/reference/actions/grpc.md
//go:embed docs/content/reference/actions/hello.md docs/content/reference/actions/http.md
//go:embed docs/content/reference/actions/imap.md docs/content/reference/actions/shell.md
//go:embed docs/content/reference/actions/smtp.md docs/content/reference/actions/ssh.md
var guideFS embed.FS

// GuideTopic is one page probe guide can print.
type GuideTopic struct {
	Name  string // what to pass to probe guide
	Group string // the heading it is listed under
	Title string // the page's first heading
	file  string
}

const (
	guideGroupReference = "Reference"
	guideGroupActions   = "Actions"
	guideGroupConcepts  = "Concepts"
	guideGroupExternal  = "External actions, read from GitHub"
)

// guideReferences names the reference pages that are not about one action.
var guideReferences = []struct{ name, file string }{
	{"yaml", "docs/content/reference/yaml-configuration.md"},
	{"cli", "docs/content/reference/cli-reference.md"},
	{"functions", "docs/content/reference/built-in-functions.md"},
	{"env", "docs/content/reference/environment-variables.md"},
	{"actions", "docs/content/reference/actions-reference.md"},
}

// conceptAliases gives the concept pages whose file names are long a
// shorter topic name.
var conceptAliases = map[string]string{
	"expressions-and-templates": "expressions",
	"testing-and-assertions":    "testing",
}

// GuideTopics lists every topic, references first, then one per action, then
// the concepts.
func GuideTopics() []GuideTopic {
	var topics []GuideTopic
	for _, r := range guideReferences {
		topics = append(topics, newGuideTopic(r.name, guideGroupReference, r.file))
	}

	for _, file := range guideFiles("docs/content/reference/actions") {
		name := strings.TrimSuffix(path.Base(file), ".md")
		topics = append(topics, newGuideTopic(name, guideGroupActions, file))
	}

	for _, file := range guideFiles("docs/content/guide/concepts") {
		stem := strings.TrimSuffix(path.Base(file), ".md")
		if alias, ok := conceptAliases[stem]; ok {
			stem = alias
		}
		topics = append(topics, newGuideTopic("concepts/"+stem, guideGroupConcepts, file))
	}
	return topics
}

func guideFiles(dir string) []string {
	entries, err := fs.ReadDir(guideFS, dir)
	if err != nil {
		return nil
	}
	var files []string
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".md") {
			files = append(files, path.Join(dir, e.Name()))
		}
	}
	sort.Strings(files)
	return files
}

func newGuideTopic(name, group, file string) GuideTopic {
	return GuideTopic{Name: name, Group: group, Title: guideTitle(file), file: file}
}

// guideTitle returns the page's first level-one heading.
func guideTitle(file string) string {
	data, err := guideFS.ReadFile(file)
	if err != nil {
		return ""
	}
	for line := range strings.SplitSeq(string(data), "\n") {
		if title, ok := strings.CutPrefix(line, "# "); ok {
			return strings.TrimSpace(title)
		}
	}
	return ""
}

// Guide returns the Markdown page for a topic. An action's page can also be
// asked for as actions/<name>.
func Guide(topic string) (string, error) {
	topic = strings.TrimSpace(strings.ToLower(topic))
	if name, ok := strings.CutPrefix(topic, "actions/"); ok {
		topic = name
	}
	topics := GuideTopics()
	names := make([]string, 0, len(topics))
	for _, t := range topics {
		if t.Name == topic {
			data, err := guideFS.ReadFile(t.file)
			if err != nil {
				return "", err
			}
			return string(data), nil
		}
		names = append(names, t.Name)
	}
	if s := suggest.Closest(topic, names); s != "" {
		return "", fmt.Errorf("unknown guide topic: %s; did you mean %q?", topic, s)
	}
	return "", fmt.Errorf("unknown guide topic: %s (run probe guide to list the topics)", topic)
}

// GuideIndex renders the list of topics probe guide prints without one.
func GuideIndex() string {
	var b strings.Builder
	b.WriteString("Usage: probe guide <topic>\n")
	b.WriteString("       probe guide <action> [workflow-file]\n\n")
	b.WriteString("Prints a page of the Probe documentation as Markdown, for the version\n")
	b.WriteString("of Probe that is running, or the description of an external action:\n")
	b.WriteString("a local one by its directory, as ./my-action, or one on GitHub as\n")
	b.WriteString("github.com/<owner>/<repo>, at its latest release, or @<commit SHA>.\n")
	b.WriteString("With a workflow file, an action can be named as its actions names it.\n\n")
	b.WriteString("Topics:\n")

	// The official actions are listed after the built-in ones, under a
	// heading of their own, with the line of description the list keeps,
	// as their pages are not in the binary.
	var lines []GuideTopic
	for _, t := range GuideTopics() {
		if t.Group == guideGroupConcepts && len(lines) > 0 && lines[len(lines)-1].Group == guideGroupActions {
			for _, a := range officialActions {
				lines = append(lines, GuideTopic{Name: a.Name, Group: guideGroupExternal, Title: a.Description})
			}
		}
		lines = append(lines, t)
	}
	width := 0
	for _, t := range lines {
		width = max(width, len(t.Name))
	}

	group := ""
	for _, t := range lines {
		if t.Group != group {
			group = t.Group
			fmt.Fprintf(&b, "\n%s:\n", group)
		}
		fmt.Fprintf(&b, "  %-*s  %s\n", width, t.Name, t.Title)
	}
	return b.String()
}
