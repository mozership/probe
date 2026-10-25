package http

import (
	"fmt"
	"regexp"
	"sort"
	"strings"

	"github.com/mozership/probe/internal/contractcheck"
	"github.com/pb33f/libopenapi/datamodel/high/base"
)

// maxStrictDepth bounds how deep undeclared walks into a body.
const maxStrictDepth = 64

// undeclaredProperty is a property of a body that its schema does not
// declare.
type undeclaredProperty struct {
	field    string   // where it is, such as $.items[0].secret
	name     string   // its name
	declared []string // the names the schema declares there
}

// undeclared returns the properties of data, a decoded JSON body, that
// schema does not declare where it says what an object holds: where the
// schema, with those it is composed of by allOf, oneOf and anyOf, declares
// properties or patternProperties and leaves additionalProperties out.
//
// An object whose schema writes additionalProperties is left to the schema:
// true or a schema allows more than it declares, as a map does, and false is
// checked without strict. An object whose schema declares nothing, such as
// one that says only type: object, holds anything.
func undeclared(schema *base.Schema, data any) []undeclaredProperty {
	var out []undeclaredProperty
	walkUndeclared([]*base.Schema{schema}, data, "$", 0, &out)
	return out
}

func walkUndeclared(schemas []*base.Schema, data any, path string, depth int, out *[]undeclaredProperty) {
	if depth > maxStrictDepth {
		return
	}
	all := composed(schemas)
	if len(all) == 0 {
		return
	}

	switch v := data.(type) {
	case map[string]any:
		walkObject(all, v, path, depth, out)
	case []any:
		var items []*base.Schema
		for _, s := range all {
			if s.Items != nil && s.Items.IsA() {
				if is := schemaOf(s.Items.A); is != nil {
					items = append(items, is)
				}
			}
		}
		for i, item := range v {
			walkUndeclared(items, item, fmt.Sprintf("%s[%d]", path, i), depth+1, out)
		}
	}
}

func walkObject(all []*base.Schema, obj map[string]any, path string, depth int, out *[]undeclaredProperty) {
	open := false
	declared := map[string][]*base.Schema{}
	var patterns []*regexp.Regexp
	var patternSchemas []*base.Schema
	var additional []*base.Schema
	for _, s := range all {
		if s.AdditionalProperties != nil {
			open = true
			if s.AdditionalProperties.IsA() {
				if as := schemaOf(s.AdditionalProperties.A); as != nil {
					additional = append(additional, as)
				}
			}
		}
		if s.Properties != nil {
			for name, p := range s.Properties.FromOldest() {
				declared[name] = append(declared[name], schemaOf(p))
			}
		}
		if s.PatternProperties != nil {
			for pattern, p := range s.PatternProperties.FromOldest() {
				re, err := regexp.Compile(pattern)
				if err != nil {
					continue
				}
				patterns = append(patterns, re)
				patternSchemas = append(patternSchemas, schemaOf(p))
			}
		}
	}
	report := !open && (len(declared) > 0 || len(patterns) > 0)

	names := make([]string, 0, len(obj))
	for name := range obj {
		names = append(names, name)
	}
	sort.Strings(names)

	for _, name := range names {
		field := path + "." + name
		children, isDeclared := declared[name]
		for i, re := range patterns {
			if re.MatchString(name) {
				isDeclared = true
				children = append(children, patternSchemas[i])
			}
		}
		if !isDeclared {
			if report {
				*out = append(*out, undeclaredProperty{field: field, name: name, declared: sortedKeys(declared)})
				continue
			}
			children = additional
		}
		walkUndeclared(children, obj[name], field, depth+1, out)
	}
}

// composed returns schemas with those they are composed of by allOf, oneOf
// and anyOf, at any depth, each once.
func composed(schemas []*base.Schema) []*base.Schema {
	var out []*base.Schema
	seen := map[*base.Schema]bool{}
	var add func(s *base.Schema, depth int)
	add = func(s *base.Schema, depth int) {
		if s == nil || seen[s] || depth > maxStrictDepth {
			return
		}
		seen[s] = true
		out = append(out, s)
		for _, list := range [][]*base.SchemaProxy{s.AllOf, s.OneOf, s.AnyOf} {
			for _, p := range list {
				add(schemaOf(p), depth+1)
			}
		}
	}
	for _, s := range schemas {
		add(s, 0)
	}
	return out
}

func sortedKeys(m map[string][]*base.Schema) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// undeclaredViolation is the violation a step reports for p, found in the
// request or the response as in says.
func undeclaredViolation(in string, p undeclaredProperty) map[string]any {
	reason := "the schema declares properties here only by pattern"
	if len(p.declared) > 0 {
		reason = "the schema declares " + strings.Join(p.declared, ", ")
	}
	return contractcheck.Violation(in, fmt.Sprintf("%s property '%s' is not declared in the schema", in, p.name), reason, p.field)
}
