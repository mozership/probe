package thirdparty

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// module writes the files of a module to a directory of its own.
func module(t *testing.T, path string, files map[string]string) Module {
	t.Helper()
	dir := t.TempDir()
	for name, content := range files {
		p := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return Module{Path: path, Version: "v1.2.3", Dir: dir, Source: "https://pkg.go.dev/" + path + "@v1.2.3"}
}

func TestFiles(t *testing.T) {
	tests := []struct {
		name  string
		files map[string]string
		want  []string
	}{
		{"a license", map[string]string{"LICENSE": "x", "README.md": "x", "main.go": "x"}, []string{"LICENSE"}},
		{"other spellings and extensions", map[string]string{"LICENCE.txt": "x", "License.md": "x", "COPYING": "x", "UNLICENSE": "x"}, []string{"COPYING", "LICENCE.txt", "License.md", "UNLICENSE"}},
		{"several licenses", map[string]string{"LICENSE": "x", "LICENSE-SQLITE": "x", "LICENSE_APACHE": "x", "LICENSE-3RD-PARTY.md": "x"}, []string{"LICENSE", "LICENSE-3RD-PARTY.md", "LICENSE-SQLITE", "LICENSE_APACHE"}},
		{"what a license asks to be passed on", map[string]string{"LICENSE": "x", "NOTICE": "x", "NOTICE.txt": "x", "PATENTS": "x", "AUTHORS": "x", "COPYRIGHT": "x"}, []string{"AUTHORS", "COPYRIGHT", "LICENSE", "NOTICE", "NOTICE.txt", "PATENTS"}},
		{"a source file named as one", map[string]string{"LICENSE": "x", "notice.go": "x", "license_test.go": "x"}, []string{"LICENSE"}},
		{"a name that only starts as one", map[string]string{"LICENSE": "x", "licenses.json": "x", "NOTICES": "x", "CONTRIBUTORS": "x"}, []string{"LICENSE"}},
		{"a directory named as one", map[string]string{"LICENSE": "x", "licenses/MIT": "x", "NOTICE/a": "x"}, []string{"LICENSE"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			files, err := Files(module(t, "example.com/m", tt.files))
			if err != nil {
				t.Fatal(err)
			}
			var got []string
			for _, f := range files {
				got = append(got, f.Name)
			}
			if strings.Join(got, ",") != strings.Join(tt.want, ",") {
				t.Errorf("Files = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestFiles_Error(t *testing.T) {
	tests := []struct {
		name       string
		module     Module
		errContain string
	}{
		{"no license file", module(t, "example.com/none", map[string]string{"README.md": "x", "notice.go": "x"}), "example.com/none: no license file"},
		{"a module that is not downloaded", Module{Path: "example.com/missing"}, "example.com/missing: the module is not downloaded"},
		{"a directory that does not exist", Module{Path: "example.com/gone", Dir: filepath.Join(t.TempDir(), "gone")}, "example.com/gone: "},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := Files(tt.module)
			if err == nil || !strings.Contains(err.Error(), tt.errContain) {
				t.Errorf("Files error = %v, want one that contains %q", err, tt.errContain)
			}
		})
	}
}

func TestWrite(t *testing.T) {
	a := module(t, "example.com/a", map[string]string{"LICENSE": "MIT License  \r\n\r\nCopyright A\t\n\n\n", "NOTICE": "Notice of A"})
	b := module(t, "example.com/b", map[string]string{"COPYING": "BSD\nCopyright B\n"})

	var out bytes.Buffer
	if err := Write(&out, "probe", []Module{a, b}); err != nil {
		t.Fatal(err)
	}
	want := `Third-party licenses

The probe executable is built from the software below. Each is under
its own license, whose text follows the list, with where its source is
published.

- example.com/a v1.2.3
- example.com/b v1.2.3

================================================================================
example.com/a v1.2.3
https://pkg.go.dev/example.com/a@v1.2.3
================================================================================

--- LICENSE ---

MIT License

Copyright A

--- NOTICE ---

Notice of A

================================================================================
example.com/b v1.2.3
https://pkg.go.dev/example.com/b@v1.2.3
================================================================================

--- COPYING ---

BSD
Copyright B
`
	if got := out.String(); got != want {
		t.Errorf("Write wrote:\n%s\nwant:\n%s", got, want)
	}
}

func TestWrite_Error(t *testing.T) {
	a := module(t, "example.com/a", map[string]string{"LICENSE": "x"})
	b := module(t, "example.com/b", map[string]string{"README": "x"})
	c := module(t, "example.com/c", map[string]string{"main.go": "x"})

	var out bytes.Buffer
	err := Write(&out, "probe", []Module{a, b, c})
	if err == nil {
		t.Fatal("Write should fail for modules without a license file")
	}
	// Every such module is named, so that they are fixed in one go.
	for _, want := range []string{"example.com/b: no license file", "example.com/c: no license file"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error = %v, want it to contain %q", err, want)
		}
	}
	if out.Len() != 0 {
		t.Errorf("nothing should be written, got %q", out.String())
	}
}

func TestModules_Error(t *testing.T) {
	tests := []struct {
		name       string
		pattern    string
		platforms  []string
		errContain string
	}{
		{"a platform that is not os/arch", "./cmd/probe", []string{"linux"}, `platform must be os/arch, not "linux"`},
		{"a package that does not exist", "./cmd/none", []string{"linux/amd64"}, "go list"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := Modules("../..", tt.pattern, tt.platforms)
			if err == nil || !strings.Contains(err.Error(), tt.errContain) {
				t.Errorf("Modules error = %v, want one that contains %q", err, tt.errContain)
			}
		})
	}
}

// TestModules_Probe checks what a release does: every module the probe
// executable links, on every platform it is released for, has a license
// file to pass on. A dependency added or updated without one fails here,
// rather than when a release is made.
func TestModules_Probe(t *testing.T) {
	modules, err := Modules("../..", "./cmd/probe", Platforms)
	if err != nil {
		t.Fatal(err)
	}
	paths := map[string]bool{}
	for i, m := range modules {
		paths[m.Path] = true
		if i > 0 && modules[i-1].Path >= m.Path {
			t.Errorf("modules are not sorted: %s comes before %s", modules[i-1].Path, m.Path)
		}
		if m.Path == "github.com/mozership/probe" {
			t.Error("the main module is not a third party")
		}
	}
	for _, want := range []string{"Go runtime and standard library", "github.com/goccy/go-yaml", "golang.org/x/sys", "google.golang.org/grpc"} {
		if !paths[want] {
			t.Errorf("modules should include %s", want)
		}
	}

	var out bytes.Buffer
	if err := Write(&out, "probe", modules); err != nil {
		t.Fatal(err)
	}
	// The NOTICE of a module under the Apache License goes with it.
	if !strings.Contains(out.String(), "google.golang.org/grpc") || !strings.Contains(out.String(), "--- NOTICE.txt ---") {
		t.Error("the licenses should include the NOTICE.txt of google.golang.org/grpc")
	}
}

func TestExecutable(t *testing.T) {
	tests := []struct {
		name       string
		pattern    string
		want       string
		errContain string
	}{
		{"a command", "./cmd/probe", "probe", ""},
		{"a library", "./internal/thirdparty", "", "is package thirdparty, not a command"},
		{"several packages", "./internal/actions/...", "", "must be one package"},
		{"a package that does not exist", "./cmd/none", "", "go list"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := Executable("../..", tt.pattern)
			if tt.errContain != "" {
				if err == nil || !strings.Contains(err.Error(), tt.errContain) {
					t.Errorf("Executable error = %v, want one that contains %q", err, tt.errContain)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if got != tt.want {
				t.Errorf("Executable = %q, want %q", got, tt.want)
			}
		})
	}
}

// writeFiles writes files under dir, by their slash-separated names.
func writeFiles(t *testing.T, dir string, files map[string]string) {
	t.Helper()
	for name, content := range files {
		p := filepath.Join(dir, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
}

// goCmd runs go in dir, offline.
func goCmd(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("go", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GOFLAGS=-mod=mod", "GOPROXY=off", "GOWORK=off")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("go %v: %v: %s", args, err, out)
	}
}

// app writes a command, example.com/app, that imports example.com/dep from
// the directory next to it.
func app(t *testing.T) (string, string) {
	t.Helper()
	root := t.TempDir()
	writeFiles(t, root, map[string]string{
		"dep/go.mod":  "module example.com/dep\n\ngo 1.21\n",
		"dep/dep.go":  "package dep\n\nconst Name = \"dep\"\n",
		"dep/LICENSE": "License of dep\n",
		"app/go.mod":  "module example.com/app\n\ngo 1.21\n\nrequire example.com/dep v1.0.0\n\nreplace example.com/dep => ../dep\n",
		"app/main.go": "package main\n\nimport \"example.com/dep\"\n\nfunc main() { println(dep.Name) }\n",
		"app/LICENSE": "License of app\n",
	})
	return filepath.Join(root, "app"), filepath.Join(root, "dep")
}

func paths(modules []Module) map[string]Module {
	m := map[string]Module{}
	for _, mod := range modules {
		m[mod.Path] = mod
	}
	return m
}

func TestModules_Vendor(t *testing.T) {
	appDir, _ := app(t)
	goCmd(t, appDir, "mod", "vendor")
	t.Setenv("GOFLAGS", "")
	t.Setenv("GOWORK", "off")
	modules, err := Modules(appDir, ".", []string{"linux/amd64"})
	if err != nil {
		t.Fatal(err)
	}
	dep, ok := paths(modules)["example.com/dep"]
	if !ok {
		t.Fatalf("modules = %v, want example.com/dep", modules)
	}
	if files, err := Files(dep); err != nil || len(files) != 1 || files[0].Name != "LICENSE" {
		t.Errorf("Files(%+v) = %v, %v, want the LICENSE of the vendor directory", dep, files, err)
	}
	if _, ok := paths(modules)["example.com/app"]; ok {
		t.Error("the module of the command is not a third party")
	}
}

func TestModules_Workspace(t *testing.T) {
	appDir, depDir := app(t)
	writeFiles(t, appDir, map[string]string{"go.work": "go 1.21\n\nuse (\n\t.\n\t../dep\n)\n"})
	t.Setenv("GOFLAGS", "")
	t.Setenv("GOWORK", "")
	modules, err := Modules(appDir, ".", []string{"linux/amd64"})
	if err != nil {
		t.Fatal(err)
	}
	dep, ok := paths(modules)["example.com/dep"]
	if !ok {
		t.Fatalf("modules = %v, want example.com/dep, which the workspace uses", modules)
	}
	if want, _ := filepath.EvalSymlinks(depDir); dep.Dir != want || dep.Source != "https://pkg.go.dev/example.com/dep" {
		t.Errorf("dep = %+v", dep)
	}
	if _, ok := paths(modules)["example.com/app"]; ok {
		t.Error("the module of the command is not a third party")
	}
}

func TestExecutable_MajorVersion(t *testing.T) {
	dir := t.TempDir()
	writeFiles(t, dir, map[string]string{
		"go.mod":  "module example.com/app/v2\n\ngo 1.21\n",
		"main.go": "package main\n\nfunc main() {}\n",
	})
	t.Setenv("GOWORK", "off")
	got, err := Executable(dir, ".")
	if err != nil {
		t.Fatal(err)
	}
	if got != "app" {
		t.Errorf("Executable = %q, want app", got)
	}
}
