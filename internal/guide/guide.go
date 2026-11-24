// Package guide holds the pages probe guide prints: the reference and
// concept pages of the documentation, which the binary carries, and the
// description of an external action, which is read from where the action
// is.
//
// The pages are given as a file system laid out as docs/content is, so
// that this package does not depend on where the binary keeps them.
package guide

import (
	"fmt"
	"io/fs"
	"path"
	"sort"
	"strings"

	"github.com/mozership/probe/internal/suggest"
)

// Topic is one page probe guide can print.
type Topic struct {
	Name  string // what to pass to probe guide
	Group string // the heading it is listed under
	Title string // the page's first heading
	File  string // the page, in the file system of the Book
}

// The groups the topics are listed under.
const (
	GroupReference = "Reference"
	GroupActions   = "Actions"
	GroupConcepts  = "Concepts"
	GroupExternal  = "External actions, read from GitHub"
)

// The directories of the pages of the actions and of the concepts.
const (
	actionsDir  = "reference/actions"
	conceptsDir = "guide/concepts"
)

// references names the reference pages that are not about one action.
var references = []struct{ name, file string }{
	{"yaml", "reference/yaml-configuration.md"},
	{"cli", "reference/cli-reference.md"},
	{"functions", "reference/built-in-functions.md"},
	{"env", "reference/environment-variables.md"},
	{"actions", "reference/actions-reference.md"},
}

// conceptAliases gives the concept pages whose file names are long a
// shorter topic name.
var conceptAliases = map[string]string{
	"expressions-and-templates": "expressions",
	"testing-and-assertions":    "testing",
}

// Book is the pages of the documentation probe guide prints.
type Book struct {
	fsys fs.FS
}

// New returns the book of the pages in fsys, laid out as docs/content is:
// the reference pages under reference, those of the actions under
// reference/actions, and the concepts under guide/concepts. Only the pages
// of the built-in actions are expected to be there: an official action is
// described by its own repository.
func New(fsys fs.FS) *Book {
	return &Book{fsys: fsys}
}

// Topics lists every topic, references first, then one per action, then
// the concepts.
func (b *Book) Topics() []Topic {
	var topics []Topic
	for _, r := range references {
		topics = append(topics, b.topic(r.name, GroupReference, r.file))
	}

	for _, file := range b.files(actionsDir) {
		name := strings.TrimSuffix(path.Base(file), ".md")
		topics = append(topics, b.topic(name, GroupActions, file))
	}

	for _, file := range b.files(conceptsDir) {
		stem := strings.TrimSuffix(path.Base(file), ".md")
		if alias, ok := conceptAliases[stem]; ok {
			stem = alias
		}
		topics = append(topics, b.topic("concepts/"+stem, GroupConcepts, file))
	}
	return topics
}

// files returns the Markdown pages in dir, sorted.
func (b *Book) files(dir string) []string {
	entries, err := fs.ReadDir(b.fsys, dir)
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

func (b *Book) topic(name, group, file string) Topic {
	return Topic{Name: name, Group: group, Title: b.title(file), File: file}
}

// title returns the page's first level-one heading.
func (b *Book) title(file string) string {
	data, err := fs.ReadFile(b.fsys, file)
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

// Page returns the Markdown page for a topic. An action's page can also be
// asked for as actions/<name>.
func (b *Book) Page(topic string) (string, error) {
	topic = strings.TrimSpace(strings.ToLower(topic))
	if name, ok := strings.CutPrefix(topic, "actions/"); ok {
		topic = name
	}
	topics := b.Topics()
	names := make([]string, 0, len(topics))
	for _, t := range topics {
		if t.Name == topic {
			data, err := fs.ReadFile(b.fsys, t.File)
			if err != nil {
				return "", err
			}
			return string(data), nil
		}
		names = append(names, t.Name)
	}
	return "", unknown(topic, names)
}

// unknown is the error of a topic that is not among names, with the one
// likely meant when one is close.
func unknown(topic string, names []string) error {
	if s := suggest.Closest(topic, names); s != "" {
		return fmt.Errorf("unknown guide topic: %s; did you mean %q?", topic, s)
	}
	return fmt.Errorf("unknown guide topic: %s (run probe guide to list the topics)", topic)
}

// Index renders the list of topics probe guide prints without one.
func (b *Book) Index() string {
	var s strings.Builder
	s.WriteString("Usage: probe guide <topic>\n")
	s.WriteString("       probe guide <action> [workflow-file]\n\n")
	s.WriteString("Prints a page of the Probe documentation as Markdown, for the version\n")
	s.WriteString("of Probe that is running, or the description of an external action:\n")
	s.WriteString("a local one by its directory, as ./my-action, or one on GitHub as\n")
	s.WriteString("github.com/<owner>/<repo>, at its latest release, or @<commit SHA>.\n")
	s.WriteString("With a workflow file, an action can be named as its actions names it.\n\n")
	s.WriteString("Topics:\n")

	// The official actions are listed after the built-in ones, under a
	// heading of their own, with the line of description the list keeps,
	// as their pages are not in the binary.
	var lines []Topic
	for _, t := range b.Topics() {
		if t.Group == GroupConcepts && len(lines) > 0 && lines[len(lines)-1].Group == GroupActions {
			for _, a := range officialActions {
				lines = append(lines, Topic{Name: a.Name, Group: GroupExternal, Title: a.Description})
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
			fmt.Fprintf(&s, "\n%s:\n", group)
		}
		fmt.Fprintf(&s, "  %-*s  %s\n", width, t.Name, t.Title)
	}
	return s.String()
}
