---
type: concept
title: Diff Viewer
description: "#60/0340 — reusable read-only diff pane: line-level Myers engine with intra-line refinement, side-by-side or unified rendering with theme diff slots, hunk navigation (n/N, enter jumps the editor), diff.files palette command, layout persistence."
resource: internal/diff
tags: [architecture, diff, pane, vcs]
timestamp: 2026-07-12T00:00:00Z
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
Syntax highlighting inside the diff is deferred polish.

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
then splits the focused leaf right with the diff pane and focuses it.
Dismissing the picker mid-flow disarms the state so a later `@` open is a
plain file open. Unreadable files diff as empty text.

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
