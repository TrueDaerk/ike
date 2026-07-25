# Getting around

Ten ways to reach a line of code, each good at something different. This page
is a map of which one to reach for.

## By name, when you know what you want

| Keys | What it does |
|---|---|
| ++cmd+shift+o++ | Go to file — fuzzy name match |
| ++cmd+o++ | Go to symbol — needs a language server |
| ++cmd+e++ | Recent files |
| ++shift++ twice | Search everywhere: commands and files in one query |

These are the fastest path when you can name the thing. Fuzzy matching means
initials work: `iaa` finds `internal/app/app.go`.

## By browsing

++cmd+1++ toggles the file tree and moves focus to it. Inside, the keys are
vim-shaped:

| Keys | What it does |
|---|---|
| `j` / `k` | Move the cursor |
| `l` / ++right++ | Expand a directory, or open a file |
| `h` / ++left++ | Collapse, or jump to the parent |
| ++enter++ | Open |
| `o` | Open in a split |
| `gg` / `G` | Top / bottom |
| ++cmd+n++ | New file |

**Explorer: Speed Search** filters the tree by typing, and
**Explorer: Toggle Hidden Files** shows dotfiles. ++alt+f1++ reveals the file
you are editing in the tree — useful after arriving somewhere via Go to file.

File operations happen here too: rename, move, delete, new folder. They are
undoable — ++cmd+z++ in the explorer undoes the last file operation, and
deleted entries are kept in `.ike-trash/` so undo has something to restore.

`explorer.exclude` hides noise (`.git`, `.idea`, `.DS_Store` by default) from
the tree without hiding it from search.

## By where you have been

| Keys | What it does |
|---|---|
| `cmd+[` | Back |
| `cmd+]` | Forward |
| Mouse buttons 4 / 5 | The same, if your mouse has them |

The history records *jumps*, not every cursor move — going to a definition,
opening a search result, jumping to a line. So Back lands where you were
before the jump, not one character to the left.

`g;` and `g,` are the related vim keys: they walk your recent **edit**
positions rather than your jumps.

## By marks you set yourself

**Vim marks** are the lightweight option: `ma` sets mark `a` at the cursor,
`` `a `` jumps back exactly, `'a` to the line. Lowercase marks are local to
the file; uppercase ones are global and jump across files.

**Bookmarks** is the same data with a UI — a picker listing the current file's
local marks plus every global one as `'x  path:line  preview`. ++enter++
jumps; ++shift+delete++ removes a mark. It is palette-only (**Bookmarks**),
because the vim keys are the interface.

## By numbered slot

Pinned files are the harpoon-style working set: pin the four files you are
actually moving between, then jump by number without thinking about names.

| Keys | What it does |
|---|---|
| ++ctrl+shift+1++ … ++ctrl+shift+4++ | Jump to slot 1–4 |
| ++cmd+2++ | Open the pins picker — reorder, unpin, or pin the current file |

**Pin File to Slot N** from the palette pins the active file. Pins are stored
per project and survive restarts.

This is the tool for a task that spans three or four files — a handler, its
test and a config — where Go to file is more typing than the job deserves.

## By structure

++cmd+3++ opens the Structure pane: the symbol tree of the focused buffer,
from the language server. It follows your cursor, so it doubles as "where am
I in this file", and ++enter++ on a node jumps to it.

The breadcrumb row under the tab bar shows the same information compressed to
one line — `file ▸ Type ▸ method` — and its segments are clickable.

## By what is wrong

| Keys | What it lists |
|---|---|
| ++cmd+8++ | Problems — every diagnostic in the project, errors first |
| ++cmd+6++ | TODO index — every `TODO`, `FIXME`, `HACK`, `XXX` comment |
| ++f2++ / ++shift+f2++ | Next / previous diagnostic in the current file |

Both windows group by file and jump on ++enter++. Problems has an `f` toggle
for current-file versus project scope; the TODO index filters by tag and
rescans a file when you save it. The tags it looks for are configurable.

**Usages** and **Find Usages** answer the other direction — where is this
symbol used — via the language server. See
[Code intelligence](code-intelligence.md).

## Back in time

Local history keeps a snapshot of every file each time you save it.
**Show Local History** opens a picker to diff a snapshot against what you have
now, or restore one — as a normal undoable edit, so restoring the wrong
snapshot costs you a ++cmd+z++.

It is per project and independent of Git: it covers the window between commits
where `git diff` has nothing to say yet.

## Which one to use

| You know… | Use |
|---|---|
| The file's name | Go to file |
| The symbol's name | Go to symbol |
| Roughly where it is in the tree | The explorer |
| That you were just there | Back, or Recent files |
| That you will return often | Pinned files, or a mark |
| Only that something is broken | Problems |
| Nothing, and want everything | Search everywhere |
