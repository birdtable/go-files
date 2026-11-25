package trust

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"syscall"
)

// Interpreters is the programs a file's #! line would have the system start, in order: the
// interpreter path names, and while that one is itself a script, the interpreter it names, the
// last being the program the system runs. A file that does not start with #! is
// none, and so is every file on Windows, which reads no #! line. Each is the name as the line
// writes it, read by the system's own rule for that line, each proven by starting scripts there:
// on Linux the first 256 bytes, a line no newline ends before a NUL in them being cut, and its
// interpreter having to end in them; on macOS a line ending at a newline or a # within the first
// 512 bytes; on both, spaces and tabs before the interpreter dropped, and the interpreter ending
// at a space, a tab or a NUL. The chain ends at /usr/bin/env, the program the system starts for
// #!/usr/bin/env: what env then runs it finds on the PATH of the run, which the caller holds.
// Nothing is judged: each program's caller walks it with Path, as a Program.
//
// It is an error, and no chain, where the system would run none of it, matching the errno the
// system gives: a line that names no interpreter, or one cut before it ends (ENOEXEC); an
// interpreter that is not absolute, which the system looks for in the folder the run is in
// (ENOENT); a name that is not a regular file (EACCES), or cannot be read, though the system may
// start a program its runner cannot read; on Linux a sixth script in one chain (ELOOP), and on
// macOS any interpreter that is itself a script (ENOEXEC). On the other Unix systems, whose rules
// are not proven here, a file that starts with #! is an error matching errors.ErrUnsupported.
func Interpreters(path string) ([]string, error) { return interpreters(path, kernel()) }

// shebang is how one system reads a #! line: size bytes of the file, zero past its end, read by
// line to the interpreter the line names, with at most scripts scripts in one chain. The zero
// shebang reads no line, as Windows; one with an unsupported error refuses every script.
type shebang struct {
	size        int
	line        func(buf []byte) (string, error)
	scripts     int
	deep        error
	unsupported error
}

// errNoInterpreter is a #! line that names no interpreter the system starts.
var errNoInterpreter = fmt.Errorf("its #! line names no interpreter: %w", syscall.ENOEXEC)

// linuxShebang is Linux's binfmt_script, load_script in fs/binfmt_script.c, as of Linux 5.1.
func linuxShebang() shebang {
	return shebang{size: 256, line: linuxLine, scripts: 5,
		deep: fmt.Errorf("it starts a chain of more than 5 scripts, which Linux refuses: %w", syscall.ELOOP)}
}

// darwinShebang is macOS's exec_shell_imgact, in bsd/kern/kern_exec.c.
func darwinShebang() shebang {
	return shebang{size: 512, line: darwinLine, scripts: 1,
		deep: fmt.Errorf("its interpreter is itself a script, which macOS does not start: %w", syscall.ENOEXEC)}
}

// unprovenShebang is a Unix system whose rule for a #! line is not proven here: every file that
// starts with #! is refused.
func unprovenShebang() shebang {
	return shebang{size: 2,
		unsupported: fmt.Errorf("it starts with #!, and how this system reads that line is not proven here: %w", errors.ErrUnsupported)}
}

// linuxLine is the interpreter Linux reads from buf, the first 256 bytes of a file that starts
// with #!, as load_script reads it. A newline before the first NUL ends the line; with none, the
// line is the first 255 bytes, and the interpreter must end by the 256th: Linux 7.0 starts an
// interpreter a space at the 256th byte ends. Spaces and tabs before the interpreter and at the
// end are dropped, and it ends at a space, a tab or a NUL.
func linuxLine(buf []byte) (string, error) {
	end := len(buf) - 1
	upToNUL, _, _ := bytes.Cut(buf, []byte{0})
	lineEnd := bytes.IndexByte(upToNUL, '\n')
	if lineEnd < 0 {
		i := nonSpaceTab(buf, 2, len(buf))
		if i < 0 {
			return "", errNoInterpreter
		}
		if terminator(buf, i, len(buf)) < 0 {
			return "", fmt.Errorf("its #! line is cut before its interpreter ends, at 256 bytes: %w", syscall.ENOEXEC)
		}
		lineEnd = end
	}
	for lineEnd > 2 && spaceTab(buf[lineEnd-1]) {
		lineEnd--
	}
	i := nonSpaceTab(buf, 2, lineEnd)
	if i < 0 {
		return "", errNoInterpreter
	}
	sep := terminator(buf, i, lineEnd)
	if sep < 0 {
		sep = lineEnd
	}
	if sep == i {
		return "", errNoInterpreter // a NUL first
	}
	return string(buf[i:sep]), nil
}

// nonSpaceTab is the index of the first byte of b[from:to] that is not a space or a tab, or -1.
func nonSpaceTab(b []byte, from, to int) int {
	for i := from; i < to; i++ {
		if !spaceTab(b[i]) {
			return i
		}
	}
	return -1
}

// terminator is the index of the first byte of b[from:to] that ends a word on Linux, a space, a
// tab or a NUL, or -1.
func terminator(b []byte, from, to int) int {
	for i := from; i < to; i++ {
		if spaceTab(b[i]) || b[i] == 0 {
			return i
		}
	}
	return -1
}

func spaceTab(c byte) bool { return c == ' ' || c == '\t' }

// darwinLine is the interpreter macOS reads from buf, the first 512 bytes of a file that starts
// with #!. The line ends at a newline or a #, which must come within them; spaces and tabs before
// the interpreter are dropped, and it ends at a space, a tab or a NUL.
func darwinLine(buf []byte) (string, error) {
	eol := func(c byte) bool { return c == '\n' || c == '#' }
	i := 2
	for i < len(buf) && spaceTab(buf[i]) {
		i++
	}
	if i == len(buf) || eol(buf[i]) {
		return "", errNoInterpreter
	}
	start := i
	for i < len(buf) && !eol(buf[i]) {
		i++
	}
	if i == len(buf) {
		return "", fmt.Errorf("its #! line has no end in its first 512 bytes: %w", syscall.ENOEXEC)
	}
	name := buf[start:i]
	if j := bytes.IndexFunc(name, func(r rune) bool { return r == ' ' || r == '\t' || r == 0 }); j >= 0 {
		name = name[:j]
	}
	if len(name) == 0 {
		return "", errNoInterpreter
	}
	return string(name), nil
}

// interpreters is Interpreters by the rules of k.
func interpreters(path string, k shebang) ([]string, error) {
	if k.size == 0 {
		return nil, nil
	}
	var chain []string
	for p, scripts := path, 0; ; scripts++ {
		buf, err := head(p, k.size)
		if err != nil {
			return nil, fmt.Errorf("trust.Interpreters: %w", err)
		}
		if buf[0] != '#' || buf[1] != '!' {
			return chain, nil
		}
		if k.unsupported != nil {
			return nil, fmt.Errorf("trust.Interpreters: %s: %w", p, k.unsupported)
		}
		if scripts == k.scripts {
			return nil, fmt.Errorf("trust.Interpreters: %s: %w", path, k.deep)
		}
		name, err := k.line(buf)
		if err != nil {
			return nil, fmt.Errorf("trust.Interpreters: %s: %w", p, err)
		}
		if !filepath.IsAbs(name) {
			return nil, fmt.Errorf("trust.Interpreters: %s: its interpreter %q is not absolute, so the system looks for it in the folder a run is in: %w", p, name, syscall.ENOENT)
		}
		chain = append(chain, name)
		p = name
	}
}

// head is the first size bytes of the regular file at p, zero past its end. p is read by its
// name first, and a name that is not a regular file is refused before it is opened, so no
// device's driver runs its open, as a tape's rewinds, for a name a script's #! line chose. The
// open does not wait, so a pipe put at p after that read is refused, never read, and the type is
// read again on the descriptor, which is what is read from.
func head(p string, size int) ([]byte, error) {
	info, err := os.Stat(p)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, notRegular(p, info)
	}
	f, err := os.OpenFile(p, os.O_RDONLY|openNoWait, 0) //nolint:gosec // G304: Interpreters reads the path its caller names, and the interpreter that path names, and checks this descriptor
	if err != nil {
		return nil, err
	}
	defer f.Close() //nolint:errcheck // a file only read
	if info, err = f.Stat(); err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, notRegular(p, info)
	}
	buf := make([]byte, size)
	if _, err := io.ReadFull(f, buf); err != nil && !errors.Is(err, io.EOF) && !errors.Is(err, io.ErrUnexpectedEOF) {
		return nil, err
	}
	return buf, nil
}

// notRegular is the refusal of p, whose facts are info, as a name the system starts no program
// from.
func notRegular(p string, info fs.FileInfo) error {
	return fmt.Errorf("%s is %s, not a regular file, and the system starts none: %w", p, typeName(info.Mode().Type()), syscall.EACCES)
}
