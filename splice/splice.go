// Package splice replaces what stands between two marker lines of a text, as a pure function over
// bytes: it reads no file and imports the standard library alone, so a program built for wasip1
// with the standard library only can take it.
package splice

import (
	"bytes"
	"errors"
)

// The refusals of Splice. Each is a plain value that names no file: the caller adds the file's name
// with %w.
var (
	// ErrNoMarker is the answer when the text holds no line for the begin marker, or none for the
	// end marker.
	ErrNoMarker = errors.New("a marker line is missing")
	// ErrMarkerTwice is the answer when a marker stands on two lines or more.
	ErrMarkerTwice = errors.New("a marker line stands twice")
	// ErrMarkerOrder is the answer when the end marker does not stand after the begin marker.
	ErrMarkerOrder = errors.New("the end marker does not stand after the begin marker")
)

// Splice answers text with what stands between the begin line and the end line replaced by block.
// Both marker lines stay. A line ends at \n, or at the end of text. It is a marker when it equals
// the marker once its \n, then one trailing \r, then its leading spaces and tabs are taken off, so
// a marker line that ends \r\n or is indented is found. Every byte of text outside the markers,
// line ends included, is kept as it is, and text is never changed.
//
// block is lines, each ending in \n. Each line of block with something besides white space, as
// bytes.TrimSpace takes it, is written after the begin line's indent, the spaces and tabs before
// its marker; a line of white space alone is written as it is, with no indent, so no trailing white
// space is made. Every byte of block is written as given, and a \n is added after a last line that
// has none, so the end marker always stands on a line of its own and the answer splices again. An
// empty block leaves the two marker lines side by side.
//
// It refuses with ErrNoMarker when either marker has no line, ErrMarkerTwice when either has two
// or more, and ErrMarkerOrder when the end marker's line is not after the begin marker's, so a
// begin equal to end is always refused. It reads no file.
func Splice(text []byte, begin, end string, block []byte) ([]byte, error) {
	bEnd, eAt, indent, err := find(text, begin, end)
	if err != nil {
		return nil, err
	}
	out := append(make([]byte, 0, len(text)+len(block)), text[:bEnd]...)
	out = indented(out, block, indent)
	return append(out, text[eAt:]...), nil
}

// find answers where the begin line ends, where the end line starts and the begin line's indent, or
// Splice's refusal.
func find(text []byte, begin, end string) (bEnd, eAt int, indent []byte, err error) {
	var nb, ne, bAt int
	for at := 0; at < len(text); {
		next := len(text)
		if i := bytes.IndexByte(text[at:], '\n'); i >= 0 {
			next = at + i + 1
		}
		core, lead := marker(text[at:next])
		if string(core) == begin {
			if nb++; nb == 1 {
				bAt, bEnd, indent = at, next, lead
			}
		}
		if string(core) == end {
			if ne++; ne == 1 {
				eAt = at
			}
		}
		at = next
	}
	switch {
	case nb == 0 || ne == 0:
		return 0, 0, nil, ErrNoMarker
	case nb > 1 || ne > 1:
		return 0, 0, nil, ErrMarkerTwice
	case eAt <= bAt:
		return 0, 0, nil, ErrMarkerOrder
	}
	return bEnd, eAt, indent, nil
}

// indented appends block to out, each line with something besides white space after indent, and a
// \n after a last line that has none.
func indented(out, block, indent []byte) []byte {
	for len(block) > 0 {
		line := block
		if i := bytes.IndexByte(block, '\n'); i >= 0 {
			line = block[:i+1]
		}
		block = block[len(line):]
		if len(bytes.TrimSpace(line)) > 0 {
			out = append(out, indent...)
		}
		out = append(out, line...)
		if line[len(line)-1] != '\n' {
			out = append(out, '\n')
		}
	}
	return out
}

// marker is a line without its \n, one trailing \r and its leading spaces and tabs, and those
// leading bytes.
func marker(line []byte) (core, lead []byte) {
	line = bytes.TrimSuffix(line, []byte("\n"))
	line = bytes.TrimSuffix(line, []byte("\r"))
	core = bytes.TrimLeft(line, " \t")
	return core, line[:len(line)-len(core)]
}
