---
type: concept
title: Editor
description: Vim-like modal editor pane built from buffer/mode/motion/operator/textobject/register/history/viewport/search sub-packages.
resource: internal/editor
tags: [architecture, editor, vim]
timestamp: 2026-07-24T22:00:00Z
---

# Editor

`editor.Model` is the text-editing pane. It owns a `*buffer.Buffer`, the cursor,
the current `mode.Mode`, and the supporting stores (registers, history,
viewport), and dispatches each key through the mode state machine. The engine is
split into focused sub-packages under `internal/editor/`; `editor.go` plus the
`keys_*.go` handlers wire them together.


## Render caching (#614)

`View()` renders the visible window line by line; the per-line body (`renderSpan`
— syntax highlight, selection, search, inlay, whitespace) is the expensive part.
It is memoized in a per-view line cache (`linecache.go`) keyed by
`(line, from, to, width)` and guarded by `renderEpoch`: a counter bumped on every
mutation that can change a body — edits, cursor/selection moves, resize,
horizontal scroll, focus, theme/config, and (via the `Update` choke point) every
decoration message (syntax, semantic, diagnostics, git marks, occurrences, inlay
hints). A **vertical** scroll deliberately does not bump it (`renderSpan` never
reads `view.Top`), so scrolling reuses cached bodies instead of re-highlighting
every visible line. The gutter (line numbers, diagnostic/git/breakpoint/paused
signs) renders fresh each frame, so those decorations can never go stale from the
cache. The cache is per-view: `New` and `ShareDocumentWith` each install a fresh
one so split views of a shared document (#142) never collide.

The gutter's sign column also carries the **test run marker** (#1150): a `▶`
in the success tone on every detected test declaration (`testmarks.go` —
detection via the language registry's `lang.TestSpec` regex seam, cached per
document version in a per-view pointer store like the line cache, so the scan
runs at most once per edit, never per frame). Sign precedence: debugger paused
`▶` > breakpoint `●` > bookmark `⚑` (#1151, accent tone — vim marks, see
"Vim marks & bookmarks") > test `▶` > diagnostic/git colouring. A plain gutter
click still toggles the breakpoint on every line; ctrl/cmd+click on a marker
line runs that test (see /architecture/run-configurations.md).

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
  A **bracketed paste** from the terminal (external text) arrives as one
  `tea.PasteMsg`; the app routes it to the focused editor's `PasteText`, which
  inserts the whole block as a single edit and one undo unit — visual mode
  replaces the selection, mid-insert it splices in, normal mode pastes after the
  cursor like `p` — without touching the yank registers or system clipboard
  (#603). A modal overlay owning the keyboard suppresses the route; a focused
  terminal pane gets the block through its own bracketed-paste path.
  Copy/cut answer with a feedback toast ("copied 3 lines", "cut 12 chars",
  #252) via `NoticeMsg`; the vim-native `y`/`d` flows stay silent.
  Every yank/delete also feeds a bounded 20-entry **history** (#57,
  `Store.History`, consecutive duplicates collapse); `editor.pasteFromHistory`
  (`cmd+shift+v`, Edit menu) opens a palette picker over it — first line +
  size per row, fuzzy filter — and the chosen entry becomes the current
  clipboard and pastes with exact Cmd+V semantics (JetBrains Paste from
  History). Saves
  report on the ex line (#261): `"file" written` on success, `E: <error>`
  on failure (read-only file, no file name) — a failed write keeps the
  buffer dirty and aborts `:wq`.
- **history** — undo/redo as `Change` records (forward edits + inverses +
  cursor before/after + timestamp), stored as an **undo tree** (#59, vim's
  undotree): every state ever reached is a node keyed by its global `seq`; an
  edit after an undo becomes a sibling branch instead of discarding the redo
  chain, and `u`/`ctrl+r` walk the *active branch* so the default feel stays
  linear. `g-`/`g+` step **chronologically** across branches (global seq
  order), `JumpTo` restores any node by applying inverses up to the common
  ancestor and forwards down to the target, and `Tree()` exposes the nodes for
  the **undo-tree overlay** (`internal/undotree`, palette `editor.undoTree`):
  a centered view of the change tree — newest first, abandoned branches
  indented, current/saved states marked — where `j`/`k` move and `enter`
  restores the selected state (the overlay stays open and refreshes, esc
  closes). A per-buffer cap (1000 nodes) prunes oldest leaf branches first; a
  purely linear history over the cap drops its oldest level, vim's
  `undolevels`. `u`/`ctrl+r` take a count (`3u` undoes three changes, stopping
  early when the history runs out, #231). `.` repeat lives in the editor
  (`dotCommand`). The history pins a **save checkpoint** (`MarkSaved`/`AtSaved`,
  #251): saving pins the current state, and undo/redo clear the dirty flag when
  they land exactly on it (vim-style), so `[+]` goes away when you undo back to
  the saved content. A crash-restored buffer marks the checkpoint unreachable —
  no undo depth makes it read as clean.
  **Persistent undo** (#148, vim's `undofile`): the tree survives a restart.
  `internal/undostore` keeps one JSON file per document under the state store
  (`.ike/undo/`, or `IKE_CONFIG_DIR/undo`), keyed by a hash of the absolute
  path and stamped with the content hash the tree describes (pre-tree
  `past`/`future` snapshots still restore as a degenerate chain). The editor
  writes it after every save and the app layer on tab/pane close and quit
  (dirty buffers are skipped — the last save's undo file still matches the
  disk content); on `Load` the stacks are adopted only when the stored hash
  matches the just-read content, so any external change (git checkout,
  another editor) discards them silently — correctness over continuity,
  mirroring the 0140 reload trade-off. Views of a shared document (#142)
  alias one history, loaded once by the first view; the adoption hash travels
  with the document (copied on share, mirrored via `SyncMsg`). Large-file
  mode (#149) opts out — load stays flat, no content hashing.
  `files.persistent_undo` (default `true`) switches it off; a 1 MiB per-file
  cap and a 200-file LRU prune bound the store.
  A flagged large file additionally shows a **persistent, dismissible banner**
  over the pane's first content row while focused (#1124): it names the cause
  and both remedies — a click runs `editor.forceCodeInsight`, the `✕` (or
  esc) dismisses per document, and the thresholds are editable in Settings →
  Files (`files.large_file_kb` / `files.large_file_lines`, 0 = guard off,
  #1125).
- **viewport** — vertical/horizontal scroll with `scroll_off`, plus the
  absolute/relative line-number gutter. The line renderer budgets by **display
  cells**, expanding each tab to `tab_width` spaces so a tabbed line's rendered
  width matches the terminal and stays inside its pane (a raw tab would be
  expanded by the terminal past the budget and wrap, pushing the pane's bottom
  border off screen). **Soft wrap** (#64, `editor.wrap` or `view.toggleWrap`)
  replaces horizontal scroll with a visual-row map: `viewport/wrap.go` splits
  each line into wrap segments by the same cell budget (`WrapSegments` /
  `SegmentIndex`), `ScrollWrapped` follows the cursor in visual rows (folds
  count 0 rows hidden / 1 row header), continuation rows carry a `↪` gutter
  marker, `j`/`k` move one visual row (vim's `gj`/`gk`; the motion is charwise,
  fold-aware, in `editor/wrap.go`), and mouse clicks map through the segment
  list (`wrapClickAt`). `ScrollXBy` is a no-op and `Left` pins to 0 while wrap
  is on; a single line taller than the window pins `Top` on it (vim's `@@@`
  case). Overlay anchors (LSP popups) go through `DisplayRow`/`DisplayOffset`,
  which count wrap segments and folds. Render-only view options (#64) overlay
  in the same span renderer: **visible whitespace** (`editor.show_whitespace =
  none|trailing|all` or `view.toggleWhitespace`; dim `·` for spaces, `→` for
  tabs, `trailing` marks only the line-end run), **indent guides**
  (`editor.indent_guides` or `view.toggleIndentGuides`; `│` on whitespace
  cells at each `tab_width` stop inside the leading indent — visible
  whitespace wins on overlap) and **column rulers** (`editor.rulers = [80]`;
  a background tint on those display columns, padded past short lines). The
  palette toggles override the config per view; theme slots `Whitespace`,
  `IndentGuide`, `Ruler` colour them. **Sticky scroll** (#168) pins the header lines of the
  declarations enclosing the first visible line as the top rows of the pane
  (JetBrains-style): the scopes come from the same Tree-sitter parse that
  produces the highlight spans (`highlight.HighlightScoped`, node kinds per
  language via `lang.Language.ScopeNodes`), `sticky.go` resolves which headers
  pin for the current `view.Top` (a fixed point, since pinned rows cover
  content and move the reference line down; capped by `sticky_scroll_depth`,
  innermost win), scrolling keeps the cursor from hiding behind the pinned
  rows, and a mouse click on a pinned row jumps to its declaration.
  **Code folding** (#144) collapses the body of a function, block, import
  list or multi-line comment behind its header line: the foldable ranges come
  from the same parse (`SpansMsg.Folds`, node kinds per language via
  `lang.Language.FoldNodes`, falling back to `ScopeNodes`), and `fold.go`
  owns the per-view collapsed set (`folded`, header line → end line — views
  of a shared document fold independently, like the cursor). A collapsed
  fold renders as one row — the header plus a dimmed `⋯ N lines` placeholder
  — and counts as one row for `j`/`k` (and counts), mouse clicks and wheel
  scrolling. Jumping *into* a fold (search landing, `G`, go-to-definition)
  auto-unfolds it via the `scroll()` choke point; an edit landing in a fold
  dissolves it, edits above shift it, and every accepted reparse reconciles
  the collapsed set against the fresh ranges (version-gated like the spans).
  Keys: `za` toggle, `zc`/`zo` close/open (repeated `zc` closes outward),
  `zM`/`zR` close/open all; the same operations are palette commands
  (`editor.fold.*`).
- **search** — `/` `?` with `n`/`N`, literal by default, regex via a `\v`
  prefix; reports per-line match spans and the next match with wrap-around.
  Case handling (#257, #1111): a `\c` query prefix forces case-insensitive
  matching, `\C` forces exact matching; without a marker the
  `editor.search_ignore_case` setting (off by default, Settings → Editor)
  makes every query case-insensitive, and with the setting off **smartcase**
  applies — an all-lowercase pattern is case-insensitive, any uppercase rune
  makes it exact. Precedence: marker > setting > smartcase. While the search
  line is open, **ctrl+c toggles** the case mode for the current query by
  rewriting the visible marker (the marker *is* the state display): unmarked →
  `\c` (setting off) or `\C` (setting on), `\c` ↔ the sensitive side, `\C` →
  `\c`. `*`/`#` always match the word exactly, and `:s` keeps its own
  explicit `i`/`I` flags. The `\v`/`\c`/`\C` markers compose in any order at
  the start of the query.
  The input line is **incremental** (#255): each keystroke recompiles the
  pattern, jumps to the nearest match from the search origin and shows a live
  counter ("3/17", "no matches") on the `/` line; Esc restores cursor and
  viewport exactly, Enter commits (zero matches / a wrapped landing leave
  "no matches: pat" / "search wrapped" on the ex line, as do wrapping
  `n`/`N`). All matches of the active query render with a background
  highlight, the current match additionally underlined; a normal-mode Esc
  clears the highlights (`:noh`-style) and `/`, `n`/`N`, `*`/`#` re-arm them.
  `cmd+f` (`editor.find`) opens the same `/` line — one engine, no divergent
  find UI. The `/` `?` and `:` lines share the single-line editing helper
  (`internal/ui.EditKey`, #763, #1110): left/right move the cursor, typing
  inserts at it, alt+backspace deletes the previous word, cmd+backspace
  clears the line, and the incremental preview keeps tracking mid-query
  edits.
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
`awaitFind`, `awaitReplace`, `awaitObject`, `awaitRecordReg`, `awaitPlayReg`,
and the mark states `awaitMark` / `awaitMarkLine` / `awaitMarkExact`, #1151)
park the handler between keys.
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
in-line word motion; mid-insert they just move the caret. A selection started
this way is GUI-style (vim's `keymodel=stopsel`, #326): releasing Shift and
pressing an unshifted navigation key (arrows, `Home`/`End`, word/paragraph and
page keys) drops the selection and just moves the caret, while vim motions and
selections entered with `v`/`V`/`Ctrl+V` keep extending as in vim.

Insert/Replace edits flow through one open `history.Recorder` so a whole insert
is a single undo unit; `Esc` commits it and records the `.`-repeat. Arrow keys,
`Home`/`End` and the word/page keys move the caret mid-insert. Backward kills
work mid-insert too (#246), mirroring the terminal pane's macOS convention:
`option+backspace` / `ctrl+w` delete the previous word, `ctrl+u` deletes to
the line start, and `cmd+backspace` is IntelliJ's Delete Line (#955): the
whole current line goes, including the preceding line break (on line 0 the
following break instead), landing the caret at the end of the previous line —
all inside the open insert's undo unit. An `undo`/`redo`
requested mid-insert (e.g. `Ctrl+Z` while typing) first **commits the open
insert session**, so it reverts the whole typed run as one unit and behaves
identically from insert and normal mode.

Smart indentation (Roadmap 0260, `indent.go`): with `editor.auto_indent` on,
`Enter` in insert mode and `o` compute the new line's indent from the
language's block openers (`lang.IndentAfter`, e.g. `:` for Python, `{ ( [` for
Go/PHP) — the reference text's leading whitespace, plus one `tabText()` unit
(honouring `use_spaces`/`tab_width`) when its trimmed form ends with an opener.
`Enter` keys off the part of the line **left of the cursor** (a mid-line split
indents by what stays behind); `o` uses the whole current line; `O` and
languages without rules keep plain copy-indent. Pure text heuristic — no
Tree-sitter — so an opener ending a trailing string literal false-positives;
accepted for v1. Mid-insert, plain `Tab` inserts one indent unit at the cursor
and `Shift+Tab` dedents the **whole current line** by one unit (the same
`dedentCols` unit as `<<` — one leading tab or up to `tab_width` spaces),
wherever the cursor sits; the cursor follows the removed columns, and the edit
stays inside the open insert's undo unit. While the completion popup is open a
plain `Tab` still accepts the completion; `Shift+Tab` dedents regardless.
`Enter` with the caret **between a matching bracket pair** (`{|}`, typically
right after an auto-close) opens a three-line block (#518): the closer moves to
its own line at the reference line's indent and the caret lands on the
smart-indented middle line. Gated on `editor.auto_indent`, per caret; without
language rules (plain text) the middle line keeps the copy-indent.

Auto-closing pairs (#517, `autoclose.go`): with `editor.auto_close_pairs` on
(default), typing `(`, `[` or `{` in insert mode also inserts the matching
closer and leaves the cursor between the pair — in every file type, no
language rules involved. The closer is only added when sensible: the cursor
sits at the line end, before whitespace, or before another closer; directly
before other text the opener inserts alone. Typing a closer whose rune already
sits at the cursor **skips over** it instead of duplicating it, and backspacing
the opener of an empty pair removes both runes. Quotes (`"`, `'`, `` ` ``, #521)
pair under the same gate with symmetric rules: the same quote at the cursor is
skipped (that is the closing keystroke), and no pair opens when the rune before
the caret is a word rune or the same quote — so the apostrophe in `don't` and
doubled quotes insert alone. Everything applies per caret
(one fan-out can mix pairing, plain insert, and skip-over) and stays inside the
open insert's undo unit. The `.`-replay text records only the keystrokes, so a
fully typed `(x)` run replays exactly; an insert that never types the closer
replays without it (same approximation as backspace).

**Macros** (#58, `macro.go`): `q{a-z}` records, `q` stops, `@{a-z}` replays,
`@@` repeats the last replay, and a count multiplies (`5@a`). Recording taps
every keypress in `Update` *before* mode dispatch, so inserts, visual
selections and ex commands are captured alike; the payload is the keystroke
list itself (`macros map[rune][]tea.KeyPressMsg`), kept per view beside the
register store rather than in it (registers hold text). Replay feeds the
recorded keys back through `Update` synchronously; a `replayDepth` counter
keeps replayed keys out of an active recording (a macro replayed while
recording stores the literal `@x`, vim-style) and caps nesting at 100 — the
recursion guard for self-invoking macros, since there is no vim-style
stop-on-error. A `q` arriving from a replay neither stops nor starts a
recording, and `q` as a pending find/replace target stays literal. While
recording, the status line shows a `recording @x` segment
(see [status-line](./status-line.md)).

Visual, V-Line and V-Block extend a selection that `View` highlights cell by
cell (the cursor wins on overlap); motions and `i`/`a` text objects grow it, and
`d c y` `>` `<` and `p` (replace selection from a register) consume it.
Backspace/Delete also remove the selection outright (#979, GUI style — they are
not the vim left-motion here); a selection entered from insert/replace mode
(mouse selection while editing) then returns to insert mode at the deletion
point so typing continues seamlessly.

Mouse: clicking the editor focuses it and `MouseClick` maps the cell — through
the gutter width and scroll offsets — to the cursor. Consecutive clicks on the
same cell within 400ms escalate click → word → line (#975): a double-click
selects the word under the pointer (vim `iw` word classes via
`textobject.Word`) as a charwise visual selection, a triple-click the whole
line linewise, and a fourth click cycles back to a plain click. The selection
is regular visual-mode state, so `cmd+c`/`cmd+x` and any visual operator
consume it; a later plain click collapses a mouse-made selection back to a
bare cursor (selections entered with `v`/`V` keep click-extends semantics).
Holding the button and dragging extends a selection (#977): char-wise from a
plain press (the press cell anchors on the first cell of travel), word-wise
after a double-click (the origin word stays fully selected in both
directions), line-wise after a triple-click; a drag during a `v`/`V`
selection keeps extending it instead of re-anchoring. The app routes the
gesture as a `dragEditSelect` drag kind — press starts it, motion events call
`MouseDrag`, release just drops the drag state (nothing to commit).
A right-click opens a floating context menu at the pointer (#1020,
`menu.Context` in `internal/menu/context.go`): the caret first moves to the
clicked cell via `ContextClick` — unless the click lands inside the active
selection (`selectionContains`), which stays put so Cut/Copy act on it. The
menu's entries (Cut/Copy/Paste, Go to Definition, Find Usages, Reformat File)
reference registered command ids resolved through the menu bar's `InfoFunc`,
so availability and shortcuts stay in sync; invoking dispatches `menu.RunMsg`
into the registry funnel. Hover highlights, left-click invokes, up/down/enter
navigate, esc or an outside press dismisses (the press never leaks to the
panes below); the box clamps to the terminal bounds at open time.
The wheel scrolls the
viewport via `ScrollBy(delta)`, which moves `view.Top` directly — clamped so
the last line stops at the bottom of the viewport (no overscroll, #1134;
soft wrap and collapsed folds keep the looser last-line clamp) without touching the cursor or mode — it works the same in Normal,
Insert, Visual, etc., unlike the vim-motion scroll commands. Horizontal wheel
(or shift+wheel) scrolls sideways via `ScrollXBy(delta)`, moving `view.Left`
clamped so the longest visible line keeps its last character on screen (#230);
the next cursor motion re-derives the offset to follow the cursor again.
A vertical scrollbar with a JetBrains-style diagnostics error stripe (#1022,
`editor/scrollbar.go`) overlays the pane's rightmost content column whenever
the buffer has more lines than the viewport: a dim track, a heavier thumb
whose position/size mirror `view.Top` and the visible fraction (same
`scrollThumb` math and glyphs as the explorer scrollbar), and a severity-
colored `■` marker at each cached diagnostic line's proportional track row
(worst severity wins a shared cell; markers draw over track and thumb). Mouse:
`ScrollbarHit` claims the rightmost column before any content click, so the
bar outranks text at that x. A left press on the thumb records the grab offset
(`ScrollbarPress` → true) and the app tracks a `dragEditScroll` drag kind
whose motion events call `ScrollbarDrag` — the viewport follows the pointer
with the grab point kept under it, clamped at both ends. A press on the track
above/below the thumb jumps the viewport to the proportional position.
Right-click (context menu) and left drags on content (selection) are
untouched; the bar renders only as an overlay, so text width, wrap, and click
mapping never shift when it appears.

Resting the pointer over content for ~600ms opens the hover popup at the
hovered cell (#1129, mouse-idle hover): the diagnostic covering the cell
shows immediately, LSP hover content follows when a server answers.
`HoverTarget(x, y)` is the read-only hit-test (gutter, scrollbar, sticky
headers, and cells past the line text are not targets); idle tracking and
scope guards live at the app layer — see [LSP](./lsp.md) for the full flow.

## Vim marks & bookmarks (#1151)

`marks.go` implements the vim marks MVP. `m{a-z}` sets a **local mark** at the
cursor; `'{a-z}` jumps to the marked line's first non-blank, `` `{a-z} `` to
the exact position (both record the departure in the navigation history via
`EventJump`). Local marks are per-view, per-session state like the caret set —
cleared on `Load`/`NewFile`/`RestoreText`/share, deliberately not persisted.
`m{A-Z}` sets a **global mark** (path + position, across files) in an
app-owned persistent store (`internal/marks`, one `marks.json` under the state
store — `IKE_CONFIG_DIR` or the project's `.ike` — loaded lazily, saved on
every change, so globals survive restarts). The editor reaches the store
through injected hooks (`SetMarkHooks`), the breakpoint-store pattern:
setting/gutter-lines/edit-adjust are closures, and a `'{A-Z}` / `` `{A-Z} ``
jump travels as `GlobalMarkJumpMsg` which the app resolves through the
standard open funnel (`openPathAt`) — cross-file jumps open the file and the
navigation history records.

Marked lines carry a `⚑` in the gutter's sign column (accent tone; the letter
shows in the picker, not the gutter), slotted below the breakpoint `●` and
above the test `▶`. **Edit adjustment** uses the same cheap line-count-delta
scheme as folds and breakpoints (`notifyMarkEdit`, beside
`notifyBreakpointEdit`): whole-line insertions/deletions above a mark shift it
exactly; multi-line replacements approximate (the mark clamps to the edit
site), and every jump additionally clamps into the buffer, so residual drift
never lands outside the text. External edits to a global mark's file are not
tracked — the jump clamps.

The **bookmarks picker** (`nav.bookmarks`, palette-only — vim keys are the
interface, no default chord) lists the focused editor's local marks plus all
globals as `'x  path:line  preview` rows; enter jumps (globals through the
open funnel), shift+delete or the `✕` zone removes the mark (the #842/#1113
prune pattern). See `internal/app/bookmarks.go`.

## Git hunk navigation (#1170)

`]c` / `[c` move between the change hunks the gutter marks (#464) describe —
a hunk is a maximal run of consecutive marked lines (kind changes inside a
run do not split it, matching `vcs.revertHunk`'s unit; a deleted-marker row
with unmarked neighbours stands alone). Strictly-past semantics with wrap and
a `change n/m (wrapped)` notice, the diagnostic/conflict jump family; lands
on the first non-blank column. Registered as `vcs.nextChange`/`prevChange`
with the vim sequences as cheatsheet doc hints; motion only — no undo, no
nav-history entries.

## Multi-caret editing (#145)

`multicaret.go` generalizes the single cursor to a primary caret plus an
ordered set of secondary carets (`carets []caret`, each with its own
`desiredCol`). Carets are **per-view state** like the cursor — two panes
sharing a document (#142) each keep their own set, and a `SyncMsg`/reload
re-clamps them into the mutated buffer like the cursor.

**Creation paths**

- `editor.caret.addNext` (`ctrl+g`, JetBrains): the first invocation locks
  onto the word under the primary caret (exact match, like `*`) and snaps to
  its start; each following one leaves a caret behind and jumps the primary to
  the next occurrence, wrapping and skipping occurrences that already hold a
  caret.
- `editor.caret.addAll` (`ctrl+shift+g`): a caret on every
  occurrence at once.
- `alt+click` toggles a secondary caret at the clicked cell; a plain click
  collapses back to a single cursor.
- Visual block `I`/`A` converts the rectangle into carets — `I` at the block's
  left edge (skipping shorter lines, vim-style), `A` one past its right edge,
  clamped to each line's end — and enters insert mode.
- `Esc` in normal mode collapses the set to the primary caret. Leaving insert
  mode keeps the carets (JetBrains semantics); the next `Esc` collapses.

**Edit fan-out.** `fanApply` runs an edit closure once per caret in ascending
buffer order, measuring how much each application grew or shrank the buffer
(in rune-offset space) and shifting the remaining carets by that delta — so no
caret drifts when an earlier caret's edit moves the text. Backward deletes
clamp to the previous caret's landing position, and carets that collide merge.
All per-caret edits go through **one `history.Recorder`**, so the whole
fan-out is a single undo unit — insert-mode typing joins the open insert
session recorder, one-shot operations commit via `fanMutate`. Fanned today:
insert-mode typing / Enter (per-caret smart indent) / backspace / word- and
line-kills / Tab / Shift+Tab (one dedent per line), `x`, `r`, operators
`d c y` with motions and text objects, `dd cc yy` (merged to one caret per
line first), `p`/`P`, `o`/`O`, `a A I s`, and completion accept (the popup
applies at every caret, JetBrains-style). A multi-caret yank/delete joins the
per-caret spans with newlines in the register. Motions (`h j k l w b` …,
arrows, `Home`/`End`) move every caret in parallel; each keeps its own
`desiredCol`.

**Explicitly single-caret** (the set collapses first): the command line and
search (`:` `/` `?`), the replace panel, visual selections (`v V ctrl+v`),
replace mode (`R`), undo/redo (history stores one cursor), and `.` — the dot
repeats the recorded change at the primary caret. The indent operators `>`/`<`
apply at the primary only; mutations that don't fan re-clamp the carets.
Out of scope (per the issue): carets across panes, regex-based caret
placement.

**Rendering.** `renderLine` draws secondary carets with the cursor's reverse
style dimmed (`Faint`); the primary keeps the full-strength cell. Selection
and search-match overlays compose as before.

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
- **`:e <path>`** reloads an existing file in place; a **nonexistent** path
  opens vim-style as an unsaved buffer seeded with the path's
  [language template](./languages.md#file-templates-170) (#170) — clean until
  edited, so `:q` discards it and the first `:w` creates the file.
- **Path completion (#543):** `tab` on a `:e` / `:w` (and `:wq`/`:x`) line
  extends the path argument to the longest unambiguous prefix via the shared
  `internal/pathcomplete` engine (`~` preserved, case-insensitive fallback);
  a single directory match completes with its trailing separator so repeated
  tab descends. While several entries match, their names render as a dim hint
  after the cursor and typing narrows the list. `tab` is inert on non-path
  commands and on the search line.
- **Errors:** unknown names and unresolvable addresses (missing selection,
  pattern not found) surface a transient `E:` message on the command-line row
  (`m.cmdMsg`), cleared by the next normal-mode key. `:g` / `:v` (global) parse
  but report *not implemented yet* (a later Roadmap 0200 sub-issue).

### `:substitute` (`substitute.go`)

`:[range]s/pat/repl/[flags]` rewrites lines over the resolved range (default: the
current line). `editor.replace` (`cmd+r`; Epic 0240, #283) fronts
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

- Markers land in the column of the **comment on the line above** the range
  when there is one (consecutive toggles stay aligned), otherwise at the
  range's **minimal indent**; a column deeper than a line's own indent clamps
  to that indent.
- **Blank lines are commented too** — a bare marker padded to the column — so
  repeated cmd+7 walks across empty lines without gaps (and without breaking
  indent-sensitive code on re-indent); uncommenting a marker-only line empties
  it again.
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
lands), or **cancel** (`esc`, buffer stays dirty + stale). The diff viewer (#60,
Epic 0320) has since landed; wiring a 'show diff' choice into this prompt is
a candidate follow-up.

External **deletes** (#83): the root model closes a clean editor whose file was
removed (the explorer's delete-closes-editor flow); a dirty one survives with
its buffer as the only copy, marked stale so the next save prompts. A
`FileRemoved` whose path still exists (replace-in-place: write temp + rename,
git checkout) is downgraded to a content change and reloads normally.

Config: `files.auto_reload = clean|never` (default `clean`; affects clean
buffers only — stale marking is unconditional).

Beyond the editor, the same per-file watcher events feed the LSP servers as
`workspace/didChangeWatchedFiles` (#1144), so a workspace index (Intelephense)
follows external creates/changes/deletes too — see
[LSP § File watching](./lsp.md#file-watching-workspacedidchangewatchedfiles-1144).

## Dependency-file edit guard (#565)

A go-to-definition (F4) commonly jumps into a **vendored dependency** — the
source under `.venv/…/site-packages`, `node_modules`, `vendor`, etc. Opening
such a file is unrestricted, but editing it is guarded so a stray keystroke does
not modify code a reinstall will overwrite. `depedit.go` classifies a buffer at
`Load` (`dependencyDir(path)` matches any known dependency-directory path
segment) and marks it `depFile`; the buffer is then read-only until the user
confirms the first edit, which flips `depOK` **for the session** (a same-path
reload keeps a prior confirmation; a freshly created file is never guarded).

The guard sits at the editor's mutation entry points — `mutate`,
`beginInsertChange`, the insert/replace entries in `normalCommand`
(`i I a A o O s R`), and `startInsertWith` as a backstop — each of which, on a
locked buffer, blocks the change, stashes a closure that re-runs it, and raises a
one-shot signal. `newRecorder()` additionally returns a **locked** recorder
(`history.Recorder.Lock`, whose `Apply` is a no-op) so any unguarded path cannot
silently mutate the file. `Update` turns the signal into a `DepEditBlockedMsg`
Cmd; the host shows a floating confirmation (`internal/app/depedit_prompt.go`,
mirroring the revert prompt) and on **enter** routes `ConfirmDepEditMsg` back to
the editor, which unlocks the buffer and replays the stashed edit through
`Update` so it reparses like any change — **esc** drops it and leaves the file
untouched and still locked.

## Git gutter & inline blame (Epic 0320)

The gutter shows diff markers against HEAD: added and changed lines recolor
their line number, a removal marks the line below it, and a diagnostic marker
wins the cell on overlap. `vcs.blameLine` toggles a dimmed inline blame
annotation at the end of the cursor line — "author, when · summary", or
"not committed yet" for unstaged lines. Both are recomputed on save, external
change, and vcs refresh, so marker positions may briefly lag unsaved edits.
See [VCS / Git Integration](/architecture/vcs.md).

## Merge-conflict resolution (#1149)

`conflict.go` detects git conflict blocks in the buffer — `<<<<<<< label`,
`=======`, `>>>>>>> label`, with an optional diff3 `||||||| base` section —
and resolves them in place. Detection follows the testmarks (#1150) caching
pattern: the scan runs at most once per document version (never per frame),
held in a pointer store shared by the Model's value copies; each rescan bumps
a conflicts epoch that keys the scrollbar stripe memo.

- **Rendering** rides the line cache (#614): the ours section tints with a
  `VCSAdded`-mixed background, theirs with `VCSModified`-mixed, the diff3
  base section renders dim, marker lines dim bold. Roles change only with the
  document version, whose bumps always travel through Update and hence
  through the render epoch, so no extra invalidation is needed.
- **Commands** (registry, palette-only — the chord budget is full, #711):
  `merge.acceptOurs` / `merge.acceptTheirs` / `merge.acceptBoth` replace the
  whole block containing the cursor with the kept side(s) — ours before
  theirs for acceptBoth, base never kept — as ONE undo unit through the
  standard mutate/Recorder path; the cursor lands on the block's start line.
  Outside a block they answer with an ex-line notice. `merge.nextConflict` /
  `merge.prevConflict` walk the block starts with wrap-around, the
  diagnostic-jump pattern (#369).
- **Context menu** (#1020): a right-click first moves the caret, then the app
  asks the cheap `ConflictAtCursor()` query — inside a block the menu gains
  the three accept entries; outside it keeps its static shape.
- **Overview ruler** (#1131): conflict blocks mark their covered rows in the
  `VCSConflicted` colour (`◆`) as a third stripe source with its own epoch;
  cell precedence is diagnostics > conflicts > git, and a click on a marked
  cell jumps to the block's start line.

## Auto-save (#174, #731)

With `editor.auto_save = focus` (the default; `off` disables), a dirty
buffer saves itself when focus leaves its pane
— every focus transition funnels through the root model's `setFocus`, so one
hook covers Ctrl+arrows, the pane switcher, mouse clicks and the explorer
toggle — and when its document is about to be replaced by opening another
file into the pane. `editor.Autosave` goes through the normal `saveAs` path:
`EventSave` fires (watcher epoch, LSP didSave, shared-view sync), and **undo
history is untouched** — returning to the pane, undo/redo work as usual, and
an undo past the saved state re-dirties the buffer so the next blur persists
it. A **stale** buffer is never auto-saved: it stays dirty for the explicit-
save conflict prompt above. Cmd+S remains the explicit save.

`editor.auto_save = idle` (#731) is a superset of `focus`: additionally, a
dirty **titled** buffer writes itself after staying quiet for
`editor.auto_save_idle_ms` (default 2000, clamped ≥ 100). The idle side rides
the same change seam and debouncer shape as the crash-recovery snapshots
(`internal/app/autosave_idle.go` mirrors `backup.go`): every `SyncMsg` from a
dirty buffer (re)arms its deadline, a clean one cancels it, and a single
armed `tea.Tick` saves the buffers that went quiet — through `Autosave()`, so
all the guarantees above (EventSave, untouched undo, stale-skip) hold and
the modified indicator clears. Untitled buffers are never idle-saved; crash
recovery covers them. Config edits apply live: an interval change re-arms,
leaving idle mode drops pending marks.

## Format & organize imports on save (#1148)

`editor.format_on_save` and `editor.organize_imports_on_save` (both bool,
default **off**; Settings → Editor) run LSP steps before a **manual** save:
organize imports (the `source.organizeImports` code action, requested with
`CodeActionContext.Only` and applied without the picker), then whole-document
formatting, then the actual write. Because both steps are async server
requests while the editor's write is synchronous, the save runs as an
explicit **chain**: `saveGuarded` parks the write (`Model.pendingSave`,
`editor/savechain.go`) and dispatches the bridge-registered provider
(`ilsp.StartSaveChain` → `plugins/lsp/savechain.go`); the bridge goroutine
runs each enabled, capability-gated step, delivers edits as a
`FormatEditsMsg` whose `Applied` callback acks when the buffer holds them
(so the next step's request reads the updated text), and always finishes
with `ilsp.SaveChainDoneMsg` — the app routes it to the parked views, whose
`CompleteChainedSave` performs the deferred write.

Guarantees:

- **Never blocks, never loses the save.** Every step — the server request
  and the applied-ack wait — is time-boxed (`saveChainStepTimeout`, 2 s);
  errors, empty answers and timeouts fall through to the next step, and the
  done message always fires. No server / no capability (formatting, or the
  organize-imports kind in `codeActionKinds`) means no chain at all: the
  write happens immediately.
- **Manual saves only.** `:w`, `:wq`, `editor.write`, `editor.write_quit`
  and `editor.saveAll` chain (save-all per dirty buffer); autosave
  (focus/idle), crash-backup snapshots and the shutdown/switch/close-guard
  writes (the `write_raw` action) stay raw by design — they must land
  synchronously and must never hinge on a language server. `:w other` is
  raw too: the chain edits this buffer, not an arbitrary target.
- **Re-entrancy coalesces.** A second save while a chain is pending joins it
  (no stacked chains); a `:wq` issued meanwhile latches its close intent,
  and the pane closes after the chained write. A conflict that appears
  mid-chain (external change) still yields the save-conflict prompt.

## Untitled buffers & save-as (#730)

An empty editor pane (fresh start, split leaf) is a typable **untitled
buffer** — keys route to it like any editor, it dirties normally, and the
dirty-close guard (#259) covers it. Saving it has no path: `saveGuarded`
emits `SaveAsPromptMsg` instead of "no file name", and the app opens a shell
prompt (`internal/app/saveas.go`, the rename-prompt pattern with the shared
`ui.EditKey` line editing). The typed path resolves against the project root
(absolute paths pass through), parent directories are created, and an
existing file is refused — the prompt stays open. Accepting writes through
`editor.SaveTo` and binds the tab: watcher tracking, MRU, explorer
active-file, layout persistence, highlighting (`Reparse`), VCS gutter marks
and the file-opened hooks all run, so the fresh file behaves exactly like
one opened from disk. `:wq` carries its close intent through the prompt
(`SaveAsPromptMsg.CloseAfter`); `:w other/path` on any buffer still saves
directly without the prompt.

## Line endings & encodings (#66)

The buffer is always **LF-joined UTF-8**; the on-disk flavor lives beside it as
document properties (`Model.eol`, `Model.enc`, `Model.mixedEOL` — like
dirty/stale: copied on `ShareDocumentWith`, mirrored via `SyncMsg`). Detection
and transcoding live in `internal/textenc`; `encoding.go` is the editor side.

- **Load / reload** decode the raw bytes (`textenc.Decode`): a BOM picks
  UTF-8 BOM / UTF-16 LE / UTF-16 BE outright; BOM-less bytes must validate as
  UTF-8 or decode via the `files.encoding` config fallback (`latin-1`,
  `windows-1252`, `utf-16le`, …) — otherwise the open **fails with a clear
  error** instead of rendering mojibake. The line-ending flavor is the first
  line break's (`LF` when none); a file containing both flavors is flagged
  *mixed* and warned about on the ex line (the next save normalizes to the
  stored flavor).
- **Save** (`saveAs`) applies trim-trailing / final-newline on the logical
  lines, then `textenc.Encode` re-applies the stored flavor: CRLF re-joined,
  BOM re-attached, text transcoded — a CRLF or UTF-16 file **round-trips
  byte-identically**. A rune the target encoding cannot represent (e.g. `€`
  in ISO 8859-1) fails the save with an error on the ex line.
- **Conversion** is explicit: the `file.setLineEndings.{lf,crlf}` and
  `file.setEncoding.{utf8,utf8bom,utf16le,utf16be,latin1,windows1252}`
  palette commands (theme-picker style, one command per choice) set the
  flavor and mark the buffer dirty — the conversion materializes on the next
  save. The status line shows both (`eol` + `encoding` segments, see
  [status-line](./status-line.md)).
- EditorConfig (#63) will layer *policy* (`end_of_line`, `charset`) on this
  mechanism once it lands.

## Shared documents (#142)

Two editor panes showing the same file are two **views of one document**
(JetBrains/vim-split semantics), not divergent copies. `share.go`:

- Opening a path another pane already shows makes the new pane a second view
  via `ShareDocumentWith`: `*buffer.Buffer` and `*history.History` are aliased
  (one text, one undo stack), while cursor, scroll, mode, and registers stay
  per pane. Session restore deduplicates the same way.
- After an edit, undo, save, or reload in one view, the emitter adapter (which
  knows its pane key) broadcasts `editor.SyncMsg{Path, FromKey, Dirty, Stale,
  Large, EOL, Enc, MixedEOL}` through `host.Send`; the root model routes it to every *other* pane
  showing the path. Receivers clamp cursor/scroll into the mutated buffer,
  mirror the document flags, bump `docVersion` and reparse — no text is copied,
  the buffer is shared. `applySync` never re-emits, so syncs cannot ping-pong.
- External reload mutates the document **in place** (`Buffer.ReplaceAll`,
  `History.Reset`) so the aliases survive; async per-path messages (highlight
  spans, LSP results, watch events) route to **all** panes owning the path
  (`editorKeysForPath`), each filtering by its own document version.
- Known edge: `:e` inside a pane loads a fresh copy and leaves any prior
  sharing (it re-points that pane's document); `:w otherfile` re-targets only
  the saving view's path.
- **Split view (#147):** `editor.splitViewRight` / `editor.splitViewDown`
  (`cmd+alt+shift+right` / `cmd+alt+shift+down`, View menu, palette) split the
  focused editor and make the new pane a second view directly — no explorer
  detour. Unlike an explorer open (which starts at the top), cursor and scroll
  are **copied from the source view**, and the new view gets focus
  (JetBrains). A file-less editor is a no-op with a toast. Layout and session
  persistence need nothing new: the split is an ordinary leaf and restore
  re-shares by path.

## Large-file mode (#149)

A document crossing `files.large_file_kb` (default 1024) or
`files.large_file_lines` (default 100000) at `Load`/reload is flagged
(`Model.largeFile`, a document property like dirty/stale: copied on
`ShareDocumentWith`, mirrored via `SyncMsg.Large`). While flagged and not
overridden (`InsightOff`), code insight degrades deliberately instead of
stalling:

- `parseCmd` returns nil — no Tree-sitter parse ever runs (the CGo parse cost
  scales with file size), so typing stays flat.
- Change events ship no `Text` payload — the per-keystroke `buf.String()`
  re-join is skipped; the LSP bridge's `didOpen` gate means nothing consumes it.
- The LSP bridge skips `didOpen` (see `/architecture/lsp.md`); diagnostics and
  completion are silently absent.
- The watcher's poll fallback never content-hashes the file (mtime+size alone
  decide, `watch.Service.SetHashLimit`).

UX: a one-time warn toast on open, plus a `[large file]` status-line segment.
The palette command `editor.forceCodeInsight` overrides per document: it
records the path in the shared `internal/largefile` override set, reparses
every view, and re-fires the file-opened hook so the LSP bridge didOpens. The
policy (thresholds + override set) lives in `internal/largefile`, shared by
editor, LSP bridge, and app. Replacing the line-slice store (piece table) is
explicitly out of scope — this mode is the cheap 90%.

## Markdown rich rendering (#881)

Vim-conceal-style semi-preview for Markdown, display-only (`markdown.go`) and
toggled by `editor.markdown_rendering` (default on, in Settings → Editor):

- **Inline attributes** (all lines): the inline grammar's `markup.*` captures
  render as terminal text attributes — `**bold**` bold, `*italic*` italic,
  `~~strike~~` struck through — composed in `styleAt` over whatever color the
  theme resolves.
- **Concealment** (lines the cursor/carets are *not* on): the query captures
  the marker chrome (`**`, `*`, `` ` ``, link `[]()` + destination) as
  `@conceal`; the `SpansMsg` handler splits those spans out of the style index
  into per-line column ranges, and `renderSpan` skips those cells so the line
  reads like rendered text. The cursor line always shows raw source. Mouse
  clicks map back through the hidden ranges (`concealClickCol`), so the cursor
  lands on the character that was clicked; buffer-column motions and
  selections are untouched by design.
- **Pipe tables** (cursor outside the block): detected from the buffer text (a
  pipe row above a `|---|` delimiter row — equivalent to the grammar's
  `pipe_table`, but it also works in `CGO_ENABLED=0` builds), re-rendered with
  box-drawing characters, cells padded/aligned per the delimiter row's `:`
  colons. **Row-preserving**: the delimiter row becomes the `├─┼─┤` separator
  and no border rows are added, so line↔row mapping and the gutter stay 1:1.
  The cursor entering the block flips it back to raw pipe source. Under soft
  wrap tables stay raw (wrap segments slice raw buffer text; a sliced
  box-drawing row would tear); with horizontal scroll the rendered row is
  sliced by the same column window as any other line (ANSI-aware, since the
  rows carry styling).
- **Cell inline rendering** (#945): cell content renders its inline markdown
  inside the box-drawing rows — the per-line conceal/style pipeline cannot
  follow text into the re-laid-out cells, so `renderCellInline` is a small
  self-contained renderer (grammar-free, like table detection): `` `code` ``
  and link text take their theme capture styles (@string / @label),
  `**bold**`/`__bold__`, `*italic*`/`_italic_` (word-boundary underscores
  only) and `~~strike~~` become text attributes with nesting, `[text](url)` /
  `![alt](url)` show just the text, `\`-escapes and unmatched markers stay
  literal. Column widths and alignment size by the concealed display width.

## Inline color preview (#790)

Recognized color literals — `#rrggbb`, `#rgb`, `rgb()/rgba()`, `hsl()/hsla()`
— render with the literal's own color as the **cell background** and a
black/white contrast foreground picked by luminance (`colorswatch.go`).
The tint approach (instead of extra `██` swatch cells) is deliberate: it adds
no display columns, so motions, mouse clicks, soft wrap and the #881 conceal
mapping stay untouched. Detection is a per-line regex scan inside the
line-cached render path — only visible lines are ever scanned, so large files
cost nothing. Invalid values (out-of-range channels, wrong arity, 4/5/8-digit
hex) yield no swatch. Alpha components parse but do not tint (no alpha
channel in a terminal cell). Toggle: `editor.color_preview` (default on,
Settings → Editor). Cursor/selection/search win the cell as usual; the
diagnostic underline composes on top.

## Live templates / snippets (#1152)

`snippet_expand.go` + `internal/snippets`: user-defined **live templates**
expand in insert mode. Pressing Tab with the cursor immediately after a
trigger word replaces the word with the template body through the existing
LSP snippet placeholder engine (`internal/lsp/snippet`), and the tabstop
session (#846) takes over — the cursor lands on `$1`, Tab/Shift+Tab cycle
placeholders exactly like an accepted LSP snippet completion, Esc ends the
session. No trigger match leaves Tab to its normal indent insertion (#1137);
with secondary carets active the expansion never fires (indentation and the
trigger word would differ per caret).

Templates come from `[[snippets]]` config entries (`trigger`, `body`,
optional `language`; see [config](./config.md)) plus a small built-in table
in `internal/snippets` (Go `iferr`/`main`/`forr`, Python `main`/`def`,
TypeScript/JS `log`/`fn`). Resolution per buffer (via `lang.ByPath`):
user language-scoped > built-in language-scoped > user global > built-in
global — a user entry with the same trigger+language shadows the built-in.
Lookups read `config.Get()` live, so a config reload applies immediately.

Multi-line bodies re-indent on expansion: literal tabs become the buffer's
indent unit (`tab_width`/`use_spaces`, editorconfig-aware) and every
continuation line inherits the current line's leading whitespace.

The same templates appear in the completion popup as snippet items (detail
`template …`) through a local completion source (see
[completion](./completion.md)) — this works with no LSP server at all, since
the local engine answers triggers independently. Accepting a template item
expands and re-indents identically to the Tab path; LSP-server snippet items
are still inserted exactly as the server sent them.

## Breadcrumbs bar (#1153)

App-level chrome, not editor state (`internal/app/breadcrumbs.go`): a one-line
row under an editor pane's tab/title row showing `file ▸ symbol ▸ child` — the
LSP `documentSymbol` chain enclosing the cursor, the same hierarchical tree
the Structure pane (#1025, `/architecture/structure-view.md`) consumes, cached
app-side per path (`Model.docSymbols`). The chain is derived at render time
(`symbolChain`, mirroring the Structure pane's Follow semantics: the last
containing node per level is the most specific), so cursor moves cost nothing;
requests ride the existing settled-pass sync (`structureSyncCmd`), which now
also fires with the Structure pane closed while breadcrumbs are on, with the
same per-path dedup and save-triggered re-request.

- **Config**: `editor.breadcrumbs` (bool, default on — the JetBrains default;
  settings panel entry). Read live; the toggle applies on the next settled
  pass.
- **Visibility**: the row renders only while cached symbol data exists for the
  pane's active file — no data, no provider, terminal tabs, zen: the row is
  absent and the editor keeps the line. Outside any symbol the row shows just
  the basename. Unfocused panes render it too whenever their file's data is
  cached.
- **Geometry**: the row is one extra *vertical chrome* line. `layout()` adds
  `breadcrumbRows(inst)` to `paneChromeH` for the pane's `SetSize`, and every
  editor-local mouse translation goes through `contentYOff(key)`
  (= `paneContentY` + row) — clicks, drags, hover-idle, LSP popup anchors and
  the large-file banner all shift together. `syncBreadcrumbLayout` in the
  settled Update pass re-runs `layout()` when any pane's row appears or
  disappears outside a layout event (data arrival, tab switch, config toggle).
- **Interaction**: each segment is a click zone (`crumbHit` mirrors
  `renderCrumbRow`'s geometry, like the tab bar's `tabHit`); a left press on a
  symbol segment jumps there through `openPathAt`, so nav history records the
  jump. The file segment is informational. At narrow widths the front segments
  elide behind a leading `… ▸ ` — the deepest segments win; a lone overflowing
  segment truncates with a trailing ellipsis.

## Config

`Configure(host.Config)` retains the config reference and `applyConfig` re-reads
the `[editor]` section on every event, so `tab_width`, `use_spaces`,
`auto_indent`, `auto_close_pairs`, `trim_trailing_whitespace`, `insert_final_newline`,
`line_numbers`, `relative_line_numbers`, `scroll_off`, `sticky_scroll`,
`sticky_scroll_depth`, `wrap`, `show_whitespace` (`none|trailing|all`),
`indent_guides`, `rulers`, `markdown_rendering` (#881), `color_preview`
(#790) and `search_ignore_case` (#1111, default off — in-file search folds
case unless a `\C` marker forces exact) take effect live. The view-option keys (#64) are
special-cased: a palette toggle (`view.toggleWrap`, `view.toggleWhitespace`,
`view.toggleIndentGuides`) marks a per-view override that the per-event config
refresh no longer clobbers. `files.encoding` names the fallback
encoding for BOM-less non-UTF-8 files (#66).

## Registry bridge & LSP seam

`commands.go` registers editor actions and ex-commands as plugin `Command`s
(`editor.write`, `editor.quit`, `editor.write_quit`, `editor.undo`,
`editor.redo`, `editor.copy`, `editor.cut`, `editor.paste`, `editor.lineStart`,
`editor.lineEnd`). Each `Run` dispatches an `ActionMsg`, which the root routes back
into the focused editor's `Update` — the single dispatch path the palette (07)
and keybindings (08) reach. `events.go` emits on-change / cursor-move /
completion-trigger `Event`s through an injectable `Emitter` (nil by default); no
language intelligence lives here (Roadmap 0100).
