// Package mapping converts between untyped action parameter maps and
// tagged Go structs.
package mapping

import (
	"fmt"
	"maps"
	"math"
	"reflect"
	"strconv"
	"strings"
)

const (
	tagMap        = "map"
	tagValidate   = "validate"
	labelRequired = "required"
)

// ValidationError collects validation messages produced while assigning
// action parameters to a struct.
type ValidationError struct {
	messages []string
}

func (e *ValidationError) Error() string {
	return fmt.Sprintf("validation error:\n%s", strings.Join(e.messages, "\n"))
}

func (e *ValidationError) HasError() bool {
	return len(e.messages) > 0
}

func (e *ValidationError) AddMessage(s string) {
	e.messages = append(e.messages, s)
}

// mergeStringMaps merges two string maps, where values from 'over' override values from 'base'.
// Only string values from 'over' are included; non-string values are ignored.
//
// Example:
//
//	base := map[string]string{"a": "1", "b": "2"}
//	over := map[string]any{"b": "overridden", "c": "3", "d": 123}
//	result := mergeStringMaps(base, over)
//	// result: map[string]string{"a": "1", "b": "overridden", "c": "3"}
//	// Note: "d": 123 is ignored because it's not a string
func mergeStringMaps(base map[string]string, over map[string]any) map[string]string {
	res := make(map[string]string)

	maps.Copy(res, base)

	for k, v := range over {
		if value, ok := v.(string); ok {
			res[k] = value
		}
	}

	return res
}

func fromAnySlice[T any](s []any) ([]T, error) {
	res := make([]T, len(s))
	for i, v := range s {
		val, ok := v.(T)
		if !ok {
			return nil, fmt.Errorf("element %d has type %T, expected %T", i, v, *new(T))
		}
		res[i] = val
	}
	return res, nil
}

// boolParam reads a parameter for a bool field: a bool, a string that
// strconv.ParseBool reads, such as "true" or "1", or the number 0 or 1,
// which reads as the same string would. Anything else is an error, rather
// than false.
func boolParam(v any, key string) (bool, error) {
	switch val := v.(type) {
	case bool:
		return val, nil
	case string:
		b, err := strconv.ParseBool(val)
		if err != nil {
			return false, fmt.Errorf("cannot convert '%s' to bool for field '%s'", val, key)
		}
		return b, nil
	case int, int64, float64:
		switch fmt.Sprint(val) {
		case "0":
			return false, nil
		case "1":
			return true, nil
		}
		return false, fmt.Errorf("cannot convert %v to bool for field '%s': use true or false", val, key)
	default:
		return false, fmt.Errorf("cannot convert %T to bool for field '%s'", v, key)
	}
}

// intParam reads a parameter for an int field: an int, an int64, a float64
// as JSON and protobuf give numbers, or a numeric string. A number that is
// not whole or does not fit an int is an error, rather than cut down to
// one: int64(2.5) is 2, and int64 of a float64 out of range is whatever the
// platform makes of it.
func intParam(v any, key string) (int64, error) {
	switch val := v.(type) {
	case int:
		return int64(val), nil
	case int64:
		if val < math.MinInt || val > math.MaxInt {
			return 0, fmt.Errorf("cannot convert %d to int for field '%s': out of range", val, key)
		}
		return val, nil
	case float64:
		if math.IsNaN(val) || math.IsInf(val, 0) || val != math.Trunc(val) {
			return 0, fmt.Errorf("cannot convert %v to int for field '%s': not a whole number", val, key)
		}
		// float64(math.MaxInt) rounds up to a power of two, which is out of
		// range, so the upper bound is exclusive.
		if val < math.MinInt || val >= -float64(math.MinInt) {
			return 0, fmt.Errorf("cannot convert %v to int for field '%s': out of range", val, key)
		}
		return int64(val), nil
	case string:
		n, err := strconv.ParseInt(val, 10, strconv.IntSize)
		if err != nil {
			return 0, fmt.Errorf("cannot convert '%s' to int for field '%s'", val, key)
		}
		return n, nil
	default:
		return 0, fmt.Errorf("cannot convert %T to int for field '%s'", v, key)
	}
}

// MapToStructByTags converts a map[string]any to a struct using struct tags.
// Fields are mapped using the "map" tag, and validation is performed using the "validate" tag.
// Supports nested structs, []byte fields, and map[string]string fields.
//
// Example:
//
//	type User struct {
//	  Name     string            `map:"name" validate:"required"`
//	  Age      int               `map:"age"`
//	  Metadata map[string]string `map:"metadata"`
//	}
//
//	params := map[string]any{
//	  "name": "John",
//	  "age": 30,
//	  "metadata": map[string]any{"role": "admin", "dept": "IT"},
//	}
//
//	var user User
//	err := MapToStructByTags(params, &user)
//	// user.Name = "John", user.Age = 30, user.Metadata = {"role": "admin", "dept": "IT"}
func MapToStructByTags(params map[string]any, dest any) error {

	val := reflect.ValueOf(dest).Elem()
	typ := val.Type()

	for i := 0; i < val.NumField(); i++ {
		field := val.Field(i)
		fieldType := typ.Field(i)

		// get the map tag
		mapTag := fieldType.Tag.Get(tagMap)
		if mapTag == "" {
			continue
		}

		// get the validate tag
		validateTag := fieldType.Tag.Get(tagValidate)

		// when nested struct
		if field.Kind() == reflect.Struct {
			nestedParams, ok := params[mapTag].(map[string]any)
			if !ok && validateTag == labelRequired {
				return fmt.Errorf("required field '%s' is missing or not a map[string]any", mapTag)
			} else if ok {
				// recursively assigning a map
				err := MapToStructByTags(nestedParams, field.Addr().Interface())
				if err != nil {
					return err
				}
			}

			// when the field is a map[string]string
		} else if field.Type() == reflect.TypeFor[map[string]string]() {
			v, ok := params[mapTag].(map[string]any)
			if !ok && validateTag == labelRequired {
				return fmt.Errorf("expected map[string]any for field '%s'", mapTag)
			} else {
				existingMap := field.Interface().(map[string]string)
				mergedMap := mergeStringMaps(existingMap, v)
				field.Set(reflect.ValueOf(mergedMap))
			}

			// when the field is []byte
		} else if field.Type() == reflect.TypeFor[[]byte]() {
			v, ok := params[mapTag].(string)
			if !ok && validateTag == labelRequired {
				return fmt.Errorf("expected string for field '%s' to convert to []byte", mapTag)
			} else {
				field.Set(reflect.ValueOf([]byte(v)))
			}

			// when the field is []???(slice)
		} else if field.Kind() == reflect.Slice {
			v, ok := params[mapTag].([]string)
			if !ok && validateTag == labelRequired {
				return fmt.Errorf("required field '%s' is missing or not a []string", mapTag)
			} else if fieldType.Type.Elem().Kind() == reflect.String {
				if ok {
					// []string ===> []string
					field.Set(reflect.ValueOf(v))

					// []any ===> []string
				} else if params[mapTag] != nil && reflect.TypeOf(params[mapTag]).String() == "[]interface {}" {
					vv, okk := params[mapTag].([]any)
					if !okk && validateTag == labelRequired {
						return fmt.Errorf("required field '%s' is missing or not a []string", mapTag)
					} else {
						stSlice, err := fromAnySlice[string](vv)
						if err != nil {
							return err
						}
						field.Set(reflect.ValueOf(stSlice))
					}
				}

			} else if fieldType.Type.Elem().Kind() == reflect.Struct || fieldType.Type.Elem().Kind() == reflect.Pointer {
				sliceParams, ok := params[mapTag].([]any)
				if !ok && validateTag == labelRequired {
					return fmt.Errorf("required field '%s' is missing or not a map[string]any", mapTag)
				} else if ok {
					elemType := field.Type().Elem()
					for _, prms := range sliceParams {
						nestedParams, okk := prms.(map[string]any)
						if okk {
							var p reflect.Value
							if elemType.Kind() == reflect.Pointer {
								// []*Struct: create new struct and get pointer
								structType := elemType.Elem()
								p = reflect.New(structType)
							} else {
								// []Struct: create new struct
								p = reflect.New(elemType)
							}

							err := MapToStructByTags(nestedParams, p.Interface())
							if err != nil {
								return err
							}

							if elemType.Kind() == reflect.Pointer {
								// []any{map[string]any{}} ===> []*Struct
								field.Set(reflect.Append(field, p))
							} else {
								// []any{map[string]any{}} ===> []Struct
								field.Set(reflect.Append(field, p.Elem()))
							}
						}
					}
				}
			}

		} else {
			// get the value corresponding to the key from the map
			if v, ok := params[mapTag]; ok {
				// set a value for a field
				if field.CanSet() {
					if field.Kind() == reflect.Bool {
						b, err := boolParam(v, mapTag)
						if err != nil {
							return err
						}
						field.SetBool(b)
					} else if field.Kind() == reflect.Int {
						n, err := intParam(v, mapTag)
						if err != nil {
							return err
						}
						field.SetInt(n)
					} else {
						// Type-safe assignment based on field type
						fieldType := field.Type()
						valueType := reflect.TypeOf(v)

						if valueType == fieldType {
							// Direct assignment if types match
							field.Set(reflect.ValueOf(v))
						} else if fieldType.Kind() == reflect.String {
							// Convert to string if field expects string
							if v != nil {
								field.SetString(fmt.Sprintf("%v", v))
							}
						} else if fieldType.Kind() == reflect.Map && valueType.Kind() == reflect.Map {
							// Handle map type conversion
							if fieldType.Key().Kind() == reflect.String && fieldType.Elem().Kind() == reflect.String {
								// Convert map[string]interface{} to map[string]string
								if mapVal, ok := v.(map[string]any); ok {
									newMap := make(map[string]string)
									for k, val := range mapVal {
										newMap[k] = fmt.Sprintf("%v", val)
									}
									field.Set(reflect.ValueOf(newMap))
								} else {
									field.Set(reflect.ValueOf(v))
								}
							} else {
								field.Set(reflect.ValueOf(v))
							}
						} else if valueType.ConvertibleTo(fieldType) {
							// Use Go's type conversion if possible
							field.Set(reflect.ValueOf(v).Convert(fieldType))
						} else {
							return fmt.Errorf("cannot assign %T to field '%s' of type %s", v, mapTag, fieldType)
						}
					}
				}

				// error when required field is missing
			} else if validateTag == "required" {
				return fmt.Errorf("required field '%s' is missing", mapTag)
			}
		}
	}

	return nil
}

// FieldTags returns the names the "map" tags of v's fields give, v being a
// struct or a pointer to one: the keys MapToStructByTags fills it from, in
// the order of the fields. A tag is taken as it is written, as
// MapToStructByTags takes it, and a field without one has no key. A struct
// with no tagged field gives an empty list, and anything else nil.
func FieldTags(v any) []string {
	t := reflect.TypeOf(v)
	for t != nil && t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	if t == nil || t.Kind() != reflect.Struct {
		return nil
	}
	names := []string{}
	for f := range t.Fields() {
		if name := f.Tag.Get(tagMap); name != "" && f.IsExported() {
			names = append(names, name)
		}
	}
	return names
}

// StructToMapByTags converts a struct to a map[string]any using struct tags.
// Fields are mapped using the "map" tag. Supports nested structs, []byte fields, and map[string]string fields.
// This is the inverse operation of MapToStructByTags.
//
// Example:
//
//	type User struct {
//	  Name     string            `map:"name"`
//	  Age      int               `map:"age"`
//	  Metadata map[string]string `map:"metadata"`
//	}
//
//	user := User{
//	  Name: "John",
//	  Age: 30,
//	  Metadata: map[string]string{"role": "admin", "dept": "IT"},
//	}
//
//	result, err := StructToMapByTags(user)
//	// result: map[string]any{
//	//   "name": "John",
//	//   "age": 30,
//	//   "metadata": map[string]string{"role": "admin", "dept": "IT"},
//	// }
func StructToMapByTags(src any) (map[string]any, error) {
	result := make(map[string]any)

	val := reflect.ValueOf(src)
	typ := reflect.TypeOf(src)

	// for pointers, access the actual value
	if val.Kind() == reflect.Pointer {
		val = val.Elem()
		typ = typ.Elem()
	}

	for i := 0; i < val.NumField(); i++ {
		field := val.Field(i)
		fieldType := typ.Field(i)

		// get the map tag
		mapTag := fieldType.Tag.Get(tagMap)
		if mapTag == "" {
			continue
		}

		// when nested struct
		if field.Kind() == reflect.Struct {
			nestedMap, err := StructToMapByTags(field.Interface())
			if err != nil {
				return nil, err
			}
			result[mapTag] = nestedMap

			// when the field is []byte
		} else if field.Type() == reflect.TypeFor[[]byte]() {
			if b, ok := field.Interface().([]byte); ok {
				result[mapTag] = string(b)
			}

		} else if field.Type() == reflect.TypeFor[map[string]string]() {
			// when the field is a map[string]string
			result[mapTag] = field.Interface()

			// when the field is []???(slice)
		} else if field.Kind() == reflect.Slice {
			if fieldType.Type.Elem().Kind() == reflect.Struct {
				// []struct ===> []any{map[string]any{}}
				sliceValue := field
				var anySlice []any
				for i := 0; i < sliceValue.Len(); i++ {
					structElem := sliceValue.Index(i)
					structMap, err := StructToMapByTags(structElem.Interface())
					if err != nil {
						return nil, err
					}
					anySlice = append(anySlice, structMap)
				}
				result[mapTag] = anySlice
			} else {
				// other slice types (string, etc.)
				result[mapTag] = field.Interface()
			}

		} else {
			// when the normal field
			result[mapTag] = field.Interface()
		}
	}

	return result, nil
}

// AssignStruct assigns values from an action's parameter map to a struct using struct tags.
// Supports string and int fields with validation. Used for legacy action parameter assignment.
//
// Example:
//
//	type Config struct {
//	  Name    string `map:"name" validate:"required"`
//	  Timeout int    `map:"timeout"`
//	}
//
//	params := map[string]any{"name": "test", "timeout": "30"}
//	var config Config
//	err := AssignStruct(params, &config)
//	// config.Name = "test", config.Timeout = 30
func AssignStruct(pa map[string]any, st any) error {
	v := reflect.ValueOf(st).Elem()
	t := v.Type()
	e := &ValidationError{}

	for i := 0; i < t.NumField(); i++ {
		field := t.Field(i)
		fType := field.Type
		mapKey := field.Tag.Get("map")
		va := field.Tag.Get("validate")
		required := strings.Contains(va, "required")

		if mapKey == "" {
			continue
		}

		value, ok := pa[mapKey]
		if ok {
			switch fType.String() {
			case "string":
				if strValue, ok := value.(string); ok {
					v.Field(i).SetString(strValue)
				} else {
					v.Field(i).SetString(fmt.Sprintf("%v", value))
				}
			case "int":
				var strValue string
				if str, ok := value.(string); ok {
					strValue = str
				} else {
					strValue = fmt.Sprintf("%v", value)
				}
				intValue, err := strconv.Atoi(strValue)
				if err != nil {
					e.AddMessage(fmt.Sprintf("params '%s' can't convert to int: %s", mapKey, err))
				} else {
					v.Field(i).SetInt(int64(intValue))
				}
			default:
				e.AddMessage(fmt.Sprintf("params '%s' not found", mapKey))
			}
		}

		// A required field has to be given, as MapToStructByTags requires; a
		// string also has to be non-empty. Value.String is "" only for an
		// empty string, never for an int, so it cannot tell a missing int.
		if required && (!ok || (fType.Kind() == reflect.String && v.Field(i).String() == "")) {
			e.AddMessage(fmt.Sprintf("params '%s' is required", mapKey))
		}
	}

	if e.HasError() {
		return e
	}

	return nil
}
