---
type: concept
title: VCS / Git Integration
description: "Epics 0320/0330 — git status snapshot behind explorer coloring, branch status-line segment, gutter diff markers, commit dialog, update/revert, branch picker, file-vs-HEAD diff, inline blame, persistent tool window (changes + log); all git calls async via tea.Cmd."
resource: internal/vcs
tags: [architecture, vcs, git]
timestamp: 2026-07-12T00:00:00Z
---

# VCS / Git Integration (Epics 0320/0330)

`internal/vcs` owns every Git interaction: it shells out to the `git` CLI
(no libgit2), always inside a `tea.Cmd` with a 5s timeout — **no blocking IO
in `Update`**. Consumers read immutable snapshots; they never run git
themselves. Non-git workspaces see a nil snapshot and degrade to quiet no-ops
everywhere.

## Status snapshot

`vcs.Load` parses `git status --porcelain=v2 --branch -z` into a `Snapshot`:
branch (+ ahead/behind, detached short hash), per-file `FileStatus`
(modified/added/deleted/renamed/untracked/conflicted), dirty-directory
propagation for the tree tint, and per-file index/worktree letters
(`FileEntry.X/Y`) for the commit dialog. Path lookups accept absolute or
repo-relative paths and survive macOS `/private` symlinked roots.

**Refresh lifecycle** (`internal/app/vcs_state.go`): the initial load rides
`StartWatcher` (main.go-only, so tests stay free of the developer repo's
state); watcher events (0140) and buffer saves arm a 250ms debounce tick;
runs are serialized with at most one queued follow-up; every mutating VCS
command answers with a refresh. Each new snapshot re-feeds the explorer, the
commit dialog, gutter marks, and enabled blame maps.

## Surfaces

- **Explorer coloring** — entries take their status hue from the new theme
  slots `VCSModified/Added/Untracked/Deleted/Conflicted` (semantic-hue
  fallbacks for sparse themes); dirty directories tint modified.
- **Status line** — right-side `⎇ branch ↑n ↓m` segment, 24-char clip,
  hidden outside repos.
- **Gutter diff markers** (`internal/vcs/marks.go`) — buffer vs HEAD blob via
  `internal/diff`; added/changed lines recolor the line number, removals mark
  the line below (EOF removals fold onto the last line). Diagnostics win the
  cell. Recomputed per snapshot and on open; only modified/conflicted/renamed
  files spawn a git subprocess.
- **Inline blame** (`internal/vcs/blame.go`, `vcs.blameLine`, `space v a`) —
  toggleable dimmed EOL annotation on the cursor line ("author, when ·
  summary", "not committed yet"); whole-file porcelain blame cached per
  document, refreshed with each snapshot.

## Commands

All registered through the `appCommands` plugin (palette-visible); Cmd chords
stay the JetBrains defaults, the leader `space v` family is the delivered
path (`space g` belongs to grep):

| Command | Keys | Behavior |
|---|---|---|
| `vcs.commit` | `cmd+k` / `space v c` | Commit dialog (`internal/commitui`): changed files with `[x]/[~]/[ ]` stage toggles (space), message pane (tab), `ctrl+s` commits, esc keeps the in-progress message; disabled-commit hints. |
| `vcs.updateProject` | `cmd+t` / `space v u` | `git pull` (merge, or rebase via config `vcs.update = "rebase"`); dirty tree blocks with a warning; summary toast (commits/files). |
| `vcs.revertFile` | `cmd+shift+t` / `space v x` | Restore the focused file to HEAD behind a confirmation prompt showing the changed-line count; buffer reloads. |
| `vcs.revertHunk` | `space v h` | JetBrains "Rollback Lines": restore the contiguous change under the caret (the gutter-marked region, deletion anchors included) to its HEAD content. Applied as one buffer edit through the undo tree — plain undo brings the hunk back; works against unsaved edits too (`internal/editor/vcs_revert.go`). |
| `vcs.branches` | `space v b` | Palette picker of local branches (current first), checkout on select. |
| `vcs.diff` | `space v d` | Diff pane: live buffer vs HEAD blob (reuses the [Diff Viewer](/architecture/diff-viewer.md)). |
| `vcs.blameLine` | `space v a` | Toggle the inline blame annotation. |

Stage/unstage/commit/checkout/pull/revert live in `internal/vcs` as async
commands resolving to result messages; errors surface as toasts carrying the
decisive git stderr line.

## VCS tool window (Epic 0330)

`vcs.panel` (`space v v`) toggles the persistent JetBrains-style tool window
(`internal/vcspanel`, pane kind `KindVCS`, singleton key `vcs`): a bottom
split below the active editor with terminal-style focus-return semantics.
Two tabs, switched with `1`/`2`/`tab`:

- **Changes** — the staging list re-derived from every snapshot: `space`
  stages/unstages through the shared ops, `enter` opens the file's
  diff-vs-HEAD, `c`/`m` focus the message field, `ctrl+s` commits with the
  dialog's validation hints. The commit message is a shared
  `vcs.MessageDraft`: the panel and the modal `vcs.commit` dialog edit the
  same text, and only a successful commit clears it.
- **Log** — windowed history (`LogCmd`, 50 per page; `j` past the tail loads
  more, `r` reloads): `enter` expands a commit's changed files (`ShowCmd`,
  renames keep their old path), `enter` on a file opens the parent-vs-commit
  diff (`FileAtCmd` blobs into the diff viewer, titled `name @ sha^ ⇄ name @
  sha`). The log reloads after commit/update/checkout.

The panel never runs git itself — it emits request messages the root model
answers with `internal/vcs` commands. The layout slot persists (kind
`"vcs"`); content is session-local and re-feeds from the first snapshot.

## Later increments

Whole-file blame gutter, hunk staging from the panel, merge-conflict
resolution UI, stash management, branch-graph rendering in the log.
