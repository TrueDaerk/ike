---
type: concept
title: Help Overlay
description: Read-only command & shortcut cheat sheet — snapshots the plugin registry, joins bindings, packs entries into width-responsive columns, hosted in the reusable floating shell.
resource: internal/help/help.go
tags: [architecture, help, overlay, responsive, bubbletea]
timestamp: 2026-06-20T00:00:00Z
---

# Help Overlay

Roadmap 0030. A discoverable, self-documenting window: pressing `?` opens an
overlay listing every registered **Command** with its bound **shortcut**. It is
a pure **consumer** — it owns no command or binding store, and (since roadmap
0035) no chrome. It snapshots the plugin registry (roadmap 0020) on open and
joins each command with its shortcut from a binding resolver (the roadmap 0080
keymap resolver, consumed through a narrow interface so help builds before 08
lands). The body packs entries into **at most two columns**.

The cheat sheet is rendered inside the reusable **floating shell**
(`internal/ui.Floating`, roadmap 0035) — `Help` is just a `ui.Content` provider.
The shell owns the centered floating box, content sizing, vertical scroll, and
`esc/?/q` dismissal; Help owns only the snapshot and column layout. See
[Floating Shell](/architecture/floating-shell.md).

## Structure

```
internal/help/
  source.go    snapshot registry Commands, join 08 resolver bindings, group by scope, deterministic sort
  layout.go    width -> column count; column-major balanced packing; min-column-width; single-column fallback
  help.go      ui.Content: Snapshot(ctxID) refresh; Title(); Render(width) -> column-packed body (max two columns)
```

The root model (`internal/app`) holds a single `*ui.Floating`. On `?` it calls
`help.Snapshot(ctx)`, sets the `*help.Help` as the shell's content, and opens the
shell; while open the shell swallows all input and the root composites it
centered via `overlay.Center`. Scrolling, chrome, sizing, and dismissal now live
in the shell, not in help.

## Source of truth

`Snapshot(src, res, ctxID)` is the join:

- **Commands** come from `registry.CommandsForContext(ctxID)` — global commands
  plus those scoped to the focused pane's context (editor / explorer). No
  parallel command list.
- **Shortcuts** come from a `BindingResolver` (`Binding(id) (string, ok)`). The
  resolver is the seam onto roadmap 0080. It is not wired yet, so the root passes
  `nil` and commands render **title-only** — graceful degradation, no hardcoded
  keys. `MapResolver` is a test/stand-in implementation.

Entries group by **scope label** (`global`, `editor`, `explorer`) with a heading
per group; ordering is deterministic (global first, then alphabetical; entries
by id) so the layout never jumps between opens. Headings are set apart by weight
and an underline — not colour alone — so the grouping survives on monochrome
terminals.

## Responsive layout

`layout.go` is pure and unit-tested:

- `ColumnCount(width, minColWidth)` = `width / (minColWidth + gutter)`, floored
  at 1 — narrow terminals fall back to a single column.
- `MinColumnWidth(cells, configMin)` derives the column width from the widest
  rendered cell, never below the configured minimum (config key
  `help.min_column_width`) or the built-in default.
- `Pack(cells, cols)` distributes entries **column-major** with
  `rows = ceil(n/cols)`, so columns differ in height by at most one (balanced).

`Render(width)` lays the snapshot out to the width budget the shell supplies.
The column count is `min(2, ColumnCount(...))` — capped at **two columns** — and
a single shared column width keeps every group's columns aligned. The shell
handles fitting the result to the terminal and scrolling on overflow.

## Scrolling

Scrolling is owned by the floating shell (`internal/ui/scroll.go`), not by help.
When the body is taller than the visible area the user scrolls with `↑`/`↓`,
`pgup`/`pgdn`, `ctrl+u`/`ctrl+d`, and `g`/`G` (top/bottom); offsets clamp at both
ends and a position indicator (`▲ … ▼  NN%`) shows there is more off-screen. See
[Floating Shell](/architecture/floating-shell.md).

## Design rules

- **Presentation only.** The overlay executes nothing and dispatches no command
  message; the only thing it emits is its own dismissal.
- **Scroll, never truncate.** Overflow scrolls; content is never cut.
- **Degrades gracefully.** Unbound commands render title-only; unknown registry
  fields are ignored.

## Boundaries

- Defining commands and their shortcuts is owned by the feature roadmaps + 0080.
- The `?` binding and `:help` command dispatch move to 0080 / 0070 once they
  land; help only *consumes* them. Today the root wires `?` directly.
- Per-command long-form help text is a future additive `Help` field; v1 renders
  title + shortcut.
