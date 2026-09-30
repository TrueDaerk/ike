---
type: concept
title: Crash Reporting
description: Every panic leaves a crash log — Update/View/Cmd guards that keep the session running, a goroutine guard (safego.Go) with a static ledger, main's fallback with captured stderr, and a next-launch notice with an open command.
resource: internal/crashlog
tags: [architecture, diagnostics, crash, panic, telemetry]
timestamp: 2026-09-30T18:00:00Z
---

# Crash Reporting

IKE died twice on a user in one week and left nothing to read (#2836). The
process ran under bubbletea's default panic catcher, which restores the
terminal and prints the panic and stack to **stderr** — inside the alternate
screen that has just been torn down, so the text is gone before anyone sees
it. A crash without a trace must not be possible in an IDE. This subsystem
makes every panic leave a **crash log**, and makes most of them survivable.

Three packages share the work:

| Package | Role |
|---|---|
| `internal/crashlog` | Leaf package. Writes `~/.ike/logs/crash-<timestamp>-<session>.log`, keeps at most 20, tracks the next-launch acknowledgement, holds the UI snapshot every writer reads. |
| `internal/safego` | `safego.Go(name, fn)` / `safego.Recover(name)` — the guard for goroutines IKE starts itself, plus the static ledger test that forbids bare `go` statements. |
| `internal/app/crash.go` | The root model's guards: `Update`, `View` and every `Cmd` run under a recover; the goroutine reporter; the `crash.openLastLog` command; the next-launch notice. |

`cmd/ike/crashfallback.go` is the last line: whatever still ends the program
gets a report from `main`.

## The crash log

One file per caught panic, plain text, content-free in the telemetry sense
(chords, command ids, context ids, pane kinds and file paths — never buffer
text):

```
IKE crash report
time:     2026-09-30T13:35:09.114+02:00
version:  0.6.113 (abc1234)
go:       go1.26 darwin/arm64
where:    update                      # update | view | cmd | goroutine:<name> | program
message:  tea.KeyPressMsg             # the message Update was handling
panic:    runtime error: invalid memory address or nil pointer dereference
context:  editor[python]              # key context at the time
pane:     editor /path/to/file.py     # focused pane kind and file

--- last 50 telemetry events (oldest first) ---
13:34:57.750 key chord=cmd+shift+f command=project.findInPath context=playground status=resolved
…
--- panicking goroutine ---
<runtime/debug.Stack of the goroutine that panicked>

--- all goroutines ---
<runtime.Stack(all=true)>
```

- **Directory**: `$IKE_CONFIG_DIR/logs`, else `~/.ike/logs` — the language
  servers' log directory, so every diagnostic file lives in one place.
- **Session**: the telemetry session id (`Recorder.SessionID`), so a crash log
  and the session's `.jsonl` line up; without a recorder a random per-process
  tag.
- **Cap**: `crashlog.KeepFiles` = 20; writing a new report prunes the oldest.
- **Throttle**: one process writes at most three reports for the same panic
  message (a View that fails on every frame would otherwise cost a goroutine
  dump per render); later repeats are counted into the next distinct report
  (`skipped: N earlier repeats`).
- **UI snapshot**: the root model publishes the key context, focused pane kind
  and file after every settled Update pass (`crashlog.SetFocusIfChanged`, a
  few string compares when nothing moved), so a goroutine's report has them
  without touching the model.
- **Telemetry ring**: the usage recorder keeps the last 50 events in memory
  (`Recorder.Recent`, [usage telemetry](/architecture/usage-telemetry.md)),
  whether or not telemetry is enabled — the ring is never written anywhere
  but a crash log.

## Guards inside the loop (`internal/app/crash.go`)

bubbletea's catcher is kept; IKE's guards run *inside* it, with the panic
value in hand, and the decision recorded here is **recover and keep the
session**:

- **`Update`** wraps the loop body (`updateUnguarded`). A panic while handling
  one message writes the report, raises an error notification naming it
  (action: *Open crash log*) and returns the model **from before the
  message**; the returned command runs one settled pass so the toast drains
  at once. The next message is handled normally. Dirty buffers stay
  protected: the backup service ([crash recovery](/architecture/crash-recovery.md))
  rides on the same loop, which is exactly what did not end. State a pointer
  shared with the failed pass mutated stays mutated — the best a value model
  can do, and still a running IDE rather than a dead one.
- **`View`** wraps the frame composer (`viewUnguarded`). A panic writes the
  report and draws a plain fallback frame naming it (*any key retries the
  render, ctrl+q quits*); the toast lands on the next pass.
- **Cmds**: every command `Update` (and `Init`) hands to the program passes
  through `guardCmd`, which recovers, writes the report and delivers a
  `crashRecoveredMsg` instead. A `tea.BatchMsg` is re-wrapped member by
  member as it resolves, since the program runs each member on its own
  goroutine. (Sequences are opaque to the model; a panic inside one falls
  through to main's fallback below.)
- **Test hooks**: `crashPanicMsg` makes `Update` panic inside the guard;
  `Model.viewPanic` does the same for `View`. `crash_test.go` is the
  acceptance test.

## Goroutines: `safego.Go`

bubbletea catches nothing on goroutines IKE starts itself — search workers,
the LSP bridge and its readers, the watcher, forge polling, playground
evaluation, terminal sessions, `host.Send`'s pump. A panic there killed the
process. Every such `go` statement is now

```go
safego.Go("search.Service.Scan", func() { s.run(ctx, gen, q) })
```

The guard recovers, writes the report (`where: goroutine:<name>`) and calls
the **reporter** the app installs (`installCrashHooks`): an error
notification *"background task <name> failed: … — crash log: …"* through the
live host (an atomic pointer the Update wrapper refreshes, so a project
switch's fresh model is followed). The goroutine ends; its owner degrades
(a dead pump, a stopped scan) instead of the IDE. `safego.Recover(name)`
is the deferred form for goroutines another package starts with IKE's
callback.

**Static ledger**: `internal/safego/goguard_test.go` parses every non-test
Go file under `internal/`, `plugins/` and `cmd/ike` and fails on a bare `go`
statement outside `bareGoAllowed`, a map of file (or `file:func`) to reason;
a stale entry fails too. The only entry is `safego.go` itself. The names are
`<package>.<Type>.<method>` of the starting function — stable, content-free
identifiers.

## main's fallback and captured stderr

Whatever still reaches bubbletea's catcher — a panic in the program's own
machinery, a sequence member, the message filter — ends `Run` with
`tea.ErrProgramPanic` after bubbletea printed the value and stack to stderr.
`cmd/ike` therefore **tees stderr for the process lifetime** (`captureStderr`:
a pipe forwarded to the real stderr, the last 64 KiB kept in memory). A `Run`
that ends in a panic writes a report (`where: program`) with that tail under
`--- stderr ---`, and the restored terminal gets exactly one line:

```
ike: crashed — crash log: /Users/me/.ike/logs/crash-20260930T113509.114Z-53c739ea28ba.log
```

The exit code is non-zero either way; any other `Run` error keeps its plain
message.

## Next-launch notice

On start, after construction, `Model.NoticeLastCrash` compares the newest
crash log against the acknowledgement marker (`logs/crash-ack`, the file name
last announced). A newer one raises a **warning** notification — *"IKE crashed
last time — see …"* — with **`crash.openLastLog`** (*Open Last Crash Log*) as
its action, and is acknowledged at once, so the notice shows exactly once per
crash. The command opens the newest report in a split, the way `lsp.showLog`
opens a server log, and is also in the palette; it ships without a default
chord (ledger: occasional one-off, the notice is its doorway).

## The crash this found (#2836)

`app.activeSelectionText` — the find-in-path / replace-in-path prefill —
walked the focused pane's active tab as an editor whenever the pane was an
editor pane. A **viewer content tab** (an image, a data, hex or archive
viewer, a notebook, an HTTP response tab, #1778) has no editor behind it, so
`cmd+shift+f` with such a tab active dereferenced nil and took the IDE down.
The prefill now treats a missing editor as "no selection"
(`selection_2836_test.go` covers every viewer kind). The telemetry sequence
of the reported crash — project switch, find in path from a focused
playground, a hit opened into a Python buffer, `f4`, `cmd+f` — is replayed by
`finder_playground_2836_test.go` in every playground layout and does not
panic; whichever panic actually ended those sessions now leaves a crash log.
