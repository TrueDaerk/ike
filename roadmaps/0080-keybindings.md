# Roadmap 0080 — Keybindings & Shortcuts

Give IKE a coherent, JetBrains-flavoured keyboard layer. Roadmap 0020 already
defines the `Keymap` capability (a binding that emits a `tea.Msg`) and the
registry that collects them; Roadmap 0040 owns the `[keymap]` config section and
the merge/precedence machinery. **This roadmap owns the layer in between:** the
binding *model* (how a key-chord resolves to a registered Command), the
*default* JetBrains-like binding set, scope/context resolution, conflict
detection, platform normalisation, and a help/cheatsheet view.

It does **not** create any Commands. Commands are owned by the editor (06),
explorer (05), palette (07), project switching (09), and a future VCS/git
roadmap. This roadmap binds **keys → command ids** that those roadmaps register.

## Prerequisites / Dependencies

- **01 Foundation** — key events arrive as `tea.KeyMsg`; the root model in
  `internal/app` dispatches to the focused pane. This roadmap inserts a
  resolution step: the root model asks the keybinding resolver "does this chord
  (in this context) map to a command?" before/around pane dispatch, and on a hit
  emits the command's `tea.Msg`/`tea.Cmd` via `host.API`.
- **02 Plugins registry** — `internal/plugin` (`Command`, `Keymap`, …),
  `internal/registry`, `internal/host` (`host.API`). The `Keymap` capability and
  registry already exist. This roadmap builds the **binding table + resolver** on
  top of them. Conflict detection here complements (does not replace) the
  registration-time conflict checks in 02.
- **04 Settings** — `internal/config` provides the merged `[keymap]` section
  (`preset`, `[keymap.bindings]`) with precedence **defaults < user < project**.
  This roadmap defines the *content* of those defaults and the meaning of an
  override entry; it does not own the loader or the precedence mechanism.
- **07 Command palette** (consumer, does not block) — the palette lists Commands
  with their bound keys and shares the registry as the command source; it queries
  this package to render the "shortcut" column and reuses the same `context`
  notion for scope.

Downstream consumers (do not block this roadmap): 05, 06, 07, 09, and the future
git/VCS roadmap, all of which register the Commands these bindings target.

## Architecture

```
internal/keymap/
  chord.go        Chord type: ordered sequence of Key steps (single, modified, multi-step)
  key.go          Key type: base key + modifier set (Ctrl/Alt/Shift/Meta); parse "Cmd+K"
  parse.go        string <-> Chord ("cmd+k cmd+c") parsing + canonical formatting
  platform.go     platform normalisation (Meta/Cmd -> Ctrl on linux/windows; aliases)
  binding.go      Binding: Chord -> commandID, with Context/scope + source layer
  table.go        BindingTable: build from defaults + config overrides, indexed lookup
  resolver.go     Resolver: feed tea.KeyMsg, track multi-step chord state, resolve in context
  context.go      Context/scope enum (Global, Editor, Explorer, Palette, ...) + matching
  defaults.go     the JetBrains-like default binding set (the table below, as data)
  conflict.go     conflict detection: same chord+context -> two command ids; reporting
  fromkeymsg.go   tea.KeyMsg -> Key adapter (bubbletea key names -> our Key model)
  help.go         cheatsheet: list bindings (grouped by context) for a help overlay
  keymap_test.go  table-driven tests
```

Data flow:

```
config [keymap] (04) ─┐
defaults.go ──────────┼─► table.go (build + conflict.go check) ─► BindingTable
                       │                                                │
tea.KeyMsg (01) ─► fromkeymsg.go ─► resolver.go ──(context)──────────────┘
                                          │
                                          ▼
                              commandID ─► host.API dispatch (registered Command's tea.Msg)
```

- `internal/keymap` depends only on `internal/config` (read merged bindings) and
  `internal/plugin`/`internal/host` types (command ids, dispatch). It must not
  import concrete panes — same leaf-discipline as the rest of the contract.
- Override entries in `[keymap.bindings]` are plain data (`"cmd+d" = "editor.duplicateLine"`,
  or `"cmd+d" = ""` to unbind). They flow through 04's precedence before this
  package ever sees them, so user/project layers are already merged.

## Design rules

- **Bindings reference command ids, never define commands.** A `Binding` is
  `(Chord, Context) -> commandID`. If the command id is not registered, the
  binding is inert and surfaced as a non-fatal diagnostic (a binding may point at
  a command from a not-yet-loaded plugin).
- **Three chord shapes, one model.** A `Chord` is an ordered list of steps:
  single key (`esc`), modified key (`cmd+t`), or multi-step JetBrains-style
  (`cmd+k cmd+c`). The resolver holds partial-chord state with a short timeout;
  an incomplete prefix that times out is discarded (and may fall through to the
  focused pane).
- **Context/scope decides precedence.** A chord resolves against the active
  context stack (most specific first: focused pane context → `Global`). A
  pane-scoped binding shadows a global one for the same chord while that pane is
  focused. This `Context` value is shared with the palette (07).
- **Layered overrides, replace-by-key.** Effective table = defaults overlaid by
  user then project (from 04). An override for a chord replaces the default for
  that chord+context; an empty value unbinds it. Layering is per chord+context,
  not whole-table replacement.
- **Platform normalisation is explicit.** Author bindings with logical modifiers
  (`Cmd`/`Meta`); `platform.go` maps `Cmd`→`Ctrl` on Linux/Windows and keeps
  `Cmd`→`Meta` on macOS where the terminal forwards it. Normalisation happens
  once at table-build time so the resolver compares like-for-like.
- **Respect terminal limitations, document the escape hatch.** Many modifier
  combos (e.g. `Cmd+T`, `Ctrl+Tab`, `Cmd+,`) are intercepted by the terminal /
  OS and never reach the program; modifier+letter support is terminal-dependent.
  The design therefore: (a) keeps every action reachable via the command palette
  / `:` line (07) as the universal fallback, (b) provides an optional **leader
  key** (default `space` in non-editor contexts, or `Ctrl+K` prefix) so chorded
  actions work without OS-reserved modifiers, and (c) marks bindings known to be
  terminal-fragile so the cheatsheet can show the palette fallback.
- **Conflict detection at build time.** Two bindings with the same chord+context
  mapping to different command ids is a conflict: report all of them with their
  source layer, keep the highest-precedence one, and surface a diagnostic. Never
  silently shadow.
- **Boundary with the editor's modal engine (06).** Roadmap 0060 owns *vim*
  keymaps **inside** the editor (normal/insert/visual motions, operators,
  text objects) — those are handled by the editor pane itself and are NOT in this
  table. This roadmap owns **global / IDE-level** shortcuts (open file, search
  everywhere, switch pane, commit, save all, navigate back/forward). When the
  editor is in insert mode, IDE-level chords still resolve only via non-conflicting
  modified/leader chords; plain letters always go to the editor.

## Default bindings (JetBrains-like)

Logical chords (Cmd = Meta on macOS, normalised to Ctrl elsewhere). Each maps to
a command id **owned by another roadmap** — listed in the Owner column. Command
ids marked **(future VCS)** have no owning roadmap yet; bind them to placeholder
ids and flag the dependency (see Out of scope).

| Chord                    | Command id                  | Action                  | Context  | Owner (roadmap)        |
|--------------------------|-----------------------------|-------------------------|----------|------------------------|
| `cmd+k`                  | `vcs.commit`                | Commit                  | Global   | **(future VCS)**       |
| `cmd+t`                  | `vcs.updateProject`         | Update Project          | Global   | **(future VCS)**       |
| `cmd+d`                  | `editor.duplicateLine`      | Duplicate line(s)       | Editor   | Editor (06)            |
| `cmd+shift+a`            | `palette.searchEverywhere`  | Search everywhere       | Global   | Palette (07)           |
| `shift shift`            | `palette.searchEverywhere`  | Search everywhere (dbl) | Global   | Palette (07)           |
| `cmd+shift+o`            | `project.goToFile`          | Go to file              | Global   | Project (09)           |
| `cmd+o`                  | `project.goToClass`         | Go to symbol/class      | Global   | Project (09) / LSP (10)|
| `cmd+e`                  | `palette.recentFiles`       | Recent files            | Global   | Palette (07)           |
| `alt+f7`                 | `editor.findUsages`         | Find usages             | Editor   | Editor (06) / LSP (10) |
| `shift+f6`               | `editor.rename`             | Rename symbol           | Editor   | Editor (06) / LSP (10) |
| `cmd+/`                  | `editor.commentLine`        | Comment line            | Editor   | Editor (06)            |
| `cmd+shift+/`            | `editor.commentBlock`       | Comment block           | Editor   | Editor (06)            |
| `cmd+s`                  | `editor.save`               | Save                    | Editor   | Editor (06)            |
| `cmd+shift+s`            | `editor.saveAll`            | Save all                | Global   | Editor (06)            |
| `cmd+c`                  | `editor.copy`               | Copy                    | Editor   | Editor (06)            |
| `cmd+x`                  | `editor.cut`                | Cut                     | Editor   | Editor (06)            |
| `cmd+v`                  | `editor.paste`              | Paste                   | Editor   | Editor (06)            |
| `cmd+z`                  | `editor.undo`               | Undo                    | Editor   | Editor (06)            |
| `cmd+shift+z`            | `editor.redo`               | Redo                    | Editor   | Editor (06)            |
| `cmd+f`                  | `editor.find`               | Find in file            | Editor   | Editor (06)            |
| `cmd+r`                  | `editor.replace`            | Replace in file         | Editor   | Editor (06)            |
| `cmd+shift+f`            | `project.findInPath`        | Find in path            | Global   | Project (09)           |
| `cmd+shift+r`            | `project.replaceInPath`     | Replace in path         | Global   | Project (09)           |
| `cmd+left-bracket`       | `nav.back`                  | Navigate back           | Global   | Editor (06) / app (01) |
| `cmd+right-bracket`      | `nav.forward`               | Navigate forward        | Global   | Editor (06) / app (01) |
| `cmd+b`                  | `editor.gotoDeclaration`    | Go to declaration       | Editor   | Editor (06) / LSP (10) |
| `cmd+1`                  | `explorer.toggle`           | Toggle project tree     | Global   | Explorer (05)          |
| `ctrl+tab`              | `pane.switcher`             | Switch pane focus       | Global   | App (01) / window mgr  |
| `cmd+w`                  | `editor.closeTab`           | Close active tab        | Global   | Editor (06)            |
| `cmd+shift+t`            | `vcs.revertFile`            | Revert file             | Global   | **(future VCS)**       |
| `cmd+k cmd+c`            | `editor.commentLine`        | Comment (chord example) | Editor   | Editor (06)            |
| `cmd+k cmd+s`            | `palette.keymapHelp`        | Show keymap cheatsheet  | Global   | this roadmap → palette |
| `f1`                     | `palette.keymapHelp`        | Help / cheatsheet       | Global   | this roadmap → palette |

`cmd+k cmd+s` and `cmd+k cmd+c` demonstrate the multi-step chord form; the rest
are single or single-modified. Terminal-fragile entries (e.g. `ctrl+tab`,
`cmd+1`, `cmd+,`-style) carry a `fragile` flag so the cheatsheet shows the
palette/leader fallback.

## Milestones

- [ ] `key.go` + `chord.go`: `Key` (base + modifier set) and `Chord` (ordered steps) types.
- [ ] `parse.go`: parse/format chord strings (`"cmd+k cmd+c"`, `"shift shift"`) with canonical round-trip.
- [ ] `platform.go`: logical-modifier normalisation (Cmd→Ctrl off macOS) applied once at build time.
- [ ] `fromkeymsg.go`: adapt bubbletea `tea.KeyMsg` (key names + modifiers) into the `Key` model.
- [ ] `context.go`: `Context`/scope values + most-specific-first matching, shared with palette (07).
- [ ] `binding.go` + `table.go`: `Binding` type and `BindingTable` built from defaults + merged `[keymap]` overrides (unbind via empty value).
- [ ] `defaults.go`: the JetBrains-like default binding set above, expressed as data with owners/contexts/fragile flags.
- [ ] `conflict.go`: build-time detection of same-chord-same-context clashes; report source layers, keep highest precedence, emit diagnostics.
- [ ] `resolver.go`: feed `tea.KeyMsg`, track partial multi-step chord state with timeout, resolve against the context stack to a command id.
- [ ] Root-model integration (01/02): wire the resolver into `internal/app` dispatch; on resolve, fire the registered Command via `host.API`; unresolved keys fall through to the focused pane.
- [ ] Leader-key / palette fallback: optional leader prefix for terminal-reserved chords; ensure every bound action is reachable from the palette.
- [ ] `help.go`: cheatsheet data grouped by context (drives an overlay/palette help view; `palette.keymapHelp`).
- [ ] Tests: chord parse/format round-trip, platform normalisation, multi-step chord + timeout, context precedence + pane shadowing, layered override + unbind, conflict detection, inert-binding-on-missing-command, KeyMsg adapter mapping.
- [ ] Wiki: add a `keymap` concept doc under `wiki/` (frontmatter `type`, `resource: internal/keymap`), documenting the binding model, default table, context/scope, platform/terminal constraints + leader fallback; bump `timestamp` and add a `log.md` entry.

## Out of scope

- **Defining any Command** — editor (06), explorer (05), palette (07), project
  (09), and the future git/VCS roadmap own their commands; here we only bind keys
  to their ids.
- **The `[keymap]` config loader / precedence mechanism** — Roadmap 0040. We define
  override *semantics* and default *content*; 04 merges the layers.
- **Vim-internal keymaps inside the editor** — Roadmap 0060 owns normal/insert/
  visual motions, operators, and text objects within the editor's modal engine.
  This roadmap covers only global / IDE-level shortcuts.
- **A future VCS/git roadmap** — `vcs.commit`, `vcs.updateProject`,
  `vcs.revertFile` are bound to placeholder ids and are **flagged as a missing
  dependency**: a git/VCS roadmap must later register these commands for the
  bindings to become live.
- **Interactive keybinding editor UI / rebinding workflow** — overrides are made
  via config (04) for now; an in-app keymap editor is later/elsewhere.
- **Recording/replaying macros and multi-key text-object grammars** — not part of
  the global shortcut layer.
