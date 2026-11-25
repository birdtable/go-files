package write

import (
	"os"
)

// syncWith flushes the folder with flush instead of syncFolder, so a test can make a flush fail,
// which no test machine's disk does on its own. put reads it only under testing.Testing().
func syncWith(flush func(root *os.Root, dir string) error) Option {
	return func(o *options) { o.flush = flush }
}

// stepWith calls at at each step of a write: "temp" once the temp file is made, before it is
// opened again to read, under Runnable's fork lock, so a test starts no process there, "closed"
// once it is written, owned, flushed and closed, outside any fork lock, before Check, "written" after
// Check and the temp file's last look, before the rename, "placed" once it is renamed or linked
// in place, before the folder's flush, "taking" in linked between its two looks at a name just linked, "gripping" in ready between its compare and gripTemp, "look" in regular between the look and the open, and "kept"
// in kept between its look at the old file and heldBy's open of it, before anything is made; in
// Append, "held" once the log is held and "checked" once it matched the name, before its names
// are counted; in Swap, "told" after its looks, before it reads the files' indexes, "swap" after it
// reads them, before the old file is moved aside, and "aside" once it is, before the new file is
// moved in. A test swaps a file or a folder there, or kills the writer, which no test can time
// from outside.
func stepWith(at func(string)) Option {
	return func(o *options) { o.step = at }
}

// asideWith is Aside for a test's appendWith, whose options settle builds: Append takes no default
// name aside, so each test names its own.
func asideWith(name string) Option {
	return func(o *options) { o.aside = name }
}

// removeWith answers linked's remove of a name, and place's remove of the temp name after the link,
// with remove, so a test can make it fail as it does in a folder this account may not write to,
// which root, running the tests, may write to. take reads it only under testing.Testing().
func removeWith(remove func(root *os.Root, name string) error) Option {
	return func(o *options) { o.remove = remove }
}

// closeWith answers the close of the grip with answer, which gets the close's own result, so a test
// can make it fail, which no descriptor opened to read does on its own. put reads it only under
// testing.Testing().
func closeWith(answer func(error) error) Option {
	return func(o *options) { o.closing = answer }
}
