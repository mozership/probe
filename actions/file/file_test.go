package file

import (
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/hashicorp/go-hclog"
	"github.com/mozership/probe/internal/actiontest"
	"github.com/mozership/probe/internal/textfile"
)

func TestRead(t *testing.T) {
	dir := t.TempDir()
	log := filepath.Join(dir, "app.log")
	if err := os.WriteFile(log, []byte("started\r\nlevel=INFO ready\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	empty := filepath.Join(dir, "empty")
	if err := os.WriteFile(empty, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "link")
	if err := os.Symlink(log, link); err != nil {
		t.Fatal(err)
	}
	dangling := filepath.Join(dir, "dangling")
	if err := os.Symlink(filepath.Join(dir, "nowhere"), dangling); err != nil {
		t.Fatal(err)
	}

	read := map[string]any{"exists": true, "type": "file", "content": "started\r\nlevel=INFO ready\n"}
	missing := map[string]any{"exists": false, "type": "", "content": ""}
	tests := []struct {
		name       string
		path       string
		wantRes    map[string]any
		wantStatus int
	}{
		{"a file", log, read, 0},
		{"an empty file", empty, map[string]any{"exists": true, "type": "file", "content": ""}, 0},
		{"a link to a file", link, read, 0},
		{"a directory", dir, map[string]any{"exists": true, "type": "dir", "content": ""}, 0},
		{"nothing", filepath.Join(dir, "gone.log"), missing, 1},
		{"a link to nothing", dangling, missing, 1},
		{"a path under a file", filepath.Join(log, "x"), missing, 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ret, err := Read(map[string]any{"path": tt.path})
			if err != nil {
				t.Fatalf("Read() error = %v", err)
			}
			if !reflect.DeepEqual(ret["res"], tt.wantRes) {
				t.Errorf("res = %#v, want %#v", ret["res"], tt.wantRes)
			}
			if ret["status"] != tt.wantStatus {
				t.Errorf("status = %v, want %d", ret["status"], tt.wantStatus)
			}
			if req := ret["req"].(map[string]any); req["path"] != tt.path {
				t.Errorf("req.path = %v, want %s", req["path"], tt.path)
			}
		})
	}
}

// TestReadASocket checks that a Unix socket, which cannot be read, is told
// to be there, so that a step can wait for a server to listen on one or to
// remove it.
func TestReadASocket(t *testing.T) {
	// The path of a socket is short on macOS, which t.TempDir may exceed.
	dir, err := os.MkdirTemp("", "file")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	path := filepath.Join(dir, "s.sock")
	l, err := net.Listen("unix", path)
	if err != nil {
		t.Skipf("cannot listen on a Unix socket: %v", err)
	}
	t.Cleanup(func() { _ = l.Close() })

	ret, err := Read(map[string]any{"path": path})
	if err != nil {
		t.Fatalf("Read() error = %v", err)
	}
	want := map[string]any{"exists": true, "type": "socket", "content": ""}
	if !reflect.DeepEqual(ret["res"], want) || ret["status"] != 0 {
		t.Errorf("Read() = %#v, want res %#v and status 0", ret, want)
	}
}

func TestReadAPipe(t *testing.T) {
	mkfifo, err := exec.LookPath("mkfifo")
	if err != nil {
		t.Skip("mkfifo is not available")
	}
	fifo := filepath.Join(t.TempDir(), "fifo")
	if out, err := exec.Command(mkfifo, fifo).CombinedOutput(); err != nil {
		t.Fatalf("mkfifo: %v: %s", err, out)
	}
	// A pipe is not opened, which would wait for a writer.
	ret, err := Read(map[string]any{"path": fifo})
	if err != nil {
		t.Fatalf("Read() error = %v", err)
	}
	if res := ret["res"].(map[string]any); res["type"] != "pipe" || res["content"] != "" {
		t.Errorf("res = %#v, want a pipe left unread", res)
	}
}

func TestReadErrors(t *testing.T) {
	dir := t.TempDir()
	big := filepath.Join(dir, "big")
	if err := os.WriteFile(big, make([]byte, maxContent+1), 0o600); err != nil {
		t.Fatal(err)
	}
	full := filepath.Join(dir, "full")
	if err := os.WriteFile(full, make([]byte, maxContent), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Read(map[string]any{"path": full}); err != nil {
		t.Errorf("a file of the largest size should be read, got %v", err)
	}

	tests := []struct {
		name    string
		with    map[string]any
		wantErr string
	}{
		{"no path", map[string]any{}, "path parameter is required"},
		{"an empty path", map[string]any{"path": ""}, "path parameter is required"},
		{"a file too large", map[string]any{"path": big}, "exceeds maximum length"},
	}
	if os.Geteuid() != 0 {
		locked := filepath.Join(dir, "locked")
		if err := os.WriteFile(locked, []byte("x"), 0o000); err != nil {
			t.Fatal(err)
		}
		tests = append(tests, struct {
			name    string
			with    map[string]any
			wantErr string
		}{"a file that cannot be read", map[string]any{"path": locked}, "permission denied"})
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := Read(tt.with); err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("Read() error = %v, want %q", err, tt.wantErr)
			}
		})
	}
}

func TestFieldsDeclareWhatItReturns(t *testing.T) {
	kinds := actiontest.NewKinds(t, Spec().ReqTypes, Spec().ResTypes)
	dir := t.TempDir()
	file := filepath.Join(dir, "a.txt")
	if err := os.WriteFile(file, []byte("a\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	a := &Action{log: hclog.NewNullLogger()}
	for _, with := range []map[string]any{{"path": file}, {"path": dir}, {"path": filepath.Join(dir, "gone")}} {
		ret, err := a.Run(with)
		if err != nil {
			t.Fatalf("%v: %v", with, err)
		}
		actiontest.CheckFields(t, Spec().ReqFields, Spec().ResFields, nil, ret)
		kinds.Check(ret)
	}
	kinds.Covered()
}

func TestRequired(t *testing.T) {
	a := &Action{log: hclog.NewNullLogger()}
	actiontest.CheckRequired(t, Spec().Required, map[string]any{"path": t.TempDir()}, func(with map[string]any) error {
		_, err := a.Run(with)
		return err
	})
}

// TestLookAgainWhenTheFileIsReplaced checks that a path which comes to hold
// another file between being looked at and being read, as a rotated log
// does, is looked at again, up to lookAttempts times.
func TestLookAgainWhenTheFileIsReplaced(t *testing.T) {
	changed := fmt.Errorf("x %w", textfile.ErrChanged)
	read := map[string]any{"exists": true, "type": "file", "content": "new"}
	tests := []struct {
		name      string
		failures  int
		wantCalls int
		wantErr   bool
	}{
		{"replaced once", 1, 2, false},
		{"replaced on every look but the last", lookAttempts - 1, lookAttempts, false},
		{"replaced on every look", lookAttempts, lookAttempts, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			calls := 0
			lookAt = func(string) (map[string]any, error) {
				calls++
				if calls <= tt.failures {
					return nil, changed
				}
				return read, nil
			}
			t.Cleanup(func() { lookAt = lookOnce })

			res, err := look("x")
			if calls != tt.wantCalls {
				t.Errorf("looked %d times, want %d", calls, tt.wantCalls)
			}
			if tt.wantErr {
				if !errors.Is(err, textfile.ErrChanged) {
					t.Errorf("look() error = %v, want ErrChanged", err)
				}
				return
			}
			if err != nil || !reflect.DeepEqual(res, read) {
				t.Errorf("look() = %v, %v, want %v", res, err, read)
			}
		})
	}
}
