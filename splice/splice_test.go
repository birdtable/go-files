package splice_test

import (
	"bytes"
	"errors"
	"slices"
	"testing"

	"github.com/birdtable/go-files/splice"
)

const (
	begin = "<!-- begin -->"
	end   = "<!-- end -->"
)

// Splice replaces what stands between the begin line and the end line and keeps both marker lines
// and every other byte of text as it is: a line end of \r\n, a text with no last line end, the
// bytes before the begin line and after the end line. A marker line is found indented by spaces
// and tabs and ending in \r\n.
//
// What it costs when this fails: a generated file loses its markers, so the next run finds none,
// or the hand-written text around them is changed with every run.
func TestSpliceReplacesWhatStandsBetweenTheMarkersAndKeepsEveryOtherByte(t *testing.T) {
	t.Parallel()
	for _, c := range []struct{ name, text, block, want string }{
		{"plain", "head\n" + begin + "\nold\nolder\n" + end + "\ntail\n", "new\n", "head\n" + begin + "\nnew\n" + end + "\ntail\n"},
		{"crlf", "head\r\n" + begin + "\r\nold\r\n" + end + "\r\ntail\r\n", "new\r\n", "head\r\n" + begin + "\r\nnew\r\n" + end + "\r\ntail\r\n"},
		{"no last line end", begin + "\nold\n" + end, "new\n", begin + "\nnew\n" + end},
		{"nothing between", "a\n" + begin + "\n" + end + "\nb", "x\ny\n", "a\n" + begin + "\nx\ny\n" + end + "\nb"},
		{"indented markers", "\t " + begin + " \n" + end + "\n", "x\n", ""},
		{"indented and crlf", "  \t" + begin + "\r\nold\r\n\t" + end + "\r\n", "x\n", "  \t" + begin + "\r\n  \tx\n\t" + end + "\r\n"},
		{"a marker inside a line is no marker", "x " + begin + "\n" + begin + "\nold\n" + end + "\n" + end + " y\n", "new\n", "x " + begin + "\n" + begin + "\nnew\n" + end + "\n" + end + " y\n"},
	} {
		got, err := splice.Splice([]byte(c.text), begin, end, []byte(c.block))
		if c.want == "" {
			if !errors.Is(err, splice.ErrNoMarker) {
				t.Errorf("%s: %q, %v, and want ErrNoMarker: a trailing space is no part of a marker's line end", c.name, got, err)
			}
			continue
		}
		if err != nil || string(got) != c.want {
			t.Errorf("%s: %q, %v, and want %q", c.name, got, err, c.want)
		}
	}
}

// Each line of the block with something besides white space takes the begin line's indent; a line
// of spaces, tabs or a line end alone is written as it is, with no indent. An empty block leaves
// the two markers side by side.
//
// What it costs when this fails: a block in an indented list or a YAML map falls out of it, or a
// generated file gains trailing white space with every run.
func TestABlockLineWithSomethingBesidesWhiteSpaceTakesTheBeginLinesIndent(t *testing.T) {
	t.Parallel()
	text := "list:\n  " + begin + "\n  - old\n  " + end + "\n"
	for _, c := range []struct{ block, want string }{
		{"- a\n\n  \n\t\n\r\n- b\n", "list:\n  " + begin + "\n  - a\n\n  \n\t\n\r\n  - b\n  " + end + "\n"},
		{" - a\n", "list:\n  " + begin + "\n   - a\n  " + end + "\n"},
		{"", "list:\n  " + begin + "\n  " + end + "\n"},
	} {
		got, err := splice.Splice([]byte(text), begin, end, []byte(c.block))
		if err != nil || string(got) != c.want {
			t.Errorf("block %q: %q, %v, and want %q", c.block, got, err, c.want)
		}
	}
}

// A block whose last line has no \n gets one, so the end marker stands on a line of its own and
// the answer splices again to the same text as a block that ended in \n.
//
// What it costs when this fails: the end marker joins the block's last line, and every later run
// on the file is refused with ErrNoMarker.
func TestABlockWithNoLastLineEndGetsOneAndTheAnswerSplicesAgain(t *testing.T) {
	t.Parallel()
	text := []byte("  " + begin + "\nold\n  " + end + "\n")
	got, err := splice.Splice(text, begin, end, []byte("a\nb"))
	if want := "  " + begin + "\n  a\n  b\n  " + end + "\n"; err != nil || string(got) != want {
		t.Fatalf("%q, %v, and want %q", got, err, want)
	}
	again, err := splice.Splice(got, begin, end, []byte("c"))
	if want := "  " + begin + "\n  c\n  " + end + "\n"; err != nil || string(again) != want {
		t.Errorf("spliced again: %q, %v, and want %q", again, err, want)
	}
}

// Splice refuses a missing marker with ErrNoMarker, a marker on two lines with ErrMarkerTwice and
// an end marker not after the begin with ErrMarkerOrder, each plain and naming no file, and
// answers no text. A begin equal to end is ErrMarkerOrder on one line and ErrMarkerTwice on two.
//
// What it costs when this fails: a file with a torn or doubled pair of markers is rewritten from
// the wrong line, and what stood outside them is lost.
func TestSpliceRefusesAMissingMarkerATwiceMarkerAndTheEndBeforeTheBegin(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		name, text, begin, end string
		want                   error
	}{
		{"no begin", "a\n" + end + "\n", begin, end, splice.ErrNoMarker},
		{"no end", begin + "\na\n", begin, end, splice.ErrNoMarker},
		{"empty text", "", begin, end, splice.ErrNoMarker},
		{"begin twice", begin + "\n" + begin + "\n" + end + "\n", begin, end, splice.ErrMarkerTwice},
		{"end twice", begin + "\n" + end + "\n" + end + "\n", begin, end, splice.ErrMarkerTwice},
		{"end before begin", end + "\nold\n" + begin + "\n", begin, end, splice.ErrMarkerOrder},
		{"begin is end, one line", "a\n" + begin + "\nb\n", begin, begin, splice.ErrMarkerOrder},
		{"begin is end, two lines", begin + "\nold\n" + begin + "\n", begin, begin, splice.ErrMarkerTwice},
	} {
		got, err := splice.Splice([]byte(c.text), c.begin, c.end, []byte("new\n"))
		if got != nil || !errors.Is(err, c.want) {
			t.Errorf("%s: %q, %v, and want nil and %v", c.name, got, err, c.want)
			continue
		}
		if err != c.want { //nolint:errorlint // the refusal is the plain value itself, wrapped by nothing
			t.Errorf("%s: %v is wrapped, and want the plain %v", c.name, err, c.want)
		}
	}
}

// Splice changes no byte of the text it is handed, and its answer shares no memory with it, nor
// with the block.
//
// What it costs when this fails: a caller that keeps the old text to compare, or to write back on
// a failure, holds the new one.
func TestSpliceNeverChangesTheTextItIsHanded(t *testing.T) {
	t.Parallel()
	text := []byte(begin + "\nold\n" + end + "\n")
	keep := bytes.Clone(text)
	block := []byte("new\n")
	got, err := splice.Splice(slices.Clip(text), begin, end, block)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(text, keep) {
		t.Fatalf("the text became %q", text)
	}
	for i := range got {
		got[i] = 'x'
	}
	if !bytes.Equal(text, keep) || string(block) != "new\n" {
		t.Errorf("a write to the answer reached the text (%q) or the block (%q)", text, block)
	}
}
