---
type: concept
title: Command Palette
description: Centered floating overlay fronting every action — a prefix-dispatched mode system (":" runs registry commands context-ranked, "@" fuzzy-finds files, locked recent-files and search-everywhere modes behind cmd+e / cmd+shift+a), pure presentation that dispatches tea.Msgs and executes nothing itself.
resource: internal/palette/palette.go
tags: [architecture, palette, overlay, fuzzy, modes, bubbletea]
timestamp: 2026-08-24T00:00:00Z
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
  search_mode.go           locked search-everywhere mode — composes command + class + file + symbol modes, per-kind cap
  context.go               Context captured at open (focused pane context id + project root + active file)
internal/app/              root model hosts the palette, toggles it, forwards keys, renders on top
  symbols.go               "$" live workspace-symbol mode — cache, kind badges, project/exact ranking tiers
  classes.go               "/" class category — kind-filtered views (symbolView) onto that one cache (#1849)
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
  its normal-mode meaning); any other key resets the pending state. The second
  esc only counts within `escEscTimeout` (350ms, #1750) of the first — a
  forgotten first esc from long before doesn't arm the toggle on an unrelated
  later esc; past the window the next esc simply re-arms as a fresh first esc.
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

**Digit shortcuts (`DigitPicker`, #2023).** An optional Mode extension —
`DigitShortcuts() bool` — turns `1`–`9` into "run the nth listed row" while
the palette is **locked** to that mode and the query is **empty**; the press
activates the row through the ordinary activation path and a digit past the
last row is swallowed. Any other mode (or a non-empty query) keeps the digit
as query text. Rows advertise their number via `Item.Hint`, a dim leading
column rendered before the title. Only the intention popup opts in — see
[intention actions](./intention-actions.md).

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
disk walk is cached per palette open (filtered on every keystroke, walked once;
the palette drops every mode's cache via the optional `Refresher` extension on
each open, #1372, so newly created files appear and deleted ones vanish), skips
hidden entries and heavy directories (`.git`, `node_modules`, `vendor`), uses
forward-slash paths for stable matching, and is capped at `maxFiles`. Activation
emits `OpenFileMsg{Path}` joined onto the root.

Ranking is fuzzy score, then **most-used** (#1419), then path: the file-usage
counter (same `Usage` type as #773, persisted in `.ike/fileusage.json`,
`IKE_CONFIG_DIR`-redirectable) counts only file selections confirmed from the
two **ranked palette windows** — Run a Command's `@` source and Search
Everywhere. The palette marks such an activation (`OpenFileMsg.CountUsage`)
and the root model bumps the counter; opens via the explorer, go-to-file, the
editor's anchored `@` finder or the recent-files mode never count. Match
quality still wins — usage only breaks equal scores, notably the empty-query
listing.

**Filesystem reach (#1433).** `@` is no longer project-only: a query typed as
a filesystem path — leading `/`, `~/`, `./` or `../` — is served by the shared
`internal/pathcomplete` engine instead of the fuzzy walk, the same candidates
the `;` picker produces. Tab extends a path query (`Completer` seam), enter on
a directory descends (`OpenPathDescendMsg` with `Prefix: '@'`, so the re-open
stays in the `@` finder), enter on a file opens it via the normal
`OpenFileMsg` path. A non-path query falls back to filesystem prefix
candidates, rendered by **absolute path** so out-of-project rows stay visually
distinct; project matches always rank above filesystem ones (#1775 widened
when that fallback applies — see below). `;` stays the explicit path-only
picker.

**Reaching out-of-project files by name (#1775).** Requiring *zero* project
matches made that fallback effectively dead — a subsequence match over the
relative path nearly always hits something. Every non-empty non-path query now
appends filesystem candidates **below** the project matches, from two anchors:
the project root (rows titled by absolute path) and the **home directory**
(rows keeping the typed `~/` notation), so `~/Hierarchie.txt` is reachable by
typing `Hierar` instead of the whole path. Paths already listed are skipped
(`seen`), each anchor is capped at `maxFsFallback` rows so the tail never
buries the project hits, and the home anchor is injectable (`FileMode.home`)
so tests stay machine-independent.

**Tab on a fuzzy query (#1775).** Tab resolves through two seams, in order:
`Completer.Complete` (textual, path queries only) and then — when that has
nothing to add — `ItemCompleter.CompleteItem`, which adopts the **selected
row** as the new query body. A project hit completes to its relative path, a
filesystem hit to its titled path; a directory keeps its trailing separator,
so the completed query is a path query again and the list immediately shows
the directory's contents (the next tab descends further). Ordering matters:
path queries decline `CompleteItem` and keep the shell-like common-prefix
completion. The `SideMode` column toggle (#778) is handled before either seam,
so a two-column open still switches focus on tab.

**Anchored descend (#1775).** Enter on a directory row inside the editor's
anchored `@` finder re-opens it **anchored** again (`OpenAnchoredWith`, with
the geometry re-derived from the focused pane via `paneAnchor`), instead of
jumping to the centered box; the `;` picker and every non-anchored open keep
`OpenLockedWith`.

**Scratch files inline (#1812).** A query that fuzzy-matches the literal word
**"scratch"** appends the scratch store's files below the project matches and
the filesystem fallback — the simplest rule that satisfies "typing scratch
lists the scratch files" without pulling scratch files into every fuzzy query
by their own name (a scratch would otherwise have to fuzzy-compete with every
project file on equal footing). Rows are newest-first, the store's order, like the `~` scratch mode
(see [Scratch Files](/architecture/scratch-files.md)), tagged with a
`"scratch"` `Detail` chip so they read as scratch, not project, rows.
Activation emits the ordinary `OpenFileMsg`. The source is injected
(`FileMode.SetScratchList`, mirroring `ScratchMode`'s own injection) over
`internal/scratch.List` — the palette core still owns no store. Path queries
(#1433) and the anchored descend above are unaffected: both return before this
step runs.

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
tab — the msg's `Prefix` names the mode to return to (zero means `;`; the
`@` finder's path queries descend within `@`, #1433). Tab completes via the `Completer` seam. With no matching candidate the
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
Projects column) applies the same rules, so both pickers match. Title
truncation measures **display cells**, not runes (#1531), so wide runes
(CJK, emoji) cannot overflow the row and push the right column out.

### Recent Projects column (#778)

The locked Recent Files dialog renders a second, left column listing
`project.history` (current project excluded), through the generic `SideMode`
extension (`recent_mode.go`): a locked mode implementing
`SideTitle`/`SideResults` gets the two-column layout. `tab` toggles the
column focus (plain `left`/`right` switch too while the query is empty;
with text they stay cursor keys), `up`/`down` navigate the focused column,
and the accent `❯` selection marker follows that focus (#1532): only the
focused column shows it in the accent color, the other column's selected
row keeps a dimmed marker and its background band — so it is always clear
whether `enter` opens a project or a file.
`enter` on a project emits `project.PickedMsg` — the normal validated
path into the seamless workspace switch (#777), so terminals and runs keep
running. The query fuzzy-filters both columns at once. Anchored palettes
and search everywhere never show the column.

**The column has its own scroll window (#2041).** `sideTop` is to the column
what `top` is to the main list: `scrollSideToSelected` keeps the selection
inside `sideVisibleRows() = visibleRows() - 1` rows (the column spends one
line on its heading), `sideView` renders that window, and the click mapping
resolves a row as `sideTop + (y - 3)`. Before it the column had a selection
but no window, so with more projects than rows everything below the fold was
invisible and unreachable. Both the aux action and `enter` go through
`sideSel`, so closing a workspace or pruning an entry still hits the row the
user sees after scrolling.

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
escape). One query is ranked across **commands, classes, files and symbols** by
*composing* the already-built `CommandMode`, `FileMode` and the two kind views
of the live symbol mode — no duplicated ranking. Each
source's top rows (per-kind cap, `searchAllPerKind`) interleave by **banded**
fuzzy score with an explicit source-priority tier (#1421): scores are quantised
to `searchAllScoreBand` (one word-boundary bonus), and within a band the
earlier source wins — command > class > file > symbol — so comparably-matched results
group by kind while a clearly stronger match (a full band apart) still outranks
a higher-priority source. Each source's own inner ranking (fuzzy score, #773
usage boost, MRU order) is preserved by the stable sort. Every row is retitled
with its source's prefix
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

**Classes are their own category (#1849).** JetBrains treats a class name as a
first-class hit, so `internal/app/classes.go` seats one next to the commands:
a `symbolView` is a **kind-filtered window onto the one symbol cache** — not a
second `workspace/symbol` source, so a keystroke still costs one request. Two
views exist: the class category (`/`, the class-like kinds `class`, `struct`,
`interface`, `enum` — Go has no classes, structs and interfaces are the
equivalent) and the search-everywhere symbol seat (`$`, everything else), so no
symbol is listed twice in the composed list. The class seat sits at tier 2
(command > class > file > symbol) and has its own `searchAllPerKind` budget, so
a matching class can no longer be crowded out by a swarm of functions; the
shared tier ranking (#377) still puts an exactly-matched project class above
every stdlib/dependency hit. Every symbol row — in both views and in the
standalone `$` mode, which keeps all kinds — carries its **kind as a dim badge**
("class", "struct", "func", …) after the title, so a class is tellable from a
function at a glance.

**Prefix scoping (#1417).** Search everywhere is locked, so the palette core
never strips a prefix here — but a query whose **leading rune is one composed
source's own prefix** scopes the list to that source alone: `:` restricts it to
commands, `/` to classes, `@` to files, `$` to the remaining workspace symbols,
matching on the remaining
body. The scoped source keeps its own ranking and is **not** per-kind capped
(the cap only exists to stop one source drowning the others), rows still carry
the kind glyph, and a live source scoped this way receives the stripped body —
the filtered-out sources are not re-queried. A prefix-only query (`:`) lists
that source's full listing. A leading rune naming no source is ordinary query
text, and an unprefixed query composes everything as before; the empty-query
recents listing (#263) is untouched.

## Pasting into overlay inputs (#1273)

The palette's `Paste(text)` inserts a block into the query at the cursor and
returns the command the resulting edit schedules (the live-mode debounce kick).
It is one of several implementations of the same seam: `finder.Paste` (the
focused query / replacement / include / exclude field), `settings.Paste` (the
inline value editor, else the entry filter), `explorer.Paste` (a name prompt
or the speed-search query), and the app's own rename, clone-dialog,
new-project, save-as, save-layout and JetBrains-import prompts (the last
group added in #1873, alongside the clone dialog's rename/palette
predecessors — every shell prompt with a text field now takes a paste, not
just the ones fixed at #1273's original cut).

The root model routes to them through **`internal/app/overlaypaste.go`**,
whose `routeOverlayPaste` mirrors the guard chain of the `KeyPressMsg` handler
exactly — the surface that owns the keyboard is the surface that gets the
paste. Overlays with no text input (menus, list overlays, decision prompts)
return false and the block is **dropped, not forwarded**: a paste must never
leak into the hidden editor underneath.

Two routes feed it. Bracketed paste (`tea.PasteMsg`, #603) reaches it from
`handlePaste`, which used to bail on `overlayCapturesKeyboard()` and discard
the block — that was the bug: nothing could be pasted into search-everywhere
or any other overlay input. `Cmd+V` needs its own hop because it maps to
`editor.paste`, which no overlay handles, and overlays own the keyboard before
the keymap layer runs; the handler intercepts the chord, reads the system
clipboard and hands the text to the same router, the same shape the terminal
pane's `Cmd+V` uses (#727).

Every target is a single-line field, so `ui.PasteText` flattens the block: a
one-line paste is inserted verbatim (a deliberate leading space survives, and
a path copied with its trailing newline arrives clean), while a genuinely
multi-line block is trimmed per line, empties dropped, joined with single
spaces. Control characters are stripped and tabs become spaces, so no paste
can corrupt the rendered row or smuggle a line break into a one-line input.

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

## Code preview column (#2047)

An optional `Mode` extension, `PreviewMode`, turns a locked, centered open
into a **two-column** box: the result list on the left, a source excerpt of
the selected row's target on the right, separated by a dim vertical rule. The
find-usages popup (`internal/app/references.go`) is its first consumer, so one
sees where a usage sits before jumping to it.

- **Opt-in per mode.** `CodePreview() bool` enables it; rows carry their target
  in `Item.Preview` (`PreviewTarget{Path, Line}`, `Line` 1-based). A row
  without a target renders an empty column. It is mutually exclusive with the
  `SideMode` left column and off for anchored opens.
- **Height bounds.** A preview open re-bounds `visibleRows()` to
  `[ui.MinResultRows, ui.MaxResultRows]` = **11 to 40**: the box keeps eleven
  rows with two (or zero) results and stops growing at forty, the list
  scrolling inside it. The list block is blank-padded to that height, so the
  popup's size no longer flickers with the result count.
- **Geometry.** The centered box takes three quarters of the terminal instead
  of half (`ui.popup_max_width` still caps it); `previewSplit` gives the
  excerpt two fifths of the content width (capped at 60 cells) and drops the
  column entirely below 64 cells of content. Presses in the excerpt are inert
  — `Click` rejects `x` past the list column so the row behind it never fires.
- **Rendering.** `internal/codepreview` reads only the window it needs (never
  the whole file), caches the last one so walking the list re-reads only when
  the target moves, centers the target line — clamped at the file's head —
  highlights it, and returns exactly the requested number of rows. An
  unreadable, deleted, or directory target renders a dim `preview unavailable`
  notice instead of failing the frame. `ui.JoinColumns` / `ui.PadRows` are the
  shared two-column seam, used by the [find-in-path
  overlay](/architecture/search.md) too.

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
  one more registered `Mode`. Each prefix rune must be unique: `New` panics if
  two modes register the same one, rather than letting the later one silently
  shadow the earlier one in `byPrefix` (#1878 — a second mode reusing recent-files'
  `%` made cmd+e open the wrong picker with nothing catching it).
- **Presentation + routing only.** The palette dispatches `tea.Msg`s and executes
  nothing; owners (editor, explorer, projects) handle them.
- **Dismissable and non-destructive.** `esc` closes with no side effects;
  `↑`/`↓`/`ctrl+p`/`ctrl+n` navigate — **wrapping** at both ends since #1666 —
  and `pgup`/`pgdn` jump one visible result window, clamped; `enter` activates.
  `home`/`end` stay with the query's text cursor. Both columns behave the same;
  see [Selection-List Navigation](/architecture/list-navigation.md).
- **Scrolloff of one entry (#2041).** Both windows follow the selection through
  `ui.ScrollToShowOff(…, scrollOff)` with `scrollOff = 1`: moving down scrolls
  already when the selection reaches the second-to-last visible row (moving up,
  the second), so the next entry is always visible. It applies to every palette
  mode, not just the recent dialog. The list ends stay flush — the first and
  last entries are not padded with blank rows.
- **The wheel scrolls the column under the pointer (#2041).** `Palette.Wheel(x,
  y, delta)` maps the box-relative `x` onto the two-column layout exactly as
  `Click` does: over the projects column it moves `sideSel`, otherwise the main
  selection, and it takes the column focus along so `enter` and the aux action
  stay on the row just scrolled to. Wheel movement **clamps** at both ends
  (keyboard steps wrap, a wheel flick must not). The root model routes wheel
  events inside the box here (`handleMouse`), coalesced like every other wheel
  burst (#238).

## Boundaries

- Defining editor/explorer commands and the keybindings (incl. the toggle key) is
  owned by the feature roadmaps and 0080.
- The project-switch command merely *appears* here; its logic is 0090.
- Symbol/line/diagnostic prefixes are future modes the `Mode` interface leaves
  room for; only `:` and `@` ship here.
