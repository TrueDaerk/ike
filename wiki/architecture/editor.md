---
type: concept
title: Editor
description: Vim-like modal editor pane built from buffer/mode/motion/operator/textobject/register/history/viewport/search sub-packages.
resource: internal/editor
tags: [architecture, editor, vim]
timestamp: 2026-08-24T00:00:00Z
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

`renderEpoch` alone was **not** a complete guard (#1327): it is bumped from
`Update`, while the mouse entry points (`MouseClick`, `MouseDrag`, `AltClick`,
`ContextClick`) mutate the caret directly. A click therefore left both the line
cache and the pane-level View cache (#615) valid, and the pane served its
previous frame — the caret stayed drawn at its old position while typing went to
the new one, and a drag selection was invisible although operations used it (the
symptom only showed in big projects, where fewer async decoration messages
happen to bump the epoch). The caches are now additionally keyed on
`caretState()`: a hash of the primary cursor, the mode + visual anchor, the
focus flag and the secondary carets — every direct render input the epoch does
not stand for. Deriving the state beats bumping a counter at each call site,
because a mouse path added later is covered without having to remember; an
unchanged caret hashes equal, so vertical scrolling still hits the cache and the
0400 benchmark is unchanged.

The gutter's sign column also carries the **test run marker** (#1150): a `▶`
in the success tone on every detected test declaration (`testmarks.go` —
detection via the language registry's `lang.TestSpec` regex seam, cached per
document version in a per-view pointer store like the line cache, so the scan
runs at most once per edit, never per frame). Sign precedence: debugger paused
`▶` > breakpoint `●` > bookmark `⚑` or its mnemonic digit (#1151/#55, accent
tone — vim marks and project bookmarks, see "Vim marks & bookmarks") > test `▶` > inheritance `↑`/`↓` (#1453, info tone —
LSP-pushed arrows on symbols that implement/override a super declaration or
have implementations; `inheritmarks.go`, per-line map like `gitMarks`, gated by
the `editor.marks.inheritance` toggle which also stops the probe traffic) >
diagnostic/git colouring. A single-cell sign column means a line that is both
a test and an overriding method shows only the test marker. A plain gutter
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
  (exclusive/inclusive/linewise): `h j k l`, `w b e` (+ `W B E`), `ge`/`gE`
  (backward word end, #1193), `0 ^ $`, `gg G`, `{ }`, `f t F T` with `;`/`,`,
  and `%` bracket match. `SmartHome`/`SmartEnd` (#1781) back the literal
  `Home`/`End` keys (not the vim `0`/`^`/`$` motions, which keep their own
  conventions): each toggles between the line's content column — first/last
  non-blank — and the true column 0/`RuneLen`, deciding the direction from the
  cursor's own column, so a line with no non-blank runes has no separate
  content column and never toggles.
- **textobject** — `iw aw` (and WORD), bracket pairs (`i( a( i{ …`, nesting and
  multi-line aware, plus the `ib`/`iB` aliases), quotes (`i" a"`), paragraphs
  (`ip ap`, linewise), sentences (`is as`) and XML/HTML tags (`it at`, text
  scan, #1193), resolved to a `Range` (+ a `Linewise` flag).
- **operator** — `d c y p` (+ `gp`), doubled `dd cc yy`, char/line-wise, with
  `Compose` turning a motion result into the operated `Target`. Writes the
  register store and records edits through a `history.Recorder`. The case
  operators `gu gU g~` (#1193) run through the same target plumbing
  (`operator.Transform`), as do `=` (heuristic reindent) and `gq` (reflow at
  `editor.text_width`) on the editor side.
- **register** — unnamed `"`, named `"a`-`"z` (uppercase appends), yank `"0`,
  small-delete `"-`, the numbered ring `"1`-`"9`, and a system-clipboard seam
  (`"+`/`"*`, injected via `SetClipboard`). One `register.Store` is shared
  **app-wide** (#1540, vim's global-register semantics): the workspace manager
  owns it — surviving the model rebuild a project switch does — and it is
  threaded via `pane.NewRegistry` → `newEditorModel` → `SetRegisters` into
  every editor in every pane, tab and workspace (plus the diff-pane inline
  editor and, idempotently, `installEmitter`), so yanks/deletes and the
  paste-from-history ring are one pool; `editor.New` keeps a private store for
  standalone use, replaced when a shared one is injected (`SetRegisters` runs
  before `Configure`/`SetClipboard` so both apply to the shared store).
  `internal/clipboard` provides the real clipboard implementation
  (pbcopy/pbpaste on macOS, wl-copy/xclip/xsel on Linux/BSD), wired in by the
  pane registry when an editor is created; without a utility on PATH the
  registers fall back to the built-in no-op clipboard. On macOS a read whose
  plain-text flavor is empty falls back to the pasteboard's raw `public.url`
  bytes (#1601): a URL copied as a *link* can carry only that flavor, which
  pbpaste prints as nothing, and converting it to text via Foundation's URL
  type would percent-encode the string a second time (`[` → `%5B`,
  `%22` → `%2522`) — the raw flavor bytes stay verbatim.
  `Cmd+C/X/V` (keymap commands `editor.copy/cut/paste`) yank / delete the
  visual selection — or the current line without one — through `"+`, and paste
  from it (mid-insert the paste is its own undo step inside the open insert
  session, #1818).
  A **bracketed paste** from the terminal (external text) arrives as one
  `tea.PasteMsg`; the app routes it to the focused editor's `PasteText`, which
  inserts the whole block as a single edit and one undo unit — visual mode
  replaces the selection, mid-insert it splices in (again as one undo step of
  its own), normal mode pastes after the
  cursor like `p` — without touching the yank registers or system clipboard
  (#603). Because this route bypasses the editor `Update` loop's
  `maybeReparse`, `PasteText` returns the reparse command itself when the
  buffer changed, so highlighting refreshes immediately instead of waiting
  for the next keystroke (#1491). A focused terminal pane gets the block through its own
  bracketed-paste path; a modal overlay owning the keyboard takes it through
  the **overlay paste router** (`internal/app/overlaypaste.go`, #1273) instead
  — see [command palette](./command-palette.md).
  **Editor-internal inputs** are one level deeper (#1380): the `/`/`?`/`:`
  command line and the find/replace panel are editor state, invisible to the
  app router, so `PasteText` (and `Cmd+V`'s `clipboardPaste`) first route
  through `pasteIntoPrompt` (`internal/editor/cmdline_paste.go`) — the paste
  lands in the open input, flattened to one line by `ui.PasteText` exactly
  like overlay fields, and re-runs the same hooks typing does (incsearch
  preview / path suggest). The `:s///c` confirm prompt has no input but still
  swallows the block, so a paste can never fall through into the buffer while
  any of these own the keyboard.
  Copy/cut answer with a feedback toast ("copied 3 lines", "cut 12 chars",
  #252) via `NoticeMsg`; the vim-native `y`/`d` flows stay silent.
  **Yank → system clipboard (#1256).** `editor.clipboard_sync` (default on,
  Settings → Editor) mirrors *unnamed* yanks — `yy`, `y{motion}`, visual `y`
  — onto the system clipboard, the conservative half of vim's
  `clipboard=unnamed`. Named registers (`"ay`) never sync, and neither do
  deletes or changes: a stray `dw` must not clobber what the user has on
  their clipboard. `p`/`P` keep reading the internal register unchanged —
  external content still comes in through `Cmd+V` / bracketed paste.
  **Smart paste (#1476, `internal/editor/smartpaste.go`).** A linewise
  multi-line `p`/`P` (any register, dot-repeat, multi-caret — the target
  indent resolves per caret) strips the block's common leading indentation
  (measured in display columns, so tabs and spaces mix) and re-applies the
  target line's indentation, preserving relative structure; the relative
  indent is re-emitted in the buffer's indent style (`editor.use_spaces` /
  `editor.tab_width`, i.e. editorconfig-aware). A blank target line takes
  the indentation of the nearest non-blank line above; blank block lines
  stay empty. The transform runs on the register text before the insert, so
  the paste stays one undo unit. A `Cmd+V` mid-insert re-indents only the
  continuation lines (the first line splices at the cursor) and lands as one
  undo step of the insert session. Single-line
  pastes, charwise pastes and terminal bracketed paste (`PasteText`) stay
  verbatim; `editor.smart_paste = false` turns the transform off.
  **Clipboard failures surface (#1255).** Every `"+` write used to be
  `_ = clip.Write(text)`, so a clipboard utility that was missing, sandboxed
  or failing produced the same "copied 3 lines" toast as a working one — the
  internal register is filled either way. The store now records the error
  (`Store.TakeClipboardError`, destructive read); the copy/cut toasts drain it
  before reporting success, and `Update` drains it after every keypress, so
  any key-driven path — `"+y`, a synced yank — reports
  `system clipboard unavailable: <cause>` instead. A failed *read* still falls
  back to the unnamed register: a paste degrades rather than dying.
  Note that the host terminal can take the chord before IKE ever sees it —
  Ghostty binds `super+c` to `copy_to_clipboard:mixed` by default, so a
  terminal-side selection wins over `Cmd+C`; `keybind = super+c=unbind` in
  `~/.config/ghostty/config` hands it back to IKE.
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
  `undolevels`. A per-buffer **byte budget** (32 MiB of retained edit text,
  #1537) prunes in the same order — whole-buffer changes (reformat-on-save,
  `:%s`) retain roughly twice the document each, so the node cap alone is no
  byte bound; the newest state always survives, even oversized. `u`/`ctrl+r` take a count (`3u` undoes three changes, stopping
  early when the history runs out, #231). `.` repeat lives in the editor
  (`dotCommand`). The history pins a **save checkpoint** (`MarkSaved`/`AtSaved`,
  #251): saving pins the current state, and undo/redo clear the dirty flag when
  they land exactly on it (vim-style), so `[+]` goes away when you undo back to
  the saved content. A crash-restored buffer marks the checkpoint unreachable —
  no undo depth makes it read as clean.
  **Insert granularity** (#1818, `insertundo.go`): an insert session commits a
  *sequence* of changes, not one — see the insert-mode section below. The store
  is untouched by this: the editor decides where a segment ends, `Recorder` and
  the tree stay change-based, so branching, `g-`/`g+`, the byte budget and the
  persisted tree work on the finer steps unchanged.
  **Change list** (#1174, `changelist.go`): a per-document ring (cap 100) of
  the `CursorAfter` of every committed `Change` — derived from the same
  `pushChange` that makes an edit undoable, so undo/redo walks add no
  entries. `g;` moves the cursor to older edit positions (the first `g;`
  lands on the most recent edit), `g,` back toward newer — cursor motion
  only, no undo (that is `g-`/`g+`) and no nav-history entry (the
  diagnostics-motion precedent). Adjacent same-line entries collapse into
  one stop keeping the newest position; a new edit resets the walk pointer.
  Entries drift-correct with the local-mark delta scheme (`notifyMarkEdit`)
  and every jump clamps into the buffer. Jumps notice `change list: n/m`,
  the ends `no earlier/later edit position`. Session state like local marks:
  it resets with the history and does not include changes restored from the
  persistent undo store.
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
  whitespace wins on overlap; each guide is coloured by its depth from the
  rainbow palette unless `editor.rainbow_indent_guides` is off, see
  [Highlighting](./highlighting.md)) and **column rulers** (`editor.rulers = [80]`;
  a background tint on those display columns, padded past short lines). The
  palette toggles override the config per view; theme slots `Whitespace`,
  `IndentGuide`, `Ruler` colour them. **Control bytes & ANSI escapes** (#1469,
  `editor/ansiescape.go`): a raw C0 control or DEL in the buffer (a log file
  with colour escapes) never reaches the terminal — each renders as its
  one-cell Unicode Control Pictures glyph (`␛` for ESC, dimmed with the
  `Whitespace` slot), so one buffer rune stays one display cell and click
  mapping, caret and selection stay aligned; tab keeps its expansion. SGR
  sequences (`ESC [ … m`) are additionally *interpreted*, per line: the text
  they govern renders with the mapped colour/attributes (basic/bright/256
  palette indices and truecolor) through the normal styling pipeline — the
  file's foreground replaces the syntax colour, the sequence's own runes
  render dim, and cursor/selection/search overlays still win, so no SGR state
  bleeds around the caret. Copy is untouched (the buffer keeps the original
  bytes). **Sticky scroll** (#168) pins the header lines of the
  declarations enclosing the first visible line as the top rows of the pane
  (JetBrains-style): the scopes come from the same Tree-sitter parse that
  produces the highlight spans (`highlight.HighlightScoped`, node kinds per
  language via `lang.Language.ScopeNodes`), `sticky.go` resolves which headers
  pin for the current `view.Top` (a fixed point, since pinned rows cover
  content and move the reference line down; capped by `sticky_scroll_depth`,
  innermost win), scrolling keeps the cursor from hiding behind the pinned
  rows, and a mouse click on a pinned row jumps to its declaration. The last
  pinned row fills its right padding with a faint dashed rule (#1910), the
  separator between the headers and the scrolling body. Large-file mode pins
  nothing (#1910): `stickyLines` gates on `InsightOff`, so headers can never
  pin from stale scope data even though the parse itself is already skipped.
  **Code folding** (#144) collapses the body of a function, block, import
  list or multi-line comment behind its header line: the foldable ranges come
  from the same parse (`SpansMsg.Folds`, node kinds per language via
  `lang.Language.FoldNodes`, falling back to `ScopeNodes`), merged (#1912)
  with the server's `textDocument/foldingRange` answer when one arrives
  (`FoldingRangesMsg` → `lspFolds`; server ranges win on the same header
  line, carry the LSP kind — imports/comment/region — and the
  `lsp.folding` toggle turns the merge off), and `fold.go`
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
  (`editor.fold.*`). **A closed fold is one unit for yank, delete and change**
  (#1741, vim's rule for a linewise operation on a closed fold): a linewise
  target whose lines cover a collapsed header grows to that fold's end line
  before the operator sees it (`expandFoldTarget` in `fold.go`, applied at the
  `runOperator` choke point, so `yy`/`dd`/`cc`, `V`+`y`/`d`/`c`, Cmd+C/Cmd+X,
  `:y`/`:d` and the visual put all inherit it). Several covered headers expand
  to the largest end, and the scan repeats so nested folds pull in too. The
  expansion is deliberately narrow: **charwise** targets pass through untouched
  (a selection inside the visible header line is what it looks like), open folds
  are ordinary lines, and the reshaping operators (`>`/`<`, `=`, `gu`/`gU`/`g~`,
  `gq`, `ys`) keep acting on the literal range because they rewrite what is on
  screen. The register entry stays linewise, so a paste reproduces the whole
  block and the copy toast (#252) reports the real line count. Folds live on the
  view, not the buffer, so the expansion happens in the editor package — the
  `operator` package has no fold knowledge. The other collapsed-row features
  (repeat runs #1650, PEM blocks #1652) need nothing here: their hidden lines
  are inside the contiguous linewise range anyway, and a cursor on their header
  row already reveals them, so what is copied is what is shown. Multi-caret
  `dd`/`cc`/`yy` also stay literal — expanded per-caret spans could overlap.
  **Copy affordance on a collapsed fold** (#1787): the header row carries a
  dimmed `⧉` behind the placeholder, right-aligned into the annotation column
  (the column the `×N` badges and inline blame share, `annotColumnWidth`), so
  the markers of a file stay one scannable column and the click target is a
  fixed cell instead of drifting with the header's length. Clicking that one
  cell copies the header through the fold's end line — raw buffer text,
  conceals and stand-ins unresolved like every other yank — into the `+`
  register and toasts `copied N lines`; the fold stays collapsed and the caret
  does not move. Render and hit test share `foldCopyCell`, so the clickable
  cell is by construction the cell the glyph is drawn in; every other cell of
  the header keeps its click meaning (caret, gutter breakpoint), and a pane too
  narrow for the affordance simply draws the placeholder alone and reports no
  hit. Open folds carry no glyph. The keyboard pendant is `zy` /
  `editor.fold.copy` ("Copy Folded Range"): it copies the collapsed fold the
  caret's line heads, else the **innermost fold containing the caret** whether
  collapsed or not — so it doubles as "yank this block" without marking it.
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
  **Query history** (#1171): `up`/`down` on the open line cycle recent
  committed queries, vim-style (up = older, most recent first; down past the
  newest restores the half-typed live line). A recall replaces the input
  with the cursor at the end and re-runs the incremental preview; typing or
  editing after a recall works normally and leaves recall mode. Enter pushes
  the typed line (markers like `\c` included, so a recall restores it
  verbatim; even a no-match search is recallable). The `/` `?` line shares
  one `search` bucket, the `:` line keeps its own `ex` bucket — separate
  histories like vim's, even though the input code path is shared (#1110).
  Buckets live in the app-owned `internal/histories` store (one
  `histories.json` under the state store, `marks.json` pattern: lazy load,
  save on push, dedupe + 50-entry cap, malformed reads as empty; the
  find-in-path overlay keeps a third `findInPath` bucket there), injected
  per editor via `SetHistories` — nil (tests) disables recall.
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

## Mode visibility (#1323, #1353)

The current mode has four glanceable signals, all driven by
`editor.ModeColor(mode, palette)`: the **caret cell** is painted in the mode's
colour (`Model.cursorStyle`, formerly a plain reverse-video block), the caret's
**shape** changes with the mode, the **focused pane's border** takes the mode
colour, and the status line's mode segment renders as a badge in the same colour
(see [Status Line Segments](/architecture/status-line.md)). The mapping reuses
existing palette slots — Accent for `NORMAL`, Success for `INSERT`, Warning for
the visual modes, Error for `REPLACE`, Info for the `:` command line — so every
theme, built-in or third-party, carries it without a schema change; the text on
top is chosen by contrast (`theme.Readable`). A contrast test guards WCAG AA for
every built-in theme × mode pair. The caret colour rides the line cache safely:
a mode change always arrives through `Update`, which bumps the render epoch.

Colour alone turned out to be too quiet, so #1353 added the two signals that
read without comparing tones:

- **Caret shape.** Normal, the visual modes, replace and the command line draw
  the solid block. Insert draws a *double underline caret* in the mode colour
  over a light tint of it (`insertCaretTintFrac`), so the character under the
  caret stays readable — its contrast must keep 80 % of the theme's own body
  text contrast, capped at WCAG AA and floored at 3:1, which a test enforces per
  built-in theme.
- **Pane border.** `renderPane` gives the focused pane's border
  `editor.ModeColor` whenever `paneEditorMode` reports a non-Normal mode
  (insert green, visual yellow, replace red, command blue). Normal keeps
  `BorderFocus`, so the resting look is unchanged; an unfocused pane keeps the
  plain `Border`; and the move/tab drag colours (`MoveSource` / `DropTarget`)
  still outrank the mode colour. A pane whose active tab hosts a terminal
  (#573) reports no mode and keeps its plain chrome.

## Modes & keys

Normal mode resolves an optional `"reg`, an optional count, an operator, and a
motion / text object before committing. Secondary-key states (`awaitG`,
`awaitFind`, `awaitReplace`, `awaitObject`, `awaitRecordReg`, `awaitPlayReg`,
the mark states `awaitMark` / `awaitMarkLine` / `awaitMarkExact` (#1151),
the surround states `awaitSurr*` (#1475), and the label-jump session
`awaitLabel` (#787)) park the handler between keys.
Visual mode accumulates counts with the same 1–9/continuing-0 rule (#265), so
`V3j` extends the selection three lines and `3G` jumps inside a selection;
the count is consumed by its motion and Esc clears the pending state.
Beyond the core motions it also binds `~` (toggle case), `*`/`#` (search the
word under the cursor), indent operators `>`/`<` (and `>>`/`<<`), `H M L`
(screen top/middle/bottom), and screen scrolling via `Ctrl-f/b` (page),
`Ctrl-d/u` (half page) and `PgUp`/`PgDn`. The vim-parity pass (#1193) added
the `g`-prefixed layer — case/reflow operators `gu gU g~ gq` (double or repeat
for linewise, e.g. `guu`/`gugu`), `gv` (reselect the last visual selection),
`gi` (insert at the last insert position), `gJ` (join without a space),
`ge`/`gE`, `gf` (open the file under the cursor via `OpenPathMsg`, resolved
app-side), `g?` (explain the concealed or masked value at the caret, #1998),
and the display-line motions `g0 g$ gj gk` (visual rows under soft
wrap, plain line motions otherwise) — plus `zz zt zb` (scroll the cursor line
to centre/top/bottom next to the `z` fold keys) and `ZZ`/`ZQ` (save-and-close /
force-close, mirroring `:x` / `:q!`). Visual mode gained `u U ~` (case), `J`
(join), `r` (replace every selected character), `=`, and the `x`/`s` aliases
for `d`/`c`. The `g` prefix stays available while an operator is pending, so
`d ge` and `gu iw` compose. `Alt/Option+←/→` (and `Ctrl+←/→`) are
word motions clamped to the current line (#303) — `.` inside identifiers counts
as a stop point (`config.editor.tabWidth` yields sub-word stops), and past the
first/last word the caret lands on the line start/end instead of crossing
lines; cross-line word motion stays on vim `w`/`b`/`e`. `Alt+b`/`Alt+f` are
aliases for the same in-line word motions (#1583) — terminals that keep
macOS Option as a composition key synthesize Option+Arrows as the readline
sequences `ESC b`/`ESC f`, which decode to those chords. `Alt+↑/↓` (and
`Ctrl+↑/↓`) are paragraph jumps — all of these work in normal, visual and
insert. In insert mode `alt+delete` (with `ctrl+delete` and readline
`alt+d` as fallbacks) kills forward to the next word start, mirroring the
`alt+backspace` word kill (#1583). `Shift+arrows` (plus `Shift+Home/End`) are selection keys: in normal
mode they enter charwise visual mode anchored at the cursor and move; in visual
mode they extend the selection like their plain counterparts.
`Shift+Alt/Option+←/→` (and `Shift+Ctrl+←/→`) extend the selection by the same
in-line word motion; mid-insert they just move the caret. A selection started
this way is GUI-style (vim's `keymodel=stopsel`, #326): releasing Shift and
pressing an unshifted navigation key (arrows, `Home`/`End`, word/paragraph and
page keys) drops the selection and just moves the caret, while vim motions and
selections entered with `v`/`V`/`Ctrl+V` keep extending as in vim.
`editor.selectAll` (`Cmd+A`, JetBrains "Select All", #1861) selects the whole
buffer as a linewise visual selection — the `ggVG` equivalent — so the usual
follow-ups (`y`/`d`/typing, or Copy from a read-only buffer) act on it; an
empty buffer has nothing to select and is left in its current mode.

Surround operations (#1475, `surround.go`, vim-surround style):
`ys{motion}{pair}` wraps a motion or text object (`ysiw)`, `yse"`), `yss`
wraps the line from its first non-blank, `S{pair}` wraps a visual selection,
`cs{old}{new}` swaps the nearest enclosing pair and `ds{old}` removes it.
Pairs are `()[]{}<>`, the quotes and backtick; the opening member of a bracket
pair surrounds with inner padding (`( x )`), the closing member without
(`(x)`), and on delete/change the opening member also strips that same-line
padding — the vim-surround convention. `cs`/`ds` reuse `textobject.Pair`
(nesting-aware, multi-line) and `textobject.Quote` to locate the delimiters.
Each operation commits as one undo unit, records a `.`-dot that re-resolves
the target at the new cursor, and fans out per caret via `fanMutate` (#145).

Increment / decrement / value toggle (#1658, `increment.go`): `Ctrl-a` and
`Ctrl-x` in normal mode raise or lower the number under the cursor, or the
first one to its right on the same line, with the count as the step
(`5<C-a>`). A `-` directly in front of the digits is the sign, decimal
arithmetic saturates at the int64 bounds, and the literal keeps its shape —
leading-zero width (`007` → `008`), hex prefix, digit width and letter case
(`0x1f` → `0x20`, `0X00ff` → `0X0100`); hex wraps in 64 bits like vim. `g!`
toggles the value under (or after) the cursor between the members of a known
pair: `true`/`false`, `on`/`off`, `yes`/`no`, `enabled`/`disabled`, `==`/`!=`,
`&&`/`||`, `<`/`>`. Matching is case-insensitive over whole tokens — a word
run or an operator run, so `<=` never toggles as `<` — and the replacement
copies the original's capitalization (`True` → `False`, `TRUE` → `FALSE`).
`editor.toggle_pairs` adds `a=b` entries, matched before the built-ins so a
member can be redefined. All three commit through `fanMutate` (one undo unit,
per caret) and record a `.`-dot; they are also registered as
`editor.increment` / `editor.decrement` / `editor.toggleValue`, so the palette
reaches them and the keymap layer can rebind them where `Ctrl-a` is taken by a
terminal multiplexer.

Insert/Replace edits flow through an open `history.Recorder`; `Esc` commits
what is left in it and records the `.`-repeat. Arrow keys,
`Home`/`End` and the word/page keys move the caret mid-insert — `Home`/`End`
use the smart toggle described above, same as normal mode. Backward kills
work mid-insert too (#246), mirroring the terminal pane's macOS convention:
`option+backspace` / `ctrl+w` delete the previous word, `ctrl+u` deletes to
the line start, and `cmd+backspace` is IntelliJ's Delete Line (#955): the
whole current line goes, including the preceding line break (on line 0 the
following break instead), landing the caret at the end of the previous line —
all inside the running undo segment. An `undo`/`redo`
requested mid-insert (e.g. `Ctrl+Z` while typing) first **commits the open
insert session**, so it behaves identically from insert and normal mode.

**Undo granularity inside an insert** (#1818, `insertundo.go`). A long insert
used to commit as *one* change, so a single undo threw away everything typed
since entering insert mode. The session now closes the running recorder at the
boundaries a user thinks in and opens a fresh one — `commitInsert` only commits
the remaining tail, and `breakInsertUndo` is the split (it commits nothing when
the segment recorded nothing, so no break can produce an empty or duplicated
change):

- **A pasted block is exactly one change.** `Cmd+V` and bracketed paste
  mid-insert (`pasteIntoInsert`) close the running segment, splice the block
  into a recorder of its own and close that one too. One undo removes the
  block, no more and no less — and characters typed after the paste undo
  *before* it, even when they never form a whole word.
- **Typing splits word-wise.** A new segment opens in front of a word run that
  follows a separator, so a change is "one word plus the separators typed after
  it": typing `foo bar baz` leaves `foo `, `bar `, `baz`, and three undos peel
  the words off from the right (the JetBrains typing granularity; vim commits
  the whole insert, which is what the issue set out to fix). Trailing
  whitespace rides with the word before it, so undoing `baz` leaves `foo bar `
  with the caret where the next word would start.
- **A segment holding no word yet never splits.** The indent after `Enter`, the
  `(` that auto-closed into `()`, the space `o` opened the line with — leading
  separators belong to the word that follows them, so no undo tears a pair or
  an indent off the keystroke that produced it. Backspace and the kills,
  `Tab`/`Shift+Tab`, completion accepts and snippet expansions join the running
  segment as well (a correction belongs to the word it corrects) and reset the
  segment's typing state, so typing on after a correction never splits
  mid-word.

Normal-mode operations keep their one-change semantics (`dd`, `:%s`, and `ciw`
together with the word typed into it: the structural edit and the first typed
word share a segment). Redo mirrors the same boundaries in reverse, and each
step carries its own `CursorBefore`/`CursorAfter`, so the caret lands where
that word started / ended.

Smart indentation (Roadmap 0260, `indent.go`): with `editor.auto_indent` on,
`Enter` in insert mode and `o` compute the new line's indent from the
language's block openers (`lang.IndentAfter`, e.g. `:` for Python, `{ ( [` for
Go/PHP) — the reference text's leading whitespace, plus one `tabText()` unit
(honouring `use_spaces`/`tab_width`) when its trimmed form ends with an opener.
`Enter` keys off the part of the line **left of the cursor** (a mid-line split
indents by what stays behind); `o` uses the whole current line; `O` and
languages without rules keep plain copy-indent. Pure text heuristic — no
Tree-sitter — so an opener ending a trailing string literal false-positives;
accepted for v1. Inside an **embedded region** the rules come from the region's
language, not the host's (#1304): a JSON body in a `.http` file indents after
`{`/`[` and an XML body after `>`, while the host's own openers still govern
everything outside the region — see `indentOpeners`, backed by `lang.RegionAt`.
A region whose language has no rules falls back to copy-indent rather than
borrowing the host's. Mid-insert, plain `Tab` inserts one indent unit at the cursor
and `Shift+Tab` dedents the **whole current line** by one unit (the same
`dedentCols` unit as `<<` — one leading tab or up to `tab_width` spaces),
wherever the cursor sits; the cursor follows the removed columns, and the edit
stays inside the insert's running undo segment. While the completion popup is open a
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
insert's running undo segment — the pair is inserted by one keystroke, so one
undo removes both runes (#1818). The `.`-replay text records only the keystrokes, so a
fully typed `(x)` run replays exactly; an insert that never types the closer
replays without it (same approximation as backspace).

**Typing assistance** (#1326, `smarttyping.go`): punctuation a language
declares in `lang.SpaceAfter` gets its conventional space written as you type —
`':'` in JSON, so `"key":` becomes `"key": ` mid-keystroke. Gated on
`editor.typing.space_after_punctuation` (on by default; the Settings → Typing
Assistance page). The governing language is resolved like the indent openers
are: an embedded region's language wins over the host's, so a JSON body inside
a `.http` request follows JSON's conventions (#1304) — the case the issue came
from. Suppression is a pure text heuristic on purpose: highlighting is parsed
off the event loop and lags the keystroke by a frame, so the aid checks quote
parity and the language's line-comment marker on the line left of the caret
instead of a (stale) capture. No space is added when a space or tab already
follows. Like auto-close it decides per caret and rides the insert's running
undo segment; the `.`-replay records what the primary caret produced.

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
the gutter width and scroll offsets — to the cursor. The horizontal mapping is
display-cell aware (`displayClickCol`, shared with the mouse-idle hover
target): a tab occupies `tabWidth` cells (a click inside them lands on the
tab itself) and concealed markdown marker columns emit nothing (#881), so
clicks on tab-indented or concealed lines land on the character actually
under the pointer (#1529). Consecutive clicks on the
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
viewport via `ScrollBy(delta)`, which moves `view.Top` directly — clamped to
a bounded overscroll (#1134, #1535): scrolling stops with `bottomOverscroll`
(5) empty rows below the last line, never past the point where only the last
line remains, and a buffer that fits the pane does not scroll at all (soft
wrap and collapsed folds keep the looser last-line clamp) without touching the cursor or mode — it works the same in Normal,
Insert, Visual, etc., unlike the vim-motion scroll commands. Horizontal wheel
(or shift+wheel) scrolls sideways via `ScrollXBy(delta)`, moving `view.Left`
clamped so the longest visible line keeps its last character on screen (#230);
the next cursor motion re-derives the offset to follow the cursor again.
A vertical scrollbar with a JetBrains-style diagnostics error stripe (#1022,
`editor/scrollbar.go`) overlays the pane's rightmost content column whenever
the buffer has more lines than the viewport: a dim track, a heavier thumb
whose position/size mirror `view.Top` and the visible fraction (the shared
`internal/scrollbar` math and glyphs, #1367 — same bar as the explorer's), and a severity-
colored `■` marker at each cached diagnostic line's proportional track row
(worst severity wins a shared cell; markers draw over track and thumb). Mouse:
`ScrollbarHit` claims the rightmost column before any content click, so the
bar outranks text at that x. A left press on the thumb records the grab offset
(`ScrollbarPress` → true) and the app tracks a `dragEditScroll` drag kind
whose motion events call `ScrollbarDrag` — the viewport follows the pointer
with the grab point kept under it, clamped at both ends. A press on the track
above/below the thumb jumps the viewport to the proportional position.
Right-click (context menu) and left drags on content (selection) are
untouched; the bar renders only as an overlay, so the rendered text width and
the click mapping never shift when it appears.

Two consumers do reserve the overlaid column, because their content would
otherwise be unreadable under the bar: right-aligned row annotations budget
against `annotColumnWidth` (`fold.go`, #1728), and horizontal cursor-following
plus the soft-wrap break width use `Model.scrollTextWidth()` (#1827) — the text
width minus one while `scrollbarGeometry()` reports a visible bar. Without it
the follow logic parks the caret in exactly the column the bar covers, and the
user cannot tell whether the line continues behind it. `TextWidth` itself stays
untouched: rendering fills the full width and the bar draws over it.

**Decoration toggles (#1259, `editor/marktoggles.go`).** The `editor.marks.*`
config switches gate which mark classes decorate the buffer, per source and
severity: `lsp_errors`/`lsp_warnings`/`lsp_info`/`lsp_hints` for LSP
diagnostics and `git_added`/`git_changed`/`git_deleted` for the VCS line
marks. A disabled class disappears consistently from the scrollbar stripe
(including click-to-jump targets), the gutter colouring and the inline
underlines — `sevVisible`/`gitVisible` are consulted at all render seams, and
a toggle flip bumps the diags/marks epochs so the memoized stripe rebuilds.
The diagnostic *data* stays complete: the details popup (#739), the
diagnostic jump (#369) and the Problems window keep the full set, so a hidden
severity is still reachable, just not painted. All switches live in the
settings panel's Diagnostics page and apply live.

Resting the pointer over content for ~600ms opens the hover popup at the
hovered cell (#1129, mouse-idle hover): the diagnostic covering the cell
shows immediately, LSP hover content follows when a server answers.
`HoverTarget(x, y)` is the read-only hit-test (gutter, scrollbar, sticky
headers, and cells past the line text are not targets); idle tracking and
scope guards live at the app layer — see [LSP](./lsp.md) for the full flow.

## Goal column for vertical movement (#1687)

`desiredCol` is the **goal column** (vim's `curswant`): the column vertical
movement aims at, independent of where a short line forced the cursor to
stop. Linewise motions — `j`/`k`, `Up`/`Down`, `PgUp`/`PgDn`, `Ctrl-f/b/d/u`,
`G`/`gg`, and their insert-mode arrow equivalents — put the cursor at
`desiredCol` on the target line and leave `desiredCol` alone; the buffer clamp
does the rest. Crossing a short line therefore clamps the caret to that line's
end, and the next long enough line restores the original column. Everything
else — horizontal motions, `Home`/`End`, mouse clicks, jumps, and every edit
(`moveTo`, `applyMutate`, `fanApply`) — resets `desiredCol` to the new cursor
column. Secondary carets carry their own `desiredCol` and follow the same rule
(see [Multi-caret editing](#multi-caret-editing-145)).

## Label jump (#787)

`labeljump.go` is the easymotion / leap.nvim-style motion: `gs` in normal mode
(or `editor.labelJump`, action `label_jump`) opens a session, the next one or
two typed characters (`leapMaxQuery`) select the visible matches
(case-sensitive, collected by `collectLeapTargets` mirroring the `View` body
loop — below the sticky headers, skipping fold-hidden lines, clipped to the
horizontal window unless soft wrap), and every match is overlaid with a short
label. Typing a label lands via `jumpTo`, so the departure records in the
navigation history like a search landing; esc cancels with the cursor
untouched; a unique match autojumps without a label.

The session is a `leapState` pointer on the Model plus the `awaitLabel` wait
state; `Capturing()` reports true while it lives, because target and label
characters include keys the app layer otherwise claims in plain normal mode
(`q`, `tab`, `@`). Key resolution is never ambiguous by construction:
`assignLeapLabels` excludes every rune that could still extend a match from
the label alphabet (`labelAlphabet`, home-row first), so a typed key either
narrows the query or picks a label, never both. Targets sort
nearest-to-the-cursor first (line distance, then document order) so the
closest get the most comfortable keys; past the alphabet, `leapLabels`
reserves tail keys as two-character prefixes (prefix-free — a reserved key is
never also a single label), and targets past the pair capacity stay unlabeled
until the query narrows. Rendering slots into `renderSpanUncached`: the label
glyph (`leapLabelAt`) wins over every decoration including the cursor, the
typed span (`leapMatchAt`) highlights search-match-style below cursor and
carets; once a two-character label's first key is typed, only its targets keep
an overlay, showing the remaining key. No cache work is needed — every session
mutation is key-driven, and `updateMsg` bumps `renderEpoch` per routed message.

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

The **bookmarks picker** (`nav.bookmarks`, `cmd+f3`) lists the focused
editor's local marks plus all globals as `'x  path:line  preview` rows and
the project's line bookmarks (see below) as `⚑[digit]  path:line` rows; enter
jumps (everything with a path through the open funnel), shift+delete or the
`✕` zone removes the entry (the #842/#1113 prune pattern). See
`internal/app/bookmarks.go`.

## Project bookmarks (#55)

Beside the vim marks sits the JetBrains flavour: **line bookmarks** owned by
the project rather than by a view. `internal/bookmarks` is the store — keyed
by path (project-relative, the breakpoint store's `bpKey`) plus 0-based line,
each bookmark carrying an optional **mnemonic** digit (`0`-`9`, unique across
the project) and a free-text **note**. It persists in `.ike/bookmarks.json`
(`IKE_CONFIG_DIR` override), saved on every change and on each buffer save;
a missing or malformed file loads empty, never a startup error.

Commands (`internal/app/bookmarks_store.go`), all on the focused editor's
cursor line:

| Command | Chord | Effect |
| --- | --- | --- |
| `bookmark.toggle` | `f11` | set/clear an anonymous bookmark |
| `bookmark.toggleMnemonic` | `alt+f3` | digit prompt: assign `0`-`9`; the digit already on the line removes the bookmark |
| `bookmark.jumpMnemonic` | palette | digit prompt: jump to the bookmark carrying that digit |
| `bookmark.annotate` | palette | note prompt, prefilled with the current annotation; an empty note clears it |
| `bookmark.next` / `bookmark.previous` | `shift+f11` / `ctrl+shift+f11` | step through all bookmarks in (path, line) order, wrapping |

The prompts run in the floating shell, the save-layout prompt's shape
(#1175): the mnemonic flavours consume a single digit (any other key is
ignored, esc cancels), the note flavour is a line editor with enter/esc. Both
mnemonic prompts list the assigned digits with their file, line and note.

The gutter renders a bookmark's **mnemonic digit** where it has one and the
same `⚑` as a vim mark where it does not (`bookmarkSigns`, injected via
`SetBookmarkHooks`) — the digit outranks the flag on a line that carries
both. Bookmarks shift with edits through the same delta scheme as marks and
breakpoints (two bookmarks squeezed onto one line merge, the lower source
line keeping the slot) and follow renames/moves: `followMovedFile` re-keys
the store, directories included.

Still open from the idea issue: a bookmarks tool-window pane and toggling by
clicking the gutter (pairs with #30).

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
- `editor.caret.addAbove` / `addBelow` (`alt+shift+up` / `alt+shift+down`,
  #1481): each press clones a caret one line above the top-most / below the
  bottom-most caret, growing a caret column for same-column edits in
  line-based files. The clone keeps the extreme caret's `desiredCol`: on a
  shorter line it clamps to the line end, but the next clone continues at the
  original column. Buffer edges are a no-op.
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
fan-out is a single undo unit — insert-mode typing joins the insert session's
running segment recorder (#1818), one-shot operations commit via `fanMutate`. Fanned today:
insert-mode typing / Enter (per-caret smart indent) / backspace / word- and
line-kills / Tab / Shift+Tab (one dedent per line), `x`, `r`, operators
`d c y` with motions and text objects, `dd cc yy` (merged to one caret per
line first), `p`/`P`, `o`/`O`, `a A I s`, the surround operations `ys`/`cs`/`ds`
(#1475), `Ctrl-a`/`Ctrl-x`/`g!` (#1658), and completion accept (the popup
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
nothing mutated, restoring cursor and viewport. Both fields are ordinary
single-line inputs (`ui.EditKey`, #2002 — they used to be append-only
strings): each keeps its own cursor across a `tab` switch, word motions and
the `opt`/`cmd` kills work, a paste lands at the cursor, and `ctrl+u` still
clears the active field. A mid-field edit re-runs the incremental preview
like typing at the end always did. The delimiter is picked to
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

### `:sort` (`sort.go`)

`:[range]sort[!] [flags]` (short form `:sor`) reorders the range's lines. It is
the one range command whose **default range is the whole buffer** instead of the
current line, matching vim — sorting one line is never what was meant.

- **Flags:** `u` drops duplicate lines, `n` compares the first decimal number in
  the line (a directly preceding `-` is part of it; lines with no number sort
  before every numbered line), `i` compares case-insensitively. They combine
  (`:sort un`), and an unknown letter is an error that leaves the buffer alone.
  `!` inverts the comparison. Vim's `r` flag and `/pat/` key are not supported.
- **Stable:** lines whose keys compare equal keep their original order, and `!`
  inverts the *comparator* rather than reversing the result, so stability holds
  in both directions. Lines are decorated with their keys once before sorting,
  so the comparator allocates nothing.
- **`u` compares whole lines** (case-folded under `i`), not the sort key, and
  only drops a line equal to the one before it — so under `n` two lines sharing
  a number are both kept.
- **One undo unit:** the range is rewritten as a *single* `buffer.Edit` spanning
  `[start, end]`, so `u` reverts the whole sort and the deletion side of `u` is
  part of the same edit. An already-sorted range is a no-op that records
  nothing (the buffer never goes dirty) and reports *already sorted*; otherwise
  the message is *sorted N lines[, M duplicates removed]*. The cursor lands on
  the range's first line.

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
to an ordinary edit. **Undo history restarts on a content-changing reload**:
the old change records describe positions in text that no longer exists, so
replaying them could corrupt the buffer; losing the stack is the documented
trade-off. A reload whose decoded disk content **equals the buffer** is a
no-op (#1406): nothing is reloaded, so nothing resets — the undo history
survives, the current history node is marked as the saved state, and dirty and
stale clear. This makes a save always a checkpoint, never a history reset,
even when an own-write event slips past the watcher's suppression.

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

## Read-only buffers (#1762)

`ShowReadOnly(path, text)` (`readonly.go`) installs content that can never be
written back: the full editor — motions, search, highlighting, folds — over a
buffer whose `path` is a *display* path with no writable home on disk. The
[archive viewer](./archive-viewer.md) uses it for an extracted archive entry
under `<archive>!<entry>`, whose tail is the member's own file name so the
language lookup, the highlighting and the tab title resolve without special
casing.

It is deliberately not the dependency guard above, which blocks the *first*
edit and unlocks on confirmation — here there is nothing to unlock into. The
same mutation entry points (`mutate`, `beginInsertChange`, `startInsertWith`,
the insert entries in `normalCommand`) refuse outright, leaving
`E45: buffer is read-only` on the ex line, and `newRecorder()` returns a locked
recorder as the backstop. `saveAs` — the single funnel under `:w`, `:wq`,
`:w other`, `SaveTo` and the focus-leave autosave — fails with the same reason,
so the display path never becomes a file. `Load` and `NewFile` clear the flag:
reusing the view for a real file unlocks it. Nothing persists: `diskHash` stays
empty (no persistent undo), the host skips such tabs when saving the layout and
never puts them in the reopen ring.

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
formatting — routed through the [formatter registry](./format.md) since
#1401, so external and built-in formatters apply on save too — then the
actual write. Because both steps are async server
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
  done message always fires. No capable source (no resolvable formatter
  provider for the format step, no server offering the organize-imports kind
  in `codeActionKinds`) means no chain at all: the write happens immediately.
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

## Buffer language — "Treat Buffer as …" (#2033)

An untitled buffer used to be typeless: everything language-derived in this
package resolves a *path* (`lang.ByPath`, `highlight.Lang`), so pasted JSON,
Markdown or an HTTP request got no highlighting, no concealing, no rendering
and no type-specific intentions until it was saved with the right extension.
`Model.SetLangOverride(id)` gives the buffer a language without a file
(`langoverride.go`): the id resolves to a **synthetic name** — `buffer.md`,
`buffer.http`, `Dockerfile` — that `langPath()` hands to every lookup in this
package in place of the empty path, so highlighting, comment toggling, indent
rules, snippets, the conceal filter, markdown rendering, the csv table layer
and log rendering all behave like a file of that type. `Path()` stays empty:
the synthetic name never reaches file I/O.

The choice lives as long as the buffer does, travels to a split
(`ShareDocumentWith`, like the encoding), is changeable at any time and is
cleared the moment the buffer gets a real path (`Load`, `NewFile`, `:w name`)
— from then on the file name classifies it, as it does for every other file.
Switching the language invalidates the version-keyed rendering caches along
with the highlight state, so the new type shows on the next frame instead of
after the next edit. The parse result finds its way back through
`ParseKey()` — the file path, or a unique per-view tag when there is none —
because routing by path skipped every path-less buffer. Completion travels
the same two names (#2048): every emitted `Event` carries `Key` (the
`ParseKey`) and `LangPath` (the `langPath()`), so the local sources index the
buffer under its own identity, gate on its language, and route their answer
batch back to this view — see
[Completion Engine](/architecture/completion.md#buffer-identity-and-language-2048).
The user-facing picker, the alt+enter entry and the
status-line marker are described in
[Language Registry](/architecture/languages.md#buffer-language-override--treat-buffer-as--2033)
and [Intention Actions](/architecture/intention-actions.md#treat-buffer-as--2033).

## Language detection on paste (#2037)

The override above still needs a decision. The common case makes it for the
user: content pasted into a **fresh, file-less buffer** is classified from the
content itself. Every paste funnel in this package — vim `p`/`P` (`paste`), a
clipboard paste in normal or insert mode (`clipboardPaste`,
`pasteIntoInsert`), a visual-mode put (`visualPaste`) and the terminal's
bracketed paste (`PasteText`) — takes a candidacy snapshot *before* the insert
and classifies *after* it (`langdetect.go`). Nested funnels (`clipboardPaste`
→ `paste`) re-check the gates, so a paste is never classified twice.

The detection itself is `lang.DetectContent` — pure, table-tested and
conservative, see
[Language Registry](/architecture/languages.md#content-detection-on-paste-2037).
It recognizes JSON, CSV/TSV, YAML, Markdown, XML/HTML, `.http` request blocks
and curl commands (as `shell`); anything else stays plain text.

Three properties keep it from becoming a nuisance:

- **It fires into a blank slate only.** No file (a path classifies), no
  override already set (a decision — the user's or an earlier detect — is
  never overwritten), and nothing but whitespace in the buffer before the
  paste. A paste into existing content never retypes it.
- **It is silent on failure, quiet on success.** An unrecognized paste says
  nothing; a recognized one emits one `NoticeMsg` naming the type
  (`buffer language: detected json — alt+enter to change`). No modal, no
  confirmation, nothing that interrupts the paste. The toast is parked in
  `Model.detectSignal` because the vim paste paths return no command of their
  own, and drained by `maybeReparse` (every buffer change passes it) or by
  `PasteText`, which bypasses it.
- **A wrong verdict costs nothing.** The language is not part of the
  document: the #2033 picker changes or clears it without touching the text.

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
  (one text, one undo stack), while cursor, scroll, and mode stay per pane
  (registers are app-wide anyway, #1540). Session restore deduplicates the
  same way.
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
toggled by `editor.markdown_rendering` (default on, in Settings → Editor) or
per view by the `view.toggleMarkdownRendering` palette command (#1599, a
sticky override like the #64 view toggles; it also gates the `markup.*` text
attributes in `styleAt`):

- **Inline attributes** (all lines): the inline grammar's `markup.*` captures
  render as terminal text attributes — `**bold**` bold, `*italic*` italic,
  `~~strike~~` struck through — composed in `styleAt` over whatever color the
  theme resolves.
- **Concealment** (positional, #1594): the query captures the marker chrome
  (`**`, `*`, `` ` ``, link `[]()` + destination) as `@conceal`; the
  `SpansMsg` handler splits those spans out of the style index into per-line
  column ranges, and `renderSpan` skips those cells so the line reads like
  rendered text. A range shows its raw source only while the caret sits
  *inside* it — or a selection intersects it — with the rest of the line
  staying rendered (`lineConcealRanges` filters revealed ranges;
  vim's concealcursor granularity). Mouse clicks map back through the hidden
  ranges (`displayClickCol`), so the cursor lands on the character that was
  clicked; buffer-column motions and selections are untouched by design.
- **Adjacent reveal** (#1686): the *value* families widen that window by one
  column on each side, so a caret sitting directly before or directly after
  the range reveals it too — appending or prepending digits puts the caret
  next to the literal, not in it, and typing against a stand-in without seeing
  the real value is confusing. The families are listed in
  `adjacentRevealCaptures` (decoded epochs #1618, the four number-hint
  captures #1627); `lineConcealRanges` marks the copies it combines with
  `concealRange.adjacent` and `inRange` widens the caret window for those.
  Everything else — marker chrome (#881), masked secrets (#1623), the sv
  separator padding (#1589), the generic `Replace` conceals — keeps the strict
  inside-only rule, since widening dense per-column ranges would flicker the
  whole line while moving through it. Selection reveal is unchanged: a
  selection has to intersect the range itself.
- **Span-extent reveal** (#1599): the query additionally captures the
  enclosing inline spans (emphasis, code span, links) as `@conceal.extent`;
  `concealSplit` routes them into a third channel (`concealExt`), and a caret
  anywhere *inside* such a span — not only on a marker — reveals the conceal
  ranges the span contains, so `` `code` `` shows both backticks while
  edited. Extent spans never reach the style index (`NewIndex` drops them —
  indexed, the node would shadow everything inside it under
  first-covering-wins).
- **Stand-in conceals** (#1585): a span carrying a `Replace` string (produced
  by a language's Go span hook, e.g. `.http` percent-encodings — `%20`
  decodes to a space, `%C3%A4` to `ä` across all six source columns) conceals
  its range behind the replacement glyph, rendered in the range's own capture
  style with a subtle background tint (`SelectionMuted`) so it reads as a
  decoded stand-in even when it decodes to a space (#1594). The same
  positional reveal applies, and `displayClickCol` / `DisplayOffset` account
  for the collapsed width.
- **Horizontal cursor following** (#1752): `view.Left` stays a raw rune column
  outside the sv table path, but the window it opens is measured in display
  cells — and a stand-in rarely has its source's width. `Model.scroll()`
  therefore hands the offset to `concealScrollFix`, which redoes the follow
  decision through `concealPrefix` (per-column display-cell prefix sums of the
  line's active conceal ranges): both the caret and the current offset convert
  to display cells, and the smallest offset keeping the caret inside the scroll
  width (the text width, less the scrollbar's column — #1827) wins. Restarting from the pre-`view.Scroll` offset keeps the raw
  comparison from leaving a scroll of its own behind, so a mask *wider* than
  the value scrolls when the caret visually reaches the right edge and a
  *narrower* one never over-scrolls. An offset landing inside a stand-in snaps
  past the range — `renderSpan` emits a replacement only at its range start, so
  none of it would render from there. Lines without conceal ranges keep the
  plain rune-column result.
- **Conceal-aware soft wrap** (#1756): under soft wrap the same distortion
  would move to the wrap points — `wrapSegs` used to split on raw rune columns,
  so a stand-in wider than its source overflowed the visual row and a narrower
  one broke rows early. On a line carrying conceal ranges `wrapSegs` now hands
  the `concealPrefix` sums to `viewport.WrapSegmentsDisplay`, which budgets
  each column's display cells (a stand-in its replacement's width at the range
  start, hidden columns nothing) instead of one cell per rune. Zero-width
  columns never start a row, so a break only lands where something renders and
  never inside a stand-in. Segments stay rune-column starts, so every consumer
  — `ScrollWrapped`, `wrapVertical` (gj/gk), `wrapRows`, `wrapClickAt`,
  `DisplayRow` and the per-segment `renderSpan` slicing — follows unchanged.
  Lines without conceal ranges wrap on raw rune columns exactly as before.
- **List markers** (#1966, `mdlist.go`): every list marker becomes a *dynamic*
  conceal range (like the sv separator padding #1589) whose stand-in is the
  indented marker — `-`, `*` and `+` render as a two-cell indent plus a `•`
  bullet, an ordered `1.` as its number right-aligned to the widest number of
  its own list, so a list crossing the 9 → 10 width boundary keeps its dots in
  one column (` 9.` under `10.`). Lists are detected from the buffer text
  (`detectListRanges`, grammar-free like table detection, fenced code blocks
  skipped) and cached per document version; a run is the consecutive items
  sharing one source indent — a paragraph at or left of that indent ends it,
  deeper text is item continuation, and a nested list aligns on its own widest
  number, keeping the item's source indent in front of the stand-in. Thematic
  breaks (`---`, `* * *`) are rules, not items. The caret on a marker reveals
  the raw source like any other conceal range.
  **Continuation lines** (#1975) are padded the same display-only way: a plain
  text line indented deeper than the innermost open item conceals its leading
  whitespace behind a run of spaces as wide as that item's *text* column
  (source indent + the two cells + the stand-in marker + the space after it),
  so wrapped item text lines up under the first character instead of under the
  bullet. An ordered item's column is only known once its run is flushed, so
  those pads are emitted with the run's number alignment. Nested items are
  items, not continuations; fenced blocks inside an item, blank lines and
  lines already indented past the text column stay untouched, as do items
  whose indent holds a tab (rune columns would lie about display width). The
  caret in a padded indent reveals the raw source.
- **Pipe tables**: detected from the buffer text (a pipe row above a `|---|`
  delimiter row — equivalent to the grammar's `pipe_table`, but it also works
  in `CGO_ENABLED=0` builds), re-rendered with box-drawing characters, cells
  padded/aligned per the delimiter row's `:` colons. **Row-preserving**: the
  delimiter row becomes the `├─┼─┤` separator and no border rows are added,
  so line↔row mapping and the gutter stay 1:1. Under soft wrap tables stay
  raw (wrap segments slice raw buffer text; a sliced box-drawing row would
  tear); with horizontal scroll the rendered row is sliced by the same column
  window as any other line (ANSI-aware, since the rows carry styling).
- **Per-cell table reveal** (#1599): the cursor entering the block no longer
  flips it raw — the frame stays box-drawn and only the cursor's cell shows
  raw source (`tableCursorRows` re-renders the block with that cell
  untrimmed, left-anchored and cursor-styled, columns growing to fit). The
  cursor on table chrome — a pipe, the delimiter row, the indent, past the
  last cell — reveals its whole row raw instead (reaching the cell edge
  de-conceals the borders). Secondary carets or a selection touching the
  block still render it fully raw (their styling only renders through the raw
  cell loop). Clicks and overlay anchors map between display and source
  columns through the cell layout (`tableClickCol` / `tableDisplayCol`, keyed
  on the block's stored column widths): border clicks land on the pipe they
  draw, cell clicks inside the cell's source segment — exact in the
  raw-revealed cell, approximate in rendered cells.
- **Cell inline rendering** (#945): cell content renders its inline markdown
  inside the box-drawing rows — the per-line conceal/style pipeline cannot
  follow text into the re-laid-out cells, so `renderCellInline` is a small
  self-contained renderer (grammar-free, like table detection): `` `code` ``
  and link text take their theme capture styles (@string / @label),
  `**bold**`/`__bold__`, `*italic*`/`_italic_` (word-boundary underscores
  only) and `~~strike~~` become text attributes with nesting, `[text](url)` /
  `![alt](url)` show just the text, `\`-escapes and unmatched markers stay
  literal. Column widths and alignment size by the concealed display width.

## Separator-delimited table rendering (#1589)

Csv/tsv/psv buffers render table-like, display-only (`svtable.go`), toggled
by `editor.csv_rendering` (default on, Settings → Editor):

- **Rainbow columns**: the csv language plugin (`plugins/languages/csv`, no
  grammar) emits one span per field through the Go span seam
  (`lang.Language.Spans`, #1585) with the theme-derived `rainbow.<col%6>`
  captures of #789, plus `punctuation` on the separators — so raw lines (the
  caret line, selections, toggle off) are already rainbow-csv colored.
- **Aligned rows** (no soft wrap): each separator becomes a *dynamic*
  conceal range (`svConcealRanges`, #1594) whose stand-in is the column's
  alignment padding — the column width minus the field's own length plus the
  two-cell gap — so rows render aligned through the ordinary cell loop, and
  cursor, selection and search styling all work on aligned rows. Only the
  separator the caret sits on — or a selection crosses — reverts to its raw
  character (the shared positional reveal in `lineConcealRanges`). Widths
  come from `svLayout`: the widest field per column across the *visible*
  rows plus the header row — viewport-bound, so large files never measure
  beyond the screen. The layout hash folds into the line cache's validity
  check (`svCacheState` in `syncEpoch`): a vertical scroll can change widths
  without an epoch bump; non-sv buffers hash to zero and keep their scroll
  cache reuse.
- **Pinned header**: `stickyLines` returns line 0 for sv buffers once the
  view scrolls (gated by `editor.sticky_scroll` like code headers), so the
  title row rides the existing sticky rendering, click remap and
  `unhideCursor` plumbing.
- **Mouse and overlays**: the generic conceal mapping in `displayClickCol` /
  `DisplayOffset` handles the padding stand-ins — a click in the padding
  lands on the concealed separator.
- **Horizontal scroll** (#1724): while the table rendering is active,
  `view.Left` counts *display cells* of the aligned row, not raw rune
  columns — slicing raw text would shed a different width per row (the
  conceal expansion differs) and the shared column edges would drift. Each
  row maps the offset back into its own expansion (`svDisplaySlice`: the
  buffer column to start at, plus the tail of a stand-in the window edge
  landed inside), so every row — the pinned header included — loses the same
  width. The caret is tracked by its expanded column (`svDisplayCol` in the
  cursor-follow scroll path), `displayClickCol` / `DisplayOffset` fold the
  offset through the expansion, and `ScrollXBy` clamps against the expanded
  row width.
- **Right pane edge** (#1847): a stand-in wider than the cells left in the
  span renders its fitting prefix (`clipCells` in the cell loop) instead of
  being skipped — clipped exactly like a tab straddling the edge. Skipping it
  left the padding's cells free for the *next* column, which then rendered
  glued onto the short field (`Schweiz+49…`, `landtelefon`) while the widest
  field of the same column merely clipped; only fields shorter than their
  column were affected, because only those carry padding beyond the gap. The
  clip applies to every stand-in, decoded ones (#1585) included.
- **Caret column** (#1659): the field the caret sits in (`sv.IndexAt`) is
  tinted over the whole visible height — `svColumnRange` gives each rendered
  line the rune range it contributes, the field plus the separator closing it
  so the stripe covers the alignment padding and reads as one block. The tint
  (`svColumnTint`) is the selection colour mixed toward the surface, so it is
  subtler than a selection on light and dark themes alike, and it is the
  *lowest* background layer: ruler, conflict and occurrence tints paint over
  it, cursor/selection/search win outright. Cost is one split of each visible
  line — rows without that column simply stay untinted, and an unfocused pane
  shows no stripe, like it shows no caret.
- **Column in the status line** (#1659): the `svcolumn` segment shows
  `column <n>: <header>` from `Model.SVColumnLabel` — the name comes from the
  first row via `sv.Header`, which accepts it as a header only when every
  field is non-empty and non-numeric (`sv.Unquote` strips quotes and padding
  for display). Without a header-ish first row, or for a column past its
  field count, the label is the bare `column <n>`. The segment is empty — and
  so hidden — for every non-table buffer, including `editor.csv_rendering`
  off.
- **Column-true vertical motion** (#1744): `j`/`k` (and every other linewise
  motion — visual mode, insert-mode arrows, page scroll) keep the caret in the
  *same field* instead of carrying the raw `desiredCol` over, which lands in a
  different field as soon as two rows pad their fields differently.
  `svVerticalCol` translates the remembered column into (field index, offset in
  field) and maps it back onto the target row: the offset clamps to that row's
  field length, and a row with fewer fields clamps to its last one. The want
  (`svWant`) travels with the motion, so the original offset comes back on the
  next wide row — desiredCol's "across short lines" rule, one level up. It
  validates itself against `(cursor line, desiredCol)` rather than being reset
  at each of desiredCol's assignment sites: any horizontal motion writes
  `desiredCol = cursor.Col` and so drops it. Non-sv buffers, `editor.csv_rendering`
  off and soft wrap (i.e. `svActive` false) keep the raw column unchanged, and
  secondary carets keep their own per-caret `desiredCol`.
- **Column profile** (#1940): `csv.columnProfile` ("CSV: Column Profile",
  palette, editor context) profiles the *caret's* column — rows, nulls,
  empties, distinct values, min/max, the ten most frequent values with their
  counts, plus the mean of a numeric column or the length range of a text one.
  `Model.SVProfileTarget` is the whole editor-side seam: the field index, its
  header name (`sv.Header`, else `column <n>`), the separator and the raw
  lines. The aggregation itself is `datasrc.ProfileCSV` — the same scan the
  Parquet backend uses (see [data viewer](./data-viewer.md)), so a csv column
  and a database column profile identically. A text buffer has no query
  engine, so the scan is **bounded at `datasrc.ProfileLimit` (100 000) rows**
  and the popup says `first 100000 rows only (scan capped)` when it caps.
  Values are unquoted like a header name is; a row too short to reach the
  column contributes a NULL, which is what keeps "empty here" apart from "no
  such field". The result opens in the floating shell (esc closes, `y`
  copies exactly the shown lines), and the scan runs as a background command
  so a million-line file costs no keystroke.
- **Quoting**: field splitting (`internal/sv`, shared with the plugin so both
  sides split identically) honors `"…"` regions — a quoted separator is
  literal, `""` escapes a quote. The csv separator is sniffed (`,` vs `;`)
  over the first lines; tsv and psv are fixed. Column highlight and header
  naming go through the same parser, so an embedded separator never shifts a
  column.

The buffer never changes — alignment is virtual padding only.

## JSON/YAML path breadcrumb (#1660)

In a deep manifest, CI config or lockfile the structure panel answers "what is
in this file" top-down; this answers "where am I" cursor-first, and makes the
answer copyable. `internal/docpath` derives the path, `internal/editor/docpath.go`
caches and renders it.

- **Derivation** is structural, not a parse: no document is loaded, no value
  unmarshalled, no second parser added. JSON runs a container stack over the
  buffer (`{`/`[` push, `}`/`]` pop, a string in key position names its object
  frame, a comma at array level advances its index) with strings and JSONC
  comments skipped, so a brace inside `"a } b"` closes nothing. YAML is
  indentation-driven: every line contributes its `- ` dashes and its `key:` at
  the columns they sit at, and a column pops the frames it is no longer nested
  inside — including the legal spelling where a sequence shares its parent
  key's column. Block scalars (`|`, `>`), `#` comments and `---` document
  boundaries are honored, and a flow value (`{a: 1}`, `[1, 2]`, also across
  lines) hands off to the JSON scanner, since inside `{ }` YAML *is* that
  grammar.
- **Graceful degradation** falls out of the design: a scan only ever reads up
  to the caret, so a buffer that is unfinished or broken below it still yields
  the nearest enclosing node, and an unbalanced closer finds an empty stack.
  Same for a caret in whitespace or on a blank line — the enclosing node stays.
- **Anchors and aliases** are reported *as written*: `<<: *base` is the `<<`
  key, an alias is never followed. A path that silently resolved one would name
  a location the file does not contain; resolution is #1629's job.
- **Status line**: the `docpath` segment (`Model.DocPathLabel`) shows
  `spec.template.containers[2].env[0].name`, truncated **from the left** with a
  leading `…` at 44 cells — the tail names where the caret actually is. Empty,
  and so hidden, at the document root and in every buffer without a scanner
  (see [Status Line Segments](/architecture/status-line.md)).
- **Copy commands** take the full, untruncated path in three flavours:
  `editor.copyDocPath` (dotted, `cmd+alt+shift+c`), `editor.copyDocPathJQ`
  (`.spec.containers[2].name`, non-identifier keys bracket-quoted) and
  `editor.copyDocPathYQ` (yq v4's spelling, `."my-key"`). They write the `+`
  register with the #1255 clipboard-failure toast, like Cmd+C.
- **Cost**: the scan reads from the first line to the caret, so it is cached
  per document version *and* caret position (`docPathCache`, a pointer field
  shared across the value copies like `svTable`, reset per view on a document
  share) and the whole feature is off in large-file mode (`InsightOff`), like
  every other whole-buffer analysis.

## Log-file rendering (#1621)

`.log` buffers render readable, display-only, toggled by
`editor.log_rendering` (default on, Settings → Editor) or per view by the
`view.toggleLogRendering` palette command (an override that sticks like
`mdRenderSet`). The log language plugin (`plugins/languages/log`, no grammar)
emits everything through the Go span seam (#1585); the parsing lives in
`internal/logline`, the editor-side style resolution in `logrender.go`:

- **Line parser** (`logline.Parse`): recognizes the header of the common
  layouts — logback/log4j (`2024-01-02 10:11:12,345 [main] INFO logger -
  msg`), slog/zap text and logfmt (`time=… level=… msg=…`), syslog (`Jan  2
  10:11:12 host proc: …`) — by classifying the leading whitespace tokens;
  the first unrecognized token ends the header, so stack-trace frames and
  plain lines fall through unstyled (graceful fallback).
- **Severity colors**: the level token captures as `log.error` / `log.warn`
  / `log.info` / `log.debug`; `styleAt` routes `log.*` / `ansi.*` captures
  through `logStyle`, which prefers an explicit `theme.captures.log.*`
  override and otherwise draws from the palette — `Error`/`Warning` for the
  two loud buckets, terminal *faint* for `log.time` and `log.debug`.
  Debug/trace lines additionally get a whole-line `log.debug` span emitted
  last, so the header spans win where they overlap and the message dims.
- **Dimmed timestamps**: `log.time`, faint, so message text stands out.
- **Logfmt pairs** (#1633, `logline.ScanPairs`): `Parse` stops at the first
  key it does not know — everything after is message payload — but logrus and
  slog text output keep emitting pairs past that point. A second pass scans
  the *whole* line for `key=value`, quote aware (`msg="Session done"` stays
  one pair, `\"` does not close it), and classifies each key: every key plus
  its `=` captures as `log.key` (faint, so the structure recedes),
  `msg`/`message` values as `log.message` (bold), `time`/`ts`/`timestamp`/
  `datetime`/`date` values as `log.time` (a docker/containerd line's
  duplicate stamp dims like the leading one), `level`/`lvl`/`severity`/`sev`
  as the matching severity capture and `logger`/`thread`/`caller`/`name`/
  `module`/`component`/`source` through the rainbow mechanic. Plain values
  keep the default foreground — the dimmed keys carry the structure. Ranges
  the header already emitted are not re-emitted.
- **Logfmt fallback**: pair styling only applies when `logline.Logfmt` holds —
  at least two pairs, or one recognized key. A lone `x=42` inside a prose
  sentence renders unchanged.
- **Rainbow threads/loggers**: thread and logger names capture as
  `log.rainbow.<fnv(name)%6>` — the #1589 palette mechanic keyed on the
  *name hash* instead of the column index, so one thread keeps one color on
  every line and interleaved threads separate visually.
- **ANSI escapes** (`logline.ScanSGR`): every CSI sequence conceals via the
  ordinary parse-produced conceal pipeline (#881's `@conceal` capture, the
  positional caret/selection reveal of #1594 included — the gate in
  `lineConcealRanges` keys on `logRender` for log buffers instead of
  `mdRender`). SGR sequences additionally drive a running style; the text
  between them carries an `ansi.<spec>` capture (`fg1.bold`, `fg#0080ff`)
  that `logStyle` resolves against the theme's terminal palette (#1363) for
  indexes 0–15, 256-color indexes and truecolor literals directly.
- **Repeat collapsing** (#1650, `logfold.go`): a polling service repeats the
  same line for pages, so a run of consecutive identical lines folds into its
  first line plus a `×N` badge (N counts the whole run). "Identical"
  is `logline.RepeatKey` equality — every *moving part* of a line is blanked to
  the sentinel `\x00` before comparing (a sentinel, not a cut, so two
  structurally different lines cannot collide), so lines that differ only in
  the numbers running along with the repeats still count as one; blank lines
  never fold. The runs are computed
  whole-buffer, cached per document version and path (`logRunCache`, a pointer
  field like `svTable`), and skipped for large files (#149) whose insight is
  already off. Hidden lines ride the fold machinery: `lineHidden` reports them
  and `hasFolds` gates on them, so motions, scrolling, the mouse map and the
  render loop treat a run exactly like a closed fold. Unlike a fold it has no
  open/close command — it reveals *positionally*, like the conceal layer
  (#1594): while the cursor is anywhere inside the run, every line renders raw
  and the marker disappears; moving out collapses it again.
- **What counts as a moving part** (#1758, `logline/variable.go`): timestamps
  were the only one until #1758, so a service that also printed an elapsed time
  or a page number kept filling the viewport. Blanked are now (a) the header
  stamp and the `time`/`ts`/`timestamp`/`datetime`/`date` pair values anywhere
  on the line, (b) duration-shaped values of duration keys (`elapsed`,
  `duration`, `took`, `latency`, `dt`, `rtt`, the `*_ms` spellings), (c) numeric
  values of pagination/counter keys (`page`, `offset`, `attempt`, `retry`,
  `count`, `rows`, `progress`, `seq`, `index`, `batch`, `step`, … — `cursor`
  also with an opaque token value), and (d) four shapes in the message text:
  duration tokens (`340ms`, `1.2s`, `2m30s`, `00:00:12`, `3 seconds` — the unit
  blanks with the number, so `980ms` and `1.2s` still match and no plural `s`
  separates two lines), a number behind a pagination keyword with its optional
  `of N` tail (`page 17 of 240`), a ratio (`17/240`) and a percentage (`42%`),
  plus a count in front of a counting noun (`1500 rows` — here only the number
  blanks, so `12 files` and `12 rows` stay apart).
- **Staying conservative** is the whole design constraint: a bare number is
  never blanked on its own, only one carrying a duration unit, a pagination
  keyword, a counting noun, a ratio or a percent sign, so `status=500` vs
  `status=200`, two ports, two ids, `/api/v1/users/42` vs `…/43`, `v1.2.3` and
  `Foo.java:42` all keep different keys. A digit run glued to a dot or a slash
  on its left is skipped outright (`1.2.3s`, `/api/v1/240`), a ratio may not
  continue into a second slash (`01/02/2024`), and a token followed by a letter
  or digit is no token (`5mb`). The scan is hand-written and digit-anchored
  rather than a regex sweep — `RepeatKey` runs over every line on every
  document version, where an alternation this wide costs tens of microseconds
  per line.
- **The `×N` badge** (#1734, `logRunMarkerStyle`): the marker draws in theme
  colours rather than `Faint` — a dimmed tag glued to a line end reads as part
  of the line and the "this row stands for N occurrences" fact was easy to miss
  entirely. Emphasis scales with the run length: `Info` below `logRunMany`
  (10), `Info` bold from there, `Warning` bold from `logRunLoud` (100) — a ×2 is
  a detail, a ×500 means the buffer is almost entirely this one line. Both
  colours are contrast-checked against every builtin theme's surfaces
  (`theme/contrast_test.go`), so the badge holds up light and dark. Placement is
  the shared annotation column (`annotWidth`, the delta hints' and blame's), so
  the badges of a file line up into one scannable column; the
  one-annotation-per-row rule is not at risk, since a collapsed header carries
  no delta hint and blame only annotates the cursor line, which is never a
  collapsed header. A line too long for the column falls back to appending the
  badge after the text, budgeted like `renderFoldHeader`: the count is buffer
  structure, not an optional hint, so unlike a delta it is never dropped.
- **Inter-line deltas** (#1651, `logline/delta.go`, `logdelta.go`): the elapsed
  time since the previous line renders as a dimmed `+30s` / `+7s 300ms` /
  `+450ms` at the right edge, so a stall reads off the column instead of being
  computed by eye. `logline.ParseStamp` reuses `Parse` to *locate* the stamp — so every
  layout the renderer knows is covered, the `time=` pair values and numeric
  epochs (#1618) included — and decodes only that text; ANSI-styled lines strip
  first. A line whose stamp does not parse (stack-trace frame, wrapped message,
  banner) shows no hint but does **not** reset the chain: the next stamped line
  measures against the last real timestamp. Non-positive differences show
  nothing either — second-resolution logs would otherwise carry a `+0ms` on
  most rows — and a dated stamp is never subtracted from a time-only one, whose
  base day is arbitrary (a time-only file crossing midnight does wrap forward).
  A delta at or above `logline.GapThreshold` — ten times the file's *median*
  cadence, floored at one second, so a millisecond trace and a per-minute
  heartbeat stall at their own scales — renders in `Warning`, bold. The hint
  splices into the row's right padding exactly like the inline blame
  annotation, only when the text leaves room for it plus two columns of air, so
  it never hides buffer content; while the scrollbar is visible both
  annotations right-align one column short of it, since the bar overlays the
  pane's rightmost cell (#1728). Blame keeps the cursor line to itself, and
  soft-wrapped rows and collapsed run headers (which carry `×N`) show no hint.
  The chain is whole-buffer, cached per document version and path
  (`logDeltaCache`) and skipped for large files, like the repeat runs.
- **Aligned hint column** (#1730, `logline.DeltaLayout`): a hint is two unit
  fields — the coarsest unit that carries signal (`ms`/`s`/`m`/`h`/`d`) plus the
  next one down, dropped when it is zero (`+2m`, not `+2m 0s`) or below the
  scale worth reading (`+45s`, never `+45s 123ms`). `LayoutDeltas` measures both
  field widths over the *whole* buffer — free, since the chain is cached per
  version anyway — and `DeltaLayout.Format` right-aligns each field in its
  column, so every hint of a file comes out the same width and adjacent rows
  line up on their unit boundaries (`+ 7s 300ms`, `+    598ms`, `+12m    8s`)
  instead of forming the ragged trail that made mixed units unscannable. A field
  nobody in the file uses costs no columns (an all-sub-second log renders
  `+450ms`, not `+   450ms`), and the trailing pad of a hint whose fine field is
  empty is what keeps the coarse field in place — the annotation right-aligns on
  the string's end. `logline.FormatDelta` stays the unpadded single-value form
  (`+2m 30s`), used where there is no column to join, e.g. the selection span
  below.
- **Selection span** (#1729, `logline.SpanDelta`, `Model.LogSpanLabel`): the
  per-line hints only answer *how long since the previous line*; a whole
  section — request to response, first to last line of a stall — is measured
  over the selection instead. With a visual selection active in a log buffer,
  the label reads `Δ +2m 30s`: the elapsed time between the **outermost
  timestamped lines** of the selection, so unstamped lines in between (stack
  frames, wrapped messages) are irrelevant. Comparability follows the per-line
  chain — the first stamp's kind wins, a stamp of the other kind (dated vs
  time-only) is skipped rather than subtracted, and a time-only selection
  crossing midnight wraps forward. The segment hides outside visual mode, for a
  single-line selection, with fewer than two comparable stamps, and for a
  non-positive span (both ends inside the same second of a second-resolution
  log). It needs no command of its own.
- **Span in the editor** (#1736, `Model.logSpanAnnotate`): the label renders
  twice — as the `logspan` status-line segment *and* at the right edge of the
  **cursor row** of the selection, both fed by `LogSpanLabel`, so they can never
  disagree. The bar alone was too easy to miss: the eye is on the selection, not
  on the bottom row, and the segment drops with the other cosmetic ones when the
  bar overflows. Only the cursor row carries the annotation — that is the end
  the user is moving — styled in `Accent`, bold, so it does not read as one of
  the faint per-line hints. It wins the one-annotation-per-row rule against both
  the per-line hint and inline blame, and unlike them it truncates the row's
  text when the line leaves no spare padding: a hint is optional, whereas a
  selection is a deliberate question whose answer must not silently vanish.
  Soft-wrapped rows and collapsed run headers show no annotation, like the
  hints.

- **Merged rotation sets** (#1996, `internal/logset`): a rotated log set —
  `app.log` plus the `app.log.1`, `app.log.2026-08-01`, `app.log.2.gz` next to
  it — opens as one chronological read-only buffer via `log.openRotatedSet`,
  every region opened by an origin separator naming its file (capture
  `log.origin`, accent colour). Everything above applies to it unchanged; the
  separator is not parsed as a log line and carries no timestamp, so it shows no
  delta hint and does not break the delta chain. Follow mode tails the set's
  newest member and re-merges across a rotation. The whole feature is described
  in [merged rotated log timeline](/architecture/log-timeline.md).

Toggling off shows plain raw source — no styling, escape bytes visible, every
repeat expanded. The buffer never changes.

## Unified-diff rendering (#1630)

`.diff`/`.patch` buffers render with the affordances the diff views already
have. The diff language plugin (`plugins/languages/diff`, no grammar — the
format is line oriented and stateful, so all structure is Go-computed in
`internal/unidiff` via the `Spans` and `Folds` seams):

- **Line coloring**: added lines capture as `diff.plus`, removed as
  `diff.minus`, `@@` hunk headers as `diff.delta`, file headers (`diff
  --git`, `---`, `+++`) as `diff.header`, and the git extension headers
  (`index`, `old/new mode`, `rename from/to`, `similarity index`, `Binary
  files`, …) plus the `\ No newline at end of file` marker as `diff.meta`.
  The captures derive from existing palette captures in
  `highlight.NewThemeKeys` (string green for added, `variable.builtin` red
  for removed, function for hunk headers, keyword for file headers, comment
  for meta — the rainbow-brackets derivation pattern), overridable per slot
  via `theme.captures.diff.*`.
- **Exact classification**: the parser consumes each hunk body by the `@@`
  header's line counts — the rule of the format — so a removed line whose
  content starts with `-- ` is never mistaken for a `---` file header, and
  prose between file sections (a format-patch commit message) stays plain.
- **Word-level emphasis**: each run of consecutive removed lines pairs with
  the added run that immediately follows; every i-th pair is refined
  rune-level by the diff views' own Myers refinement (`diff.Refine`,
  `maxRefineRunes` cap included). The changed ranges carry
  `diff.plus.emph` / `diff.minus.emph` — the dotted-prefix fallback keeps
  the line's foreground and `styleAt` layers the palette's `DiffChanged`
  background underneath, so a `.diff` buffer reads like the diff panes.
  Toggled by `editor.diff_word_highlight` (default on); a config flip
  re-parses open editors like the rainbow-brackets toggle does.
- **Folding**: every hunk folds behind its `@@` header and every file
  section behind its `diff` header via the Go fold seam
  (`lang.Language.Folds`, see
  [highlighting](/architecture/highlighting.md)) — `za`/`zc`/`zM`/`zR`
  work as in any buffer.

## Inline epoch-timestamp decoding (#1618)

Numeric Unix timestamps render as their UTC form in place, display-only,
toggled by `editor.timestamp_decoding` (default on, Settings → Editor) or per
view by the `view.toggleTimestampDecoding` palette command (a sticky override
like `mdRenderSet`). Detection lives in `internal/epochtime`, the editor half
in `timestamps.go`:

- **Stand-in spans**: `epochtime.Spans` emits one conceal-with-stand-in span
  (#1585) per detected number — capture `timestamp`, `Replace` the decoded
  `2024-08-06 12:00:00Z` (a `.123` fraction is appended when the value carries
  milliseconds). Rendering, the positional caret/selection reveal (#1594 —
  widened to the columns directly before and after the number by #1686, epochs
  being a value family) and the click/offset remapping are the shared stand-in
  path: the raw digits are always one motion away, and the buffer never
  changes. The `timestamp`
  capture derives from the palette's `number` colour (`decodeSources` in
  `highlight/theme.go`, #1681) so decoded epochs highlight like the #1627
  number-hint families; a theme table entry or `theme.captures.timestamp` key
  overrides it.
- **Own conceal channel**: `concealSplit` routes `timestamp` stand-ins into
  the decode-family channel map (`decodes`, keyed by capture — shared with the
  #1620 escape families) instead of `conceal`, so `tsDecode` gates them
  independently of the markdown (#1599) and log (#1621) rendering toggles,
  which gate the other channels, and of the other decode families.
- **Range heuristic**: only 9–10 digit seconds and 12–13 digit milliseconds
  between 2001-01-01 and 2100-01-01 decode; a leading zero disqualifies a run.
  Ports, byte counts, ids and years are left alone.
- **Context heuristic**: the caller picks the strictness. `epochtime.JSONValue`
  accepts a run only in a JSON value position — after `:`, `[` or `,` and
  before `,`, `]`, `}` or the line end, quoted runs included — so object keys
  and digits inside prose strings never match. `epochtime.Value` (#1684)
  widens that to the value positions the other formats write: `=` opens one
  and `&` closes one, so `KEY=1722945600`, `ttl = 1722945600`, a YAML
  `created_at:` and a `?since=…` query parameter all decode, while a run
  *before* a separator is still a key and never matches. `epochtime.Loose`
  accepts any run whose neighbours are plain delimiters. All three reject a
  run glued to a character that makes it part of a larger token (letters,
  `_`, `.`, `-`, `:`, `/`, `%`, `+`); the one exception is a leading `:`
  under `JSONValue`/`Value`, JSON's member separator.
- **Producers**: the JSON languages (`json`, `ndjson`) scan whole buffers in
  the JSON context, YAML, TOML, ini/conf and dotenv in the `Value` context,
  the log language scans every line in the loose context (mapped back through
  ANSI escapes like the header spans), and the `.http` producer scans its
  collected value ranges — query parameters, folded query lines, header
  values and inline bodies — in the `Value` context (#1684). The HTTP
  *response* viewer
  (`internal/httppane`) has no caret, hence no positional reveal, so it is not
  part of this layer; a response saved to a `.json` file decodes like any
  other buffer.

## JWT decoding (#1619)

JSON Web Tokens — the `eyJ…` blobs that fill `.http` `Authorization:` headers
and `.env` files — are detected by `internal/jwt` and handled in two halves,
the editor half in `jwt.go`:

- **Dimmed signature** (passive): a span producer calls `jwt.LineSpans`, which
  emits one span per token over the *signature segment only*, capture
  `jwt.signature`. `styleAt` resolves it terminal *faint* when no
  `theme.captures.jwt.signature` key names it, so the meaningless third segment
  stops competing with the claims. This is a color, not a stand-in: no toggle,
  no conceal, the bytes always read as they stand.
- **Decode popup** (active): `editor.decodeJWT` ("Decode JWT at Caret", palette
  only) opens the token's header and payload as pretty-printed JSON in the
  hover popup (fenced blocks, so the #379 hover renderer syntax-highlights
  them), anchored at the token's first column and dismissed by the next key
  like any hover. Decoding on demand rather than concealing the token with its
  payload is deliberate: a JWT is far too long to stand in for inline, and the
  decoded form is multi-line JSON, not a single value. A caret that is not
  inside a token falls back to the line's first one — the caret usually sits at
  the start of the header line, not in the blob.
- **Registered claims**: `exp`, `iat`, `nbf`, `auth_time` and `updated_at`
  carry their UTC date as a trailing `// 2024-08-06 12:00:00Z` comment,
  rendered through `epochtime.Decode` (#1618). The dependency is soft: a value
  outside the plausible epoch range keeps its raw number.
- **Structural detection**: a run of base64url characters must split into
  exactly three non-empty dot-separated segments whose first two decode to JSON
  *objects*. Version strings, hashes and dotted hostnames therefore never
  match, and a malformed or truncated token is simply not a token. Trailing
  sentence punctuation is trimmed off the run; padded segments (`=`) decode
  too.
- **Producers**: the `.http` span producer scans every line of the buffer (a
  token shows up in a header, a body, a `@variable` and a pasted response
  alike), the dotenv producer scans every value.

## Escaped-text decoding (#1620)

The #1585 percent-decoding generalized to other escape families: escaped text
renders decoded in place, display-only, via the shared stand-in mechanic
(#1585) with the positional caret/selection reveal (#1594) — the raw bytes
reappear under the caret and edits always operate on the raw source.
Detection lives in `internal/escapes`, the editor half in `escapes.go`; each
family has its own capture, conceal channel and toggle:

- **Unicode escapes** (`escape.unicode`, `editor.unicode_escape_decoding` /
  `view.toggleUnicodeEscapeDecoding`): `\uXXXX` — UTF-16 surrogate pairs
  combine into one span — and Go's `\UXXXXXXXX`, decoded only inside a
  single-line `"`/`'` literal (that is where JSON, JS/TS and Go put them; the
  scanner walks the quote state and consumes escapes pairwise, so `\\u0041`
  stays raw). A truncated escape, a lone surrogate, a value beyond the
  Unicode range or a non-graphic code point stays raw. Producers: `json`,
  `ndjson`, `go`, `typescript`.
- **HTML/XML entities** (`escape.entity`, `editor.entity_decoding` /
  `view.toggleEntityDecoding`): `&name;`, `&#123;`, `&#x1F600;`. The `html`
  producer decodes by the full HTML named-entity table (stdlib
  `html.UnescapeString`, prefix-only matches like `&notit;` rejected); the
  `xml` producer decodes only the five predefined entities plus numeric
  references — other names are document-defined in XML, guessing the HTML
  table would lie. Non-graphic code points (ZWJ, controls) stay raw: an
  invisible stand-in would hide that the reference exists.
- **Base64 values** (`escape.base64`, `editor.base64_decoding` /
  `view.toggleBase64Decoding`): decoded inline only where base64 is the
  *convention*, not on every base64-looking string — the `data:` block of a
  YAML document declaring `kind: Secret` (per `---`-separated document), and
  only when the payload decodes to printable single-line UTF-8 (one trailing
  newline forgiven — `echo secret | base64` leaves it). Binary secrets stay
  raw; `stringData:` holds plaintext and is never touched.

`concealSplit` routes all decode-family stand-ins (these three plus #1618's
`timestamp`) into a per-capture channel map (`decodes`), and
`lineConcealRanges` gates each family on its own toggle (`decodeOn`) — so the
families switch independently of each other and of the markdown/log layers.
All toggles default on and stick per view like the #64 toggles.

## Cron schedule hints (#1624)

A cron expression draws with its English reading appended — `*/5 * * * *`
renders as `*/5 * * * *  every 5 min`, `0 3 * * 1` as `0 3 * * 1  Mon 03:00` —
display-only, toggled by `editor.cron_hints` (default on, Settings → Editor)
or per view by `view.toggleCronHints`. Parsing, rendering and context
detection live in `internal/cronhint`:

- **Stand-in spans**: the span covers the raw expression and its `Replace`
  repeats the expression with `cronhint.Gap` (two spaces) and the hint
  appended, so the hint reads as an annotation *after* the expression rather
  than replacing it. `concealSplit` routes the `cron.hint` capture into the
  `decodes` channel map like the decode families, so `decodeOn` gates it on
  `cronHints` alone, and the #1594 positional reveal applies unchanged: a
  caret inside the expression (or a selection across it) drops the hint and
  the bare expression is what is edited.
- **Dialect**: the standard five fields (minute hour day-of-month month
  day-of-week) with an optional *leading* seconds field (the Quartz/Spring
  six-field form), ranges, steps, lists, names (`MON`, `JAN`), `?`, and the
  crontab `@daily`/`@reboot`/… shorthands. The extensions that need a
  calendar — `L`, `W`, `#` — and Quartz' seventh (year) field are rejected
  outright: no hint beats a wrong one. systemd's `OnCalendar=` is a different
  dialect and is not handled.
- **Rendering**: compact and deterministic — `every 5 min`, `hourly :05,:35`,
  `daily 09:00,17:00`, `Mon-Fri 04:30`, `Jan day 1 00:00`, `every 2 h`. Cron
  ORs day-of-month against day-of-week when both are restricted, which the
  rendering says out loud (`day 15 or Tue 03:30`); long time lists truncate
  with a `,+n` count so a hint never grows past a glance.
- **Contexts**: crontab lines (the leading five fields of a line that is
  neither a comment nor a `NAME=value` assignment, plus the `@` shorthands),
  CI YAML `cron:`/`schedule:` values (GitHub Actions, GitLab CI — the key
  names the value a schedule, so quoted and plain scalars both count), and
  quoted scalars in YAML, JSON and TOML. The quoted-scalar path additionally
  requires a cron-specific character or a field name, so a quoted list of
  numbers (`"1 2 3 4 5"`) is never mistaken for a schedule.

## Number-readability hints (#1627)

Large numeric literals in config files draw as the thing they mean —
`10485760` as `10 MiB`, `86400000` as `24h`, `1000000` as `1_000_000`,
`0x1F4` as `0x1F4  = 500` — display-only, on the #1585 stand-in channel, so
the raw literal reappears under the caret (#1594) — or directly next to it
(#1686, the adjacent reveal) — and edits operate on the buffer bytes. Four
families, four captures, four toggles, all default on:

| Family | Capture | Toggle | Config key |
| --- | --- | --- | --- |
| Byte sizes | `number.size` | `view.toggleByteSizeHints` | `editor.byte_size_hints` |
| Durations | `number.duration` | `view.toggleDurationHints` | `editor.duration_hints` |
| Digit grouping | `number.group` | `view.toggleDigitGrouping` | `editor.digit_grouping` |
| Radix | `number.radix` | `view.toggleRadixHints` | `editor.radix_hints` |

Detection and formatting live in `internal/numhint`, a leaf package over
`lang.Span`; `concealSplit` routes each capture into its own `m.decodes`
channel and `decodeOn` gates it, exactly like the decode families (#1620).

- **Context heuristics** come from the key a value hangs off, since config
  formats carry no types. Byte counts: `*size*`, `*byte(s)*`, `*memory*`,
  `*capacity*`, `*buffer*`, `*payload*`, `*storage*`. Durations: the timeout
  family (`*timeout*`, `*interval*`, `*delay*`, `*duration*`, `*backoff*`,
  `*period*`, `*latency*`) counted in milliseconds and the TTL family
  (`*ttl*`, `*expires*`, `*expiry*`, `*lifetime*`, `*lease*`, `*max_age*`)
  in seconds, unless the key's **last word** spells the unit out (`*_ms`,
  `*_seconds`, `*_us`, `flushMs`, `FLUSH_MS` — split on both separators and
  camel-case boundaries, so `params` is not a millisecond key). Radix:
  `*mode*`, `*perm*`, `*umask*` read octal, `*mask*`, `*flag(s)*` read hex.
  Weak quantifiers (`limit`, `max`, `quota`) name no family on their own — a
  rate limit is not a byte count, and `max_body_limit_bytes` already matches
  on `bytes`.
- **Shape heuristics** cover the unkeyed cases: a value that is a multiple
  of 1024 is a byte size wherever it appears, a `0x` literal always has a
  decimal reading, and a plain integer of five digits or more always reads
  better grouped. Family order is fixed — radix key, size key, duration key,
  size shape, grouping — so a literal never carries two hints; the duration
  key wins over the size shape because `86400000` happens to be a multiple
  of 1024.
- **Formatting** is deterministic and conservative. Sizes use binary units
  (`4 KiB`, `1.5 GiB`, exact multiples whole, everything else to one
  decimal, nothing below 1 KiB — decimal kB/MB are never offered, since it
  is the power-of-two shape that makes such numbers round at all).
  Durations use at most two components (`24h`, `1h30m`, `1s500ms`,
  `2d12h`), and the day unit only past 48h — one day still reads `24h`, so
  neighbouring timeouts stay comparable. A duration whose largest unit *is*
  the key's base unit gets no hint (`500` in a `*_ms` key says nothing new).
  Grouping starts at five digits (four-digit numbers are years, ports and
  status codes) and skips zero-padded runs. Radix hints are appended after
  the literal with `numhint.Gap`, and are skipped where both bases render
  the same digits (`0x9`, `mode: 7`) or the run is hash-length.
- **Guards**: tokens glue on `.`, `-`, `+`, `/`, `%` and letters, so
  versions, ISO dates, floats, paths, percentages and `30s`-style suffixed
  values never parse as bare integers; full-line and trailing comments are
  cut; a token followed by `:`/`=` is a key, not a value. A run that decodes
  as a plausible Unix timestamp is left to the epoch family (#1618) for the
  duration and grouping families, and in every producer that decodes epochs
  too, a hint the value's *shape* alone produced gives way to a colliding
  timestamp stand-in.
- **Field context beats the value pattern** (#1685). The other half of that
  collision: where the *field name* names the unit, the field wins — a value
  in a `bytes` key draws as a byte size even when its digits also read as a
  timestamp, because a large enough byte count simply lands in that range.
  `numhint.Hints` reports every literal as a `Hint` carrying `Claims`, set
  when the field decided the reading (a built-in key word or a mapping entry
  below) and independent of whether a stand-in was produced at all;
  `numhint.Allowed` drops the other families' spans over a claimed literal.
  Producers use `numhint.SpansWith(lines, stamps)`, which returns the hints
  and the surviving stamps in one pass; `.http` and the log renderer scan
  through `Hints`/`Allowed` directly because they map columns themselves.
- **Field-unit mapping** (#1685) is the escape hatch from the heuristics,
  which are ambiguous by nature: `size` is not necessarily bytes, a
  `duration` is counted in seconds as often as in milliseconds.
  `editor.number_hint_units` (Settings → Editor, a list, empty by default) maps
  field names to units, each entry written `pattern=unit`:

  ```toml
  [editor]
  number_hint_units = ["*_bytes=bytes", "retention=s", "created_at=timestamp-s", "session_id=none"]
  ```

  Patterns match case-insensitively over the whole field name with `*`
  wildcards, and a camel-case name is matched in its snake_case form too, so
  `max_body_size` covers `maxBodySize`. Units: `bytes`, any duration unit word
  (`ns`, `us`, `ms`, `s`, `min`, `h`, `d`, plus the spelled-out forms),
  `timestamp-s`, `timestamp-ms`, `octal`, `hex`, `group` and `none`. Earlier
  entries win, so a specific name may precede a wildcard covering it.
  A mapped field is read in that unit and **no other**: the shape triggers and
  the built-in key words are not consulted, a value the unit says nothing
  about (512 in a `bytes` field) stays bare rather than falling through, and
  `none` silences every family over the field's values — including the epoch
  decoding. The unit is the base the raw number is read **in**, not just the
  family it draws as (#2008): under `request_timeout=s` the value `1500` is
  1500 *seconds* and draws as `25m`, where the built-in `timeout` word would
  have counted it in milliseconds (`1s500ms`) — and every position the mapping
  reaches reads it that way, the Python keyword argument of a call line
  (#1761/#1773) exactly like the assignment beside it.

  A malformed entry or unknown unit is skipped rather than failing the whole
  mapping, and the skip is reported: `numhint.EntryError` words what is wrong
  with an entry, `numhint.InvalidEntries` lists the ones the install dropped,
  and `app` turns those into `editor.number_hint_units` config diagnostics
  (toasted like an unknown language id in a file association). The Settings
  list editor runs the same check per element and refuses to commit one that
  would be skipped, listing the unit words while the entry is typed — a
  dropped entry is invisible otherwise, and reads as the mapping being
  ignored: the field simply keeps its built-in reading. The mapping is a process-wide global in `numhint` (the span
  producers are `lang.Language.Spans` hooks with no config plumbing, as with
  `internal/idcolor`); `applyNumberHintUnits` in `app` pushes it on every
  config load and re-parses the open editors when it moved. Since #1998 an
  entry can also be written from the buffer: `g?` on a literal explains which
  rule read it and reclassifies the field into this very list — see
  *Conceal explain popover*.
- **Contexts** are every position the highlighting already recognises as a
  *value* (#1684), never a key: the config formats, where keys carry the
  intent — JSON/ndjson, YAML, TOML, ini/conf and dotenv — plus `.http` query
  parameters, folded query continuation lines, header values and inline
  request bodies, and the payload of a log line (its logfmt pairs and JSON
  tail, scanned over the ANSI-stripped visible text and mapped back, with the
  parsed header ranges and epoch stand-ins excluded). Keys are safe by
  construction rather than by a list: `numhint`'s scanner only ever hints a
  token that follows a separator, so a query key, a header name, a logfmt key
  or a JSON member name — numeric or not — is never concealed.

## Constant conceals in code (#1701)

The number families extend into source code: a **constant assignment** in a
Python, Go or PHP buffer reads by its name exactly like a config value reads
by its key — `MAX_BYTES = 10 * 1024 * 1024` draws as `10 MiB`,
`TIMEOUT_MS = 30 * 1000` as `30s` — with a pure literal-arithmetic right-hand
side **evaluated first**. No new captures, toggles or settings: the spans
carry the numhint/epochtime captures, so they ride the existing conceal
channels, obey the same toggles, reveal under and next to the caret
(#1594/#1686) and honour the `editor.number_hint_units` mapping (#1685) over
the constant's name.

Recognition and evaluation live in `internal/consthint`, a leaf package over
`lang.Span`, `numhint` and `epochtime`, appended to the `Spans` hooks of the
three language plugins. Each language keys on what marks a constant there:

- **Python** — a CONST_CASE name (`MAX_BYTES`, `_BUFFER_SIZE`), optionally
  annotated (`RETRIES: Final = 3`).
- **Go** — the `const` keyword: single-line declarations and the lines of a
  `const ( … )` block, with an optional type between name and `=`.
- **PHP** — `const NAME = …` (with visibility/`final` modifiers and an
  optional type) and `define('NAME', …)`.

Two shapes without a constant marker also conceal (#1761), gated by the name
instead of the case: a **lowercase assignment** (Python `duration = 5000`,
PHP `$duration = 5000;`) and a **keyword argument** inside call parentheses
(Python `f(timeout_ms=5000)`, PHP named arguments `f(timeout: 5000)`). Both
fire only when the name carries a recognised unit context — the user's
`number_hint_units` mapping (a `none` mapping vetoes) or a built-in key word
(duration, byte-size, radix families) — so loop counters (`n = 8`,
`attempts=3`) stay raw; single-letter names never qualify. The kwarg scan is
quote-aware (a `"duration=5"` inside a string never fires), only an
identifier directly after `(` or `,` is an argument name, and a doubled
separator (`==`, `::`) is never one, so comparisons don't match; several
kwargs on one line each conceal independently. Python `def` defaults are
syntactically kwargs and conceal under the same rule — a default is a
de-facto constant read the same way.

The kwarg scan runs over the whole buffer rather than one line at a time
(#1773), so a **formatted, multi-line call** conceals like a single-line one:
the parenthesis depth and the argument-slot state are carried across the line
break, and a `maxBytes=10 * 1024 * 1024` sitting on a continuation line of a
nested call still draws as `10 MiB`. Carrying the depth means carrying the
lexical states that would make it lie, so the scanner also tracks Python
triple-quoted strings and PHP `/* … */` blocks across lines — the lines inside
them conceal nothing — and drops back to "no open call" on anything it cannot
read to the end of the line (an unterminated one-line string, a PHP heredoc),
where depth zero means nothing conceals. A line inside an open call is not a
statement, so the assignment shapes skip it and each kwarg reports once.

Go stays `const`-only: a `var`/`:=`
binding is mutable, so its literal is an initial value, not a constant, and
idiomatic Go constants already use `const`. The evaluator's safety rule is
unchanged for all shapes — any identifier, call, float or string on the
value side leaves the line raw.

The **evaluator** (`consthint.Eval`) accepts number literals — decimal, `0x`,
`0o`, `0b`, digit-separating underscores, the Go/PHP leading-zero octal — and
the side-effect-free integer operators (`+ - * / % << >> & | ^`, parens,
unary sign, Python's `//`, Go's `&^`) over `math/big`. Anything else — an
identifier (`iota`, `self::BASE`), a call, a float, a string — fails the
parse and the source stays raw, which is the safety rule. Semantics the
languages disagree on are refused rather than guessed: division must be exact
(Go truncates where Python floats), modulo/bitwise/shift operands must be
non-negative, shift counts and intermediate magnitudes are capped, and the
result must fit an uint64. Precedence is per-flavor — Go binds `<<`/`&` at
the multiplicative level where Python and PHP use the C ladder, so `1<<4 + 1`
is 17 in a Go buffer and 32 in a Python one.

Rendering runs the numhint ladder over the computed value: the user's field
mapping first (final, including `none`), the built-in key words second, the
value's shape third (a 1024-multiple as bytes, anything else with its digits
grouped — a computed expression is concealed even when small, `6 * 7` as
`42`). A single prefixed literal (`0xCAFE`, `0755`) gains its decimal reading
appended instead, the same rule the config scan applies to hex; a stand-in
identical to the source (`10_000_000` regrouped) is dropped as noise.

## Network-literal hints (#1653)

The two network literals nobody reads by eye draw with their meaning appended,
display-only on the #1585 stand-in channel, so the raw literal reappears under
the caret (#1594) and edits operate on the buffer bytes:

```
10.0.0.0/8  10.0.0.0–10.255.255.255, 16,777,214 hosts
xn--mnchen-3ya.de  münchen.de
```

Two families, three captures, two toggles, both default on:

| Family | Capture | Toggle | Config key |
| --- | --- | --- | --- |
| CIDR prefixes | `net.cidr` | `view.toggleCIDRHints` | `editor.cidr_hints` |
| Punycode hosts | `net.idn`, `net.idn.mixed` | `view.toggleIDNHints` | `editor.idn_hints` |

Decoding, formatting and the context scan live in `internal/nethint`, a leaf
package over `lang.Span` (`net/netip` plus `golang.org/x/net/idna`);
`concealSplit` routes each capture into its own `m.decodes` channel and
`decodeOn` gates it, exactly like the decode families (#1620). The two IDN
captures share one toggle — the homograph capture is the same decode drawn in
a different colour, not a family of its own.

- **CIDR readings** come from `net/netip`, so what parses is what is hinted.
  A prefix with host bits set (`10.0.0.1/8`, a common slip) describes the
  network the address falls in, since that is what the notation means to the
  parser reading it. Counts follow the protocol: IPv4 subtracts the network
  and broadcast addresses, except on the point-to-point prefixes where there
  are none to subtract — a `/31` carries **2 hosts** (RFC 3021) and a `/32`
  one. IPv6 has neither, so its prefixes count *addresses*, and the ones too
  large to read stay a power of two (`2001:db8::/32` → `2^96 addresses`,
  `::/0` → `2^128 addresses`); below 2^20 the exact count is spelled out.
- **Punycode decoding** turns every `xn--` label back into its Unicode form
  (`xn--e1afmkfd.xn--p1ai` → `пример.рф`), keeping any `:port` suffix
  unchanged. A decoded form that is not printable text is dropped rather than
  rendered — no hint beats a wrong one.
- **Homographs** take `net.idn.mixed`, which `styleAt` draws in the theme's
  `Warning` colour unless a `theme.captures.net.idn.mixed` key names its own.
  Two shapes qualify: a label **mixing scripts** (`аpple` — Cyrillic `а` and
  Latin letters), and a non-Latin label spelled **entirely in Latin
  look-alikes** (`аррӏе` — all Cyrillic, and precisely the point of it). The
  script pairs ordinary names combine are not homographs: Japanese writes Han
  with the kana, Korean Han with Hangul, Traditional Chinese Han with
  Bopomofo. The look-alike table is small and hand-picked — the letters that
  carry the attack, not the full Unicode confusables data.
- **Contexts** split the way the other hint families split. Whole lines are
  scanned where every line is data — YAML, JSON/ndjson, TOML, ini/conf,
  dotenv and `.http` buffers; only **string literals** are scanned in source
  files (Go, JavaScript/TypeScript, Python), because a bare `10.0.0.0/8` in
  code is arithmetic, not a prefix.
- **Guards** are positional. An address run must not start right after a `/`,
  so a URL or filesystem path segment (`https://host/10.0.0.0/8`) is never
  read as a prefix; the prefix length must end the literal, a trailing `.`
  excepted so a prefix can end a sentence. Host runs stop at `/`, so a URL's
  authority is scanned and its path is not.

## Permission hints (#1656)

An octal file mode draws with its symbolic form appended — the reading `ls`
prints and nobody computes by eye:

```
chmod 755 build.sh            → 755  rwxr-xr-x
os.WriteFile(p, b, 0o644)     → 0o644  rw-r--r--
mode: '4755'                  → 4755  rwsr-xr-x
mode: '1777'                  → 1777  rwxrwxrwt
```

One family, one capture (`perm.mode`), one toggle: `editor.permission_hints`
(default on, Settings → Editor) or per view `view.togglePermissionHints`.
Decoding and the context scan live in `internal/permhint`, a leaf package of
plain Go over `lang.Span`; `concealSplit` routes the capture into its own
`m.decodes` channel and `decodeOn` gates it, exactly like the decode families
(#1620) and the other hint families (#1624, #1627, #1653). The stand-in shape
is the shared one: the span covers the raw literal and `Replace` repeats it
with `permhint.Gap` (two spaces) and the symbolic form appended, so #1594's
positional reveal applies unchanged — a caret inside the literal (or a
selection across it) drops the hint and the raw digits are what is edited.

- **Decoding** accepts three or four octal digits, written bare (`755`), with
  a leading zero (`0644`) or with the Go/Python `0o` prefix (`0o755`). The
  fourth (leading) digit is the special-bit field: 4 setuid, 2 setgid,
  1 sticky. Each replaces the execute character of its triad — `x` becomes
  `s` (`t` for sticky), and with the execute bit clear the capital `S` (`T`),
  which is what `ls` prints for a special bit set without an execute bit
  under it (`4644` → `rwSr--r--`).
- **Contexts carry the whole burden here.** Unlike a CIDR prefix (#1653) the
  literal has no syntax of its own — a bare three-digit number is a port or a
  year far more often than a mode — so only the things that *carry* a
  permission produce a hint: `chmod`'s first operand and the `-m`/`--mode`
  value of `install`/`mkdir` in shell; the `--chmod=` flag of `COPY`/`ADD`
  plus the shell scan over `RUN` lines in Dockerfiles; the octal literals in
  the argument list of a mode API in Go and Python (`os.Chmod`,
  `os.WriteFile`, `os.MkdirAll`, `os.FileMode(…)`, `os.chmod`,
  `os.makedirs(…, mode=0o755)`, `Path(…).chmod(…)`); and the
  `mode:`/`defaultMode:`/`directory_mode:` keys in YAML and Ansible.
- **Guards.** Shell modes are octal by definition, so a bare `755` decodes
  there. The code and YAML contexts additionally require the literal to be
  *written* as octal (a leading `0` or `0o`): in Go and Python a bare `644` is
  decimal, and in YAML `mode: 644` is the decimal 644 Ansible really reads —
  the classic footgun. That case is deliberately left to the radix hint of
  #1627, which says `644  = 01204` and so states the problem rather than
  papering over it; `yamlSpans` runs the permission hints first and feeds them
  to `numhint.SpansExcept`, so the two families never claim the same columns.
  In code, a run glued to an identifier (`x0644`) or sitting inside a quoted
  string is not a literal, and a matched call consumes its whole argument
  region, so a nested `os.Chmod(p, os.FileMode(0o755))` is annotated once.

## Invisible & deceptive Unicode (#1654)

The classic trap — code that looks identical but does not compile, or a string
comparison that fails — comes from characters the editor renders as nothing or
as an ASCII look-alike. Two independent halves close it, both always on (a
security rendering has no off switch), both language-agnostic, both inert on
pure-ASCII buffers.

**Placeholders** (`unihint.Placeholder`, hooked into `renderSpanUncached`'s
rune switch next to the #1469 control glyphs): every invisible/format rune
draws as a one-cell glyph in the theme's `Warning` colour, so nothing renders
as nothing — and no zero-width rune reaches the terminal raw, which would
desync the one-rune-one-cell mapping exactly like a raw control byte.

| Runes | Glyph |
| --- | --- |
| NBSP `U+00A0`, narrow NBSP `U+202F` | `⍽` (distinct from the `·` space glyph) |
| soft hyphen `U+00AD` | `-` |
| zero-width space/joiner/non-joiner `U+200B–D`, word joiner `U+2060`, BOM `U+FEFF` | `∅` |
| bidi controls `U+202A–E`, `U+2066–69`, LRM/RLM `U+200E/F`, ALM `U+061C` | `◊` |

An interior BOM survives `textenc.Decode` (only a leading one is stripped), so
it renders like the rest of the zero-width family.

**Diagnostics** (`internal/unihint.Notes`, a leaf package over `lang.Note`):
the highlight pass now runs for *every* buffer — `parseCmd` schedules even
without a language, computing only the Unicode scan there — and its findings
ride the #1623 note channel into the gutter tint, inline underline, hover and
caret popups. Three finding classes:

- **Invisible characters** — one note per run of identical runes (`× n` for a
  stretch), warning severity for the zero-width family, info for NBSP and the
  soft hyphen (common in legitimate prose). A ZWNJ/ZWJ sitting between two
  non-ASCII runes is legitimate typography (Persian needs ZWNJ, emoji families
  join with ZWJ) and downgrades to info.
- **Bidi controls** — always a warning; these enable the Trojan-Source attack
  class, and the message says so.
- **Confusable identifiers** — an identifier-like token mixing ASCII letters
  with Cyrillic/Greek ASCII look-alikes (`pаssword` with a Cyrillic а) flags
  as a warning naming each offender with script and codepoint. The look-alike
  table is `nethint.LatinLookAlike`, shared with the #1653 IDN homograph
  check. Legitimate non-Latin text never lights up: pure-Cyrillic words —
  even ones spelled entirely in look-alikes, unlike hostnames — ordinary
  Russian or Greek prose, `Δt`, `straße` all stay silent, because only the
  *mixed* token carries the attack in a buffer.

Notes now also travel beyond the marks (#1654): `NoteDiagnostics` converts
them to diagnostic values (source `lint`) so `lsp.next/prevDiagnostic` walks
them, `DiagnosticCounts` counts them, the #739 popups list them, and the app
feeds them to the Problems store on every `SpansMsg` — its own store channel,
so server publishes and lint findings replace independently (see
[/architecture/problems.md](/architecture/problems.md)). Large-file
degradation (#149) skips the scan with the rest of the insight features.

## Terminal hyperlinks (#1655)

URLs in a buffer are real links (`hyperlink.go`): the render loop wraps every
display cell of a detected URL in an OSC 8 open/close pair, so terminals that
support the sequence (Ghostty, iTerm2, kitty, WezTerm) make the text
clickable — cmd/ctrl+click opens the browser — and terminals without support
ignore the zero-width sequences by spec, degrading silently. The
`editor.hyperlinks` switch (default on) disables emission entirely.

Detection is a per-line scan (`scanLinks`), riding the #614 line cache like
every other per-span computation:

- **Bare URLs** — `http(s)://` at a word boundary, extended over URL-safe
  printable ASCII, with trailing prose punctuation trimmed (`.` `,` `;` `:`
  `!` `?` quotes) and a trailing closer (`)`, `]`, `}`) kept only while it
  has a matching opener inside the URL, so
  `https://en.wikipedia.org/wiki/Go_(language)` survives but `(https://x)`
  drops the parenthesis. The charset excludes control bytes and non-ASCII, so
  a scanned URL can never smuggle a sequence terminator into the emitted
  escape.
- **Markdown link labels** — `[label](target)` attaches *target* to the label
  cells (angle-bracket wrappers unwrapped, a `"title"` stripped), so the
  rendered label is clickable even while #881 conceals the destination. Only
  targets with a URI scheme (`http`, `https`, `mailto`, `file`) qualify —
  OSC 8 needs an absolute URI, so relative paths add nothing. The scan is
  textual, not grammar-driven, so it also works in READMEs viewed without a
  parser.

The emission shape is the #1469 lesson applied: the sequences are zero-width
— one buffer rune stays one display cell — so width budgeting, cursor
positioning, `DisplayOffset` and click mapping need no changes at all. Each
cell carries its *own* complete open/close pair rather than one pair per run:
a later splice of the rendered row (an overlay float truncating with
`ansi.Cut`) can then only ever fall between complete pairs, never strand an
unclosed open that would bleed the link over following text. A shared
`id=ike-<line>-<start>` parameter tells the terminal the cells form one link,
so hover-highlighting still spans the whole URL; downstream, ultraviolet
parses the pairs into per-cell link state and re-emits them diffed, so the
per-cell shape never reaches the wire verbatim.

## PEM / certificate summaries (#1652)

A PEM block collapses onto its `-----BEGIN …-----` line with a decoded
one-line summary appended, so a `.pem`/`.crt`/`.cer` file — or a certificate
pasted into a YAML block scalar — reads as facts instead of forty lines of
base64:

```
-----BEGIN CERTIFICATE-----  certificate  CN=example.com  expires in 12d  2026-07-08→2027-06-03  issuer=Example CA  RSA-2048  SAN=example.com, www.example.com
```

Toggled by `editor.pem_summary` (default on, Settings → Editor) or per view by
`view.togglePemSummary`. Decoding lives in `internal/peminfo` (a leaf package
over the standard library); the editor half is `pemsummary.go`.

- **Mechanic**: the log repeat run's (#1650), not the conceal layer's — a
  conceal range is per-line and a PEM block is not. The body lines ride the
  fold machinery (`lineHidden`, `hasFolds`, `collapsedRow`), so they are hidden
  for motions, scrolling, mouse mapping and the render loop alike, and the
  block costs one row of scrolling. Like a repeat run and unlike a fold it has
  no open/close command: it reveals **positionally** like every stand-in family
  (#1594) — the cursor anywhere inside the block renders all of it raw, base64
  included, and leaving collapses it again. The buffer is never altered.
- **Severity**: an expired or not-yet-valid certificate draws its summary in
  the theme's error color and bold; one expiring inside `peminfo.WarnWindow`
  (30 days, the usual ACME renewal lead time) in the warning color; everything
  else faint. A certificate comfortably inside its window carries no verdict
  text at all — the dates already said so, and a summary that shouts on every
  healthy block teaches the reader to ignore it.
- **Decoding depth**, deliberately asymmetric: `CERTIFICATE` decodes fully
  (subject CN, expiry verdict, validity window, issuer CN, key type, SANs —
  capped at three names plus a `+n more` count). That order is also the one
  that survives a narrow pane: the row truncates the summary from the right
  rather than dropping it, so the CN and the verdict are the last to go, and
  only a pane too narrow for `pemMinSummary` columns falls back to the bare
  marker. `CERTIFICATE REQUEST` and `PUBLIC KEY` decode to their
  subject/key facts; **private keys are never parsed** — they get a label
  built from the PEM type text that was already on screen (`private key
  (rsa)`, `private key (encrypted)`) and nothing else, because a summary that
  renders a secret defeats the point of the file being opaque. Any other type
  gets a plain label (`certificate revocation list`, `Diffie-Hellman
  parameters`), and anything that fails to parse falls back to
  `<label>  (unparseable)`: a wrong summary is worse than no summary.
- **Detection** is structural and language-agnostic: the layer reads buffer
  text rather than the span pipeline, because the blocks worth summarising are
  as often pasted into YAML or a config file as they are alone in a `.pem`.
  Marker lines are trimmed before matching, so an indented block decodes like
  one at the margin; the label must hold only RFC 7468 characters, so prose
  that resembles a marker is not claimed; and an **unterminated** block is
  never claimed — without its END marker there is no telling where the base64
  stops, and a second BEGIN ends the search rather than being swallowed.
- **Cost**: the blocks are cached per document version and path (`pemCache`,
  a pointer field like `logRunCache`), and the whole layer is gated by the
  large-file guard (#149) — re-scanning a multi-megabyte buffer per version is
  exactly what that guard exists to avoid.

## Secret masking (#1623)

Values whose key names a credential render as `••••`
(`secret.value`, `editor.secret_masking` / `view.toggleSecretMasking`) — in a
`.env` file, in a Python assignment (#1811) and in a JSON member (#1813). The
key alone decides — `internal/secret.Suspect` matches `*_SECRET`, `PASSWORD`,
`*_TOKEN`, `*_KEY`, `CREDENTIALS`, `DSN` and friends, and clears keys that
only look like one (`PUBLIC_KEY`, `API_KEY_ID`, `TOKEN_URL`, `AUTHOR`) — so
the value is never inspected to decide whether to hide it. The mask is fixed
width: sizing it to the value would leak the value's length.

The built-in tables are a guess about naming conventions, and a guess is wrong
in both directions, so `editor.secret_masking_keys` (#1712) lets the user
settle it: each entry is a pattern matched case-insensitively over the whole
key with `*` wildcards (`MY_API_KEY`, `*_LICENSE`, `db_pass*`), a `-`/`!`
prefix turning it into an exemption (`-PUBLIC_TOKEN`). The configured list is
consulted first and decides on its own — earlier entries win, and only a key no
entry matches reaches the built-in tables. It lives in a package global
(`secret.SetKeyPatterns`, the arrangement `numhint` uses for its field units),
because the producer is a `lang.Language.Spans` hook with no config plumbing of
its own; `app` installs it on load and re-parses the open editors when the list
moves, since which values carry a mask is decided when the spans are produced.
Nothing else changes: the per-family toggle, the conceal file filters (#1704)
and the positional reveal all apply to a custom-matched key exactly as they do
to a built-in one. `g?` on a value names the entry or built-in word that
decided it and writes the correcting entry into this list (#1998, see *Conceal
explain popover*).

It is not a decode, but it rides the identical mechanic: the producer
emits the value as a stand-in span (#1585) and `concealSplit` gives it its own
channel in `decodes`, gated by `decodeOn` like the escape families. So the
positional reveal of #1594 applies unchanged — put the caret inside a value
(or select across the line) and the raw secret is there to read and edit;
move away and it masks again. The buffer is never altered, and a masked value
copies, saves and diffs as itself. Masking is on by default; the toggle is
per view and sticks like the other view toggles.

A credential is just as exposed in source as in a `.env` file, so since #1811
**Python assignments** mask too (`plugins/languages/python/mask.go`): the
target names the value exactly as a dotenv key does, so `self.password =
"hunter2"` hides what follows, decided by the same `secret.Suspect` — the same
built-in tables and the same `secret_masking_keys` extensions and exemptions,
which is what lets a configured `*timeout*` mask `self.timeout = "500ms"`.
Everything downstream is the dotenv behaviour unchanged: one fixed-width mask,
the positional reveal, the view toggle and the `secret_masking=` conceal file
rules. The recognition is deliberately shallow — one statement line, an
identifier (dotted targets read by their last component) with an optional
annotation, a bare `=` (never `==` or `+=`), and the value to the end of the
statement. The value scan is quote-aware, so a trailing `#` comment stays
readable while a `#` inside the value does not cut the mask short and leak its
tail; a triple-quoted value masks on every line it spans, since hiding only the
first line of a pasted private key hides nothing, and lines inside an ordinary
docstring are prose, not assignments. The mask spans are emitted ahead of the
other Python span families, so a masked value outranks the network-literal
(#1653) or constant (#1701) hint that would otherwise read it.

What the mask *covers*, though, is the string literals of that value and not
the expression around them (#1930): only a literal can put a credential on
screen, so `token = item["token"]`, `token = get_token()` and `token = other`
carry no mask at all — masking a reference hides nothing and only trains
people to switch masking off. `PROXY_API_KEY = os.environ.get("PROXY_API_KEY",
"8479…")` masks the fallback alone; the environment variable's name is not a
secret, and hiding it would destroy the line's meaning. As in JSON the quotes
stay visible and the content masks, so the value still reads as a string.
Telling a key name from a value is a heuristic: a literal that is **not** the
whole right-hand side, is shaped like an identifier and is itself
secret-suspect (`"PROXY_API_KEY"`, `"token"`, `"db.token"`) is a name being
looked up and stays readable — which covers `os.environ.get`,
`config["api_key"]` and `d.get("token")` at once — while everything else masks,
keeping the `internal/secret` stance of erring towards hiding. A literal that
*is* the whole value is the value whatever it says, so `password = "token"`
masks, and a hyphenated or spaced literal (`"my-secret"`) is no key name and
masks too. In an f-string the literal text masks and the `{...}`
interpolations stay readable (`f"pw-{user}-x"` masks `pw-` and `-x`), since an
interpolation is an expression like any other; a doubled `{{`/`}}` is an
escaped brace and therefore text. A triple-quoted value left open still masks
whole, quotes included, on every line it spans — a pasted PEM key has no
closed content span to cover. **JSON** needs none of this: a JSON value is a
literal already, and the producer has masked its content and not its
surroundings since #1813. Dotenv values are raw text with no expression around
them, so they mask whole as before.

**JSON** (#1813) masks by the same rule, one producer down
(`plugins/languages/json/mask.go`): in a member `"password": "hunter2"` the
string's **content** carries the mask and the quotes stay visible, so the value
still reads as a string. Only the key directly in front of a value counts — a
nested object masks by its own members' keys, never by the one it hangs under —
and a value that is not a string (number, boolean, object, array) is left
alone: the mask stands in for a credential, and a structure has no single span
to stand in for. A member broken over two lines (`"password":` then the value
on the next) still masks, and an empty value does not — a mask over nothing
only reads as a value that is not there. The scan is a string-token walk, not a
grammar query, so it also holds while the buffer does not parse — which is
exactly the moment a freshly pasted credential is on screen. The masks are
emitted ahead of the buffer's other stand-ins (epoch, escape, hint spans), so
first-covering-wins cannot let a decode render a piece of the secret. ndjson
shares the producer. Other languages are follow-up work; the shared core in
`internal/secret` is what they will dock onto.

Duplicate keys in the same file are marked in the gutter and underlined
inline: the dotenv language registers a `lang.Lint` (see
`/architecture/languages.md`) that flags every assignment of a key except the
last, since that is the one loaders keep. The notes ride the highlight pass in
`highlight.SpansMsg.Notes`, live in their own channel (`Model.notes`) so
language-server diagnostics cannot clobber them, and merge into the same
severity lookups the LSP diagnostics use — including the decoration toggles,
so hiding warnings hides these too.

## Per-file conceal filter (#1704)

Every conceal family carries an on/off switch, but the switch says nothing
about *where*. `internal/concealfilter` adds that dimension: file globs,
matched against the buffer path, gating the families independently of their
toggles. Masking belongs in a checked-in `.env.example` and not in a fixture
whose point is the literal bytes, and that distinction is a property of the
file, not of the family.

Three settings feed it. `editor.conceal_include` and `editor.conceal_exclude`
are the global level, covering every family; `editor.conceal_file_rules` is
the per-family override, its entries written `family=pattern` with the pattern
prefixed `-`/`!` for an exclude and bare/`+` for an include, the family named
by its `editor.*` toggle key minus the prefix. Within a level precedence is
**exclude > include > allow**. Across levels, a family whose own rules decide
a path never consults the global one — that is what makes it an override
rather than a second opinion. An entry naming no known family is dropped with
a config diagnostic (`validate.go`) rather than silently doing nothing.

Patterns match through `internal/pathglob` — the LSP watched-files matcher of
#1144, lifted out of `lsp/manager` so both callers share one vocabulary
(`**`, `*`, `?`, `{a,b}`, `[...]`). `concealfilter` adds the path convention
on top: a pattern without a separator matches the base name (`*.py`,
`Makefile`), one with a separator the whole path, anchored at any segment
boundary unless it starts with `/` or `**`; matching is case-insensitive and
backslashes normalize to `/`. A buffer with no path is always allowed — an
include list names files that exist.

The gate applies where a family is **read**, not where its toggle is resolved.
`Model.concealGate(family, on, set)` composes the two dimensions;
`decodeOn` (escapes.go) routes the thirteen stand-in families through it via
`decodeFamily`, and the four layers read directly by their renderers get named
accessors (`mdRenderOn`, `svRenderOn`, `logRenderOn`, `pemSummaryOn`). Keeping
the toggle fields unfiltered is what makes the dimensions independent: neither
can strand the other in a state it cannot come back from, and `Settings →
Editor` keeps reading as the family's own state.

Two consequences are deliberate. A **per-view toggle bypasses the filter** —
that is the `set` flag in `concealGate` — because a pattern list states a
default and an explicit toggle in this buffer says otherwise. And because
`applyConfig` runs on every routed message, an edited pattern list or a
changed buffer path takes effect on the next frame with no reload;
`refreshConcealRules` memoizes the compiled filter on the joined raw values
(the `rulersRaw` discipline) so the per-keystroke pass stays free.

## Conceal explain popover (#1998)

Every family above rests on a heuristic — a field name mapped to a unit
(#1685), a built-in key word, the shape of the digits, a secret key pattern
(#1712) — and a heuristic that misfires used to be invisible from the buffer:
a byte count drawn as a date, an over-masked assignment (#1930), a credential
left readable. `g?` (`editor.explainConceal`, `explain_conceal`) opens a
popover on the value at the caret that says which rule fired, what it decided,
and offers the one-key corrections.

**Provenance comes from the producers, not from a second guess.** Each hint
source now reports which rule it applied: `numhint.Hint.Why` records the level
(`SourceFieldRule` / `SourceKeyWord` / `SourceShape`), the pattern or key word
it matched and the unit it chose — filled in on the same branches that pick the
family, so it cannot drift from the rendering — with `numhint.FieldRule` and
`KeyWord` naming the entry or word behind a reading, `ValueAt`/`HintAt`
resolving a column back to a value or a hint, `secret.Explain` replaying the
key tables in order (user pattern, strong word, public marker, marker, suffix,
exact name), `epochtime.Unit` reporting the digit-count reading, and
`consthint.Eval` re-evaluating a computed constant right-hand side.
`internal/concealexplain` composes those into one `Explanation` (raw value,
stand-in, key, family, rule sentence, reading, mapping word) and words it;
`internal/editor/explainconceal.go` is the caret → span → popover half plus the
keys.

The caret finds its span through `decodes` directly, in the fixed
`decodeCaptures` order and with #1686's one-column widening, so a caret
appended to a literal still explains it and a family switched **off** in this
view still answers — "nothing draws here" is exactly the case people ask
about. With no stand-in at all the popover explains the plain value instead
("why is this *not* masked"), which is the other half of the same question.

The actions write into the **existing** stores rather than a parallel one:
`r` reveals (the caret inside the span *is* the reveal, #1594/#1686), `1`–`9`
reclassify the field into `editor.number_hint_units`, `a` pins the reading the
heuristic chose as a rule of its own, and on a masked value `m`/`u` add the
masking or exempting entry to `editor.secret_masking_keys`. The editor writes
no config itself — it emits `editor.ConcealRuleMsg` and `app`
(`conceal_rule.go`) persists through `config.WriteAndReload`, so the ordinary
`ConfigReloadedMsg` path re-installs the mapping and re-parses the open
editors, and the new entry is listed and editable in `Settings → Editor` like a
hand-written one. A rule for a pattern that already has an entry **replaces**
it: both stores resolve by first match, so appending would leave the
reclassification shadowed by the reading it was meant to correct.

## Inline color preview (#790, #1622)

Recognized color literals — `#rgb`, `#rgba`, `#rrggbb`, `#rrggbbaa`,
`rgb()/rgba()`, `hsl()/hsla()` — render with the literal's own color as the
**cell background** and a black/white contrast foreground picked by luminance
(`colorswatch.go`).
The tint approach (instead of extra `██` swatch cells) is deliberate: it adds
no display columns, so motions, mouse clicks, soft wrap and the #881 conceal
mapping stay untouched. Detection is a per-line regex scan inside the
line-cached render path — only visible lines are ever scanned, so large files
cost nothing. Invalid values (out-of-range channels, wrong arity, 5/7-digit
hex) yield no swatch. Alpha components parse but do not tint (no alpha
channel in a terminal cell).

- **Scope per language** (#1622): `colorPolicy` resolves the buffer language
  through `lang.ByPath`. The **CSS family** (`css`, and the `scss`/`less`
  extensions it covers) tints *every* literal — there a hex triple is never
  anything else. **Every other language** — config formats (TOML/YAML/JSON)
  and code alike — tints only literals in a **value position**: the literal
  must be delimited by a line edge, whitespace, a quote, or one of
  `: = , ( [ { < | -` on the left and the matching closers on the right. So
  `accent = "#ff8800"` and `accent: #ff8800` light up, while the fragment of
  `https://example.com/p#ff8800` or a `abc#ff8800` suffix stays plain.
- **Toggles**: the `editor.color_preview` config default (default on,
  Settings → Editor) plus the per-view `view.toggleColorPreview` palette
  action, which sticks like the #64 view toggles (`applyConfig` stops tracking
  the config value once toggled).

Cursor/selection/search win the cell as usual; the diagnostic underline
composes on top.

## Identifier color hashing (#1626)

UUIDs and long hex hashes — git SHAs, request/trace/correlation IDs — render
in a color derived from **the identifier's own hash**, so every occurrence of
the same identifier shares one color. In a log or a JSON payload it becomes
visible at a glance which lines belong to the same request, without reading a
hex digit. It is the #1589 palette mechanic keyed on content instead of column
index, the same trick the log thread/logger names use.

- **Detection** (`internal/idcolor`, `Scan`): the canonical UUID form
  `8-4-4-4-12` first (its groups are then excluded from the hex pass), then
  bare hex runs. A run only counts when it is **standalone** — neither
  neighbour a letter or digit, so the tail of `0xdeadbeef` or of a base64 blob
  never matches — reaches the **minimum length** (`editor.id_color_min_length`,
  default 7, the abbreviated git SHA, floor 6) and carries at least one hex
  letter, because an all-digit run is a number, not an identifier. A leading
  `#` excludes the run: that is a color literal the inline color preview (#790)
  owns.
- **Color assignment** (`idcolor.Slot`): FNV-1a over the case-folded
  identifier, modulo the rainbow cycle length, so `3F2A…` and `3f2a…` share a
  slot and the mapping is stable across lines, panes, files and runs. The slot
  renders through the shared rainbow palette (`idcolor.Capture` →
  `rainbow.<N>`), so a `theme.captures.rainbow.N` override moves identifier
  colors along with bracket colors.
- **Scope** (`idcolors.go`, `idColorLangs`): the formats where an opaque
  identifier is the point — `log` (#1621), `json`/`ndjson` and `http`.
  Everywhere else a hex run is far more often a literal. Detection runs per
  rendered line inside the line-cached render path, so only visible lines are
  ever scanned.
- **Rendering**: the hashed color replaces the syntax **foreground** only, so
  the backgrounds below it (ruler, conflict, occurrence) stay intact; the
  color swatch (#790), diagnostics, cursor, selection and search still win
  their cells.
- **`.http` response bodies**: `internal/httppane` colors identifiers in the
  body rows through the same package. That pane has no config of its own, so
  it reads the `idcolor` package globals, which `applyIDColorConfig` in
  `internal/app` pushes from `editor.id_colors` /
  `editor.id_color_min_length` at startup and on every config reload.
- **Toggles**: the `editor.id_colors` config default (default on, Settings →
  Editor) plus the per-view `view.toggleIdentifierColors` palette action,
  which sticks like the #64 view toggles.

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
`auto_indent`, `auto_close_pairs`, `typing.space_after_punctuation` (#1326),
`trim_trailing_whitespace`, `insert_final_newline`,
`line_numbers`, `relative_line_numbers`, `scroll_off`, `sticky_scroll`,
`sticky_scroll_depth`, `wrap`, `show_whitespace` (`none|trailing|all`),
`indent_guides`, `rulers`, `markdown_rendering` (#881), `log_rendering`
(#1621), `timestamp_decoding` (#1618), `cron_hints` (#1624),
`byte_size_hints`/`duration_hints`/`digit_grouping`/`radix_hints` (#1627),
`number_hint_units` (#1685, app-level: pushed into `numhint` on config load),
`cidr_hints`/`idn_hints` (#1653), `permission_hints` (#1656),
`hyperlinks` (#1655),
`pem_summary` (#1652),
`color_preview`
(#790), `id_colors` / `id_color_min_length` (#1626)
and `search_ignore_case` (#1111, default off — in-file search folds
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
