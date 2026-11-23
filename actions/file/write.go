package file

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"

	"github.com/mozership/probe/internal/mapping"
)

// The ops of a file step, as op takes them.
const (
	opRead   = "read"
	opWrite  = "write"
	opAppend = "append"
	opDelete = "delete"
)

// newFileMode is the mode a file that write or append makes is given when
// mode does not say, before the umask of the run, as a shell gives one.
const newFileMode = 0o644

// newDirMode is the mode of a directory that write or append makes to hold
// a file, before the umask of the run, as mkdir -p gives one.
const newDirMode = 0o755

// parse reads the request from with, and checks that it asks for an op that
// is there, with only the parameters that op takes.
func parse(with map[string]any) (*Req, error) {
	// A mode written as a number in YAML, as 0600, is read as decimal or as
	// octal depending on the parser, so it is taken only as a string.
	if m, ok := with["mode"]; ok {
		if _, isString := m.(string); !isString {
			return nil, fmt.Errorf("mode must be a string of octal digits in quotes, as \"0600\", not %v", m)
		}
	}

	// content is written as text, so a map or a list, as a template of a
	// whole JSON body gives, is an error rather than Go's form of it.
	if c, ok := with["content"]; ok {
		switch c.(type) {
		case string, bool, int, int64, float64:
		default:
			return nil, fmt.Errorf("content must be text, not %T; encode a map or a list as text, as JSON, first", c)
		}
	}

	req := &Req{}
	if err := mapping.MapToStructByTags(with, req); err != nil {
		return nil, fmt.Errorf("failed to parse request: %w", err)
	}
	if req.Path == "" {
		return nil, errors.New("path parameter is required")
	}
	if req.Op == "" {
		req.Op = opRead
	}

	writes := req.Op == opWrite || req.Op == opAppend
	switch req.Op {
	case opRead, opWrite, opAppend, opDelete:
	default:
		return nil, fmt.Errorf("op %q is not one of read, write, append and delete", req.Op)
	}
	if _, ok := with["content"]; ok && !writes {
		return nil, fmt.Errorf("content is written only by op write or append, not %s", req.Op)
	}
	if _, ok := with["mode"]; ok && !writes {
		return nil, fmt.Errorf("mode is set only by op write or append, not %s", req.Op)
	}
	if _, ok := with["recursive"]; ok && req.Op != opDelete {
		return nil, fmt.Errorf("recursive is taken only by op delete, not %s", req.Op)
	}
	if writes && (strings.HasSuffix(req.Path, "/") || strings.HasSuffix(req.Path, string(filepath.Separator))) {
		return nil, fmt.Errorf("path %s ends with a separator, and names no file to write", req.Path)
	}
	if req.Mode != "" {
		if _, err := parseMode(req.Mode); err != nil {
			return nil, err
		}
	}
	return req, nil
}

// echo returns the req of the step's result.
func (r *Req) echo() map[string]any {
	return map[string]any{
		"path":      r.Path,
		"op":        r.Op,
		"content":   r.Content,
		"mode":      r.Mode,
		"recursive": r.Recursive,
	}
}

// change does what the op of req asks for at its path, and nothing for
// read.
func change(req *Req) error {
	switch req.Op {
	case opWrite:
		return write(req, os.O_WRONLY|os.O_CREATE|os.O_TRUNC)
	case opAppend:
		return write(req, os.O_WRONLY|os.O_CREATE|os.O_APPEND)
	case opDelete:
		return remove(req.Path, req.Recursive)
	default:
		return nil
	}
}

// write writes the content of req to its path, opened with flag, making the
// directories that are to hold it, and sets its mode when req gives one. A
// symbolic link is followed, as a shell's > does. What is not a regular
// file is not written: a pipe would wait for a reader, and a directory
// cannot be.
func write(req *Req, flag int) error {
	if info, err := os.Stat(req.Path); err == nil && !info.Mode().IsRegular() {
		return fmt.Errorf("%s is a %s, not a file, and is not written", req.Path, typeOf(info.Mode()))
	}
	if err := os.MkdirAll(filepath.Dir(req.Path), newDirMode); err != nil {
		return err
	}
	// O_NONBLOCK keeps a pipe put at the path since it was looked at from
	// blocking the open; a regular file is written as it is without it.
	f, err := os.OpenFile(req.Path, flag|syscall.O_NONBLOCK, newFileMode)
	if err != nil {
		return err
	}
	info, err := f.Stat()
	if err == nil && !info.Mode().IsRegular() {
		err = fmt.Errorf("%s is a %s, not a file, and is not written", req.Path, typeOf(info.Mode()))
	}
	if err == nil {
		_, err = f.WriteString(req.Content)
	}
	if err == nil && req.Mode != "" {
		mode, _ := parseMode(req.Mode)
		err = f.Chmod(mode)
	}
	if closeErr := f.Close(); err == nil {
		err = closeErr
	}
	return err
}

// remove deletes what is at path, taken as its absolute path, cleaned, so
// that what is checked is what is deleted: .. is the directory that holds
// the one named before it, as it is written, and a trailing separator
// does not make a symbolic link stand for what it points to. A symbolic link is deleted, not what it
// points to. A directory is deleted only when it is empty, or with
// recursive along with all it holds. Nothing at the path is not an error,
// as rm -f does, so that a step can remove what may not be there. The
// working directory, or one that holds it, such as /, is never deleted,
// so that a path made of a variable that is empty does not remove it.
func remove(path string, recursive bool) error {
	abs, err := filepath.Abs(path)
	if err != nil {
		return err
	}
	if err := checkRemovable(path, abs); err != nil {
		return err
	}
	path = abs
	info, err := os.Lstat(path)
	if isMissing(err) {
		return nil
	}
	if err != nil {
		return err
	}
	if info.IsDir() && recursive {
		return os.RemoveAll(path)
	}
	err = os.Remove(path)
	// A directory that is not empty is fs.ErrExist on Unix and on Windows.
	if info.IsDir() && errors.Is(err, fs.ErrExist) {
		return fmt.Errorf("%s is a directory that is not empty; set recursive to delete what it holds", path)
	}
	return err
}

// checkRemovable returns an error when abs, the absolute path of path, is
// the working directory, or one that holds it. Each is taken as it is
// written and with its symbolic links followed, so that a working directory
// reached through a link, as /tmp is on macOS, is kept whichever way a path
// names it.
func checkRemovable(path, abs string) error {
	targets := []string{abs, resolveDir(abs)}
	if resolved, err := filepath.EvalSymlinks(abs); err == nil {
		targets = append(targets, resolved)
	}
	wd, err := os.Getwd()
	if err != nil {
		return err
	}
	wds := []string{wd}
	if resolved, err := filepath.EvalSymlinks(wd); err == nil {
		wds = append(wds, resolved)
	}
	for _, target := range targets {
		for _, wd := range wds {
			if holds(target, wd) {
				return fmt.Errorf("%s holds the working directory, and is not deleted", path)
			}
		}
	}
	return nil
}

// holds says whether dir is path, or a directory that holds it.
func holds(dir, path string) bool {
	rel, err := filepath.Rel(dir, path)
	if err != nil {
		return false
	}
	return rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

// resolveDir returns abs with the symbolic links of the directories that
// hold it followed, but not one at abs itself, which delete removes rather
// than what it points to.
func resolveDir(abs string) string {
	dir, base := filepath.Dir(abs), filepath.Base(abs)
	if dir == abs {
		return abs
	}
	if resolved, err := filepath.EvalSymlinks(dir); err == nil {
		dir = resolved
	}
	return filepath.Join(dir, base)
}

// parseMode reads mode, octal digits as chmod takes them, such as "0600"
// or "4755", into a file mode with the setuid, setgid and sticky bits.
func parseMode(mode string) (fs.FileMode, error) {
	m, err := strconv.ParseUint(mode, 8, 32)
	if err != nil || m > 0o7777 {
		return 0, fmt.Errorf("mode %q is not octal digits as chmod takes them, as \"0644\" or \"4755\"", mode)
	}
	fm := fs.FileMode(m & 0o777)
	if m&0o4000 != 0 {
		fm |= fs.ModeSetuid
	}
	if m&0o2000 != 0 {
		fm |= fs.ModeSetgid
	}
	if m&0o1000 != 0 {
		fm |= fs.ModeSticky
	}
	return fm, nil
}
