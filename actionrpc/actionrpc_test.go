package actionrpc

import (
	"bytes"
	"context"
	"errors"
	"maps"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/hashicorp/go-hclog"
	"github.com/mozership/probe/pb"
	spb "google.golang.org/genproto/googleapis/rpc/status"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/structpb"
)

// MockActions implements the Action interface for testing
type MockActions struct {
	RunFunc func(with map[string]any) (map[string]any, error)
}

func (m *MockActions) Run(with map[string]any) (map[string]any, error) {
	if m.RunFunc != nil {
		return m.RunFunc(with)
	}
	return map[string]any{"result": "success"}, nil
}

func TestProtocolConstants(t *testing.T) {
	// Test that the basic types and constants are defined correctly
	if BuiltinCmd == "" {
		t.Error("BuiltinCmd should not be empty")
	}

	expectedBuiltinCmd := "builtin-actions"
	if BuiltinCmd != expectedBuiltinCmd {
		t.Errorf("BuiltinCmd = %q, want %q", BuiltinCmd, expectedBuiltinCmd)
	}

	// Test HandshakeConfig
	if Handshake.ProtocolVersion != 1 {
		t.Errorf("Handshake.ProtocolVersion = %d, want 1", Handshake.ProtocolVersion)
	}

	if Handshake.MagicCookieKey != "probe" {
		t.Errorf("Handshake.MagicCookieKey = %q, want %q", Handshake.MagicCookieKey, "probe")
	}

	if Handshake.MagicCookieValue != "actions" {
		t.Errorf("Handshake.MagicCookieValue = %q, want %q", Handshake.MagicCookieValue, "actions")
	}

	// Test PluginMap
	if len(PluginMap) != 1 {
		t.Errorf("PluginMap length = %d, want 1", len(PluginMap))
	}

	if _, exists := PluginMap["actions"]; !exists {
		t.Error("PluginMap should contain 'actions' key")
	}
}

func TestPlugin_GRPCServer(t *testing.T) {
	plugin := &Plugin{
		Impl: &MockActions{},
	}

	// Create a valid gRPC server for testing
	server := grpc.NewServer()
	defer server.Stop()

	// Test that GRPCServer method exists and returns nil for valid input
	err := plugin.GRPCServer(nil, server)
	if err != nil {
		t.Errorf("GRPCServer() returned error: %v", err)
	}
}

func TestPlugin_GRPCClient(t *testing.T) {
	plugin := &Plugin{}

	// Test that GRPCClient method exists and can be called
	// We can't easily test the actual GRPC client creation without complex setup
	client, err := plugin.GRPCClient(context.Background(), nil, nil)
	if err != nil {
		t.Errorf("GRPCClient() returned error: %v", err)
	}

	// Verify the client is of the expected type
	if _, ok := client.(*Client); !ok {
		t.Errorf("GRPCClient() returned wrong type: %T", client)
	}
}

func TestServer_Run(t *testing.T) {
	tests := []struct {
		name        string
		mockFunc    func(with map[string]any) (map[string]any, error)
		with        map[string]any
		expectError bool
		expectedRes map[string]any
	}{
		{
			name: "successful run",
			mockFunc: func(with map[string]any) (map[string]any, error) {
				return map[string]any{
					"status": "success",
					"action": with["action"],
				}, nil
			},
			with:        map[string]any{"action": "test-action", "param": "value"},
			expectError: false,
			expectedRes: map[string]any{
				"status": "success",
				"action": "test-action",
			},
		},
		{
			name: "error case",
			mockFunc: func(with map[string]any) (map[string]any, error) {
				return nil, errors.New("mock error")
			},
			with:        map[string]any{},
			expectError: true,
			expectedRes: nil,
		},
		{
			name: "empty result",
			mockFunc: func(with map[string]any) (map[string]any, error) {
				return map[string]any{}, nil
			},
			with:        map[string]any{},
			expectError: false,
			expectedRes: map[string]any{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockActions := &MockActions{
				RunFunc: tt.mockFunc,
			}

			server := &Server{
				Impl: mockActions,
			}

			// Convert with to structpb.Struct
			withStruct, err := structpb.NewStruct(tt.with)
			if err != nil {
				t.Fatalf("Failed to convert with to struct: %v", err)
			}

			req := &pb.RunRequest{
				With: withStruct,
			}

			resp, err := server.Run(context.Background(), req)

			if tt.expectError {
				if err == nil {
					t.Error("expected error but got none")
				}
				return
			}

			if err != nil {
				t.Errorf("unexpected error: %v", err)
				return
			}

			if resp == nil {
				t.Fatal("response should not be nil")
				return
			}

			// Convert result back to map for comparison
			resultMap := resp.Result.AsMap()

			// Compare results
			if len(resultMap) != len(tt.expectedRes) {
				t.Errorf("result length = %d, want %d", len(resultMap), len(tt.expectedRes))
			}

			for k, v := range tt.expectedRes {
				if resultMap[k] != v {
					t.Errorf("result[%q] = %v, want %v", k, resultMap[k], v)
				}
			}
		})
	}
}

func TestMockActions_Run(t *testing.T) {
	// Test the mock implementation itself
	t.Run("default behavior", func(t *testing.T) {
		mock := &MockActions{}

		result, err := mock.Run(map[string]any{"key": "value"})

		if err != nil {
			t.Errorf("unexpected error: %v", err)
		}

		expected := map[string]any{"result": "success"}
		if len(result) != len(expected) {
			t.Errorf("result length = %d, want %d", len(result), len(expected))
		}

		if result["result"] != "success" {
			t.Errorf("result[result] = %q, want %q", result["result"], "success")
		}
	})

	t.Run("custom function", func(t *testing.T) {
		mock := &MockActions{
			RunFunc: func(with map[string]any) (map[string]any, error) {
				return map[string]any{
					"custom":    "response",
					"with_size": len(with),
				}, nil
			},
		}

		result, err := mock.Run(map[string]any{"a": 1, "b": 2})

		if err != nil {
			t.Errorf("unexpected error: %v", err)
		}

		if result["custom"] != "response" {
			t.Errorf("result[custom] = %q, want %q", result["custom"], "response")
		}
	})

	t.Run("error case", func(t *testing.T) {
		mock := &MockActions{
			RunFunc: func(with map[string]any) (map[string]any, error) {
				return nil, errors.New("test error")
			},
		}

		result, err := mock.Run(map[string]any{})

		if err == nil {
			t.Error("expected error but got none")
		}

		if result != nil {
			t.Errorf("result should be nil on error, got %v", result)
		}
	})
}

// Test type definitions and interfaces
func TestActionInterface(t *testing.T) {
	// Test that MockActions implements Action interface
	var _ Action = &MockActions{}
}

func TestConvertFloatToInt(t *testing.T) {
	tests := []struct {
		name     string
		input    any
		expected any
	}{
		{"nil value", nil, nil},
		{"float64 integer value", float64(1767851301), int64(1767851301)},
		{"float64 with decimal", float64(0.091026392), float64(0.091026392)},
		{"float64 zero", float64(0), int64(0)},
		{"float64 negative integer", float64(-12345), int64(-12345)},
		{"string unchanged", "test", "test"},
		{"bool unchanged", true, true},
		{"int64 unchanged", int64(12345), int64(12345)},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := convertFloatToInt(tt.input)
			if result != tt.expected {
				t.Errorf("convertFloatToInt(%v) = %v (%T), want %v (%T)",
					tt.input, result, result, tt.expected, tt.expected)
			}
		})
	}
}

func TestConvertFloatToInt_Map(t *testing.T) {
	input := map[string]any{
		"time":  float64(1767851301),
		"value": float64(0.091),
		"name":  "test",
	}

	result := convertFloatToInt(input).(map[string]any)

	if result["time"] != int64(1767851301) {
		t.Errorf("time = %v (%T), want int64(1767851301)", result["time"], result["time"])
	}
	if result["value"] != float64(0.091) {
		t.Errorf("value = %v (%T), want float64(0.091)", result["value"], result["value"])
	}
	if result["name"] != "test" {
		t.Errorf("name = %v, want test", result["name"])
	}
}

func TestConvertFloatToInt_Array(t *testing.T) {
	input := []any{
		map[string]any{
			"name":  "api.response.time",
			"time":  float64(1767851301),
			"value": float64(0.091026392),
		},
	}

	result := convertFloatToInt(input).([]any)
	m := result[0].(map[string]any)

	if m["time"] != int64(1767851301) {
		t.Errorf("time = %v (%T), want int64(1767851301)", m["time"], m["time"])
	}
	if m["value"] != float64(0.091026392) {
		t.Errorf("value = %v (%T), want float64(0.091026392)", m["value"], m["value"])
	}
}

func TestConvertForProtobuf(t *testing.T) {
	type inner struct {
		Code   int    `map:"code"`
		Name   string // no tag: the field name is the key
		hidden string
	}
	at := time.Date(2026, 10, 5, 9, 30, 0, 0, time.UTC)
	n := 7

	tests := []struct {
		name string
		in   any
		want any
	}{
		{name: "nil", in: nil, want: nil},
		{name: "a supported scalar", in: "s", want: "s"},
		{name: "a duration", in: 1500 * time.Millisecond, want: "1.5s"},
		{name: "a time", in: at, want: "2026-10-05T09:30:00Z"},
		{name: "a string map", in: map[string]string{"a": "b"}, want: map[string]any{"a": "b"}},
		{name: "a nested map", in: map[string]any{"rt": time.Second, "deep": map[string]any{"at": at}}, want: map[string]any{"rt": "1s", "deep": map[string]any{"at": "2026-10-05T09:30:00Z"}}},
		{name: "a list", in: []any{time.Second, "x"}, want: []any{"1s", "x"}},
		{name: "a string list", in: []string{"a", "b"}, want: []any{"a", "b"}},
		{name: "another slice", in: []int{1, 2}, want: []any{1, 2}},
		{name: "a nil pointer", in: (*int)(nil), want: nil},
		{name: "a pointer", in: &n, want: 7},
		{name: "a struct", in: inner{Code: 1, Name: "x", hidden: "h"}, want: map[string]any{"code": 1, "Name": "x"}},
		{name: "a pointer to a struct", in: &inner{Code: 2}, want: map[string]any{"code": 2, "Name": ""}},
		{name: "another string-keyed map", in: map[string]int{"a": 1}, want: map[string]any{"a": 1}},
		{name: "a map keyed by numbers", in: map[int]string{1: "a", 20: "b"}, want: map[string]any{"1": "a", "20": "b"}},
		{name: "a map keyed by bools", in: map[bool]int{true: 1}, want: map[string]any{"true": 1}},
		{name: "a map keyed by floats", in: map[float64]string{0.5: "half"}, want: map[string]any{"0.5": "half"}},
		{name: "an any map with distinct keys", in: map[any]any{1: "a", "b": time.Second}, want: map[string]any{"1": "a", "b": "1s"}},
		{name: "a nested map keyed by numbers", in: map[string]any{"codes": map[uint32]string{404: "not found"}}, want: map[string]any{"codes": map[string]any{"404": "not found"}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := convertForProtobuf(tt.in)
			if err != nil {
				t.Fatalf("convertForProtobuf(%#v) error = %v", tt.in, err)
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("convertForProtobuf(%#v) = %#v, want %#v", tt.in, got, tt.want)
			}
		})
	}
}

// A map whose entries cannot all be keyed by a string is an error, so that
// no entry of an action's result is dropped without a word.
func TestConvertForProtobufRejectsUnsendableKeys(t *testing.T) {
	type point struct{ X, Y int }
	tests := []struct {
		name    string
		in      any
		wantErr string
	}{
		{name: "struct keys", in: map[point]string{{1, 2}: "a"}, wantErr: "map key of type actionrpc.point"},
		{name: "keys that print alike", in: map[any]any{1: "number", "1": "string"}, wantErr: `map keys collide as "1"`},
		{name: "deep inside a result", in: map[string]any{"res": []any{map[point]int{{0, 0}: 1}}}, wantErr: "res: [0]: map key"},
		{name: "in a struct field", in: struct {
			Data map[point]int `map:"data"`
		}{map[point]int{{0, 0}: 1}}, wantErr: "data: map key"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := convertForProtobuf(tt.in)
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("convertForProtobuf() error = %v, want one containing %q", err, tt.wantErr)
			}
		})
	}
}

// What convertForProtobuf returns for an action's result has to be
// something a protobuf Struct accepts, which the result is sent as.
func TestConvertForProtobufIsAcceptedByStruct(t *testing.T) {
	result := map[string]any{
		"req":    map[string]string{"url": "http://x"},
		"res":    struct{ Code int }{200},
		"rt":     250 * time.Millisecond,
		"at":     time.Now(),
		"lines":  []string{"a"},
		"status": 0,
	}
	c, err := convertForProtobuf(result)
	if err != nil {
		t.Fatalf("convertForProtobuf() error = %v", err)
	}
	converted, ok := c.(map[string]any)
	if !ok {
		t.Fatalf("convertForProtobuf() returned %T, want a map", c)
	}
	if _, err := structpb.NewStruct(converted); err != nil {
		t.Errorf("structpb.NewStruct() error = %v", err)
	}
}

// A result the runner could not receive whole is an error of the action, not
// a result with entries missing.
func TestServer_RunUnsendableResult(t *testing.T) {
	type point struct{ X, Y int }
	server := &Server{Impl: &MockActions{RunFunc: func(map[string]any) (map[string]any, error) {
		return map[string]any{"res": map[point]int{{1, 2}: 3}}, nil
	}}}

	_, err := server.Run(context.Background(), &pb.RunRequest{})
	if err == nil || !strings.Contains(err.Error(), "cannot send the action's result: res: map key") {
		t.Fatalf("Run() error = %v, want the result refused", err)
	}
}

// statefulAction keeps the number of times it ran in its state, and records
// the step it was told about.
type statefulAction struct {
	MockActions
	keep    bool
	step    Step
	actions map[string]string
}

func (a *statefulAction) RunStep(call Call) (map[string]any, map[string]any, error) {
	a.step = call.Step
	a.actions = call.Actions
	if a.keep {
		return map[string]any{"kept": true}, nil, nil
	}
	n, _ := call.State["n"].(int64)
	return map[string]any{"n": n}, map[string]any{"n": n + 1, "secret": "s3cr3t"}, nil
}

// directClient calls a Server in the same process, as the gRPC client of a
// plugin would.
type directClient struct{ s *Server }

func (c directClient) Run(ctx context.Context, in *pb.RunRequest, _ ...grpc.CallOption) (*pb.RunResponse, error) {
	res, err := c.s.Run(ctx, in)
	if err != nil {
		// An error arrives as the status it was sent in, and nothing more.
		return res, status.FromProto(proto.Clone(status.Convert(err).Proto()).(*spb.Status)).Err()
	}
	return res, nil
}

func (c directClient) Cleanup(ctx context.Context, in *pb.CleanupRequest, _ ...grpc.CallOption) (*pb.CleanupResponse, error) {
	return c.s.Cleanup(ctx, in)
}

func TestClientRunStep(t *testing.T) {
	var logBuf bytes.Buffer
	log := hclog.New(&hclog.LoggerOptions{Output: &logBuf, Level: hclog.Trace})
	c := &Client{client: directClient{&Server{Impl: &statefulAction{}, log: log}}}

	result, state, err := c.RunStep(context.Background(), Call{With: map[string]any{"a": "b"}})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !reflect.DeepEqual(result, map[string]any{"n": int64(0)}) {
		t.Errorf("result = %v", result)
	}
	if !reflect.DeepEqual(state, map[string]any{"n": int64(1), "secret": "s3cr3t"}) {
		t.Errorf("state = %v", state)
	}

	result, state, err = c.RunStep(context.Background(), Call{With: map[string]any{}, State: state})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result["n"] != int64(1) || state["n"] != int64(2) {
		t.Errorf("the state should reach the action, got result %v and state %v", result, state)
	}

	// The state may hold credentials the runner has not learned to hide.
	if strings.Contains(logBuf.String(), "s3cr3t") {
		t.Errorf("the state should not be logged, got %s", logBuf.String())
	}
}

func TestClientRunStepOfAnActionWithoutState(t *testing.T) {
	for _, impl := range []Action{&MockActions{}, &statefulAction{keep: true}} {
		c := &Client{client: directClient{&Server{Impl: impl}}}
		_, state, err := c.RunStep(context.Background(), Call{With: map[string]any{}, State: map[string]any{"n": 1}})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if state != nil {
			t.Errorf("%T should leave no state, got %v", impl, state)
		}
	}
}

// TestServerLogsResultWithCredentialsHidden checks that the result the server
// logs has the credentials in it hidden, since the runner learns them only
// once it has the result.
func TestServerLogsResultWithCredentialsHidden(t *testing.T) {
	var logBuf bytes.Buffer
	log := hclog.New(&hclog.LoggerOptions{Output: &logBuf, Level: hclog.Trace})
	s := &Server{Impl: &MockActions{RunFunc: func(map[string]any) (map[string]any, error) {
		return map[string]any{"res": map[string]any{
			"cookies": map[string]any{"session": "cookie-value"},
			"headers": map[string]any{"Set-Cookie": "session=header-value; Path=/"},
			"body":    `{"session":"cookie-value"}`,
		}}, nil
	}}, log: log}
	if _, err := s.Run(context.Background(), &pb.RunRequest{}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	for _, v := range []string{"cookie-value", "header-value"} {
		if strings.Contains(logBuf.String(), v) {
			t.Errorf("the log should not hold %q, got %s", v, logBuf.String())
		}
	}
}

func TestSendable(t *testing.T) {
	tags := []string{"a"}
	labels := map[string]string{"k": "v"}
	in := map[string]any{"tags": tags, "labels": labels, "n": 1}

	got, err := Sendable(in)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Numbers come back as they do from an action in its own process.
	want := map[string]any{"tags": []any{"a"}, "labels": map[string]any{"k": "v"}, "n": int64(1)}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Sendable() = %#v, want %#v", got, want)
	}
	tags[0] = "changed"
	labels["k"] = "changed"
	if !reflect.DeepEqual(got, want) {
		t.Errorf("the copy should share nothing with the original, got %#v", got)
	}

	if got, err := Sendable(nil); got != nil || err != nil {
		t.Errorf("Sendable(nil) = %v, %v", got, err)
	}
	if _, err := Sendable(map[string]any{"bad": map[struct{}]int{{}: 1}}); err == nil {
		t.Error("a map keyed by structs cannot be sent")
	}
	for name, v := range map[string]any{"a channel": make(chan int), "a function": func() {}, "a complex number": complex(1, 2)} {
		if _, err := Sendable(map[string]any{"bad": v}); err == nil {
			t.Errorf("%s cannot be sent", name)
		}
	}
}

func TestClientRunStepTellsTheStep(t *testing.T) {
	a := &statefulAction{}
	c := &Client{client: directClient{&Server{Impl: a}}}
	step := Step{RunID: "r1", JobID: "login", JobName: "Log in", Index: 2, ID: "auth", Name: "Post", Repeat: 1, Attempt: 3}
	if _, _, err := c.RunStep(context.Background(), Call{With: map[string]any{}, Step: step}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if a.step != step {
		t.Errorf("the action was told %+v, want %+v", a.step, step)
	}
}

func TestClientRunStepTellsTheNamesOfActions(t *testing.T) {
	a := &statefulAction{}
	c := &Client{client: directClient{&Server{Impl: a}}}
	names := map[string]string{"redis": "github.com/mozership/probe-redis@0123456789abcdef0123456789abcdef01234567", "greet": "/work/greet"}
	if _, _, err := c.RunStep(context.Background(), Call{With: map[string]any{}, Actions: names}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !maps.Equal(a.actions, names) {
		t.Errorf("the action was told %v, want %v", a.actions, names)
	}

	if _, _, err := c.RunStep(context.Background(), Call{With: map[string]any{}}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(a.actions) != 0 {
		t.Errorf("the action was told %v, want no names", a.actions)
	}
}

// cleaningAction runs steps of its own, as embedded does, and names the
// actions they ran that are to clean up; it also cleans up itself.
type cleaningAction struct {
	// promised is whether the runner said it cleans up.
	promised bool
	ran      []string
	// result and state are what the steps return.
	result   map[string]any
	state    map[string]any
	err      error
	cleaned  []string
	cleanErr error
}

func (a *cleaningAction) Run(with map[string]any) (map[string]any, error) {
	return map[string]any{}, a.err
}

func (a *cleaningAction) RunStep(call Call) (map[string]any, map[string]any, error) {
	a.promised = call.Cleanups != nil
	call.Cleanups.Add(a.ran...)
	if a.result == nil {
		a.result = map[string]any{}
	}
	return a.result, a.state, a.err
}

func (a *cleaningAction) Cleanup(call CleanupCall) error {
	a.cleaned = append(a.cleaned, call.RunID)
	return a.cleanErr
}

func TestCleanups(t *testing.T) {
	var none *Cleanups
	none.Add("/a")
	if got := none.List(); got != nil {
		t.Errorf("a nil Cleanups lists %v", got)
	}

	c := &Cleanups{}
	if got := c.List(); len(got) != 0 {
		t.Errorf("an empty Cleanups lists %v", got)
	}
	c.Add("/b", "/a")
	c.Add("/a", "/c", "/b")
	want := []string{"/b", "/a", "/c"}
	got := c.List()
	if !reflect.DeepEqual(got, want) {
		t.Errorf("List() = %v, want %v", got, want)
	}
	// The list is the caller's own.
	got[0] = "changed"
	if got := c.List(); !reflect.DeepEqual(got, want) {
		t.Errorf("List() = %v after the list it returned was changed, want %v", got, want)
	}
}

func TestClientRunStepGathersCleanups(t *testing.T) {
	refused := Refuse("not allowed")
	tests := []struct {
		name string
		// cleanups is what the runner gives, nil for one that does not
		// clean up.
		cleanups *Cleanups
		ran      []string
		err      error
		// result and state are what the action returns, and wantErr the
		// error when one of them cannot be sent.
		result  map[string]any
		state   map[string]any
		wantErr string
		want    []string
	}{
		{name: "a runner that does not clean up", ran: []string{"/a"}},
		{name: "nothing to clean up", cleanups: &Cleanups{}},
		{name: "the actions the steps ran", cleanups: &Cleanups{}, ran: []string{"/a", "/b"}, want: []string{"/a", "/b"}},
		{name: "of an action that failed", cleanups: &Cleanups{}, ran: []string{"/a"}, err: errors.New("boom"), want: []string{"/a"}},
		{name: "of an action that was refused", cleanups: &Cleanups{}, ran: []string{"/a"}, err: refused, want: []string{"/a"}},
		{name: "a failure with nothing to clean up", cleanups: &Cleanups{}, err: errors.New("boom")},
		{name: "of an action whose result cannot be sent", cleanups: &Cleanups{}, ran: []string{"/a"}, result: map[string]any{"c": make(chan int)}, wantErr: "result", want: []string{"/a"}},
		{name: "of an action whose state cannot be sent", cleanups: &Cleanups{}, ran: []string{"/a"}, state: map[string]any{"c": make(chan int)}, wantErr: "state", want: []string{"/a"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			action := &cleaningAction{ran: tt.ran, err: tt.err, result: tt.result, state: tt.state}
			c := &Client{client: directClient{&Server{Impl: action}}}

			_, _, err := c.RunStep(context.Background(), Call{With: map[string]any{}, Cleanups: tt.cleanups})
			if (err != nil) != (tt.err != nil || tt.wantErr != "") {
				t.Fatalf("RunStep() error = %v, want %v%s", err, tt.err, tt.wantErr)
			}
			if tt.wantErr != "" && !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("RunStep() error = %v, want one about the %s", err, tt.wantErr)
			}
			// The error is the one the action returned, whatever it carried.
			if tt.err != nil && !strings.Contains(err.Error(), tt.err.Error()) {
				t.Errorf("RunStep() error = %v, want %v", err, tt.err)
			}
			if IsRefused(err) != IsRefused(tt.err) {
				t.Errorf("IsRefused(%v) = %v, want %v", err, IsRefused(err), IsRefused(tt.err))
			}
			if action.promised != (tt.cleanups != nil) {
				t.Errorf("the action was told the runner cleans up: %v, want %v", action.promised, tt.cleanups != nil)
			}
			if got := tt.cleanups.List(); !reflect.DeepEqual(got, tt.want) && (len(got) != 0 || len(tt.want) != 0) {
				t.Errorf("gathered %v, want %v", got, tt.want)
			}
		})
	}
}

// backgroundAction leaves procs running in the background, and then returns
// what it is given to.
type backgroundAction struct {
	procs  []Process
	result map[string]any
	err    error
}

func (a *backgroundAction) Run(with map[string]any) (map[string]any, error) {
	return map[string]any{}, a.err
}

func (a *backgroundAction) RunStep(call Call) (map[string]any, map[string]any, error) {
	call.Processes.Add(a.procs...)
	if a.result == nil {
		a.result = map[string]any{}
	}
	return a.result, nil, a.err
}

// The processes an action leaves running reach the runner, also when the
// action fails after it started them.
func TestClientRunStepGathersProcesses(t *testing.T) {
	procs := []Process{{PID: 10, Log: "/tmp/a.log"}, {PID: 20}}
	tests := []struct {
		name   string
		procs  []Process
		result map[string]any
		err    error
		want   []Process
	}{
		{name: "none"},
		{name: "those left running", procs: procs, want: procs},
		{name: "of an action that failed", procs: procs, err: errors.New("boom"), want: procs},
		{name: "of an action whose result cannot be sent", procs: procs, result: map[string]any{"c": make(chan int)}, want: procs},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			action := &backgroundAction{procs: tt.procs, result: tt.result, err: tt.err}
			c := &Client{client: directClient{&Server{Impl: action}}}

			got := &Processes{}
			_, _, err := c.RunStep(context.Background(), Call{With: map[string]any{}, Processes: got})
			if (err != nil) != (tt.err != nil || tt.result != nil) {
				t.Fatalf("RunStep() error = %v", err)
			}
			if list := got.List(); !reflect.DeepEqual(list, tt.want) && (len(list) != 0 || len(tt.want) != 0) {
				t.Errorf("gathered %v, want %v", list, tt.want)
			}
		})
	}

	// A runner that does not hear of them is not told.
	c := &Client{client: directClient{&Server{Impl: &backgroundAction{procs: procs}}}}
	if _, _, err := c.RunStep(context.Background(), Call{With: map[string]any{}}); err != nil {
		t.Errorf("RunStep() without Processes error = %v", err)
	}
}

func TestProcesses(t *testing.T) {
	var none *Processes
	none.Add(Process{PID: 1})
	if got := none.List(); got != nil {
		t.Errorf("a nil Processes lists %v", got)
	}
	p := &Processes{}
	p.Add(Process{PID: 1, Log: "a"})
	p.Add(Process{PID: 2})
	got := p.List()
	want := []Process{{PID: 1, Log: "a"}, {PID: 2}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("List() = %v, want %v", got, want)
	}
	got[0].PID = 99
	if got := p.List(); !reflect.DeepEqual(got, want) {
		t.Errorf("List() = %v after the list it returned was changed, want %v", got, want)
	}
}

func TestClientCleanup(t *testing.T) {
	action := &cleaningAction{}
	c := &Client{client: directClient{&Server{Impl: action, log: hclog.NewNullLogger()}}}
	if err := c.Cleanup(context.Background(), CleanupCall{RunID: "run1"}); err != nil {
		t.Fatalf("Cleanup() error = %v", err)
	}
	if !reflect.DeepEqual(action.cleaned, []string{"run1"}) {
		t.Errorf("cleaned up after %v, want run1", action.cleaned)
	}

	action.cleanErr = errors.New("still running")
	if err := c.Cleanup(context.Background(), CleanupCall{RunID: "run2"}); err == nil || !strings.Contains(err.Error(), "still running") {
		t.Errorf("Cleanup() error = %v, want the action's", err)
	}
}

// An action.yml can ask for a cleanup the executable does not have.
func TestClientCleanupOfAnActionWithoutOne(t *testing.T) {
	c := &Client{client: directClient{&Server{Impl: &statefulAction{}}}}
	err := c.Cleanup(context.Background(), CleanupCall{RunID: "run1"})
	if status.Code(err) != codes.Unimplemented {
		t.Errorf("Cleanup() error = %v, want Unimplemented", err)
	}
}

// An action that does not come up is given no longer than one that does not
// answer.
func TestCleanupExecutableStopsAnActionThatDoesNotStart(t *testing.T) {
	exe := filepath.Join(t.TempDir(), "hang")
	if err := os.WriteFile(exe, []byte("#!/bin/sh\nexec sleep 60\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()

	start := time.Now()
	err := CleanupExecutable(ctx, exe, nil, CleanupCall{RunID: "run1"}, hclog.NewNullLogger())
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("CleanupExecutable() error = %v, want the deadline exceeded", err)
	}
	if took := time.Since(start); took > 10*time.Second {
		t.Errorf("CleanupExecutable() took %s", took)
	}
}
