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
)

// builtin maps an action name to the function that serves it. Each value
// blocks until the workflow runner closes the plugin connection. The embedded
// action is added by init.
var builtin = map[string]func(){
	"db":    db.Serve,
	"dns":   dns.Serve,
	"grpc":  grpc.Serve,
	"hello": hello.Serve,
	"http":  http.Serve,
	"imap":  imap.Serve,
	"shell": shell.Serve,
	"smtp":  smtp.Serve,
	"ssh":   ssh.Serve,
}

// keeps are the kinds of guard each built-in action declares it keeps to,
// as its package says. An action that keeps to none is left out: it is
// refused under a guard unless it is allowed by name.
var keeps = map[string]func() []string{
	"db":       db.Keeps,
	"dns":      dns.Keeps,
	"embedded": embedded.Keeps,
	"grpc":     grpc.Keeps,
	"hello":    hello.Keeps,
	"http":     http.Keeps,
}

// Keeps returns the kinds of guard each built-in action keeps to, by its
// name, leaving out those that keep to none. It is what a guard's Keeps
// starts from.
func Keeps() map[string][]string {
	m := make(map[string][]string, len(keeps))
	for name, f := range keeps {
		m[name] = f()
	}
	return m
}

// params are the keys each built-in action takes in with. hello, which
// takes any, is left out.
var params = map[string]func() []string{
	"db":       db.Params,
	"dns":      dns.Params,
	"embedded": embedded.Params,
	"grpc":     grpc.Params,
	"http":     http.Params,
	"imap":     imap.Params,
	"shell":    shell.Params,
	"smtp":     smtp.Params,
	"ssh":      ssh.Params,
}

// contracts are the keys of with that ask each built-in action to check a
// step against a contract. An action that checks none is left out.
var contracts = map[string]func() []string{
	"grpc": grpc.Contracts,
	"http": http.Contracts,
}

// Contracts returns the keys of with that ask each built-in action to check
// a step against a contract, by its name, leaving out those that check
// none.
func Contracts() map[string][]string {
	out := make(map[string][]string, len(contracts))
	for name, f := range contracts {
		out[name] = f()
	}
	return out
}

// AllParams returns the keys each built-in action takes in with, by its
// name, leaving out those that take any key.
func AllParams() map[string][]string {
	out := make(map[string][]string, len(params))
	for name, f := range params {
		out[name] = f()
	}
	return out
}

// reqFields and resFields are the fields of the req and the res each
// built-in action returns, as its package declares them.
var (
	reqFields = map[string]func() []string{
		"db":       db.ReqFields,
		"dns":      dns.ReqFields,
		"embedded": embedded.ReqFields,
		"grpc":     grpc.ReqFields,
		"hello":    hello.ReqFields,
		"http":     http.ReqFields,
		"imap":     imap.ReqFields,
		"shell":    shell.ReqFields,
		"smtp":     smtp.ReqFields,
		"ssh":      ssh.ReqFields,
	}
	resFields = map[string]func() []string{
		"db":       db.ResFields,
		"dns":      dns.ResFields,
		"embedded": embedded.ResFields,
		"grpc":     grpc.ResFields,
		"hello":    hello.ResFields,
		"http":     http.ResFields,
		"imap":     imap.ResFields,
		"shell":    shell.ResFields,
		"smtp":     smtp.ResFields,
		"ssh":      ssh.ResFields,
	}
)

// AllReqFields returns the fields of the req each built-in action returns,
// by its name.
func AllReqFields() map[string][]string {
	return collect(reqFields)
}

// AllResFields returns the fields of the res each built-in action returns,
// by its name.
func AllResFields() map[string][]string {
	return collect(resFields)
}

// reqTypes and resTypes are the kinds of the fields of the req and the res
// each built-in action returns, as its package declares them.
var (
	reqTypes = map[string]func() map[string]string{
		"db":       db.ReqTypes,
		"dns":      dns.ReqTypes,
		"embedded": embedded.ReqTypes,
		"grpc":     grpc.ReqTypes,
		"hello":    hello.ReqTypes,
		"http":     http.ReqTypes,
		"imap":     imap.ReqTypes,
		"shell":    shell.ReqTypes,
		"smtp":     smtp.ReqTypes,
		"ssh":      ssh.ReqTypes,
	}
	resTypes = map[string]func() map[string]string{
		"db":       db.ResTypes,
		"dns":      dns.ResTypes,
		"embedded": embedded.ResTypes,
		"grpc":     grpc.ResTypes,
		"hello":    hello.ResTypes,
		"http":     http.ResTypes,
		"imap":     imap.ResTypes,
		"shell":    shell.ResTypes,
		"smtp":     smtp.ResTypes,
		"ssh":      ssh.ResTypes,
	}
)

// AllReqTypes returns the kinds of the fields of the req each built-in
// action returns, by its name, as number, string, bool, map or list, for
// probe check to report an expression that does with a field what its kind
// does not allow. A field left out may hold anything.
func AllReqTypes() map[string]map[string]string {
	return collectTypes(reqTypes)
}

// AllResTypes returns the kinds of the fields of the res each built-in
// action returns, by its name; see AllReqTypes.
func AllResTypes() map[string]map[string]string {
	return collectTypes(resTypes)
}

func collectTypes(m map[string]func() map[string]string) map[string]map[string]string {
	out := make(map[string]map[string]string, len(m))
	for name, f := range m {
		out[name] = f()
	}
	return out
}

// WithFields returns the names of the built-in actions whose req and res
// hold the keys of the step's with, besides the fields they declare: hello,
// which returns what it is given.
func WithFields() []string {
	return []string{"hello"}
}

// NeedsNoCheck returns the names of the built-in actions whose steps pass
// without a test: hello, which does nothing that could fail, so that a step
// of it there for its echo, or to publish what it is given, is not counted
// as untested.
func NeedsNoCheck() []string {
	return []string{"hello"}
}

func collect(m map[string]func() []string) map[string][]string {
	out := make(map[string][]string, len(m))
	for name, f := range m {
		out[name] = f()
	}
	return out
}

// required are the keys of with each built-in action cannot run without.
// hello, which needs none, is left out.
var required = map[string]func() [][]string{
	"db":       db.Required,
	"dns":      dns.Required,
	"embedded": embedded.Required,
	"grpc":     grpc.Required,
	"http":     http.Required,
	"imap":     imap.Required,
	"shell":    shell.Required,
	"smtp":     smtp.Required,
	"ssh":      ssh.Required,
}

// AllRequired returns the keys of with each built-in action cannot run
// without, by its name, each as the keys that stand for one another, such
// as url and get for the http action, of which one is enough. An action
// that needs none is left out.
func AllRequired() map[string][][]string {
	out := make(map[string][][]string, len(required))
	for name, f := range required {
		out[name] = f()
	}
	return out
}

// Lookup returns the serve function of a built-in action.
func Lookup(name string) (func(), bool) {
	serve, ok := builtin[name]
	return serve, ok
}

// The embedded action runs steps of its own, so it is told which actions
// there are. It is added here rather than in builtin, which Names reads, as
// referring to Names there would be an initialization cycle.
func init() {
	builtin["embedded"] = func() { embedded.ServeWithActions(Names(), NeedsNoCheck()) }
}

// Names returns the names of the built-in actions in alphabetical order.
func Names() []string {
	names := make([]string, 0, len(builtin))
	for name := range builtin {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}
