// Package envfile reads files of environment variables, one NAME=value a
// line, as the .env files many tools keep beside a project.
package envfile

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"strings"
)

// Var is a variable a file sets.
type Var struct {
	Name  string
	Value string
}

// Read reads the variables of the file at path, in the order they are
// written. An error names the file and the line.
func Read(path string) ([]Var, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()

	vars, err := Parse(f)
	if err != nil {
		return nil, fmt.Errorf("%s:%w", path, err)
	}
	return vars, nil
}

// Parse reads variables from r, in the order they are written:
//
//	# a comment
//	NAME=value
//	export NAME=value
//	NAME="a value with spaces, \n for a newline and \" for a quote"
//	NAME='a value taken as it is, $ and \ included'
//	NAME=value # a comment after a value that is not quoted
//
// A value that is not quoted has the spaces around it removed. A value in
// double quotes may go on over several lines. Nothing is expanded: $OTHER
// in a value is those characters. A name written twice is returned twice,
// for the last to win.
//
// An error starts with the line it is about, as "3: ...".
func Parse(r io.Reader) ([]Var, error) {
	var vars []Var
	sc := bufio.NewScanner(r)
	// A value may be long, such as a certificate, so a line may be longer
	// than a scanner takes by default.
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)

	line := 0
	for sc.Scan() {
		line++
		start := line
		// The spaces at the end of the line are kept until the value is
		// known not to be quoted: in quotes that go on over lines they
		// are part of the value.
		text := strings.TrimLeft(strings.TrimPrefix(sc.Text(), "\xef\xbb\xbf"), " \t")
		if trimmed := strings.TrimSpace(text); trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		if rest, ok := strings.CutPrefix(text, "export "); ok {
			text = strings.TrimLeft(rest, " \t")
		}

		name, value, ok := strings.Cut(text, "=")
		name = strings.TrimSpace(name)
		if !ok {
			return nil, fmt.Errorf("%d: %q is not NAME=value", start, strings.TrimSpace(text))
		}
		if !validName(name) {
			return nil, fmt.Errorf("%d: %q is not a name of an environment variable: letters, digits and _, not starting with a digit", start, name)
		}

		value = strings.TrimLeft(value, " \t")
		switch {
		case strings.HasPrefix(value, `"`):
			// What is quoted goes on until the quote that closes it, on
			// this line or on one after it.
			body := value[1:]
			for {
				unquoted, rest, closed := unquoteDouble(body)
				if closed {
					if err := onlyComment(rest); err != nil {
						return nil, fmt.Errorf("%d: %s: %w", line, name, err)
					}
					value = unquoted
					break
				}
				if !sc.Scan() {
					// A file that could not be read to its end is told
					// as that, not as a quote left open.
					if err := sc.Err(); err != nil {
						return nil, fmt.Errorf("%d: %w", line+1, err)
					}
					return nil, fmt.Errorf("%d: %s: the double quote is not closed", start, name)
				}
				line++
				body += "\n" + sc.Text()
			}
		case strings.HasPrefix(value, `'`):
			end := strings.IndexByte(value[1:], '\'')
			if end < 0 {
				return nil, fmt.Errorf("%d: %s: the single quote is not closed", start, name)
			}
			if err := onlyComment(value[end+2:]); err != nil {
				return nil, fmt.Errorf("%d: %s: %w", start, name, err)
			}
			value = value[1 : end+1]
		default:
			// A # starts a comment when a space or a tab comes before it,
			// so that a value such as a URL with a fragment keeps its own.
			value = strings.TrimSpace(uncomment(value))
		}
		vars = append(vars, Var{Name: name, Value: value})
	}
	if err := sc.Err(); err != nil {
		return nil, fmt.Errorf("%d: %w", line+1, err)
	}
	return vars, nil
}

// uncomment returns s, a value that is not quoted, without the comment
// after it: what starts at a # that comes first or after a space or a tab.
func uncomment(s string) string {
	for i := 0; i < len(s); i++ {
		if s[i] == '#' && (i == 0 || s[i-1] == ' ' || s[i-1] == '\t') {
			return s[:i]
		}
	}
	return s
}

// unquoteDouble reads s, what comes after an opening double quote, up to
// the quote that closes it, and returns what is quoted with its escapes
// replaced, and what comes after the closing quote. closed is false when s
// ends before the quote is closed.
func unquoteDouble(s string) (value, rest string, closed bool) {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		switch s[i] {
		case '"':
			return b.String(), s[i+1:], true
		case '\\':
			if i+1 >= len(s) {
				return "", "", false
			}
			i++
			switch s[i] {
			case 'n':
				b.WriteByte('\n')
			case 'r':
				b.WriteByte('\r')
			case 't':
				b.WriteByte('\t')
			case '"', '\\', '$':
				b.WriteByte(s[i])
			default:
				// Any other escape is kept as it is written.
				b.WriteByte('\\')
				b.WriteByte(s[i])
			}
		default:
			b.WriteByte(s[i])
		}
	}
	return "", "", false
}

// onlyComment returns an error unless s, what comes after a closing quote,
// is empty or a comment.
func onlyComment(s string) error {
	s = strings.TrimSpace(s)
	if s == "" || strings.HasPrefix(s, "#") {
		return nil
	}
	return fmt.Errorf("unexpected %q after the closing quote", s)
}

// validName reports whether name can be the name of an environment
// variable: letters, digits and _, not starting with a digit.
func validName(name string) bool {
	if name == "" {
		return false
	}
	for i, c := range name {
		switch {
		case c == '_', c >= 'A' && c <= 'Z', c >= 'a' && c <= 'z':
		case c >= '0' && c <= '9' && i > 0:
		default:
			return false
		}
	}
	return true
}
