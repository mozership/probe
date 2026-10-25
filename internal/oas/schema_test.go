package oas

import (
	"testing"

	yaml "github.com/pb33f/go-yaml"
	"github.com/pb33f/libopenapi/datamodel/high/base"
	"github.com/pb33f/libopenapi/orderedmap"
	"github.com/stretchr/testify/assert"
)

func newExtensions() *orderedmap.Map[string, *yaml.Node] {
	return orderedmap.New[string, *yaml.Node]()
}

func TestGenerateExample_String(t *testing.T) {
	tests := []struct {
		name     string
		format   string
		expected any
	}{
		{"plain string", "", "example"},
		{"email", "email", "user@example.com"},
		{"date-time", "date-time", "2024-01-01T00:00:00Z"},
		{"date", "date", "2024-01-01"},
		{"uuid", "uuid", "550e8400-e29b-41d4-a716-446655440000"},
		{"uri", "uri", "https://example.com"},
		{"ipv4", "ipv4", "192.168.1.1"},
		{"ipv6", "ipv6", "::1"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			schema := &base.Schema{
				Type:       []string{"string"},
				Format:     tt.format,
				Extensions: newExtensions(),
			}
			result := generateExample(schema, 0)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestGenerateExample_Integer(t *testing.T) {
	schema := &base.Schema{
		Type:       []string{"integer"},
		Extensions: newExtensions(),
	}
	result := generateExample(schema, 0)
	assert.Equal(t, 0, result)
}

func TestGenerateExample_Number(t *testing.T) {
	schema := &base.Schema{
		Type:       []string{"number"},
		Extensions: newExtensions(),
	}
	result := generateExample(schema, 0)
	assert.Equal(t, 0.0, result)
}

func TestGenerateExample_Boolean(t *testing.T) {
	schema := &base.Schema{
		Type:       []string{"boolean"},
		Extensions: newExtensions(),
	}
	result := generateExample(schema, 0)
	assert.Equal(t, true, result)
}

func TestGenerateExample_WithExplicitExample(t *testing.T) {
	schema := &base.Schema{
		Type:       []string{"string"},
		Example:    &yaml.Node{Kind: yaml.ScalarNode, Value: "my-example"},
		Extensions: newExtensions(),
	}
	result := generateExample(schema, 0)
	assert.Equal(t, "my-example", result)
}

func TestGenerateExample_WithEnum(t *testing.T) {
	schema := &base.Schema{
		Type: []string{"string"},
		Enum: []*yaml.Node{
			{Kind: yaml.ScalarNode, Value: "active"},
			{Kind: yaml.ScalarNode, Value: "inactive"},
		},
		Extensions: newExtensions(),
	}
	result := generateExample(schema, 0)
	assert.Equal(t, "active", result)
}

func TestGenerateExample_WithDefault(t *testing.T) {
	schema := &base.Schema{
		Type:       []string{"string"},
		Default:    &yaml.Node{Kind: yaml.ScalarNode, Value: "default-value"},
		Extensions: newExtensions(),
	}
	result := generateExample(schema, 0)
	assert.Equal(t, "default-value", result)
}

func TestGenerateExample_Nil(t *testing.T) {
	result := generateExample(nil, 0)
	assert.Nil(t, result)
}

func TestGenerateExample_MaxDepth(t *testing.T) {
	schema := &base.Schema{
		Type:       []string{"object"},
		Extensions: newExtensions(),
	}
	result := generateExample(schema, maxSchemaDepth+1)
	assert.Nil(t, result)
}

func TestGenerateExample_Object(t *testing.T) {
	props := orderedmap.New[string, *base.SchemaProxy]()
	nameSchema := base.CreateSchemaProxy(&base.Schema{
		Type:       []string{"string"},
		Extensions: newExtensions(),
	})
	props.Set("name", nameSchema)

	ageSchema := base.CreateSchemaProxy(&base.Schema{
		Type:       []string{"integer"},
		Extensions: newExtensions(),
	})
	props.Set("age", ageSchema)

	schema := &base.Schema{
		Type:       []string{"object"},
		Properties: props,
		Extensions: newExtensions(),
	}

	result := generateExample(schema, 0)
	m, ok := result.(map[string]any)
	assert.True(t, ok)
	assert.Equal(t, "example", m["name"])
	assert.Equal(t, 0, m["age"])
}

func TestGenerateExample_OneOfDepthLimit(t *testing.T) {
	// oneOf at max depth should return nil, not recurse infinitely
	schema := &base.Schema{
		OneOf: []*base.SchemaProxy{
			base.CreateSchemaProxy(&base.Schema{
				Type:       []string{"string"},
				Extensions: newExtensions(),
			}),
		},
		Extensions: newExtensions(),
	}
	result := generateExample(schema, maxSchemaDepth)
	assert.Nil(t, result)
}

func TestGenerateExample_AnyOfDepthLimit(t *testing.T) {
	schema := &base.Schema{
		AnyOf: []*base.SchemaProxy{
			base.CreateSchemaProxy(&base.Schema{
				Type:       []string{"string"},
				Extensions: newExtensions(),
			}),
		},
		Extensions: newExtensions(),
	}
	result := generateExample(schema, maxSchemaDepth)
	assert.Nil(t, result)
}

func TestGenerateExample_AllOfDepthLimit(t *testing.T) {
	props := orderedmap.New[string, *base.SchemaProxy]()
	props.Set("name", base.CreateSchemaProxy(&base.Schema{
		Type:       []string{"string"},
		Extensions: newExtensions(),
	}))

	schema := &base.Schema{
		AllOf: []*base.SchemaProxy{
			base.CreateSchemaProxy(&base.Schema{
				Type:       []string{"object"},
				Properties: props,
				Extensions: newExtensions(),
			}),
		},
		Extensions: newExtensions(),
	}
	result := generateExample(schema, maxSchemaDepth)
	assert.Nil(t, result)
}

func TestGenerateExample_Array(t *testing.T) {
	itemSchema := base.CreateSchemaProxy(&base.Schema{
		Type:       []string{"string"},
		Extensions: newExtensions(),
	})

	schema := &base.Schema{
		Type: []string{"array"},
		Items: &base.DynamicValue[*base.SchemaProxy, bool]{
			A: itemSchema,
		},
		Extensions: newExtensions(),
	}

	result := generateExample(schema, 0)
	arr, ok := result.([]any)
	assert.True(t, ok)
	assert.Len(t, arr, 1)
	assert.Equal(t, "example", arr[0])
}
