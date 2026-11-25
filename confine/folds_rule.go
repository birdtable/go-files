package confine

import (
	"fmt"
	"strings"
)

// The numbers Folds' rules read, each from the header named, defined here so that each rule is
// built, and its table run, on every system.
const (
	// fsCasefoldFL is FS_CASEFOLD_FL, from linux/fs.h: the folder's names are looked up folded, by
	// the volume's Unicode encoding, in case and normal form both.
	fsCasefoldFL = 0x40000000
	// The f_type of each Linux file system the rule names, from linux/magic.h. ext2, ext3 and ext4
	// share one; only ext4 keeps casefold, and the others never set it.
	ext4Magic  = 0xef53
	f2fsMagic  = 0xf2f52010
	tmpfsMagic = 0x01021994
	btrfsMagic = 0x9123683e
	// fileCaseSensitiveSearch is FILE_CASE_SENSITIVE_SEARCH, from winnt.h: the volume can look a
	// name up by case, so a folder on it can be set case-sensitive.
	fileCaseSensitiveSearch = 0x1
	// fileCSFlagCaseSensitiveDir is FILE_CS_FLAG_CASE_SENSITIVE_DIR, from ntifs.h: the folder is set
	// case-sensitive.
	fileCSFlagCaseSensitiveDir = 0x1
)

// darwinFolding is the rule of a macOS volume of the type typ, whose pathconf _PC_CASE_SENSITIVE
// answered sensitive, or failed with err. HFS+ keeps each name decomposed, so it folds composition,
// cased or not. APFS that folds case is always normalization-insensitive; a case-sensitive APFS
// volume is made with or without it, and only its superblock says which.
func darwinFolding(root, typ string, sensitive int, err error) (Folding, error) {
	if typ != "apfs" && typ != "hfs" {
		return FoldUnknown, unknownFolding(root, typ, "is not APFS or HFS+")
	}
	switch {
	case err != nil:
		return FoldUnknown, err
	case sensitive == 0:
		return FoldBoth, nil
	case sensitive == 1 && typ == "hfs":
		return FoldComposition, nil
	case sensitive == 1:
		return FoldUnknown, unknownFolding(root, typ, "is case-sensitive, and may or may not fold composition")
	}
	return FoldUnknown, unknownFolding(root, typ, "answers no case sensitivity")
}

// casefolds is whether the Linux file system of the magic number magic keeps casefold per folder.
func casefolds(magic uint32) bool {
	return magic == ext4Magic || magic == f2fsMagic || magic == tmpfsMagic
}

// linuxFolding is the rule of a Linux folder on the file system of the magic number magic, whose
// attributes are flags, or failed with err, where casefolds asks them.
func linuxFolding(root string, magic, flags uint32, err error) (Folding, error) {
	switch {
	case casefolds(magic) && err != nil:
		return FoldUnknown, err
	case casefolds(magic) && flags&fsCasefoldFL != 0:
		return FoldBoth, nil
	case casefolds(magic), magic == btrfsMagic:
		return FoldNone, nil
	}
	return FoldUnknown, unknownFolding(root, fmt.Sprintf("file system type %#x", magic), "is not ext4, f2fs, tmpfs or btrfs")
}

// windowsFolding is the rule of a Windows folder whose NT path is nt, on a volume of the file
// system fsName and the flags flags, whose own case-sensitivity flags are cs, or failed with
// csErr. Only a local disk volume is answered: a network volume names its server's file system,
// whatever the server folds. NTFS, ReFS, FAT and exFAT never fold composition, and fold case as
// Windows opens a name unless the folder is set case-sensitive, which a volume without
// case-sensitive search cannot be. A folder whose flag cannot be read is taken as folding case.
func windowsFolding(root, nt, fsName string, flags, cs uint32, csErr error) (Folding, error) {
	if !localDisk(nt) {
		return FoldUnknown, unknownFolding(root, nt, "is not a local disk volume")
	}
	switch fsName {
	case "NTFS", "ReFS", "FAT", "FAT32", "exFAT":
	default:
		return FoldUnknown, unknownFolding(root, fsName, "is not NTFS, ReFS, FAT or exFAT")
	}
	if flags&fileCaseSensitiveSearch != 0 && csErr == nil && cs&fileCSFlagCaseSensitiveDir != 0 {
		return FoldNone, nil
	}
	return FoldCase, nil
}

// localDisk is whether nt, an NT path, is on a disk volume of this machine:
// \Device\HarddiskVolume, a number, then a separator.
func localDisk(nt string) bool {
	rest, ok := strings.CutPrefix(nt, `\Device\HarddiskVolume`)
	digits := len(rest) - len(strings.TrimLeft(rest, "0123456789"))
	return ok && digits > 0 && strings.HasPrefix(rest[digits:], `\`)
}
