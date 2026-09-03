---
type: concept
title: Project Time Report
description: The singleton Time pane (#2426) reports active time per project and day from the local usage log alone — Today/Week/Month tabs, sessions and top commands per row, a per-day ASCII bar, a filter row, CSV export to a scratch, and an opt-in status-line segment; read-only and never uploaded.
resource: internal/timepanel/timepanel.go
tags:
  - tool-window
  - telemetry
  - time-tracking
  - privacy
timestamp: 2026-09-03T00:00:00Z
---

# Project Time Report (#2426)

`time.toggle` (default `cmd+alt+0`, Tools ▸ Project Time Report) opens the
singleton **Time** pane at the bottom zone, following the Problems pane's
toggle state machine (`internal/app/time_panel.go`): open+focus, focus,
return focus. It answers "how long did I work on X today / this week / this
month" from the log IKE already writes, so the question needs no external
tracker and no second tool.

Everything it shows is derived from `~/.ike/telemetry/*.jsonl` — read-only,
local, never uploaded. How the aggregation works (span rules, the idle-gap
removal for pre-v4 logs, the mtime cache, the token→name join) lives with the
log it reads: [Usage Telemetry](/architecture/usage-telemetry.md).

## What a row says

One row per project, most active first:

```
 ike                          3h 12m    4 sess  editor.save×61, lsp.gotoDefinition×22, …
 site                           47m     1 sess  editor.save×9
 (unknown)                       8m     2 sess
```

- **Active time** is the span rule's answer for the selected range, not wall
  clock: background time (v4 logs) and idle gaps over five minutes (older
  logs) are already out.
- **Sessions** counts the session markers in the range — one per launch and
  one per switch *into* the project, so a day of ping-ponging reads as several.
- **Top commands** are the five most dispatched command ids in the range, the
  cheapest available answer to "what was the work".
- **`(unknown)`** groups every token no known project path hashes to.

Below the list, the selected project's range is broken down as a per-day
ASCII bar chart, scaled against its own busiest day; a non-zero day always
draws at least one block, so "a little" never renders as "nothing". On a
short pane the chart is dropped — the list is the primary content.

## Keys

| Key | Effect |
| --- | ------ |
| `tab` / `shift+tab`, `h`/`l`, `←`/`→` | cycle Today / Week / Month |
| `j`/`k`, arrows, page keys, `g`/`G` | move the project cursor |
| `/`, the shared find chord (`cmd+f`) | open the filter row (`project:`, free text over the name) |
| `n` / `N` (`cmd+g`) | step the filtered rows |
| `e` | export the current view as CSV |
| `r` | re-read the usage log |

A click selects a row; a click on the header's tab bar switches the range;
the wheel scrolls the list.

## CSV export

`e` renders the *current view* — the visible rows of the selected tab, in
display order — as CSV and writes it to a **scratch file**
(`scratch.CreateWithContent`), which then opens in the editor. Columns:

```
range,from,to,project,token,active,active_seconds,sessions,top_commands
Week,2026-08-28,2026-09-03,ike,4f3a1c9d2b08,3h 12m,11520,4,editor.save=61 lsp.gotoDefinition=22
```

The range is repeated on every row, so concatenating several exports stays
self-describing. Names containing commas or quotes are quoted the usual way.
A scratch is the target on purpose: the export lands somewhere the session
already manages instead of in an arbitrary directory.

## Status-line segment

`statusline.project_time` (enum `on`/`off`, **default `off`**, Settings ▸
Usage Telemetry) adds a `⏱ 3h 12m` segment showing **today's** active time in
the current project. Clicking it opens the Time window.

It is opt-in on purpose: a permanent clock on the status line is a
working-hours display, and whether that reads as motivating or as oppressive
is not something an IDE gets to decide for anyone. While the setting is off
nothing is armed at all — no directory scan happens. While it is on, one
background read runs at Init and a 60-second ticker refreshes it; the segment
stays hidden until the first read lands and on a day with nothing recorded,
because a segment reading `0m` all morning is noise rather than information.
The number needs `telemetry.enabled` to have anything to read.

## Session restore

The pane is a singleton in the layout store under key `time` and restores
**empty** in its saved slot, like the other tool windows whose content is
derived state; the next read fills it.

## Commands and settings

`time.toggle` (`cmd+alt+0`, palette, Tools menu) and `time.refresh` (pane
`r` / palette; ledgered in `cmd/ike/keybind_audit_test.go`). The one setting
is `statusline.project_time`.

Related: [Usage Telemetry](/architecture/usage-telemetry.md),
[Status Line](/architecture/status-line.md),
[Scratch Files](/architecture/scratch-files.md),
[Settings UI & Menu Bar](/architecture/settings-ui.md).
