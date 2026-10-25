// Package safefile writes files that a symlink planted in the current
// directory must not redirect.
package safefile

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// The files Probe writes by default, reports and the agent skill, sit in the
// current directory, which is often a checked-out project that Probe did not
// write. A symlink planted there, as the file or as any directory on the way
// to it, must not redirect a write to a file elsewhere. So a path inside the
// current directory is resolved through an os.Root opened on it, which
// refuses every link that leads out. A path outside it, absolute or through
// "..", is one the user chose explicitly: its directory is used as given, and
// only the file itself is resolved within that directory.

// openTarget returns the root to resolve path in and the name of path within
// it.
func openTarget(path string) (*os.Root, string, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return nil, "", err
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return nil, "", err
	}

	if rel, err := filepath.Rel(cwd, abs); err == nil && rel != ".." &&
		!strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		root, err := os.OpenRoot(cwd)
		if err != nil {
			return nil, "", err
		}
		return root, rel, nil
	}

	dir := filepath.Dir(abs)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, "", err
	}
	root, err := os.OpenRoot(dir)
	if err != nil {
		return nil, "", err
	}
	return root, filepath.Base(abs), nil
}

// mkdirParent creates the directory name lives in, inside root.
func mkdirParent(root *os.Root, name string) error {
	if dir := filepath.Dir(name); dir != "." {
		return root.MkdirAll(dir, 0o755)
	}
	return nil
}

// Replace writes path with write, by writing a temporary file next to it
// and renaming it over. A rename replaces the directory entry, so a symlink
// at path is replaced rather than written through. A new file gets the mode
// the umask allows, as os.Create would give it; an existing regular file
// keeps its own, so a report someone made private stays private.
func Replace(path string, write func(io.Writer) error) error {
	root, name, err := openTarget(path)
	if err != nil {
		return err
	}
	defer func() { _ = root.Close() }()

	if err := mkdirParent(root, name); err != nil {
		return err
	}

	var keepMode os.FileMode
	if info, err := root.Lstat(name); err == nil {
		switch {
		case info.IsDir():
			return fmt.Errorf("%s is a directory", path)
		case info.Mode().IsRegular():
			keepMode = info.Mode().Perm()
		}
	}

	suffix := make([]byte, 6)
	if _, err := rand.Read(suffix); err != nil {
		return err
	}
	tmp := filepath.Join(filepath.Dir(name), "."+filepath.Base(name)+".tmp"+hex.EncodeToString(suffix))

	f, err := root.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o666)
	if err != nil {
		return err
	}
	err = write(f)
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err == nil && keepMode != 0 {
		err = root.Chmod(tmp, keepMode)
	}
	if err == nil {
		err = root.Rename(tmp, name)
	}
	if err != nil {
		_ = root.Remove(tmp)
		return err
	}
	return nil
}

// OpenForAppend opens path to append to, creating it and its directory as
// needed, and returns its size before the append. With confine, the path is
// resolved as openTarget resolves it, so a link leading out of the current
// directory is refused; without it, the path is used as given.
func OpenForAppend(path string, confine bool) (*os.File, int64, error) {
	if !confine {
		if dir := filepath.Dir(path); dir != "." {
			if err := os.MkdirAll(dir, 0o755); err != nil {
				return nil, 0, err
			}
		}
		f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o666)
		if err != nil {
			return nil, 0, err
		}
		return f, fileSize(f), nil
	}

	root, name, err := openTarget(path)
	if err != nil {
		return nil, 0, err
	}
	defer func() { _ = root.Close() }()

	if err := mkdirParent(root, name); err != nil {
		return nil, 0, err
	}
	f, err := root.OpenFile(name, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o666)
	if err != nil {
		return nil, 0, err
	}
	return f, fileSize(f), nil
}

func fileSize(f *os.File) int64 {
	if info, err := f.Stat(); err == nil {
		return info.Size()
	}
	return 0
}
