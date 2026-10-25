package http

import (
	"errors"
	hp "net/http"
	"strings"

	"github.com/hashicorp/go-hclog"
	"github.com/mozership/probe/actionrpc"
	"github.com/mozership/probe/internal/actionspec"
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

func serve() {
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

// Spec declares the http action. It keeps to every kind of guard: it sends
// only GET, HEAD and OPTIONS under read-only, and connects only to a host
// the guard allows, redirects included. openapi, an OpenAPI document, asks
// it to check a step against the document. Its req holds, besides the
// fields of Req, multipart, for a request that sends one, which may hold
// anything; its res holds, besides the fields of Res, violations and
// contract, for a response checked against an OpenAPI document.
func Spec() actionspec.Spec {
	return actionspec.Spec{
		Serve:     serve,
		Keeps:     []string{actionrpc.KindReadOnly, actionrpc.KindAllowHost},
		Params:    params(),
		Contracts: []string{"openapi"},
		ReqFields: append(mapping.FieldTags(Req{}), "multipart"),
		ResFields: append(mapping.FieldTags(Res{}), "contract", "violations"),
		ReqTypes:  kind.Fields(Req{}),
		ResTypes:  resTypes(),
		Required:  required(),
	}
}

// params returns the keys the action takes in with: those of Req, the
// method shorthands, such as get and post, and those it reads apart from
// Req.
func params() []string {
	params := mapping.FieldTags(Req{})
	for _, m := range httpMethods {
		params = append(params, strings.ToLower(m))
	}
	return append(params, "basic_auth", "cookies", "form", "keep_cookies", "multipart", "openapi", "trace_header")
}

// resTypes returns the kinds of the fields of Res and of those the action
// adds to its res.
func resTypes() map[string]string {
	types := kind.Fields(Res{})
	types["contract"] = kind.Map
	types["violations"] = kind.List
	return types
}

// required returns the keys of with the action cannot run without, each as
// the keys that stand for it: the URL, which url or a method shorthand, such
// as get, gives, and the method, which method or a shorthand gives.
func required() [][]string {
	var shorthands []string
	for _, m := range httpMethods {
		shorthands = append(shorthands, strings.ToLower(m))
	}
	return [][]string{
		append([]string{"url"}, shorthands...),
		append([]string{"method"}, shorthands...),
	}
}
