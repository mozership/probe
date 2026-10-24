package http

import (
	"errors"
	hp "net/http"
	"strings"

	"github.com/hashicorp/go-hclog"
	"github.com/mozership/probe/actionrpc"
	"github.com/mozership/probe/mapping"
)

type Action struct {
	log hclog.Logger
}

func (a *Action) Run(with map[string]any) (map[string]any, error) {
	ret, _, err := a.RunStep(actionrpc.Call{With: with})
	return ret, err
}

// RunStep runs the request for a step: with the cookies the job keeps, when
// the step asks for keep_cookies, returning the cookies to keep, and with a
// header that names the step, when it asks for trace_header.
func (a *Action) RunStep(call actionrpc.Call) (map[string]any, map[string]any, error) {
	with := call.With
	// Validate that required parameters are provided
	if len(with) == 0 {
		return map[string]any{}, nil, errors.New("http action requires parameters in 'with' section. Please specify request details like url, method, or use method fields (get, post, etc.)")
	}

	actionrpc.LogParams(a.log, "received request parameters", with)

	// The request and the response are logged as the parameters are, with
	// credentials such as cookies hidden: the runner learns those in the
	// response only once it has the result, after these records have gone.
	before := WithBefore(func(req *hp.Request) {
		actionrpc.LogParams(a.log, "http request prepared", map[string]any{
			"method":  req.Method,
			"url":     req.URL.String(),
			"headers": joinHeader(req.Header),
		})
	})
	after := WithAfter(func(res *hp.Response) {
		actionrpc.LogParams(a.log, "http response received", map[string]any{
			"status":  res.Status,
			"headers": joinHeader(res.Header),
		})
	})
	ret, newState, err := RequestStep(call, before, after)

	actionrpc.LogOutcome(a.log, "http request", ret, err)

	return ret, newState, err
}

func Serve() {
	actionrpc.Serve(func(log hclog.Logger) actionrpc.Action {
		return &Action{log: log}
	})
}

// joinHeader returns h with the values of each header joined, as the result
// shows them.
func joinHeader(h hp.Header) map[string]any {
	out := make(map[string]any, len(h))
	for k, v := range h {
		out[k] = strings.Join(v, ", ")
	}
	return out
}

// Keeps returns the kinds of guard the http action keeps to: it sends only
// GET, HEAD and OPTIONS under read-only, and connects only to a host the
// guard allows, redirects included.
func Keeps() []string {
	return []string{actionrpc.KindReadOnly, actionrpc.KindAllowHost}
}

// Contracts returns the keys of with that ask the action to check a step
// against a contract: openapi, an OpenAPI document.
func Contracts() []string {
	return []string{"openapi"}
}

// Params returns the keys the http action takes in with: those of Req,
// the method shorthands, such as get and post, and those it reads apart
// from Req.
func Params() []string {
	params := mapping.FieldTags(Req{})
	for _, m := range httpMethods {
		params = append(params, strings.ToLower(m))
	}
	return append(params, "basic_auth", "cookies", "form", "keep_cookies", "multipart", "openapi", "trace_header")
}

// ReqFields returns the fields of the req the http action returns: those of
// Req, and multipart, for a request that sends one. ResFields returns those
// of its res: those of Res, and violations and contract, for a response
// checked against an OpenAPI document.
func ReqFields() []string {
	return append(mapping.FieldTags(Req{}), "multipart")
}

// ResFields returns the fields of the res the http action returns; see
// ReqFields.
func ResFields() []string {
	return append(mapping.FieldTags(Res{}), "contract", "violations")
}

// Required returns the keys of with the http action cannot run without, each
// as the keys that stand for it: the URL, which url or a method shorthand,
// such as get, gives, and the method, which method or a shorthand gives.
func Required() [][]string {
	var shorthands []string
	for _, m := range httpMethods {
		shorthands = append(shorthands, strings.ToLower(m))
	}
	return [][]string{
		append([]string{"url"}, shorthands...),
		append([]string{"method"}, shorthands...),
	}
}
