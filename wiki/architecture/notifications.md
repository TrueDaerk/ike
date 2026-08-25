---
type: concept
title: Notifications
description: Toast notifications — host.Notify severities, expiry, stacking, Esc dismissal; the prominent forge event dialog, the status-line unread badge and the per-event-kind style setting; SetStatus stays for persistent status segments.
resource: internal/app/notifications.go
tags: [architecture, notifications, host, ui, forge]
timestamp: 2026-08-24T00:00:00Z
---

# Notifications

Roadmap 0130. Event-like messages ("saved 3 files", "server crashed") surface
as **toasts** — short, severity-colored lines stacked bottom-right directly
above the status line — instead of overwriting the status line. `SetStatus`
remains for *plugin-set persistent status segments*; LSP server state is
tracked per language and scoped to the focused buffer (#380); everything
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

The ring is session state, not workspace state (#1514): it survives a seamless
project switch (the root model carries `history` and the unseen counter into
the rebuilt model). Each entry records the project root it was emitted in;
the view labels entries from other projects with a dimmed `[project]` suffix,
while current-project entries stay unlabeled.

The status line shows an unseen-count segment (`● N`, #101): entries recorded
since the history view was last opened; opening it resets the counter. See
[Status Line Segments](/architecture/status-line.md).

Config (typed section `[notifications]`, live-reloaded — the root model
re-feeds the host's config view on `ConfigReloadedMsg`):

- `notifications.timeout_seconds` (default 4, min 1) — info/warn toast lifetime.
- `notifications.min_severity` (`info` | `warn` | `error`, default `info`) —
  the toast floor: notifications below it go to the history only, never toast.

## Prominent forge events (#2086)

A toast is the wrong surface for something as actionable as a new issue
appearing on the forge — it stacks bottom-right and expires. The forge poller's
typed events (`forge.EventsMsg`, see
[Forge Layer](/architecture/forge.md)) therefore pick their surface per event
kind, in `internal/app/forgenotify.go`:

- **dialog** — a centered, bordered, dismissable dialog over the workspace,
  hosted in the floating shell like the other app dialogs. It shows the number,
  title, author and labels, and answers to `enter` (open in the issues window),
  `d`/`esc` (dismiss), `j`/`k` (walk the queue) and `a` (dismiss all).
- **badge** — a persistent `● 2 new issues` segment in the status line. Unlike
  a toast it never expires: it stays until the events are viewed.
- **toast** — the ordinary notification above.
- **off** — the history entry only.

**One dialog, never a stack.** Pending events collapse into a single dialog
whose heading carries the count (`Forge events (3)`); an event arriving while
it is open grows the queue instead of opening a second dialog.

**Do-not-interrupt guard.** A key press landing in an editor or terminal stamps
`lastInputAt`; within `forgeTypingWindow` (3 s) a dialog would land mid-word, so
it is held back and shown as the badge instead. The same holds when another
overlay owns the floating shell or the keyboard — an event never steals an open
help view or prompt. Opening the issues window, or the dialog itself, counts as
viewing: the badge clears and its events join the one dialog.

**Nothing is lost.** Every event is recorded in the history ring exactly once —
directly for the dialog/badge/off styles, through `Notify` for the toast style —
so a dismissed dialog is still reviewable in `notifications.history`.

The dialog's open action jumps straight to the issue's **detail view** in the
issues tool window (`ghissues.Reveal`, filters dropped); when the pane's listing
does not carry the issue yet, the reveal runs on the next fetch. A pull-request
event has no pane of its own and opens in the browser.

Config (typed section `[forge.notify]`, one key per event kind, all editable in
Settings → Forge Notifications): `issue_opened` (default `dialog`),
`issue_closed`, `pr_opened`, `pr_merged`, `pr_closed` (default `toast`) and
`pr_checks_failing` (default `badge`). An unknown value falls back to that
kind's default with a config diagnostic.

## Call-site migration (#79)

Every `SetStatus` call site was audited and classified. `SetStatus` now renders
as **one more segment** on the status line (after mode/file/diagnostics) — it
never replaces the line, fixing the sticky-message defect observed with the
example plugin's hook.

| Call site | Classification | Now |
|---|---|---|
| Save-all (`SaveAllMsg`) | event | `Notify(Info, "saved N files")`, `"nothing to save"` on a no-op (#275) |
| Theme select confirm / unknown-theme warning / reload warning | event | `Notify(Info/Warn, …)` |
| Startup theme warning | event | `Notify(Warn, …)` |
| LSP server ready / disabled / binary missing | persistent state | per-language status-line segment (see below) |
| LSP server crashed | event | `Notify(Warn, …)` |
| LSP server restarted (auto or `lsp.restart`) | event | `Notify(Info, …)` |
| LSP launch error / disabled after repeated crashes | event | `Notify(Error, …)` |

LSP classification travels with the message: `lsp.ServerStatusMsg` carries a
`ServerStatusKind` (`ServerState`, `ServerEventInfo/Warn/Error`) assigned where
the status originates (`internal/lsp/manager`); the root model routes events to
`Notify`.

## Per-language server segment (#380)

`ServerState` messages carry `Lang`; the root model records them in a
per-language map instead of the global host status. The status line's server
segment renders only the *focused buffer's* language entry (`lang.ByPath` on
the editor's path), so "gopls not found" never follows the user into a
plain-text buffer, and each buffer reflects its own server's current state.
Buffers whose language has no tracked server state show no server text.
`host.SetStatus` stays as the plugin-facing global segment (WASM `set_status`
capability).

