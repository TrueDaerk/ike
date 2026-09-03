# The integrated terminal

A real shell in a pane — a PTY, not an emulation of one. `vim`, `htop`,
`less`, `ssh` and anything else full-screen work exactly as they do outside
IKE.

| Keys | What it does |
|---|---|
| ++cmd+alt+t++ | Popup terminal — a floating shell over the layout; the same chord dismisses it |
| ++alt+f12++ | Toggle the docked terminal pane — and the reliable way back out |
| ++cmd+alt+shift+t++ | New terminal pane |
| ++cmd+t++ | New terminal tab, in the focused terminal's pane |
| ++cmd+d++ | Split a fresh terminal to the right |
| ++cmd+w++ | Close the terminal |

For a quick command the **popup terminal** (++cmd+alt+t++) is the everyday
flow — see below. ++alt+f12++ toggles a docked terminal *pane* instead; note
that some terminal emulators do not deliver ++alt++ + F-keys, in which case
rebind `terminal.toggle` or reach it through the palette.

## Popup terminal

++cmd+alt+t++ drops a floating shell over the layout; the same chord hides it
again, so a check-something-and-back round trip is two keystrokes. Hiding is
not closing: the shell keeps running, and reopening reveals the same session,
tabs and scrollback. Inside it, ++cmd+t++ opens a sibling tab, ++cmd+d++
splits the box, ++cmd+w++ closes the active tab — closing the last one drops
the shell for real.

The box remembers its size and position: drag the border to resize, drag the
title bar to move, and both persist per project (with your last adjustment as
the default for projects you never touched).

Where a fresh popup shell starts is the `terminal.popup_cwd` setting
(Settings → Terminal): `project` (default) spawns it in the project root,
`file` in the focused file's directory. It applies when the shell is spawned —
the retained session keeps whatever directory you `cd`-ed to.

**Pin it** with ++cmd+alt+shift+k++ (++ctrl+alt+shift+k++ on Linux/Windows) to
keep it beside your work: the box docks to the bottom edge across the full
width, at the height you sized it to, and stays there while you edit. The popup
chord then only moves the keyboard — press it to type in the shell, press it
again to go back to the editor, without the box ever disappearing. Pressing the
pin chord again unpins and hides it.

The popup belongs to its project: switching projects parks it with everything
running inside, and coming back restores it exactly as you left it — closed if
it was closed, pinned if it was pinned. If your first move in a project is
usually opening the terminal, set `terminal.popup_on_switch`
(Settings → Terminal) to `always-open`: every project switch then leaves the
popup open, resuming the project's parked terminal when there is one and
spawning a fresh shell when there is not. The default, `restore`, keeps the
as-you-left-it behaviour.

If you would rather have **one popup terminal for everything**, set
`terminal.popup_scope` (Settings → Terminal) to `global`. That single shell —
with its scrollback and whatever is running in it — follows you from project to
project instead of parking, and whenever it sits idle at its prompt a switch
sends it a `cd` into the new project root. A shell busy with a foreground job
keeps it: the line would land in the job, not the prompt, so the terminal stays
where it is and catches up on the next switch it is idle for. The default,
`project`, is the per-project popup described above.

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

`terminal.shell` overrides which shell is spawned; empty follows `$SHELL`.
It lives on the Settings → Terminal page, next to the other `[terminal]`
options.

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
