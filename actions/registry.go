// Package actions maps the action names usable in a step's `uses` to the
// built-in plugins that implement them.
//
// Each built-in lives in its own package below this one and serves itself over
// the go-plugin protocol, exactly as an out-of-tree plugin would. This package
// only records which names exist, so that cmd/probe does not have to.
package actions

import (
	"sort"

	"github.com/mozership/probe/actions/db"
	"github.com/mozership/probe/actions/dns"
	"github.com/mozership/probe/actions/embedded"
	"github.com/mozership/probe/actions/grpc"
	"github.com/mozership/probe/actions/hello"
	"github.com/mozership/probe/actions/http"
	"github.com/mozership/probe/actions/imap"
	"github.com/mozership/probe/actions/shell"
	"github.com/mozership/probe/actions/smtp"
	"github.com/mozership/probe/actions/ssh"
	"github.com/mozership/probe/internal/actionspec"
)

// specs maps the name of each built-in action to the function that
// declares it, which builds the declaration afresh on each call, so that
// what each function of this package returns is held by nothing else. The
// embedded action is declared by what init gives it.
var specs = map[string]func() actionspec.Spec{
	"db":       db.Spec,
	"dns":      dns.Spec,
	"embedded": embedded.Spec,
	"grpc":     grpc.Spec,
	"hello":    hello.Spec,
	"http":     http.Spec,
	"imap":     imap.Spec,
	"shell":    shell.Spec,
	"smtp":     smtp.Spec,
	"ssh":      ssh.Spec,
}

// The embedded action runs steps of its own, so it is told which actions
// there are. Its serve function is set here rather than in specs, which
// Names reads, as referring to Names there would be an initialization
// cycle.
func init() {
	specs["embedded"] = func() actionspec.Spec {
		s := embedded.Spec()
		s.Serve = func() { embedded.ServeWithActions(Names(), embedded.WithNeedsNoCheck(NeedsNoCheck())) }
		return s
	}
}

// collect returns what get gives of the spec of each built-in action, by
// its name, leaving out the actions for which it gives false.
func collect[T any](get func(actionspec.Spec) (T, bool)) map[string]T {
	out := make(map[string]T, len(specs))
	for name, spec := range specs {
		if v, ok := get(spec()); ok {
			out[name] = v
		}
	}
	return out
}

// Keeps returns the kinds of guard each built-in action keeps to, by its
// name, leaving out those that keep to none. It is what a guard's Keeps
// starts from.
func Keeps() map[string][]string {
	return collect(func(s actionspec.Spec) ([]string, bool) {
		return s.Keeps, len(s.Keeps) > 0
	})
}

// Contracts returns the keys of with that ask each built-in action to check
// a step against a contract, by its name, leaving out those that check
// none.
func Contracts() map[string][]string {
	return collect(func(s actionspec.Spec) ([]string, bool) {
		return s.Contracts, len(s.Contracts) > 0
	})
}

// AllParams returns the keys each built-in action takes in with, by its
// name, leaving out those that take any key.
func AllParams() map[string][]string {
	return collect(func(s actionspec.Spec) ([]string, bool) {
		return s.Params, !s.AnyParams
	})
}

// AllReqFields returns the fields of the req each built-in action returns,
// by its name.
func AllReqFields() map[string][]string {
	return collect(func(s actionspec.Spec) ([]string, bool) {
		return s.ReqFields, true
	})
}

// AllResFields returns the fields of the res each built-in action returns,
// by its name.
func AllResFields() map[string][]string {
	return collect(func(s actionspec.Spec) ([]string, bool) {
		return s.ResFields, true
	})
}

// AllReqTypes returns the kinds of the fields of the req each built-in
// action returns, by its name, as number, string, bool, map or list, for
// probe check to report an expression that does with a field what its kind
// does not allow. A field left out may hold anything.
func AllReqTypes() map[string]map[string]string {
	return collect(func(s actionspec.Spec) (map[string]string, bool) {
		return s.ReqTypes, true
	})
}

// AllResTypes returns the kinds of the fields of the res each built-in
// action returns, by its name; see AllReqTypes.
func AllResTypes() map[string]map[string]string {
	return collect(func(s actionspec.Spec) (map[string]string, bool) {
		return s.ResTypes, true
	})
}

// WithFields returns, in alphabetical order, the names of the built-in
// actions whose req and res hold the keys of the step's with, besides the
// fields they declare, such as hello, which returns what it is given.
func WithFields() []string {
	var names []string
	for _, name := range Names() {
		if specs[name]().WithFields {
			names = append(names, name)
		}
	}
	return names
}

// NeedsNoCheck returns, in alphabetical order, the names of the built-in
// actions whose steps pass without a test, such as hello, which does nothing
// that could fail, so that a step of it there for its echo, or to publish
// what it is given, is not counted as untested.
func NeedsNoCheck() []string {
	var names []string
	for _, name := range Names() {
		if specs[name]().NeedsNoCheck {
			names = append(names, name)
		}
	}
	return names
}

// AllRequired returns the keys of with each built-in action cannot run
// without, by its name, each as the keys that stand for one another, such
// as url and get for the http action, of which one is enough. An action
// that needs none is left out.
func AllRequired() map[string][][]string {
	return collect(func(s actionspec.Spec) ([][]string, bool) {
		return s.Required, len(s.Required) > 0
	})
}

// Lookup returns the serve function of a built-in action.
func Lookup(name string) (func(), bool) {
	spec, ok := specs[name]
	if !ok {
		return nil, false
	}
	return spec().Serve, true
}

// Names returns the names of the built-in actions in alphabetical order.
func Names() []string {
	names := make([]string, 0, len(specs))
	for name := range specs {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}
