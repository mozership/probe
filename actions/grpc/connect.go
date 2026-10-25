package grpc

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/mozership/probe/internal/contractcheck"
	"google.golang.org/grpc/codes"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
)

// The headers of the Connect protocol a unary call uses.
const (
	connectProtocolVersion = "1"
	connectTrailerPrefix   = "trailer-"
)

// connectCodes are the codes of the Connect protocol, as an error names
// them, by the gRPC code each stands for.
var connectCodes = map[string]codes.Code{
	"canceled":            codes.Canceled,
	"unknown":             codes.Unknown,
	"invalid_argument":    codes.InvalidArgument,
	"deadline_exceeded":   codes.DeadlineExceeded,
	"not_found":           codes.NotFound,
	"already_exists":      codes.AlreadyExists,
	"permission_denied":   codes.PermissionDenied,
	"resource_exhausted":  codes.ResourceExhausted,
	"failed_precondition": codes.FailedPrecondition,
	"aborted":             codes.Aborted,
	"out_of_range":        codes.OutOfRange,
	"unimplemented":       codes.Unimplemented,
	"internal":            codes.Internal,
	"unavailable":         codes.Unavailable,
	"data_loss":           codes.DataLoss,
	"unauthenticated":     codes.Unauthenticated,
}

// httpToCode returns the code of an error response that does not name one,
// from its HTTP status, as the Connect protocol says.
func httpToCode(status int) codes.Code {
	switch status {
	case http.StatusBadRequest:
		return codes.Internal
	case http.StatusUnauthorized:
		return codes.Unauthenticated
	case http.StatusForbidden:
		return codes.PermissionDenied
	case http.StatusNotFound:
		return codes.Unimplemented
	case http.StatusTooManyRequests, http.StatusBadGateway, http.StatusServiceUnavailable, http.StatusGatewayTimeout:
		return codes.Unavailable
	default:
		return codes.Unknown
	}
}

// connectBase returns the URL the calls go under: addr when it is a URL, or
// addr as a host and port, over https when tls is set and http otherwise.
func (r *Req) connectBase() (string, error) {
	addr := r.Addr
	if !strings.Contains(addr, "://") {
		// A host and port is checked as the URL it stands for, so that one
		// with a query or a fragment cannot take in the procedure's path.
		scheme := "http"
		if r.TLS {
			scheme = "https"
		}
		addr = scheme + "://" + addr
	}
	u, err := url.Parse(addr)
	if err != nil {
		return "", fmt.Errorf("invalid addr %q: %w", r.Addr, err)
	}
	switch u.Scheme {
	case "https":
	case "http":
		if r.TLS {
			return "", fmt.Errorf("addr %s is http, but tls is true", r.Addr)
		}
	default:
		return "", fmt.Errorf("addr %s must be an http or https URL for protocol connect", r.Addr)
	}
	if u.Host == "" {
		return "", fmt.Errorf("addr %s names no host", r.Addr)
	}
	if u.RawQuery != "" || u.Fragment != "" || u.ForceQuery {
		return "", fmt.Errorf("addr %s must not have a query or a fragment", r.Addr)
	}
	return strings.TrimSuffix(u.String(), "/"), nil
}

// connectClient returns the HTTP client a Connect call is made with. It
// follows no redirect, which the protocol has no use for, so that one comes
// back as the answer it is.
func (r *Req) connectClient(base string) (*http.Client, error) {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	if strings.HasPrefix(base, "https://") {
		tlsConfig, err := r.tlsConfig()
		if err != nil {
			return nil, err
		}
		transport.TLSClientConfig = tlsConfig
	}
	return &http.Client{
		Transport: transport,
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}, nil
}

// doConnect makes the call with the Connect protocol: a POST of the request
// message, answered by the response message or an error.
func (r *Req) doConnect(ctx context.Context, timeout time.Duration) (*Result, error) {
	codec := r.Codec
	if codec == "" {
		codec = "json"
	}
	if codec != "json" && codec != "proto" {
		return nil, fmt.Errorf("unknown codec %q: use json or proto", r.Codec)
	}
	base, err := r.connectBase()
	if err != nil {
		return nil, err
	}
	if err := r.checkConnectHost(base); err != nil {
		return nil, err
	}
	client, err := r.connectClient(base)
	if err != nil {
		return nil, err
	}
	// The client makes this one call, so the connection it keeps for the
	// next is closed rather than left open until it times out.
	defer client.CloseIdleConnections()

	if r.cb != nil && r.cb.before != nil {
		r.cb.before(ctx, r.Service, r.Method)
	}

	result := &Result{Req: *r}
	start := time.Now()
	res, err := r.invokeConnect(ctx, client, base, codec, timeout)
	result.RT = time.Since(start)
	if err != nil {
		result.Status = 1
		return result, err
	}

	result.Res = *res
	if res.StatusCode != "OK" {
		result.Status = 1
	}
	if r.cb != nil && r.cb.after != nil {
		r.cb.after(res)
	}
	return result, nil
}

func (r *Req) invokeConnect(ctx context.Context, client *http.Client, base, codec string, timeout time.Duration) (*Res, error) {
	// The call is made with the method the .proto files declare, when they
	// are given; without them, a JSON body is sent as it is written, under
	// the service's full name, which the step then has to give.
	var spec protoreflect.MethodDescriptor
	if r.contract != nil {
		spec = r.contract.method(r.Service, r.Method)
	}
	if err := r.checkReadOnly(nil, spec); err != nil {
		return nil, err
	}
	// Only the files tell whether the method streams, which a call without
	// them is taken not to.
	var clientStream, serverStream bool
	bodies := []string{r.Body}
	if spec != nil {
		var err error
		if clientStream, serverStream, err = streams(spec); err != nil {
			return nil, err
		}
		if bodies, err = requestBodies(r.Body, clientStream); err != nil {
			return nil, err
		}
	} else if strings.HasPrefix(strings.TrimSpace(r.Body), "[") {
		return nil, errors.New("body is a list, which only a method that streams its requests takes, and the .proto files of proto have to tell that it does")
	}
	if r.MaxMessages > 0 && !serverStream {
		return nil, errors.New("max_messages is for a method that streams its responses, which the .proto files of proto have to tell")
	}

	var violations []any
	if r.contract != nil {
		violations = []any{}
		if spec == nil {
			violations = append(violations, contractcheck.Violation("response", fmt.Sprintf("the .proto files declare no method %s in %s", r.Method, r.Service), "", ""))
		} else {
			violations = append(violations, r.contract.checkRequests(spec, bodies, clientStream)...)
		}
	}
	if codec == "proto" && spec == nil {
		if r.contract == nil {
			return nil, errors.New("codec proto needs proto.files, which define the messages to encode")
		}
		return nil, fmt.Errorf("codec proto needs the .proto files to declare %s in %s", r.Method, r.Service)
	}

	service := r.Service
	var body []byte
	if spec != nil {
		service = string(spec.Parent().FullName())
		msgs, err := buildRequests(spec.Input(), bodies)
		if err != nil {
			// A body the .proto files refuse fails the step as a request
			// that breaks its contract, and is not sent.
			if hasRequestViolation(violations) {
				return &Res{Metadata: map[string]string{}, violations: violations}, nil
			}
			return nil, err
		}
		if clientStream || serverStream {
			return r.invokeConnectStream(ctx, client, base+"/"+service+"/"+r.Method, codec, timeout, spec, msgs, violations, serverStream)
		}
		if codec == "proto" {
			body, err = proto.Marshal(msgs[0])
		} else {
			body, err = protojson.Marshal(msgs[0])
		}
		if err != nil {
			return nil, fmt.Errorf("failed to encode the request: %w", err)
		}
	} else {
		body = []byte(r.Body)
		if len(bytes.TrimSpace(body)) == 0 {
			body = []byte("{}")
		}
		if !json.Valid(body) {
			return nil, errors.New("failed to unmarshal request JSON: body is not JSON")
		}
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, base+"/"+service+"/"+r.Method, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("failed to make the request: %w", err)
	}
	for k, v := range r.Metadata {
		req.Header.Set(k, v)
	}
	req.Header.Set("Content-Type", "application/"+codec)
	req.Header.Set("Connect-Protocol-Version", connectProtocolVersion)
	if ms, ok := connectTimeout(timeout); ok {
		req.Header.Set("Connect-Timeout-Ms", ms)
	}

	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read the response: %w", err)
	}

	res := &Res{
		StatusCode: "OK",
		Metadata:   connectMetadata(resp.Header),
		violations: violations,
	}
	if spec != nil {
		res.matched = r.contract.matched(spec)
	}

	// Any answer of the server is a result, an error one too, as a status a
	// gRPC call ends with is.
	if code, message, ok := connectError(resp, codec, data); ok {
		res.StatusCode = statusCodeName(code)
		res.StatusMessage = message
		return res, nil
	}

	if spec == nil {
		// Without the files the body is not read, but a reply that is not
		// JSON at all cannot be the message, as one the files cannot read
		// is not.
		if !json.Valid(data) {
			return nil, errors.New("failed to read the response: body is not JSON")
		}
		res.Body = string(data)
		return res, nil
	}
	if err := r.readResponses(res, spec.Output(), spec, [][]byte{data}, codec, false, true); err != nil {
		return nil, err
	}
	return res, nil
}

// connectTimeout returns timeout as the Connect-Timeout-Ms header writes
// it: in whole milliseconds, at least 1 for a timeout shorter than one, and
// in ten digits at most, beyond which no header is sent and the deadline is
// kept on this side alone.
func connectTimeout(timeout time.Duration) (string, bool) {
	if timeout <= 0 {
		return "", false
	}
	ms := max(timeout.Milliseconds(), 1)
	v := strconv.FormatInt(ms, 10)
	if len(v) > 10 {
		return "", false
	}
	return v, true
}

// connectError returns the code and the message of resp when it is an
// error, as connect-go reads one: a status other than 200 with the error as
// JSON, or with no such error the code its HTTP status implies, and a 200
// whose content type is not the codec the call was made with.
func connectError(resp *http.Response, codec string, data []byte) (codes.Code, string, bool) {
	mediaType, _, _ := mime.ParseMediaType(resp.Header.Get("Content-Type"))
	if resp.StatusCode != http.StatusOK {
		if mediaType == "application/json" {
			var wire struct {
				Code    string `json:"code"`
				Message string `json:"message"`
			}
			if err := json.Unmarshal(data, &wire); err == nil {
				code, ok := connectCodes[wire.Code]
				if !ok {
					code = httpToCode(resp.StatusCode)
				}
				return code, wire.Message, true
			}
		}
		return httpToCode(resp.StatusCode), resp.Status, true
	}
	if !strings.HasPrefix(mediaType, "application/") {
		return codes.Unknown, fmt.Sprintf("invalid content-type: %q; expecting %q", resp.Header.Get("Content-Type"), "application/"+codec), true
	}
	if mediaType != "application/"+codec {
		return codes.Internal, fmt.Sprintf("invalid content-type: %q; expecting %q", resp.Header.Get("Content-Type"), "application/"+codec), true
	}
	return codes.OK, "", false
}

// connectMetadata returns the headers of a response as metadata, by their
// names in lower case: those a Connect server sends after the message have
// the prefix Trailer-, which is taken off, and win over a header of the same
// name, as a gRPC trailer does.
func connectMetadata(h http.Header) map[string]string {
	out := map[string]string{}
	for k, v := range h {
		k = strings.ToLower(k)
		if len(v) > 0 && !strings.HasPrefix(k, connectTrailerPrefix) {
			out[k] = v[0]
		}
	}
	for k, v := range h {
		k = strings.ToLower(k)
		if len(v) > 0 && strings.HasPrefix(k, connectTrailerPrefix) {
			out[strings.TrimPrefix(k, connectTrailerPrefix)] = v[0]
		}
	}
	return out
}
