package probe

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/goccy/go-yaml"
	"github.com/mozership/probe/actionref"
	"github.com/mozership/probe/internal/suggest"
)

// officialRepoPrefix is where an official action is published: the action
// <name> in github.com/mozership/probe-<name>.
const officialRepoPrefix = "github.com/mozership/probe-"

// OfficialAction is an external action published alongside Probe.
type OfficialAction struct {
	Name        string
	Description string
}

// Repository is where the action is published.
func (a OfficialAction) Repository() string {
	return officialRepoPrefix + a.Name
}

// officialActions are the external actions published alongside Probe,
// each in the repository its name gives. They are kept as a list rather
// than found among the repositories of mozership, where probe-action is a
// GitHub Action, not one of Probe, and so that they are known offline. No
// version is kept: a release of an action is found when it is read.
var officialActions = []OfficialAction{
	{"agent", "Test a web application from a task written in natural language, with a model that drives a browser"},
	{"android", "Drive an app on an Android emulator or device through adb"},
	{"browser", "Drive a browser with chromedp"},
	{"container", "Start a container, and remove it once the run is over"},
	{"github-notes", "Put the notes of a run on GitHub, in a pull request comment, the job summary or an issue"},
	{"graphql", "Send a GraphQL query over HTTP"},
	{"ios", "Drive an app on an iOS simulator through XCUITest"},
	{"jmap", "Call JMAP methods"},
	{"mail-latency", "Measure mail delivery latency from a Maildir"},
	{"redis", "Run commands on a Redis or Valkey server"},
	{"s3", "Read and write objects in S3 and S3-compatible storage"},
	{"slack", "Post a message to a Slack channel, of the notes of a run or a text"},
	{"websocket", "Exchange messages with a WebSocket server"},
}

// OfficialActions lists the external actions published alongside Probe.
func OfficialActions() []OfficialAction {
	return append([]OfficialAction(nil), officialActions...)
}

// DocReader reads the description of an external action, as
// actionref.ReadDoc does.
type DocReader func(uses, baseDir string) (*actionref.Doc, error)

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
	arg = strings.TrimSpace(arg)
	if workflow != "" && !isActionPath(arg) {
		names, baseDir, err := workflowActions(workflow)
		if err != nil {
			return "", err
		}
		if uses, ok := names[arg]; ok {
			return docPage(read(uses, baseDir))
		}
	}
	if isActionPath(arg) {
		return docPage(read(arg, ""))
	}

	page, err := Guide(arg)
	if err == nil {
		return page, nil
	}
	name := strings.TrimPrefix(strings.ToLower(arg), "actions/")
	for _, a := range officialActions {
		if a.Name == name {
			return docPage(read(a.Repository(), ""))
		}
	}
	return "", unknownGuide(arg)
}

// isActionPath reports whether arg names an external action by where it
// is, rather than by a name.
func isActionPath(arg string) bool {
	return strings.HasPrefix(arg, "./") || strings.HasPrefix(arg, "../") || filepath.IsAbs(arg) ||
		strings.HasPrefix(arg, "github.com/")
}

func unknownGuide(arg string) error {
	topic := strings.TrimPrefix(strings.ToLower(arg), "actions/")
	var names []string
	for _, t := range GuideTopics() {
		names = append(names, t.Name)
	}
	for _, a := range officialActions {
		names = append(names, a.Name)
	}
	if s := suggest.Closest(topic, names); s != "" {
		return fmt.Errorf("unknown guide topic: %s; did you mean %q?", topic, s)
	}
	return fmt.Errorf("unknown guide topic: %s (run probe guide to list the topics)", topic)
}

// workflowActions returns the names the workflow gives under actions, and
// the directory a local path among them is taken from.
func workflowActions(workflow string) (map[string]string, string, error) {
	files, err := yamlFiles(workflow)
	if err != nil {
		return nil, "", fmt.Errorf("workflow file %w", err)
	}
	if len(files) == 0 {
		return nil, "", fmt.Errorf("%s: no YAML file", workflow)
	}
	text, contents, err := readYamlFiles(files)
	if err != nil {
		return nil, "", fmt.Errorf("workflow file %w", err)
	}
	src, err := newSource(files, contents, text)
	if err != nil {
		return nil, "", fmt.Errorf("%s: %w", workflow, err)
	}
	var w struct {
		Actions map[string]string `yaml:"actions"`
	}
	// The files override one another as a run reads them, a key written
	// again taking the place of the first.
	if err := src.decode(&w, yaml.AllowDuplicateMapKey()); err != nil {
		return nil, "", fmt.Errorf("%s: %w", workflow, err)
	}
	abs, err := filepath.Abs(files[0])
	if err != nil {
		return nil, "", err
	}
	return w.Actions, filepath.Dir(abs), nil
}

// docPage writes d with the lines that tell where it comes from: that it is
// written by the authors of the action, not by Probe, and, for a remote
// action, the uses line that pins it.
func docPage(d *actionref.Doc, err error) (string, error) {
	if err != nil {
		return "", err
	}
	var b strings.Builder
	what := "its " + d.File
	if d.File != actionref.DocFile {
		what = "what its " + d.File + " declares, as it has no " + actionref.DocFile
	}
	if d.Local {
		fmt.Fprintf(&b, "> The local action in %s. What follows is %s, written by the authors of the action, not by Probe.\n", d.Uses, what)
	} else {
		repo, _, _ := strings.Cut(d.Uses, "@")
		at := "at the commit given"
		if d.Latest {
			at = "at its latest release"
		}
		fmt.Fprintf(&b, "> The action %s %s. What follows is %s at that commit, written by the authors of the action, not by Probe. Pin it as:\n", repo, at, what)
		b.WriteString("\n```yaml\nuses: " + d.Uses)
		if d.Version != "" {
			b.WriteString(" # " + d.Version)
		}
		b.WriteString("\n```\n")
	}
	if d.Warning != "" {
		fmt.Fprintf(&b, "\n> Warning: %s.\n", d.Warning)
	}
	b.WriteString("\n---\n\n")
	b.WriteString(d.Text)
	if !strings.HasSuffix(d.Text, "\n") {
		b.WriteString("\n")
	}
	return b.String(), nil
}
