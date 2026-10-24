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
//   - A control character in a string is escaped, so that it cannot move the
//     cursor of a terminal, and an empty string is written as "".
//   - A string that is not UTF-8, or a []byte, is not written at all, only how
//     many bytes it holds, as binary data is no use on a terminal.
//
// Options can cut what is long, and say how much was cut, so that a large
// response body cannot bury what it is shown with.
package dump

import (
	"fmt"
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
	// OmitEmpty leaves out the entries of a map whose value is nil, an empty
	// string or an empty map or list. A zero or a false is kept, as it is a
	// value a test may have read.
	OmitEmpty bool
}

// indent is what a level is indented by.
const indent = "  "

// Lines lays out m as YAML-like lines, without a newline at their end. The
// lines of an entry nested in another are indented by two spaces for each
// level; the entries of m itself are not indented.
func Lines(m map[string]any, o Options) []string {
	return o.mapLines(reflect.ValueOf(m))
}

// entry lays out a value after the prefix it is written with: a key and a
// colon, or the - of an entry of a list.
func (o Options) entry(prefix string, v any) []string {
	rv := reflect.ValueOf(v)
	switch {
	case v == nil:
		return []string{prefix + " null"}
	case rv.Kind() == reflect.Map:
		lines := o.mapLines(rv)
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
		return []string{prefix + " " + scalarText(v)}
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

// mapLines lays out the entries of a map, sorted by their keys.
func (o Options) mapLines(rv reflect.Value) []string {
	keys := rv.MapKeys()
	names := make([]string, len(keys))
	for i, k := range keys {
		names[i] = fmt.Sprint(k.Interface())
	}
	order := make([]int, len(keys))
	for i := range order {
		order[i] = i
	}
	sort.Slice(order, func(a, b int) bool { return names[order[a]] < names[order[b]] })

	var lines []string
	shown := 0
	for n, i := range order {
		v := rv.MapIndex(keys[i]).Interface()
		if o.OmitEmpty && isEmpty(v) {
			continue
		}
		if o.MaxItems > 0 && shown == o.MaxItems {
			lines = append(lines, moreText(o.countShown(rv, keys, order[n:]), "keys"))
			break
		}
		lines = append(lines, o.entry(names[i]+":", v)...)
		shown++
	}
	return lines
}

// countShown is how many of the keys, in the given order, would be shown
// were there no limit.
func (o Options) countShown(rv reflect.Value, keys []reflect.Value, order []int) int {
	if !o.OmitEmpty {
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
			lines = append(lines, moreText(rv.Len()-i, "items"))
			break
		}
		lines = append(lines, o.entry("-", rv.Index(i).Interface())...)
	}
	return lines
}

// stringEntry lays out a string: on the line of its prefix when it is one
// line, or under it, as a YAML block, when it is several.
func (o Options) stringEntry(prefix, s string) []string {
	switch {
	case !utf8.ValidString(s):
		return []string{prefix + " " + binaryText(len(s))}
	case s == "":
		return []string{prefix + ` ""`}
	}

	shown, rest := o.cut(s)
	text := strings.TrimSuffix(shown, "\n")
	// One line, unless more lines were cut after it.
	if !strings.Contains(text, "\n") && (rest == 0 || !strings.HasSuffix(shown, "\n")) {
		if text == "" {
			// Nothing but a newline, which is written escaped.
			text = shown
		}
		line := prefix + " " + escapeControls(text)
		if rest > 0 {
			line += moreText(rest, "bytes")
		}
		return []string{line}
	}

	out := []string{prefix + " |"}
	for l := range strings.SplitSeq(text, "\n") {
		out = append(out, indentLine(escapeControls(strings.TrimSuffix(l, "\r"))))
	}
	if rest > 0 {
		out = append(out, indent+moreText(rest, "bytes"))
	}
	return out
}

// cut returns as much of s as is shown, and how many bytes of it are not.
// It cuts at the end of a line when there are too many lines, and never in
// the middle of a character.
func (o Options) cut(s string) (string, int) {
	end := len(s)
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
		end = min(end, at)
	}
	if o.MaxBytes > 0 && end > o.MaxBytes {
		end = o.MaxBytes
		for end > 0 && !utf8.RuneStart(s[end]) {
			end--
		}
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
// A float is written without an exponent, as JSON numbers decoded as floats
// are counts and codes more often than not.
func scalarText(v any) string {
	switch n := v.(type) {
	case float64:
		return strconv.FormatFloat(n, 'f', -1, 64)
	case float32:
		return strconv.FormatFloat(float64(n), 'f', -1, 32)
	}
	return escapeControls(fmt.Sprint(v))
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

// moreText is the marker of what was cut: … (12 more bytes).
func moreText(n int, unit string) string {
	return fmt.Sprintf("… (%d more %s)", n, unit)
}
