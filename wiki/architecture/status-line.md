---
type: concept
title: Status Line Segments
description: Extensible left/right slot model behind the bottom status bar — mode, file, diagnostics, host/LSP status, toolchain interpreter, notification counter.
resource: internal/app/statusline.go
tags: [architecture, ui, status-line, toolchain, notifications]
timestamp: 2026-07-12T22:00:00Z
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
| `file` | project-relative path + `[+]` / `[disk changed]` / `[large file]` markers | never (`no file`) |
| `eol` | on-disk line-ending flavor, `LF` / `CRLF` (+ ` (mixed)` when the load saw both, #66) | no file |
| `encoding` | on-disk character encoding (`UTF-8`, `UTF-16 LE`, …, #66) | no file |
| `diagnostics` | `NE NW` error/warning counts | buffer clean |
| `host` | plugin-set persistent status (`SetStatus`) | unset |
| `lsp` | focused buffer's language server state (#380) | no tracked state |
| `toolchain` | effective interpreter, see below | not resolvable |
| `notifications` | `● N` unseen notification count, see below | count is 0 |

The drag hint and the non-editor focus branches (terminal/explorer, #381) keep
their dedicated rendering; the terminal/explorer line appends the host status
and the notification counter.

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

## Notification counter

`Model.notifUnseen` counts history-ring entries recorded since the
notification history view (0130, #78) was last opened; the segment renders
`● N` and disappears at zero. Opening the history — the `notifications.history`
command — resets it. Opening on *click* is deferred until mouse support (#30)
grows clickable status line zones.
