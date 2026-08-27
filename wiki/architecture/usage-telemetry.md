---
type: concept
title: Usage Telemetry
description: Local-only usage recording — command, keybinding and layout events appended as per-session JSONL under ~/.ike/telemetry, asynchronous and content-free, switched by telemetry.enabled.
resource: internal/telemetry/telemetry.go
tags: [architecture, telemetry, usage, jsonl, privacy]
timestamp: 2026-08-27T00:00:00Z
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
currently 1); readers must tolerate unknown fields and filter on `v`.

```json
{"v":1,"ts":"2026-08-27T10:15:30.123Z","sid":"a1b2c3d4e5f6","type":"command","data":{"id":"editor.save","source":"keybind"}}
```

- `ts` — event time, UTC, millisecond RFC 3339.
- `sid` — random per-session id; also part of the file name.
- `type` + `data`:
  - `command` — a registered command dispatched. `id` is the command id,
    `source` one of `palette`, `menu`, `keybind`, `mouse`, `internal`.
  - `key` — a keymap resolution. `chord` is the canonical chord string
    (`"cmd+k cmd+c"`), `context` the focus context it resolved in
    (`"editor[go]"`), `status` one of `resolved` (plus `command`), `blocked`
    (a documented blocked default) or `unbound` (no binding matched — the
    expected-but-missing-keybind signal, modifier/function keys only).
  - `layout` — a structural operation. `op` is one of `split`, `pane.move`,
    `pane.focus`, `resize`, `tab.switch`, `tab.move`, `project.switch`;
    `zone`/`direction` name an edge (`left`/`right`/`top`/`bottom`/`center`)
    where the op has one.

## Where events come from

All hooks sit at the existing funnels, so coverage is by construction:

- **Commands**: `dispatchCommandFrom` in `internal/app/app.go` — the single
  dispatch funnel (#679). Callers that know their origin pass it (palette
  `RunCommandMsg`, menu `RunMsg`, keymap resolution, status-line mouse
  clicks); everything else counts as `internal`.
- **Keys**: `resolveKeymap` (and the chord-timeout branch) in
  `internal/app/app.go` — resolved, blocked and unbound outcomes.
- **Layout**: `SplitFocused`, `setFocus` (real focus transitions only),
  `commitMove`, divider drags and resize mode, `switchTab`/`moveTab`, and the
  project-switch transaction.

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
- **Bounded growth**: the session file is capped (5 MiB; past it the recorder
  stops writing), and opening a new session prunes the directory down to the
  newest 20 session files.
- **Switch**: `telemetry.enabled` (Settings → Usage Telemetry, default on) is
  read live per event, so a settings flip applies immediately; off, nothing
  is written at all. A nil recorder is inert, mirroring the frecency store.

## Analysis examples

```sh
jq -r 'select(.type=="command") | .data.source' ~/.ike/telemetry/*.jsonl | sort | uniq -c
jq -r 'select(.data.status=="unbound") | .data.chord' ~/.ike/telemetry/*.jsonl | sort | uniq -c | sort -rn
```

Evaluation/statistics UI is explicitly out of scope; the JSONL schema above is
the stable interface.

Related: [Configuration System](/architecture/config.md),
[Settings UI & Menu Bar](/architecture/settings-ui.md),
[Keybindings](/architecture/keybindings.md).
