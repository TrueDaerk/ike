---
type: concept
title: Debugger
description: Work stream 0350 — DAP debug sessions over run configurations; breakpoints hit, paused-line marker, IntelliJ stepping chords (F7/F8/F9/Shift+F8), one session at a time.
resource: internal/app/debugsession.go
tags: [architecture, debug, dap, run, breakpoints]
timestamp: 2026-07-17T09:00:00Z
---

# Debugger (0350)

Epic #572. `internal/app/debugsession.go` orchestrates one live DAP session
(#579) on top of the DAP client (`internal/dap`, #578), the run
configurations (#575/#576) and the breakpoint store (#577).

## Adapter runtime auto-install (#589)

`debug.start` preflights the adapter runtime before spawning anything
(`lang.DebugAdapterInstaller`): Python probes `interpreter -c "import
debugpy"`. A missing runtime notifies ("… installing…") and installs asynchronously,
trying four candidates in order until one succeeds: `interpreter -m pip
install debugpy` (a venv with pip), `uv pip install --python <interpreter>
debugpy` (uv-created venvs ship without pip), then the same two again with
`--break-system-packages` for an externally-managed interpreter — a
Homebrew/system python (PEP 668) or a uv-managed standalone python, where a
plain install is otherwise refused. When a project has no virtualenv the
detected interpreter is the only environment the adapter can run in, so
overriding the guard is deliberate; debugpy is a developer tool. Candidates
whose program is absent from `PATH` (e.g. uv when not installed) are skipped
rather than reported, and the surfaced error leads with the install failure's
cause. A runtime still missing after the install surfaces the manual command
instead of looping. Handshake errors carry the adapter's stderr tail, so a
dead adapter is diagnosable from the notification alone.

## Session lifecycle

- **`debug.start`** (shift+f9, Run menu, palette) resolves the active file's
  run configuration (`EnsureFor`, same as `run.file`) and requires the
  language to contribute a debug adapter (`lang.SupportsDebug`; Python via
  debugpy today). The adapter spawns like a language server, but **detached
  into its own session** (`transport.Spec.Detached` → `setsid`, #620):
  debugpy's launcher otherwise `tcsetpgrp`s the inherited controlling terminal
  to hand the debuggee terminal foreground, which steals the tty from the TUI
  and stops it with SIGTTIN. A concurrent `debug.start` while a launch is in
  flight is ignored (`dbgLaunching` guard) so a second adapter never tears down
  the first. Empty program `args` are omitted from the `launch` request — a
  JSON `null` trips debugpy's vectorizing validator (`"args"[0] must be str`).
  The handshake runs asynchronously: `initialize` → `launch` (answered late by design) —
  and on the adapter's `initialized` event every stored breakpoint is pushed
  (`setBreakpoints` per file, absolute paths, 1-based on the wire) before
  `configurationDone` releases the debuggee.
- **One session at a time** (MVP): starting a new session stops the old.
  `debug.stop` disconnects (terminating the debuggee); `terminated`/`exited`
  events clean up and toast the exit code.
- Session state lives in a `debugState` behind a pointer on the root model:
  thread id, paused flag, the current stack frames, and the debuggee's DAP
  `output` events (rendered by the debug tool window, #580).

## Interactive input — runInTerminal (#625)

Programs that read stdin (Python `input()`) need a real tty. The Python launch
config uses `console: "integratedTerminal"`, so debugpy asks the client to
launch the debuggee itself via the DAP **runInTerminal** reverse request
instead of running it under the adapter's `/dev/null` stdin.

- The client advertises `supportsRunInTerminalRequest: true`. `internal/dap`'s
  `Conn` gained a reverse-request seam: `SetReverseHandler` routes an
  adapter-initiated request to a handler (else it is politely refused, as
  before), and `Respond`/`RefuseRequest` reply on the wire. `Session` exposes
  `OnRunInTerminal(fn)` (decodes `RunInTerminalArgs`), `RespondRunInTerminal(seq,
  pid)`, and `RefuseReverse`.
- The handler runs on the read-loop goroutine and MUST hand off — it sends a
  `debugRunInTerminalMsg` onto the Update loop. There `runDebuggeeInTerminal`
  spawns the given argv in a bottom-split **command terminal pane**
  (`AddCommandTerminal`, the same infra `run.file` uses) and answers with the
  child's pid (`terminal.Model.Pid`). The debuggee connects back to the adapter
  on its own; breakpoints, stepping, frames and variables all work as usual —
  only its stdio now lives in that terminal, where the user types input.
- Trade-off: with `integratedTerminal` the debuggee's output goes to the
  terminal, so the tool window's OUTPUT column and `.ike/debug-session.log`
  (#624) stay empty for Python sessions.

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

## Debug tool window (#580)

`internal/debugpanel` + `pane.KindDebug` (singleton key `debug`, vcspanel
pattern): a bottom-split panel that opens on the first stop — without
stealing focus from the paused line — and closes when the session ends.
Session-local like the terminal tabs: it persists in the layout as an empty
slot and re-feeds on the next stop.

- **Frames view** (left): the paused thread's stack; `j`/`k` move, `enter`
  emits `SelectFrameMsg` — the app navigates the editor to the frame's
  location and re-fetches its scopes, so the variables show the state
  *outside* the current function too.
- **Output column** (#624): the debuggee's captured stdout/stderr, streamed
  from DAP `output` events. stderr lines take the error tone; the column has its
  own scroll offset (`outTop`, pinned to the newest line) reachable via the
  `tab`/`h`/`l` column cycle and the wheel. Output that arrives before the panel
  opens (a program printing before the first stop) is buffered on `debugState`
  and flushed in on open, so nothing is lost. Every chunk is also appended
  verbatim to a per-project transcript, `.ike/debug-session.log` (stderr chunks
  prefixed `[stderr] `), reusing the `debug.log` append-logger pattern. Note:
  this column is populated only for adapters using `internalConsole`; Python now
  launches with `integratedTerminal` (see below), so its I/O lives in the
  debuggee terminal instead.
- **Variables tree** (middle, `tab`/`h`/`l` switch columns): roots are the
  selected frame's scopes (Locals expands eagerly); `enter` expands/collapses
  a node — unloaded references emit `ExpandVarMsg` and the app answers with
  the adapter's `variables` response (`SetChildren`), loaded ones toggle
  locally.
- The panel is pure view/state: data arrives via `SetFrames`/`SetScopes`/
  `SetChildren`/`SetRunning`; the app resolves intents against the live
  session (`fetchScopes`/`fetchVariables`).
- **Mouse** (#626, `mouse.go`, vcspanel pattern): the app routes wheel and
  left-click over `KindDebug` to the panel. A click focuses the column under
  the cursor (x against the separator) and selects the row; a **double-click**
  (same row within 400ms) activates it, mirroring `enter`. The wheel scrolls
  the focused column. Both columns carry a scroll offset (`frameTop`/`varTop`),
  and keyboard `j`/`k` auto-scroll to keep the selection visible — the panel
  previously clipped long stacks/var lists at the pane height.
- **Editing values** (#627): `e` on a variable row opens an inline line editor
  (prefilled with the current value); typing/backspace/←→/home/end edit it,
  `enter` commits and `esc` cancels. Commit emits `SetVarMsg{Ref, Name, Value}`;
  the app calls `Session.SetVariable` (DAP `setVariable`, targeting the row's
  *containing* `variablesReference`) then refetches that reference so the panel
  shows the adapter's new value. The affordance is gated on the adapter's
  `supportsSetVariable` capability (read from the initialize response and pushed
  to the panel via `SetEditable` when it opens); scope roots aren't editable.
  While the editor is open the app routes every key to the panel
  (`debugPanelEditing`), like an editor in insert mode.
