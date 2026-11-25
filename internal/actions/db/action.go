package db

import (
	"fmt"

	"github.com/hashicorp/go-hclog"
	"github.com/mozership/probe/internal/actionspec"
	"github.com/mozership/probe/internal/kind"
	"github.com/mozership/probe/internal/mapping"
	"github.com/mozership/probe/sdk"
)

type Action struct {
	log hclog.Logger
}

func (a *Action) Run(with map[string]any) (map[string]any, error) {
	ret, _, err := a.RunStep(sdk.Call{With: with})
	return ret, err
}

// RunStep runs the query under the guard of the run, which may allow only
// reading, or only some hosts.
func (a *Action) RunStep(call sdk.Call) (map[string]any, map[string]any, error) {
	ret, err := a.run(call.With, call.Guard)
	return ret, nil, err
}

func (a *Action) run(with map[string]any, guard sdk.Guard) (map[string]any, error) {
	// The password in the DSN is hidden by the workflow runner, which learns
	// it before the action starts and masks the records this action logs.
	sdk.LogParams(a.log, "received db request parameters", with)

	// Validate required parameters
	dsnVal, exists := with["dsn"]
	if !exists || dsnVal == nil {
		return map[string]any{}, fmt.Errorf("dsn parameter is required")
	}
	dsn, ok := dsnVal.(string)
	if !ok || dsn == "" {
		return map[string]any{}, fmt.Errorf("dsn parameter must be a non-empty string")
	}

	queryVal, exists := with["query"]
	if !exists || queryVal == nil {
		return map[string]any{}, fmt.Errorf("query parameter is required")
	}
	query, ok := queryVal.(string)
	if !ok || query == "" {
		return map[string]any{}, fmt.Errorf("query parameter must be a non-empty string")
	}

	// Execute database query with logger callbacks
	result, err := ExecuteQuery(with,
		WithBefore(func(query string, params []any) {
			a.log.Trace("executing database query", "query", query, "params", params)
		}),
		WithAfter(func(result *Result) {
			a.log.Trace("database query completed", "rows_affected", result.Res.RowsAffected, "duration", result.RT)
		}),
		WithGuard(guard),
	)
	sdk.LogOutcome(a.log, "database query", result, err)

	return result, err
}

func serve() {
	sdk.Serve(func(log hclog.Logger) sdk.Action {
		return &Action{log: log}
	})
}

// Spec declares the db action. It keeps to every kind of guard: it runs
// only a statement that reads under read-only, and connects only to a host
// the guard allows. It cannot run without dsn and query. It is being
// replaced by github.com/mozership/probe-db, which a workflow can name db.
func Spec() actionspec.Spec {
	return actionspec.Spec{
		Serve:     serve,
		Keeps:     []string{sdk.KindReadOnly, sdk.KindAllowHost},
		Params:    mapping.FieldTags(Req{}),
		Contracts: actionspec.NoContracts(),
		ReqFields: mapping.FieldTags(Req{}),
		ResFields: mapping.FieldTags(Res{}),
		ReqTypes:  kind.Fields(Req{}),
		ResTypes:  kind.Fields(Res{}),
		Required:  [][]string{{"dsn"}, {"query"}},
		Choices:   actionspec.NoChoices(),

		Replaceable: true,
	}
}
