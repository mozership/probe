package grpc

import (
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/hashicorp/go-hclog"
	"github.com/mozership/probe/actionrpc"
)

func TestListServicesByReflection(t *testing.T) {
	addr := startUserServer(t)
	ret, err := Request(map[string]any{"addr": addr, "list": true})
	if err != nil {
		t.Fatal(err)
	}
	res := ret["res"].(map[string]any)
	// The services of reflection itself are left out.
	if !reflect.DeepEqual(res["services"], []any{"UserService"}) || res["status_code"] != "OK" {
		t.Errorf("res = %v, want UserService alone", res)
	}
	if _, ok := res["methods"]; ok {
		t.Errorf("res.methods = %v in a listing of the services", res["methods"])
	}
	if rt := fmt.Sprint(ret["rt"]); rt == "" || rt == "0s" || rt == "<nil>" {
		t.Errorf("rt = %v, want the time the listing took", ret["rt"])
	}
}

func TestDescribeAServiceByReflection(t *testing.T) {
	addr := startUserServer(t)
	ret, err := Request(map[string]any{"addr": addr, "list": true, "service": "UserService"})
	if err != nil {
		t.Fatal(err)
	}
	res := ret["res"].(map[string]any)
	methods := res["methods"].([]any)
	if len(methods) != 7 {
		t.Fatalf("methods = %v, want the 7 of UserService", methods)
	}
	byName := map[string]map[string]any{}
	for _, m := range methods {
		byName[m.(map[string]any)["name"].(string)] = m.(map[string]any)
	}
	want := map[string]any{
		"name": "GetUser", "service": "UserService", "request": "GetUserRequest", "response": "GetUserResponse",
		"client_streaming": false, "server_streaming": false, "idempotency": "IDEMPOTENCY_UNKNOWN",
	}
	if !reflect.DeepEqual(byName["GetUser"], want) {
		t.Errorf("GetUser = %v, want %v", byName["GetUser"], want)
	}
	if byName["WatchUsers"]["server_streaming"] != true || byName["ImportUsers"]["client_streaming"] != true {
		t.Errorf("the streams are not told: %v, %v", byName["WatchUsers"], byName["ImportUsers"])
	}

	// The messages are described at any depth, the ones a field holds
	// included.
	types := res["types"].(map[string]any)
	for _, name := range []string{"GetUserRequest", "GetUserResponse", "User", "Profile", "ListUsersRequest"} {
		if types[name] == nil {
			t.Errorf("types lacks %s: %v", name, types)
		}
	}
	user := types["User"].(map[string]any)
	if user["kind"] != "message" {
		t.Errorf("User = %v", user)
	}
	var prefs, profile map[string]any
	for _, f := range user["fields"].([]any) {
		switch f.(map[string]any)["name"] {
		case "preferences":
			prefs = f.(map[string]any)
		case "profile":
			profile = f.(map[string]any)
		}
	}
	if prefs["type"] != "repeated string" || prefs["number"] != 5 || profile["type"] != "Profile" {
		t.Errorf("preferences = %v, profile = %v", prefs, profile)
	}
	var filter map[string]any
	for _, f := range types["ListUsersRequest"].(map[string]any)["fields"].([]any) {
		if f.(map[string]any)["name"] == "filter" {
			filter = f.(map[string]any)
		}
	}
	if filter["type"] != "map<string, string>" || filter["json_name"] != "filter" {
		t.Errorf("filter = %v", filter)
	}

	// A method of the service is described alone.
	ret, err = Request(map[string]any{"addr": addr, "list": true, "service": "UserService", "method": "GetUser"})
	if err != nil {
		t.Fatal(err)
	}
	res = ret["res"].(map[string]any)
	if ms := res["methods"].([]any); len(ms) != 1 || res["types"].(map[string]any)["ListUsersRequest"] != nil {
		t.Errorf("res = %v, want GetUser and its messages alone", res)
	}
}

func TestListByProto(t *testing.T) {
	// A server without reflection is listed by the .proto files, and so is
	// none at all, as nothing is sent.
	addr := startUserServerWith(t, false)
	files := protoOf(writeProto(t, userProto(t)), false)
	for _, a := range []string{addr, "127.0.0.1:1"} {
		ret, err := Request(map[string]any{"addr": a, "list": true, "proto": files})
		if err != nil {
			t.Fatal(err)
		}
		if res := ret["res"].(map[string]any); !reflect.DeepEqual(res["services"], []any{"UserService"}) {
			t.Errorf("res = %v", res)
		}
	}
	ret, err := Request(map[string]any{"addr": addr, "list": true, "service": "UserService", "method": "WatchUsers", "proto": files})
	if err != nil {
		t.Fatal(err)
	}
	res := ret["res"].(map[string]any)
	if ms := res["methods"].([]any); len(ms) != 1 || ms[0].(map[string]any)["response"] != "User" {
		t.Errorf("res = %v", res)
	}
	if _, ok := res["violations"]; ok {
		t.Errorf("a listing is checked against the files: %v", res)
	}

	// A Connect server is listed only with the files.
	ret, err = Request(map[string]any{"protocol": "connect", "addr": "http://127.0.0.1:1", "list": true, "proto": files})
	if err != nil || !reflect.DeepEqual(ret["res"].(map[string]any)["services"], []any{"UserService"}) {
		t.Errorf("connect with proto: %v, %v", ret, err)
	}
}

// A field declared optional is told so, in proto2 as in proto3.
func TestDescribeOptionalFields(t *testing.T) {
	for _, src := range []string{
		"syntax = \"proto3\";\nmessage Req { optional string a = 1; string b = 2; oneof c { string d = 3; } }\nservice S { rpc M(Req) returns (Req); }\n",
		"syntax = \"proto2\";\nmessage Req { optional string a = 1; required string b = 2; oneof c { string d = 3; } }\nservice S { rpc M(Req) returns (Req); }\n",
	} {
		ret, err := Request(map[string]any{"addr": "127.0.0.1:1", "list": true, "service": "S", "proto": protoOf(writeProto(t, src), false)})
		if err != nil {
			t.Fatal(err)
		}
		fields := map[string]map[string]any{}
		for _, f := range ret["res"].(map[string]any)["types"].(map[string]any)["Req"].(map[string]any)["fields"].([]any) {
			fields[f.(map[string]any)["name"].(string)] = f.(map[string]any)
		}
		if fields["a"]["optional"] != true || fields["b"]["optional"] != nil || fields["d"]["oneof"] != "c" || fields["a"]["oneof"] != nil {
			t.Errorf("%s: fields = %v", src[:17], fields)
		}
	}
}

// A message that holds itself is described once, and the messages and
// enums a map holds as its values are described too.
func TestDescribeRecursiveAndMapTypes(t *testing.T) {
	src := `syntax = "proto3";
package tree.v1;
enum Color { COLOR_UNSPECIFIED = 0; RED = 1; }
message Leaf { Color color = 1; }
message Node { repeated Node children = 1; map<string, Leaf> leaves = 2; map<string, Color> colors = 3; }
service Trees { rpc Get(Node) returns (Node); }
`
	ret, err := Request(map[string]any{"addr": "127.0.0.1:1", "list": true, "service": "Trees", "proto": protoOf(writeProto(t, src), false)})
	if err != nil {
		t.Fatal(err)
	}
	types := ret["res"].(map[string]any)["types"].(map[string]any)
	if len(types) != 3 {
		t.Errorf("types = %v, want Node, Leaf and Color", types)
	}
	if types["tree.v1.Leaf"] == nil || !reflect.DeepEqual(types["tree.v1.Color"], map[string]any{"kind": "enum", "values": []any{"COLOR_UNSPECIFIED", "RED"}}) {
		t.Errorf("types = %v", types)
	}
	fields := map[string]any{}
	for _, f := range types["tree.v1.Node"].(map[string]any)["fields"].([]any) {
		fields[f.(map[string]any)["name"].(string)] = f.(map[string]any)["type"]
	}
	want := map[string]any{"children": "repeated tree.v1.Node", "leaves": "map<string, tree.v1.Leaf>", "colors": "map<string, tree.v1.Color>"}
	if !reflect.DeepEqual(fields, want) {
		t.Errorf("fields of Node = %v, want %v", fields, want)
	}
}

func TestListRefused(t *testing.T) {
	withRefl := startUserServer(t)
	without := startUserServerWith(t, false)
	files := protoOf(writeProto(t, userProto(t)), false)
	for _, tt := range []struct {
		name string
		with map[string]any
		want string
	}{
		{"no reflection", map[string]any{"addr": without, "list": true}, "the server serves no reflection"},
		{"no reflection, a service", map[string]any{"addr": without, "list": true, "service": "UserService"}, "the server serves no reflection"},
		{"a service not listed", map[string]any{"addr": withRefl, "list": true, "service": "NoService"}, "does not list the service"},
		{"a method not there", map[string]any{"addr": withRefl, "list": true, "service": "UserService", "method": "Nope"}, "method Nope not found"},
		{"a service the files lack", map[string]any{"addr": without, "list": true, "service": "NoService", "proto": files}, "the .proto files declare no service NoService"},
		{"a method without a service", map[string]any{"addr": withRefl, "list": true, "method": "GetUser"}, "method needs service"},
		{"a body", map[string]any{"addr": withRefl, "list": true, "service": "UserService", "body": `{}`}, "list takes no body"},
		{"connect without proto", map[string]any{"protocol": "connect", "addr": "http://127.0.0.1:1", "list": true}, "listed only with proto"},
		{"an unknown protocol", map[string]any{"protocol": "grpc-web", "addr": withRefl, "list": true, "proto": files}, "unknown protocol"},
	} {
		if _, err := Request(tt.with); err == nil || !strings.Contains(err.Error(), tt.want) {
			t.Errorf("%s: err = %v, want %q", tt.name, err, tt.want)
		}
	}
}

// A listing reads only, so it is made under --read-only, and keeps to
// --allow-host as a call does.
func TestListUnderAGuard(t *testing.T) {
	addr := startUserServer(t)
	a := &Action{log: hclog.NewNullLogger()}
	ret, _, err := a.RunStep(actionrpc.Call{
		With:  map[string]any{"addr": addr, "list": true, "service": "UserService"},
		Guard: actionrpc.Guard{ReadOnly: true, AllowHosts: []string{"127.0.0.1"}},
	})
	if err != nil || len(ret["res"].(map[string]any)["methods"].([]any)) != 7 {
		t.Errorf("read-only: %v, %v", ret, err)
	}
	_, _, err = a.RunStep(actionrpc.Call{
		With:  map[string]any{"addr": addr, "list": true},
		Guard: actionrpc.Guard{AllowHosts: []string{"other.test"}},
	})
	if !actionrpc.IsRefused(err) {
		t.Errorf("err = %v, want it refused", err)
	}
}
