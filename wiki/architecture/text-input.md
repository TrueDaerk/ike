---
type: architecture
title: Single-Line Text Input
description: The shared single-line editing helpers in internal/ui (EditKey, PasteText, CursorView) that every text field in the IDE routes through — plus the convention, the audit of every input site, and the guard test that keeps new fields from re-inventing them.
resource: internal/ui/textinput.go
tags: [ui, input, keys, paste, conventions]
timestamp: 2026-08-20T00:00:00Z
---

# Single-Line Text Input

Every one-line text field in IKE — search prompts, rename dialogs, filter
lines, query lines, path pickers — edits through the same three helpers in
`internal/ui/textinput.go`. That is a convention, not a suggestion: it is why
paste, cursor movement, word operations and the macOS editing chords behave
identically in the command palette, the settings filter, the explorer's speed
search and the debugger's inline value editor.

## The convention

> **A new single-line text input MUST route its keys through `ui.EditKey`,
> its pastes through `ui.PasteText`, and render its cursor with
> `ui.CursorView`.** A field-specific chord (tab completion, ctrl+u, a
> preselected prefill) is handled *before* the call and the rest is handed
> over. Do not hand-roll `tea.KeyMsg` cases that mutate a string field.

The reason is a decade of one-field-at-a-time bug reports: a broken paste
(#1273), a missing cursor (#763), a byte-sliced backspace corrupting umlauts
(#888), missing `opt`/`cmd` chords (#733, #955). Each was fixed in one field
while the next hand-rolled input shipped with the same gaps. #2002 swept the
remainder and added a guard so the pattern cannot come back quietly.

## The helpers

`internal/ui/textinput.go`:

- **`EditKey(msg, text, cur) (out string, ncur int, handled, changed bool)`** —
  applies one key to `text` at rune cursor `cur` (clamped first). It handles
  `left`/`right`, `home`/`end` and their `super+arrow` equivalents,
  `alt`/`ctrl+arrow` word motions, `backspace`, `delete`, the word kills
  (`alt+backspace`, `ctrl+w`, `alt+delete`, `alt+d`), the line kill
  (`super+backspace`), and printable insertion at the cursor. `handled`
  reports the key was an editing key — an unhandled key is the caller's to
  interpret; `changed` reports the text actually differs, which is the signal
  to re-run a live preview, re-filter a list, or refresh completions.
- **`PasteText(text, cur, paste) (out string, ncur int, changed bool)`** —
  inserts a pasted block at the cursor. A single-line block goes in verbatim
  (a deliberate leading space survives; a path copied with its trailing
  newline arrives clean); a genuinely multi-line block is trimmed per line,
  empties dropped, the rest joined with single spaces. A line break can never
  land in a one-line field.
- **`CursorView(text, cur)`** — renders the text with a reverse-video cursor
  cell (end-of-text shows a reversed space).

`internal/settings` wraps them once more for its own use: `textField`
(`text`+`cur` with `Handle`/`Paste`/`View`) for inline edits and forms, and
`filterKey`/`filterPaste`/`filterView` for the filter lines whose text has to
stay a plain `string` because every renderer and matcher reads it.

## Ordering rules

- **Caller chords win.** Run the field's own bindings first, then call
  `EditKey` with what is left. The terminal's scrollback search keeps `ctrl+w`
  as a toggle that way; the find/replace panel keeps `ctrl+u` as "clear the
  field"; the explorer's speed search keeps `ctrl+n`/`ctrl+p` as match
  stepping.
- **`changed`, not `handled`, drives side effects.** A cursor motion is
  `handled` but not `changed`; re-running an incremental search on it wastes a
  pass and can move the viewport for no reason.
- **A preselected prefill is a caller concern.** Fields that open with their
  text selected (#292 find/replace, #1047 explorer rename) drop the selection
  in their own branch and then let `EditKey` do the insertion — never a second
  hand-written insert path.
- **Paste is routed, not typed.** A bracketed paste (or `cmd+v`) arrives as
  one message at the top of the app and is routed to whichever surface owns
  the keyboard: `routeOverlayPaste` for overlays and shell prompts,
  `pasteIntoPrompt` for editor-internal inputs, `settings.Model.Paste` for the
  panel (sub-panel form → detail editor → filter), and `handlePaste`'s pane
  switch for the tool windows. A surface with no text input returns `false`
  and the block is dropped — it must never leak into the buffer underneath.
  The router's order mirrors the `KeyPressMsg` guard chain exactly: the
  overlays resolved *before* the popup terminal layer (`overlayCapturesAbovePopup`)
  always take the paste, the ones after it (`overlayCapturesBelowPopup` — the
  single-line prompts, the regex tester, a focused jq playground, the capturing
  explorer) only while that layer is closed. Anything else would let a merely
  *mounted* surface steal a paste from the focused one, which is exactly what
  the jq playground did over a floating terminal (#2236).

## Input site audit (#2002)

Every single-line text input in `internal/`, and what it uses.

### Overlays and shell prompts

| Site | File | Status |
| --- | --- | --- |
| Command palette query | `internal/palette/palette.go` | shared (#763) |
| Find-in-path / replace-in-path | `internal/finder/finder.go` | shared (#763) |
| Editor command line (`:` `/` `?`) | `internal/editor/keys_command.go` | shared (#763, paste #1380) |
| Editor find/replace panel | `internal/editor/replace_panel.go` | **migrated (#2002)** — was append-only, no cursor |
| Save-as prompt | `internal/app/saveas.go` | shared |
| Clone dialog (URL + name) | `internal/app/clone_prompt.go` | shared (#1873) |
| New-project wizard name | `internal/app/newproject_prompt.go` | shared |
| Save-layout prompt | `internal/app/layouts_ui.go` | shared |
| Bookmark prompt | `internal/app/bookmarks_store.go` | shared |
| Regex tester (pattern + test text) | `internal/app/regextester.go` | shared (#1937) |
| jq playground query | `internal/app/jqplayground.go` | shared (#1936); vertical motion over the wrapped rows on top (#2038) |
| File rename prompt | `internal/app/fileops.go` | **migrated (#2002)** |
| Symbol rename prompt (LSP) | `internal/app/lsprename.go` | **migrated (#2002)** |
| JetBrains keymap import path | `internal/app/jbimport_prompt.go` | **migrated (#2002)** |
| OpenAPI import path | `internal/app/openapi_import.go` | **migrated (#2002)** |

### Panes and tool windows

| Site | File | Status |
| --- | --- | --- |
| Terminal scrollback search | `internal/terminal/search.go` | shared (#1882) |
| HTTP response search | `internal/httppane/httppane.go` | shared (#1845, #1955) |
| Data viewer filter clause | `internal/dataview/filter.go` | shared; **paste routed (#2002)** |
| DOM inspector selector | `internal/domview/domview.go` | shared (#1929); **paste routed (#2002)** |
| GitHub issues filter | `internal/ghissues/ghissues.go` | shared (#1934); **paste routed (#2002)** |
| Explorer name prompt (new/rename) | `internal/explorer/fileops.go` | **migrated (#2002)** |
| Explorer speed search | `internal/explorer/search.go` | **migrated (#2002)** — was append-only, no cursor |
| Debug variables inline editor | `internal/debugpanel/debugpanel.go` | **migrated (#2002)**; paste routed |
| Breakpoint refinement editor | `internal/breakpanel/meta.go` | **migrated (#2002)**; paste routed |

### Settings

| Site | File | Status |
| --- | --- | --- |
| Inline value editors (int/text/path/list) | `internal/settings/editor.go` | shared via `textField` (#888) |
| Sub-panel forms (ES, tools, format, associations, debug map, LSP override, keymap import, venv wizard) | `internal/settings/*_form.go`, `migrated_panels.go`, `venv_wizard.go` | shared via `textField` (#888); **paste routed (#2002)** |
| Colour page free-token input | `internal/settings/colors_page.go` | shared via `textField` |
| Toolchain custom path / package install | `internal/settings/toolchain_page.go` | shared via `textField`; **paste routed (#2002)** |
| Panel-wide filter | `internal/settings/panel.go` | **migrated (#2002)** — byte-sliced backspace |
| Colour page filter | `internal/settings/colors_page.go` | **migrated (#2002)** |
| Keymap page filter | `internal/settings/keymap_page.go` | **migrated (#2002)** — byte-sliced backspace |
| Enum editor type-to-filter | `internal/settings/editor.go` | **migrated (#2002)** |

### Deliberately not migrated

| Site | File | Why |
| --- | --- | --- |
| Editor buffer insertion | `internal/editor/keys_insert.go` | Multi-line document, not a one-line field: insertion goes through the editor's own edit path (undo grouping, auto-indent, pair completion). |
| Terminal key encoding | `internal/terminal/model.go` | Keys are encoded for the pty; the shell inside owns its line editing (the emulator only maps the macOS chords to their readline equivalents, #733/#955). |
| Floating-shell live filter | `internal/ui/floating.go` | A type-to-filter over a scrollable view: arrows and page keys must stay scroll keys, so the filter is intentionally append-only with a rune-safe backspace. |
| Chord capture | `internal/settings/keys.go`, `migrated_panels.go` | Captures key chords, not text; backspace undoes one captured step. |

## The guard

`internal/ui/inputsweep_test.go` walks `internal/` and fails on the two shapes
a hand-rolled input always has — `x += key.Text` and the `key.Text != ""`
guard fronting a hand-written insertion branch — outside `internal/ui` and an
explicit allowlist. Each allowlist entry carries the reason it is exempt, and
a second test fails when an entry no longer matches the pattern, so a stale
exemption cannot silently cover whatever that file grows next.

The check is deliberately crude. It is not a type system; it is the review
prompt that makes "why isn't this using `ui.EditKey`?" happen before the merge
instead of after the bug report.
