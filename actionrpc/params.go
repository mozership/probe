package actionrpc

import (
	"time"

	"github.com/mozership/probe/internal/mapping"
)

// DecodeParams fills dest, a pointer to a struct, from with, the parameters
// a step gives the action, as the built-in actions read theirs. A field
// takes the parameter its map tag names, such as `map:"url"`, and one
// tagged `validate:"required"` must be given.
//
// A parameter is read strictly by the field's type: a number field takes a
// number or a decimal string that fits it, and refuses a fraction or a
// value out of its range rather than cutting it down; a bool field takes a
// bool, 0 or 1, or a string such as "true"; a map[string]string field takes
// a string, a number or a bool as its text and nil as an empty string, and
// refuses a list or a map. A nested struct, or a list of them, is filled
// from a map, or a list of maps, the same way. The error names the field.
//
// Example:
//
//	type Req struct {
//	    URL     string            `map:"url" validate:"required"`
//	    Retries int               `map:"retries"`
//	    Headers map[string]string `map:"headers"`
//	}
//
//	req := Req{Headers: map[string]string{}}
//	if err := actionrpc.DecodeParams(with, &req); err != nil {
//	    return nil, err
//	}
func DecodeParams(with map[string]any, dest any) error {
	return mapping.MapToStructByTags(with, dest)
}

// EncodeResult turns v, a struct or a pointer to one, into the map an
// action returns, keyed by the map tags of its fields, the reverse of
// DecodeParams. A nested struct becomes a map, a list of them a list of
// maps, and a []byte a string; any other field is kept as it is.
//
// Example:
//
//	type Res struct {
//	    Code int    `map:"code"`
//	    Body string `map:"body"`
//	}
//
//	res, err := actionrpc.EncodeResult(Res{Code: 200, Body: "ok"})
//	// res: map[string]any{"code": 200, "body": "ok"}
func EncodeResult(v any) (map[string]any, error) {
	return mapping.StructToMapByTags(v)
}

// ParamNames returns the names the map tags of v's fields give, v being a
// struct or a pointer to one: the parameters DecodeParams fills it from, in
// the order of the fields, such as for the params of an action.yml. A
// struct with no tagged field gives an empty list, and anything else nil.
func ParamNames(v any) []string {
	return mapping.FieldTags(v)
}

// ParseTimeout reads a timeout parameter as the built-in actions read
// theirs: a duration string such as "30s", a number of seconds, or a
// numeric string. It refuses anything else, and zero too, rather than
// falling back to a default, and its error says why.
func ParseTimeout(v any) (time.Duration, error) {
	return mapping.ParseTimeout(v)
}
