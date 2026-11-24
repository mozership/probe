package sdk

import "github.com/mozership/probe/internal/actionref"

type (
	// Manifest is the action.yml of an external action.
	Manifest = actionref.Manifest
	// Runs is how the executable of an external action is fetched or found.
	Runs = actionref.Runs
	// Choice is the values a key of with takes, under choices in action.yml.
	Choice = actionref.Choice
	// Ref is a parsed uses that names an external action.
	Ref = actionref.Ref
)

// ParseManifest reads data as an action.yml, as Probe reads it before a run.
func ParseManifest(data []byte) (*Manifest, error) {
	return actionref.ParseManifest(data)
}

// ParseRef parses uses as a reference to an external action.
func ParseRef(uses string) (Ref, error) {
	return actionref.Parse(uses)
}
