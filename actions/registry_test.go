package actions

import (
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/mozership/probe/actionrpc"
)

func TestKeepingAreBuiltin(t *testing.T) {
	names := Names()
	for _, name := range Keeping() {
		if !slices.Contains(names, name) {
			t.Errorf("%s keeps to the guard but is not a built-in action", name)
		}
	}
	if !slices.IsSorted(Keeping()) {
		t.Errorf("Keeping() = %v, want it sorted", Keeping())
	}
}

func TestKeepingIsACopy(t *testing.T) {
	k := Keeping()
	k[0] = "shell"
	if slices.Contains(Keeping(), "shell") {
		t.Error("changing what Keeping returns should not change the actions that keep to the guard")
	}
}

func TestKeeps(t *testing.T) {
	names := Names()
	known := []string{actionrpc.KindReadOnly, actionrpc.KindAllowHost}
	for name, kinds := range Keeps() {
		if !slices.Contains(names, name) {
			t.Errorf("%s declares guard kinds but is not a built-in action", name)
		}
		for _, k := range kinds {
			if !slices.Contains(known, k) {
				t.Errorf("%s declares %q, which is no kind of guard", name, k)
			}
		}
	}
	// The actions that cannot tell what they are about to do keep to none.
	for _, name := range []string{"imap", "shell", "smtp", "ssh"} {
		if _, ok := Keeps()[name]; ok {
			t.Errorf("%s should keep to no guard", name)
		}
	}
	if want := []string{"db", "dns", "embedded", "grpc", "hello", "http"}; !slices.Equal(Keeping(), want) {
		t.Errorf("Keeping() = %v, want %v", Keeping(), want)
	}
	k := Keeps()
	k["http"][0] = "changed"
	if Keeps()["http"][0] == "changed" {
		t.Error("changing what Keeps returns should change no declaration")
	}
}

func TestParams(t *testing.T) {
	for _, name := range Names() {
		params, ok := Params(name)
		if name == "hello" {
			if ok {
				t.Errorf("hello takes any key, want no params, got %v", params)
			}
			continue
		}
		if !ok || len(params) == 0 {
			t.Errorf("Params(%s) = %v, %v; want the keys it takes", name, params, ok)
		}
	}
	if _, ok := Params("no-such-action"); ok {
		t.Error("an action that is not built in should have no params")
	}
	http, _ := Params("http")
	for _, key := range []string{"url", "headers", "body", "get", "post", "form", "multipart", "openapi", "trace_header"} {
		if !slices.Contains(http, key) {
			t.Errorf("http params %v lack %s", http, key)
		}
	}
	if len(AllParams()) != len(Names())-1 {
		t.Errorf("AllParams() = %d actions, want all but hello", len(AllParams()))
	}
}

// TestContracts checks that each key asking a built-in action to check a
// step against a contract is a key the action takes.
func TestContracts(t *testing.T) {
	want := map[string][]string{"grpc": {"proto"}, "http": {"openapi"}}
	got := Contracts()
	if len(got) != len(want) {
		t.Errorf("Contracts() = %v, want %v", got, want)
	}
	for name, keys := range want {
		if !slices.Equal(got[name], keys) {
			t.Errorf("Contracts()[%s] = %v, want %v", name, got[name], keys)
		}
	}
	for name, keys := range got {
		params, _ := Params(name)
		for _, key := range keys {
			if !slices.Contains(params, key) {
				t.Errorf("%s checks against a contract by %q, which is not among its params %v", name, key, params)
			}
		}
	}
}

// TestParamsCoverTheDocs checks that each key the reference page of an
// action lists in its parameters is one Params gives, so that probe check
// does not refuse a key the action takes. Keys of a map nested in with,
// which the pages list too, are left out.
func TestParamsCoverTheDocs(t *testing.T) {
	nested := map[string][]string{
		// multipart's fields
		"http": {"file", "content", "filename", "content_type"},
		// the fields of commands
		"imap": {"name", "mailbox", "oldmailbox", "reference", "criteria", "sequence", "dataitem", "value"},
	}
	row := regexp.MustCompile("(?m)^\\| `([a-z_]+)`")
	for name, keys := range AllParams() {
		data, err := os.ReadFile(filepath.Join("..", "docs", "content", "reference", "actions", name+".md"))
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		page := string(data)
		i := strings.Index(page, "## Parameters")
		if i < 0 {
			continue
		}
		section := page[i:]
		if j := strings.Index(section[len("## Parameters"):], "\n## "); j >= 0 {
			section = section[:len("## Parameters")+j]
		}
		for _, m := range row.FindAllStringSubmatch(section, -1) {
			if !slices.Contains(keys, m[1]) && !slices.Contains(nested[name], m[1]) {
				t.Errorf("the %s page lists %s, which Params does not give", name, m[1])
			}
		}
	}
}
