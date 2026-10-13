package main

import (
	"bytes"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/mozership/probe"
	"github.com/mozership/probe/yamlfmt"
)

const fmtUsage = "Usage: probe fmt [--check] <workflow-file>...\n"

// runFmt formats the workflow files given, in place, and prints the path of
// each it changed. With --check it changes none, and exits with 1 when one
// is not formatted. A file that is not YAML is an error, with 2, and the
// others are still formatted.
func (c *Cmd) runFmt() int {
	args := c.SubCommandArgs
	if len(args) == 0 {
		_, _ = fmt.Fprintf(c.errWriter, "[ERROR] workflow file is required\n")
		_, _ = fmt.Fprint(c.errWriter, fmtUsage)
		return probe.ExitConfigError
	}
	if len(args) == 1 && args[0] == "-" {
		return c.fmtStdin()
	}

	files, err := fmtFiles(args)
	if err != nil {
		_, _ = fmt.Fprintf(c.errWriter, "[ERROR] %v\n", err)
		return probe.ExitConfigError
	}

	code := probe.ExitOK
	for _, path := range files {
		changed, err := c.fmtFile(path)
		if err != nil {
			_, _ = fmt.Fprintf(c.errWriter, "[ERROR] %s: %v\n", path, firstLine(err.Error()))
			code = probe.ExitConfigError
			continue
		}
		if !changed {
			continue
		}
		_, _ = fmt.Fprintln(c.outWriter, path)
		if c.FmtCheck && code == probe.ExitOK {
			code = probe.ExitTestFailed
		}
	}
	return code
}

// fmtStdin formats the standard input to the standard output, for an editor
// to format what it holds. With --check it prints nothing.
func (c *Cmd) fmtStdin() int {
	src, err := io.ReadAll(c.inReader)
	if err != nil {
		_, _ = fmt.Fprintf(c.errWriter, "[ERROR] %v\n", err)
		return probe.ExitConfigError
	}
	out, err := yamlfmt.Format(src)
	if err != nil {
		_, _ = fmt.Fprintf(c.errWriter, "[ERROR] %v\n", firstLine(err.Error()))
		return probe.ExitConfigError
	}
	if c.FmtCheck {
		if !bytes.Equal(src, out) {
			return probe.ExitTestFailed
		}
		return probe.ExitOK
	}
	_, _ = c.outWriter.Write(out)
	return probe.ExitOK
}

// fmtFile formats the file at path, and tells whether it was not formatted.
// It is written only when it changes, and not with --check.
func (c *Cmd) fmtFile(path string) (bool, error) {
	info, err := os.Stat(path)
	if err != nil {
		return false, err
	}
	src, err := os.ReadFile(path)
	if err != nil {
		return false, err
	}
	out, err := yamlfmt.Format(src)
	if err != nil {
		return false, err
	}
	if bytes.Equal(src, out) {
		return false, nil
	}
	if c.FmtCheck {
		return true, nil
	}
	return true, os.WriteFile(path, out, info.Mode().Perm())
}

// fmtFiles returns the files the arguments name: a file as it is, whatever
// its name, and every YAML file under a directory, apart from those under a
// directory whose name starts with a dot.
func fmtFiles(args []string) ([]string, error) {
	var files []string
	for _, arg := range args {
		info, err := os.Stat(arg)
		if err != nil {
			return nil, fmt.Errorf("%s: no such file or directory", arg)
		}
		if !info.IsDir() {
			files = append(files, arg)
			continue
		}
		err = filepath.WalkDir(arg, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() {
				if path != arg && strings.HasPrefix(d.Name(), ".") {
					return filepath.SkipDir
				}
				return nil
			}
			if ext := filepath.Ext(path); ext == ".yml" || ext == ".yaml" {
				files = append(files, path)
			}
			return nil
		})
		if err != nil {
			return nil, err
		}
	}
	return files, nil
}

// firstLine is the first line of a message, which is what the parser says
// before it quotes the YAML.
func firstLine(s string) string {
	line, _, _ := strings.Cut(s, "\n")
	return line
}
