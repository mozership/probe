package mapping

import (
	"fmt"
	"reflect"
	"strconv"
	"strings"
	"time"

	"github.com/mozership/probe/internal/duration"
)

// HeaderToStringValue converts header values to strings for HTTP processing.
// This function ensures all header values are strings, converting numbers and other types as needed.
//
// Example:
//
//	data := map[string]any{
//	  "headers": map[string]any{
//	    "Content-Length": 1024,
//	    "X-Rate-Limit": 100.5,
//	    "Authorization": "Bearer token",
//	  },
//	}
//
//	result := HeaderToStringValue(data)
//	// result["headers"] = map[string]any{
//	//   "Content-Length": "1024",
//	//   "X-Rate-Limit": "100.5",
//	//   "Authorization": "Bearer token",
//	// }
func HeaderToStringValue(data map[string]any) map[string]any {
	v, exists := data["headers"]
	if !exists {
		return data
	}

	newHeaders := make(map[string]any)
	if headers, ok := v.(map[string]any); ok {
		for key, value := range headers {
			switch v := value.(type) {
			case string:
				newHeaders[key] = v
			case int:
				newHeaders[key] = strconv.Itoa(v)
			case float64:
				newHeaders[key] = strconv.FormatFloat(v, 'f', -1, 64)
			default:
				newHeaders[key] = fmt.Sprintf("%v", v)
			}
		}
	}

	if len(newHeaders) > 0 {
		data["headers"] = newHeaders
	}

	return data
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
	if s, ok := AnyToString(v); ok {
		return s
	}
	return fmt.Sprintf("%v", v)
}

// AnyToString attempts to convert any type to a string.
// Returns the string representation and a boolean indicating success.
//
// Example:
//
//	str, ok := AnyToString(42)        // "42", true
//	str, ok := AnyToString(3.14)      // "3.14", true
//	str, ok := AnyToString("hello")   // "hello", true
//	str, ok := AnyToString(nil)       // "nil", true
//	str, ok := AnyToString([]int{1})  // "", false
func AnyToString(value any) (string, bool) {
	if value == nil {
		return "nil", true
	}

	switch v := value.(type) {
	case string:
		return v, true
	case bool:
		return strconv.FormatBool(v), true
	case int, int8, int16, int32, int64:
		return strconv.FormatInt(reflect.ValueOf(v).Int(), 10), true
	case uint, uint8, uint16, uint32, uint64:
		return strconv.FormatUint(reflect.ValueOf(v).Uint(), 10), true
	case float32, float64:
		return strconv.FormatFloat(reflect.ValueOf(v).Float(), 'f', -1, 64), true
	case []byte:
		return string(v), true
	case fmt.Stringer:
		return v.String(), true
	default:
		if reflect.ValueOf(value).IsZero() {
			return "nil", true
		}
		return "", false
	}
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
