package probe

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/goccy/go-yaml"
	"github.com/goccy/go-yaml/ast"
	"github.com/goccy/go-yaml/lexer"
	"github.com/goccy/go-yaml/parser"
)

// includeKey is the key of an entry of steps that stands for the steps of
// a file, which holds a list of them.
const includeKey = "include"

// source is a workflow as read: the text of the files given, one after
// another, parsed, with the steps of the file each entry of steps includes
// in place of the entry. The included files are numbered on past the text,
// each from a line of its own, so that a line of the source tells the file
// it is of and the line in it, as a line of the text does.
type source struct {
	file  *ast.File
	files fileLines
	// included is the line the first included file starts on; 0 when none
	// is included.
	included int
	// next is the line the next included file starts on.
	next int
	// problems are what kept an entry from including a file, by the line it
	// is found on. Such an entry is left out.
	problems []includeProblem
}

// includeProblem is what kept an entry from including a file, on a line of
// the source.
type includeProblem struct {
	line int
	msg  string
}

// newSource parses text, the files given one after another, whose contents
// are data, and includes the files the entries of the steps of its jobs
// name. The error is for a text that does not parse; what keeps an entry
// from including its file is told in problems.
func newSource(files []string, data [][]byte, text string) (*source, error) {
	f, err := parser.ParseBytes([]byte(text), 0, parser.AllowDuplicateMapKey())
	if err != nil {
		return nil, err
	}
	s := &source{
		file:  f,
		files: mapFiles(files, data),
		next:  strings.Count(text, "\n") + 2,
	}
	if len(f.Docs) > 0 {
		s.include(f.Docs[0].Body)
	}
	return s, nil
}

// body returns the top of the workflow, which is decoded; nil for a text
// that holds nothing.
func (s *source) body() ast.Node {
	if len(s.file.Docs) == 0 {
		return nil
	}
	return s.file.Docs[0].Body
}

// decode decodes the workflow into v. An error on a line of an included
// file is told by the file and its line, as the line of the source means
// nothing to whoever reads it.
func (s *source) decode(v any, opts ...yaml.DecodeOption) error {
	body := s.body()
	if body == nil {
		return errors.New("the workflow holds nothing")
	}
	err := yaml.NodeToValue(body, v, opts...)
	var yerr yaml.Error
	if s.included == 0 || !errors.As(err, &yerr) || yerr.GetToken() == nil {
		return err
	}
	pos := yerr.GetToken().Position
	if pos.Line < s.included {
		return err
	}
	file, line := s.files.at(pos.Line)
	return fmt.Errorf("%s:%d:%d: %s", file, line, pos.Column, yerr.GetMessage())
}

// problemError returns the first of problems as an error, by the file and
// the line it is found on.
func (s *source) problemError() error {
	if len(s.problems) == 0 {
		return nil
	}
	p := s.problems[0]
	file, line := s.files.at(p.line)
	return NewConfigurationError("include_steps", "failed to include steps", fmt.Errorf("%s:%d: %s", file, line, p.msg))
}

func (s *source) problem(line int, format string, args ...any) {
	s.problems = append(s.problems, includeProblem{line: line, msg: fmt.Sprintf(format, args...)})
}

// include puts in place of each entry of the steps of the jobs of body that
// includes a file the steps the file holds. Only the jobs that run are read:
// when more than one file given writes jobs, those of the last. The jobs
// stay in the workflow, so that what each needs is read in one place.
func (s *source) include(body ast.Node) {
	jobs := lastKey(body, "jobs")
	if jobs == nil {
		return
	}
	seq := sequence(resolve(body, jobs.Value))
	if seq == nil {
		return
	}

	// A path is told from the file that writes jobs, wherever the run is.
	file, _ := s.files.at(lineOf(jobs.Key))
	dir := filepath.Dir(file)
	// A list of steps anchored once and read by more than one job is
	// included in once.
	done := map[*ast.SequenceNode]bool{}
	var out []ast.Node
	for _, job := range seq.Values {
		if isInclude(job) {
			s.problem(lineOf(job), "include is an entry of steps, not of jobs: a job and what it needs are written in the workflow")
			continue
		}
		out = append(out, job)
		steps := stepsOf(body, job)
		if steps == nil {
			continue
		}
		list := sequence(resolve(body, steps.Value))
		if list == nil || done[list] {
			continue
		}
		done[list] = true
		s.expand(list, dir)
	}
	seq.Values = out
}

// expand puts in place of each entry of steps that includes a file the
// steps it holds, the path told from dir.
func (s *source) expand(steps *ast.SequenceNode, dir string) {
	var out []ast.Node
	for _, item := range steps.Values {
		path, ok := s.includePath(item)
		if !ok {
			out = append(out, item)
			continue
		}
		if path == "" {
			continue
		}
		line := lineOf(item)
		p, err := includeFile(dir, path)
		if err != nil {
			s.problem(line, "%s", err)
			continue
		}
		out = append(out, s.read(p, line)...)
	}
	steps.Values = out
}

// includePath returns the path the entry item of steps includes, and
// whether item includes a file at all. The path is empty for an entry that
// is wrong, which is told in problems.
func (s *source) includePath(item ast.Node) (string, bool) {
	kvs := pairs(item)
	i := slices.IndexFunc(kvs, func(kv *ast.MappingValueNode) bool { return keyOf(kv) == includeKey })
	if i < 0 {
		return "", false
	}
	if len(kvs) > 1 {
		var others []string
		for _, kv := range kvs {
			if k := keyOf(kv); k != includeKey {
				others = append(others, fmt.Sprintf("%q", k))
			}
		}
		s.problem(lineOf(item), "include takes no other key, but %s is written with it", listNames(others))
		return "", true
	}
	value := kvs[i].Value
	if tag, ok := value.(*ast.TagNode); ok {
		value = tag.Value
	}
	str, ok := value.(*ast.StringNode)
	if !ok || strings.TrimSpace(str.Value) == "" {
		s.problem(lineOf(kvs[i].Key), "include takes the path of a file")
		return "", true
	}
	return str.Value, true
}

// read parses the file at path, which the entry of steps on line includes,
// and returns the steps it holds.
func (s *source) read(path string, line int) []ast.Node {
	data, err := readFile(path)
	if err != nil {
		s.problem(line, "%s", err)
		return nil
	}
	start := s.next
	s.next += strings.Count(string(data), "\n") + 2
	s.files = append(s.files, fileStart{name: path, line: start})
	if s.included == 0 {
		s.included = start
	}

	// The lines of the file are numbered as those of the source, before
	// it is parsed, so that what the parser makes of them is too.
	tokens := lexer.Tokenize(string(data))
	for _, tk := range tokens {
		tk.Position.Line += start - 1
	}
	f, err := parser.Parse(tokens, 0, parser.AllowDuplicateMapKey())
	if err != nil {
		var yerr yaml.Error
		if errors.As(err, &yerr) && yerr.GetToken() != nil {
			s.problem(yerr.GetToken().Position.Line, "YAML: %s", yerr.GetMessage())
		} else {
			s.problem(line, "%s: YAML: %s", path, firstLine(err.Error()))
		}
		return nil
	}

	var docs []ast.Node
	for _, d := range f.Docs {
		if d.Body != nil {
			docs = append(docs, d.Body)
		}
	}
	switch {
	case len(docs) == 0:
		s.problem(line, "%s: holds no step", path)
		return nil
	case len(docs) > 1:
		s.problem(lineOf(docs[1]), "an included file holds a list of steps, not more than one document")
		return nil
	}
	steps := sequence(docs[0])
	if steps == nil {
		s.problem(line, "%s: an included file holds a list of steps", path)
		return nil
	}

	// The steps are decoded with the workflow, where an alias would be read
	// as an anchor of another file, so that a step read here could not be
	// read on its own.
	if alias := foreignAlias(docs[0]); alias != nil {
		s.problem(lineOf(alias), "alias *%s names no anchor of this file; an anchor is not shared between files", alias.Value.GetToken().Value)
		return nil
	}
	// The workflow is decoded as one, where an anchor would be read by an
	// alias of another file, and would take the place of one of the same
	// name in the aliases after it. Each anchor of the file, and the aliases
	// that name it, are given a name no YAML can write, of their own each
	// time the file is included.
	renameAnchors(docs[0], fmt.Sprintf(" %s:%d", path, start))

	var out []ast.Node
	for _, st := range steps.Values {
		if isInclude(st) {
			s.problem(lineOf(st), "an included file cannot include")
			continue
		}
		out = append(out, st)
	}
	return out
}

// includeFile returns the file path names, relative to dir unless it is
// absolute.
func includeFile(dir, path string) (string, error) {
	p := path
	if !filepath.IsAbs(p) {
		p = filepath.Join(dir, p)
	}
	info, err := os.Stat(p)
	if errors.Is(err, fs.ErrNotExist) {
		return "", fmt.Errorf("%s: no such file", path)
	}
	if err != nil {
		return "", err
	}
	if info.IsDir() {
		return "", fmt.Errorf("%s: is a directory; include takes a file, as the order of the steps is written", path)
	}
	if !isYamlFile(p) {
		return "", fmt.Errorf("%s: not a YAML file (.yml or .yaml)", path)
	}
	return p, nil
}

// foreignAlias returns the first alias of n that names no anchor written
// in n before it; nil when there is none.
func foreignAlias(n ast.Node) *ast.AliasNode {
	anchors := map[string]bool{}
	var found *ast.AliasNode
	ast.Walk(visitorFunc(func(n ast.Node) bool {
		switch v := n.(type) {
		case *ast.AnchorNode:
			if v.Name != nil && v.Name.GetToken() != nil {
				anchors[v.Name.GetToken().Value] = true
			}
		case *ast.AliasNode:
			if v.Value != nil && v.Value.GetToken() != nil && !anchors[v.Value.GetToken().Value] {
				found = v
			}
		}
		return found == nil
	}), n)
	return found
}

// renameAnchors adds suffix to the name of each anchor of n and of each
// alias that names one.
func renameAnchors(n ast.Node, suffix string) {
	rename := func(n ast.Node) {
		if str, ok := n.(*ast.StringNode); ok {
			str.Value += suffix
			if str.Token != nil {
				str.Token.Value += suffix
			}
		}
	}
	ast.Walk(visitorFunc(func(n ast.Node) bool {
		switch v := n.(type) {
		case *ast.AnchorNode:
			rename(v.Name)
		case *ast.AliasNode:
			rename(v.Value)
		}
		return true
	}), n)
}

// resolve returns n, or the node it names when it is an alias of an anchor
// of body.
func resolve(body, n ast.Node) ast.Node {
	if alias, ok := n.(*ast.AliasNode); ok && alias.Value != nil && alias.Value.GetToken() != nil {
		return anchored(body, alias.Value.GetToken().Value)
	}
	return n
}

// stepsOf returns the pair of job, an entry of jobs, that gives its steps:
// its own, or those a merge key gives it, the first of them as the decoder
// takes them; nil when there is none.
func stepsOf(body, job ast.Node) *ast.MappingValueNode {
	job = resolve(body, job)
	if kv := lastKey(job, "steps"); kv != nil {
		return kv
	}
	for _, kv := range pairs(job) {
		if kv.Key == nil || !kv.Key.IsMergeKey() {
			continue
		}
		sources := []ast.Node{kv.Value}
		if seq := sequence(kv.Value); seq != nil {
			sources = seq.Values
		}
		for _, src := range sources {
			if steps := stepsOf(body, src); steps != nil {
				return steps
			}
		}
	}
	return nil
}

// lastKey returns the last pair of the mapping n whose key is key, which is
// the one decoded; nil when there is none.
func lastKey(n ast.Node, key string) *ast.MappingValueNode {
	var found *ast.MappingValueNode
	for _, kv := range pairs(n) {
		if keyOf(kv) == key {
			found = kv
		}
	}
	return found
}

// anchored returns the node anchored as name in n, as the decoder reads an
// alias *name; nil when there is none. The decoder reads the anchors of the
// whole document before the aliases, so an alias reads the last anchor of
// its name, even one written after it.
func anchored(n ast.Node, name string) ast.Node {
	var found ast.Node
	ast.Walk(visitorFunc(func(n ast.Node) bool {
		if a, ok := n.(*ast.AnchorNode); ok && a.Name != nil && a.Name.GetToken() != nil && a.Name.GetToken().Value == name {
			found = a.Value
		}
		return true
	}), n)
	return found
}

// isInclude reports whether n, an entry of steps or of jobs, includes a
// file.
func isInclude(n ast.Node) bool {
	return slices.ContainsFunc(pairs(n), func(kv *ast.MappingValueNode) bool { return keyOf(kv) == includeKey })
}

// keyOf returns the key of kv as it is written.
func keyOf(kv *ast.MappingValueNode) string {
	if kv.Key == nil || kv.Key.GetToken() == nil {
		return ""
	}
	return kv.Key.GetToken().Value
}

// sequence returns n as a sequence, through an anchor or a tag; nil when it
// is not one.
func sequence(n ast.Node) *ast.SequenceNode {
	switch v := n.(type) {
	case *ast.SequenceNode:
		return v
	case *ast.AnchorNode:
		return sequence(v.Value)
	case *ast.TagNode:
		return sequence(v.Value)
	}
	return nil
}
