// Package actions serves the actions built into Probe, for a build of Probe
// that adds actions of its own.
package actions

import "github.com/mozership/probe/internal/actions"

// Lookup returns the serve function of a built-in action.
func Lookup(name string) (func(), bool) {
	return actions.Lookup(name)
}
