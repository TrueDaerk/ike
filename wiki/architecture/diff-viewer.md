---
type: concept
title: Diff Viewer
description: "#60/0340 — reusable read-only diff pane: line-level Myers engine with intra-line refinement, side-by-side or unified rendering with theme diff slots and per-side tree-sitter syntax highlighting, hunk navigation (n/N, enter jumps the editor), diff.files palette command, layout persistence."
resource: internal/diff
tags: [architecture, diff, pane, vcs]
timestamp: 2026-08-07T00:00:00Z
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
gutters and a `│` separator, both sides wrapped to their column budget with
`viewport.WrapSegments` (the editor's cell-budgeting; `↪` marks continuation
rows, tabs display four cells wide). `u` toggles the unified single-column
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

Keys (focused pane): `j`/`k`/arrows scroll by visual row, `ctrl+u`/`ctrl+d`
and page keys page, `g`/`G` jump to the ends, the mouse wheel scrolls. `n`/`N`
step through hunks (scrolling the hunk a third down the view); `enter`
dispatches `diff.JumpMsg` and the root model opens the right-hand file with
the cursor on the hunk's first line. The view is read-only; hunk-level "take
left/right" staging is a later increment for #28. The status line shows
`DIFF │ left ⇄ right │ hunk i/n`.

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
per-conflict accept ours/theirs/both and next/previous navigation unchanged
(palette `merge.*` commands — the pane advertises the editor context, and
`editor.ActionMsg` routes into the result editor when a merge pane has
focus). The header and statusline show `unresolved/total`; the side columns
follow the result editor's scroll offset.

Entry points: `vcs.mergeFile` on the focused conflicted file, or `enter` on
a conflicted VCS-panel row. `vcs.mergeApply` saves the result, stages the
file and closes the view (blocked while conflicts remain); closing the pane
with unresolved conflicts or an unsaved result opens a discard/cancel guard.
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
