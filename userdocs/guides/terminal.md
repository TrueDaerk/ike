# The integrated terminal

A real shell in a pane — a PTY, not an emulation of one. `vim`, `htop`,
`less`, `ssh` and anything else full-screen work exactly as they do outside
IKE.

| Keys | What it does |
|---|---|
| ++alt+f12++ | Toggle the terminal — and the reliable way back out |
| ++cmd+alt+t++ | New terminal |
| ++cmd+t++ | New terminal tab, in the focused terminal's pane |
| ++cmd+d++ | Split a fresh terminal to the right |
| ++cmd+w++ | Close the terminal |

A terminal can be a pane of its own or a tab inside an editor pane, so a pane
can hold a mix of files and shells.

![A terminal pane below the editor, its title bar naming the shell and the working directory, with a command and its output](../screenshots/features/terminal-pane.png)

The screenshot uses the `monokai-pro` theme. The pane's title bar names the
shell and the directory it runs in, and gains a further segment when the
project has a toolchain to activate — a virtualenv interpreter, say.

## Keys go to the shell

While a terminal has focus **every key goes to it raw**. That is the point:
++tab++, ++ctrl+c++, ++esc++ and the F-keys have to reach `vim` and `htop`
rather than being intercepted by the IDE.

A small, deliberate set of keys is reserved for IKE:

| Key | Why it is reserved |
|---|---|
| ++alt+f12++ | Return focus to the previous pane — the hatch that always works |
| ++ctrl+tab++ | Next pane (not every terminal can deliver it) |
| ++ctrl++ + arrows | Move focus out of the terminal, spatially |
| ++cmd+t++ / ++cmd+d++ / ++cmd+w++ | New tab, split right, close — iTerm-style |
| ++cmd+c++ | Copy, but only when there is a mouse selection; otherwise the shell gets it |
| ++cmd+v++ | Paste, through bracketed paste |

Note what is *not* reserved: ++ctrl+w++ stays with the shell (it deletes a
word), and so does ++esc++ ++esc++ — `vim` and `lazygit` would see side
effects.

Beyond that, a handful of IDE chords stay global even inside a terminal, so
you are never trapped: Search everywhere (including the **double-shift tap**,
which means nothing to a shell), Recent files, Go to file, Go to symbol, Find
and Replace in path, Switch project, Settings, the explorer toggle, the
pinned-file slots, the TODO index, the VCS window, notification history, and
tab next/previous.

!!! tip "If you are ever stuck in a shell"
    ++alt+f12++ gets you out. It is the one chord worth memorising here.

### Closing a busy terminal

++cmd+w++ on an idle shell sends it an EOF, it exits, the pane closes. If
something is actually running, you get a confirmation first — ++enter++ closes
anyway, ++esc++ cancels.

## Scrollback

++shift+page-up++ and ++shift+page-down++ page through the scrollback, half a screen
at a time, with a position marker on the bottom line. Any key you type snaps
back to the live view.

The exception is `/`, which opens **scrollback search** — useful for finding
that error you scrolled past. ++cmd+f++ opens the same search directly, no
scrolling first — including from the live view; ++esc++ closes it and returns
you where you were. Full-screen apps like vim keep the chord for their own
find.

## Clickable file references

Output containing `path/file.go:12` or `./pkg/x.go:3:14` — compiler errors,
test failures, `grep -n` output — is underlined, and **Cmd+click** (Ctrl+click off macOS) opens it
in the editor, at that line and column.

Relative paths resolve against the shell's current directory, not the spawn
directory, so this keeps working after you `cd`. A plain click still starts a
text selection; only cmd+click follows a link.

Extensionless files are deliberately not detected — that is the rule that
keeps `12:30` and `localhost:8080` from being treated as file references.

## Selection and the mouse

Drag to select, ++cmd+c++ to copy. Mouse events pass through to the
application in the terminal, so mouse-aware programs keep working.

## The environment IKE gives it

A fresh terminal activates the project's toolchain the way JetBrains does, so
`which python3` shows the interpreter IKE is actually using — not whatever
your login shell defaults to. For a virtual environment that means `PATH` and
`VIRTUAL_ENV` are set exactly as `source .venv/bin/activate` would, with no
shim in between.

The interpreter is the one from the Toolchain settings page if you chose one,
otherwise the one detected in the project. It is the same resolution the
language server and the debugger use, so all three agree.

`terminal.shell` overrides which shell is spawned; empty follows `$SHELL`. It
has no entry in the settings panel — set it in `settings.toml` directly.

While you type at the prompt, a completion popup offers commands, paths and
`make` targets. ++ctrl+space++ opens it on demand; set `terminal.autosuggest` to `false` to
stop it appearing by itself.

`terminal.scrollback_lines` bounds how much history each terminal keeps
(default 10000 lines). Scrollback is the main memory cost of terminal panes —
lower it if you run many terminals or keep several workspaces open in the
background. Changes apply to new sessions; lowering also trims running ones as
they produce output.

## Terminals and your session

Terminal panes are part of the layout and come back when you reopen a
project — as **fresh shells**, not restored ones. IKE does not pretend to
resurrect process state.

Sessions do survive a project switch within a running IKE.

## Tool panes

A tool pane is a TUI program pinned into the layout as a first-class pane
rather than something you start by hand — `lazygit`, `htop`, `k9s`. Configure
one and it gets a `tool.<name>` palette command with toggle-focus semantics.

```toml
[[tools.custom]]
name = "lazygit"
command = "lazygit"
```

Like every auxiliary pane (terminal, HTTP response, debug view, …), a tool
pane opens below the active editor — or to its right when that editor is
wide (over 120 cells) and wider than it is tall, so a big landscape pane is
not squashed vertically.

If `lazygit` is on your `PATH`, that entry exists already — it is the
preconfigured example, and the answer to "where is the Git workflow" (see the
Git guide, once it lands). **Set Up Tool Panes** from the palette walks you
through adding others.

Tool panes get IKE's theme colors in their environment, so a program that
reads them follows your theme. When the program exits, the pane stays open
with restart and close actions rather than vanishing.

!!! note "A project-level `[[tools.custom]]` replaces your user-level one"
    TOML arrays replace across settings layers instead of merging. If you
    define tools in a project, your personal ones are hidden while it is open.

## Related

- [Panes, tabs and layouts](../concepts/panes-and-layout.md) — where terminal
  panes and tabs fit
- [Settings reference](../reference/settings.md) — the `[terminal]` and
  `[tools]` sections
