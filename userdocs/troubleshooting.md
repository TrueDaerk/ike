# Troubleshooting

Most surprises fall into two groups: the terminal is not delivering something,
or IKE is doing exactly what it was designed to do and you did not expect it.
This page covers both, roughly in the order people hit them.

## A keyboard shortcut does nothing

Almost always the terminal, not IKE: the emulator claims the chord and never
forwards it. Two things to check.

**Does your terminal speak the Kitty keyboard protocol?** Without it, modified
chords arrive flattened or not at all. Ghostty, kitty, WezTerm, foot,
Alacritty and iTerm2 3.5+ do.

**Has the terminal bound the chord itself?** Clear its keybindings. For
Ghostty:

```
keybind = clear
keybind = super+,=open_config
keybind = super+shift+,=reload_config
```

Use `ctrl` instead of `super` off macOS. Ghostty merges every config file it
finds, and a later `keybind = clear` wipes binds from earlier files — check
the effective result with `ghostty +show-config`.

On macOS, Option is a composition key (it produces `{ [ @ ~` on international
layouts), so ++alt+backspace++ arrives without the alt modifier. Send the
sequence explicitly, *after* `keybind = clear`:

```
keybind = alt+backspace=text:\x1b\x7f
```

[Terminal setup](getting-started/terminal-setup.md) has the full walkthrough,
including the other terminals.

### Working out what the terminal actually sends

The repository ships a probe. It lists the chords IKE cares about; press them,
then quit with ++ctrl+d++:

```sh
go run ./cmd/keyprobe
```

On exit it prints one `PROBE` line per chord — delivered or missing, including
what actually arrived when the terminal rewrote it. A chord reported missing
there never reached IKE either, so the fix belongs in the terminal
configuration, not in the keymap.

Two cases cannot be fixed there: ++ctrl+tab++ inside `tmux`, which tmux
consumes for its own tab switching, and the double-shift tap in terminals
without key-release reporting. Both have alternatives — ++ctrl++ + arrows for
pane focus, ++cmd+shift+a++ for Search everywhere — and anything else can be
rebound or reached from the palette.

## I typed and nothing appeared

You are in normal mode. The editor is modal: ++i++ starts inserting, ++esc++
goes back, and the current mode is shown at the left of the status bar. See
[The modal editor](concepts/modal-editor.md).

## I am stuck inside a terminal pane

++alt+f12++ always returns focus to the previous pane. Inside a terminal
nearly every key goes to the shell on purpose, so that `vim` and `htop` work;
[the terminal guide](guides/terminal.md) lists the small set of keys IKE
keeps for itself.

## Tabs are closing on their own

Designed behaviour. `editor.tabs.limit` (default `5`) caps document tabs per
pane and closes the least recently used one when you open a file beyond it.
Unsaved, pinned, active, scratch and terminal tabs are never evicted, and
++cmd+shift+t++ reopens what was.

Set `editor.tabs.limit = 0` to switch the cap off.

## `ike` opened the wrong project

If you started it somewhere that is not a project — no `.git`, no `.ike` — and
`project.restore_last` is on, IKE reopens your most recent project instead of
the current directory. Starting inside a project directory, or naming a file
on the command line, always wins over the history.

## A setting has no effect

**Check the spelling.** An unknown key is ignored rather than rejected, with a
notification saying so. If you missed the toast, **Notification History**
(++cmd+alt+n++) still has it.

**Check the layer.** `<project>/.ike/settings.toml` overrides your
`~/.ike/settings.toml`. A project file you forgot about beats the setting you
just changed.

**Check whether it is an array.** TOML arrays *replace* across layers instead
of merging, so a project-level `[[tools.custom]]` or `[[snippets]]` block
hides your personal ones entirely.

**Some settings apply only to files opened afterwards** rather than to what is
already open — the large-file thresholds, for instance. The
[settings reference](reference/settings.md) says so per key where it matters.

## Colours look wrong, or the mouse does nothing

Both need terminal support: truecolor for the themes, mouse reporting for
dragging pane dividers and clicking. Every terminal in the known-good list
does both, but check that neither is disabled in its configuration — and that
you are not inside an old `tmux` or `screen` stripping them.

If the colours are merely not to your taste rather than broken, there are
twenty-six themes to pick from — see
[Customising IKE](guides/customising.md).

## No highlighting or code intelligence in one file

**Is the file large?** Above `files.large_file_kb` (1 MB) or
`files.large_file_lines` (100 000 lines), IKE deliberately switches off
Tree-sitter parsing and language features for that file — the parse cost
scales with size and would make typing stutter. **Force Code Insight (Large
File)** from the palette overrides it for the file you are in.

**Is a language server installed?** Diagnostics, completion, hover and
go-to-definition all come from one. The **Language Servers** settings page
shows each server's state, and
[Code intelligence](guides/code-intelligence.md) covers the rest.

## Undo does not reach back far enough after a restart

Undo history persists per file, but only while the file has not changed on
disk in the meantime. A file touched by something else between sessions — a
formatter, a `git checkout`, another editor — comes back with an empty history
rather than an undo stack that no longer matches its contents.

## Project search is slow

Install [ripgrep](https://github.com/BurntSushi/ripgrep). IKE prefers `rg` and
falls back to a pure-Go implementation when it is missing: correct, but
noticeably slower on large trees and with simpler `.gitignore` handling.

## Where to look when something actually breaks

**`debug.log`**, in the project's `.ike/` directory (or under
`$IKE_CONFIG_DIR`), collects errors not worth interrupting you for — language
server failures, install errors, adapter problems.

**Notification History** (++cmd+alt+n++) shows what scrolled past, including
notifications suppressed by `notifications.min_severity`.

**LSP: Show Server Log** dumps what a language server has been saying.

**Profiling**, when something is slow rather than broken: `IKE_PPROF=<addr>`
serves `net/http/pprof`, and `SIGUSR1` writes goroutine and heap dumps to
`IKE_PPROF_DIR`.

## Reporting it

If it really is a bug,
[open an issue](https://github.com/TrueDaerk/ike/issues/new/choose). Two lines
save a round trip:

- **Your terminal emulator and version** — a large share of reports turn out
  to be the terminal.
- **The output of `ike --version`** — it names the exact commit, and whether
  the build came from a modified tree.
