package thirdparty

import (
	"bytes"
	"os"
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
	if err := Write(&out, []Module{a, b}); err != nil {
		t.Fatal(err)
	}
	want := `Third-party licenses

The probe executable is built from the software below. Each is under its own
license, whose text follows the list, with where its source is published.

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
	err := Write(&out, []Module{a, b, c})
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
		{"a package that does not exist", "./cmd/none", []string{"linux/amd64"}, "go list for linux/amd64"},
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
	modules, err := Modules("../..", "./cmd/probe", []string{"linux/amd64", "linux/arm64", "darwin/amd64", "darwin/arm64"})
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
	if err := Write(&out, modules); err != nil {
		t.Fatal(err)
	}
	// The NOTICE of a module under the Apache License goes with it.
	if !strings.Contains(out.String(), "google.golang.org/grpc") || !strings.Contains(out.String(), "--- NOTICE.txt ---") {
		t.Error("the licenses should include the NOTICE.txt of google.golang.org/grpc")
	}
}
