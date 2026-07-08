---
type: concept
title: Project Search (Find in Path)
description: Streaming project-wide search engine — rg --json backend with a pure-Go walker fallback, generation-based cancellation, bounded results.
resource: internal/search
tags: [architecture, search, find-in-path]
timestamp: 2026-07-08T00:00:00Z
---

# Project Search (Find in Path)

`internal/search` is the engine behind Roadmap 0150 (JetBrains' Cmd+Shift+F /
Cmd+Shift+R): a streaming scanner with one result shape and two backends. The
query UI and results pane (#85) and replace-in-path (#86) consume it.

## Result shape & streaming

A `Query` (pattern, case / whole-word / regex flags, include/exclude globs,
root, result bound) yields a stream of `Match`es — path, 1-based line, line
text, and the matched range as 0-based **rune** columns (byte offsets from the
backends are converted, so the UI can highlight without re-deriving).

Scans run on their own goroutine and report through the host's `Send` as
`BatchMsg`s (64 matches per flush — first results render while the scan
continues) followed by exactly one `DoneMsg` carrying the total, a `Truncated`
flag (the `MaxResults` bound stopped the scan early; default 2000), and any
scan error. "No matches" is a clean empty Done, never an error.

## Cancellation: generations

`Service.Scan` cancels the running scan (context cancellation; the rg child is
killed) and increments a **generation** counter; every message carries its
scan's generation. Consumers keep only the latest generation's messages — a
stale scan may still flush a batch or its Done after being superseded, and
that is fine: filtering by generation is the contract, matching the
version-tagging pattern the highlight pipeline uses.

## Backends

- **ripgrep** (`rg --json`, when on PATH): flags map 1:1 (`-i`/`-s`, `-w`,
  `-F` for literal, `-g` globs). `--no-require-git` keeps `.gitignore`
  respected even outside a git repository, so behavior does not change with
  repo status. Exit code 1 ("no matches") is success; code 2 (bad
  pattern/glob) surfaces as the Done error.
- **Pure-Go fallback** (no ripgrep): `filepath.WalkDir` + one compiled regexp
  (literal patterns are quoted; whole-word wraps `\b`; case-insensitivity is
  `(?i)` — the same semantics the rg flags select, guarded by a parity test).
  Skips `.git`, hidden dot-entries (the explorer's rule), gitignored paths,
  binaries (NUL sniff in the first KiB), and files over 4 MiB.

The fallback's `.gitignore` matcher is deliberately small: directory rules
(`gen/`), globs (`*.log`), anchored paths (`/dist`, `docs/api`), and `**/`
prefixes, scoped per declaring directory as the walker descends. Negation
(`!pattern`) is unsupported — when the fallback and rg disagree on an exotic
pattern, rg is right.

## Consumers

- **#85 Find-in-path UI:** query input + toggles in the floating shell,
  results pane grouped by file, navigation via the definition-jump path.
- **#86 Replace in path:** the same matches drive previewed replacements —
  through open dirty buffers (one undo unit per file), directly on disk
  otherwise.
