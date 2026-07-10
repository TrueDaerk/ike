---
type: concept
title: Plugin Extension Contract
description: Compile-in plugin registry — the extension points (Command, Keymap, Pane, FileHandler, Hook), the host API, and how the root model consumes them.
resource: internal/plugin/plugin.go
tags: [architecture, plugins, extension, bubbletea]
timestamp: 2026-07-10T16:20:00Z
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

## WASM runtime (Roadmap 9900, #23)

`internal/wasm` embeds [wazero](https://github.com/tetratelabs/wazero)
(pure Go, no CGo) as the second plugin producer, alongside the compile-in
registry:

- **Scan**: `Runtime.ScanDir` loads every `.wasm` in the plugins directory
  (`$IKE_CONFIG_DIR/plugins`, else `~/.ike/plugins`) at startup, in
  deterministic name order; a faulting file is recorded as a diagnostic and
  skipped — one broken plugin never stops the scan or IKE.
- **Lifecycle**: load → compile → instantiate → (unload | Close). Both WASI
  conventions are supported: command modules run `_start` (a clean
  `proc_exit(0)` is a normal end, not a fault), reactor modules (Go's
  wasip1 `-buildmode=c-shared`, TinyGo's default) initialize via
  `_initialize` and keep their exports callable — the shape plugins use.
  `Module.ExportedFunction` is the seam the ABI (#24) calls through.
- **Safety posture** (full sandbox rules are #27): WASI with no preopened
  filesystem, no environment, no args — no ambient FS or network; guest
  stdout/stderr are sunk so a chatty module cannot corrupt the TUI frame;
  any load/instantiate/start fault isolates and unloads that module only.

### ABI (#24)

`internal/wasm/abi` fixes the host↔guest contract. Wasm passes only numbers,
so every richer value crosses as a byte region in guest linear memory:
arguments as `(ptr, len)` u32 pairs (host→guest buffers via the guest's
exported `ike_alloc`), guest returns as one packed u64 `(ptr<<32)|len`, and
payload bytes as JSON — language-neutral by construction (Rust/Zig guests
need any JSON library, nothing assumes Go; the Go SDK, #26, is merely the
first client). Unknown JSON fields are tolerated on both sides for forward
compatibility.

- **Guest exports**: `ike_alloc(size)`, `register() → Capabilities JSON`
  (name, commands, keymaps, hooks), `on_command(id)`, `on_key(id)`,
  `on_hook(id, payload)`.
- **Host imports** (module `"ike"`, thin marshalling shims mirroring
  `host.API` through the narrow `abi.Host` interface): `open_file`,
  `dispatch` (typed envelope; unknown types rejected, not guessed),
  `notify`, `set_status`, `config_get` (result written back through
  `ike_alloc`). Malformed guest payloads are dropped — a plugin cannot
  crash the host with garbage bytes.

The contract is verified end to end against a real Go wasip1 c-shared
guest that registers capabilities and answers `on_command` through every
shim.

### Capability bridge (#25)

`internal/wasm/bridge` closes the loop: after the scan, `RegisterModules`
calls each module's `register()` and adapts the declared descriptors into a
`plugin.Plugin` added to the registry — from then on a WASM module is
indistinguishable from a compile-in plugin (palette, keymaps, help sheet,
settings Plugins page, `plugins.<id>.enabled` toggles all apply unchanged).

- **Translation**: `CommandDesc` → `plugin.Command` (empty context = global,
  else `PaneScope`), `KeymapDesc` → `plugin.Keymap` at `CorePriority`,
  `HookDesc` → `plugin.Hook` (`file_opened` / `buffer_saved` /
  `buffer_closed`; unknown events are skipped). The plugin id is the guest's
  self-declared name, falling back to the file base name.
- **Callbacks off the Update loop**: every guest entry point runs inside the
  returned `tea.Cmd`, so a slow or faulting guest never stalls Update;
  faults surface as warn toasts. Hook payloads cross as JSON bytes.
- **Host adapter**: `bridge.HostAdapter` implements `abi.Host` over the real
  `host.API`, binding late — `main.go` instantiates the `"ike"` host module
  before any guest loads and calls `SetAPI` once the model exists (calls
  before binding are dropped). Severity 0/1/2 maps to info/warn/error;
  guest-initiated opens re-enter Update via `Send(OpenFileRequest)`; the
  `open_file` dispatch envelope is the one mapped type, unknown types warn.
- **Failure posture**: a module whose `register()` traps is unloaded with a
  diagnostic; a module without the export stays loaded but contributes
  nothing.

Parity is pinned by tests: the same capability set registered once
compile-in and once via a real WASM guest is asserted identical through
every registry view (`Commands`, `Keymaps`, `Binding`, `Hooks`, context
scoping) and produces identical notifications when invoked.

### Guest SDK (#26)

`sdk/` (a nested Go module, `ike/sdk`) is the typed guest-side SDK: plugin
authors declare commands/keymaps/hooks as Go callbacks in `init()` and call
typed host functions; the SDK owns the wasm exports, the allocator, and the
JSON marshalling. `sdk/example/` is a buildable reference plugin; the full
pipeline (build → scan → register → invoke) is pinned by a test. Authoring
guide: [Writing WASM Plugins](/architecture/plugin-authoring.md). The
remaining 9900 slice is sandbox limits + manifest validation (#27).
