// Package kind names the kinds of value an action returns, as an expression
// reads them once the runner has received them, so that probe check can tell
// what an expression does with a field before the workflow runs.
package kind

import (
	"reflect"
	"time"
)

// The kinds of value, as they are named where a kind is given as text, as
// probe.CheckOptions takes them. A whole number and one with a fraction are
// both a number: an action sends its numbers as one kind, and the runner
// receives those that are whole as integers. Any other name, the empty one
// included, is of a value that may be of any kind.
const (
	Number = "number"
	String = "string"
	Bool   = "bool"
	Map    = "map"
	List   = "list"
)

// Of returns the kind of v, a value as the runner receives it from an
// action: a number, a string, a bool, a map keyed by strings or a list. It
// returns false for nil, which is of no kind, and for a value of any other
// type.
func Of(v any) (string, bool) {
	switch v.(type) {
	case int, int8, int16, int32, int64, uint, uint8, uint16, uint32, uint64, float32, float64:
		return Number, true
	case string:
		return String, true
	case bool:
		return Bool, true
	case map[string]any:
		return Map, true
	case []any:
		return List, true
	}
	return "", false
}

// Fields returns the kinds of the fields of v, a struct or a pointer to one,
// by the names their map tags give, as an action that returns the struct
// sends them: a number for a field of a number type, a string for one of
// string, []byte, time.Duration or time.Time, which are sent as text, a bool
// for one of bool, a list for a slice or an array, and a map for a map or a
// struct. A field of an interface type, which may hold anything, is left
// out, as is a field without a tag.
func Fields(v any) map[string]string {
	t := reflect.TypeOf(v)
	for t != nil && t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	if t == nil || t.Kind() != reflect.Struct {
		return nil
	}
	out := map[string]string{}
	for f := range t.Fields() {
		name := f.Tag.Get("map")
		if name == "" || !f.IsExported() {
			continue
		}
		if k, ok := OfType(f.Type); ok {
			out[name] = k
		}
	}
	return out
}

// OfType returns the kind a value of t is sent as, as Fields tells it, and
// false for a type whose values may be of any kind, as an interface's are.
func OfType(t reflect.Type) (string, bool) {
	for t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	switch t {
	case reflect.TypeFor[time.Duration](), reflect.TypeFor[time.Time](), reflect.TypeFor[[]byte]():
		return String, true
	}
	switch t.Kind() {
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64,
		reflect.Float32, reflect.Float64:
		return Number, true
	case reflect.String:
		return String, true
	case reflect.Bool:
		return Bool, true
	case reflect.Slice, reflect.Array:
		return List, true
	case reflect.Map, reflect.Struct:
		return Map, true
	}
	return "", false
}
