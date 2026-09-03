---
type: concept
title: Usage Telemetry
description: Local-only usage recording — command (with outcome), keybinding, layout, session, heartbeat, operation-lifecycle, palette-dismissal and project-time events appended as per-session JSONL under ~/.ike/telemetry, asynchronous and content-free, switched by telemetry.enabled.
resource: internal/telemetry/telemetry.go
tags: [architecture, telemetry, usage, jsonl, privacy, diagnostics]
timestamp: 2026-09-03T00:00:00Z
---

# Usage Telemetry

Issue #2235. IKE records how it is used — never *what* it is used on — into
local JSONL files, so questions like "which commands actually run, and how are
they invoked?" or "which chords do people press that have no binding?" can be
answered later with jq or a script. Nothing ever leaves the machine; there is
no upload path anywhere in the code.

## The hard privacy line

Events carry structure only: command ids, canonical chord strings, context
ids, layout operation names. **No typed text, no file contents, no clear-text
paths.** Two guards enforce it:

- The hook points in `internal/app` never pass content. Unresolved key
  presses are recorded only when they carry a command modifier (ctrl/alt/cmd)
  or are a function key (`recordableUnbound`, `internal/app/telemetry.go`) —
  plain and shift-only presses are typing and never reach the log, bound or
  not.
- Tests pin the line: the payload-key allowlist in
  `internal/telemetry/telemetry_test.go` and the plain-typing / clear-text
  path scans in `internal/app/telemetry_test.go`.

## Event schema (the analysis interface)

One JSON object per line. `v` is the schema version (`telemetry.SchemaVersion`,
currently 4); readers must tolerate unknown fields and filter on `v`.

```json
{"v":4,"ts":"2026-08-27T10:15:30.123Z","sid":"a1b2c3d4e5f6","type":"command","data":{"id":"editor.save","source":"keybind"}}
{"v":4,"ts":"2026-08-27T10:15:31.456Z","sid":"a1b2c3d4e5f6","type":"internal","data":{"id":"lsp.documentSymbols","source":"internal"}}
```

### Version history (what an analysis script must branch on)

| `v` | Issue | What changed |
| --- | ----- | ------------ |
| 1 | #2235 | The original `command`/`key`/`layout` types. Internal dispatches sit under `command` with `data.source == "internal"`. |
| 2 | #2304 | Internal dispatches move to their own `internal` type; nothing else changes. |
| 3 | #2348 | The diagnostic types `session`, `heartbeat` and `op` join; no existing field changes meaning. A v3 log ending without heartbeats is evidence of a hard stop, a v2 log ending simply predates them. |
| 4 | #2408 | Heartbeats slow from 10s to 60s (any per-hour rate must branch on `v`); `command`/`internal` events gain `ok` and `ms` when the dispatch failed or was slow; the types `palette.dismiss` and `project.leave` join; the v3 pseudo-command `palette.recentFiles.dismiss` (#2399, `data.qlen`) is gone — its successor is `palette.dismiss` with `data.query_len`. |

An export spanning versions therefore needs three guards: filter v1 `command`
events on `data.source != "internal"`, treat a missing `ok`/`ms` on v4 as
"succeeded, under 50 ms" (and as "unknown" below v4), and normalise heartbeat
counts by the version's interval before comparing sessions.

- `ts` — event time, UTC, millisecond RFC 3339.
- `sid` — random per-session id; also part of the file name.
- `type` + `data`:
  - `command` — a **user-triggered** command dispatch. `id` is the command
    id, `source` one of `palette`, `menu`, `keybind`, `mouse`. Since v4
    (#2408) the event also carries the *outcome*, but only when there is
    something to say: `ok` (`"true"`/`"false"`) and `ms` appear when the
    dispatch failed — an id nothing registers, the dispatch funnel's one
    failure mode — or when its synchronous part took at least
    `telemetry.CommandSlowThreshold` (50 ms). Their absence means "succeeded,
    under the threshold". `ms` measures the part that blocks the update loop;
    work the returned `tea.Cmd` does off-loop belongs to the `op` events.
  - `internal` — a command dispatched by an internal funnel, not the user
    (polling/background work — e.g. the structure panel's/breadcrumbs'
    `lsp.documentSymbols` refresh on cursor move or save). Same `data` shape
    as `command` (`id`, `source`, always `"internal"`); split into its own
    type (#2304) so a high-frequency poller like `lsp.documentSymbols` can't
    dominate a `type=="command"` query and skew top-command lists or the
    palette-vs-keybind ratio. **v1 compatibility**: files written before this
    split (`v":1`) carry these same dispatches under `type:"command"` with
    `data.source == "internal"` — a reader spanning both versions must filter
    v1 files on `data.source != "internal"` in addition to selecting
    `type=="command"` on v2+ files.
  - `key` — a keymap resolution. `chord` is the canonical chord string
    (`"cmd+k cmd+c"`), `context` the focus context it resolved in
    (`"editor[go]"`), `status` one of `resolved` (plus `command`), `blocked`
    (a documented blocked default) or `unbound` (no binding matched — the
    expected-but-missing-keybind signal, modifier/function keys only).
  - `layout` — a structural operation. `op` is one of `split`, `pane.move`,
    `pane.focus`, `resize`, `tab.switch`, `tab.move`, `project.switch`;
    `zone`/`direction` name an edge (`left`/`right`/`top`/`bottom`/`center`)
    where the op has one.
  - `session` (#2348) — the attribution anchor: `app` (Ike version), `os`
    (GOOS) and `project`, a 12-hex-char SHA-256 prefix of the project root
    path — never the path itself, but stable per project and re-computable
    from a candidate root, so a log can be matched to the project (and its
    `.ike/debug.log`) after the fact. Emitted at startup and again on every
    project switch; the token in effect is the last one recorded. Deferred
    like `pane.focus`, so it alone never creates a file, and it survives the
    pending trim.
  - `heartbeat` (#2348) — a liveness stamp every 60s
    (`telemetryHeartbeatInterval`, `internal/app/telemetry.go`; 10s before
    v4 — at that cadence the beats were 61% of a two-day export and buried
    the usage data they accompany) carrying
    `passes`, the cumulative update-loop pass count (`diag.LoopPasses`). It
    reads three ways: heartbeats continuing with a frozen count → the loop is
    stuck (or starved); continuing with an advancing count → the freeze sits
    outside the loop (input reader, renderer, terminal); stopping dead → the
    process ended. Since #2402 it also carries `top`: the interval's three
    loudest pass sources as `type:count` pairs
    (`app.termCheckMsg:5,view/render:5`), diffed from the always-on
    per-message-type counter (`diag.MessageCounts`) — the field that names
    an idle-wake culprit in an export without a local repro. Omitted when
    the interval was dead quiet, so a truly idle session stays cheap to
    store. Structure only, never content — message *type names* are code
    identifiers, not user data. The goroutine lives in the recorder, starts
    with the session file and never depends on the update loop. Cost: ~1 MB
    per day-long session, inside the 5 MiB cap.
  - `op` (#2348) — the lifecycle of a long-running operation. `id` names it,
    `phase` is `start`, `ok`, `error` or `canceled`, and every end phase
    carries `ms`. All of them are timed through one helper,
    `Recorder.OpTimer(id)` (#2403): it emits the `start` phase and returns the
    closer that stamps the elapsed time into the end phase, so no call site
    computes a duration of its own. A start without a matching end is the
    "it never came back" signal and is kept as exactly that. The ids:
    - `http.flight` — one HTTP dispatch (run, re-send, re-run). The end
      phases add `class` (`2xx`…`5xx`, when a response arrived) and `stream`
      (`true`/`false`). No URL, request key, header or body.
    - `project.switch` (#2403) — the seamless switch transaction
      (`performSwitchOpts`, `internal/app/switch.go`): persisting the
      departing project's session and layout, the chdir, parking the old
      workspace, and building, sizing and reconciling the new model. It ends
      `ok` when that model is ready, `error` when the chdir failed. The `ok`
      event carries `parked` (`true` when the target came back from the
      background set instead of being restored from disk), `panes` (the pane
      count of the model that was built) and `lsp`, which is `-1`: the
      incoming root's language servers publish long after the model is ready,
      so the warm-up is reported separately as an extra `lsp` phase on the
      same id, whose `ms` counts from the switch's start to the first
      `publishDiagnostics` for the new root. Deliberately a distinct phase
      and not a second `ok`, so the `start`/`ok` pairing that prices the
      transaction itself stays unambiguous. Before this the export held only
      the `layout` marker and the session marker, and the seconds between a
      switch and the next key press could not be split into work and thinking.
    - `project.close` (#2403) — closing the current project
      (`performCloseAndSwitch`, `internal/app/project_close.go`). Its span
      wraps a whole `project.switch` plus the departing workspace's teardown,
      so the two nest and the difference is what the teardown cost.
    - `session.restore` (#2403) — the startup layout/session restore
      (`restoreLayout` + `restoreSession` in `buildModel`), the one startup
      phase whose cost scales with what the user left open; `panes` is what
      came back. Only the startup model records it — a project switch builds
      its model on a recorder that is discarded right after — and like the
      session marker it never opens a session file on its own, so a launch
      that only restores and quits stays a ghost (#2318).
  - `palette.dismiss` (#2408) — a palette mode closed with esc instead of a
    pick, the one palette outcome that otherwise leaves no trace at all.
    `mode` is the mode's prefix rune (`":"`, `"@"`, `"%"`, …), `query_len` the
    number of runes typed — **never the query itself** — and `ms` how long the
    box stood open. Picks keep going through the ordinary command funnel, so
    the dismissal rate per mode is `palette.dismiss` over its opens. A mode
    that re-opens itself with a seeded query (the directory descend) starts a
    new open and is timed on its own.
  - `project.leave` (#2408) — how long a project was actually worked in.
    `project` is the same hashed token the session marker carries, `reason` is
    `switch`, `close` or `quit`, and `ms` is the **foreground** time since the
    matching session marker: the clock pauses while the terminal window
    reports itself blurred (`tea.BlurMsg`), so a project left open in a
    background tab overnight does not read as a night of work. Terminals that
    never report focus simply never pause it. Like the session marker it never
    opens a session file on its own (#2318) — a launch that only leaves again
    stays a ghost.

## Where events come from

All hooks sit at the existing funnels, so coverage is by construction:

- **Commands**: `dispatchCommandFrom` in `internal/app/app.go` — the single
  dispatch funnel (#679). Callers that know their origin pass it (palette
  `RunCommandMsg`, menu `RunMsg`, keymap resolution, status-line mouse
  clicks); everything else counts as `internal` and is recorded under the
  `internal` event type, not `command` (`Recorder.Command` in
  `internal/telemetry/telemetry.go` picks the type from the source).
- **Keys**: `resolveKeymap` (and the chord-timeout branch) in
  `internal/app/app.go` — resolved, blocked and unbound outcomes. With an
  editor focused the `unbound` verdict is deferred until the pane has seen the
  key (#2303): the editor owns editing chords the keymap table never lists
  (`alt+delete`, `alt+backspace`, `ctrl+u`, …), and `routeKey` records the
  event only when `editor.HandledLastKey()` says the editor ignored it too.
  Otherwise those keys drown the real missing-keybind signal.
- **Layout**: `SplitFocused`, `setFocus` (real focus transitions only),
  `commitMove`, divider drags and resize mode, `switchTab`/`moveTab`, and the
  project-switch transaction.
- **Session**: `recordTelemetrySession` (`internal/app/telemetry.go`) in the
  model constructor and after the project-switch chdir.
- **Palette dismissals**: `recordPaletteDismissal` (`internal/app/app.go`) runs
  after every palette `Update` in the overlay's key branch and takes the record
  the palette left behind (`Palette.TakeDismissal`, `internal/palette`), which
  carries the mode prefix, the query length and the open duration.
- **Project time**: `projectClock` (`internal/app/telemetry.go`) — a pointer on
  the model, because the model is copied by value on every Update pass — starts
  with `buildModel`, pauses/resumes on `tea.BlurMsg`/`tea.FocusMsg`, and is read
  by `recordProjectLeave` in the switch transaction (`internal/app/switch.go`,
  after the chdir committed and before the fresh model's clock starts; the
  departing token is taken *before* the chdir) and in the quit teardown. A
  project close runs through the same switch transaction with
  `switchOpts.closing`, which only changes the event's `reason`.
- **HTTP flights**: `dispatchHTTP` (start) and `fillHTTPPanel` via
  `recordHTTPFlightEnd` (`internal/app/http.go`) — the end phase derives from
  the flight entry's canceled mark and the response/error, the streaming flag
  is set when `HTTPStreamStartMsg` arrives.

## Storage, rotation, asynchrony

`internal/telemetry.Recorder` appends to one file per session,
`<UTC-timestamp>-<sid>.jsonl`, under `$IKE_CONFIG_DIR/telemetry` or
`~/.ike/telemetry` — the user layer, **not** the project's `.ike`, because a
session spans project switches (the recorder is session state and rides
across the model rebuild in `internal/app/switch.go`, like the notification
history).

- **Asynchronous**: `Record` enqueues onto a buffered channel; one writer
  goroutine does all disk I/O. A full buffer drops the event — the render
  loop never blocks and a usage log never disrupts the session. The recorder
  opens its file lazily on the first accepted event and is flushed/closed in
  the quit path.
- **No ghost sessions (#2318)**: a bare `layout`/`pane.focus` event does not
  open the file. The session restore moves focus from the explorer to the
  restored editor on every launch, so a start-and-quit would otherwise leave
  a file holding that single synthetic event — ten such ghosts out of sixteen
  files in one day. Deferred focus events are held in memory (capped at 32,
  oldest dropped) and written ahead of the first *meaningful* event, so real
  sessions keep the full chronology; a session that never produces one leaves
  no file. The rule lives in `startsSession`
  (`internal/telemetry/telemetry.go`) — everything except a bare pane focus
  starts a session.
- **Flush before risky work (#2348)**: `FlushSoon` enqueues a flush request
  without waiting for it — never blocking the update loop — and `dispatchHTTP`
  calls it right after the flight-start event, so everything up to and
  including that start (the dispatching `command` event too) is on disk
  before the exchange leaves. A dispatch that hangs the session still leaves
  its trace.
- **Periodic flush (#2295)**: the writer goroutine also holds a ticker
  (`Recorder.FlushInterval`, default 3s) and flushes the `bufio.Writer` on
  every tick, independent of buffer fill or an explicit `Flush()`/`Close()`
  call. It lives entirely inside the writer's own `select` over the event
  channel and the ticker channel, so it keeps running even if the
  render/update loop that would otherwise drive `Record` calls is frozen —
  events already enqueued reach disk within a few seconds instead of waiting
  on the next full buffer or session end. The ticker stops with the writer
  goroutine on `Close`; `newTicker` is a seam so tests can drive a fake tick
  channel instead of sleeping.
- **Bounded growth**: the session file is capped (5 MiB; past it the recorder
  stops writing), and opening a new session prunes the directory down to the
  newest 20 session files. The same pass deletes zero-byte session files left
  by launches killed before the writer flushed anything (#2318).
- **Switch**: `telemetry.enabled` (Settings → Usage Telemetry, default on) is
  read live per event, so a settings flip applies immediately; off, nothing
  is written at all. A nil recorder is inert, mirroring the frecency store.

## Analysis examples

```sh
jq -r 'select(.type=="command") | .data.source' ~/.ike/telemetry/*.jsonl | sort | uniq -c
jq -r 'select(.type=="internal") | .data.id' ~/.ike/telemetry/*.jsonl | sort | uniq -c | sort -rn
jq -r 'select(.data.status=="unbound") | .data.chord' ~/.ike/telemetry/*.jsonl | sort | uniq -c | sort -rn
# slow or failed dispatches (v4+)
jq -r 'select(.type=="command" and .data.ms) | [.data.id, .data.ok, .data.ms] | @tsv' ~/.ike/telemetry/*.jsonl
# dismissals per palette mode (v4+)
jq -r 'select(.type=="palette.dismiss") | .data.mode' ~/.ike/telemetry/*.jsonl | sort | uniq -c | sort -rn
# minutes per project (v4+)
jq -r 'select(.type=="project.leave") | [.data.project, (.data.ms|tonumber/60000|floor)] | @tsv' ~/.ike/telemetry/*.jsonl
```

Evaluation/statistics UI is explicitly out of scope; the JSONL schema above is
the stable interface.

Related: [Configuration System](/architecture/config.md),
[Settings UI & Menu Bar](/architecture/settings-ui.md),
[Keybindings](/architecture/keybindings.md).
