---
type: architecture
title: Documentation Screenshots
description: Feature screenshots for the user docs generated from the app itself — a headless model driven by cmd/shotgen, its frame painted to PNG by internal/shotpng.
resource: cmd/shotgen/main.go
tags: [docs, screenshots, tooling]
timestamp: 2026-08-07T00:00:00Z
---

# Documentation Screenshots

The user documentation embeds screenshots of features (#1634): syntax highlighting, the
Markdown/CSV/log rendering layers with their raw counterpart, the diff viewer, the HTTP client.
They are **generated**, not captured: `make shots` drives the real root model headlessly and paints
the frame it renders into a PNG. A shot is therefore always a frame the current code produced, and
refreshing the set after a UI change costs one command.

## Two pieces

* **`cmd/shotgen`** — the driver. It unpacks an embedded demo project (`fixtures/`) into a temp
  directory, points `$IKE_CONFIG_DIR` and `$HOME` at throwaway paths, and runs one scripted
  scenario per shot: window size, optional `window.hideAllTools`, an `explorer.OpenFileMsg`, then
  palette commands and key presses. The frame comes from `Model.View().Content`.
* **`internal/shotpng`** — the painter. It replays the frame's ANSI through the VT emulator the
  integrated terminal uses (`charmbracelet/x/vt`), which flattens escapes into a grid of styled
  cells, then draws each cell: background rectangle, glyph, underline/strikethrough.

## Decisions worth knowing

* **Theme.** Every feature shot uses `monokai-pro`, written into the scenario's `settings.toml`
  along with `ui.onboarded = true` — without the latter the first-run tour covers the shot.
* **Cropping is done by the app.** `hideTools` runs `window.hideAllTools` so the editor fills the
  frame; `trimRows` cuts trailing rows. No pixel cropping, so a shot always shows whole cells.
* **Box characters are painted, not typed.** A font glyph is designed for the font's line box, not
  for the taller screenshot cell, so glyph-drawn borders show hairline gaps between rows.
  `box.go` paints the line-drawing and block ranges as rectangles that meet exactly.
* **Fonts.** The platform default is Menlo on macOS (faces resolved by PostScript name out of the
  `.ttc`) and DejaVu Sans Mono on Linux, with symbol fallbacks for glyphs the monospace face lacks.
  With no system font — CI — the embedded Go Mono set renders instead, which is what keeps the
  package's tests machine-independent.
* **The pump.** There is no bubbletea event loop, so the driver runs the command tree itself and
  feeds the messages back, including what background workers push through the host's sender
  (`Model.SetSender`) — that is how the initial `git status` snapshot arrives. Each command has a
  timeout and each pump a deadline, because several of the app's timers never quiesce.
* **One working directory.** The model caches the process working directory (it may only change on
  a project switch), so all scenarios share one project. The Git shots therefore come last in the
  list: the working copy is edited in place right before the first of them.
* **Not CI-checked.** Unlike `cmd/docgen`'s reference pages, the PNGs are not regenerated in CI —
  rendering depends on the machine's fonts. They are refreshed deliberately, when the UI they show
  changes.

## Adding a shot

Append a `shot` to the table in `cmd/shotgen/main.go` (name, description, fixture file, frame size,
optional commands/keys), run `make shots`, and embed the PNG in the matching page under
`userdocs/`. `go run ./cmd/shotgen -list` prints the set; `-only <name>` regenerates one.

Related: [Themes](/architecture/themes.md), [Syntax Highlighting](/architecture/highlighting.md),
[Integrated Terminal](/architecture/terminal.md) (the VT emulator), [Diff Viewer](/architecture/diff-viewer.md).
