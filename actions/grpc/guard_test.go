package grpc

import (
	"maps"
	"slices"
	"strings"
	"testing"

	"github.com/mozership/probe/actionrpc"
)

// readOnlyGetUser is the server's .proto file with GetUser declared free of
// side effects.
func readOnlyGetUser(t *testing.T) string {
	t.Helper()
	return strings.Replace(userProto(t),
		"rpc GetUser(GetUserRequest) returns (GetUserResponse);",
		"rpc GetUser(GetUserRequest) returns (GetUserResponse) { option idempotency_level = NO_SIDE_EFFECTS; }", 1)
}

func guardedRequest(guard actionrpc.Guard, with map[string]any) (map[string]any, error) {
	data := map[string]any{"service": "UserService", "method": "GetUser", "body": `{"user_id": "123"}`}
	maps.Copy(data, with)
	return RequestStep(actionrpc.Call{With: data, Guard: guard})
}

func TestGRPCHosts(t *testing.T) {
	tests := []struct {
		addr string
		want []string
		ok   bool
	}{
		{addr: "localhost:50051", want: []string{"localhost:50051"}, ok: true},
		{addr: "api.example.com", want: []string{"api.example.com:443"}, ok: true},
		{addr: "dns:///api.example.com:8443", want: []string{"api.example.com:8443"}, ok: true},
		{addr: "dns://8.8.8.8/api.example.com", want: []string{"api.example.com:443", "8.8.8.8:53"}, ok: true},
		{addr: "dns://ns.example.com:5353/api.example.com:8443", want: []string{"api.example.com:8443", "ns.example.com:5353"}, ok: true},
		{addr: "passthrough:///10.0.0.1:50051", want: []string{"10.0.0.1:50051"}, ok: true},
		{addr: "[::1]:50051", want: []string{"[::1]:50051"}, ok: true},
		{addr: "unix:///tmp/grpc.sock", ok: false},
		{addr: "unix:grpc.sock", ok: false},
		{addr: "xds:///users", ok: false},
		{addr: "dns:///", ok: false},
	}
	for _, tt := range tests {
		got, ok := grpcHosts(tt.addr)
		if !slices.Equal(got, tt.want) || ok != tt.ok {
			t.Errorf("grpcHosts(%q) = %q, %v, want %q, %v", tt.addr, got, ok, tt.want, tt.ok)
		}
	}
}

func TestRequestUnderAllowHost(t *testing.T) {
	addr := startUserServer(t)
	s := startConnectServer(t, "")

	tests := []struct {
		name        string
		with        map[string]any
		allow       []string
		wantRefused string
	}{
		{name: "gRPC to a host allowed", with: map[string]any{"addr": addr}, allow: []string{addr}},
		{name: "gRPC to another host", with: map[string]any{"addr": addr}, allow: []string{"api.example.com"}, wantRefused: "is not one the run allows"},
		{name: "gRPC through a DNS server not allowed", with: map[string]any{"addr": "dns://8.8.8.8/" + addr}, allow: []string{addr}, wantRefused: "8.8.8.8:53"},
		{name: "gRPC to a Unix socket", with: map[string]any{"addr": "unix:///tmp/probe.sock"}, allow: []string{"localhost"}, wantRefused: "names no host"},
		{name: "Connect to a host allowed", with: map[string]any{"protocol": "connect", "addr": s.url, "body": `{"userId": "123"}`}, allow: []string{strings.TrimPrefix(s.url, "http://")}},
		{name: "Connect to another host", with: map[string]any{"protocol": "connect", "addr": s.url}, allow: []string{"api.example.com"}, wantRefused: "is not one the run allows"},
		{name: "Connect to a host without a port, at that of its scheme", with: map[string]any{"protocol": "connect", "addr": "https://api.example.com"}, allow: []string{"api.example.com:80"}, wantRefused: "api.example.com:443"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ret, err := guardedRequest(actionrpc.Guard{AllowHosts: tt.allow}, tt.with)
			if tt.wantRefused == "" {
				if err != nil {
					t.Fatalf("Request() error: %v", err)
				}
				if res, _ := ret["res"].(map[string]any); res["status_code"] != "OK" {
					t.Errorf("status_code = %v, want OK", res["status_code"])
				}
				return
			}
			if !actionrpc.IsRefused(err) || !strings.Contains(err.Error(), tt.wantRefused) {
				t.Errorf("Request() error = %v, want a refusal saying %q", err, tt.wantRefused)
			}
		})
	}
}

func TestRequestUnderReadOnly(t *testing.T) {
	withReflection := startUserServerWith(t, true)
	withoutReflection := startUserServerWith(t, false)
	s := startConnectServer(t, "")
	readOnly := protoOf(writeProto(t, readOnlyGetUser(t)), false)
	plain := protoOf(writeProto(t, userProto(t)), false)

	tests := []struct {
		name        string
		with        map[string]any
		wantRefused string
	}{
		{
			name:        "gRPC to a method nothing declares free of side effects",
			with:        map[string]any{"addr": withReflection},
			wantRefused: "its idempotency_level in the server's definition is unset, not NO_SIDE_EFFECTS",
		},
		{
			name:        "gRPC to a method the files declare free of side effects, but not the server",
			with:        map[string]any{"addr": withReflection, "proto": readOnly},
			wantRefused: "in the server's definition is unset",
		},
		{
			name: "gRPC without reflection to a method the files declare free of side effects",
			with: map[string]any{"addr": withoutReflection, "proto": readOnly},
		},
		{
			name:        "gRPC without reflection to a method the files do not declare free of side effects",
			with:        map[string]any{"addr": withoutReflection, "proto": plain},
			wantRefused: "its idempotency_level in the .proto files is unset",
		},
		{
			name:        "gRPC to a method the files declare idempotent, which may write",
			with:        map[string]any{"addr": withoutReflection, "proto": protoOf(writeProto(t, strings.Replace(readOnlyGetUser(t), "NO_SIDE_EFFECTS", "IDEMPOTENT", 1)), false)},
			wantRefused: "its idempotency_level in the .proto files is IDEMPOTENT, not NO_SIDE_EFFECTS",
		},
		{
			name:        "Connect without .proto files",
			with:        map[string]any{"protocol": "connect", "addr": s.url, "body": `{"userId": "123"}`},
			wantRefused: "no definition of UserService/GetUser tells",
		},
		{
			name: "Connect to a method the files declare free of side effects",
			with: map[string]any{"protocol": "connect", "addr": s.url, "proto": readOnly},
		},
		{
			name:        "Connect to a method the files do not declare free of side effects",
			with:        map[string]any{"protocol": "connect", "addr": s.url, "proto": plain},
			wantRefused: "its idempotency_level in the .proto files is unset",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			before := s.calls.Load()
			ret, err := guardedRequest(actionrpc.Guard{ReadOnly: true}, tt.with)
			if tt.wantRefused == "" {
				if err != nil {
					t.Fatalf("Request() error: %v", err)
				}
				if res, _ := ret["res"].(map[string]any); res["status_code"] != "OK" {
					t.Errorf("status_code = %v, want OK", res["status_code"])
				}
				return
			}
			if !actionrpc.IsRefused(err) || !strings.Contains(err.Error(), tt.wantRefused) {
				t.Errorf("Request() error = %v, want a refusal saying %q", err, tt.wantRefused)
			}
			if s.calls.Load() != before {
				t.Error("the refused call reached the server")
			}
		})
	}
}
