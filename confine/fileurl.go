// Ported from the Go project's cmd/go/internal/web (url.go, url_other.go and url_windows.go, at
// go1.27.4), Copyright 2019 The Go Authors, under the BSD-style licence quoted in NOTICE.
// It takes the system as an argument, where the original reads the one it is
// built for, and refuses more: see FileURL and FromFileURL.

package confine

import (
	"errors"
	"fmt"
	"io/fs"
	"net/url"
	"path"
	"strings"
)

// FileURL is the file URL of the absolute path abs on the system goos, in RFC 8089's form, as Go's
// own go command writes it: /a/b is file:///a/b, C:\a\b is file:///C:/a/b, and the share path
// \\host\share\a is file://host/share/a. On Windows a slash is read as a backslash. Each name in
// the path is escaped as a URL path escapes it, so a ?, a # or a % in a name stays in the name.
//
// Refused, with an error matching fs.ErrInvalid: a path that is not absolute on goos; on Windows a
// device path (\\?\, \\.\) and a share with no host or share name; a path with a . or .. element,
// which the URL would keep and a reader may clean another way; and a path holding a NUL byte. A
// system with no form here is refused with an error matching errors.ErrUnsupported.
func FileURL(goos, abs string) (string, error) {
	if err := knownSystem(goos); err != nil {
		return "", err
	}
	if strings.IndexByte(abs, 0) >= 0 {
		return "", fmt.Errorf("%q holds a NUL byte, which no file name holds: %w", abs, fs.ErrInvalid)
	}
	u := &url.URL{Scheme: "file"}
	if goos != "windows" {
		if !path.IsAbs(abs) {
			return "", fmt.Errorf("%q is not an absolute path on %s: %w", abs, goos, fs.ErrInvalid)
		}
		u.Path = abs
	} else {
		p := strings.ReplaceAll(abs, `\`, "/")
		host, rest, err := windowsParts(p)
		if err != nil {
			return "", fmt.Errorf("%q: %w", abs, err)
		}
		u.Host, u.Path = host, rest
	}
	if err := noDots(u.Path); err != nil {
		return "", fmt.Errorf("%q: %w", abs, err)
	}
	return u.String(), nil
}

// FromFileURL is the absolute path on the system goos that the file URL uri names, FileURL's
// inverse: file:///a/b is /a/b, and on Windows file:///C:/a/b is C:\a\b and file://host/share/a is
// the share path \\host\share\a. The host localhost is the local machine, as RFC 8089 says. Each
// name is unescaped.
//
// A share path reaches the host it names when it is opened, and Windows offers that host this
// account's credentials: a program that takes a URL from a less-trusted source refuses one whose
// path begins \\ before it opens it.
//
// Refused, with an error matching fs.ErrInvalid: another scheme; a URL with a user, a port, a
// query or a fragment, which no file URL FileURL writes has; the short form file:/a with no //,
// and file:a; on a system but Windows any host but localhost; on Windows a drive letter as the
// host (file://C:/a, one slash too few) and a URL with no drive and no host; a path that is not
// absolute; a . or .. element, escaped or not; a NUL byte, escaped or not; and on Windows an
// element Windows reads as another name than the one written, which FileURL writes as it is given:
// one ending in a dot or a space, which the Win32 name rules strip, so file:///C:/a/.%20 would
// open C:\a; one holding a colon past the drive, a stream of another file; and a device name, CON,
// NUL, COM1, CONIN$ and the rest, with or without an extension; and on Windows a host that is .
// or .., or that starts with ;, which names a redirector and not the host the open reaches
// (file://;LanmanRedirector/host/share/a). A system with no form here is refused with an error
// matching errors.ErrUnsupported.
//
// An 8.3 short name, as file:///C:/PROGRA~1/x, passes where a volume keeps short names, though
// Windows reads it as another name: by its spelling it cannot be told from a plain name. A caller
// that matches the path against others resolves it first and compares by file ID, as Below does,
// never by spelling.
func FromFileURL(goos, uri string) (string, error) {
	if err := knownSystem(goos); err != nil {
		return "", err
	}
	u, err := fileURL(uri)
	if err != nil {
		return "", err
	}
	if goos == "windows" {
		return fromWindowsURL(uri, u)
	}
	if u.Host != "" && u.Host != "localhost" {
		return "", fmt.Errorf("%q names the host %q, not this machine: %w", uri, u.Host, fs.ErrInvalid)
	}
	if !path.IsAbs(u.Path) {
		return "", fmt.Errorf("%q names no absolute path: %w", uri, fs.ErrInvalid)
	}
	if err := noDots(u.Path); err != nil {
		return "", fmt.Errorf("%q: %w", uri, err)
	}
	return u.Path, nil
}

// fileURL parses uri and refuses what is not a file URL in the form FileURL writes, on any system.
func fileURL(uri string) (*url.URL, error) {
	u, err := url.Parse(uri)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", err, fs.ErrInvalid)
	}
	switch {
	case u.Scheme != "file":
		return nil, fmt.Errorf("%q is not a file URL: %w", uri, fs.ErrInvalid)
	case u.User != nil || u.Port() != "" || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" || strings.Contains(uri, "#"):
		return nil, fmt.Errorf("%q has a user, a port, a query or a fragment, which a file URL does not: %w", uri, fs.ErrInvalid)
	case u.Opaque != "" || !strings.HasPrefix(uri[len("file:"):], "//"):
		return nil, fmt.Errorf("%q has no // after file:, the form FileURL writes: %w", uri, fs.ErrInvalid)
	case strings.IndexByte(u.Path, 0) >= 0 || strings.IndexByte(u.Host, 0) >= 0:
		return nil, fmt.Errorf("%q holds a NUL byte, which no file name holds: %w", uri, fs.ErrInvalid)
	}
	return u, nil
}

// fromWindowsURL is the Windows path the file URL u names: a drive path, or a share's when u has a
// host but localhost.
func fromWindowsURL(uri string, u *url.URL) (string, error) {
	if strings.Contains(u.Path, `\`) {
		return "", fmt.Errorf("%q holds a backslash, which Windows reads as a separator the URL did not write: %w", uri, fs.ErrInvalid)
	}
	if err := noDots(u.Path); err != nil {
		return "", fmt.Errorf("%q: %w", uri, err)
	}
	host := u.Host
	if host == "localhost" {
		host = ""
	}
	var p string
	switch {
	case host != "":
		p = "//" + host + u.Path
	case len(u.Path) > 0 && u.Path[0] == '/':
		p = u.Path[1:]
	}
	if h, _, err := windowsParts(p); err != nil || (h == "") != (host == "") { // a share is the host, never a path of five slashes
		return "", fmt.Errorf("%q names no absolute Windows path: %w", uri, fs.ErrInvalid)
	}
	if err := windowsNames(u.Path, host == ""); err != nil {
		return "", fmt.Errorf("%q: %w", uri, err)
	}
	return strings.ReplaceAll(p, "/", `\`), nil
}

// windowsNames refuses an element of the slash path p, past the drive where drive is true, that
// Windows reads as another name than the one written: an end in a dot or a space, which the Win32
// name rules strip; a colon, which names a stream of another file; and a device name, CON, PRN,
// AUX, NUL, COM0 to COM9, LPT0 to LPT9, COM and LPT with a superscript 1, 2 or 3, CONIN$ and
// CONOUT$, in any case, with any extension and any spaces before it, as Windows before 11 reads
// CON.txt as CON.
func windowsNames(p string, drive bool) error {
	elems := strings.Split(p, "/")
	if drive && len(elems) > 1 {
		elems = elems[2:] // "" before the first slash, then C:
	}
	for _, e := range elems {
		switch {
		case e == "":
		case strings.HasSuffix(e, ".") || strings.HasSuffix(e, " "):
			return fmt.Errorf("the element %q ends in a dot or a space, which the Win32 name rules strip: %w", e, fs.ErrInvalid)
		case strings.Contains(e, ":"):
			return fmt.Errorf("the element %q holds a colon, which names a stream of another file: %w", e, fs.ErrInvalid)
		case device(e):
			return fmt.Errorf("the element %q is a device name, which Windows opens as the device: %w", e, fs.ErrInvalid)
		}
	}
	return nil
}

// device reports whether the element e is a Windows device name, its extension and the spaces
// before it aside.
func device(e string) bool {
	base, _, _ := strings.Cut(e, ".")
	base = strings.ToUpper(strings.TrimRight(base, " "))
	switch base {
	case "CON", "PRN", "AUX", "NUL", "CONIN$", "CONOUT$":
		return true
	}
	if len(base) < 4 || (base[:3] != "COM" && base[:3] != "LPT") {
		return false
	}
	n := base[3:]
	return len(n) == 1 && '0' <= n[0] && n[0] <= '9' || n == "\u00b9" || n == "\u00b2" || n == "\u00b3"
}

// windowsParts splits the Windows path p, written with slashes, into a URL's host and path: a
// drive path C:/a is no host and /C:/a, and a share path //host/share/a is host and /share/a. A
// host that is . or .., or that starts with ;, which names a redirector to the multiple UNC
// provider with the server in the next element, is refused, so the host a caller reads is the
// host the open reaches.
func windowsParts(p string) (host, rest string, err error) {
	switch {
	case len(p) >= 3 && p[1] == ':' && p[2] == '/' && 'a' <= p[0]|0x20 && p[0]|0x20 <= 'z':
		return "", "/" + p, nil
	case strings.HasPrefix(p, "//"):
		host, share, _ := strings.Cut(p[2:], "/")
		name, _, _ := strings.Cut(share, "/")
		if host == "" || host == "?" || host == "." || name == "" || strings.Contains(host, ":") {
			return "", "", fmt.Errorf("a share path names a host and a share; a device path names neither, and a host with a colon is a drive with one slash too few, or a port: %w", fs.ErrInvalid)
		}
		if host == ".." || strings.HasPrefix(host, ";") {
			return "", "", fmt.Errorf("the host %q is a .. or names a redirector, after which the open reaches the host the next element names, not this one: %w", host, fs.ErrInvalid)
		}
		return host, "/" + share, nil
	}
	return "", "", fmt.Errorf("not an absolute Windows path, a drive's or a share's: %w", fs.ErrInvalid)
}

// noDots refuses a slash path with a . or .. element.
func noDots(p string) error {
	for e := range strings.SplitSeq(p, "/") {
		if e == "." || e == ".." {
			return fmt.Errorf("a %s element, which a reader may clean another way: %w", e, fs.ErrInvalid)
		}
	}
	return nil
}

// knownSystem refuses a system with no file URL form here.
func knownSystem(goos string) error {
	switch goos {
	case "windows", "darwin", "linux", "android", "freebsd", "openbsd", "netbsd", "dragonfly", "illumos", "solaris", "aix":
		return nil
	}
	return fmt.Errorf("%q: no file URL form is known for this system: %w", goos, errors.ErrUnsupported)
}
