package embedded

import (
	"testing"

	"github.com/hashicorp/go-hclog"
	"github.com/mozership/probe/internal/actiontest"
)

func TestFieldsDeclareWhatItReturns(t *testing.T) {
	kinds := actiontest.NewKinds(t, Spec().ReqTypes, Spec().ResTypes)
	a := &Action{log: hclog.NewNullLogger()}
	for _, job := range []string{
		"name: j\nsteps:\n- uses: hello\n  skipif: \"true\"\n",
		"name: j\nsteps:\n- uses: hello\n  id: Not Valid\n",
	} {
		with := map[string]any{"path": writeJob(t, job), "vars": map[string]any{"k": "v"}}
		// A job that cannot run is an error, with a result that tells why
		// in res.error.
		ret, _ := a.Run(with)
		actiontest.CheckFields(t, Spec().ReqFields, Spec().ResFields, nil, ret)
		kinds.Check(ret)
	}
	kinds.Covered()
}

func TestRequired(t *testing.T) {
	a := &Action{log: hclog.NewNullLogger()}
	with := map[string]any{"path": "job.yml", "vars": map[string]any{"k": "v"}}
	actiontest.CheckRequired(t, Spec().Required, with, func(with map[string]any) error {
		_, err := a.Run(with)
		return err
	})
}
