package shell

import (
	"bytes"
	"io"
	"os"
	"time"
)

// readyPoll is how often the log of a background command is read while the
// step waits for it to be ready.
var readyPoll = 50 * time.Millisecond

// readyState is how a wait for a background command to be ready ended.
type readyState int

const (
	ready         readyState = iota // The command wrote the text
	readyExited                     // It exited, and nothing it started is left, first
	readyTimedOut                   // The timeout passed first
)

// waitReady waits until the log at path holds text, the command whose shell
// is pid has exited with nothing left of its process group, or timeout has
// passed. exited is closed once the shell has exited.
//
// A shell that exits while its process group lives on, as `server &` does,
// is waited for still: what it started may write the text later. A process
// that leaves the group, as one that calls setsid to daemonize does, is not
// followed.
func waitReady(path, text string, pid int, exited <-chan struct{}, timeout time.Duration) readyState {
	m := &logMatcher{path: path, text: []byte(text)}
	deadline := time.NewTimer(timeout)
	defer deadline.Stop()
	tick := time.NewTicker(readyPoll)
	defer tick.Stop()

	shellExited := false
	for {
		if m.found() {
			return ready
		}
		if shellExited && !groupAlive(pid) {
			// Whatever the group wrote before it ended has been read above
			// only if it was there then; read once more.
			if m.found() {
				return ready
			}
			return readyExited
		}
		select {
		case <-exited:
			shellExited = true
			// A closed channel is always ready; it is not selected again.
			exited = nil
		case <-tick.C:
		case <-deadline.C:
			if m.found() {
				return ready
			}
			// The shell may have exited before the deadline although this
			// case was chosen: select picks at random among the ready ones.
			if !shellExited {
				select {
				case <-exited:
					shellExited = true
				default:
				}
			}
			if shellExited && !groupAlive(pid) {
				return readyExited
			}
			return readyTimedOut
		}
	}
}

// logMatcher reads a growing log from where it last stopped, and looks for
// text in it, also across the boundary between two reads.
type logMatcher struct {
	path   string
	text   []byte
	offset int64
	tail   []byte
}

func (m *logMatcher) found() bool {
	f, err := os.Open(m.path)
	if err != nil {
		return false
	}
	defer func() { _ = f.Close() }()
	if _, err := f.Seek(m.offset, io.SeekStart); err != nil {
		return false
	}
	data, err := io.ReadAll(f)
	if err != nil || len(data) == 0 {
		return false
	}
	m.offset += int64(len(data))

	buf := append(m.tail, data...)
	if bytes.Contains(buf, m.text) {
		return true
	}
	keep := min(len(m.text)-1, len(buf))
	m.tail = append([]byte(nil), buf[len(buf)-keep:]...)
	return false
}

// readLog returns what a background command has written to its log, for a
// step that failed waiting for it, or an empty string when it cannot be read.
func readLog(path string) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return string(data)
}
