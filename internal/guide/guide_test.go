package guide

import (
	"strings"
	"testing"
	"testing/fstest"
)

// testBook is a book of a few pages laid out as docs/content is, with files
// beside them that are no pages.
func testBook() *Book {
	page := func(s string) *fstest.MapFile { return &fstest.MapFile{Data: []byte(s)} }
	return New(fstest.MapFS{
		"reference/yaml-configuration.md":             page("# YAML Configuration\n"),
		"reference/cli-reference.md":                  page("---\ntitle: x\n---\n# CLI Reference\n"),
		"reference/built-in-functions.md":             page("# Built-in Functions\n"),
		"reference/environment-variables.md":          page("# Environment Variables\n"),
		"reference/actions-reference.md":              page("# Actions Reference\n"),
		"reference/index.mdx":                         page("# Not a page\n"),
		"reference/actions/http.md":                   page("# HTTP Action\n"),
		"reference/actions/dns.md":                    page("# DNS Action\n"),
		"reference/actions/_meta.ts":                  page("export default {}\n"),
		"guide/concepts/actions.md":                   page("# Actions\n"),
		"guide/concepts/testing-and-assertions.md":    page("# Testing and Assertions\n"),
		"guide/concepts/expressions-and-templates.md": page("# Expressions and Templates\n"),
	})
}

func TestTopics(t *testing.T) {
	var got []string
	for _, topic := range testBook().Topics() {
		got = append(got, topic.Group+" "+topic.Name+" "+topic.File+" "+topic.Title)
	}
	want := []string{
		"Reference yaml reference/yaml-configuration.md YAML Configuration",
		"Reference cli reference/cli-reference.md CLI Reference",
		"Reference functions reference/built-in-functions.md Built-in Functions",
		"Reference env reference/environment-variables.md Environment Variables",
		"Reference actions reference/actions-reference.md Actions Reference",
		"Actions dns reference/actions/dns.md DNS Action",
		"Actions http reference/actions/http.md HTTP Action",
		"Concepts concepts/actions guide/concepts/actions.md Actions",
		"Concepts concepts/expressions guide/concepts/expressions-and-templates.md Expressions and Templates",
		"Concepts concepts/testing guide/concepts/testing-and-assertions.md Testing and Assertions",
	}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Errorf("Topics() =\n%s\nwant\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
}

func TestPage(t *testing.T) {
	b := testBook()
	for _, name := range []string{"http", "actions/http", "HTTP", " http "} {
		got, err := b.Page(name)
		if err != nil {
			t.Errorf("Page(%q): %v", name, err)
		} else if got != "# HTTP Action\n" {
			t.Errorf("Page(%q) = %q, want the page of http", name, got)
		}
	}

	// The reference overview and the concept page share the word "actions".
	ref, _ := b.Page("actions")
	concept, _ := b.Page("concepts/actions")
	if ref != "# Actions Reference\n" || concept != "# Actions\n" {
		t.Errorf("actions = %q and concepts/actions = %q, want different pages", ref, concept)
	}
}

func TestPage_Unknown(t *testing.T) {
	b := testBook()
	if _, err := b.Page("nope"); err == nil || err.Error() != "unknown guide topic: nope (run probe guide to list the topics)" {
		t.Errorf("error = %v", err)
	}
	if _, err := b.Page("htpp"); err == nil || err.Error() != `unknown guide topic: htpp; did you mean "http"?` {
		t.Errorf("error = %v", err)
	}
}

// TestIndex lists the official actions after the built-in ones, under a
// heading of their own, with the names lined up.
func TestIndex(t *testing.T) {
	saved := officialActions
	t.Cleanup(func() { officialActions = saved })
	officialActions = []OfficialAction{{Name: "zeta", Description: "Do zeta"}}

	index := testBook().Index()
	if !strings.HasPrefix(index, "Usage: probe guide <topic>\n") {
		t.Errorf("index should start with the usage line, got %.40q", index)
	}
	_, topics, _ := strings.Cut(index, "Topics:\n")
	want := `
Reference:
  yaml                  YAML Configuration
  cli                   CLI Reference
  functions             Built-in Functions
  env                   Environment Variables
  actions               Actions Reference

Actions:
  dns                   DNS Action
  http                  HTTP Action

External actions, read from GitHub:
  zeta                  Do zeta

Concepts:
  concepts/actions      Actions
  concepts/expressions  Expressions and Templates
  concepts/testing      Testing and Assertions
`
	if topics != want {
		t.Errorf("topics =\n%s\nwant\n%s", topics, want)
	}
}
