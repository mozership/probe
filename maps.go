package probe

import (
	"maps"
	"os"
	"strings"
)

// mergeMapsShared returns base with over merged into it, over's value taking
// the place of base's where a key is in both. Where both values are maps,
// they are merged in turn, into a map of their own. Every other value, a
// map only one of them has included, is shared with base or over rather
// than copied, so the result must not be changed in place: unlike
// mergeDefaults, which gives each step a copy of what it is given, it is for
// a map that is only read, such as the vars of a step.
//
// Example:
//
//	base := map[string]any{
//	  "a": 1,
//	  "nested": map[string]any{"x": 1, "y": 2},
//	}
//	over := map[string]any{
//	  "nested": map[string]any{"y": 3, "z": 4},
//	  "c": 5,
//	}
//	result := mergeMapsShared(base, over)
//	// result: map[string]any{
//	//   "a": 1,
//	//   "nested": map[string]any{"x": 1, "y": 3, "z": 4},
//	//   "c": 5,
//	// }
func mergeMapsShared(base, over map[string]any) map[string]any {
	merged := make(map[string]any)

	// Copy all entries from base into the result
	maps.Copy(merged, base)

	// Merge entries from over, overriding base's values if keys conflict
	for key, value := range over {
		if existing, ok := merged[key]; ok {
			// If both values are maps, merge them recursively
			if map1Nested, ok1 := existing.(map[string]any); ok1 {
				if map2Nested, ok2 := value.(map[string]any); ok2 {
					merged[key] = mergeMapsShared(map1Nested, map2Nested)
					continue
				}
			}
		}
		// Otherwise, overwrite the value from over
		merged[key] = value
	}

	return merged
}

// envMap returns all environment variables as a map[string]string.
// Each environment variable is parsed from "KEY=VALUE" format.
//
// Example:
//
//	env := envMap()
//	// env contains all environment variables like:
//	// {"PATH": "/usr/bin:/bin", "HOME": "/home/user", "USER": "username", ...}
func envMap() map[string]string {
	env := make(map[string]string)
	for _, v := range os.Environ() {
		parts := strings.SplitN(v, "=", 2)
		if len(parts) == 2 {
			env[parts[0]] = parts[1]
		}
	}
	return env
}
