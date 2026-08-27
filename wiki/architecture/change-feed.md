---
type: concept
title: External-Change Feed
description: Session-scoped list of files changed by other processes (coding agents, git, formatters) with a mini-diff per entry and open / reload / revert actions, filtered by the watcher's own ignore rules
resource: internal/changefeed/changefeed.go
tags: [watch, agents, diff, revert, local-history, panel]
timestamp: 2026-08-20T00:00:00Z
---

# External-Change Feed

A coding agent (`claude` in a tool pane, a formatter, a `git checkout`) rewrites
project files while the user edits in parallel. The watcher already saw every
one of those writes — buffers reload from them (Roadmap 0140, #1515) — but
nothing said *what* had changed: the edits landed silently across the tree and
were discovered file by file.

The change feed (#2000) is that overview. Every external watcher event is
recorded into a session-scoped list, and `watch.changeFeed` ("Show External
Changes") raises a two-pane panel over it: the changed files newest-first on
the left, a mini-diff of the selected one on the right, with per-entry actions
to open, reload and revert.

## Data layer (`internal/changefeed`)

Pure data — no I/O, no clock, no config, so ordering, coalescing and the caps
stay testable without a filesystem. The caller supplies the timestamp, the
pre-change text and the noise predicate.

- **`Entry`** is one *file*, not one event: `Path`, `Time`, `Kind`
  (`Changed` / `Created` / `Removed`), `Count`, and the pre-change `Before`
  content with the `Origin` that produced it.
- **Coalescing** is per path. A repeated write moves the row back to the front,
  advances its timestamp, counts up, and **keeps the content it was first
  recorded with** — reverting an agent's fifth rewrite restores what the file
  held before the *first* one, which is what "undo what the agent did" means.
  Kinds merge like the watcher's: a removal wins, and a file created this
  session stays "created" however often it is rewritten afterwards. An entry
  first recorded without pre-change content adopts a later event's, because
  some content beats none.
- **Origin** records how far the revert can be trusted: `FromBuffer` (the open,
  unmodified buffer held exactly the bytes the write replaced), `FromSnapshot`
  (the newest local-history snapshot — what IKE last *wrote* there, so anything
  changed externally since is part of the diff), `NoBefore`, or `Dropped`.
- **Two caps.** `Limit` bounds the row count (default 200, oldest dropped
  first). `MaxBytes` bounds the *retained pre-change content* across all
  entries (4 MiB): past it the oldest entries release their content and their
  origin becomes `Dropped` — the row survives, the diff does not. Without the
  byte cap a batch touching large files would grow the session's memory with
  every write; without keeping the rows, the user would silently lose the
  knowledge that a file was touched at all.
- **`Ignore`** rejects noisy paths before they ever reach the list.

## Recording (`internal/app/changefeed.go`)

`recordChangeFeedBatch` runs from the root model's `watch.EventBatchMsg` case
(#2176) — `recordChangeFeed` from the single-event `watch.EventMsg` case —
**before** the events are routed onward: the auto-reload routing triggers
would otherwise overwrite the very buffer the pre-change content is read
from. In the batch path only the open-clean-buffer capture runs inline; the
local-history fallback resolves on a background goroutine (one command per
flush, entries land via `changeFeedCapturedMsg`), so a 300-file checkout
never costs 300 disk reads on the Update loop.

- Only file kinds are recorded. `DirChanged`, `GitChanged` and `ConfigChanged`
  name no project file the user edited.
- **IKE's own saves never appear.** The watcher drops them through its save
  epoch (`MarkSaved`, checked at ingest and again at flush); the feed re-asks
  `SavedRecently` before recording, so a save that reaches the model by another
  route (the poll fallback, a replayed message) cannot masquerade as an agent's
  write either.
- **Noise follows the watcher.** `watch.Ignored(root, path)` exports the rule
  `skipWatchDir` walks with — dot-directories and vendored noise
  (`node_modules`, `__pycache__`, `site-packages`, `vendor`) — judged on the
  segments *below* the watch root, so a project living under `~/.config` is not
  wholesale ignored. Exported rather than re-derived so the feed hides exactly
  what the watcher would never have descended into.
- **Pre-change content** comes from the open, unmodified buffer where there is
  one; a dirty or stale buffer holds text that was never on disk, so the newest
  local-history snapshot (#1023) is the fallback. A created file had no previous
  content at all.
- The feed lives on the root model, not on the panel, so it survives pane
  switches, panel closes and focus moves for the whole session.

## Panel

`watch.changeFeed` opens the floating two-pane panel (the shared `ui.Floating`
shell hosting a width-aware `ui.Content`, the Local History layout of #1969):

- **Left:** one row per file — kind marker (`~` / `+` / `-`), humanized age,
  file name, then its project-relative directory. The name comes before the
  directory because the column is truncated to half the panel and the name is
  the part that has to survive the cut. A repeated write shows `(×N)`.
- **Right:** the selected entry's mini-diff — its captured pre-change content
  against what the file holds *now* (the live buffer where one is open, the
  file on disk otherwise; a removed file's right side is empty). Rendered git
  style: `@@` hunk headers with three context lines, `+`/`-` markers, added
  green, removed red. An entry with no pre-change content renders the reason
  in place of the diff instead of notifying, so the selection can sweep across
  it without side effects.
- A detail line under the panes names the kind, the absolute timestamp, and
  where the "before" side came from.
- The list is **live**: the agent keeps writing while the panel is open, so a
  new event folds into the open list and the selection is re-found by path
  rather than sliding down as rows prepend.

Keys: `j`/`k` move (the mini-diff follows the selection), `enter` opens the
file, `d` sends the before/after pair to the reusable diff pane (#60), `R`
reloads the buffer, `r` reverts the external change, `x` dismisses the row, `c`
clears the feed, `esc`/`q` closes. An action that cannot apply — reverting a
created file, reloading one that is not open — notifies and leaves the panel
up; closing a modal only to toast a refusal would cost the user the list they
were reading.

The command is `watch.changeFeed`, reachable from the palette and from
**View → External Changes**.

## Actions

- **Open** (`enter`) lands the file in the last-focused editor pane through the
  ordinary open funnel. A removed file says so instead.
- **Reload** (`R`) re-reads the file into its open buffer. A clean buffer has
  usually reloaded itself already (`files.auto_reload`) and the reload no-ops on
  identical content; the action exists for the conflicted case — a dirty, stale
  buffer whose edits the user decided to drop.
- **Revert** (`r`) is **confirmed first**: reverting somebody else's write is
  destructive enough to ask, and the prompt is where the two shapes are spelled
  out.
  - An existing file is restored **into its buffer** through the local-history
    restore path (`ApplyTextEdits`, one history change): the buffer marks dirty,
    a single undo brings the external version back, and the file on disk is
    untouched until the next save. A revert is therefore never itself
    destructive. A file that is not open is opened first — the restore path
    edits a buffer.
  - A file **deleted** externally has no buffer to restore into, so the
    pre-change content is written back to disk and opened. The write stamps the
    watcher's save epoch, so the restore does not echo back into the feed.
  - An entry with no pre-change content (a created file, a released one) offers
    no revert; there is nothing to restore to.
- A reverted entry leaves the feed — it has been dealt with.

## Configuration

`files.change_feed_limit` (Settings → Files, "External-change feed size") caps
the list; **0 turns the feed off**, which clears it as well — an off switch
must not leave a stale list behind. The setting is read on every recorded
event, so changing it takes effect immediately, including trimming an
over-long existing list.

## Related

- [Foundation Slice](/architecture/foundation.md) — the watcher service, its
  debounce, save-epoch suppression and poll fallback (Roadmap 0140).
- [Local History](/architecture/local-history.md) — the snapshot store the feed
  falls back to for pre-change content, and the restore path revert reuses.
- [Diff Viewer](/architecture/diff-viewer.md) — the reusable pane `d` targets.
