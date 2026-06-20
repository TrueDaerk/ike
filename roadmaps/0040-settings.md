# Roadmap 0040 — Settings / Configuration

Give IKE a single, typed configuration system. Settings live in TOML files,
merge across well-defined layers, and are read everywhere through one package.
This roadmap owns the **schema skeleton, the defaults, and the merge/precedence
rules**; later roadmaps fill the content of their own sections (explorer colors,
keybindings, LSP servers, themes) through a small registration hook.

The aim is that by the end of this roadmap every subsystem can ask
`config.Get()` for typed values, and a user can drop a `settings.toml` in their
home directory or a project to override behaviour.

## Prerequisites / Dependencies

- **Roadmap 0010 (Foundation):** root model in `internal/app`; opening a file is a
  routed `tea.Msg`. Config changes that affect the UI are surfaced as a
  `tea.Msg` so the root model can react (re-theme, rebuild keymaps, etc.).
- **Roadmap 0020 (Plugins):** `internal/host` exposes `host.API` with a
  **read config** method. This roadmap provides the package that *backs* that
  method — `internal/host` depends on `internal/config`, not the reverse.
- A TOML library (e.g. `github.com/BurntSushi/toml` or
  `github.com/pelletier/go-toml/v2`). Pick one and pin it; isolate it behind the
  package so the rest of the codebase never imports TOML types directly.

Downstream roadmaps that **consume** this (do not block it): 05 Explorer,
08 Keybindings, 09 Project switching, 10 LSP.

## Architecture

```
internal/config/
  config.go        Config root struct (typed sections) + Get() accessor
  schema.go        section structs: Editor, Explorer, Keymap, LSP, Theme, Project
  defaults.go      built-in defaults (the lowest precedence layer)
  discovery.go     locate user (~/.ike/settings.toml) + project ({root}/.ike/settings.toml)
  load.go          read file -> decode TOML -> raw layer
  merge.go         layered merge (defaults < user < project), merge semantics
  validate.go      validation + clamping + actionable error messages
  extend.go        registration hook for roadmaps to add sections + defaults
  watch.go         (optional) file-watch -> ConfigReloadedMsg (tea.Msg)
  config_test.go   table-driven tests for discovery/merge/validate/extend
```

Data flow:

```
defaults.go ─┐
user toml  ──┼─► merge.go ─► validate.go ─► *config.Config ─► config.Get()
project toml ┘                                                      ▲
                                                                    │
internal/host (host.API.ReadConfig) ────────────────────────────────┘
```

- `internal/config` depends on nothing in IKE except a tiny `tea.Msg` for
  reload. It must stay leaf-level so any package can import it without cycles.
- `host.API` reads through `internal/config`; plugins never touch the package
  directly (keeps the Wasm bridge in Roadmap 9900 simple — config crosses the
  boundary as plain data).

## Design rules

- **Precedence:** built-in defaults < user-global < project. Project always
  wins. A field absent from a higher layer inherits the lower layer — never
  zero it out.
- **Merge semantics, made explicit:**
  - Scalars (int/bool/string): higher layer replaces lower.
  - Tables/sections (e.g. `[lsp]` server entries, `[explorer]` color map):
    merged key-by-key; higher layer adds/overrides individual keys, lower-layer
    keys survive.
  - Lists (e.g. `project.history`): **replace by default**, not append. Document
    this clearly; appending is surprising for ordered/bounded lists.
- **Typed access only.** Outside `internal/config`, code reads strongly typed
  structs — no stringly-typed map lookups, no TOML types leaking out.
- **Defaults are code, not a shipped file.** The default layer is constructed in
  `defaults.go` so IKE works with zero config files present.
- **Validation clamps, then warns.** Invalid values fall back to the default and
  produce a non-fatal, actionable diagnostic (bad config must never crash the
  IDE). Only a TOML parse error is hard-failed for that one file (lower layers
  still apply).
- **Discovery is explicit and overridable.** Honor `IKE_CONFIG_DIR` /
  `--config` style override for tests and power users; project root is detected
  the same way the rest of IKE detects it (passed in, not guessed in this pkg).
- **One write path is out of scope here** — this roadmap is read/merge only;
  writing config back (e.g. updating `project.history`) is exposed as a typed
  setter API but its UX lives with the owning roadmap (09).
- **Extension without edits:** other roadmaps register their section + defaults
  via `extend.go` rather than editing the core structs where practical; the
  baseline structs define the *slots*, downstream defines the *entries*.

## Baseline schema + defaults

Concrete defaults this roadmap can commit to now:

- `[editor]` — `tab_width = 4`, `use_spaces = true`, `line_numbers = true`,
  `relative_line_numbers = false`, `wrap = false`, `scroll_off = 3`,
  `auto_indent = true`, `trim_trailing_whitespace = true`,
  `insert_final_newline = true`, `show_whitespace = false`.
- `[explorer]` — `show_hidden = false`, `git_status = true`,
  `tree_indent = 2`, `sort = "name"`; plus a `[explorer.colors]` per-filetype
  color map **slot** (left empty here — Roadmap 0050 fills concrete entries).
- `[keymap]` — `preset = "jetbrains"`; an `[keymap.bindings]` override **slot**
  (empty here — Roadmap 0080 supplies the JetBrains default bindings + semantics).
- `[lsp]` — `enabled = true`, `log_level = "warn"`; a `[lsp.servers]` table
  **slot** (empty here — Roadmap 0100 fills per-language server entries).
- `[theme]` — `name = "default"`, `dark = true` (theme contents owned by the
  theme/explorer work; this defines the selector).
- `[project]` — `history = []`, `max_history = 20`, `restore_last = false`.

## Milestones

- [x] `internal/config` skeleton: root `Config` + section structs (`Editor`, `Explorer`, `Keymap`, `LSP`, `Theme`, `Project`) with the schema slots above.
- [x] `defaults.go`: built-in default layer with the concrete baseline values listed.
- [x] `discovery.go`: locate `~/.ike/settings.toml` and `{project_root}/.ike/settings.toml`; honor `IKE_CONFIG_DIR` / explicit-path override.
- [x] `load.go`: TOML decode behind an internal boundary (no TOML types exported).
- [x] `merge.go`: layered merge (defaults < user < project) with documented scalar/table/list semantics.
- [x] `validate.go`: clamp-and-warn validation, per-field defaults fallback, non-fatal diagnostics.
- [x] Typed accessor: `config.Get()` / `config.Load(...)` returning the merged, validated `*Config`.
- [x] `extend.go`: registration hook so other roadmaps add a named section + defaults; document the contract.
- [x] Back `host.API` read-config: wire `internal/host` to `internal/config`.
- [~] (Optional) `watch.go`: reload seam present — `ConfigReloadedMsg` `tea.Msg` + `Reload(opts)` cmd the root model can route; actual fs-watching deferred to its owning roadmap.
- [x] Tests: discovery precedence, scalar/table/list merge, clamp-and-warn validation, parse-error isolation, extend-registration round-trip.
- [x] Wiki: document config locations, precedence, the baseline schema, and the section-extension hook under `wiki/`.

## Out of scope

- Concrete `[explorer.colors]` entries and explorer rendering behavior — **Roadmap 0050**.
- Keybinding semantics and the JetBrains default bindings list — **Roadmap 0080**.
- Per-language `[lsp.servers]` specifics and LSP lifecycle — **Roadmap 0100**.
- Project-switch UX and how `project.history` is presented/edited — **Roadmap 0090**.
- Theme palette definitions and rendering (only the theme *selector* lives here).
- A settings editor UI / interactive config TUI.
