package actionref

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

var docManifest = `name: x
description: Do x
params: [a, b]
res: [code]
runs:
  using: binary
  url: https://github.com/o/r/releases/download/v1.2.0/x_{os}_{arch}
  checksums:
    linux_arm64: "` + strings.Repeat("a", 64) + `"
`

func newDocResolver(t *testing.T, g *github) *Resolver {
	return &Resolver{CacheDir: t.TempDir(), RawBaseURL: g.URL, APIBaseURL: g.URL, Token: "t0ken"}
}

func TestDocLatest(t *testing.T) {
	g := newGitHub(t)
	g.files["/repos/o/r/commits"] = []byte(`[{"sha":"` + sha + `"}]`)
	g.files["/repos/o/r/releases/latest"] = []byte(`{"tag_name":"v1.2.0"}`)
	g.files["/o/r/"+sha+"/action.yml"] = []byte(docManifest)
	g.files["/o/r/"+sha+"/README.md"] = []byte("# x\n\nDoes x.\n")

	d, err := newDocResolver(t, g).Doc("github.com/o/r", "")
	if err != nil {
		t.Fatal(err)
	}
	want := Doc{Uses: "github.com/o/r@" + sha, Version: "v1.2.0", Latest: true, File: DocFile, Text: "# x\n\nDoes x.\n"}
	if *d != want {
		t.Errorf("Doc = %+v, want %+v", *d, want)
	}
	if g.auth != "Bearer t0ken" {
		t.Errorf("Authorization = %q, want the token", g.auth)
	}
}

func TestDocLatestInADirectory(t *testing.T) {
	g := newGitHub(t)
	g.files["/repos/o/r/commits"] = []byte(`[{"sha":"` + sha + `"}]`)
	g.files["/o/r/"+sha+"/sub/action.yml"] = []byte(docManifest)
	g.files["/o/r/"+sha+"/sub/README.md"] = []byte("# x\n")

	d, err := newDocResolver(t, g).Doc("github.com/o/r/sub", "")
	if err != nil {
		t.Fatal(err)
	}
	if d.Uses != "github.com/o/r/sub@"+sha || d.Text != "# x\n" {
		t.Errorf("Doc = %+v", *d)
	}
	// No latest release is found, which warns of nothing.
	if d.Warning != "" {
		t.Errorf("Warning = %q, want none", d.Warning)
	}
}

func TestDocLatestWarnsOfAnotherRelease(t *testing.T) {
	g := newGitHub(t)
	g.files["/repos/o/r/commits"] = []byte(`[{"sha":"` + sha + `"}]`)
	g.files["/repos/o/r/releases/latest"] = []byte(`{"tag_name":"v1.3.0"}`)
	g.files["/o/r/"+sha+"/action.yml"] = []byte(docManifest)

	d, err := newDocResolver(t, g).Doc("github.com/o/r", "")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(d.Warning, "downloads v1.2.0, but the latest release is v1.3.0") {
		t.Errorf("Warning = %q", d.Warning)
	}
}

func TestDocLatestOfNoRepository(t *testing.T) {
	g := newGitHub(t)
	_, err := newDocResolver(t, g).Doc("github.com/o/none", "")
	if err == nil || err.Error() != "action github.com/o/none: no repository o/none on GitHub" {
		t.Errorf("error = %v", err)
	}

	g.files["/repos/o/r/commits"] = []byte(`[]`)
	_, err = newDocResolver(t, g).Doc("github.com/o/r", "")
	if err == nil || !strings.Contains(err.Error(), "no commit of the default branch has action.yml") {
		t.Errorf("error = %v", err)
	}
}

// TestDocPinnedIsCached checks that a commit once read is read again
// without GitHub, and that the API is not asked for a commit given.
func TestDocPinnedIsCached(t *testing.T) {
	g := newGitHub(t)
	g.files["/o/r/"+sha+"/action.yml"] = []byte(docManifest)
	g.files["/o/r/"+sha+"/README.md"] = []byte("# x\n")
	r := newDocResolver(t, g)

	d, err := r.Doc("github.com/o/r@"+sha, "")
	if err != nil {
		t.Fatal(err)
	}
	if d.Latest || d.Warning != "" || d.Uses != "github.com/o/r@"+sha {
		t.Errorf("Doc = %+v", *d)
	}
	if n := g.hit("/repos/o/r/commits"); n != 0 {
		t.Errorf("the API was asked %d times for a commit given", n)
	}

	g.Close()
	d, err = (&Resolver{CacheDir: r.CacheDir, RawBaseURL: g.URL}).Doc("github.com/o/r@"+sha, "")
	if err != nil {
		t.Fatalf("read from the cache: %v", err)
	}
	if d.Text != "# x\n" {
		t.Errorf("Text = %q", d.Text)
	}
}

// TestDocWithoutReadme checks that an action without a README is told by
// what its action.yml declares, and that a commit without one is read
// again from the cache.
func TestDocWithoutReadme(t *testing.T) {
	g := newGitHub(t)
	g.files["/o/r/"+sha+"/action.yml"] = []byte(docManifest)
	r := newDocResolver(t, g)

	d, err := r.Doc("github.com/o/r@"+sha, "")
	if err != nil {
		t.Fatal(err)
	}
	want := "# x\n\nDo x\n\n## Parameters\n\n- `a`\n- `b`\n\n## Response\n\n- `code`\n"
	if d.File != ManifestFile || d.Text != want {
		t.Errorf("Doc = %+v, want the text\n%s", *d, want)
	}

	g.Close()
	if _, err := (&Resolver{CacheDir: r.CacheDir, RawBaseURL: g.URL}).Doc("github.com/o/r@"+sha, ""); err != nil {
		t.Errorf("read from the cache: %v", err)
	}
}

func TestDocOfNoBinary(t *testing.T) {
	g := newGitHub(t)
	g.files["/o/r/"+sha+"/action.yml"] = []byte("name: x\nruns:\n  using: node24\n  main: index.js\n")
	_, err := newDocResolver(t, g).Doc("github.com/o/r@"+sha, "")
	if err == nil || !strings.Contains(err.Error(), `runs.using must be "binary"`) {
		t.Errorf("error = %v", err)
	}
}

func TestDocLocal(t *testing.T) {
	base := t.TempDir()
	dir := filepath.Join(base, "tools", "x")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	manifest := "name: x\ndescription: Do x\nruns:\n  using: binary\n  path: x\n"
	if err := os.WriteFile(filepath.Join(dir, ManifestFile), []byte(manifest), 0o644); err != nil {
		t.Fatal(err)
	}

	// A path is taken from baseDir, as a workflow's is.
	d, err := (&Resolver{}).Doc("./tools/x", base)
	if err != nil {
		t.Fatal(err)
	}
	if !d.Local || d.Uses != dir || d.File != ManifestFile || d.Text != "# x\n\nDo x\n" {
		t.Errorf("Doc = %+v", *d)
	}

	if err := os.WriteFile(filepath.Join(dir, DocFile), []byte("# x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Chdir(base)
	// Without baseDir, from the working directory.
	d, err = (&Resolver{}).Doc("./tools/x", "")
	if err != nil {
		t.Fatal(err)
	}
	if d.File != DocFile || d.Text != "# x\n" {
		t.Errorf("Doc = %+v", *d)
	}
}

func TestDownloadTag(t *testing.T) {
	for u, want := range map[string]string{
		"https://github.com/o/r/releases/download/v0.1.2/x_{os}_{arch}": "v0.1.2",
		"https://example.com/x": "",
	} {
		if got := downloadTag(u); got != want {
			t.Errorf("downloadTag(%q) = %q, want %q", u, got, want)
		}
	}
}
