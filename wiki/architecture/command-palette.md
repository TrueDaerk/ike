---
type: concept
title: Command Palette
description: Centered floating overlay fronting every action — a prefix-dispatched mode system (":" runs registry commands context-ranked, "@" fuzzy-finds files, locked recent-files and search-everywhere modes behind cmd+e / cmd+shift+a), pure presentation that dispatches tea.Msgs and executes nothing itself.
resource: internal/palette/palette.go
tags: [architecture, palette, overlay, fuzzy, modes, bubbletea]
timestamp: 2026-07-10T00:00:00Z
---

# Command Palette

Roadmap 0070. A single modal overlay that fronts every action in IKE. It opens
centered over the layout (default `ctrl+p`) and reads a leading **prefix rune**
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

- **Toggle key** (config `palette.toggle_key`, default `ctrl+p`) — `Open` centered
  for the focused pane's context.
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
- **`palette.recentFiles`** (default `cmd+e`, leader `m`, Navigate menu) — opens
  the centered palette locked to the recent-files mode (below).
- **`palette.searchEverywhere`** (default `cmd+shift+a` / double-shift, leader
  `A`, **`space space`** — the terminal stand-in for JetBrains' double-shift,
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

## Command mode (`:`)

Snapshots `registry.Commands()` per open (the registry is the single source of
truth), fuzzy-filters each command's `Title` (falling back to its id so `:hello`
finds `example.hello`), and ranks **context-first**:

1. **in-context** — pane scope equal to the focused context id,
2. **global** — `Scope.Global`,
3. **off-context** — scoped to a different context (ranked last, or hidden when
   `palette.off_context = "hide"`).

Within a tier, higher fuzzy score wins, then title. The dim detail shows the
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

## Recent-files mode (`cmd+e`, Roadmap 0230)

JetBrains' Recent Files popup, palette-style (`recent_mode.go`). The mode is
locked-only (its prefix rune is internal, never typed): `palette.recentFiles`
opens it centered via `OpenLocked`. The palette owns no MRU store — the list
func is injected by the root model (`internal/app/recent.go`), which touches a
path on every file open (`openPath`) and tab activation, deduplicates
(touch moves to front) and caps at 50. The list persists as `recent_files` in
`.ike/session.json` beside the rest of the session state, so history survives a
restart; a missing section loads as empty (presence-versioned schema).

With an empty query the items keep MRU order — most recent first — with the
**currently active file excluded**, so `cmd+e` + `enter` jumps to the previous
file (the `Context.ActivePath` field carries the exclusion). A query
fuzzy-matches the project-relative path; equal scores keep MRU order. Files
that vanished from disk are dropped from the listing. Activation emits the same
`OpenFileMsg` as the `@` mode.

## Search-everywhere mode (`cmd+shift+a` / double-shift, Roadmap 0230)

JetBrains' Search Everywhere, palette-style (`search_mode.go`). Locked-only like
the recent-files mode (`palette.searchEverywhere` opens it via `OpenLocked`);
`shift shift` resolves through the ordinary multi-step chord engine, so it works
off macOS too (it needs key-up reporting, hence leader `A` as the universal
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
back to the plain listing (#263). Symbols join once a workspace-symbol source
exists (idea #146).

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
- `toggle_key` — default open key (default `ctrl+p`).

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
