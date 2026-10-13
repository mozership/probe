// Package thirdparty collects the licenses of what is built into the probe
// executable: the modules it links, and the Go standard library. Most of
// them ask that their copyright notice and license go with a copy in binary
// form, which a release archive is, so the archive carries them in one file.
package thirdparty

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// Module is a module built into the executable.
type Module struct {
	Path    string
	Version string
	Dir     string // where its files are
	Source  string // where its source is published
}

// File is a license or notice file of a module.
type File struct {
	Name    string
	Content []byte
}

// noticeFile matches the files of a module that hold its license, its
// copyright notices, or what its license asks to be passed on with them,
// such as the NOTICE of the Apache License.
var noticeFile = regexp.MustCompile(`(?i)^(licen[sc]e|copying|unlicense|notice|patents|copyright|authors)([-_.].*)?$`)

// Modules returns the modules that the package at pattern links when it is
// built for each of the platforms, given as os/arch, apart from the main
// module, and the Go standard library, which every build links. They are
// sorted by path.
func Modules(dir, pattern string, platforms []string) ([]Module, error) {
	seen := map[string]Module{}
	for _, platform := range platforms {
		goos, goarch, ok := strings.Cut(platform, "/")
		if !ok {
			return nil, fmt.Errorf("platform must be os/arch, not %q", platform)
		}
		cmd := exec.Command("go", "list", "-deps", "-f", "{{with .Module}}{{if not .Main}}{{.Path}}\t{{.Version}}\t{{.Dir}}{{end}}{{end}}", pattern)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(), "GOOS="+goos, "GOARCH="+goarch, "CGO_ENABLED=0")
		var stderr bytes.Buffer
		cmd.Stderr = &stderr
		out, err := cmd.Output()
		if err != nil {
			return nil, fmt.Errorf("go list for %s: %w: %s", platform, err, strings.TrimSpace(stderr.String()))
		}
		sc := bufio.NewScanner(bytes.NewReader(out))
		for sc.Scan() {
			fields := strings.Split(sc.Text(), "\t")
			if len(fields) != 3 {
				continue
			}
			seen[fields[0]] = Module{
				Path:    fields[0],
				Version: fields[1],
				Dir:     fields[2],
				Source:  "https://pkg.go.dev/" + fields[0] + "@" + fields[1],
			}
		}
	}

	std, err := standardLibrary(dir)
	if err != nil {
		return nil, err
	}
	modules := []Module{std}
	for _, m := range seen {
		modules = append(modules, m)
	}
	sort.Slice(modules, func(i, j int) bool { return modules[i].Path < modules[j].Path })
	return modules, nil
}

// standardLibrary is the Go distribution that builds the executable, whose
// runtime and standard library are part of it.
func standardLibrary(dir string) (Module, error) {
	cmd := exec.Command("go", "env", "GOROOT", "GOVERSION")
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		return Module{}, fmt.Errorf("go env: %w", err)
	}
	fields := strings.Fields(string(out))
	if len(fields) != 2 {
		return Module{}, fmt.Errorf("go env: unexpected output %q", out)
	}
	return Module{Path: "Go runtime and standard library", Version: fields[1], Dir: fields[0], Source: "https://go.dev/dl/"}, nil
}

// Files returns the license and notice files at the top of a module, sorted
// by name. A module without one is an error: its license cannot be passed
// on, and has to be looked at by hand.
func Files(m Module) ([]File, error) {
	if m.Dir == "" {
		return nil, fmt.Errorf("%s: the module is not downloaded; run go mod download", m.Path)
	}
	entries, err := os.ReadDir(m.Dir)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", m.Path, err)
	}
	var files []File
	for _, e := range entries {
		// A source file may be named as one, such as notice.go.
		if !e.Type().IsRegular() || !noticeFile.MatchString(e.Name()) || strings.HasSuffix(e.Name(), ".go") {
			continue
		}
		content, err := os.ReadFile(filepath.Join(m.Dir, e.Name()))
		if err != nil {
			return nil, fmt.Errorf("%s: %w", m.Path, err)
		}
		files = append(files, File{Name: e.Name(), Content: content})
	}
	if len(files) == 0 {
		return nil, fmt.Errorf("%s: no license file in %s", m.Path, m.Dir)
	}
	return files, nil
}

const rule = "================================================================================\n"

// Write writes the licenses of the modules to w: a list of the modules,
// and then, for each, where its source is and the text of its files. It
// writes nothing for the modules when any of them has no license file, and
// the error names every such module.
func Write(w io.Writer, modules []Module) error {
	files := make([][]File, len(modules))
	var errs []error
	for i, m := range modules {
		f, err := Files(m)
		if err != nil {
			errs = append(errs, err)
		}
		files[i] = f
	}
	if err := errors.Join(errs...); err != nil {
		return err
	}

	// A write to a buffer does not fail.
	var b bytes.Buffer
	b.WriteString("Third-party licenses\n\n")
	b.WriteString("The probe executable is built from the software below. Each is under its own\n")
	b.WriteString("license, whose text follows the list, with where its source is published.\n\n")
	for _, m := range modules {
		b.WriteString("- " + m.Path + " " + m.Version + "\n")
	}
	for i, m := range modules {
		b.WriteString("\n" + rule + m.Path + " " + m.Version + "\n" + m.Source + "\n" + rule)
		for _, f := range files[i] {
			b.WriteString("\n--- " + f.Name + " ---\n\n")
			content := bytes.TrimRight(bytes.ReplaceAll(f.Content, []byte("\r\n"), []byte("\n")), " \t\n")
			for line := range bytes.SplitSeq(content, []byte("\n")) {
				b.Write(bytes.TrimRight(line, " \t"))
				b.WriteByte('\n')
			}
		}
	}
	_, err := w.Write(b.Bytes())
	return err
}
