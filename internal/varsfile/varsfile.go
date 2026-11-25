// Package varsfile reads the vars of a run from a JSON object, for a program
// that runs Probe to give them without putting them on the command line,
// where anyone who lists the processes sees them.
package varsfile

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
)

// Read reads the vars of the file at path, or of r when path is -. An error
// names the file.
func Read(path string, r io.Reader) (map[string]string, error) {
	if path == "-" {
		vars, err := Parse(r)
		if err != nil {
			return nil, fmt.Errorf("the standard input: %w", err)
		}
		return vars, nil
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()
	vars, err := Parse(f)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return vars, nil
}

// Parse reads a JSON object of vars by name. A string is the value as it
// is; a number, a boolean, an object or a list is its JSON, with numbers as
// they are written; a null is no value, which leaves the var to what the
// workflow writes. A name is taken without the white space around it, as
// --var takes it, and is refused when the object has it twice.
func Parse(r io.Reader) (map[string]string, error) {
	br := bufio.NewReader(r)
	skipped := 0
	if bom, err := br.Peek(len(utf8BOM)); err == nil && bytes.Equal(bom, utf8BOM) {
		skipped, _ = br.Discard(len(utf8BOM))
	}
	dec := json.NewDecoder(br)
	dec.UseNumber()
	vars, err := parseObject(dec)
	if err != nil {
		var syntax *json.SyntaxError
		if errors.As(err, &syntax) {
			return nil, fmt.Errorf("%w, at byte %d", err, syntax.Offset+int64(skipped))
		}
		return nil, err
	}
	return vars, nil
}

// utf8BOM is the mark some editors write at the start of a UTF-8 file.
var utf8BOM = []byte{0xEF, 0xBB, 0xBF}

func parseObject(dec *json.Decoder) (map[string]string, error) {
	start, err := dec.Token()
	if errors.Is(err, io.EOF) {
		return nil, errors.New("no JSON object")
	}
	if err != nil {
		return nil, err
	}
	if start != json.Delim('{') {
		return nil, fmt.Errorf("a JSON object of vars by name is needed, not %s", kind(start))
	}
	vars := map[string]string{}
	seen := map[string]bool{}
	for dec.More() {
		key, err := dec.Token()
		if err != nil {
			return nil, err
		}
		name := strings.TrimSpace(key.(string))
		if name == "" {
			return nil, errors.New("a var has no name")
		}
		if seen[name] {
			return nil, fmt.Errorf("var %s is given more than once", name)
		}
		seen[name] = true
		var v any
		if err := dec.Decode(&v); err != nil {
			return nil, err
		}
		if v == nil {
			continue
		}
		s, err := text(v)
		if err != nil {
			return nil, fmt.Errorf("var %s: %w", name, err)
		}
		vars[name] = s
	}
	if _, err := dec.Token(); err != nil {
		return nil, err
	}
	if _, err := dec.Token(); !errors.Is(err, io.EOF) {
		return nil, errors.New("more follows the JSON object")
	}
	return vars, nil
}

// text returns v, a value decoded from JSON, as the text of a var.
func text(v any) (string, error) {
	switch val := v.(type) {
	case string:
		return val, nil
	case json.Number:
		return val.String(), nil
	}
	var b bytes.Buffer
	enc := json.NewEncoder(&b)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		return "", err
	}
	return strings.TrimSuffix(b.String(), "\n"), nil
}

// kind names what t, the first token of a JSON value, starts.
func kind(t json.Token) string {
	switch t.(type) {
	case json.Delim:
		return "a list"
	case string:
		return "a string"
	case json.Number:
		return "a number"
	case bool:
		return "a boolean"
	default:
		return "null"
	}
}
