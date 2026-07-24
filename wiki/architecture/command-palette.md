---
type: concept
title: Command Palette
description: Centered floating overlay fronting every action — a prefix-dispatched mode system (":" runs registry commands context-ranked, "@" fuzzy-finds files, locked recent-files and search-everywhere modes behind cmd+e / cmd+shift+a), pure presentation that dispatches tea.Msgs and executes nothing itself.
resource: internal/palette/palette.go
tags: [architecture, palette, overlay, fuzzy, modes, bubbletea]
timestamp: 2026-07-24T00:00:00Z
---

# Command Palette

Roadmap 0070. A single modal overlay that fronts every action in IKE. It opens
centered over the layout (esc-esc, or a configured `palette.toggle_key`) and reads a leading **prefix rune**
that selects a **Mode**: `:` runs registered **Commands**, `@` fuzzy-finds
**files**, and a locked-only **directory mode** (`dir_mode.go`, no user-facing
prefix) is the target picker behind `file.move` (#175), emitting a
`MoveTargetMsg` the root model combines with the pending source path.
The chosen result is dispatched as a `tea.Msg` the root model applies;
the palette executes nothing itself. The prefix system is built to grow — adding
a mode is registering one more `Mode`, the core stays prefix-agnostic.

Like the [Help Overlay](/architecture/help-overlay.md), the palette is a pure
**consumer** of the plugin registry (roadmap 0020): command mode never caches a
command list beyond a per-open snapshot. Unlike help, the palette takes typed
input and ranks results live, so it is its own overlay model rather than a
`ui.Content` in the read-only floating shell (the shell treats every non-dismiss
key as a scroll key).

## Structure

```
internal/fuzzy/            reusable matcher: optimal-alignment score + matched rune spans for highlighting
internal/palette/
  palette.go               overlay tea-model: open/close, input line, ranked list, key nav, esc-dismiss, render
  mode.go                  Mode interface (Prefix/Placeholder/Results) + Item + activation msgs
  command_mode.go          ":" mode — snapshot registry, fuzzy-filter, context-first ranking
  file_mode.go             "@" mode — fuzzy file finder over the project tree (cached walk)
  recent_mode.go           locked recent-files mode — injected MRU list, active file excluded
  search_mode.go           locked search-everywhere mode — composes command + file modes, per-kind cap
  context.go               Context captured at open (focused pane context id + project root + active file)
internal/app/              root model hosts the palette, toggles it, forwards keys, renders on top
```

The root model (`internal/app`) holds a `*palette.Palette`. While open it forwards
every key to `palette.Update` and composites `palette.View()` on top. On **enter**
the palette returns a `tea.Cmd` emitting the mode's result message and closes; the
root applies it: `RunCommandMsg` → `RunCommand(id)`, `OpenFileMsg` → the normal
open-file path.

### Opening

Four entry points, all from a non-capturing context:

- **Toggle key** (config `palette.toggle_key`, default **empty** since #523 —
  `ctrl+p` now belongs to `lsp.parameterInfo`; set the key to restore a
  dedicated chord) — `Open` centered for the focused pane's context.
- **esc-esc** — two consecutive `esc` presses outside a text-capturing editor
  mode open the centered palette (the first esc is still forwarded, so it keeps
  its normal-mode meaning); any other key resets the pending state.
- **`@` in an editor's normal mode** — opens a slimmed, **file-only** palette
  *anchored* over the editor pane via `OpenAnchored(cx, '@', x, y, w)`. The root
  composites it with `overlay.Place` at the pane's interior top-left rather than
  `overlay.Center`.
- **`project.goToFile`** (default `cmd+shift+o`, or from the palette itself) —
  opens the **centered** palette locked to the `@` file mode via
  `OpenLocked(cx, '@')`, so go-to-file works from any context, not just an
  editor pane.
- **`palette.recentFiles`** (default `cmd+e`, Navigate menu) — opens
  the centered palette locked to the recent-files mode (below).
- **`palette.searchEverywhere`** (default `cmd+shift+a` / double-shift,
  #263) — opens the centered palette locked to the search-everywhere mode
  (below).

A palette can be **locked** to a single mode (no prefix switching): the anchored
editor finder and the go-to-file open are locked to `@`, so a typed `:` is part
of the query, not a mode switch. The plain centered palette is unlocked and
switches freely.

## Modes & prefix dispatch

A `Mode` declares its `Prefix()` rune, a `Placeholder()` hint, and
`Results(query, Context) []Item` returning a fully ranked list (best first). The
palette stores the raw query including its prefix; `mode()` resolves it: a leading
rune that names a registered mode selects that mode and strips it from the query
body; otherwise the **default mode** (config `palette.default_mode`, default `:`)
ranks the whole query. Each `Item` carries the `tea.Msg` it activates, so the
palette dispatches without knowing what any mode does.

The query is a full single-line editor (`internal/ui.EditKey`, #763): a rune
cursor over the raw query (prefix included) — arrows/home/end move it,
`alt+left`/`alt+right` jump words, `alt+backspace`/`alt+delete` delete words,
`delete` removes forward, typed and pasted text insert at the cursor.
`queryView` renders the cursor inside the prefix-stripped body; the finder's
input fields share the same helper.

## Command mode (`:`)

Snapshots `registry.Commands()` per open (the registry is the single source of
truth), fuzzy-filters each command's `Title` (falling back to its id so `:hello`
finds `example.hello`), and ranks **context-first**:

1. **in-context** — pane scope equal to the focused context id,
2. **global** — `Scope.Global`,
3. **off-context** — scoped to a different context (ranked last, or hidden when
   `palette.off_context = "hide"`).

Within a tier, higher fuzzy score wins, then **most-used** (#773), then title.
The usage counter (`usage.go`, persisted per project in `.ike/cmdusage.json`,
`IKE_CONFIG_DIR`-redirectable) counts only selections confirmed **from the
palette window** — the root model bumps it on `palette.RunCommandMsg`, a path
keybind invocations never take — so shortcut users don't skew the listing. On
an empty query all scores tie, so the listing opens most-used-first; a typed
query's match quality still wins over usage. Search everywhere inherits the
order through its composed command source. The dim detail shows the
command's resolved key binding (`registry.Binding`), else its documentation-only
`Shortcut`, else its owner. Context-aware filtering relies on the additive
`Scope` field on `plugin.Command` (`plugin.GlobalScope()` / `PaneScope(ctxID)`),
the same field [help](/architecture/help-overlay.md) groups by.

## File mode (`@`)

A fuzzy file finder over the project tree. It matches the query against each
file's path **relative to the root, directory segments included**, so `@app/app`
finds `internal/app/app.go` the way a JetBrains/Claude-Code file picker does —
the fuzzy matcher's word-boundary bonus rewards matches at path separators. The
disk walk is cached per-root (filtered on every keystroke, walked once), skips
hidden entries and heavy directories (`.git`, `node_modules`, `vendor`), uses
forward-slash paths for stable matching, and is capped at `maxFiles`. Activation
emits `OpenFileMsg{Path}` joined onto the root.

## Open-path mode (`file.openPath`, #999)

The "Open File…" picker (`openpath_mode.go`) opens files **outside the
workspace** — configs in `~/`, logs under `/var` — without switching projects.
Locked-only (prefix `;` is internal): the `file.openPath` command (palette /
File menu) opens it centered. Candidates come from the shared
`internal/pathcomplete` engine (`Complete`, files + dirs, `~` expanded): file
rows activate the normal `OpenFileMsg` open path (out-of-root buffers behave
like jumped-to dependency files, #565 — full editing/LSP, no explorer entry);
directory rows emit `OpenPathDescendMsg`, which re-opens the picker with the
accepted directory as the query (`OpenLockedWith`), so enter descends like
tab. Tab completes via the `Completer` seam. With no matching candidate the
raw query stays activatable — a missing file surfaces as an error toast
(`openInTab` now reports load failures instead of failing silently). An empty
query seeds `~/` and `/`.

## Recent-files mode (`cmd+e`, Roadmap 0230)

JetBrains' Recent Files popup, palette-style (`recent_mode.go`). The mode is
locked-only (its prefix rune is internal, never typed): `palette.recentFiles`
opens it centered via `OpenLocked`. The palette owns no MRU store — the list
func is injected by the root model (`internal/app/recent.go`), which touches a
path on every file open (`openPath`) and tab activation, deduplicates
(touch moves to front) and caps at 50. Every entry carries its **last-opened
timestamp** (#1113), stamped on touch. The list persists as `recent_files` in
`.ike/session.json` beside the rest of the session state — as `{path, ts}`
objects since #1113; the pre-#1113 bare-string-array shape still loads
(timestamps migrate as zero and render no time). The MRU reloads from the
session file on **every** startup path (#1112), including the
resumed-workspace path of a project switch, which skips the rest of
`restoreSession` — before the fix that path started empty and the next
session save wiped the persisted history.

With an empty query the items keep MRU order — most recent first — with the
**currently active file excluded**, so `cmd+e` + `enter` jumps to the previous
file (the `Context.ActivePath` field carries the exclusion). A query
fuzzy-matches the project-relative path; equal scores keep MRU order. Files
that vanished from disk are dropped from the listing. Activation emits the same
`OpenFileMsg` as the `@` mode. Each row shows its relative last-opened time
(`ui.RelTime`: "just now", "5m ago", …) right-aligned in the `Item.Time`
column (#1114 layout, see below), and carries an aux action mirroring the
project picker's #842 prune: `shift+delete` on the selected row or a click on
its right-pinned `✕` zone emits `RemoveRecentFileMsg{Path}` — the root model
removes the entry from the MRU, persists the session immediately and
refreshes the still-open palette.

### Row layout: the right-aligned time column (#1114)

`Item.Time` is a generic palette field: rows render `marker + title (+ badge)
… detail chip + time + ✕`, with the time pinned to the right (two cells of
separation from the title/detail, one before the `✕`). At narrow widths the
**title truncates first** (ellipsis) so the time and the `✕` zone stay
intact; when the title would fall below `minRowTitleW` (8 cells) the time
column drops entirely so the name stays readable. `sideRow` (the Recent
Projects column) applies the same rules, so both pickers match.

### Recent Projects column (#778)

The locked Recent Files dialog renders a second, left column listing
`project.history` (current project excluded), through the generic `SideMode`
extension (`recent_mode.go`): a locked mode implementing
`SideTitle`/`SideResults` gets the two-column layout. `tab` toggles the
column focus (plain `left`/`right` switch too while the query is empty;
with text they stay cursor keys), `up`/`down` navigate the focused column,
and `enter` on a project emits `project.PickedMsg` — the normal validated
path into the seamless workspace switch (#777), so terminals and runs keep
running. The query fuzzy-filters both columns at once. Anchored palettes
and search everywhere never show the column.

**Automatic focus placement (#819).** On open and after every query edit the
column focus follows the best match: an empty files list starts the focus on
the projects column (fresh project, `enter` reopens the previous project),
and a query whose top project strictly outscores the top file — or matches
only projects — shifts the focus there, so `enter` opens the best hit. Files
win ties, and an empty query with any recent files starts on the files
column as before. An explicit column switch (`tab`, empty-query arrows, or a
click) overrides the automatic placement until the query changes again.

## Search-everywhere mode (`cmd+shift+a` / double-shift, Roadmap 0230)

JetBrains' Search Everywhere, palette-style (`search_mode.go`). Locked-only like
the recent-files mode (`palette.searchEverywhere` opens it via `OpenLocked`);
`shift shift` resolves through the ordinary multi-step chord engine, so it works
off macOS too (it needs key-up reporting, hence the palette as the universal
escape). One query is ranked across **commands and files** by *composing* the
already-built `CommandMode` and `FileMode` — no duplicated ranking. Each
source's top rows (per-kind cap, `searchAllPerKind`) interleave by fuzzy score,
ties keeping commands first; every row is retitled with its source's prefix
glyph (`:` / `@`, match spans shifted alongside) so the kind is visible, command
rows keep their binding chip, file rows their project-relative path. Activation
dispatches whatever the underlying item carries (`RunCommandMsg` /
`OpenFileMsg`). An **empty query lists the recent files first** (MRU order,
active file excluded — the same injected source as the recent-files mode)
followed by the command listing; a fresh session without MRU history falls
back to the plain listing (#263). The workspace-symbol mode holds its
reserved seat (#295): a **live source** — `palette.LiveMode`, re-queried
per settled keystroke through the debounce plumbing (`live.go`), its cached
rows composed and capped like any other source.

## Resizing (#774)

`ctrl+shift+left/right` widen/narrow the centered box (the width delta feeds
`boxWidth()` before its floor/room clamps; anchored palettes ignore it) and
`ctrl+shift+up/down` grow/shrink the visible result rows
(`visibleRows() = maxResults + delta`, floored at 3). Deltas persist in the
shared per-project `winsize.json` store (kind `"palette"`), so Run a Command,
Search Everywhere, Recent Files and the go-to modes share one remembered
size. Handled before the plain-arrow selection keys, which match on the key
code alone. **Mouse resize** (#933): pressing the centered box's border ring
starts a drag — edges resize one axis (left/right → width columns, top/bottom
→ result rows), corners both; deltas nudge the same store un-persisted per
motion step and flush on release. Anchored palettes are not mouse-resizable
(their geometry follows the anchor). **Width cap** (#932,
`ui.popup_max_width`, default 110, 0 disables): on large terminals the
centered box's default width stops at the cap and extra terminal width just
adds margin; the user's #774 delta applies on top of the capped base and
still clamps to the terminal.

## Fuzzy matching

`internal/fuzzy` is pure and dependency-free: `Match(pattern, text) (Result, ok)`
returns a score and the matched **rune indices**, so ranking and highlighting use
the exact same spans. Matching is case-insensitive subsequence with an **optimal
alignment** (a small dynamic program), not a greedy left-to-right scan: a pattern
binds to word-boundary and consecutive runs when they exist rather than to the
earliest positions. Scoring rewards, strongest first, boundary matches, then
consecutive runs, then a start anchor; it penalises gaps and a long unmatched
lead. An empty pattern matches everything with a zero score.

## Rendering

The box is compact: centered it is half the terminal width clamped to a readable
floor (`minBoxWidth`); anchored it tracks the host pane's width down to a smaller
floor (`minAnchorWidth`). Each result row shows the highlighted title on the left
and the command's key binding as a **highlighted chip pinned to the right** (a
key-cap style, distinct from the dim matched-character accent). The title is
truncated first, so the binding chip is never dropped on a narrow box.

## Configuration

`[palette]` config (read once at construction, flattened through `host.Config`):

- `max_results` — result rows shown (default 12; the list scrolls past it),
- `default_mode` — prefix used when none is typed (default `:`),
- `off_context` — `"rank"` (last) or `"hide"` for off-context commands,
- `toggle_key` — dedicated open key (default empty since #523; esc-esc stays).

The toggle key is a binding-agnostic default; the final keymap (and the `:`/`@`
discoverability, the project-switch command's appearance) is owned by roadmaps
0080 / 0090.

## Design rules

- **Registry is the source of truth.** Command mode holds only a per-open
  snapshot; no parallel command store.
- **Modes are pluggable by prefix.** The core is prefix-agnostic; a new mode is
  one more registered `Mode`.
- **Presentation + routing only.** The palette dispatches `tea.Msg`s and executes
  nothing; owners (editor, explorer, projects) handle them.
- **Dismissable and non-destructive.** `esc` closes with no side effects;
  `↑`/`↓`/`ctrl+p`/`ctrl+n` navigate; `enter` activates.

## Boundaries

- Defining editor/explorer commands and the keybindings (incl. the toggle key) is
  owned by the feature roadmaps and 0080.
- The project-switch command merely *appears* here; its logic is 0090.
- Symbol/line/diagnostic prefixes are future modes the `Mode` interface leaves
  room for; only `:` and `@` ship here.
