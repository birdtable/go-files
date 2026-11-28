package confine

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
)

const opFolds = "folds"

// Folding is how a folder reads two names: whether it takes names that differ only in case as one,
// names that differ only in Unicode composition (é composed and e with a combining acute) as one,
// both or neither. FoldUnknown holds both fold bits, so a caller that asks FoldsCase or
// FoldsComposition of it is told yes: two names that might be one are kept as one.
type Folding uint8

const (
	// FoldNone is a folder that compares names byte for byte, as ext4 and NTFS's case-sensitive
	// folders do.
	FoldNone Folding = 0
	// FoldCase is a folder that folds case alone, as NTFS does.
	FoldCase Folding = 1
	// FoldComposition is a folder that folds Unicode composition alone.
	FoldComposition Folding = 2
	// FoldBoth is a folder that folds case and composition, as APFS and HFS+ as macOS formats them
	// do, and an ext4, f2fs or tmpfs folder with casefold set.
	FoldBoth = FoldCase | FoldComposition
	// FoldUnknown is the answer where the system cannot be asked without writing. It holds both
	// fold bits, so it reads as FoldBoth to FoldsCase and FoldsComposition.
	FoldUnknown = FoldBoth | 4
)

// FoldsCase is whether the folder may take names that differ only in case as one: true for
// FoldCase, FoldBoth and FoldUnknown.
func (f Folding) FoldsCase() bool { return f&FoldCase != 0 }

// FoldsComposition is whether the folder may take names that differ only in Unicode composition as
// one: true for FoldComposition, FoldBoth and FoldUnknown.
func (f Folding) FoldsComposition() bool { return f&FoldComposition != 0 }

func (f Folding) String() string {
	switch f {
	case FoldNone:
		return "none"
	case FoldCase:
		return "case"
	case FoldComposition:
		return "composition"
	case FoldBoth:
		return "both"
	case FoldUnknown:
		return "unknown"
	}
	return fmt.Sprintf("Folding(%d)", uint8(f))
}

// ErrNoFolding is Folds' answer where the system cannot say how the folder reads names without a
// file written to find out. It comes with FoldUnknown, and matches errors.ErrUnsupported.
var ErrNoFolding = fmt.Errorf("the system cannot say how this folder reads names without a write, so take it as folding both case and composition: %w", errors.ErrUnsupported)

// Folds answers how the folder at root reads names, its volume's rule and the folder's own, and
// writes nothing: root is opened read-only, links followed as the system follows them, and the
// system is asked by its own means.
//   - On macOS, the volume's case sensitivity (pathconf _PC_CASE_SENSITIVE) and its type: HFS+
//     keeps every name decomposed, and APFS that folds case folds composition too. A case-sensitive
//     APFS volume may or may not fold composition, and nothing short of a write says which.
//   - On Windows, the volume's case-sensitive-search flag and the folder's own case-sensitivity
//     flag; without it NTFS, ReFS, FAT and exFAT fold case and never composition, as Windows opens
//     a name. A network volume reports its server's file system, not the server's rule.
//   - On Linux, the file system's type and, for ext4, f2fs and tmpfs, the folder's casefold
//     attribute, which folds both. btrfs folds neither.
//
// Any other volume, network shares, FUSE, FAT on macOS and Linux, XFS (which can be made to fold
// ASCII case) and ZFS among them, and any other system, is FoldUnknown with an error matching
// ErrNoFolding. A failure to ask is FoldUnknown with the system's error, and root that is not a
// folder is FoldUnknown with an *fs.PathError matching fs.ErrInvalid. On Windows a root that names
// a device or a pipe, not a path on a drive or a volume, is FoldUnknown with an *fs.PathError
// matching fs.ErrInvalid and ErrOutside, before anything is opened. Treat FoldUnknown as FoldBoth,
// as its bits do: two names that might be one are never reported apart.
//
// The answer is the folder's rule now: a folder another account may change, as by chattr +F on an
// empty one, may answer otherwise later. Collide asks by a probe, for a caller that wants a definite
// answer and may write.
func Folds(root string) (Folding, error) {
	if err := notDevice(opFolds, root); err != nil {
		return FoldUnknown, err
	}
	f, err := openFolder(root)
	if err != nil {
		return FoldUnknown, err
	}
	got, err := askFolding(f, root)
	if got, err = answerClosed(f, got, err); err != nil {
		return FoldUnknown, err
	}
	return got, nil
}

// openFolder opens root read-only, never waiting on a pipe, and refuses what is not a folder.
func openFolder(root string) (*os.File, error) {
	f, err := openFolderPath(root)
	if err != nil {
		return nil, err
	}
	info, err := f.Stat()
	if err == nil && !info.IsDir() {
		err = foldsNotFolder(root, info.Mode())
	}
	if err != nil {
		return nil, errors.Join(err, f.Close())
	}
	return f, nil
}

// unknownFolding is FoldUnknown's error for root, of the volume type typ, which the system names
// but cannot say the rule of.
func unknownFolding(root, typ, why string) error {
	return &fs.PathError{Op: opFolds, Path: root, Err: fmt.Errorf("%s volume %s: %w", typ, why, ErrNoFolding)}
}

// foldsNotFolder is the refusal of a root that is not a folder.
func foldsNotFolder(root string, mode fs.FileMode) error {
	return &fs.PathError{Op: opFolds, Path: root, Err: fmt.Errorf("it is %v, and only a folder reads names: %w", mode.Type(), fs.ErrInvalid)}
}
