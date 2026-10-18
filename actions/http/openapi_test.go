package http

import (
	"io"
	"maps"
	hp "net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/mozership/probe/actionrpc"
	"github.com/pb33f/libopenapi"
)

// writeSpec writes an OpenAPI document of the users API served under /v1 of
// base: GET /users/{id}, which takes a verbose flag in the query, POST /users,
// which takes a name, GET /me, which takes a session cookie, GET /admin,
// which requires an API key, and GET and POST /items, which declare their
// responses by a range of codes and by default.
func writeSpec(t *testing.T, base string) string {
	t.Helper()
	spec := `openapi: 3.0.3
info: {title: users, version: "1"}
servers:
- url: ` + base + `/v1
paths:
  /users/{id}:
    get:
      parameters:
      - {name: id, in: path, required: true, schema: {type: integer}}
      - {name: verbose, in: query, schema: {type: boolean}}
      responses:
        "200":
          description: a user
          content:
            application/json:
              schema:
                type: object
                required: [id, name]
                properties:
                  id: {type: integer}
                  name: {type: string}
                  password: {type: string, writeOnly: true}
  /users:
    post:
      requestBody:
        required: true
        content:
          application/json:
            schema:
              type: object
              required: [name]
              properties:
                id: {type: integer, readOnly: true}
                name: {type: string}
      responses:
        "201":
          description: created
          content:
            application/json:
              schema: {type: object}
        "400":
          description: rejected
          content:
            application/json:
              schema: {type: object}
  /me:
    get:
      parameters:
      - {name: session, in: cookie, required: true, schema: {type: string}}
      responses:
        "200":
          description: the user signed in
          content:
            application/json:
              schema: {type: object}
  /admin:
    get:
      security:
      - apiKey: []
      responses:
        "200":
          description: the admin page
          content:
            application/json:
              schema: {type: object}
  /items:
    get:
      responses:
        "2XX":
          description: an item
          content:
            application/json:
              schema:
                type: object
                properties:
                  id: {type: integer}
    post:
      responses:
        default:
          description: an error
          content:
            application/json:
              schema:
                type: object
                properties:
                  error: {type: string}
components:
  securitySchemes:
    apiKey: {type: apiKey, in: header, name: X-API-Key}
`
	path := filepath.Join(t.TempDir(), "openapi.yml")
	if err := os.WriteFile(path, []byte(spec), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func usersServer(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(hp.HandlerFunc(func(w hp.ResponseWriter, r *hp.Request) {
		if r.URL.Path == "/v1/old" {
			hp.Redirect(w, r, "/v1/users/1", hp.StatusFound)
			return
		}
		code, body := hp.StatusNotFound, `{}`
		switch r.URL.Path {
		case "/v1/users/1":
			code, body = hp.StatusOK, `{"id":1,"name":"probe"}`
		case "/v1/users/2":
			code, body = hp.StatusOK, `{"id":"2"}`
		case "/v1/users/4":
			code, body = hp.StatusOK, `{"id":4,"name":"probe","password_hash":"x"}`
		case "/v1/users/5":
			code, body = hp.StatusOK, `{"id":5,"name":"probe","password":"x"}`
		case "/v1/users/3":
			code, body = hp.StatusInternalServerError, `{"error":"down"}`
		case "/v1/users":
			b, _ := io.ReadAll(r.Body)
			code, body = hp.StatusBadRequest, `{"error":"name is required"}`
			if strings.Contains(string(b), `"name"`) {
				code, body = hp.StatusCreated, `{"id":1}`
			}
		case "/v1/items":
			code, body = hp.StatusOK, `{"id":1,"x":2}`
			if r.Method == hp.MethodPost {
				code, body = hp.StatusInternalServerError, `{"error":"down","trace":"at main.go:1"}`
			}
		case "/v1/me", "/v1/admin":
			code, body = hp.StatusOK, `{}`
		case "/v1/teams":
			code, body = hp.StatusOK, `[]`
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(code)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestRequestStepChecksAgainstOpenAPI(t *testing.T) {
	srv := usersServer(t)
	spec := writeSpec(t, srv.URL)
	json := map[string]any{"content-type": "application/json"}

	type want struct {
		in   string
		text string // matched by substring against the message and reason
	}
	tests := []struct {
		name string
		with map[string]any
		// openapi is merged into the openapi given, which names spec.
		openapi map[string]any
		// want is one entry for each violation; none when the document
		// allows the request and the response.
		want []want
	}{
		{name: "a response the document allows", with: map[string]any{"get": "/users/1"}},
		{name: "a redirect is matched by where it leads", with: map[string]any{"get": "/old"}},
		{name: "a body that breaks the schema", with: map[string]any{"get": "/users/2"}, want: []want{
			{"response", "missing property 'name'"},
			{"response", "got string, want integer"},
		}},
		{name: "a status the operation does not declare", with: map[string]any{"get": "/users/3"}, want: []want{
			{"response", "'500'"},
		}},
		{name: "a path the document does not have is reported once", with: map[string]any{"get": "/teams"}, want: []want{
			{"response", "'/v1/teams'"},
		}},
		{name: "a request the document allows", with: map[string]any{"post": "/users", "headers": json, "body": map[string]any{"name": "probe"}}},
		{name: "a request body that breaks the schema", with: map[string]any{"post": "/users", "headers": json, "body": map[string]any{}}, want: []want{
			{"request", "missing property 'name'"},
		}},
		{name: "a request body left unchecked", with: map[string]any{"post": "/users", "headers": json, "body": map[string]any{}},
			openapi: map[string]any{"request": false}},
		{name: "a query parameter that breaks the schema", with: map[string]any{"get": "/users/1?verbose=maybe"}, want: []want{
			{"request", "verbose"},
		}},
		{name: "a cookie sent from cookies", with: map[string]any{"get": "/me", "cookies": map[string]any{"session": "s"}}},
		{name: "a cookie the operation requires, missing", with: map[string]any{"get": "/me"}, want: []want{
			{"request", "session"},
		}},
		{name: "a property the schema does not declare", with: map[string]any{"get": "/users/4"}},
		{name: "a property the schema does not declare, strict", with: map[string]any{"get": "/users/4"},
			openapi: map[string]any{"strict": true}, want: []want{
				{"response", "'password_hash'"},
			}},
		{name: "a writeOnly property in the response", with: map[string]any{"get": "/users/5"}},
		{name: "a writeOnly property in the response, strict", with: map[string]any{"get": "/users/5"},
			openapi: map[string]any{"strict": true}, want: []want{
				{"response", "'password' at '$.body.password' is writeOnly"},
			}},
		{name: "a request property the schema does not declare, strict",
			with:    map[string]any{"post": "/users", "headers": json, "body": map[string]any{"name": "probe", "role": "admin"}},
			openapi: map[string]any{"strict": true}, want: []want{
				{"request", "'role'"},
			}},
		{name: "a readOnly property in the request, strict",
			with:    map[string]any{"post": "/users", "headers": json, "body": map[string]any{"name": "probe", "id": 1}},
			openapi: map[string]any{"strict": true}, want: []want{
				{"request", "'id' at '$.body.id' is readOnly"},
			}},
		{name: "a query parameter the operation does not declare, strict", with: map[string]any{"get": "/users/1?debug=1"},
			openapi: map[string]any{"strict": true}, want: []want{
				{"request", "'debug'"},
			}},
		{name: "a response declared by a range of codes, strict", with: map[string]any{"get": "/items"},
			openapi: map[string]any{"strict": true}, want: []want{
				{"response", "'x'"},
			}},
		{name: "a response declared by default, strict", with: map[string]any{"post": "/items"},
			openapi: map[string]any{"strict": true}, want: []want{
				{"response", "'trace'"},
			}},
		{name: "headers and cookies the document does not declare are not strict",
			with: map[string]any{
				"get":          "/me",
				"cookies":      map[string]any{"session": "s", "lb": "1"},
				"headers":      map[string]any{"x-request-source": "probe"},
				"trace_header": true,
			},
			openapi: map[string]any{"strict": true}},
		{name: "a credential the operation requires", with: map[string]any{"get": "/admin", "headers": map[string]any{"x-api-key": "k"}}},
		{name: "a credential the operation requires, missing", with: map[string]any{"get": "/admin"}, want: []want{
			{"request", "X-API-Key"},
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			openapi := map[string]any{"spec": spec}
			maps.Copy(openapi, tt.openapi)
			with := map[string]any{"url": srv.URL + "/v1", "openapi": openapi}
			maps.Copy(with, tt.with)
			ret, _, err := RequestStep(actionrpc.Call{With: with})
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			res := ret["res"].(map[string]any)
			got, ok := res["violations"].([]any)
			if !ok {
				t.Fatalf("res.violations = %#v, want a list", res["violations"])
			}
			if len(got) != len(tt.want) {
				t.Fatalf("violations = %#v, want %d", got, len(tt.want))
			}
			for i, w := range tt.want {
				v := got[i].(map[string]any)
				if v["in"] != w.in {
					t.Errorf("violations[%d].in = %v, want %s", i, v["in"], w.in)
				}
				reason, _ := v["reason"].(string)
				if text := v["message"].(string) + " " + reason; !strings.Contains(text, w.text) {
					t.Errorf("violations[%d] = %#v, want it to say %q", i, v, w.text)
				}
			}
			if _, ok := ret["req"].(map[string]any)["openapi"]; ok {
				t.Error("openapi should not be carried into req")
			}
		})
	}
}

func TestRequestStepReportsWhatTheResponseWasMatchedTo(t *testing.T) {
	srv := usersServer(t)
	spec := writeSpec(t, srv.URL)

	tests := []struct {
		name string
		with map[string]any
		want map[string]any // nil when the document has no operation
	}{
		{name: "by status code", with: map[string]any{"get": "/users/1"},
			want: map[string]any{"spec": spec, "operation": "GET /users/{id}", "response": "200"}},
		{name: "after a redirect, by where it leads", with: map[string]any{"get": "/old"},
			want: map[string]any{"spec": spec, "operation": "GET /users/{id}", "response": "200"}},
		{name: "by a range of codes", with: map[string]any{"get": "/items"},
			want: map[string]any{"spec": spec, "operation": "GET /items", "response": "2XX"}},
		{name: "by default", with: map[string]any{"post": "/items"},
			want: map[string]any{"spec": spec, "operation": "POST /items", "response": "default"}},
		{name: "a status the operation does not declare", with: map[string]any{"get": "/users/3"},
			want: map[string]any{"spec": spec, "operation": "GET /users/{id}"}},
		{name: "a path the document does not have", with: map[string]any{"get": "/teams"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			with := map[string]any{"url": srv.URL + "/v1", "openapi": map[string]any{"spec": spec}}
			maps.Copy(with, tt.with)
			ret, _, err := RequestStep(actionrpc.Call{With: with})
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			got, ok := ret["res"].(map[string]any)["contract"]
			if tt.want == nil {
				if ok {
					t.Errorf("res.contract = %#v, want none", got)
				}
				return
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("res.contract = %#v, want %#v", got, tt.want)
			}
		})
	}
}

// The check against the document is told to the runner, as res shows it,
// and none is told for a request made without one.
func TestRequestStepReportsTheContractToTheRunner(t *testing.T) {
	srv := usersServer(t)
	spec := writeSpec(t, srv.URL)

	tests := []struct {
		name string
		with map[string]any
	}{
		{name: "kept", with: map[string]any{"get": "/users/1"}},
		{name: "broken", with: map[string]any{"get": "/users/2"}},
		{name: "by a path the document does not have", with: map[string]any{"get": "/teams"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			with := map[string]any{"url": srv.URL + "/v1", "openapi": map[string]any{"spec": spec}}
			maps.Copy(with, tt.with)
			contract := &actionrpc.Contract{}
			ret, _, err := RequestStep(actionrpc.Call{With: with, Contract: contract})
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			res := ret["res"].(map[string]any)
			vs, _ := res["violations"].([]any)
			matched, _ := res["contract"].(map[string]any)
			if got, want := contract.Check(), actionrpc.ContractCheckOf(vs, matched); got == nil || !reflect.DeepEqual(*got, want) {
				t.Errorf("Check() = %+v, want %+v as res shows it", got, want)
			}
		})
	}

	contract := &actionrpc.Contract{}
	if _, _, err := RequestStep(actionrpc.Call{With: map[string]any{"url": srv.URL + "/v1", "get": "/users/1"}, Contract: contract}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got := contract.Check(); got != nil {
		t.Errorf("Check() = %+v without openapi, want none", got)
	}
}

func TestRequestStepNamesTheFieldThatBreaksTheSchema(t *testing.T) {
	srv := usersServer(t)
	ret, _, err := RequestStep(actionrpc.Call{With: map[string]any{
		"url":     srv.URL + "/v1",
		"get":     "/users/2",
		"openapi": map[string]any{"spec": writeSpec(t, srv.URL)},
	}})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	for _, v := range ret["res"].(map[string]any)["violations"].([]any) {
		if v.(map[string]any)["field"] == "$.id" {
			return
		}
	}
	t.Errorf("violations = %#v, want one for $.id", ret["res"].(map[string]any)["violations"])
}

func TestRequestStepWithoutOpenAPIChecksNothing(t *testing.T) {
	srv := usersServer(t)

	for _, openapi := range []any{nil, false} {
		with := map[string]any{"url": srv.URL + "/v1", "get": "/teams"}
		if openapi != nil {
			with["openapi"] = openapi
		}
		ret, _, err := RequestStep(actionrpc.Call{With: with})
		if err != nil {
			t.Fatalf("with openapi %v: unexpected error: %v", openapi, err)
		}
		if v, ok := ret["res"].(map[string]any)["violations"]; ok {
			t.Errorf("with openapi %v: res.violations = %#v, want none", openapi, v)
		}
	}
}

func TestRequestStepOpenAPIRejected(t *testing.T) {
	var hits atomic.Int32
	srv := httptest.NewServer(hp.HandlerFunc(func(w hp.ResponseWriter, r *hp.Request) {
		hits.Add(1)
	}))
	defer srv.Close()

	broken := filepath.Join(t.TempDir(), "broken.yml")
	if err := os.WriteFile(broken, []byte("openapi: [\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name    string
		openapi any
		wantErr string
	}{
		{name: "true", openapi: true, wantErr: "openapi must be a map with spec, or false"},
		{name: "a path alone", openapi: "openapi.yml", wantErr: "openapi must be a map with spec, or false"},
		{name: "an unknown key", openapi: map[string]any{"spec": broken, "headers": true}, wantErr: "openapi takes spec, request and strict, not headers"},
		{name: "request not a boolean", openapi: map[string]any{"spec": broken, "request": "no"}, wantErr: "openapi.request must be true or false"},
		{name: "strict not a boolean", openapi: map[string]any{"spec": broken, "strict": "yes"}, wantErr: "openapi.strict must be true or false"},
		{name: "no spec", openapi: map[string]any{}, wantErr: "openapi.spec must be the path of an OpenAPI document"},
		{name: "a missing file", openapi: map[string]any{"spec": filepath.Join(t.TempDir(), "none.yml")}, wantErr: "openapi.spec:"},
		{name: "a document that cannot be parsed", openapi: map[string]any{"spec": broken}, wantErr: "openapi.spec: " + broken},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, _, err := RequestStep(actionrpc.Call{With: map[string]any{
				"url":     srv.URL,
				"get":     "/",
				"openapi": tt.openapi,
			}})
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("err = %v, want it to contain %q", err, tt.wantErr)
			}
		})
	}
	// A step whose document cannot be used sends nothing.
	if n := hits.Load(); n != 0 {
		t.Errorf("the server was sent %d requests, want 0", n)
	}
}

func TestRequestStepChecksBodiesBeyondJSON(t *testing.T) {
	srv := httptest.NewServer(hp.HandlerFunc(func(w hp.ResponseWriter, r *hp.Request) {
		bodies := map[string]struct{ contentType, body string }{
			"/text/long":     {"text/plain", "abcd"},
			"/text/short":    {"text/plain", "abc"},
			"/json/null":     {"application/json", "null"},
			"/json/nullable": {"application/json", "null"},
			"/json/empty":    {"application/json", ""},
			"/pdf":           {"application/pdf", "%PDF-1.7"},
		}
		b, ok := bodies[r.URL.Path]
		if !ok {
			w.WriteHeader(hp.StatusNoContent)
			return
		}
		w.Header().Set("Content-Type", b.contentType)
		_, _ = w.Write([]byte(b.body))
	}))
	defer srv.Close()

	object := `{type: object, required: [id], properties: {id: {type: integer}}}`
	spec := `openapi: 3.0.3
info: {title: bodies, version: "1"}
paths:
  /text/long:
    get:
      responses:
        "200": {description: text, content: {text/plain: {schema: {type: string, maxLength: 3}}}}
  /text/short:
    get:
      responses:
        "200": {description: text, content: {text/plain: {schema: {type: string, maxLength: 3}}}}
  /json/null:
    get:
      responses:
        "200": {description: an object, content: {application/json: {schema: ` + object + `}}}
  /json/nullable:
    get:
      responses:
        "200": {description: an object or null, content: {application/json: {schema: {type: object, nullable: true}}}}
  /json/empty:
    get:
      responses:
        "200": {description: an object, content: {application/json: {schema: ` + object + `}}}
  /pdf:
    get:
      responses:
        "200": {description: a PDF, content: {application/pdf: {schema: {type: string, format: binary}}}}
  /none:
    get:
      responses:
        "204": {description: nothing}
`
	path := filepath.Join(t.TempDir(), "openapi.yml")
	if err := os.WriteFile(path, []byte(spec), 0o600); err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		path string
		want string // the violation expected, matched by substring; empty for none
	}{
		{"/text/long", "maxLength: got 4, want 3"},
		{"/text/short", ""},
		{"/json/null", "the schema does not allow null"},
		{"/json/nullable", ""},
		{"/json/empty", "the response declares a schema for application/json"},
		// A body of a type the validator cannot decode is left unchecked.
		{"/pdf", ""},
		{"/none", ""},
	}
	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			ret, _, err := RequestStep(actionrpc.Call{With: map[string]any{
				"url":     srv.URL,
				"get":     tt.path,
				"openapi": map[string]any{"spec": path},
			}})
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			got := ret["res"].(map[string]any)["violations"].([]any)
			if tt.want == "" {
				if len(got) != 0 {
					t.Errorf("violations = %#v, want none", got)
				}
				return
			}
			if len(got) != 1 {
				t.Fatalf("violations = %#v, want 1", got)
			}
			v := got[0].(map[string]any)
			if text := v["message"].(string) + " " + v["reason"].(string); !strings.Contains(text, tt.want) {
				t.Errorf("violation = %#v, want it to say %q", v, tt.want)
			}
		})
	}
}

func TestAllowsNull(t *testing.T) {
	const doc = `openapi: 3.1.0
info: {title: null, version: "1"}
paths: {}
components:
  schemas:
    Object: {type: object}
    Nullable: {type: object, nullable: true}
    TypeNull: {type: [object, "null"]}
    EnumWithNull: {enum: [a, null]}
    EnumWithoutNull: {enum: [a, b]}
    Untyped: {description: anything}
    AllOfObject: {allOf: [{$ref: "#/components/schemas/Object"}]}
    AllOfNullable: {allOf: [{$ref: "#/components/schemas/Object"}], nullable: true}
    OneOfWithNull: {oneOf: [{$ref: "#/components/schemas/Object"}, {type: "null"}]}
    AnyOfObjects: {anyOf: [{$ref: "#/components/schemas/Object"}, {type: string}]}
`
	d, err := libopenapi.NewDocument([]byte(doc))
	if err != nil {
		t.Fatal(err)
	}
	m, err := d.BuildV3Model()
	if err != nil {
		t.Fatal(err)
	}
	tests := map[string]bool{
		"Object":          false,
		"Nullable":        true,
		"TypeNull":        true,
		"EnumWithNull":    true,
		"EnumWithoutNull": false,
		"Untyped":         true,
		"AllOfObject":     false,
		"AllOfNullable":   true,
		"OneOfWithNull":   true,
		"AnyOfObjects":    false,
	}
	for name, want := range tests {
		s := m.Model.Components.Schemas.GetOrZero(name).Schema()
		if got := allowsNull(s, 0); got != want {
			t.Errorf("allowsNull(%s) = %v, want %v", name, got, want)
		}
	}
}
