package procgroup

import (
	"fmt"
	"os"
	"strconv"
	"strings"
)

// processStartTime returns when the process with pid started, in a form that
// is only meant to be compared with another value from this function.
func processStartTime(pid int) (int64, error) {
	stat, err := os.ReadFile(fmt.Sprintf("/proc/%d/stat", pid))
	if err != nil {
		return 0, err
	}
	// The command name is in parentheses and can contain spaces, so the
	// fields are counted from the last closing one. starttime is field 22,
	// which is the 20th after it.
	s := string(stat)
	i := strings.LastIndexByte(s, ')')
	if i < 0 {
		return 0, fmt.Errorf("unexpected /proc/%d/stat: %q", pid, s)
	}
	fields := strings.Fields(s[i+1:])
	if len(fields) < 20 {
		return 0, fmt.Errorf("unexpected /proc/%d/stat: %q", pid, s)
	}
	return strconv.ParseInt(fields[19], 10, 64)
}
