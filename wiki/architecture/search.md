---
type: concept
title: Project Search (Find in Path)
description: Streaming project-wide search engine — rg --json backend with a pure-Go walker fallback, generation-based cancellation, bounded results.
resource: internal/search
tags: [architecture, search, find-in-path]
timestamp: 2026-08-30T00:00:00Z
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
  `tab`/`shift+tab` cycle field focus. All fields are full single-line
  editors (`internal/ui.EditKey`, #763): arrows/home/end move the cursor,
  `alt+left`/`alt+right` jump words, `alt+backspace`/`alt+delete` delete
  words, `delete` removes forward, typing (and pasted text) inserts at the
  cursor; the same helper drives the command palette's query. Every edit
  restarts the scan — the
  service's generation counter cancels the superseded one, and `Apply` drops
  stale-generation messages. Re-opening keeps the previous query
  **preselected** (rendered inverted, #277): the first typed character
  replaces it wholesale; backspace, tab, arrows or history recall keep the
  text and drop the mark.
- **Prefill from the active selection (#2165):** opening the overlay while a
  text selection is live seeds the query with the selected text, JetBrains
  style, and marks it preselected — the first typed character replaces it, no
  extra keys. `Model.OpenPrefilled` / `OpenReplacePrefilled` take the
  selection; the root model resolves it with `app.activeSelectionText`, which
  asks the **focused** pane first and falls back to the last-focused editor,
  so invoking the command from a tool pane still picks up the visual selection
  left in the code. The selection sources it knows (the full audit — every
  other pane has a row cursor, not a text selection) are: editor panes (vim
  visual mode), terminal panes plus terminal tabs and the debug console
  (`Instance.ActiveTerminal`), diff panes (side-column mouse selection and the
  editable right side's visual mode), merge panes, and the HTTP response
  viewer. Rules: a selection **spanning more than one line prefills nothing**
  — the query language has no line-spanning match to offer, the same rule the
  editor's `/` (#2063) and the HTTP viewer's search (#2122) follow — and so
  does a blank one; a single trailing newline (a linewise selection of one
  line) is not a span and still prefills. With **regex** mode on the prefill
  is escaped with `regexp.QuoteMeta`, so the selected text is found literally.
  A prefill outranks both the remembered query and the restored result cursor
  (#2054): the reopened scan is a different search. With no selection nothing
  changes — the remembered query and cursor resume as before.
- **Toggles:** case (`ctrl+c`), whole word (`ctrl+w`), regex (`ctrl+x`);
  `alt+c`/`alt+w`/`alt+x` still work where terminals deliver alt (#422 —
  on macOS Option is a composition key, so ctrl is the delivered primary,
  mirroring the tab-key story in #248). The overlay owns the keyboard, so
  these ctrl chords never reach the global quit/close-pane bindings.
- **Query history:** committed on enter; recalled with `ctrl+up`/`ctrl+down`
  (`alt+up`/`alt+down` as secondary, and plain `up`/`down` while the result
  list is empty — with results those keys move the selection). The list
  **persists** (#1171): commits push into the `findInPath` bucket of the
  app-owned query-history store (`internal/histories`, one `histories.json`
  under the state store beside `marks.json`; deduped, capped at 50, malformed
  file reads as empty), and the first `Open` of a session seeds the recall
  list from it. The editor's `/` and `:` lines keep sibling buckets in the
  same file (see [Editor § search](/architecture/editor.md)).
- **Resume last search (#2054):** `Close` saves the *full* current state —
  query, the three toggles, both glob fields, and the selected result's index
  — as `histories.FindState` in the same `histories.json` (one struct, not a
  recall bucket). Re-opening always keeps this state in memory already (see
  preselect above); what `FindState` adds is carrying it across a restart:
  the first `Open` of a fresh session (empty in-memory model, store injected)
  loads it and seeds the query/toggles/globs the same way. Either way — a
  same-session reopen or a fresh session — the remembered result index is
  restored once the reopened scan's `DoneMsg` lands (`Model.pendingCursor`,
  clamped by `locations.List.SetCursor`), so the cursor lands back on the
  same match rather than resetting to the first hit. There is no separate
  command or keybind for this: it is unconditional, same as the query
  preselect it extends — the simplest option that stays consistent with
  #277's existing behavior. A query edit (typed, pasted, or history-recalled)
  invalidates any pending restore, same as it always invalidated the old
  result set.
- **Results:** the reusable `internal/locations` list — items grouped by
  file (headers show per-file counts), match ranges highlighted, cursor row
  selected, scrolled into view; **one row per file:line**, so a line the
  query hits several times is listed once with every occurrence highlighted
  in it (#1121: `List.Append` folds consecutive same-line items together,
  keeping the extra ranges in `Item.More`; `Item.Ranges()` returns them all,
  ordered by column). Counts follow the rows — per matching line, not per
  occurrence — in the per-file headers and the status row alike.
  The status row shows live counts, `…` while
  streaming, `(truncated)` at the result bound, and scan errors. The
  component is consumer-agnostic: the Problems window (#33) and TODO index
  (#61) are its planned next hosts.
- **Layout (#2047):** the results block is a **fixed-height** region between
  `ui.MinResultRows` and `ui.MaxResultRows` (**11 to 40** rows, headers
  included): eleven rows even with no matches at all — so the overlay stops
  resizing under the cursor while a scan streams in — and forty at most, past
  which the list scrolls instead of growing. Beside it, separated by a dim
  vertical rule, a **code preview** shows the selected match's file around its
  line; it follows the list cursor and degrades to a dim `preview unavailable`
  notice when the file is gone or unreadable. Since #2327 it is a **read-only
  mini editor**, not a text dump: syntax-highlighted, with a line-number
  gutter, the hit line backgrounded across the full column, the match ranges
  emphasised on top of the syntax colours, and — once focused — scrollable
  through the whole file (see [Command palette § code
  preview](/architecture/command-palette.md)). The body comes from
  `Cache.Columns`; the geometry from `Cache.SplitFor`, which sizes the column
  to the code around the hit within **50 to 120** cells, never past half the
  content and never leaving the list under 40 cells, and drops the column
  below 64 cells of content, where the list keeps the full width. The box
  itself may grow to 120 columns to carry both. `internal/codepreview` is the
  shared component behind every picker with a preview column (#2053) — the
  [palette pickers](/architecture/command-palette.md) (usages, symbols, files,
  bookmarks) and the call-hierarchy overlay use the same pieces (`Target`,
  `SplitWidth`/`Natural`, `Cache`), so the columns line up and behave alike.
- **Preview focus (#2327):** `alt+p` (or `ctrl+e`, the macOS-safe alias) hands
  the excerpt column the keyboard; a mouse press inside it does the same. The
  rule turns accent-coloured, the status row spells the motions, and the
  editor's own read-only motions apply: `j`/`k` and the arrows one line,
  `ctrl+d`/`ctrl+u` half a page, `ctrl+f`/`ctrl+b` (and `pgup`/`pgdn`) a full
  page, `h`/`l` four columns sideways, `0`/`$` to the line's ends, `g`/`G` to
  the file's, and `z` back to the match. Nothing edits. `esc` blurs — a second
  `esc` closes the overlay — and moving the result selection re-centres the
  excerpt on the new hit, dropping the scroll offsets.
- **Navigation:** `enter` opens the file at the match via the
  definition-jump path (`openPathAt`) and closes the overlay; the results
  survive closing, so `search.nextMatch` / `search.prevMatch` (f3/shift+f3,
  plus the IntelliJ macOS aliases cmd+g/cmd+shift+g, also palette commands)
  keep stepping matches — wrapping across files — without the overlay. The most recent search wins those keys (#376): a
  committed in-file search (`/`, `?`, cmd+f) makes f3/shift+f3 repeat it like
  `n`/`N` on the active editor (the editor announces the commit with
  `editor.SearchCommittedMsg`); the next find-in-path scan reclaims them.
  `editor.RepeatSearch` scrolls the landing into view itself (#1198): the
  root model calls it directly on the model, so the trailing `scroll()` of
  `Update`'s key branch — which is what makes `n`/`N` follow the cursor —
  never runs on this path.
- **Mouse (#424):** the overlay hit-tests first in the root mouse handler
  (it renders above every other overlay); a click outside dismisses it and
  clicks inside never leak to the panes below (#116). Inside: a click on an
  input row focuses that field, on a toggle flips it (and rescans), on a
  result row selects the match — a second press on the selected match opens
  it (the settings panel's press-again-to-activate semantics, #127). Presses
  in the code-preview column never activate a row — `layoutInfo.listW` bounds
  the clickable region (#2047) — they focus the excerpt instead (#2327). The
  wheel scrolls the result list, or the focused excerpt. `View` records the row layout in a
  `layoutInfo` each render; `Click` maps panel-local coordinates through it
  and `locations.List.ItemAt` (window-relative row → item index).

## Replace in path (#86)

`project.replaceInPath` (cmd+shift+r) opens the same overlay in replace mode:
a replacement-template input joins the field cycle, and a before/after
preview for the selected match renders under the results (`- old` / `+ new`).
Every result row also previews inline (#2154): each match renders struck
through with its replacement appended beside it (`locations.List.Rewrite`, a
hook the finder installs per render — editing the template redraws the
previews without a rescan; a regex range the template no longer matches
renders plainly).

**Selective apply (#2154):** `ctrl+t` toggles the selected match out of (or
back into) the apply set and steps on; `ctrl+g` toggles the selected file as
a group (any included item excludes them all, a fully excluded file
re-includes). Excluded rows render faint with a `✗` marker, skip the inline
preview, stay counted in the status row (`…, K excluded`), and survive batch
applies — what was deliberately left alone stays visible.

Apply keys: `enter` replaces the selected match (and steps on), `ctrl+f` the
selected file's non-excluded matches, `ctrl+a` every non-excluded match —
apply-all when nothing is toggled off, apply-selected otherwise;
`ctrl+enter` navigates instead (alt variants remain as secondaries, #422).
Applied matches leave the list; the overlay stays open.

Application (`internal/app/replace.go`) first expands each row back into one
match per range (`flattenRanges`) — a collapsed multi-match line (#1121) is a
single row but still every occurrence to rewrite — then routes per file:

- **Dirty open buffer:** matches become `editor.Replacement`s applied through
  the buffer as **one undo unit per file** (a single `u` reverts the batch);
  the file on disk keeps only the user's saved state. The change event drives
  LSP/highlight/shared-document sync as usual.
- **Everything else:** the file is rewritten on disk. A clean open buffer
  picks the write up through the 0140 watcher path (external change →
  auto-reload) — deliberately the same flow as any external edit. The write
  preserves the file's encoding and line-ending flavor (#2154): bytes decode
  the way the editor would open them (`internal/textenc` — BOM, then UTF-8,
  then the `files.encoding` fallback) and re-encode with the detected
  flavor; an undecodable file, or a replacement the encoding cannot
  represent, skips instead of corrupting.
- **Stale-file guard (#2154):** the finder records each result file's mtime
  when its first match streams in; the apply request carries the map. A disk
  file whose mtime moved on since then — changed after the search ran — is
  skipped whole (line numbers are no longer trusted) and reported as a
  warning (`… (N files changed since the search, skipped)`). The apply
  path's own write refreshes the shared baseline, so later batches from the
  same result set still apply. Dirty buffers are exempt — the buffer content
  is the truth there, guarded per line by `Replacement.Expect`.
- **Stale-line guard:** a match applies only while the line's prefix up to the
  match end still reads as scanned (prefix, not whole-line, so several
  matches on one line stay valid while applying right-to-left). Skipped
  matches are counted in the summary notification
  (`N replacements in M files (K stale matches skipped)`).
- **Capture groups:** regex replacements expand `$1`/`${name}` via
  `search.RewriteRange` (Go regexp Expand semantics; the whole-word wrapper
  is non-capturing, so user group numbers are stable). Literal replacements
  never expand.
