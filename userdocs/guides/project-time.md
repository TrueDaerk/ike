# Project time

IKE can tell you how long you worked on a project today, this week or this
month — without an external time tracker, and without anything leaving your
machine. The numbers come from the usage log IKE already writes for itself.

Open the report with **Project Time Report** (`cmd+alt+0`, or Tools ▸ Project
Time Report, or the command palette). It opens as a pane at the bottom.

## Reading the report

```
 Time   Today Week Month     3h 59m active

 ike                          3h 12m    4 sess  editor.save×61, lsp.gotoDefinition×22
 site                           47m     1 sess  editor.save×9
 (unknown)                       8m     2 sess

 ike — per day
 2026-08-28 ███████████                        1h 5m
 2026-08-29                                       0s
 …
```

- **Active** is time you were actually working, not time the project was open.
  Stretches where the terminal window was in the background do not count, and
  neither does a pause longer than five minutes with no keys and no commands.
  A long lunch with the editor open does not become billable time.
- **sess** counts how many times you started or switched into that project in
  the range — a day of jumping back and forth shows several.
- The last column is the five commands you ran most in that project, which is
  usually a decent one-line summary of what the work was.
- **(unknown)** collects projects IKE cannot name any more — the log stores a
  hash of the project path, never the path itself, so a project you have since
  removed from the recent list can no longer be matched back to a name.

Under the list, the selected project's range is broken down per day as a bar
chart. Move the selection with `j`/`k` to switch which project it shows.

## Keys

| Key | What it does |
|---|---|
| `tab`, `shift+tab`, `←`/`→` | switch between Today, Week and Month |
| `j` / `k`, arrows, page keys | move between projects |
| `/` or `cmd+f` | filter — type a project name, or `project:ike` |
| `e` | export what you are looking at as CSV |
| `r` | re-read the log |

You can also click a project row, or click *Today* / *Week* / *Month* in the
header.

## Exporting

`e` writes the visible rows of the current tab to a CSV
[scratch file](scratch-and-snippets.md) and opens it:

```
range,from,to,project,token,active,active_seconds,sessions,top_commands
Week,2026-08-28,2026-09-03,ike,4f3a1c9d2b08,3h 12m,11520,4,editor.save=61 lsp.gotoDefinition=22
```

Since the range is on every row, you can concatenate several exports and still
know what each row is. From there it is a spreadsheet or an invoice like any
other CSV.

## Today's time in the status line

If you want the number without opening the pane, switch on **Project time in
the status line** (Settings ▸ Usage Telemetry, or
`statusline.project_time = "on"`). A `⏱ 3h 12m` segment then shows today's
active time in the current project, updated about once a minute; clicking it
opens the report.

It is off by default on purpose — a permanent clock on the status line is a
working-hours display, and that is a choice, not a default.

## Where the numbers come from

The report reads the per-session JSONL files under `~/.ike/telemetry` that the
local usage recording writes (Settings ▸ Usage Telemetry ▸ **Local usage
telemetry**, on by default). Those files record structure only — which
commands ran, which chords resolved, which project (as a hash) was open for
how long — never typed text, file contents or paths, and nothing is ever
uploaded anywhere. The report only reads them.

Two consequences worth knowing:

- With `telemetry.enabled` off, nothing is recorded, so the report stays empty
  for that period. Switching it back on does not recover the gap.
- The log is capped: IKE keeps the newest 20 session files, so a very old
  month may be partly gone.

If you would rather have no record at all, switch telemetry off — the report
simply has nothing to show, and neither does anyone else.
