---
type: concept
title: Performance & Diagnostics
description: Idle-behavior rules (who may wake the render loop, and how often), the render budget and the always-on per-message-type pass accounting, the in-app performance HUD, startup/project-open phase instrumentation and the async open path, the always-on update-loop stall watchdog, the opt-in update-loop trace log, the freeze-triage procedure, the selection-overlay rule for drag latency (#2495), and the opt-in runtime diagnostics hooks (IKE_PPROF endpoint, SIGUSR1 dumps).
resource: internal/perfhud
tags: [architecture, performance, pprof, idle, diagnostics, hud, watchdog, startup, freeze, render-budget]
timestamp: 2026-09-04T00:00:00Z
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
  flags in `internal/app`). Each such tick also carries the generation of the
  model that armed it, so a project switch cannot leave two chains running on
  the same clock (#2194, below).
- **One documented exception: the forge poll** (#2085). Watching a remote
  forge has no change seam to arm on, so its tick repeats while
  `forge.poll_interval_seconds` is non-zero — a standing 1 in the armed-ticker
  count, one wake per interval (default 20s), and `0` is the opt-out. It stops
  on its own while the forge is unavailable and backs off exponentially while
  fetches fail. Its handler only dispatches the fetch `Cmd`, so the wake stays
  a wake and never becomes a stall; see [Forge Layer](/architecture/forge.md).
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
- **Modal-deferred work waits event-driven, not on a ticker** (#2402): the
  terminal-capability verdict due while another modal owns the floating shell
  used to re-poll on a 2s tick (capped at ~a minute, #2163) — still the
  loudest idle source in the #2402 telemetry, ~10 passes per 10s for the
  first minute of every session parked in the tour/onboarding. It now parks
  as a `pending` flag and Update's settled pass draws it on the very message
  that closes the blocking modal — zero wakes while waiting, and no give-up
  cap needed. The pattern generalizes: anything waiting for UI state to
  change should ride the message that changes it, never poll for it.
- **LSP servers cannot re-render an unchanged truth** (#2402): notifications
  other than `publishDiagnostics` are dropped in the manager before any
  message exists ($/progress, logMessage, telemetry — an idle server's
  chatter never wakes the loop), diagnostics coalesce into one
  `DiagnosticsBatchMsg` per 50ms window (#597), and since #2402 the bridge
  also drops a publish whose converted set equals the last one delivered for
  that path — including "still empty" for a path never delivered. Servers
  that republish their whole workspace view on every watched-file round now
  cost zero passes while nothing changed
  (`TestNoChangeRepublishDropped`).
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

## The render budget & the idle pass count (#2402)

The unit the idle rules are enforced in is the **pass**: one
`diag.LoopEnter`/`LoopExit` bracket around an `Update` dispatch or a `View`
composition. Every message costs **two** passes — bubbletea calls `View`
after every accepted `Update` with no dirty check. Two framework facts,
verified against bubbletea v2.0.7, bound what a fix can and cannot do:

- **A `Cmd` returning `nil` costs nothing**: nil messages are dropped before
  `Update` (tea.go's receive loop), so a poll that found no change should
  return nil, not a "nothing changed" message.
- **The renderer already dedups identical frames** (`viewEquals` in the
  cursed renderer): composing the same string twice writes to the terminal
  once. A frame hash inside IKE would therefore only save compose CPU, never
  a pass — the way to cut the pass rate is to cut *messages at their source*,
  not to short-circuit `View`.

**The budget**: an idle session — editor, terminal tool printing nothing,
LSP server running — targets **p50 ≤ 5 passes / 10s, p99 ≤ 60** (#2402
telemetry targets). The measured steady state is 0 outside the documented
exceptions (explorer keep-alive ~2 passes/min, forge poll ~6–9/min where a
forge is configured). Rules for new code, in budget terms:

- A repeating tick is 2 passes per firing, forever — demand-arm it (idle
  rules above) or it is a standing spend no one approved.
- A per-event message flood is 2 passes per event — coalesce at the producer
  (the 8ms terminal quiet window + adaptive 16–66ms input coalescer, the
  50ms diagnostics batch, the 100ms watch debounce are the patterns).
- A message that changes nothing visible is 2 wasted passes — drop it before
  `host.Send` (the #2402 no-change diagnostics drop), return a nil msg, or
  wait event-driven on the pass that changes the state (the #2402 termcheck
  deferral).

**Accounting is always on** (#2402): `diag.LoopEnter` tallies every outermost
pass under the message's Go type name (or the `view/render` label), two map
operations per pass. `diag.MessageCounts` exposes the snapshot; the telemetry
heartbeat diffs two snapshots and ships the interval's top 3 as the `top`
field (`app.termCheckMsg:5,view/render:5,…`), so an idle regression in the
field names its own culprit in the next telemetry export — the HUD (below)
is the interactive view of the same question.

## The performance HUD (`internal/perfhud`, #1999)

Every idle-CPU and memory regression so far (#1001, #1886, #1537) needed
external profiling to diagnose, which is why they were noticed late and
reported without data. The HUD is the self-service answer: **Performance HUD**
(`perf.hud`, `ctrl+alt+p`, View menu) floats a box in the workspace's
top-right corner — above every overlay but the toasts, so it cannot be hidden
by the very frames it explains — showing, per refresh interval:

- **Message rate**, total and by coarse category (key, mouse, resize, tick,
  other) plus the loudest *concrete* message types (top 3 since #2402). The
  buckets say what kind of thing wakes the loop; the type names
  (`followTickMsg`, `terminal.OutputMsg`) are what actually named the
  culprits in the regressions above. Categories are
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

## Startup instrumentation & the async open path (#2260)

Opening a project used to be unmeasured, so a slow start was guesswork. Now
every coarse startup phase stamps its wall-clock cost into the collector
(`perfhud.RecordStartupPhase`): `project-history` (restore-last + recent list),
`wasm-plugins` (scan + compile), `model-build` (the whole constructor) with its
sub-phases `config`, `settings-ui`, `session-restore` and `recovery-scan`, and
`cli-open`. The first *sized* frame closes the measurement
(`perfhud.RecordFirstFrame` in `render`) — everything after that point fills in
asynchronously by design. Unlike the rest of the collector this is **not**
gated on `Enabled()`: startup is over before the HUD can be toggled on, so the
phases record unconditionally (a handful of appends, nothing periodic).

The numbers surface in two places:

- a **startup** block in the HUD box (first-frame total plus the costliest
  phases) and, in full, in the `perf.snapshot` clipboard text;
- one line in the state-dir log (`.ike/debug.log`) per process:
  `startup: first frame in 41ms, project-history 2ms, …` — written once, off
  the render path, after the chdir into the project root.

A project switch re-runs the constructor phases and re-records them by name
(replace-in-place), so the block always describes the most recent open.

**What must not block the first frame.** The rule since #2260: nothing after
the first sized frame may wait on tree-sized work. The blocking steps that
were on the critical path and are now asynchronous:

- **The file watcher's recursive registration** (`watch.Service.Start`) walks
  the whole project tree to `Add` each directory — on a large repository the
  single biggest pre-first-frame cost. main.go starts it via
  `StartWatcherAsync` (a goroutine); the switch path and tests keep the
  synchronous `StartWatcher`, and a `Start` superseded by a project switch
  notices mid-walk (`Service.owns`) and abandons its closed watcher.
- **The explorer's session restore** (`explorer.Restore`) used to re-read
  every saved expanded directory synchronously on the constructor thread. Now
  only the root loads synchronously (one `ReadDir` for the first frame's
  rows); the saved expansions are recorded as pending, `Init` issues the
  reachable scans, and each landing scan expands the pending descendants it
  uncovered (`continueRestore`). The saved cursor parks via the pending-snap
  mechanism when its row appears. `TestRestoreReadsNoExpandedDirSynchronously`
  is the regression guard.
- **The LSP bridge's `fileOpened` hook** read each restored file's bytes on
  the caller (Init) before handing off to the didOpen goroutine; the read and
  the large-file gate now run inside the goroutine.

Already asynchronous before this work, and kept that way: the explorer's
initial root scan (`scanCmd`), the git status snapshot (`vcsInvalidateMsg`
through the debounced refresh), the LSP server spawn/handshake, the completion
word/symbol project scans, and the TODO index scan.

## The update-loop stall watchdog (`internal/diag`, #2163)

A freeze reported after the fact used to leave nothing: the SIGUSR1 hook
below only helps when someone sends the signal while the hang is live. The
watchdog is the always-on complement. `app.Update` and `app.View` stamp
`diag.LoopEnter`/`LoopExit` around every pass — two atomic ops, nothing else
on the hot path — and a monitor goroutine (which by construction can never be
the blocked one) polls the stamps. When one pass stays in flight past
`perf.watchdog_seconds` (default 15s; `0` opts out; Settings UI on the
Performance HUD page) it writes every goroutine's full stack (pprof
`debug=2`, the variant that shows blocking state and wait durations) to
`ike-watchdog-<pid>-<stamp>-goroutines.txt` next to `debug.log` in the state
dir (`.ike/`, or `IKE_CONFIG_DIR`), with a header naming the message type the
pass was handling, and logs the stall to `debug.log`. One dump per stall
episode, ten per session at most, and a "recovered after Xs" line when a
stall ends on its own — so a hard hang, a livelock and a transient multi-
second stall all stay distinguishable and attributable afterwards. Both a
stuck `Update` and a frame that never finishes composing are covered; the
threshold sits far above the 200ms slow-update log line, which keeps naming
the merely-slow passes. The monitor's own cadence is threshold-derived
(50ms–1s) and never touches the render loop — it is a plain sleeping
goroutine, not a `tea.Tick`.

**What the watchdog does not see (#2348).** It measures Update/View passes
and nothing else. A freeze in the terminal input reader, the renderer's
terminal write, or the terminal emulator itself leaves the loop idle-but-
healthy — no stall, no dump, no `debug.log` line — which is exactly the
signature of the #2348 incident. Its dumps also land in the *project's* state
dir, so without knowing which project a session ran in there is nothing to
find. Both gaps are covered by telemetry now: the `session` event's project
token attributes a log to a project, and the `heartbeat` event's pass count
is the loop-independent liveness stamp (see
[Usage Telemetry](/architecture/usage-telemetry.md)).

## The update-loop trace log (`perf.trace_log`, #2348)

For "what is the IDE doing *right now*" during a live diagnosis, the opt-in
trace (Settings UI, Performance HUD page; default off) appends one line per
processed message to `.ike/trace.log` (or `IKE_CONFIG_DIR/trace.log`): the
timestamp, the message's Go type and the number of open HTTP flights —
structure only, never key text or content. It writes through a held file
handle (`heldLog`, the #2176 transcript mechanism) so even a message flood
pays one `write(2)` per line, and costs literally nothing while off (one
config load per pass). It is a diagnosis tool, not a journal: the file grows
with every keystroke, so switch it off when done.

## Diagnosing a freeze (#2348)

The evidence trail, in the order worth checking:

1. **The telemetry session file** (`~/.ike/telemetry/*.jsonl`, newest): the
   last `heartbeat` brackets when the process stopped working to within 10s.
   Heartbeats that continue with a frozen `passes` count → the update loop is
   stuck; with an advancing count → the loop is fine and the freeze sits
   outside it (input reader, renderer, terminal); heartbeats stopping dead →
   the process ended (crash, kill, exit). The `top` field (#2402) names the
   interval's three loudest message types — for a freeze, what the loop was
   chewing on; for an idle-CPU report, the wake source, with no repro needed. An `op` `http.flight` start without
   its end phase means a dispatch never came back.
2. **The project's state dir** — found via the `session` event's `project`
   token (hash candidate roots to match): `debug.log` for watchdog stall/
   recovery lines and slow-update entries, `ike-watchdog-*-goroutines.txt`
   for the full stack dump of a loop stall.
3. **If the session is still hung**: `SIGUSR1` (dump below) or, with
   `IKE_PPROF` set, the live pprof endpoint.
4. **If it reproduces**: flip `perf.trace_log` on and read `trace.log` up to
   the freeze — the last line names the message the loop took on last.

The #2348 incident itself (frozen right after an `http.run`, telemetry ending
with the dispatch, no watchdog dump anywhere): the dispatch path was audited
and cannot wedge the update loop — the exchange runs on its own goroutine,
every stream message re-arms exactly one channel read, and the coalescer's
mutex-held emit pins chunk-before-final ordering — so the silent watchdog
points outside the loop (or to a project dir never located). The events above
exist so the next occurrence answers this in minutes.

## The #2163 freeze audit

The follow-up sweep after another 100%-CPU freeze. Every tick site, goroutine
IO loop and Update/View hot path was audited; findings fixed in #2163:

- **Explorer poll chains multiplied across project switches.** The
  auto-refresh loop is a chained Cmd goroutine with no cancellation seam; a
  project switch or workspace resume re-armed the departed model's chain into
  a permanent duplicate stat-walker — one more per switch. Chains now carry a
  process-wide id (`pollID`); a `pollMsg` from a chain the model does not own
  retires silently, `Init` rotates the id so it is idempotent, and
  disabling/re-enabling `explorer.auto_refresh` round-trips
  (`RearmPoll` on config reload).
- **LSP diagnostic ranges are clamped to the buffer** in `setDiagnostics`:
  servers idiomatically express "to end of document" as line 2^31-1, and only
  columns were clamped — the per-line index loop would have run billions of
  iterations on the update loop.
- **The .http flight tick could double-arm**: the "map was empty" guard raced
  the chain's final in-flight tick; a dispatch in that window built a second
  permanent 4 Hz re-parse chain. An explicit `httpTickArmed` flag now guards
  it, and it is counted in the HUD's armed-timers gauge.
- **Past-due debounce marks are always consumed** (backup, idle autosave):
  `Due()` is the only thing clearing deadlines, and an unconsumed past-due
  mark under a disabled feature was a zero-delay `tea.Tick` loop — 100% CPU —
  held off only by invariant, not construction.
- **The watch debounce has a flush ceiling** (`maxDebounceWait`, 10 windows):
  sustained churn faster than the 100ms window used to reset the timer
  forever — no flush, unbounded pending growth, changes invisible until the
  writer paused.
- **Message floods coalesce instead of hitting Update per event** (#2176):
  HTTP stream chunks fold into one message per ~12ms window
  (`httpChunkCoalescer`) and the response viewer extends its projection and
  search at the tail instead of recomputing them per chunk; task problem
  matchers snapshot per ~50ms window instead of deep-copying per matched
  chunk; DAP output batches unconditionally (not just parked) and the
  transcript appends through a held handle instead of open/close per event;
  a watch flush arrives as one `EventBatchMsg` (one Update pass per
  checkout, not per file), the change feed's local-history capture runs
  off-loop, and symbol-index invalidation queues behind one worker instead
  of one goroutine per changed file.
- **The terminal-check retry tick gives up** after ~a minute of a parked
  modal instead of waking the program every 2s for the session's life.
- **Clipboard subprocesses are deadline-bounded** (3s): they run on the
  update loop, and `osascript` can hang indefinitely behind a TCC prompt —
  previously a whole-IDE wedge with no way out.
- **Terminal scrollback search matches are memoized** by (query, grid
  version, line count): `searchLine` ran the full 10k-line scan — 10k
  `gridMu` acquisitions and 20k allocations — per *frame* while the field
  was open, pinning a core against a busy shell.
- **Follow mode re-evaluates the large-file gate** as the tail grows: the
  flag was load-time-only, so a small log tailed to hundreds of MB kept
  materializing its entire text on every append's change event.
- **The DBGp accept loop survives transient errors** (`EMFILE` under a
  php-fpm burst) with a 100ms backoff instead of dying silently for the
  session.
- **The host `Send` outbox is bounded and coalescing** (#2169): 1024
  messages, drop-newest with a counted `debug.log` line when the bound
  trips, and keyed in-place replacement (`host.Coalescable`, the jsonrpc
  `NotifyCoalesced` pattern) for idempotent snapshots — the last hop that
  otherwise defeated every producer's own backpressure.

Audited and left as they were (safe by construction): the forge poller
(idempotent arm, exponential backoff, root-tagged messages), the demand-armed
debounce ticks, the terminal read/feed/write loops (error exits, `sync.Cond`
spool with a 16 MB cap), the jsonrpc read/write loops, the LSP restart
backoff (capped at 3), the input coalescer, and the watcher's fsnotify loop.
Larger flood/coalescing findings (HTTP stream chunk coalescing, task-problem
snapshots, active-session DAP output, watcher event batching,
completion-source locking, per-frame sticky/fold/speed-search memoization,
tick generation stamps across model rebuilds) are split into follow-up
issues — see the #2163 issue trail; the host outbox bounding landed as
#2169, the per-frame memoizations as #2187 (next section) and the tick
generation stamps as #2194.

## The remaining per-frame recomputation (#2187)

The #2163 follow-up: the four hot paths the #612/#614/#1096–#1099 caches did
not cover. All four take the same shape — a memo behind a pointer field, so
the value copies of a Model share it, keyed on everything the computation
reads and invalidated where that changes.

- **Sticky-scroll headers** (`internal/editor/sticky.go`): `enclosingHeaders`
  scans every scope of the parse, and the fixed-point loop that settles the
  pinned count repeats that up to `stickyDepth` times — per ask, and `View`,
  the scroll paths and the mouse map each ask per frame. `stickyLines` now
  memoizes against `stickyKey` (viewport top, the depth cap, buffer line
  count, `scopeEpoch`, `docVersion`). `scopeEpoch` exists because a landing
  parse replaces the scopes *without* bumping the document version — the
  version alone could not see them change — so every `scopes` write funnels
  through `setScopes`.
- **Fold hiding** (`internal/editor/fold.go`): `lineHidden` scanned the whole
  collapsed map per rendered row per frame — a 100k-line file under `zM` is
  ~400k map iterations per frame between the `View` body loop and
  `wrap.DisplayRow`. The collapsed set now carries an interval index
  (`foldSpans`: header lines ascending plus the running maximum of the fold
  ends), binary-searched instead of scanned, rebuilt once per fold mutation.
  Every mutation of `folded` funnels through `bumpFolds` — the index is only
  as trustworthy as that funnel — and a second view of a shared document gets
  its own index, like its own collapsed set (#144). The same path also stopped
  re-deciding "is this a log buffer" per row: `logBuffer` resolves
  `lang.ByPath` (a mutex, a regexp and a dozen allocations) once per language
  path now, dropped on a config reload since `files.associations` can rename
  it. `BenchmarkLineHiddenManyFolds` guards a frame's worth of rows over 2000
  folds: ~420µs/480 allocs → 31µs/0.
- **Explorer speed search** (`internal/explorer/search.go`): `searchLine`
  renders its "3/17" counter from `searchMatches`, which lowercases and scans
  every flattened row — tens of thousands on a monorepo — per frame for as
  long as the `/` field is open. Memoized on the `searchState` by query and a
  `rowsEpoch` bumped in `rebuild`, the `widthCache` pattern (#1096); the
  terminal's scrollback search got the same fix in #2163.
- **The settled pass** (`internal/app/app.go`): the reconcile steps at the end
  of `Update` run once per *message*, so a flood paid O(panes × tabs) each
  time. `imageSyncCmd` returns immediately unless the registry ever minted an
  image pane (`Registry.ImagesMinted`) or placements are still resident, and
  allocates its live-id map only once an image pane is actually found;
  `domSyncCmd` returns before resolving the active editor when neither the
  inspector nor a highlighted file exists; `syncBreadcrumbLayout` returns
  before building the pane signature when no pane *can* show the row (zen, the
  feature off, or no symbol tree cached) and the recorded signature is already
  empty. `internal/app/settledearlyout_test.go` pins all three at zero
  allocations per pass in those states.

## Tick generation stamps across model rebuilds (#2194)

The other #2163 follow-up. Every `*TickArmed` guard lives on the `Model`
*value*, but a project switch rebuilds the model (`buildModel`) while a tick
minted by the departed one may still be sleeping — a window of up to the whole
tick interval. The fresh model's zeroed flag then let that tick arm a second
chain on the same clock. For the follow poll both chains self-sustain while a
view keeps following: one extra 500ms poll chain per park/resume race. The
backup and idle-autosave chains converged on their own (re-arming needs
pending marks), but the class kept reappearing per site.

The guard is structural now (`internal/app/tickgen.go`): `buildModel` stamps
each model with a generation from a process-wide counter (`modelGen`), every
demand-armed tick message carries the generation of the model that armed it,
and `Update` drops a tick whose generation the receiving model does not own —
without a re-arm, which retires the departed chain, and without touching the
live model's armed flag. It is the pattern `preview.RenderTickMsg`,
`palette.LiveTickMsg`, `terminal.AutoScrollMsg`, `playDebounceMsg` and the
explorer's `pollMsg` (#2163) already used, applied to the remaining armed-flag
ticks: backup, idle autosave, follow poll, mouse-idle hover, the HUD sample,
the `.http` flight indicator and the `.http` variable lint.

The same stamp fixes a visible symptom: `termCheckMsg`. A grace or retry tick
outliving a project switch was judged against the fresh model's zero-valued
`termCaps`, and an unprobed model reads as "no Kitty keyboard protocol" — so a
Kitty-capable terminal could be told its shortcuts will not work. A stale
verdict tick now retires; the model that actually probed the terminal is the
only one that reports.

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

## Selection highlight is a per-frame overlay, never a cached line (#2495)

The rule for read-only viewers with mouse text selection (`internal/diff`, and
the shape the merge view already had): **a drag moves the selection anchors;
the highlight is painted by `View` over the visible window.** The cached
render — the styled line per document row every viewer keeps — stays
selection-free and therefore stays valid for the whole drag.

The diff viewer broke this rule: `MouseDrag` called `render()`, so every
motion event restyled all rows of the document. On a 3000-row syntax-
highlighted side-by-side diff that is **69 ms per motion event**, well past
the coalescer's 66 ms back-off ceiling, and the highlight visibly trailed the
pointer. Painting the covered *visible* lines in `View` instead: **0.79 ms**
per motion event plus frame, and the drag itself (`MouseDrag` alone) 150 ns —
constant in the document size. `BenchmarkDragMotion` / `BenchmarkDragMotionOnly`
in `internal/diff` keep it measurable.

What a drag must therefore *not* do: re-run the diff, re-parse either side for
syntax, or extract the selected text. `SelectionText` runs when the selection
is copied (`y` / `ctrl+c` / `cmd+c`), not per frame. Drag events already ride
the adaptive input coalescer above (`coalescedInputMsg`); a pane that feels
slow under a drag needs a cheaper frame, not a second throttle. See the
caching rule in [diff viewer](diff-viewer.md).

## Large-file keystroke latency (#2159)

Two guards keep typing flat regardless of file size:

- **Per-feature degradation**: every per-edit service that does O(file) work —
  the Tree-sitter parse, the LSP didChange full-text sync, the VCS gutter diff
  recompute, the search match tally, the on-save format chain — is gated
  through `largefile.Thresholds` (`editor.Model.FeatureOff`). Past the base
  `files.large_file_kb`/`files.large_file_lines` cliff everything is off;
  the `files.large_file_<feature>_kb` keys switch single features off earlier.
  See the Large-file mode section in `/architecture/editor.md`.
- **Buffer splice fast path**: `buffer.Apply` used to rebuild the entire
  backing `[]string` on every edit — ~2 MB copied plus matching GC pressure
  per keystroke on a 120k-line buffer. A line-count-preserving edit (the
  typing case) now rewrites the touched lines in place; only edits that add or
  remove lines take the full splice. `BenchmarkKeystrokeLargeFile`
  (`internal/editor/keystroke_bench_test.go`) guards the result: an insert
  keystroke on a 120k-line degraded buffer dropped 407µs → ~11µs/op, cheaper
  than the small-file case (which still schedules a parse). The deterministic
  companion `TestKeystrokeAvoidsFullBufferWorkWhenLarge` asserts no change
  text ships and no parse schedules on the degraded path.
