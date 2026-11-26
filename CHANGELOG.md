# Changelog

What each release of go-files changed. Made by changie from the files in .changes/unreleased.


## v0.18.0 (2026-11-26)

v0.18.0 adds one function to `appdirs`, `Volume`, which answers what kind of volume holds a path, `Local`, `Network`, `FUSE`, `SyncPlaceholder` or `Unknown`, so a program that keeps a database or a lock file there can refuse a volume whose locks and fsync it may not trust; only `Local` reads as local, and an error and `Unknown` are separate answers. Windows Cloud Files detection is not yet proved on a real OneDrive sync root, and until the run of go-files#19 proves it, a caller should take `SyncPlaceholder` on Windows as best effort. The pre-push tier is now judged by its CPU time. No exported name is removed, moved or changed.

The full release checks, `task release`, ran on macOS at the tagged commit. On Linux, macOS and Windows, the test workflow ran at the tagged commit in CI: go vet, the tests with the race detector, lint, the API check and the vulnerability check. No full release checks ran on Linux or Windows for this release.

### Added

- appdirs.Volume(path) answers what kind of volume holds a path, links followed, as an appdirs.VolumeInfo: Type, the system's own name for the file system, and Kind, one of appdirs.Local, Network, FUSE, SyncPlaceholder and Unknown. VolumeInfo.Local() is true for Local alone; the zero VolumeKind is Unknown, and an error comes with the zero VolumeInfo, so neither reads as local. Removable disks are Local. What is detected where: macOS asks statfs's type name and local-mount flag: APFS, HFS+, FAT and exFAT with the flag are Local, a type that holds fuse is FUSE whatever the flag says, NFS, SMB, AFP, WebDAV, CIFS and FTP are Network, FUSE-T among them, as it mounts through NFS or SMB, and a path in a File Provider domain, told by the system's domain mark or a com.apple.fileprovider. attribute on it or a folder above it, or an evicted file, is SyncPlaceholder. Linux asks statfs's magic number, which alone decides, and the mount table's fstype for Type, fuse.sshfs and other FUSE subtypes among them: ext2 to ext4, XFS, btrfs, f2fs, tmpfs, ZFS, FAT, exFAT, bcachefs and overlay are Local, NFS, SMB, CIFS, AFS, Ceph, Coda and 9p Network, and FUSE FUSE; Linux keeps no sync placeholders. Windows asks the volume's NT device, drive type and file system name: a remote drive or a \Device\Mup path is Network, a file system whose name holds FUSE is FUSE, and only NTFS, ReFS, FAT and exFAT on a numbered local disk volume, fixed or removable, under no sync root, Local; a path is SyncPlaceholder where it or a folder above it, up to the volume's root, each opened with no link followed, carries an IO_REPARSE_TAG_CLOUD reparse tag or one of its variants, or where a sync root registered under the SyncRootManager key holds it, matched by whole names with no case. The Cloud Files API's CfGetSyncRootInfoByPath is not asked, as go-files reaches no DLL's call by name. The Windows Cloud Files detection is not yet proved on a real OneDrive sync root, as no runner it is tested on has one: the Cloud Files filter can disguise placeholders from a process that has not opted in to seeing them, so the tag half may never see a tag, and the registry half matches a root only as the registry spells it. Until the run of go-files#19 (https://github.com/birdtable/go-files/issues/19) proves it, a caller that must not run on a sync folder should take SyncPlaceholder on Windows as best effort. What reads as Unknown: on macOS a path below Library/CloudStorage or Library/Mobile Documents with no mark, a path whose folders' attributes cannot be listed, a volume without the local-mount flag that is neither FUSE nor network, and any other type, devfs among them; on Linux any other magic number, procfs, sysfs and the kernel's NTFS among them; on Windows a path whose reparse tags or the registered sync roots cannot be read, another device, a WinFsp or Dokan drive that names itself NTFS among them, another drive type and another file system. Any other system, FreeBSD among them, is an *fs.PathError matching errors.ErrUnsupported. Non-test Go lines, as task size counts them: appdirs 949 to 1,528, 579 more, the 97 of volume.go, the rules of each system in volume_rule.go, a file for macOS, Linux, Windows and the other systems, and 1 of the package doc; 13,874 to 14,453 in all, of the ceiling's 14,453.

### Changed

- The pre-push tier is judged by the CPU time its tree uses, user plus system, through hatch budget --cpu: on macOS arm64 it fails over 322 s, twice the least of three runs from a fresh clone, each after go clean -testcache (160.6, 164.7 and 165.4 s). CPU time is the same on a quiet machine and a loaded one, so the load no longer excuses a push. Its wall budget of 10 s is only a hang guard: a push past it and within its CPU limit passes, saying so, and one still running at 100 s is killed and fails as hung. The commit and release tiers keep their wall-time budgets.

### API

Against v0.17.0, no exported name is removed, moved or changed: the API check's breaks from v0.17.0 to v0.18.0 have no lines, alike on darwin, linux and windows.

Added, each `experimental`: in `appdirs`, `Volume`, `VolumeInfo` with its method `Local`, `VolumeKind` with its method `String`, and the constants `Unknown`, `Local`, `Network`, `FUSE` and `SyncPlaceholder`.

### Size

Non-test Go lines outside internal/ but internal/winapi, v0.17.0 to v0.18.0, as `task size` counts them: appdirs 949 to 1,528, 579 more, in volume.go, volume_rule.go, a file for each of macOS, Linux, Windows and the other systems, and the package doc; every other package unchanged. 13,874 to 14,453 in all, 579 more: 14,453 of the ceiling's 14,453. treesum: 548, unchanged, of its ceiling's 548.

go-files v0.18.0 requires golang.org/x/sys v0.50.0, golang.org/x/text v0.44.0 and, for its tests alone, github.com/birdtable/go-testkit v0.3.1, as v0.17.0 did. go.mod is unchanged since v0.17.0.

## v0.17.0 (2026-11-25)

v0.17.0 adds one function to `confine`, `Folds`, which answers how a folder reads names, whether it takes two that differ only in case or in Unicode composition for one, by asking the system and writing nothing, with the type `Folding` and the error `ErrNoFolding`. No exported name is removed, moved or changed.

The full release checks, `task release`, ran on macOS at the tagged commit. On Linux, macOS and Windows, the test workflow ran at the tagged commit in CI: go vet, the tests with the race detector, lint, the API check and the vulnerability check. No full release checks ran on Linux or Windows for this release.

### Added

- confine.Folds(root) answers how a folder reads names, writing nothing, as a confine.Folding: FoldNone, FoldCase, FoldComposition, FoldBoth or FoldUnknown. macOS asks the volume's type and case sensitivity, Windows the volume's file system and flags and the folder's own case-sensitivity flag, Linux the file system's type and the folder's casefold attribute. Where the system cannot say without a write, a case-sensitive APFS volume, a network share, FUSE, XFS and ZFS among them, the answer is FoldUnknown with an error matching confine.ErrNoFolding and errors.ErrUnsupported. FoldUnknown holds both fold bits, so FoldsCase and FoldsComposition answer true for it. Collide's doc points to Folds for a caller that only asks or must not be swayed to true by other calls' probes, and to Portable's keys as the filter before either. Non-test Go lines, as task size counts them: confine 4,156 to 4,488, 332 more, the 328 of folds.go, the rules of each system in folds_rule.go and a file for macOS, Linux, Windows and the other systems, and 4 of Collide's doc; 13,542 to 13,874 in all, of the ceiling's 13,874.

### API

Against v0.16.2, no exported name is removed, moved or changed: the API check's breaks from v0.16.2 to v0.17.0 have no lines, alike on darwin, linux and windows.

Added, each `experimental`: in `confine`, `Folds`, `Folding` with its methods `FoldsCase`, `FoldsComposition` and `String`, the constants `FoldNone`, `FoldCase`, `FoldComposition`, `FoldBoth` and `FoldUnknown`, and `ErrNoFolding`.

### Size

Non-test Go lines outside internal/ but internal/winapi, v0.16.2 to v0.17.0, as `task size` counts them: confine 4,156 to 4,488, 332 more, in folds.go, folds_rule.go, a file for each of macOS, Linux, Windows and the other systems, and Collide's doc; every other package unchanged. 13,542 to 13,874 in all, 332 more: 13,874 of the ceiling's 13,874. treesum: 548, unchanged, of its ceiling's 548.

go-files v0.17.0 requires golang.org/x/sys v0.50.0, golang.org/x/text v0.44.0 and, for its tests alone, github.com/birdtable/go-testkit v0.3.1, as v0.16.2 did. go.mod is unchanged since v0.16.2.

## v0.16.2 (2026-11-25)

v0.16.2 fixes one security defect, GHSA-5j6g-3rm2-q3r8: `confine.Collide` could answer that two names are apart when a file made or removed at one of them, another call's probe among them, fell between its looks at the two. The rest of the release is the repository's own CI and tooling. No exported name is added, removed, moved or changed.

On Linux, macOS and Windows, the test workflow ran at the tagged commit in CI: go vet, the tests with the race detector, lint, the API check and the vulnerability check. No full release checks ran for this release. The pre-push tier ran on macOS on the tagged tree.

### Added

- A test workflow, .github/workflows/test.yml, runs go vet and go test with the race detector on GitHub's hosted Linux, macOS and Windows runners for every pull request and every push to main, prints the passed, failed and skipped counts, and is red on any failure or skip.

### Changed

- The repository's own tools resolve from the public module proxy: each tools/*.mod requires go-testkit v0.3.1, and tools/hatch.mod requires hatch v0.7.0 and go-proc v0.16.0. tools/hatch.mod names a module of its own, github.com/birdtable/go-files/tools, since hatch imports go-files: hatch is built against the published go-files it requires, never against the tree it checks. The test workflow runs every test again, with none skipped, and its tidy check covers each tools/*.mod as well as go.mod.
- The test workflow also runs lint, the API check and the vulnerability check, each through the repository's own task tooling as the local tiers do, forced so that no stamp under .task/ can skip one, with no wall-clock budget, and each writes its pass or fail line to the job summary. A test fails when git tracks any file under .task/. Pull requests run on Linux and Windows; pushes to main, v* tags and runs by hand add macOS. A newer run for the same ref cancels the older one.

### Removed

- The release workflow, .github/workflows/release.yml, which ran only by hand. Each release is proved by the test workflow at its commit on Linux, macOS and Windows, and the release notes say what else ran.

### Security

- GitHub Actions on this repository accepts only actions pinned by a full commit sha, and a workflow's token can read the repository and nothing more. The test workflow holds no secret and keeps no credential after checkout.
- GHSA-5j6g-3rm2-q3r8: confine.Collide no longer answers that two names are apart when a file made or removed at one of them, another call's probe among them, falls between its looks at the two. Before it answers apart it looks at both names again, and answers apart only where neither changed; where they did, it looks again from the start, and where the folder changes under each of three tries it answers that the names are one. Before, on a volume that folds the two names, such as APFS or NTFS, two Collide calls on one folder at once could be told that names the volume takes for one are two. The answer that the names are one is the safe one, and it can now be wrong the other way: raced by other calls, Collide can answer that two names are one on a volume that folds neither, 2 to 9 calls in 100 with eight at once on one folder. One race is left: a file moved away, back and away again at the first name within one call, each move between two of its looks, can still be answered apart.

### API

Against v0.16.1, no exported name is removed, moved or changed: the API check's breaks from v0.16.1 to v0.16.2 have no lines, alike on darwin, linux and windows. No name is added.

### Size

Non-test Go lines outside internal/ but internal/winapi, v0.16.1 to v0.16.2, as `task size` counts them: confine 4,122 to 4,156, all of it in collide.go; every other package unchanged. 13,508 to 13,542 in all, 34 more: 13,542 of the ceiling's 13,542. treesum: 548, unchanged, of its ceiling's 548.

go-files v0.16.2 requires golang.org/x/sys v0.50.0, golang.org/x/text v0.44.0 and, for its tests alone, github.com/birdtable/go-testkit v0.3.1, as v0.16.1 did. go.mod is unchanged since v0.16.1.

## v0.16.1 (2026-11-25)

v0.16.1 changes no code a program runs. Its comments, messages and description files are rewritten for readers outside the project, and two description files move to a new folder. No exported name is added, removed, moved or changed, and nothing behaves differently.

Each release is proved by its release checks, `task release`, run at the release's commit on three systems: macOS, Linux and Windows.

### Changed

* The Go doc comments, the other Go comments, the comments in the configuration files and the messages that the checks, lint rules and tests print now say what each rule does, in words that need no knowledge of the project's own tracker. They no longer cite internal issues, decisions or runs. No behaviour changes.
* The two files that describe the library move from their old folder to `.library/`: `.library/library.yaml`, which names each package, its one job and the calls a program no longer makes by hand once it imports it, and `.library/uses.yaml`, which names the libraries and tools this repository uses, the lint rule that keeps out a second way to do each job, and the libraries it chose not to use, with the reason. A tool that reads them finds them there. Their keys, the libraries and tools they name, the calls each lint rule bans and the patterns are unchanged. Their free text now cites this repository's own files, lint rules and commits.
* go-files' tests require go-testkit v0.3.1, up from v0.2.0. Nothing a program imports from go-files changes.

### API

Against v0.16.0, no exported name is removed, moved or changed: the API check's breaks from v0.16.0 to v0.16.1 have no lines, alike on darwin, linux and windows. No name is added.

### Size

Non-test Go lines outside internal/ but internal/winapi, v0.16.0 to v0.16.1, as `task size` counts them: appdirs 949, unchanged; confine 4,123 to 4,122; filestest 236, unchanged; lock 755 to 753; nowait 44, unchanged; splice 109, unchanged; trust, trust/trusttest among it, 4,455 to 4,450; write 2,191 to 2,186; and internal/winapi 661 to 659. 13,523 to 13,508 in all, 15 fewer, all of it in comments: 13,508 of the ceiling's 13,508. treesum: 550 to 548, of its ceiling's 548.

go-files v0.16.1 requires golang.org/x/sys v0.50.0, golang.org/x/text v0.44.0 and, for its tests alone, github.com/birdtable/go-testkit v0.3.1. go.mod's one change since v0.16.0:

```diff
-	github.com/birdtable/go-testkit v0.2.0
+	github.com/birdtable/go-testkit v0.3.1
```

go-testkit is required by go-files' tests alone, so a program that imports go-files builds none of it. The other two requirements are unchanged.

## v0.16.0 (2026-11-25)

v0.16.0 adds a package, `splice`, and new names to `confine`, `filestest`, `lock`, `treesum`, `trust` and `trust/trusttest`, and lets `write` keep a copy under a second root and put back a file that was removed. No exported name is removed or moved. Three change, and a program that uses one must change with them:

- **`write.KeepIn` and `write.PutBackIn` take two roots.** Each now takes the root of the file and the root of the copy, one for each name: `KeepIn(from, file, to, kept, opts...)` and `PutBackIn(from, kept, to, file, holds, opts...)`. A call written for v0.15.0 gives the one root twice: `KeepIn(root, file, kept)` becomes `KeepIn(root, file, root, kept)`, and `PutBackIn(root, kept, file, holds)` becomes `PutBackIn(root, kept, root, file, holds)`. The refusals, the mode, the group rule and `holds` are as they were.
- **`treesum.Caps` is no longer comparable.** It holds the func field `Skip`, so two `Caps` values no longer compare with `==`, and a `Caps` cannot be a map key. A `Caps` written with field names compiles as before. Compare the fields a program sets instead.

One change touches no code: the README is rewritten, and `SECURITY.md` and `CONTRIBUTING.md` are new.

Each release is proved by its release checks, `task release`, run at the release's commit on three systems: macOS, Linux and Windows.

### Changed

**write**
* `KeepIn` and `PutBackIn` take two roots, one for each name: `KeepIn(from *os.Root, file string, to *os.Root, kept string, opts ...Option) error` and `PutBackIn(from *os.Root, kept string, to *os.Root, file string, holds [sha256.Size]byte, opts ...Option) error`. So a program keeps its copies in a folder of its own anywhere, on another file system included. Each name is opened through its own root, and no rename or link crosses the two: the copy is made in `to` by an exclusive create, and `PutBackIn` replaces `file` by a temp file beside it in `to`. An error names each name beneath its root.

**treesum**
* `Caps` holds the func field `Skip`, below, so two `Caps` no longer compare with `==`.

**trust**
* `Fix.Commands` gives the step that makes an account the owner for any account in `Account`'s form, not only root and Administrators. So a caller's own `Fix` with `Account()`'s answer as its `Owner` gives a name to this account: `chown -h <N> <path>` for `uid:N`, `root` still for `uid:0`, and `icacls <path> /setowner *<SID> /L` for a SID, each with `Admin` true. An owner not in that form, such as a name, a gid or a uid with a leading zero, gives no steps and `ok` false, as any owner but those two did before.

### Added

**splice**, a new package
* `Splice(text, begin, end, block)` replaces what stands between two marker lines with a block, and keeps both markers and every other byte. It is a pure function over bytes that reads no file and imports the standard library alone, so a wasip1 build with the standard library only can use it. A marker line is found when indented by spaces and tabs or ending in `\r\n`. Each block line that holds more than white space takes the begin line's indent, and a `\n` is added after a last block line that has none. A missing marker is `ErrNoMarker`, a doubled one `ErrMarkerTwice`, and an end not after the begin `ErrMarkerOrder`, a begin equal to the end included.

**confine**
* `CopyTree(dst, src *os.Root) error` copies everything in `src` into `dst`, root to root, walking each name by handle and following no link. Each folder is made as `Mkdir` makes one, 0o700 and the owner-only list on Windows, and each file 0o600, or 0o700 where `IsRunnableFile` answers the source runnable, whatever the umask. Each link that leads inside `src`, as `Below` says, is made again at the same name in `dst`: to a folder as `LinkFolder` makes one, a junction on Windows, and to a file as `LinkFile` does, `ErrNoFileLink` where this account may not make one. Its target is relative to its own folder on Linux, macOS and FreeBSD, so the copy can be moved and read through an `os.Root`, and absolute under `dst`'s `Name` on Windows, as a junction needs. A link that leads outside `src` is refused with `ErrOutside`, a link to nothing with `fs.ErrNotExist`, a name already in `dst` with `fs.ErrExist`, and a pipe, a device or a socket with `fs.ErrInvalid`. `src` and `dst` the same folder, and a root whose `Name` is not absolute, clean and the folder it holds, are refused before anything is made. `dst` met in the walk of `src` is refused when it is met, with `ErrOutside`. What was made before a refusal stays, for the caller's `RemoveAll`.
* `ReadCapped(root *os.Root, name string, limit int64) ([]byte, error)` reads one file below a held root by its local name. It opens the file as `Open` does, read-only, with no link followed at any component, and reads from that handle alone. A link or junction at any component, the last included, and a name that is not clean and local are refused with an `*fs.PathError` matching `ErrOutside`. Anything that opened as other than a regular file (a folder, a pipe, a device), by the handle's `Stat`, is refused with the new `ErrNotRegular`; a socket is refused by the open itself, with the system's own error. A file longer than `limit` bytes is refused with the new `ErrOverCap`, told by reading `limit`+1 bytes and never more. A file of exactly `limit` bytes is answered whole, a `limit` below zero is `fs.ErrInvalid` with nothing opened, and a pipe is refused at once, never waited on. A file with a second name, by a hard link, is read as any other.
* `ErrChanged` and `ErrNotEmpty`, each matching `ErrOutside`, so a caller can tell two races with a second run apart from a hostile change. `Resolve` and `Below` refuse a path that changed while it was walked with `ErrChanged`: the system's own look found another file than the walk did, or something where the walk found nothing, a missing name another process made meanwhile among them, or a folder of the walk was another file when its file index was read. `Mkdir`, `MkdirPrivate`, `MkdirShared` and `MkdirFresh` refuse a folder the call made that holds something when it is checked with `ErrNotEmpty`, whether another process put a name in it or swapped in a folder that holds one. Another account's folder, a link and what is not a folder stay `ErrOutside` alone. Nothing refused before is answered now.
* `RemoveAllOwned(root, name)` is `RemoveAll` for a tree the caller made and owns that a program may have left read-only, as the go command leaves a module cache. On Unix each folder this account owns that lacks its owner's write bit is given that bit (u+w) and no other, by `fchmod` on the folder opened and held to be the one looked at, before it is emptied. A folder another account owns, or one without its owner's search bit, keeps its mode; a link is never followed or changed; a folder swapped in is refused with `ErrOutside` before its mode is touched; and the folder the name is in is not changed. On Windows it is `RemoveAll`, which already removes read-only folders and files.

**filestest**
* `CopyTree(t, dst, src string)` is `confine.CopyTree` for a test's fixture tree, and fails the test naming both paths.

**lock**
* `TakeIn(root *os.Root, name, note string)`, `WaitIn(ctx, root, name, note)` and `HolderIn(root, note)` take, wait for and read a lock and its note below a held root, by local name, so no step goes by a path that could be swapped between steps. The lock file is opened or made, checked, locked and its name read again, and the note written, read and removed, each through the root. A name or note that leaves the root is refused as `os.Root` refuses it, and one whose last element is `..` before any open; nothing is made outside. The `Lock` keeps a root of its own, which `Release` closes, so the caller may close its root once `TakeIn` returns. An older `TakeIn` and `WaitIn` were removed at v0.1.0 with their options; these are new, take a note and take no option.

**treesum**
* `Caps.Skip` leaves names out of the walk of `Sum` and `Check`. It is asked of each name a listing holds, by its slash path from the top, before anything about the name is looked at. A name it skips, and everything below it, is never opened, counted against the caps or refused, and is not listed. The listing's format and the `t2:` digest are unchanged, and a walk with no `Skip` gives the digest it gave before. A link whose target reaches a skipped name, or a missing name in a folder where a name was skipped, is refused with `confine.ErrOutside`.

**trust**
* `MkdirPrivate(dir string, who Who) (*os.Root, error)` makes a folder only this account reaches and answers it as a root. The nearest part of `dir` that is there, and every folder above the first missing name, are judged by `Path` for `who` before anything is made. Each missing folder is made by `confine.MkdirPrivate`, 0700 on Unix and the owner-only list on Windows, below the folder the walk reached. `dir` is then judged as a `PrivateFolder` and held to be the folder answered. A folder that was there and is not private is refused with `Path`'s refusal and its `Fix`, and never changed. Another make of `dir` at the same time sends the call back to the judge, as does a folder filled meanwhile (`confine.ErrNotEmpty`) or one not there at a second look; after 8 such tries it is refused with `ErrReplaced`. `RootOnly` is refused, matching `ErrNotOwners`. `trust` now imports `confine`.

**trust/trusttest**
* `GiveGroup(t testing.TB, path string, gid uint32)` gives `path` to the group `gid` until the test ends, following a link, so a test can plant a name of this account's given to a group, as the one `trust.AdminGroup` answers, with no `os.Chown` of its own. At the test's end it puts back the group, and the set-user-ID and set-group-ID bits a change of group clears. It skips the test, saying why, where this account may not give the name to `gid` (`EPERM`), where a Linux user namespace maps no such gid (`EINVAL`), where the disk keeps another group, and on Windows, which keeps no group id on a name. A `gid` of `math.MaxUint32`, which `chown` reads as no change, fails the test, as does any other failure.

**write**
* `Gone()` lets `PutBackIn` put a copy back where the file was removed. It places the copy exclusively, by a temp file linked to the name and never by a replace, with the copy's mode and `Keep`'s group rule. Anything at the name, a link or a folder included, is `ErrChanged`, and nothing there or through it is written. `holds` must be zero with `Gone()`, and a sum is refused with an error matching `fs.ErrInvalid`. Without `Gone()` nothing widens: a missing file is `ErrChanged` whatever `holds` is. `Gone()` given to `PutBack`, the path form, or to `File`, `FileFrom`, `FileIn`, `Keep`, `KeepIn` or `Swap`, is refused with an error matching `fs.ErrInvalid`.

### Known limitations

* On iOS, a build with `osusergo` has `AccountHome` read `/etc/passwd`, where it should be refused as macOS is, and no test yet holds which of `AccountHome`'s files each system builds with cgo on, off and with `osusergo`.
* No test yet swaps the folder of `KeepIn`'s copy for a link while a copy that could not be finished is removed. `KeepIn` removes it by its last name under the folder of `to` it first opened, which is read from the code and not yet shown by a test.
* On Windows, with `lock.Take`, `lock.Wait`, `lock.TakeIn` or `lock.WaitIn` held on a log, `write.Append` to an empty or one-byte log may fail at once with the system's lock error, neither let in nor kept waiting, and its line is not written. This is read from the system's locking rule and not yet seen on a run. Rely on `Append`'s own hold for a log, not on `lock`'s.
* On Windows, `MkdirShared` checks that every entry of a folder's list was inherited, not from which folder it was inherited.
* The release workflow in `.github/workflows` runs only by hand and is not what proves a release: each release is proved by its release checks on macOS, Linux and Windows machines of the maintainers' own, and no hosted runner proves it.
* In a Linux user namespace that maps root over the host's files, as `unshare --map-root-user` makes, `/` is the overflow account's, and `filestest.Private` fails the test. In one that maps this account alone, as `--map-current-user` makes, it passes.

### API

Against v0.15.0, no exported name is removed or moved, so no name has a new place to point to. Three change, the three lines of the API check's breaks from v0.15.0 to v0.16.0, alike on darwin, linux and windows:

* **treesum**: `Caps` was comparable and is not.
* **write**: `KeepIn` changed from `func(*os.Root, string, string, ...Option) error` to `func(*os.Root, string, *os.Root, string, ...Option) error`.
* **write**: `PutBackIn` changed from `func(*os.Root, string, string, [32]byte, ...Option) error` to `func(*os.Root, string, *os.Root, string, [32]byte, ...Option) error`.

Added, each `experimental`: the package `splice`, with `Splice`, `ErrNoMarker`, `ErrMarkerTwice` and `ErrMarkerOrder`; in `confine`, `CopyTree`, `ErrChanged`, `ErrNotEmpty`, `ErrNotRegular`, `ErrOverCap`, `ReadCapped` and `RemoveAllOwned`; in `filestest`, `CopyTree`; in `lock`, `HolderIn`, `TakeIn` and `WaitIn`; in `treesum`, the field `Caps.Skip`; in `trust`, `MkdirPrivate`; in `trust/trusttest`, `GiveGroup`; and in `write`, `Gone`.

### Size

Non-test Go lines outside internal/ but internal/winapi, v0.15.0 to v0.16.0, as `task size` counts them: appdirs 949, unchanged; confine 3,679 to 4,123; filestest 201 to 236; lock 567 to 755; nowait 44, unchanged; splice, new, 109; trust, trust/trusttest among it, 4,094 to 4,455; write 2,145 to 2,191; and internal/winapi 661, unchanged. 12,340 to 13,523 in all, 1,183 more: 13,523 of the ceiling's 13,523, with no headroom. treesum: 479 to 550, of its ceiling's 550.

go-files v0.16.0 requires golang.org/x/sys v0.50.0, golang.org/x/text v0.44.0 and, for its tests alone, github.com/birdtable/go-testkit v0.2.0, all unchanged since v0.15.0: go.mod's requirements have no diff. Its youngest requirement, github.com/birdtable/go-testkit v0.2.0, tagged 2026-11-21, is 4 days old on 2026-11-25; golang.org/x/text v0.44.0, published 2026-11-09, is 16 days old.

## v0.15.0 (2026-11-22)

v0.15.0 adds one name to `appdirs` with its refusal, `AccountHome` and `ErrNoAccountHome`, which answer the running account's home from the system's account records and never from the environment. No exported name is removed, moved or changed. Before upgrading, check two things a program may meet:

- **A build without cgo, or with `osusergo`, and Android in every build.** On Linux and the BSDs built so, and on Android with cgo or without, `AccountHome` reads `/etc/passwd` itself, so an account kept only in LDAP or sssd, or a process run under a uid `/etc/passwd` does not hold, as a container may be, is `ErrNoAccountHome`, never the `HOME` the environment gives. On macOS a build without cgo still asks the system; one with `osusergo` is `ErrNoAccountHome`, since macOS keeps its accounts outside `/etc/passwd`. A program built so that runs under such accounts needs another way to its home.
- **A path over 260 bytes on Windows.** The promise that every call takes one now says what it was proved under: Windows with `LongPathsEnabled` set to 1. It is not measured with the setting unset or 0. No call changes.

One doc says more and changes no code: `confine`'s now says that `os.Root` follows a link that stays inside its folder only where the link's target is relative, and refuses a junction.

### Added

**appdirs**
* `AccountHome()` answers the home folder of the account that runs the process, from the system's account records, and reads no variable, `HOME` and `USERPROFILE` among them, so a home a caller or a test sets in the environment never answers. On Windows it is the profile folder of the process's token. On Unix it is the home of the real user ID, `os.Getuid`: from the C library's account database in a build with cgo but on Android, and on macOS in any build without `osusergo`; and in a build without cgo or with `osusergo`, and on Android in every build, from that user ID's line in `/etc/passwd`, read by `appdirs` itself, where macOS is refused. The answer is absolute, clean and never empty. Nothing is made, and the folder is not opened: judge it with `trust` before trusting what is in it.
* `ErrNoAccountHome` is `AccountHome`'s refusal, matched by `errors.Is`, with the cause: a user ID the records do not hold, an account kept only in LDAP or sssd where `/etc/passwd` is read, a line of `/etc/passwd` that is not seven fields, a home the records give empty, relative or not clean, a macOS build with `osusergo`, and every system but Unix and Windows, where the home is read from the environment.

### Changed

**The promise index**
* The line for a path of more than 260 bytes on Windows now says it was proved with `LongPathsEnabled` set to 1, and not measured with it unset or 0. A program whose users may run Windows without that setting has no proof here for a path over 260 bytes. The line as it stood at v0.14.0 is kept in `api/breaks/v0.14.0.contract.txt`, and the README says the same. No call changes.

### Fixed

**confine** and **treesum**
* Their docs said `os.Root` follows a link that stays inside its folder. It follows one only where the link's target is relative: a link with an absolute target is refused wherever it leads, as v0.14.0's notes say of `write.KeepIn`. A comment also said `os.Root` follows a Windows junction: a run on Windows shows it refuses one, even to a folder beneath the root. The package doc, the README's opening and its table of what was read, and the root README's row now say so. `confine`'s own promise is unchanged: it follows no link.

### Known limitations

* On iOS, a build with `osusergo` has `AccountHome` read `/etc/passwd`, where it should be refused as macOS is, and no test yet holds which of `AccountHome`'s files each system builds with cgo on, off and with `osusergo`.
* No test yet swaps the folder of `KeepIn`'s copy for a link while a copy that could not be finished is removed. `KeepIn` removes it by its last name under the folder it first opened, which is read from the code and not yet shown by a test.
* On Windows, with `lock.Take` or `lock.Wait` held on a log, `write.Append` to an empty or one-byte log may fail at once with the system's lock error, neither let in nor kept waiting, and its line is not written. This is read from the system's locking rule and not yet seen on a run. Rely on `Append`'s own hold for a log, not on `lock`'s.
* On Windows, `MkdirShared` checks that every entry of a folder's list was inherited, not from which folder it was inherited.
* The release workflow in `.github/workflows` is not run: each release is proved by its release checks on macOS, Linux and Windows machines of the maintainers' own, and no hosted runner proves it.
* In a Linux user namespace that maps root over the host's files, as `unshare --map-root-user` makes, `/` is the overflow account's, and `filestest.Private` fails the test. In one that maps this account alone, as `--map-current-user` makes, it passes.

### API

Against v0.14.0, no exported name is removed, moved or changed: the API check finds no break from v0.14.0 to v0.15.0 on darwin, linux or windows. Added, each `experimental`: in `appdirs`, `AccountHome` and `ErrNoAccountHome`.

### Size

Non-test Go lines outside internal/ but internal/winapi, v0.14.0 to v0.15.0, as `task size` counts them: appdirs 799 to 949; confine 3,679, unchanged; filestest 201, unchanged; lock 567, unchanged; nowait 44, unchanged; trust, trust/trusttest among it, 4,094, unchanged; write 2,145, unchanged; and internal/winapi 661, unchanged. 12,190 to 12,340 in all, 150 more: 12,340 of the ceiling's 12,340, with no headroom. treesum: 479, unchanged.

go-files v0.15.0 requires golang.org/x/sys v0.50.0, golang.org/x/text v0.44.0 and, for its tests alone, github.com/birdtable/go-testkit v0.2.0, all unchanged since v0.14.0. Its youngest requirement, github.com/birdtable/go-testkit v0.2.0, tagged 2026-11-21, is 1 day old on 2026-11-22; golang.org/x/text v0.44.0, published 2026-11-09, is 13 days old.

## v0.14.0 (2026-11-22)

v0.14.0 adds two names to `write`, `KeepIn` and `PutBackIn`, which keep a copy of a file and put it back by names beneath an `os.Root`, and narrows the mode a kept or restored copy gets: its group bits now stay only where the new file is in the group of the file it copies, or in the group `Owner` named. No exported name is removed, moved or changed. Before upgrading, check each program that calls `Keep` or `PutBack` on a file with group bits, where the copy lands in another group than the file's: the copy's group bits are cleared. Read Security. Two docs say more and change no code: `write.Append`'s and `lock.Take`'s now say that a lock taken with `lock` on a log keeps `Append` out on Unix and not on Windows, and `trust/trusttest`'s says how a test finds out whether a plant for `RootOnly` can be made on Windows.

### Added

**write**
* `KeepIn(root, file, kept, opts...)` keeps a copy of the file `file` at `kept`, as `Keep` does, where both are names beneath an `os.Root` and may lie in different folders. Every step goes through the root, as `FileIn`'s do: a name, or a folder on the way turned into a link, that leads out of the root is refused, and nothing outside the root is read or made. A folder on the way linked by a relative path to another beneath the root is followed, and a link with an absolute target is refused wherever it leads: the root is the bound, not a folder beneath it. The refusals are `Keep`'s: a taken `kept`, a link among them, is `fs.ErrExist`; a last name of `kept` that is not plain is refused as `Keep` refuses its `name`; a link or a pipe at `file` is refused; a missing `file` is `fs.ErrNotExist`. `kept`'s folder must be there, and none is made. A copy that could not be finished is removed from the folder it was made in. The copy takes `file`'s mode, with the group bits as Security says.
* `PutBackIn(root, kept, file, holds, opts...)` puts the copy at `kept` back over `file`, as `PutBack` does, by the same two names beneath the root and through it at every step, so nothing outside the root is read, hashed or written over. A `file` that holds anything but `holds`, or is missing, is `ErrChanged`, and is left as it is. A link or a pipe at `kept` or at `file` is refused, and not as `ErrChanged`.

### Security

**write**
* `Keep`, `KeepIn`, `PutBack` and `PutBackIn` keep a copy's group bits only where the new file is in its source's group, or in the group `Owner` gave it. What decides is the group the new file lands in: its folder's on macOS and in a setgid folder on Linux, and elsewhere on Linux its writer's. So a file 0640 whose copy landed in another group was 0640 in that group, and its members could read what only the source's group could. It is 0600 now, and 0644 is 0604: others' bits are not touched. Nothing is chowned to make the groups match: `Owner` alone gives a group. Windows is unchanged. This narrows the mode `Keep` and `PutBack` promised.

### Known limitations

* No test yet swaps the folder of `KeepIn`'s copy for a link while a copy that could not be finished is removed. `KeepIn` removes it by its last name under the folder it first opened, which is read from the code and not yet shown by a test.
* On Windows, with `lock.Take` or `lock.Wait` held on a log, `write.Append` to an empty or one-byte log may fail at once with the system's lock error, neither let in nor kept waiting, and its line is not written. This is read from the system's locking rule and not yet seen on a run. Rely on `Append`'s own hold for a log, not on `lock`'s.
* The library's own release checks ran in 54 s of their 60 s on Windows for this release's candidate, from 51 s at v0.13.0 and 36 s at v0.12.0. Where they grew is not yet measured.
* On Windows, `MkdirShared` checks that every entry of a folder's list was inherited, not from which folder it was inherited.
* The release workflow's run at a given commit, by hand, is not yet proved on GitHub's hosted runners.
* In a Linux user namespace that maps root over the host's files, as `unshare --map-root-user` makes, `/` is the overflow account's, and `filestest.Private` fails the test. In one that maps this account alone, as `--map-current-user` makes, it passes.

### API

Against v0.13.0, no exported name is removed, moved or changed: the API check finds no break from v0.13.0 to v0.14.0 on darwin, linux or windows. Added, `experimental`: in `write`, `KeepIn` and `PutBackIn`.

### Size

Non-test Go lines outside internal/ but internal/winapi, v0.13.0 to v0.14.0, as `task size` counts them: appdirs 799, unchanged; confine 3,679, unchanged; filestest 201, unchanged; lock 561 to 567; nowait 44, unchanged; trust, trust/trusttest among it, 4,091 to 4,094; write 2,029 to 2,145; and internal/winapi 661, unchanged. 12,065 to 12,190 in all, 125 more: 12,190 of the ceiling's 12,190, with no headroom. treesum: 479, unchanged.

go-files v0.14.0 requires golang.org/x/sys v0.50.0, golang.org/x/text v0.44.0 and, for its tests alone, github.com/birdtable/go-testkit v0.2.0, all unchanged since v0.13.0. Its youngest requirement, github.com/birdtable/go-testkit v0.2.0, tagged 2026-11-21, is 1 day old on 2026-11-22; golang.org/x/text v0.44.0, published 2026-11-09, is 13 days old.

## v0.13.0 (2026-11-22)

v0.13.0 adds two names to `confine`, for how many names a file has, by hard links. `treesum`, and `write` on Windows, now count a file's names by them, as `confine`'s own `Open` does, so the count is read one way everywhere. No exported name is removed, moved or changed, and no package a program runs with changes what it does, so there is nothing to check before upgrading.

### Added

**confine**
* `Names(root, name)` answers how many names the file `name` in `root` has, from the file itself, opened as `Open` opens it, read-only and never waiting, and from nothing outside the root. A link or junction at any component, the last included, is refused with an error matching `ErrOutside`, whether it leads outside the root or stays inside it; a name swapped for a link during the call is never read outside; a pipe is refused at once. An error is no count, and a guard should refuse on it: a file this account may write but not read is refused by the read-only open, with its error.
* `NamesFile(f)` answers the same for a file the caller opened itself, from that handle: `st_nlink` on Unix and `NumberOfLinks` on Windows, as the volume reports them. Only a regular file is counted: a folder, a pipe, a device or a socket is refused with an error matching `fs.ErrInvalid`. A count the system won't give is its error, never 1. A count past 2^31-1 is answered as 2^31-1 on every build, 32-bit ones included. 0 is an answer, for a file whose every name was removed while it was held, so a guard compares with 1. A volume that cannot hold a second name, as FAT, reports 1, and a network volume reports what its server sends. A name made after the count is not seen, so a guard keeps the handle it counted through to the act it guards.

### Known limitations

* On Windows, `MkdirShared` checks that every entry of a folder's list was inherited, not from which folder it was inherited.
* The release workflow's run at a given commit, by hand, is not yet proved on GitHub's hosted runners.
* In a Linux user namespace that maps root over the host's files, as `unshare --map-root-user` makes, `/` is the overflow account's, and `filestest.Private` fails the test. In one that maps this account alone, as `--map-current-user` makes, it passes.

### API

Against v0.12.0, no exported name is removed, moved or changed: the API check finds no break from v0.12.0 to v0.13.0 on darwin, linux or windows. Added, `experimental`: in `confine`, `Names` and `NamesFile`.

### Size

Non-test Go lines outside internal/ but internal/winapi, v0.12.0 to v0.13.0, as `task size` counts them: appdirs 799, unchanged; confine 3,601 to 3,679; filestest 201, unchanged; lock 561, unchanged; nowait 44, unchanged; trust, trust/trusttest among it, 4,091, unchanged; write 2,031 to 2,029; and internal/winapi 661, unchanged. 11,989 to 12,065 in all, 76 more: 12,065 of the ceiling's 12,065, with no headroom. treesum: 504 to 479, 25 fewer, its count of names now `confine`'s. Across the module, 51 more.

go-files v0.13.0 requires golang.org/x/sys v0.50.0, golang.org/x/text v0.44.0 and, for its tests alone, github.com/birdtable/go-testkit v0.2.0, all unchanged since v0.12.0. Its youngest requirement, github.com/birdtable/go-testkit v0.2.0, tagged 2026-11-21, is 1 day old on 2026-11-22; golang.org/x/text v0.44.0, published 2026-11-09, is 13 days old.

## v0.12.0 (2026-11-22)

v0.12.0 adds two names to `confine`, for whether this system would run a file, and corrects a line of `write`'s doc. No exported name is removed, moved or changed, and no package a program runs with changes what it does, so there is nothing to check before upgrading.

### Added

**confine**
* `IsRunnable(root, name)` answers whether this system would run the file `name` in `root` for this account, from the file itself, opened as `Open` opens it, read-only and never waiting, and from nothing outside the root. A link or junction at any component, the last included, is refused with false and an error matching `ErrOutside`, whether it leads outside the root or stays inside it; a name swapped for a link during the call is never read outside; a pipe or a device is false at once. A file this account may run but not read (mode 0111, or a Windows list that grants execute alone) is refused by the read-only open, with false and that error.
* `IsRunnableFile(f)` answers the same for a file the caller opened itself, for whatever file it is: anything but a regular file is false, with no error. On Unix it is the exec bit that applies to this account by the open file's `fstat`: the owner's, else the group's for an account in the file's group, primary or supplementary, else other's, and for root, euid 0, any one. Access lists, mount options and capabilities are not read, so a 0755 file on a `noexec` mount is answered true, and so may a file be for a root without `CAP_DAC_OVERRIDE` over it. On Windows it is the extension of the long name the handle gives, whatever the caller spelled, against `PATHEXT`, without case, or Go's own `.com`, `.exe`, `.bat` and `.cmd` when it is unset; a name with no extension is false, and a file with several names is answered by the one the handle gives.

Both are stricter than `exec.LookPath` on an absolute path, which follows links and on Windows asks only for a dot in the name, so it answers `README.md` runnable.

### Fixed

**write**
* `Runnable`'s doc no longer says that `exec.LookPath` on an absolute path checks `PATHEXT` on Windows, as v0.5.0's changelog said ("which checks the exec bit on Unix and `PATHEXT` on Windows"): on Windows it asks only for a dot in the name, and it follows links. The doc now points at `confine.IsRunnable`. Only the doc changes; `Runnable` does what it did.

### Known limitations

* On Windows, `MkdirShared` checks that every entry of a folder's list was inherited, not from which folder it was inherited.
* The release workflow's run at a given commit, by hand, is not yet proved on GitHub's hosted runners.
* In a Linux user namespace that maps root over the host's files, as `unshare --map-root-user` makes, `/` is the overflow account's, and `filestest.Private` fails the test. In one that maps this account alone, as `--map-current-user` makes, it passes.

### API

Against v0.11.0, no exported name is removed, moved or changed: the API check finds no break from v0.11.0 to v0.12.0 on darwin, linux or windows. Added, `experimental`: in `confine`, `IsRunnable` and `IsRunnableFile`.

### Size

Non-test Go lines outside internal/ but internal/winapi, v0.11.0 to v0.12.0, as `task size` counts them: appdirs 799, unchanged; confine 3,436 to 3,601; filestest 201, unchanged; lock 561, unchanged; nowait 44, unchanged; trust, trust/trusttest among it, 4,091, unchanged; write 2,030 to 2,031; and internal/winapi 661, unchanged. 11,823 to 11,989 in all, 166 more: 11,989 of the ceiling's 11,989, with no headroom. treesum: 504, unchanged.

go-files v0.12.0 requires golang.org/x/sys v0.50.0, golang.org/x/text v0.44.0 and, for its tests alone, github.com/birdtable/go-testkit v0.2.0, all unchanged since v0.11.0. Its youngest requirement, github.com/birdtable/go-testkit v0.2.0, tagged 2026-11-21, is 1 day old on 2026-11-22; golang.org/x/text v0.44.0, published 2026-11-09, is 13 days old.

## v0.11.0 (2026-11-22)

v0.11.0 adds one name to `trust/trusttest`, for a program's tests. No exported name is removed, moved or changed, and no package a program runs with changes, so there is nothing to check before upgrading.

### Added

**trust/trusttest**
* `SystemFile(t)` answers a file the system owns that this account neither owns nor may open to write, and its owner as `trust.OwnerOf` reads it and `trust.ReadOwned` takes it, for a test of a refusal of another account's file. The file is found, never named: the first of a short list per system (`ntdll.dll` in the system folder `GetSystemDirectory` gives on Windows, `/private/etc/hosts` and `/usr/bin/true` on macOS, `/etc/passwd` and `/bin/sh` on Linux and another Unix) that is a regular file reached with no link on the way, owned by another account, and refused to an open to write. In a Linux user namespace that does not map root, root's file is answered owned by the overflow account, `uid:65534` by default, not `uid:0`. Where none passes, the test is skipped when this account is root or SYSTEM, saying nothing is proved, and failed otherwise; each names every candidate and why it did not pass.

### Known limitations

* On Windows, `MkdirShared` checks that every entry of a folder's list was inherited, not from which folder it was inherited.
* The release workflow's run at a given commit, by hand, is not yet proved on GitHub's hosted runners.
* In a Linux user namespace that maps root over the host's files, as `unshare --map-root-user` makes, `/` is the overflow account's, and `filestest.Private` fails the test. In one that maps this account alone, as `--map-current-user` makes, it passes.

### API

Against v0.10.0, no exported name is removed, moved or changed: the API check finds no break from v0.10.0 to v0.11.0 on darwin, linux or windows. Added, `experimental`: in `trust/trusttest`, `SystemFile`.

### Size

Non-test Go lines outside internal/ but internal/winapi, v0.10.0 to v0.11.0, as `task size` counts them: appdirs 799, unchanged; confine 3,436, unchanged; filestest 201, unchanged; lock 561, unchanged; nowait 44, unchanged; trust, trust/trusttest among it, 3,932 to 4,091; write 2,030, unchanged; and internal/winapi 661, unchanged. 11,664 to 11,823 in all, 159 more: 11,823 of the ceiling's 11,823, with no headroom. treesum: 504, unchanged.

go-files v0.11.0 requires golang.org/x/sys v0.50.0, golang.org/x/text v0.44.0 and, for its tests alone, github.com/birdtable/go-testkit v0.2.0, all unchanged since v0.10.0. Its youngest requirement, github.com/birdtable/go-testkit v0.2.0, tagged 2026-11-21, is 1 day old on 2026-11-22; golang.org/x/text v0.44.0, published 2026-11-09, is 13 days old.

## v0.10.0 (2026-11-22)

v0.10.0 adds eight names to `confine` and `write` and a new package, `filestest`, for a program's tests. No exported name is removed, moved or changed. Before upgrading, check four things a program may meet:

- **Windows volumes with no file IDs.** `confine`, `lock`, `appdirs`, `treesum` and `trust` now refuse, with `write.ErrNoFileIndex`, a file whose 64-bit index cannot tell it apart from another, as a WebDAV drive or some SMB servers give every file and ReFS gives some. They took two such files for one before. `confine.Below` refuses there too, where it answered that any path was below the folder.
- **Unclean paths in `confine.Resolve` and `confine.Below`.** An absolute path with `..` or a doubled separator is now taken and walked where the system takes it, where it was refused matching `fs.ErrInvalid`. On Unix the path is walked as written, so `tree/d/../x`, with `d` a link, is the `x` beside `d`'s target, and `Below(tree, ...)` refuses it with `ErrOutside`.
- **`trust`'s refusal texts.** Eighteen refusals that named a field of `Who` now say what the field stands for. Match the errors with `errors.Is`; a program that compared the text breaks.
- **go.mod.** It now requires `github.com/birdtable/go-testkit` for the module's tests alone. A program that imports go-files builds none of it, but it appears in `go mod graph`.

### Added

**confine**
* `MkdirShared(root, name)` makes folders other accounts reach, and `MkdirPrivate(root, name)` folders this account's alone. On Unix they are `Mkdir` with 0o755 and 0o700. On Windows a `MkdirShared` folder has no access list of its own and holds the folder above's inheritable entries alone, so who reaches it is that folder's say. Where a caller meets a refusal, with `ErrOutside`: a folder it made that is not this account's (or Administrators' where the token is elevated) or not empty when opened, and one whose list is not wholly inherited, as one swapped in with an entry of its own or a protected list. That includes every folder made under a folder above that has no inheritable entry: Windows gives such a folder the token's default list, with no inherited entry, so `MkdirShared` refuses it. A volume with no access lists is refused with `ErrNoAccessList`.
* `MkdirFresh(root, prefix)` makes one new folder of this account's alone under a name it picks, `prefix` and 16 random hex digits, and answers it opened, with its name, for `RenameNoReplace` to move into place. After 16 names taken it is refused matching `fs.ErrExist`; a prefix `Portable` would refuse in the name, or `.git`, matching `fs.ErrInvalid`. The folder is the owner's alone, but its name is `root`'s: an account that can write `root` can rename or remove it.
* `Nearest(start, marker, top)` answers the nearest folder at `start` or above it that holds `marker`, walked up by `start`'s own names with no link resolved. A link marker counts, a dangling one too. A `marker` `Local` refuses, an empty one, and a `top` that is not `start` or above it are refused with `ErrOutside` before any look; with no marker found, the error matches `fs.ErrNotExist`. The answer is not judged: hand it to `trust.Path(dir, trust.Folder, who)` before confining to it, or name a `top` you trust.
* `Readlink(root, name)` answers the target of the link at `name`, one hop, as the link holds it, with no link followed on the way. A program moving from `os.Root.Readlink` meets other refusals: a name with a link, junction or other reparse point before its last component is refused with `ErrOutside`, where `os.Root.Readlink` follows one that stays inside; a name that is no link matches `fs.ErrInvalid`, where it gave a bare `EINVAL`; and on Windows a reparse point that is neither a link nor a junction matches `ErrOutside` and never `fs.ErrNotExist`, which `os.Readlink` answered. A name that is not there still matches `fs.ErrNotExist`.
* `Remove(root, name)` and `RemoveAll(root, name)` remove a name in an `os.Root` with no link followed before the last component, where `root.Remove` follows one that stays inside. A link or junction at the last component, or in `RemoveAll`'s tree, is removed as the link and never walked. A name that is not clean and local, the root itself, and on Windows a component ending in a dot or a space are refused with `ErrOutside`. On Windows a file another process holds is tried again for about 600 ms, a read-only file is removed, and a refusal past that matches `fs.ErrPermission`.

**write**
* `Shared()` writes a file other accounts read: 0644 on Unix, 0755 with `Runnable`, and on Windows no access list of its own, so it holds the folder above's inheritable entries. It is taken on a volume with no access lists. `Shared` with `Mode`, in either order, is refused matching `fs.ErrInvalid` before anything is made, and `Swap` and `Append` refuse it. Over an old plain file under `KeepMode` or `KeepOwner` a Windows file keeps the owner-only list, and `Keep` and `PutBack` take `Shared` without applying it.

**filestest**, a new package for a program's tests
* `WriteFile(t, path, content)` writes the library's private file, and `WriteProgram(t, path, content)` a program the test then starts at once, with no "text file busy". Neither makes a missing folder, and each fails the test saying the path.
* `Private(t)` answers a fresh folder of this account's alone, which `trust.Path` judges a `trust.PrivateFolder` where on Unix it refuses `t.TempDir`'s. It is removed at the test's end, and a failed removal fails the test.
* `AllPortable(t, paths...)` fails a test once, naming each slash-separated path `confine.Local` refuses, each name in one `confine.Portable` refuses, and each pair that meets where a folder folds case or a normal form, with the same message on every system. `HostileNames()` answers the list of names and paths a program must refuse, each with the rule that refuses it.

**lock**
* A lock file this account may not open to write, or may not make in its folder, matches `fs.ErrPermission` on every system, Windows included, with nothing made.

### Changed

**confine**
* `Resolve` and `Below` take any absolute path, clean or not, and answer where the system takes it: walked as written on Unix, and on Windows, which takes a `..` by its spelling, as `filepath.Clean` gives it. A relative path is still refused matching `fs.ErrInvalid`, and so is a `folder` for `Below` that is not absolute and clean. A path the system refuses, as a trailing separator after a file, is refused with the system's own error. On Windows a device path (`\\?\`, `\??\`, `\\.\`) is still taken only clean.

**trust**
* The refusal texts that named a field of `Who` now say what it stands for: "the mounts named as trusted", "the folders named to be kept apart", "no file system type named as trusted" and "the overflow account setting". `ErrMountEntry`'s own text changes with them. Error identities, `Fix`, `Unsafe.Path` and every exported name are unchanged: match `ErrMount`, `ErrMountEntry`, `ErrInside` or `ErrOverflow` with `errors.Is`.

**The module**
* go.mod requires `github.com/birdtable/go-testkit`, the kit the module's own tests start from. No package of the module imports it, so a program that takes one builds none of it, and the module graph names no go-proc.
* `API.txt` is gone. A tag's exported surface is `go doc -all <package>@<tag>`, and `api/` holds what the release checks: the names each system exports alike, the breaks since the last tag, and the marks.
* `api/contract.txt` is the promise index: each line names a package, the test that proves the promise, the systems it holds on and the promise.
* What each package may import is said in its doc comment and held by the layer rows of `.golangci.yml`.

### Fixed

**confine**, **lock**, **appdirs**, **treesum**, **trust** and **write**, on Windows
* A file with no 64-bit index that tells it apart is refused, where two such files were taken for one and a file swapped in at a name could be missed. A zero index is what a volume with no file IDs gives every file, and an all-ones one what ReFS, whose real file ID is 128-bit, gives some files. The refusal is one value, `write.ErrNoFileIndex`, which matches `errors.ErrUnsupported`, so `errors.Is(err, write.ErrNoFileIndex)` holds of each package's refusal. `confine.Below` refuses on such a volume, where it would have answered that any path is below a folder. `write` already refused such a file but in two compares, which now refuse it too.

**write**
* A `NoReplace` write whose temp name cannot be removed once the file is placed now finishes the write before it says so: the folder is flushed, and on Windows a mode without owner write leaves the name read-only. The error still says the file is in place and its temp file is left, and a flush that then fails joins `ErrNotDurable` to it.

**trust/trusttest**, on Linux
* `Bind` skips, as its doc says, on a kernel from 5.2 to 5.11, where it failed the test on 5.2 to 5.7. Each skip of a refused mount names the call refused, `open_tree` before 5.2 and `mount_setattr` from 5.2 to 5.11, besides its errno. Its doc now says that a caller mixing `Nosuid` and `NoexecWithoutNosuid` across processes runs those `Bind`s one at a time.

The library needs no privilege: its tests run with no root on Linux and no elevated shell on Windows.

### Known limitations

* On Windows, `MkdirShared` checks that every entry of a folder's list was inherited, not from which folder it was inherited.
* The release workflow's run at a given commit, by hand, is not yet proved on GitHub's hosted runners.
* In a Linux user namespace that maps root over the host's files, as `unshare --map-root-user` makes, `/` is the overflow account's, and `filestest.Private` fails the test. In one that maps this account alone, as `--map-current-user` makes, it passes.

### API

Against v0.9.1, no exported name is removed, moved or changed: the API check finds no break from v0.9.1 to v0.10.0 on darwin, linux or windows. Added, each `experimental`: in `confine`, `MkdirFresh`, `MkdirPrivate`, `MkdirShared`, `Nearest`, `Readlink`, `Remove` and `RemoveAll`; in `write`, `Shared`; and the package `filestest`, with `AllPortable`, `HostileName`, `HostileNames`, `Private`, `WriteFile` and `WriteProgram`. A tag's whole surface is `go doc -all <package>@<tag>`.

### Size

Non-test Go lines outside internal/ but internal/winapi, v0.9.1 to v0.10.0, as `task size` counts them: appdirs 733 to 799; confine 2,728 to 3,436; filestest, new, 201; lock 529 to 561; nowait 44, unchanged; trust, trust/trusttest among it, 3,884 to 3,932; write 1,936 to 2,030; and internal/winapi 515 to 661. 10,369 to 11,664 in all, 1,295 more: 11,664 of the ceiling's 11,664, with no headroom. treesum: 462 to 504, at its ceiling of 504.

go-files v0.10.0 requires golang.org/x/sys v0.50.0 and golang.org/x/text v0.44.0, both unchanged since v0.9.1, and now github.com/birdtable/go-testkit v0.2.0, for its tests alone. Its youngest requirement, github.com/birdtable/go-testkit v0.2.0, tagged 2026-11-21, is 1 day old on 2026-11-22; golang.org/x/text v0.44.0, published 2026-11-09, is 13 days old.

## v0.9.1 (2026-11-21)

v0.9.1 closes a way for an account that is not root to name another account as its FUSE mount's mounter, in how `trust` reads Linux's `/proc/self/mountinfo`, and fixes a test helper, `trust/trusttest`'s `Bind`. No exported name is added, removed or moved. Upgrade every program that calls `trust` on Linux: read Security. Before upgrading, check the kernel and the seccomp profile your tests that call `trusttest.Bind` run under: `Bind` now needs Linux 5.12 and a profile that lets `open_tree`, `mount_setattr` and `move_mount` through, and skips its test, saying so and naming the errno, `ENOSYS` or `EPERM`, where either is missing; v0.9.0's named no kernel of its own. A skip is not a pass: a test that skips has shown nothing of the mount it asked for, so read a run's skips on a kernel before 5.12 or under a container engine's seccomp profile.

### Fixed

**trust/trusttest**
* `Bind` judges an entry held across processes as well as in one, so test binaries that share one namespace, as `go test ./...` in one `unshare` runs them, no longer stack a mount over a live one's entry or skip where one is free. It reads `/proc/self/mountinfo` before and after its mount, with no lock between processes and no wait: an entry where a `Bind`'s mount stands, its root a folder named `trusttest-bind`, is held, and of two mounts made over one entry at once the earliest keeps it and the later is undone, its `Bind` going on to the next entry. A `Bind`'s mount left by a process that ended without its unmount holds its entry while the namespace lasts, and the skip says so. Each mount is made with its flag set before it is placed, by `open_tree`, `mount_setattr` and `move_mount`, so `Bind` now needs Linux 5.12 and a seccomp profile that lets those three calls through, and skips, saying so and naming the errno, `ENOSYS` or `EPERM`, where either is missing. Its read of mountinfo splits each line on the kernel's one space alone, so a temp folder named with a `\r` or U+00A0 hides no `Bind`'s mount from another, and two processes' `Bind`s do not stack on one entry there.

### Security

**trust**, on Linux
* `trust` read a FUSE mount's mounter, and a mount's type, from `/proc/self/mountinfo` with each line split at every character Unicode counts as space, where the kernel separates the fields with one space and escapes only a space, tab, newline or backslash within one. An account that is not root, mounting FUSE with `allow_other`, as `user_allow_other` in `/etc/fuse.conf` lets it, with a source or subtype holding a `\r`, `\v`, `\f`, U+0085 or U+00A0 followed by `user_id=` and another account's number, was taken for that account: named as the reader's own, `Path` and `ReadOwned` passed the reader a name on a server the mounter runs, its owner and mode the server's word. A mount point so spelled could read as virtiofs, which has no mounter, or as another type. Each line is now split on the kernel's one space alone, so such a mount names the account that mounted it, and a name on it is refused, with `ErrNotOwners`, as on any mount of an account not trusted. v0.9.0 and every earlier version are affected. `TestAMountinfoLineIsSplitOnTheKernelsSpaceAlone` holds it.

The library needs no privilege: its tests run with no root on Linux and no elevated shell on Windows.

### API

API.txt is the same at v0.9.0 and at v0.9.1: `git diff v0.9.0 v0.9.1 -- API.txt` prints nothing. No name is added, removed, moved or changed.

### Size

Non-test Go lines outside internal/ but internal/winapi, v0.9.0 to v0.9.1, as `task size` counts them: appdirs 733, unchanged; confine 2,728, unchanged; lock 529, unchanged; nowait 44, unchanged; trust, trust/trusttest among it, 3,735 to 3,884; write 1,936, unchanged; and internal/winapi 515, unchanged. 10,220 to 10,369 in all, 149 more: 10,369 of the ceiling's 10,369, with no headroom. treesum: 462, unchanged.

go-files v0.9.1 requires golang.org/x/sys v0.50.0 and golang.org/x/text v0.44.0, both unchanged since v0.9.0. Its youngest requirement, golang.org/x/text v0.44.0, published 2026-11-09, is 12 days old on 2026-11-21.

## v0.9.0 (2026-11-20)

v0.9.0 adds five names, `trust.ErrMountEntry` and, in `trust/trusttest`, `Bind`, its type `Bound` and the two `Bound` values `Nosuid` and `NoexecWithoutNosuid`, and changes no signature. `trust` now judges each folder of `Who.Mounts` before it lends a mount's pass. Before upgrading, check each `Who.Mounts` entry a program names: an entry that is not absolute and clean, lies below a name an account but root may change, is a link, is no mount's root, or names nothing now refuses the call with `ErrMountEntry`, where before it lent its pass or named none. `ReadOwned` now reads `Who.Overflow` from its `mounts`, and refuses it with `ErrOverflow` outside a test in a Linux namespace that maps no root, where before it ignored it.

### Added

**trust**
* `ErrMountEntry`, the refusal of a folder of `Who.Mounts` that is not a mount's root at a name only root can move, with the entry as `Unsafe.Path`; it matches `ErrMount` too. An entry that passes is not vouched for by whose ask it was mounted on: a mount point a root helper fills for whoever plugs a disk in, as `/media/usb0`, lends its pass to that disk.

**trust/trusttest**
* `Bind(t, how)`, which gives a test on Linux, with no root, a mount nosuid (`Nosuid`) or noexec and nodev without nosuid (`NoexecWithoutNosuid`) for trust to judge: it binds a folder of the test's temp folder over the first folder real root made of `/mnt` and `/srv`, whatever it holds, only in a user namespace other than the first, as `unshare --user --map-current-user --mount --keep-caps` makes, and returns that entry and a folder of this account's on the mount, which a test walks with `trust.Who{Overflow: true, Mounts: []string{entry}}`. Parallel tests may each call it. It skips, saying why, off Linux, in the first user namespace, where it may not mount, where no folder qualifies, and where the temp folder's disk is one whose flags trust does not judge, as a tmpfs `/tmp`.

### Changed

**trust**
* `Path`, `Held` and `ReadOwned` judge each folder of `Who.Mounts`, at every call and before the name, and refuse the call with the new `ErrMountEntry` unless the folder is absolute and clean, every name above it is one only root may change, with the call's entries above it, judged first, shallowest first, as the walk's `Who.Mounts`, so nested entries pass where each is named and none vouches for itself, it is no link, and it is the root of a mount: on Linux by its mount (statx's `STATX_ATTR_MOUNT_ROOT`, so a disk bound over a folder of the same disk counts, and a kernel before 5.8 tells none), on macOS and every other Unix by a device other than its parent's. The folder's owner is not judged, so an account's `/run/user/<uid>` passes. An entry that is a link, a folder an account made, or one whose disk is not mounted, which named the disk beneath it, lent a mount's pass before; an entry that names nothing is now refused with `ErrMountEntry` for every call, never matching `fs.ErrNotExist`. Windows reads no `Who.Mounts`, as before.
* `ReadOwned` reads `Who.Overflow` from its `mounts` too, as `Path` and `Held` do, where any of them sets it, behind the same gates, so a test in a namespace that maps no root can read a file below a `Who.Mounts` entry; outside them it refuses with `ErrOverflow`, reading nothing, where it ignored the field before. A read that sets no `Who.Overflow` is unchanged.

**appdirs**
* `Temp` and `TempOverflow` take the judgement of `trust` above: a `Who.Mounts` entry `trust` refuses refuses them too, before anything is made, and one that names nothing is no longer taken for the temp folder not yet made.

The library needs no privilege: its tests run with no root on Linux and no elevated shell on Windows.

### API

Against v0.8.0, API.txt gains five lines and loses none: `const trust/trusttest.NoexecWithoutNosuid`, `const trust/trusttest.Nosuid Bound`, `func trust/trusttest.Bind(t testing.TB, how Bound) (entry, dir string)`, `type trust/trusttest.Bound int` and `var trust.ErrMountEntry`, each marked `experimental`. No name is moved, removed or changed.

### Size

Non-test Go lines outside internal/ but internal/winapi, v0.8.0 to v0.9.0, as `task size` counts them: appdirs 733, unchanged; confine 2,728, unchanged; lock 529, unchanged; nowait 44, unchanged; trust, trust/trusttest among it, 3,369 to 3,735; write 1,936, unchanged; and internal/winapi 515, unchanged. 9,854 to 10,220 in all, 366 more, at the ceiling of 10,220 with no headroom. treesum: 462, unchanged.

go-files v0.9.0 requires golang.org/x/sys v0.50.0 and golang.org/x/text v0.44.0, both unchanged since v0.8.0. Its youngest requirement, golang.org/x/text v0.44.0, published 2026-11-09, is 11 days old on 2026-11-20.

## v0.8.0 (2026-11-20)

v0.8.0 adds two names, `appdirs.HomeVar` and `appdirs.Sources`, and removes or changes no other. They say which variables `HereWith` reads, for a program that may not name the system. Nothing to change before upgrading.

### Added

**appdirs**
* `HomeVar`, the variable `HereWith` reads the home from on the system it is built for (`USERPROFILE` on Windows, `HOME` on every Unix system), and `Sources(layout)`, for each of the five folders the variable `For` reads it from there, or `""` where the home alone moves it. A test that may not name the system builds its program's environment from `map[string]string{appdirs.HomeVar: dir}` and sets the variables `Sources` names, to give each of a parallel test's programs folders of its own.

The library needs no privilege: its tests run with no root on Linux and no elevated shell on Windows.

### API

Against v0.7.0, API.txt gains two lines and loses none: `const appdirs.HomeVar` and `func appdirs.Sources(layout Layout) (Folders, error)`, each marked `experimental`. No name is moved, removed or changed.

### Size

Non-test Go lines outside internal/ but internal/winapi, v0.7.0 to v0.8.0, as `task size` counts them: appdirs 686 to 733; confine 2,728, unchanged; lock 529, unchanged; nowait 44, unchanged; trust, trust/trusttest among it, 3,369, unchanged; write 1,936, unchanged; and internal/winapi 515, unchanged. 9,807 to 9,854 in all, 47 more, at the ceiling of 9,854 with no headroom. treesum: 462, unchanged.

go-files v0.8.0 requires golang.org/x/sys v0.50.0 and golang.org/x/text v0.44.0, both unchanged since v0.7.0. Its youngest requirement, golang.org/x/text v0.44.0, published 2026-11-09, is 11 days old on 2026-11-20.

## v0.7.0 (2026-11-20)

v0.7.0 adds one name, `appdirs.HereWith`, and removes or changes no other. It is `Here` with the caller's environment, for a program that may not name the system. Nothing to change before upgrading.

### Added

**appdirs**
* `HereWith(app, layout, env)`, `Here` with the caller's environment. It takes the system from its build constraints as `Here` does, reads the home from `env` (`USERPROFILE` on Windows, `HOME` on every Unix system, as `os.UserHomeDir` reads it) and every other variable from `env` too, and answers exactly as `For` does: a nil `env`, or an empty home, is `ErrHome`, and it never falls back to the process's own home or environment. A program that may not name the system can now give each of a parallel test's programs an environment of its own.

The library needs no privilege: its tests run with no root on Linux and no elevated shell on Windows.

### API

Against v0.6.0, API.txt gains one line and loses none: `func appdirs.HereWith(app string, layout Layout, env func(string) string) (Folders, error)`, marked `experimental`. No name is moved, removed or changed.

### Size

Non-test Go lines outside internal/ but internal/winapi, v0.6.0 to v0.7.0, as `task size` counts them: appdirs 656 to 686; confine 2,728, unchanged; lock 529, unchanged; nowait 44, unchanged; trust, trust/trusttest among it, 3,369, unchanged; write 1,936, unchanged; and internal/winapi 515, unchanged. 9,777 to 9,807 in all, 30 more, at the ceiling of 9,807 with no headroom. treesum: 462, unchanged.

go-files v0.7.0 requires golang.org/x/sys v0.50.0 and golang.org/x/text v0.44.0, both unchanged since v0.6.0. Its youngest requirement, golang.org/x/text v0.44.0, published 2026-11-09, is 11 days old on 2026-11-20.

## v0.6.0 (2026-11-20)

v0.6.0 adds one name, `write.ErrNoFileIndex`, and removes or changes no other. On Linux, `trust` now refuses a disk mounted `noexec` without `nosuid` as it refuses one mounted `nosuid`, until `Who.Mounts` names it. On Windows, `write` refuses a volume or file whose index cannot tell two files apart, and on every system `NoReplace` places only the file written. Read Breaking and Security before upgrading.

### Breaking

**trust**, on Linux
* `Path`, `Held`, `ReadOwned` and `appdirs.Temp` refuse a name on a disk mounted `noexec` without `nosuid` with `ErrMount`, as they refuse one mounted `nosuid`, until `Who.Mounts` names a folder on the same device. No CIS or STIG profile, systemd's `tmp.mount` or udisks mounts a disk so. These do, and carry the cost: a systemd unit's `NoExecPaths=`, which remounts each path `noexec` alone, so in a unit with `NoExecPaths=/` a walk is refused at `/` until `Who.Mounts` names each disk it crosses; a disk mounted in fstab with `noexec` alone; and a ZFS dataset with `exec=off`.

**trust/trusttest**, on Linux
* `Perm` calls `Held` with no `Who`, so a private want on a disk mounted `noexec` without `nosuid` now fails a program's test with "it is not private to this account", whatever the mode, as one on a disk mounted `nosuid` already did in v0.5.0. `Perm` has no `Who` to name the mount with, and its doc now says so: a test that needs a private name keeps it on a disk without those flags, as a GitHub runner's work folder is, or on a tmpfs.

### Added

**write**, on Windows
* `ErrNoFileIndex`, matching `errors.ErrUnsupported`: the refusal of a file whose 64-bit file index cannot tell it from another, as Fixed says.

### Fixed

**write**, on Windows
* A file whose 64-bit file index reads zero or all-ones is refused, where `os.SameFile` would call any two such files the same and miss one swapped in at the name. A zero index is a volume's answer (every file on a WebDAV drive or some SMB servers); an all-ones one is a file's (ReFS, whose 128-bit ID the 64 bits cannot hold, for some files). `File`, `Keep`, `PutBack`, `Swap` and `Append` each read the index of a handle they hold and refuse with the new `ErrNoFileIndex`. FAT32 answers a non-zero directory-entry index (measured on one FAT32 volume) and is refused earlier anyway for want of access lists; exFAT was not measured. Reading ReFS's 128-bit ID, and the compares still unguarded (`heldBy`, and `NoReplace`'s second look at the placed name), are left for a later release.

**write**
* `NoReplace` places only the file written. On Windows it links by the handle held on the file written, so a file put at the temp name after the last check is never linked (where the swap took the written file's last name, NTFS refuses the link, and the write is refused as swapped). On Unix, where no call links a descriptor on every system, it links by the temp name, reads the name against the file written, which the write holds open from the last check, and takes a file that is not it away again and refuses the write as swapped; a process that opens the name between the link and the take-away has the other file, and a take-away that fails says so. A close that fails on the handle held to the end of the write, after the file is in place, is an error that says the file is in place.

### Security

**trust**, on Linux
* An account's stick that usbmount mounts with its default options, `noexec` and `nodev` without `nosuid`, no longer passes on the owners its volume holds, so a file its maker set to root's is no longer taken as root's. A mount without `nosuid` passed because whoever can forge owners there can already plant a program setuid-root, and `noexec` takes that program away. A FUSE mount root's helper made `noexec` without `nosuid` is refused so too. The `noexec` flag is read by fstatfs on the name held, and the refusal is shown with no root, on a disk bound `noexec,nodev` in a user and mount namespace.

The library needs no privilege: its tests run with no root on Linux and no elevated shell on Windows.

### API

Against v0.5.0, API.txt gains one line and loses none: `var write.ErrNoFileIndex`, marked `experimental`. No name is moved, removed or changed.

### Size

Non-test Go lines outside internal/ but internal/winapi, v0.5.0 to v0.6.0, as `task size` counts them: appdirs 655 to 656; confine 2,728, unchanged; lock 529, unchanged; nowait 44, unchanged; trust, trust/trusttest among it, 3,345 to 3,369; write 1,683 to 1,936; and internal/winapi 494 to 515. 9,478 to 9,777 in all, 299 more, at the ceiling of 9,777 with no headroom. treesum: 462, unchanged.

go-files v0.6.0 requires golang.org/x/sys v0.50.0 and golang.org/x/text v0.44.0, both unchanged since v0.5.0. Its youngest requirement, golang.org/x/text v0.44.0, published 2026-11-09, is 11 days old on 2026-11-20.

## v0.5.0 (2026-11-20)

v0.5.0 adds 29 names in five packages, changes one signature, `appdirs.Temp`'s, and removes nothing else. New are a way to judge a file already open (`trust.Held`), the commands that make a `Fix` (`Fix.Commands`), the programs a `#!` line starts (`trust.Interpreters`), helpers for the tests of programs that use `trust` (`trust/trusttest`), path and name checks in `confine` (`Resolve`, `Below`, `Portable`, `Collide`, `LinkFile`, `FileURL` and `FromFileURL`), `write.Swap` for replacing a running program, and three `appdirs` calls (`Here`, `System`, `TempOverflow`). `trust.Path` now follows a `..` after a link as the system does, and on Linux refuses a disk mounted nosuid until the new `Who.Mounts` names it: read Breaking and Security before upgrading. The module now requires `golang.org/x/text`, for `confine.Portable`.

### Breaking

**trust**, on Linux
* A name on a disk mounted nosuid, as udisks, fuse2fs run by an account and fstab's user option mount an account's stick or image, and as a CIS /home, /tmp or /var/tmp partition is mounted, is refused with `ErrMount`, which matches `ErrNotOwners`, until the new `Who.Mounts` names a folder on the same device; a `Who.Mounts` entry that is relative, or that cannot be read, names none. A FUSE mount root's helper made nosuid, as udisks mounts a stick with ntfs-3g or exfat-fuse, is refused so too until `Who.Mounts` names it; tmpfs, ramfs, overlayfs, NFS and a FUSE mount this account made are not judged by nosuid. In a systemd unit with `ProtectSystem=strict` or `DynamicUser=`, on a podman volume and in every bwrap or Flatpak bind, every disk is nosuid, so a walk is refused at / until `Who.Mounts` names each disk it crosses; a Docker bind is not nosuid. `ReadOwned` refuses it as `Path` does, unless a `Who` given it names the mount.

**appdirs**
* `Temp(app string, apart ...string)` is now `Temp(app string, who ...trust.Who)`: apart folders move to `Who.Apart`, and the `Who.FileSystems` and `Who.Mounts` of each `Who` reach both of its walks, so a temp folder on a nosuid /tmp, /var/tmp or TMPDIR disk passes once named.

### Added

**trust**
* `Held(f, last, who)` judges a file or folder already open as `Path` judges the last name of a path of the kind `last`, on its descriptor, never by a name, and walks no folder above it: its type, owner, mode bits and access lists, and the volume it is on, by `fstat`, `fstatfs` and on Linux `fgetxattr`, and on Windows by `GetSecurityInfo` on the handle, so a folder there is judged by its access list and not by the 0777 Go reports. Every rule `Path` keeps for the last name holds, with the same refusals, and no `Fix`: `Held` judges a file, not its name, so a fix for a name is `Path`'s to give. `Who.Apart` is held to the file's own file ID. How `f` was reached is the caller's: it is not held to root's sticky folder, and a program walks the folder with `Path` before it opens the name. On macOS, where the access list is read by the name, a name moved away or replaced is refused with `ErrReplaced`.
* `Fix.Commands()` gives a `Fix` as the commands that make it, each a `Step` whose `Argv` is one program's words, never quoted or joined: `chmod` with symbolic words and `chown -h root` on Unix, and `icacls /reset /L`, `icacls /inheritance:d /L` then `/remove:g *<SID> /L`, and `icacls /setowner *S-1-5-32-544 /L` on Windows. Each step acts on the name itself, never on what a link there names, as the walk judges a link by its own owner and list, and the owner step comes last. `Step.Admin` says a step needs root or an elevated prompt; no argv holds `sudo`. What the fix holds decides the system its commands are for, not the system this runs on. It gives `ok` false, and no steps, for a fix no command here makes whole: a path not absolute for the fix's system or holding a NUL, bits beyond the nine, another owner, a macOS or NFSv4 revoke, a SID that is not one, or Windows's and Unix's changes mixed. The caller quotes each word for the shell it names. Nothing in trust quotes or joins a command line.
* `Interpreters(path)` gives the programs a file's `#!` line would have the system start, in order, the last being the program the system runs, each read by that system's own rule, proven by starting scripts there: on Linux the first 256 bytes and up to 5 scripts in one chain, on macOS a line a newline or a `#` ends within 512 bytes and no script's interpreter a script. `#!/usr/bin/env x` ends at `/usr/bin/env`; env's argument is not read. It is an error matching the system's errno where the system starts none of it: a line naming no interpreter or cut, an interpreter not absolute, a name not a regular file, a chain the system refuses. A file with no `#!` line gives none, and so does every file on Windows; on FreeBSD and NetBSD a `#!` file is an error matching `errors.ErrUnsupported`.
* `Who.Mounts`, the folders whose nosuid disks are trusted on Linux, as Breaking says.

**trust/trusttest**, a new package, for the tests of a program that judges its paths with `trust`
* `Loosen(t, path, who)` lets its group or others read or write a name, by one mode bit on Unix and an allow entry for Users or Everyone on Windows; `Deny(t, path)` keeps this account out, by clearing the mode on Unix and an empty protected access list on Windows; and `Perm(t, path, want)` checks the mode this system keeps for `want` and, where `want` lets group and others nothing, that `Held` judges the name private to this account. Each puts back what it changed when the test ends, and skips the test, saying so, only where this account still opens a denied name, as root on Unix, or a Windows volume keeps no access lists.

**confine**
* `Resolve(path)` answers an absolute, clean path with every link on it followed one hop at a time, as the system follows them, a junction on Windows included, which `filepath.EvalSymlinks` has not followed since Go 1.23, and judges no ownership: a relative target is taken against the link's own folder, no path is cleaned before its links are read, so `d/../x` with `d` a link out leads out on Unix, a target rooted with no drive is on the link's volume, and the system's own `os.Stat` must find the same file at the end or the path is refused as changed. `Below(folder, path)` answers the name of the resolved path in `folder`, comparing folders by file ID, never by spelling. Both refuse, matching `ErrOutside`, more than 32 links (`ErrLoop`), and on Windows a target on a drive with no root (`C:x`).
* The README says how to ask whether a path is inside a root, with no new function: `filepath.Rel` and `filepath.IsLocal`, then open the name through the `*os.Root` already held, so the file system and not the string decides about links.
* `Portable(name)` answers whether one name means one file, and the same one, on every system, with a key two names share when a folder that folds case or Unicode normal form reads them as one. It refuses a name Windows reserves or strips, an 8.3 short name, a format character and any other character that shows as nothing and no normal form removes (the combining grapheme joiner, the variation selectors, the Hangul fillers), a character whose full case folding is several, a name whose composed or decomposed form one of these rules refuses (`≮`, which macOS keeps as `<` and a slash), and `.git` in any case. A name in any normal form passes: the key is the name decomposed, folded a character at a time and composed again, by `golang.org/x/text/unicode/norm`, so `café` composed and decomposed share one. Its `.git`, 8.3 and HFS+ rules are ported from go-git.
* `FileURL(goos, abs)` and `FromFileURL(goos, uri)` turn an absolute path into a `file:` URL and back for the system named, in the form Go's own `go` command writes, ported from its `cmd/go/internal/web` with the licence in NOTICE: `/a/b` is `file:///a/b`, `C:\a\b` is `file:///C:/a/b`, and `\\host\share\a` is `file://host/share/a`. Both refuse, matching `fs.ErrInvalid`, a path that is not absolute, a `.` or `..` element and a NUL byte, and `FromFileURL` refuses a user, port, query or fragment, the short forms `file:/a` and `file:a`, a host but `localhost` off Windows, and on Windows a drive as the host, a backslash, and an element ending in a dot or a space, holding a colon, or a device name such as `CON` or `NUL`. A host that starts with `;`, and the hosts `.` and `..`, are refused on every system. An 8.3 short name such as `PROGRA~1` passes, since its spelling can't be told from a plain name, so a caller that matches names resolves the path first. A system with no form here matches `errors.ErrUnsupported`.
* `Collide(root, a, b)` answers whether a folder reads two names as one file, by looking at what is there with no link followed, or by making, looking for and removing a probe: case and Unicode normalisation belong to the volume and the folder, not to `runtime.GOOS`. The probe is held open from its making until its removal, so a file put in its place is never taken for it, even where the file system reuses a freed inode number at once (ext4).
* `LinkFile` makes a symbolic link to a file, under `LinkFolder`'s refusals, never a hard link or a copy: on Windows it is set by handle on a new file, and an account without the privilege to make a symbolic link, as one without Developer Mode, is refused with `ErrNoFileLink`, matching `errors.ErrUnsupported`, with nothing made.

**write**
* `Swap(file, fresh, aside, opts...)` replaces a program that may be running: the old file is moved aside to the name the program gives, the new one takes its name, and the old one is moved back if that fails, so the name never ends without one of the two. Windows will not write over or remove a running program but renames one. The three names are plain names in one folder, compared by `confine.Portable`'s key, so `café` composed beside `café` decomposed, or `pass` beside `paß`, is refused before anything moves. Neither move goes over anything, so a taken aside name is refused with `fs.ErrExist` before anything moves, and a file put at the name between the moves is kept, with `ErrNotSwapped` where the put-back fails too: then the name may hold a file `Swap` never checked, which the caller must not run. `IfHolds` refuses a program that changed, and a link, folder or pipe at either name is refused. On Unix each move is a hard link and a removal; on Windows a rename by the file's handle with no replace, written through unless `NoSync`. Every option but `IfHolds` and `NoSync` is refused with `fs.ErrInvalid`.

**appdirs**
* `Here(app, layout)` is `For` on the system it runs on, with the system it is built for, `os.UserHomeDir` and `os.Getenv` read inside, and `System(goos, app, env)` names the machine-wide folder: `%PROGRAMDATA%\<app>` on Windows, never a share, `/Library/Application Support/<app>` on macOS, `/etc/xdg/<app>` elsewhere, never moved by `XDG_CONFIG_DIRS`.
* `TempOverflow(app, who...)` is `Temp` for a test in a Linux user namespace that maps this account and not root, where `/` and root's `/tmp` show as the kernel's overflow account and `Temp` refuses, naming it: both of its walks set `trust.Who.Overflow`, so `trust` trusts that account as root and judges every other rule as `Temp` does. It is for tests alone: outside a test binary in such a namespace it refuses with an error matching `trust.ErrOverflow`, and off Linux one matching `errors.ErrUnsupported` too, and nothing is made. `Temp` never sets `Who.Overflow`.

### Changed

**appdirs** and **write**
* A program's name in `appdirs` (`For`, `System`, `Here`, `Temp`) and a name in `write` (`Keep`'s copy, `Swap`'s three) are judged by `confine.Portable`, on every system, so each now also refuses an 8.3 short name, `.git`, a format character or another character that shows as nothing, and a character whose case folding is several. `appdirs` still refuses `..` inside a name. Every name in the two packages' own tests gives the answer it gave before.

**write**
* `Runnable`'s doc says how to ask whether this system would run a file: `exec.LookPath` on its absolute path, which checks the exec bit on Unix and `PATHEXT` on Windows. No function is added.

**The module**
* go-files now requires `golang.org/x/text` v0.44.0, whose `unicode/norm` alone `confine` links; `write`, `appdirs` and `treesum` link it through `confine`, and `lock`, `trust`, `trust/trusttest` and `nowait` do not.

### Fixed

**write**
* `Append` ends a torn last line before it appends: a log whose last byte is not a newline, as a crash in another writer's write can leave, gets a newline before the new line, in the same one write, so the new line never joins the half-written one. The torn line is kept as it is, never cut; the newline counts toward the cap, and a log moved aside keeps its torn line.

### Security

**trust**
* `Path` no longer cleans a link's target before its links are read: on Unix a `..` in a target, after a link, goes up from where that link leads, as the system goes, and is judged there, so a link to `d/../x`, with `d` a link elsewhere, is judged at the `x` the system opens and not the `x` beside the link; Windows still takes a target by its spelling, as the system does.
* On Linux, an account's stick or image that udisks, fusermount, fuse2fs or fstab's user option mounted nosuid no longer passes on the owners it holds, so a file its maker set to root's is no longer taken as root's; nor does a stick root's FUSE helper mounted nosuid, as udisks' ntfs-3g or exfat-fuse, whose owners come from a map on the volume. The nosuid flag is read by fstatfs on the name held, never by a path, and the refusal is proven with no root, on a disk bound nosuid in a user and mount namespace, and with root, in a privileged container.

**write**, on Windows
* A write opens the handle it renames by, and compares it with the file written, while it still holds that file, and keeps it open to the rename, so no other file can take the written file's record between the last check and the rename; it opened the handle after letting the file go.

Not yet proven natively: `trust/trusttest` as root on Linux; on Windows, the `/setowner` step and `LinkFile` by FSCTL, which need an elevated account, and `LinkFile` with Developer Mode switched, and `Collide` on a case-sensitive folder, which need an admin to set them up.

### API

Against v0.4.0, API.txt gains 30 lines and loses one, every new line marked `experimental`. The one it loses is `func appdirs.Temp(app string, apart ...string) (string, error)`, whose signature is now `func appdirs.Temp(app string, who ...trust.Who) (string, error)`, as Breaking says; no name is moved, and none is kept as an alias. The 29 new names:
* **appdirs**: `func Here(app string, layout Layout) (Folders, error)`, `func System(goos, app string, env func(string) string) (string, error)`, `func TempOverflow(app string, who ...trust.Who) (string, error)`.
* **confine**: `func Below(folder, path string) (string, error)`, `func Collide(root *os.Root, a, b string) (bool, error)`, `func FileURL(goos, abs string) (string, error)`, `func FromFileURL(goos, uri string) (string, error)`, `func LinkFile(root *os.Root, target, at string) error`, `func Portable(name string) (key string, err error)`, `func Resolve(path string) (string, error)`, `var ErrLoop`, `var ErrNoFileLink`.
* **trust**: `func Held(f *os.File, last Kind, who Who) error`, `func Interpreters(path string) ([]string, error)`, `method Fix.Commands() (steps []Step, ok bool)`, `type Step struct` with `field Step.Admin bool` and `field Step.Argv []string`, `field Who.Mounts []string`.
* **trust/trusttest**: `type Who int`, `const GroupReads Who`, `const GroupWrites`, `const OthersRead`, `const OthersWrite`, `func Deny(t testing.TB, path string)`, `func Loosen(t testing.TB, path string, who Who)`, `func Perm(t testing.TB, path string, want fs.FileMode)`.
* **write**: `func Swap(file, fresh, aside string, opts ...Option) error`, `var ErrNotSwapped`.

### Size

Non-test Go lines outside internal/ but internal/winapi, v0.4.0 to v0.5.0, as `task size` counts them: appdirs 487 to 655; confine 1,687 to 2,728; lock 529, unchanged; nowait 44, unchanged; trust, trust/trusttest among it, 2,319 to 3,345; write 1,332 to 1,683; and internal/winapi 494, unchanged. 6,892 to 9,478 in all, 2,586 more, at the ceiling of 9,478 with no headroom. treesum: 462, unchanged.

go-files v0.5.0 requires golang.org/x/sys v0.50.0, unchanged since v0.4.0, and golang.org/x/text v0.44.0, new. Its youngest requirement, golang.org/x/text v0.44.0, published 2026-11-09, is 11 days old on 2026-11-20.

## v0.4.0 (2026-11-19)

v0.4.0 adds two names, `nowait.Open`, in a package of its own, and `trust.AdminGroup`, and changes no signature. On Windows, `write` and `confine.Mkdir` now take modes and owners they refused before, where the owner-only access list gives no more than they ask. Nothing changes on Unix but the two new names. No change is needed to keep a program that builds against v0.3.0 working.

### Added

**nowait**, a new package
* `Open(name, flag, perm)` opens a clean absolute path outside any root as `os.OpenFile` does, and never waits for a pipe's other end, one call on every system: on Unix it opens with `O_NONBLOCK`, so a pipe a user names opens at once to be read, or is refused at once (`ENXIO`) to be written with no reader, and a program needs no file of its own per system for it. It adds nothing else: it follows a link at any component, as `os.OpenFile` does, and checks nothing of what it opened, so a program that must follow no link opens through `confine` instead. A path that is not absolute, or not clean, is refused, matching `fs.ErrInvalid`.

**trust**
* `AdminGroup()` is the system's admin group: on macOS the group named `admin` (gid 80 as a Mac ships), read by `os/user` at each call, with cgo or without it, and on every other system none, `ok` false. A lookup that fails, or a gid that is not a decimal 32-bit number, is an error, never a guess. It trusts nothing: a program that lets the admin group write its folders passes it to `Who.Group` itself, as `func(g uint32) bool { return ok && g == gid }`.

### Changed

**write** and **confine**, on Windows
* Windows takes write's `Mode`, `KeepMode`, `KeepOwner` and `Owner(-1, -1)`, and `confine.Mkdir`'s perms beyond 0o700, where the owner-only access list gives no more than they ask. Before, each was refused there.
  * A file written with `Mode` keeps the owner-only list, narrower than the mode, and a mode without owner write makes it read-only, as `os.Chmod` does. A mode without owner read, or with setuid, setgid or sticky, is refused with `ErrNoMode`.
  * A `NoReplace` write whose mode has no owner write leaves the placed name read-only: it reads the attribute once the file is in place, and sets it again where the remove of the temp name took it off. Where it cannot be read or set, or another file has taken the placed name, the write answers an error, never success over a writable file, and the other file is left as it is.
  * `KeepMode` keeps the old file's read-only attribute, and is refused with `ErrNoMode` over a file whose list grants any account but the writer's, SYSTEM and Administrators, and over a name that is neither a plain file nor a link, as a junction or a reparse point of a driver's own tag, a cloud-files placeholder that shows its tag among them.
  * `KeepOwner` is taken over a file the writer's account owns, and refused with `ErrNoOwner` over another's, Administrators' among them, and over a name that is neither a plain file nor a link, as a junction or a reparse point of a driver's own tag, a cloud-files placeholder that shows its tag among them. `Owner` with any ids but exactly -1 and -1, a negative one included, is still refused with `ErrNoOwner`. `Keep` refuses `KeepMode` and `KeepOwner` as before.
  * `Keep`'s copy keeps the read-only attribute of the file it copies, and `PutBack` brings kept's back, with a `Mode` given or not, as Unix gives both their source's mode.
  * `Mkdir` takes a perm with owner read, write and search, and makes the owner-only, protected folder; one without, or with a bit beyond the nine, is refused with `ErrNoMode`. A folder it made that has another owner or list when opened, swapped in at the name, is refused with `ErrOutside`, and nothing is removed.

### API

Against v0.3.0, API.txt gains two lines and loses none: `func nowait.Open(name string, flag int, perm fs.FileMode) (*os.File, error)` and `func trust.AdminGroup() (gid uint32, ok bool, err error)`, both marked `experimental`. No signature changes, no name is moved, and none is kept as an alias.

### Size

Non-test Go lines outside internal/ but internal/winapi, v0.3.0 to v0.4.0, as `task size` counts them: appdirs 487, unchanged; confine 1,654 to 1,687; lock 529, unchanged; nowait 44, new; trust 2,275 to 2,319; write 1,128 to 1,332; and internal/winapi 370 to 494. 6,443 to 6,892 in all, the ceiling with no headroom. treesum: 462, unchanged.

Most of the growth is on Windows: in write, the read-only attribute and the checks over the old file; in internal/winapi, the reads of a file's owner and access list that `KeepOwner`, `KeepMode` and `Mkdir` share.

go-files v0.4.0 requires golang.org/x/sys v0.50.0 and no other module.

## v0.3.0 (2026-11-18)

v0.3.0 removes one exported name, `appdirs.Option`, and `appdirs.Native` is now a constant, not a function. Four signatures change: `trust.ReadOwned`'s, which still takes the calls it took, `appdirs.For`'s, and `lock.Take`'s and `lock.Wait`'s. Most of the release is two things. `trust` on Linux now needs Linux 5.8 or later, judges a file system by its type, and refuses one whose owners it cannot judge. On Windows, what `write`, `lock`, `confine` and `appdirs` make now has an access list only its owner and SYSTEM reach. Each break below says the change to make, or, where there is none, that there is none but to accept it.

### Breaking

**trust**
* **Linux 5.8 or later.** On Linux, trust needs 5.8 or later, and below it fails closed: with no `STATX_MNT_ID` it cannot read a mount's type, so every name on tmpfs, ramfs, overlayfs, FUSE, NFS, 9p, btrfs, ZFS or a type `Who.FileSystems` names is refused, and only ext4, xfs and f2fs keep trust. An older kernel loses `$XDG_RUNTIME_DIR` and `/dev/shm`, and a btrfs or ZFS root. The same holds on any kernel where the process is not in `/proc`'s pid namespace, so `/proc/self` names nothing, as after `nsenter --mount` into a container but not its pid namespace (not under `unshare --pid` without `--mount-proc`, whose `/proc` still holds the process), and where a seccomp filter refuses `openat2` with ENOSYS, as `/proc/self/mountinfo` cannot then be held to the kernel's procfs. Where one refuses `openat2` with EPERM, as older container runtimes' default profiles did, every name on those types is an error instead, the container's own overlayfs root among them. Run trust in the pid namespace whose `/proc` is mounted, and let `openat2` through the seccomp filter, as newer default profiles do: there is no other way to keep trust on those types.
* `ReadOwned(path, owner, private, limit, mounts ...Who)` takes optional `Who` values, of which only `FileSystems` is read. A call with four arguments compiles as before; a variable of the old function type does not take `ReadOwned` until its type gains `...Who`.
* On Linux, a name on a file system whose owners trust does not judge is refused with the new `ErrMount`, which matches `ErrNotOwners`. Judged are ext2/3/4, xfs, btrfs, f2fs, ZFS, tmpfs, ramfs, overlayfs, FUSE and NFS. Refused are CIFS, 9p, vfat, exfat, squashfs (so a snap's files), erofs and every other type, until the new `Who.FileSystems` names it by its `statfs` type. A type of 0 is refused. Keep the files on a judged type, or name the type in `Who.FileSystems` only if you trust whoever serves that file system.
  * ext2/3/4, xfs and f2fs pass on a device whose major number is not 0. On major 0, and for every other type, trust reads the mount's own type from `/proc/self/mountinfo`, found by the mount's `statx` number, and passes only that type. So a 9p share of a host's ext4, tmpfs or overlayfs is refused as 9p, whatever type its server answers, until `Who.FileSystems` names 9p's own type, `0x01021997`.
  * On ZFS, a socket, fifo or device, such as a program's socket under a home folder on ZFS, is an error, its NFSv4 list unread. A socket in `$XDG_RUNTIME_DIR`, a tmpfs, is judged.
  * `ReadOwned` refuses all of these as `Path` does, unless a `Who` given to it names the type.
* On Linux, on an NFS mount that cannot show the server's access list, as NFSv3, a key, a `PrivateFolder` and `ReadOwned`'s private file are refused, matching `ErrOthersRead` and `errors.ErrUnsupported`: the server judges access by a list this process cannot read. Other kinds pass on the mode as before. Keep keys on a local disk, or on a mount that shows its list.
* On Linux, on an NFS mount, each name is opened again to read its NFSv4 list, at three costs. A name there this account may not read, such as a folder of mode `--x`, and a device, fifo or socket there, are now errors and go unjudged, where their list was read before. Each name read costs the server one OPEN. A device that an owner of the mount namespace binds over the name's path is opened, without blocking, before it is refused. To have such a name judged, give this account read on it, and keep a device, fifo or socket off NFS. The OPEN per name and the open of a bound device have no change to make but to accept them.
* On Linux, a file under `/proc` larger than 4 MiB is refused, and a call that reads one is an error. There is no change to make but to accept it.

**write**
* `Append` takes no default name for a full log's move. Without the `Aside` option, or with `Aside("")`, it is refused with the new `ErrNoAside` before anything is opened, and no line is written. Before, a full log moved to `<name>.1`. Add `Aside(name + ".1")` to every `Append` to keep the old name.
* On Windows, `SyncFolder` flushes the folder: it opens the folder again by its own handle, to write, with backup semantics, and calls `FlushFileBuffers`. Before, it checked the folder and flushed nothing. A volume that will not flush a folder is now an error matching `errors.ErrUnsupported`. A folder this account may not write to, or that another process holds sharing no write, now gives that open's error, matching `fs.ErrPermission` or a sharing violation. None is answered nil. A program that flushes a folder it may only read takes the error, or keeps its change in a folder it may write.
* On Windows, `KeepOwner` is refused with `ErrNoOwner` and `KeepMode` with `ErrNoMode`, both matching `errors.ErrUnsupported`, as `Owner` and `Mode` are. This holds in `File`, `FileFrom`, `FileIn`, `Keep` and `PutBack`, with a file at the name or none, and nothing is written. Before, both did nothing there, and the new file took its folder's access list. A program that passes either on every system leaves it out on Windows.
* On Windows, a file `write` makes (`File`, `FileFrom`, `FileIn`, `PutBack`, `Keep`'s copy and `Append`'s new log) has an access list that only the making account and SYSTEM reach. The list is protected, so nothing of the folder above is inherited. Before, the file took its folder's list, which Administrators, Users or a program's own group may have been on. A file a write replaces in place gets that list too, as `KeepMode` is refused there, so the program must give its list after the write to a file that other accounts or services read.
* On Windows, where that list cannot be held, or would shut this account out of what it makes, a file to make is refused with the new `ErrNoAccessList`, which matches `errors.ErrUnsupported`, and nothing is made. These are a volume that keeps no access lists (FAT, exFAT), a share that will not take this account as the owner of what it makes (ERROR_INVALID_OWNER), a volume that will not say whether it keeps access lists, and a place where this account cannot open by name what it makes with that list: under a restricted token or an AppContainer the list does not name, or on a share that keeps the list and knows this account by another SID (see Security). Where the cause is a denial, as under a restricted token, the error matches `fs.ErrPermission` too, so a handler that tests `fs.ErrPermission` takes its permission branch: test `ErrNoAccessList` first. Every write is refused there, a replace of a file already there included, as each write makes a new file first. A log already there is written as before until `Append`'s first move: the new log it then makes is refused, so `Append` starts failing at its first move, not at its first call. No option turns `ErrNoAccessList` off: keep the files on a volume that keeps access lists (NTFS, ReFS), run with a token and in a share that can open what this account makes, or handle `ErrNoAccessList`.
* On Windows, `Append` refuses a last name ending in a dot or a space with an error matching `fs.ErrInvalid` before it opens anything, so a log at the stripped name is left as it was. Before, it made the name with the end stripped, as the Win32 rules read it. Name the log with no dot or space at its end.

**lock**
* The note's path is the caller's, given whole as the new `note` argument: `Take(path, note string)` and `Wait(ctx, path, note string)`. `Holder` reads the note at the path it is handed, no longer `<path>.holder`, and `lock` names no note of its own. Pass `path + ".holder"` to `Take`, `Wait` and `Holder` to keep the old name.
  * With `note` `""` the lock has no note: `Take` and `Wait` remove none, a declined run is told `Held{}`, and `Note` is refused with the new `ErrNoNote`.
  * A note path that cleans to the lock's is refused. One that is the lock file by another case or a second name is left alone by `Take` and `Wait`, which win the lock, and is refused by `Note` and `Release`.
  * On Unix, a note takes the lock file's owner only in the lock file's own folder. In any other folder it keeps the lock file's mode and is owned by the process that writes it.
* On Windows, a lock file `Take` or `Wait` makes has the owner-only access list that `write`'s files have. Where `write` refuses with `ErrNoAccessList`, a lock file to make is refused with `lock`'s new `ErrNoAccessList` and nothing is made. A lock file already there is taken as before. Keep the lock file on a volume that keeps access lists (NTFS, ReFS), or handle `ErrNoAccessList`. A last name ending in a dot or a space is refused with an error matching `fs.ErrInvalid`. Before, the name was made with the end stripped. Name the lock file with no dot or space at its end.

**confine**
* On Windows, `Mkdir` refuses any `perm` but 0o700 with the new `ErrNoMode`, which matches `errors.ErrUnsupported` as `write.ErrNoMode` does, before anything is made. Before, it applied no perm there. Pass 0o700 on Windows. Each folder it makes has the owner-only access list, and what is made in the folder inherits it. Where `write` refuses with `ErrNoAccessList`, `Mkdir` is refused with `confine`'s new `ErrNoAccessList` at the first folder to make. Keep the folders on a volume that keeps access lists (NTFS, ReFS), or handle `ErrNoAccessList`. A last name ending in a dot or a space is refused with an error matching `fs.ErrInvalid`. Before, the name was made with the end stripped. Name the folder with no dot or space at its end. Linux, macOS and FreeBSD are unchanged.

**appdirs**
* `For`'s call is now `For(goos, app, home, env, layout)`, and the macOS layout has no default. `layout` is `appdirs.XDG` or `appdirs.Native`, constants of the new type `Layout`, which replaces the `Option` type and the `Native()` function. A call that names neither, the zero `Layout` among them, is refused with the new `ErrLayout` on every system, so a program that chose no layout finds out on the system it is tested on. Write `For(goos, app, home, env, appdirs.XDG)` for a call that passed no option, and `appdirs.Native` for one that passed `appdirs.Native()`.
* In the XDG layout, where `XDG_RUNTIME_DIR` is unset, `Run` is `""` and `For` gives the other four folders with an error matching the new `ErrRun`. Before, `Run` was put in `State`, whose files outlive the session. A caller checks the error before it uses `Run`, and one that keeps no socket goes on past `ErrRun`.
* `Temp` keeps v0.2.0's signature and still judges its folder with `trust`, so on Linux it takes the rules above with no `Who.FileSystems` to pass. A temp folder on a type trust refuses (CIFS, 9p, vfat and the rest), or, below Linux 5.8, on tmpfs, overlayfs, btrfs or ZFS, is refused. Set `TMPDIR` to a folder on a type trust judges.
* On Windows, `Temp`'s folder has the owner-only access list. Where `write` refuses with `ErrNoAccessList`, `Temp`'s folder to make is refused with `appdirs`' new `ErrNoAccessList` and nothing is made. Set `TMP` to a folder on a volume that keeps access lists (NTFS, ReFS), or handle `ErrNoAccessList`.

### Changed

**lock**
* The lock is `flock(2)`, or `LockFileEx` on Windows, on the lock file `Take` opened and checked, so the file is opened once, and go-files no longer requires `github.com/gofrs/flock`. Once it holds the lock, `Take` reads the name again and refuses the lock unless the name is still that file, so a file, link or pipe put there after the check is still never locked. `Lock.File` still refuses with `ErrNoInherit` on every system, and `lock` still doesn't build on aix or solaris.

### Fixed

**lock**
* A `Lock` the program keeps no reference to is held until `Release` or the process exits, as `Lock`'s documentation says. Before, the garbage collector could close its file and drop the lock while the program ran, so a second run could take it.

### Security

**write, lock, confine and appdirs on Windows**
* The private defaults are private. A file or folder that `write`, `lock`, `appdirs.Temp` and `confine.Mkdir` make with no mode asked is made with an owner-only access list in the same call that makes it. So it never has its folder's list, even for a moment. Before, a key written 0600 or a temp folder made 0700 was readable by every account its folder let in.
* A make this account could not open again by name is refused, not left. Each file or folder made with the owner-only list is first made on a throwaway name in the same folder and removed. Where this account cannot then open it by name to read and write, as under a restricted token or an AppContainer the list does not name, or on a share that keeps the list and knows this account by another SID, the make is refused with the package's `ErrNoAccessList` and nothing is made. The denial is in the error, which matches `fs.ErrPermission` too, so test `ErrNoAccessList` first. Before, such a file was made and left, and the program's next open of it was denied. The try is made only where the name is not there already, so a `Take` of a lock file there, or a `Mkdir` over folders there, makes nothing, on a read-only or full volume too. A process killed in that instant leaves the throwaway name, `.probe-` and random letters, empty, for the caller to remove, and so does a removal of it that fails: the error names its path, and is `ErrNoAccessList` only where the throwaway could not be opened by name either. A file make's handle is the create's own, with no right to delete, so a lock's contender opening a new lock file is not refused for sharing.

**write**
* A temp name removed and made again between the temp file's close and the last check before the rename is refused. The write holds a descriptor of its temp file, opened before the one that wrote it is closed, until that last check, so a file made at the name cannot take the freed inode number, which Linux gives to the next file made. Before, anyone who could write the folder could have their own file put in place, unflushed, between the close and the rename. `Check` runs on that held descriptor. Between the last check and the rename, which goes by the name, a file put at the temp name is still put in place, as before.

**trust**
* Right in root's sticky folder, such as `/tmp`, another account's folder is refused with no `Fix`, since making it root's would leave it refused as root's. When this account is root, its own folder there passes. The sticky folder's rules now come before the owner's.
* On Linux, a name on an NFSv4 mount is judged by the server's access list, `system.nfs4_acl`, by the same rule as a macOS access list, in NFSv4's rights.
  * An allow entry giving another account a right that changes the name is refused with `ErrOthersWrite`.
  * On a key, `ReadOwned`'s private file or a `PrivateFolder`, an entry giving another account READ_DATA or EXECUTE, which a server reads a file for, is refused with `ErrOthersRead`. A mode of 0700 did not show it.
  * `OWNER@` is the owner and `GROUP@` the group; a named principal is never trusted. A `GROUP@` or `EVERYONE@` entry within WRITE_DATA, APPEND_DATA and DELETE_CHILD, as the Linux NFS server derives from the mode, is judged as the mode's bit for its class is. An entry of a type trust does not read, or a list that does not parse, is refused.
  * The list is read on the file held open, never by its path under `/proc/self/fd`, which an owner of the mount namespace could mount over to answer a clean list, or none. The name is opened again through `/proc/self/fd`, and that descriptor is held to the open one by mount number, device and inode before the list is read. Another file there, or any error before the read, leaves the name unjudged; it is never read as having no list.
* On Linux, a FUSE mount's `user_id=` is read only where this process's user namespace is the initial one and is the owner of its mount namespace, read by `NS_GET_USERNS`. Elsewhere every FUSE name is refused, as of a mounter not named. `user_id=` is numbered in the mounter's namespace: before, a reader in a user namespace of its own took a host account's `allow_other` mount as its own or root's, and host root that joined a rootless container's mount namespace read its fuse-overlayfs as root's. A container with no user namespace of its own reads as the host. `/proc` is trusted as the kernel's, and a loop or udisks mount is judged by what its files report.
* On Linux, what trust reads under `/proc` is held to the kernel's procfs, since an owner of a joined mount namespace may mount over `/proc`, or bind another procfs file there, such as a process's own `cmdline` over `/proc/self/mountinfo`.
  * `/proc/self/mountinfo` is opened by `openat2` from `/proc`, held to a procfs root by `fstatfs` and inode 1, crossing no mount and following no magic link. A mountinfo not so held names no mounter, so a name on a FUSE mount is refused with `ErrNotOwners`.
  * `Who.Overflow`'s `uid_map` and overflow account are read only from a file `fstatfs` shows is on procfs. Each file is read up to 4 MiB.
  * The user and mount namespaces come from a pidfd of this process by Linux 6.11's namespace calls, and on an older kernel from `/proc/self/ns`. Each is held to nsfs, and to its type by `NS_GET_NSTYPE`, before `NS_GET_USERNS` is asked of it.
* On Linux, a name on a mount whose server or image reports any owner, as CIFS, 9p, vfat or squashfs, no longer passes on the owners it reports (see Breaking). A 9p share of a host's disk, tmpfs or overlayfs, whose server answers `fstatfs` with the host's type, is refused as 9p is: by its device's major number for a disk, and by the type mountinfo gives its mount for any other. A name on ZFS carrying `system.nfs4_acl_xdr`, the NFSv4 list TrueNAS SCALE enforces and trust does not read, is refused with `ErrMount`. The type and that list are read on the name held, never by a path.
* Not yet judged: an account's ext4 stick that udisks mounted stays trusted on the owners it holds, as in v0.2.0, and so, from Linux 5.8, does a stick udisks mounts through root's FUSE helper, ntfs-3g or exfat-fuse, on the owners its volume gives, until v0.5.0.

### API

Against v0.2.0, gorelease reports the same on Linux, macOS and Windows. Its incompatible changes are each in Breaking above:
* `appdirs.For`'s signature, `appdirs.Native` changed from a function to a constant, and `appdirs.Option` removed;
* `lock.Take`'s and `lock.Wait`'s signatures;
* `trust.ReadOwned`'s signature.

Its additions are `appdirs.ErrLayout`, `ErrNoAccessList`, `ErrRun`, `Layout` and `XDG`; `confine.ErrNoAccessList` and `ErrNoMode`; `lock.ErrNoAccessList` and `ErrNoNote`; `trust.ErrMount` and `Who.FileSystems`; and `write.ErrNoAccessList` and `ErrNoAside`. No name is moved, and none is kept as an alias.

### Size

Non-test Go lines outside internal/ but internal/winapi, v0.2.0 to v0.3.0: write 1,089 to 1,128, lock 440 to 529, confine 1,633 to 1,654, trust 1,649 to 2,275, appdirs 451 to 487, and internal/winapi, new, 370; 5,262 to 6,443 in all. treesum: 462, unchanged. Most of the growth is trust's Linux checks of mounts, procfs and NFSv4 lists, and all of trust's 626 added lines are mechanism those checks need. The rest is Windows: the owner-only access list, made by one helper in internal/winapi with its try that this account can open by name what it makes, and the folder flush. It also counts the caller's note and log names, the layout `appdirs` now requires, and the Windows calls write, lock and confine now share in internal/winapi.

go-files v0.3.0 requires golang.org/x/sys v0.50.0 and no other module.

## v0.2.0 (2026-11-18)

v0.2.0 removes no exported name. Four signatures change, and several defaults that were the library's choice are now the program's. Each break below says the change to make.

### Breaking

**write**
* `Append(ctx, root, name, line, limit, opts ...LogOption)` takes the caller's context first, and options of their own type, `LogOption`.
  * It waits on another process's hold on the log until the context ends, not a fixed five seconds. The refusal now matches `context.Canceled` or `context.DeadlineExceeded`, no longer `os.ErrDeadlineExceeded`.
  * With no deadline it waits as long as the hold is kept. The hold is always tried once, so an ended context still writes to a log no other process holds.
  * `LogMode(perm)` sets the log's mode exactly, whatever the umask, at first and after each move. It is refused on Windows with `ErrNoMode`. A mode without owner read and write is refused.
  * `Aside(name)` names where a full log moves, in place of `<name>.1`. An `Aside` that is the log's own name, or one the volume reads as it, is refused.
* `Keep(file, folder, name string, opts ...Option)` makes the copy at the caller's name, in place of `now time.Time` and a timestamped name.
  * A taken name, a link among them, is refused with an error matching `fs.ErrExist`, and nothing is written.
  * A name that isn't one plain name in the folder is refused before anything is opened. That covers empty, `.`, `..`, a separator or colon, a trailing dot or space, and a name the system keeps.
* On Windows, an explicit `Mode` is refused with `ErrNoMode`, matching `errors.ErrUnsupported`, in `File`, `FileIn`, `FileFrom`, `Keep` and `PutBack`, and nothing is written. It was never applied there. Drop it: a new file takes its folder's access list. `PutBack`'s copy mode is not refused.
* On Windows, `File`, `FileIn`, `FileFrom` and `PutBack` refuse a name that isn't clean (a trailing or doubled separator, `.` or `..`), or that has a component ending in a dot or space, before anything opens, with an error matching `fs.ErrInvalid`. A device name such as `NUL` is refused with or without `NoSync`.
* `Check` gets a read-only descriptor of its own, opened by name after the written one is closed, in place of the written descriptor. So a `Check` that wrote to the temp file now gets an error, and a `Mode` or `KeepMode` that gives the owner no read is refused with EACCES before `Check` runs, with nothing put in place. With `Check`, give the owner read. A `Check` may run the file it checks.

**lock**
* `Lock.File` returns `(*os.File, error)`, and refuses on every system with `ErrNoInherit`, matching `errors.ErrUnsupported`. The lock is on a descriptor of gofrs/flock's own, so a child given the lock file wouldn't hold the lock. `Lock.Stat` is new for the file's information.
* The Unix lock is now taken through gofrs/flock, as on Windows. `Take` refuses the lock unless gofrs/flock's open is the lock file it opened and checked, so a file, link or pipe put at the name between the two is never locked. `lock` doesn't build on aix or solaris.
* `Take` and `Wait` write no `<name>.holder` note. One is written only when the holder calls `Note`. Until then a second `Take` is declined with `Held{}` ("another process holds the lock"), not the holder's details. Call `Note(nil)` after `Take` to have refused runs told who holds it. A note an earlier holder left is removed once the lock is won; a link there is removed as the link.

**trust**
* `Unsafe.Fix` is a `Fix` value, not a command line: `Path`, `Clear` and `Set` (mode bits), `Owner`, `Revoke` (a SID whose allow entries come off), and `Reset`. The zero `Fix` is none. Format it yourself; nothing in trust quotes a path for a shell.

**treesum**
* Digests start `t2:` and no longer cover the executable bit, so one tree has one digest on Linux, macOS and Windows. `Check` refuses a digest of any other format, v0.1's `t1:` included, with the new `ErrVersion`, before it reads the tree. A digest with no format prefix is refused with `ErrDiffers`. Check an old pin with the release that made it, or fetch the tree again, then `Sum` it anew. Set a run file's mode yourself, and choose what to run by its path, never its mode.

### Security

**trust**
* On macOS, `Path` and `ReadOwned` read each name's access list. They refuse an allow entry giving any account or group but root and this account (the file's owner, for `ReadOwned`) a right that changes the name, or that lets another read a key or a private file, with `Fix.Revoke` naming the entry. A list that can't be read is refused, matching `errors.ErrUnsupported`. An entry neither allow nor deny is refused with `ErrOthersWrite`.
  * The list is held to the file by device and inode. A name replaced between the two reads is read again, up to eight times. Then it is refused with the new `ErrReplaced`, which a later call may pass.
  * This account is trusted only by its kernel id, so an entry naming it by name is refused.
  * On FreeBSD, NetBSD and every Unix but Linux and macOS, whose lists aren't read, every name is refused.
* On macOS, a name on a volume that ignores ownership, as an external disk usually does, is refused with the new `ErrOwnersIgnored`, which matches `ErrNotOwners`.
* A name on a volume mounted by an account not trusted is refused with `ErrNotOwners`, wherever it is on the way. On Linux that is a FUSE file system's `user_id=`; on macOS, every volume's `f_owner`. `ReadOwned` trusts root and the owner it is asked for.
* A folder right in root's sticky folder, such as `/tmp`, on another device than that folder, is refused with `ErrNotOwners`, whoever it shows as owner. A bind mount of another volume, or a btrfs subvolume, there is refused too.
* When this account is root, root's own folder right in root's sticky folder passes as its own, on that folder's device only. For every other account, a folder of root's there is still refused.
* New kind `PrivateFolder`: a folder only its owner may read or write, judged as a `Key` is.
  * A mode or access-list entry that lets another account read it is refused with `ErrOthersRead`. That covers a Linux list's mask, macOS and Windows lists, and on macOS an entry giving search.
  * Its inherit-only entries are judged as its own, on Windows and macOS, all but CREATOR OWNER's.

**appdirs**
* `Temp` judges its folder as a `PrivateFolder`, before the make and after it. A temp folder made earlier that its group or others may read is refused, and its mode is left alone. Run as root with `/tmp`, it answers root's folder there.

**confine**
* `Open` never waits on a pipe. On Unix it opens with `O_NONBLOCK`, so a pipe at a name opens at once to be read, or is refused at once (ENXIO) for writing with no reader.

### Added

**confine**
* `Mkdir(root, name, perm)` makes each missing folder of a name beneath a root, with no link followed at any component, and answers the last as a root of its own.
  * A link or non-folder on the way is refused with `ErrOutside`. A folder swapped before its mode is set is refused, and its mode is left alone.
  * On Unix, `perm` is set exactly, whatever the umask, and the folder above is flushed. A `perm` beyond nine bits, or without this account's read and search, is refused with `fs.ErrInvalid`. On Windows `perm` is not applied.
* `LinkFolder(root, target, at)` makes `at` a link to the folder `target`: a symbolic link on Linux, macOS and FreeBSD, and a junction, which needs no privilege, on Windows.
  * It refuses an `at` that isn't a clean name below the root, or that passes a link on the way (`ErrOutside`), and a name already there (`fs.ErrExist`).
  * It refuses, with an `*os.LinkError` matching `fs.ErrInvalid`, a `target` that isn't a clean absolute path to a folder. That covers, on Windows, a letter not of a fixed or removable disk (a share, a mapped or `subst` letter among them), a letter defined only in one logon session, not mounted by the mount manager, or that `QueryDosDevice` fails on, a component ending in a dot or space, and mount data over 16 KiB. A `target` where nothing is gives the stat's own error in the `*os.LinkError`, such as `fs.ErrNotExist`.
  * On NetBSD and other systems it returns `errors.ErrUnsupported`. A refusal makes nothing.

**write**
* `Runnable()` writes a file that is run soon after.
  * On Unix the write holds `syscall.ForkLock`'s read side, so no child of this process holds the write descriptor (Linux's "text file busy").
  * The file is 0700 unless `Mode` or `KeepMode` says otherwise.
  * Under `Runnable`, `FileFrom` reads its reader whole before taking the lock; bound a large one with `io.LimitReader`.
* `KeepOwner()` gives the new file the owner and group of the file it replaces. On Unix, one this account may not give is refused, matching `fs.ErrPermission`. On Windows it does nothing.
* `SyncFolder(root, dir)` flushes a folder beneath a root, refusing a missing name, a non-folder, a link out of the root, or a pipe. On Windows it checks the folder and flushes nothing.
* On Windows, a write that isn't `NoSync` renames its file written through to disk. A volume that refuses that gets a plain rename and `ErrNotDurable`. NTFS and exFAT take it; SMB is not yet probed.

**trust**
* `Who.Apart` names folders a path may neither be nor lie below, compared by file ID, never by path. A match is refused with `ErrInside`, naming both paths. An empty entry, one not absolute and clean, or one that can't be read, is refused before any name is read.
* `OwnerOf(f)` is an open file's owner, read on its descriptor, as `uid:N` or a SID.
* `Unsafe.Path` is the name a refusal is about, as the walk reached it.

**appdirs**
* `Temp(app, apart...)` is the account's own temp folder for a program: `<temp>/<app>-<uid>` on Unix, `%TEMP%\<app>` on Windows. It is made 0700 when missing, and judged before and after the make, so a folder swapped for a link in between is refused. One that is, lies below, or holds a folder of `apart` is refused with `trust.ErrInside`. appdirs now imports trust.

**lock**
* `Lock.Stat` is the lock file's information, from the file `Take` opened and checked. After `Release` it is an error matching `fs.ErrClosed`.
* The README shows how to sweep the scratch folders stopped runs leave, safely, with no new function.

### Changed

**confine**
* On Windows, the refusal of a letter the mount manager doesn't list says so.

### Size

Non-test Go lines outside internal/, v0.1.2 to v0.2.0: write 689 to 1,089, lock 388 to 440, confine 1,028 to 1,633, trust 919 to 1,649, appdirs 236 to 451; 3,260 to 5,262 in all. treesum and apart: 450 to 462. The growth is the macOS access list, the Windows names and links, `Mkdir` and `LinkFolder`, `Runnable`, the new trust kinds, `Temp`, and the mount and device checks. An independent read found about 150 lines of it duplicated; v0.3.0 takes out what of that it can.

go-files v0.2.0 requires golang.org/x/sys v0.50.0 and github.com/gofrs/flock v0.13.2.

## v0.1.2 (2026-11-17)
### Added
- trust: `Who.Overflow` trusts the kernel's overflow account as root, and nothing more, for a test in a Linux user namespace that maps this account and not root, where every account the namespace does not map, root's `/` among them, shows as that one. `Path` refuses it with the new `ErrOverflow`, walking nothing: off Linux, test binary or not, where it matches `errors.ErrUnsupported` too; outside a test binary; and where `/proc/self/uid_map` cannot be read, maps root, maps none or maps the overflow id itself.
### Changed
- trust: on Linux, `Path`, `ReadOwned` and `Account` refuse with `ErrOverflow` a program that runs as the kernel's overflow account where `/proc/self/uid_map` is not the one line `0 0 4294967295`: in a user namespace that does not map it, the account is any the namespace leaves out, and every name it does not own shows as its own. In the first namespace, and in a container with no user namespace of its own, the overflow id is `nobody`, and a program run as `nobody` is served as at v0.1.0. In a namespace that maps part of the ids, as a rootless container's, a program run as `nobody` is newly refused.
- trust: on Linux, `Path`, `ReadOwned` and `Account` read the kernel's overflow account from `/proc/sys/kernel/overflowuid` before any name, and where it cannot be read they refuse every call with `ErrOverflow`, failing closed, as on a system with `/proc` not mounted.

The non-test Go lines, by the repo's own measure: 3260, and treesum 450 counted apart.

go-files v0.1.2 requires golang.org/x/sys v0.50.0 and github.com/gofrs/flock v0.13.2, as v0.1.1 does. A project that takes a module only once it is 14 days old can take it from 22 November 2026.


## v0.1.1 (2026-11-17)
### Added
- treesum: `Sum` gives a pinned digest of a folder tree beneath an os.Root, `t1:` and the SHA-256 of a listing of every entry by its slash path, and the listing itself; `Check` refuses a tree that is not the one a digest and its listing pin, naming the first entry that differs. Every entry is opened through confine and no link is followed: a link is hashed as its target's text, and one that leads out of the tree or loops is refused, as are a hard link, a device, a FIFO, a socket and a Windows junction. `Caps` bounds the entries and bytes a walk takes, and the steps its links take to resolve; a path of more than 4,095 bytes is refused with it, so the listing grows with the entries alone. The listing's format is in treesum/README.md, byte for byte.
### Changed
- go-files' own lint bans the modules weighed for a job and not taken (moby/sys/user, go-acl, securejoin, safeopen, continuity, go-filemutex, fslock, lockfile, renameio, atomicwriter, natefinch/atomic, safefile, tailscale atomicfile, xdg, configdir) in any file, gofrs/flock in lock/lock_windows.go alone, raw unix.Openat2 and unix.Renameat2 in confine's Linux files alone, and the kernel's lock calls (syscall.Flock, unix.Flock, windows.LockFileEx) in the system files of lock and of write's Append alone.

The non-test Go lines, by the repo's own measure: 3195, and treesum 450 counted apart.

go-files v0.1.1 requires golang.org/x/sys v0.50.0 and github.com/gofrs/flock v0.13.2, as v0.1.0 does. A project that takes a module only once it is 14 days old can take it from 22 November 2026.


## v0.1.0 (2026-11-16)
### Breaking
- lock: `Take` and `Wait` take no options.
### Added
- The first release of go-files: `write`, `lock`, `confine` and `trust`, taken from the module they were written in and cut to what os.Root does not already do, and `appdirs`, which is new. Each package's exported names are listed in API.txt, and a check fails when the source and API.txt differ.
- appdirs: `For` names the five folders a program keeps its files in, `Config`, `State`, `Cache`, `Run` and `Data`, on Linux, FreeBSD, macOS and Windows, from the system, the program's name, the home folder and a reader of the environment the program hands it, so it reads nothing itself. `Native` gives macOS's own layout under Library. `Cache` is never the same folder as `Config` or `Data`, nor one that holds them. `ErrApp`, `ErrHome` and `ErrSystem` refuse a name, a home or a system it cannot place. `Free` says how much room a folder has.
- write: `FileFrom` takes the content from an io.Reader. `Check` runs a function on the new file, read from its start, before it is put in place. `IfHolds` writes only over the file the program expects, and refuses with `ErrChanged` when it holds anything else, is gone or is not a plain file.
- write: `Append` adds a line to a log kept under a cap. At the cap the log moves to `<name>.1`, and a line is never cut. A link, or anything not a plain file, at the name or at `.1` is refused, and so is a log with a second name. The log is held against a second writer while it is checked, moved and written, for at most five seconds, then refused with os.ErrDeadlineExceeded.
- write: `ErrNoOwner`, for `Owner` on Windows, matching errors.ErrUnsupported; and `ErrNoHardLinks`, for `NoReplace` on a volume that keeps no hard links (FAT and exFAT on Windows), where nothing is written.
- lock: `Note` writes a note beside the lock, in `<name>.holder`, given the lock file's owner and mode, and `Holder` reads it back. The note is for a message only: a note that is missing, a link, not a plain file, cut short, over 64 KiB or naming no process reads as held by another process, never as an error. `File` gives the lock's open file.
- confine: `ErrNoExchange`, matching errors.ErrUnsupported, when the kernel or the file system cannot swap two names in one step.
- trust: `Path` checks a path from the top of its volume, following no link it has not judged, and answers it with no link left in it, or refuses it with an `*Unsafe` that says what was seen and the command that fixes it. The last name is a `File`, `Folder`, `Key`, `Program` or `Socket`. `Who.NoLinks` refuses a link anywhere on the path, and `Who.Group` lets a group the program names write a name. `ReadOwned` reads a whole file through one descriptor checked to be plain, owned and no larger than a limit. `Unsafe.Unwrap` gives the `Err` value a refusal matches, when there is one.
- trust: `Account` gives this account as `ReadOwned` takes an owner and the walk trusts it, `uid:N` on Unix and the SID on Windows, so a program reads its own file with no Windows code of its own. Where this account's SID cannot be read, it is the walk's own refusal.
### Changed
- write: every write is one recipe on os.Root: a hidden temporary file beside the name, made by an exclusive create at 0600, given its owner, content and mode on its own descriptor and flushed, checked to still be the file written, renamed over the name, and the folder flushed. A link at the name is replaced and its target is untouched. With `NoReplace`, a taken name gives an error matching fs.ErrExist. On Windows the folder is not flushed, `Mode` is not applied, and a read-only old file is made writable for the rename.
- lock: the lock is flock(2) on Linux, macOS, FreeBSD and the other Unix systems, and gofrs/flock on Windows, which is linked there alone. A link at the lock's name is refused and never followed, on Windows too. `Release` removes the note and drops the lock, even while a child forked on its way to exec still shares the descriptor.
- confine: `Open` opens in the kernel through golang.org/x/sys, never following a link: openat2 with RESOLVE_BENEATH and no links on Linux, O_NOFOLLOW_ANY on macOS, and elsewhere a walk by handle that checks each folder is the one it looked at. `RenameNoReplace` and `RenameExchange` use renameat2 on Linux and renameatx_np on macOS, through descriptors of the two folders. On Windows `RenameNoReplace` moves by handle, from folders it holds open with no share to delete, with NtSetInformationFile and ReplaceIfExists false, and refuses a name with a component that ends in a dot or a space; `RenameExchange` refuses there with `ErrNoExchange`. FreeBSD and the other systems refuse both, moving nothing.
- go-files takes golang.org/x/sys v0.50.0 and gofrs/flock v0.13.2, so a program that builds with it takes them as its least versions.
- trust: on Windows, `Path` checks a name by six rules of its owner and access list, where it refused every name before.
- trust: a link right in root's sticky folder, as `/tmp`, is refused whoever owns it, where a link of this account's passed before: any account can add a hard link to it there.
### Fixed
- write: on Windows, a rename over a file another process holds open for a moment, which Windows refuses as a sharing violation or as access denied, is tried again for about 600 ms. A denial from an access list looks the same, so it too reaches the caller about 600 ms late.
- write: `Append` counts the log's names again once it holds the log, so a link at the name after the look, swapped for a hard link to its target before the hold's check, is refused, and the line lands in no other file. On Unix the count comes from the look the check matched. On Windows the log is first opened again by its name, sharing no delete until the line is written or the log moved, and the count is taken through the handle after that: a remove of the name after that open is refused, and one before it is tried again.
### Removed
- write: `FollowLink`, `ErrOutside` and `ErrExists`, with no alias (a taken name under `NoReplace` matches fs.ErrExist), and the sweep of stale temporary files.
- lock: `Group`, `Mode`, `Option`, `TakeIn`, `WaitIn`, `ErrNotPermitted`, `ErrNotPlain`, `ErrNotTried` and `ErrOutside`, with no alias; the check that the holder's process is alive; and the making of the lock's folder.
- confine: `Create`, `List`, `Mkdir`, `Rmdir`, `Symlink`, `Unlink`, `OpenWith`, `NoMounts` and `Option`, with no alias. confine's README gives each one's stand-in on os.Root: `Create` is `Open` with O_CREATE|O_EXCL, `List` is `Open` and then ReadDir, and the rest are the root's own.
- trust: `Walk`, `Home`, `Stat`, `Ends`, `Read`, `ReadFile`, `RootOwned`, `AccountOf`, `InTheBlock`, `Opened`, `Quote`, the `Link` kind, the `Accounts`, `Facts`, `Failed`, `Look`, `Missing`, `Name`, `Subject` and `Want` types, `Who.Me`, `Who.Of` and `Unsafe.Owned`, with no alias, and the `trust/accounts` package. `Path` takes the place of `Walk` and `Home`, `ReadOwned` the place of `Read` and `ReadFile`, and `Who.Group` the place of a group reader.

The non-test Go lines, by the repo's own measure: 3195.

go-files v0.1.0 requires golang.org/x/sys v0.50.0 and github.com/gofrs/flock v0.13.2. A project that takes a module only once it is 14 days old can take this release from 22 November 2026 (x/sys from 20 November, flock from 22 November).

