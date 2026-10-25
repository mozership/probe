// Package mapping converts between untyped action parameter maps and
// tagged Go structs.
package mapping

import (
	"errors"
	"fmt"
	"maps"
	"math"
	"reflect"
	"strconv"
	"strings"
	"time"
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
// strconv.ParseBool reads, such as "true" or "1", or a number equal to 0 or
// 1, which reads as the same string would. Anything else is an error,
// rather than false.
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
	}
	n, ok := number(v)
	if !ok {
		return false, fmt.Errorf("cannot convert %T to bool for field '%s'", v, key)
	}
	switch {
	case n.equals(0):
		return false, nil
	case n.equals(1):
		return true, nil
	}
	return false, fmt.Errorf("cannot convert %v to bool for field '%s': use true or false", v, key)
}

// numeric is a number a parameter holds, kept as the kind it came as, so
// that an int64 or a uint64 is compared and range checked exactly rather
// than through a float64.
type numeric struct {
	kind reflect.Kind // reflect.Int64, reflect.Uint64 or reflect.Float64
	i    int64
	u    uint64
	f    float64
}

// number reads v when it is of a numeric kind.
func number(v any) (numeric, bool) {
	if v == nil {
		return numeric{}, false
	}
	rv := reflect.ValueOf(v)
	switch rv.Kind() {
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return numeric{kind: reflect.Int64, i: rv.Int()}, true
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
		return numeric{kind: reflect.Uint64, u: rv.Uint()}, true
	case reflect.Float32, reflect.Float64:
		return numeric{kind: reflect.Float64, f: rv.Float()}, true
	}
	return numeric{}, false
}

// equals reports whether n is the small whole number m. A negative zero
// equals 0.
func (n numeric) equals(m int64) bool {
	switch n.kind {
	case reflect.Int64:
		return n.i == m
	case reflect.Uint64:
		return m >= 0 && n.u == uint64(m)
	default:
		return n.f == float64(m)
	}
}

// durationType is the type of a time.Duration field, which takes only a
// time.Duration: a number has no unit, and reading it as nanoseconds, as a
// plain conversion does, makes 30 thirty nanoseconds. An action reads a
// duration it takes as text or seconds first, with ParseTimeout.
var durationType = reflect.TypeFor[time.Duration]()

// isDecimalFloat reports whether s is written as a decimal number, with an
// optional sign, digits, a point and an exponent, and nothing else.
func isDecimalFloat(s string) bool {
	s = strings.TrimLeft(s, "+-")
	if s == "" {
		return false
	}
	digits := false
	for _, r := range s {
		switch {
		case r >= '0' && r <= '9':
			digits = true
		case r == '.' || r == 'e' || r == 'E' || r == '+' || r == '-':
		default:
			return false
		}
	}
	return digits
}

// isNumberKind reports whether a field of kind k is read by numberParam.
func isNumberKind(k reflect.Kind) bool {
	switch k {
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64,
		reflect.Float32, reflect.Float64:
		return true
	}
	return false
}

// numberParam reads a parameter for a field of an int, uint or float kind
// of type t: a number of any kind, as JSON and protobuf give a float64, or
// a numeric string. A number that does not fit t is an error, rather than
// cut down or wrapped round as a plain conversion does it: int64(2.5) is 2,
// uint(-1) is the largest uint, and int32(1<<40) is 0. For an int or uint
// that means a number that is not whole, or out of t's range; for a float,
// one that is not finite, or beyond the largest float32 for a float32. A
// string is read in base 10 for every kind: strconv.ParseFloat also reads
// hexadecimal, underscores, "Inf" and "NaN", which an int field never took,
// so a float field refuses them too.
func numberParam(v any, t reflect.Type, key string) (reflect.Value, error) {
	out := reflect.New(t).Elem()
	if t == durationType {
		if d, ok := v.(time.Duration); ok {
			out.SetInt(int64(d))
			return out, nil
		}
		return out, fmt.Errorf("cannot convert %T to %s for field '%s': give a time.Duration", v, t, key)
	}
	bits := t.Bits()
	kind := t.Kind()
	isInt := kind >= reflect.Int && kind <= reflect.Int64
	isUint := kind >= reflect.Uint && kind <= reflect.Uint64

	if s, ok := v.(string); ok {
		var err error
		switch {
		case isInt:
			var i int64
			if i, err = strconv.ParseInt(s, 10, bits); err == nil {
				out.SetInt(i)
			}
		case isUint:
			var u uint64
			if u, err = strconv.ParseUint(s, 10, bits); err == nil {
				out.SetUint(u)
			}
		default:
			var f float64
			if !isDecimalFloat(s) {
				err = strconv.ErrSyntax
			} else if f, err = strconv.ParseFloat(s, bits); err == nil {
				out.SetFloat(f)
			}
		}
		if errors.Is(err, strconv.ErrRange) {
			return out, fmt.Errorf("cannot convert '%s' to %s for field '%s': out of range", s, t, key)
		}
		if err != nil {
			return out, fmt.Errorf("cannot convert '%s' to %s for field '%s'", s, t, key)
		}
		return out, nil
	}

	n, ok := number(v)
	if !ok {
		return out, fmt.Errorf("cannot convert %T to %s for field '%s'", v, t, key)
	}
	outOfRange := fmt.Errorf("cannot convert %v to %s for field '%s': out of range", v, t, key)

	if !isInt && !isUint {
		f := n.f
		switch n.kind {
		case reflect.Int64:
			f = float64(n.i)
		case reflect.Uint64:
			f = float64(n.u)
		}
		if math.IsNaN(f) || math.IsInf(f, 0) {
			return out, fmt.Errorf("cannot convert %v to %s for field '%s': not a finite number", v, t, key)
		}
		if bits == 32 && math.Abs(f) > math.MaxFloat32 {
			return out, outOfRange
		}
		out.SetFloat(f)
		return out, nil
	}

	if n.kind == reflect.Float64 {
		if math.IsNaN(n.f) || math.IsInf(n.f, 0) || n.f != math.Trunc(n.f) {
			return out, fmt.Errorf("cannot convert %v to %s for field '%s': not a whole number", v, t, key)
		}
		// The bounds are powers of two, which a float64 holds exactly, and
		// the upper one is exclusive, as float64 of the largest value
		// rounds up to it.
		limit := math.Ldexp(1, bits)
		if isInt {
			if n.f < -limit/2 || n.f >= limit/2 {
				return out, outOfRange
			}
			out.SetInt(int64(n.f))
		} else {
			if n.f < 0 || n.f >= limit {
				return out, outOfRange
			}
			out.SetUint(uint64(n.f))
		}
		return out, nil
	}

	if isInt {
		lo, hi := int64(-1)<<(bits-1), int64(1)<<(bits-1)-1
		switch {
		case n.kind == reflect.Uint64 && n.u > uint64(hi):
			return out, outOfRange
		case n.kind == reflect.Uint64:
			out.SetInt(int64(n.u))
		case n.i < lo || n.i > hi:
			return out, outOfRange
		default:
			out.SetInt(n.i)
		}
		return out, nil
	}
	hi := uint64(math.MaxUint64) >> (64 - bits)
	switch {
	case n.kind == reflect.Int64 && (n.i < 0 || uint64(n.i) > hi):
		return out, outOfRange
	case n.kind == reflect.Int64:
		out.SetUint(uint64(n.i))
	case n.u > hi:
		return out, outOfRange
	default:
		out.SetUint(n.u)
	}
	return out, nil
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
					// A number goes through numberParam even of the field's own type,
					// so that a NaN or an infinity is refused there.
					if v != nil && reflect.TypeOf(v) == field.Type() && !isNumberKind(field.Kind()) {
						field.Set(reflect.ValueOf(v))
					} else if field.Kind() == reflect.Bool {
						b, err := boolParam(v, mapTag)
						if err != nil {
							return err
						}
						field.SetBool(b)
					} else if isNumberKind(field.Kind()) {
						n, err := numberParam(v, field.Type(), mapTag)
						if err != nil {
							return err
						}
						field.Set(n)
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
