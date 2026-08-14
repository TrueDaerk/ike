# Git

IKE's Git integration is deliberately half a Git client. It does the parts an
external tool cannot do — showing you Git state *inside the file you are
editing* — and hands the rest to a tool pane.

That split is a design decision, not a gap:

**Native, in the editor** — status colours in the file tree, the branch in the
status line, change markers in the gutter, diff against HEAD, reverting a hunk
or a file, inline blame, merge-conflict resolution, a read-only changes list.

**Delegated to a tool pane** — staging, committing, branches, stash, log
browsing, interactive rebase. [`lazygit`](https://github.com/jesseduffield/lazygit)
does all of that better than a from-scratch reimplementation would, and if it
is on your `PATH` it is already configured as a tool pane — run **Tool:
lazygit** from the palette.

## What you see without doing anything

**In the file tree**, entries take a colour from their status: modified,
added, untracked, deleted, conflicted. A directory containing changes is
tinted too, so you can see where the work is without expanding.

**In the status line**, a `⎇ branch ↑2 ↓1` segment on the right — branch,
commits ahead, commits behind. It disappears outside a repository.

**In the gutter**, added and changed lines recolour their line number, and a
removal marks the line below it. Diagnostics win the cell when both apply.

All screenshots on this page use the `monokai-pro` theme.

![Modified files marked in the tree, changed lines in the gutter, the branch in the status line](../screenshots/features/vcs-gutter.png)

Everything is recomputed as the repository changes. Git runs asynchronously,
never blocking the editor, and only files that are actually modified cost a
subprocess.

## Working with changes

| Keys / command | What it does |
|---|---|
| `]c` / `[c` | Next / previous change in the file |
| **Diff File Against HEAD** | Open the file's diff in a pane |
| ++cmd+alt+z++ | Revert the whole file |
| **Revert Hunk Under Caret** | Revert just the hunk at the cursor |
| **Undo Revert…** | Take a revert back — reverts are logged |
| **Toggle Inline Blame** | Dimmed end-of-line annotation on the cursor line |

Inline blame shows author, when, and the commit summary — or "not committed
yet" for a line you just wrote. It follows the cursor and refreshes with the
repository.

## The diff viewer

**Diff File Against HEAD** opens a read-only diff pane; **Diff Two Files…**
diffs any two files against each other. Inside a diff pane:

| Keys | What it does |
|---|---|
| ++f7++ / ++shift+f7++ | Next / previous change |
| `n` / `N` | The same, vim-style |
| ++enter++ | Jump the editor to that change |
| `h` / `l`, ++left++ / ++right++ | Scroll sideways by one column |
| ++shift+left++ / ++shift+right++ | Scroll sideways by half a column |
| `0` / `$` | Jump to the first / last column |

![The diff viewer side by side with the editor](../screenshots/features/diff-viewer.png)

The diff is line-level with intra-line refinement, so a one-character change
highlights the character rather than the whole line. Side-by-side and unified
rendering are both available, and the pane persists in your layout like any
other.

Long lines are never wrapped — they are clipped at the edge of their column and
you scroll sideways instead, with both sides moving together so the two
versions of a line always stay on the same row. The horizontal wheel (or
++shift++ + wheel) does the same as the keys above.

## The changes tool window

++cmd+9++ toggles a list of every changed file in the repository — status
badge and path per row, coloured the same way the explorer is. `j`/`k`, the
wheel or a click move through it; ++enter++ or a double-click opens that
file's diff against HEAD.

![The VCS tool window beside the editor, listing the modified file with its status badge and the current branch](../screenshots/features/vcs-panel.png)

It is read-only on purpose: no staging checkboxes, no commit message field, no
log tab. That is the lazygit pane's job.

## Merge conflicts

Conflict blocks in a file are detected and tinted, and resolvable where they
are: accept ours, accept theirs, or accept both, with navigation that wraps
around and marks in the overview ruler. You never leave the editor to resolve
a conflict in a file.

The surrounding merge *workflow* — starting it, aborting it, continuing a
rebase — stays with lazygit.

## Setting up the tool pane

If `lazygit` is on your `PATH`, nothing to do. Otherwise install it, or point
a tool pane at whatever you prefer:

```toml
[[tools.custom]]
name = "lazygit"
command = "lazygit"
```

**Set Up Tool Panes** from the palette walks through this interactively. See
[the terminal guide](terminal.md#tool-panes) for how tool panes behave.

!!! note "IKE never runs a destructive Git command on its own"
    Reverting a file or hunk is the only write path, it is logged, and
    **Undo Revert…** takes it back. Everything else is a read.
