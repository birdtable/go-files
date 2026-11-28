package winapi

import (
	"encoding/binary"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"testing"
)

// Readlink reads a link or a junction from a handle on the name itself, opened with Identify, and
// spells its target as os.Readlink spells it: a junction to a folder, a junction made by hand to a
// \??\ name that is no drive, an absolute link and a relative one each answer what os.Readlink
// answers, and a name that is no reparse point is refused as os.Readlink refuses it.
//
// What it costs when this fails: confine's walk, which reads each link by Readlink, follows a
// target spelled otherwise than os.Readlink spells it, to another folder than the system's.
func TestReadlinkAnswersWhatOsReadlinkAnswers(t *testing.T) {
	t.Parallel()
	d := t.TempDir()
	folder := filepath.Join(d, "folder")
	if err := os.Mkdir(folder, 0o700); err != nil {
		t.Fatal(err)
	}
	junction := filepath.Join(d, "junction")
	if err := plantJunction(junction, `\??\`+folder); err != nil {
		t.Fatal(err)
	}
	noDrive := filepath.Join(d, "nodrive")
	if err := plantJunction(noDrive, `\??\Volume{00000000-0000-0000-0000-000000000000}\`); err != nil {
		t.Fatal(err)
	}
	absolute, relative := filepath.Join(d, "absolute"), filepath.Join(d, "relative")
	if err := os.Symlink(folder, absolute); err != nil {
		t.Fatalf("a link (CI runs as an administrator, who may make one): %v", err)
	}
	if err := os.Symlink(`.\folder`, relative); err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{junction, noDrive, absolute, relative} {
		want, werr := os.Readlink(p)
		got, err := Readlink(p)
		if got != want || (err == nil) != (werr == nil) {
			t.Errorf("Readlink(%s) = %q, %v, and os.Readlink answers %q, %v", p, got, err, want, werr)
		}
	}
	_, err := Readlink(folder)
	var pe *fs.PathError
	if err == nil || !errors.As(err, &pe) {
		t.Errorf("Readlink of the folder %s, no reparse point: %v, and want an *fs.PathError", folder, err)
	}
	if _, err := Readlink(filepath.Join(d, "missing")); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("Readlink of a name not there: %v, and want fs.ErrNotExist", err)
	}
}

// linkTarget reads a reparse buffer it is handed with no trust in its lengths: a buffer too short
// for its header or for a link's flags, and a substitute name that lies past the names or starts
// halfway into a character, are refused, and a reparse point of another tag matches
// fs.ErrNotExist, as os.Readlink answers one.
//
// What it costs when this fails: a reparse buffer the file system hands back short, or with a
// name's offset past its end, panics the program or reads past the names as a target.
func TestLinkTargetRefusesABufferItsLengthsDoNotFit(t *testing.T) {
	t.Parallel()
	buffer := func(tag uint32, off, size uint16, names int) []byte {
		b := binary.LittleEndian.AppendUint32(nil, tag)
		for _, v := range []uint16{0, 0, off, size, 0, 0} {
			b = binary.LittleEndian.AppendUint16(b, v)
		}
		return append(b, make([]byte, names)...)
	}
	const symlink, mountPoint = 0xA000000C, 0xA0000003
	for _, c := range []struct {
		name     string
		b        []byte
		notThere bool
	}{
		{"a header cut short", buffer(mountPoint, 0, 0, 0)[:15], false},
		{"a link's flags cut short", buffer(symlink, 0, 0, 0)[:19], false},
		{"a name past the names", buffer(mountPoint, 0, 8, 6), false},
		{"a name at an odd offset", buffer(mountPoint, 1, 2, 6), false},
		{"a name of an odd length", buffer(mountPoint, 0, 3, 6), false},
		{"another tag", buffer(0x80000017, 0, 0, 4), true},
	} {
		got, err := linkTarget(c.b)
		if err == nil || errors.Is(err, fs.ErrNotExist) != c.notThere {
			t.Errorf("%s: %q, %v, and want a refusal (fs.ErrNotExist: %v)", c.name, got, err, c.notThere)
		}
	}
}

// dosTarget spells an NT path as os.Readlink spells it: \??\ and a drive as the drive's path,
// \??\UNC\ as \\, any other \??\ name past \\?\, and a path without \??\ as it is.
//
// What it costs when this fails: confine's walk follows a junction's target as another path than
// the system's, or takes a share for a local folder.
func TestDosTargetSpellsAnNTPathAsOsReadlinkDoes(t *testing.T) {
	t.Parallel()
	for in, want := range map[string]string{
		`\??\C:\dir`:             `C:\dir`,
		`\??\UNC\host\share\dir`: `\\host\share\dir`,
		`\??\Volume{abc}\`:       `\\?\Volume{abc}\`,
		`\??\pipe\name`:          `\\?\pipe\name`,
		`\Device\NamedPipe\name`: `\Device\NamedPipe\name`,
		`dir\file`:               `dir\file`,
		`\??\unc\host\share`:     `\\?\unc\host\share`,
	} {
		if got := dosTarget(in); got != want {
			t.Errorf("dosTarget(%q) = %q, and want %q", in, got, want)
		}
	}
}
