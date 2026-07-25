# Troubleshooting

Failure modes worth knowing about. The full terminal walkthrough lives in
[Terminal setup](getting-started/terminal-setup.md); this page is the short
diagnostic path.

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

## Working out what the terminal actually sends

The repository ships a probe. It lists the chords IKE cares about; press them,
then quit with ++ctrl+d++:

```sh
go run ./cmd/keyprobe
```

On exit it prints one `PROBE` line per chord — delivered or missing, including
what actually arrived when the terminal rewrote it. A chord reported missing
there never reached IKE either, so the fix belongs in the terminal
configuration.

## Code intelligence is missing for a language

IKE detects language servers on your `PATH` and disables LSP per language when
one is missing — `gopls` for Go, `pyright-langserver` for Python,
`intelephense` for PHP. Install the server, then restart IKE.

## Project search feels slow

Install [ripgrep](https://github.com/BurntSushi/ripgrep). IKE prefers `rg`
and falls back to a pure-Go implementation when it is not on the `PATH`.
