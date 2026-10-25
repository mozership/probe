package safefile

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
)

// checkout makes a working directory with a file outside it and a symlink
// "out" inside it pointing at the outside directory, as a checked-out
// project could carry. It returns the outside file.
func checkout(t *testing.T) (outsideFile string) {
	t.Helper()
	base := t.TempDir()
	outside := filepath.Join(base, "outside")
	project := filepath.Join(base, "project")
	for _, d := range []string{outside, project} {
		if err := os.Mkdir(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	outsideFile = filepath.Join(outside, "victim")
	if err := os.WriteFile(outsideFile, []byte("keep me"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(project, "out")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	t.Chdir(project)
	return outsideFile
}

func unchanged(t *testing.T, path string) {
	t.Helper()
	if data, _ := os.ReadFile(path); string(data) != "keep me" {
		t.Errorf("%s was changed to %.40q", path, data)
	}
}

func writeText(s string) func(io.Writer) error {
	return func(w io.Writer) error {
		_, err := io.WriteString(w, s)
		return err
	}
}

// umaskMode is the mode os.Create gives a new file under the current umask.
func umaskMode(t *testing.T) os.FileMode {
	t.Helper()
	ref := filepath.Join(t.TempDir(), "ref")
	f, err := os.Create(ref)
	if err != nil {
		t.Fatal(err)
	}
	_ = f.Close()
	info, err := os.Stat(ref)
	if err != nil {
		t.Fatal(err)
	}
	return info.Mode().Perm()
}

func TestReplace_RefusesLinkedParentDirectory(t *testing.T) {
	victim := checkout(t)

	err := Replace("out/victim", writeText("report"))
	if err == nil || !strings.Contains(err.Error(), "path escapes") {
		t.Errorf("error = %v, want the linked directory refused", err)
	}
	unchanged(t, victim)

	// A deeper directory created under the link is refused the same way.
	if err := Replace("out/new/report.json", writeText("report")); err == nil {
		t.Error("creating a directory through the link should be refused")
	}
	if _, err := os.Stat(filepath.Join(filepath.Dir(victim), "new")); err == nil {
		t.Error("nothing should have been created outside the project")
	}
}

func TestReplace_ReplacesLinkToDirectory(t *testing.T) {
	checkout(t)

	// "out" itself is a link to a directory: as the final component it is
	// replaced by the file, not refused as a directory.
	if err := Replace("out", writeText("report")); err != nil {
		t.Fatalf("a link at the path should be replaced: %v", err)
	}
	info, err := os.Lstat("out")
	if err != nil {
		t.Fatal(err)
	}
	if !info.Mode().IsRegular() {
		t.Errorf("out is %v, want a regular file", info.Mode())
	}
}

func TestReplace_RefusesRealDirectory(t *testing.T) {
	t.Chdir(t.TempDir())
	if err := os.Mkdir("dir", 0o755); err != nil {
		t.Fatal(err)
	}
	err := Replace("dir", writeText("report"))
	if err == nil || !strings.Contains(err.Error(), "is a directory") {
		t.Errorf("error = %v, want a directory refused", err)
	}
}

func TestReplace_Modes(t *testing.T) {
	t.Chdir(t.TempDir())

	if err := Replace("new.json", writeText("a")); err != nil {
		t.Fatal(err)
	}
	if info, _ := os.Stat("new.json"); info.Mode().Perm() != umaskMode(t) {
		t.Errorf("new file mode = %v, want %v as the umask allows", info.Mode().Perm(), umaskMode(t))
	}

	if err := os.WriteFile("private.json", []byte("old"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod("private.json", 0o600); err != nil {
		t.Fatal(err)
	}
	if err := Replace("private.json", writeText("new")); err != nil {
		t.Fatal(err)
	}
	info, _ := os.Stat("private.json")
	if info.Mode().Perm() != 0o600 {
		t.Errorf("existing file mode = %v, want its 0600 kept", info.Mode().Perm())
	}
	if data, _ := os.ReadFile("private.json"); string(data) != "new" {
		t.Errorf("content = %q", data)
	}
}

func TestReplace_OutsideTheWorkingDirectory(t *testing.T) {
	t.Chdir(t.TempDir())
	outside := filepath.Join(t.TempDir(), "nested", "report.json")

	if err := Replace(outside, writeText("report")); err != nil {
		t.Fatalf("an explicit path outside the working directory should be written: %v", err)
	}
	if data, _ := os.ReadFile(outside); string(data) != "report" {
		t.Errorf("content = %q", data)
	}
}

func TestOpenForAppend_RefusesLinkedParentDirectory(t *testing.T) {
	victim := checkout(t)

	if _, _, err := OpenForAppend("out/victim", true); err == nil {
		t.Error("appending through a linked directory should be refused")
	}
	unchanged(t, victim)

	// Without confine, as for GITHUB_STEP_SUMMARY, the path is used as given.
	f, _, err := OpenForAppend("out/victim", false)
	if err != nil {
		t.Fatalf("an unconfined path should be opened as given: %v", err)
	}
	_ = f.Close()
}

// tempFiles returns the temporary files Replace left next to name in the
// current directory.
func tempFiles(t *testing.T, name string) []string {
	t.Helper()
	left, err := filepath.Glob("." + name + ".tmp*")
	if err != nil {
		t.Fatal(err)
	}
	return left
}

func TestReplace_WriteFails(t *testing.T) {
	t.Chdir(t.TempDir())
	if err := os.WriteFile("report.json", []byte("keep me"), 0o600); err != nil {
		t.Fatal(err)
	}

	// The write gets part of the way before it fails, as one cut short by a
	// full disk would.
	failure := errors.New("disk full")
	err := Replace("report.json", func(w io.Writer) error {
		_, _ = io.WriteString(w, "half a rep")
		return failure
	})
	if !errors.Is(err, failure) {
		t.Errorf("error = %v, want the write's own", err)
	}
	unchanged(t, "report.json")
	if left := tempFiles(t, "report.json"); len(left) != 0 {
		t.Errorf("temporary files left behind: %v", left)
	}

	// A file that did not exist is not created half written.
	if err := Replace("new.json", func(io.Writer) error { return failure }); !errors.Is(err, failure) {
		t.Errorf("error = %v, want the write's own", err)
	}
	if _, err := os.Lstat("new.json"); !os.IsNotExist(err) {
		t.Errorf("new.json exists after a failed write: %v", err)
	}
	if left := tempFiles(t, "new.json"); len(left) != 0 {
		t.Errorf("temporary files left behind: %v", left)
	}
}

func TestReplace_UnwritableDirectory(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root writes to a read-only directory")
	}
	dir := t.TempDir()
	t.Chdir(dir)
	if err := os.WriteFile("report.json", []byte("keep me"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(dir, 0o555); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0o755) })

	if err := Replace("report.json", writeText("report")); !errors.Is(err, fs.ErrPermission) {
		t.Errorf("error = %v, want the temporary file refused", err)
	}
	unchanged(t, "report.json")
}

func TestReplace_OutsideUnderAFile(t *testing.T) {
	// The directory of an explicit path outside the working directory cannot
	// be made where a file already is.
	t.Chdir(t.TempDir())
	file := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(file, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := Replace(filepath.Join(file, "report.json"), writeText("report")); !errors.Is(err, syscall.ENOTDIR) {
		t.Errorf("error = %v, want ENOTDIR for the directory under a file", err)
	}
}

func TestOpenForAppend_Size(t *testing.T) {
	for _, confine := range []bool{true, false} {
		t.Run(fmt.Sprintf("confine=%v", confine), func(t *testing.T) {
			t.Chdir(t.TempDir())
			if err := os.WriteFile("summary.md", []byte("0123456789"), 0o600); err != nil {
				t.Fatal(err)
			}

			f, size, err := OpenForAppend("summary.md", confine)
			if err != nil {
				t.Fatal(err)
			}
			if size != 10 {
				t.Errorf("size = %d, want the 10 bytes the file holds", size)
			}
			if _, err := io.WriteString(f, "more"); err != nil {
				t.Fatal(err)
			}
			_ = f.Close()
			if data, _ := os.ReadFile("summary.md"); string(data) != "0123456789more" {
				t.Errorf("content = %q, want the write appended", data)
			}

			// A missing file is created along with its directory, empty.
			f, size, err = OpenForAppend(filepath.Join("new", "summary.md"), confine)
			if err != nil {
				t.Fatal(err)
			}
			_ = f.Close()
			if size != 0 {
				t.Errorf("size = %d for a new file, want 0", size)
			}
			if _, err := os.Stat(filepath.Join("new", "summary.md")); err != nil {
				t.Errorf("the new file was not created: %v", err)
			}
		})
	}
}

func TestOpenForAppend_Errors(t *testing.T) {
	for _, confine := range []bool{true, false} {
		t.Run(fmt.Sprintf("confine=%v", confine), func(t *testing.T) {
			t.Chdir(t.TempDir())
			if err := os.Mkdir("dir", 0o755); err != nil {
				t.Fatal(err)
			}
			if _, _, err := OpenForAppend("dir", confine); !errors.Is(err, syscall.EISDIR) {
				t.Errorf("error = %v, want EISDIR for a directory", err)
			}

			// The directory of a path outside the working directory cannot
			// be made where a file already is.
			file := filepath.Join(t.TempDir(), "file")
			if err := os.WriteFile(file, nil, 0o600); err != nil {
				t.Fatal(err)
			}
			if _, _, err := OpenForAppend(filepath.Join(file, "summary.md"), confine); !errors.Is(err, syscall.ENOTDIR) {
				t.Errorf("error = %v, want ENOTDIR for the directory under a file", err)
			}
		})
	}
}
