# lock

`lock` keeps two runs of one program from changing the machine at once: `Take`, or `Wait` until a context ends, and a second run is declined with `Held`, which says who holds the lock when the holder wrote a note with `Note` and it can be read, and "another process" when it did not or it cannot. `Release` drops it; a process that exits, or is killed, drops it too, and the next `Take` wins.

## Install

```sh
go get github.com/birdtable/go-files/lock@latest
```

The full API is on [pkg.go.dev](https://pkg.go.dev/github.com/birdtable/go-files/lock). Runnable examples are in [example_test.go](example_test.go).

## How it works

The lock is flock(2) on Unix and `LockFileEx` on the first byte on Windows, taken on the one descriptor or handle `Take` opened and checked: the lock file is opened once. Once it holds the lock, `Take` reads the name again, not followed, and refuses the lock, and drops it, unless the name is still that file: a plain file, a link or a pipe put at the name after the check is never locked in its place. On aix and solaris the one lock is fcntl's, which a second open of the file in the same process wins and its close drops, so `lock` does not build there. The lock file holds nothing and is never written: who holds the lock, its process and since when, and what `Note` adds, is in the note, a file at the path the program hands `Take` or `Wait` as `note`, written only when the holder calls `Note`: `Take` and `Wait` write none, so a program says nothing of itself unless it chooses to. `lock` names no note of its own: the path is the program's, whole, not a suffix `lock` adds to the lock's name, so it is the one string `Note` writes, `Take`, `Wait` and `Release` remove and `Holder` reads, and it may be beside the lock file or wherever the note's readers look. A lock taken with `note` `""` has no note: `Take` and `Wait` remove none, a second run is told "another process", and `Note` is refused with `ErrNoNote`. A note path that cleans to the lock's own is refused before anything is opened, and a note path the volume alone reads as the lock file, another case or a second name of it, is left alone by `Take` and `Wait`, which win the lock and remove nothing there, and refused by `Note` and `Release`, which leave the lock file as it is: a note written or removed there could take the lock file's name. `Note` makes it as a temp file `<note>.<digits>` beside it and renames it over the name, and each call replaces the one before. `Take` and `Wait` remove a note an earlier holder left once they win the lock, as it names a process that holds nothing, and `Release` removes the holder's own before the lock is dropped. `Holder` reads it. A note that is not there, cut short, over 64 KiB, or not a plain file reads as held by another process, never as free and never as an error. `Note` refuses a note over 64 KiB and leaves the one before it. Nothing proves who wrote the note: whoever can make a name in its folder can write it, and a killed holder's process number may since be another process's, so the process, the time and the note are for a message only, never to signal or wait on, and nothing decides by them.

On every system a symbolic link at the lock path is opened as the link itself, never its target, and refused, so the file it leads to is not opened, made, locked or changed; a lock file that is not a plain file, a pipe or a socket, is refused too. A link at the note's path is replaced by `Note`, and removed as the link by `Take`, `Wait` and `Release`, never followed, its target untouched, and `Holder` reads anything there that is not a plain file, a link, a pipe held open by a writer or a socket among them, as a torn note, and never waits on it. On Windows their tests run with developer mode on, and do not skip. On Unix a lock file `Take` makes is 0600, set on the descriptor whatever the umask, and one that is there keeps its mode and owner, and the note is given the lock file's mode, and its owner too only when the note is in the lock file's own folder: in any other folder the note is the writing process's, as a new file there may take a group this account cannot give away, and a holder that runs as root never hands a note to another account in a folder that account could not write. On Windows a lock file `Take` makes has an access list only this account and SYSTEM reach, none of its folder's inherited, set by the create itself; on a volume that keeps no access lists (FAT, exFAT), and where this account cannot open by name what it makes with that list (a restricted token or an AppContainer the list does not name, a share that knows this account by another SID), `Take` and `Wait` refuse to make one with `ErrNoAccessList`, which matches `errors.ErrUnsupported`, and `fs.ErrPermission` too where the cause is a denial, so test `ErrNoAccessList` first, and make nothing, and a lock file there is taken as before; so do a share that will not take this account as the owner and a volume that will not say whether it keeps lists. The lock file is made and opened sharing read and write alone, so while it is held no process removes or renames it; the make's handle has no right to delete, so a contender's open of the file is not refused for sharing. Whether this account can open by name what it makes with that list is first tried on a throwaway name beside it, `.probe-` and random letters, made and removed, and only where the lock file is not there already, so a `Take` of a lock file there makes nothing; a process killed in that instant leaves the throwaway file, empty, and it is the caller's to remove, and so does a removal of it that fails, with an error that names it and is `ErrNoAccessList` only where it could not be opened by name either. A lock path whose last name ends in a dot or a space is refused on Windows with an error matching `fs.ErrInvalid`, and nothing is made: the make would keep the end, and every later call strips it. On Windows `Note` and `Release` try again for about 600 ms while another process has the note open (`Holder` and a declined `Take` open it for a moment), and the rename over a held note is refused as an access denial, so any denial there is tried again too. The window is a fact of the system, how Windows answers a reader or a scanner that holds a file for a moment, not a value of the program's, so no option lengthens or shortens it; a note held open past that stays, and `Release` drops the lock all the same and says no error. A lock file this account may not open to read and write is refused with an error that is `fs.ErrPermission`, never `Held`, and so is a missing one in a folder this account may not make a file in, with nothing made there, the throwaway name included: on Unix by the mode, on Windows by an access list or the read-only attribute, so `errors.Is(err, fs.ErrPermission)` is the one match on every system, and no error of the package's own stands for it. The lock file's folder must exist.

`TakeIn`, `WaitIn` and `HolderIn` are `Take`, `Wait` and `Holder` below an `*os.Root` the program holds, by the lock's local name and the note's, for a program that holds its state folder as a root and would otherwise lock by a path that could be swapped between steps. Every step goes through the root, by local name: the lock file is opened or made, checked, locked and its name read again, and the note made, renamed over, read and removed, each beneath the root, so a folder renamed above it, a name with `..`, a path from the top or a link to a folder outside is never reached, and a name or note that leaves the root is refused as `os.Root` refuses it, nothing made, locked or written outside; a name or note whose last element is `..` is refused so before any open, wherever it leads. Within the root a link in a folder of the name is followed as `os.Root` follows it, one with a relative target that stays inside, and a link at the last element of the lock's name or the note's is treated as `Take`'s and `Holder`'s are: refused at the lock, replaced or removed at the note, read as a torn note. The `Lock` keeps a root of its own, opened at the root's folder, so the program may close the root it passed once `TakeIn` returns, and `Note` and `Release` keep to that folder though it is renamed while the lock is held; `Release` closes it. The note's temp file below a root is `<note>.` and 26 random characters, made by an exclusive create, which follows no link. Every other promise above holds below a root as by a path: the modes, the owner, the access list, the refusals and the tries again. `Take` and `TakeIn` of the same file exclude each other.

`File` is refused with `ErrNoInherit` on every system, which matches `errors.ErrUnsupported`: the lock is this process's, and `Release` drops it for every copy of the lock file, a child's too, so a child given the file would not keep the lock. `Stat` is the lock file's, the file the lock was checked to be on, for a caller that reached the lock file by another name and compares the two with `os.SameFile`; after `Release` it is `fs.ErrClosed`. Where the lock sits on a Windows volume whose files read a zero index (a WebDAV or SMB drive) or an all-ones one (ReFS, with no 64-bit form of its 128-bit ID), `os.SameFile` has nothing to tell the lock file from another at its name, so the lock's own check of its note and path by file ID refuses with `ErrNoFileIndex`, the one value `write.ErrNoFileIndex` is, matching `errors.ErrUnsupported`. `Release` drops the lock while a fork still on its way to exec shares its descriptor. The note lives only in its own file on every system, since the lock file's first byte is locked on Windows.

- **What it imports**: the standard library, on Unix `golang.org/x/sys/unix` for the open of the last name below a root with no link followed, and on Windows `golang.org/x/sys/windows`, the module's Windows calls and `internal/fileid`; no other module, on any system. The layer rows of the root's [.golangci.yml](../.golangci.yml) say so, and `task lint` holds them.

`go doc github.com/birdtable/go-files/lock` has the rest; every exported name is documented.

## Clearing the folders runs leave behind

A program whose runs each work in a scratch folder of their own, `run-<random>` under one base, clears the folders that stopped runs left with no function of `lock`'s: each run holds one lock while it is in its folder, and a sweep takes the same lock and removes every `run-*` folder beneath the base before it lets go. `sweep` is in [example_test.go](example_test.go), and the tests beside it hold that it leaves every folder while a run holds the lock, that no run starts while it removes, and that it removes the runs' folders alone, a link among them removed and never followed.

```go
func sweep(path, base string, removed func(name string)) (err error) {
	l, err := lock.Take(path, "")
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, l.Release()) }()
	root, err := os.OpenRoot(base)
	if err != nil {
		return err
	}
	defer root.Close()
	names, err := fs.Glob(root.FS(), "run-*")
	if err != nil {
		return err
	}
	for _, name := range names {
		if err := root.RemoveAll(name); err != nil {
			return err
		}
		removed(name)
	}
	return nil
}
```

```go
func ExampleTake_sweep() {
	dir, err := os.MkdirTemp("", "myprogram")
	if err != nil {
		log.Fatal(err)
	}
	defer os.RemoveAll(dir)
	base := filepath.Join(dir, "scratch")
	if err := os.Mkdir(base, 0o700); err != nil {
		log.Fatal(err)
	}
	for range 2 {
		if _, err := os.MkdirTemp(base, "run-"); err != nil {
			log.Fatal(err)
		}
	}

	n := 0
	err = sweep(filepath.Join(dir, "scratch.lock"), base, func(string) { n++ })
	fmt.Println(n, err)
	// Output: 2 <nil>
}
```

- **The lock goes beside the base, never in it.** On Windows a folder that holds an open lock file cannot be removed, and a sweep that lets go first to remove it lets a run start in between.
- **The sweep holds the lock until its last removal.** A run takes the lock before it makes its folder, so a sweep that holds it never removes a folder a run is starting in.
- **Removal goes through an `os.Root` at the base,** so a name beneath it that is a link is removed as the link, and what it leads to is left.
- **A sweep that is declined says so**, with an error `lock.IsHeld` reports: the runs' folders are in use, and it tries later.
- **A program that takes a lock for the whole program as well** takes that one first and the scratch lock second, in every run and in the sweep, so two of them never wait on each other.

A program that would rather lock each folder by itself has three more traps, which is why `lock` has no sweep of its own:

- **A lock file inside the folder it guards** is held open, so the folder cannot be removed on Windows while the lock is held; it goes beside the folder, `run-x.lock` by `run-x`.
- **A run that makes its folder before it takes its lock** can have the folder removed from under it by a sweep that comes in between and takes the new, free lock itself; the run takes the lock first.
- **A lock file that is missing, a link or not a plain file:** `Take` makes a missing one, which is right for a run that stopped; a link or a pipe is refused, and the sweep reports it, never counting it as held or free. The lock files then build up, since removing one after `Release` races a `Take` of the same name, as `Take` says.

## Pipes on Windows

A lock path, a note or a root that leads to a host's named pipes would have the lock's look and
open reach a pipe, whose server could act as the program. On Windows each call refuses one before
any open of it, matching `fs.ErrInvalid`, and each look and open by path asks for identification
alone.

| guard | test |
| --- | --- |
| `Take`, `Wait` and `Holder`: a lock path or a note that names a device or a pipe (`\\.\`, or `\\?\` or `\??\` naming neither a drive nor a `Volume{GUID}`), or is on a host's pipes share (`IPC$` or `pipe`, in any UNC spelling or a `\\?\GLOBALROOT\Device\Mup\` name), or whose folder, opened with identification alone, is a host's pipes by its own handle, a junction to the pipes' folder among them, refused before any open of it | `TestEachCallRefusesALockOrANoteInAHostsPipes` |
| `TakeIn`, `WaitIn` and `HolderIn`: a root on the pipes' device or on a host's pipes share, as confine refuses one, refused before any open beneath it | `TestEachCallRefusesALockOrANoteInAHostsPipes` (native: where the system opens no root on a folder, logged) |
| the looks and the open of a lock path or note by path (`winapi.Lstat`, `winapi.Stat`, `winapi.CreateFile`), and the folder a lock file is made in by path (`winapi.OpenFolder`), ask for identification alone; the make refuses a folder on a host's pipes by its handle | internal/winapi's `TestAFolderOnAHostsPipesIsRefusedByItsOwnHandleAndNoMakeInItReachesAPipe` |
| no `os` call by name in a file of lock that Windows builds, but `os.Remove`, `os.Rename` and `os.CreateTemp` of a note, each after the check found its folder on a disk and with a nolint that says so | the lint's pipe-by-path row; the lint task's plants, Windows-built `lock/p.go` and `lock/p_unix.go` each calling `os.ReadFile`, named by the row |

## Libraries looked at

The standard library and `golang.org/x` first.

| looked at | why it falls short, or what it gives | verdict |
|---|---|---|
| the standard library | `syscall.Flock` locks a descriptor already open; on Windows `LockFileEx` is in `golang.org/x/sys/windows`, kept by hand | **used**: flock(2) and `LockFileEx` on the file `Take` opened and checked, in lock's two system files, which the module's lint excepts from its ban on path locks |
| `github.com/gofrs/flock` v0.13.2 | flock(2) on Unix, and the first byte on Windows, so the note goes beside the lock file; opens its own descriptor or handle by path at every lock and takes none, so `Take` checks it is the file it opened, and `File` is refused (read 11 Nov 2026 at v0.13.1; v0.13.2 leaves its Windows file as it was and only tries the lock once before it looks at a cancelled context). On aix and solaris it takes fcntl locks, which a second open in the process drops | not taken: it opens the lock file a second time, by path, so `Take` opened it twice |
| `nightlyone/lockfile` | a PID file, which names a process that may be gone and whose number another may take | not taken |
| `rogpeppe/go-internal/lockedfile` | an internal package of the Go tool, copied out, with no API promise | not taken |

The verdict: the kernel's lock on the file `Take` opened and checked, the name read again once it is held.
