package http

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"maps"
	hp "net/http"
	"net/url"
	"path"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/mozership/probe/actionrpc"
	"github.com/mozership/probe/internal/body"
	"github.com/mozership/probe/internal/contractcheck"
	"github.com/mozership/probe/internal/duration"
	"github.com/mozership/probe/internal/hostport"
	"github.com/mozership/probe/internal/mapping"
)

// DefaultTimeout bounds a request when the step does not set `timeout`. The
// zero value of http.Client means "wait forever", which lets a single
// unresponsive endpoint hold a workflow open until the step timeout fires.
const DefaultTimeout = 30 * time.Second

type Req struct {
	URL     string            `map:"url" validate:"required"`
	Method  string            `map:"method" validate:"required"`
	Header  map[string]string `map:"headers"`
	Body    string            `map:"body"` // Changed from []byte to string for text data
	Timeout string            `map:"timeout"`
	cb      *Callback
	// payload is sent in place of Body when it is set. It holds a multipart
	// body, which may carry the bytes of files, and is never shown.
	payload []byte
	// jar sends and records cookies. Do uses one of its own when it is nil,
	// so that cookies set on a redirect are sent on to where it leads.
	jar *cookieJar
	// contract, when it is set, checks the response against an OpenAPI
	// document.
	contract *contract
	// guard is what the run allows: Do refuses a request, or a redirect,
	// it does not allow.
	guard actionrpc.Guard
}

type Res struct {
	Status   string            `map:"status"`
	Code     int               `map:"code"`
	Header   map[string]string `map:"headers"`
	Body     string            `map:"body"`     // Changed from []byte to string for text data
	FilePath string            `map:"filepath"` // New field for binary file paths
	// Cookies holds the names and values of the cookies the server set, on
	// redirects included.
	Cookies map[string]string `map:"cookies"`
}

type Result struct {
	Req    Req           `map:"req"`
	Res    Res           `map:"res"`
	RT     time.Duration `map:"rt"`
	Status int           `map:"status"`
	// violations are what the contract does not allow in the response; nil
	// when there is no contract.
	violations []any
	// matched is what the contract matched the response to; nil when there
	// is no contract or the document has no operation for the request.
	matched map[string]any
}

func NewReq() *Req {
	return &Req{
		Method: "GET",
		Header: map[string]string{
			"Accept":     "*/*",
			"User-Agent": "probe-http/1.0.0",
		},
		Timeout: DefaultTimeout.String(),
	}
}

// parseTimeout turns the request's timeout field into a duration. It reads
// it as mapping.ParseTimeout does, as a duration string ("10s", "1m30s") or a
// number of seconds, except that an empty value falls back to DefaultTimeout
// and a zero, which ParseTimeout refuses, disables the limit.
func parseTimeout(s string) (time.Duration, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return DefaultTimeout, nil
	}
	if d, err := duration.Parse(s); err == nil && d == 0 {
		return 0, nil
	}
	return mapping.ParseTimeout(s)
}

// joinHeader returns h with the values of each header joined, as the result
// shows them, such as Accept: text/html, application/xml;q=0.9.
func joinHeader(h hp.Header) map[string]string {
	out := make(map[string]string, len(h))
	for k, v := range h {
		out[k] = strings.Join(v, ", ")
	}
	return out
}

// hasHeader reports whether h holds the header name, in any case.
func hasHeader(h map[string]string, name string) bool {
	for k := range h {
		if strings.EqualFold(k, name) {
			return true
		}
	}
	return false
}

// setHeader sets the header name of h to value, in place of any it holds in
// another case, and returns h, made when it is nil.
func setHeader(h map[string]string, name, value string) map[string]string {
	if h == nil {
		h = make(map[string]string)
	}
	for k := range h {
		if strings.EqualFold(k, name) {
			delete(h, k)
		}
	}
	h[name] = value
	return h
}

// mergeHeaders merges custom headers with default headers, handling case-insensitive duplicates
// Custom headers override defaults when header names match (case-insensitive)
func mergeHeaders(defaultHeaders, customHeaders map[string]string) map[string]string {
	if customHeaders == nil {
		return defaultHeaders
	}

	result := make(map[string]string)

	// First, copy all default headers
	maps.Copy(result, defaultHeaders)

	// Then, add/override with custom headers, removing case-insensitive duplicates
	for customKey, customValue := range customHeaders {
		// Check if this custom header should override a default header
		var keyToRemove string
		for existingKey := range result {
			if strings.EqualFold(existingKey, customKey) {
				keyToRemove = existingKey
				break
			}
		}

		// Remove the existing header if found
		if keyToRemove != "" {
			delete(result, keyToRemove)
		}

		// Add the custom header
		result[customKey] = customValue
	}

	return result
}

func (r *Req) Do() (*Result, error) {
	if r.URL == "" {
		return nil, errors.New("Req.URL is required")
	}

	timeout, err := parseTimeout(r.Timeout)
	if err != nil {
		return nil, err
	}

	var reqBody io.Reader = strings.NewReader(r.Body)
	if r.payload != nil {
		reqBody = bytes.NewReader(r.payload)
	}
	req, err := hp.NewRequest(r.Method, r.URL, reqBody)
	if err != nil {
		return nil, err
	}
	if err := r.checkGuard(req); err != nil {
		return nil, err
	}

	for k, v := range r.Header {
		// Clean header value by removing newlines and other invalid characters
		cleanValue := strings.ReplaceAll(strings.ReplaceAll(v, "\n", ""), "\r", "")
		req.Header.Set(mapping.TitleCase(k, "-"), cleanValue)
	}

	// callback
	if r.cb != nil && r.cb.before != nil {
		r.cb.before(req)
	}

	result := &Result{Req: *r}

	jar := r.jar
	if jar == nil {
		if jar, err = newCookieJar(r.URL, nil, nil); err != nil {
			return nil, err
		}
	}
	// The contract checks the request as it is sent, with the cookies the
	// client adds from the jar, which change once the response sets some.
	var sent *hp.Request
	if r.contract != nil {
		sent = req.Clone(req.Context())
		for _, c := range jar.Cookies(req.URL) {
			sent.AddCookie(c)
		}
	}

	cl := &hp.Client{Timeout: timeout, Jar: jar}
	if r.guard.Active() {
		// A redirect is a request of its own, which the guard allows or
		// refuses as it does the first.
		cl.CheckRedirect = func(next *hp.Request, via []*hp.Request) error {
			if len(via) >= maxRedirects {
				return fmt.Errorf("stopped after %d redirects", maxRedirects)
			}
			return r.checkGuard(next)
		}
	}
	start := time.Now()
	res, err := cl.Do(req)
	result.RT = time.Since(start)
	if err != nil {
		return result, err
	}
	defer func() { _ = res.Body.Close() }()

	// callback
	if r.cb != nil && r.cb.after != nil {
		r.cb.after(res)
	}

	result.Res = Res{
		Status:  res.Status,
		Code:    res.StatusCode,
		Cookies: jar.Received(),
	}

	// Determine status based on HTTP status code (200-299 = success, others = failure)
	status := 1 // default to failure
	if res.StatusCode >= 200 && res.StatusCode < 300 {
		status = 0 // success
	}
	result.Status = status

	resBody, err := io.ReadAll(res.Body)
	if err != nil {
		return result, err
	}

	// A textual body is kept as text, and any other is written to a file.
	contentType := res.Header.Get("Content-Type")
	bodyString, filePath, err := body.TextOrFile(resBody, contentType)
	if err != nil {
		return result, err
	}
	result.Res.Body = bodyString
	result.Res.FilePath = filePath

	result.Res.Header = joinHeader(res.Header)

	if r.contract != nil {
		sentBody := []byte(r.Body)
		if r.payload != nil {
			sentBody = r.payload
		}
		result.violations, result.matched = r.contract.check(sent, sentBody, res, resBody)
	}

	return result, nil
}

// maxRedirects is how many redirects a request follows, as many as
// net/http follows when it is not told otherwise.
const maxRedirects = 10

// readMethods are the methods allowed under a read-only guard.
var readMethods = []string{hp.MethodGet, hp.MethodHead, hp.MethodOptions}

// checkGuard returns a Refused error when the guard of the run does not
// allow req: a method that may write under --read-only, or a host it does
// not allow. A host without a port is taken at the port of its scheme.
func (r *Req) checkGuard(req *hp.Request) error {
	if r.guard.ReadOnly && !slices.Contains(readMethods, req.Method) {
		return actionrpc.Refuse("%s %s may write, and the run is read-only; only GET, HEAD and OPTIONS are sent", req.Method, req.URL.Redacted())
	}
	return r.guard.CheckHost(hostport.OfURL(req.URL))
}

type Option func(*Callback)

type Callback struct {
	before func(req *hp.Request)
	after  func(res *hp.Response)
}

var httpMethods = []string{
	hp.MethodGet,
	hp.MethodHead,
	hp.MethodPost,
	hp.MethodPut,
	hp.MethodPatch,
	hp.MethodDelete,
	hp.MethodConnect,
	hp.MethodOptions,
	hp.MethodTrace,
}

// ResolveMethodAndURL resolves HTTP method fields and updates the data map
// Converts method fields like "get", "post" to "method" and "url" fields
func ResolveMethodAndURL(data map[string]any) error {
	for _, method := range httpMethods {
		lowerMethod := strings.ToLower(method)
		routeValue, ok := data[lowerMethod]
		if !ok {
			continue
		}

		route, ok := routeValue.(string)
		if !ok {
			return fmt.Errorf("method field %s must be a string", lowerMethod)
		}

		data["method"] = method
		delete(data, lowerMethod)

		// If route is a complete URL (starts with http:// or https://), use it directly
		if strings.HasPrefix(route, "http://") || strings.HasPrefix(route, "https://") {
			data["url"] = route
		} else {
			// If route is a relative path, combine with base URL
			baseURLValue, ok := data["url"]
			if !ok {
				return errors.New("url is missing for relative path")
			}

			baseURL, ok := baseURLValue.(string)
			if !ok {
				return fmt.Errorf("base URL must be a string")
			}

			// renew url as full-url
			u, err := url.Parse(baseURL)
			if err != nil {
				return err
			}
			routeURL, err := url.Parse(route)
			if err != nil {
				return fmt.Errorf("invalid route: %w", err)
			}
			u.Path = path.Join(u.Path, routeURL.Path)
			if routeURL.RawQuery != "" {
				u.RawQuery = routeURL.RawQuery
			}
			data["url"] = u.String()
		}

		break
	}

	return nil
}

// hasJSONContentType checks if headers declare a JSON Content-Type, such as
// application/json; charset=utf-8 or application/problem+json.
// Supports multiple header map types and case-insensitive matching
func hasJSONContentType(headers any) bool {
	checkHeader := func(k string, v any) bool {
		s, ok := v.(string)
		return ok && strings.EqualFold(k, "content-type") && body.IsJSON(s)
	}

	switch h := headers.(type) {
	case map[string]any: // also matches map[string]interface{}
		for k, v := range h {
			if checkHeader(k, v) {
				return true
			}
		}
	case map[string]string:
		for k, v := range h {
			if checkHeader(k, v) {
				return true
			}
		}
	}
	return false
}

// MarshalBodyIfJSON converts body to JSON string when Content-Type is application/json
// Supports map[string]any and []any body types
func MarshalBodyIfJSON(data, m map[string]any) {
	bodyData, bodyExists := data["body"]
	if !bodyExists {
		return
	}

	if !hasJSONContentType(data["headers"]) {
		return
	}

	switch body := bodyData.(type) {
	case map[string]any:
		if jsonBytes, err := json.Marshal(body); err == nil {
			m["body"] = string(jsonBytes)
		}
	case []any:
		if jsonBytes, err := json.Marshal(body); err == nil {
			m["body"] = string(jsonBytes)
		}
	}
}

func Request(data map[string]any, opts ...Option) (map[string]any, error) {
	ret, _, err := RequestStep(actionrpc.Call{With: data}, opts...)
	return ret, err
}

// RequestStep is Request for a step, with the state the action keeps in a
// job. When keep_cookies is true, the cookies kept in the state are sent, and
// the state returned keeps those the server set as well; otherwise the state
// returned is nil, which leaves the job's state as it was. When trace_header
// is given, the request carries a header that names the run, job and step.
func RequestStep(call actionrpc.Call, opts ...Option) (map[string]any, map[string]any, error) {
	ret, newState, err := request(call, opts...)
	if err != nil {
		return map[string]any{}, nil, err
	}
	return ret, newState, nil
}

func request(call actionrpc.Call, opts ...Option) (map[string]any, map[string]any, error) {
	state := call.State
	// Create a copy to avoid modifying the original data
	m := make(map[string]any)
	maps.Copy(m, call.With)

	// Resolve HTTP method fields (get, post, etc.) to method and url
	if err := ResolveMethodAndURL(m); err != nil {
		return nil, nil, err
	}

	explicitCookies, keepCookies, err := takeCookies(m)
	if err != nil {
		return nil, nil, err
	}

	traceHeader, err := takeTrace(m)
	if err != nil {
		return nil, nil, err
	}

	contract, err := takeOpenAPI(m)
	if err != nil {
		return nil, nil, err
	}

	// form and multipart are turned into the body they stand for.
	payload, multipartSpec, contentType, err := takeFormBody(m)
	if err != nil {
		return nil, nil, err
	}

	// Handle body conversion for JSON content-type
	MarshalBodyIfJSON(call.With, m)

	// Extract custom headers and merge with defaults before MapToStructByTags
	var customHeaders map[string]string
	if headersInterface, exists := m["headers"]; exists {
		if headers, ok := headersInterface.(map[string]string); ok {
			// A copy, since basic_auth, form and multipart change it.
			customHeaders = maps.Clone(headers)
		} else if headersInterfaceMap, ok := headersInterface.(map[string]any); ok {
			// A number such as Content-Length: 1024 is sent as its text.
			customHeaders = mapping.StringMap(headersInterfaceMap)
		}
	}

	// basic_auth is turned into the Authorization header it stands for.
	if auth, exists := m["basic_auth"]; exists {
		delete(m, "basic_auth")
		value, err := basicAuthHeader(auth)
		if err != nil {
			return nil, nil, err
		}
		if hasHeader(customHeaders, "authorization") {
			return nil, nil, errors.New("basic_auth and an authorization header cannot be given together")
		}
		customHeaders = setHeader(customHeaders, "authorization", value)
	}

	// The body built from form or multipart is sent with its own Content-Type,
	// which replaces one set for the job's other requests, such as JSON in
	// defaults; a multipart one also carries the boundary only it knows.
	if contentType != "" {
		customHeaders = setHeader(customHeaders, "content-type", contentType)
	}

	if traceHeader != "" {
		if hasHeader(customHeaders, traceHeader) {
			return nil, nil, fmt.Errorf("trace_header and a %s header cannot be given together", traceHeader)
		}
		customHeaders = setHeader(customHeaders, traceHeader, traceValue(call.Step))
	}

	// Create new request with merged headers
	r := NewReq()
	r.Header = mergeHeaders(r.Header, customHeaders)

	// Update the map with merged headers to avoid duplication in MapToStructByTags
	m["headers"] = r.Header

	cb := &Callback{}
	for _, opt := range opts {
		opt(cb)
	}
	r.cb = cb

	if err := mapping.MapToStructByTags(m, r); err != nil {
		return nil, nil, err
	}
	r.payload = payload
	r.contract = contract
	r.guard = call.Guard

	var stored []storedCookie
	if keepCookies {
		stored = cookiesFromState(state)
	}
	r.jar, err = newCookieJar(r.URL, explicitCookies, stored)
	if err != nil {
		return nil, nil, err
	}

	ret, err := r.Do()
	if err != nil {
		return nil, nil, err
	}

	mapRet, err := mapping.StructToMapByTags(ret)
	if err != nil {
		return nil, nil, err
	}

	// The violations, and what the response was matched to, are told to
	// the runner, which takes them as the step's test, and are in res, for
	// the step's own test to read.
	contractcheck.Report(call.Contract, mapRet, ret.violations, ret.matched)

	// A multipart request shows what was written in place of the body sent.
	if multipartSpec != nil {
		if req, ok := mapRet["req"].(map[string]any); ok {
			req["multipart"] = multipartSpec
		}
	}

	var newState map[string]any
	if keepCookies {
		newState = cookiesToState(r.jar.Stored(stored))
	}

	// Return the result directly without flattening
	return mapRet, newState, nil
}

// basicAuthHeader builds the value of an Authorization header for HTTP Basic
// authentication from basic_auth, a map of username and password, as RFC 7617
// defines it. A password may be a number, as a YAML value written without
// quotes is, and may be empty; a username may not hold a colon, which would
// end it early.
func basicAuthHeader(v any) (string, error) {
	auth, ok := v.(map[string]any)
	if !ok {
		return "", errors.New("basic_auth must be a map of username and password")
	}
	for k := range auth {
		if k != "username" && k != "password" {
			return "", fmt.Errorf("basic_auth takes username and password, not %s", k)
		}
	}
	username, err := basicAuthField(auth, "username")
	if err != nil {
		return "", err
	}
	if username == "" {
		return "", errors.New("basic_auth.username is required")
	}
	if strings.Contains(username, ":") {
		return "", errors.New("basic_auth.username must not contain a colon")
	}
	password, err := basicAuthField(auth, "password")
	if err != nil {
		return "", err
	}
	return "Basic " + base64.StdEncoding.EncodeToString([]byte(username+":"+password)), nil
}

// basicAuthField returns the field key of basic_auth as a string: a string
// as it is, a number or a boolean as written, and a missing one as empty.
func basicAuthField(auth map[string]any, key string) (string, error) {
	switch v := auth[key].(type) {
	case nil:
		return "", nil
	case string:
		return v, nil
	case float64:
		return strconv.FormatFloat(v, 'f', -1, 64), nil
	case int, int64, uint64, bool:
		return fmt.Sprint(v), nil
	default:
		return "", fmt.Errorf("basic_auth.%s must be a string", key)
	}
}

func WithBefore(f func(req *hp.Request)) Option {
	return func(c *Callback) {
		c.before = f
	}
}

func WithAfter(f func(res *hp.Response)) Option {
	return func(c *Callback) {
		c.after = f
	}
}
