package envfile

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestParse(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  []Var
	}{
		{"nothing", "", nil},
		{"a value", "URL=http://localhost:8080\n", []Var{{"URL", "http://localhost:8080"}}},
		{"no newline at the end", "A=1", []Var{{"A", "1"}}},
		{"comments and blank lines", "# a comment\n\n  # another\nA=1\n", []Var{{"A", "1"}}},
		{"export", "export A=1\nexport  B=2\n", []Var{{"A", "1"}, {"B", "2"}}},
		{"spaces around the name and the value", "  A = 1  \n", []Var{{"A", "1"}}},
		{"an empty value", "A=\nB=''\nC=\"\"\n", []Var{{"A", ""}, {"B", ""}, {"C", ""}}},
		{"a value that holds =", "DSN=postgres://u:p@h/db?sslmode=disable\n", []Var{{"DSN", "postgres://u:p@h/db?sslmode=disable"}}},
		{"a comment after a value", "A=1 # one\nB=2\t # two\n", []Var{{"A", "1"}, {"B", "2"}}},
		{"a # that is part of a value", "URL=http://h/#top\nCOLOR=a#b\n", []Var{{"URL", "http://h/#top"}, {"COLOR", "a#b"}}},
		{"a value that is only a comment", "A=# nothing\n", []Var{{"A", ""}}},
		{"double quotes", `A="a b # c"` + "\n", []Var{{"A", "a b # c"}}},
		{"escapes in double quotes", `A="l1\nl2\t\"q\" \\ \$X \d"` + "\n", []Var{{"A", "l1\nl2\t\"q\" \\ $X \\d"}}},
		{"a comment after double quotes", `A="1" # one` + "\n", []Var{{"A", "1"}}},
		{"double quotes over several lines", "KEY=\"-----BEGIN\nabc\n-----END\"\nB=2\n", []Var{{"KEY", "-----BEGIN\nabc\n-----END"}, {"B", "2"}}},
		{"single quotes", `A='a $B \n "c" # d' # e` + "\n", []Var{{"A", `a $B \n "c" # d`}}},
		{"nothing is expanded", "A=1\nB=$A/${A}\n", []Var{{"A", "1"}, {"B", "$A/${A}"}}},
		{"a name written twice", "A=1\nA=2\n", []Var{{"A", "1"}, {"A", "2"}}},
		{"CRLF line endings", "A=1\r\nB=\"2\"\r\n", []Var{{"A", "1"}, {"B", "2"}}},
		{"a byte order mark", "\xef\xbb\xbfA=1\n", []Var{{"A", "1"}}},
		{"a name with digits and _", "_A1=x\n", []Var{{"_A1", "x"}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := Parse(strings.NewReader(tt.input))
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("Parse() = %#v, want %#v", got, tt.want)
			}
		})
	}
}

func TestParse_Errors(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{"a line without =", "A=1\njust text\n", `2: "just text" is not NAME=value`},
		{"no name", "=1\n", `1: "" is not a name`},
		{"a name that starts with a digit", "1A=x\n", `1: "1A" is not a name`},
		{"a name with a space", "A B=x\n", `1: "A B" is not a name`},
		{"a name with a dash", "\n\nA-B=x\n", `3: "A-B" is not a name`},
		{"a double quote left open", "A=1\nB=\"abc\nC=2\n", "2: B: the double quote is not closed"},
		{"a single quote left open", "A='abc\n", "1: A: the single quote is not closed"},
		{"text after a closing double quote", `A="1" 2` + "\n", `1: A: unexpected "2" after the closing quote`},
		{"text after a closing single quote", "A='1'2\n", `1: A: unexpected "2" after the closing quote`},
		{"text after a quote closed on a later line", "A=\"1\n2\" x\n", `2: A: unexpected "x" after the closing quote`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := Parse(strings.NewReader(tt.input))
			if err == nil || !strings.HasPrefix(err.Error(), tt.want) {
				t.Errorf("Parse() error = %v, want one starting with %q", err, tt.want)
			}
		})
	}
}

func TestRead(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".env")
	if err := os.WriteFile(path, []byte("A=1\nB=2\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := Read(path)
	if err != nil {
		t.Fatal(err)
	}
	if want := []Var{{"A", "1"}, {"B", "2"}}; !reflect.DeepEqual(got, want) {
		t.Errorf("Read() = %#v, want %#v", got, want)
	}

	if _, err := Read(filepath.Join(dir, "missing")); !os.IsNotExist(err) {
		t.Errorf("Read() of a missing file = %v, want it to say the file does not exist", err)
	}

	bad := filepath.Join(dir, "bad.env")
	if err := os.WriteFile(bad, []byte("A=1\nnope\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Read(bad); err == nil || !strings.HasPrefix(err.Error(), bad+":2: ") {
		t.Errorf("Read() error = %v, want it to name the file and the line", err)
	}
}
