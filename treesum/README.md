# treesum

`treesum` gives a pinned digest of a folder tree beneath an `os.Root`, and checks a tree against
it, so a program can tell that a tree it is about to run from, such as a browser folder with its
links, is the tree it pinned. It follows no link, ever: a link is hashed as the text of its
target. It imports the standard library and `confine` alone.

## Install

```sh
go get github.com/birdtable/go-files/treesum@latest
```

The full API is on [pkg.go.dev](https://pkg.go.dev/github.com/birdtable/go-files/treesum).

## How it works

Built on SHA-256, a listing in the manner of `golang.org/x/mod/sumdb/dirhash`, and `confine`'s
open, which follows no link. It adds what dirhash leaves out: a link taken as its target's text,
never followed, and the refusal of what a pinned tree should not hold. The program configures the
root, the name, and the `Caps` on entries and bytes.

## What it holds

| call | what it does |
|---|---|
| `Sum(root, name, caps)` | Walks the folder `name` in `root` by handle and gives the digest and the listing it hashes. `name` is clean and local, as `confine.Local` gives it; `"."` is the root. |
| `Check(root, name, caps, digest, listing)` | Refuses the tree, matching `ErrDiffers`, unless `listing` hashes to `digest` and the tree is the tree `listing` lists. A tree that differs is refused as an `*fs.PathError` naming the first entry, in the listing's order, that differs. It refuses what `Sum` refuses, and a digest of another format than `t2:`, a `t1:` one among them, with `ErrVersion`, naming the format. |
| `Caps{Entries, Bytes, Skip}` | The most entries (folders, files and links, the top folder aside) and the most bytes of the files' content a walk takes. Both are the caller's; a tree above either, or a cap below zero, is refused with `ErrCap`. Resolving the links takes at most 1,024 steps for each entry `Entries` allows, a step being one component of a target, or one folder of the path it is read from; a tree whose links take more is refused with `ErrCap` too, so the walk's work is bounded by the caps however deep its links go. An entry whose path from the top is longer than 4,095 bytes, the longest path Linux opens by name, is refused with `ErrCap` as well: the listing then holds at most `Entries` lines of at most 4,095 bytes of path each, so it grows with `Entries` alone, and the walk holds at most 2,048 folders open at once, the depth a path that long allows. At its peak, as it opens the deepest to list it, the walk holds 2,051 descriptors on Linux and macOS, as measured, the count to set beside a process's limit on open files: one for the top, one for each of the 2,048 folders below it, and two for the listing, which `confine.Open` opens through a second descriptor on its folder. The caller's own `root` is not counted. `Skip`, when set, leaves names out of the walk, as [Leaving names out](#leaving-names-out) says. |
| `ErrCap`, `ErrKind`, `ErrDiffers`, `ErrVersion` | What a refusal of a cap, of an entry the digest does not take, of `Check`, and of a digest of another format match. |

The digest alone names no entry, so a program keeps the listing beside it, and `Check` takes both.

### The walk

Every folder is listed through `confine.Open` and its `ReadDir`, and every file is read through
`confine.Open`, so no link is followed on the way. Each entry is looked at with `Lstat` in the
folder opened before it. A folder is opened and checked to be the one looked at; a file is
opened, checked to still be a plain file with one name, and read. A file or a folder swapped for
a FIFO between the look and the open is refused, not waited on. The folder's check that it is the
one looked at goes by file ID, as `os.SameFile` compares it; a Windows folder whose 64-bit index
reads zero (a WebDAV or SMB drive) or all-ones (ReFS, with no 64-bit form of its 128-bit ID) gives
`os.SameFile` nothing to tell it apart by, so it is refused with `ErrNoFileIndex`, the one value
`write.ErrNoFileIndex` is, matching `errors.ErrUnsupported`, rather than its check trusted.

### What is refused

| entry | refused with |
|---|---|
| a link whose target is empty, absolute (`/x`, `//server/share`), names a volume (`C:/x`), or holds a backslash or a colon | `confine.ErrOutside` |
| a link whose target has a component `..namedfork`, in any case, a file's fork on macOS, as `confine` refuses it | `confine.ErrOutside` |
| a link whose target, read as text from the link's folder, reaches above the top (as Windows resolves it) | `confine.ErrOutside` |
| a link whose target, resolved from the link's folder through the tree's folders and other links, reaches above the top (as Linux and macOS resolve it) | `confine.ErrOutside` |
| a link whose target holds a `..` after a component that is a file or is missing | `confine.ErrOutside` |
| a loop of links, or a chain of more than 40 | `confine.ErrOutside` |
| a Windows junction or another reparse point that is not a symbolic link | `confine.ErrOutside` |
| a file with a second name, a hard link | `confine.ErrOutside` |
| a link or a FIFO swapped in for a file or a folder between its look and its open | `confine.ErrOutside`, or `ENOTDIR` for a folder |
| a device, a FIFO, a socket | `ErrKind` |
| a name, or a link's target, that is not UTF-8 text or holds a control character | `ErrKind` for a name, `confine.ErrOutside` for a target |
| a link whose target reaches a name `Caps.Skip` left out, or a missing name in a folder that left one out, or, once it left any name out, goes on below a missing name | `confine.ErrOutside` |
| more entries or bytes than `Caps` allows, links that take more steps to resolve than it allows, or a path longer than 4,095 bytes | `ErrCap` |
| to `Check`, a digest whose format, the text before its first colon, is not `t2:`, a `t1:` one among them, before the tree is read | `ErrVersion`, naming the format |

A refusal of an entry is an `*fs.PathError` naming it by its slash path from the top, so a caller
finds it with `errors.As`, never by the text. A link that stays inside is taken as it is, one that
points at a missing name included.

### Leaving names out

`Caps.Skip` is asked of each name a folder's listing holds, by its slash path from the top, as the
listing writes it, before anything about the name is looked at. It is asked in the order the
folder's listing gives the names, before they are sorted and before the checks of a name below, so
it may not count on any order. A name it answers true for is left
out: it is not in the listing, and it is never opened, looked at, counted against the caps or
refused, nor is anything below it. A program that pins a checkout leaves out `node_modules`, which
may hold hard-linked files and grow past any cap, by answering true for a path whose last
component is `node_modules`. `Skip` is given the path alone, not the entry: Go reads an entry's
kind, where the file system does not give it in the listing, and its `Info`, by the folder's path,
which the walk never does. A skipped name is still read from its folder's listing, so listing a
folder the walk enters takes time with every name it holds, skipped or not, and `Entries` bounds
the folders it enters.

The listing's lines keep their meaning: a tree's listing with names left out is the listing of the
same tree with those names gone, so its digest is `t2:` as before, and a `Sum` with no `Skip` gives
the digest it gave. `Check` is given the `Skip` the pin was made with. With another `Skip` it
refuses a tree whose skipped names the listing lists, or lists none of, but it says nothing of what
a skipped name holds: `Check` with a `Skip` passes a tree whatever is in the names it leaves out,
so a program runs nothing from them on the pin's word.

A link whose target reaches a name left out is refused, since the walk never looked at what it
reaches, which may be a link out of the tree. So is a link whose target reaches a missing name in a
folder that left a name out: the walk keeps no list of the names it skipped, so it cannot tell one
from a name that is not there, and refuses both. Once any name is left out, a link whose target
goes on below a missing name, as `b -> A/node_modules/x` beside a kept `a` that left out its
`node_modules`, is refused too, and so is one that reaches the missing name through another link,
as `c -> A` and `b -> c/node_modules/x`. A volume that ignores case or folds Unicode forms reads `A`
as `a`, so the link reaches what the walk left out, and `.` left nothing out to tell it by. A link
that ends at a missing name still stays inside, and with no `Skip` nothing of this changes.

The `..` after a file or a missing name is refused because a system may read that name otherwise:
on a volume that ignores case or folds Unicode forms, `A/..` may be a folder the walk does not
know, and its `..` may climb. A backslash and a colon are refused on every system because Windows
reads them as a separator and a volume, and `..namedfork` because macOS reads a file's fork
through it, which no line of the listing holds. A name or target with a control character is
refused because a tab or a newline would split a line of the listing.

## The format

The listing is one line per entry, every folder, file and link below the top, the top itself
left out. Each line is one of these, where `\t` is a tab (0x09) and `\n` a newline (0x0a):
    folder\t<path>\t\n
    file\t<path>\t<sha256>\n
    link\t<path>\t<target>\n

- `<path>` is the entry's path from the top, its components joined by `/`, with no `./` before it
  and no `/` after it.
- `<sha256>` is the SHA-256 of the file's content, 64 lowercase hexadecimal digits.
- `<target>` is the link's target as the system gives it, with each `\` turned to `/` on Windows.
- A folder's line ends in a tab before its newline: its third field is empty.

The lines are sorted by `<path>`, comparing the bytes. The digest is `t2:` followed by the
SHA-256 of the whole listing, in 64 lowercase hexadecimal digits. The `t2:` names this format, so
a digest is never taken for `go.sum`'s `h1:`, and `Check` refuses a digest of any other format,
the text before its first colon, with an error matching `ErrVersion` that names it, before it
reads the tree. A `t1:` digest, of go-files v0.1.1 and v0.1.2, listed whether each file was
executable. `ErrVersion` says nothing about the tree, since it comes before the tree is read: to
pin again a tree that an earlier release pinned, check it first with the release that pinned it,
or fetch it again from where the pin was made, and only then pin it with `Sum`. A `Sum` in
answer to `ErrVersion` alone pins whatever the tree now holds, a changed tree among them.

A tree of a folder `a` holding the file `f` (`hello` and a newline), an empty folder `e`, a link
`a/up` to `../e` and a link `l` to `a/f` lists as:

    folder\ta\t\n
    file\ta/f\t5891b5b522d5df086d0ff0b110fbd9d21bb4fc7163af34d08286a2e846f6be03\n
    link\ta/up\t../e\n
    folder\te\t\n
    link\tl\ta/f\n

and its digest is
`t2:44a046609b31eaa243ae812f399f36d333fce9440496e59182c7c4991e554b72`. A test holds this tree
and digest on Linux, macOS and Windows.

No mode bit is in the listing, the executable bits included, and no time, owner or access list.

## The same on every system

A tree gives the same digest on Linux, macOS and Windows. Go reports no executable bit on
Windows, so the listing holds none on any system: a file made executable, or no
longer, digests as it did, and a program that runs a file from the tree sets its mode itself. A
program chooses what it runs by the file's path in the listing, never by its mode: a file's owner,
or whoever unpacks the tree, can flip a bit and leave the content alone, so a runner that skips a
file that is not executable, or a folder put on `PATH`, would run one file more, or one fewer,
than at the pin. A test holds one digest for a file under three modes.

## A check is a look before a use

`Check` says what the tree was while it walked. Another process may change the tree after it
returns. A program that must hold the tree between the check and the use opens every file it
uses through `confine`, and keeps the tree in a folder no other account can change, which
`trust.Path` checks.

## Why not another library

- `golang.org/x/mod/sumdb/dirhash` opens each file by name, so it follows a link, and has no kind
  for a link.
- `codeclysm/extract` and `mholt/archives` unpack an archive; they give no digest of a tree.
- `hashicorp/go-getter` brings the AWS and Google Cloud SDKs with it.

The safe part of the walk is `confine`'s, so the digest sits beside it.
