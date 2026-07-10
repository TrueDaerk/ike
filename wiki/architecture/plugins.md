---
type: concept
title: Plugin Extension Contract
description: Compile-in plugin registry — the extension points (Command, Keymap, Pane, FileHandler, Hook), the host API, and how the root model consumes them.
resource: internal/plugin/plugin.go
tags: [architecture, plugins, extension, bubbletea]
timestamp: 2026-07-10T18:30:00Z
---

# Plugin Extension Contract

Roadmap 0020. Establishes the extension architecture. Plugins are Go packages
compiled into the binary that self-register from `init()`. There is no runtime
loading yet — this slice exists to fix the **extension points** and the **stable
internal API** that the later Wasm layer (roadmap 9900) plugs into without a
rewrite. The mechanism is cheap (a map + `init()`); the contract is what matters.

## Structure

```
internal/plugin/    Plugin interface + capability types (Command, Keymap, …)
internal/host/      host.API surface plugins call (open file, dispatch, status, config)
internal/registry/  global registry: Register(), conflict detection, ordering, lookups
plugins/example/    reference plugin exercising every extension point
```

Dependency direction (kept narrow and Wasm-portable):

```
host  ← plugin  ← registry  ← app
        (plugin callbacks take host.API; host never imports plugin)
```

`cmd/ike/main.go` blank-imports the enabled plugin packages
(`import _ "ike/plugins/example"`); their `init()` calls `registry.Register`.
The root model queries `registry.Global()` at startup to build the palette,
merge keymaps, and route file opens.

## Extension points

Everything a plugin contributes is **data plus a callback**, never inheritance.
Each capability is owned by the plugin id for diagnostics and ordering. A plugin
returns them all from `Capabilities()`:

- **Command** — named action for the palette / `:` line. Carries a `Scope`
  (global or pane-context id) so the palette and keybinding resolution can filter
  by the focused pane's context.
- **Keymap** — key binding that runs an action. Layered by `Priority`: a plugin
  binding never shadows a core binding unless its `Priority` exceeds
  `plugin.CorePriority`.
- **Pane** — a `tea.Model`-shaped component the window manager can host.
- **FileHandler** — opener keyed by file extension or a content sniff (`Match`).
- **Hook** — subscribes to a lifecycle `Event` (`EventFileOpened`,
  `EventBufferSaved`, `EventBufferClosed`).

### Scope and pane context

`Scope{Global: true}` always applies; `Scope{ContextID: "editor"}` applies only
when the focused pane advertises that context id. Core panes advertise
`"explorer"` / `"editor"`; a plugin pane advertises its own by implementing
`plugin.ContextProvider` (`ContextID() string`). `Scope.Matches(ctxID)` is the
single resolution rule used by both commands and keymaps.

## Host API

Plugins affect the editor only through `host.API` — never globals — so roadmap
9900 can swap the in-process impl for a Wasm bridge behind the same interface:

```go
type API interface {
    OpenFile(path string) tea.Cmd   // → host.OpenFileRequest, routed via handlers
    Dispatch(msg tea.Msg) tea.Cmd   // re-inject a message into Update
    SetStatus(text string)          // transient status-line text
    Config() Config                 // read-only key/value config
}
```

## Registry semantics

- **Deterministic ordering.** Lookups sort enabled plugins by id, then results by
  capability id; keymaps order by `Priority` (desc) then owner.
- **Conflict detection, not silent overwrite.** `Conflicts()` reports duplicate
  command/pane ids, file-extension claims contested by two handlers, and
  same-key bindings sharing a priority (ambiguous). Duplicates are dropped
  (first owner by sorted order wins), and the clash is surfaced.
- **Enable/disable.** Build-time set = which packages are blank-imported.
  Runtime on/off = `SetEnabled`, driven from config keys
  `plugins.<id>.enabled = false` (a real `[plugins]` config section since
  #133, applied symmetrically on every reload — flipping the toggle back
  re-enables). Disabled plugins vanish from every lookup;
  `Registry.Describe` still lists them with their contributed capabilities,
  which backs the settings panel's **Plugins page** (#133): one row per
  plugin with id, live state, capability summary and an expandable
  inspection; `e` toggles through the write-back layer. Language packages
  register through `plugins/languages/register` — the lang registry entry
  plus a `lang-<id>` plugin shim (dash, not dot: toggles persist as dotted
  keys) — so disabling a language plugin takes its LSP server with it, and
  enabling one kicks the missing-server install (#131) via
  `lsp.installMissing`.

## Root model wiring

`internal/app` consumes the registry:

- **File opens** (`explorer.OpenFileMsg` / `host.OpenFileRequest`) go through
  `ResolveHandler`: a claiming handler's `Open` runs, otherwise the file loads
  into the editor. `EventFileOpened` hooks fire either way.
- **Keys** resolve against `ResolveKey(keys, focusContext)` before the core
  switch; a plugin binding only pre-empts a core key when it out-prioritises it.
- **Commands** run via `Model.RunCommand(id)` — the seam the command palette
  (roadmap 0070) drives.

## Out of scope (roadmap 9900)

Runtime loading, `.wasm` plugins, sandboxing, cross-language plugins, and plugin
install/distribution.
