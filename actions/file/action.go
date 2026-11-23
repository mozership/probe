package file

import (
	"github.com/hashicorp/go-hclog"
	"github.com/mozership/probe/actionrpc"
	"github.com/mozership/probe/internal/actionspec"
	"github.com/mozership/probe/internal/kind"
	"github.com/mozership/probe/internal/mapping"
)

type Action struct {
	log hclog.Logger
}

func (a *Action) Run(with map[string]any) (map[string]any, error) {
	actionrpc.LogParams(a.log, "received file request parameters", with)

	ret, err := Read(with)
	actionrpc.LogOutcome(a.log, "file read", ret, err)

	return ret, err
}

func serve() {
	actionrpc.Serve(func(log hclog.Logger) actionrpc.Action {
		return &Action{log: log}
	})
}

// Spec declares the file action. It keeps to every kind of guard, as it
// reads a file and connects to nothing, and cannot run without path.
func Spec() actionspec.Spec {
	return actionspec.Spec{
		Serve:     serve,
		Keeps:     []string{actionrpc.KindReadOnly, actionrpc.KindAllowHost},
		Params:    mapping.FieldTags(Req{}),
		Contracts: actionspec.NoContracts(),
		ReqFields: mapping.FieldTags(Req{}),
		ResFields: []string{"content", "exists", "group", "mode", "modified", "owner", "size", "type"},
		ReqTypes:  kind.Fields(Req{}),
		ResTypes: map[string]string{
			"content":  kind.String,
			"group":    kind.String,
			"mode":     kind.String,
			"modified": kind.String,
			"owner":    kind.String,
			"size":     kind.Number,
			"exists":   kind.Bool,
			"type":     kind.String,
		},
		Required: [][]string{{"path"}},
	}
}
