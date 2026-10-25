package procgroup

import "golang.org/x/sys/unix"

// processStartTime returns when the process with pid started, in a form that
// is only meant to be compared with another value from this function.
func processStartTime(pid int) (int64, error) {
	k, err := unix.SysctlKinfoProc("kern.proc.pid", pid)
	if err != nil {
		return 0, err
	}
	tv := k.Proc.P_starttime
	if tv.Sec == 0 && tv.Usec == 0 {
		// The kernel answers an unknown pid with an empty record.
		return 0, unix.ESRCH
	}
	return tv.Sec*1_000_000 + int64(tv.Usec), nil
}
