# nowait

`nowait` opens a file by a path a program is handed, as `os.OpenFile` does, and never waits for a
pipe's other end. That is all it adds. It follows a link at any component, the last included, as
`os.OpenFile` does, holds the path beneath no folder, and checks nothing of what it opened, so the
caller checks what it got. A program that must follow no link opens through
[`confine`](../confine/README.md) instead. It imports the standard library alone.

## Install

```sh
go get github.com/birdtable/go-files/nowait@latest
```

The full API is on [pkg.go.dev](https://pkg.go.dev/github.com/birdtable/go-files/nowait).

## How it works

Built on `os.OpenFile`, with `O_NONBLOCK` on Unix. The program configures the path, the flags and
the mode.

## What it holds

| call | what it does |
|---|---|
| `Open(name, flag, perm)` | Opens `name`, a clean absolute path, as `os.OpenFile` does, and never waits: on Unix it is made with `O_NONBLOCK`, so a pipe at `name` opens at once to be read, or is refused at once (`ENXIO`) to be written with no reader; a regular file or a folder reads and writes as without. One call on every system. It follows a link at any component and checks nothing of what it opened. A name that is not absolute, or not clean as `filepath.Clean` gives it, is refused with an `*fs.PathError` matching `fs.ErrInvalid`, and nothing is opened. |

## Per system

| call | Linux | macOS | Windows | FreeBSD and the others |
|---|---|---|---|---|
| `Open` | `open` with `O_NONBLOCK` | the same | `CreateFile`, as `os.OpenFile` makes it: an open there never waits for a pipe's other end | `open` with `O_NONBLOCK` on a Unix; on a system that is not Unix, `os.OpenFile` with no flag added |
