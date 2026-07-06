# Roadmap 0050 — File Explorer

The full file explorer. Roadmap 0010 shipped a minimal tree (list a directory,
navigate, emit an "open file" msg). This roadmap turns that MVP into a real
JetBrains-style project tree: a persistent tree rooted at the project root with
expand/collapse, per-filetype colouring driven by config, a hidden-file toggle,
optional file operations, and a set of explorer Commands/Keymaps contributed
through the plugin registry.

It deliberately does **not** add a `..` row. Going "up" is achieved by
collapsing a node or via a dedicated action — the tree stays anchored to the
project root.

## Prerequisites / Dependencies

- **01 Foundation** — the explorer already exists as a `tea.Model`-shaped pane in
  `internal/explorer`, embedded by the root model in `internal/app`. Selecting a
  file emits an "open file" `tea.Msg` the root routes to the editor. This
  roadmap extends that pane in place; it does not introduce a second explorer.
- **02 Plugins registry** — `internal/plugin` (Command, Keymap, Pane,
  FileHandler, Hook), `internal/registry`, and `internal/host` (`host.API`).
  All explorer actions are contributed as registry `Command`s with default
  `Keymap`s; opening a file goes through registry `FileHandler`s, never a bespoke
  path. The explorer pane is the `Pane` capability already registered in 01.
- **04 Settings** — `internal/config` provides merged config
  (defaults < user < project). The explorer reads its settings from the
  `[explorer]` section. This roadmap **defines the exact `[explorer]` keys**
  (below) and consumes them; it does not own the loader.

## Architecture

```
internal/explorer/
  model.go        tea.Model: tree state, focus, Update/View (extends 01 MVP)
  tree.go         Node type, lazy child loading, expand/collapse, flatten-to-rows
  scan.go         directory reading, sort (dirs first, then name), hidden filter
  render.go       row rendering: indent guides, expand glyphs, name + colour
  colors.go       extension/glob -> style resolution from [explorer] config
  config.go       typed view of the [explorer] config section + defaults
  commands.go     registry Command + default Keymap contributions (reveal,
                  refresh, toggle-hidden, new-file, rename, delete, collapse-all)
  fileops.go      create / rename / delete with confirmation msgs (optional)
  msgs.go         explorer-local tea.Msg types (open-file reuses 01/registry msg)
```

The explorer keeps a single root `Node`. Children are loaded lazily on first
expand and cached. The visible list is a flattened slice of `(node, depth)`
pairs recomputed when expansion state changes; `j/k` and arrows move a cursor
over that flat slice. File opening dispatches the registry's open-file flow via
`host.API`; the explorer never imports the editor.

## Design rules

- **No `..` entry.** The tree is rooted at the project root. Upward movement is
  collapse (`h` / left on an expanded dir, or on a leaf collapses its parent) or
  the `explorer.collapseAll` command. Never render a synthetic parent row.
- **Lazy + cached.** A directory's children are read on first expansion and
  cached; `explorer.refresh` invalidates the cache for the selected subtree (or
  root). Never re-scan the whole tree on every keystroke.
- **Config-driven colours.** Filetype colours come exclusively from
  `[explorer.colors]`; no colours hard-coded in `render.go`. `colors.go` resolves
  a node to a style by checking, in order: exact glob match, extension match,
  the `dir` / `default` fallbacks.
- **Italic hidden files and foldes.** Hidden files are rendered in italics and
  can be toggled on/off via `explorer.toggleHidden`. 
- **Actions are registry capabilities.** Every user-facing action is a
  `registry.Command` with a stable id (`explorer.reveal`, `explorer.refresh`,
  `explorer.toggleHidden`, `explorer.newFile`, `explorer.rename`,
  `explorer.delete`, `explorer.collapseAll`). The explorer ships **default**
  `Keymap`s for them, but the canonical JetBrains binding set is owned by
  Roadmap 0080 — we expose commands, 08 maps keys.
- **Open through handlers.** Pressing enter / opening a file resolves a registry
  `FileHandler` and dispatches the open via `host.API`. No direct editor call.
- **No blocking IO in Update.** Directory scans and file ops run as `tea.Cmd`s
  returning result msgs, so a slow/large directory never freezes the UI.

## `[explorer]` config shape

This roadmap owns these keys (consumed via `internal/config`):

The concrete key names are owned by the `internal/config` schema (Roadmap 0040);
the explorer consumes them through the merged config. As implemented:

```toml
[explorer]
show_hidden = false   # initial hidden-file visibility (toggleable at runtime)
tree_indent = 2       # spaces per depth level
sort        = "name"  # ordering within a level (directories are always first)
git_status  = true    # reserved for git decorations (later)

# extension/glob -> colour. Keys are bare extensions ("go") or globs
# ("*.test.go"); values are colour names or hex. "dir" and "default" are the
# required fallbacks.
[explorer.colors]
dir     = "blue"
default = "white"
go      = "cyan"
md      = "green"
toml    = "yellow"
json    = "yellow"
yaml    = "yellow"
lock    = "gray"
```

Globs are matched before bare extensions; `dir` styles directory rows; anything
unmatched falls back to `default`. Directories are always sorted first; `sort`
selects the within-level ordering (`name` today).

## Milestones

- [x] Tree model: root `Node`, lazy child load + cache, expand/collapse state.
- [x] Flatten + navigation: `j`/`k` + up/down over the visible flat list, cursor clamping, scroll on overflow.
- [x] Enter behaviour: toggle on a directory, open via registry `FileHandler` on a file; `h`/left collapses (no `..` row).
- [x] Directory scan as `tea.Cmd`: dirs-first sort, name sort, no UI blocking.
- [x] Hidden-file toggle: `showHidden` default from config + runtime `explorer.toggleHidden`.
- [x] Per-filetype colours: parse `[explorer.colors]`, glob-then-extension-then-fallback resolution in `colors.go`.
- [x] Rendering: indent guides, expand/collapse glyphs, colour applied, selected-row highlight.
- [x] Registry commands + default keymaps: `reveal`, `refresh`, `collapseAll`, `toggleHidden` registered via `internal/registry`.
- [x] File operations (optional): `newFile`, `rename`, `delete` as commands with confirmation msgs; refresh affected subtree afterward.
- [x] Refresh: invalidate cache for selected subtree (or root) and re-scan, preserving expansion where possible.
- [x] Tests: navigation, expand/collapse, flatten correctness, no-`..` invariant, hidden toggle, colour resolution (glob/ext/fallback), command registration, file-op round-trips.
- [x] Wiki: add/refresh the explorer concept doc under `wiki/` (frontmatter `type`, `resource: internal/explorer`), document the `[explorer]` config keys, bump `timestamp`, add a `log.md` entry.

## Out of scope

Editor behaviour and `FileHandler` rendering internals (06); command palette UI
that lists/invokes these commands (07); the canonical JetBrains default
keybinding set (08); the config loader/merge mechanism (04); fuzzy file finder,
git status decorations in the tree, file watching/auto-refresh, multi-root
workspaces, and nerd-font icon themes (later/elsewhere).
