<p align="center">
  <picture>
    <source media="(prefers-color-scheme: dark)" srcset="docs/mark/go-files-banner-dark.svg">
    <source media="(prefers-color-scheme: light)" srcset="docs/mark/go-files-banner-light.svg">
    <img src="docs/mark/go-files-banner-light.svg" width="285" alt="go-files: a page, its folded corner in coral, then a coral bird in flight and the name">
  </picture>
</p>

<p align="center">
  <a href="https://github.com/birdtable/go-files/releases"><img src="https://img.shields.io/github/v/release/birdtable/go-files" alt="Latest release"></a>
  <a href="https://pkg.go.dev/github.com/birdtable/go-files"><img src="https://pkg.go.dev/badge/github.com/birdtable/go-files.svg" alt="Go reference"></a>
  <a href="LICENSE"><img src="https://img.shields.io/github/license/birdtable/go-files" alt="License: Apache-2.0"></a>
</p>

<p align="center"><b>Safe file writes, locks and paths for Go programs.</b></p>

go-files is for Go programs that write, lock and open files on a machine where another account
may be working against them. It replaces a file whole, keeps two runs of a program apart, holds a
path from untrusted input inside its folder with no link followed, and checks that nobody else
can change what your program is about to run or read. Your program hands it every name, path and
mode; the library decides none of them.

## Install

```sh
go get github.com/birdtable/go-files@latest
```

Then import the packages you need, such as `github.com/birdtable/go-files/write`. go-files needs
Go 1.27.1 or later, and brings only two modules with it:

- `golang.org/x/sys`, for the system calls the standard library does not offer, such as opening
  a file relative to a folder with no link followed, locking a file, renaming without replacing,
  and reading a file's owner and access list on Unix and Windows.
- `golang.org/x/text`, whose `unicode/norm` alone `confine` uses, to tell whether two names are
  one name to a folder that folds case or Unicode forms.

go.mod also names `github.com/birdtable/go-testkit`, a test kit that gives each of go-files' own
tests an environment of its own. Only the tests import it, so a program that imports go-files
never builds it.

## Quick start

Save a settings file whole: a reader sees the old file or the new one, never half of either.

```go
func ExampleFile() {
	dir, err := os.MkdirTemp("", "myprogram")
	if err != nil {
		log.Fatal(err)
	}
	defer os.RemoveAll(dir)
	settings := filepath.Join(dir, "settings.json")

	err = write.File(settings, []byte("{\"theme\": \"dark\"}\n"))
	if err != nil {
		log.Fatal(err)
	}
	b, err := os.ReadFile(settings)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Print(string(b))
	// Output: {"theme": "dark"}
}
```

This is `ExampleFile` in [write/example_test.go](write/example_test.go), so `go test` runs it.

## What's inside

| package | what it does |
|---|---|
| [`write`](write/README.md) | Replaces a file whole, keeps a log under a cap, keeps a copy and puts it back, and swaps in a new copy of a program that may be running. |
| [`lock`](lock/README.md) | Keeps two runs of one program apart, and tells the second who holds the lock. |
| [`confine`](confine/README.md) | Opens, makes, lists, renames and copies files named by untrusted input inside one folder, following no link, and tells how a folder reads names. |
| [`nowait`](nowait/README.md) | Opens a path as `os.OpenFile` does, but never waits for a pipe's other end. |
| [`splice`](splice/README.md) | Replaces the text between two marker lines, as a pure function over bytes. |
| [`trust`](trust/README.md) | Checks that no other account can change a program, a key or a file before you run or read it. |
| [`treesum`](treesum/README.md) | Pins a folder tree by a digest, links and all, and checks it before you run from it. |
| [`appdirs`](appdirs/README.md) | Names the folders a program keeps its files in, on every system, says what kind of volume holds a folder, and makes a safe temp folder. |
| [`filestest`](filestest/README.md) | Helpers for your tests: private folders, programs a test starts, and checks of names on every system. |

Each package stands alone: take one or several. Each README says what its calls promise, and
[pkg.go.dev](https://pkg.go.dev/github.com/birdtable/go-files) has the full API.

## A short tour: one run at a time

While one run of your program holds the lock, a second is refused, and the `Held` error it gets
names the holder from a note the first run wrote. `lock` names no file of its own: your program
chooses both paths.

```go
func ExampleTake() {
	dir, err := os.MkdirTemp("", "myprogram")
	if err != nil {
		log.Fatal(err)
	}
	defer os.RemoveAll(dir)
	path := filepath.Join(dir, "myprogram.lock")
	note := filepath.Join(dir, "myprogram.holder") // the program names its note: lock names none

	l, err := lock.Take(path, note)
	if err != nil {
		log.Fatal(err)
	}
	defer l.Release()
	if err := l.Note("building"); err != nil {
		log.Fatal(err)
	}

	_, err = lock.Take(path, note) // a second run, while the first holds the lock
	var h lock.Held
	fmt.Println(errors.As(err, &h) && h.By == os.Getpid())
	// Output: true
}
```

Both examples keep their files in a folder of the program's own: `os.MkdirTemp` makes it 0700,
under a name no other account chose. In a shared folder such as `/tmp`, another account could
make the settings file first, take the lock first, or put a link at its name, which `Take`
refuses. A real program takes its folders from [`appdirs`](appdirs/README.md).

## Systems

go-files works on **Linux, macOS and Windows**. Its tests pass on Linux on amd64, macOS on arm64
and Windows on amd64, unprivileged: no root on Linux and no elevated shell on Windows. Every
change is also built and vetted for the three systems on amd64 and arm64 and for Linux on 386 and
arm, and linted for FreeBSD and NetBSD, where the tests have not run. No package is for one system
only.

- **Linux** needs 5.8 or later. Below it, `trust` cannot read a mount's type and fails closed: it
  refuses every name on tmpfs, overlayfs, FUSE, NFS, 9p, btrfs or ZFS, `$XDG_RUNTIME_DIR` and
  `/dev/shm` among them. The test helper `trusttest.Bind` needs Linux 5.12 and a seccomp profile
  that allows `open_tree`, `mount_setattr` and `move_mount`, and skips its test, saying so,
  where either is missing.
- **Windows:** paths over 260 bytes are proved with `LongPathsEnabled` set to 1, and not measured
  with it unset. The tests that make a symbolic link need developer mode.
- **FreeBSD:** `appdirs` names its folders, and its tests check them from any system.
- `lock` does not build on aix or solaris, where the only lock is fcntl's, which a second open in
  the same process drops.

<details>
<summary>What each package refuses on each system</summary>

| package | Linux | macOS | Windows |
|---|---|---|---|
| `write` | nothing | nothing | `Owner` with any ids but exactly -1 and -1, a negative one included, and `KeepOwner` over another account's file or over a name that is neither a plain file nor a link, as a junction or a reparse point of a driver's own tag, with `ErrNoOwner`; `NoReplace` on a volume that keeps no hard links (FAT, exFAT), with `ErrNoHardLinks`; on exFAT, a file over one another process holds open, refused as access denied once the tries of about 600 ms are spent, with the old file left as it was; the window is the system's, how it answers a scanner that holds a file for a moment, not the program's, and has no option. `LogMode`, a `Mode` without owner read or with setuid, setgid or sticky, and `KeepMode` over a file whose list grants any account but the writer's, SYSTEM and Administrators, or over a name that is neither a plain file nor a link, as a junction or a reparse point of a driver's own tag, with `ErrNoMode`, as a new file has an access list, not a mode: only this account and SYSTEM reach it, and none of its folder's is inherited. A file to make on a volume that keeps no access lists (FAT, exFAT), or one where this account cannot open by name what it makes with that list (a restricted token or an AppContainer the list does not name, a share that knows this account by another SID), with `ErrNoAccessList`, nothing made, so every write there, a replace included, as each makes a new file first; a log there is written as before until `Append`'s first move, whose new log is refused. The folder is not flushed: a write's rename is written through to the disk instead, and a volume that refuses that gives `ErrNotDurable` with the file in place. A name with a component ending in a dot or a space, with an error matching `fs.ErrInvalid`, before anything is opened |
| `lock` | `Lock.File`, with `ErrNoInherit`: `Release` drops the lock for every copy of the file, so a child given it would not keep it | `Lock.File`, with `ErrNoInherit`, as on Linux | `Lock.File`, with `ErrNoInherit`, as on Linux; a lock file to make on a volume that keeps no access lists (FAT, exFAT), or one where this account cannot open by name what it makes with that list (a restricted token or an AppContainer the list does not name, a share that knows this account by another SID), with `ErrNoAccessList`, nothing made: a lock file there is taken as before |
| `confine` | `RenameNoReplace` and `RenameExchange` on a kernel or file system without `renameat2` or its flag | `RenameNoReplace` and `RenameExchange` on a file system that does not take the flag, FAT among them | `RenameExchange`, always: no call there swaps two names in one step; `Mkdir` with a `perm` without owner read, write and search (0o700) or with a bit beyond the nine, with `ErrNoMode`, as a folder there has an access list, not a mode, and a folder to make on a volume that keeps no access lists (FAT, exFAT), or one where this account cannot open by name what it makes with that list (a restricted token or an AppContainer the list does not name, a share that knows this account by another SID), with `ErrNoAccessList`; `LinkFolder`, with `fs.ErrInvalid`, a target not on a drive letter of a fixed or removable disk (a share, a mapped letter and a `subst` letter among them), one with a component ending in a dot or a space, one too long for a junction's 16 KiB of data, and one on a letter `DefineDosDevice` or a link object made in a logon session, or that the mount manager did not mount |
| `nowait` | nothing | nothing | nothing |
| `splice` | nothing | nothing | nothing |
| `trust` | a program run as the kernel's overflow account where `/proc/self/uid_map` is not the whole range, or where the overflow account cannot be read, with `ErrOverflow`, for `Path`, `ReadOwned`, `Held` and `Account`; a name on a file system whose owners it does not judge, as CIFS, 9p, vfat, exfat or squashfs, with `ErrMount`, until `Who.FileSystems` names the type; a name on a disk mounted `nosuid`, as an account's stick, a CIS `/home` or every disk in a `ProtectSystem=strict` unit is, or `noexec` without `nosuid`, as usbmount mounts a stick and `NoExecPaths=` a unit's disks, with `ErrMount`, until `Who.Mounts` names its folder; below Linux 5.8, every name on tmpfs, overlayfs, FUSE, NFS, 9p, btrfs or ZFS, `$XDG_RUNTIME_DIR` and `/dev/shm` among them; a key, a private file or a `PrivateFolder` on an NFS mount that shows no NFSv4 list, as NFSv3 | a name whose access list cannot be read; a name on a volume mounted to ignore ownership, with `ErrOwnersIgnored`; a name replaced by another file at each of eight tries to read it, with `ErrReplaced`, for `Path`, `ReadOwned` and, on macOS, `Held` | nothing |
| `appdirs` | nothing | nothing | `Temp`'s folder to make on a volume that keeps no access lists (FAT, exFAT), or one where this account cannot open by name what it makes with that list (a restricted token or an AppContainer the list does not name, a share that knows this account by another SID), with `ErrNoAccessList`, nothing made; `Free` refuses only on a system other than these and FreeBSD, and `Volume` only on a system other than these |
| `treesum` | nothing | nothing | nothing |

A call in this table that refuses changes nothing. The refusals of `confine`'s renames, `appdirs.Free` and `appdirs.Volume`,
`write`'s `ErrNoOwner` and `ErrNoMode`, `confine.ErrNoMode`, each package's `ErrNoAccessList`, and `trust`'s of a list it cannot read on macOS, match `errors.ErrUnsupported`, and an `ErrNoAccessList` whose cause is a denial, as under a restricted token, matches `fs.ErrPermission` too, so a handler tests `ErrNoAccessList` first; `LinkFolder`'s on Windows and `write`'s of a name match `fs.ErrInvalid`, and `ErrOwnersIgnored` and `ErrMount` match `ErrNotOwners`. On
FreeBSD, NetBSD and every other Unix, `trust` reads no access list, and so refuses every name,
matching `errors.ErrUnsupported` too. On NetBSD and every system but Linux, macOS, FreeBSD and Windows, `confine.LinkFolder` refuses with `errors.ErrUnsupported`. `lock`'s `ErrNoInherit` matches it too, and `lock` does not build on aix or solaris, where the one lock is fcntl's, which a second open in the process drops.

</details>

## Stability

go-files is v0: the API may change between minor versions. [CHANGELOG.md](CHANGELOG.md) says what
each release changed. [api/contract.txt](api/contract.txt) lists every promise a program may build
on, with the test that proves it and the systems it holds on; a promise a release drops is listed
in [api/breaks](api/breaks).

## Contributing

Issues and pull requests are welcome. [CONTRIBUTING.md](CONTRIBUTING.md) says how to run the checks
and what a pull request needs.

## Security

Please report a vulnerability privately, never in a public issue. [SECURITY.md](SECURITY.md) says
how.

## License

[Apache-2.0](LICENSE). See also [NOTICE](NOTICE). The marks under `docs/mark/` are the project's own.
