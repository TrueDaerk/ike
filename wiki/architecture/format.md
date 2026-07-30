---
type: concept
title: Formatter Registry
description: The neutral reformat layer — providers (config override, external command, LSP, built-in) registered per language, one resolution chain behind lsp.format/lsp.formatRange and format-on-save, edits applied as one undo unit.
resource: internal/format
tags: [architecture, format, reformat, registry, lsp, plugins]
timestamp: 2026-07-30T00:00:00Z
---

# Formatter Registry

Roadmap 0470 (#1400, #1401). Reformat used to have exactly one hard-wired
source: `textDocument/formatting` through the LSP manager, gated on the
server's capability. For every language whose server offers no formatting
(pyright, marksman, ansible-language-server) and every language without a
server at all (`.sql`, `.xml`), `cmd+alt+l` did nothing. The formatter
registry (`internal/format`) generalizes the source: **one resolution chain,
one command**, with the language server as just one entry.

## Package & shape

`internal/format` is a leaf package like `internal/lang`: pure Go, no
bubbletea, no LSP import, so the editor, the app and every plugin can depend
on it without a cycle. Providers register from `init()`:

```
format.Register(format.Provider{
    Name:      "built-in",        // status-line label; NameFor overrides per path
    Languages: []string{"sql"},   // empty = every language (the LSP provider)
    Tier:      format.TierBuiltin,
    Available: func(path string) bool { ... },   // binary on PATH, server ready …
    Format:    func(ctx, req) (format.Result, error) { ... },
    FormatRange: nil,             // optional; nil = no range support
})
```

- `Request` carries the buffer snapshot (`Path`, `Language`, `Lines`) plus
  `Options` — the **effective per-buffer settings** (indent style/width, max
  line length, final newline) resolved by the editor through its usual
  layering (built-in defaults < IKE config < language default <
  [.editorconfig](/architecture/editorconfig.md)); `editor.FormatOptions()`
  is the accessor. Providers must honour them so their output agrees with the
  editor.
- `Result` is either full text (`TextResult`) or `[]Edit` (0-based rune
  coordinates, the `lsp.FormatEdit` convention). `EditsForResult` normalizes:
  text answers get a minimal line diff (common prefix/suffix trimmed, middle
  replaced), so the cursor stays put for local changes.

## Resolution chain

`Resolve(langID, path)` walks the registered providers in tier order and
returns the first whose `Available` answers true:

1. **TierOverride** — explicit `[format.<languageID>]` config (#1402)
2. **TierExternal** — the language plugin's default external command formatter
3. **TierLSP** — `textDocument/formatting` via the attached server
4. **TierBuiltin** — a built-in Go formatter shipped by the language plugin
   (#1403 SQL, #1404 XML)

`ResolveRange` is the reformat-selection variant: a range-less winner falls
through to the next range-capable provider; when none of the available
providers does ranges the caller reports "only Reformat File is available"
instead of silently formatting the whole file. No provider at all is a clear
"no formatter for `<language>`" toast, never a silent no-op.

## Command flow

The `plugins/format` compile-in plugin owns the commands. The ids stay
`lsp.format` / `lsp.formatRange` (keymaps — `cmd+alt+l` —, JetBrains imports
and user bindings keep working) but the palette titles are language-neutral:
**Reformat File** / **Reformat Selection**. The commands dispatch
`format.FileRequestMsg` / `RangeRequestMsg`; the app's handler
(`internal/app/reformat.go`) — the only layer holding the active buffer, its
effective options and the view — resolves the chain and runs the winner off
the Update loop (time-boxed). Results return on the established path: an
`ilsp.FormatEditsMsg` applied as **one undo-able edit** through exactly one
view (`editor/textedit.go`, cursor clamped), plus a status toast naming the
source (`reformat: gopls`, `reformat: built-in`). A provider error leaves the
buffer untouched and surfaces as an error toast — failure is visible, never
destructive.

The LSP entry (`plugins/lsp/provider.go`) serves every language; its
`Available` asks the manager for a ready server with the formatting
capability, `NameFor` reports the server binary's name for the status toast,
and its `Format` reads only `Request.Path` — the manager owns the synced
document text and the UTF-16 conversion, exactly as before.

## External command formatters (#1402)

`format.External` (external.go) is the generic ecosystem-CLI invoker: a
command line with per-invocation placeholders (`${FILE}`, `${TAB_WIDTH}`,
`${INDENT_STYLE}`, `${MAX_LINE_LENGTH}`; `${START_LINE}`/`${END_LINE}` in
`RangeArgs`), the buffer on **stdin** and the formatted text on **stdout** —
or *temp-file mode* for tools that cannot read stdin: the buffer is written
to a temp copy (same extension), the tool formats it in place via `${FILE}`,
the copy is read back. The tool runs with the project root as cwd (so it
picks up its own project config), under the caller's timeout context
(`WaitDelay` force-closes pipes a killed tool's children hold), behind a
10 MB size guard, with stderr captured and truncated into the error. The
original file is **never** written by the tool — output flows back through
the registry like every other provider's.

Two registration paths:

- **Plugin defaults** (`RegisterExternalDefault`, TierExternal): a language
  plugin declares its ecosystem tool (#1405 wires Python/Markdown/Shell/
  Ansible); the spec is also recorded for the settings page.
- **Config overrides** (`plugins/format.overrideProvider`, TierOverride): a
  `[format.<languageID>]` table — `command`, `args`, `range_args`,
  `temp_file`, `install`, `enabled` — layered user < project like
  `[lsp.servers.*]`, read live from `config.Get()` on every resolution so
  edits apply without a restart. `enabled = false` turns a language's
  external formatting off entirely (override *and* plugin default, via the
  `SetExternalEnabled` hook).

`Available` gates on the binary being on PATH; a missing binary raises the
**one-time install hint** (the #1067 companion-hint pattern, `SetNotifier`
wired to a warn toast by the app) naming the install command, then the chain
falls through to the next tier. `range_args` opts into reformat-selection
(1-based inclusive line numbers); absent means the registry's usual range
fallback. The **Formatters settings page**
(`internal/settings/format_page.go`, registered by `plugins/format`) lists
each language's effective command, the supplying layer (plugin default /
user / project) and whether the binary was found.

## Format-on-save

The [save chain](/architecture/lsp.md) (#1148) routes its format step through
the registry too: `editor.beginSaveChain` passes the buffer snapshot and
options in the `ilsp.SaveChainRequest`, and the bridge's `formatStep` resolves
the same chain — so external and built-in formatters apply on save with the
existing `editor.format_on_save` setting, for languages that never had a
capable server. Organize-imports stays LSP-only. Text-based providers get the
freshest lines available (the manager's synced document when a server tracks
the file, else the snapshot — no server means no organize step ran, so it
cannot be stale).
