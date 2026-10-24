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
//go:embed docs/content/reference/*.md docs/content/reference/actions/*.md docs/content/guide/concepts/*.md
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
	b.WriteString("Usage: probe guide <topic>\n\n")
	b.WriteString("Prints a page of the Probe documentation as Markdown, for the version\n")
	b.WriteString("of Probe that is running. Topics:\n")

	topics := GuideTopics()
	width := 0
	for _, t := range topics {
		width = max(width, len(t.Name))
	}

	group := ""
	for _, t := range topics {
		if t.Group != group {
			group = t.Group
			fmt.Fprintf(&b, "\n%s:\n", group)
		}
		fmt.Fprintf(&b, "  %-*s  %s\n", width, t.Name, t.Title)
	}
	return b.String()
}
