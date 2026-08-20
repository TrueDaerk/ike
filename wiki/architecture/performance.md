---
type: concept
title: Performance & Diagnostics
description: Idle-behavior rules (who may wake the render loop, and how often), the in-app performance HUD, and the opt-in runtime diagnostics hooks (IKE_PPROF endpoint, SIGUSR1 dumps).
resource: internal/perfhud
tags: [architecture, performance, pprof, idle, diagnostics, hud]
timestamp: 2026-08-20T00:00:00Z
---

# Performance & Diagnostics

Epic 0400 fixed the *active* hot paths (render/scroll caches #612/#614/#615,
input coalescing #602/#803). This page covers the *idle* complement (#1001):
what may wake the program while nothing happens, and how to diagnose a
long-session regression.

## Idle rules

A bubbletea message wakes Update **and** a full View composite of every pane —
so with many panes each unnecessary wake is expensive. The standing rules:

- **No unconditional repeating ticks.** Debounce-style timers (autosave idle,
  backup, VCS refresh, keymap chord timeout, the follow-mode poll #1928) arm
  on demand and re-arm only while work is pending (`arm*Tick` + `*TickArmed`
  flags in `internal/app`).
- **The explorer auto-refresh poll loops off-loop** (#1001): the 2s directory
  mtime comparison runs inside its own Cmd goroutine and only returns a
  `pollMsg` when something actually changed — or after `pollIdleRounds` (30)
  quiet intervals, so the stamp snapshot refreshes about once a minute and
  newly expanded directories join monitoring on that wake.
- **Terminal output** wakes are bounded by the per-session quiet interval
  (8ms, CAS-guarded single timer) and folded across sessions by the adaptive
  input coalescer (#803). Shell prompts that redraw on their own (clocks, git
  polling) still cost one wake per burst — that part is the shell's choice.
- **Single-shot debounce timers die with their owner** (#1001): a terminal
  session cancels its pending trailing resize on Close, the watch service its
  debounce flush on Stop, the LSP bridge its highlight/resolve/completion/
  diagnostics timers on workspaceClosed — an armed timer never fires against
  a torn-down owner.
- **The recursive file watch is capped** (#1011, `maxWatchDirs` = 4096):
  fsnotify's kqueue backend holds an fd per watched object, so an unbounded
  walk over a huge root (a stray `$HOME` restore, a monorepo) exhausts the
  process fd limit before bubbletea can create its input reader. Past the
  cap the walk stops, a `watch.TruncatedMsg` toasts once, and open buffers
  stay covered by the poll fallback; root/`.git`/`.ike` watches always land.
- **Caches stay bounded**: the editor line cache clears past `lineCacheCap`
  (4096) and on every render-epoch bump; terminal render caches key by
  mutation version, not history.
- **The watcher never reports its own consequences** (#1886): IKE's VCS
  refresh runs `git status`, and every status run echoes back through the
  `.git` watch — an atime bump on `index` (kqueue `NOTE_ATTRIB`/Chmod),
  `index.lock` create/remove churn, sometimes a Write on the `.git`
  directory itself. Treating any of those as a change re-arms the refresh
  that caused it: a self-sustaining ~2.5 Hz status+repaint loop that pinned
  idle sessions at high CPU on every project with a real `.git` directory.
  Three guards in `internal/watch`: the service normalizes its root to an
  absolute path on `Start` (fsnotify reports event paths exactly as watched,
  and main starts the watcher on `"."` — with relative paths the `.git`
  classification never matched at all, the actual live-app failure);
  `ingestGit` drops attribute-only events (no repository state fits in an
  atime); and it drops the git dir's own directory-self events (interior
  files — `index`, `HEAD`, `packed-refs`, `logs/HEAD` — carry every real
  change). Regression tests: `TestGitStatusEchoesStaySilent`,
  `TestRelativeRootClassifiesGitDir`.

## The performance HUD (`internal/perfhud`, #1999)

Every idle-CPU and memory regression so far (#1001, #1886, #1537) needed
external profiling to diagnose, which is why they were noticed late and
reported without data. The HUD is the self-service answer: **Performance HUD**
(`perf.hud`, `ctrl+alt+p`, View menu) floats a box in the workspace's
top-right corner — above every overlay but the toasts, so it cannot be hidden
by the very frames it explains — showing, per refresh interval:

- **Message rate**, total and by coarse category (key, mouse, resize, tick,
  other) plus the loudest *concrete* message type. The buckets say what kind of
  thing wakes the loop; the type name (`followTickMsg`, `terminal.OutputMsg`)
  is what actually named the culprit in the regressions above. Categories are
  structural where bubbletea's own interfaces allow (`KeyMsg`, `MouseMsg`,
  `WindowSizeMsg`); a timer deadline is recognised by the `Tick` spelling every
  ticker in the codebase uses, and everything else is a Cmd result.
- **Frame cost**: mean and worst full-frame composition in the window, plus the
  frame rate — the same measurement the input coalescer paces scroll injection
  with (`renderNanos`).
- **Per-pane render cost**, avg per frame, most expensive first: the answer to
  "which pane is burning CPU". Attribution sits in `renderPane`, so a leaf's
  chrome *and* its content are booked against its registry key (`editor:2`,
  `explorer`, `terminal`).
- **Runtime gauges**: goroutines, armed tickers, GCs and pause in the window,
  heap in use, and RSS. Only Linux exposes a current RSS (`/proc/self/statm`);
  macOS falls back to `getrusage`'s peak, which the HUD and the snapshot label
  as such rather than passing a peak off as a live number.
- **Sparklines** over the rolling history (`perf.hud_history_seconds`, default
  60s), scaled to the window's own maximum, so a spike stays readable after it
  passed.

**Armed tickers** is the number the idle rules above are about: it should sit
near zero in a quiet session, and a stuck debounce loop shows up as a count
that never falls. The HUD's own sampling tick is counted in it — the HUD does
not get to hide its cost — and it is the only wake the HUD adds
(`perf.hud_interval_ms`, default 1000ms, floor 100ms so a diagnostic overlay
cannot become the regression it is looking for).

**Cheap when hidden** is the design constraint, taking the #1095–#1101 lesson
seriously: measurement must not become the regression. Every hook —
`Update` (counting), `render` (frame cost), `renderPane` (attribution) — is
wrapped in a `perfhud.Enabled()` check, an atomic bool load. With the HUD off
there is no clock read, no map touch, no defer closure and no allocation on
any hot path; `runtime.ReadMemStats` (which briefly stops the world) runs once
per interval and only while someone is looking. The box itself is laid out on
the sampling tick and cached in the model (`perfBox`, keyed by the width it was
laid out for) — its numbers cannot change between samples, so composing it per
frame would be its own small regression. Turning the HUD on drops the previous
history: rates spanning the closed period would be fiction.

**Copy Performance Snapshot** (`perf.snapshot`) puts the whole set on the
clipboard as a plain-text block — build stamp, every rate, the per-pane table
and min/avg/max over the history — ready to paste into a bug report. With the
HUD open it copies the sample on screen (not a fresh sub-second window nobody
can reason about); with it closed it samples on the spot and says plainly that
the rates are missing rather than printing measured-looking zeros.

The HUD's open state lives in the collector, not in the root model: the model
is rebuilt on a project switch and the in-flight sampling tick has to find the
HUD still open across it.

Reach for the HUD first — it answers "what is waking me and which pane is
expensive" in seconds. Reach for the pprof hooks below when the answer is
"something inside one pane" and you need the stack.

## Diagnostics hooks (`internal/diag`, #1001)

Off by default; `diag.Start` in `cmd/ike/main.go` wires them:

- `IKE_PPROF=<addr>` (e.g. `localhost:6060`) serves `net/http/pprof`:
  `go tool pprof http://localhost:6060/debug/pprof/profile` for CPU,
  `/debug/pprof/goroutine?debug=1` for stacks.
- `SIGUSR1` writes `ike-<pid>-<time>-goroutines.txt` and `-heap.pprof` to
  `IKE_PPROF_DIR` (default: the OS temp dir) — the no-listener option for a
  session that is already misbehaving.
- Palette commands (#1537), always available: **Memory Statistics**
  (`diag.memoryStats`) scavenges (`debug.FreeOSMemory`) and toasts a one-line
  `runtime.ReadMemStats` summary — `HeapInuse` growing means a real leak,
  a large freed-not-returned share (`HeapIdle−HeapReleased`) is runtime-held
  memory that macOS keeps counted against the footprint until pressure.
  **Write Heap Dump** (`diag.heapDump`) writes the same profile pair as
  `SIGUSR1` and toasts the path.

## Memory bounds in long sessions (#1537)

Growth caps and give-backs so hours of use stay bounded:

- **Undo history byte budget**: 32 MiB of retained edit text per buffer on
  top of the 1000-node cap (`internal/editor/history`) — whole-buffer
  changes hold ~2× the document each, so the node cap alone allowed
  gigabytes.
- **Crashed LSP servers are stopped for real** (`watchExit` → `srv.stop()`):
  the read loop can end while the child lives (closed stdout, bad framing);
  the orphan used to keep its process, goroutines, stderr ring and log
  handle for the session. Queued outbound frames are also dropped once a
  connection's stream is gone (`jsonrpc.stopWriter`).
- **Closed buffers untrack from the poll watcher** and drop their per-path
  toasts, so a session's every-file-ever-opened set stops accumulating
  stamps and per-poll stats.
- **Workspace teardown scavenges** (`debug.FreeOSMemory`, async): the
  largest frees this process makes go back to the OS instead of lingering
  as `MADV_FREE` pages in the macOS footprint.

Long-session triage: dump goroutines at minute 1 and after an hour idle with
~10 mixed panes; a growing count names the leaking loop, a flat count with
rising CPU points at wakeups (profile 30s of "idle" CPU and look for View/
render frames). `TestSessionCloseLeavesNoGoroutines` pins the terminal
session lifecycle as a regression test.

## Frame wash (#1095)

The palette background/foreground wash at the end of `render()` used to run
`lipgloss` with `Width`/`Height` over the fully composed screen — re-wrapping,
re-aligning and grapheme-scanning ~12k cells per keystroke (52% of frame CPU,
68% of allocations in the profile). Since the frame is composed at exactly
`width x height` (#612), the wash now styles per line without measurement; a
non-full-height frame (defensive) falls back to the padded variant. Benchmark
`BenchmarkAppRender` guards the cost.

## Explorer width cache & colour index (#1096, #1098)

`contentWidth` used to rebuild every flattened row's text (plus an
`ansi.StringWidth` grapheme parse) on every frame — and on every Update-path
`viewport()` call (mouse hit-tests included). It is now memoized in a
pointer-held `widthCache`, invalidated in `rebuild`/`SetSize`/`Configure`; the
pass also stores each node's plain row width (`node.rowW`), which `View` reuses
instead of re-parsing styled strings for clipping. The colour table's glob
list is sorted once per table build and colour strings resolve once into
`colorVals` (#1098) instead of per row per frame. Benchmarks
`BenchmarkExplorerView` / `BenchmarkExplorerViewport` guard it: 2000-row View
1.12ms/7.0k allocs → 0.72ms/2.9k; viewport 0.57ms → ~29ns steady-state.

## Per-row VCS resolution (#1099)

Explorer rows resolved their snapshot facts (status, ignored, tint, status
letter) through up to five separate `Snapshot.relPath` calls per row per frame
— each an allocating `filepath.Rel`, and on symlinked roots (macOS `/tmp`) a
double `EvalSymlinks` syscall pair per call. `View` now resolves a `rowVCS`
struct once per row and threads it through style/tint/letter; the Snapshot
caches its `EvalSymlinks`-resolved root after the first fallback miss. The
per-path half of that fallback is memoized too (#1886): on a symlinked root
every row still paid one full `EvalSymlinks` walk per frame — the dominant
render cost on medium projects — so `relPath` now resolves only the parent
directory, cached per directory on the snapshot. Keeping the base unresolved
also matches git's view: a tracked symlink is reported as the entry itself,
never as its target.

## Editor scrollbar overlay (#1097)

The #1022 scrollbar overlay ran outside the #614 line cache: per frame it
rebuilt the diagnostics stripe map from every diagnostic and allocated a
lipgloss style chain + Render per bar cell. The stripe is now memoized in a
pointer-held `sbCache` keyed on a diagnostics epoch (bumped in
`setDiagnostics`) plus the track geometry, and the track/thumb/severity cells
render once per overlay call instead of once per row. Warm 5k-line View with
an active stripe: 329µs/606 allocs → 234µs/334 (BenchmarkEditorViewWarm).
The remaining per-row `ansi.Truncate` pass is bounded by the pane width; the
deeper option (carrying row widths in the line cache) stays open in the issue
notes if it ever shows up again.
