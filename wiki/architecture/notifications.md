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

## Later increments (epic #71)

History ring + list view and config keys beyond the timeout (#78); migration
of event-like `SetStatus` call sites (#79).
