---
type: concept
title: Debugger
description: Work stream 0350 — DAP debug sessions over run configurations; breakpoints hit, paused-line marker, IntelliJ stepping chords (F7/F8/F9/Shift+F8), one session at a time.
resource: internal/app/debugsession.go
tags: [architecture, debug, dap, run, breakpoints]
timestamp: 2026-07-14T00:00:00Z
---

# Debugger (0350)

Epic #572. `internal/app/debugsession.go` orchestrates one live DAP session
(#579) on top of the DAP client (`internal/dap`, #578), the run
configurations (#575/#576) and the breakpoint store (#577).

## Session lifecycle

- **`debug.start`** (shift+f9, Run menu, palette) resolves the active file's
  run configuration (`EnsureFor`, same as `run.file`) and requires the
  language to contribute a debug adapter (`lang.SupportsDebug`; Python via
  debugpy today). The adapter spawns like a language server; the handshake
  runs asynchronously: `initialize` → `launch` (answered late by design) —
  and on the adapter's `initialized` event every stored breakpoint is pushed
  (`setBreakpoints` per file, absolute paths, 1-based on the wire) before
  `configurationDone` releases the debuggee.
- **One session at a time** (MVP): starting a new session stops the old.
  `debug.stop` disconnects (terminating the debuggee); `terminated`/`exited`
  events clean up and toast the exit code.
- Session state lives in a `debugState` behind a pointer on the root model:
  thread id, paused flag, the current stack frames, and the debuggee's DAP
  `output` events (rendered by the debug tool window, #580).

## Stops and stepping

- A `stopped` event fetches the thread's stack asynchronously and lands as
  one message: the editor **jumps to the top frame** (standard open flow, so
  the file opens if needed) and the frame's line gets the **paused marker**
  — the gutter line number in the warning tone, bold + reversed, outranking
  breakpoint/diagnostic/VCS colours (`editor.SetPausedLine`).
- Stepping mirrors IntelliJ verbatim and only acts while paused: F8
  `debug.stepOver`, F7 `debug.stepInto` (the diff pane's context-scoped F7
  stays more specific and wins there), shift+F8 `debug.stepOut`, F9
  `debug.continue`. A step clears the paused state; the next `stopped` event
  re-marks wherever execution lands.
- Toggling a breakpoint during a live session pushes the file's new set to
  the adapter immediately.

## Consumers

- The debug tool window (#580) renders the frames and variables of the
  paused session and re-scopes on frame selection.
