package trust

import (
	"bytes"
	"encoding/binary"
	"testing"
)

// The SID in an access entry, read from the entry's bytes on every system, as entries reads it on
// Windows.

// sidBytes is the SID written s as winnt.h lays it out, from sidParts.
func sidBytes(s string) []byte {
	auth, subs := sidParts(s)
	b := append([]byte{1, byte(len(subs))}, auth[:]...)
	for _, x := range subs {
		b = binary.LittleEndian.AppendUint32(b, x)
	}
	return b
}

// What it costs when this fails: an entry's SID is read as some other account's, so an account
// that may write a folder is judged as one that may not, or as root, or as an AppContainer's.
func TestASIDIsReadFromItsBytesAsWindowsWritesIt(t *testing.T) {
	t.Parallel()
	for _, s := range []string{
		"S-1-5", "S-1-1-0", "S-1-5-18", "S-1-5-32-544", "S-1-5-32-545", "S-1-3-4", "S-1-16-12288",
		evilSID, capability, allPackages, aPackage, "S-1-15-2", "S-1-15-3-1",
		"S-1-5-21-4294967295-4294967295-4294967295-4294967295-4294967295-4294967295-4294967295-4294967295-4294967295-4294967295-4294967295",
	} {
		b := sidBytes(s)
		// An entry may be longer than its SID: what follows the SID is not read.
		for _, in := range [][]byte{b, append(bytes.Clone(b), 0xff, 0xff, 0xff, 0xff)} {
			auth, subs, ok := sidFields(in)
			if !ok {
				t.Errorf("%s in %d bytes: not read whole", s, len(in))
				continue
			}
			if got := sidString(auth, subs); got != s {
				t.Errorf("%s in %d bytes: read as %s", s, len(in), got)
			}
			if got, want := appContainer(auth, subs), appContainer(sidParts(s)); got != want {
				t.Errorf("%s in %d bytes: an AppContainer's is %v, and want %v", s, len(in), got, want)
			}
		}
	}
}

// What it costs when this fails: the test above and sidFields share one mistake in the order of
// the bytes, and every SID is read wrong on Windows while both agree here.
func TestASIDsAuthorityIsHighByteFirstAndItsSubAuthoritiesLowByteFirst(t *testing.T) {
	t.Parallel()
	// S-1-5-32-545, BUILTIN\Users, as a Windows list holds it.
	b := []byte{1, 2, 0, 0, 0, 0, 0, 5, 0x20, 0, 0, 0, 0x21, 0x02, 0, 0}
	auth, subs, ok := sidFields(b)
	if got := sidString(auth, subs); !ok || got != "S-1-5-32-545" {
		t.Errorf("BUILTIN\\Users' bytes: read as %s, %v; want S-1-5-32-545", got, ok)
	}
}

// What it costs when this fails: an entry whose SID runs past the entry is read from the bytes
// after it, an account the list never named, or the read runs off the list.
func TestASIDNotWholeInItsBytesIsNotRead(t *testing.T) {
	t.Parallel()
	users := sidBytes("S-1-5-32-545")
	for _, c := range []struct {
		name string
		b    []byte
	}{
		{"no bytes", nil},
		{"a header cut short", users[:7]},
		{"its last sub-authority cut short", users[:len(users)-1]},
		{"one sub-authority of two", users[:12]},
		{"revision 2", append([]byte{2}, users[1:]...)},
		{"revision 0", append([]byte{0}, users[1:]...)},
		{"16 sub-authorities", append([]byte{1, 16}, make([]byte, 6+4*16)...)},
		{"255 sub-authorities", append([]byte{1, 255}, make([]byte, 6+4*255)...)},
	} {
		if auth, subs, ok := sidFields(c.b); ok {
			t.Errorf("%s: read as %s, and want it not read", c.name, sidString(auth, subs))
		}
	}
}

// What it costs when this fails: a SID of a large authority is written as another SID's string,
// and the judging matches it to an account it is not.
func TestASIDsAuthorityOf2To32OrMoreIsWrittenInHex(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		auth [6]byte
		want string
	}{
		{[6]byte{0, 0, 0xff, 0xff, 0xff, 0xff}, "S-1-4294967295-7"},
		{[6]byte{0, 1, 0, 0, 0, 0}, "S-1-0x000100000000-7"},
		{[6]byte{0x12, 0x34, 0x56, 0x78, 0x9a, 0xbc}, "S-1-0x123456789ABC-7"},
		{[6]byte{0xff, 0xff, 0xff, 0xff, 0xff, 0xff}, "S-1-0xFFFFFFFFFFFF-7"},
	} {
		if got := sidString(c.auth, []uint32{7}); got != c.want {
			t.Errorf("authority % x: %s, and want %s", c.auth, got, c.want)
		}
	}
}
