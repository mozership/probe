package textfile

import (
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

func TestRead(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "a.txt")
	if err := os.WriteFile(file, []byte("hello\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if got, err := Read(file, 6); err != nil || got != "hello\n" {
		t.Errorf("Read() = %q, %v, want the whole file", got, err)
	}
	for _, tt := range []struct {
		name, path string
		limit      int
		wantErr    string
	}{
		{"over the limit", file, 5, file + " exceeds maximum length (5 bytes)"},
		{"a directory", dir, 10, dir + " is a directory"},
		{"nothing", filepath.Join(dir, "gone.txt"), 10, "no such file or directory"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := Read(tt.path, tt.limit); err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("Read(%q) error = %v, want %q", tt.path, err, tt.wantErr)
			}
		})
	}
}

// TestReadOpened gives ReadOpened what Read found at a path before it
// opened it, as if the path had come to hold something else in between.
func TestReadOpened(t *testing.T) {
	dir := t.TempDir()
	first := filepath.Join(dir, "first.txt")
	second := filepath.Join(dir, "second.txt")
	for _, p := range []string{first, second} {
		if err := os.WriteFile(p, []byte(p), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	info, err := os.Stat(first)
	if err != nil {
		t.Fatal(err)
	}

	if got, err := ReadOpened(first, info, 1000); err != nil || got != first {
		t.Errorf("ReadOpened of the file found = %q, %v", got, err)
	}
	for _, tt := range []struct {
		name, path, wantErr string
	}{
		{"another file", second, second + " changed while it was opened"},
		{"a directory", dir, dir + " is a directory"},
		{"nothing", filepath.Join(dir, "gone.txt"), "no such file or directory"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := ReadOpened(tt.path, info, 1000); err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("ReadOpened(%q) error = %v, want %q", tt.path, err, tt.wantErr)
			}
		})
	}
}

func TestReadDoesNotBlockOnAFIFO(t *testing.T) {
	mkfifo, err := exec.LookPath("mkfifo")
	if err != nil {
		t.Skip("mkfifo is not available")
	}
	fifo := filepath.Join(t.TempDir(), "fifo")
	if out, err := exec.Command(mkfifo, fifo).CombinedOutput(); err != nil {
		t.Fatalf("mkfifo: %v: %s", err, out)
	}
	start := time.Now()
	if _, err := Read(fifo, 10); err == nil || !strings.Contains(err.Error(), "is not a regular file") {
		t.Errorf("Read() error = %v, want one about the file", err)
	}
	if time.Since(start) > time.Second {
		t.Errorf("a FIFO should be refused at once, took %v", time.Since(start))
	}
}

func TestLines(t *testing.T) {
	for _, tt := range []struct {
		in   string
		want []any
	}{
		{"", []any{}},
		{"\n", []any{""}},
		{"a", []any{"a"}},
		{"a\nb\n", []any{"a", "b"}},
		{"a\r\nb\r\n", []any{"a", "b"}},
		{"a\n\nb", []any{"a", "", "b"}},
	} {
		if got := Lines(tt.in); !slices.Equal(got, tt.want) {
			t.Errorf("Lines(%q) = %#v, want %#v", tt.in, got, tt.want)
		}
	}
}
