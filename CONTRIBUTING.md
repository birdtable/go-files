# Contributing

Issues and pull requests are welcome. For a security problem, please read [SECURITY.md](SECURITY.md)
first: those are reported privately, never in an issue.

## Issues

- **A bug:** the go-files version, the Go version, the system, and the shortest program that shows
  it. The [bug form](.github/ISSUE_TEMPLATE/bug.yml) asks for each.
- **A feature:** what your program is trying to do, not only the API you would like. The
  [feature form](.github/ISSUE_TEMPLATE/feature.yml) asks for it.

## Running the checks

You need Go and nothing else. Every tool is pinned in its own modfile under `tools/` and built by
`go tool` on first use, so your program never inherits the tools' dependencies.

    go tool -modfile=tools/task.mod task hooks       # once per clone: installs the pre-push hook
    go tool -modfile=tools/task.mod task pre-commit  # quick: format, vet, lint, the secret scan, the promise index
    go tool -modfile=tools/task.mod task pre-push    # everything a push runs, tests with -race included

A test that fails, a run of no test and a skipped test are all red.

## Pull requests

- Keep a pull request to one change, with a test that fails without it.
- A refusal, a guard or a promise gets a test of its own. A promise a program may build on gets its
  line in [api/contract.txt](api/contract.txt).
- A change a program can see gets a changelog entry:
  `go tool -modfile=tools/changie.mod changie new`.
- Every value a package uses comes from its caller, unless it is a fact of the system or of a
  format. Ask of each new constant, default or message: would a program with another job than yours
  ever want it different? If so, the caller sets it, and the default is a safe one that refuses.
  [internal/secondconsumer](internal/secondconsumer/doc.go) is a program unlike the usual ones, and
  its tests catch a package that decides for its caller.
- The pre-push hook must pass. Please don't push past it.

By contributing, you agree that your contribution is licensed under the
[Apache License 2.0](LICENSE).
