//go:build !unix

package file

import "io/fs"

// ownerOf returns no owner or group where the system has none of Unix.
func ownerOf(fs.FileInfo) (string, string) { return "", "" }
