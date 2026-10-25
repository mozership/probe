package db

import (
	"fmt"

	"github.com/hashicorp/go-hclog"
	"github.com/mozership/probe/actionrpc"
	"github.com/mozership/probe/internal/kind"
	"github.com/mozership/probe/mapping"
)

type Action struct {
	log hclog.Logger
}

func (a *Action) Run(with map[string]any) (map[string]any, error) {
	ret, _, err := a.RunStep(actionrpc.Call{With: with})
	return ret, err
}

// RunStep runs the query under the guard of the run, which may allow only
// reading, or only some hosts.
func (a *Action) RunStep(call actionrpc.Call) (map[string]any, map[string]any, error) {
	ret, err := a.run(call.With, call.Guard)
	return ret, nil, err
}

func (a *Action) run(with map[string]any, guard actionrpc.Guard) (map[string]any, error) {
	// The password in the DSN is hidden by the workflow runner, which learns
	// it before the action starts and masks the records this action logs.
	actionrpc.LogParams(a.log, "received db request parameters", with)

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
	actionrpc.LogOutcome(a.log, "database query", result, err)

	return result, err
}

func Serve() {
	actionrpc.Serve(func(log hclog.Logger) actionrpc.Action {
		return &Action{log: log}
	})
}

// Keeps returns the kinds of guard the db action keeps to: it runs only a
// statement that reads under read-only, and connects only to a host the
// guard allows.
func Keeps() []string {
	return []string{actionrpc.KindReadOnly, actionrpc.KindAllowHost}
}

// Params returns the keys the db action takes in with.
func Params() []string {
	return mapping.FieldTags(Req{})
}

// ReqFields returns the fields of the req the db action returns, those of
// Req, and ResFields those of its res, those of Res, for probe check to
// report a step that reads another.
func ReqFields() []string {
	return mapping.FieldTags(Req{})
}

// ResFields returns the fields of the res the db action returns; see
// ReqFields.
func ResFields() []string {
	return mapping.FieldTags(Res{})
}

// ReqTypes returns the kinds of the fields of the req the db action
// returns, those of Req, and ResTypes those of its res, those of Res, for
// probe check to report an expression that does with a field what its kind
// does not allow.
func ReqTypes() map[string]kind.Kind {
	return kind.Fields(Req{})
}

// ResTypes returns the kinds of the fields of the res the db action
// returns; see ReqTypes.
func ResTypes() map[string]kind.Kind {
	return kind.Fields(Res{})
}

// Required returns the keys of with the db action cannot run without: dsn
// and query.
func Required() [][]string {
	return [][]string{{"dsn"}, {"query"}}
}
