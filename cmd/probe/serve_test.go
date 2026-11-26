package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mozership/probe"
)

func TestCmd_runServeRefuses(t *testing.T) {
	bad := filepath.Join(t.TempDir(), "serve.yml")
	if err := os.WriteFile(bad, []byte("name: t\nworkflows:\n  w:\n    loop: true\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		args []string
		want string
	}{
		{[]string{"probe", "serve"}, "serve takes one serve file, not 0"},
		{[]string{"probe", "serve", "a.yml", "b.yml"}, "serve takes one serve file, not 2"},
		{[]string{"probe", "serve", filepath.Join(t.TempDir(), "missing.yml")}, "no such file"},
		{[]string{"probe", "serve", bad}, "workflow w: names no files"},
	}
	for _, tt := range tests {
		c := newBufferCmd()
		if got := c.start(tt.args); got != probe.ExitConfigError {
			t.Errorf("start(%q) = %d, want %d", tt.args, got, probe.ExitConfigError)
		}
		if msg := c.errWriter.(*bytes.Buffer).String(); !strings.Contains(msg, tt.want) {
			t.Errorf("start(%q) said %q, want %q", tt.args, msg, tt.want)
		}
	}
}

func TestCmd_runServeLoads(t *testing.T) {
	path := filepath.Join(t.TempDir(), "serve.yml")
	if err := os.WriteFile(path, []byte("name: t\nworkflows:\n  w:\n    files: [w.yml]\n    loop: true\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := newBufferCmd().start([]string{"probe", "serve", path}); got != probe.ExitOK {
		t.Errorf("start = %d, want %d", got, probe.ExitOK)
	}
}
