package confine

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
)

const opReadlink = "readlink"

// Readlink answers the target of the link at name, one hop, as the link holds it: not resolved,
// not cleaned and not judged, so a relative target stays relative to the link's own folder, and
// any target may lead outside root; Resolve or Below says where it leads. name is clean and local,
// as Local gives it, and "." is refused. The walk to name's folder follows no link, as NoLinks
// walks: a link, junction or other reparse point at a component before the last is refused,
// matching ErrOutside. On Linux, macOS and the rest a link is a symbolic link; on Windows it is a
// symbolic link or a junction, spelled as os.Readlink spells it (C:\dir, \\host\share\dir, the
// stored text of a relative one, \\?\Volume{...}\ for a mount point of a whole volume).
//
// Each refusal is an *fs.PathError with name slash-separated: a name that is not there matches
// fs.ErrNotExist; one that is there and is no link, a file or a folder, matches fs.ErrInvalid; and
// a link that cannot be read, as on Windows a reparse point that is neither a symbolic link nor a
// junction, or one that changed as it was read, matches ErrOutside and never fs.ErrNotExist.
func Readlink(root *os.Root, name string) (target string, err error) {
	if err := clean(name); err != nil {
		return "", err
	}
	at := filepath.ToSlash(name)
	if name == "." {
		return "", &fs.PathError{Op: opReadlink, Path: at, Err: fmt.Errorf("it is the root, not a link in it: %w", ErrOutside)}
	}
	missing := &fs.PathError{Op: opReadlink, Path: at, Err: fs.ErrNotExist}
	dir, err := walkedFolder(root, filepath.Dir(name))
	if err != nil {
		return "", err
	}
	if dir == nil {
		return "", missing
	}
	defer func() { err = errors.Join(err, dir.Close()) }()
	base := filepath.Base(name)
	look, err := dir.Lstat(base)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return "", missing
	case err != nil:
		return "", err
	case linkKind(look.Mode()) == "":
		return "", &fs.PathError{Op: opReadlink, Path: at, Err: fmt.Errorf("it is no link: %w", fs.ErrInvalid)}
	}
	if target, err = dir.Readlink(base); err != nil {
		return "", otherReparse(opReadlink, at, err)
	}
	return target, nil
}

// walkedFolder opens the folder name in root by the walk, refused where a component is a link or
// changed; nil and no error when name or a folder above it is missing.
func walkedFolder(root *os.Root, name string) (*os.Root, error) {
	if name == "." {
		return root.OpenRoot(".")
	}
	w, err := walkTo(root, name, func(string) {})
	if err != nil || w.dir == nil {
		return nil, err
	}
	if w.info == nil {
		return nil, w.dir.Close()
	}
	if !w.info.IsDir() {
		return nil, errors.Join(&fs.PathError{Op: opWalk, Path: filepath.ToSlash(name), Err: errors.New("it is not a folder")}, w.dir.Close())
	}
	sub, err := next(w.dir, w.name, w.info, filepath.ToSlash(name))
	if err = errors.Join(err, w.dir.Close()); err != nil && sub != nil {
		return nil, errors.Join(err, sub.Close())
	}
	return sub, err
}

// otherReparse is the refusal of name, a link that could not be read, with err's text and not err
// itself, so the refusal never matches fs.ErrNotExist, which os.Readlink answers on Windows for a
// reparse point that is neither a link nor a junction.
func otherReparse(op, name string, err error) error {
	return &fs.PathError{Op: op, Path: name, Err: fmt.Errorf("it is a reparse point that is neither a link nor a junction, or it changed as it was read (%s): %w", err.Error(), ErrOutside)}
}
