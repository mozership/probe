package engine

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/mozership/probe/internal/actions/file"
)

// TestFileContentIsNotReadAsJSON checks that what the file action reads
// stays the text of the file once the runner has read the result, which
// reads a body that looks like JSON as the value it holds: a log of JSON
// lines, or a line that starts with [ and ends with ], is text to search.
func TestFileContentIsNotReadAsJSON(t *testing.T) {
	dir := t.TempDir()
	for _, content := range []string{
		`{"orders":[1,2,3]}`,
		"{\"level\":\"INFO\"}\n{\"level\":\"ERROR\"}\n",
		"[INFO] ready [pid 42]",
	} {
		path := filepath.Join(dir, "f")
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
		ret, err := file.Run(map[string]any{"path": path})
		if err != nil {
			t.Fatal(err)
		}
		r := ReadActionResult(ret)
		if r.Res["content"] != content {
			t.Errorf("res.content = %#v, want the text %q", r.Res["content"], content)
		}
		if _, ok := r.Res["rawbody"]; ok {
			t.Errorf("res = %#v, want no rawbody", r.Res)
		}
	}
}
