// Package file is the file action: it looks at what is at a path, and reads
// it when it is a regular file, so that a step can check a log, a file
// written by what is tested, or that a file has come or gone. It writes,
// appends to or deletes a file as well, so that a workflow can make what it
// tests with and remove it, which it refuses under a read-only guard.
package file

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"syscall"
	"time"

	"github.com/mozership/probe/actionrpc"
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
	Path      string `map:"path"`
	Op        string `map:"op"`
	Content   string `map:"content"`
	Mode      string `map:"mode"`
	Recursive bool   `map:"recursive"`
}

// Run does what with asks for at a path, under no guard. See RunGuarded.
func Run(with map[string]any) (map[string]any, error) {
	return RunGuarded(with, actionrpc.Guard{})
}

// RunGuarded does what op of with asks for at the path with gives, and
// returns the step's result: what is at the path afterwards, as read gives
// it. read, the default, only looks; write, append and delete change what
// is at the path, and are refused under a read-only guard before they do.
//
// Nothing at the path is a result for the step's test to check, with
// res.exists false and status 1, so that a step can wait with retry for a
// file to come or to go. What is there is read only when it is a regular
// file; a path that cannot be looked at or read is an error.
func RunGuarded(with map[string]any, guard actionrpc.Guard) (map[string]any, error) {
	start := time.Now()

	req, err := parse(with)
	if err != nil {
		return map[string]any{}, err
	}
	if req.Op != opRead && guard.ReadOnly {
		return map[string]any{}, actionrpc.Refuse("op %s changes what is at %s, and the run is read-only; only read is done", req.Op, req.Path)
	}

	if err := change(req); err != nil {
		return map[string]any{}, err
	}
	res, err := look(req.Path)
	// A file that write or append gave a mode the run cannot read, as
	// "0200", is told as it is without its content: the step did what it
	// was asked, and failing it would have a retry write it again.
	if errors.Is(err, fs.ErrPermission) && req.Op != opRead {
		res, _, err = stat(req.Path)
	}
	if err != nil {
		return map[string]any{}, err
	}
	status := statusMissing
	if res["exists"] == true {
		status = statusFound
	}
	return map[string]any{
		"req":    req.echo(),
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

// missing returns the res of nothing at a path.
func missing() map[string]any {
	return map[string]any{
		"exists": false, "type": "", "content": "",
		"mode": "", "owner": "", "group": "", "size": int64(0), "modified": "",
	}
}

// stat returns the res of what is at path without its content, and what
// it is, nil when nothing is there. A symbolic link is followed, as test -e
// does, so that a link to nothing is nothing.
func stat(path string) (map[string]any, fs.FileInfo, error) {
	info, err := os.Stat(path)
	if isMissing(err) {
		return missing(), nil, nil
	}
	if err != nil {
		return nil, nil, err
	}

	owner, group := ownerOf(info)
	return map[string]any{
		"exists": true, "type": typeOf(info.Mode()), "content": "",
		"mode":     modeOf(info.Mode()),
		"owner":    owner,
		"group":    group,
		"size":     info.Size(),
		"modified": info.ModTime().Format(time.RFC3339Nano),
	}, info, nil
}

// lookOnce looks at path once, as look does.
func lookOnce(path string) (map[string]any, error) {
	res, info, err := stat(path)
	if err != nil || info == nil || !info.Mode().IsRegular() {
		return res, err
	}
	content, err := textfile.ReadOpened(path, info, maxContent)
	// A file removed after it was looked at is gone by the time it would
	// be read, as if it had never been there.
	if isMissing(err) {
		return missing(), nil
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

// modeOf returns the permissions of mode as chmod takes them in octal, with
// the setuid, setgid and sticky bits, as "0644" or "4755".
func modeOf(mode fs.FileMode) string {
	m := uint32(mode.Perm())
	if mode&fs.ModeSetuid != 0 {
		m |= 0o4000
	}
	if mode&fs.ModeSetgid != 0 {
		m |= 0o2000
	}
	if mode&fs.ModeSticky != 0 {
		m |= 0o1000
	}
	return fmt.Sprintf("%04o", m)
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
