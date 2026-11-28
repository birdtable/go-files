//go:build unix

package trust

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"strconv"
	"syscall"
)

// local is this system's host: names read with lstat, files opened without following a link or
// waiting, root and this account's effective id trusted; on Linux, the overflow account, never this one.
func local() host {
	h := host{look: look, ident: ident, readlink: os.Readlink, open: openRead, stat: statFile, mask: aclMaskFile, mountRoot: mountRoot,
		root: []string{"uid:0"}, me: "uid:" + strconv.Itoa(os.Geteuid())}
	if overflowUID != "" {
		h.overflow, h.broken = overflowOf(h.me, overflowUID, readProc)
	}
	return h
}

// look reads the facts of the one name path, following no link: on macOS its access list and
// whether its volume ignores ownership, and on Linux, for a name its group can write and others
// cannot, whether it holds one, and on an NFS mount its NFSv4 list; on both, who mounted a FUSE
// file system.
func look(path string) (node, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return node{}, err
	}
	n, err := nodeOf(info, path)
	if err == nil {
		n, err = listOf(n, path, info)
	}
	if err == nil {
		n, err = mountOf(n, path)
	}
	if err != nil || n.perm.Perm()&0o022 != 0o020 {
		return n, err
	}
	n.acl, err = aclMask(path)
	return n, err
}

// ident reads the file ID of what path names, following its links, with stat.
func ident(path string) (fileID, error) {
	info, err := os.Stat(path)
	if err != nil {
		return fileID{}, err
	}
	n, err := nodeOf(info, path)
	return n.id, err
}

func nodeOf(info fs.FileInfo, path string) (node, error) {
	st, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return node{}, &fs.PathError{Op: "stat", Path: path, Err: errors.New("this system's stat gives no owner")}
	}
	m := info.Mode()
	return node{id: fileID{vol: bits(st.Dev), idx: bits(st.Ino)}, typ: m.Type(), owner: "uid:" + strconv.FormatUint(uint64(st.Uid), 10),
		perm: m.Perm() | m&fs.ModeSticky, gid: st.Gid, size: info.Size()}, nil
}

// bits is a device or inode number as its 64 bits, whichever integer type this system's stat
// gives it: a signed one is taken as its bits, the same for every name, so equal numbers stay equal.
func bits[T ~int32 | ~uint32 | ~int64 | ~uint64](v T) uint64 { return uint64(v) }

// openRead opens path to be read, never waiting for a writer (O_NONBLOCK, so a pipe at the name
// answers at once) and refusing a link at the name (O_NOFOLLOW). A link, or a socket, which cannot
// be opened at all (ENXIO on Linux, EOPNOTSUPP on macOS), is refused with ErrNotPlain.
func openRead(path string) (*os.File, error) {
	f, err := os.OpenFile(path, os.O_RDONLY|syscall.O_NONBLOCK|syscall.O_NOFOLLOW, 0) //nolint:gosec // G304: ReadOwned reads the path its caller names, and checks this descriptor
	switch {
	case errors.Is(err, syscall.ELOOP):
		return nil, refuse(path, path+" is a link, and none is followed", Fix{}, ErrNotPlain)
	case errors.Is(err, syscall.ENXIO), errors.Is(err, syscall.EOPNOTSUPP):
		return nil, refuse(path, fmt.Sprintf("%s cannot be opened to be read (%v), as a socket cannot", path, err), Fix{}, ErrNotPlain)
	}
	return f, err
}

func statFile(f *os.File) (node, error) {
	info, err := f.Stat()
	if err != nil {
		return node{}, err
	}
	n, err := nodeOf(info, f.Name())
	if err != nil {
		return node{}, err
	}
	// On macOS the list is read by f.Name(): a name that now names nothing, as one moved away or
	// removed, or one relative to a folder left by a chdir, is not f gone, but a name that no
	// longer leads to it, read again as one that names another file.
	n, err = listOf(n, f.Name(), info)
	if errors.Is(err, fs.ErrNotExist) {
		return node{}, errMoved
	}
	if err != nil {
		return node{}, err
	}
	return mountOfFile(n, f)
}

// ownerOf is f's owner, from the stat of its descriptor.
func ownerOf(f *os.File) (string, error) {
	info, err := f.Stat()
	if err != nil {
		return "", err
	}
	n, err := nodeOf(info, f.Name())
	return n.owner, err
}

// bySpelling is false: a .. in a link's target, or after a link, goes up from the folder the walk
// reached through the link, so no path is cleaned before its links are read.
const bySpelling = false

// nameThere is whether the one name path is there, read by lstat: nil where it is, an error
// matching fs.ErrNotExist where it is not.
func nameThere(path string) error {
	_, err := os.Lstat(path)
	return err
}
