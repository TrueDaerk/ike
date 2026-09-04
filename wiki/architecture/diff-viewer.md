---
type: concept
title: Diff Viewer
description: "#60/0340 — reusable read-only diff pane: line-level Myers engine with intra-line refinement, an ignore-whitespace mode (w, persisted as diff.ignore_whitespace, #2170), side-by-side or unified rendering with per-side theme diff slots including bold/underlined intra-line emphasis and tree-sitter syntax highlighting, no soft-wrap with a horizontal offset shared by both sides, scroll-aware hunk navigation with a current-hunk gutter marker, side-label headers and a hunk/progress footer (#2494), mouse text selection with y/ctrl+c/cmd+c copy (#2070) painted as a per-frame overlay so a drag stays cheap (#2495), a hard 2 MiB/side input budget with a bounded Myers core (#2505), live reload of file-vs-file diffs off the 0140 watcher with a removed-file footer notice (#2506), clickable collapsed-context separators, diff.files palette command, opens as a content tab of the focused editor pane (diff.placement, #2507), layout persistence."
resource: internal/diff
tags: [architecture, diff, pane, vcs]
timestamp: 2026-09-04T00:00:00Z
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

`ComputeWith(left, right, Options)` is the same computation under options;
`Compute` is it with the zero value. `Options.IgnoreWhitespace` (#2170) drops
whitespace from every comparison the way `git diff -w` does: lines compare by
their whitespace-stripped key, so a re-indented or re-wrapped line pairs up as
a `RowSame` row (each side keeping its own **raw** text — the option changes
what is compared, never what is shown), and intra-line refinement trims each
span to its non-whitespace core, dropping the ones left empty. A hunk whose
lines only moved sideways therefore disappears from the hunk list entirely.

**Size budget** (#2505): the engine refuses oversized input instead of diffing
it. A side over `MaxDiffBytes` (2 MiB, a constant — deliberately not a
setting) makes `ComputeWith` return `Result{TooLarge: true}` with no rows;
`TooLarge(left, right)` exposes the same check so openers
(`openDiffTexts` in `internal/app`) can toast the refusal with the limit
instead of opening an empty pane. Below the budget the Myers core itself is
bounded: each D-round snapshots only its reachable diagonal band (`-d..d`,
`bandSnapshot`) rather than the full diagonal array — the full-width copies
made backtrack memory O(D·(N+M)), which turned two ~600 KiB divergent
responses into ~25 GiB of allocation and an OOM kill — and rounds past
`maxMyersRounds` (1024) abandon the optimal alignment for a plain
delete-all/insert-all script over the trimmed middle, which `buildRows` still
pairs positionally into changed rows. Intra-line refinement additionally skips
line pairs over 4 KiB (`maxRefineBytes`) *before* the `[]rune` conversion, so
a multi-megabyte single line (minified JSON) never allocates a rune per byte
just to learn it is over the 400-rune cap anyway.

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
emphasis and highlighting stay on the right runes at any offset. Each rendered
segment carries the shared horizontal-scroll edge marks (#2377,
`diff/hscroll.go`, `internal/hscroll`): `‹` on its first cell while `hoff > 0`
and `›` on its last while the row runs past the column, so both sides of a
side-by-side diff answer independently and a scrolled view stops looking like
an unscrolled one showing different text. The blank counterpart of a one-sided
row is not content and stays unmarked. The marks overlay edge cells instead of
adding columns, so the click-to-column mapping in `selection.go` is untouched;
`ui.h_scroll_marks = false` (threaded in by `applyDiffCfg` →
`SetHScrollMarks`) turns them off. `u` toggles the unified single-column
layout, where a changed pair renders as its removed line followed by its added
line under a dual old/new gutter. Line backgrounds come from the theme `ui`
slots `DiffAdded` / `DiffRemoved` (and `DiffChanged`, which the editor's
`.diff`/`.patch` word highlighting uses), declared by every builtin and
defaulted for sparse themes by tinting the theme's own
`Success`/`Error`/`Warning` toward its `Surface` (`theme.Mix`).

Intra-line changed ranges (#2170) do **not** borrow a third colour: they use
the side's own emphasis slot — `DiffAddedEmph` on an added line,
`DiffRemovedEmph` on a removed one — so an emphasized range reads as a
stronger patch of the line it sits in instead of turning both sides of a
changed pair the same colour. Every builtin declares both; a theme that omits
them gets them derived from its own diff backgrounds, pushed toward the side's
semantic hue and then pulled back until the result stays inside that
background's readability envelope (`emphHeadroom`, guarded by the theme
contrast audit). Because that envelope keeps the background step deliberately
small, the renderer carries the rest of the distinction as **bold +
underline** on the emphasized runes — visible in every theme, in both layouts,
and over the syntax foreground.

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
step through hunks — unless a search is open, when they step its matches
instead (see below); `w` toggles
ignore-whitespace on the open diff (see below); `enter`
dispatches `diff.JumpMsg` and the root model opens the right-hand file with
the cursor on the hunk's first line. The view is read-only; hunk-level "take
left/right" staging is a later increment for #28. The status line shows
`DIFF │ left ⇄ right │ -w │ hunk i/n` (the `-w` segment only while whitespace
is ignored, the sides being the side labels below) and the pane's title band
`DIFF left ⇄ right [-w]`.

### Scroll-aware hunk stepping & the current-hunk marker (#2494)

Hunk stepping is anchored to the **viewport**, not to a private counter. The
anchor row is the visual line a third down the view — the same place a step
scrolls its target hunk to. Every vertical scroll that actually moves the
view (keys, wheel, `g`/`G`, search jumps — everything funnels through
`scrollTo`) re-syncs the current hunk (`syncCurrentHunk`) to the hunk
covering or nearest the anchor, so after scrolling anywhere, `F7`/`n` steps
to the **first hunk starting below the anchor** and `shift+F7`/`N` to the
**last one starting above it** — never back to hunk 1. With no hunk in the
step's direction (a short diff fully visible) the step falls back to the
plain `cur±1` walk from wherever it stands (−1 on a fresh diff, so the first
`F7` still lands on hunk 1). Only while the current hunk was itself placed
by a step (no scroll in between, `curStepped`) does a step always walk the
`cur±1` sequence, which keeps repeated `F7` progressing when the viewport is
clamped at the document end. `CurrentHunk` is therefore the on-screen hunk
(−1 before the first step or scroll), and the pane paints it with a `▎`
**gutter marker** on
every row of the hunk, both sides, in the `DiffMarker` theme slot (falls back
to the theme's `Accent`). The marker is a View-time overlay like the
selection (#2495): the cached lines never carry it, so a scroll costs a
viewport, not a re-render.

### Header & footer (#2494)

The pane spends its top row on the **side labels** and its last row on a
**footer**: ` hunk i/n · p% · o expands ▸ marked gap · c shows all` — the
current hunk, the scroll progress (`Progress()`: 0 at the top, 100 when the
end is visible), and the collapsed-gap hint while separators are on screen
(`no changes` replaces the hunk part on an empty diff). The in-pane search
prompt (#2409) takes over the footer row while a search lives instead of
costing the body an extra line, and the live-reload notice (#2506) does the
same below it in precedence. Panes under three rows shed the header,
under two also the footer.

### Side labels (#2494)

Every diff has two named sides and the caller opening it knows them:
`SetSideLabels(left, right)` records what each side *is* — `HEAD` diffs pass
`name @ HEAD` / `working copy`, revision diffs the `name @ rev` pair (or
`working copy` for an unversioned right side), the clipboard diff
`clipboard` / `name (buffer)`, local history / external-change / timeline
diffs their snapshot title against `working copy`, `diff.files` the two base
names (the full paths when the bases collide, `diff.NewFiles`). The labels
render as **column headers** over the two sides (side-by-side) or as one
`left → right` line (unified), and replace the generic sides in the title
band and status line (`SideLabels()` falls back to the column titles where
no label was set — also the header-row trigger: no labels, no header row).
`Retarget` clears them; the retargeting call sites re-label.

## In-pane search (#2409)

`/` — and the shared find chord `cmd+f` / `ctrl+f` — opens a one-line prompt on
the pane's last row (`internal/diff/search.go`), the shape the explorer speed
search and the response viewer's prompt already wear: the slash prefix, the
query with its text cursor, and a `i/n` match counter (or `no matches`). Typing
re-matches live, `enter` applies and leaves the prompt, `esc` abandons the
search entirely. The state and those rules are the shared `ui.LineSearch`
(#2461, see [Project Search § In-pane search](./search.md)); the file keeps
only the match rule and the landing.

The match runs over the **diff rows** — a row's raw `Left`/`Right` text —
rather than the rendered lines, so a query finds the same thing in both
layouts, at any horizontal offset, and through the styling wrapped around the
text. Matching is smartcase like the editor's `/` (#257): an all-lowercase
pattern folds case, any uppercase rune makes it exact.

While a search is open `n`/`N` walk its matches (wrapping at both ends,
scrolling the match a third down the view like the hunk steps do); with no
search open they keep their hunk meaning — a diff without a query is still
navigated by change, which is what the pane is for. The prompt shares the
footer row (#2494) while the search lives, and `esc` gives the footer back.

Search landings always resolve against the **current** visual-row layout
(#2494): a match hidden inside a collapsed gap expands that gap first
(`revealRow` in `scrollToMatch`), so the jump lands on the match row itself,
never on the separator standing in for it — before and after `o`, a
separator click, or a `c` toggle, whose renders rebuild the row→line map the
landing reads. The match rows themselves are row indices into `res.Rows`,
so `SetContents` and every re-diff (`w`, edit-mode keystrokes) rebuild the
match list of a surviving query rather than stepping stale positions.

`OpenSearch` is the pane's `pane.Searchable` implementation, which is how the
Global `search.open` command reaches the prompt (see
[Keybindings](./keybindings.md)).

`NextMatch` / `PrevMatch` complete the capability (#2410): `cmd+g` /
`cmd+shift+g` do what `n`/`N` do, but **while the prompt still holds the
keyboard**, where `n` and `N` are query text. The prompt's counter marks the
step that came back around — `1/12 (wrapped)` — and every query edit drops the
marker, which describes one step rather than the search.

## Ignore whitespace (#2170)

`w` flips the pane between whitespace-significant and whitespace-insensitive
comparison. The toggle re-diffs the retained texts in place — no reload, no
lost scroll position — and the current hunk is clamped to the (usually
shorter) new hunk list. `SetIgnoreWhitespace` is the same switch for
non-interactive callers; `Rediff` and `SetContents` keep the mode, so edit
mode and re-opened contents stay on the reader's choice.

The state is persisted, not pane-local: the pane emits
`diff.IgnoreWhitespaceMsg` and the root model writes `diff.ignore_whitespace`
into the user settings (`config.WriteAndReload`, the conceal-rule shape from
#1998). The reload that follows re-applies both diff keys — `diff.context` and
`diff.ignore_whitespace` — to **every** open diff pane through
`Instance.configure`, so panes never drift apart, and a fresh pane picks the
value up in `Registry.applyDiffConfig`. Both keys are editable in the settings
panel's **Diff Viewer** page.

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

### The caching rule: the drag moves anchors, `View` paints (#2495)

The selection highlight is **not** baked into the cached lines. `render()`
builds every visual line selection-free, once per content/layout/size/theme
change; `View` re-styles only the visible lines the selection actually covers
(`selectedLines` gives the covered span, `renderVLine` re-paints one line), so
a frame with a selection costs a viewport, never a document.

This is the rule to keep: **a mouse motion event during a drag must only move
the selection anchors**. `MouseDrag` does exactly `textsel.Selection.Drag` —
no re-render, no re-diff, no re-parse, and no `SelectionText`, which runs only
when the selection is copied. Before #2495 the drag called `render()`, so
every pointer cell restyled all rows of the diff — 69 ms per motion event on a
3000-row syntax-highlighted side-by-side diff, and the highlight visibly
trailed the pointer. Drag events already ride the app's input coalescing
(`coalescedInputMsg`, see [performance](performance.md)); no pane-local
throttle exists or should be added.

Two consequences hold the design together. `renderVLine` is the *only* line
painter — `render()` loops it and `View` calls it for covered lines, so an
overlaid line is byte-identical to the one a full render would have produced.
And it must stay read-only: `View` has a value receiver, so its copy shares
these slices. `buildVRows` is the layout pass proper (it also fills
`rowStarts` and `sepLines`), and the gutter widths, which scan every row, are
cached per render pass (`gutL`/`gutR`) because every mouse-cell mapping asks
for them.

`BenchmarkDragMotion` in `internal/diff` measures one motion event plus the
frame it causes over 3000 rows; anything that scales with the document size
there is the regression this section exists to prevent.

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

## Live reload (#2506)

A file-vs-file diff **follows its two files on disk**. When the 0140 watcher
reports a change for either `leftPath` or `rightPath` — the user saving that
side in another pane, a build regenerating an output file, a `git checkout`,
an `echo x >> file` from a terminal — `routeWatchEvent` hands the event to
`reloadDiffsForPath` (`internal/app/diffwatch.go`), which re-reads *both*
sides and calls `Model.ReloadContents`. That is a re-diff in place, the same
move the ignore-whitespace toggle makes: the scroll offset is retained
(clamped to the new document), the current hunk is clamped to the new hunk
list, and the gaps the reader expanded stay expanded — matched by the left
line number their first hidden row carries, so an edit above them does not
fold them again. Identical bytes are a no-op. Always on, no setting: a stale
diff is never what the reader wants.

A **removed** side is not an error. `fixRemovedWatchKind` already reclassifies
a replace-in-place (write temp + rename) as a change; a file genuinely gone
diffs as the empty side and the pane's footer row carries a one-line notice —
`left file removed` / `right file removed` / `left and right file removed`
(`Model.SetNotice`, painted over the hunk/progress footer (#2494); the search
prompt outranks both while it is open). Writing the file again clears the
notice and brings the content back.

Only file-backed diffs reload. A HEAD/commit diff carries a revision
(`Revs()`) and a clipboard or local-history diff has no path on its left side;
both are snapshots by definition and keep their snapshot semantics. A diff in
edit mode (#496) is skipped too — its right column *is* a live editor buffer,
which reloads through the editor's own external-change path.

**Watching both sides.** The watcher only walks the project root, so a diff
between `/tmp/a.json` and a project file would never hear about the outside
side. `syncDiffWatches` reconciles, once per settled `Update` pass, the set of
paths the open file diffs need against `watch.Service.WatchPath` /
`UnwatchPath` (see [Foundation](./foundation.md)) — one reconcile against the
panes that are actually open, rather than a hook on every open, close,
retarget and restore site. Closing or retargeting a diff therefore releases
its registrations on the next pass; `watcher.WatchedPaths()` is the exported
state that proves it.

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

**A diff opens where the user is looking (#2507).** Every diff-open — HEAD and
commit diffs from the VCS panel, `diff.files`, the local-history and Timeline
diffs, `diff.compareWithClipboard`, the HTTP response diff — routes its freshly
created viewer through the one helper `openDiffLeaf`
(`internal/app/diff_placement.go`). Before #2507 each open split the editor
area to the right, so working in a full layout carved off yet another column
and the diff landed away from the eye.

- **Target pane.** `diffTabTarget` picks the pane in the layout's **flexible
  region** (the editor area of [Pane Layout](./pane-layout.md)) the user works
  in: the focused pane when it lies in that region, else `recentFlex` — the
  most recently focused flex pane, tracked in `setFocus` — else the first flex
  pane in tree order. `flexPane` defines the region: a tabbable content kind
  (editor, and the viewer panes — markdown, diff, image, archive, data, hex,
  notebook), never the explorer, a tool window, a terminal pane or a pure
  tool-tab host (#1989). The popup terminal and the floating panels are no
  layout leaves, so they never qualify. A diff requested from the VCS panel or
  the Issues window therefore lands in the editor pane the user came from, not
  in the bottom strip (#489).
- **Open means a content tab.** `nestDiffTab` moves the new viewer into that
  pane as a focused **content tab** (#1778): the pane converts into a tab host
  if it was a viewer pane, the diff instance detaches from its own pane
  (`DetachContent`) and joins the tab list, and the emptied pane closes. The
  pane's existing file tabs stay open beside it — no split, no resize.
- **An empty scratch pane is still taken over in place (#628).** When the
  target `Instance.IsEmptyEditor` (a single tab, no file, no text), the diff
  becomes that leaf via `layout.Replace` and the blank editor is dropped,
  rather than becoming the sole tab of an otherwise empty pane. A file-backed
  or dirty-scratch editor is never reused — its content is preserved and the
  diff joins it as a tab.
- **Reuse is per pane.** The single-diff-window rule (#513) now asks
  `diffSlot` for a diff **in the target pane** — the pane itself when it is a
  dedicated diff, else its first diff content tab. A second diff from that
  pane retargets that tab; a diff someone parked in another pane is left
  alone. `diff.windows = "multi"` skips the reuse and adds another tab. The
  re-open-same-pair shortcut (#509, `findDiffPane`) is untouched: an already
  open identical diff is focused wherever in the workspace it lives.
- **`diff.placement`** selects the mode: `focused` (default) is the above,
  `split` restores the pre-#2507 behaviour exactly — `placeDiffLeaf` beside
  the active editor with the workspace-wide single slot. The key is editable
  on the settings panel's *Diff Viewer* page. `placeDiffLeaf` is also the
  fallback whenever the layout has **no flexible pane at all** (a workspace of
  explorer plus tool windows), and stays the merge view's own placement
  (#1478).

## Persistence

Layout persistence saves `{kind: "diff", path, path2}`; restore rebuilds the
pane and re-reads both files from disk (a vanished side restores empty rather
than breaking the layout).

## Diff viewer v2 (Epic 0340)

- **Collapsed context** — unchanged runs fold into `▸ ··· N unchanged lines ···`
  separators around a context budget (default 3, config `diff.context`;
  negative disables). `c` toggles collapsed/full (keeping the row at the top
  of the view in place, like the layout toggle), `o` expands the gap nearest
  the viewport center; expansions reset with new contents. Every separator
  carries a `▸` **expand button** on its left edge (#2494) — a mouse press on
  it (`sepButtonWidth`, checked in `MousePress` before the selection) expands
  that gap; presses past the button still select the row (fold copy, #1741).
  The separator `o` would expand renders whole in the `DiffMarker` colour
  (the `targetGap` overlay, painted per frame like the selection), the others
  stay dim but keep the accent-coloured button; the footer hint reads
  `o expands ▸ marked gap · c shows all`. Hunk navigation
  and jumps work over collapsed maps.
- **F7 / shift+F7** — next/previous change via the diff-scoped default
  bindings (`diff.nextChange`/`diff.prevChange`); `n`/`N` stay. Both are
  scroll-aware (#2494, see § Scroll-aware hunk stepping above).
- **Editable current side** — `e` on a worktree-backed diff (diff.files,
  vcs.diff, the changes view) mounts a live editor as the right column: full
  vim editing, `:w` saves, shared document with open tabs, the left column
  re-aligns per keystroke; `ctrl+e` returns to browsing. Revision-vs-revision
  diffs (the log view) stay read-only with a hint.
