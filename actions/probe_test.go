package actions

import (
	"reflect"
	"slices"
	"testing"

	"github.com/mozership/probe"
)

// TestConfigure checks that a run is told the names of the built-in actions
// and which of them need no test, and that the rest of its configuration is
// left as it was.
func TestConfigure(t *testing.T) {
	c := probe.Config{Verbose: true, Jobs: []string{"j"}}
	Configure(&c)
	if !slices.Equal(c.Actions, Names()) || !slices.Equal(c.NeedsNoCheck, NeedsNoCheck()) {
		t.Errorf("Actions, NeedsNoCheck = %v, %v, want %v, %v", c.Actions, c.NeedsNoCheck, Names(), NeedsNoCheck())
	}
	if !c.Verbose || !slices.Equal(c.Jobs, []string{"j"}) {
		t.Errorf("Configure changed what it does not set: %+v", c)
	}
}

// TestCheckOptions checks that probe check is told every fact the registry
// has, and reads no external action unless it is told to.
func TestCheckOptions(t *testing.T) {
	got := CheckOptions()
	if got.Manifest != nil {
		t.Error("Manifest is set, want nothing read of an external action")
	}
	want := probe.CheckOptions{
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
	if !reflect.DeepEqual(got, want) {
		t.Errorf("CheckOptions() = %+v, want %+v", got, want)
	}
	// Every field but Manifest is set, so that one added to CheckOptions is
	// not left out here unseen.
	v := reflect.ValueOf(got)
	for i := range v.NumField() {
		if name := v.Type().Field(i).Name; name != "Manifest" && v.Field(i).IsZero() {
			t.Errorf("CheckOptions().%s is not set", name)
		}
	}
}
