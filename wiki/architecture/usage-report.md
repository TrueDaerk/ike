---
type: concept
title: Usage Report
description: The singleton Usage pane (#2552) answers "what did I do" from the local usage log alone — Commands / Keys / Palette / Ops tabs over a Today / Week / Month period, a filter row, CSV export to a scratch; read-only and never uploaded.
resource: internal/usagepanel/usagepanel.go
tags:
  - tool-window
  - telemetry
  - keybindings
  - privacy
timestamp: 2026-09-08T18:00:00Z
---

# Usage Report (#2552)

`usage.toggle` (default `cmd+alt+u`, Tools ▸ Usage Report) opens the
singleton **Usage** pane at the bottom zone, following the Time pane's toggle
state machine (`internal/app/usage_panel.go`): open+focus, focus, return
focus. It is the second in-app reader of the usage log after the
[Project Time Report](/architecture/project-time.md): where that window
answers "how long, where", this one answers "what" — which commands run and
from where, which chords miss, how often each palette mode is dismissed, and
which operations cost the most. Every one of those questions needed jq on the
command line before.

Everything it shows is derived from `~/.ike/telemetry/*.jsonl` — read-only,
local, never uploaded. The aggregation (per-day buckets, the v1 internal
filter, the dismissal-rate definition, the op phase rules) lives with the log
it reads: [Usage Telemetry § usage aggregates](/architecture/usage-telemetry.md#usage-aggregates-what-did-i-do-2552).

## One read, two windows

The pane shares the Time window's `telemetry.Reader` and its last
`telemetry.Report` on the root model: one background read over the telemetry
directory fills both aggregates, `handleTimeReport` hands the result to
whichever of the two panes is open, and opening the Usage window while a
report is already on the model seeds it instantly. `r` in either pane (or
`usage.refresh` / `time.refresh`) re-reads for both.

## Tabs

| Tab | Rows | Columns |
| --- | ---- | ------- |
| **Commands** | one per command id, most dispatched first | total, keybind, palette, menu, mouse |
| **Keys** | one per (context, chord) that found no binding, most missed first | context, chord, misses, removed default (faint row when a user override unbound a default, #2539) |
| **Palette** | one per palette mode, most dismissed first | opens, picks, dismissed, rate, with query, no query, no results (fruitless searches), average open time |
| **Ops** | one per op id (slowest max first), then the slow / failed command dispatches as faint *dispatch* rows | started, ok, error, canceled, avg, max |

The period selector next to the tab bar picks Today / Week (7 days) / Month
(30 days), today included, exactly like the Time window's tabs.

## Keys

| Key | Effect |
| --- | ------ |
| `tab` / `shift+tab`, `h`/`l`, `←`/`→` | cycle the Commands / Keys / Palette / Ops tabs |
| `p` / `P` | cycle the Today / Week / Month period forwards / backwards |
| `j`/`k`, arrows, page keys, `g`/`G` | move the row cursor |
| `/`, the shared find chord (`cmd+f`) | open the filter row (free text over the row's name: command id, chord, mode, op id) |
| `n` / `N` (`cmd+g`) | step the filtered rows |
| `e` | export the current tab as CSV |
| `r` | re-read the usage log |

A click selects a row; a click on the header's tab bar switches the tab, one
on the period selector the period; the wheel scrolls the list.

## CSV export

`e` renders the *current tab* — the visible rows in display order — as CSV
and writes it to a **scratch file** (`scratch.CreateWithContent`), which then
opens in the editor. The first four columns name the tab and the period on
every row, so a concatenated export stays self-describing; the rest are the
tab's columns:

```
tab,period,from,to,context,chord,misses,removed_default
Keys,Week,2026-09-02,2026-09-08,editor[go],cmd+shift+z,3,
```

## Session restore

The pane is a singleton in the layout store under key `usage` and restores
**empty** in its saved slot, like the Time pane; the next read fills it.

## Commands

`usage.toggle` (`cmd+alt+u`, palette, Tools menu) and `usage.refresh` (pane
`r` / palette; ledgered in `cmd/ike/keybind_audit_test.go`). No setting: the
window needs only `telemetry.enabled` to have anything to read.

Related: [Usage Telemetry](/architecture/usage-telemetry.md),
[Project Time Report](/architecture/project-time.md),
[Scratch Files](/architecture/scratch-files.md),
[Keybindings](/architecture/keybindings.md).
