# Roadmap 0030 — Help Overlay (Command & Shortcut Cheat Sheet)

A discoverable, self-documenting help window. Pressing `?` (or running `:help`)
opens an overlay that lists every registered **Command** with its bound
**shortcut**. It is a *consumer* of the plugin registry (Roadmap 0020) and the
keybinding resolver (Roadmap 0080) — it owns no command or binding store of its
own; it reads what is registered and renders it. The layout is
**responsive**: it packs commands into as many columns as the current window
width allows, and **scrolls vertically** when the content is taller than the
viewport.
The documentation should display groups using the current pane context and installed plugin.

## Prerequisites / Dependencies

- **01 Foundation** — bubbletea root model in `internal/app`; the overlay is a
  `tea.Model` the root hosts and toggles, rendered on top of the active layout.
  Window-size (`tea.WindowSizeMsg`) flows from the root so the overlay can
  recompute its column count on resize.
- **02 Plugins registry** — `internal/plugin` (`Command`, `Scope`),
  `internal/registry` (lookups, ordering). The registry **is** the command
  source; help lists registered `Command`s grouped by owner/scope.
- **08 Keybindings** — `internal/keymap` (resolver, owned by 08) maps a command
  id to its current shortcut string. Help asks the resolver for each command's
  binding; it never parses keys itself. (Soft dependency: commands with no
  binding simply render without a shortcut.)
- **04 Settings** — `internal/config`, read-only, for optional help tuning
  (min column width, grouping, sort order).

> **No new contract.** Help√22 reuses `plugin.Command` (id, title, `Scope`) and the
> 08 resolver as-is. If a one-line `Help`/`Description` string per command is
> wanted, that is an *additive* field proposed against Roadmap 0020 — not
> required for v1, which renders title + shortcut.

## Architecture

```
internal/help/            the overlay model + responsive layout
  help.go                 tea.Model overlay: open/close, key nav, render-on-top
  layout.go               responsive column packing: width -> column count, balance entries
  viewport.go             vertical scrolling when content height > viewport (wraps bubbles/viewport)
  source.go               pull Commands from registry, join with 08 resolver -> bindings, group/sort
internal/keymap/          (08) command-id -> shortcut string lookup (consumed read-only)
internal/registry/        (02) registered Command snapshot (consumed read-only)
internal/app/             (01) root hosts the help overlay, toggles it, forwards size + keys
```

Wiring: the root model (`internal/app`) holds a `*help.Help` and toggles it on
the help binding (`?`, plus the `:help` command — binding/command ownership per
08/0070). While open, the root forwards key `tea.Msg`s and the current
`tea.WindowSizeMsg` to the overlay; `esc`/`?`/`q` closes it. The overlay is
read-only and emits no actions other than close.

## Design rules

- **Registry + resolver are the source of truth.** Help snapshots commands from
  `registry` on open and joins each with its shortcut from the 08 resolver. No
  parallel command list, no hardcoded shortcut strings.
- **Responsive by width.** `layout.go` computes `columns = max(1, width /
  minColWidth)` (minColWidth derived from the longest entry or config), then
  distributes entries column-major so columns stay balanced. Recomputes on every
  `tea.WindowSizeMsg`. Down to one column on narrow terminals.
- **Scroll, never truncate.** When total rows exceed the viewport height, wrap a
  `bubbles/viewport` so the user scrolls (`↑`/`↓`, `pgup`/`pgdn`, `ctrl+u`/`ctrl+d`,
  `g`/`G`). A scrollbar/position indicator shows there is more.
- **Grouped + sorted, stable.** Entries group by `Scope`/owner (e.g. `editor`,
  `explorer`, `global`) with a heading per group; deterministic order so the
  layout does not jump between opens.
- **Presentation only.** The overlay executes nothing; it dispatches no command
  `tea.Msg`. It is a non-destructive, dismissable view.
- **Degrades gracefully.** Commands without a binding render title-only;
  unknown/extra registry fields are ignored.

## Milestones

- [x] `internal/help/source.go`: snapshot registry Commands, join with 08 resolver bindings, group by scope/owner, deterministic sort.
- [x] `internal/help/layout.go`: width → column-count packing; column-major balanced distribution; min-column-width handling; single-column fallback.
- [x] `internal/help/viewport.go`: vertical scrolling (wrap `bubbles/viewport`) when content height exceeds the visible area, with a position indicator.
- [x] `internal/help/help.go`: overlay `tea.Model` — open/close, recompute layout on `tea.WindowSizeMsg`, scroll keys, `esc`/`?`/`q` dismiss.
- [x] Root-model integration in `internal/app`: host overlay, toggle on `?`, forward size + keys, render on top of the active layout.
- [x] `:help` command + `?` binding wired through the registry/resolver (coordinate with 0070/0080; help only consumes them).
- [x] Optional config hooks (`internal/config`): min column width, grouping, sort order.
- [x] Tests: column-count for several widths, column-major balancing, scroll bounds (top/bottom clamp), command+shortcut join, scope grouping/sort, dismiss behaviour.
- [x] Wiki: document the help overlay, responsive column model, and scrolling under `wiki/`.

## Out of scope

- Defining commands or their shortcuts (owned by their feature roadmaps + 08).
- The keybinding resolver implementation and JetBrains defaults (owned by 08).
- The `:` palette and `:help` command dispatch plumbing (owned by 0070; help
  merely is opened by it).
- The `internal/config` loader implementation (owned by 04).
- Per-command long-form help text / docs pages (v1 renders title + shortcut; an
  additive `Help` field is left as a future extension).
- Interactive rebinding from the help view (read-only here; rebinding belongs to 08).
