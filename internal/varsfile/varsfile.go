// Package varsfile reads the vars of a run from a JSON object, for a program
// that runs Probe to give them without putting them on the command line,
// where anyone who lists the processes sees them.
package varsfile

import (
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
// workflow writes.
func Parse(r io.Reader) (map[string]string, error) {
	dec := json.NewDecoder(r)
	dec.UseNumber()
	var raw any
	if err := dec.Decode(&raw); err != nil {
		if errors.Is(err, io.EOF) {
			return nil, errors.New("no JSON object")
		}
		return nil, err
	}
	if _, err := dec.Token(); !errors.Is(err, io.EOF) {
		return nil, errors.New("more follows the JSON object")
	}
	object, ok := raw.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("a JSON object of vars by name is needed, not %s", kind(raw))
	}
	vars := make(map[string]string, len(object))
	for name, v := range object {
		if strings.TrimSpace(name) == "" {
			return nil, errors.New("a var has no name")
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

// kind names what v, a value decoded from JSON, is.
func kind(v any) string {
	switch v.(type) {
	case []any:
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
