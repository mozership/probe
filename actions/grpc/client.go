package grpc

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"maps"
	"os"
	"sync/atomic"
	"time"

	"github.com/mozership/probe/actionrpc"
	"github.com/mozership/probe/internal/contractcheck"
	"github.com/mozership/probe/mapping"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/reflection/grpc_reflection_v1alpha"
	"google.golang.org/grpc/stats"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protodesc"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/reflect/protoregistry"
	"google.golang.org/protobuf/types/descriptorpb"
	"google.golang.org/protobuf/types/dynamicpb"
)

type Req struct {
	// Protocol is how the call is made: grpc, the default, or connect, the
	// Connect protocol over HTTP, which reaches a server through an HTTP/1.1
	// proxy too.
	Protocol string            `map:"protocol"`
	Addr     string            `map:"addr" validate:"required"`
	Service  string            `map:"service" validate:"required"`
	Method   string            `map:"method" validate:"required"`
	Body     string            `map:"body"`
	Timeout  string            `map:"timeout"`
	TLS      bool              `map:"tls"`
	Insecure bool              `map:"insecure"`
	CertFile string            `map:"cert_file"`
	KeyFile  string            `map:"key_file"`
	CAFile   string            `map:"ca_file"`
	Metadata map[string]string `map:"metadata"`
	// Codec is how a Connect call encodes its messages: json, the default,
	// or proto. A gRPC call takes none.
	Codec string `map:"codec"`
	// MaxMessages, when it is above 0, is how many responses of a stream
	// are read before the call is left.
	MaxMessages int `map:"max_messages"`
	cb          *Callback
	// contract, when it is set, checks the call against .proto files.
	contract *contract
	// guard is what the run allows: a call to a host it does not allow, or
	// under --read-only one to a method that may write, is refused before
	// anything is sent.
	guard actionrpc.Guard
}

type Res struct {
	Body          string            `map:"body"`
	StatusCode    string            `map:"status_code"`
	StatusMessage string            `map:"status_message"`
	Metadata      map[string]string `map:"metadata"`
	// violations are what the .proto files do not allow in the call; nil
	// when there is no contract.
	violations []any
	// matched is what the call was matched to in the .proto files; nil
	// when there is no contract or they declare no such method.
	matched map[string]any
	// messages are the responses of a stream, each as an object; nil for a
	// call whose responses do not stream.
	messages []any
	// complete is whether the server ended the stream of responses, rather
	// than the call leaving it; nil for a call whose responses do not
	// stream.
	complete *bool
}

type Result struct {
	Req    Req           `map:"req"`
	Res    Res           `map:"res"`
	RT     time.Duration `map:"rt"`
	Status int           `map:"status"`
}

func NewReq() *Req {
	return &Req{
		Timeout:  "30s",
		TLS:      false,
		Insecure: false,
		Metadata: make(map[string]string),
	}
}

func (r *Req) Do() (re *Result, er error) {
	if r.Addr == "" {
		return nil, errors.New("Req.Addr is required")
	}
	if r.Service == "" {
		return nil, errors.New("Req.Service is required")
	}
	if r.Method == "" {
		return nil, errors.New("Req.Method is required")
	}

	// Setup timeout
	timeout, err := mapping.ParseTimeout(r.Timeout)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	switch r.Protocol {
	case "", "grpc":
		if r.Codec != "" {
			return nil, fmt.Errorf("codec is for protocol connect; a gRPC call is encoded as protobuf")
		}
	case "connect":
		return r.doConnect(ctx, timeout)
	default:
		return nil, fmt.Errorf("unknown protocol %q: use grpc or connect", r.Protocol)
	}

	// Setup connection credentials
	var creds credentials.TransportCredentials
	if !r.TLS {
		// Plain text connection
		creds = insecure.NewCredentials()
	} else {
		tlsConfig, err := r.tlsConfig()
		if err != nil {
			return nil, err
		}
		creds = credentials.NewTLS(tlsConfig)
	}

	if err := r.checkGRPCHost(); err != nil {
		return nil, err
	}

	// Establish connection
	answer := &answerHandler{}
	conn, err := grpc.NewClient(r.Addr, grpc.WithTransportCredentials(creds), grpc.WithStatsHandler(answer))
	if err != nil {
		return nil, fmt.Errorf("failed to connect to gRPC server: %w", err)
	}

	defer func() {
		err := conn.Close()
		if er == nil {
			er = err
		}
	}()

	// Prepare metadata
	md := metadata.New(r.Metadata)
	ctx = metadata.NewOutgoingContext(ctx, md)

	// Callback before request
	if r.cb != nil && r.cb.before != nil {
		r.cb.before(ctx, r.Service, r.Method)
	}

	result := &Result{Req: *r}
	start := time.Now()

	// Use reflection to get service descriptor
	reflectionClient := grpc_reflection_v1alpha.NewServerReflectionClient(conn)
	res, err := r.invokeMethod(ctx, conn, reflectionClient, answer)
	result.RT = time.Since(start)

	if err != nil {
		result.Status = 1 // failure
		return result, err
	}

	result.Res = *res
	result.Status = 0 // success
	if res.StatusCode != "OK" {
		result.Status = 1
	}

	// Callback after response
	if r.cb != nil && r.cb.after != nil {
		r.cb.after(res)
	}

	return result, nil
}

// tlsConfig returns the TLS configuration tls, insecure, ca_file, cert_file
// and key_file ask for. insecure skips verifying the server, which leaves
// ca_file nothing to do, but the client certificate is still presented.
func (r *Req) tlsConfig() (*tls.Config, error) {
	if (r.CertFile == "") != (r.KeyFile == "") {
		return nil, errors.New("cert_file and key_file must be given together")
	}
	tlsConfig := &tls.Config{}

	if r.Insecure {
		// TLS without certificate verification (for development)
		tlsConfig.InsecureSkipVerify = true
	} else if r.CAFile != "" {
		// Custom CA certificate
		caCert, err := os.ReadFile(r.CAFile)
		if err != nil {
			return nil, fmt.Errorf("failed to read CA file: %w", err)
		}
		caCertPool := x509.NewCertPool()
		if !caCertPool.AppendCertsFromPEM(caCert) {
			return nil, errors.New("failed to parse CA certificate")
		}
		tlsConfig.RootCAs = caCertPool
	}

	// Client certificate for mTLS
	if r.CertFile != "" {
		cert, err := tls.LoadX509KeyPair(r.CertFile, r.KeyFile)
		if err != nil {
			return nil, fmt.Errorf("failed to load client certificate: %w", err)
		}
		tlsConfig.Certificates = []tls.Certificate{cert}
	}
	return tlsConfig, nil
}

func (r *Req) invokeMethod(ctx context.Context, conn *grpc.ClientConn, reflectionClient grpc_reflection_v1alpha.ServerReflectionClient, answer *answerHandler) (*Res, error) {
	// The method the .proto files declare, when they are given.
	var spec protoreflect.MethodDescriptor
	if r.contract != nil {
		spec = r.contract.method(r.Service, r.Method)
	}

	// The server's own definition of the method, as its reflection tells
	// it, which the call is made with. A server without reflection tells
	// nothing, nor does one whose definition lacks the method; the call is
	// then made with the .proto files' definition, as a client built from
	// them would make it.
	var server protoreflect.MethodDescriptor
	// Only a server that tells no definition of the service is called with
	// the files'; a lookup that went wrong, such as one whose reply cannot
	// be read, stays an error, so that the server's definition is never
	// left unchecked without a word.
	symbol := r.Service
	if spec != nil {
		// Reflection finds a service by its full name, which the files know
		// when the step gives a short one.
		symbol = string(spec.Parent().FullName())
	}
	serviceDesc, err := r.getServiceDescriptor(ctx, reflectionClient, symbol)
	switch {
	case err == nil:
		server = serviceDesc.Methods().ByName(protoreflect.Name(r.Method))
		if server == nil && spec == nil {
			return nil, fmt.Errorf("method %s not found in service %s", r.Method, r.Service)
		}
	case spec == nil || (!errors.Is(err, errNoReflection) && !errors.Is(err, errNotListed)):
		return nil, fmt.Errorf("failed to get service descriptor: %w", err)
	}
	methodDesc := server
	if methodDesc == nil {
		methodDesc = spec
	}
	if err := r.checkReadOnly(server, spec); err != nil {
		return nil, err
	}

	// The .proto files, when they are given, are checked against the
	// request, the server's definition when it tells one, and then the
	// response.
	var definition []any
	if r.contract != nil {
		switch {
		case spec == nil:
			definition = append(definition, contractcheck.Violation("response", fmt.Sprintf("the .proto files declare no method %s in %s", r.Method, r.Service), "", ""))
		case server != nil:
			definition = append(definition, r.contract.checkDefinition(spec, server)...)
		case serviceDesc != nil:
			definition = append(definition, contractcheck.Violation("response", fmt.Sprintf("the server's definition of %s declares no method %s", serviceDesc.FullName(), r.Method), "the .proto files declare it", ""))
		}
	}

	// The call is made as the server's definition streams. A step written
	// for the files' definition may not fit it, which is then told as the
	// definitions differing, without anything sent.
	clientStream, serverStream, bodies, err := r.callShape(methodDesc)
	if err != nil {
		if len(definition) > 0 {
			return &Res{Metadata: map[string]string{}, violations: definition}, nil
		}
		return nil, err
	}
	var violations []any
	if r.contract != nil {
		violations = []any{}
		if spec != nil {
			violations = append(violations, r.contract.checkRequests(spec, bodies, clientStream)...)
		}
		violations = append(violations, definition...)
	}

	msgs, err := buildRequests(methodDesc.Input(), bodies)
	if err != nil {
		// A body the .proto files refuse as well is the workflow's mistake,
		// which fails the step as a request that breaks its contract rather
		// than as an action that could not run. The call cannot be made with
		// it, so nothing is sent, and no status comes back.
		if hasRequestViolation(violations) {
			return &Res{Metadata: map[string]string{}, violations: violations}, nil
		}
		return nil, err
	}
	if clientStream || serverStream {
		return r.invokeStream(ctx, conn, answer, methodDesc, spec, msgs, violations, clientStream, serverStream)
	}
	requestMsg := msgs[0]

	// Invoke the method. The reply is received as the bytes it came in, and
	// read apart from the call, so that one the definition the call is made
	// with cannot read is told as such rather than lost in the call.
	fullMethodName := fmt.Sprintf("/%s/%s", methodDesc.Parent().FullName(), methodDesc.Name())
	var header, trailer metadata.MD
	var raw []byte
	err = conn.Invoke(answer.mark(ctx), fullMethodName, requestMsg, &raw, grpc.ForceCodec(rawCodec{}), grpc.Header(&header), grpc.Trailer(&trailer))

	responseMsg := dynamicpb.NewMessage(methodDesc.Output())
	var readErr error
	if err == nil {
		readErr = proto.Unmarshal(raw, responseMsg)
	}

	// The server's metadata arrives as headers before the reply and trailers
	// after it; a trailer wins over a header of the same name.
	metadataMap := make(map[string]string)
	for _, md := range []metadata.MD{header, trailer} {
		for key, values := range md {
			if len(values) > 0 {
				metadataMap[key] = values[0] // Take first value
			}
		}
	}

	// Convert response to JSON
	responseJSON := ""
	if readErr == nil {
		responseBytes, jsonErr := protojson.Marshal(responseMsg)
		if jsonErr == nil {
			responseJSON = string(responseBytes)
		}
	}

	res := &Res{
		Body:          responseJSON,
		StatusCode:    "OK",
		StatusMessage: "",
		Metadata:      metadataMap,
		violations:    violations,
	}
	if spec != nil {
		res.matched = r.contract.matched(spec)
	}
	// Only a call that succeeded answers with a message to check.
	var responseViolations []any
	if err == nil && spec != nil {
		responseViolations = r.contract.checkResponse(spec, raw)
		res.violations = append(res.violations, responseViolations...)
	}
	// A reply the call's definition cannot read is an error, unless the
	// files' check has just told it as a reply that breaks them.
	if readErr != nil && len(responseViolations) == 0 {
		return nil, fmt.Errorf("failed to read the response: %w", readErr)
	}

	// A status the server sent is its answer, which a test can check like an
	// HTTP status code. Every error from a call is a status, but one made up
	// on this side, such as the timeout running out or the connection
	// failing, arrives without the server's trailers and stays an error.
	if err != nil {
		st, ok := status.FromError(err)
		if !ok || !answer.answered.Load() {
			return nil, err
		}
		res.StatusCode = statusCodeName(st.Code())
		res.StatusMessage = st.Message()
	}

	return res, nil
}

// rawCodec sends a message as protobuf does, and receives the reply as the
// bytes it came in, for them to be read apart from the call.
type rawCodec struct{}

// Name is the codec's name, as the content type says it: the reply is
// protobuf, only left unread.
func (rawCodec) Name() string { return "proto" }

func (rawCodec) Marshal(v any) ([]byte, error) {
	m, ok := v.(proto.Message)
	if !ok {
		return nil, fmt.Errorf("cannot send %T as a protobuf message", v)
	}
	return proto.Marshal(m)
}

func (rawCodec) Unmarshal(data []byte, v any) error {
	b, ok := v.(*[]byte)
	if !ok {
		return fmt.Errorf("cannot receive into %T", v)
	}
	*b = append((*b)[:0], data...)
	return nil
}

// answerHandler records whether the server sent the trailers that carry the
// status of the call marked with mark. It ignores the reflection lookup that
// goes over the same connection.
type answerHandler struct {
	answered atomic.Bool
}

type answerKey struct{}

// mark returns the context to make the call with.
func (h *answerHandler) mark(ctx context.Context) context.Context {
	return context.WithValue(ctx, answerKey{}, true)
}

func (h *answerHandler) TagRPC(ctx context.Context, _ *stats.RPCTagInfo) context.Context {
	return ctx
}

func (h *answerHandler) HandleRPC(ctx context.Context, s stats.RPCStats) {
	if _, ok := s.(*stats.InTrailer); ok && ctx.Value(answerKey{}) != nil {
		h.answered.Store(true)
	}
}

func (h *answerHandler) TagConn(ctx context.Context, _ *stats.ConnTagInfo) context.Context {
	return ctx
}

func (h *answerHandler) HandleConn(context.Context, stats.ConnStats) {}

// statusCodeName returns the canonical name of a gRPC status code, such as
// NOT_FOUND, which is how the gRPC specification and other tools spell it.
func statusCodeName(c codes.Code) string {
	switch c {
	case codes.OK:
		return "OK"
	case codes.Canceled:
		return "CANCELLED"
	case codes.Unknown:
		return "UNKNOWN"
	case codes.InvalidArgument:
		return "INVALID_ARGUMENT"
	case codes.DeadlineExceeded:
		return "DEADLINE_EXCEEDED"
	case codes.NotFound:
		return "NOT_FOUND"
	case codes.AlreadyExists:
		return "ALREADY_EXISTS"
	case codes.PermissionDenied:
		return "PERMISSION_DENIED"
	case codes.ResourceExhausted:
		return "RESOURCE_EXHAUSTED"
	case codes.FailedPrecondition:
		return "FAILED_PRECONDITION"
	case codes.Aborted:
		return "ABORTED"
	case codes.OutOfRange:
		return "OUT_OF_RANGE"
	case codes.Unimplemented:
		return "UNIMPLEMENTED"
	case codes.Internal:
		return "INTERNAL"
	case codes.Unavailable:
		return "UNAVAILABLE"
	case codes.DataLoss:
		return "DATA_LOSS"
	case codes.Unauthenticated:
		return "UNAUTHENTICATED"
	default:
		return c.String()
	}
}

// Errors of the reflection lookup that say the server does not tell its
// definition of the service, rather than that the lookup went wrong.
var (
	// errNoReflection is that the server serves no reflection.
	errNoReflection = errors.New("the server serves no reflection")
	// errNotListed is that the server's reflection does not list the
	// service.
	errNotListed = errors.New("the server's reflection does not list the service")
)

// getServiceDescriptor returns the server's definition of the service, as
// its reflection tells it, looked up by symbol, the service's name.
func (r *Req) getServiceDescriptor(ctx context.Context, client grpc_reflection_v1alpha.ServerReflectionClient, symbol string) (re protoreflect.ServiceDescriptor, er error) {
	stream, err := client.ServerReflectionInfo(ctx)
	if err != nil {
		return nil, reflectionError("failed to create reflection stream", err)
	}
	defer func() {
		err := stream.CloseSend()
		if er == nil {
			er = err
		}
	}()

	// Request service descriptor
	//nolint:staticcheck // v1alpha reflection API is still widely used
	err = stream.Send(&grpc_reflection_v1alpha.ServerReflectionRequest{
		MessageRequest: &grpc_reflection_v1alpha.ServerReflectionRequest_FileContainingSymbol{
			FileContainingSymbol: symbol,
		},
	})
	if err != nil {
		// A stream the server has ended fails a send with io.EOF, and tells
		// why, such as that it serves no reflection, to the receive.
		if errors.Is(err, io.EOF) {
			if _, recvErr := stream.Recv(); recvErr != nil && !errors.Is(recvErr, io.EOF) {
				err = recvErr
			}
		}
		return nil, reflectionError("failed to send reflection request", err)
	}

	// Receive response
	resp, err := stream.Recv()
	if err != nil {
		if err == io.EOF {
			return nil, errors.New("unexpected end of reflection stream")
		}
		return nil, reflectionError("failed to receive reflection response", err)
	}

	// Handle error response
	//nolint:staticcheck // v1alpha reflection API is still widely used
	if errResp := resp.GetErrorResponse(); errResp != nil {
		//nolint:staticcheck // v1alpha reflection API is still widely used
		if codes.Code(errResp.GetErrorCode()) == codes.NotFound {
			//nolint:staticcheck // v1alpha reflection API is still widely used
			return nil, fmt.Errorf("%w: %s", errNotListed, errResp.GetErrorMessage())
		}
		//nolint:staticcheck // v1alpha reflection API is still widely used
		return nil, fmt.Errorf("reflection error: %s", errResp.GetErrorMessage())
	}

	// Get file descriptor response
	//nolint:staticcheck // v1alpha reflection API is still widely used
	fileDescResp := resp.GetFileDescriptorResponse()
	if fileDescResp == nil {
		return nil, errors.New("unexpected response type from reflection")
	}

	// The reply holds the file of the service and the files it imports, as
	// the server has not sent them before. Each file is built once the
	// files it imports are, from the reply or, for the well-known types,
	// from those built into Probe.
	var pending []*descriptorpb.FileDescriptorProto
	//nolint:staticcheck // v1alpha reflection API is still widely used
	for _, fdBytes := range fileDescResp.GetFileDescriptorProto() {
		fd := &descriptorpb.FileDescriptorProto{}
		if err := proto.Unmarshal(fdBytes, fd); err != nil {
			return nil, fmt.Errorf("failed to unmarshal file descriptor: %w", err)
		}
		pending = append(pending, fd)
	}
	files, err := buildFiles(pending)
	if err != nil {
		return nil, err
	}

	var found protoreflect.ServiceDescriptor
	files.RangeFiles(func(f protoreflect.FileDescriptor) bool {
		services := f.Services()
		for i := 0; i < services.Len(); i++ {
			service := services.Get(i)
			// Support both short name and full name
			if string(service.Name()) == symbol || string(service.FullName()) == symbol {
				found = service
				return false
			}
		}
		return true
	})
	if found == nil {
		return nil, fmt.Errorf("%w: service %s not found", errNotListed, symbol)
	}
	return found, nil
}

// buildFiles builds the files of a reflection reply, each once the files
// it imports are: from the reply when it holds them, or else, for the
// well-known types, from those built into Probe.
func buildFiles(pending []*descriptorpb.FileDescriptorProto) (*protoregistry.Files, error) {
	files := new(protoregistry.Files)
	resolver := filesResolver{files: files, reply: map[string]bool{}}
	for _, fd := range pending {
		resolver.reply[fd.GetName()] = true
	}
	for len(pending) > 0 {
		var rest []*descriptorpb.FileDescriptorProto
		var lastErr error
		for _, fd := range pending {
			built, err := protodesc.NewFile(fd, resolver)
			if err != nil {
				rest = append(rest, fd)
				lastErr = err
				continue
			}
			if err := files.RegisterFile(built); err != nil {
				return nil, fmt.Errorf("failed to create file descriptor: %w", err)
			}
		}
		if len(rest) == len(pending) {
			return nil, fmt.Errorf("failed to create file descriptor: %w", lastErr)
		}
		pending = rest
	}
	return files, nil
}

// reflectionError wraps err with what failed, marking a status that says
// the server serves no reflection.
func reflectionError(what string, err error) error {
	if status.Code(err) == codes.Unimplemented {
		return fmt.Errorf("%s: %w: %w", what, errNoReflection, err)
	}
	return fmt.Errorf("%s: %w", what, err)
}

// filesResolver finds a file, or a descriptor, among the files built from a
// reflection reply, and then among those built into Probe, which hold the
// well-known types. A file the reply holds is found only once it is built
// from the reply, so that a file importing it is bound to the server's
// version rather than to one built into Probe by the same name.
type filesResolver struct {
	files *protoregistry.Files
	// reply are the paths of the files the reply holds.
	reply map[string]bool
}

func (r filesResolver) FindFileByPath(path string) (protoreflect.FileDescriptor, error) {
	if fd, err := r.files.FindFileByPath(path); err == nil {
		return fd, nil
	}
	if r.reply[path] {
		return nil, protoregistry.NotFound
	}
	return protoregistry.GlobalFiles.FindFileByPath(path)
}

func (r filesResolver) FindDescriptorByName(name protoreflect.FullName) (protoreflect.Descriptor, error) {
	if d, err := r.files.FindDescriptorByName(name); err == nil {
		return d, nil
	}
	d, err := protoregistry.GlobalFiles.FindDescriptorByName(name)
	if err == nil && r.reply[d.ParentFile().Path()] {
		return nil, protoregistry.NotFound
	}
	return d, err
}

type Option func(*Callback)

type Callback struct {
	before func(ctx context.Context, service, method string)
	after  func(res *Res)
}

func Request(data map[string]any, opts ...Option) (map[string]any, error) {
	return RequestStep(actionrpc.Call{With: data}, opts...)
}

// RequestStep is Request for a step, made under the guard of the run.
func RequestStep(call actionrpc.Call, opts ...Option) (map[string]any, error) {
	// Create a copy to avoid modifying the original data
	m := make(map[string]any)
	maps.Copy(m, call.With)

	// Handle body conversion for structured data: an object, or a list of
	// the messages a stream of requests sends.
	if bodyData, bodyExists := m["body"]; bodyExists {
		switch bodyData.(type) {
		case map[string]any, []any:
			if jsonBytes, err := json.Marshal(bodyData); err == nil {
				m["body"] = string(jsonBytes)
			}
		}
	}

	contract, err := takeProto(m)
	if err != nil {
		return map[string]any{}, err
	}

	m = mapping.HeaderToStringValue(m)

	// Create new request
	r := NewReq()
	r.contract = contract
	r.guard = call.Guard

	cb := &Callback{}
	for _, opt := range opts {
		opt(cb)
	}
	r.cb = cb

	if err := mapping.MapToStructByTags(m, r); err != nil {
		return map[string]any{}, err
	}

	ret, err := r.Do()
	if err != nil {
		return map[string]any{}, err
	}

	mapRet, err := mapping.StructToMapByTags(ret)
	if err != nil {
		return map[string]any{}, err
	}

	// The violations, and what the call was matched to, are told to the
	// runner, which takes them as the step's test, and are in res, for the
	// step's own test to read.
	contractcheck.Report(call.Contract, mapRet, ret.Res.violations, ret.Res.matched)
	if res, ok := mapRet["res"].(map[string]any); ok {
		if ret.Res.messages != nil {
			res["messages"] = ret.Res.messages
		}
		if ret.Res.complete != nil {
			res["complete"] = *ret.Res.complete
		}
	}

	return mapRet, nil
}

func WithBefore(f func(ctx context.Context, service, method string)) Option {
	return func(c *Callback) {
		c.before = f
	}
}

func WithAfter(f func(res *Res)) Option {
	return func(c *Callback) {
		c.after = f
	}
}
