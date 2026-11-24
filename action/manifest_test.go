package action

import (
	"reflect"
	"testing"
)

func TestParseManifest(t *testing.T) {
	m, err := ParseManifest([]byte(`name: greet
runs:
  using: binary
  path: greet
params: [name, mode]
guard: [read-only]
choices:
  mode: [fast, slow]
cleanup: true
`))
	if err != nil {
		t.Fatal(err)
	}
	if m.Name != "greet" || m.Runs.Path != "greet" || !m.Cleanup {
		t.Errorf("manifest = %+v", m)
	}
	if want := map[string]Choice{"mode": {Values: []string{"fast", "slow"}}}; !reflect.DeepEqual(m.Choices, want) {
		t.Errorf("choices = %v, want %v", m.Choices, want)
	}
}

func TestParseRef(t *testing.T) {
	ref, err := ParseRef("github.com/mozership/probe-slack@0123456789abcdef0123456789abcdef01234567")
	if err != nil {
		t.Fatal(err)
	}
	if ref.Owner != "mozership" || ref.Repo != "probe-slack" || ref.Local != "" {
		t.Errorf("ref = %+v", ref)
	}
	if ref, err := ParseRef("./greet"); err != nil || ref.Local != "./greet" {
		t.Errorf("ParseRef(./greet) = %+v, %v", ref, err)
	}
}
