---
type: concept
title: Status Line Segments
description: Extensible left/right slot model behind the bottom status bar — mode, file, buffer language, diagnostics, host/LSP status, toolchain interpreter, csv column, json/yaml path, notification counter, forge unread badge.
resource: internal/app/statusline.go
tags: [architecture, ui, status-line, toolchain, notifications]
timestamp: 2026-08-21T13:00:00Z
---

# Status Line Segments

Issue #101. The editor status line is a **segment model** — two ordered slot
lists (`statusLeft`, `statusRight` in `internal/app/statusline.go`) — instead
of string concatenation in `statusLine()`. Each slot is a `statusSegment`:
an id plus a `render(m Model, ed *editor.Model) string` function; an empty
result hides the slot for that frame. Non-empty slots are joined with `" │ "`,
the right list is right-aligned (default: the cursor position). Appending to
the lists is the (in-process) extension point for future plugin-contributed
segments.

## Default left slots (in order)

| id | content | hidden when |
|---|---|---|
| `mode` | editor input mode (`NORMAL`, `INSERT`, …) | never |
| `macro` | `recording @x` while a macro recording is active (#58) | idle |
| `file` | project-relative path + `[+]` / `[disk changed]` / `[large file]` markers | never (`no file`) |
| `buflang` | chosen buffer language of a file-less buffer, `as Markdown` (#2033, see [Language Registry](/architecture/languages.md)) | the buffer has a file, or no type was chosen |
| `hint` | empty-editor discovery hint, `? help · shift shift find` (#659); the search chord renders resolver-truth (a remap outside the known defaults shows the live chord) | a file is open, or the terminal is narrower than ~70 columns |
| `eol` | on-disk line-ending flavor, `LF` / `CRLF` (+ ` (mixed)` when the load saw both, #66) | no file |
| `encoding` | on-disk character encoding (`UTF-8`, `UTF-16 LE`, …, #66) | no file |
| `indent` | effective indent style + width, `Spaces: 2` / `Tab: 4`, including any `.editorconfig` override (#63) | no file |
| `svcolumn` | caret's table column in a csv/tsv/psv buffer, `column 3: qty` — the name from a header-ish first row, bare `column 3` otherwise (#1659, see [Editor](/architecture/editor.md)) | not a table-rendered buffer |
| `docpath` | caret's path in a JSON/YAML buffer, `spec.containers[2].env[0].name` — truncated from the left at 44 cells, since the tail is the interesting part (#1660, see [Editor](/architecture/editor.md)) | no path scanner for the buffer, or the caret is at the document root |
| `diagnostics` | `NE NW` error/warning counts | buffer clean |
| `host` | plugin-set persistent status (`SetStatus`) | unset |
| `lsp` | focused buffer's language server state (#380) | no tracked state |
| `toolchain` | effective interpreter, see below | not resolvable |
| `notifications` | `● N` unseen notification count, see below | count is 0 |
| `forge` | `● 2 new issues` unread forge events (#2086), see [Notifications](/architecture/notifications.md) | nothing unread |

The drag hint and the non-editor focus branches (terminal/explorer, #381) keep
their dedicated rendering; the terminal/explorer line appends the host status,
the notification counter and the forge unread badge — the badge is persistent
state, so it must stay visible wherever the focus is.

The rendered bar is clamped to the terminal width (#659): lipgloss pads but
does not clip, so without the guard an over-wide segment set would wrap the
bar onto a second row and corrupt the layout. Overflow shrinks
priority-aware (#471, `composeStatus`): first the file segment shortens by
exactly the overflow with a JetBrains-style middle ellipsis (floor 16
cells), then low-priority segments drop in a defined order (hint, eol,
encoding, indent, svcolumn, docpath, toolchain, todo, host, notifications, macro,
branch, buflang, forge, diagnostics, lsp — mode, file and the cursor never drop), and only as a
last resort the bar hard-clips on the right.

## Mode badge (#1323)

The `mode` slot renders as a **coloured badge**, not as plain panel text: the
label reads in bold on a background taken from `editor.ModeColor` — Accent for
`NORMAL`, Success for `INSERT`, Warning for the visual modes, Error for
`REPLACE`, Info for the `:` command line. The same colour paints the caret cell
in the editor (see [Editor](/architecture/editor.md)), so the two signals always
agree and the mode is recognizable by colour alone.

Mechanics: the bar is composed as plain text first, then `badgeMode` splices the
badge over the mode span reported by `composeStatusSpans` (plus the pad cell on
either side) and re-styles the tail with the panel background, which the badge's
own SGR reset would otherwise drop. No cells are added, so segment spans stay
valid for hit-testing, and a hard clip that cuts the mode segment away falls back
to the plain bar. The badge's text colour is picked by contrast
(`theme.Readable` over `Background`/`Foreground`), so a theme with a light or a
dark mode colour both stay readable.

## Toolchain segment

Shows `<langID>:<name>` for the focused buffer's language — the *same*
`lang.Interpreter` resolution (explicit `[lang.<id>] interpreter` config beats
detection) that the toolchain settings page (0160, #94) and the terminal shims
(#98) read; one source of truth. The name is the virtualenv directory's base
name when the binary lives in a venv (`pyvenv.cfg` beside its `bin`), else the
binary's base name (e.g. `python3.12`). Resolution stats the filesystem and
scans PATH, so the label is **cached per language** (`Model.toolchainSeg`, a
shared map across value copies) and the cache is dropped on every config
reload — an interpreter change on the settings page re-resolves immediately.

## Git branch segment

Epic 0320 adds a `vcs` slot to the **right** list: `⎇ branch ↑n ↓m` — the
current branch (clipped to 24 characters) plus ahead/behind counters against
the upstream. It renders from the vcs status snapshot rather than shelling out
per frame, and hides entirely outside a git repository. See
[VCS / Git Integration](/architecture/vcs.md).

## Notification counter

`Model.notifUnseen` counts history-ring entries recorded since the
notification history view (0130, #78) was last opened; the segment renders
`● N` and disappears at zero. Opening the history — the `notifications.history`
command — resets it, and since #1128 a left click on the segment opens the
history directly (see "Clickable segments" below).

## Clickable segments (#1128)

`composeStatusSpans` is `composeStatus` plus the per-segment cell spans
(`statusSpan{id, x0, x1}`) of the final line — shrunken segments narrower,
dropped segments absent — so hit-testing always matches what is drawn.
`Model.statusSegmentAt(x)` resolves a status-row cell to a segment id (empty
while the row shows a drag hint or a non-editor focus summary), and the mouse
router dispatches a left press through `statusSegmentCommands`:

| segment | command |
|---|---|
| `buflang` (chosen buffer language) | `editor.setBufferLanguage` |
| `todo` (TODO count) | `todo.list` |
| `notifications` (`● N` counter) | `notifications.history` |
| `forge` (unread forge events) | `issues.toggle` |
| `lsp` (server state) | `lsp.showLog` |

Only segments with one clear, obvious target are wired; every other press on
the status row is swallowed (the row sits outside the layout tree, so nothing
below can receive it anyway).
