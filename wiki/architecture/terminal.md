---
type: concept
title: Integrated Terminal
description: Roadmap 0170 — PTY-spawned shell rendered through a VT emulator as a pane; raw key routing with a documented reserved set, scrollback paging, layout restore as fresh shells, sessions surviving project switches.
resource: internal/terminal
tags: [architecture, terminal, pty, vt, pane]
timestamp: 2026-07-10T12:00:00Z
---

# Integrated Terminal (Roadmap 0170)

`internal/terminal` embeds a real shell as a pane (spec: epic #88), complete
across the epic's four slices: PTY + VT core (#95), workspace integration
(#96), commands & UX (#97) and toolchain environment injection (#98).

## Session (`session.go`)

- **PTY lifecycle** via `creack/pty`: the shell (`terminal.shell` config
  override → `$SHELL` → `/bin/sh`) spawns in the project root with
  `TERM=xterm-256color`; pane resizes propagate through `pty.Setsize`
  (SIGWINCH for the child) and the emulator; `Close` kills the child and
  releases the PTY, and a shell `exit` sends `ExitedMsg` so the root model
  closes the pane.
- **VT emulation** via `charmbracelet/x/vt` (`SafeEmulator` — the read loop
  writes while Update/View read): PTY output feeds `Write`, the screen
  renders with `Render()` (ANSI-styled, so 16/256/truecolor pass through),
  and key presses go through `SendKey`, which encodes per the emulator's
  input modes (application cursor keys etc.); a write loop pumps the
  emulator's host-bound bytes (key encodings, DA/DSR query replies) back
  into the PTY. The emulator drops non-special keys that still carry a
  modifier, so the pane normalizes text-producing presses whose only
  modifiers are shift/caps-lock/num-lock (`toVTKeys` in `model.go`) —
  uppercase letters reach the shell as their produced text (#224).
- **Batching**: output notifications are coalesced (`OutputMsg`, one per 8ms
  quiet interval), so `yes` or a build log cannot flood the render loop.

## Pane citizenship (#96)

`pane.KindTerminal` joins explorer/editor in the instance registry
(`AddTerminal`, keys `terminal`, `terminal:2`, …; `Close` ends the session).
The pane title shows **shell + origin dir** (`TERMINAL — zsh · goproj`; the
dir compacts once it differs from the working directory). The cursor cell
reverse-videos while focused (`model.go` splices it ANSI-aware).
`terminal.new` splits the active editor's leaf toward the bottom — the
conventional JetBrains placement.

**Layout persistence**: terminal leaves save with their origin dir
(`paneIdentity{Kind: "terminal", Path: dir}`) and restore as **fresh shells**
in the saved position — no process resurrection, the cwd respawns.

**Project switch (0090)**: live sessions are adopted into the freshly built
workspace (`adoptTerminals` in app/switch.go), split below the new active
editor and titled with their origin root; dead ones close for good. New
terminals root in the new project as always (spawn dir is pinned absolute).

## Key routing — the reserved set

While a live terminal is focused, **every key goes raw to the PTY** — vim,
htop and less must see tab, ctrl+c, esc and the F-keys. The documented
reserved set (`terminalReservedKey` in internal/app) is exactly:

| Key | Effect |
|---|---|
| `ctrl+tab` | move focus to the next pane (delivery is terminal-dependent — many terminals cannot send it; 0081's reality probe owns the call) |
| `alt+f12` | `terminal.toggle` — return focus to the previous pane (the reliable hatch) |

`shift+pgup` / `shift+pgdn` page the **scrollback** inside the pane (half a
grid per step, position marker on the bottom line, any typed key snaps back
to live); the mouse wheel scrolls it too. A dead session (shell exited)
falls back to normal key handling so `ctrl+w` can close the pane.

**macOS editing chords** (#225, `motionKey` in `model.go`): the pane
translates the iTerm "natural text editing" motions to the readline/ZLE
emacs-mode defaults — `option+left`/`right` → `ESC b`/`ESC f` (word jump),
`cmd+left`/`right` → `ctrl+a`/`ctrl+e` (line start/end). Shift-augmented
variants behave the same (a PTY has no selection). Cmd delivery is
terminal-dependent (the 0081 reality-probe caveat).

## Commands (#97)

- **`terminal.toggle`** (default `alt+f12`, fragile like every alt+F-key):
  the JetBrains state machine — no terminal → open one below the active
  editor; one exists unfocused → focus it (remembering where focus was);
  focused → return focus to the remembered pane (falling back to the active
  editor, then the explorer). Inside a focused terminal the reserved-set
  handler catches `alt+f12` before the raw pass-through.
- **`terminal.new`** opens an additional session; **`terminal.clear`** wipes
  screen and scrollback via the canonical `CSI 2J` + `CSI 3J` pair (2J alone
  pushes the visible lines *into* the scrollback — the xterm behaviour) and
  asks the shell to repaint its prompt with the ctrl+l convention.
- The Tools menu carries "Terminal" (toggle) and "New Terminal"; all three
  commands are palette-reachable.
- **Titles**: the shell's OSC 0/2 reports (the running command) append to
  the pane title — `TERMINAL — zsh · goproj · npm run build`.

## Toolchain environment injection (#98)

The interpreter chosen on the **settings page** — and only that; silent
detection never injects — is what `php` / `python` / `python3` resolve to
inside the IDE terminal (`internal/terminal/env.go`):

- A per-project **shim directory** (`.ike/shims`, IKE_CONFIG_DIR-overridable)
  holds `#!/bin/sh` exec scripts for `php`, `python`, `python3` (python
  covers both names). Shims exec by absolute path and are re-read per
  invocation, so regenerating them — on every config reload — retargets even
  already-running sessions. Stale shims sweep when a setting is removed.
- The **spawn environment** prepends the shim dir to PATH; a venv interpreter
  (its bin's parent carries `pyvenv.cfg`) additionally sets `VIRTUAL_ENV` and
  puts the venv bin on PATH, per convention. uv-managed interpreters resolve
  through their absolute path in the shim. With no explicit setting the
  environment stays untouched.
- The pane **title indicates the mapping**: `… · python→~/proj/.venv/bin/python`.
- The mapping reads through the same `lang.Interpreter` seam the LSP
  toolchain uses — one source of truth, `source == "config"` only.
- **Windows**: the shims are POSIX `sh` scripts; a windows port writes
  `<name>.cmd` wrappers into the same directory (`@"%target%" %*`) —
  documented here, darwin/linux land first like the rest of the PTY stack.

## Quality bar

Verified inside the pane: `vim` (alt screen, insert/normal, `:wq` writes),
`less` (paging), shell line editing and wrapping, colored output, `stty size`
reflecting pane resizes, scrollback paging over `seq` output, layout restore
with a fresh prompt, a session surviving a project switch with its
origin-root title, and `command -v python3` resolving to the shim with
`sys.executable` reporting the configured venv interpreter.
