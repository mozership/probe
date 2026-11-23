// Package textfile reads a text file as the file function of expressions and
// the file action do, and splits text into its lines.
package textfile

import (
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
)

// ErrChanged is the error, wrapped, of a path that came to hold another file
// between being looked at and being opened, as when a log is rotated.
var ErrChanged = errors.New("changed while it was opened")

// Read returns the content of the file at path, relative to the working
// directory, as the paths of actions are. Only a regular file is read, and no
// more of it than limit bytes: a device such as /dev/zero, or a file that
// grows while it is read, would otherwise be read on and on. A larger file is
// an error rather than cut short.
func Read(path string, limit int) (string, error) {
	// The path is looked at before it is opened: opening a FIFO to read
	// waits for a writer.
	info, err := os.Stat(path)
	if err != nil {
		return "", err
	}
	if err := CheckRegular(path, info); err != nil {
		return "", err
	}
	return ReadOpened(path, info, limit)
}

// ReadOpened opens the file at path and reads it, as Read does, once info,
// what was found at path before it was opened, is known to be of a regular
// file. The file opened must be the one info is of.
func ReadOpened(path string, info os.FileInfo, limit int) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer func() { _ = f.Close() }()

	// It is looked at again once it is open, in case it was replaced.
	opened, err := f.Stat()
	if err != nil {
		return "", err
	}
	if err := CheckRegular(path, opened); err != nil {
		return "", err
	}
	if !os.SameFile(info, opened) {
		return "", fmt.Errorf("%s %w", path, ErrChanged)
	}

	data, err := io.ReadAll(io.LimitReader(f, int64(limit)+1))
	if err != nil {
		return "", err
	}
	if len(data) > limit {
		return "", fmt.Errorf("%s exceeds maximum length (%d bytes)", path, limit)
	}
	return string(data), nil
}

// CheckRegular returns an error unless info, of what is at path, is of a
// regular file.
func CheckRegular(path string, info os.FileInfo) error {
	if info.IsDir() {
		return fmt.Errorf("%s is a directory", path)
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("%s is not a regular file", path)
	}
	return nil
}

// Lines returns the lines of s, without their line endings, \n or \r\n. The
// end of the last line is not a line of its own, so the lines of a file are
// those an editor shows, and an empty s has none.
func Lines(s string) []any {
	if s == "" {
		return []any{}
	}
	parts := strings.Split(strings.TrimSuffix(s, "\n"), "\n")
	out := make([]any, len(parts))
	for i, p := range parts {
		out[i] = strings.TrimSuffix(p, "\r")
	}
	return out
}
