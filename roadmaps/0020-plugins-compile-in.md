# Roadmap 0020 — Plugins: Compile-in Registry

Establish the extension architecture. Plugins are Go packages compiled into the
binary and self-register via `init()`. No runtime loading yet — this roadmap
exists to define the **extension points** and the **stable internal API** that a
later Wasm layer (Roadmap 9900) plugs into without a rewrite.

## Why first

The mechanism is cheap (a map + `init()`), but the contract is expensive to get
wrong. Defining extension points now forces the foundation (explorer, editor) to
sit behind interfaces, so adding out-of-process/Wasm plugins later only adds a
new *producer* of already-registered extensions.

## Extension points (the contract)

Everything an extension can contribute, expressed in `tea.Msg`/`tea.Cmd` terms:

- **Command** — named action invokable from the command palette / `:` line.
- **Keymap** — binding that emits a `tea.Msg` (layered: plugin bindings never
  shadow core without explicit priority).
- **Pane** — a `tea.Model`-shaped component the window manager can host.
- **FileHandler** — opener/renderer keyed by extension or content sniff.
- **Hook** — subscribe to lifecycle `tea.Msg`s (file opened, buffer saved, etc.).

## Architecture

```
internal/plugin/        Plugin interface + capability types (Command, Keymap, …)
internal/registry/      global registry: Register(), lookups, ordering
internal/host/          host API surface plugins call (open file, set status, dispatch msg)
plugins/                first-party plugins, each its own package with init()
  plugins/example/      reference plugin exercising every extension point
```

Wiring: `cmd/ike/main.go` blank-imports the enabled plugin packages
(`import _ "ike/plugins/example"`); their `init()` calls `registry.Register`.
Root model queries the registry at startup to build keymaps, palette, handlers.

## Design rules

- Plugins depend only on `internal/plugin` + `internal/host` — never on concrete
  pane/editor types. Keeps the contract narrow and Wasm-portable.
- Host calls go through an interface (`host.API`), not globals — Roadmap 9900 swaps
  the in-process impl for a Wasm-bridged one behind the same interface.
- Capabilities are data + a callback, not inheritance. Each carries an owner id
  for diagnostics and ordering.
- Conflicts (duplicate command name, key clash) are detected at registration and
  surfaced, not silently overwritten.

## Milestones

- [ ] `internal/plugin`: define `Plugin` interface + `Command`, `Keymap`, `Pane`, `FileHandler`, `Hook` types.
- [ ] `Command` carries a `Scope` field (global / pane-context id) — additive, required by the command palette (Roadmap 0070) and keybinding resolution (Roadmap 0080) for context-aware filtering. Define a small pane-context advertisement mechanism so a focused pane reports its context id.
- [ ] `internal/registry`: `Register`, dedupe/conflict detection, deterministic ordering, lookups.
- [ ] `internal/host`: `host.API` interface (open file, dispatch msg, set status, read config) + in-process impl.
- [ ] Root model consumes registry: build command palette, merge keymaps, route file opens through handlers.
- [ ] `plugins/example`: reference plugin hitting every extension point.
- [ ] Enable/disable plugins via config (build-time list + runtime on/off flag).
- [ ] Tests: registration, conflict detection, ordering, dispatch round-trip, handler resolution.
- [ ] Wiki: document the extension contract under `wiki/`.

## Out of scope (Roadmap 9900)

Runtime loading, `.wasm` plugins, sandboxing, cross-language plugins, plugin
install/distribution.
