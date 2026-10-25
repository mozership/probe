package probe

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// readFile reads a file of the workflow. It is os.ReadFile but in the tests
// that count what is read.
var readFile = os.ReadFile

// readYamlFiles returns the files at paths as one text, and the content of
// each, in their order.
func readYamlFiles(paths []string) (string, [][]byte, error) {
	var y strings.Builder
	contents := make([][]byte, 0, len(paths))

	for i, path := range paths {
		data, err := readFile(path)
		if err != nil {
			return "", nil, err
		}
		contents = append(contents, data)
		y.Write(data)

		// Add newline between files to prevent concatenation issues
		if i < len(paths)-1 && len(data) > 0 && data[len(data)-1] != '\n' {
			y.WriteByte('\n')
		}
	}

	return y.String(), contents, nil
}

// jobsRedefinition is a file that writes jobs at the top after an earlier
// file did. The files are read as one text, so its jobs replace the earlier
// ones, which do not run.
type jobsRedefinition struct {
	File, Prev     string
	Line, PrevLine int
}

func (r jobsRedefinition) String() string {
	return fmt.Sprintf("%s:%d: %s", r.File, r.Line, r.message())
}

func (r jobsRedefinition) message() string {
	return fmt.Sprintf("jobs replaces the jobs of %s:%d, which do not run", r.Prev, r.PrevLine)
}

// topJobsKey is jobs as a key of the top mapping, which no other line
// starts with: what a key at the top holds is indented.
var topJobsKey = regexp.MustCompile(`^(?:jobs|"jobs"|'jobs')[ \t]*:(?:[ \t]|$)`)

// redefinedJobs returns each of files, whose contents are data, that writes
// jobs at the top after an earlier file did, with the one it replaces. Only
// jobs is told of: a file that replaces vars, say, is how a workflow is run
// in other environments.
func redefinedJobs(files []string, data [][]byte) []jobsRedefinition {
	var out []jobsRedefinition
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
			out = append(out, jobsRedefinition{File: f, Line: line, Prev: prev, PrevLine: prevLine})
		}
		prev, prevLine = f, line
	}
	return out
}

// yamlFiles resolves filePath, paths separated by commas, and returns a list of YAML (.yml and .yaml) files
func yamlFiles(filePath string) ([]string, error) {
	var files []string
	paths := strings.SplitSeq(filePath, ",")

	for path := range paths {
		path = strings.TrimSpace(path)

		// Check if it's a single file or a directory
		if info, err := os.Stat(path); err == nil {
			if info.IsDir() {
				// Read all YAML files from the directory
				rfiles, err := os.ReadDir(path)
				if err != nil {
					return nil, err
				}
				for _, rf := range rfiles {
					if !rf.IsDir() && isYamlFile(rf.Name()) {
						files = append(files, filepath.Join(path, rf.Name()))
					}
				}
			} else if isYamlFile(path) {
				// Single file path
				files = append(files, path)
			} else {
				// A file named on its own is not left out without a word,
				// as one a directory or a glob finds is.
				return nil, fmt.Errorf("%s: not a YAML file (.yml or .yaml)", path)
			}
		} else {
			// Handle wildcard pattern
			matches, err := filepath.Glob(path)
			if err != nil {
				return nil, err
			}
			if len(matches) == 0 {
				return nil, fmt.Errorf("%s: no such file or directory", path)
			}
			for _, match := range matches {
				if isYamlFile(match) {
					files = append(files, match)
				}
			}
		}
	}

	return files, nil
}

// isYamlFile checks if the filename has a .yml or .yaml extension
func isYamlFile(f string) bool {
	return strings.HasSuffix(f, ".yml") || strings.HasSuffix(f, ".yaml")
}
