package oas

import (
	"maps"
	"strconv"

	yaml "github.com/pb33f/go-yaml"
	"github.com/pb33f/libopenapi/datamodel/high/base"
)

const maxSchemaDepth = 3

// generateExample generates an example value from an OpenAPI schema.
// It follows this priority: example → x-example → enum[0] → default → type-based generation.
func generateExample(schema *base.Schema, depth int) any {
	if schema == nil {
		return nil
	}

	if depth > maxSchemaDepth {
		return nil
	}

	// Check for explicit example
	if schema.Example != nil {
		return nodeToValue(schema.Example)
	}

	// Check for x-example extension
	if ext, ok := schema.Extensions.Get("x-example"); ok {
		return nodeToValue(ext)
	}

	// Check for enum values
	if len(schema.Enum) > 0 {
		return nodeToValue(schema.Enum[0])
	}

	// Check for default value
	if schema.Default != nil {
		return nodeToValue(schema.Default)
	}

	// Handle allOf: merge properties from all schemas
	if len(schema.AllOf) > 0 {
		return generateAllOfExample(schema.AllOf, depth+1)
	}

	// Handle oneOf/anyOf: use first option
	if len(schema.OneOf) > 0 {
		s := schema.OneOf[0].Schema()
		if s != nil {
			return generateExample(s, depth+1)
		}
	}
	if len(schema.AnyOf) > 0 {
		s := schema.AnyOf[0].Schema()
		if s != nil {
			return generateExample(s, depth+1)
		}
	}

	// Type-based generation
	schemaType := schemaTypeName(schema)

	switch schemaType {
	case "string":
		return generateStringExample(schema.Format)
	case "integer":
		return 0
	case "number":
		return 0.0
	case "boolean":
		return true
	case "array":
		return generateArrayExample(schema, depth)
	case "object":
		return generateObjectExample(schema, depth)
	default:
		return nil
	}
}

func schemaTypeName(schema *base.Schema) string {
	if len(schema.Type) > 0 {
		return schema.Type[0]
	}
	// Infer type from properties
	if schema.Properties != nil && schema.Properties.Len() > 0 {
		return "object"
	}
	if schema.Items != nil {
		return "array"
	}
	return ""
}

func generateStringExample(format string) any {
	switch format {
	case "email":
		return "user@example.com"
	case "date-time":
		return "2024-01-01T00:00:00Z"
	case "date":
		return "2024-01-01"
	case "uuid":
		return "550e8400-e29b-41d4-a716-446655440000"
	case "uri", "url":
		return "https://example.com"
	case "ipv4":
		return "192.168.1.1"
	case "ipv6":
		return "::1"
	default:
		return "example"
	}
}

func generateArrayExample(schema *base.Schema, depth int) any {
	if schema.Items == nil || schema.Items.A == nil {
		return []any{}
	}
	itemSchema := schema.Items.A.Schema()
	if itemSchema == nil {
		return []any{}
	}
	item := generateExample(itemSchema, depth+1)
	if item == nil {
		return []any{}
	}
	return []any{item}
}

func generateObjectExample(schema *base.Schema, depth int) any {
	result := make(map[string]any)
	if schema.Properties == nil {
		return result
	}
	for pair := schema.Properties.First(); pair != nil; pair = pair.Next() {
		propName := pair.Key()
		propProxy := pair.Value()
		propSchema := propProxy.Schema()
		if propSchema == nil {
			continue
		}
		val := generateExample(propSchema, depth+1)
		if val != nil {
			result[propName] = val
		}
	}
	return result
}

// nodeToValue converts a yaml.Node to a Go primitive value.
func nodeToValue(node *yaml.Node) any {
	if node == nil {
		return nil
	}
	switch node.Kind {
	case yaml.ScalarNode:
		switch node.Tag {
		case "!!int":
			if v, err := strconv.ParseInt(node.Value, 10, 64); err == nil {
				return int(v)
			}
		case "!!float":
			if v, err := strconv.ParseFloat(node.Value, 64); err == nil {
				return v
			}
		case "!!bool":
			if v, err := strconv.ParseBool(node.Value); err == nil {
				return v
			}
		case "!!null":
			return nil
		}
		return node.Value
	case yaml.SequenceNode:
		var result []any
		for _, child := range node.Content {
			result = append(result, nodeToValue(child))
		}
		return result
	case yaml.MappingNode:
		result := make(map[string]any)
		for i := 0; i+1 < len(node.Content); i += 2 {
			key := node.Content[i].Value
			result[key] = nodeToValue(node.Content[i+1])
		}
		return result
	default:
		return node.Value
	}
}

func generateAllOfExample(allOf []*base.SchemaProxy, depth int) any {
	result := make(map[string]any)
	for _, proxy := range allOf {
		s := proxy.Schema()
		if s == nil {
			continue
		}
		val := generateExample(s, depth)
		if m, ok := val.(map[string]any); ok {
			maps.Copy(result, m)
		}
	}
	if len(result) == 0 {
		return nil
	}
	return result
}
