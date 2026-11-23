package file

import (
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/mozership/probe/actionrpc"
)

// run runs with, and fails the test on an error.
func run(t *testing.T, with map[string]any) map[string]any {
	t.Helper()
	ret, err := Run(with)
	if err != nil {
		t.Fatalf("Run(%v) error = %v", with, err)
	}
	return ret
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestWrite(t *testing.T) {
	dir := t.TempDir()

	t.Run("a new file, in directories that are made", func(t *testing.T) {
		path := filepath.Join(dir, "a", "b", "config.yml")
		ret := run(t, map[string]any{"path": path, "op": "write", "content": "port: 8080\n", "mode": "0600"})
		want := map[string]any{"exists": true, "type": "file", "content": "port: 8080\n", "mode": "0600", "size": int64(11)}
		if got := pick(ret["res"], "exists", "type", "content", "mode", "size"); !reflect.DeepEqual(got, want) {
			t.Errorf("res = %#v, want %#v", got, want)
		}
		if ret["status"] != 0 {
			t.Errorf("status = %v, want 0", ret["status"])
		}
		wantReq := map[string]any{"path": path, "op": "write", "content": "port: 8080\n", "mode": "0600", "recursive": false}
		if !reflect.DeepEqual(ret["req"], wantReq) {
			t.Errorf("req = %#v, want %#v", ret["req"], wantReq)
		}
	})

	t.Run("a file there is replaced, and keeps its mode", func(t *testing.T) {
		path := filepath.Join(dir, "old.txt")
		if err := os.WriteFile(path, []byte("a long old line\n"), 0o640); err != nil {
			t.Fatal(err)
		}
		ret := run(t, map[string]any{"path": path, "op": "write", "content": "new\n"})
		if got := pick(ret["res"], "content", "mode"); !reflect.DeepEqual(got, map[string]any{"content": "new\n", "mode": "0640"}) {
			t.Errorf("res = %#v", got)
		}
	})

	t.Run("a mode is set on a file there", func(t *testing.T) {
		path := filepath.Join(dir, "key.pem")
		if err := os.WriteFile(path, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
		ret := run(t, map[string]any{"path": path, "op": "write", "content": "secret", "mode": "400"})
		if got := pick(ret["res"], "mode"); got["mode"] != "0400" {
			t.Errorf("res.mode = %v, want 0400", got["mode"])
		}
	})

	t.Run("without content, an empty file", func(t *testing.T) {
		path := filepath.Join(dir, "empty")
		ret := run(t, map[string]any{"path": path, "op": "write"})
		if got := pick(ret["res"], "exists", "content"); !reflect.DeepEqual(got, map[string]any{"exists": true, "content": ""}) {
			t.Errorf("res = %#v", got)
		}
	})

	t.Run("through a symbolic link", func(t *testing.T) {
		target := filepath.Join(dir, "target")
		link := filepath.Join(dir, "link")
		if err := os.WriteFile(target, []byte("old"), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(target, link); err != nil {
			t.Fatal(err)
		}
		run(t, map[string]any{"path": link, "op": "write", "content": "new"})
		if got := readFile(t, target); got != "new" {
			t.Errorf("target = %q, want new", got)
		}
	})
}

func TestAppend(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "logs", "app.log")

	run(t, map[string]any{"path": path, "op": "append", "content": "level=INFO msg=started\n"})
	ret := run(t, map[string]any{"path": path, "op": "append", "content": "level=INFO msg=ready\n"})

	want := "level=INFO msg=started\nlevel=INFO msg=ready\n"
	if got := pick(ret["res"], "content"); got["content"] != want {
		t.Errorf("res.content = %q, want %q", got["content"], want)
	}
}

// TestWriteOnlyToAFile checks that what is not a regular file is not
// written: a directory cannot be, and a pipe would wait for a reader.
func TestWriteOnlyToAFile(t *testing.T) {
	dir := t.TempDir()
	paths := map[string]string{"a directory": dir}
	if mkfifo, err := exec.LookPath("mkfifo"); err == nil {
		fifo := filepath.Join(dir, "fifo")
		if out, err := exec.Command(mkfifo, fifo).CombinedOutput(); err != nil {
			t.Fatalf("mkfifo: %v: %s", err, out)
		}
		paths["a pipe"] = fifo
	}
	for name, path := range paths {
		for _, op := range []string{"write", "append"} {
			t.Run(name+" by "+op, func(t *testing.T) {
				_, err := Run(map[string]any{"path": path, "op": op, "content": "x"})
				if err == nil || !strings.Contains(err.Error(), "not a file, and is not written") {
					t.Errorf("Run() error = %v, want that it is not written", err)
				}
			})
		}
	}
}

func TestDelete(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "app.log")
	empty := filepath.Join(dir, "empty")
	full := filepath.Join(dir, "full")
	target := filepath.Join(dir, "target")
	link := filepath.Join(dir, "link")
	for _, d := range []string{empty, filepath.Join(full, "sub"), target} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	for _, f := range []string{file, filepath.Join(full, "sub", "a"), filepath.Join(target, "kept")} {
		if err := os.WriteFile(f, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name string
		with map[string]any
	}{
		{"a file", map[string]any{"path": file, "op": "delete"}},
		{"nothing", map[string]any{"path": filepath.Join(dir, "gone"), "op": "delete"}},
		{"an empty directory", map[string]any{"path": empty, "op": "delete"}},
		{"a directory with all it holds", map[string]any{"path": full, "op": "delete", "recursive": true}},
		{"a link, not what it points to", map[string]any{"path": link, "op": "delete", "recursive": true}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ret := run(t, tt.with)
			if got := pick(ret["res"], "exists"); got["exists"] != false {
				t.Errorf("res.exists = %v, want false", got["exists"])
			}
			if ret["status"] != 1 {
				t.Errorf("status = %v, want 1", ret["status"])
			}
			if _, err := os.Lstat(tt.with["path"].(string)); !os.IsNotExist(err) {
				t.Errorf("%s is still there: %v", tt.with["path"], err)
			}
		})
	}
	if got := readFile(t, filepath.Join(target, "kept")); got != "x" {
		t.Errorf("what the link pointed to = %q, want it kept", got)
	}
}

func TestDeleteADirectoryThatIsNotEmpty(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "data")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "a"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := Run(map[string]any{"path": dir, "op": "delete"})
	if err == nil || !strings.Contains(err.Error(), "set recursive") {
		t.Errorf("Run() error = %v, want that recursive is to be set", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "a")); err != nil {
		t.Errorf("what the directory holds is gone: %v", err)
	}
}

// TestDeleteKeepsTheWorkingDirectory checks that the working directory, and
// a directory that holds it, are not deleted, as a path made of a variable
// that is empty may name them.
func TestDeleteKeepsTheWorkingDirectory(t *testing.T) {
	root := t.TempDir()
	wd := filepath.Join(root, "work")
	sub := filepath.Join(wd, "tmp")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Chdir(wd)

	for _, path := range []string{".", "./", wd, root, "..", "/", "tmp/.."} {
		t.Run(path, func(t *testing.T) {
			_, err := Run(map[string]any{"path": path, "op": "delete", "recursive": true})
			if err == nil || !strings.Contains(err.Error(), "holds the working directory") {
				t.Errorf("Run() error = %v, want that it holds the working directory", err)
			}
		})
	}
	if _, err := os.Stat(wd); err != nil {
		t.Fatalf("the working directory is gone: %v", err)
	}

	// A directory under the working directory is deleted.
	run(t, map[string]any{"path": "tmp", "op": "delete", "recursive": true})
	if _, err := os.Stat(sub); !os.IsNotExist(err) {
		t.Errorf("tmp is still there: %v", err)
	}
}

// TestWritesAreRefusedUnderReadOnly checks that an op that writes is
// refused before it changes anything, and that read is done.
func TestWritesAreRefusedUnderReadOnly(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "app.log")
	if err := os.WriteFile(path, []byte("kept"), 0o644); err != nil {
		t.Fatal(err)
	}
	guard := actionrpc.Guard{ReadOnly: true}

	for _, with := range []map[string]any{
		{"path": path, "op": "write", "content": "x"},
		{"path": path, "op": "append", "content": "x"},
		{"path": path, "op": "delete"},
		{"path": filepath.Join(dir, "new"), "op": "write"},
	} {
		_, err := RunGuarded(with, guard)
		if !actionrpc.IsRefused(err) || !strings.Contains(err.Error(), "the run is read-only") {
			t.Errorf("RunGuarded(%v) error = %v, want a refusal", with, err)
		}
	}
	if got := readFile(t, path); got != "kept" {
		t.Errorf("the file = %q, want it unchanged", got)
	}
	if _, err := os.Stat(filepath.Join(dir, "new")); !os.IsNotExist(err) {
		t.Errorf("a file was made: %v", err)
	}

	for _, with := range []map[string]any{{"path": path}, {"path": path, "op": "read"}} {
		ret, err := RunGuarded(with, guard)
		if err != nil {
			t.Fatalf("RunGuarded(%v) error = %v", with, err)
		}
		if got := pick(ret["res"], "content"); got["content"] != "kept" {
			t.Errorf("res.content = %v, want kept", got["content"])
		}
	}
}

func TestParseErrors(t *testing.T) {
	path := filepath.Join(t.TempDir(), "a")
	tests := []struct {
		name    string
		with    map[string]any
		wantErr string
	}{
		{"an op that is not there", map[string]any{"path": path, "op": "remove"}, `op "remove" is not one of`},
		{"content to read", map[string]any{"path": path, "content": "x"}, "content is written only by op write or append, not read"},
		{"content to delete", map[string]any{"path": path, "op": "delete", "content": "x"}, "content is written only"},
		{"a mode to read", map[string]any{"path": path, "mode": "0600"}, "mode is set only by op write or append"},
		{"recursive to write", map[string]any{"path": path, "op": "write", "recursive": true}, "recursive is taken only by op delete"},
		{"a mode as a number", map[string]any{"path": path, "op": "write", "mode": 384}, "mode must be a string of octal digits in quotes"},
		{"a mode that is not octal", map[string]any{"path": path, "op": "write", "mode": "0699"}, `mode "0699" is not octal digits`},
		{"a mode too large", map[string]any{"path": path, "op": "write", "mode": "17777"}, `mode "17777" is not octal digits`},
		{"a mode of words", map[string]any{"path": path, "op": "write", "mode": "u+rw"}, `mode "u+rw" is not octal digits`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := Run(tt.with)
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("Run() error = %v, want %q", err, tt.wantErr)
			}
			if _, statErr := os.Stat(path); !os.IsNotExist(statErr) {
				t.Errorf("%s was made: %v", path, statErr)
			}
		})
	}
}

func TestParseMode(t *testing.T) {
	tests := []struct {
		mode string
		want fs.FileMode
	}{
		{"0644", 0o644},
		{"600", 0o600},
		{"0", 0},
		{"4755", 0o755 | fs.ModeSetuid},
		{"2775", 0o775 | fs.ModeSetgid},
		{"1777", 0o777 | fs.ModeSticky},
	}
	for _, tt := range tests {
		got, err := parseMode(tt.mode)
		if err != nil || got != tt.want {
			t.Errorf("parseMode(%q) = %v, %v, want %v", tt.mode, got, err, tt.want)
		}
		if back := modeOf(got); len(tt.mode) == 4 && back != tt.mode {
			t.Errorf("modeOf(parseMode(%q)) = %s", tt.mode, back)
		}
	}
}
