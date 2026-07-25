---
type: concept
title: Versioning
description: "#1214 — the release version in internal/version, the build stamp injected by the Makefile through -ldflags, the --version flag short-circuiting cli.Parse, and what SemVer means for a binary whose public surface is the config format, keymap and plugin ABI."
resource: internal/version
tags: [architecture, version, build, cli]
timestamp: 2026-07-25T00:00:00Z
---

# Versioning

IKE carries a version from #1214 onwards. Before that there were no tags, no
version string and no releases — the first number is **0.1.0**.

## Where the number lives

`internal/version` is a leaf package with no dependencies, so anything may
import it:

- `Version` — the release number, the source of truth, bumped in this file.
  It is a `var` rather than a `const` so a release build can stamp the tag it
  is cutting: a binary built from a tag and one built from the same commit on
  a branch are then distinguishable.
- `Commit`, `Dirty` — the build stamp, empty under a plain `go build` and
  filled in by the Makefile through `-ldflags -X`. `Dirty` is a string, not a
  bool, because `-X` can only set strings.
- `Short()` returns the bare number (the help overlay title). `Full()` returns
  the banner: number, parenthesised stamp when there is one, then the Go
  toolchain and `GOOS/GOARCH`.

`buildStamp()` renders nothing when `Commit` is empty, so the banner never
shows an empty `()` pair — including the hand-rolled `-ldflags` case that sets
only `Dirty`.

## The flag

`--version` / `-v` is parsed in `internal/cli` like every other argument form,
so the grammar stays in one table-tested place. It **short-circuits**: the
first occurrence returns `Invocation{Version: true}` immediately, without
validating the rest of the line — nothing else on it can change what the flag
does, and a version request must work even when the rest of the invocation is
malformed.

`cmd/ike` prints `version.Full()` and returns before any terminal setup, so
the flag works in a pipe, in CI, and in a terminal IKE itself could not run
in.

## The build stamp

The Makefile computes the stamp and passes it to both `build` and `install`:

```make
COMMIT  := $(shell git rev-parse --short HEAD 2>/dev/null)
DIRTY   := $(shell git diff --quiet 2>/dev/null || echo true)
LDFLAGS := -X ike/internal/version.Commit=$(COMMIT) -X ike/internal/version.Dirty=$(DIRTY)
```

Both `git` calls are failure-tolerant: a build from an exported tarball with
no `.git` yields empty strings and a banner without the parenthesised part,
rather than a build error. `make version` builds and prints the banner.

## What SemVer means here

IKE ships as a binary and is not importable, so "the public API" is not a Go
package API. The surface that users and plugin authors actually depend on is:

- the settings-file format (`internal/config`),
- the keymap schema and the default bindings,
- the WASM plugin ABI and the marketplace index format.

Read the version against those:

| Bump | Means |
|---|---|
| Patch | Bug fixes, refactors, new language plugins — nothing a config, keymap or plugin can notice. |
| Minor | New features, new settings keys, new default bindings — additive, existing configs keep working. |
| Pre-1.0 minor / post-1.0 major | A change that breaks an existing settings file, removes or remaps a default binding users may rely on, or requires plugins to be rebuilt. |

1.0.0 is deliberately unclaimed. It would assert a compatibility promise, and
the project has no test gate in CI, no release process and no changelog to
back one up — see the "no support promise" in
[the user documentation](https://truedaerk.github.io/ike/contributing/).

## Surfaces

The help overlay title (`internal/help`) carries `version.Short()`: it is the
screen a user is already looking at when they need to quote which build they
are on. The `--version` flag carries the full banner. The bug-report issue
template asks for that banner verbatim.
