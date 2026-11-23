//go:build unix

package file

import (
	"io/fs"
	"os/user"
	"strconv"
	"syscall"
)

// ownerOf returns the names of the user and the group that own what info is
// of, or their numbers where no name is found. Probe is built without cgo,
// so on Linux a name is looked for only in /etc/passwd and /etc/group, not
// in a directory service such as LDAP; on macOS the system is asked.
func ownerOf(info fs.FileInfo) (string, string) {
	st, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return "", ""
	}
	uid := strconv.FormatUint(uint64(st.Uid), 10)
	gid := strconv.FormatUint(uint64(st.Gid), 10)
	owner, group := uid, gid
	if u, err := user.LookupId(uid); err == nil {
		owner = u.Username
	}
	if g, err := user.LookupGroupId(gid); err == nil {
		group = g.Name
	}
	return owner, group
}
