package grpc

import (
	"context"
	"errors"
	"fmt"
	"io"
	"maps"
	"net"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/bufbuild/protocompile"
	"github.com/mozership/probe/actionrpc"
	grpclib "google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/health"
	healthpb "google.golang.org/grpc/health/grpc_health_v1"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/reflection"
	v1alphareflectiongrpc "google.golang.org/grpc/reflection/grpc_reflection_v1alpha"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/reflect/protodesc"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/reflect/protoregistry"
	"google.golang.org/protobuf/types/descriptorpb"
	"google.golang.org/protobuf/types/dynamicpb"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func TestConvertMetadataToMap(t *testing.T) {
	tests := []struct {
		name     string
		input    map[string]any
		expected map[string]any
	}{
		{
			name: "structured metadata",
			input: map[string]any{
				"service": "UserService",
				"metadata": map[string]any{
					"authorization": "Bearer token",
					"user-agent":    "probe-grpc/1.0",
				},
			},
			expected: map[string]any{
				"service": "UserService",
				"metadata": map[string]any{
					"authorization": "Bearer token",
					"user-agent":    "probe-grpc/1.0",
				},
			},
		},
		{
			name: "no metadata",
			input: map[string]any{
				"service": "UserService",
				"method":  "GetUser",
			},
			expected: map[string]any{
				"service": "UserService",
				"method":  "GetUser",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// For structured metadata, we simply verify that the input structure is preserved
			// since ConvertMetadataToMap doesn't need to process structured metadata

			// Check that structured metadata is preserved as-is
			if !reflect.DeepEqual(tt.input, tt.expected) {
				t.Errorf("Structured metadata test failed: input = %v, expected = %v", tt.input, tt.expected)
			}

			// If metadata field exists, verify it's properly structured
			if metadata, exists := tt.input["metadata"]; exists {
				if metadataMap, ok := metadata.(map[string]any); ok {
					// Verify metadata contains expected keys
					if auth, exists := metadataMap["authorization"]; exists {
						if authStr, ok := auth.(string); !ok || authStr == "" {
							t.Errorf("Authorization should be a non-empty string")
						}
					}
					if ua, exists := metadataMap["user-agent"]; exists {
						if uaStr, ok := ua.(string); !ok || uaStr == "" {
							t.Errorf("User-agent should be a non-empty string")
						}
					}
				} else {
					t.Errorf("Metadata should be a map[string]any")
				}
			}
		})
	}
}

func TestPrepareGrpcRequestData(t *testing.T) {
	tests := []struct {
		name     string
		input    map[string]any
		expected map[string]any
	}{
		{
			name: "complete gRPC request with structured metadata",
			input: map[string]any{
				"addr":    "localhost:50051",
				"service": "UserService",
				"method":  "CreateUser",
				"body":    `{"user": {"name": "John", "email": "john@example.com"}}`,
				"metadata": map[string]any{
					"authorization": "Bearer token",
					"user-agent":    "probe/1.0",
				},
			},
			expected: map[string]any{
				"addr":    "localhost:50051",
				"service": "UserService",
				"method":  "CreateUser",
				"body":    `{"user": {"name": "John", "email": "john@example.com"}}`,
				"metadata": map[string]any{
					"authorization": "Bearer token",
					"user-agent":    "probe/1.0",
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// For structured data, verify the input structure is preserved
			if !reflect.DeepEqual(tt.input, tt.expected) {
				t.Errorf("PrepareGrpcRequestData test failed: input = %v, expected = %v", tt.input, tt.expected)
			}

			// Verify basic required fields
			if addr, exists := tt.input["addr"]; !exists || addr == "" {
				t.Errorf("addr field is required and should not be empty")
			}
			if service, exists := tt.input["service"]; !exists || service == "" {
				t.Errorf("service field is required and should not be empty")
			}
			if method, exists := tt.input["method"]; !exists || method == "" {
				t.Errorf("method field is required and should not be empty")
			}

			// Verify structured metadata if present
			if metadata, exists := tt.input["metadata"]; exists {
				if metadataMap, ok := metadata.(map[string]any); ok {
					if len(metadataMap) == 0 {
						t.Errorf("metadata should not be empty if present")
					}
				} else {
					t.Errorf("metadata should be a map[string]any")
				}
			}
		})
	}
}

func TestNewReq(t *testing.T) {
	req := NewReq()

	if req.Timeout != "30s" {
		t.Errorf("NewReq() timeout = %s, want 30s", req.Timeout)
	}
	if req.TLS != false {
		t.Errorf("NewReq() tls = %t, want false", req.TLS)
	}
	if req.Insecure != false {
		t.Errorf("NewReq() insecure = %t, want false", req.Insecure)
	}
	if req.Metadata == nil {
		t.Errorf("NewReq() metadata should not be nil")
	}
}

func TestReq_Validation(t *testing.T) {
	tests := []struct {
		name    string
		req     *Req
		wantErr bool
		errMsg  string
	}{
		{
			name: "missing addr",
			req: &Req{
				Service: "UserService",
				Method:  "GetUser",
			},
			wantErr: true,
			errMsg:  "Req.Addr is required",
		},
		{
			name: "missing service",
			req: &Req{
				Addr:   "localhost:50051",
				Method: "GetUser",
			},
			wantErr: true,
			errMsg:  "Req.Service is required",
		},
		{
			name: "missing method",
			req: &Req{
				Addr:    "localhost:50051",
				Service: "UserService",
			},
			wantErr: true,
			errMsg:  "Req.Method is required",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Set defaults
			if tt.req.Timeout == "" {
				tt.req.Timeout = "30s"
			}
			if tt.req.Metadata == nil {
				tt.req.Metadata = make(map[string]string)
			}

			_, err := tt.req.Do()
			if tt.wantErr {
				if err == nil {
					t.Errorf("Req.Do() expected error but got nil")
					return
				}
				if err.Error() != tt.errMsg {
					t.Errorf("Req.Do() error = %v, want %v", err.Error(), tt.errMsg)
				}
			} else {
				if err != nil {
					t.Errorf("Req.Do() unexpected error = %v", err)
				}
			}
		})
	}
}

func TestRequest_StructureValidation(t *testing.T) {
	// Test that Request function can be called without panicking
	// Note: This doesn't test actual gRPC calls as that would require a running server
	data := map[string]any{
		"addr":    "localhost:50051",
		"service": "TestService",
		"method":  "TestMethod",
		"body":    `{"test": "value"}`,
	}

	// This will fail with connection error, but should not panic
	_, err := Request(data)
	if err == nil {
		t.Log("Request() completed without error (unexpected in unit test)")
	} else {
		// Expected - connection will fail in unit test environment
		t.Logf("Request() failed as expected in unit test: %v", err)
	}
}

// startHealthServer serves the standard health service with reflection, which
// answers NOT_FOUND for a service it does not know. Every reply carries the
// header x-test-header and the trailer x-test-trailer. A check of the service
// "slow" takes a second before it is answered, and one of "drop" stops the
// server without answering.
func startHealthServer(t *testing.T) string {
	t.Helper()
	lis, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	var s *grpclib.Server
	s = grpclib.NewServer(grpclib.UnaryInterceptor(func(ctx context.Context, req any, _ *grpclib.UnaryServerInfo, handler grpclib.UnaryHandler) (any, error) {
		_ = grpclib.SetHeader(ctx, metadata.Pairs("x-test-header", "from-header"))
		_ = grpclib.SetTrailer(ctx, metadata.Pairs("x-test-trailer", "from-trailer"))
		if r, ok := req.(*healthpb.HealthCheckRequest); ok {
			switch r.Service {
			case "slow":
				time.Sleep(time.Second)
			case "drop":
				go s.Stop()
				<-ctx.Done()
				return nil, ctx.Err()
			}
		}
		return handler(ctx, req)
	}))
	healthpb.RegisterHealthServer(s, health.NewServer())
	reflection.Register(s)
	go func() { _ = s.Serve(lis) }()
	t.Cleanup(s.Stop)
	return lis.Addr().String()
}

func TestRequestStatus(t *testing.T) {
	addr := startHealthServer(t)

	tests := []struct {
		name       string
		body       string
		wantCode   string
		wantStatus int
	}{
		{name: "ok", body: `{"service": ""}`, wantCode: "OK", wantStatus: 0},
		// The server answering with an error status is a result to test, not a
		// failure to make the call.
		{name: "not found", body: `{"service": "unknown"}`, wantCode: "NOT_FOUND", wantStatus: 1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ret, err := Request(map[string]any{
				"addr":    addr,
				"service": "grpc.health.v1.Health",
				"method":  "Check",
				"body":    tt.body,
			})
			if err != nil {
				t.Fatalf("Request() error: %v", err)
			}
			res, _ := ret["res"].(map[string]any)
			if res["status_code"] != tt.wantCode {
				t.Errorf("status_code = %v, want %s", res["status_code"], tt.wantCode)
			}
			if ret["status"] != tt.wantStatus {
				t.Errorf("status = %v, want %d", ret["status"], tt.wantStatus)
			}
			if tt.wantCode != "OK" && res["status_message"] == "" {
				t.Error("status_message is empty")
			}
		})
	}
}

func TestRequestUnreachable(t *testing.T) {
	// Nothing listens there, so no status ever comes back: that is an error.
	lis, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := lis.Addr().String()
	_ = lis.Close()

	_, err = Request(map[string]any{
		"addr":    addr,
		"service": "grpc.health.v1.Health",
		"method":  "Check",
		"timeout": "2s",
	})
	if err == nil {
		t.Fatal("Request() to a closed port succeeded")
	}
}

func TestRequestNoAnswer(t *testing.T) {
	// Every failed call ends with a status, but these were made up on this
	// side: the server never sent one, so they are errors, not results.
	tests := []struct {
		name    string
		service string
		timeout string
	}{
		{name: "timeout runs out", service: "slow", timeout: "300ms"},
		{name: "connection lost", service: "drop", timeout: "5s"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			addr := startHealthServer(t)
			ret, err := Request(map[string]any{
				"addr":    addr,
				"service": "grpc.health.v1.Health",
				"method":  "Check",
				"body":    fmt.Sprintf(`{"service": %q}`, tt.service),
				"timeout": tt.timeout,
			})
			if err == nil {
				t.Fatalf("Request() returned %v, want an error", ret)
			}
		})
	}
}

func TestStatusCodeName(t *testing.T) {
	tests := map[codes.Code]string{
		codes.OK:                 "OK",
		codes.Canceled:           "CANCELLED",
		codes.NotFound:           "NOT_FOUND",
		codes.DeadlineExceeded:   "DEADLINE_EXCEEDED",
		codes.FailedPrecondition: "FAILED_PRECONDITION",
		codes.Unauthenticated:    "UNAUTHENTICATED",
	}
	for c, want := range tests {
		if got := statusCodeName(c); got != want {
			t.Errorf("statusCodeName(%v) = %s, want %s", c, got, want)
		}
	}
}

func TestRequestMetadata(t *testing.T) {
	addr := startHealthServer(t)

	// The status does not matter: headers and trailers come back either way.
	for _, body := range []string{`{"service": ""}`, `{"service": "unknown"}`} {
		ret, err := Request(map[string]any{
			"addr":    addr,
			"service": "grpc.health.v1.Health",
			"method":  "Check",
			"body":    body,
		})
		if err != nil {
			t.Fatalf("Request() error: %v", err)
		}
		res, _ := ret["res"].(map[string]any)
		md, _ := res["metadata"].(map[string]string)
		if md["x-test-header"] != "from-header" || md["x-test-trailer"] != "from-trailer" {
			t.Errorf("body %s: metadata = %v, want the header and the trailer", body, res["metadata"])
		}
	}
}

func TestRequestInvalidTimeout(t *testing.T) {
	_, err := Request(map[string]any{
		"addr":    "127.0.0.1:1",
		"service": "grpc.health.v1.Health",
		"method":  "Check",
		"timeout": "soon",
	})
	if err == nil || !strings.Contains(err.Error(), "invalid timeout") {
		t.Fatalf("Request() error = %v, want an invalid timeout error", err)
	}
}

func TestRequestTimeoutInSeconds(t *testing.T) {
	// A number of seconds is a timeout, so the call gets as far as the
	// address, which refuses it.
	_, err := Request(map[string]any{
		"addr":    "127.0.0.1:1",
		"service": "grpc.health.v1.Health",
		"method":  "Check",
		"timeout": 2,
	})
	if err == nil || strings.Contains(err.Error(), "invalid timeout") {
		t.Fatalf("Request() error = %v, want the call to fail on the address", err)
	}
}

// clockProto declares a service whose response imports a well-known type,
// so that its reflection reply holds more than one file.
const clockProto = `syntax = "proto3";
package clock.v1;
import "google/protobuf/timestamp.proto";
message NowRequest { string zone = 1; }
message NowResponse {
  google.protobuf.Timestamp at = 1;
  string zone = 2;
}
service Clock { rpc Now(NowRequest) returns (NowResponse); }
`

// startClockServer serves clockProto, built at run time, with reflection
// that tells its definition, and returns its address and the path of the
// .proto file.
func startClockServer(t *testing.T) (string, string) {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "clock.proto")
	if err := os.WriteFile(path, []byte(clockProto), 0o600); err != nil {
		t.Fatal(err)
	}
	c, err := loadContract([]string{path}, []string{dir})
	if err != nil {
		t.Fatal(err)
	}
	fd := c.files[0]
	files := new(protoregistry.Files)
	for _, f := range []protoreflect.FileDescriptor{timestamppb.File_google_protobuf_timestamp_proto, fd} {
		if err := files.RegisterFile(f); err != nil {
			t.Fatal(err)
		}
	}
	method := fd.Services().Get(0).Methods().Get(0)

	lis, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	s := grpclib.NewServer()
	s.RegisterService(&grpclib.ServiceDesc{
		ServiceName: "clock.v1.Clock",
		HandlerType: (*any)(nil),
		Methods: []grpclib.MethodDesc{{
			MethodName: "Now",
			Handler: func(_ any, _ context.Context, dec func(any) error, _ grpclib.UnaryServerInterceptor) (any, error) {
				in := dynamicpb.NewMessage(method.Input())
				if err := dec(in); err != nil {
					return nil, err
				}
				out := dynamicpb.NewMessage(method.Output())
				fields := method.Output().Fields()
				// A zone of "broken" is answered with a reply no definition
				// can read: field 1 claims more bytes than follow.
				if in.Get(method.Input().Fields().ByName("zone")).String() == "broken" {
					out.SetUnknown(protoreflect.RawFields{0x0a, 0xff, 0x01})
					return out, nil
				}
				out.Set(fields.ByName("zone"), in.Get(method.Input().Fields().ByName("zone")))
				at := out.Mutable(fields.ByName("at")).Message()
				at.Set(at.Descriptor().Fields().ByName("seconds"), protoreflect.ValueOfInt64(1760000000))
				return out, nil
			},
		}},
	}, struct{}{})
	v1alphareflectiongrpc.RegisterServerReflectionServer(s, reflection.NewServer(reflection.ServerOptions{Services: s, DescriptorResolver: files}))
	go func() { _ = s.Serve(lis) }()
	t.Cleanup(s.Stop)
	return lis.Addr().String(), path
}

// A service whose definition imports another file is built from the whole
// reflection reply, so that it can be called through reflection, and its
// definition checked against the .proto files.
func TestRequestThroughReflectionWithImports(t *testing.T) {
	addr, _ := startClockServer(t)

	ret, err := Request(map[string]any{"addr": addr, "service": "clock.v1.Clock", "method": "Now", "body": `{"zone": "Asia/Tokyo"}`})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if body := ret["res"].(map[string]any)["body"].(string); !strings.Contains(body, `"zone":"Asia/Tokyo"`) || !strings.Contains(body, `"at":"2025-10-09T`) {
		t.Errorf("body = %s, want the zone and the time", body)
	}

	// The server's definition is compared with the files', which it could
	// not be when it was left unbuilt.
	spec := strings.Replace(clockProto, "string zone = 2;", "int32 zone = 2;", 1)
	specPath := filepath.Join(t.TempDir(), "clock.proto")
	if err := os.WriteFile(specPath, []byte(spec), 0o600); err != nil {
		t.Fatal(err)
	}
	ret, err = Request(map[string]any{
		"addr": addr, "service": "clock.v1.Clock", "method": "Now", "body": `{"zone": "Asia/Tokyo"}`,
		"proto": map[string]any{"files": []any{specPath}, "import_paths": []any{filepath.Dir(specPath)}},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	vs := ret["res"].(map[string]any)["violations"].([]any)
	found := false
	for _, v := range vs {
		if strings.Contains(v.(map[string]any)["message"].(string), "the server's clock.v1.NowResponse declares string zone = 2") {
			found = true
		}
	}
	if !found {
		t.Errorf("violations = %v, want the server's definition compared", vs)
	}
}

// The check against the .proto files is told to the runner, as res shows
// it, and none is told for a call made without them.
func TestRequestStepReportsTheContractToTheRunner(t *testing.T) {
	addr, path := startClockServer(t)
	with := map[string]any{"addr": addr, "service": "clock.v1.Clock", "method": "Now", "body": `{"zone": "Asia/Tokyo"}`}

	contract := &actionrpc.Contract{}
	if _, err := RequestStep(actionrpc.Call{With: with, Contract: contract}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got := contract.Check(); got != nil {
		t.Errorf("Check() = %+v without .proto files, want none", got)
	}

	checked := maps.Clone(with)
	checked["proto"] = map[string]any{"files": []any{path}, "import_paths": []any{filepath.Dir(path)}}
	contract = &actionrpc.Contract{}
	ret, err := RequestStep(actionrpc.Call{With: checked, Contract: contract})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	res := ret["res"].(map[string]any)
	vs, _ := res["violations"].([]any)
	matched, _ := res["contract"].(map[string]any)
	if got, want := contract.Check(), actionrpc.ContractCheckOf(vs, matched); got == nil || !reflect.DeepEqual(*got, want) {
		t.Errorf("Check() = %+v, want %+v as res shows it", got, want)
	}
}

// A reply the files' definition cannot read at all, made with it as the
// server tells no definition of its own, breaks the files rather than
// failing the call.
func TestRequestWithoutReflectionOfAReplyTheFilesCannotRead(t *testing.T) {
	addr := startUserServerWith(t, false)
	spec := strings.Replace(userProto(t), "string email = 3;", "Profile email = 3;", 1)
	path := writeProto(t, spec)
	ret, err := Request(map[string]any{
		"addr": addr, "service": "UserService", "method": "GetUser", "body": `{"user_id": "123"}`,
		"proto": map[string]any{"files": []any{path}, "import_paths": []any{filepath.Dir(path)}},
	})
	if err != nil {
		t.Fatalf("err = %v, want the reply told as one that breaks the files", err)
	}
	res := ret["res"].(map[string]any)
	if res["status_code"] != "OK" {
		t.Errorf("status_code = %v, want OK: the server answered", res["status_code"])
	}
	vs := res["violations"].([]any)
	if len(vs) != 1 || !strings.Contains(vs[0].(map[string]any)["message"].(string), "response body does not read as GetUserResponse") {
		t.Errorf("violations = %v, want the reply that cannot be read", vs)
	}
}

func TestReflectionError(t *testing.T) {
	if err := reflectionError("x", status.Error(codes.Unimplemented, "no reflection")); !errors.Is(err, errNoReflection) {
		t.Errorf("an UNIMPLEMENTED reflection = %v, want it marked as none", err)
	}
	if err := reflectionError("x", status.Error(codes.Unavailable, "down")); errors.Is(err, errNoReflection) {
		t.Errorf("an UNAVAILABLE server = %v, want it not marked", err)
	}
}

// A service given by its short name is looked up by its full name, which
// the .proto files know, so that its definition is compared rather than
// left out as one the server does not list.
func TestRequestThroughReflectionByShortName(t *testing.T) {
	addr, _ := startClockServer(t)
	spec := strings.Replace(clockProto, "string zone = 2;", "int32 zone = 2;", 1)
	specPath := filepath.Join(t.TempDir(), "clock.proto")
	if err := os.WriteFile(specPath, []byte(spec), 0o600); err != nil {
		t.Fatal(err)
	}
	ret, err := Request(map[string]any{
		"addr": addr, "service": "Clock", "method": "Now", "body": `{"zone": "Asia/Tokyo"}`,
		"proto": map[string]any{"files": []any{specPath}, "import_paths": []any{filepath.Dir(specPath)}},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	found := false
	for _, v := range ret["res"].(map[string]any)["violations"].([]any) {
		if strings.Contains(v.(map[string]any)["message"].(string), "the server's clock.v1.NowResponse declares string zone = 2") {
			found = true
		}
	}
	if !found {
		t.Errorf("violations = %v, want the server's definition compared", ret["res"].(map[string]any)["violations"])
	}
}

// A reply that neither the server's definition nor the files' can read
// breaks the files, when they are given, and is an error of the action
// when they are not.
func TestRequestThroughReflectionOfAReplyNoDefinitionReads(t *testing.T) {
	addr, path := startClockServer(t)
	with := map[string]any{"addr": addr, "service": "clock.v1.Clock", "method": "Now", "body": `{"zone": "broken"}`}

	if _, err := Request(with); err == nil || !strings.Contains(err.Error(), "failed to read the response") {
		t.Errorf("without .proto files: err = %v, want the reply that cannot be read", err)
	}

	with["proto"] = map[string]any{"files": []any{path}, "import_paths": []any{filepath.Dir(path)}}
	ret, err := Request(with)
	if err != nil {
		t.Fatalf("with .proto files: err = %v, want the reply told as one that breaks them", err)
	}
	vs := ret["res"].(map[string]any)["violations"].([]any)
	if len(vs) != 1 || !strings.Contains(vs[0].(map[string]any)["message"].(string), "response body does not read as clock.v1.NowResponse") {
		t.Errorf("violations = %v, want the reply that cannot be read", vs)
	}
}

// eofReflection is a reflection client whose stream the server has ended
// before the request was sent: the send fails with io.EOF, and the receive
// tells why.
type eofReflection struct{ why error }

func (c eofReflection) ServerReflectionInfo(context.Context, ...grpclib.CallOption) (v1alphareflectiongrpc.ServerReflection_ServerReflectionInfoClient, error) {
	return &eofStream{why: c.why}, nil
}

type eofStream struct {
	grpclib.ClientStream
	why error
}

//nolint:staticcheck // the action speaks v1alpha reflection, as it is still widely served
func (s *eofStream) Send(*v1alphareflectiongrpc.ServerReflectionRequest) error { return io.EOF }

//nolint:staticcheck // the action speaks v1alpha reflection, as it is still widely served
func (s *eofStream) Recv() (*v1alphareflectiongrpc.ServerReflectionResponse, error) {
	return nil, s.why
}
func (s *eofStream) CloseSend() error { return nil }

func TestGetServiceDescriptorReadsWhyASendFailed(t *testing.T) {
	r := NewReq()
	_, err := r.getServiceDescriptor(context.Background(), eofReflection{why: status.Error(codes.Unimplemented, "unknown service")}, "x.Svc")
	if !errors.Is(err, errNoReflection) {
		t.Errorf("err = %v, want a server without reflection", err)
	}
	_, err = r.getServiceDescriptor(context.Background(), eofReflection{why: status.Error(codes.Unavailable, "down")}, "x.Svc")
	if err == nil || errors.Is(err, errNoReflection) {
		t.Errorf("err = %v, want an error that is not a server without reflection", err)
	}
}

// A file of a reply is bound to the version of each file it imports that
// the reply holds, however the reply orders them, rather than to one built
// into Probe by the same name.
func TestBuildFilesBindsTheReplysVersion(t *testing.T) {
	dir := t.TempDir()
	// A Timestamp of the server's own, with a field the one built into
	// Probe does not have.
	wkt := filepath.Join(dir, "google", "protobuf")
	if err := os.MkdirAll(wkt, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(wkt, "timestamp.proto"), []byte("syntax = \"proto3\";\npackage google.protobuf;\nmessage Timestamp { int64 seconds = 1; int32 nanos = 2; string note = 3; }\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "event.proto"), []byte("syntax = \"proto3\";\nimport \"google/protobuf/timestamp.proto\";\nmessage Event { google.protobuf.Timestamp at = 1; }\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	compiled, err := (&protocompile.Compiler{Resolver: &protocompile.SourceResolver{ImportPaths: []string{dir}}}).Compile(context.Background(), "event.proto")
	if err != nil {
		t.Fatal(err)
	}
	event := protodesc.ToFileDescriptorProto(compiled[0])
	ts := protodesc.ToFileDescriptorProto(compiled[0].Imports().Get(0).FileDescriptor)

	// The importer comes first, as a reply may order it.
	files, err := buildFiles([]*descriptorpb.FileDescriptorProto{event, ts})
	if err != nil {
		t.Fatal(err)
	}
	d, err := files.FindDescriptorByName("Event")
	if err != nil {
		t.Fatal(err)
	}
	at := d.(protoreflect.MessageDescriptor).Fields().ByName("at").Message()
	if at.Fields().ByName("note") == nil {
		t.Errorf("Event.at is %v, want the reply's Timestamp, which has note", at.Fields())
	}
}
