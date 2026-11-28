// Package fileid tells whether a file's 64-bit index can tell it apart from another on its volume,
// the identity os.SameFile reads. A Windows volume that gives a file no such index, a zero index (a
// WebDAV drive, some SMB servers) or an all-ones one (ReFS, whose real file ID is 128-bit and does
// not fit the 64 bits read), makes os.SameFile call any two files the same, so a reader of a path
// cannot see one swapped in at the name. write, confine, lock, appdirs, treesum and trust share this
// one refusal rather than each trust the compare. On Unix every file has
// an inode os.SameFile can trust, so Indexed never refuses there. It holds too the one rule trust,
// confine and appdirs share for a path that names no file at all, DevicePath: a Windows device or
// pipe name, which no file index can be read for, refused before it is opened.
// It imports the standard library alone, syscall reading the Windows index.
package fileid

import (
	"errors"
	"fmt"
)

// ErrNoFileIndex is the refusal of a Windows file whose 64-bit index reads zero, which every file on
// a volume with no file IDs (a WebDAV drive, some SMB servers) reads, or all-ones, which ReFS gives
// some files, its real file ID being 128-bit. A caller wraps it with the name and the step it was
// taking. It matches errors.ErrUnsupported.
var ErrNoFileIndex = fmt.Errorf("a file's index is zero or all-ones, so os.SameFile cannot tell it apart from another put in its place: %w", errors.ErrUnsupported)

// Known reports whether a 64-bit file index tells a file apart from another. A zero index (which every
// file on a volume with no file IDs, a WebDAV drive or some SMB servers, reads) and an all-ones index
// (which ReFS gives some files, its real file ID being 128-bit and not fitting the 64 bits os.SameFile
// reads) each tell nothing: os.SameFile would call any two such files the same.
func Known(hi, lo uint32) bool {
	switch {
	case hi == 0 && lo == 0:
		return false
	case hi == 0xFFFFFFFF && lo == 0xFFFFFFFF:
		return false
	}
	return true
}
