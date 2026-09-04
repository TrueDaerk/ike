---
type: architecture
title: Single-Line Text Input
description: The shared single-line editing helpers in internal/ui (ui.Field, EditKey, PasteText, CursorView) that every text field in the IDE routes through — plus the chord table, the convention, the audit of every input site, and the guard test that keeps new fields from re-inventing them.
resource: internal/ui/textinput.go
tags: [ui, input, keys, paste, conventions]
timestamp: 2026-09-03T18:00:00Z
---

# Single-Line Text Input

Every one-line text field in IKE — search prompts, rename dialogs, filter
lines, query lines, path pickers — edits through the same three helpers in
`internal/ui/textinput.go`. That is a convention, not a suggestion: it is why
paste, cursor movement, word operations and the macOS editing chords behave
identically in the command palette, the settings filter, the explorer's speed
search and the debugger's inline value editor.

## The convention

> **A new single-line text input MUST hold its state in a `ui.Field` and
> route its keys through `Field.Key`, its pastes through `Field.Paste`, and
> its rendering through `Field.View`.** A field-specific chord (tab
> completion, ctrl+u, a preselected prefill) is handled *before* the call and
> the rest is handed over. Do not hand-roll `tea.KeyMsg` cases that mutate a
> string field.

A host that cannot own the string — a filter column whose text every renderer
and matcher reads, a form keeping one cursor across several strings — still
calls the free functions `ui.EditKey` / `ui.PasteText` / `ui.CursorView`
underneath; `Field` is a thin value wrapper over exactly those.

The reason is a decade of one-field-at-a-time bug reports: a broken paste
(#1273), a missing cursor (#763), a byte-sliced backspace corrupting umlauts
(#888), missing `opt`/`cmd` chords (#733, #955). Each was fixed in one field
while the next hand-rolled input shipped with the same gaps. #2002 swept the
remainder and added a guard so the pattern cannot come back quietly.

## The helpers

`internal/ui/field.go`:

- **`ui.Field`** (#2459) — `struct { Text string; Cur int }`, the state every
  one-line input should hold. `Key(msg) (handled, changed bool)`,
  `Paste(s) bool`, `View() string`, `ViewSel(selStart, selEnd, style) string`,
  `Set(s)` (cursor to the end), `Clear()`, `Empty()`, `Runes()`, `Len()`, and
  the `NewField(text)` constructor. The fields are exported on purpose: a
  matcher, a renderer or a completion source reads `Text` directly, and a host
  seeding a caret writes `Cur`. The zero value is an empty field. It removes
  the four-line "call `EditKey`, store the result if `handled`" dance that was
  written out at ~60 call sites, each of them a place to forget writing the
  cursor back.

`internal/ui/textinput.go`:

- **`EditKey(msg, text, cur) (out string, ncur int, handled, changed bool)`** —
  applies one key to `text` at rune cursor `cur` (clamped first); the chord
  table is below. `handled` reports the key was an editing key — an unhandled
  key is the caller's to interpret; `changed` reports the text actually
  differs, which is the signal to re-run a live preview, re-filter a list, or
  refresh completions.
- **`PasteText(text, cur, paste) (out string, ncur int, changed bool)`** —
  inserts a pasted block at the cursor. A single-line block goes in verbatim
  (a deliberate leading space survives; a path copied with its trailing
  newline arrives clean); a genuinely multi-line block is trimmed per line,
  empties dropped, the rest joined with single spaces. A line break can never
  land in a one-line field.
- **`CursorView(text, cur)`** — renders the text with a reverse-video cursor
  cell (end-of-text shows a reversed space).
- **`Typing(msg) bool`** (#2327) — EditKey's own insertion guard, exported:
  the key carries printable text and no modifier that would make it a chord.
  A host that must decide *something else* about a typed key asks this rather
  than re-deriving it — the find-in-path overlay and the palette use it to
  blur a focused [code preview](/architecture/command-palette.md) the moment
  one types, so the query field takes the key. The sweep guard below reads
  `msg.Text != ""` as a hand-rolled input, which is exactly the drift this
  predicate removes.

`internal/settings` wraps them once more for its own use: `textField` (a
`ui.Field` plus the settings-local constructors and the older `Handle` name)
for inline edits and forms, and `filterKey`/`filterPaste`/`filterView` for the
filter lines whose text has to stay a plain `string` because every renderer
and matcher reads it. `internal/filterbar.Model` and `ui.SpeedSearch` hold a
`ui.Field` too.

## The chord table

`EditKey` matches on **`msg.Code` + `msg.Mod`**, never on `msg.String()`
(#2459). bubbletea's `String()` prefers the literal text a terminal reported
over the keystroke form, so under the Kitty protocol's *report associated
text* flag (or the Windows Console API) `alt+d` arrives as a bare `"d"` and
`ctrl+w` as `"w"` — the modifier is masked and no string case can ever match
it (#2064, the same trap `keymap.FromKeyMsg` works around with `Keystroke()`).
`Code` is the physical key and survives that.

The **Command key** is accepted in all its spellings: bubbletea reports it as
`super+` under one terminal protocol and `meta+` under another, and the keymap
layer's canonical name is `cmd+`. All three are the same physical key.
`shift` is tolerated on the *modified* chords — `shift+cmd+left` is still line
start, the way the terminal pane's `motionKey` treats it — while plain
`shift+left`/`shift+right` stay unhandled, so a host can keep them as
selection keys.

| Chord | Effect |
| --- | --- |
| `left` / `right` | move the caret one rune |
| `home` / `end`, `cmd+left` / `cmd+right` | line start / line end |
| `alt+left` / `ctrl+left` | word left |
| `alt+right` / `ctrl+right` | word right |
| `backspace`, `ctrl+h` | delete the rune before the caret |
| `delete` | delete the rune under the caret |
| `alt+backspace`, `ctrl+backspace`, `ctrl+w` | kill the word before the caret |
| `alt+delete`, `ctrl+delete`, `alt+d` | kill the word after the caret |
| `cmd+backspace`, `ctrl+u` | kill to line start |
| `cmd+delete`, `ctrl+k` | kill to line end |
| printable rune (no chord modifier) | insert at the caret |

Everything else — `enter`, `tab`, `esc`, the vertical keys, `cmd+c`, `cmd+v` —
comes back `handled=false` and is the caller's.

`ctrl+a`/`ctrl+e`/`ctrl+b`/`ctrl+f`/`ctrl+d`/`ctrl+t` and `alt+f`/`alt+b` are
deliberately **not** bound, even though readline has them: each is load-bearing
somewhere a text field is open (list stepping, the editor's own bindings), and
a field that swallowed them would break the surface around it.

The satellites match the Command key the same way: `ui.CopyKey` (the shared
copy chord; `ui.CopyChord`, its string form, additionally accepts `meta+c`),
`ssReserved` in `ui/speedsearch.go`, and the palette's `cmd+backspace` aux
action.

## Ordering rules

- **Caller chords win.** Run the field's own bindings first, then call
  `Field.Key`/`EditKey` with what is left. The terminal's scrollback search
  keeps `ctrl+w` as a toggle that way; the find/replace panel, the palette and
  the LSP rename prompt keep `ctrl+u` as "clear the field" ahead of the
  kill-to-line-start added in #2459; the palette keeps `cmd+backspace` as its
  aux action; the explorer's speed search keeps `ctrl+n`/`ctrl+p` as match
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

## Input site audit (#2002, swept onto `ui.Field` in #2460)

Every single-line text input in `internal/`, and what it uses. #2459 hardened
`EditKey` (Code+Mod matching, every Command spelling, ctrl+backspace/delete,
ctrl+u/ctrl+k) and introduced `ui.Field`; #2460 is the sweep that moved the
remaining `text string` + `cur int` pairs onto it and closed the paste/caret
gaps the epic's survey found. A row marked **`ui.Field` (#2460)** held its own
pair before this sweep; **paste routed (#2460)** / **caret added (#2460)**
close a gap the survey found without changing the storage shape.

### Overlays and shell prompts

| Site | File | Status |
| --- | --- | --- |
| Command palette query | `internal/palette/palette.go` | `ui.Field` (#2460) |
| Find-in-path / replace-in-path (query, replace, include, exclude) | `internal/finder/finder.go` | `ui.Field` (#2460) — one field per input, was one shared `cur` |
| Find in All Projects (query, include, exclude) | `internal/allfind/form.go` | `ui.Field` (#2460) |
| Editor command line (`:` `/` `?`) | `internal/editor/keys_command.go` | shared via `ui.EditKey` (#763, paste #1380); storage stays `cmdline string` + `cmdCur int` — 11 files read it, out of scope for #2460 (see below) |
| Editor find/replace panel (Find + Replace) | `internal/editor/replace_panel.go` | `ui.Field` (#2460) — was two `string`/`int` pairs |
| Save-as prompt | `internal/app/saveas.go` | `ui.Field` (#2460) |
| Clone dialog (URL + name) | `internal/app/clone_prompt.go` | `ui.Field` (#2460) |
| New-project wizard name | `internal/app/newproject_prompt.go` | `ui.Field` (#2460) |
| Save-layout prompt | `internal/app/layouts_ui.go` | `ui.Field` (#2460) |
| Bookmark prompt (note) | `internal/app/bookmarks_store.go` | `ui.Field` (#2460) |
| Regex tester pattern line | `internal/app/regextester.go` | `ui.Field` (#2460) — the multi-line test-text area (`text []string` + `line, col`) stays hand-rolled, a mini buffer rather than one field |
| jq/yq playground query | `internal/app/playground.go`, `playcheat.go`, `playcomplete.go`, `playfilters.go` | `ui.Field` (#2460); vertical motion over the wrapped rows on top (#2038) |
| jq/yq saved-filter name prompt | `internal/app/playfilters.go` | `ui.Field` (#2460) |
| File rename prompt | `internal/app/fileops.go` | `ui.Field` (#2460) |
| Symbol rename prompt (LSP) | `internal/app/lsprename.go` | `ui.Field` (#2460); paste now routed through `overlaypaste.go` |
| JetBrains keymap import path | `internal/app/jbimport_prompt.go` | `ui.Field` (#2460) |
| OpenAPI import path | `internal/app/openapi_import.go` | `ui.Field` (#2460) |
| HTTP response save path / cURL import | `internal/app/http_save.go`, `http_curl.go` | `ui.Field` (#2460); both now render through the shared `renderCompletionPrompt(ui.Field, …)` |
| Archive extract target path | `internal/app/archextract.go` | `ui.Field` (#2460) |
| Debugger evaluate-expression / run-to-line / pane-number prompts | `internal/app/debugeval.go`, `runtocursor.go`, `panenumbers.go` | `ui.Field` (#2460) |
| Deep-link paste-URL prompt | `internal/app/deeplink.go` | `ui.Field` (#2460) |
| Breakpoint properties form (condition, hit count, log message) | `internal/app/breakpoint_form.go` | `ui.Field` (#2460) — `[3]string`/`[3]int` became `[3]ui.Field` |
| Run-configuration env-row editor (key, value) | `internal/app/runconfig_form.go` | `ui.Field` (#2460) |
| Scratch rename / promote / custom-extension prompts | `internal/app/scratch_manager.go`, `scratch_promote.go`, `scratch_custom_ext.go` | `ui.Field` (#2460) |
| Test-data generator header cells (rows, seed, table) + save-template name | `internal/app/scratch_generate.go` | `ui.Field` (#2460) — the DSL editor's `lines []string` + `curL, curC` stays hand-rolled, a small multi-line buffer |
| Copy mode search (`/` `?` inside a terminal pane) | `internal/terminal/copymode.go` | `ui.Field` (#2460); paste routed through `terminal.Model.PasteText` |

### Panes and tool windows

| Site | File | Status |
| --- | --- | --- |
| Terminal scrollback search | `internal/terminal/search.go` | shared, on `ui.LineSearch` (#2461) |
| HTTP response search | `internal/httppane/httppane.go` | shared, on `ui.LineSearch` (#2461) |
| HTTP WebSocket session input line | `internal/httppane/websocket.go` | `ui.Field` (#2460) |
| Data viewer filter clause + export path | `internal/dataview/filter.go`, `export.go` | `ui.Field` (#2460) |
| DOM inspector selector | `internal/domview/domview.go` | `ui.Field` (#2460) |
| GitHub issues filter (match input) | `internal/ghissues/ghissues.go`, `filterov.go`, `qualifier.go`, `savedfilter.go` | `ui.Field` (#2460) |
| GitHub issues close/reopen comment prompt + PR merge/close comment stage | `internal/ghissues/mutations.go`, `prdetail.go` | `ui.Field` (#2460); **paste routed (#2460)** — `ghissues.PasteText` previously routed only to the filter, dropping a paste typed into the comment prompt |
| Explorer name prompt (new/rename), scratch rename | `internal/explorer/fileops.go`, `scratches.go` | `ui.Field` (#2460) |
| Explorer speed search | `internal/explorer/search.go` | shared, on `ui.SpeedSearch` |
| Picker type-ahead (issues pickers, action menu, scratch/bookmark/project lists) | `internal/ui/speedsearch.go` | shared, on `ui.Field` |
| Debug variables inline editor | `internal/debugpanel/debugpanel.go`, `watches.go` | `ui.Field` (#2460) — dropped the `[]rune(editBuf)` round-trip |
| Breakpoint refinement inline editor | `internal/breakpanel/meta.go` | `ui.Field` (#2460) — dropped the `[]rune(editBuf)` round-trip |
| Undo-tree time-jump age prompt | `internal/undotree/timejump.go` | `ui.Field` (#2460) with a digit-only guard in front of `Key` |
| List-pane filter rows (Usages, Problems, TODO index, Dependencies, Archive viewer, Time machine) | `internal/usages`, `internal/problems`, `internal/todoindex`, `internal/depspanel`, `internal/archview`, `internal/timepanel` | shared via `internal/filterbar.Model` (on `ui.Field`); **paste routed (#2460)** — none of the six wired `PasteText`, so a paste into any of their filter rows silently did nothing; wired through `app/inputcoalesce.go`'s pane-kind switch (five pane-hosted panels) and `app/overlaypaste.go` (the TODO index, an app-level overlay, not a pane) |

### Settings

| Site | File | Status |
| --- | --- | --- |
| Inline value editors (int/text/path/list) | `internal/settings/editor.go` | shared via `textField`, itself a `ui.Field` (#888, #2459) |
| Sub-panel forms (ES, tools, format, associations, debug map, LSP override, keymap import, venv wizard) | `internal/settings/*_form.go`, `migrated_panels.go`, `venv_wizard.go` | shared via `textField`; paste routed (#2002) |
| Colour page free-token input | `internal/settings/colors_page.go` | shared via `textField` |
| Toolchain custom path / package install | `internal/settings/toolchain_page.go` | shared via `textField`; paste routed (#2002) |
| Panel-wide filter | `internal/settings/panel.go` | shared via `filterKey`/`filterPaste` (#2002) |
| Colour page filter | `internal/settings/colors_page.go` | shared via `filterKey`/`filterPaste` (#2002) |
| Keymap page filter | `internal/settings/keymap_page.go` | shared via `filterKey`/`filterPaste` (#2002); the list-mode `backspace` shortcut hand-sliced the last rune off the end even after that — **fixed (#2460)** to go through `filterKey` like every other edit |
| Enum editor type-to-filter | `internal/settings/editor.go` | shared via `filterKey`/`filterPaste` (#2002) |

### Deliberately not migrated

| Site | File | Why |
| --- | --- | --- |
| Editor buffer insertion | `internal/editor/keys_insert.go` | Multi-line document, not a one-line field: insertion goes through the editor's own edit path (undo grouping, auto-indent, pair completion). |
| Editor command line storage (`cmdline`/`cmdCur`) | `internal/editor/keys_command.go` and 10 more files (`cmdline_complete.go`, `cmdline_paste.go`, `editor.go`, `keys_normal.go`, `keys_visual.go`, `logfilter.go`, `readonly.go`, `share.go`, `view.go`, plus the ex-command path) | Already edits through `ui.EditKey`/`ui.PasteText` (the behavioral goal of the sweep); wrapping the storage itself in `ui.Field` would touch selection-range and prefix-rewrite invariants (`\c`/`\C` case toggling, tab-completion, history walk) across all 11 files for no behavior change. Left as the two loose fields, same call as `internal/explorer/fileops.go`'s preselect branch below. |
| Regex tester / test-data generator multi-line areas | `internal/app/regextester.go` (`text []string`, `line, col`), `internal/app/scratch_generate.go` (`lines []string`, `curL, curC`) | A small hand-rolled multi-line buffer (per-line `ui.EditKey`, manual line split/join at the boundaries), not a single-line field — the single-line parts of both files (pattern line, header cells, save-name) are on `ui.Field`. |
| Terminal key encoding | `internal/terminal/model.go` | Keys are encoded for the pty; the shell inside owns its line editing (the emulator only maps the macOS chords to their readline equivalents, #733/#955). |
| Floating-shell live filter | `internal/ui/floating.go` | A type-to-filter over a scrollable view: arrows and page keys must stay scroll keys, so the filter is intentionally append-only with a rune-safe backspace. |
| Chord capture | `internal/settings/keys.go`, `migrated_panels.go` | Captures key chords, not text; backspace undoes one captured step. |

## The guard

`internal/ui/inputsweep_test.go` walks `internal/` and fails on the shapes a
hand-rolled input always has, outside `internal/ui` and an explicit allowlist:

- `x += key.Text` and the `key.Text != ""` guard fronting a hand-written
  insertion branch (the original #2002 check);
- `x = string(r[:cur]) + text + string(r[cur:])` (#2460) — a pasted block
  spliced into a rune slice by hand instead of `ui.PasteText`/`Field.Paste`,
  the shape `nbview.go` and `hexview.go` had before #2461 wired their search
  lines onto `ui.LineSearch`;
- `x = x[:len(x)-1]` (#2460) — a self-referential slice dropping the last
  rune, the shape a hand-rolled backspace takes — but only when `x`'s name
  hints at a text field (`Input`, `Query`, `Filter`, `Text`, `Find`, `Repl`,
  `Program`, `Pattern`). Go's `regexp` cannot backreference a capture group,
  so the check matches the hint loosely and then compares the two occurrences
  of the identifier in code. The hint keeps the same shape on a stack pop, an
  undo list's `ops` slice, or `parts`/`steps`/`segs` from ever tripping the
  guard — this check is for text fields, not every self-shrinking slice in
  the tree.

Each allowlist entry carries the reason it is exempt, and a second test fails
when an entry no longer matches any of the three patterns, so a stale
exemption cannot silently cover whatever that file grows next.

The check is deliberately crude. It is not a type system; it is the review
prompt that makes "why isn't this using `ui.EditKey`?" happen before the merge
instead of after the bug report.
