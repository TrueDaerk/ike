# Your first project

## Opening a directory

IKE has no "open project" dialog at startup. The directory you run it in
*is* the project:

```sh
cd ~/src/my-project
ike
```

That root is what the file tree shows, what project-wide search covers, and
what per-project settings and session state attach to.

!!! note "Running `ike` outside a project"
    With `project.restore_last` turned on — it is off by default — running
    `ike` in a directory that is *not* a project — no `.git`, no `.ike` — reopens your most recent project
    instead of the current directory. Inside a project directory that never
    happens: an explicit checkout always wins over the history. Passing a file
    on the command line also counts as explicit.

To switch projects later without leaving the IDE, use **Switch project**
(++cmd+shift+p++), which offers your recent-projects history. Switching saves
your edits first — every modified file writes itself, format-on-save and all,
before the other project opens. Buffers that have nowhere to go (an untitled
buffer, a read-only one, or a file changed on disk behind your back) are
collected into a single dialog where you can name them, switch anyway, or
cancel. Turn the whole behaviour off with `project.auto_save_on_switch` if you
prefer your unsaved edits to just stay put.

## The welcome tour

The first time IKE starts it opens a five-page welcome tour covering the keys
that open everything, the modal editor, panes, and the tool windows. Some
pages have a "try it" task that ticks itself when you actually press the key.

Skip it if you like — ++esc++ closes it — and reopen it any time with
**Welcome Tour** from the palette.

## Finding your way around

The window is made of **panes**: the file tree, editor panes with tabs, and
tool windows along the edges. Any pane can be split, moved and resized, and
the arrangement is remembered per project.

![The IKE window after opening a project: the menu bar on top, the file tree on the left, an editor pane on the right, the status line at the bottom](../screenshots/features/window-overview.png)

Four things to locate in that window, because the rest of this documentation
names them:

| In the shot | What it is |
|---|---|
| The top row, `File Edit View …` | The **menu bar** — ++f10++ opens it |
| `EXPLORER` on the left | The **file tree** — ++cmd+1++ focuses it |
| The bordered region on the right | An **editor pane**; its top line is the **tab bar** |
| The bottom row, `NORMAL  main.go …` | The **status line** — mode, file, encoding, branch, cursor position |

The highlighted border marks the **focused** pane. Focus decides which
keybindings are live and where a newly opened file lands.

Screenshots in this documentation use the `monokai-pro` theme; yours will
follow whatever theme you pick.

| Keys | What it does |
|---|---|
| ++cmd+1++ | Toggle the file tree / move focus to it |
| ++ctrl+tab++ | Switch pane focus (++ctrl++ + arrows also works) |
| ++cmd+k++ then an arrow | Split the focused pane in that direction |
| ++cmd+k++ then ++z++ | Maximize the focused pane, and back |
| ++f10++ | Open the menu bar |

Panes also respond to the mouse: drag a divider to resize, drag a pane's title
bar to move it somewhere else.

## Opening files

| Keys | What it does |
|---|---|
| ++cmd+shift+o++ | Go to file — fuzzy-match by name |
| ++cmd+e++ | Recent files |
| ++shift++ twice | Search everywhere: type `@` for files, `:` for commands |
| ++cmd+o++ | Go to symbol — needs a language server |

Each of those opens the same overlay in a different mode. Tapping ++shift++
twice and typing nothing lists what you had open last:

![Search everywhere with an empty query, listing the recently opened files first](../screenshots/features/palette-everywhere.png)

Files open as tabs in the focused editor pane. The same buffer can be visible
in several panes at once; edits show up in all of them.

## Editing

The editor is modal, like vim. If you type and nothing appears, you are in
normal mode — press ++i++ to insert, ++esc++ to return. The mode is shown at
the left of the status bar, and the cursor takes the mode's colour:

![The status line in insert mode, showing a green INSERT badge at the far left](../screenshots/features/mode-insert.png)

The chords below all assume an **editor pane has focus** — with the file tree
focused, the same keys do explorer things.

| Keys | What it does |
|---|---|
| ++cmd+s++ (or `:w`) | Save |
| ++ctrl+z++ (or ++u++) | Undo |
| ++cmd+f++ (or ++slash++) | Find in the current file |
| ++cmd+7++ | Toggle line comment |
| ++cmd+shift+f++ | Find in the whole project |

Motions, operators, text objects, registers and ex-commands all work the way
vim taught you. The [commands reference](../reference/commands.md) lists the
vim key next to each command where one exists.

## Settings

++cmd+comma++ opens the settings panel — a searchable list of every option, with
its description, written back to `~/.ike/settings.toml` as you change it.
Changes apply live; nothing needs a restart.

![The settings panel: pages on the left, the selected page's keys in the middle, the highlighted key's description on the right](../screenshots/features/settings-panel.png)

You can equally edit the file by hand:

```toml
[theme]
name = "tokyo-night"

[editor]
relative_line_numbers = true
```

Per-project overrides go in `<project>/.ike/settings.toml` and win over your
personal defaults. The [settings reference](../reference/settings.md) lists
every key.

## Quitting

++q++ in the file tree, or in an editor while you are not typing, quits. So
does ++ctrl+c++. Unsaved changes always prompt first — and if IKE ever dies
without asking, your dirty buffers were snapshotted vim-swapfile style and are
offered back on the next start.

## Where to go next

- [Command line](command-line.md) — open files at a line, in tabs, from a pipe
- [Concepts](../concepts/index.md) — how panes, the modal editor and projects
  fit together
- [Keybindings](../reference/keybindings.md) — the complete table
