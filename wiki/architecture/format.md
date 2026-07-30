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
2. **TierExternal** — the language plugin's default external command (#1402)
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
