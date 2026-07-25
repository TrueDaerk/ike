# Panes, tabs and layouts

IKE's window is a **split tree**. There is no fixed sidebar-plus-editor
arrangement: every region is a pane, every pane can be split, and any pane can
end up anywhere.

## The pieces

**Pane** — one rectangle holding one thing: the file tree, an editor, or a tool
window. Panes tile the window exactly; there is no gap between them, and the
seam you see is the two panes' own borders meeting.

**Split** — a pane divided in two, either side by side or stacked, at a ratio
you can drag. Splitting a split is how you get arbitrary arrangements.

**Tab** — editor panes hold an ordered list of tabs. Terminals live in tabs
too, so a pane can hold a mix of files and shells.

**Focus** — exactly one pane has it. It decides which keybindings are active
(the [keybinding reference](../reference/keybindings.md) is grouped by
context), which pane a newly opened file lands in, and what most commands act
on.

## Splitting and moving

| Keys | What it does |
|---|---|
| ++cmd+k++ then ++left++ / ++right++ / ++up++ / ++down++ | Split the focused pane in that direction |
| ++cmd+k++ then ++z++ | Maximize the focused pane, and back |
| ++ctrl+tab++ | Cycle pane focus (++ctrl++ + arrows moves directionally) |
| ++cmd+shift+f12++ | Hide all tool windows |
| ++shift+f12++ | Restore the default layout |

With the mouse: drag a divider to resize, drag a pane's title bar to move the
pane somewhere else, right-click a title bar for the pane's context menu. A
drag only engages after the pointer travels a little, so a plain click on a
title bar just focuses.

!!! note "`ctrl+tab` inside tmux"
    tmux consumes ++ctrl+tab++ for its own tab switching and never forwards
    it. Use the arrow variants, or rebind `pane.switcher`.

## Tabs

Tabs belong to a pane, not to the window. Opening a file routes it into the
**focused** pane's tab list, so where your focus is decides where the file
lands.

| Keys | What it does |
|---|---|
| ++cmd+ctrl+right++ / ++cmd+ctrl+left++ | Next / previous tab |
| ++alt+1++ … ++alt+9++ | Jump to tab *n* |
| ++ctrl+shift+page-up++ / ++ctrl+shift+page-down++ | Move the current tab left / right |
| ++cmd+w++ | Close the active tab |
| ++cmd+shift+t++ | Reopen the last closed tab |

### The tab limit

`editor.tabs.limit` (default `5`) caps the document tabs per pane,
JetBrains-style: opening a file beyond the limit closes the least recently used
tab instead of growing the bar forever. Set it to `0` to disable the cap.

Eviction never costs you anything you cannot get back. Exempt from it are the
active tab, tabs with unsaved changes, scratch tabs (there is no path to reopen
them from), terminal tabs, and **pinned** tabs. If nothing is eligible — every
other tab pinned, say — the limit is simply exceeded rather than something
being closed that should not be. Evicted tabs go into the reopen ring, so
++cmd+shift+t++ brings them back.

Pin a tab with **Pin/Unpin Tab** from the palette or the tab context menu. A
pinned tab is exempt from eviction and survives the close-others actions.

## Buffers are shared

The same file open in two panes is one buffer, not two copies: type in one and
the other updates, and it is dirty or clean in both. Closing one tab does not
discard the content the other still shows.

That is what makes a split-with-the-same-file useful — two viewports, one
document, each with its own cursor and scroll position.

## Layouts persist

The arrangement is saved **per project**: reopen a project and the panes, the
splits and their ratios come back the way you left them.

Beyond that you can name arrangements:

- **Save Window Layout…** stores the current arrangement under a name.
- **Window Layouts…** opens a picker to apply one.
- **Set Default Window Layout…** marks one as the layout new projects start from.
- ++shift+f12++ restores the default layout when a session has drifted.

Named layouts are user-scoped, so they follow you across projects. A saved
layout that no longer makes sense — a pane type that is gone, a window that
shrank — degrades rather than failing; you never lose a pane to a stale
layout.

## What is *not* a pane

Overlays are not part of the tree and do not disturb it: the command palette,
the help cheatsheet, the settings panel, the welcome tour and confirmation
dialogs all float above the layout and give focus back when they close.
