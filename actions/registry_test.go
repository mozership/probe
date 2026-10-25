package actions

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/mozership/probe/actionrpc"
	"github.com/mozership/probe/internal/actionspec"
	"github.com/mozership/probe/internal/kind"
)

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
	// The others keep to every kind.
	for _, name := range []string{"db", "dns", "embedded", "grpc", "hello", "http"} {
		for _, k := range known {
			if !slices.Contains(Keeps()[name], k) {
				t.Errorf("%s should keep to %q, got %v", name, k, Keeps()[name])
			}
		}
	}
	k := Keeps()
	k["http"][0] = "changed"
	if Keeps()["http"][0] == "changed" {
		t.Error("changing what Keeps returns should change no declaration")
	}
}

func TestParams(t *testing.T) {
	for _, name := range Names() {
		params, ok := AllParams()[name]
		if name == "hello" {
			if ok {
				t.Errorf("hello takes any key, want no params, got %v", params)
			}
			continue
		}
		if !ok || len(params) == 0 {
			t.Errorf("AllParams()[%s] = %v, %v; want the keys it takes", name, params, ok)
		}
	}
	if _, ok := AllParams()["no-such-action"]; ok {
		t.Error("an action that is not built in should have no params")
	}
	http := AllParams()["http"]
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
		params := AllParams()[name]
		for _, key := range keys {
			if !slices.Contains(params, key) {
				t.Errorf("%s checks against a contract by %q, which is not among its params %v", name, key, params)
			}
		}
	}
}

// TestParamsCoverTheDocs checks that each key the reference page of an
// action lists in its parameters is one AllParams gives, so that probe check
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
				t.Errorf("the %s page lists %s, which AllParams does not give", name, m[1])
			}
		}
	}
}

// Each built-in action declares the fields of what it returns, and the keys
// of with it needs are keys it takes. That the fields are those it returns
// is tested in each action's package, by running it.
func TestFieldsAndRequired(t *testing.T) {
	req, res := AllReqFields(), AllResFields()
	required := AllRequired()
	for _, name := range Names() {
		if _, ok := req[name]; !ok || len(res[name]) == 0 {
			t.Errorf("%s declares no fields of req or of res: %v, %v", name, req[name], res[name])
		}
		params, takesAny := AllParams()[name]
		if name == "hello" {
			if _, ok := required[name]; ok || takesAny {
				t.Errorf("hello needs no key of with, want none required")
			}
			continue
		}
		if len(required[name]) == 0 {
			t.Errorf("%s declares no key of with it needs", name)
		}
		for _, group := range required[name] {
			for _, key := range group {
				if !slices.Contains(params, key) {
					t.Errorf("%s needs %q, which is not a key it takes", name, key)
				}
			}
		}
	}
	for name := range res {
		if !slices.Contains(Names(), name) {
			t.Errorf("%s declares fields but is not a built-in action", name)
		}
	}
}

// Each built-in action declares the kinds of fields it declares it returns,
// and of no other. That the kinds are those of what it returns is tested in
// each action's package, by running it.
func TestTypesAreOfFields(t *testing.T) {
	for part, all := range map[string]struct {
		fields map[string][]string
		types  map[string]map[string]string
	}{
		"req": {AllReqFields(), AllReqTypes()},
		"res": {AllResFields(), AllResTypes()},
	} {
		for _, name := range Names() {
			types, ok := all.types[name]
			if !ok {
				t.Errorf("%s declares no kinds of the fields of its %s", name, part)
			}
			for field := range types {
				if !slices.Contains(all.fields[name], field) {
					t.Errorf("%s declares the kind of %s.%s, which is not a field it declares", name, part, field)
				}
			}
		}
		if len(all.types) != len(Names()) {
			t.Errorf("the kinds of %s are declared for %d actions, want %d", part, len(all.types), len(Names()))
		}
	}
	// A field declared without a kind may hold anything, so that nothing is
	// checked of what reads it; each is one that does.
	untyped := map[string][]string{"http": {"req.multipart"}}
	got := map[string][]string{}
	for _, name := range Names() {
		for part, all := range map[string]struct {
			fields []string
			types  map[string]string
		}{
			"req": {AllReqFields()[name], AllReqTypes()[name]},
			"res": {AllResFields()[name], AllResTypes()[name]},
		} {
			for _, field := range all.fields {
				if _, ok := all.types[field]; !ok {
					got[name] = append(got[name], part+"."+field)
				}
			}
		}
		slices.Sort(got[name])
	}
	for _, name := range Names() {
		if !slices.Equal(got[name], untyped[name]) {
			t.Errorf("%s declares no kind of %v, want none but %v", name, got[name], untyped[name])
		}
	}
	// The status code of an http response is a number, which a test
	// compares with a number.
	if got := AllResTypes()["http"]["code"]; got != kind.Number {
		t.Errorf("http res.code is a %q, want a number", got)
	}
}

// TestTypesMatchTheDocs checks that the type the reference page of a
// built-in action gives a field of res or req, in a row such as | `res.code`
// | Integer |, is the kind the action declares, so that probe check reports
// what the page says. A field a page gives as Any, as a body is, which is
// read as the JSON it holds, is of any kind whatever the action declares.
func TestTypesMatchTheDocs(t *testing.T) {
	words := map[string]string{
		"integer": kind.Number, "int": kind.Number, "number": kind.Number, "float": kind.Number,
		"string":  kind.String,
		"boolean": kind.Bool, "bool": kind.Bool,
		"object": kind.Map, "map": kind.Map,
		"array": kind.List, "list": kind.List,
	}
	row := regexp.MustCompile("(?m)^\\| `(res|req)\\.([a-z_]+)` \\| ([A-Za-z]+) \\|")
	declared := map[string]map[string]map[string]string{"req": AllReqTypes(), "res": AllResTypes()}
	rows := 0
	for _, name := range Names() {
		data, err := os.ReadFile(filepath.Join("..", "docs", "content", "reference", "actions", name+".md"))
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		for _, m := range row.FindAllStringSubmatch(string(data), -1) {
			rows++
			part, field, word := m[1], m[2], strings.ToLower(m[3])
			got, ok := declared[part][name][field]
			if word == "any" {
				continue
			}
			want, known := words[word]
			switch {
			case !known:
				t.Errorf("the %s page gives %s.%s the type %s, which is no kind", name, part, field, m[3])
			case !ok:
				// What the runner adds, as res.rawbody, and what a field
				// of an action holds that it does not declare a kind of.
			case got != want:
				t.Errorf("the %s page gives %s.%s the type %s, but the action declares it a %s", name, part, field, m[3], got)
			}
		}
	}
	if rows == 0 {
		t.Error("no page gives the type of a field, so nothing was checked")
	}
}

// Each name Names gives is served, the embedded action as well, which init
// adds.
func TestLookup(t *testing.T) {
	if !slices.Contains(Names(), "embedded") {
		t.Errorf("Names() = %v, want embedded among them", Names())
	}
	for _, name := range Names() {
		if serve, ok := Lookup(name); !ok || serve == nil {
			t.Errorf("Lookup(%s) gave no serve function", name)
		}
	}
	if _, ok := Lookup("no-such-action"); ok {
		t.Error("Lookup() found an action that is not built in")
	}
}

// Each built-in action declares every field of its spec, an action that has
// none of something saying so, so that one that forgets a field, such as the
// guards it keeps to, is told apart from one that keeps to none.
func TestSpecsDeclareEveryField(t *testing.T) {
	for _, name := range Names() {
		if missing := specs[name]().Undeclared(); len(missing) > 0 {
			t.Errorf("%s leaves %v undeclared", name, missing)
		}
	}
}

// spoil changes each element of the lists and maps in v, a value of what the
// registry or a spec gives, so that a test can tell whether any of them is
// held by what gives it.
func spoil(v reflect.Value) {
	switch v.Kind() {
	case reflect.Map:
		for _, k := range v.MapKeys() {
			e := v.MapIndex(k)
			if e.Kind() == reflect.String {
				v.SetMapIndex(k, reflect.ValueOf("spoiled"))
				continue
			}
			spoil(e)
		}
	case reflect.Slice:
		for i := range v.Len() {
			spoil(v.Index(i))
		}
	case reflect.String:
		v.SetString("spoiled")
	case reflect.Struct:
		for _, field := range v.Fields() {
			if field.Kind() != reflect.String {
				spoil(field)
			}
		}
	}
}

// specString formats s, but for its serve function, as what a test compares
// copies none of the lists and maps of s.
func specString(s actionspec.Spec) string {
	s.Serve = nil
	return fmt.Sprintf("%+v", s)
}

// What the spec of each built-in action gives is built afresh on each call,
// so that changing it changes no later call.
func TestSpecsAreFresh(t *testing.T) {
	for _, name := range Names() {
		want := specString(specs[name]())
		got := specs[name]()
		spoil(reflect.ValueOf(&got).Elem())
		if again := specString(specs[name]()); again != want {
			t.Errorf("changing the spec of %s changed what it gives next: %s, want %s", name, again, want)
		}
	}
}

// Each function of the registry returns what nothing else holds, so that
// what a caller does with it changes no later call, and so what a guard
// keeps to under --read-only and --allow-host.
func TestRegistryReturnsCopies(t *testing.T) {
	for name, get := range map[string]func() any{
		"Keeps":        func() any { return Keeps() },
		"Contracts":    func() any { return Contracts() },
		"AllParams":    func() any { return AllParams() },
		"AllReqFields": func() any { return AllReqFields() },
		"AllResFields": func() any { return AllResFields() },
		"AllReqTypes":  func() any { return AllReqTypes() },
		"AllResTypes":  func() any { return AllResTypes() },
		"AllRequired":  func() any { return AllRequired() },
		"WithFields":   func() any { return WithFields() },
		"Names":        func() any { return Names() },
	} {
		want := fmt.Sprint(get())
		spoil(reflect.ValueOf(get()))
		if got := fmt.Sprint(get()); got != want {
			t.Errorf("changing what %s returns changed what it returns next: %v, want %v", name, got, want)
		}
	}
}

// WithFields gives the actions that declare their req and res hold the keys
// of with: hello alone.
func TestWithFields(t *testing.T) {
	if got := WithFields(); !slices.Equal(got, []string{"hello"}) {
		t.Errorf("WithFields() = %v, want [hello]", got)
	}
}
