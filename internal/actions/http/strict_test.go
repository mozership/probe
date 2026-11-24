package http

import (
	"encoding/json"
	"reflect"
	"testing"

	"github.com/pb33f/libopenapi"
	"github.com/pb33f/libopenapi/datamodel/high/base"
)

// strictSchemas are the schemas undeclared is tested against, by name.
const strictSchemas = `openapi: 3.0.3
info: {title: strict, version: "1"}
paths: {}
components:
  schemas:
    User:
      type: object
      properties:
        id: {type: integer}
        name: {type: string}
        owner:
          type: object
          properties:
            id: {type: integer}
        tags:
          type: array
          items:
            type: object
            properties:
              key: {type: string}
    Open:
      type: object
      additionalProperties: true
      properties:
        id: {type: integer}
    Closed:
      type: object
      additionalProperties: false
      properties:
        id: {type: integer}
    Labels:
      type: object
      properties:
        labels:
          type: object
          additionalProperties:
            type: object
            properties:
              value: {type: string}
    Anything:
      type: object
    Named:
      allOf:
      - {$ref: "#/components/schemas/Base"}
      - type: object
        properties:
          name: {type: string}
    Base:
      type: object
      properties:
        id: {type: integer}
    OpenByAllOf:
      allOf:
      - {$ref: "#/components/schemas/Base"}
      - type: object
        additionalProperties: true
    Pet:
      oneOf:
      - type: object
        properties:
          bark: {type: boolean}
      - type: object
        properties:
          meow: {type: boolean}
    Extensible:
      type: object
      properties:
        id: {type: integer}
      patternProperties:
        "^x-": {type: string}
`

func strictSchema(t *testing.T, name string) *base.Schema {
	t.Helper()
	doc, err := libopenapi.NewDocument([]byte(strictSchemas))
	if err != nil {
		t.Fatal(err)
	}
	m, err := doc.BuildV3Model()
	if err != nil {
		t.Fatal(err)
	}
	p := m.Model.Components.Schemas.GetOrZero(name)
	if p == nil {
		t.Fatalf("no schema %s", name)
	}
	return p.Schema()
}

func TestUndeclared(t *testing.T) {
	tests := []struct {
		name   string
		schema string
		body   string
		want   []undeclaredProperty
	}{
		{name: "declared properties", schema: "User", body: `{"id":1,"name":"a"}`},
		{
			name:   "a property the schema does not declare",
			schema: "User", body: `{"id":1,"password_hash":"x"}`,
			want: []undeclaredProperty{{field: "$.password_hash", name: "password_hash", declared: []string{"id", "name", "owner", "tags"}}},
		},
		{
			name:   "in a nested object",
			schema: "User", body: `{"owner":{"id":1,"email":"a@example.com"}}`,
			want: []undeclaredProperty{{field: "$.owner.email", name: "email", declared: []string{"id"}}},
		},
		{
			name:   "in an item of an array",
			schema: "User", body: `{"tags":[{"key":"a"},{"key":"b","secret":"s"}]}`,
			want: []undeclaredProperty{{field: "$.tags[1].secret", name: "secret", declared: []string{"key"}}},
		},
		{name: "additionalProperties: true allows more", schema: "Open", body: `{"id":1,"extra":true}`},
		{name: "additionalProperties: false is left to the schema", schema: "Closed", body: `{"id":1,"extra":true}`},
		{name: "a map holds any key", schema: "Labels", body: `{"labels":{"a":{"value":"x"},"b":{"value":"y"}}}`},
		{
			name:   "a value of a map is checked against its schema",
			schema: "Labels", body: `{"labels":{"a":{"value":"x","color":"red"}}}`,
			want: []undeclaredProperty{{field: "$.labels.a.color", name: "color", declared: []string{"value"}}},
		},
		{name: "an object that declares nothing holds anything", schema: "Anything", body: `{"a":1,"b":{"c":2}}`},
		{name: "allOf declares what each schema declares", schema: "Named", body: `{"id":1,"name":"a"}`},
		{
			name:   "allOf, with a property none declares",
			schema: "Named", body: `{"id":1,"name":"a","role":"admin"}`,
			want: []undeclaredProperty{{field: "$.role", name: "role", declared: []string{"id", "name"}}},
		},
		{name: "allOf with a schema that allows more", schema: "OpenByAllOf", body: `{"id":1,"extra":true}`},
		{name: "oneOf declares what any branch declares", schema: "Pet", body: `{"bark":true}`},
		{
			name:   "oneOf, with a property no branch declares",
			schema: "Pet", body: `{"bark":true,"fly":true}`,
			want: []undeclaredProperty{{field: "$.fly", name: "fly", declared: []string{"bark", "meow"}}},
		},
		{name: "patternProperties declares what matches", schema: "Extensible", body: `{"id":1,"x-trace":"t"}`},
		{
			name:   "patternProperties, with a name that does not match",
			schema: "Extensible", body: `{"id":1,"trace":"t"}`,
			want: []undeclaredProperty{{field: "$.trace", name: "trace", declared: []string{"id"}}},
		},
		{name: "a body that is not an object", schema: "User", body: `"text"`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var data any
			if err := json.Unmarshal([]byte(tt.body), &data); err != nil {
				t.Fatal(err)
			}
			got := undeclared(strictSchema(t, tt.schema), data)
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("undeclared = %+v, want %+v", got, tt.want)
			}
		})
	}
}

func TestUndeclaredViolation(t *testing.T) {
	tests := []struct {
		p    undeclaredProperty
		want map[string]any
	}{
		{
			p: undeclaredProperty{field: "$.secret", name: "secret", declared: []string{"id", "name"}},
			want: map[string]any{
				"in":      "response",
				"field":   "$.secret",
				"message": "response property 'secret' is not declared in the schema",
				"reason":  "the schema declares id, name",
			},
		},
		{
			p: undeclaredProperty{field: "$.trace", name: "trace"},
			want: map[string]any{
				"in":      "response",
				"field":   "$.trace",
				"message": "response property 'trace' is not declared in the schema",
				"reason":  "the schema declares properties here only by pattern",
			},
		},
	}
	for _, tt := range tests {
		if got := undeclaredViolation("response", tt.p); !reflect.DeepEqual(got, tt.want) {
			t.Errorf("undeclaredViolation = %#v, want %#v", got, tt.want)
		}
	}
}
