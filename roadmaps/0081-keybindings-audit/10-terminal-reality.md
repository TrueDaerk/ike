# 0081/10 — Terminal Reality & Capability Probe

Ground-truth which chords a TUI actually receives. Every downstream decision
(primary default, leader fallback, "fragile" labelling) reads from the table this
doc produces. No guessing from the JetBrains layout — measure.

## Why this exists

bubbletea (and the underlying terminal) only sees what the terminal emulator
forwards as an escape sequence. Reality, roughly:

- **Reliable:** plain keys, `Ctrl+<letter>` (most), function keys `f1`–`f12`,
  `alt+<key>` (often as ESC-prefixed), `esc`, `tab`, `enter`, arrows.
- **Often intercepted by the OS / window manager:** `Cmd+*` on macOS (system
  shortcuts, app menus) — rarely reaches a terminal program at all.
- **Often intercepted by the terminal emulator:** `Ctrl+Tab` / `Ctrl+Shift+Tab`
  (tab switching), `Cmd+1..9` (tab selection), `Ctrl+<digit>` partially.
- **Undetectable in a terminal:** modifier-only chords like `shift shift`
  (double-tap Shift) — terminals send no key-up and no event for a bare modifier.
- **Terminal-dependent:** `Ctrl+Shift+<letter>`, `Cmd`/`Super` forwarding —
  varies by emulator (iTerm2, Kitty, Alacritty, WezTerm, GNOME Terminal, tmux).

## Approach

- **A debug probe mode.** Add a hidden command / flag (`ike --keyprobe` or a
  `:keyprobe` ex-command) that renders each incoming `tea.KeyMsg` as the
  `internal/keymap` `Key` it maps to (`FromKeyMsg`), plus the raw `msg.String()`.
  The user presses chords; the probe records which arrive. This makes "does this
  reach the program" answerable in any emulator without code spelunking.
- **A reachability classification.** Each default chord is tagged:
  `delivered` (arrives intact), `fragile` (arrives in some emulators, not others),
  `intercepted` (effectively never), `undetectable` (no terminal event possible).
- **A captured baseline.** Record results for the maintainer's reference set
  (macOS Terminal.app + at least one of iTorm2/Kitty/Alacritty, and tmux) into a
  table checked into this doc. Treat unknowns conservatively as `fragile`.
- **Feed the `Fragile` flag from truth.** 0080 already carries a `Fragile` bool
  per binding; this doc replaces the hand-guessed flags with probe-derived values
  and may add a finer `Reach` classification the cheatsheet can render.

## Reachability table (to be filled by the probe)

Fill `Reach` from real measurement; `Notes` records the failing emulators.

| Chord                | Expected Reach | Notes |
|----------------------|----------------|-------|
| plain letters/digits | delivered      | editor owns these in insert mode |
| `ctrl+<letter>`      | delivered      | a few collide w/ flow control (ctrl+s/q in some setups) |
| `f1`..`f12`          | delivered      | f1 also our help |
| `alt+f7`             | fragile        | alt+F-keys vary |
| `shift+f6`           | delivered      | |
| `alt+<letter>`       | delivered      | as ESC-prefixed |
| `cmd+<letter>`       | intercepted    | macOS OS/menu; rarely forwarded |
| `cmd+shift+<letter>` | intercepted    | |
| `cmd+<digit>`        | intercepted    | tab selection |
| `ctrl+tab`           | fragile        | terminal tab switch |
| `cmd+left/right-bracket` | intercepted | |
| `shift shift`        | undetectable   | no key-up / bare-modifier event |
| `esc`, `tab`, `enter`| delivered      | |

## Milestones

- [ ] Add a key probe (flag or ex-command) rendering `FromKeyMsg` + raw string per keypress.
- [ ] Run the probe across the reference emulators (macOS Terminal + one modern emulator + tmux); record results.
- [ ] Fill the reachability table above with measured `Reach` values and emulator notes.
- [ ] Extend `internal/keymap` binding metadata with a `Reach` classification (or recompute `Fragile`) sourced from the table.
- [ ] Tests: probe maps representative `tea.KeyMsg` values to the expected `Key`; classification parses/round-trips.
