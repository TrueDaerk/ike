---
type: concept
title: Diff Viewer
description: "#60/0340 — reusable read-only diff pane: line-level Myers engine with intra-line refinement, side-by-side or unified rendering with theme diff slots and per-side tree-sitter syntax highlighting, no soft-wrap with a horizontal offset shared by both sides, hunk navigation (n/N, enter jumps the editor), mouse text selection with y/ctrl+c/cmd+c copy (#2070), diff.files palette command, layout persistence."
resource: internal/diff
tags: [architecture, diff, pane, vcs]
timestamp: 2026-08-28T12:00:00Z
---

# Diff Viewer (#60)

`internal/diff` is the shared diff infrastructure: two text versions in,
highlighted side-by-side (or unified) diff out. It exists as a pane so future
consumers — VCS status (#28: file vs HEAD), local history (#35: snapshot vs
current), and the external-change conflict guard (#53) — can open it instead
of growing their own renderers. On its own it is reachable through the
`diff.files` palette command.

## Engine (`engine.go`)

Pure computation, no rendering or bubbletea. `Compute(left, right)` splits the
texts into lines, runs Myers' greedy O(ND) diff (with common prefix/suffix
trimming), and folds the edit script into aligned display `Row`s: unchanged
lines, changed pairs (a delete run paired positionally with the following
insert run), and one-sided adds/removes with a gap on the other side. Changed
pairs are refined at rune level through the same Myers core into per-side
`Span`s for intra-line emphasis; lines longer than 400 runes skip refinement
(quadratic cost, unreadable emphasis). Contiguous runs of non-equal rows form
the `Hunk` list used for navigation. `Lines(a, b)` exposes the raw line-level
edit script for future consumers that need scripts rather than rows.

## Pane model (`model.go`)

`diff.Model` mirrors the other pane components (value type, pointer-receiver
mutators) and is embedded in a `pane.Instance` as the fifth `pane.Kind`
(`KindDiff`), keyed `"diff"`, `"diff:2"`, … by the registry's monotonic
minting; it advertises the `"diff"` context id. It can equally back a floating
prompt via `ui.ModelContent` (the conflict-guard use case) since it is just a
sized, palette-threaded `View() string` component.

Rendering is side-by-side by default: two columns with per-side line-number
gutters and a `│` separator, tabs displayed four cells wide. Lines never
soft-wrap (#1700): every row is exactly one visual line, clipped at its column
edge, and the model carries one shared horizontal offset (`hoff`) both sides
render from — so the columns move in lockstep and row alignment survives any
line length. The render pass measures the widest displayed line (`hmax`) and
the visible column budget (`hcol`) and re-clamps `hoff`, so a resize, a layout
toggle, or new content can never leave the view scrolled past the end. Spans
and syntax captures are indexed in absolute display columns, so intra-line
emphasis and highlighting stay on the right runes at any offset. `u` toggles the unified single-column
layout, where a changed pair renders as its removed line followed by its added
line under a dual old/new gutter. Line backgrounds come from three new theme
`ui` slots — `DiffAdded`, `DiffRemoved`, `DiffChanged` (intra-line emphasis) —
declared by every builtin and defaulted for sparse themes by tinting the
theme's own `Success`/`Error`/`Warning` toward its `Surface` (`theme.Mix`).

Syntax highlighting (#1699) rides on top: both sides parse independently with
the language resolved from the compared file's path (the editor's
`internal/lang` resolution) — the right side is (or mirrors) a real buffer,
the left side is a virtual document (HEAD blob, snapshot, clipboard) parsed
on its own from the text git handed over. Capture colours come from the
palette's capture table (`highlight.NewTheme`, the HTTP pane's shape) and
compose as *foreground* under the diff-state *backgrounds*, in both
side-by-side and unified layouts — added/removed line backgrounds and the
intra-line emphasis stay visible over the syntax colours. Sides re-parse when
content is set (`SetContents`) or re-diffed (`Rediff`); while edit mode
renders the right column through the embedded editor (which highlights live
itself), the per-keystroke re-diffs skip the right-side parse and a single
parse runs when edit mode ends. Language-less paths and CGo-free builds
render diff colouring only.

Keys (focused pane): `j`/`k`/`↑`/`↓` scroll by visual row, `ctrl+u`/`ctrl+d`
and page keys page, `g`/`G` jump to the ends, the mouse wheel scrolls.
`h`/`l`/`←`/`→` scroll horizontally by one column and `shift+←`/`shift+→` by
half a column; `0` jumps back to column 0 and `$` to the widest line's end.
The horizontal wheel and `shift`+wheel do the same through `ScrollXBy` — all
of it moves both sides at once (#1700). `n`/`N`
step through hunks (scrolling the hunk a third down the view); `enter`
dispatches `diff.JumpMsg` and the root model opens the right-hand file with
the cursor on the hunk's first line. The view is read-only; hunk-level "take
left/right" staging is a later increment for #28. The status line shows
`DIFF │ left ⇄ right │ hunk i/n`.

## Text selection & copy (#2070)

`selection.go` gives the viewer mouse text selection with the shared
click-streak gestures the terminal (#227/#951) and HTTP response viewer
(#1266) established, via the extracted engine in `internal/textsel`: a drag
selects by character, a double click a word, a triple click a line, drags
extend by the streak's unit, and `y`/`ctrl+c`/`cmd+c` copy. Selection
positions are (visual line, display column) pairs over the *content* cells —
gutters and the separator are not selectable — recorded per render in a
`vrows` map (row index + side per visual line; a unified changed pair
occupies two entries). In side-by-side layout the pressed column pins the
selection (`selRight`): a drag past the other column clamps to the line end
instead of mixing sides. Extraction (`SelectionText`) maps display columns
back onto the raw row text (`textsel.RawSlice` undoes the tab expansion), and
a selection touching a collapsed-context separator copies the gap's hidden
rows in full — never the placeholder label (the fold-copy rule from #1741). A
covered separator label and content cells render with the theme's
`Selection`/`SelectionText` colours, outranking diff backgrounds and syntax.

The copy chords without a selection copy the current hunk (the first before
any `n`/`N`) as a minimal unified patch (`HunkPatchText`), analog to the
response pane's "no selection → whole body". Copying clears the selection;
`esc` clears it too. The selection drops whenever the visual-line map shifts
(new contents, re-diff, layout toggle, context change, collapse toggle or gap
expansion) and when edit mode starts — the embedded editor (#496) brings its
own selection and the two never overlay. The pane cannot reach the clipboard
itself: it emits `diff.CopyMsg` and the root model writes the clipboard and
notifies, like `httppane.CopyMsg`. The root also routes the ctrl+c quit chord
into the pane while a selection lives (`paneSelectionCopy`, the audit rule
from #2062), and the mouse press/drag/release arrive via the app's
`dragDiffSelect` gesture — presses are ignored in edit mode.

The merge view's read-only side columns get the same treatment
(`internal/merge/selection.go`, `dragMergeSelect`): a press in ours/theirs
anchors a side-pinned selection, the copy chords intercept in
`merge.Model.Update` before the result editor (bare `y` stays with a
capturing editor — insert-mode typing is never stolen), and the middle
column's presses are left to the editor entirely.

## diff.files command

`diff.files` (palette) picks two files via the `@` fuzzy finder: the root
model arms a two-step pick state, intercepts the two `palette.OpenFileMsg`
picks (left/old first, right/new second, with toasts prompting each step),
then places the diff pane and focuses it. Dismissing the picker mid-flow
disarms the state so a later `@` open is a plain file open. Unreadable files
diff as empty text.

## diff.compareWithClipboard command

`diff.compareWithClipboard` (palette, #1477) compares the active buffer
against the system clipboard: clipboard on the left (read-only), the live
buffer text (unsaved edits included) on the right. An active visual selection
narrows the right side to the selected text (`editor.Model.SelectionText` —
whole lines in visual-line mode, the inclusive charwise span otherwise); the
right title gains a `(selection)` suffix and the file link (jump/edit) is
dropped. A whole-buffer compare of a file-backed editor keeps the path, so
enter-jump and `e`-editing work like the HEAD diff. An empty clipboard (or a
missing clipboard utility) shows a notification instead of an empty diff. The
pane routes through the single diff slot / `placeDiffLeaf` like every other
diff-open (`internal/app/diff_clipboard.go`).

## Three-way merge view (#1478)

For git-conflicted files the engine grows a three-way half
(`internal/diff/threeway.go`): `Compute3` aligns ours and theirs against the
merge base (the classic diff3 walk over the two base-relative Myers scripts)
and classifies every region as `Chunk3Same` / `Chunk3Ours` / `Chunk3Theirs` /
`Chunk3Both` (identical change on both sides) / `Chunk3Conflict`; `Merge3`
folds that into a merged text where only true conflicts remain, emitted as
diff3-style marker blocks (`<<<<<<<`/`|||||||`/`=======`/`>>>>>>>`).

The view (`internal/merge`, pane kind `KindMerge`, keys `merge`, `merge:N`)
renders ours (left) and theirs (right) as read-only columns around an
**editable result editor** in the middle, seeded via `Merge3` from the
`:1`/`:2`/`:3` index stages (`vcs.MergeStagesCmd`; a missing `:1` stage —
both-added — degrades to an empty base). Because the remaining conflicts are
ordinary markers, the editor's inline conflict machinery (#1149) provides
per-conflict resolution and navigation unchanged (the `merge.*` commands — the
pane advertises the editor context, and `editor.ActionMsg` routes into the
result editor when a merge pane has focus). The side columns follow the result
editor's scroll offset — which is why `merge.Wheel` (#2259) simply moves that
editor's viewport: one notch scrolls all three columns in lockstep, and the
view is no longer the one viewer the wheel skipped.

### Resolving (#2258)

- **Navigation** — `]n` / `[n` cycle the *remaining* blocks with wrap-around
  (`merge.nextConflict` / `merge.prevConflict`), reporting `merge conflict
  n/m`. As blocks are resolved they leave the cycle, so the walk always
  visits work that is left.
- **Resolution** — `go` ours, `gt` theirs, `gb` both (ours then theirs), `gm`
  keep the hand-merged block (marker lines only are dropped). Each is one undo
  unit, so `u` puts the block back — which the counter follows. Free editing
  of the result buffer resolves a block just as well: a block stops counting
  the moment its markers are gone.
- **Counter** — the pane header reads `⚠ conflict 2/3 · 3/5 unresolved` (the
  caret's place in the cycle, then remaining out of the total) and flips to
  `✓ resolved — apply to finish`; the status line's `MERGE` segment carries
  the same reading. Both come from `merge.Model.Unresolved()` /
  `ConflictIndex()`, i.e. the editor's cached block scan — never a re-scan per
  frame.
- **Finishing** — when a view's last conflict goes, the settled Update pass
  (`syncMergeFinish`) raises the centered **Merge complete** offer: `s`/enter
  applies (save + `git add` + close), esc keeps the view open. It watches the
  count rather than one key route, so it fires however the block was resolved,
  and an undo that brings a conflict back re-arms it. `vcs.mergeApply` refuses
  to finish while any conflict remains, with a toast naming the count.
- **Marker guard** — the block scan only sees complete `<<<<<<<`…`>>>>>>>`
  runs, so a *half-edited* block (a deleted closer, a stray separator) counts
  zero conflicts while still poisoning the file. Both the finish offer and
  `vcs.mergeApply` therefore also check `MarkerLines()` — a buffer walk, run
  only on the transition to zero and before a write, never per frame — so the
  written file is always marker-free.

Entry points: `vcs.mergeFile` on the focused conflicted file, `enter` on a
conflicted VCS-panel row, or the **Conflicted file** offer (#2258) raised when
a file git reports as conflicted is opened in the editor — `m`/enter opens the
merge view, esc leaves the user editing the markers in place. That offer
interrupts once per path per session, like the large-file toast, and never
over another dialog. Closing the pane with unresolved conflicts or an unsaved
result opens a discard/cancel guard.
The pane is session state: a saved layout records its slot as an anonymous
editor pane.

## Pane placement

Every diff-open (HEAD diff, commit diff, `diff.files`) routes its freshly
created pane through `placeDiffLeaf`: if the active editor is an **empty
scratch pane** (`Instance.IsEmptyEditor` — a single tab, no file, no text), the
diff takes over that pane's slot in place via `layout.Replace` (renaming the
leaf) and the empty editor is dropped; otherwise it splits the target leaf right
with `layout.SplitLeaf`. This avoids leaving a blank editor stranded beside a
new diff (#628). A file-backed or dirty-scratch editor is never reused — its
content is preserved and the diff splits beside it.

## Persistence

Layout persistence saves `{kind: "diff", path, path2}`; restore rebuilds the
pane and re-reads both files from disk (a vanished side restores empty rather
than breaking the layout).

## Diff viewer v2 (Epic 0340)

- **Collapsed context** — unchanged runs fold into `··· N unchanged lines ···`
  separators around a context budget (default 3, config `diff.context`;
  negative disables). `c` toggles collapsed/full, `o` expands the gap nearest
  the viewport center; expansions reset with new contents. Hunk navigation
  and jumps work over collapsed maps.
- **F7 / shift+F7** — next/previous change via the diff-scoped default
  bindings (`diff.nextChange`/`diff.prevChange`); `n`/`N` stay.
- **Editable current side** — `e` on a worktree-backed diff (diff.files,
  vcs.diff, the changes view) mounts a live editor as the right column: full
  vim editing, `:w` saves, shared document with open tabs, the left column
  re-aligns per keystroke; `ctrl+e` returns to browsing. Revision-vs-revision
  diffs (the log view) stay read-only with a hint.
