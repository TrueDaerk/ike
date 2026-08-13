# Commands and the palette

Almost everything IKE can do is a **command** — a named action with an ID, a
title, and a scope. Saving a file is a command. Splitting a pane is a command.
So is toggling the terminal, opening settings, and every action a plugin adds.

That is not an implementation detail you can ignore, because it is what makes
the rest predictable: if something is a command, it is in the palette, it is in
the [reference](../reference/commands.md), and it can be bound to a key.

## Three views of one list

There is exactly one registry of commands. Everything you use to reach them is
a view onto it:

**The palette** ranks commands by name and context. It knows what is focused,
so the commands that apply where you are rank first.

![The palette in command mode: the query `:spl`, matching commands ranked below it, each with its chord on the right](../screenshots/features/palette-commands.png)

Read a row left to right: the prefix glyph says where the entry came from, the
title is what the palette matched, and the right-hand column is the chord that
runs the same command without the palette. All screenshots on this page use
the `monokai-pro` theme.

**The menu bar** (++f10++) groups a curated subset — File, Edit, View,
Navigate, Tools, Help — for when you want to browse rather than recall. An
entry whose command is not registered in this build simply is not shown.

![The menu bar with the File menu open](../screenshots/features/menu-bar.png)

**The keymap** binds chords to command IDs. Rebinding is editing that mapping;
see [Customising IKE](../guides/customising.md).

None of the three has its own dispatch path. The menu emits the same message
the palette does, which is the same one a keybinding produces. A command
behaves identically however you reached it — and anything reachable one way is
reachable the others.

## The palette's modes

The palette is one overlay with several modes, selected by a **prefix
character** you type into it:

| Prefix | What it searches |
|---|---|
| `:` | Commands |
| `@` | Files in the project, by fuzzy name |

The whole flow is three steps: **++shift++ twice → type `@re` → ++enter++**.

![The palette in file mode after typing `@re`: the matching project files, path and all](../screenshots/features/palette-files.png)

Some modes have their own chord and open locked into that mode — you cannot
prefix your way out of them:

| Keys | Mode |
|---|---|
| ++cmd+shift+a++, or ++shift++ twice | Search everywhere — commands *and* files at once |
| ++cmd+e++ | Recent files |
| ++cmd+shift+o++ | Go to file |
| ++cmd+o++ | Go to symbol (needs a language server) |

**Search everywhere** is the one to reach for when you do not want to think
about which. It runs a single query across commands and files, interleaves them
by match score, and marks each row with its source's prefix glyph so you can
see what you are about to open. With an empty query it lists your recent files
first, then all commands — so it doubles as "what was I just working on".

![Search everywhere with an empty query: recent files marked `%` on top, commands marked `:` below](../screenshots/features/palette-everywhere.png)

The palette itself executes nothing. It ranks, you pick, and the choice is
dispatched to whatever owns that action.

## Scopes

A command is either global or scoped to a pane type. `editor.write` is scoped
to editors; `terminal.toggle` is global.

Scope decides two things: whether the command shows up in the palette right
now, and whether a keybinding for it fires. This is why the
[keybinding reference](../reference/keybindings.md) is grouped by context — the
same chord can mean different things depending on what has focus, and the more
specific binding wins.

## Everything is discoverable

Three ways to find something you cannot remember:

- **++f1++** — the cheatsheet, showing the live bindings for your build. It
  opens on a curated **Essentials** view of the couple of dozen commands worth
  knowing first; ++tab++ switches to the complete list.

    ![The F1 cheatsheet on its Essentials view, grouped by topic](../screenshots/features/cheatsheet.png)

- **The palette** — type what you think it is called. Fuzzy matching means
  `nsf` finds "New Scratch File".
- **The [commands reference](../reference/commands.md)** — generated from this
  build's registry, so it lists every command, its ID, its default chord and
  the vim key where there is one.

A command with no chord is not hidden — it is one palette query away. That is
why IKE ships far more commands than keybindings, and why running out of
sensible chords is not a problem.

## What adds commands

The registry is assembled at startup from everything compiled in, plus:

- **Plugins**, including WASM ones, which can register commands, themes and
  languages.
- **Tool panes** — each `[[tools.custom]]` entry you configure becomes a
  `tool.<name>` command.
- **Languages** — scratch-file commands appear per registered language
  (`New Scratch File: Python`).

Which means the command reference describes *your* build. Install a plugin and
the list grows.
