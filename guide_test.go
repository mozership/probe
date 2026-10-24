package probe

import (
	"io/fs"
	"strings"
	"testing"
)

func TestGuideTopics_CoverEveryEmbeddedPage(t *testing.T) {
	reachable := make(map[string]bool)
	names := make(map[string]bool)
	for _, topic := range GuideTopics() {
		if names[topic.Name] {
			t.Errorf("topic name %q is used twice", topic.Name)
		}
		names[topic.Name] = true
		reachable[topic.file] = true

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

func TestGuide_Unknown(t *testing.T) {
	_, err := Guide("nope")
	if err == nil || err.Error() != "unknown guide topic: nope (run probe guide to list the topics)" {
		t.Errorf("error = %v", err)
	}

	_, err = Guide("htpp")
	if err == nil || err.Error() != `unknown guide topic: htpp; did you mean "http"?` {
		t.Errorf("error = %v", err)
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
}
