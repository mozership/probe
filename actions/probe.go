package actions

import "github.com/mozership/probe"

// Configure tells c what the engine is to know of the built-in actions: their
// names, which a workflow cannot give to an external action, and those whose
// steps pass without a test. The kinds of guard each keeps to are given with
// the guard, from Keeps.
func Configure(c *probe.Config) {
	c.Actions = Names()
	c.NeedsNoCheck = NeedsNoCheck()
}

// CheckOptions returns what probe check is to know of the built-in actions.
// Its Manifest is left nil, so that nothing is read of an external action,
// from the network or elsewhere, unless the caller sets it, as to
// actionref.ReadManifest.
func CheckOptions() probe.CheckOptions {
	return probe.CheckOptions{
		Actions:      Names(),
		Params:       AllParams(),
		Contracts:    Contracts(),
		ReqFields:    AllReqFields(),
		ResFields:    AllResFields(),
		ReqTypes:     AllReqTypes(),
		ResTypes:     AllResTypes(),
		WithFields:   WithFields(),
		NeedsNoCheck: NeedsNoCheck(),
		Required:     AllRequired(),
	}
}
