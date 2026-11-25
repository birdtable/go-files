# Security

go-files exists to make file handling safe where another account may be working against your
program, so we take reports seriously and are glad to have them.

## Reporting a vulnerability

Please report it privately, through GitHub's private vulnerability reporting:
[open a report](https://github.com/birdtable/go-files/security/advisories/new). Never open a public
issue for a security problem.

A good report says:

- the go-files version and the Go version,
- the system and, where it matters, the file system,
- what another account or an untrusted path can do that the documentation says it cannot,
- the shortest program or steps that show it.

We will confirm the report, work on a fix with you, and credit you in the release notes if you
would like.

## Supported versions

go-files is at v0, and only the latest minor release gets security fixes. Upgrade to it before
reporting, if you can.
