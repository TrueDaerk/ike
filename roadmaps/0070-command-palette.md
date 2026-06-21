# Roadmap 0070 — Command Palette

A single overlay that fronts every action in IKE: a `:` prefix for running
registered **Commands** and an `@` prefix for fuzzy **file** search. The palette
owns no command store of its own — it is a *consumer* of the plugin registry
(Roadmap 0020). It reads what is registered, filters by the focused pane's context,
and dispatches the chosen action as a `tea.Msg`/`tea.Cmd`. The prefix system is
designed to grow (more modes later) without reworking the core.

## Prerequisites / Dependencies

- **01 Foundation** — bubbletea root model in `internal/app`; panes are
  `tea.Model`-shaped; focus switching exists. The palette is an overlay the root
  model hosts and toggles. File opening uses the existing "open file" `tea.Msg`.
- **02 Plugins registry** — `internal/plugin` (`Command`, …), `internal/registry`
  (`Register`, lookups, ordering), `internal/host` (`host.API`). The palette
  lists/filters/executes registered `Command`s; the registry **is** the command
  source. Commands carry an owner id.
- **04 Settings** — `internal/config` (loader owned by 04) for optional palette
  tuning (fuzzy behaviour, max results, recents). Read-only here.

> **Contract extension (additive to 02).** Context-aware filtering requires each
> `Command` to advertise a **scope/context**. This roadmap *proposes* adding a
> small `Scope` field to the `plugin.Command` type defined in Roadmap 0020, plus a
> way for a focused pane to report its context id. Roadmap 0020's contract may need
> this minor additive field; nothing existing is removed.

## Architecture

```
internal/palette/         the overlay model + mode dispatch
  palette.go              tea.Model overlay: open/close, input line, result list, key nav
  mode.go                 Mode interface (prefix, Query, Activate) + registry of modes
  command_mode.go         ':' mode — lists registry Commands, context-filtered, ranked
  file_mode.go            '@' mode — fuzzy file finder over the project tree
  context.go              focused-pane context resolution (pane -> context id)
internal/fuzzy/           reusable matcher: score + matched-index spans for highlighting
internal/plugin/          (02) Command gains additive `Scope` field (proposed extension)
internal/app/             (01) root model hosts the palette overlay, routes its msgs
```

Wiring: the root model (`internal/app`) holds a `*palette.Palette` and toggles it
on the palette keybinding (binding itself owned by 08). While open, the root
forwards key `tea.Msg`s to the palette and renders it on top of the active layout.
On **enter** the palette emits the mode's result `tea.Msg` (run command / open
file) through `host.API`; the root applies it and closes the overlay.

## Design rules

- **Registry is the source of truth.** Command mode never caches its own command
  list beyond a per-open snapshot from `registry`. No parallel command store.
- **Modes are pluggable by prefix.** A `Mode` declares its prefix rune (`:`,
  `@`), produces ranked results for a query, and turns the chosen result into a
  `tea.Msg`. Adding a prefix = registering one more `Mode`; the palette core is
  prefix-agnostic. No prefix typed defaults to a configurable mode.
- **Context-aware, not context-locked.** Command mode asks the root for the
  focused pane's context id, then ranks: matching-scope commands first, global
  commands next, off-context commands last (or hidden, per config). Scopes use
  stable string ids (e.g. `editor`, `explorer`, `global`).
- **Fuzzy matching is shared and pure.** `internal/fuzzy` returns a score and the
  matched character indices so both modes render consistent highlighting; it has
  no UI or registry dependency.
- **Palette is presentation + routing only.** It executes nothing itself — it
  dispatches `tea.Msg`s and lets owners (editor 06, explorer 05, projects 09)
  handle them. The project-switch command appears here but is owned by 09.
- **Dismissable and non-destructive.** `esc` closes without side effects; arrows /
  `ctrl+n`/`ctrl+p` navigate; `enter` activates; reopening restores nothing
  stateful beyond optional recents.

## Milestones

- [x] `internal/fuzzy`: scoring matcher returning score + matched index spans for highlighting.
- [x] `internal/palette`: overlay `tea.Model` — open/close, input line, result list, keyboard nav, esc-to-dismiss.
- [x] `internal/palette` `Mode` interface + prefix dispatch (prefix-agnostic core, extensible).
- [x] Propose + apply additive `Scope` field on `plugin.Command` (coordinate with Roadmap 0020).
- [x] Pane context resolution: focused pane reports a context id; `context.go` maps it for ranking.
- [x] `:` command mode: snapshot registry, context-filter/rank, execute selection via `host.API` on enter.
- [x] `@` file mode: fuzzy file finder over the project tree; selection emits the 01 "open file" `tea.Msg`.
- [x] Root-model integration in `internal/app`: host overlay, toggle, forward keys, render on top.
- [x] Optional config hooks (`internal/config`): fuzzy behaviour, max results, off-context visibility, recents.
- [x] Tests: fuzzy scoring + spans, mode prefix routing, context ranking, command execution dispatch, file-mode open msg, esc/nav behaviour.
- [x] Wiki: document the palette, mode/prefix model, and the `Command.Scope` extension under `wiki/`.

## Out of scope

- Defining editor/explorer commands themselves (owned by 06 / 05).
- Keybinding-to-command bindings and JetBrains defaults, incl. the palette toggle
  key (owned by 08).
- The project-switching command's logic (owned by 09 — it merely appears here).
- The `internal/config` loader implementation (owned by 04).
- Symbol/line/diagnostic search prefixes and other future modes beyond `:` and
  `@` (the `Mode` interface leaves room for them; they are not built here).
- Runtime/Wasm plugin loading (Roadmap 9900).
