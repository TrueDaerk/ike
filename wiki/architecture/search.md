---
type: concept
title: Project Search (Find in Path)
description: Streaming project-wide search engine — rg --json backend with a pure-Go walker fallback, generation-based cancellation, bounded results.
resource: internal/search
tags: [architecture, search, find-in-path]
timestamp: 2026-07-08T12:00:00Z
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

## Find-in-path overlay (#85)

`internal/finder` is the modal UI over the engine, opened by
`project.findInPath` (cmd+shift+f, palette, or the menu-reachable command
table). It owns the keyboard while open (routed by the root model ahead of
the palette):

- **Inputs:** the query plus include/exclude glob fields (comma-separated);
  `tab`/`shift+tab` cycle field focus. Every edit restarts the scan — the
  service's generation counter cancels the superseded one, and `Apply` drops
  stale-generation messages.
- **Toggles:** case (`alt+c`), whole word (`alt+w`), regex (`alt+x`).
- **Query history:** committed on enter; recalled with `alt+up`/`alt+down`
  (and plain `up`/`down` while the result list is empty — with results those
  keys move the selection).
- **Results:** the reusable `internal/locations` list — items grouped by
  file (headers show per-file counts), match ranges highlighted, cursor row
  selected, scrolled into view; the status row shows live counts, `…` while
  streaming, `(truncated)` at the result bound, and scan errors. The
  component is consumer-agnostic: the Problems window (#33) and TODO index
  (#61) are its planned next hosts.
- **Navigation:** `enter` opens the file at the match via the
  definition-jump path (`openPathAt`) and closes the overlay; the results
  survive closing, so `search.nextMatch` / `search.prevMatch` (palette
  commands) keep stepping matches — wrapping across files — without the
  overlay.

## Replace in path (#86)

`project.replaceInPath` (cmd+shift+r) opens the same overlay in replace mode:
a replacement-template input joins the field cycle, and a before/after
preview for the selected match renders under the results (`- old` / `+ new`).
Apply keys: `enter` replaces the selected match (and steps on), `alt+f` the
selected file's matches, `alt+a` everything; `alt+enter` navigates instead.
Applied matches leave the list; the overlay stays open.

Application (`internal/app/replace.go`) routes per file:

- **Dirty open buffer:** matches become `editor.Replacement`s applied through
  the buffer as **one undo unit per file** (a single `u` reverts the batch);
  the file on disk keeps only the user's saved state. The change event drives
  LSP/highlight/shared-document sync as usual.
- **Everything else:** the file is rewritten on disk. A clean open buffer
  picks the write up through the 0140 watcher path (external change →
  auto-reload) — deliberately the same flow as any external edit.
- **Staleness guard:** a match applies only while the line's prefix up to the
  match end still reads as scanned (prefix, not whole-line, so several
  matches on one line stay valid while applying right-to-left). Skipped
  matches are counted in the summary notification
  (`N replacements in M files (K stale matches skipped)`).
- **Capture groups:** regex replacements expand `$1`/`${name}` via
  `search.RewriteRange` (Go regexp Expand semantics; the whole-word wrapper
  is non-capturing, so user group numbers are stable). Literal replacements
  never expand.
