package actions

import (
	"github.com/mozership/probe/internal/check"
	"github.com/mozership/probe/internal/engine"
)

// Configure tells c what the engine is to know of the built-in actions: their
// names, which a workflow cannot give to an external action unless the
// action is replaceable, and those whose steps pass without a test. The kinds of guard each keeps to are given with
// the guard, from Keeps.
func Configure(c *engine.Config) {
	c.Actions = Names()
	c.Replaceable = Replaceable()
	c.NeedsNoCheck = NeedsNoCheck()
}

// CheckOptions returns what probe check is to know of the built-in actions.
// Its Manifest is left nil, so that nothing is read of an external action,
// from the network or elsewhere, unless the caller sets it, as to
// actionref.ReadManifest.
func CheckOptions() check.CheckOptions {
	return check.CheckOptions{
		Actions:      Names(),
		Replaceable:  Replaceable(),
		Params:       AllParams(),
		Contracts:    Contracts(),
		ReqFields:    AllReqFields(),
		ResFields:    AllResFields(),
		ReqTypes:     AllReqTypes(),
		ResTypes:     AllResTypes(),
		WithFields:   WithFields(),
		NeedsNoCheck: NeedsNoCheck(),
		Required:     AllRequired(),
		Choices:      AllChoices(),
	}
}
