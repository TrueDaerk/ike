---
type: concept
title: Integrated Terminal
description: Roadmap 0170 — PTY-spawned shell rendered through a VT emulator as a pane; raw key routing with a ctrl+tab escape hatch; workspace integration and command polish are the follow-up issues.
resource: internal/terminal
tags: [architecture, terminal, pty, vt, pane]
timestamp: 2026-07-10T22:00:00Z
---

# Integrated Terminal (Roadmap 0170)

`internal/terminal` embeds a real shell as a pane (spec: epic #88). Landed so
far is the **PTY + VT core** (#95); workspace integration (#96), command/UX
polish (#97) and toolchain environment injection (#98) build on it.

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
  into the PTY.
- **Batching**: output notifications are coalesced (`OutputMsg`, one per 8ms
  quiet interval), so `yes` or a build log cannot flood the render loop.

## Pane citizenship

`pane.KindTerminal` joins explorer/editor in the instance registry
(`AddTerminal`, keys `terminal`, `terminal:2`, …; `Close` ends the session).
The pane renders the grid with the cursor cell reverse-videoed while focused
(`model.go` splices it ANSI-aware). `terminal.new` (palette/registry command)
splits the active editor's leaf toward the bottom — the conventional
JetBrains placement. Terminal leaves are pruned from the saved layout on
restore (their sessions died with the process); re-spawning is #96.

## Key routing

While a live terminal is focused, **every key goes raw to the PTY** — vim,
htop and less must see tab, ctrl+c, F-keys and friends — except `ctrl+tab`,
the escape hatch that moves focus away (the boundary is finalised with
#96/#97). A dead session (shell exited) falls back to normal key handling so
`ctrl+w` can close the pane.

## Quality bar

Verified inside the pane: `vim` (alt screen, insert/normal, `:wq` writes),
`less` (paging), shell line editing and wrapping, colored output, `stty size`
reflecting pane resizes.
