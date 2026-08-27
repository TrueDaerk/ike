---
type: concept
title: Debugger
description: Work streams 0350/0360 — DAP debug sessions over run configurations; breakpoints hit (with conditions, hit counts and logpoints), paused-line marker, IntelliJ stepping chords (F7/F8/F9/Shift+F8), one session at a time; a combined debug area (frames/variables panel + debuggee console behind internal tabs) with per-project watch expressions, an evaluate popup (Alt+F8) and inline variable values in the editor; Python via debugpy, Go via delve (dlv dap over a socket), PHP via the in-process Xdebug/DBGp bridge.
resource: internal/app/debugsession.go
tags: [architecture, debug, dap, dbgp, xdebug, delve, run, breakpoints, watches, evaluate]
timestamp: 2026-08-27T00:00:00Z
---

# Debugger (0350)

Epic #572. `internal/app/debugsession.go` orchestrates one live DAP session
(#579) on top of the DAP client (`internal/dap`, #578), the run
configurations (#575/#576) and the breakpoint store (#577).

## Go: delve over a socket (#1914)

`dlv dap` speaks DAP only over a socket — there is no stdio mode — so the Go
plugin rides the **in-process connect seam** (`lang.DebugAdapterInProcess`,
the PHP bridge's path) instead of the argv spawn:
`plugins/languages/go/debug.go`'s `DebugAdapterConnect` spawns
`dlv dap --listen=127.0.0.1:0` (cwd = project root), scans stdout for the
`DAP server listening at:` banner (10 s bound, stderr tail in the error),
dials the port and returns a connection whose `Close` also kills and reaps
the dlv process. Past construction the session is one code path with every
other adapter.

- **Resolution beyond PATH**: dlv is located with `transport.Resolve` —
  `go install` drops it into `GOBIN`/`GOPATH/bin`, which a GUI-launched IKE
  typically misses. The #589 installer seam preflights it:
  `DebugAdapterMissing` = unresolvable, candidates
  `go install github.com/go-delve/delve/cmd/dlv@latest` then
  `brew install delve`.
- **Launch args**: mode `debug` with the file as program; a test-scope
  configuration (#1150) launches mode `test` with the file's package
  directory as program and the selection as `-test.run '^X$'`
  (benchmarks `-test.bench` + `-test.run '^$'`) — that is
  **debug.testAtCursor** (Run menu / palette): run.testAtCursor's selection
  rules, upserting the same test configuration, launched through
  `startDebugConfig`. `lang.RunSpec` carries `Tests`/`TestName`/`TestKind`
  for this. `console: "integratedTerminal"` puts the debuggee into the
  debug area's console view via runInTerminal (#625/#1370/#2190), so
  interactive and TUI programs get a real tty.
- A real-delve end-to-end test (`debug_e2e_test.go`) exercises conditional
  breakpoints, logpoints, evaluate and stepping; it self-skips without dlv.

## Breakpoint refinements: conditions, hit counts, logpoints (#1914)

The store (#577) attaches an optional `debug.Meta{Condition, HitCondition,
LogMessage}` per breakpoint (`SetMeta`/`MetaAt`; `EnabledSpecs` is what
adapters receive). Persistence adds a backward-compatible `"meta"` field to
`.ike/breakpoints.json`; `AdjustEdit` shifts refinements with their line like
the disabled flag, and removing a breakpoint drops them.

In the Breakpoints tool window `c`, `n` and `l` open a one-line editor
prefilled with the current value; it shares the same input helpers as the
variables editor (`ui.EditKey`, #2002), so word motions, the `opt`/`cmd`
kills and `cmd+v` all work there too.

On the wire, `dap.SourceBreakpoint` carries `condition`/`hitCondition`/
`logMessage`, and **`Session.SetBreakpoints` strips fields the adapter did
not advertise** (`supportsConditionalBreakpoints`,
`supportsHitConditionalBreakpoints`, `supportsLogPoints` from the initialize
response) — the breakpoint itself is always sent, so an unsupported
refinement degrades to a plain stop instead of a silently missing breakpoint.
The DBGp bridge advertises none of the three: PHP breakpoints stop
unconditionally; delve and debugpy support all three.

The Breakpoints window (#1377) edits and shows them: `c` condition · `n` hit
count · `l` log message open an inline one-line editor on the row (enter
applies — an emptied field clears — esc cancels; the commit is a
`SetMetaMsg` the root model applies, persists and syncs to a live session
like every other mutation). Rows render `if …` / `hit …` / `log "…"`
suffixes; a logpoint's glyph is `◆` in the warning tone (`◇` disabled) — it
logs instead of stopping.

## Watch expressions (#1914, persisted #2174)

`dap.Session.Evaluate(expr, frameID, "watch")` is the request; the DBGp
bridge answers `evaluate` through DBGp `eval` (flat results — eval properties
carry no stable fullname to page children through), so PHP watches work too.

The expression list is a **per-project store** (`debug.Watches`,
`internal/debug/watches.go` — `.ike/watches.json`, `IKE_CONFIG_DIR` override
like the breakpoint store): loaded at start, saved after every mutation
(`saveWatches`, a warning toast on failure like `saveBreakpoints`), so
watches survive session restarts *and* IDE restarts. A missing or malformed
file loads empty, blank expressions are dropped on the way in, and an
out-of-range mutation is a no-op — the panel and the store never have to
agree on indices to stay safe. On every stop, and on frame selection, the app
re-evaluates all of them against the current frame (`evaluateWatches` next to
`fetchScopes`; results ride one `debugWatchesMsg`, session-guarded per #1523)
and pushes `debugpanel.SetWatches`. A failed expression carries its error in
place of a value — one bad watch never hides the others, and never breaks the
session.

The panel renders a **Watches** section leading the variables tree (a
synthetic root; `SetScopes` does not clobber it): `a` adds (an inline editor
on a placeholder row — allowed while running, evaluated on the next stop),
`e` on a watch row edits the expression (`e` on a variable row still edits
its value), `d` removes, and enter on a structured result expands it through
the ordinary `ExpandVarMsg`/`SetChildren` round trip. Committing an emptied
expression removes the watch.

## Evaluate: the capability gate (#2174)

DAP has **no initialize capability for `evaluate`** — the spec requires every
adapter to implement it — so `Session.SupportsEvaluate()` starts optimistic
and is discovered at first use: an adapter answering with an
"unsupported/unimplemented/unknown command" message latches the verdict off
(`unsupportedRequest` classifies the message; a *bad expression* error is
explicitly not a capability signal, or one mistyped watch would disable
watches), and every later `Evaluate` returns `dap.ErrEvaluateUnsupported`
without touching the wire.

What the latch changes:

- `refreshWatches` stops evaluating and pushes the bare expression list; the
  panel's Watches header reads `evaluate unsupported`
  (`SetEvaluateSupported`, mirrored from the session in `attachDebugPanel`
  beside the `setVariable` gate). The expressions stay listed and editable —
  the list is per project, not per adapter.
- `debug.evaluate` refuses before opening its prompt, so an expression is
  never typed for nothing.
- Either path notifies **once per session** (`debugState.evalNoticed`), so a
  stop with five watches is one notification, not five.

`supportsEvaluateForHovers` *is* a real initialize flag and is decoded: the
evaluate popup sends the `hover` context for a selection when the adapter
advertises it, and falls back to `repl` — the context every adapter
understands — otherwise.

## Evaluate popup (#2174)

`debug.evaluate` (**Alt+F8**, JetBrains' Evaluate Expression; palette and
Run menu too) evaluates in the frame the debugger is paused in:

- **The selection wins**: a visual selection is flattened to one line
  (`strings.Fields` join, so a wrapped call still evaluates) and sent
  straight away. With nothing selected, a single-field shell prompt asks for
  an expression (`ui.EditKey`/`ui.PasteText`, enter/esc, like the curl and
  OpenAPI import prompts).
- **Preflight** (`evalReady`): no session, a running debuggee and a latched
  capability each explain themselves instead of failing silently.
- **The popup** (`internal/editor/evalpopup.go`) is a cursor-anchored box in
  the same frame as hover/peek (#316), composited first in
  `compositeLSPPopups` — it is explicitly invoked and keyboard-driven, so it
  outranks the transient popups. It owns `j`/`k`/arrows (move),
  `enter`/`space`/`l`/`right` (expand or fold), `h`/`left` (fold, or step to
  the parent) and `esc`; **any other key dismisses it and falls through**,
  the hover/peek precedent, so a popup key never reaches the buffer and a
  normal key is never swallowed.
- **Structured results page in**: a non-zero `variablesReference` starts
  collapsed (expanding costs a request), and expanding emits
  `editor.EvalExpandMsg{Ref}` → `fetchEvalChildren` → DAP `variables` →
  `debugEvalVarsMsg` → `SetEvalChildren`. Deliberately *not* the panel's
  `debugVarsMsg`: the two trees expand independently and a shared message
  would cross them. Loaded children never re-request; a failed expansion
  leaves the row collapsed rather than broken.
- The editor stays protocol-free: rows cross as `editor.EvalVar` values, the
  same seam `editor.DebugLocal` uses for inline values.
- **The result never outlives its frame**: `clearPausedMarker` closes the
  popup in every editor view (continue, step, stop, session end), the rule
  the paused marker and the inline values already follow.

## Inline variable values (#1914)

While stopped, the selected frame's Locals render as line-end annotations in
that frame's file — `x = 42, y = 7` after the code, blame-style (` ▏ `
prefix, InlayHint tone, italic + faint, only when the line has room; code is
never truncated). `editor.SetDebugLocals` computes a line → text map in one
O(lines) scan (whole-word identifier matching, recomputed at most once per
document version — the testmarks discipline) and the annotation branch sits
in the view chain after log spans.

The push rides the existing Locals fetch: `fetchScopes` additionally sends
`debugLocalsMsg{sess, path, vars}`, and the app fans it out to every editor
view of the file — following the selected frame, so activating another frame
moves the values there. `clearPausedMarker` clears them (continue, steps,
stop, session end): inline values describe a paused state and never outlive
it. Gated by `debug.inline_values` (Settings › Debug, default on).

## PHP: in-process Xdebug/DBGp bridge (0360, epic #697)

PHP needs no external adapter (and no Node): IKE speaks **DBGp**, Xdebug's
native protocol, itself. `internal/dbgp` is the protocol client (NUL-delimited
XML packets over TCP, transaction-id correlated commands, latin-1-tolerant XML
decoding — real Xdebug declares `iso-8859-1`, #705). `internal/dbgp/bridge` is
an **in-process DAP adapter**: the debug manager talks DAP to it over a
`net.Pipe` (seam: `lang.DebugAdapterInProcess` → `dap.Connect`; preferred over
argv adapters, single code path past session construction), and the bridge
translates:

- **launch** opens an ephemeral loopback listener and asks the client to spawn
  `php -dxdebug.mode=debug -dxdebug.start_with_request=yes
  -dxdebug.client_host=127.0.0.1 -dxdebug.client_port=<port> script.php` via
  runInTerminal — DBGp's direction is reversed (the engine dials the IDE); the
  per-run `-d` overrides leave the user's php.ini untouched. One connection is
  accepted, then the listener closes (CLI debugging only; web/request debugging
  is out of scope for 0360).
- Breakpoints map to `breakpoint_remove`/`breakpoint_set`, stepping to
  `step_into`/`step_over`/`step_out`/`run`, the stack to `stack_get` (frame id =
  DBGp depth + 1); a `status="break"` continuation response becomes a DAP
  `stopped` event, end-of-run becomes `terminated`. One synthetic thread.
- **Variables**: scopes ↔ `context_names` (non-Locals marked expensive),
  variables ↔ `context_get`/paged fullname-based `property_get` (≤1000 children),
  `setVariable` ↔ `property_set` + echo. `variablesReference`s live per paused
  state and die on resume (stale refs answer empty). Strings render quoted with
  a `…` clip marker when `max_data` truncated them; arrays as `array(N)`,
  objects as their class name.
- Bridge goroutines are recover-guarded: a bridge bug fails the session, never
  the app. A real-Xdebug end-to-end test (`e2e_real_test.go`) runs when
  php+Xdebug are present and self-skips otherwise.

**Xdebug preflight** rides the #589 installer seam: missing = `xdebug` absent
from `php -m` of the resolved interpreter; candidates are `pecl install xdebug`
then `brew install shivammathur/extensions/xdebug@<major.minor>` (version from
`php -r`). Note Homebrew's tap-trust gate: the brew candidate needs
`brew tap shivammathur/php && brew trust shivammathur/php` (and
`…/extensions`) once — until then the auto-install surfaces the brew command as
the manual instruction.

### Web/request debugging — listen mode (#823)

`debug.listen` (palette / Run menu) toggles JetBrains-style **"listen for
PHP debug connections"**: instead of spawning a process, the bridge opens a
persistent DBGp listener (launch args `mode: "listen"`) and every request
served through php-fpm/Apache with Xdebug triggered attaches as a debug
session — breakpoints, stepping, stack and variables through the same
bridge. Sequential model: one session per accepted connection; a request
arriving while another is being debugged is **detached** (runs through
undisturbed), and when a request finishes the bridge emits `continued`,
drops the connection and keeps listening. Stopping the listener detaches a
live request instead of killing it mid-response.

**Vetting is concurrent, adoption is not** (#1328). php-fpm opens several DBGp
connections for one page load (subrequests, assets), and both the handshake and
the hostname probe are round trips — running them inside the accept loop meant
every pending request waited behind the slowest one, and a connection that
never completed its handshake stalled the listener for the whole 30s accept
timeout. Each connection is now vetted in its own goroutine; `adoptConn`
re-checks the session under the lock, so exactly one of several racing
connections becomes the session and the losers are turned away like any other
drop.

**No silent drop path remains** (#1328). Every rejection goes through
`dropConn`, which logs a console line *and* emits `ike.debugDrop`
`{reason, detail, count}`; the reasons are `handshake` (no DBGp init in time —
previously the one path that left no evidence at all), `busy` (another request
is being debugged), `filter` (hostname mismatch, keeping its more specific
`ike.filterDetach` event) and `ended`. The client notifies on the first drop of
each reason and then every tenth, since one page load can produce a burst; the
debug console keeps the complete list.

**Dead sessions are reaped** (#1328). While the debuggee is paused no command
is outstanding, so a php-fpm worker that dies (browser abort, request timeout)
went unnoticed: `b.dc` stayed set and the bridge answered *every* later request
with "one session at a time" until the user pressed continue — the intermittent
capture the issue reported. Before deciding a connection is a latecomer the
bridge now checks `Conn.Closed()` on the live session and, if it is gone, ends
the run like a normal request end.

Settings live in `[debug.php]`:

- `port` — DBGp listener port (default 9003, Xdebug's default).
- `hostname` — only accept sessions whose request's `$_SERVER['HTTP_HOST']`
  matches (case-insensitive, `:port` suffix ignored); everything else is
  detached so another vhost on the same fpm pool cannot hijack the
  debugger. The DBGp `init` packet carries no HTTP host, so the bridge
  steps onto the first statement (`step_into`) and `eval`s the superglobal
  before deciding — `property_get` cannot see it (#938): without `-c` it
  searches context 0 (Locals) while superglobals live in context 1, and
  PHP's default `auto_globals_jit=On` leaves `$_SERVER` uninitialized until
  user code references it, so the probe silently rejected every request. A
  connection without `HTTP_HOST` (CLI) is treated as non-matching while a
  filter is set. Every filter detach raises an `ike.filterDetach` event and
  the app shows a warning notification naming the rejected host and the
  filter — never a silent drop.
- `[[debug.php.path_mappings]]` — `server`/`local` prefix pairs for
  docroot ≠ project layout; `local` may be project-relative. Breakpoints
  translate local→server on replay, stack frames server→local, longest
  prefix wins.

Breakpoints set while no request is attached are cached (`bpLines`) and
verified optimistically; each accepted connection gets them replayed before
the initial `run`. The Xdebug CLI preflight is skipped for listen — the
engine lives in the server's PHP, not the CLI interpreter.

Listener state is visible throughout: the debug console logs
"Listening for Xdebug connections on port N (host filter: …)…",
"Accepted debug connection (path)", "Dropped debug connection: …" (one line per
turned-away connection, with the reason), "Debugged request ended without a
reply — listening…" (a reaped dead session) and "Request finished —
listening…" as they happen (the first output auto-opens the panel).

### Xdebug Doctor (#1991)

`debug.doctor` (palette / Run menu) toggles the **Xdebug Doctor** tool
window (`internal/debugdoctor`, pane key `xdoctor`), which mechanizes the
troubleshooting list below: the listener state (running/stopped, bound port,
hostname filter, path-mapping count) plus a live, newest-first trace of every
connection attempt with its outcome. Observability only — accept/reject
decisions stay in the bridge; the doctor just surfaces them.

The bridge emits two structured events alongside the existing console lines:

- `ike.listenState` `{state, port, hostname, mappings}` on listener
  start (with the actually bound port) and, best-effort, on shutdown.
- `ike.debugConn` `{outcome, reason, detail, remote, ideKey, fileURI, host,
  local, mapped}` — one per attempt. Accepted entries carry the locally
  mapped entry file and `mapped=false` when it does not resolve (the #832
  hint's diagnosis, rendered as a warning row). Rejected entries carry the
  reason (`handshake`, `init`, `busy`, `filter`, `ended`) plus the identity
  needed to fix it: source address, the init packet's IDE key and file URI,
  and the probed `HTTP_HOST` for filter rejects.

`init` is a new *distinguished* handshake failure (still a rejection as
before): `dbgp.Conn` records a pre-init packet that fails to parse (or a
well-formed non-init first packet) and `WaitInit` fails fast with
`ErrBadInit` instead of running out the 30s timeout, so "malformed init
packet: …" points at the peer while `handshake` keeps meaning "no init at
all".

The app owns the trace (`Model.doctorLog`, a 200-entry ring in
`internal/debugdoctor.Log`): it is fed in `handleDebugEvent` *before* the
session-tagging guard, so attempts from a parked workspace's listen session
are recorded too, and it survives the panel being closed. The listener is
additionally marked stopped when the listen session ends client-side
(`doctorSessionEnded`), covering a disconnect that closes the pipe before
the bridge's stopped event is read. `c` clears the trace; the panel works
with or without an active debug session.

Troubleshooting a request that never stops at breakpoints (the doctor shows
1, 3 and 4 directly):

1. Listener bound? `lsof -iTCP:9003 -sTCP:LISTEN` while listening — the
   bind happens at `debug.listen` start and a failure fails the launch
   loudly, so nothing listed means the listener was never started.
2. Xdebug connecting? `xdebug.log` on the PHP side shows attempts and
   refusals; php-fpm needs `xdebug.mode=debug` plus a start trigger
   (`start_with_request` or the `XDEBUG_TRIGGER` cookie/param) to dial out
   at all.
3. Hostname filter rejecting? Watch for the warning notification / the
   "Detached request from …" console line — it names the host the request
   actually sent, so a filter typo or an unexpected `HTTP_HOST` is visible
   immediately. An empty host means the request never sent one (CLI).
4. Requests dropped rather than ignored? Since #1328 every connection the
   listener turns away says so — a console line plus a throttled warning
   notification with the reason. `busy` in a burst means requests arrive while
   an earlier one is paused (continue or stop the session first); `handshake`
   means something dialed the port without speaking DBGp; `filter` is the
   hostname check. Nothing on screen while `xdebug.log` shows a connect
   attempt is now a bug, not a configuration question.
5. Breakpoints in files the request never executes: check the path
   mappings — breakpoint paths translate local→server on replay, and a
   mapping miss binds them to files the server doesn't run. The
   `ike.pathMappingHint` prompt fires when the entry file doesn't resolve
   locally.

The settings are editable in-IDE (#832): the settings panel's **Debug**
section carries `debug.php.port` (user scope) and `debug.php.hostname`
(project scope) as schema entries, and the **PHP Debug Mappings** custom
page manages the `[[debug.php.path_mappings]]` list (a add · enter edit ·
d delete, project scope — the `[[tools.custom]]` editor pattern). Mapping
changes apply on the next `debug.listen` start. When a listening session
accepts a request whose `init` fileuri does not resolve to an existing
local file (after mappings), the bridge raises an `ike.pathMappingHint`
event and the app offers a one-key prompt: `m` writes a mapping from the
server file's directory to the project root (project scope), `esc`
ignores; the request keeps running either way — breakpoints just cannot
bind until a mapping exists.

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
  and on the adapter's `initialized` event every stored **enabled** breakpoint
  is pushed (`setBreakpoints` per file, absolute paths, 1-based on the wire;
  disabled breakpoints (#1377) stay out of the adapter) before
  `configurationDone` releases the debuggee.
- **One session at a time** (MVP): starting a new session stops the old.
  `debug.stop` disconnects (terminating the debuggee); `terminated`/`exited`
  events clean up and toast the exit code. A `debug.stop` **during the
  launching window** (auto-install/handshake, `dbg` still nil) cancels the
  pending launch (#636): it clears `dbgLaunching`, bumps a launch generation
  counter (`dbgLaunchGen`), and toasts "launch cancelled"; the deferred
  post-install retry carries the generation it was started under and is
  dropped on mismatch, so no session starts after the install resolves.
  Stop teardown is fully non-blocking (#1375): the pipe terminal's
  `FinishPipe` repaint send is asynchronous (a synchronous `Program.Send`
  from the Update goroutine self-deadlocks — the event loop is the only
  receiver and is busy executing Update), and the PHP bridge's polite
  `detach`/`stop` DBGp round trips during shutdown are bounded
  (`teardownTimeout`, 5s): an engine that never answers — DBGp processes no
  commands mid-run — gets its connection force-closed, which releases every
  pending call.
- Session state lives in a `debugState` behind a pointer on the root model:
  thread id, paused flag, the current stack frames, and the debuggee's DAP
  `output` events (rendered by the debug area's console, #1370/#2190).
- **Session end** is configurable (`debug.session_end`, #2190): `keep` (the
  default) leaves the area open in the finished state for review; `close`
  removes it from the layout — `applyDebugSessionEnd` on the active
  workspace, `applyDebugSessionEndAt` for a parked one (#1544).

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
  uses) and **installs it as the debug area's console** (#1370/#2190,
  `debugpanel.SetTerm`, taking over the pipe placeholder; the console view
  surfaces via `AutoTab` so the tty is visible for input) — the area is
  force-opened first so the PTY has a host even when the program never
  pauses. It answers with the child's pid (`terminal.Model.Pid`). The
  debuggee connects back to the adapter on its own; breakpoints, stepping,
  frames and variables all work as usual — its stdio lives in the console
  view, where the user types input. The session key is minted via
  `Registry.MintTerminalKey` so output/exit messages route uniquely (the
  debuggee is a command session, so its `ExitedMsg` never closes the pane).
- **Every bail-out path answers** (#638): once the reverse handler claims the
  request the adapter blocks on the response, so a gone session (the message
  carries its own `*dap.Session`), an empty argv, a missing panel host and a
  failed spawn all send an error refusal. A failed spawn installs nothing —
  the pipe-fed console keeps showing DAP output. Malformed `runInTerminal` arguments
  are refused with a diagnostic in `Session.OnRunInTerminal` instead of being
  silently zeroed; `RunInTerminalArgs.Env` is `map[string]*string` because the
  spec allows JSON `null` values (= unset; the spawn path skips them). Other
  reverse requests are still refused "unsupported" (off the read loop — a
  synchronous write there can deadlock against a mid-write adapter).
- **Terminal lifetime** (#638, #1370, #2190): the console deliberately stays
  in the area after its process exits so the output can be reviewed
  (`debug.session_end = keep`, the default); the next session reuses it
  (`SetTerm` closes the old session). Closing the debug pane — guarded by the
  busy check while the debuggee runs, since the console is the pane's
  `ActiveTerminal` — ends the session like any terminal pane. A **pipe-backed
  console** (#1370) keeps `Session.Running()` true past `FinishPipe` — the
  emulator stays feedable for trailing output — so the app's focus predicates
  read `terminal.Model.Exited()` instead (#2192); without that the finished
  area counted as a live terminal and swallowed its own close chord. The pane
  title carries the `✗ exited (code N)` marker once the debuggee ended.
- Trade-off: with `integratedTerminal` the debuggee's output goes to the PTY,
  so the DAP `output` stream and `.ike/debug-session.log` (#624) stay empty
  for Python sessions — but the PTY renders in the console view, so the area
  shows the live program anyway.

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
- Toggling a breakpoint during a live session pushes the file's new enabled
  set to the adapter immediately (`syncSessionBreakpoints`); list-side
  enable/disable/delete actions (#1377) sync the same way.

## Breakpoints list (#1377)

`internal/breakpanel` + `pane.KindBreakpoints` (singleton key `breakpoints`,
Problems-panel pattern) is the Breakpoints tool window — JetBrains'
cmd+shift+F8 dialog as an adaptive split of the active editor (`auxZone`,
#1588), reachable via `debug.breakpoints`
(palette, Run menu, cmd+shift+f8). It lists every breakpoint grouped by file
with a source-line preview and is a **pure consumer** of the store: enter (or
double-click) jumps through the standard open funnel, space (or a click on
the glyph cell) toggles enable/disable, `y` (or `cmd+c`/`super+c`) copies the
marked row — `path:line` plus preview and refinements, a header its path,
via `breakpanel.CopyMsg` and the shared `copyToClipboard` seam (#2071; `c` is
the condition editor here, so only the chord aliases `y`) — `d` deletes,
`D` deletes all — each
action is a message the root model handles (`handleBreakpanelMsg`), which
mutates the store, saves, refreshes the panel and syncs a live session, so
gutter, list, persistence and adapter never disagree. Gutter toggles call
`refreshBreakpointsPanel` for the reverse direction.

The store (#577) gained a **disabled subset** (#1377): `SetEnabled`/`Enabled`,
`EnabledLines` (what adapters receive), `DisabledLines` (rendered hollow `○`
faint in the gutter via `SetBreakpointDisabledSource`, vs. the filled `●`).
Persistence moved to `{"files": …, "disabled": …}` in `.ike/breakpoints.json`;
the legacy bare-map layout still loads (upgraded on the next save), and
`AdjustEdit` shifts the disabled flag together with its line. The panel
restores from saved layouts seeded from the persisted store (identity kind
`breakpoints`).

## Combined debug area (#580, #1370, #2190)

`internal/debugpanel` + `pane.KindDebug` (singleton key `debug`, vcspanel
pattern) is the **combined debug area**: one pane hosting the
frames/variables panel and the debuggee's console behind an internal tab bar
(`Variables │ Console`, the ghissues in-pane bar pattern, #2090). The console
is a whole `terminal.Model` embedded in the panel (`SetTerm`/`Term`), so the
same stream the run/terminal path renders — reflow, scrollback, search,
selection — lives inside the area, and **view switches keep the model in
place**: scrollback offset, search and selection all survive. On session
start (first stop, first output, or a `runInTerminal` request) the area opens
without stealing focus, splitting the active editor at the adaptive placement
(`layout.SplitLeaf` with `auxZone`, #1588 — below, or right of a wide
landscape host). A `[tools.layout]` slot assigned to `debug` (#1897, #1946)
pins it to its template position instead — independent of a slot assigned to
`run`. It is an ordinary pane: the whole area resizes, moves and closes as
one unit through the normal windowing system.

**View switching** (#2190): the tab bar renders as the panel's first content
row once a console exists. `tab`/`shift+tab` cycle the views (on a pipe
console too — a PTY debuggee gets every raw key instead, `h`/`l` keep
switching the panel's columns); a click on a label switches; and the
`debug.console` command (palette) toggles + focuses the area from anywhere —
the keyboard route that works even while a PTY debuggee owns the keys.
`AutoTab` drives the view until the user picks one per session
(`tabTouched`, re-armed by `ResetSession`): output before the first stop
surfaces the console, a stop surfaces the variables, a PTY install surfaces
the console.

**Routing seams**: while the console view is visible the pane advertises the
terminal keymap context (`Instance.ContextID`) and exposes the embedded
terminal as `Instance.ActiveTerminal()` — so terminal keys, selection drags
(`dragTermSelect` via `dragTerminal`), paste, scrollback search and the
close-guard busy check all reuse the terminal pane paths. Mouse translation
adds the tab-bar row to `contentYOff` (`debugConsoleRows`) so coordinates
arrive terminal-local; the bar itself then sits at content-local y = −1. The
console session parks/unparks and tears down with the workspace like any
terminal (`setWorkspaceTerminalsParked`, `teardownWorkspace`, quit path), and
`registry.Close` on the pane ends it (`CloseTerm`).

**Console feeding modes**:

- **Pipe mode** (`terminal.NewPipe` / `Session.FeedBytes`): a process-less
  terminal session — emulator, spool and feed loop without a PTY — fed with
  the DAP `output` events (`FeedText` normalizes `\n` to `\r\n`). Output
  arriving before the area opens is buffered on `debugState` (capped at 5000
  chunks) and flushed in on open (`ensureDebugConsole`). On session end
  `FinishPipe` renders the usual `[process exited with code N]` dead view
  while the session stays feedable, so trailing output the adapter flushes
  past `terminated` still lands (#637).
- **PTY mode**: a `runInTerminal` debuggee's command session replaces the
  pipe placeholder in the same area (see "Interactive input" above); keys,
  mouse, paste and scrollback behave exactly like a terminal pane through
  the routing seams above.

When the session ends the area **stays open in a finished state** by default
(#689): the panel's frames clear to a `finished (exit code N)` placeholder
and the console keeps its scrollback reviewable; `debug.session_end = close`
(config + Settings UI, #2190) removes the area instead. The next launch
reuses the still-open area: `ResetSession` wipes the panel and re-arms the
automatic view selection, and a fresh pipe session replaces the console
(`SetTerm`) — so the placement the user arranged survives across sessions.
Every chunk is also appended to the per-project transcript
`.ike/debug-session.log` (#624; stderr chunks prefixed `[stderr] `, ANSI
stripped via `debugpanel.StripANSI`). Output events **coalesce per ~50ms
quiet window** before reaching the Update loop — parked *and* active since
#2176 (`debugEventCoalescer`, #1557): a print-looping debuggee costs one
Update+render batch per window instead of one per event — and the transcript
appends through a **held file handle** (#2176) instead of paying
MkdirAll+open+close per event; a session start drops the handle so a deleted
transcript is recreated.

**Persistence** (#1370, #2190): `saveLayout` records the area as one `debug`
leaf (restored empty, variables view, no console — session state never
resurrects); its position persists per project like any pane. Layouts
written before #2190 may still carry the separate debuggee terminal as a
`debugTerm` leaf — restore **prunes** it instead of resurrecting a shell in
its place.

The panel's two columns are **resizable** (#691): dragging the
frames│variables separator adjusts the widths
(`SeparatorHit`/`ResizeSeparator`, app gesture `dragDebugDiv`), clamped to a
minimum column width; the width is stored in exact cells (#695) and rescales
proportionally on panel resize, session-local like scroll state.

- **Frames view** (left): the paused thread's stack; `j`/`k` move, `enter`
  emits `SelectFrameMsg` — the app navigates the editor to the frame's
  location and re-fetches its scopes, so the variables show the state
  *outside* the current function too. The panel renders in **every state**
  (#637) — `not paused` before the first stop; while the debuggee runs a
  `running…` indicator leads and the **last stop's frames/variables stay
  visible faint as stale context** (#693). The first output event **opens the
  area** if it is closed (once per session, so an area the user closes stays
  closed) — a program that never hits a breakpoint is still visible, and the
  console view surfaces automatically until a stop arrives (#2190).
- **Variables tree** (right, `h`/`l` switch columns — `tab` cycles the
  area's views once a console exists, #2190): roots are the
  selected frame's scopes (Locals expands eagerly); `enter` expands/collapses
  a node — unloaded references emit `ExpandVarMsg` and the app answers with
  the adapter's `variables` response (`SetChildren`), loaded ones toggle
  locally.
- The panel is pure view/state: data arrives via `SetFrames`/`SetScopes`/
  `SetChildren`/`SetRunning`; the app resolves intents against the live
  session (`fetchScopes`/`fetchVariables`).
- **Mouse** (#626, `mouse.go`, vcspanel pattern): the app routes wheel and
  left-click over `KindDebug` to the panel — while the console view is
  visible they route to the embedded terminal instead (#2190, tab-bar row
  excepted; a press on the bar switches views and outranks the column
  separator). A click focuses the column under
  the cursor (x against the separator) and selects the row; a **double-click**
  (same row within 400ms) activates it, mirroring `enter`. The wheel scrolls
  the focused column. Both columns carry a scroll offset (`frameTop`/`varTop`),
  and keyboard `j`/`k` auto-scroll to keep the selection visible. Hardening
  (#639): coordinates outside the pane interior (border clicks — the layout
  hit-test spans the whole pane rectangle) are rejected instead of mapping onto
  a row/column; every click records into the double-click tracker, so an
  intervening click elsewhere resets a pending double-click; the wheel drags
  the selection along to stay inside the visible window (vcspanel behavior); a
  click while the inline value editor is open cancels the edit first and then
  selects normally, and a wheel while editing scrolls without moving the
  selection (which would re-anchor the editor onto a different row).
- **Editing values** (#627): `e` on a variable row opens an inline line editor
  (prefilled with the current value); it is a shared single-line input
  (`ui.EditKey`, #2002 — word motions, the `opt`/`cmd` kills, rune-safe
  backspace, and `cmd+v` routed to it while it is open), `enter` commits and
  `esc` cancels. Commit emits `SetVarMsg{Ref, Name, Value}`;
  the app calls `Session.SetVariable` (DAP `setVariable`, targeting the row's
  *containing* `variablesReference`) then refetches that reference so the panel
  shows the adapter's new value. The affordance is gated on the adapter's
  `supportsSetVariable` capability (read from the initialize response and pushed
  to the panel via `SetEditable` when it opens); scope roots aren't editable.
  While the editor is open the app routes every key to the panel
  (`debugPanelEditing`), like an editor in insert mode. Hardening (#640):
  `openDebugPanel` runs the attach step (`attachDebugPanel`: the `SetEditable`
  gate) even when the panel already exists — a panel restored
  from a saved layout becomes editable at the session's first stop instead of
  staying read-only; `SetScopes`/`SetChildren` cancel an open inline editor
  (an async refresh replaces the tree, and enter would commit a stale
  ref/name); `setDebugVariable` refuses with an Info notice while the debuggee
  runs, and a spontaneous `continued` event flips the panel into the running
  state (`SetRunning`) like stepping does — the stale rows remain visible
  (#693) but frame activation, variable expansion and inline editing are gated
  until the next stop, and an open editor is cancelled; a refetch failure after a
  successful set surfaces as an error toast ("value set, refresh failed")
  instead of silently showing the old value; the inline editor row is windowed
  to the variables column width around the cursor, so a long value cannot
  overflow into the output column; and the esc that cancels an edit is consumed
  by the panel *before* the double-esc detector, so it never arms the esc-esc
  palette shortcut.
