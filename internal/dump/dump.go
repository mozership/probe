// Package dump lays out the data an action returned, such as the request and
// the response of a step, as YAML-like lines to be read by a person:
//
//	headers:
//	  Accept: */*
//	method: GET
//	url: https://example.com/
//
// It is meant for reading, not for parsing back:
//
//   - The keys of a map are sorted, so the same data is always laid out the
//     same way.
//   - A string of several lines is written under its key, as a YAML block, and
//     each of its lines is indented there.
//   - A string of one line is quoted, as Go quotes it, when it could be read
//     as something else: when it is empty, looks like a number, true, false
//     or null, has a space at either end, or holds a control character, such
//     as the newline at its end. A control character is never written as it
//     is, in a key either, so that it cannot move the cursor of a terminal.
//   - A block is written after |, as YAML writes it, when the string ends in
//     a newline, and after |- when it does not.
//   - A string that is not UTF-8, or a []byte, is not written at all, only how
//     many bytes it holds, as binary data is no use on a terminal.
//
// Options can cut what is long, and say how much was cut, so that a large
// response body cannot bury what it is shown with.
package dump

import (
	"fmt"
	"math"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
)

// Options say what is cut and left out. The zero value lays out everything.
type Options struct {
	// MaxBytes is how many bytes of a string are shown, or 0 for all of them.
	MaxBytes int
	// MaxLines is how many lines of a string are shown, or 0 for all of them.
	MaxLines int
	// MaxItems is how many entries of a map or a list are shown, or 0 for all
	// of them.
	MaxItems int
	// OmitEmpty leaves out the entries of the map given to Lines whose value
	// is nil, an empty string or an empty map or list, such as the empty
	// fields every action fills in. A zero or a false is kept, as it is a
	// value a test may have read. The maps nested in it are laid out whole:
	// what they hold is the data, such as a body or a row, empty values and
	// all.
	OmitEmpty bool
}

// indent is what a level is indented by.
const indent = "  "

// Lines lays out m as YAML-like lines, without a newline at their end. The
// lines of an entry nested in another are indented by two spaces for each
// level; the entries of m itself are not indented.
func Lines(m map[string]any, o Options) []string {
	return o.mapLines(reflect.ValueOf(m), o.OmitEmpty)
}

// entry lays out a value after the prefix it is written with: a key and a
// colon, or the - of an entry of a list.
func (o Options) entry(prefix string, v any) []string {
	rv := reflect.ValueOf(v)
	switch {
	case v == nil:
		return []string{prefix + " null"}
	case rv.Kind() == reflect.Map:
		lines := o.mapLines(rv, false)
		if len(lines) == 0 {
			return []string{prefix + " {}"}
		}
		return o.nested(prefix, lines, true)
	case rv.Kind() == reflect.Slice && rv.Type().Elem().Kind() == reflect.Uint8:
		return []string{prefix + " " + binaryText(rv.Len())}
	case rv.Kind() == reflect.Slice || rv.Kind() == reflect.Array:
		if rv.Len() == 0 {
			return []string{prefix + " []"}
		}
		return o.nested(prefix, o.listLines(rv), false)
	case rv.Kind() == reflect.String:
		return o.stringEntry(prefix, rv.String())
	default:
		text, rest := o.cutBytes(scalarText(v))
		return []string{prefix + " " + text + moreText(rest, "byte", " ")}
	}
}

// nested writes the lines of a map or a list under its prefix. The first
// entry of a map in a list is written on the line of its -, as YAML writes
// it, and the rest are indented to line up with it.
func (o Options) nested(prefix string, lines []string, isMap bool) []string {
	if prefix == "-" && isMap {
		out := []string{"- " + lines[0]}
		for _, l := range lines[1:] {
			out = append(out, indentLine(l))
		}
		return out
	}
	out := []string{prefix}
	for _, l := range lines {
		out = append(out, indentLine(l))
	}
	return out
}

// indentLine indents a line by a level. An empty line, as a string may
// hold, is left empty, so that no line ends in whitespace.
func indentLine(l string) string {
	if l == "" {
		return l
	}
	return indent + l
}

// mapLines lays out the entries of a map, sorted by their keys, and leaves
// out those whose values are empty when omit is set.
func (o Options) mapLines(rv reflect.Value, omit bool) []string {
	keys := rv.MapKeys()
	names := make([]string, len(keys))
	for i, k := range keys {
		names[i] = fmt.Sprint(k.Interface())
	}
	order := make([]int, len(keys))
	for i := range order {
		order[i] = i
	}
	sort.Slice(order, func(a, b int) bool {
		i, j := order[a], order[b]
		if x, ok := number(keys[i]); ok {
			if y, ok := number(keys[j]); ok && x != y {
				return x < y
			}
		}
		return names[i] < names[j]
	})

	var lines []string
	shown := 0
	for n, i := range order {
		v := rv.MapIndex(keys[i]).Interface()
		if omit && isEmpty(v) {
			continue
		}
		if o.MaxItems > 0 && shown == o.MaxItems {
			lines = append(lines, moreText(countShown(rv, keys, order[n:], omit), "key", ""))
			break
		}
		lines = append(lines, o.entry(keyText(names[i])+":", v)...)
		shown++
	}
	return lines
}

// number is a key of a numeric kind as a float, for the keys to be sorted
// as numbers: 2 before 10.
func number(k reflect.Value) (float64, bool) {
	switch k.Kind() {
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return float64(k.Int()), true
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
		return float64(k.Uint()), true
	case reflect.Float32, reflect.Float64:
		return k.Float(), true
	default:
		return 0, false
	}
}

// keyText is a key as it is written: quoted when it is empty or holds a
// control character.
func keyText(k string) string {
	if k == "" || strings.IndexFunc(k, isEscaped) >= 0 {
		return strconv.Quote(k)
	}
	return k
}

// countShown is how many of the keys, in the given order, would be shown
// were there no limit.
func countShown(rv reflect.Value, keys []reflect.Value, order []int, omit bool) int {
	if !omit {
		return len(order)
	}
	n := 0
	for _, i := range order {
		if !isEmpty(rv.MapIndex(keys[i]).Interface()) {
			n++
		}
	}
	return n
}

// listLines lays out the entries of a list, each after a -.
func (o Options) listLines(rv reflect.Value) []string {
	var lines []string
	for i := range rv.Len() {
		if o.MaxItems > 0 && i == o.MaxItems {
			lines = append(lines, moreText(rv.Len()-i, "item", ""))
			break
		}
		lines = append(lines, o.entry("-", rv.Index(i).Interface())...)
	}
	return lines
}

// stringEntry lays out a string: on the line of its prefix when it is one
// line, or under it, as a YAML block, when it is several.
func (o Options) stringEntry(prefix, s string) []string {
	if !utf8.ValidString(s) {
		return []string{prefix + " " + binaryText(len(s))}
	}

	// One line, a newline at its end aside, unless lines were cut after it.
	shown, rest, unit := o.cut(s)
	text := strings.TrimSuffix(shown, "\n")
	if !strings.Contains(text, "\n") && (rest == 0 || unit == "byte") {
		if needsQuotes(shown) {
			shown = strconv.Quote(shown)
		}
		return []string{prefix + " " + shown + moreText(rest, unit, " ")}
	}

	header := prefix + " |"
	if !strings.HasSuffix(s, "\n") {
		header += "-"
	}
	out := []string{header}
	for l := range strings.SplitSeq(text, "\n") {
		out = append(out, indentLine(escapeControls(strings.TrimSuffix(l, "\r"))))
	}
	if rest > 0 {
		out = append(out, indent+moreText(rest, unit, ""))
	}
	return out
}

// needsQuotes tells a string of one line that is quoted, as it could be read
// as something else written plain: an empty string, a number, true, false or
// null, a string with a space at either end, or one with a control character
// in it.
func needsQuotes(s string) bool {
	switch s {
	case "", "true", "false", "null", "{}", "[]":
		return true
	}
	if strings.TrimSpace(s) != s || strings.IndexFunc(s, isEscaped) >= 0 || strings.HasPrefix(s, `"`) {
		return true
	}
	_, err := strconv.ParseFloat(s, 64)
	return err == nil
}

// cut returns as much of s as is shown, and how much of it is not, in the
// unit it was cut by: lines when there are too many of them, as it cuts at
// the end of a line then, or bytes. It never cuts in a character.
func (o Options) cut(s string) (string, int, string) {
	if o.MaxLines > 0 {
		at := 0
		for range o.MaxLines {
			i := strings.IndexByte(s[at:], '\n')
			if i < 0 {
				at = len(s)
				break
			}
			at += i + 1
		}
		if at < len(s) && (o.MaxBytes == 0 || at <= o.MaxBytes) {
			rest := s[at:]
			lines := strings.Count(rest, "\n")
			if !strings.HasSuffix(rest, "\n") {
				lines++
			}
			return s[:at], lines, "line"
		}
	}
	shown, rest := o.cutBytes(s)
	return shown, rest, "byte"
}

// cutBytes returns as much of s as MaxBytes shows, and how many bytes of it
// it does not.
func (o Options) cutBytes(s string) (string, int) {
	if o.MaxBytes == 0 || len(s) <= o.MaxBytes {
		return s, 0
	}
	end := o.MaxBytes
	for end > 0 && !utf8.RuneStart(s[end]) {
		end--
	}
	return s[:end], len(s) - end
}

// isEmpty tells a value OmitEmpty leaves out.
func isEmpty(v any) bool {
	if v == nil {
		return true
	}
	rv := reflect.ValueOf(v)
	switch rv.Kind() {
	case reflect.String, reflect.Map, reflect.Slice, reflect.Array:
		return rv.Len() == 0
	case reflect.Pointer, reflect.Interface:
		return rv.IsNil()
	default:
		return false
	}
}

// scalarText writes a value that is neither a string, a map nor a list.
// A float is written as JSON writes it, without an exponent unless it is
// very large or very small, as JSON numbers decoded as floats are counts and
// codes more often than not.
func scalarText(v any) string {
	switch n := v.(type) {
	case float64:
		return floatText(n, 64)
	case float32:
		return floatText(float64(n), 32)
	}
	return escapeControls(fmt.Sprint(v))
}

func floatText(f float64, bits int) string {
	if a := math.Abs(f); a != 0 && (a < 1e-6 || a >= 1e21) {
		return strconv.FormatFloat(f, 'g', -1, bits)
	}
	return strconv.FormatFloat(f, 'f', -1, bits)
}

// escapeControls writes the control characters of s, a tab aside, as Go
// escapes them, and leaves the rest of it as it is.
func escapeControls(s string) string {
	if strings.IndexFunc(s, isEscaped) < 0 {
		return s
	}
	var b strings.Builder
	for _, r := range s {
		if isEscaped(r) {
			q := strconv.QuoteRune(r)
			b.WriteString(q[1 : len(q)-1])
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}

func isEscaped(r rune) bool {
	return r != '\t' && unicode.IsControl(r)
}

// binaryText is what binary data of n bytes is shown as.
func binaryText(n int) string {
	return fmt.Sprintf("(binary, %d bytes)", n)
}

// moreText is the marker of what was cut, after sep: … (12 more bytes). It
// is empty when nothing was.
func moreText(n int, unit, sep string) string {
	if n == 0 {
		return ""
	}
	if n != 1 {
		unit += "s"
	}
	return fmt.Sprintf("%s… (%d more %s)", sep, n, unit)
}
