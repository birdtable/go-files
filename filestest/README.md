# filestest

Helpers for a program's tests that write a file, write a program the test then starts, take a
folder a trust walk accepts and copy a fixture tree, naming no mode and no temp path, and check a program's
names against every system's rules and the one list of hostile names, on every
system. Each is the library's own call under it, calls `t.Helper()`, and fails the test, saying the
path, when that call fails; none skips. No package of go-files imports it outside its tests.

## Install

```sh
go get github.com/birdtable/go-files/filestest@latest
```

The full API is on [pkg.go.dev](https://pkg.go.dev/github.com/birdtable/go-files/filestest).

## What it holds

| call | what it does |
|---|---|
| `WriteFile(t, path, content)` | `write.File(path, content)`: the library's private file, 0600 on Unix and the owner-only list on Windows. It makes no missing folder. |
| `WriteProgram(t, path, content)` | `write.File(path, content, write.Runnable())`: 0700 on Unix and the owner-only list on Windows, with the fork lock held from the create to the close, so the test starts it at once, with no "text file busy". It makes no missing folder. |
| `CopyTree(t, dst, src)` | `confine.CopyTree` from the root at `src` to the root at `dst`, a folder that is there, each path made absolute from the working folder first: each folder 0700 and each file 0600, or 0700 where the source is runnable, on Unix, the owner-only list on Windows, and each link that leads inside `src` made again to the same name in `dst`. |
| `Private(t)` | A new folder of this account's alone, which `trust.Path` judges a `trust.PrivateFolder` where on Unix it refuses `t.TempDir`'s (0777 less the umask). It is one folder `confine.MkdirFresh` makes in the account's own temp folder for the app `filestest`: `appdirs.TempOverflow`'s in a Linux user namespace that maps this account and not root, and `appdirs.Temp`'s where that is refused with `trust.ErrOverflow`. Its path holds no link and no test's name, and two calls, from parallel tests or two test binaries, never answer one folder. At the test's end it is removed with all it holds; a removal that fails fails the test. On Windows `t.TempDir`'s folder may pass the walk or not, as `TEMP` names a folder of the account's alone or one others share, so a test calls `Private` on every system. |
| `AllPortable(t, paths...)` | Each slash-separated path through `confine.Local`, and each of its names through `confine.Portable`, a name's refusal said before `Local`'s, so the answer is the same on every system; and every two paths whose leading names, spelled apart, have equal keys, which a folder that folds case or a normal form reads as one: `a/README` and `a/readme` as files, and `A/x` and `a/y` as the folders `A` and `a`. It fails the test once, naming each path refused with its rule and each pair that meets, a line each in sorted order, and says nothing when all pass. |
| `HostileNames()` | A fresh copy of the one list of names and paths a program must refuse, each with the rule that refuses it in the library's words (`HostileName{Path, Why}`): drive letters, backslashes, rooted and climbing paths, the device names Windows keeps, a trailing dot and space, 8.3 short names, `.git` in any case and disguise, the format and default-ignorable code points, the HFS+ ignorable ones among them, the characters that fold to several, and a file's fork on macOS; most rows are go-git's own tests of `.git` (Apache-2.0, in `NOTICE`). `confine`'s own tests read it: each is refused saying its `Why`, and every reason `Portable` and `Local` give is said by some entry's refusal. |

## Refusals

| refusal | how the test fails |
|---|---|
| `WriteFile` or `WriteProgram` to a path whose folder is missing, or any error of `write.File` | `Fatalf`, saying the path; no folder is made |
| `Private` with no account's own temp folder (`appdirs.Temp` refused) or no fresh folder in it (`confine.MkdirFresh` refused) | `Fatalf`, saying why |
| `Private`'s folder that cannot be removed at the test's end | `Errorf`, saying the path |
| `CopyTree` with any error of `confine.CopyTree`, a link that leads outside `src` or a name already in `dst` among them | `Fatalf`, saying both paths; what it made stays in `dst` |
| `AllPortable` with a path refused, or two paths that meet where a folder folds case or a normal form | `Errorf`, once, naming each such path with its rule and each such pair |

## Per system

| call | Linux | macOS | Windows | FreeBSD and the others |
|---|---|---|---|---|
| `WriteFile` | 0600 | the same | the owner-only list | 0600 |
| `WriteProgram` | 0700, under the fork lock | the same | the owner-only list | 0700, under the fork lock |
| `CopyTree` | folders 0700, files 0600 or 0700 by the exec bit, links made again | the same | folders the owner-only list, files their folder's, a link to a folder a junction | FreeBSD: as on Linux. NetBSD and the others: a tree with a link fails the test |
| `Private` | `<temp>/filestest-<uid>/t` and 16 hex digits, 0700; `TempOverflow`'s in a user namespace that maps this account and not root | `Temp`'s, 0700 | `<temp>\filestest\t` and 16 hex digits, the owner-only list | `Temp`'s, 0700 |
| `AllPortable`, `HostileNames` | the same answer on every system: the strictest of their rules, held on every host | the same | the same | the same |
