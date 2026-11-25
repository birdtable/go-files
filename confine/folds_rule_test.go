package confine_test

import (
	"errors"
	"syscall"
	"testing"

	"github.com/birdtable/go-files/confine"
)

// Folds' rule on macOS, held on every system: APFS and HFS+ that fold case fold both,
// case-sensitive HFS+ folds composition, and case-sensitive APFS, any other volume type, an odd
// pathconf answer and a failed one are FoldUnknown.
//
// What it costs when this fails: a case-sensitive APFS volume made normalization-sensitive is
// guessed at, or a volume Folds cannot read is answered as one that tells names apart.
func TestDarwinFoldingRule(t *testing.T) {
	t.Parallel()
	failed := errors.New("pathconf failed")
	for _, c := range []struct {
		typ       string
		sensitive int
		err       error
		want      confine.Folding
		wantErr   error
	}{
		{"apfs", 0, nil, confine.FoldBoth, nil},
		{"hfs", 0, nil, confine.FoldBoth, nil},
		{"hfs", 1, nil, confine.FoldComposition, nil},
		{"apfs", 1, nil, confine.FoldUnknown, confine.ErrNoFolding},
		{"apfs", 2, nil, confine.FoldUnknown, confine.ErrNoFolding},
		{"apfs", -1, failed, confine.FoldUnknown, failed},
		{"msdos", 0, nil, confine.FoldUnknown, confine.ErrNoFolding},
		{"smbfs", 1, nil, confine.FoldUnknown, confine.ErrNoFolding},
		{"devfs", -1, syscall.EINVAL, confine.FoldUnknown, confine.ErrNoFolding},
	} {
		got, err := confine.DarwinFolding("/v", c.typ, c.sensitive, c.err)
		if got != c.want || !errors.Is(err, c.wantErr) || (c.wantErr == nil) != (err == nil) {
			t.Errorf("%s volume answering %d, %v: %v, %v, want %v, %v", c.typ, c.sensitive, c.err, got, err, c.want, c.wantErr)
		}
	}
}

// Folds' rule on Linux, held on every system: an ext4, f2fs or tmpfs folder folds both with
// casefold set and neither without, a failed read of its attributes is FoldUnknown with the error,
// btrfs folds neither, and any other file system, XFS among them, is FoldUnknown with ErrNoFolding.
//
// What it costs when this fails: a casefold folder is answered as telling names apart, or an XFS
// volume made to fold ASCII case is.
func TestLinuxFoldingRule(t *testing.T) {
	t.Parallel()
	failed := errors.New("ioctl failed")
	const ext4, f2fs, tmpfs, btrfs, xfs, nfs, fuse, proc = 0xef53, 0xf2f52010, 0x01021994, 0x9123683e, 0x58465342, 0x6969, 0x65735546, 0x9fa0
	const casefold, extents = confine.FSCasefoldFL, 0x80000
	for _, c := range []struct {
		magic, flags uint32
		err          error
		want         confine.Folding
		wantErr      error
	}{
		{ext4, casefold, nil, confine.FoldBoth, nil},
		{ext4, casefold | extents, nil, confine.FoldBoth, nil},
		{ext4, extents, nil, confine.FoldNone, nil},
		{f2fs, casefold, nil, confine.FoldBoth, nil},
		{tmpfs, 0, nil, confine.FoldNone, nil},
		{tmpfs, casefold, nil, confine.FoldBoth, nil},
		{ext4, 0, failed, confine.FoldUnknown, failed},
		{btrfs, 0, nil, confine.FoldNone, nil},
		{btrfs, casefold, nil, confine.FoldNone, nil},
		{xfs, 0, nil, confine.FoldUnknown, confine.ErrNoFolding},
		{nfs, 0, nil, confine.FoldUnknown, confine.ErrNoFolding},
		{fuse, 0, nil, confine.FoldUnknown, confine.ErrNoFolding},
		{proc, 0, nil, confine.FoldUnknown, confine.ErrNoFolding},
	} {
		got, err := confine.LinuxFolding("/v", c.magic, c.flags, c.err)
		if got != c.want || !errors.Is(err, c.wantErr) || (c.wantErr == nil) != (err == nil) {
			t.Errorf("file system %#x, flags %#x, %v: %v, %v, want %v, %v", c.magic, c.flags, c.err, got, err, c.want, c.wantErr)
		}
	}
}

// Folds' rule on Windows, held on every system: NTFS, ReFS, FAT and exFAT on a local disk fold
// case unless the volume searches by case and the folder is set case-sensitive; a folder whose
// flag cannot be read folds case; a network path, a path not on a numbered disk volume and any
// other file system are FoldUnknown with ErrNoFolding.
//
// What it costs when this fails: a share whose server folds is answered by the file system name
// the server reports, or a folder whose flag cannot be read is answered as telling names apart.
func TestWindowsFoldingRule(t *testing.T) {
	t.Parallel()
	const local, search, sensitive = `\Device\HarddiskVolume3\Users\x`, 0x1, 0x1
	failed := errors.New("query failed")
	for _, c := range []struct {
		nt, fs    string
		flags, cs uint32
		csErr     error
		want      confine.Folding
	}{
		{local, "NTFS", search, 0, nil, confine.FoldCase},
		{local, "NTFS", search, sensitive, nil, confine.FoldNone},
		{`\Device\HarddiskVolume12\`, "ReFS", search, sensitive, nil, confine.FoldNone},
		{local, "NTFS", 0, sensitive, nil, confine.FoldCase},
		{local, "NTFS", search, sensitive, failed, confine.FoldCase},
		{local, "FAT32", 0, 0, failed, confine.FoldCase},
		{local, "FAT", 0, 0, nil, confine.FoldCase},
		{local, "exFAT", 0, 0, nil, confine.FoldCase},
		{local, "UDF", search, 0, nil, confine.FoldUnknown},
		{`\Device\Mup\server\share\x`, "NTFS", search, sensitive, nil, confine.FoldUnknown},
		{`\Device\HarddiskVolume\x`, "NTFS", search, 0, nil, confine.FoldUnknown},
		{`\Device\HarddiskVolume3`, "NTFS", search, 0, nil, confine.FoldUnknown},
		{`\Device\HarddiskVolumeShadowCopy1\x`, "NTFS", search, 0, nil, confine.FoldUnknown},
		{`\Device\CdRom0\x`, "NTFS", search, 0, nil, confine.FoldUnknown},
	} {
		got, err := confine.WindowsFolding(`C:\v`, c.nt, c.fs, c.flags, c.cs, c.csErr)
		wantErr := c.want == confine.FoldUnknown
		if got != c.want || errors.Is(err, confine.ErrNoFolding) != wantErr || (err != nil) != wantErr {
			t.Errorf("%s on %s, flags %#x, folder %#x, %v: %v, %v, want %v", c.nt, c.fs, c.flags, c.cs, c.csErr, got, err, c.want)
		}
	}
}
