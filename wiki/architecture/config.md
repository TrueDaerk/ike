---
type: concept
title: Configuration System
description: Single typed configuration package — TOML files merged across defaults < user < project, clamp-and-warn validation, an extension hook for downstream sections, and a flat read-only view backing the plugin host API.
resource: internal/config/config.go
tags: [architecture, config, toml, merge, precedence, validation, plugins]
timestamp: 2026-07-07T00:00:00Z
---

# Configuration System

Roadmap 0040. IKE reads all settings through one leaf-level package,
`internal/config`. Settings live in TOML files that merge across three layers and
are exposed as strongly typed structs; no TOML type leaks past the package.

## Locations & precedence

Three layers merge lowest-to-highest, **project always wins**:

1. **Built-in defaults** — constructed in code (`defaults.go`), so IKE runs with
   zero config files present.
2. **User-global** — `~/.ike/settings.toml`, or `$IKE_CONFIG_DIR/settings.toml`
   when that env var is set (the test / power-user override).
3. **Project** — `{project_root}/.ike/settings.toml`. The root is passed in, never
   guessed inside the package.

A field absent from a higher layer inherits the lower layer; it is never zeroed.

## Merge semantics

Merging happens on the raw decoded maps (`merge.go`), then the combined override
map is decoded onto the defaults-filled struct (`load.go`):

- **Scalars** (int/bool/string): higher layer replaces lower.
- **Tables/sections** (`[explorer.colors]`, `[lsp.servers]`, …): merged
  key-by-key; a higher layer adds/overrides individual keys, lower keys survive.
- **Lists** (`project.history`): **replace by default**, never append — appending
  is surprising for bounded/ordered lists.

## Validation: clamp, then warn

`validate.go` enforces ranges and enums by **falling back to a default and
emitting a non-fatal `Diagnostic`**. Bad config must never crash the IDE. Only a
TOML *parse* error hard-fails a single file — its layer is dropped and the lower
layers still apply, reported as a file-sourced diagnostic. Clamped today:
`editor.tab_width >= 1`, `editor.scroll_off >= 0`, `explorer.tree_indent >= 0`,
`project.max_history >= 0`, `explorer.sort` ∈ {name,type,size,modified},
`lsp.log_level` ∈ {error,warn,info,debug}, and `project.history` truncated to
`max_history`.

## Baseline schema

Sections and their default-bearing slots (`schema.go`):

- `[editor]` — tab width, spaces, line numbers, wrap, scroll-off, auto-indent,
  trim/insert-newline, show-whitespace.
- `[explorer]` — show-hidden, git-status, tree-indent, sort, plus an empty
  `[explorer.colors]` slot (Roadmap 0050 fills entries).
- `[keymap]` — `preset` + an empty `[keymap.bindings]` slot (Roadmap 0080).
- `[lsp]` — enabled, log-level + an empty `[lsp.servers]` slot (Roadmap 0100).
- `[theme]` — `name`, `dark` (the selector; palettes owned by Roadmap 0110).
- `[project]` — history list, `max_history`, `restore_last` (UX in Roadmap 0090).

## Extension hook

Downstream roadmaps grow config additively via `extend.go` rather than editing
the core structs. An `Extension` supplies `Defaults(*Config)` (runs on the base
defaults *before* merge, so a user can still override them) and optional
`Validate(*Config) []Diagnostic` (runs on the final config). `Register` is
idempotent by `Name`. The baseline structs define the *slots*; extensions define
the *entries*.

## Accessors, reload & writes

- `Load(Options) (*Config, []Diagnostic)` runs the whole pipeline and never errors.
- `Set` / `Get` hold the process-wide config; `Get` returns defaults before the
  first `Set`.
- `watch.go` defines `ConfigReloadedMsg` (a `tea.Msg`) and `Reload(opts)` — the
  reload seam; actual file-watching is left to its owning roadmap.
- `write.go` exposes the typed setter seam (e.g. `PushHistory`) with the bounded
  semantics.

## Write-back (Roadmap 0160, #89)

`write.go` also persists single keys back to disk — the layer every settings-UI
control writes through:

- `WriteKey(opts, scope, key, value)` / `RemoveKey(opts, scope, key)` set or
  delete one dotted key (`"editor.tab_width"`) in the **user** or **project**
  settings file, creating it (and `.ike/`) when missing. The file round-trips
  through the TOML parser: unknown keys survive untouched (comments are not
  preserved — the library re-emits the document). A file that no longer parses
  is left alone and the error returned; write-back never destroys a
  recoverable config.
- **Scope model:** `DefaultScope(key)` names the conventional layer —
  `project.*`, `lsp.servers.*` and `toolchain.*` are project-scoped, everything
  else (theme, keymap, editor look & feel) user-scoped. Callers may pass an
  explicit scope.
- **Reset to default** = `RemoveKey`: the value falls back through
  defaults < user < project; emptied sections are pruned.
- `WriteAndReload` / `RemoveAndReload` (watch.go) chain the write with the
  normal `Load` pipeline and deliver `ConfigReloadedMsg` — a UI change applies
  through exactly the flow a manual file edit takes; write failures surface as
  a `Diagnostic` on the message.

## Host integration

`internal/host` depends on `internal/config` (never the reverse).
`host.FromConfig(*config.Config)` flattens the typed schema to dotted string keys
via `Config.Flat`, backing the read-only `host.API.Config()` view plugins use.
The typed structs stay the single source of truth for those key names; plugins
read config as plain data and never import the package.
