// Package file is the file action: it looks at what is at a path, and reads
// it when it is a regular file, so that a step can check a log, a file
// written by what is tested, or that a file has come or gone.
package file

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"syscall"
	"time"

	"github.com/mozership/probe/internal/mapping"
	"github.com/mozership/probe/internal/textfile"
)

const (
	// maxContent is as much of a file as the file function of expressions
	// reads: a larger file is an error rather than cut short.
	maxContent    = 1000000
	statusFound   = 0
	statusMissing = 1
)

// The types of what is at a path, as res.type gives them.
const (
	typeFile   = "file"
	typeDir    = "dir"
	typeSocket = "socket"
	typePipe   = "pipe"
	typeDevice = "device"
	typeOther  = "other"
)

type Req struct {
	Path string `map:"path"`
}

// Read looks at what is at the path with gives, and returns the step's
// result. Nothing at the path is a result for the step's test to check, with
// res.exists false and status 1, so that a step can wait with retry for a
// file to come or to go. What is there is read only when it is a regular
// file; a path that cannot be looked at or read is an error.
func Read(with map[string]any) (map[string]any, error) {
	start := time.Now()

	req := &Req{}
	if err := mapping.MapToStructByTags(with, req); err != nil {
		return map[string]any{}, fmt.Errorf("failed to parse request: %w", err)
	}
	if req.Path == "" {
		return map[string]any{}, errors.New("path parameter is required")
	}

	res, err := look(req.Path)
	if err != nil {
		return map[string]any{}, err
	}
	status := statusMissing
	if res["exists"] == true {
		status = statusFound
	}
	return map[string]any{
		"req":    map[string]any{"path": req.Path},
		"res":    res,
		"rt":     time.Since(start).String(),
		"status": status,
	}, nil
}

// lookAttempts is how many times a path that comes to hold another file
// between being looked at and being read, as a rotated log does, is looked
// at before it is an error.
const lookAttempts = 3

// look returns the res of what is at path. The content is that of a regular
// file as it is, never read as JSON: it is not given as body, which the
// runner reads as JSON when it looks like JSON, so that a log of JSON lines,
// or one that starts with [ and ends with ], is text.
func look(path string) (map[string]any, error) {
	for attempt := 1; ; attempt++ {
		res, err := lookAt(path)
		if errors.Is(err, textfile.ErrChanged) && attempt < lookAttempts {
			continue
		}
		return res, err
	}
}

// lookAt looks at path once, as look does. The tests replace it.
var lookAt = lookOnce

// lookOnce looks at path once, as look does.
func lookOnce(path string) (map[string]any, error) {
	missing := map[string]any{"exists": false, "type": "", "content": ""}

	// A symbolic link is followed, as test -e does, so that a link to
	// nothing is nothing.
	info, err := os.Stat(path)
	if isMissing(err) {
		return missing, nil
	}
	if err != nil {
		return nil, err
	}

	res := map[string]any{"exists": true, "type": typeOf(info.Mode()), "content": ""}
	if !info.Mode().IsRegular() {
		return res, nil
	}
	content, err := textfile.ReadOpened(path, info, maxContent)
	// A file removed after it was looked at is gone by the time it would
	// be read, as if it had never been there.
	if isMissing(err) {
		return missing, nil
	}
	if err != nil {
		return nil, err
	}
	res["content"] = content
	return res, nil
}

// isMissing says whether err is that of a path at which there is nothing,
// such as one under a regular file.
func isMissing(err error) bool {
	return errors.Is(err, fs.ErrNotExist) || errors.Is(err, syscall.ENOTDIR)
}

// typeOf returns the type of what has mode, as res.type gives it.
func typeOf(mode fs.FileMode) string {
	switch {
	case mode.IsRegular():
		return typeFile
	case mode.IsDir():
		return typeDir
	case mode&fs.ModeSocket != 0:
		return typeSocket
	case mode&fs.ModeNamedPipe != 0:
		return typePipe
	case mode&fs.ModeDevice != 0:
		return typeDevice
	default:
		return typeOther
	}
}
