package confine

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"slices"
)

const (
	opCopy = "copy"

	stepCopyLooked = "between the look at a file to copy and its open"
)

// CopyTree copies everything in src into dst, root to root: each folder made by Mkdir's own make
// with 0o700, the owner-only list on Windows, each file written 0o600, or 0o700 where
// IsRunnableFile answers the source runnable, so a program stays runnable, and each link made
// again once every folder and file is there. Each name is walked by handle, with no link
// followed: a folder of src is opened only when it is the folder looked at, and a file is read
// through Open. The modes are set exactly, whatever the umask; on Windows a file takes its
// folder's list, and one at the top of dst takes dst's. Times, owners and a second name, a hard
// link, are not kept: each name is a file of its own.
//
// A link, a junction on Windows among them, is made again where Below, from src's Name, says it
// leads in src, to the same name in dst: a link to a folder as LinkFolder makes one, a junction on
// Windows, and one to a file as LinkFile does, refused with ErrNoFileLink where the system will not
// make one. On Linux, macOS and FreeBSD its target is relative to its own folder, so the copy can
// be moved, and read through an os.Root over it. On Windows its target is absolute, under dst's
// Name, as a junction holds no other, and so is a file link's, so the copy's links lead into it
// only while dst keeps its Name. The target is the link's own spelling, as Below answers it, so a
// copy from a volume that ignores case to one that does not may refuse a link spelt otherwise than
// the name on disk. A link that leads outside src, or through more than 32 links, is refused,
// matching ErrOutside, and one that leads to nothing there is refused, matching fs.ErrNotExist. No
// link of the copy leads outside dst as it is made.
//
// It refuses, before anything is made: a root whose Name is not absolute and clean, matching
// fs.ErrInvalid, as a link made in the copy names its target by it; a root whose Name no longer
// names the folder it holds, and src and dst one folder, matching ErrOutside. While it copies, it
// refuses: a name already in dst, matching fs.ErrExist, and nothing there is changed; dst met in
// the walk of src, matching ErrOutside, as a tree is not copied into itself; and anything but a
// folder, a file or a link, a pipe, a device or a socket, matching fs.ErrInvalid, but a reparse
// point on Windows that is neither a link nor a junction, refused as Below refuses one, matching
// ErrOutside. A refusal of a root is an *fs.PathError naming its Name; one of a name in the tree
// is an *fs.PathError naming its path below the roots, slash-separated, but where the make of a
// link is refused as LinkFolder or LinkFile refuses one, whose own error, an *os.LinkError among
// them, names it. The first error ends the copy, and what it made before stays in dst for the
// caller to remove, with RemoveAll.
//
// A name of src changed while it is copied is copied as it is when read, and a file swapped for
// anything but a file between its look and its open is refused, matching fs.ErrInvalid. A link
// holds its target by name: a folder swapped in at that name later is what the link leads to.
func CopyTree(dst, src *os.Root) error {
	return copyTree(dst, src, func(string) {})
}

// copier is one CopyTree: dst's folder, which the walk of src refuses, the links it found, by
// their name in this system's form, the test's reached, called at each step.
type copier struct {
	dst     fs.FileInfo
	links   []string
	reached func(step string)
}

// copyTree is CopyTree, calling reached at each of its steps, so a test swaps there.
func copyTree(dst, src *os.Root, reached func(step string)) error {
	if err := rootOnPipes(dst, "."); err != nil && dst == nil {
		return err
	}
	to, held, err := rootName(dst)
	if err != nil {
		return err
	}
	from, walked, err := rootName(src)
	if err != nil {
		return err
	}
	if os.SameFile(held, walked) {
		return &fs.PathError{Op: opCopy, Path: from, Err: fmt.Errorf("src and dst are one folder, and a tree is not copied into itself: %w", ErrOutside)}
	}
	c := copier{dst: held, reached: reached}
	if err := c.folder(dst, src, ""); err != nil {
		return err
	}
	for _, at := range c.links {
		if err := relink(dst, src, from, to, at); err != nil {
			return err
		}
	}
	return nil
}

// rootName answers r's Name and its folder, refusing a Name that is not absolute and clean, or
// that names another folder than r holds.
func rootName(r *os.Root) (string, fs.FileInfo, error) {
	name := r.Name()
	if !filepath.IsAbs(name) || filepath.Clean(name) != name {
		return "", nil, &fs.PathError{Op: opCopy, Path: name, Err: fmt.Errorf("a root's name must be absolute and clean, as a link made in the copy names its target by it: %w", fs.ErrInvalid)}
	}
	held, err := r.Stat(".")
	if err != nil {
		return "", nil, err
	}
	named, err := sysStat(name)
	if err == nil && !os.SameFile(held, named) {
		err = &fs.PathError{Op: opCopy, Path: name, Err: fmt.Errorf("the root's name names another folder than the root holds now: %w", ErrOutside)}
	}
	return name, held, err
}

// folder copies everything in s into d, at being s's path below src, slash-separated.
func (c *copier) folder(d, s *os.Root, at string) error {
	f, err := s.Open(".")
	if err != nil {
		return err
	}
	names, err := f.Readdirnames(-1)
	if err = errors.Join(err, f.Close()); err != nil {
		return err
	}
	slices.Sort(names)
	for _, n := range names {
		name := path.Join(at, n)
		look, err := s.Lstat(n)
		switch {
		case err != nil:
			return err
		case linkKind(look.Mode()) != "":
			c.links = append(c.links, filepath.FromSlash(name))
		case look.IsDir():
			err = c.sub(d, s, n, name, look)
		case look.Mode().IsRegular():
			c.reached(stepCopyLooked)
			err = copyFile(d, s, n, name)
		default:
			err = notCopied(name)
		}
		if err != nil {
			return err
		}
	}
	return nil
}

// sub makes the folder n in d, as Mkdir makes one, that the folder look found at n in s, opened
// held to be it, is copied into.
func (c *copier) sub(d, s *os.Root, n, name string, look fs.FileInfo) (err error) {
	if os.SameFile(look, c.dst) {
		return &fs.PathError{Op: opCopy, Path: name, Err: fmt.Errorf("it is dst, and a tree is not copied into itself: %w", ErrOutside)}
	}
	from, err := next(s, n, look, name)
	if err != nil {
		return err
	}
	defer closing(from, &err)
	if err := newFolder(d, n, 0o700, ownerOnly, false); err != nil {
		return atComponent(err, name)
	}
	to, err := into(d, n, name, true, walk{perm: 0o700, kind: ownerOnly, reached: c.reached})
	if err != nil {
		return err
	}
	defer closing(to, &err)
	if err := flushRoot(d); err != nil {
		return err
	}
	return c.folder(to, from, name)
}

// copyFile writes the file n of s at n in d, a name not there, with 0o600, or 0o700 when the
// source is runnable.
func copyFile(d, s *os.Root, n, name string) (err error) {
	in, err := Open(s, n, os.O_RDONLY, 0)
	if err != nil {
		return atComponent(err, name)
	}
	defer closing(in, &err)
	got, err := in.Stat()
	if err != nil {
		return err
	}
	if !got.Mode().IsRegular() {
		return notCopied(name)
	}
	runs, err := IsRunnableFile(in)
	if err != nil {
		return err
	}
	perm := fs.FileMode(0o600)
	if runs {
		perm = 0o700
	}
	out, err := Open(d, n, os.O_WRONLY|os.O_CREATE|os.O_EXCL, perm)
	if err != nil {
		return atComponent(err, name)
	}
	defer closing(out, &err)
	if err := out.Chmod(perm); err != nil {
		return err
	}
	_, err = io.Copy(out, in)
	return err
}

// notCopied is the refusal of name, which is not a folder, a file or a link.
func notCopied(name string) error {
	return &fs.PathError{Op: opCopy, Path: name, Err: fmt.Errorf("it is not a folder, a file or a link, and is not copied: %w", fs.ErrInvalid)}
}

// relink makes the link at in src again at in dst, to the name in dst that Below says it leads
// to in src, from src's Name from, under dst's Name to.
func relink(dst, src *os.Root, from, to, at string) error {
	name := filepath.ToSlash(at)
	in, err := Below(from, filepath.Join(from, at))
	if err != nil {
		return &fs.PathError{Op: opCopy, Path: name, Err: err}
	}
	var look fs.FileInfo
	if in == "." {
		look, err = src.Stat(".")
	} else {
		look, err = NoLinks(src, in)
	}
	switch {
	case err != nil:
		return &fs.PathError{Op: opCopy, Path: name, Err: err}
	case look == nil:
		return &fs.PathError{Op: opCopy, Path: name, Err: fmt.Errorf("it leads to %s, which is not there: %w", filepath.ToSlash(in), fs.ErrNotExist)}
	case look.IsDir() || look.Mode().IsRegular():
		target := filepath.Join(to, in)
		return linkAs(dst, target, linkText(to, at, target), at, look.IsDir())
	}
	return notCopied(name)
}

// linkText is what the link at at in dst holds to lead to target, both under dst's Name to: target
// relative to at's own folder, made by this call with no link in it, so the copy can be moved and
// read through an os.Root over it, or target itself where a link holds only an absolute one.
func linkText(to, at, target string) string {
	if absoluteLinks {
		return target
	}
	rel, err := filepath.Rel(filepath.Join(to, filepath.Dir(at)), target)
	if err != nil {
		return target
	}
	return rel
}
