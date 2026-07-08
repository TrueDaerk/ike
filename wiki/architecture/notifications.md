---
type: concept
title: Notifications
description: Toast notifications — host.Notify severities, expiry, stacking, Esc dismissal; SetStatus stays for persistent status segments.
resource: internal/app/notifications.go
tags: [architecture, notifications, host, ui]
timestamp: 2026-07-07T00:00:00Z
---

# Notifications

Roadmap 0130. Event-like messages ("saved 3 files", "server crashed") surface
as **toasts** — short, severity-colored lines stacked bottom-right directly
above the status line — instead of overwriting the status line. `SetStatus`
remains for *persistent state segments* (mode, LSP server state); everything
event-shaped goes through `Notify`.

## API

`host.API` carries `Notify(sev host.Severity, text string)` with severities
`Info`, `Warn`, `Error`. The `Host` queues notifications under a mutex (safe
from background goroutines); the root model drains the queue after **every**
Update pass (`Model.Update` wraps the dispatch switch in `updateMsg` and calls
`drainNotifications`), so a toast appears in the same frame its event
produced.

## Behavior

- **Expiry:** Info/Warn toasts expire via `tea.Tick` after
  `notifications.timeout_seconds` (default 4). Each toast carries a unique id;
  the expiry msg removes exactly that toast.
- **Errors persist** until the user presses `Esc`. Esc *passes through* — it
  dismisses error toasts and still performs its normal role (leaving insert
  mode, closing overlays), so it never costs an extra press.
- **Stacking:** newest on top, at most 3 rendered; older toasts surface as
  newer ones expire. The stack renders above the status line and never covers
  it.
- **Theming:** severity → palette slots (`Info`/`Warning`/`Error`) on the
  `Surface` background — light/dark aware without new theme slots.

## History & config (#78)

Every notification (toast-worthy or not) is recorded in a **ring of the newest
100** entries with timestamp and severity. The `notifications.history` registry
command (palette) opens the ring in the floating shell: newest first,
severity-colored, `HH:MM:SS` timestamps.

Config (typed section `[notifications]`, live-reloaded — the root model
re-feeds the host's config view on `ConfigReloadedMsg`):

- `notifications.timeout_seconds` (default 4, min 1) — info/warn toast lifetime.
- `notifications.min_severity` (`info` | `warn` | `error`, default `info`) —
  the toast floor: notifications below it go to the history only, never toast.

## Call-site migration (#79)

Every `SetStatus` call site was audited and classified. `SetStatus` now renders
as **one more segment** on the status line (after mode/file/diagnostics) — it
never replaces the line, fixing the sticky-message defect observed with the
example plugin's hook.

| Call site | Classification | Now |
|---|---|---|
| Example plugin (hello / handler opened / saw open) | event | `Notify(Info, …)` |
| Save-all (`SaveAllMsg`) | event | `Notify(Info, "saved N files")` (silent when nothing is dirty) |
| Theme select confirm / unknown-theme warning / reload warning | event | `Notify(Info/Warn, …)` |
| Startup theme warning | event | `Notify(Warn, …)` |
| LSP server ready / disabled / binary missing | persistent state | `SetStatus` (status-line segment) |
| LSP server crashed | event | `Notify(Warn, …)` |
| LSP server restarted (auto or `lsp.restart`) | event | `Notify(Info, …)` |
| LSP launch error / disabled after repeated crashes | event | `Notify(Error, …)` |

LSP classification travels with the message: `lsp.ServerStatusMsg` carries a
`ServerStatusKind` (`ServerState`, `ServerEventInfo/Warn/Error`) assigned where
the status originates (`internal/lsp/manager`); the root model routes state to
`SetStatus` and events to `Notify`.

