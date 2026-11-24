package probe

import "testing"

func TestBuiltinAction(t *testing.T) {
	if serve, ok := BuiltinAction("shell"); !ok || serve == nil {
		t.Error("BuiltinAction(shell) gave no serve function")
	}
	if _, ok := BuiltinAction("no-such-action"); ok {
		t.Error("BuiltinAction() found an action that is not built in")
	}
}
