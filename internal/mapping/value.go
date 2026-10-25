package mapping

import (
	"fmt"
	"maps"
	"slices"
	"strings"
	"time"

	"github.com/mozership/probe/internal/duration"
	"github.com/mozership/probe/internal/scalar"
)

// StringMap returns m with each value written as text: a string as it is,
// a bool or a number as scalar.String writes it, so a header such as
// X-Retry: 3 is passed on as "3", and nil, as YAML reads X-Empty: with
// nothing after it, as an empty string. Any other value, such as a list or
// a map, has no text to stand for it, and is an error that names its key.
// m itself is not modified.
//
// Example:
//
//	StringMap(map[string]any{"a": "x", "b": 1024, "c": 0.5, "d": true, "e": nil})
//	// map[string]string{"a": "x", "b": "1024", "c": "0.5", "d": "true", "e": ""}
func StringMap(m map[string]any) (map[string]string, error) {
	out := make(map[string]string, len(m))
	// The keys in order, so that of two bad values the same one is told.
	for _, k := range slices.Sorted(maps.Keys(m)) {
		switch v := m[k].(type) {
		case nil:
			out[k] = ""
		case string, bool,
			int, int8, int16, int32, int64,
			uint, uint8, uint16, uint32, uint64,
			float32, float64:
			out[k], _ = scalar.String(v)
		case []any:
			return nil, fmt.Errorf("cannot convert a list to a string for '%s'", k)
		case map[string]any:
			return nil, fmt.Errorf("cannot convert a map to a string for '%s'", k)
		default:
			return nil, fmt.Errorf("cannot convert %T to a string for '%s'", v, k)
		}
	}
	return out, nil
}

// envPrefix marks a flat key that names one environment variable, as in
// env__NODE_ENV. It is accepted alongside a nested env map.
const envPrefix = "env__"

// EnvToStringValue returns a copy of data whose env entry is a map of
// strings, ready for a map[string]string field. Flat env__NAME keys are folded
// into it and removed, and every value is turned into a string, so that a
// number such as PORT: 8080 is passed on instead of dropped. When a name is
// given both ways, the nested env map wins. data itself is not modified.
//
// Example:
//
//	data := map[string]any{
//	  "cmd": "run",
//	  "env": map[string]any{"PORT": 8080},
//	  "env__MODE": "test",
//	}
//
//	result := EnvToStringValue(data)
//	// result = map[string]any{
//	//   "cmd": "run",
//	//   "env": map[string]any{"PORT": "8080", "MODE": "test"},
//	// }
func EnvToStringValue(data map[string]any) map[string]any {
	out := make(map[string]any, len(data))
	env := make(map[string]any)

	for k, v := range data {
		name, ok := strings.CutPrefix(k, envPrefix)
		if !ok || name == "" {
			out[k] = v
			continue
		}
		env[name] = envString(v)
	}

	if nested, ok := data["env"].(map[string]any); ok {
		for name, v := range nested {
			env[name] = envString(v)
		}
	} else if nested, ok := data["env"].(map[string]string); ok {
		for name, v := range nested {
			env[name] = v
		}
	}

	if len(env) > 0 {
		out["env"] = env
	}
	return out
}

func envString(v any) string {
	if v == nil {
		return ""
	}
	if s, ok := scalar.String(v); ok {
		return s
	}
	return fmt.Sprintf("%v", v)
}

// TitleCase converts a string to title case using a specified separator character.
// Each part separated by the character has its first letter capitalized.
//
// Example:
//
//	TitleCase("content-type", "-")     // "Content-Type"
//	TitleCase("user_name", "_")        // "User_Name"
//	TitleCase("hello-world-test", "-") // "Hello-World-Test"
func TitleCase(st string, char string) string {
	parts := strings.Split(st, char)
	for i, part := range parts {
		if len(part) > 0 {
			parts[i] = strings.ToUpper(part[:1]) + part[1:]
		}
	}
	return strings.Join(parts, char)
}

// ParseTimeout reads an action's timeout parameter as duration.Parse reads a
// duration: a duration string such as "30s", a number of seconds, or a
// numeric string. It refuses anything else, and zero too, rather than
// falling back to a default, so a mistyped timeout is reported instead of
// silently ignored. The error says why, as duration.Parse does.
func ParseTimeout(v any) (time.Duration, error) {
	d, err := duration.Parse(v)
	if err != nil {
		return 0, fmt.Errorf("invalid timeout %v: %w", v, err)
	}
	if d == 0 {
		return 0, fmt.Errorf("invalid timeout %v: a timeout must be above zero", v)
	}
	return d, nil
}
