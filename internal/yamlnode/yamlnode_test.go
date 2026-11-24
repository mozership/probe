package yamlnode

import (
	"slices"
	"testing"

	"github.com/goccy/go-yaml/ast"
	"github.com/goccy/go-yaml/parser"
)

func body(t *testing.T, text string) ast.Node {
	t.Helper()
	f, err := parser.ParseBytes([]byte(text), 0)
	if err != nil {
		t.Fatal(err)
	}
	return f.Docs[0].Body
}

func keys(pairs []*ast.MappingValueNode) []string {
	var out []string
	for _, p := range pairs {
		out = append(out, p.Key.String())
	}
	return out
}

func TestMapFiles(t *testing.T) {
	tests := []struct {
		name  string
		files []string
		data  []string
		want  Files
	}{
		{"one file", []string{"a.yml"}, []string{"a: 1\nb: 2\n"}, Files{{"a.yml", 1}}},
		{
			"each file after the lines of those before it",
			[]string{"a.yml", "b.yml"},
			[]string{"a: 1\nb: 2\n", "c: 3\n"},
			Files{{"a.yml", 1}, {"b.yml", 3}},
		},
		{
			"a file without a final newline is given one",
			[]string{"a.yml", "b.yml", "c.yml"},
			[]string{"a: 1", "b: 2\n", "c: 3"},
			Files{{"a.yml", 1}, {"b.yml", 2}, {"c.yml", 3}},
		},
		{
			"an empty file takes no line",
			[]string{"a.yml", "b.yml", "c.yml"},
			[]string{"a: 1\n", "", "c: 3\n"},
			Files{{"a.yml", 1}, {"b.yml", 2}, {"c.yml", 2}},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var data [][]byte
			for _, d := range tt.data {
				data = append(data, []byte(d))
			}
			if got := MapFiles(tt.files, data); !slices.Equal(got, tt.want) {
				t.Errorf("MapFiles = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestFiles_At(t *testing.T) {
	two := Files{{"a.yml", 1}, {"b.yml", 4}}
	tests := []struct {
		name     string
		files    Files
		line     int
		wantFile string
		wantLine int
	}{
		{"no file", nil, 3, "", 0},
		{"a line of the first file", two, 2, "a.yml", 2},
		{"the last line of the first file", two, 3, "a.yml", 3},
		{"the first line of the second file", two, 4, "b.yml", 1},
		{"a line past the start of the last file", two, 10, "b.yml", 7},
		{"no line of the only file", Files{{"a.yml", 1}}, 0, "a.yml", 0},
		{"no line of several files", two, 0, "", 0},
		{"the file after an empty one", Files{{"a.yml", 1}, {"b.yml", 2}, {"c.yml", 2}}, 2, "c.yml", 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			file, line := tt.files.At(tt.line)
			if file != tt.wantFile || line != tt.wantLine {
				t.Errorf("At(%d) = %s, %d, want %s, %d", tt.line, file, line, tt.wantFile, tt.wantLine)
			}
		})
	}
}

func TestPairs(t *testing.T) {
	tests := []struct {
		name string
		text string
		want []string
	}{
		{"a mapping", "a: 1\nb: 2\n", []string{"a", "b"}},
		{"an anchored mapping", "&x\na: 1\nb: 2\n", []string{"a", "b"}},
		{"a tagged mapping", "!!map\na: 1\nb: 2\n", []string{"a", "b"}},
		{"a sequence", "- a\n- b\n", nil},
		{"a scalar", "a\n", nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := keys(Pairs(body(t, tt.text))); !slices.Equal(got, tt.want) {
				t.Errorf("Pairs = %v, want %v", got, tt.want)
			}
		})
	}
	t.Run("a pair on its own", func(t *testing.T) {
		pair := Pairs(body(t, "a: 1\nb: 2\n"))[1]
		if got := keys(Pairs(pair)); !slices.Equal(got, []string{"b"}) {
			t.Errorf("Pairs = %v, want [b]", got)
		}
	})
}

func TestItems(t *testing.T) {
	tests := []struct {
		name string
		text string
		want int
	}{
		{"a sequence", "- a\n- b\n- c\n", 3},
		{"an anchored sequence", "&x\n- a\n- b\n", 2},
		{"a tagged sequence", "!!seq\n- a\n- b\n", 2},
		{"a mapping", "a: 1\n", 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := len(Items(body(t, tt.text))); got != tt.want {
				t.Errorf("Items has %d nodes, want %d", got, tt.want)
			}
		})
	}
}

func TestLineOf(t *testing.T) {
	pairs := Pairs(body(t, "a: 1\n\nb:\n  c: 2\n"))
	if got := LineOf(pairs[0].Key); got != 1 {
		t.Errorf("LineOf(a) = %d, want 1", got)
	}
	if got := LineOf(Pairs(pairs[1].Value)[0].Key); got != 4 {
		t.Errorf("LineOf(c) = %d, want 4", got)
	}
	if got := LineOf(nil); got != 0 {
		t.Errorf("LineOf(nil) = %d, want 0", got)
	}
}

func TestHoldsAnchor(t *testing.T) {
	tests := []struct {
		name string
		text string
		want bool
	}{
		{"none", "a:\n  b: 1\n", false},
		{"at the top", "a: &x 1\n", true},
		{"deep in a sequence", "a:\n  - b:\n      c: &x 1\n", true},
		{"an alias only", "a: *x\n", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := HoldsAnchor(body(t, tt.text)); got != tt.want {
				t.Errorf("HoldsAnchor = %v, want %v", got, tt.want)
			}
		})
	}
	t.Run("the anchored value itself", func(t *testing.T) {
		value := Pairs(body(t, "a: &x\n  b: 1\n"))[0].Value
		if _, ok := value.(*ast.AnchorNode); !ok {
			t.Fatalf("the value is a %T, not an anchor", value)
		}
		if !HoldsAnchor(value) {
			t.Error("HoldsAnchor = false, want true")
		}
	})
}

func TestVisitorFunc_StopsWhereItReturnsFalse(t *testing.T) {
	n := body(t, "a:\n  b: 1\nc: 2\n")
	var seen []string
	ast.Walk(VisitorFunc(func(n ast.Node) bool {
		mv, ok := n.(*ast.MappingValueNode)
		if !ok {
			return true
		}
		seen = append(seen, mv.Key.String())
		return mv.Key.String() != "a"
	}), n)
	if want := []string{"a", "c"}; !slices.Equal(seen, want) {
		t.Errorf("visited %v, want %v, without going into a", seen, want)
	}
}
