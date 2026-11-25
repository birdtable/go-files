# splice

`splice` replaces what stands between two marker lines of a text with a block, and keeps both
marker lines and every other byte. It is a pure function over bytes: it reads and writes no file,
so the caller reads the file, splices it and writes it back, with [`write`](../write/README.md) or
as it likes. It imports the standard library alone, so a program built for wasip1 with the
standard library only can take it.

## Install

```sh
go get github.com/birdtable/go-files/splice@latest
```

The full API is on [pkg.go.dev](https://pkg.go.dev/github.com/birdtable/go-files/splice).

## How it works

It keeps a generated section between a pair of marker lines in a hand-written file, as many
generators do. The program configures the two markers and the block.

## What it holds

| call | what it does |
|---|---|
| `Splice(text, begin, end, block)` | Answers `text` with what stands between the begin line and the end line replaced by `block`. A line is a marker when it equals the marker once its `\n`, one trailing `\r` and its leading spaces and tabs are taken off. Every byte outside the markers is kept, and `text` is never changed. Each block line with something besides white space is written after the begin line's indent; a line of white space alone is written as it is. A `\n` is added after a last block line that has none, so the answer splices again. It refuses with `ErrNoMarker` when either marker has no line, `ErrMarkerTwice` when either has two or more, and `ErrMarkerOrder` when the end is not after the begin, so a begin equal to end is always refused. Each refusal is a plain value that names no file. |

## Per system

| call | Linux | macOS | Windows | FreeBSD and the others |
|---|---|---|---|---|
| `Splice` | the same on every system: it makes no system call | the same | the same | the same, wasip1 included |
