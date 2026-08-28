# Getting around

Ten ways to reach a line of code, each good at something different. This page
is a map of which one to reach for. All screenshots use the `monokai-pro`
theme.

## By name, when you know what you want

| Keys | What it does |
|---|---|
| ++cmd+shift+o++ | Go to file — fuzzy name match |
| ++cmd+o++ | Go to symbol — needs a language server |
| ++cmd+e++ | Recent files |
| ++shift++ twice | Search everywhere: commands and files in one query |

These are the fastest path when you can name the thing. Fuzzy matching means
initials work: `iaa` finds `internal/app/app.go`.

![The recent-files palette, most recently visited first, each row with the time it was last open](../screenshots/features/recent-files.png)

## By browsing

++cmd+1++ toggles the file tree and moves focus to it. Inside, the keys below
apply **while the tree has focus** — they are vim-shaped:

| Keys | What it does |
|---|---|
| `j` / `k` | Move the cursor |
| `l` / ++right++ | Expand a directory, or open a file |
| `h` / ++left++ | Collapse, or jump to the parent |
| ++enter++ | Open |
| `o` | Open in a split |
| `gg` / `G` | Top / bottom |
| ++cmd+n++ | New file |

![The file tree with every directory expanded, the open file underlined, and the selected row highlighted](../screenshots/features/window-overview.png)

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

## By what you can see

When the place you want is already on screen, the **label jump** (`gs` in
normal mode, *Jump to Visible Text* in the palette) is the shortest path: type
one or two characters of what you are looking at, every visible match gets a
short label overlaid on it — home-row keys first, the nearest match gets `a` —
and typing a label puts the caret right there. Two to four keystrokes to any
visible position, no counting, no search-and-enter round trip.

Details that make it fast:

- A **unique match jumps immediately** — no label needed.
- Labels never collide with the text: a key that could still narrow the search
  keeps narrowing; everything else picks a label.
- With more matches than keys, the overflow gets **two-letter labels** — or
  just type a second target character to thin the field.
- ++esc++ cancels; the cursor stays where it was.

The landing counts as a jump, so Back (`cmd+[`) returns you afterwards.

## By marks you set yourself

**Vim marks** are the lightweight option: `ma` sets mark `a` at the cursor,
`` `a `` jumps back exactly, `'a` to the line. Lowercase marks are local to
the file; uppercase ones are global and jump across files.

**Bookmarks** are the JetBrains flavour: a bookmark belongs to the project,
not to a key you have to remember. ++f11++ toggles one on the current line —
it shows as a `⚑` in the gutter and survives restarts.

| Keys | What it does |
|---|---|
| ++f11++ | Toggle a bookmark on the current line |
| ++alt+f3++ | Assign a mnemonic `0`–`9` (the same digit again removes the bookmark) |
| ++shift+f11++ / ++ctrl+shift+f11++ | Next / previous bookmark, wrapping across files |
| ++cmd+f3++ | Open the bookmarks picker |

A bookmark with a mnemonic shows that digit in the gutter instead of the flag,
and **Go to Bookmark by Mnemonic** (palette) jumps to it by pressing the digit.
**Edit Bookmark Note** (palette) annotates the line — the note replaces the
line preview in the picker, so a bookmark can say *why* it matters.

The picker (**Bookmarks**, ++cmd+f3++) lists everything at once: the current
file's local marks and every global one as `'x  path:line  preview`, plus the
project's bookmarks as `⚑x  path:line`. ++enter++ jumps; ++shift+delete++
removes the entry.

Bookmarks move with your edits, follow files you rename or move, and are kept
per project in `.ike/bookmarks.json`.

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

++cmd+3++ opens the Structure tool window: the symbol tree of the focused buffer,
from the language server. It follows your cursor, so it doubles as "where am
I in this file", and ++enter++ on a node jumps to it.

The breadcrumb row under the tab bar shows the same information compressed to
one line — `file ▸ Type ▸ method` — and its segments are clickable.

### In JSON and YAML

A deep manifest or lockfile has no symbols, so the status line answers instead:
it shows the path to whatever the caret is on — `spec.template.containers[2].env[0].name`
— and truncates it from the left when it does not fit, keeping the end you are
actually looking at. Sequence indices count from zero, the way `jq` and `yq`
count them.

++cmd+alt+shift+c++ copies that path in full. **Copy JSON/YAML Path as jq
Expression** and **… as yq Expression** copy the same position as an expression
the matching CLI tool takes, quoting keys where those tools need it:

```
.spec.containers[2].name
.metadata.labels["app.kubernetes.io/name"]
```

The path is read from the document as written: a YAML alias (`*base`) or merge
key (`<<`) is reported where it stands, never as the values it would pull in.

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

**Show Timeline** puts both halves on one list: the file's snapshots *and* the
commits that touched it, newest first. ++enter++ diffs the selected entry
against what is in the buffer, ++m++ marks an entry and ++d++ diffs it against
the selected one — a snapshot against a commit works too. ++r++ restores a
snapshot, ++y++ copies a commit hash, ++f++ switches between showing both
sources, snapshots only or commits only, and ++shift+l++ loads older commits
for long histories. Which sources it starts with is the *Timeline source
filter* setting.

**Show Project History Timeline** asks the other question: not what happened to
*this* file, but what you changed *today*. It lists the snapshots of every file
in the project, newest first, under day headings — Today, Yesterday, then the
date. Typing narrows the list by path, ++enter++ opens the file's own local
history at that snapshot (where ++r++ restores it), and ++ctrl+l++ reveals more
rows in a long history.

## Which one to use

| You know… | Use |
|---|---|
| The file's name | Go to file |
| The symbol's name | Go to symbol |
| You can see it on screen | Label jump (`gs`) |
| Roughly where it is in the tree | The explorer |
| That you were just there | Back, or Recent files |
| That you will return often | Pinned files, or a mark |
| Only that something is broken | Problems |
| Nothing, and want everything | Search everywhere |
