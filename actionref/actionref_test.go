package actionref

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
)

const sha = "0123456789abcdef0123456789abcdef01234567"

func TestIsExternal(t *testing.T) {
	tests := map[string]bool{
		"http":                          false,
		"mail-latency":                  false,
		"./actions/foo":                 true,
		"github.com/linyows/foo@" + sha: true,
	}
	for uses, want := range tests {
		if got := IsExternal(uses); got != want {
			t.Errorf("IsExternal(%q) = %v, want %v", uses, got, want)
		}
	}
}

func TestParse(t *testing.T) {
	tests := []struct {
		uses    string
		want    Ref
		wantErr string
	}{
		{uses: "./actions/foo", want: Ref{Local: "./actions/foo"}},
		{uses: "../foo", want: Ref{Local: "../foo"}},
		{uses: "/opt/foo", want: Ref{Local: "/opt/foo"}},
		{uses: "github.com/mozership/probe-graphql@" + sha, want: Ref{Owner: "mozership", Repo: "probe-graphql", SHA: sha}},
		{uses: "github.com/o/r/a/b@" + sha, want: Ref{Owner: "o", Repo: "r", Dir: "a/b", SHA: sha}},
		{uses: "gitlab.com/o/r@" + sha, wantErr: "unsupported action"},
		{uses: "actions/foo", wantErr: "unsupported action"},
		{uses: "github.com/o/r", wantErr: "must be pinned to a commit"},
		{uses: "github.com/o/r@", wantErr: "must be pinned to a commit"},
		{uses: "github.com/o/r@v1.0.0", wantErr: "full 40-character commit SHA"},
		{uses: "github.com/o/r@0123456", wantErr: "full 40-character commit SHA"},
		{uses: "github.com/o/r@" + strings.ToUpper(sha), wantErr: "full 40-character commit SHA"},
		{uses: "github.com/o@" + sha, wantErr: "must name a repository"},
		{uses: "github.com/o/r/../x@" + sha, wantErr: "invalid path element"},
		{uses: "github.com/o//r@" + sha, wantErr: "invalid path element"},
	}
	for _, tt := range tests {
		t.Run(tt.uses, func(t *testing.T) {
			got, err := Parse(tt.uses)
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("Parse(%q) error = %v, want one containing %q", tt.uses, err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("Parse(%q) error = %v", tt.uses, err)
			}
			if got != tt.want {
				t.Errorf("Parse(%q) = %+v, want %+v", tt.uses, got, tt.want)
			}
		})
	}
}

func TestParseManifest(t *testing.T) {
	sum := strings.Repeat("a", 64)
	tests := []struct {
		name    string
		yml     string
		wantErr string
	}{
		{name: "url", yml: "runs:\n  using: binary\n  url: https://example.com/x_{os}_{arch}\n  checksums:\n    linux_amd64: " + sum + "\n"},
		{name: "path", yml: "runs:\n  using: binary\n  path: bin/x\n"},
		{name: "no using", yml: "runs:\n  url: https://example.com/x\n", wantErr: "runs.using must be"},
		{name: "go", yml: "runs:\n  using: go\n  path: ./cmd/x\n", wantErr: "runs.using must be"},
		{name: "neither", yml: "runs:\n  using: binary\n", wantErr: "exactly one of url and path"},
		{name: "both", yml: "runs:\n  using: binary\n  url: https://example.com/x\n  path: x\n", wantErr: "exactly one of url and path"},
		{name: "bad checksum", yml: "runs:\n  using: binary\n  url: https://example.com/x\n  checksums:\n    linux_amd64: abc\n", wantErr: "runs.checksums.linux_amd64"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := ParseManifest([]byte(tt.yml))
			if tt.wantErr == "" {
				if err != nil {
					t.Fatalf("ParseManifest() error = %v", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("ParseManifest() error = %v, want one containing %q", err, tt.wantErr)
			}
		})
	}
}

func TestParseManifestDeclarations(t *testing.T) {
	m, err := ParseManifest([]byte("runs:\n  using: binary\n  path: x\nguard: [read-only, allow-host]\nparams: [url, calls]\n"))
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(m.Guard, []string{"read-only", "allow-host"}) || !reflect.DeepEqual(m.Params, []string{"url", "calls"}) {
		t.Errorf("Guard = %v, Params = %v", m.Guard, m.Params)
	}

	// An action.yml that says nothing declares no guard, and leaves the keys
	// of with unchecked; one with an empty list takes no key.
	m, err = ParseManifest([]byte("runs:\n  using: binary\n  path: x\n"))
	if err != nil {
		t.Fatal(err)
	}
	if m.Guard != nil || m.Params != nil {
		t.Errorf("Guard = %v, Params = %v, want both nil", m.Guard, m.Params)
	}
	// Nor does it ask for a cleanup.
	if m.Cleanup {
		t.Error("Cleanup = true, want false")
	}
	m, err = ParseManifest([]byte("runs:\n  using: binary\n  path: x\ncleanup: true\n"))
	if err != nil {
		t.Fatal(err)
	}
	if !m.Cleanup {
		t.Error("Cleanup = false, want true")
	}
	if _, err := ParseManifest([]byte("runs:\n  using: binary\n  path: x\ncleanup: [a]\n")); err == nil {
		t.Error("a cleanup that is not a boolean should fail")
	}
	m, err = ParseManifest([]byte("runs:\n  using: binary\n  path: x\nparams: []\n"))
	if err != nil {
		t.Fatal(err)
	}
	if m.Params == nil || len(m.Params) != 0 {
		t.Errorf("Params = %#v, want an empty list", m.Params)
	}
}

// github serves an action.yml from a fake raw.githubusercontent.com and the
// binaries it points to, and counts the requests for each path.
type github struct {
	*httptest.Server
	mu    sync.Mutex
	files map[string][]byte
	hits  map[string]int
}

func newGitHub(t *testing.T) *github {
	g := &github{files: map[string][]byte{}, hits: map[string]int{}}
	g.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		g.mu.Lock()
		defer g.mu.Unlock()
		g.hits[r.URL.Path]++
		data, ok := g.files[r.URL.Path]
		if !ok {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write(data)
	}))
	t.Cleanup(g.Close)
	return g
}

func (g *github) hit(path string) int {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.hits[path]
}

func digest(b []byte) string {
	s := sha256.Sum256(b)
	return hex.EncodeToString(s[:])
}

func newResolver(t *testing.T, g *github) *Resolver {
	return &Resolver{CacheDir: t.TempDir(), RawBaseURL: g.URL, OS: "linux", Arch: "arm64"}
}

func TestResolveRemote(t *testing.T) {
	g := newGitHub(t)
	bin := []byte("#!/bin/sh\necho linux arm64\n")
	g.files["/bin/x_linux_arm64"] = bin
	g.files["/o/r/"+sha+"/sub/action.yml"] = fmt.Appendf(nil, `name: x
runs:
  using: binary
  url: %s/bin/x_{os}_{arch}
  checksums:
    linux_arm64: %s
    darwin_arm64: %s
`, g.URL, digest(bin), strings.Repeat("f", 64))

	r := newResolver(t, g)
	uses := "github.com/o/r/sub@" + sha
	exe, err := r.Resolve(uses, "")
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}

	got, err := os.ReadFile(exe.Path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, bin) {
		t.Errorf("executable = %q, want %q", got, bin)
	}
	if hex.EncodeToString(exe.SHA256) != digest(bin) {
		t.Errorf("SHA256 = %x, want %s", exe.SHA256, digest(bin))
	}
	if info, err := os.Stat(exe.Path); err != nil || info.Mode().Perm()&0o100 == 0 {
		t.Errorf("executable is not executable: %v %v", info.Mode(), err)
	}

	// A second resolver sharing the cache takes the executable from it, but
	// reads action.yml again: only the copy at the pinned commit is trusted.
	r2 := &Resolver{CacheDir: r.CacheDir, RawBaseURL: g.URL, OS: "linux", Arch: "arm64"}
	if _, err := r2.Resolve(uses, ""); err != nil {
		t.Fatalf("Resolve() from cache error = %v", err)
	}
	if n := g.hit("/o/r/" + sha + "/sub/action.yml"); n != 2 {
		t.Errorf("action.yml fetched %d times, want 2", n)
	}
	if n := g.hit("/bin/x_linux_arm64"); n != 1 {
		t.Errorf("binary fetched %d times, want 1", n)
	}

	// Nothing but executables, named by their digest, is kept in the cache.
	err = filepath.WalkDir(r.CacheDir, func(p string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		if want := filepath.Join(r.CacheDir, "sha256", digest(bin)); p != want {
			t.Errorf("cache holds %s, want only %s", p, want)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

// Once action.yml at the pinned commit changes what it points at, the
// executable it now names runs, whatever an earlier run left in the cache.
// TestManifestFetchesNoExecutable checks that what an action declares can
// be read without its executable being downloaded, and that Resolve then
// reads action.yml no more.
func TestManifestFetchesNoExecutable(t *testing.T) {
	g := newGitHub(t)
	bin := []byte("#!/bin/sh\n")
	g.files["/bin/x"] = bin
	manifest := "/o/r/" + sha + "/action.yml"
	g.files[manifest] = fmt.Appendf(nil, "runs:\n  using: binary\n  url: %s/bin/x\n  checksums:\n    linux_arm64: %s\nguard: [read-only]\n", g.URL, digest(bin))

	r := newResolver(t, g)
	uses := "github.com/o/r@" + sha
	m, err := r.Manifest(uses, "")
	if err != nil {
		t.Fatalf("Manifest() error = %v", err)
	}
	if !reflect.DeepEqual(m.Guard, []string{"read-only"}) {
		t.Errorf("Guard = %v", m.Guard)
	}
	if n := g.hit("/bin/x"); n != 0 {
		t.Errorf("the executable was fetched %d times, want none", n)
	}
	if _, err := r.Resolve(uses, ""); err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}
	if n := g.hit(manifest); n != 1 {
		t.Errorf("action.yml was fetched %d times, want once", n)
	}
	if n := g.hit("/bin/x"); n != 1 {
		t.Errorf("the executable was fetched %d times, want once", n)
	}
}

func TestManifestRetriesAfterFailure(t *testing.T) {
	g := newGitHub(t)
	r := newResolver(t, g)
	uses := "github.com/o/r@" + sha
	if _, err := r.Manifest(uses, ""); err == nil || !strings.Contains(err.Error(), "action "+uses) {
		t.Fatalf("Manifest() error = %v, want one naming the action", err)
	}
	g.mu.Lock()
	g.files["/o/r/"+sha+"/action.yml"] = []byte("runs:\n  using: binary\n  url: https://example.com/x\n")
	g.mu.Unlock()
	if _, err := r.Manifest(uses, ""); err != nil {
		t.Errorf("Manifest() error = %v once action.yml is there", err)
	}
}

func TestResolveRemoteFollowsManifest(t *testing.T) {
	g := newGitHub(t)
	old, cur := []byte("old"), []byte("current")
	g.files["/old"] = old
	g.files["/current"] = cur
	manifest := "runs:\n  using: binary\n  url: %s%s\n  checksums:\n    linux_arm64: %s\n"
	g.files["/o/r/"+sha+"/action.yml"] = fmt.Appendf(nil, manifest, g.URL, "/old", digest(old))

	r := newResolver(t, g)
	if _, err := r.Resolve("github.com/o/r@"+sha, ""); err != nil {
		t.Fatal(err)
	}

	g.mu.Lock()
	g.files["/o/r/"+sha+"/action.yml"] = fmt.Appendf(nil, manifest, g.URL, "/current", digest(cur))
	g.mu.Unlock()

	r2 := &Resolver{CacheDir: r.CacheDir, RawBaseURL: g.URL, OS: "linux", Arch: "arm64"}
	exe, err := r2.Resolve("github.com/o/r@"+sha, "")
	if err != nil {
		t.Fatal(err)
	}
	if hex.EncodeToString(exe.SHA256) != digest(cur) {
		t.Errorf("SHA256 = %x, want the digest action.yml names now, %s", exe.SHA256, digest(cur))
	}
}

func TestResolveRemoteRetriesAfterFailure(t *testing.T) {
	g := newGitHub(t)
	bin := []byte("bin")
	g.files["/o/r/"+sha+"/action.yml"] = fmt.Appendf(nil, "runs:\n  using: binary\n  url: %s/bin\n  checksums:\n    linux_arm64: %s\n", g.URL, digest(bin))

	r := newResolver(t, g)
	if _, err := r.Resolve("github.com/o/r@"+sha, ""); err == nil {
		t.Fatal("Resolve() succeeded before the binary was published")
	}

	g.mu.Lock()
	g.files["/bin"] = bin
	g.mu.Unlock()

	if _, err := r.Resolve("github.com/o/r@"+sha, ""); err != nil {
		t.Fatalf("Resolve() after the binary was published error = %v", err)
	}
}

func TestResolveRemoteRestoresMode(t *testing.T) {
	g := newGitHub(t)
	bin := []byte("bin")
	g.files["/bin"] = bin
	g.files["/o/r/"+sha+"/action.yml"] = fmt.Appendf(nil, "runs:\n  using: binary\n  url: %s/bin\n  checksums:\n    linux_arm64: %s\n", g.URL, digest(bin))

	r := newResolver(t, g)
	exe, err := r.Resolve("github.com/o/r@"+sha, "")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(exe.Path, 0o644); err != nil {
		t.Fatal(err)
	}

	r2 := &Resolver{CacheDir: r.CacheDir, RawBaseURL: g.URL, OS: "linux", Arch: "arm64"}
	if _, err := r2.Resolve("github.com/o/r@"+sha, ""); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(exe.Path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm()&0o100 == 0 {
		t.Errorf("mode = %v, want it executable again", info.Mode())
	}
	if n := g.hit("/bin"); n != 1 {
		t.Errorf("binary fetched %d times, want 1", n)
	}
}

func TestResolveRemoteOnce(t *testing.T) {
	g := newGitHub(t)
	bin := []byte("bin")
	g.files["/bin"] = bin
	g.files["/o/r/"+sha+"/action.yml"] = fmt.Appendf(nil, "runs:\n  using: binary\n  url: %s/bin\n  checksums:\n    linux_arm64: %s\n", g.URL, digest(bin))

	r := newResolver(t, g)
	var wg sync.WaitGroup
	var failed atomic.Int32
	for range 8 {
		wg.Go(func() {
			if _, err := r.Resolve("github.com/o/r@"+sha, ""); err != nil {
				failed.Add(1)
			}
		})
	}
	wg.Wait()
	if failed.Load() != 0 {
		t.Fatalf("%d resolutions failed", failed.Load())
	}
	if n := g.hit("/bin"); n != 1 {
		t.Errorf("binary fetched %d times, want 1", n)
	}
}

func TestResolveRemoteErrors(t *testing.T) {
	bin := []byte("bin")
	tests := []struct {
		name     string
		manifest func(url string) string
		wantErr  string
	}{
		{
			name:    "no action.yml",
			wantErr: "404",
		},
		{
			name: "checksum mismatch",
			manifest: func(url string) string {
				return fmt.Sprintf("runs:\n  using: binary\n  url: %s/bin\n  checksums:\n    linux_arm64: %s\n", url, strings.Repeat("f", 64))
			},
			wantErr: "has SHA-256",
		},
		{
			name: "unsupported platform",
			manifest: func(url string) string {
				return fmt.Sprintf("runs:\n  using: binary\n  url: %s/bin\n  checksums:\n    darwin_arm64: %s\n", url, digest(bin))
			},
			wantErr: "no checksum for linux_arm64",
		},
		{
			name: "path",
			manifest: func(string) string {
				return "runs:\n  using: binary\n  path: bin\n"
			},
			wantErr: "runs.path is only for local actions",
		},
		{
			name: "action.yml too large",
			manifest: func(string) string {
				return "description: " + strings.Repeat("x", maxManifestSize) + "\n"
			},
			wantErr: "larger than",
		},
		{
			name: "binary missing",
			manifest: func(url string) string {
				return fmt.Sprintf("runs:\n  using: binary\n  url: %s/missing\n  checksums:\n    linux_arm64: %s\n", url, digest(bin))
			},
			wantErr: "404",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := newGitHub(t)
			g.files["/bin"] = bin
			if tt.manifest != nil {
				g.files["/o/r/"+sha+"/action.yml"] = []byte(tt.manifest(g.URL))
			}
			r := newResolver(t, g)
			_, err := r.Resolve("github.com/o/r@"+sha, "")
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("Resolve() error = %v, want one containing %q", err, tt.wantErr)
			}
			if !strings.Contains(err.Error(), "github.com/o/r@"+sha) {
				t.Errorf("error %q does not name the action", err)
			}
		})
	}
}

func TestResolveRemoteRedownloadsCorruptCache(t *testing.T) {
	g := newGitHub(t)
	bin := []byte("bin")
	g.files["/bin"] = bin
	g.files["/o/r/"+sha+"/action.yml"] = fmt.Appendf(nil, "runs:\n  using: binary\n  url: %s/bin\n  checksums:\n    linux_arm64: %s\n", g.URL, digest(bin))

	r := newResolver(t, g)
	exe, err := r.Resolve("github.com/o/r@"+sha, "")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(exe.Path, []byte("tampered"), 0o755); err != nil {
		t.Fatal(err)
	}

	r2 := &Resolver{CacheDir: r.CacheDir, RawBaseURL: g.URL, OS: "linux", Arch: "arm64"}
	if _, err := r2.Resolve("github.com/o/r@"+sha, ""); err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(exe.Path)
	if !bytes.Equal(got, bin) {
		t.Errorf("cached executable = %q, want %q", got, bin)
	}
}

func TestResolveLocal(t *testing.T) {
	base := t.TempDir()
	dir := filepath.Join(base, "actions", "x")
	if err := os.MkdirAll(filepath.Join(dir, "bin"), 0o755); err != nil {
		t.Fatal(err)
	}
	bin := []byte("local")
	if err := os.WriteFile(filepath.Join(dir, "bin", "x_linux_arm64"), bin, 0o755); err != nil {
		t.Fatal(err)
	}

	t.Run("path", func(t *testing.T) {
		if err := os.WriteFile(filepath.Join(dir, ManifestFile), []byte("runs:\n  using: binary\n  path: bin/x_{os}_{arch}\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		r := &Resolver{CacheDir: t.TempDir(), OS: "linux", Arch: "arm64"}
		exe, err := r.Resolve("./actions/x", base)
		if err != nil {
			t.Fatalf("Resolve() error = %v", err)
		}
		if want := filepath.Join(dir, "bin", "x_linux_arm64"); exe.Path != want {
			t.Errorf("Path = %q, want %q", exe.Path, want)
		}
		if len(exe.SHA256) != 0 {
			t.Errorf("SHA256 = %x, want none", exe.SHA256)
		}
	})

	t.Run("path with checksum", func(t *testing.T) {
		if err := os.WriteFile(filepath.Join(dir, ManifestFile), fmt.Appendf(nil, "runs:\n  using: binary\n  path: bin/x_{os}_{arch}\n  checksums:\n    linux_arm64: %s\n", digest(bin)), 0o644); err != nil {
			t.Fatal(err)
		}
		r := &Resolver{CacheDir: t.TempDir(), OS: "linux", Arch: "arm64"}
		exe, err := r.Resolve("./actions/x", base)
		if err != nil {
			t.Fatalf("Resolve() error = %v", err)
		}
		if hex.EncodeToString(exe.SHA256) != digest(bin) {
			t.Errorf("SHA256 = %x, want %s", exe.SHA256, digest(bin))
		}
	})

	t.Run("url", func(t *testing.T) {
		g := newGitHub(t)
		g.files["/bin"] = bin
		if err := os.WriteFile(filepath.Join(dir, ManifestFile), fmt.Appendf(nil, "runs:\n  using: binary\n  url: %s/bin\n  checksums:\n    linux_arm64: %s\n", g.URL, digest(bin)), 0o644); err != nil {
			t.Fatal(err)
		}
		r := newResolver(t, g)
		exe, err := r.Resolve("./actions/x", base)
		if err != nil {
			t.Fatalf("Resolve() error = %v", err)
		}
		if !strings.HasPrefix(exe.Path, r.CacheDir) {
			t.Errorf("Path = %q, want one in the cache %q", exe.Path, r.CacheDir)
		}
	})

	t.Run("not executable", func(t *testing.T) {
		if err := os.WriteFile(filepath.Join(dir, "bin", "plain"), bin, 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, ManifestFile), []byte("runs:\n  using: binary\n  path: bin/plain\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		r := &Resolver{CacheDir: t.TempDir(), OS: "linux", Arch: "arm64"}
		_, err := r.Resolve("./actions/x", base)
		if err == nil || !strings.Contains(err.Error(), "not an executable file") {
			t.Fatalf("Resolve() error = %v, want one saying the file is not executable", err)
		}
	})

	t.Run("directory", func(t *testing.T) {
		if err := os.WriteFile(filepath.Join(dir, ManifestFile), []byte("runs:\n  using: binary\n  path: bin\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		r := &Resolver{CacheDir: t.TempDir(), OS: "linux", Arch: "arm64"}
		_, err := r.Resolve("./actions/x", base)
		if err == nil || !strings.Contains(err.Error(), "not an executable file") {
			t.Fatalf("Resolve() error = %v, want one saying the path is not an executable file", err)
		}
	})

	t.Run("missing executable", func(t *testing.T) {
		if err := os.WriteFile(filepath.Join(dir, ManifestFile), []byte("runs:\n  using: binary\n  path: bin/nope\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		r := &Resolver{CacheDir: t.TempDir(), OS: "linux", Arch: "arm64"}
		if _, err := r.Resolve("./actions/x", base); err == nil {
			t.Fatal("Resolve() succeeded with a missing executable")
		}
	})

	t.Run("missing action.yml", func(t *testing.T) {
		r := &Resolver{CacheDir: t.TempDir()}
		if _, err := r.Resolve("./nowhere", base); err == nil {
			t.Fatal("Resolve() succeeded without an action.yml")
		}
	})
}

func TestRelease(t *testing.T) {
	a, b, c := strings.Repeat("a", 64), strings.Repeat("b", 64), strings.Repeat("1", 64)
	checksums := a + "  probe-greet_linux_amd64\n" +
		strings.ToUpper(b) + " *dist/probe-greet_darwin_arm64\n" +
		c + "  probe-greet_linux_arm64\n" +
		c + "  probe-greet_windows_amd64.exe\n" +
		c + "  probe-other_linux_amd64\n" +
		"\n"

	tests := []struct {
		name      string
		manifest  string
		tag       string
		checksums string
		want      string
		wantErr   string
	}{
		{
			name: "replaces the tag and the checksums, and keeps the rest",
			manifest: "# It declares allow-host, as it connects to no host.\n\n" +
				"name: greet\ndescription: Say hello\n" +
				"guard:\n  - allow-host # not read-only\nparams: [name, 'to, whom']\ncleanup:   true # removes its files\n" +
				"runs:\n    using: binary\n    checksums:\n        linux_amd64: " + c + "\n        freebsd_amd64: " + c + "\n" +
				"    url: https://github.com/example/probe-greet/releases/download/v0.1.0/probe-greet_{os}_{arch}\n",
			tag:       "v0.2.0",
			checksums: checksums,
			want: "# It declares allow-host, as it connects to no host.\n\n" +
				"name: greet\ndescription: Say hello\n" +
				"guard: [allow-host]\nparams: [name, \"to, whom\"]\ncleanup: true\n" +
				"runs:\n  using: binary\n" +
				"  url: https://github.com/example/probe-greet/releases/download/v0.2.0/probe-greet_{os}_{arch}\n" +
				"  checksums:\n" +
				"    darwin_arm64: \"" + b + "\"\n" +
				"    linux_amd64: \"" + a + "\"\n" +
				"    linux_arm64: \"" + c + "\"\n",
		},
		{
			name:      "a first release, with no checksums yet",
			manifest:  "description: \"Say: hello\"\nparams: []\nruns:\n  using: binary\n  url: https://github.com/example/probe-greet/releases/download/v0.0.0/probe-greet_{os}_{arch}\n",
			tag:       "v0.1.0",
			checksums: checksums,
			want: "description: \"Say: hello\"\nparams: []\n" +
				"runs:\n  using: binary\n" +
				"  url: https://github.com/example/probe-greet/releases/download/v0.1.0/probe-greet_{os}_{arch}\n" +
				"  checksums:\n" +
				"    darwin_arm64: \"" + b + "\"\n" +
				"    linux_amd64: \"" + a + "\"\n" +
				"    linux_arm64: \"" + c + "\"\n",
		},
		{
			name:      "an asset with an extension",
			manifest:  "runs:\n  using: binary\n  url: https://github.com/example/probe-greet/releases/download/v0.1.0/probe-greet_{os}_{arch}.exe\n",
			tag:       "v0.2.0",
			checksums: checksums,
			want: "runs:\n  using: binary\n" +
				"  url: https://github.com/example/probe-greet/releases/download/v0.2.0/probe-greet_{os}_{arch}.exe\n" +
				"  checksums:\n    windows_amd64: \"" + c + "\"\n",
		},
		{
			name:      "a tag with build metadata, and the architecture first",
			manifest:  "runs:\n  using: binary\n  url: https://github.com/example/probe-greet/releases/download/v0.1.0/greet-{arch}.{os}\n",
			tag:       "v0.2.0-rc.1+build_5",
			checksums: a + "  greet-amd64.linux\n",
			want: "runs:\n  using: binary\n" +
				"  url: https://github.com/example/probe-greet/releases/download/v0.2.0-rc.1+build_5/greet-{arch}.{os}\n" +
				"  checksums:\n    linux_amd64: \"" + a + "\"\n",
		},
		{
			name:      "invalid manifest",
			manifest:  "runs:\n  using: go\n",
			tag:       "v0.2.0",
			checksums: checksums,
			wantErr:   "runs.using must be",
		},
		{
			name:      "path",
			manifest:  "runs:\n  using: binary\n  path: probe-greet\n",
			tag:       "v0.2.0",
			checksums: checksums,
			wantErr:   "runs.url is needed",
		},
		{
			name:      "not a release URL",
			manifest:  "runs:\n  using: binary\n  url: https://example.com/probe-greet_{os}_{arch}\n",
			tag:       "v0.2.0",
			checksums: checksums,
			wantErr:   "runs.url must be https://github.com/",
		},
		{
			name:      "no placeholder",
			manifest:  "runs:\n  using: binary\n  url: https://github.com/example/probe-greet/releases/download/v0.1.0/probe-greet\n",
			tag:       "v0.2.0",
			checksums: checksums,
			wantErr:   "one {os} and one {arch}",
		},
		{
			name:      "bad tag",
			manifest:  "runs:\n  using: binary\n  url: https://github.com/example/probe-greet/releases/download/v0.1.0/probe-greet_{os}_{arch}\n",
			tag:       "release/v0.2.0",
			checksums: checksums,
			wantErr:   "tag \"release/v0.2.0\"",
		},
		{
			name:      "a tag that would end the path",
			manifest:  "runs:\n  using: binary\n  url: https://github.com/example/probe-greet/releases/download/v0.1.0/probe-greet_{os}_{arch}\n",
			tag:       "release#1",
			checksums: checksums,
			wantErr:   "tag \"release#1\"",
		},
		{
			name:      "a tag that would start an escape",
			manifest:  "runs:\n  using: binary\n  url: https://github.com/example/probe-greet/releases/download/v0.1.0/probe-greet_{os}_{arch}\n",
			tag:       "v1%20",
			checksums: checksums,
			wantErr:   "tag \"v1%20\"",
		},
		{
			name:      "placeholders with nothing between them",
			manifest:  "runs:\n  using: binary\n  url: https://github.com/example/probe-greet/releases/download/v0.1.0/probe-greet_{os}{arch}\n",
			tag:       "v0.2.0",
			checksums: a + "  probe-greet_linuxamd64\n",
			wantErr:   "a character between {os} and {arch}",
		},
		{
			name:      "placeholders with only a letter between them",
			manifest:  "runs:\n  using: binary\n  url: https://github.com/example/probe-greet/releases/download/v0.1.0/probe-greet_{arch}x{os}\n",
			tag:       "v0.2.0",
			checksums: a + "  probe-greet_amd64xlinux\n",
			wantErr:   "a character between {os} and {arch}",
		},
		{
			name:      "no checksum of the asset",
			manifest:  "runs:\n  using: binary\n  url: https://github.com/example/probe-greet/releases/download/v0.1.0/probe-hello_{os}_{arch}\n",
			tag:       "v0.2.0",
			checksums: checksums,
			wantErr:   "checksums list no file named probe-hello_{os}_{arch}",
		},
		{
			name:      "not a digest",
			manifest:  "runs:\n  using: binary\n  url: https://github.com/example/probe-greet/releases/download/v0.1.0/probe-greet_{os}_{arch}\n",
			tag:       "v0.2.0",
			checksums: "abc  probe-greet_linux_amd64\n",
			wantErr:   "checksum of probe-greet_linux_amd64 must be a SHA-256 digest",
		},
		{
			name:      "one platform twice",
			manifest:  "runs:\n  using: binary\n  url: https://github.com/example/probe-greet/releases/download/v0.1.0/probe-greet_{os}_{arch}\n",
			tag:       "v0.2.0",
			checksums: a + "  probe-greet_linux_amd64\n" + b + "  probe-greet_linux_amd64\n",
			wantErr:   "twice, with different digests",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := Release([]byte(tt.manifest), tt.tag, strings.NewReader(tt.checksums))
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("Release() error = %v, want one containing %q", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("Release() error = %v", err)
			}
			if string(got) != tt.want {
				t.Errorf("Release() =\n%s\nwant\n%s", got, tt.want)
			}
			// Releasing what was released changes nothing.
			again, err := Release(got, tt.tag, strings.NewReader(tt.checksums))
			if err != nil || string(again) != tt.want {
				t.Errorf("Release() of its own result =\n%s\nerror = %v", again, err)
			}
		})
	}
}

// TestReleaseWritesWhatParseReads checks values that YAML would read as
// something else unquoted.
func TestReleaseWritesWhatParseReads(t *testing.T) {
	manifest := "name: \"true\"\ndescription: \"two\\nlines # and: more\"\nguard: []\nparams: [\"123\", \"null\", \"a b\", \"[x]\", \"\"]\n" +
		"runs:\n  using: binary\n  url: https://github.com/example/probe-greet/releases/download/v0.1.0/probe-greet_{os}_{arch}\n"
	sum := strings.Repeat("0", 64)
	got, err := Release([]byte(manifest), "v0.2.0", strings.NewReader(sum+"  probe-greet_linux_amd64\n"))
	if err != nil {
		t.Fatalf("Release() error = %v", err)
	}
	m, err := ParseManifest(got)
	if err != nil {
		t.Fatalf("ParseManifest() error = %v\n%s", err, got)
	}
	want := &Manifest{
		Name:        "true",
		Description: "two\nlines # and: more",
		Guard:       []string{},
		Params:      []string{"123", "null", "a b", "[x]", ""},
		Runs: Runs{
			Using:     UsingBinary,
			URL:       "https://github.com/example/probe-greet/releases/download/v0.2.0/probe-greet_{os}_{arch}",
			Checksums: map[string]string{"linux_amd64": sum},
		},
	}
	if !reflect.DeepEqual(m, want) {
		t.Errorf("Release() wrote\n%s\nwhich reads as %+v, want %+v", got, m, want)
	}
}

func TestScaffold(t *testing.T) {
	for _, repo := range []string{"example/probe-greet", "github.com/example/probe-greet"} {
		got, err := Scaffold(repo)
		if err != nil {
			t.Fatalf("Scaffold(%q) error = %v", repo, err)
		}
		m, err := ParseManifest(got)
		if err != nil {
			t.Fatalf("ParseManifest() error = %v\n%s", err, got)
		}
		want := &Manifest{
			Name: "greet",
			Runs: Runs{Using: UsingBinary, URL: "https://github.com/example/probe-greet/releases/download/v0.0.0/probe-greet_{os}_{arch}"},
		}
		if !reflect.DeepEqual(m, want) {
			t.Errorf("Scaffold(%q) reads as %+v, want %+v", repo, m, want)
		}

		// What it wrote can be released as it is.
		sum := strings.Repeat("a", 64)
		released, err := Release(got, "v0.1.0", strings.NewReader(sum+"  probe-greet_linux_amd64\n"))
		if err != nil {
			t.Fatalf("Release() error = %v", err)
		}
		wantReleased := "name: greet\nruns:\n  using: binary\n" +
			"  url: https://github.com/example/probe-greet/releases/download/v0.1.0/probe-greet_{os}_{arch}\n" +
			"  checksums:\n    linux_amd64: \"" + sum + "\"\n"
		if string(released) != wantReleased {
			t.Errorf("Release() =\n%s\nwant\n%s", released, wantReleased)
		}
	}

	if got, err := Scaffold("example/hello"); err != nil || !strings.HasPrefix(string(got), "name: hello\n") || !strings.Contains(string(got), "/hello_{os}_{arch}\n") {
		t.Errorf("Scaffold() of a repository without probe- = %s, error = %v", got, err)
	}

	for _, repo := range []string{"", "probe-greet", "example/probe-greet/dir", "example/..", "example/probe greet", "gitlab.com/example/probe-greet"} {
		if _, err := Scaffold(repo); err == nil {
			t.Errorf("Scaffold(%q) should fail", repo)
		}
	}
}

func TestFull(t *testing.T) {
	// The temporary directory is behind a symbolic link on macOS.
	base, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(base, "greet"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(base, "greet"), filepath.Join(base, "link")); err != nil {
		t.Fatal(err)
	}
	remote := "github.com/example/probe-greet@" + strings.Repeat("a", 40)
	tests := []struct {
		name    string
		uses    string
		baseDir string
		want    string
	}{
		{name: "a remote action is as it is", uses: remote, baseDir: base, want: remote},
		{name: "a local action is taken from the base directory", uses: "./greet", baseDir: base, want: filepath.Join(base, "greet")},
		{name: "by whichever way it is written", uses: "./x/../greet/", baseDir: base, want: filepath.Join(base, "greet")},
		{name: "an absolute one is left there", uses: filepath.Join(base, "greet"), baseDir: "/elsewhere", want: filepath.Join(base, "greet")},
		{name: "a symbolic link is followed", uses: "./link", baseDir: base, want: filepath.Join(base, "greet")},
		{name: "one that is not there is as it is written", uses: "./missing", baseDir: base, want: filepath.Join(base, "missing")},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := Full(tt.uses, tt.baseDir)
			if err != nil {
				t.Fatalf("Full() error = %v", err)
			}
			if got != tt.want {
				t.Errorf("Full() = %q, want %q", got, tt.want)
			}
		})
	}

	// Without a base directory, a local action is taken from the working one.
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if got, err := Full("./no-such-action", ""); err != nil || got != filepath.Join(wd, "no-such-action") {
		t.Errorf("Full() = %q, %v, want %q", got, err, filepath.Join(wd, "no-such-action"))
	}
	if _, err := Full("greet", base); err == nil {
		t.Error("Full() of what names no external action should fail")
	}
}
