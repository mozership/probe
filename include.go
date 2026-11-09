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
	"github.com/goccy/go-yaml/parser"
)

// includeKey is the key of an entry of jobs that stands for the jobs of
// other files: a file, the YAML files of a directory, or those a glob
// matches, each of which holds a job or a list of jobs.
const includeKey = "include"

// source is a workflow as read: the text of the files given, one after
// another, parsed, with the jobs of the files each entry of jobs includes in
// place of the entry. The included files are numbered on past the text, each
// from a line of its own, so that a line of the source tells the file it is
// of and the line in it, as a line of the text does.
type source struct {
	file  *ast.File
	files fileLines
	// included is the line the first included file starts on; 0 when none
	// is included.
	included int
	// next is the line the next included file starts on.
	next int
	// problems are what kept an entry of jobs from including its files, by
	// the line it is found on. Such an entry is left out of jobs.
	problems []includeProblem
}

// includeProblem is what kept an entry of jobs from including its files,
// on a line of the source.
type includeProblem struct {
	line int
	msg  string
}

// newSource parses text, the files given one after another, whose contents
// are data, and includes the files the entries of its jobs name. The error
// is for a text that does not parse; what keeps an entry from including its
// files is told in problems.
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
		s.include(f.Docs[0].Body, files)
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
	return NewConfigurationError("include_jobs", "failed to include jobs", fmt.Errorf("%s:%d: %s", file, line, p.msg))
}

func (s *source) problem(line int, format string, args ...any) {
	s.problems = append(s.problems, includeProblem{line: line, msg: fmt.Sprintf(format, args...)})
}

// include puts in place of each entry of the jobs of body that includes
// files the jobs those files hold. Only the jobs that run are read: when
// more than one file given writes jobs, those of the last. given are the
// files of the workflow, which none of jobs can be.
func (s *source) include(body ast.Node, given []string) {
	var jobs *ast.MappingValueNode
	for _, kv := range pairs(body) {
		if keyOf(kv) == "jobs" {
			jobs = kv
		}
	}
	if jobs == nil {
		return
	}
	seq := sequence(jobs.Value)
	if seq == nil {
		return
	}

	// A path is told from the file that writes jobs, wherever the run is.
	file, _ := s.files.at(lineOf(jobs.Key))
	dir := filepath.Dir(file)
	seen := map[string]int{}
	for _, g := range given {
		if abs, err := filepath.Abs(g); err == nil {
			seen[abs] = 0
		}
	}

	var out []ast.Node
	for _, item := range seq.Values {
		path, ok := s.includePath(item)
		if !ok {
			out = append(out, item)
			continue
		}
		if path == "" {
			continue
		}
		line := lineOf(item)
		paths, err := includeFiles(dir, path)
		if err != nil {
			s.problem(line, "%s", err)
			continue
		}
		for _, p := range paths {
			abs, err := filepath.Abs(p)
			if err != nil {
				s.problem(line, "%s: %s", p, err)
				continue
			}
			if at, ok := seen[abs]; ok {
				if at == 0 {
					s.problem(line, "%s: a file of the workflow cannot be included", p)
				} else {
					f, l := s.files.at(at)
					s.problem(line, "%s: included already, at %s:%d", p, f, l)
				}
				continue
			}
			seen[abs] = line
			out = append(out, s.read(p, line)...)
		}
	}
	seq.Values = out
}

// includePath returns the path the entry item of jobs includes, and
// whether item includes files at all. The path is empty for an entry that
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
	str, ok := kvs[i].Value.(*ast.StringNode)
	if !ok || strings.TrimSpace(str.Value) == "" {
		s.problem(lineOf(kvs[i].Key), "include takes the path of a file, a directory or a glob")
		return "", true
	}
	return str.Value, true
}

// read parses the file at path, which the entry of jobs on line includes,
// and returns the jobs it holds: the file itself, a job, or each entry of
// it, a list of jobs.
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

	// The lines before the file are left blank, so that its lines are
	// those of the source.
	text := strings.Repeat("\n", start-1) + string(data)
	f, err := parser.ParseBytes([]byte(text), 0, parser.AllowDuplicateMapKey())
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
		s.problem(line, "%s: holds no job", path)
		return nil
	case len(docs) > 1:
		s.problem(lineOf(docs[1]), "an included file holds a job or a list of jobs, not more than one document")
		return nil
	}

	// The jobs are decoded with the workflow, where an alias would be
	// read as an anchor of another file, so that a job read here could not
	// be read on its own, as the embedded action reads it.
	if alias := foreignAlias(docs[0]); alias != nil {
		s.problem(lineOf(alias), "alias *%s names no anchor of this file; an anchor is not shared between files", alias.Value.GetToken().Value)
		return nil
	}

	jobs := []ast.Node{docs[0]}
	if seq := sequence(docs[0]); seq != nil {
		jobs = seq.Values
	} else if slices.ContainsFunc(pairs(docs[0]), func(kv *ast.MappingValueNode) bool { return keyOf(kv) == "jobs" }) {
		s.problem(line, "%s: is a workflow; an included file holds a job or a list of jobs", path)
		return nil
	}
	var out []ast.Node
	for _, job := range jobs {
		if isInclude(job) {
			s.problem(lineOf(job), "an included file cannot include")
			continue
		}
		out = append(out, job)
	}
	return out
}

// includeFiles returns the files path names, relative to dir unless it is
// absolute: the file, the YAML files of the directory, or the YAML files
// the glob matches, in the order of their names.
func includeFiles(dir, path string) ([]string, error) {
	p := path
	if !filepath.IsAbs(p) {
		p = filepath.Join(dir, p)
	}

	if !strings.ContainsAny(path, "*?[") {
		info, err := os.Stat(p)
		if errors.Is(err, fs.ErrNotExist) {
			return nil, fmt.Errorf("%s: no such file or directory", path)
		}
		if err != nil {
			return nil, err
		}
		if !info.IsDir() {
			if !isYamlFile(p) {
				return nil, fmt.Errorf("%s: not a YAML file (.yml or .yaml)", path)
			}
			return []string{p}, nil
		}
		entries, err := os.ReadDir(p)
		if err != nil {
			return nil, err
		}
		var files []string
		for _, e := range entries {
			if !e.IsDir() && isYamlFile(e.Name()) {
				files = append(files, filepath.Join(p, e.Name()))
			}
		}
		if len(files) == 0 {
			return nil, fmt.Errorf("%s: no YAML file in the directory", path)
		}
		return files, nil
	}

	matches, err := filepath.Glob(p)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	var files []string
	for _, m := range matches {
		if info, err := os.Stat(m); err == nil && !info.IsDir() && isYamlFile(m) {
			files = append(files, m)
		}
	}
	if len(files) == 0 {
		return nil, fmt.Errorf("%s: no YAML file matches", path)
	}
	slices.Sort(files)
	return files, nil
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

// isInclude reports whether n, an entry of jobs, includes files.
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
