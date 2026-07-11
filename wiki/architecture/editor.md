---
type: concept
title: Editor
description: Vim-like modal editor pane built from buffer/mode/motion/operator/textobject/register/history/viewport/search sub-packages.
resource: internal/editor
tags: [architecture, editor, vim]
timestamp: 2026-07-11T00:00:00Z
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
  (`"+`/`"*`, injected via `SetClipboard`). `internal/clipboard` provides the
  real implementation (pbcopy/pbpaste on macOS, wl-copy/xclip/xsel on
  Linux/BSD), wired in by the pane registry when an editor is created; without
  a utility on PATH the registers fall back to the built-in no-op clipboard.
  `Cmd+C/X/V` (keymap commands `editor.copy/cut/paste`) yank / delete the
  visual selection — or the current line without one — through `"+`, and paste
  from it (mid-insert the paste joins the open insert session's undo unit).
  Copy/cut answer with a feedback toast ("copied 3 lines", "cut 12 chars",
  #252) via `NoticeMsg`; the vim-native `y`/`d` flows stay silent. Saves
  report on the ex line (#261): `"file" written` on success, `E: <error>`
  on failure (read-only file, no file name) — a failed write keeps the
  buffer dirty and aborts `:wq`.
- **history** — undo/redo as `Change` records (forward edits + inverses +
  cursor before/after); linear today, with parent/seq fields reserved for an
  undo tree. `u`/`ctrl+r` take a count (`3u` undoes three changes, stopping
  early when the history runs out, #231). `.` repeat lives in the editor
  (`dotCommand`). The history pins a **save checkpoint** (`MarkSaved`/`AtSaved`,
  #251): saving pins the current state, and undo/redo clear the dirty flag when
  they land exactly on it (vim-style), so `[+]` goes away when you undo back to
  the saved content. A crash-restored buffer marks the checkpoint unreachable —
  no undo depth makes it read as clean.
- **viewport** — vertical/horizontal scroll with `scroll_off`, plus the
  absolute/relative line-number gutter. The line renderer budgets by **display
  cells**, expanding each tab to `tab_width` spaces so a tabbed line's rendered
  width matches the terminal and stays inside its pane (a raw tab would be
  expanded by the terminal past the budget and wrap, pushing the pane's bottom
  border off screen).
- **search** — `/` `?` with `n`/`N`, literal by default, regex via a `\v`
  prefix; reports per-line match spans and the next match with wrap-around.
  Matching is **smartcase** (#257): an all-lowercase pattern is
  case-insensitive, any uppercase rune makes it exact; `*`/`#` always match
  the word exactly, and `:s` keeps its own explicit `i`/`I` flags.
  The input line is **incremental** (#255): each keystroke recompiles the
  pattern, jumps to the nearest match from the search origin and shows a live
  counter ("3/17", "no matches") on the `/` line; Esc restores cursor and
  viewport exactly, Enter commits (zero matches / a wrapped landing leave
  "no matches: pat" / "search wrapped" on the ex line, as do wrapping
  `n`/`N`). All matches of the active query render with a background
  highlight, the current match additionally underlined; a normal-mode Esc
  clears the highlights (`:noh`-style) and `/`, `n`/`N`, `*`/`#` re-arm them.
  `cmd+f` (`editor.find`) opens the same `/` line — one engine, no divergent
  find UI.
- **excmd** — parses the `:` line into a typed `Command{Range, Name, Bang, Args}`
  AST and resolves its range. The grammar is `[range] name[!] [args]`: a range is
  one or two comma-separated *addresses* (or `%` = whole file), and an address is
  a base — a line number, `.` (current), `$` (last), `'<` / `'>` (visual bounds),
  `/pat/` or `?pat?` (pattern search) — plus an optional signed offset (`.+2`,
  `$-1`). `Parse` is pure; `Range.Resolve` maps addresses onto 0-based buffer
  lines given a `Resolver` (cursor line, visual bounds, a line-search hook). The
  editor executes recognised names (`:w :q :wq :q! :e`, plus a bare range as a
  line jump); `:g` / `:v` / `:s` are reserved and report *not implemented*. See
  [command line](#command-line-ex-commands-roadmap-0200).

## Modes & keys

Normal mode resolves an optional `"reg`, an optional count, an operator, and a
motion / text object before committing. Secondary-key states (`awaitG`,
`awaitFind`, `awaitReplace`, `awaitObject`) park the handler between keys.
Visual mode accumulates counts with the same 1–9/continuing-0 rule (#265), so
`V3j` extends the selection three lines and `3G` jumps inside a selection;
the count is consumed by its motion and Esc clears the pending state.
Beyond the core motions it also binds `~` (toggle case), `*`/`#` (search the
word under the cursor), indent operators `>`/`<` (and `>>`/`<<`), `H M L`
(screen top/middle/bottom), and screen scrolling via `Ctrl-f/b` (page),
`Ctrl-d/u` (half page) and `PgUp`/`PgDn`. `Alt/Option+←/→` (and `Ctrl+←/→`) are
word motions clamped to the current line (#303) — `.` inside identifiers counts
as a stop point (`config.editor.tabWidth` yields sub-word stops), and past the
first/last word the caret lands on the line start/end instead of crossing
lines; cross-line word motion stays on vim `w`/`b`/`e`. `Alt+↑/↓` (and
`Ctrl+↑/↓`) are paragraph jumps — all of these work in normal, visual and
insert. `Shift+arrows` (plus `Shift+Home/End`) are selection keys: in normal
mode they enter charwise visual mode anchored at the cursor and move; in visual
mode they extend the selection like their plain counterparts.
`Shift+Alt/Option+←/→` (and `Shift+Ctrl+←/→`) extend the selection by the same
in-line word motion; mid-insert they just move the caret.

Insert/Replace edits flow through one open `history.Recorder` so a whole insert
is a single undo unit; `Esc` commits it and records the `.`-repeat. Arrow keys,
`Home`/`End` and the word/page keys move the caret mid-insert. Backward kills
work mid-insert too (#246), mirroring the terminal pane's macOS convention:
`option+backspace` / `ctrl+w` delete the previous word, `cmd+backspace` /
`ctrl+u` delete to the line start — all inside the open insert's undo unit. An `undo`/`redo`
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
Insert, Visual, etc., unlike the vim-motion scroll commands. Horizontal wheel
(or shift+wheel) scrolls sideways via `ScrollXBy(delta)`, moving `view.Left`
clamped so the longest visible line keeps its last character on screen (#230);
the next cursor motion re-derives the offset to follow the cursor again.

## Command line (ex commands, Roadmap 0200)

`:` opens the command line (`keys_command.go`). On `Enter`, `runExLine` calls
`excmd.Parse`, which returns a typed `Command{Range, Name, Bang, Args}`. Parsing
is pure and table-tested; execution stays in the editor model, which maps a
`Name` onto its save / close / open actions.

The grammar is `[range] name[!] [args]`:

- **Ranges** are one or two comma-separated addresses, or `%` (the whole file).
  An **address** is a base plus an optional signed offset: line number `N`, `.`
  (current line), `$` (last line), `'<` / `'>` (the last visual selection's first
  / last line), and pattern searches `/pat/` (next matching line) and `?pat?`
  (previous). Offsets stack: `.+2`, `$-1`, `.-2,.+2`.
- **One resolver** (`Range.Resolve`) turns any command's range into a 0-based
  `[start, end]` span. It consults an `excmd.Resolver` the editor fills from live
  state — cursor line, visual bounds, and `exSearchLine` (a regex line search
  that wraps around the buffer) — clamps to the buffer, and swaps a reversed
  span. A bare range with no name (`:42`, `:1,5`, `:$`) jumps to the range's last
  line.
- **Entering `:` from Visual** pre-fills `'<,'>` and records the selection bounds,
  matching vim; those bounds back the `'<` / `'>` addresses.
- **Errors:** unknown names and unresolvable addresses (missing selection,
  pattern not found) surface a transient `E:` message on the command-line row
  (`m.cmdMsg`), cleared by the next normal-mode key. `:g` / `:v` (global) parse
  but report *not implemented yet* (a later Roadmap 0200 sub-issue).

### `:substitute` (`substitute.go`)

`:[range]s/pat/repl/[flags]` rewrites lines over the resolved range (default: the
current line). `editor.replace` (`cmd+r`, leader `R`; Epic 0240, #283) fronts
this same engine with a **two-field panel** (`replace_panel.go`) rendered as
the pane's bottom rows: Find (seeded from the committed literal search,
driving the incremental-search preview — live highlight, match tally, jump to
the nearest match) and Replace, `tab` switching fields. `ctrl+a` runs
`%s/find/repl/g` (replace all, the engine's "N substitutions" report), `enter`
runs the `gc` variant and hands over to the y/n/a/q/l confirm flow — exactly
replace-current / skip / all with one undo unit — and `esc` cancels with
nothing mutated, restoring cursor and viewport. The delimiter is picked to
avoid both fields, so slashes need no escaping; the panel (like the confirm
prompt) counts as *capturing*, so global plain keys (`tab` pane cycle) never
steal its input. The pattern follows the search-layer convention — literal by
default, `\v` prefix for regex — so `:s//bar/` reuses the last search (then the
last substitute) as its pattern. Any non-alphanumeric delimiter works
(`:s#a#b#`), and `\<delim>` is a literal delimiter.

- **Flags:** `g` (every match per line, not just the first), `i` / `I`
  (case-insensitive / -sensitive), `n` (report the count without changing
  anything), `c` (confirm each match interactively — see below). An unknown flag
  is an error.
- **Replacement** is vim-style: `&` / `\0` is the whole match, `\1`-`\9` the
  capture groups, `\&` and `\\` literal `&` / `\` (Go's `$name` syntax is *not*
  used — `$` is literal).
- **One undo unit:** all replacements of one invocation are applied inside a
  single `mutate`, so a single `u` reverts the whole run; the cursor lands on the
  last changed line. A bare `:s` (optionally with a range) repeats the last
  substitute. The outcome is reported as *N substitutions on M lines*.

### Confirm mode (`substitute_confirm.go`)

The `c` flag (`:s/pat/repl/gc`) turns substitution into an interactive walk
instead of a batch replace. It runs as a sub-state of the mode machine — no new
pane — driven by `m.subConfirm`: the editor precomputes every match over the
range, jumps to the first with it highlighted (reusing the selection highlight),
and shows `replace (y/n/a/q/l)?` on the command-line row.

- `y` replaces and advances, `n` skips, `a` replaces this and every remaining
  match, `q` quits, `l` replaces this one then quits; `Esc` cancels. Any other
  key waits.
- Accepted replacements accumulate in one open `history.Recorder`, so the whole
  interaction is a **single undo unit** and cancelling keeps what was already
  applied. A per-line rune-column delta maps each precomputed match's original
  span onto the shifted buffer, so multiple matches on one line stay aligned as
  earlier replacements change the line's length.

### Range companions (`excmd_ops.go`)

Line-range commands that reuse the existing operator / register / indent logic,
each over the shared resolver and each a single undo unit:

- `:[range]d [reg]` deletes the range's lines into a register (unnamed by
  default), leaving the cursor on the line that takes the range's place — the
  `dd` cursor rule.
- `:[range]y [reg]` yanks into a register; like vim, the cursor does not move.
- `:[range]>` / `:[range]<` indent / outdent through the same tab-unit / dedent
  logic as the normal-mode `>`/`<` operators; a repeated verb (`:>>`) shifts that
  many times, and the cursor lands on the range's last line at its first
  non-blank.

## Comment toggling (Roadmap 0120)

`editor.commentLine` (cmd+7, alias `cmd+k cmd+c`) toggles the language's line
comment — resolved per buffer path via `lang.Comments` — on the current line or
every line of the visual selection, JetBrains-style (`comment.go`):

- Markers land at the range's **minimal indent**; blank lines are skipped.
- A **mixed** range comments its uncommented lines; a fully commented range
  uncomments.
- A single-line toggle advances the cursor one line; a selection is preserved
  (visual mode stays active).
- One undo unit, `.`-repeatable; an open insert session commits first.
- A buffer without comment syntax is a no-op that raises an info toast via
  `editor.NoticeMsg` (the editor stays host-free; the root model notifies).

`editor.commentBlock` (cmd+shift+7) wraps in the language's block pair:

- A **charwise** selection wraps inline (`/* sel */`); toggling an exactly
  wrapped selection unwraps it (one replace edit).
- A **linewise** selection — or the current line — gets marker lines above and
  below at the first line's indent; selecting a block whose first/last lines
  are exactly the markers removes the pair.
- Languages without a block pair (python) fall back to line-comment toggling.
- One undo unit, `.`-repeatable; visual mode ends after the toggle.

## External file changes (Roadmap 0140)

The watcher service (`internal/watch`, see [foundation](./foundation.md))
reports external changes as `watch.EventMsg`s that the root model routes to the
editor leaf owning the path. `reload.go` consumes them: a **clean** buffer whose
file changed on disk (kinds `FileChanged` and `FileCreated` — a write-temp-and-
rename save coalesces to the latter) is reloaded in place. Cursor and scroll are
preserved, clamped to the new content exactly like session restore
(`SetCursor` + `SetScroll`). The reload emits `EventChange`, so `docVersion`
bumps, Tree-sitter reparses, and the LSP bridge sends the new text — identical
to an ordinary edit. **Undo history restarts on reload**: the old change records
describe positions in text that no longer exists, so replaying them could
corrupt the buffer; losing the stack is the documented trade-off.

A **dirty** buffer is never silently reloaded (#82): the external change marks
it *stale* (`Stale()`), shown as `!` after the tab title's dirty `*` and a
`[disk changed]` status-line segment. Every save entry point (`:w`,
`editor.write`, save-all) goes through `saveGuarded`: saving a stale buffer to
its own file yields an `editor.ConflictMsg` instead of writing (`:w other`
bypasses the guard — a different path clobbers nothing). The root model answers
it with a floating prompt (`internal/app/conflict.go`): **keep mine** (`k`,
force-save — clears staleness; the save event stamps the watcher's epoch so the
overwrite doesn't echo back), **reload** (`r`, discard edits via the
clean-reload path; local history #35 will snapshot before the discard once it
lands), or **cancel** (`esc`, buffer stays dirty + stale). A 'show diff' choice
joins once the diff viewer (#60) exists.

External **deletes** (#83): the root model closes a clean editor whose file was
removed (the explorer's delete-closes-editor flow); a dirty one survives with
its buffer as the only copy, marked stale so the next save prompts. A
`FileRemoved` whose path still exists (replace-in-place: write temp + rename,
git checkout) is downgraded to a content change and reloads normally.

Config: `files.auto_reload = clean|never` (default `clean`; affects clean
buffers only — stale marking is unconditional).

## Auto-save (#174)

With `editor.auto_save = focus` (the default; `off` disables, an `idle` mode
is reserved for #54), a dirty buffer saves itself when focus leaves its pane
— every focus transition funnels through the root model's `setFocus`, so one
hook covers Ctrl+arrows, the pane switcher, mouse clicks and the explorer
toggle — and when its document is about to be replaced by opening another
file into the pane. `editor.Autosave` goes through the normal `saveAs` path:
`EventSave` fires (watcher epoch, LSP didSave, shared-view sync), and **undo
history is untouched** — returning to the pane, undo/redo work as usual, and
an undo past the saved state re-dirties the buffer so the next blur persists
it. A **stale** buffer is never auto-saved: it stays dirty for the explicit-
save conflict prompt above. Cmd+S remains the explicit save.

## Shared documents (#142)

Two editor panes showing the same file are two **views of one document**
(JetBrains/vim-split semantics), not divergent copies. `share.go`:

- Opening a path another pane already shows makes the new pane a second view
  via `ShareDocumentWith`: `*buffer.Buffer` and `*history.History` are aliased
  (one text, one undo stack), while cursor, scroll, mode, and registers stay
  per pane. Session restore deduplicates the same way.
- After an edit, undo, save, or reload in one view, the emitter adapter (which
  knows its pane key) broadcasts `editor.SyncMsg{Path, FromKey, Dirty, Stale}`
  through `host.Send`; the root model routes it to every *other* pane showing
  the path. Receivers clamp cursor/scroll into the mutated buffer, mirror the
  dirty/stale flags, bump `docVersion` and reparse — no text is copied, the
  buffer is shared. `applySync` never re-emits, so syncs cannot ping-pong.
- External reload mutates the document **in place** (`Buffer.ReplaceAll`,
  `History.Reset`) so the aliases survive; async per-path messages (highlight
  spans, LSP results, watch events) route to **all** panes owning the path
  (`editorKeysForPath`), each filtering by its own document version.
- Known edge: `:e` inside a pane loads a fresh copy and leaves any prior
  sharing (it re-points that pane's document); `:w otherfile` re-targets only
  the saving view's path.

## Config

`Configure(host.Config)` retains the config reference and `applyConfig` re-reads
the `[editor]` section on every event, so `tab_width`, `use_spaces`,
`auto_indent`, `trim_trailing_whitespace`, `insert_final_newline`,
`line_numbers`, `relative_line_numbers` and `scroll_off` take effect live.

## Registry bridge & LSP seam

`commands.go` registers editor actions and ex-commands as plugin `Command`s
(`editor.write`, `editor.quit`, `editor.write_quit`, `editor.undo`,
`editor.redo`, `editor.copy`, `editor.cut`, `editor.paste`, `editor.lineStart`,
`editor.lineEnd`). Each `Run` dispatches an `ActionMsg`, which the root routes back
into the focused editor's `Update` — the single dispatch path the palette (07)
and keybindings (08) reach. `events.go` emits on-change / cursor-move /
completion-trigger `Event`s through an injectable `Emitter` (nil by default); no
language intelligence lives here (Roadmap 0100).
