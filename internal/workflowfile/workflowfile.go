// Package workflowfile finds the files a workflow is written in and reads
// them, as one text, in the order they are given: a later file overrides
// what an earlier one wrote.
package workflowfile

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// Paths resolves filePath, paths separated by commas, into the YAML (.yml and
// .yaml) files it names. A path is a file, a directory, whose YAML files are
// taken, or a glob. A file named on its own that is not YAML is refused, and
// one a directory or a glob finds is left out, as is a directory named as a
// YAML file.
func Paths(filePath string) ([]string, error) {
	var files []string

	for path := range strings.SplitSeq(filePath, ",") {
		path = strings.TrimSpace(path)

		if info, err := os.Stat(path); err == nil {
			if info.IsDir() {
				entries, err := os.ReadDir(path)
				if err != nil {
					return nil, err
				}
				for _, e := range entries {
					if f := filepath.Join(path, e.Name()); IsYAML(f) && !isDir(f) {
						files = append(files, f)
					}
				}
			} else if IsYAML(path) {
				files = append(files, path)
			} else {
				// A file named on its own is not left out without a word,
				// as one a directory or a glob finds is.
				return nil, fmt.Errorf("%s: not a YAML file (.yml or .yaml)", path)
			}
			continue
		}

		matches, err := filepath.Glob(path)
		if err != nil {
			return nil, err
		}
		if len(matches) == 0 {
			return nil, fmt.Errorf("%s: no such file or directory", path)
		}
		for _, m := range matches {
			if IsYAML(m) && !isDir(m) {
				files = append(files, m)
			}
		}
	}

	return files, nil
}

// isDir reports whether path is a directory, or a symlink to one. A
// directory named as a YAML file, which a directory or a glob may find, is
// left out, as it holds no workflow to read.
func isDir(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}

// IsYAML reports whether name has a .yml or .yaml extension.
func IsYAML(name string) bool {
	return strings.HasSuffix(name, ".yml") || strings.HasSuffix(name, ".yaml")
}

// Read reads the files at paths with readFile, and returns them as one text,
// and the content of each, in their order. A newline is put after a file
// that does not end in one, unless it is the last, so that its last line is
// not joined to the first of the next.
func Read(paths []string, readFile func(string) ([]byte, error)) (string, [][]byte, error) {
	var text strings.Builder
	contents := make([][]byte, 0, len(paths))

	for i, path := range paths {
		data, err := readFile(path)
		if err != nil {
			return "", nil, err
		}
		contents = append(contents, data)
		text.Write(data)

		if i < len(paths)-1 && len(data) > 0 && data[len(data)-1] != '\n' {
			text.WriteByte('\n')
		}
	}

	return text.String(), contents, nil
}

// Redefinition is a file that writes jobs at the top after an earlier file
// did. The files are read as one text, so its jobs replace the earlier
// ones, which do not run.
type Redefinition struct {
	File, Prev     string
	Line, PrevLine int
}

// String returns the redefinition as a warning on the line of File.
func (r Redefinition) String() string {
	return fmt.Sprintf("%s:%d: %s", r.File, r.Line, r.Message())
}

// Message tells what the redefinition does, without where it is.
func (r Redefinition) Message() string {
	return fmt.Sprintf("jobs replaces the jobs of %s:%d, which do not run", r.Prev, r.PrevLine)
}

// topJobsKey is jobs as a key of the top mapping, which no other line
// starts with: what a key at the top holds is indented.
var topJobsKey = regexp.MustCompile(`^(?:jobs|"jobs"|'jobs')[ \t]*:(?:[ \t]|$)`)

// RedefinedJobs returns each of files, whose contents are data, that writes
// jobs at the top after an earlier file did, with the one it replaces. Only
// jobs is told of: a file that replaces vars, say, is how a workflow is run
// in other environments.
func RedefinedJobs(files []string, data [][]byte) []Redefinition {
	var out []Redefinition
	prev, prevLine := "", 0
	for k, f := range files {
		line := 0
		for i, l := range strings.Split(string(data[k]), "\n") {
			if topJobsKey.MatchString(strings.TrimSuffix(l, "\r")) {
				line = i + 1
			}
		}
		if line == 0 {
			continue
		}
		if prev != "" {
			out = append(out, Redefinition{File: f, Line: line, Prev: prev, PrevLine: prevLine})
		}
		prev, prevLine = f, line
	}
	return out
}
