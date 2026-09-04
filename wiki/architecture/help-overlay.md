---
type: concept
title: Help Overlay
description: Read-only command & shortcut cheat sheet — snapshots the plugin registry, shows only the focused pane's own bindings (or a keyboard-owning mode's, via a Focused extra group) plus a curated Global section and the file-type-gated commands matching the buffer, packs entries into width-responsive columns with right-aligned shortcuts, hosted in the reusable floating shell.
resource: internal/help/help.go
tags: [architecture, help, overlay, responsive, bubbletea]
timestamp: 2026-08-27T00:00:00Z
---

# Help Overlay

Roadmap 0030. A discoverable, self-documenting window: pressing `?` (or `F1`)
opens an overlay listing every registered **Command** with its bound
**shortcut**. It is
a pure **consumer** — it owns no command or binding store, and (since roadmap
0035) no chrome. It snapshots the registered commands from the plugin
registry (roadmap 0020) on open
and joins each command with its shortcut from a binding resolver
(the roadmap 0080 keymap resolver, consumed through a narrow interface so help
builds before 08 lands). The default view **shows only the focused pane**
(#2182, #2483): the focused context's own bindings in their own titled
section, the file-type-gated commands matching the focused buffer, and a
hand-curated `Global (essentials)` section — the other contexts are not
listed; `tab` switches to the complete flat reference (every scope) and to the
curated Essentials set — see [Views](#views-656-2182). Commands handled outside
the keymap layer (the editor's
vim ex-commands `:w`/`:q`/`:wq` and modal keys `u`/`ctrl+r`) carry a
documentation-only `Shortcut` hint on the `plugin.Command` that help shows when
no live binding resolves. The body packs entries into **at most two columns**
with each shortcut right-aligned to its column's edge (titles left, keys right,
never closer than a two-space gap), and the scope groups (Global, Editor,
Explorer, …) are separated by a blank line.

The cheat sheet is rendered inside the reusable **floating shell**
(`internal/ui.Floating`, roadmap 0035) — `Help` is just a `ui.Content` provider.
The shell owns the centered floating box, content sizing, vertical scroll, and
`esc/?/f1/q` dismissal; Help owns only the snapshot, column layout, and the
**live filter** (#271): typed printable keys narrow the sheet (case-insensitive
substring over titles and shortcuts, empty groups drop out, the title echoes
the filter). `Help` implements the shell's optional `ui.Filterable` extension —
with an active filter, `q`/`?` act as letters, `backspace` edits, and `esc`
first clears the filter before a second `esc` closes; each open starts
unfiltered. See [Floating Shell](/architecture/floating-shell.md).

## Views (#656, #2182)

The overlay has **three views**; `tab` cycles them (`Help` implements the
shell's `ui.KeyHandler` extension), skipping any view with nothing to show, and
the title and footer always name the view and the one `tab` leads to next.

| View | Shows | Opens when |
| --- | --- | --- |
| **Context** | only the focused pane's context (heading `Explorer — focused pane`), the file-type-gated commands matching the buffer (`This file (json)`), and the curated `Global (essentials)` section | a context is focused and it owns commands |
| **Essentials** | the curated starter set, focus-independent | no focused context (the degradation path) |
| **Flat** | the complete reference: every registered scope, global first | neither of the above resolved |

### Context view (#2182, reduced by #2483)

`ContextSnapshot(src, res, contextID, langID)` builds the short sheet: the
focused context's own group, flagged `Focused` so its heading renders as
`<Context> — focused pane`, then the global file-type-gated commands matching
the focused buffer's language (see below), then the hand-curated
`Global (essentials)` section — and nothing else. The other contexts are
deliberately *gone*, not reordered (#2483): showing every scope made the sheet
a scroll-wall nobody read, and the complete reference stays one `tab` away in
the flat view. The curated global spec lives next to the Essentials spec in
`essentials.go` (`globalEssentialIDs`, ≤ 20 rows so a typical context section
plus the global one fit a 40-line screen); a drift test in `internal/app`
asserts every ID resolves and a budget test renders the editor, explorer and
playground contexts at 100×40. An empty context id or `global` yields the
plain full `Snapshot` — there is nothing to narrow to — and the view is
skipped in the `tab` cycle; a focused context owning no registered commands
yields just the curated global section, over which a keyboard-owning mode's
extra groups (#2237, below) can still lead. The cycle is context → flat →
essentials → context; the responsive two-column layout is identical in every
view.

#### File-type-gated commands (#2483)

A command can declare the buffer languages it applies to
(`plugin.Command.Languages`; `AppliesToLang` is the predicate). The playground
families are the founding users — jq (`json`/`jsonc`/`ndjson`), yq
(`yaml`/`ansible`), xmq (`xml`/`html`) and their cheatsheet/filter siblings,
declared once in `internal/app/commands.go` from the same language lists
`playKindFor` routes on (`playgroundopen.go`), alongside `markdown.preview`,
`csv.columnProfile` and `log.openRotatedSet`. The root model passes the
focused buffer's language to `help.Snapshot(contextID, langID)` —
`focusLang()`: the focused HTTP pane's shown body type, else the focused
editor tab's buffer language. In the context view, matching gated global
commands surface in their own `This file (<lang>)` section (context-scoped
gated ones stay inside their context's group), non-matching ones are dropped,
and every gated row carries a bracketed accent-coloured badge with the
family's canonical name (`[json]`) so it reads as conditional on the current
file. The flat view — the complete reference — keeps every gated command,
badged. The [command palette](./command-palette.md) consumes the same
declaration: command mode ranks a non-matching gated command off-context
(`Context.Lang`), so help and palette agree.

#### Contexts without a registry scope (#2237)

Some keyboards belong to no pane and no command: a **mode** mounted inside
another pane owns every key while it is up, yet advertises no context id and
registers no commands, so the context view would lead with the bindings of the
pane whose component the mode replaced — none of which apply. The
[jq/yq playground](./jq-playground.md) is the case that forced the seam.

An extra group (`SetExtra`) flagged `Focused` covers it: `withExtraLeading`
puts such groups at the **head** of the context view instead of appending them,
so they sit exactly where a registered focused scope would, and
`hasContextView` counts them, so the sheet opens on that view. The flat sheet
keeps its own ordering — extras trail there, flagged or not. The caller decides
what "focused" means: `internal/app`'s `helpContext` reports `playground` while
the mode owns the keyboard, and only the help snapshot reads it — keymap
resolution, palette scoping and the mode indicator keep the plain
`focusContext`.

### Essentials view (#656)

The Essentials view is a curated set, not the full registry dump:
~25 hand-picked commands in feature groups (Get around / Edit / Panes & tabs /
Project & tools / Customize), each group ≤6 entries so the view fits one
screen. Each open resets the view; the title reflects it (`HELP — essentials`,
`HELP — Explorer context`, `HELP — commands & shortcuts`) and a dim footer line
shows the count and the toggle hint.

Curation lives in `essentials.go` as command IDs joined against the same
registry + resolver as the full snapshot — deliberately hand-maintained, since
`Binding.Owner` values are internal roadmap tags unusable as user-facing
groups. Unregistered curated IDs drop silently (stub registries degrade to the
flat view); a drift test in `internal/app` asserts every curated ID resolves
against the real global registry. Essentials ignores the focus context — the
starter set is the same everywhere. The caller-supplied extra groups
(`SetExtra`: the "blocked" section and the focused pane's local keys) are
appended to the context and flat views, never to Essentials.

A **non-empty filter always searches the full set** (typing means hunting for
something specific, so the curated subset would only hide the answer — and the
"full set" is the flat view's every-scope dump, so a filter typed in one
context still finds another context's commands and the file-type-gated ones,
badged); the footer switches to `N of M matches · searching all commands` and `tab` is a
no-op until the filter clears, which restores the prior view.

## Structure

```
internal/help/
  source.go      snapshot registry Commands, join 08 resolver bindings, group by scope, deterministic sort; ContextSnapshot = focused scope + file-type + curated global only (#2182, #2483)
  essentials.go  hand-curated Essentials spec + EssentialsSnapshot join (#656); curated Global spec for the context view (#2483)
  layout.go      column count + width for a budget (ColumnLayout); widest/typical column width; column-major balanced packing; single-column fallback
  help.go        ui.Content: Snapshot(ctxID) refresh; Title(); Render(width) -> column-packed body (max two columns);
                 withExtraLeading = Focused extra groups lead the context view (#2237)
```

The root model (`internal/app`) holds a single `*ui.Floating`. Its `openHelp`
calls `help.Snapshot(helpContext, focusLang)` — which builds both the reduced
context view and the flat reference — sets the `*help.Help` as the shell's content, and opens
the shell; while open the shell swallows all input and the root composites it
centered via `overlay.Center`. It is reached three ways: the registered
`palette.keymapHelp` command (default `f1`, also
palette-invokable), the plain `?` key, and a hardcoded `f1` fallback for
registries without the app plugin. Scrolling, chrome, sizing, and dismissal now live
in the shell, not in help.

## Source of truth

`Snapshot(src, res, contextID, langID)` is the join:

- **Commands** come from `registry.Commands()`, narrowed to the scopes that
  apply to the focused pane: the `global` group plus the group whose label
  matches `contextID`, minus the file-type-gated commands whose gate does not
  list `langID` (#2483). An empty `contextID` keeps every scope and every
  gated command — the flat view's complete reference. No parallel command
  list.
- **Shortcuts** come from a `BindingResolver` (`Binding(id) (string, ok)`). The
  root now passes the `*registry.Registry` itself: it resolves a command's key by
  matching the command id against keymaps that declare a `CommandID`
  (`plugin.Keymap.CommandID`). When no keymap resolves, help falls back to the
  command's own documentation-only `Shortcut` hint (`plugin.Command.Shortcut`) —
  this is how the editor's vim ex-commands (`:w`, `:q`, `:wq`) and modal keys
  (`u`, `ctrl+r`), which live outside the keymap layer, still show a shortcut. A
  command with neither stays title-only — graceful degradation, no hardcoded
  keys. The full keymap layer (preset + overrides) is still owned by roadmap
  0080; this is the minimal command→shortcut seam. `MapResolver` remains a test
  stand-in.

Entries group by **scope label** (`global`, `editor`, `explorer`) with a heading
per group; ordering is deterministic (global first, then alphabetical; entries
by id) so the layout never jumps between opens — except in the context view,
where the focused scope leads (see above) and its heading says so. Headings are set apart by weight
and an underline — not colour alone — so the grouping survives on monochrome
terminals.

## Responsive layout

`layout.go` is pure and unit-tested:

- `MinColumnWidth(cells, configMin)` derives the column width from the widest
  rendered cell, never below the configured minimum (config key
  `help.min_column_width`) or the built-in default.
- `TypicalColumnWidth(cells, configMin, pct)` is its outlier-tolerant
  counterpart: the narrowest width that still shows `pct` percent of the cells
  in full (help uses 90%).
- `ColumnLayout(width, natural, floor, maxCols)` picks the column count and the
  width to render columns at: the largest count up to `maxCols` whose columns
  stay at or above `floor`, using the fair share `(width - gutters) / cols` when
  the natural width does not fit, and falling back to one budget-clamped column.
- `Pack(cells, cols)` distributes entries **column-major** with
  `rows = ceil(n/cols)`, so columns differ in height by at most one (balanced).

`Render(width)` lays the snapshot out to the width budget the shell supplies.
The column count is capped at **two columns** and a single shared column width
keeps every group's columns aligned. Each column carries a fixed slack
(`colSlack`) beyond its cell width so the pane gets breathing room rather than
hugging the text. Within a cell the title sits left and the shortcut is padded
out to the column's right edge, so the keys line up as their own visual column;
a minimum two-space gap is kept even when the column is clamped narrower than
the entry. The shell handles fitting the result to the terminal and scrolling
on overflow.

### Sizing columns for the typical entry, not the longest (#2215)

Columns aim at `TypicalColumnWidth` (+ slack), not at the widest cell. The
flat view shows **every** scope at once, so the widest cell is drawn
from hundreds of commands: one verbose title used to push the column width past
half the terminal, `ColumnLayout` then had room for a single column, and the
overlay degraded to one endlessly tall column in every section. Sizing to the
90th-percentile width keeps two columns wherever the budget allows and costs
only the handful of overlong rows an ellipsis — `renderEntry` truncates the
*title* (never the shortcut) so each row stays one line and its key stays
visible. A column with less than `minTitleWidth` room left for a title keeps the
untruncated row and lets it overflow, since a bare `…` says nothing. When even
one shrunken column would fall below the floor — genuinely narrow terminals —
the sheet still falls back to a single column.

## Scrolling

Scrolling is owned by the floating shell (`internal/ui/scroll.go`), not by help.
When the body is taller than the visible area the user scrolls with `↑`/`↓`,
`pgup`/`pgdn`, `ctrl+u`/`ctrl+d`, and `g`/`G` (top/bottom); offsets clamp at both
ends and a position indicator (`▲ … ▼  NN%`) shows there is more off-screen. See
[Floating Shell](/architecture/floating-shell.md).

## Design rules

- **Presentation only.** The overlay executes nothing and dispatches no command
  message; the only thing it emits is its own dismissal.
- **Scroll vertically, ellipsise horizontally.** Vertical overflow scrolls and
  no entry is ever dropped; only a title too long for its column is ellipsised,
  and never its shortcut (#2215).
- **Degrades gracefully.** Unbound commands render title-only; unknown registry
  fields are ignored.

## Boundaries

- Defining commands and their shortcuts is owned by the feature roadmaps + 0080.
- The `?` binding and `:help` command dispatch move to 0080 / 0070 once they
  land; help only *consumes* them. Today the root wires `?` directly.
- Per-command long-form help text is a future additive `Help` field; v1 renders
  title + shortcut.
