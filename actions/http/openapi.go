package http

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	hp "net/http"
	"os"
	"slices"
	"strconv"
	"strings"

	"github.com/mozership/probe/binary"
	"github.com/pb33f/libopenapi"
	validator "github.com/pb33f/libopenapi-validator"
	"github.com/pb33f/libopenapi-validator/config"
	verrors "github.com/pb33f/libopenapi-validator/errors"
	"github.com/pb33f/libopenapi-validator/helpers"
	"github.com/pb33f/libopenapi-validator/paths"
	"github.com/pb33f/libopenapi/datamodel/high/base"
	v3 "github.com/pb33f/libopenapi/datamodel/high/v3"
	"github.com/pb33f/libopenapi/orderedmap"
)

// contract checks requests and responses against an OpenAPI document.
type contract struct {
	v validator.Validator
	// request is whether the request is checked as well as the response. A
	// step that sends what the document does not allow on purpose, to see
	// it rejected, turns it off.
	request bool
	// strict is whether what the document does not declare breaks it.
	strict bool
	// spec is the path of the document, as the step gives it.
	spec string
	// model is the document, which tells which operation and response a
	// response was matched to, and which undeclared walks.
	model *v3.Document
}

// takeOpenAPI removes openapi from the parameters and returns the contract it
// names, or nil when the step checks nothing: when openapi is missing, or
// false, as a step writes it to leave out a check its job's defaults ask for.
// The document is read before the request is sent, so that one that cannot
// be read fails the step without sending anything.
func takeOpenAPI(m map[string]any) (*contract, error) {
	v, ok := m["openapi"]
	if !ok {
		return nil, nil
	}
	delete(m, "openapi")

	switch o := v.(type) {
	case bool:
		if o {
			return nil, errors.New("openapi must be a map with spec, or false")
		}
		return nil, nil
	case map[string]any:
		for k := range o {
			if k != "spec" && k != "request" && k != "strict" {
				return nil, fmt.Errorf("openapi takes spec, request and strict, not %s", k)
			}
		}
		path, ok := o["spec"].(string)
		if !ok || path == "" {
			return nil, errors.New("openapi.spec must be the path of an OpenAPI document")
		}
		request, err := openAPIFlag(o, "request", true)
		if err != nil {
			return nil, err
		}
		strict, err := openAPIFlag(o, "strict", false)
		if err != nil {
			return nil, err
		}
		c, err := loadContract(path, strict)
		if err != nil {
			return nil, err
		}
		c.request = request
		return c, nil
	default:
		return nil, errors.New("openapi must be a map with spec, or false")
	}
}

// openAPIFlag returns the boolean key of openapi, or def when it is missing.
func openAPIFlag(o map[string]any, key string, def bool) (bool, error) {
	v, ok := o[key]
	if !ok {
		return def, nil
	}
	b, ok := v.(bool)
	if !ok {
		return false, fmt.Errorf("openapi.%s must be true or false", key)
	}
	return b, nil
}

// loadContract reads the OpenAPI document at path. When strict is true, a
// property of a JSON body that its schema does not declare breaks the
// document where the schema leaves additionalProperties out, as undeclared
// says, and so do a query parameter the operation does not declare, a
// readOnly property in a request and a writeOnly one in a response.
func loadContract(path string, strict bool) (*contract, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("openapi.spec: %w", err)
	}
	doc, err := libopenapi.NewDocument(data)
	if err != nil {
		return nil, fmt.Errorf("openapi.spec: %s: %w", path, err)
	}
	// Bodies of the types the validator can decode besides JSON, such as
	// text, XML and forms, are checked against their schemas too. A body of
	// a type it cannot decode, such as an image or a PDF, is left
	// unchecked rather than failed, as a document declares such bodies
	// rightly.
	opts := []config.Option{config.WithStandardBodyDecoders()}
	if strict {
		opts = append(opts,
			config.WithStrictMode(),
			config.WithStrictRejectReadOnly(),
			config.WithStrictRejectWriteOnly(),
		)
	}
	v, errs := validator.NewValidator(doc, opts...)
	if len(errs) > 0 {
		return nil, fmt.Errorf("openapi.spec: %s: %w", path, errors.Join(errs...))
	}
	m, err := doc.BuildV3Model()
	if err != nil {
		return nil, fmt.Errorf("openapi.spec: %s: %w", path, err)
	}
	return &contract{v: v, strict: strict, spec: path, model: &m.Model}, nil
}

// check returns what in the request and the response the document does not
// allow, as the violations a step reports, or an empty list when it allows
// all of it. req is the request the step sent, with reqBody, the body sent;
// res is the response, with resBody, its body, which has been read.
//
// It also returns what the response was matched to, which a report of the
// coverage of the document counts: the document, the operation, such as
// GET /users/{id}, and the response the operation declares for it, such as
// 200, 2XX or default, which is left out when it declares none. It is nil
// when the document has no operation for the request.
//
// The request is checked as the step sent it, before any redirect, which
// the client made rather than the step. The response is matched to an
// operation by the request that received it, the last one when the request
// was redirected.
func (c *contract) check(req *hp.Request, reqBody []byte, res *hp.Response, resBody []byte) ([]any, map[string]any) {
	out := []any{}
	if c.request {
		out = append(out, c.checkRequest(req, reqBody)...)
	}

	r := *res
	r.Body = io.NopCloser(bytes.NewReader(resBody))
	_, errs := c.v.ValidateHttpResponse(res.Request, &r)
	out = append(out, violations("response", strictKept(errs))...)

	op, template := c.operation(res.Request)
	if op == nil {
		return out, nil
	}
	matched := map[string]any{
		"spec":      c.spec,
		"operation": res.Request.Method + " " + template,
	}
	key, resp := responseOf(op, res.StatusCode)
	if resp != nil {
		matched["response"] = key
	}
	if resp != nil {
		if res.Request.Method != hp.MethodHead {
			out = append(out, emptyOrNull(resp.Content, res.Header.Get("Content-Type"), resBody)...)
		}
		if c.strict {
			out = append(out, undeclaredIn("response", mediaSchema(resp.Content, res.Header.Get("Content-Type")), resBody)...)
		}
	}
	return out, matched
}

// checkRequest returns what in the request the document does not allow. A
// request to an operation the document does not have is left to the check
// of the response, which reports it, so that it is not reported twice.
func (c *contract) checkRequest(req *hp.Request, body []byte) []any {
	r := req.Clone(req.Context())
	r.Body = io.NopCloser(bytes.NewReader(body))

	_, errs := c.v.ValidateHttpRequest(r)
	kept := errs[:0]
	for _, e := range strictKept(errs) {
		if e.ValidationType != helpers.PathValidation {
			kept = append(kept, e)
		}
	}
	out := violations("request", kept)

	if c.strict {
		if op, _ := c.operation(req); op != nil {
			out = append(out, undeclaredIn("request", requestSchema(op, req.Header.Get("Content-Type")), body)...)
		}
	}
	return out
}

// strictKept leaves out of errs what the validator's strict mode finds
// undeclared but a strict contract does not report: headers and cookies,
// since a document rarely declares those that proxies, servers and clients
// add, such as Server, a trace header or a load balancer's cookie; and the
// properties of a body, which undeclared finds instead, keeping to what the
// schema writes in additionalProperties.
func strictKept(errs []*verrors.ValidationError) []*verrors.ValidationError {
	kept := make([]*verrors.ValidationError, 0, len(errs))
	for _, e := range errs {
		if e.ValidationType == verrors.StrictValidationType {
			switch e.ValidationSubType {
			case verrors.StrictSubTypeHeader, verrors.StrictSubTypeCookie, verrors.StrictSubTypeProperty:
				continue
			}
		}
		kept = append(kept, e)
	}
	return kept
}

// requestSchema returns the schema of the request body op takes as
// contentType, or nil when it declares none.
func requestSchema(op *v3.Operation, contentType string) *base.Schema {
	if op.RequestBody == nil {
		return nil
	}
	return mediaSchema(op.RequestBody.Content, contentType)
}

// undeclaredIn returns a violation for each property of body, a JSON body
// found in the request or the response as in says, that schema does not
// declare. A body that is not JSON is checked by the validator alone.
func undeclaredIn(in string, schema *base.Schema, body []byte) []any {
	if schema == nil {
		return nil
	}
	var data any
	if err := json.Unmarshal(body, &data); err != nil {
		return nil
	}
	var out []any
	for _, p := range undeclared(schema, data) {
		out = append(out, undeclaredViolation(in, p))
	}
	return out
}

// emptyOrNull returns a violation when body, a JSON body that content
// declares a schema for, is empty or null and the schema does not allow
// null. The validator checks neither: it takes both for a body with nothing
// to check.
func emptyOrNull(content *orderedmap.Map[string, *v3.MediaType], contentType string, body []byte) []any {
	schema := mediaSchema(content, contentType)
	if schema == nil {
		return nil
	}
	switch strings.TrimSpace(string(body)) {
	case "":
		mediaType, _, _ := mime.ParseMediaType(contentType)
		return []any{violation("response", "response body is empty", "the response declares a schema for "+mediaType, "")}
	case "null":
		if !allowsNull(schema, 0) {
			return []any{violation("response", "response body is null", "the schema does not allow null", "")}
		}
	}
	return nil
}

// allowsNull reports whether schema allows null: by nullable: true, as
// OpenAPI 3.0 writes it, by the type null, as 3.1 does, or by an enum that
// holds null. A schema that names no type allows null when the schemas it is
// composed of do: all of allOf, and any of oneOf or anyOf.
func allowsNull(s *base.Schema, depth int) bool {
	if s == nil || depth > maxNullDepth {
		return true
	}
	if len(s.Enum) > 0 {
		for _, n := range s.Enum {
			if n != nil && n.Tag == "!!null" {
				return true
			}
		}
		return false
	}
	if (s.Nullable != nil && *s.Nullable) || slices.Contains(s.Type, "null") {
		return true
	}
	if len(s.Type) > 0 {
		return false
	}
	for _, p := range s.AllOf {
		if !allowsNull(schemaOf(p), depth+1) {
			return false
		}
	}
	if len(s.OneOf)+len(s.AnyOf) == 0 {
		return true
	}
	for _, p := range append(slices.Clone(s.OneOf), s.AnyOf...) {
		if allowsNull(schemaOf(p), depth+1) {
			return true
		}
	}
	return false
}

// maxNullDepth bounds how deep allowsNull follows allOf, oneOf and anyOf.
const maxNullDepth = 64

// operation returns the operation the document has for the method and path
// of req, with the path as the document writes it, such as /users/{id}, or
// nil when it has none.
func (c *contract) operation(req *hp.Request) (*v3.Operation, string) {
	item, _, template := paths.FindPath(req, c.model, nil)
	if item == nil {
		return nil, ""
	}
	var op *v3.Operation
	switch req.Method {
	case hp.MethodGet:
		op = item.Get
	case hp.MethodPut:
		op = item.Put
	case hp.MethodPost:
		op = item.Post
	case hp.MethodDelete:
		op = item.Delete
	case hp.MethodOptions:
		op = item.Options
	case hp.MethodHead:
		op = item.Head
	case hp.MethodPatch:
		op = item.Patch
	case hp.MethodTrace:
		op = item.Trace
	}
	if op == nil {
		return nil, ""
	}
	return op, template
}

// responseOf returns the response op declares for the status code, with the
// key it is declared under: the code, a range such as 2XX, or default. It
// returns nil when op declares none.
func responseOf(op *v3.Operation, code int) (string, *v3.Response) {
	if op.Responses == nil {
		return "", nil
	}
	if op.Responses.Codes != nil {
		for _, key := range []string{strconv.Itoa(code), fmt.Sprintf("%dXX", code/100), fmt.Sprintf("%dxx", code/100)} {
			if resp := op.Responses.Codes.GetOrZero(key); resp != nil {
				return key, resp
			}
		}
	}
	if op.Responses.Default != nil {
		return "default", op.Responses.Default
	}
	return "", nil
}

// mediaSchema returns the schema content declares for contentType, when it
// is JSON, or nil.
func mediaSchema(content *orderedmap.Map[string, *v3.MediaType], contentType string) *base.Schema {
	if content == nil || !binary.IsJSONMediaType(contentType) {
		return nil
	}
	want, _, _ := mime.ParseMediaType(contentType)
	for name, mt := range content.FromOldest() {
		got, _, err := mime.ParseMediaType(name)
		if err == nil && strings.EqualFold(got, want) && mt != nil {
			return schemaOf(mt.Schema)
		}
	}
	return nil
}

// schemaOf returns the schema p stands for, or nil when there is none or it
// cannot be built.
func schemaOf(p *base.SchemaProxy) *base.Schema {
	if p == nil {
		return nil
	}
	return p.Schema()
}

// violations turns the errors of the validator into the violations a step
// reports, found in the request or the response as in says: one for each
// failure of a schema, which names the field, and one for each other error.
func violations(in string, errs []*verrors.ValidationError) []any {
	out := []any{}
	for _, e := range errs {
		if len(e.SchemaValidationErrors) == 0 {
			out = append(out, violation(in, e.Message, e.Reason, ""))
			continue
		}
		for _, se := range e.SchemaValidationErrors {
			out = append(out, violation(in, e.Message, se.Reason, se.FieldPath))
		}
	}
	return out
}

// violation is one violation, found in the request or the response.
func violation(in, message, reason, field string) map[string]any {
	v := map[string]any{
		"in":      in,
		"message": message,
	}
	if reason != "" {
		v["reason"] = reason
	}
	if field != "" {
		v["field"] = field
	}
	return v
}
