package actions

import "testing"

func TestLookup(t *testing.T) {
	if serve, ok := Lookup("shell"); !ok || serve == nil {
		t.Error("Lookup(shell) gave no serve function")
	}
	if _, ok := Lookup("no-such-action"); ok {
		t.Error("Lookup() found an action that is not built in")
	}
}
