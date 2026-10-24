// Package actions maps the action names usable in a step's `uses` to the
// built-in plugins that implement them.
//
// Each built-in lives in its own package below this one and serves itself over
// the go-plugin protocol, exactly as an out-of-tree plugin would. This package
// only records which names exist, so that cmd/probe does not have to.
package actions

import (
	"slices"
	"sort"

	"github.com/mozership/probe/actionrpc"

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
// blocks until the workflow runner closes the plugin connection.
var builtin = map[string]func(){
	"db":       db.Serve,
	"dns":      dns.Serve,
	"embedded": embedded.Serve,
	"grpc":     grpc.Serve,
	"hello":    hello.Serve,
	"http":     http.Serve,
	"imap":     imap.Serve,
	"shell":    shell.Serve,
	"smtp":     smtp.Serve,
	"ssh":      ssh.Serve,
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

// Keeping returns the names of the built-in actions that keep to every kind
// of guard, such as --read-only and --allow-host, in alphabetical order.
func Keeping() []string {
	all := []string{actionrpc.KindReadOnly, actionrpc.KindAllowHost}
	var names []string
	for name, kinds := range Keeps() {
		if !slices.ContainsFunc(all, func(k string) bool { return !slices.Contains(kinds, k) }) {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	return names
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

// Params returns the keys the built-in action name takes in with, and false
// for an action that takes any key, or that is not built in.
func Params(name string) ([]string, bool) {
	f, ok := params[name]
	if !ok {
		return nil, false
	}
	return f(), true
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

// fields are the fields of the req and the res each built-in action
// returns, as its package declares them.
var fields = map[string]func() actionrpc.Fields{
	"db":       db.Fields,
	"dns":      dns.Fields,
	"embedded": embedded.Fields,
	"grpc":     grpc.Fields,
	"hello":    hello.Fields,
	"http":     http.Fields,
	"imap":     imap.Fields,
	"shell":    shell.Fields,
	"smtp":     smtp.Fields,
	"ssh":      ssh.Fields,
}

// AllFields returns the fields of the req and the res each built-in action
// returns, by its name.
func AllFields() map[string]actionrpc.Fields {
	out := make(map[string]actionrpc.Fields, len(fields))
	for name, f := range fields {
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
	// The embedded action runs steps of its own, so it is told which
	// actions there are.
	if name == "embedded" {
		return func() { embedded.ServeWithActions(Names()) }, true
	}
	serve, ok := builtin[name]
	return serve, ok
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
