---
type: concept
title: VCS / Git Integration
description: "Epics 0320/0330, slimmed by #750 — git status snapshot behind explorer coloring, branch status-line segment, gutter diff markers, file-vs-HEAD diff, hunk/file revert, inline blame, and a read-only changes tool window; git *workflow* (staging, commits, branches, log) is delegated to custom tool panes, with lazygit preconfigured; all git calls async via tea.Cmd."
resource: internal/vcs
tags: [architecture, vcs, git]
timestamp: 2026-07-24T00:00:00Z
---

# VCS / Git Integration (Epics 0320/0330, slimmed by #750)

`internal/vcs` owns every Git interaction: it shells out to the `git` CLI
(no libgit2), always inside a `tea.Cmd` with a 5s timeout — **no blocking IO
in `Update`**. Consumers read immutable snapshots; they never run git
themselves. Non-git workspaces see a nil snapshot and degrade to quiet no-ops
everywhere.

## The split (#750): file-context native, workflow via tool panes

Since #750 the native VCS integration is deliberately limited to what an
external TUI cannot provide — **file-context features in the editor**:

- the `internal/vcs` library (status snapshot, marks, blame, diff blobs,
  revert log) backing every other subsystem,
- editor gutter markers, inline blame, file-vs-HEAD diff, hunk/file revert,
- explorer status coloring, the statusline branch segment,
- a read-only changes list in the tool window.

The git **workflow** — staging, commits, branch management, stash, log
browsing, interactive rebase — is delegated to
[custom TUI tool panes](/architecture/tool-panes.md) (#741). `lazygit` is the
shipped example: when it is on PATH the default configuration preconfigures it
as a `[[tools.custom]]` entry (`internal/config/defaults.go`), so
`tool.lazygit` opens it as a bottom pane with zero setup; when it is missing,
the `tools.setup` onboarding offers to install it (`internal/toolcatalog`) —
a convenience default, never a hard dependency. The former native commit
dialog (`internal/commitui`), the panel's Log tab and staging affordances, and
the `vcs.commit`/`vcs.updateProject`/`vcs.branches` commands were removed.

## Status snapshot

`vcs.Load` parses `git status --porcelain=v2 --branch -z` into a `Snapshot`:
branch (+ ahead/behind, detached short hash), per-file `FileStatus`
(modified/added/deleted/renamed/untracked/conflicted), dirty-directory
propagation for the tree tint, and per-file index/worktree letters
(`FileEntry.X/Y`). Path lookups accept absolute or repo-relative paths and
survive macOS `/private` symlinked roots.

**Refresh lifecycle** (`internal/app/vcs_state.go`): the initial load rides
`StartWatcher` (main.go-only, so tests stay free of the developer repo's
state); watcher events (0140) and buffer saves arm a 250ms debounce tick;
runs are serialized with at most one queued follow-up; every mutating VCS
command answers with a refresh. External git changes are covered too (#738):
the watch service additionally watches `.git` and `.git/logs` (index, HEAD,
packed-refs, reflog — lock/temp churn filtered) and reports them as one
coalesced `GitChanged` event, so commits, branch switches, staging or pulls
made in a lazygit tool pane or a terminal refresh the snapshot automatically.
Each new snapshot re-feeds the explorer, the VCS panel, gutter marks, and
enabled blame maps.

## Surfaces

- **Explorer coloring** — entries take their status hue from the theme slots
  `VCSModified/Added/Untracked/Deleted/Conflicted` (semantic-hue fallbacks
  for sparse themes); dirty directories tint modified.
- **Status line** — right-side `⎇ branch ↑n ↓m` segment, 24-char clip,
  hidden outside repos.
- **Gutter diff markers** (`internal/vcs/marks.go`) — buffer vs HEAD blob via
  `internal/diff`; added/changed lines recolor the line number, removals mark
  the line below (EOF removals fold onto the last line). Diagnostics win the
  cell. Recomputed per snapshot and on open; only modified/conflicted/renamed
  files spawn a git subprocess.
- **Inline blame** (`internal/vcs/blame.go`, `vcs.blameLine`, palette) —
  toggleable dimmed EOL annotation on the cursor line ("author, when ·
  summary", "not committed yet"); whole-file porcelain blame cached per
  document, refreshed with each snapshot.

## Commands

All registered through the `appCommands` plugin (palette-visible); Cmd chords
stay the JetBrains defaults; the palette (esc-esc) is the delivered path
since the leader layer retired (#711):

| Command | Keys | Behavior |
|---|---|---|
| `vcs.revertFile` | `cmd+alt+z` (JetBrains rollback) | Restore the focused file to HEAD behind a confirmation prompt showing the changed-line count; buffer reloads. The pre-revert content is snapshotted into the revert history first (`internal/vcs/revertlog.go`, under the state store, capped + age-pruned). |
| `vcs.undoRevert` | palette | Palette picker over the focused file's revert-history snapshots (newest first, timestamp + changed-line count); selecting one re-applies it to the buffer as a single undo-tree change — dirty, undoable, saved only explicitly. |
| `vcs.revertHunk` | palette | JetBrains "Rollback Lines": restore the contiguous change under the caret (the gutter-marked region, deletion anchors included) to its HEAD content. Applied as one buffer edit through the undo tree — plain undo brings the hunk back; works against unsaved edits too (`internal/editor/vcs_revert.go`). |
| `vcs.diff` | palette | Diff pane: live buffer vs HEAD blob (reuses the [Diff Viewer](/architecture/diff-viewer.md)). |
| `vcs.blameLine` | palette | Toggle the inline blame annotation. |
| `vcs.panel` | `cmd+9` | Toggle the VCS tool window (below). |
| `tool.lazygit` | palette | Open/focus the preconfigured lazygit tool pane (when lazygit is on PATH; a [#741 custom tool](/architecture/tool-panes.md), not part of `internal/vcs`). |

`vcs.commit` (`cmd+k`) and `vcs.updateProject` (`cmd+t`) were removed in
#750; `cmd+k` remains solely the prefix of the pane-split sequence family
(`cmd+k down/up/left/right/z` keep working — the resolver holds the prefix
pending and a bare `cmd+k` simply times out), and `cmd+t` keeps its
terminal-context meaning (new terminal tab) with no global binding.
JetBrains-XML imports of `CheckinProject`/`Vcs.UpdateProject`/`Git.Branches`
land unmapped (`internal/keymap/jbimport`).

The revert/diff/blame helpers live in `internal/vcs` as async commands
resolving to result messages; errors surface as toasts carrying the decisive
git stderr line. The library keeps its full op surface (stage, commit,
checkout, log, show — `ops.go`, `log.go`, `branches.go`, `update.go`) for
tests and future consumers, but the app no longer wires workflow UIs to it.

## VCS tool window (Epic 0330, slimmed by #750)

`vcs.panel` (`cmd+9`, JetBrains' Version Control window chord) toggles the
persistent tool window (`internal/vcspanel`, pane kind `KindVCS`, singleton
key `vcs`): a bottom split below the active editor with terminal-style
focus-return semantics. It is a **read-only changes list** re-derived from
every snapshot: status badge + path per row, VCS-colored via the shared
status recipe (#1052), `j`/`k`/wheel/click navigation with the muted
unfocused cursor (#1034), and `enter`/double-click opens the file's
diff-vs-HEAD. No staging checkboxes, no commit message, no Log tab — that
workflow lives in the lazygit tool pane.

The panel never runs git itself — it emits request messages the root model
answers with `internal/vcs` commands. The layout slot persists (kind
`"vcs"`); content is session-local and re-feeds from the first snapshot.

## Later increments

Whole-file blame gutter and merge-conflict resolution UI remain candidates as
*editor* file-context features. Workflow features (staging, stash, branch
graph, interactive rebase) are out of scope for the native integration —
they belong to the delegated tool panes.
