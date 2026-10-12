package grpc

import (
	"context"
	"maps"
	"net"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"connectrpc.com/connect"
	"github.com/mozership/probe/actions/grpc/testserver/pb"
)

// connectUserServer serves GetUser of the user server with connect-go, as a
// server a connect-web client calls does, under prefix. It counts the calls
// that reach it, and the last request's headers.
type connectUserServer struct {
	url    string
	calls  atomic.Int32
	header atomic.Value
}

func startConnectServer(t *testing.T, prefix string) *connectUserServer {
	t.Helper()
	s := &connectUserServer{}
	path, handler := "/UserService/GetUser", connect.NewUnaryHandler("/UserService/GetUser",
		func(ctx context.Context, req *connect.Request[pb.GetUserRequest]) (*connect.Response[pb.GetUserResponse], error) {
			s.calls.Add(1)
			s.header.Store(req.Header().Clone())
			got, err := userServer{}.GetUser(ctx, req.Msg)
			if err != nil {
				return nil, connect.NewError(connect.CodeNotFound, err)
			}
			res := connect.NewResponse(got)
			res.Header().Set("X-Request-Id", "r1")
			res.Trailer().Set("X-Trace", "t1")
			return res, nil
		})
	mux := http.NewServeMux()
	mux.Handle(prefix+path, http.StripPrefix(prefix, handler))
	mux.Handle(prefix+"/UserService/WatchUsers", http.StripPrefix(prefix, connect.NewServerStreamHandler("/UserService/WatchUsers",
		func(ctx context.Context, req *connect.Request[pb.WatchUsersRequest], stream *connect.ServerStream[pb.User]) error {
			s.calls.Add(1)
			s.header.Store(req.Header().Clone())
			stream.ResponseTrailer().Set("X-Trace", "t1")
			return connectStatus(watchTestUsers(ctx, req.Msg, stream.Send))
		})))
	mux.Handle(prefix+"/UserService/ImportUsers", http.StripPrefix(prefix, connect.NewClientStreamHandler("/UserService/ImportUsers",
		func(ctx context.Context, stream *connect.ClientStream[pb.User]) (*connect.Response[pb.ImportUsersResponse], error) {
			s.calls.Add(1)
			n := 0
			for stream.Receive() {
				n++
			}
			if err := stream.Err(); err != nil {
				return nil, err
			}
			return connect.NewResponse(&pb.ImportUsersResponse{Imported: int32(n)}), nil
		})))
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	s.url = srv.URL + prefix
	return s
}

func connectRequest(t *testing.T, with map[string]any) (map[string]any, map[string]any, error) {
	t.Helper()
	data := map[string]any{"protocol": "connect", "service": "UserService", "method": "GetUser"}
	maps.Copy(data, with)
	ret, err := Request(data)
	res, _ := ret["res"].(map[string]any)
	return ret, res, err
}

// protoOf returns proto naming the .proto file at path, under its own
// directory as the import path.
func protoOf(path string, strict bool) map[string]any {
	return map[string]any{"files": []any{path}, "import_paths": []any{filepath.Dir(path)}, "strict": strict}
}

func TestConnectRequest(t *testing.T) {
	s := startConnectServer(t, "/rpc")
	spec := writeProto(t, userProto(t))

	tests := []struct {
		name  string
		with  map[string]any
		check func(t *testing.T, res map[string]any)
	}{
		{
			name: "json without .proto files sends the body as written",
			with: map[string]any{"body": `{"userId": "123"}`},
			check: func(t *testing.T, res map[string]any) {
				if !strings.Contains(res["body"].(string), `"name":"Test User"`) {
					t.Errorf("body = %v", res["body"])
				}
				if _, ok := res["violations"]; ok {
					t.Errorf("violations = %v, want none without .proto files", res["violations"])
				}
			},
		},
		{
			name: "json with .proto files",
			with: map[string]any{"body": map[string]any{"user_id": "123"}, "proto": protoOf(spec, false)},
			check: func(t *testing.T, res map[string]any) {
				if !strings.Contains(res["body"].(string), `"name":"Test User"`) {
					t.Errorf("body = %v", res["body"])
				}
				if v, _ := res["violations"].([]any); len(v) != 0 {
					t.Errorf("violations = %v, want none", v)
				}
				c, _ := res["contract"].(map[string]any)
				if c["operation"] != "UserService/GetUser" {
					t.Errorf("contract = %v", c)
				}
			},
		},
		{
			name: "proto with .proto files",
			with: map[string]any{"codec": "proto", "body": `{"userId": "123"}`, "proto": protoOf(spec, false)},
			check: func(t *testing.T, res map[string]any) {
				if !strings.Contains(res["body"].(string), `"name":"Test User"`) {
					t.Errorf("body = %v", res["body"])
				}
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.with["addr"] = s.url
			ret, res, err := connectRequest(t, tt.with)
			if err != nil {
				t.Fatalf("Request() error: %v", err)
			}
			if res["status_code"] != "OK" || ret["status"] != 0 {
				t.Fatalf("status_code = %v, status = %v, want OK and 0", res["status_code"], ret["status"])
			}
			md, _ := res["metadata"].(map[string]string)
			if md["x-request-id"] != "r1" || md["x-trace"] != "t1" {
				t.Errorf("metadata = %v, want the header and the trailer", md)
			}
			tt.check(t, res)
		})
	}
}

func TestConnectRequestSendsHeaders(t *testing.T) {
	s := startConnectServer(t, "")
	_, _, err := connectRequest(t, map[string]any{
		"addr":     s.url,
		"body":     `{"userId": "123"}`,
		"metadata": map[string]any{"authorization": "Bearer t"},
		"timeout":  "3s",
	})
	if err != nil {
		t.Fatalf("Request() error: %v", err)
	}
	h, _ := s.header.Load().(http.Header)
	if h.Get("Authorization") != "Bearer t" {
		t.Errorf("Authorization = %q, want the metadata", h.Get("Authorization"))
	}
	if h.Get("Connect-Timeout-Ms") != "3000" {
		t.Errorf("Connect-Timeout-Ms = %q, want 3000", h.Get("Connect-Timeout-Ms"))
	}
}

func TestConnectRequestErrorStatus(t *testing.T) {
	s := startConnectServer(t, "")
	ret, res, err := connectRequest(t, map[string]any{"addr": s.url, "body": `{"userId": "999"}`})
	if err != nil {
		t.Fatalf("Request() error: %v", err)
	}
	if res["status_code"] != "NOT_FOUND" || ret["status"] != 1 {
		t.Errorf("status_code = %v, status = %v, want NOT_FOUND and 1", res["status_code"], ret["status"])
	}
	if !strings.Contains(res["status_message"].(string), "user 999 not found") {
		t.Errorf("status_message = %v", res["status_message"])
	}
}

func TestConnectRequestAnswerWithoutAConnectError(t *testing.T) {
	tests := []struct {
		name        string
		status      int
		contentType string
		body        string
		wantCode    string
		wantMessage string
	}{
		{name: "a proxy's error page", status: http.StatusBadGateway, contentType: "text/html", body: "<h1>Bad Gateway</h1>", wantCode: "UNAVAILABLE", wantMessage: "502 Bad Gateway"},
		{name: "an error without a code", status: http.StatusUnauthorized, contentType: "application/json", body: `{"message": "who are you"}`, wantCode: "UNAUTHENTICATED", wantMessage: "who are you"},
		{name: "a page that is no Connect answer", status: http.StatusOK, contentType: "text/html", body: "<p>hello</p>", wantCode: "UNKNOWN", wantMessage: "invalid content-type"},
		{name: "an answer in another codec", status: http.StatusOK, contentType: "application/proto", body: "", wantCode: "INTERNAL", wantMessage: "invalid content-type"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", tt.contentType)
				w.WriteHeader(tt.status)
				_, _ = w.Write([]byte(tt.body))
			}))
			t.Cleanup(srv.Close)
			ret, res, err := connectRequest(t, map[string]any{"addr": srv.URL})
			if err != nil {
				t.Fatalf("Request() error: %v, want the answer as a result", err)
			}
			if res["status_code"] != tt.wantCode || ret["status"] != 1 {
				t.Errorf("status_code = %v, status = %v, want %s and 1", res["status_code"], ret["status"], tt.wantCode)
			}
			if !strings.Contains(res["status_message"].(string), tt.wantMessage) {
				t.Errorf("status_message = %v, want %q in it", res["status_message"], tt.wantMessage)
			}
		})
	}
}

func TestConnectRequestUnreachable(t *testing.T) {
	lis, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := lis.Addr().String()
	_ = lis.Close()

	if _, _, err := connectRequest(t, map[string]any{"addr": addr, "timeout": "2s"}); err == nil {
		t.Fatal("Request() to a closed port succeeded")
	}
}

func TestConnectRequestChecksAgainstProto(t *testing.T) {
	s := startConnectServer(t, "")
	server := userProto(t)

	tests := []struct {
		name       string
		proto      string
		strict     bool
		body       string
		wantIn     string
		wantCalled bool
	}{
		{
			name:       "a response of another type than the files declare",
			proto:      strings.Replace(server, "string email = 3;", "int32 email = 3;", 1),
			body:       `{"userId": "123"}`,
			wantIn:     "response",
			wantCalled: true,
		},
		{
			name:       "a field the files do not declare, under strict",
			proto:      strings.Replace(server, "string created_at = 6;", "", 1),
			strict:     true,
			body:       `{"userId": "123"}`,
			wantIn:     "response",
			wantCalled: true,
		},
		{
			name:       "a request the files refuse is not sent",
			proto:      server,
			body:       `{"userId": 123}`,
			wantIn:     "request",
			wantCalled: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			before := s.calls.Load()
			_, res, err := connectRequest(t, map[string]any{
				"addr":  s.url,
				"body":  tt.body,
				"proto": protoOf(writeProto(t, tt.proto), tt.strict),
			})
			if err != nil {
				t.Fatalf("Request() error: %v", err)
			}
			v, _ := res["violations"].([]any)
			if len(v) == 0 {
				t.Fatalf("violations = none, want one in the %s", tt.wantIn)
			}
			if m, _ := v[0].(map[string]any); m["in"] != tt.wantIn {
				t.Errorf("violation = %v, want one in the %s", m, tt.wantIn)
			}
			if called := s.calls.Load() > before; called != tt.wantCalled {
				t.Errorf("called = %v, want %v", called, tt.wantCalled)
			}
		})
	}

	t.Run("a field the files do not declare, without strict", func(t *testing.T) {
		_, res, err := connectRequest(t, map[string]any{
			"addr":  s.url,
			"body":  `{"userId": "123"}`,
			"proto": protoOf(writeProto(t, strings.Replace(server, "string created_at = 6;", "", 1)), false),
		})
		if err != nil {
			t.Fatalf("Request() error: %v", err)
		}
		if v, _ := res["violations"].([]any); len(v) != 0 {
			t.Errorf("violations = %v, want none", v)
		}
	})
}

func TestConnectRequestRejected(t *testing.T) {
	tests := []struct {
		name    string
		with    map[string]any
		wantErr string
	}{
		{name: "an unknown protocol", with: map[string]any{"protocol": "grpc-web", "addr": "localhost:1"}, wantErr: "unknown protocol"},
		{name: "a codec for gRPC", with: map[string]any{"protocol": "grpc", "codec": "json", "addr": "localhost:1"}, wantErr: "codec is for protocol connect"},
		{name: "an unknown codec", with: map[string]any{"codec": "xml", "addr": "localhost:1"}, wantErr: "unknown codec"},
		{name: "codec proto without .proto files", with: map[string]any{"codec": "proto", "addr": "localhost:1"}, wantErr: "codec proto needs proto.files"},
		{name: "a body that is not JSON", with: map[string]any{"body": "{", "addr": "localhost:1"}, wantErr: "body is not JSON"},
		{name: "an addr of another scheme", with: map[string]any{"addr": "ftp://example.com"}, wantErr: "must be an http or https URL"},
		{name: "an http addr with tls", with: map[string]any{"addr": "http://example.com", "tls": true}, wantErr: "is http, but tls is true"},
		{name: "a host and port with a query", with: map[string]any{"addr": "localhost:8080/rpc?token=x"}, wantErr: "must not have a query or a fragment"},
		{name: "a host and port with a fragment", with: map[string]any{"addr": "localhost:8080/rpc#x"}, wantErr: "must not have a query or a fragment"},
		{name: "a URL with an empty query", with: map[string]any{"addr": "http://localhost:8080/rpc?"}, wantErr: "must not have a query or a fragment"},
		{name: "a URL without a host", with: map[string]any{"addr": "http:///rpc"}, wantErr: "names no host"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, _, err := connectRequest(t, tt.with)
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("Request() error = %v, want %q", err, tt.wantErr)
			}
		})
	}
}

func TestConnectRequestOfAMethodThatStreamsBothWays(t *testing.T) {
	spec := writeProto(t, `syntax = "proto3";
message Ping { string id = 1; }
service PingService { rpc Chat(stream Ping) returns (stream Ping); }
`)
	_, _, err := connectRequest(t, map[string]any{
		"addr":    "localhost:1",
		"service": "PingService",
		"method":  "Chat",
		"proto":   protoOf(spec, false),
	})
	if err == nil || !strings.Contains(err.Error(), "streams both ways") {
		t.Errorf("Request() error = %v, want one saying the method streams both ways", err)
	}
}

func TestConnectRequestClosesItsConnection(t *testing.T) {
	closed := make(chan struct{}, 1)
	srv := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte("{}"))
	}))
	srv.Config.ConnState = func(_ net.Conn, state http.ConnState) {
		if state == http.StateClosed {
			select {
			case closed <- struct{}{}:
			default:
			}
		}
	}
	srv.Start()
	t.Cleanup(srv.Close)

	if _, _, err := connectRequest(t, map[string]any{"addr": srv.URL}); err != nil {
		t.Fatalf("Request() error: %v", err)
	}
	select {
	case <-closed:
	case <-time.After(2 * time.Second):
		t.Error("the connection was left open after the call")
	}
}

func TestConnectTimeout(t *testing.T) {
	tests := []struct {
		timeout time.Duration
		want    string
		ok      bool
	}{
		{timeout: 3 * time.Second, want: "3000", ok: true},
		{timeout: 500 * time.Microsecond, want: "1", ok: true},
		{timeout: 9999999999 * time.Millisecond, want: "9999999999", ok: true},
		{timeout: 10000000000 * time.Millisecond, ok: false},
		{timeout: 0, ok: false},
	}
	for _, tt := range tests {
		got, ok := connectTimeout(tt.timeout)
		if got != tt.want || ok != tt.ok {
			t.Errorf("connectTimeout(%v) = %q, %v, want %q, %v", tt.timeout, got, ok, tt.want, tt.ok)
		}
	}
}

func TestConnectRequestOfAReplyThatIsNotJSON(t *testing.T) {
	for _, body := range []string{"", "{"} {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(body))
		}))
		t.Cleanup(srv.Close)
		if _, _, err := connectRequest(t, map[string]any{"addr": srv.URL}); err == nil || !strings.Contains(err.Error(), "body is not JSON") {
			t.Errorf("body %q: Request() error = %v, want one saying it is not JSON", body, err)
		}
	}
}

func TestConnectBase(t *testing.T) {
	tests := []struct {
		addr string
		tls  bool
		want string
	}{
		{addr: "localhost:8080", want: "http://localhost:8080"},
		{addr: "api.example.com", tls: true, want: "https://api.example.com"},
		{addr: "https://api.example.com/rpc/", want: "https://api.example.com/rpc"},
		{addr: "http://localhost:8080", want: "http://localhost:8080"},
		{addr: "localhost:8080/rpc/", want: "http://localhost:8080/rpc"},
	}
	for _, tt := range tests {
		r := &Req{Addr: tt.addr, TLS: tt.tls}
		got, err := r.connectBase()
		if err != nil || got != tt.want {
			t.Errorf("connectBase(%q, tls %v) = %q, %v, want %q", tt.addr, tt.tls, got, err, tt.want)
		}
	}
}

func TestConnectMetadata(t *testing.T) {
	h := http.Header{}
	h.Set("X-Id", "header")
	h.Set("Trailer-X-Id", "trailer")
	h.Set("Content-Type", "application/json")
	got := connectMetadata(h)
	if got["x-id"] != "trailer" {
		t.Errorf("x-id = %q, want the trailer to win", got["x-id"])
	}
	if got["content-type"] != "application/json" {
		t.Errorf("content-type = %q", got["content-type"])
	}
	if _, ok := got["trailer-x-id"]; ok {
		t.Error("the trailer kept its prefix")
	}
}
