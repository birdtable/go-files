package confine_test

import (
	"errors"
	"io/fs"
	"testing"

	"github.com/birdtable/go-files/confine"
)

// FileURL writes each system's absolute paths in RFC 8089's form, as Go's own go command does, and
// FromFileURL reads each back to the path it came from: a drive as the path's first name, a share's
// host as the URL's host, and a ?, a #, a % or a space in a name escaped and kept.
//
// What it costs when this fails: a program hands an editor or a browser a link to the wrong file,
// or one with a name cut at a # or a ?, and reads a link it wrote back to another path.
func TestFileURLGoesBothWaysOnEachSystem(t *testing.T) {
	t.Parallel()
	cases := []struct{ goos, abs, uri string }{
		{"linux", "/", "file:///"},
		{"linux", "/a/b", "file:///a/b"},
		{"linux", "/a b/c%d", "file:///a%20b/c%25d"},
		{"linux", "/a?b/c#d", "file:///a%3Fb/c%23d"},
		{"linux", `/a\b`, "file:///a%5Cb"},
		{"linux", "/émigré/x", "file:///%C3%A9migr%C3%A9/x"},
		{"darwin", "/Users/u/My File", "file:///Users/u/My%20File"},
		{"freebsd", "/home/u/", "file:///home/u/"},
		{"windows", `C:\`, "file:///C:/"},
		{"windows", `C:\a\b`, "file:///C:/a/b"},
		{"windows", `d:\a b\c#d`, "file:///d:/a%20b/c%23d"},
		{"windows", `\\host\share`, "file://host/share"},
		{"windows", `\\host\share\a\b`, "file://host/share/a/b"},
		{"windows", `\\host.example.com\Share\a b`, "file://host.example.com/Share/a%20b"},
	}
	for _, c := range cases {
		got, err := confine.FileURL(c.goos, c.abs)
		if err != nil || got != c.uri {
			t.Errorf("FileURL(%s, %q) = %q, %v, want %q", c.goos, c.abs, got, err, c.uri)
		}
		back, err := confine.FromFileURL(c.goos, c.uri)
		if err != nil || back != c.abs {
			t.Errorf("FromFileURL(%s, %q) = %q, %v, want %q", c.goos, c.uri, back, err, c.abs)
		}
	}
	read := []struct{ goos, uri, abs string }{
		{"linux", "file://localhost/a/b", "/a/b"},
		{"linux", "FILE:///a", "/a"},
		{"windows", "file://localhost/C:/a", `C:\a`},
		{"windows", "file:///C:/a%20b", `C:\a b`},
		{"windows", "file:///C:/a/.b", `C:\a\.b`},
		{"windows", "file:///C:/a/CONSOLE/con1/COM10/LPT%C2%B4.txt/nul_", "C:\\a\\CONSOLE\\con1\\COM10\\LPT\u00b4.txt\\nul_"}, // U+00B4 is no superscript digit
		{"windows", "file:///C:/a/", `C:\a\`},
	}
	for _, c := range read {
		if got, err := confine.FromFileURL(c.goos, c.uri); err != nil || got != c.abs {
			t.Errorf("FromFileURL(%s, %q) = %q, %v, want %q", c.goos, c.uri, got, err, c.abs)
		}
	}
	if got, err := confine.FileURL("windows", "C:/a/b"); err != nil || got != "file:///C:/a/b" {
		t.Errorf(`FileURL(windows, "C:/a/b") = %q, %v, a slash read as a backslash`, got, err)
	}
}

// FileURL refuses a path it cannot write as a file URL that reads back to it, with an error
// matching fs.ErrInvalid, and gives no URL.
//
// What it costs when this fails: a relative path is written as a URL a reader resolves against
// its own folder, a .. in it climbs out of the folder a reader meant, a device path names no file,
// or a NUL cuts the name where C code reads it.
func TestFileURLRefusesWhatNoFileURLWrites(t *testing.T) {
	t.Parallel()
	cases := []struct{ goos, abs string }{
		{"linux", ""}, {"linux", "a/b"}, {"linux", "./a"}, {"linux", `C:\a`},
		{"linux", "/a/../b"}, {"linux", "/a/./b"}, {"linux", "/a/.."}, {"linux", "/a\x00b"},
		{"darwin", "~/a"},
		{"windows", ""}, {"windows", `a\b`}, {"windows", `\a\b`}, {"windows", `C:a`}, {"windows", "C:"}, {"windows", "/a/b"},
		{"windows", `\\?\C:\a`}, {"windows", `\\.\C:\a`}, {"windows", `\\host`}, {"windows", `\\host\`}, {"windows", `\\\share\a`}, {"windows", `\\C:\share\a`}, {"windows", `\\host:1\share`},
		{"windows", `C:\a\..\b`}, {"windows", `C:\a\.\b`}, {"windows", `\\host\share\..\x`}, {"windows", "C:\\a\x00b"}, {"windows", `1:\a`},
		{"windows", `\\;LanmanRedirector\host\share\a`}, {"windows", `\\..\share\a`},
	}
	for _, c := range cases {
		if got, err := confine.FileURL(c.goos, c.abs); !errors.Is(err, fs.ErrInvalid) || got != "" {
			t.Errorf("FileURL(%s, %q) = %q, %v, want fs.ErrInvalid and no URL", c.goos, c.abs, got, err)
		}
	}
}

// FromFileURL refuses a URL that is not one FileURL writes, or names no absolute path on the
// system, with an error matching fs.ErrInvalid, and gives no path.
//
// What it costs when this fails: a link from a file a person did not write names a path with a ..
// escaped as %2E%2E that climbs out of the folder the program checked, a host on Unix is read as
// this machine, a drive written as a host (one slash too few) is read as a share, or a query
// is cut away and the file opened is not the one the link named.
func TestFromFileURLRefusesWhatFileURLDoesNotWrite(t *testing.T) {
	t.Parallel()
	cases := []struct{ goos, uri string }{
		{"linux", ""}, {"linux", "/a/b"}, {"linux", "http://host/a"}, {"linux", "file:a"}, {"linux", "file:/a"},
		{"linux", "file://host/a"}, {"linux", "file://localhost"}, {"linux", "file://u@localhost/a"}, {"linux", "file://localhost:80/a"},
		{"linux", "file:///a?b"}, {"linux", "file:///a?"}, {"linux", "file:///a#b"}, {"linux", "file:///a#"},
		{"linux", "file:///a/../b"}, {"linux", "file:///a/%2E%2E/b"}, {"linux", "file:///a%2F..%2Fb"}, {"linux", "file:///a/./b"},
		{"linux", "file:///a%00b"}, {"linux", "file:///a%zz"},
		{"windows", "file:///a/b"}, {"windows", "file://C:/a"}, {"windows", "file:C:/a"}, {"windows", "file:/C:/a"}, {"windows", "file:///C:"},
		{"windows", "file:///C:a"}, {"windows", "file://host"}, {"windows", "file://host/"}, {"windows", "file:////host/share/a"},
		{"windows", "file://///host/share/a"}, {"windows", "file:///C:/a/%2E%2E/b"}, {"windows", "file:///C:/a%5C..%5Cb"},
		{"windows", "file:///C:/a%5Cb"}, {"windows", "file:///C:/a?b"}, {"windows", "file:///C:/a%00b"}, {"windows", "file://host:445/share/a"},
		{"windows", "file://[::1]/share/a"}, {"windows", "file:///%3F/C:/a"},
		{"windows", "file:///C:/a/..."}, {"windows", "file:///C:/a/.%20"}, {"windows", "file:///C:/a/..%20/b"}, {"windows", "file:///C:/a./b"},
		{"windows", "file:///C:/a%20"}, {"windows", "file://host/share/a."}, {"windows", "file:///C:/a:stream"}, {"windows", "file:///C:/a/b:c/d"},
		{"windows", "file://host/share/a:b"}, {"windows", "file:///C:/CON"}, {"windows", "file:///C:/a/nul"}, {"windows", "file:///C:/a/Com1.txt"},
		{"windows", "file:///C:/a/LPT0"}, {"windows", "file:///C:/a/COM%C2%B9"}, {"windows", "file:///C:/a/aux%20.c"}, {"windows", "file:///C:/conin$"},
		{"windows", "file:///C:/CONOUT$/a"}, {"windows", "file://host/PRN/a"},
		{"windows", "file://;LanmanRedirector/host/share/a"}, {"windows", "file://;/share/a"}, {"windows", "file://../share/a"}, {"windows", "file://./share/a"},
	}
	for _, c := range cases {
		if got, err := confine.FromFileURL(c.goos, c.uri); !errors.Is(err, fs.ErrInvalid) || got != "" {
			t.Errorf("FromFileURL(%s, %q) = %q, %v, want fs.ErrInvalid and no path", c.goos, c.uri, got, err)
		}
	}
}

// Both refuse a system with no file URL form here, with an error matching errors.ErrUnsupported,
// so a mistyped "Windows" is never read as a Unix system.
//
// What it costs when this fails: a program that spells the system wrong writes C:\a as a Unix
// path, and its link names nothing.
func TestFileURLRefusesAnUnknownSystem(t *testing.T) {
	t.Parallel()
	for _, g := range []string{"", "Windows", "plan9", "js", "macos"} {
		if got, err := confine.FileURL(g, "/a"); !errors.Is(err, errors.ErrUnsupported) || got != "" {
			t.Errorf("FileURL(%q) = %q, %v, want errors.ErrUnsupported", g, got, err)
		}
		if got, err := confine.FromFileURL(g, "file:///a"); !errors.Is(err, errors.ErrUnsupported) || got != "" {
			t.Errorf("FromFileURL(%q) = %q, %v, want errors.ErrUnsupported", g, got, err)
		}
	}
}
