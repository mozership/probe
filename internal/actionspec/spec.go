// Package actionspec declares what Probe knows of a built-in action: how to
// serve it, the guards it keeps to, and what probe check knows of the keys
// it takes and the fields it returns.
//
// Each built-in action declares its Spec by a function in its own package,
// which builds it afresh on each call, so that what one caller does with the
// lists and maps of a Spec changes what no other is given. The registry in
// package actions maps the action names to those functions.
//
// Every field is declared, an action that has none of something saying so
// with KeepsNone, NoContracts, NoneRequired or AnyParams, so that one which
// forgets a field is told apart from one that has nothing to give;
// Undeclared reports the fields left out.
package actionspec

// Spec declares a built-in action.
type Spec struct {
	// Serve serves the action over the plugin protocol. It blocks until the
	// workflow runner closes the plugin connection.
	Serve func()
	// Keeps are the kinds of guard the action keeps to, of
	// actionrpc.KindReadOnly and actionrpc.KindAllowHost. An action that
	// keeps to none, as it cannot tell what it is about to do, declares
	// KeepsNone, and is refused under a guard unless it is allowed by name.
	Keeps []string
	// Params are the keys the action takes in with. An action that takes
	// any key declares AnyParams instead and leaves Params nil. The fields
	// of a Req, which mapping.FieldTags gives, declare Params even when
	// none is tagged, as FieldTags then gives an empty list.
	Params []string
	// AnyParams says that the action takes any key in with, so that probe
	// check refuses none.
	AnyParams bool
	// Contracts are the keys of with that ask the action to check a step
	// against a contract, such as openapi for the http action. An action
	// that checks none declares NoContracts.
	Contracts []string
	// ReqFields and ResFields are the fields of the req and the res the
	// action returns, for probe check to report a step that reads another.
	ReqFields []string
	ResFields []string
	// ReqTypes and ResTypes are the kinds of those fields, as number,
	// string, bool, map or list, for probe check to report an expression
	// that does with a field what its kind does not allow. A field left out
	// may hold anything.
	ReqTypes map[string]string
	ResTypes map[string]string
	// WithFields says that the req and the res hold the keys of the step's
	// with besides the fields declared, as they do for an action that
	// returns what it is given.
	WithFields bool
	// Required are the keys of with the action cannot run without, each as
	// the keys that stand for one another, of which one is enough. An action
	// that needs none declares NoneRequired.
	Required [][]string
}

// KeepsNone declares an action that keeps to no guard. It is empty but not
// nil, which would leave Keeps undeclared, as are NoContracts and
// NoneRequired.
func KeepsNone() []string { return []string{} }

// NoContracts declares an action that checks a step against no contract.
func NoContracts() []string { return []string{} }

// NoneRequired declares an action that needs no key of with.
func NoneRequired() [][]string { return [][]string{} }

// Undeclared returns the names of the fields s leaves undeclared, in the
// order Spec has them, or none when s declares every field. Params is
// undeclared when it is nil without AnyParams, and declared twice when it is
// given with AnyParams, which Undeclared reports too. The bools, whose zero
// value says no, are declared by being left out.
func (s Spec) Undeclared() []string {
	var out []string
	if s.Serve == nil {
		out = append(out, "Serve")
	}
	if s.Keeps == nil {
		out = append(out, "Keeps")
	}
	if s.Params == nil && !s.AnyParams {
		out = append(out, "Params")
	}
	if s.Params != nil && s.AnyParams {
		out = append(out, "Params with AnyParams")
	}
	if s.Contracts == nil {
		out = append(out, "Contracts")
	}
	if s.ReqFields == nil {
		out = append(out, "ReqFields")
	}
	if s.ResFields == nil {
		out = append(out, "ResFields")
	}
	if s.ReqTypes == nil {
		out = append(out, "ReqTypes")
	}
	if s.ResTypes == nil {
		out = append(out, "ResTypes")
	}
	if s.Required == nil {
		out = append(out, "Required")
	}
	return out
}
