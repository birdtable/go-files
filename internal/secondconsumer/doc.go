// Package secondconsumer is a program of another business than go-files', written against go-files
// as any program takes it: a kiln's watcher, which keeps its settings readable by its group, logs
// each firing under a small cap, keeps its old settings in a folder of their own, locks the kiln
// with a note of its own, and pins its glaze recipes by a digest. Every value it hands a package is
// deliberately unlike the usual ones: other names, other modes, other caps, other hold times and
// another layout for the copies it keeps. Its tests are the program; the pre-commit tier runs
// them, so a change that makes go-files decide a value for its caller fails before it is
// committed.
//
// The names a program's folders take on Windows (State, Cache and Run beneath its own folder) are
// For's, but appdirs.Folders is five strings the caller may replace, so they are set; so is the
// lock file's mode, which Take keeps when the caller makes the file first, and the log's mode and
// the name it moves aside to, which Append takes as LogMode and Aside, Aside with no default; a
// program that keeps more old logs moves its last one on itself; so is the name of each copy Keep
// makes, in a folder of the program's own; so is whether its lock carries a note at all, which
// lock writes only when the program calls Note, and the note's path, which the program hands Take
// and Wait whole, as lock names no file of its own. On Windows, write and lock try again for about
// 600 ms when another process holds a file open: that is a fact of the system, not a value of the
// program's.
//
// On Windows a file or folder go-files makes with no mode asked, the settings, a kept copy, a new
// log, a lock file Take makes, the temp folder and a folder confine.Mkdir makes 0o700, has an
// access list only the watcher's account and SYSTEM reach, and a mode the watcher asks of
// confine.Mkdir there, its attic's 0o750, is refused with confine.ErrNoMode, as write refuses its
// Mode: a folder there has a list, not a mode.
//
// It imports write, lock, confine, trust, appdirs, treesum and the standard library, as any
// program does, and its tests filestest besides; no package imports it.
package secondconsumer
