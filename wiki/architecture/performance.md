---
type: concept
title: Performance & Diagnostics
description: Idle-behavior rules (who may wake the render loop, and how often) and the opt-in runtime diagnostics hooks (IKE_PPROF endpoint, SIGUSR1 dumps).
resource: internal/diag
tags: [architecture, performance, pprof, idle, diagnostics]
timestamp: 2026-07-23T00:00:00Z
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
  backup, VCS refresh, keymap chord timeout) arm on demand and re-arm only
  while work is pending (`arm*Tick` + `*TickArmed` flags in `internal/app`).
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
- **Caches stay bounded**: the editor line cache clears past `lineCacheCap`
  (4096) and on every render-epoch bump; terminal render caches key by
  mutation version, not history.

## Diagnostics hooks (`internal/diag`, #1001)

Off by default; `diag.Start` in `cmd/ike/main.go` wires them:

- `IKE_PPROF=<addr>` (e.g. `localhost:6060`) serves `net/http/pprof`:
  `go tool pprof http://localhost:6060/debug/pprof/profile` for CPU,
  `/debug/pprof/goroutine?debug=1` for stacks.
- `SIGUSR1` writes `ike-<pid>-<time>-goroutines.txt` and `-heap.pprof` to
  `IKE_PPROF_DIR` (default: the OS temp dir) — the no-listener option for a
  session that is already misbehaving.

Long-session triage: dump goroutines at minute 1 and after an hour idle with
~10 mixed panes; a growing count names the leaking loop, a flat count with
rising CPU points at wakeups (profile 30s of "idle" CPU and look for View/
render frames). `TestSessionCloseLeavesNoGoroutines` pins the terminal
session lifecycle as a regression test.
