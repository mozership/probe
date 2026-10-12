package grpc

import (
	"context"
	"errors"
	"fmt"
	"io"
	"maps"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"

	"connectrpc.com/connect"
	"github.com/mozership/probe/actions/grpc/testserver/pb"
	grpclib "google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// watchTestUsers sends count users, numbered from 1; with count 0, one
// every 20ms until the call ends; with a count below 0, one, and then the
// status INVALID_ARGUMENT.
func watchTestUsers(ctx context.Context, req *pb.WatchUsersRequest, send func(*pb.User) error) error {
	if req.Count < 0 {
		if err := send(&pb.User{Id: "1", Name: "User 1"}); err != nil {
			return err
		}
		return status.Error(codes.InvalidArgument, "count must not be negative")
	}
	for i := 1; req.Count == 0 || i <= int(req.Count); i++ {
		if err := send(&pb.User{Id: strconv.Itoa(i), Name: fmt.Sprintf("User %d", i)}); err != nil {
			return err
		}
		if req.Count == 0 {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(20 * time.Millisecond):
			}
		}
	}
	return nil
}

// connectStatus turns a gRPC status into the Connect error of its code. An
// error of the context is left to connect-go, which tells it by its own
// code, such as DEADLINE_EXCEEDED.
func connectStatus(err error) error {
	if err == nil || errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
		return err
	}
	st := status.Convert(err)
	return connect.NewError(connect.Code(st.Code()), errors.New(st.Message()))
}

func (userServer) WatchUsers(req *pb.WatchUsersRequest, stream grpclib.ServerStreamingServer[pb.User]) error {
	return watchTestUsers(stream.Context(), req, stream.Send)
}

func (userServer) ImportUsers(stream grpclib.ClientStreamingServer[pb.User, pb.ImportUsersResponse]) error {
	n := 0
	for {
		_, err := stream.Recv()
		if errors.Is(err, io.EOF) {
			return stream.SendAndClose(&pb.ImportUsersResponse{Imported: int32(n)})
		}
		if err != nil {
			return err
		}
		n++
	}
}

// streamCall makes a call to the user server's streaming methods, by gRPC
// or by Connect, and returns its result and res.
func streamCall(t *testing.T, with map[string]any) (map[string]any, map[string]any, error) {
	t.Helper()
	data := map[string]any{"service": "UserService"}
	maps.Copy(data, with)
	ret, err := Request(data)
	res, _ := ret["res"].(map[string]any)
	return ret, res, err
}

// messageIDs returns the id of each message of res.messages.
func messageIDs(res map[string]any) []string {
	var ids []string
	msgs, _ := res["messages"].([]any)
	for _, m := range msgs {
		msg, _ := m.(map[string]any)
		id, _ := msg["id"].(string)
		ids = append(ids, id)
	}
	return ids
}

func TestStreamingCalls(t *testing.T) {
	grpcAddr := startUserServer(t)
	s := startConnectServer(t, "")
	files := protoOf(writeProto(t, userProto(t)), false)

	protocols := []struct {
		name string
		with map[string]any
	}{
		{name: "gRPC", with: map[string]any{"addr": grpcAddr}},
		{name: "Connect as JSON", with: map[string]any{"protocol": "connect", "addr": s.url, "proto": files}},
		{name: "Connect as protobuf", with: map[string]any{"protocol": "connect", "addr": s.url, "codec": "proto", "proto": files}},
	}
	tests := []struct {
		name         string
		with         map[string]any
		wantCode     string
		wantIDs      []string
		wantComplete any
		wantBody     string
		// single is a method that answers once, without res.messages.
		single bool
	}{
		{
			name:         "responses until the server ends the stream",
			with:         map[string]any{"method": "WatchUsers", "body": `{"count": 3}`},
			wantCode:     "OK",
			wantIDs:      []string{"1", "2", "3"},
			wantComplete: true,
			wantBody:     `"id":"3"`,
		},
		{
			name:         "responses up to max_messages of a stream that does not end",
			with:         map[string]any{"method": "WatchUsers", "body": `{"count": 0}`, "max_messages": 2},
			wantCode:     "",
			wantIDs:      []string{"1", "2"},
			wantComplete: false,
		},
		{
			name:         "a stream the server ends with an error",
			with:         map[string]any{"method": "WatchUsers", "body": `{"count": -1}`},
			wantCode:     "INVALID_ARGUMENT",
			wantIDs:      []string{"1"},
			wantComplete: true,
		},
		{
			// The server is told the timeout too, and may end the stream
			// itself, so res.complete is either.
			name:     "a stream the timeout cuts",
			with:     map[string]any{"method": "WatchUsers", "body": `{"count": 0}`, "timeout": "300ms"},
			wantCode: "DEADLINE_EXCEEDED",
		},
		{
			name:     "requests in a list",
			with:     map[string]any{"method": "ImportUsers", "body": []any{map[string]any{"name": "a"}, map[string]any{"name": "b"}, map[string]any{"name": "c"}}},
			wantCode: "OK",
			wantBody: `"imported":3`,
			single:   true,
		},
		{
			name:     "one object as the only request",
			with:     map[string]any{"method": "ImportUsers", "body": map[string]any{"name": "a"}},
			wantCode: "OK",
			wantBody: `"imported":1`,
			single:   true,
		},
	}
	for _, p := range protocols {
		for _, tt := range tests {
			t.Run(p.name+"/"+tt.name, func(t *testing.T) {
				with := map[string]any{}
				maps.Copy(with, p.with)
				maps.Copy(with, tt.with)
				_, res, err := streamCall(t, with)
				if err != nil {
					t.Fatalf("Request() error: %v", err)
				}
				if res["status_code"] != tt.wantCode {
					t.Errorf("status_code = %v (%v), want %q", res["status_code"], res["status_message"], tt.wantCode)
				}
				if tt.wantIDs != nil && !reflect.DeepEqual(messageIDs(res), tt.wantIDs) {
					t.Errorf("messages = %v, want ids %v", res["messages"], tt.wantIDs)
				}
				if tt.wantCode == "DEADLINE_EXCEEDED" && len(messageIDs(res)) == 0 {
					t.Error("messages = none, want those before the timeout")
				}
				if tt.wantComplete != nil && res["complete"] != tt.wantComplete {
					t.Errorf("complete = %v, want %v", res["complete"], tt.wantComplete)
				}
				if tt.single {
					if _, ok := res["messages"]; ok {
						t.Errorf("messages = %v, want none for a single response", res["messages"])
					}
				}
				if body, _ := res["body"].(string); tt.wantBody != "" && !strings.Contains(body, tt.wantBody) {
					t.Errorf("body = %v, want %s in it", res["body"], tt.wantBody)
				}
			})
		}
	}
}

func TestConnectStreamTrailers(t *testing.T) {
	s := startConnectServer(t, "")
	_, res, err := streamCall(t, map[string]any{
		"protocol": "connect", "addr": s.url, "method": "WatchUsers", "body": `{"count": 1}`,
		"proto": protoOf(writeProto(t, userProto(t)), false),
	})
	if err != nil {
		t.Fatalf("Request() error: %v", err)
	}
	md, _ := res["metadata"].(map[string]string)
	if md["x-trace"] != "t1" {
		t.Errorf("metadata = %v, want the trailer of the end message", md)
	}
}

func TestStreamingCallsChecksEachMessage(t *testing.T) {
	grpcAddr := startUserServerWith(t, false)
	s := startConnectServer(t, "")
	server := userProto(t)

	tests := []struct {
		name      string
		proto     string
		method    string
		body      any
		wantField string
		wantSent  bool
	}{
		{
			name:      "a response of the stream the files do not take",
			proto:     strings.Replace(server, "string name = 2;", "int32 name = 2;", 1),
			method:    "WatchUsers",
			body:      `{"count": 2}`,
			wantField: "$[1]",
			wantSent:  true,
		},
		{
			name:      "a request of the stream the files do not take is not sent",
			proto:     server,
			method:    "ImportUsers",
			body:      []any{map[string]any{"name": "a"}, map[string]any{"name": 5}},
			wantField: "$[1]",
			wantSent:  false,
		},
	}
	for _, protocol := range []string{"grpc", "connect"} {
		for _, tt := range tests {
			t.Run(protocol+"/"+tt.name, func(t *testing.T) {
				addr := grpcAddr
				if protocol == "connect" {
					addr = s.url
				}
				before := s.calls.Load()
				_, res, err := streamCall(t, map[string]any{
					"protocol": protocol, "addr": addr, "method": tt.method, "body": tt.body,
					"proto": protoOf(writeProto(t, tt.proto), false),
				})
				if err != nil {
					t.Fatalf("Request() error: %v", err)
				}
				v, _ := res["violations"].([]any)
				found := false
				for _, item := range v {
					m, _ := item.(map[string]any)
					if f, _ := m["field"].(string); strings.HasPrefix(f, tt.wantField) {
						found = true
					}
				}
				if !found {
					t.Errorf("violations = %v, want one of %s", v, tt.wantField)
				}
				if protocol == "connect" && (s.calls.Load() > before) != tt.wantSent {
					t.Errorf("sent = %v, want %v", s.calls.Load() > before, tt.wantSent)
				}
			})
		}
	}
}

func TestStreamingCallsRejected(t *testing.T) {
	grpcAddr := startUserServerWith(t, false)
	s := startConnectServer(t, "")
	files := protoOf(writeProto(t, userProto(t)), false)
	bidi := protoOf(writeProto(t, strings.Replace(userProto(t),
		"rpc ImportUsers(stream User) returns (ImportUsersResponse);",
		"rpc ImportUsers(stream User) returns (stream ImportUsersResponse);", 1)), false)

	tests := []struct {
		name    string
		with    map[string]any
		wantErr string
	}{
		{name: "gRPC to a method that streams both ways", with: map[string]any{"addr": grpcAddr, "method": "ImportUsers", "proto": bidi}, wantErr: "streams both ways"},
		{name: "Connect to a method that streams both ways", with: map[string]any{"protocol": "connect", "addr": s.url, "method": "ImportUsers", "proto": bidi}, wantErr: "streams both ways"},
		{name: "a list to a method that takes one request", with: map[string]any{"addr": grpcAddr, "method": "GetUser", "body": []any{map[string]any{}}, "proto": files}, wantErr: "only a method that streams its requests takes"},
		{name: "max_messages to a method that answers once", with: map[string]any{"addr": grpcAddr, "method": "GetUser", "max_messages": 1, "proto": files}, wantErr: "max_messages is for a method that streams its responses"},
		{name: "a list to Connect without .proto files", with: map[string]any{"protocol": "connect", "addr": s.url, "method": "ImportUsers", "body": []any{map[string]any{}}}, wantErr: "the .proto files of proto have to tell"},
		{name: "max_messages to Connect without .proto files", with: map[string]any{"protocol": "connect", "addr": s.url, "method": "WatchUsers", "max_messages": 1}, wantErr: "max_messages is for a method that streams its responses"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, _, err := streamCall(t, tt.with)
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("Request() error = %v, want %q", err, tt.wantErr)
			}
		})
	}
}

func TestRequestBodies(t *testing.T) {
	tests := []struct {
		body   string
		client bool
		want   []string
		err    bool
	}{
		{body: `{"a": 1}`, want: []string{`{"a": 1}`}},
		{body: "", want: []string{""}},
		{body: `[{"a": 1}, {"a": 2}]`, client: true, want: []string{`{"a": 1}`, `{"a": 2}`}},
		{body: `[]`, client: true, want: []string{}},
		{body: `{"a": 1}`, client: true, want: []string{`{"a": 1}`}},
		{body: `[{"a": 1}]`, err: true},
		{body: `[{"a": 1}`, client: true, err: true},
	}
	for _, tt := range tests {
		got, err := requestBodies(tt.body, tt.client)
		if (err != nil) != tt.err || (!tt.err && !reflect.DeepEqual(got, tt.want)) {
			t.Errorf("requestBodies(%q, %v) = %q, %v, want %q, error %v", tt.body, tt.client, got, err, tt.want, tt.err)
		}
	}
}

func TestAtIndex(t *testing.T) {
	got := atIndex([]any{
		map[string]any{"field": "$.user.name"},
		map[string]any{"message": "no field"},
	}, 2)
	if got[0].(map[string]any)["field"] != "$[2].user.name" || got[1].(map[string]any)["field"] != "$[2]" {
		t.Errorf("atIndex() = %v", got)
	}
}
