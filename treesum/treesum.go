// Package treesum gives a pinned digest of a folder tree beneath an os.Root, and checks a tree
// against one, with no link followed.
//
// Sum walks the tree by handle through confine, hashes each file, and takes each link as the
// text of its target, never as what it points at. It refuses a link that leads out of the tree,
// a loop of links, a device, a FIFO, a socket, a reparse point that is not a link and a file
// with a second name. Check is a look before a use: a caller that must hold the tree between the
// two opens through confine after it. The README holds the listing's format byte for byte.
//
// Built on SHA-256, a listing in the manner of golang.org/x/mod/sumdb/dirhash, and confine's open,
// which follows no link. It adds a link taken as its target's text, never followed, and the refusal
// of what a pinned tree should not hold. The program configures the root, the name and the Caps.
// It imports the standard library, confine and internal/fileid.
package treesum

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"math"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/birdtable/go-files/confine"
)

// prefix starts every digest, so it is never taken for go.sum's h1:, and names the listing's
// format: t1: listed whether a file was executable, which Windows does not report.
const prefix = "t2:"

const opSum, opCheck = "sum", "check"

// The steps where another process could change the tree, for a test's reached.
const stepFolder, stepFile = "between the look at a folder and its open", "between the look at a file and its open"

// hops is the most links one link's target is resolved through, as Linux's SYMLOOP_MAX, and
// longest the most bytes of an entry's path from the top, the longest path Linux opens by name.
const hops, longest = 40, 4095

// perEntry is the steps of the links' resolution each entry Caps.Entries allows, as Caps says.
const perEntry = 1024

func budget(entries int) int { return min(entries, math.MaxInt/perEntry) * perEntry }

// ErrCap is matched by a refusal of a tree that holds more entries or bytes than Caps allows.
var ErrCap = errors.New("the tree holds more than its caps allow")

// ErrKind is matched by a refusal of an entry the digest does not take: a device, a FIFO, a
// socket, or a name that is not UTF-8 text with no control character.
var ErrKind = errors.New("the tree holds an entry the digest does not take")

// ErrDiffers is matched by Check's refusal: the tree differs from the listing, or the listing
// does not hash to the digest.
var ErrDiffers = errors.New("the tree is not the tree pinned")

// ErrVersion is matched by Check's refusal of a digest of another format than Sum gives, as a
// t1: digest, which listed whether each file was executable. It says nothing about the tree, as
// it comes before the tree is read: such a tree is checked first with the release that pinned it,
// or fetched again from where the pin was made, and only then pinned again with Sum.
var ErrVersion = errors.New("the digest is of another format than this version gives")

// Caps bounds a walk, so a hostile tree cannot make it run without end. Both are the caller's
// to set: a tree with more entries, its folders, files and links with the top folder aside, or
// more bytes in its files, is refused with ErrCap, and so is one whose links take more than
// 1024 steps for each entry Entries allows to resolve, a step being a component of a target or
// a folder of the path it is read from, and one holding a path of more than 4095 bytes, so the
// listing grows with Entries alone and the walk holds at most 2048 folders open at once.
//
// Skip, when set, is asked of each name a listing holds, by its slash path from the top, before
// anything about it is looked at: a name it answers true for is left out of the listing, and is
// never opened, counted or refused, nor anything below it. A link whose target reaches a name left
// out, or a missing name in a folder that left one out, is refused, as the digest does not cover it,
// and so, once any name is left out, is one whose target goes on below a missing name, directly or
// through another link: a volume that folds case or Unicode forms may read that name as a kept
// folder's, and what lies below it as a name left out there.
type Caps struct {
	Entries int
	Bytes   int64
	Skip    func(rel string) bool
}

// entry is one line of the listing: a folder, a file with its hash, or a link with the text of
// its target. No mode bit is listed, as Windows reports none.
type entry struct {
	path, kind, text string
}

// Sum gives the digest of the folder tree at name in root and the listing it hashes, in the
// README's format. name is clean and local, as confine.Local gives it; "." is the root. A
// refusal of a path is an *fs.PathError naming the entry, slash-separated from the tree's top;
// a link that leads out of the tree or loops, a link or reparse point on the way, and a file
// with a second name, match confine.ErrOutside.
func Sum(root *os.Root, name string, caps Caps) (string, []byte, error) {
	return sum(root, name, caps, func(string, string) {})
}

// Check refuses the tree at name in root, with an error matching ErrDiffers, unless it is the
// tree listing lists and listing hashes to digest, both as Sum gave them. The refusal of a tree
// that differs is an *fs.PathError naming the first entry, in the listing's order, that differs.
// It refuses what Sum refuses, and a digest whose format, the text before its first colon, is not
// Sum's with an error matching ErrVersion that names it, before it reads the tree.
func Check(root *os.Root, name string, caps Caps, digest string, listing []byte) error {
	if format, _, ok := strings.Cut(digest, ":"); ok && format+":" != prefix {
		return fmt.Errorf("the digest %q is of the format %q, and this version reads %q alone: %w",
			digest, format+":", prefix, ErrVersion)
	}
	if hashOf(listing) != digest {
		return fmt.Errorf("the listing does not hash to the digest %q: %w", digest, ErrDiffers)
	}
	_, got, err := Sum(root, name, caps)
	if err != nil {
		return err
	}
	want, have := slices.Collect(strings.Lines(string(listing))), slices.Collect(strings.Lines(string(got)))
	for i := 0; i < len(want) || i < len(have); i++ {
		switch {
		case i == len(want):
			return differs(have[i], "is in the tree and not in the listing")
		case i == len(have):
			return differs(want[i], "is in the listing and not in the tree")
		case want[i] != have[i]:
			return differs(min(pathOf(want[i]), pathOf(have[i])), "is not the entry the listing holds")
		}
	}
	return nil
}

// pathOf is the path field of a listing line, its second.
func pathOf(line string) string {
	f := strings.SplitN(line, "\t", 3)
	return f[min(1, len(f)-1)] // a line with no tab is its own path
}

func differs(line, why string) error {
	return &fs.PathError{Op: opCheck, Path: pathOf(line), Err: fmt.Errorf("it %s: %w", why, ErrDiffers)}
}

func hashOf(listing []byte) string {
	h := sha256.Sum256(listing)
	return prefix + hex.EncodeToString(h[:])
}

// walk is one Sum's state: the caps left and the entries found.
type walk struct {
	entries, steps int
	bytes          int64
	found          []entry
	kinds          map[string]entry
	pruned         map[string]bool // the folders a name was skipped in
	skip           func(rel string) bool
	reached        func(step, at string)
}

func sum(root *os.Root, name string, caps Caps, reached func(step, at string)) (string, []byte, error) {
	if caps.Entries < 0 || caps.Bytes < 0 {
		return "", nil, fmt.Errorf("caps of %d entries and %d bytes: %w", caps.Entries, caps.Bytes, ErrCap)
	}
	top, err := confine.Open(root, name, os.O_RDONLY|nonblock, 0)
	if err != nil {
		return "", nil, err
	}
	look, err := top.Stat()
	if err = errors.Join(err, top.Close()); err != nil {
		return "", nil, err
	}
	dir, err := enter(root, name, ".", look) // and a file is no root
	if err != nil {
		return "", nil, err
	}
	w := &walk{entries: caps.Entries, bytes: caps.Bytes, steps: budget(caps.Entries), kinds: map[string]entry{}, pruned: map[string]bool{}, skip: caps.Skip, reached: reached}
	if err := errors.Join(w.folder(dir, "."), dir.Close()); err != nil {
		return "", nil, err
	}
	if err := w.links(); err != nil {
		return "", nil, err
	}
	var b bytes.Buffer
	for _, e := range w.found {
		b.WriteString(e.kind + "\t" + e.path + "\t" + e.text)
		b.WriteByte('\n')
	}
	return hashOf(b.Bytes()), b.Bytes(), nil
}

// enter opens the folder name in dir and refuses it unless it is look: os.Root follows a relative
// link that stays inside. The "/." opens name as a folder on the way, so a FIFO swapped in is
// refused, not waited on.
func enter(dir *os.Root, name, at string, look fs.FileInfo) (*os.Root, error) {
	sub, err := dir.OpenRoot(name + "/.")
	if err != nil {
		return nil, err
	}
	got, err := sub.Stat(".")
	if err == nil && !os.SameFile(look, got) {
		err = &fs.PathError{Op: opSum, Path: at, Err: fmt.Errorf("it changed between its look and its open: %w", confine.ErrOutside)}
	}
	if err == nil {
		err = indexAt(sub, at)
	}
	if err != nil {
		return nil, errors.Join(err, sub.Close())
	}
	return sub, nil
}

// folder lists dir, at the slash path at ("." for the top), and takes each entry in it.
func (w *walk) folder(dir *os.Root, at string) error {
	f, err := confine.Open(dir, ".", os.O_RDONLY, 0)
	if err != nil {
		return err
	}
	var names []string
	for {
		batch, err := f.ReadDir(256)
		for _, d := range batch {
			if w.skip != nil && w.skip(path.Join(at, d.Name())) {
				w.pruned[at] = true
				continue
			}
			names = append(names, d.Name())
			w.entries--
		}
		if w.entries < 0 {
			return errors.Join(&fs.PathError{Op: opSum, Path: at, Err: ErrCap}, f.Close())
		}
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return errors.Join(err, f.Close())
		}
	}
	if err := f.Close(); err != nil {
		return err
	}
	slices.Sort(names) // so a refusal names the same entry on every file system, whatever it reads first
	for _, name := range names {
		if err := w.take(dir, name, path.Join(at, name)); err != nil {
			return err
		}
	}
	return nil
}

// take looks at the entry name in dir, at p, and adds it, a folder with all it holds.
func (w *walk) take(dir *os.Root, name, p string) error {
	if len(p) > longest {
		return &fs.PathError{Op: opSum, Path: p, Err: fmt.Errorf("its path is longer than %d bytes: %w", longest, ErrCap)}
	}
	if !text(name) {
		return &fs.PathError{Op: opSum, Path: p, Err: fmt.Errorf("its name is not UTF-8 text with no control character: %w", ErrKind)}
	}
	look, err := dir.Lstat(name)
	if err != nil {
		return err
	}
	m := look.Mode()
	switch {
	case m&fs.ModeSymlink != 0:
		target, err := dir.Readlink(name)
		if err != nil {
			return err
		}
		return w.link(p, target)
	case m&fs.ModeIrregular != 0:
		return &fs.PathError{Op: opSum, Path: p, Err: fmt.Errorf("it is a junction or another reparse point, not a link: %w", confine.ErrOutside)}
	case m.IsDir():
		w.add(entry{path: p, kind: "folder"})
		w.reached(stepFolder, p)
		sub, err := enter(dir, name, p, look)
		if err != nil {
			return err
		}
		return errors.Join(w.folder(sub, p), sub.Close())
	case m.IsRegular():
		w.reached(stepFile, p)
		return w.file(dir, name, p)
	}
	return &fs.PathError{Op: opSum, Path: p, Err: fmt.Errorf("it is a %v, not a folder, a file or a link: %w", m.Type(), ErrKind)}
}

func (w *walk) add(e entry) {
	w.found = append(w.found, e)
	w.kinds[e.path] = e
}

func text(s string) bool {
	return utf8.ValidString(s) && !strings.ContainsFunc(s, unicode.IsControl)
}

// file hashes the file name in dir, opened through confine so a link swapped in since the look
// is refused, and refuses it when it is no longer a file, a FIFO opened with no wait among them,
// or has a second name.
func (w *walk) file(dir *os.Root, name, p string) error {
	f, err := confine.Open(dir, name, os.O_RDONLY|nonblock, 0)
	if err != nil {
		return err
	}
	return errors.Join(w.hash(f, p), f.Close())
}

func (w *walk) hash(f *os.File, p string) error {
	info, err := f.Stat()
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() {
		return &fs.PathError{Op: opSum, Path: p, Err: fmt.Errorf("it changed between its look and its open: %w", confine.ErrOutside)}
	}
	names, err := confine.NamesFile(f)
	if err != nil {
		return err
	}
	if names > 1 {
		return &fs.PathError{Op: opSum, Path: p, Err: fmt.Errorf("it has another name, a hard link: %w", confine.ErrOutside)}
	}
	h := sha256.New()
	n, err := io.CopyN(h, f, min(w.bytes, math.MaxInt64-1)+1)
	if w.bytes -= n; w.bytes < 0 {
		return &fs.PathError{Op: opSum, Path: p, Err: ErrCap}
	}
	if err != nil && !errors.Is(err, io.EOF) {
		return err
	}
	w.add(entry{path: p, kind: "file", text: hex.EncodeToString(h.Sum(nil))})
	return nil
}

// link adds the link at p with its target's text, slash-separated, refusing one that is absolute,
// names a volume or a file's fork on macOS (as confine does), or is not text; inside does the rest.
func (w *walk) link(p, target string) error {
	t := filepath.ToSlash(target)
	if t == "" || strings.HasPrefix(t, "/") || strings.ContainsAny(t, `\:`) || !text(t) ||
		slices.ContainsFunc(strings.Split(t, "/"), func(c string) bool { return strings.EqualFold(c, "..namedfork") }) {
		return &fs.PathError{Op: opSum, Path: p, Err: fmt.Errorf("its target %q is absolute, names a volume or a fork, or is not a relative path the same on every system: %w", target, confine.ErrOutside)}
	}
	w.add(entry{path: p, kind: "link", text: t})
	return nil
}

// links refuses each link that leads out of the tree or loops, in the listing's order on every system.
func (w *walk) links() error {
	slices.SortFunc(w.found, func(a, b entry) int { return strings.Compare(a.path, b.path) })
	var err error
	for _, e := range w.found {
		if e.kind == "link" && err == nil {
			err = w.inside(e)
		}
	}
	return err
}

// spend takes n steps of the links' resolution from what the caps allow.
func (w *walk) spend(n int) error {
	if n > w.steps {
		return ErrCap
	}
	w.steps -= n
	return nil
}

// inside refuses the link e when its target, read as text from its own folder or resolved
// through the tree's other links, reaches above the tree's top, or when it loops.
func (w *walk) inside(e entry) error {
	n, at := 0, strings.Split(e.path, "/")
	err := w.spend(len(at) + strings.Count(e.text, "/") + 1)
	if c := path.Join(path.Dir(e.path), e.text); c == ".." || strings.HasPrefix(c, "../") {
		err = errAbove
	} else if err == nil {
		_, _, err = w.follow(at[:len(at)-1], e.text, &n)
	}
	if errors.Is(err, errMissing) { // a target that ends at a missing name stays inside
		err = nil
	}
	switch {
	case errors.Is(err, ErrCap):
		return &fs.PathError{Op: opSum, Path: e.path, Err: fmt.Errorf("its target takes more steps than the caps allow: %w", ErrCap)}
	case errors.Is(err, errLoop):
		return &fs.PathError{Op: opSum, Path: e.path, Err: fmt.Errorf("it is a loop of links, or a chain of more than %d: %w", hops, confine.ErrOutside)}
	case err != nil:
		return &fs.PathError{Op: opSum, Path: e.path, Err: fmt.Errorf("its target %q leads out of the tree: %w", e.text, confine.ErrOutside)}
	}
	return nil
}

var (
	errLoop, errAbove, errSkipped = errors.New("a loop"), errors.New("above the top"), errors.New("a name skipped")
	// errMissing is a target that ends at a missing name in a walk that skipped one: no refusal of
	// its own, but a link whose target goes on below it is refused.
	errMissing = errors.New("a missing name")
)

// follow resolves target from the folders at, through the tree's links, as Linux and macOS do,
// counting each link in n. It answers the folders it reached and true, or, where it reached a
// file or a missing name, false. A ".." pops only a folder of the tree: one after a file or a
// missing name, whose case or form a system may read otherwise, is refused.
func (w *walk) follow(at []string, target string, n *int) ([]string, bool, error) {
	at = slices.Clone(at)
	parts := strings.Split(target, "/")
	if err := w.spend(len(parts)); err != nil { // each component of the target, walked or not
		return nil, false, err
	}
	for i, part := range parts {
		if err := w.spend(len(at)); err != nil { // the folders of the path the component is read from
			return nil, false, err
		}
		switch {
		case part == "" || part == ".":
			continue
		case part == ".." && len(at) == 0:
			return nil, false, errAbove
		case part == "..":
			at = at[:len(at)-1]
			continue
		}
		e, ok := w.kinds[strings.Join(append(slices.Clip(at), part), "/")]
		switch {
		case ok && e.kind == "folder":
			at = append(at, part)
		case ok && e.kind == "link":
			res, folder, err := w.through(at, e.text, parts[i+1:], n)
			if err != nil || !folder {
				return nil, false, err
			}
			at = res
		default:
			return nil, false, w.end(ok, at, parts[i+1:])
		}
	}
	return at, true, nil
}

// through follows a link of target text met in the folders at, with rest left to walk after it,
// counting it in n: the folders it reached and true, or false with what climbs and missing say
// of the file or missing name it reached, or the refusal.
func (w *walk) through(at []string, text string, rest []string, n *int) ([]string, bool, error) {
	if *n++; *n > hops {
		return nil, false, errLoop
	}
	res, folder, err := w.follow(at, text, n)
	if errors.Is(err, errMissing) { // the link ends at a missing name: what follows it decides
		err = w.missing(false, rest)
	}
	if err != nil || !folder {
		return nil, false, climbs(err, rest)
	}
	return res, true, nil
}

// end is what follow answers where it reached a file, or a name not in the tree, found false, in
// the folders at, with rest left to walk.
func (w *walk) end(found bool, at, rest []string) error {
	if err := climbs(w.skipped(found, at), rest); err != nil {
		return err
	}
	return w.missing(found, rest)
}

// skipped is errSkipped for a name not in the tree, in the folder at, when that folder left a name
// out: the walk keeps no list of the names it skipped, so the name may be one.
func (w *walk) skipped(found bool, at []string) error {
	if !found && w.pruned[path.Join(append([]string{"."}, at...)...)] {
		return errSkipped
	}
	return nil
}

// missing is, for a name not in the tree once the walk skipped any name, errSkipped where a
// component is left to walk below it, and errMissing where none is. A volume that folds case or
// Unicode forms reads A, or a name's other form, as the kept folder a, and what lies below it may
// be a name skipped there; a folder that skipped nothing cannot tell it from a missing name.
func (w *walk) missing(found bool, rest []string) error {
	switch {
	case found || len(w.pruned) == 0:
		return nil
	case slices.ContainsFunc(rest, func(p string) bool { return p != "" && p != "." }):
		return errSkipped
	}
	return errMissing
}

// climbs is err, or with none a refusal of the parts left after a file or a missing name when one is "..".
func climbs(err error, rest []string) error {
	if err == nil && slices.Contains(rest, "..") {
		return errAbove
	}
	return err
}
