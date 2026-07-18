---
type: concept
title: Debugger
description: Work stream 0350 — DAP debug sessions over run configurations; breakpoints hit, paused-line marker, IntelliJ stepping chords (F7/F8/F9/Shift+F8), one session at a time.
resource: internal/app/debugsession.go
tags: [architecture, debug, dap, run, breakpoints]
timestamp: 2026-07-18T00:00:00Z
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
  events clean up and toast the exit code. A `debug.stop` **during the
  launching window** (auto-install/handshake, `dbg` still nil) cancels the
  pending launch (#636): it clears `dbgLaunching`, bumps a launch generation
  counter (`dbgLaunchGen`), and toasts "launch cancelled"; the deferred
  post-install retry carries the generation it was started under and is
  dropped on mismatch, so no session starts after the install resolves.
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
  builds a command terminal (`terminal.NewCommand`, the same infra `run.file`
  uses) and **embeds it in the debug panel's Output column** (#676,
  `debugpanel.SetTerminal`) — the panel is force-opened first so the PTY has a
  host even when the program never pauses. It answers with the child's pid
  (`terminal.Model.Pid`). The debuggee connects back to the adapter on its
  own; breakpoints, stepping, frames and variables all work as usual — its
  stdio lives in the embedded terminal, where the user types input. The
  session key is minted via `Registry.MintTerminalKey` so output/exit messages
  route uniquely (an `ExitedMsg` for a non-pane key is a no-op).
- **Every bail-out path answers** (#638): once the reverse handler claims the
  request the adapter blocks on the response, so a gone session (the message
  carries its own `*dap.Session`), an empty argv, a missing panel host and a
  failed spawn all send an error refusal. A failed spawn embeds nothing — the
  Output column keeps showing DAP output. Malformed `runInTerminal` arguments
  are refused with a diagnostic in `Session.OnRunInTerminal` instead of being
  silently zeroed; `RunInTerminalArgs.Env` is `map[string]*string` because the
  spec allows JSON `null` values (= unset; the spawn path skips them). Other
  reverse requests are still refused "unsupported" (off the read loop — a
  synchronous write there can deadlock against a mid-write adapter).
- **Terminal lifetime** (#638, #676): the embedded debuggee terminal
  deliberately stays in the panel after its process exits so the output can be
  reviewed; the next session's runInTerminal replaces it (`SetTerminal` closes
  the old model). It dies with its host: closing the debug panel — by the user,
  or via a new launch's `ResetSession` — closes the embedded session
  (`CloseTerminal` in the pane registry's `Close`).
- Trade-off: with `integratedTerminal` the debuggee's output goes to the PTY,
  so the DAP `output` stream and `.ike/debug-session.log` (#624) stay empty
  for Python sessions — but the PTY now renders inside the Output column, so
  the tool window shows the live program anyway.

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
stealing focus from the paused line. When the session ends the panel **stays
open in a finished state** (#689): frames/variables clear to a
`finished (exit code N)` placeholder while the Output column — text lines or
the embedded terminal's scrollback — remains reviewable; trailing adapter
output flushed past `terminated` still appends. The user closes it like any
pane; the next launch reuses it after `ResetSession` wipes the previous
session's output and dead terminal.

The three columns are **resizable** (#691): dragging a column separator with
the mouse adjusts the proportions (`SeparatorHit`/`ResizeSeparator`, app
gesture `dragDebugDiv` mirroring the pane-divider drag), clamped to a minimum
column width. Proportions are stored as fractions, so they survive panel
resizes; they are session-local like scroll state.

- **Frames view** (left): the paused thread's stack; `j`/`k` move, `enter`
  emits `SelectFrameMsg` — the app navigates the editor to the frame's
  location and re-fetches its scopes, so the variables show the state
  *outside* the current function too.
- **Output column** (#624, live behavior #637): the debuggee's captured
  stdout/stderr, streamed from DAP `output` events. The panel renders its
  columns in **every state** — while the debuggee runs or before the first stop
  the frames column shows a placeholder (`running…` / `not paused`) but the
  OUTPUT column keeps streaming, which is exactly when output arrives; the
  first output event **opens the tool window** if it is closed (once per
  session, so a panel the user closes stays closed) — a program that never hits
  a breakpoint is still visible. stderr lines take the error tone; the column
  has its own scroll offset (`outTop`) reachable via the `tab`/`h`/`l` column
  cycle, `j`/`k` and the wheel. **Auto-follow**: the view pins to the newest
  line; a manual scroll away from the bottom holds the position (appends stop
  re-pinning), scrolling back to the bottom resumes following. Chunks are
  **sanitized before buffering** (`sanitize.go`): ANSI escapes (CSI/OSC/two-byte
  ESC) are stripped per completed line — so an escape split across chunks is
  still removed whole — a `\r` keeps only the text after it (progress-bar
  overwrites; CRLF endings survive), tabs expand to 8-column stops and other
  control bytes are dropped. Output that arrives before the panel opens is
  buffered on `debugState` (capped at 5000 chunks, oldest dropped) and flushed
  in on open. Every chunk is also appended to a per-project transcript,
  `.ike/debug-session.log` (stderr chunks prefixed `[stderr] `, ANSI stripped
  too, `\r`/`\t` kept as printed; a `──── debug session: <name> · <time> ────`
  delimiter separates sessions, and trailing output arriving after `terminated`
  still reaches the log), reusing the `debug.log` append-logger pattern. Note:
  the DAP-event rows are populated only for adapters using `internalConsole`;
  Python launches with `integratedTerminal`, so its I/O lives in the
  **embedded debuggee terminal** that renders in this very column (see below).
- **Embedded debuggee terminal** (#676, `debugpanel/terminal.go`): a
  `runInTerminal` debuggee's PTY (`terminal.Model`) embeds into the Output
  column via `SetTerminal` — while set, its grid view replaces the DAP output
  rows (they return when it detaches) and is sized to the column
  (`colWidths` × rows under the title) on every `SetSize`. **Keys**: with the
  Output column focused and the process running, the app routes keys raw to
  the panel (`debugPanelTermCapturing`, bypassing the keymap like a terminal
  pane) and the panel forwards them to the PTY; `shift+tab` is the reserved
  escape back to the variables column, and the spatial focus moves
  (ctrl+arrows) still leave the pane. After the process exits the panel's
  navigation returns (`h` leaves, `j`/`k` page the dead terminal's
  scrollback). **Mouse**: clicks/wheel/drags forward with column-local
  coordinates (`MousePress`/`MouseWheel`; drags via the app's `dragDebugTerm`
  gesture → `TermDrag`/`TermRelease`), so child mouse reporting and text
  selection work as in a terminal pane. Lifecycle: the terminal is replaced
  across sessions and closed with the panel (see "Terminal lifetime" above).
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
  previously clipped long stacks/var lists at the pane height. Hardening
  (#639): coordinates outside the pane interior (border clicks — the layout
  hit-test spans the whole pane rectangle) are rejected instead of mapping onto
  a row/column; every click — including output-column and title-row clicks —
  records into the double-click tracker, so an intervening click elsewhere
  resets a pending double-click; the wheel drags the selection along to stay
  inside the visible window (vcspanel behavior); a click while the inline value
  editor is open cancels the edit first and then selects normally, and a wheel
  while editing scrolls without moving the selection (which would re-anchor the
  editor onto a different row).
- **Editing values** (#627): `e` on a variable row opens an inline line editor
  (prefilled with the current value); typing/backspace/←→/home/end edit it,
  `enter` commits and `esc` cancels. Commit emits `SetVarMsg{Ref, Name, Value}`;
  the app calls `Session.SetVariable` (DAP `setVariable`, targeting the row's
  *containing* `variablesReference`) then refetches that reference so the panel
  shows the adapter's new value. The affordance is gated on the adapter's
  `supportsSetVariable` capability (read from the initialize response and pushed
  to the panel via `SetEditable` when it opens); scope roots aren't editable.
  While the editor is open the app routes every key to the panel
  (`debugPanelEditing`), like an editor in insert mode. Hardening (#640):
  `openDebugPanel` runs the attach step (`attachDebugPanel`: `SetEditable` gate
  + pending-output flush) even when the panel already exists — a panel restored
  from a saved layout becomes editable at the session's first stop instead of
  staying read-only; `SetScopes`/`SetChildren` cancel an open inline editor
  (an async refresh replaces the tree, and enter would commit a stale
  ref/name); `setDebugVariable` refuses with an Info notice while the debuggee
  runs, and a spontaneous `continued` event blanks the panel (`SetRunning`)
  like stepping does, so no stale rows stay editable; a refetch failure after a
  successful set surfaces as an error toast ("value set, refresh failed")
  instead of silently showing the old value; the inline editor row is windowed
  to the variables column width around the cursor, so a long value cannot
  overflow into the output column; and the esc that cancels an edit is consumed
  by the panel *before* the double-esc detector, so it never arms the esc-esc
  palette shortcut.
