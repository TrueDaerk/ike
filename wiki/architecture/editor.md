---
type: concept
title: Editor
description: Vim-like modal editor pane built from buffer/mode/motion/operator/textobject/register/history/viewport/search sub-packages.
resource: internal/editor
tags: [architecture, editor, vim]
timestamp: 2026-07-06T00:00:00Z
---

# Editor

`editor.Model` is the text-editing pane. It owns a `*buffer.Buffer`, the cursor,
the current `mode.Mode`, and the supporting stores (registers, history,
viewport), and dispatches each key through the mode state machine. The engine is
split into focused sub-packages under `internal/editor/`; `editor.go` plus the
`keys_*.go` handlers wire them together.

## Sub-packages

- **buffer** — the text store: a line slice (`[]string`, never empty) with
  rune-aware `Position`/`Range` and a single primitive edit, `Apply(Edit)`, that
  replaces a range with text and returns the *inverse* edit (the basis of undo).
  It is the only place that maps rune columns to byte offsets.
- **mode** — the `Mode` enum (Normal, Insert, Visual, V-Line, V-Block,
  CommandLine, Replace) and the `Pending` operator/count/register sub-state.
- **motion** — motions return a target `Position` + a `Kind`
  (exclusive/inclusive/linewise): `h j k l`, `w b e` (+ `W B E`), `0 ^ $`,
  `gg G`, `{ }`, `f t F T` with `;`/`,`, and `%` bracket match.
- **textobject** — `iw aw` (and WORD), bracket pairs (`i( a( i{ …`, nesting and
  multi-line aware) and quotes (`i" a"`), resolved to a `Range`.
- **operator** — `d c y p` (+ `gp`), doubled `dd cc yy`, char/line-wise, with
  `Compose` turning a motion result into the operated `Target`. Writes the
  register store and records edits through a `history.Recorder`.
- **register** — unnamed `"`, named `"a`-`"z` (uppercase appends), yank `"0`,
  small-delete `"-`, the numbered ring `"1`-`"9`, and a system-clipboard seam
  (`"+`/`"*`, injected via `SetClipboard`).
- **history** — undo/redo as `Change` records (forward edits + inverses +
  cursor before/after); linear today, with parent/seq fields reserved for an
  undo tree. `.` repeat lives in the editor (`dotCommand`).
- **viewport** — vertical/horizontal scroll with `scroll_off`, plus the
  absolute/relative line-number gutter. The line renderer budgets by **display
  cells**, expanding each tab to `tab_width` spaces so a tabbed line's rendered
  width matches the terminal and stays inside its pane (a raw tab would be
  expanded by the terminal past the budget and wrap, pushing the pane's bottom
  border off screen).
- **search** — `/` `?` with `n`/`N`, literal by default, regex via a `\v`
  prefix; reports per-line match spans and the next match with wrap-around.
- **excmd** — parses the `:` line (`:w :q :wq :q! :e`, `:<n>` line jump) into a
  structured intent the editor executes.

## Modes & keys

Normal mode resolves an optional `"reg`, an optional count, an operator, and a
motion / text object before committing. Secondary-key states (`awaitG`,
`awaitFind`, `awaitReplace`, `awaitObject`) park the handler between keys.
Beyond the core motions it also binds `~` (toggle case), `*`/`#` (search the
word under the cursor), indent operators `>`/`<` (and `>>`/`<<`), `H M L`
(screen top/middle/bottom), and screen scrolling via `Ctrl-f/b` (page),
`Ctrl-d/u` (half page) and `PgUp`/`PgDn`. `Shift+←/→` (and `Ctrl+←/→`) are word
motions, `Shift+↑/↓` paragraph jumps — these work in normal, visual and insert.

Insert/Replace edits flow through one open `history.Recorder` so a whole insert
is a single undo unit; `Esc` commits it and records the `.`-repeat. Arrow keys,
`Home`/`End` and the word/page keys move the caret mid-insert. An `undo`/`redo`
requested mid-insert (e.g. `Ctrl+Z` while typing) first **commits the open
insert session**, so it reverts the whole typed run as one unit and behaves
identically from insert and normal mode.

Visual, V-Line and V-Block extend a selection that `View` highlights cell by
cell (the cursor wins on overlap); motions and `i`/`a` text objects grow it, and
`d c y` `>` `<` and `p` (replace selection from a register) consume it.

Mouse: clicking the editor focuses it and `MouseClick` maps the cell — through
the gutter width and scroll offsets — to the cursor. The wheel scrolls the
viewport via `ScrollBy(delta)`, which moves `view.Top` directly (clamped to the
buffer) without touching the cursor or mode — it works the same in Normal,
Insert, Visual, etc., unlike the vim-motion scroll commands.

## Config

`Configure(host.Config)` retains the config reference and `applyConfig` re-reads
the `[editor]` section on every event, so `tab_width`, `use_spaces`,
`auto_indent`, `trim_trailing_whitespace`, `insert_final_newline`,
`line_numbers`, `relative_line_numbers` and `scroll_off` take effect live.

## Registry bridge & LSP seam

`commands.go` registers editor actions and ex-commands as plugin `Command`s
(`editor.write`, `editor.quit`, `editor.write_quit`, `editor.undo`,
`editor.redo`). Each `Run` dispatches an `ActionMsg`, which the root routes back
into the focused editor's `Update` — the single dispatch path the palette (07)
and keybindings (08) reach. `events.go` emits on-change / cursor-move /
completion-trigger `Event`s through an injectable `Emitter` (nil by default); no
language intelligence lives here (Roadmap 0100).
