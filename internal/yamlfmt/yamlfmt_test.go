package yamlfmt

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestFormat(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{"nothing", "", ""},
		{"only blank lines", "\n\n", ""},
		{"a formatted workflow", "name: x\n\njobs:\n- name: A\n  steps:\n  - uses: hello\n", "name: x\n\njobs:\n- name: A\n  steps:\n  - uses: hello\n"},

		// Indentation.
		{"four spaces", "a:\n    b:\n        c: 1\n    d: 2\n", "a:\n  b:\n    c: 1\n  d: 2\n"},
		{"one space", "a:\n b:\n  c: 1\n", "a:\n  b:\n    c: 1\n"},
		{"an indented sequence", "a:\n  - 1\n  - 2\nb: 3\n", "a:\n- 1\n- 2\n\nb: 3\n"},
		{"an indented sequence of mappings", "a:\n  b:\n    - c: 1\n      d: 2\n    - c: 3\n", "a:\n  b:\n  - c: 1\n    d: 2\n  - c: 3\n"},
		{"a sequence that is not indented", "a:\n  b:\n  - c: 1\n    d:\n    - 2\n  e: 3\n", "a:\n  b:\n  - c: 1\n    d:\n    - 2\n  e: 3\n"},
		{"spaces after a -", "a:\n-   b: 1\n    c: 2\n-     d\n", "a:\n- b: 1\n  c: 2\n- d\n"},
		{"a sequence in a sequence", "a:\n  -   - 1\n      - 2\n  - - 3\n", "a:\n- - 1\n  - 2\n- - 3\n"},
		{"a - on a line of its own", "a:\n  -\n      b: 1\n  -\n      - 2\n", "a:\n-\n  b: 1\n-\n  - 2\n"},
		{"a sequence at the top", "  - a\n  - b:\n      - c\n", "- a\n- b:\n  - c\n"},
		{"a value on the line after its key", "a:\n    value\nb:\n    \"quoted\"\n", "a:\n  value\nb:\n  \"quoted\"\n"},
		{"a document that is indented", "  a: 1\n  b:\n      c: 2\n", "a: 1\n\nb:\n  c: 2\n"},

		// Scalars are left as they are written.
		{"quotes are kept", "a: \"x\"\nb: 'y'\nc: z\nd: \"{{vars.url}}\"\n", "a: \"x\"\nb: 'y'\nc: z\nd: \"{{vars.url}}\"\n"},
		{"a # in a quoted scalar", "a: \"x #y\"   #z\nb: 'x #y'\n", "a: \"x #y\" # z\nb: 'x #y'\n"},
		{"a literal scalar", "a:\n    b: |\n        one\n          two\n\n        three\n    c: 1\n", "a:\n  b: |\n    one\n      two\n\n    three\n  c: 1\n"},
		{"a literal scalar under an indented sequence", "a:\n    - b: |\n          # not a comment\n          x  \n      c: 1\n", "a:\n- b: |\n    # not a comment\n    x  \n  c: 1\n"},
		{"a literal scalar that is an entry", "a:\n    -   |\n        x\n    - >\n        y\n", "a:\n- |\n  x\n- >\n  y\n"},
		{"a literal scalar with its indentation given", "a:\n    b: |2\n          x\n        y\n", "a:\n  b: |2\n        x\n      y\n"},
		{"spaces that are indentation in a literal scalar", "a: |\n  x\n  \n \n  y\n", "a: |\n  x\n\n\n  y\n"},
		{"spaces that are content in a literal scalar", "a: |\n  x\n     \n  y\n", "a: |\n  x\n     \n  y\n"},
		{"a literal scalar that keeps its blank lines", "a: |+\n  x\n\n\nb: 1\n", "a: |+\n  x\n\n\nb: 1\n"},
		{"blank lines after a literal scalar", "a: |\n  x\n\n\nb: 1\n", "a: |\n  x\nb: 1\n"},
		{"an empty literal scalar", "a: |\nb: 1\n", "a: |\nb: 1\n"},
		{"a folded scalar", "a:\n    b: >-\n\n        one\n         two\n", "a:\n  b: >-\n\n    one\n     two\n"},
		{"a plain scalar over lines", "a:\n    b: one\n       two\n\n       three\n    c: 1\n", "a:\n  b: one\n     two\n\n     three\n  c: 1\n"},
		{"a quoted scalar over lines", "a:\n    b: \"one\n\n       two  \n       three\"\n    c: 1\n", "a:\n  b: \"one\n\n     two\n     three\"\n  c: 1\n"},
		{"a flow collection over lines", "a:\n    b: [1,\n        2,   3]\n    c: {d: 1,\n\n        e: 2}\n", "a:\n  b: [1,\n      2,   3]\n  c: {d: 1,\n\n      e: 2}\n"},
		{"a flow collection on a line", "a: [ 1,2 ]\nb: { c:   1 }\n", "a: [ 1,2 ]\nb: { c: 1 }\n"},
		{"anchors, aliases and tags", "a: &x\n    b: 1\nc: *x\nd: !!str   2\ne:\n    <<: *x\n", "a: &x\n  b: 1\n\nc: *x\nd: !!str   2\n\ne:\n  <<: *x\n"},
		{"an alias defined in another file", "a:\n    b: *shared\n", "a:\n  b: *shared\n"},
		{"a key that is a template", "a:\n    x-{{outputs.t.id}}:   1\n    \"{{outputs.t.id}}\":\n        b: 2\n", "a:\n  x-{{outputs.t.id}}: 1\n  \"{{outputs.t.id}}\":\n    b: 2\n"},
		{"a key written twice", "a: 1\na: 2\n", "a: 1\na: 2\n"},
		{"characters wider than a byte", "名前:   \"日本語\"   #コメント\nb:\n    説明: あ   # い\n", "名前: \"日本語\" # コメント\n\nb:\n  説明: あ # い\n"},
		{"a complex key", "? a\n:   b\n", "? a\n: b\n"},

		// Spaces.
		{"spaces after a key", "a:    1\nb:\n  c:  \"x\"\n  d:\t2\n  e: \t 3\n  f:\t# g\n", "a: 1\n\nb:\n  c: \"x\"\n  d: 2\n  e: 3\n  f: # g\n"},
		{"a : in a value", "a: b:c\nd: \"e:   f\"\ng: http://h:80/\n", "a: b:c\nd: \"e:   f\"\ng: http://h:80/\n"},
		{"spaces at the end of a line", "a: 1  \nb:  \n  c: 2\t\n", "a: 1\n\nb:\n  c: 2\n"},
		{"no newline at the end", "a: 1", "a: 1\n"},
		{"blank lines at the end", "a: 1\n\n\n", "a: 1\n"},
		{"CRLF line endings", "a: 1\r\nb:\r\n    c: 2\r\n", "a: 1\n\nb:\n  c: 2\n"},
		// A scalar that ended without a newline says so once the file
		// ends in one.
		{"a literal scalar the file ends in without a newline", "a: |\n  x", "a: |-\n  x\n"},
		{"a kept literal scalar the file ends in without a newline", "a: |+ #c\n  x", "a: |- # c\n  x\n"},
		{"a folded scalar the file ends in without a newline", "a: >2\n    x", "a: >2-\n    x\n"},
		{"a stripped scalar the file ends in without a newline", "a: |-\n  x", "a: |-\n  x\n"},

		// Blank lines.
		{"keys at the top that hold scalars", "name: x\n\ndescription: y\n\nversion: 1\n", "name: x\ndescription: y\nversion: 1\n"},
		{"keys at the top that hold collections", "name: x\ndescription: |\n  y\nvars:\n  a: 1\nsecrets: [A]\njobs:\n- name: A\n", "name: x\ndescription: |\n  y\n\nvars:\n  a: 1\n\nsecrets: [A]\n\njobs:\n- name: A\n"},
		{"keys at the top that hold flow collections", "name: x\nsecrets: [A]\nneeds: {a: 1}\nlist:\n  [1, 2]\nmap:\n  {a: 1}\ndescription: y\n", "name: x\nsecrets: [A]\nneeds: {a: 1}\nlist:\n  [1, 2]\nmap:\n  {a: 1}\ndescription: y\n"},
		{"many blank lines", "name: x\n\n\n\nvars:\n\n  a: 1\n\n\n  b: 2\n", "name: x\n\nvars:\n  a: 1\n  b: 2\n"},
		{"blank lines at the start", "\n\nname: x\n", "name: x\n"},
		{"jobs", "jobs:\n\n- name: A\n  steps:\n  - uses: hello\n- name: B\n  steps:\n  - uses: hello\n\n\n- name: C\n", "jobs:\n- name: A\n  steps:\n  - uses: hello\n\n- name: B\n  steps:\n  - uses: hello\n\n- name: C\n"},
		{"jobs that are indented", "jobs:\n    - name: A\n    - name: B\n", "jobs:\n- name: A\n\n- name: B\n"},
		{"steps", "jobs:\n- name: A\n  steps:\n\n  - uses: hello\n\n  - uses: hello\n    with:\n\n      name: x\n", "jobs:\n- name: A\n  steps:\n  - uses: hello\n  - uses: hello\n    with:\n      name: x\n"},
		{"the steps of a job file", "name: x\nsteps:\n- uses: hello\n\n- uses: hello\n", "name: x\n\nsteps:\n- uses: hello\n- uses: hello\n"},
		{"a sequence at the top that is not jobs", "items:\n- a: 1\n\n- a: 2\n", "items:\n- a: 1\n- a: 2\n"},
		{"documents", "a: 1\n\n---\n\nb:\n  c: 2\nd: 3\n...\n", "a: 1\n---\nb:\n  c: 2\n\nd: 3\n...\n"},
		{"jobs in a later document", "jobs:\n- name: A\n- name: B\n---\nother:\n- name: A\n- name: B\n", "jobs:\n- name: A\n\n- name: B\n---\nother:\n- name: A\n- name: B\n"},

		// Comments.
		{"a space after the #", "#a\n# b\n#  c\n##d\n#\n", "# a\n# b\n#  c\n## d\n#\n"},
		{"the #! a file starts with", "#!/usr/bin/env probe\n#!not on the first line\na: 1\n", "#!/usr/bin/env probe\n# !not on the first line\na: 1\n"},
		{"a comment at the end of a line", "a: 1#not a comment\nb: 2    #c\nc: 3 # d  \nd:   # e\n  f: 4\n", "a: 1#not a comment\nb: 2 # c\nc: 3 # d\n\nd: # e\n  f: 4\n"},
		{"a comment after a -", "a:\n-   # b\n    c: 1\n-   d   #e\n", "a:\n- # b\n  c: 1\n- d # e\n"},
		{"a comment after the header of a literal scalar", "a: |   #b\n  x\nc: >-   # d\n  y\n", "a: | # b\n  x\nc: >- # d\n  y\n"},
		{"a comment on what follows it", "a:\n    # on b\n    b: 1\n  # on c\n    c: 2\n# on d\n    d: 3\n", "a:\n  # on b\n  b: 1\n  # on c\n  c: 2\n  # on d\n  d: 3\n"},
		{"a comment on a step", "jobs:\n- name: A\n  steps:\n  - uses: hello\n\n  # the second\n  # step\n\n  - uses: hello\n", "jobs:\n- name: A\n  steps:\n  - uses: hello\n  # the second\n  # step\n  - uses: hello\n"},
		{"a comment that ends a block", "a:\n    b:\n        c: 1\n        # ends b\n    # ends a\nd: 2\n", "a:\n  b:\n    c: 1\n    # ends b\n  # ends a\n\nd: 2\n"},
		{"a comment that ends a sequence", "a:\n    - b: 1\n      # in the entry\n    # - b: 2\n    #   c: 3\nd: 2\n", "a:\n- b: 1\n  # in the entry\n\n# - b: 2\n#   c: 3\nd: 2\n"},
		{"a comment the file ends in", "a:\n    - b: 1\n    # - b: 2\n", "a:\n- b: 1\n# - b: 2\n"},
		{"a comment before a job", "jobs:\n- name: A\n# on B\n\n# and more\n- name: B\n", "jobs:\n- name: A\n\n# on B\n# and more\n- name: B\n"},
		{"a comment that ends a job, and one before the next", "jobs:\n- name: A\n  steps:\n  - uses: hello\n  # ends A\n# on B\n- name: B\n", "jobs:\n- name: A\n  steps:\n  - uses: hello\n  # ends A\n\n# on B\n- name: B\n"},
		{"a comment before a key at the top", "# the file\nname: x\n# the vars\nvars:\n  a: 1\n", "# the file\nname: x\n\n# the vars\nvars:\n  a: 1\n"},
		{"only comments", "  #a\n\n  #b\n", "# a\n# b\n"},
		{"a comment before a document", "a: 1\n  # on the document\n---\nb: 2\n", "a: 1\n# on the document\n---\nb: 2\n"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := Format([]byte(tt.input))
			if err != nil {
				t.Fatal(err)
			}
			if string(got) != tt.want {
				t.Errorf("Format(%q)\n got: %q\nwant: %q", tt.input, got, tt.want)
			}
			// What is formatted stays as it is.
			again, err := Format(got)
			if err != nil {
				t.Fatal(err)
			}
			if string(again) != string(got) {
				t.Errorf("Format is not stable for %q\n once: %q\ntwice: %q", tt.input, got, again)
			}
		})
	}
}

func TestFormat_Error(t *testing.T) {
	tests := []struct {
		name  string
		input string
	}{
		{"a flow sequence that is not closed", "a: [1, 2\n"},
		{"a key under a scalar", "a: 1\n  b: 2\n"},
		{"a tab for indentation", "a:\n\tb: 1\n"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got, err := Format([]byte(tt.input)); err == nil {
				t.Errorf("Format(%q) should fail, got %q", tt.input, got)
			}
		})
	}
}

// TestFormat_Workflows keeps the workflows of the repository formatted, and
// with them checks that a workflow is formatted without an error and stays
// as it is when it is formatted again.
func TestFormat_Workflows(t *testing.T) {
	var files []string
	for _, pattern := range []string{"../../examples/*.yml", "../../examples/jobs/*.yml", "../../e2e/*.yml", "../../misc/demo/*.yml"} {
		matches, err := filepath.Glob(pattern)
		if err != nil {
			t.Fatal(err)
		}
		files = append(files, matches...)
	}
	if len(files) == 0 {
		t.Fatal("no workflow found")
	}
	for _, path := range files {
		src, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		got, err := Format(src)
		if err != nil {
			t.Errorf("%s: %v", path, err)
			continue
		}
		if string(got) != string(src) {
			t.Errorf("%s is not formatted: run probe fmt %s", path, strings.TrimPrefix(path, "../../"))
		}
	}
}
