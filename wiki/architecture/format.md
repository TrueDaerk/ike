---
type: concept
title: Formatter Registry
description: The neutral reformat layer — providers (config override, external command, LSP, built-in) registered per language, one resolution chain behind lsp.format/lsp.formatRange and format-on-save, edits applied as one undo unit.
resource: internal/format
tags: [architecture, format, reformat, registry, lsp, plugins]
timestamp: 2026-08-07T00:00:00Z
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
through to the next range-capable provider. No provider at all is a clear
"no formatter for `<language>`" toast, never a silent no-op.

The plain reformat command is **context-sensitive** (#1603, JetBrains'
Reformat Code semantics): with an active visual selection `lsp.format`
formats only the selected range via `ResolveRange`; without one it formats
the whole file. When a selection exists but no available provider does
ranges, the command widens to the whole file with a notice ("no range
formatter … — reformatting the whole file") — never silently. The explicit
`lsp.formatRange` command keeps its stricter contract: no selection is a
"select a range first" hint, and no range-capable provider reports "only
Reformat File is available" instead of widening.

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
source (`reformat: gopls`, `reformat: built-in`). Because this apply bypasses
the editor's own Update loop, the handler then drops the applying view's
highlight/conceal caches and schedules a fresh parse
(`editor.Model.ReparseEdits`, #1683) — the change-sync broadcast already does
the same for other views of the document. A provider error leaves the
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
fallback.

## Formatters settings page (#1402, #1662)

`internal/settings/format_page.go` (registered by `plugins/format`) lists one
row per language with a formatter — plugin default, built-in and/or
`[format.<languageID>]` override — with the effective command, the supplying
layer (plugin default / user / project), whether the binary was found and the
built-in's switch state. Since #1662 it **writes** those overrides instead of
only reporting them, reaching parity with the Language Servers page:

- `e` toggles `enabled`, `b` toggles `builtin` (only where a built-in exists;
  otherwise the row says so).
- `enter` (or a press on the selected row) pushes `formatForm`
  (`format_form.go`), a sub-panel over the whole table: `command` with path
  completion through the shared `pathSuggest`, `args`, `range_args`,
  `temp_file`, `install`, plus the keys the language's built-in declares.
  The form seeds from the **effective** values, and saving writes only what
  differs — a field equal to the plugin default, or emptied, has its key
  *removed*, so an override file never freezes a default. Validation
  (booleans, declared value sets, `range_args` without any command) happens
  before anything reaches disk; the whole table is one `ApplyAndReload`
  batch, one reload.
- `r` resets the language: every key this page owns, removed from every
  addressable layer, back to the plugin default.
- `s` picks the write layer (project ↔ user), like the panel's own scope
  selector (#794); it starts on the conventional layer for `format.*`
  (project) and falls back to user when there is no project to write to.

The page never learns about individual languages. A plugin that ships a
built-in formatter declares it with `format.RegisterBuiltin(langID, keys…)`
(`internal/format/settings.go`): that both marks the `builtin` switch as
applicable and declares the built-in's own `[format.<lang>]` keys as
`format.ConfigKey` values (key, accepted values, default, help) — SQL's
`keywords`, for instance — which the form renders and validates generically.

## Built-in SQL formatter (#1403)

SQL is the language with no dependable external option, so the SQL plugin
ships its own formatter (`plugins/languages/sql/formatter.go`): a pure-Go
lexer + clause-layout printer (runs without CGo) with the Tree-sitter parse
as validity gate where available (`parsecheck_cgo.go`) — SQL that fails to
lex (unterminated string/comment, unbalanced parens) or parse is left
untouched. Layout: clause-per-line (SELECT/FROM/JOIN/ON/WHERE/GROUP BY/
HAVING/ORDER BY/LIMIT), select lists / SET assignments / CREATE TABLE column
lists one item per line indented one level, AND/OR chains broken under
WHERE/ON/HAVING, subqueries as indented blocks with the closing parenthesis
on its own line. Keyword casing via `[format.sql] keywords = "upper" |
"lower" | "preserve"` (upper default); identifiers, strings and quoted
identifiers are never re-cased. Comments stay with their statement (trailing
end-of-line comments remain trailing); statements separate with one blank
line, `;` kept. Output is idempotent (golden-tested). Range formatting
reformats exactly the statements overlapping the selection. The provider
registers **above the LSP tier** — #1403 pins the built-in over sqls —
and `[format.sql] builtin = false` restores the sqls path. Both keys are
declared through `RegisterBuiltin("sql", ConfigKey{Key: "keywords", …})`, so
the Formatters settings page edits them without knowing SQL exists.

## Built-in XML formatter (#1404)

XML has no server in IKE (lemminx is a JVM application, #1253), so the XML
plugin ships a pretty-printer for `.xml` and its dialects
(`.xsd/.xsl/.xslt/.svg/.plist/.wsdl/.csproj` & co):
`plugins/languages/xml/formatter.go`, a pure-Go tokenizer + tree + printer
with the Tree-sitter parse as a second validity gate (documents that fail to
parse are left untouched with a message). Element nesting indents per
editorconfig/settings; attributes stay on one line until the effective
`max_line_length`, then wrap one per line aligned under the first (0 = never
wrap). Passed through verbatim: XML declaration, DOCTYPE, processing
instructions, comments, CDATA, entity references, `xml:space="preserve"`
subtrees and mixed content (whitespace there is significant). Text-only
elements stay on one line (`<name>value</name>`), self-closing tags stay
self-closing, nothing beyond whitespace is rewritten. Range formatting
narrows to the element subtrees overlapping the selection. Registered at the
built-in tier; `[format.xml] builtin = false` disables it.

## Built-in .http formatter (#1602)

The HTTP plugin ships a reformatter for `.http`/`.rest` request files
(`plugins/languages/http/format.go`): it folds a request line's query
parameters onto the indented `?`/`&` continuation lines the parser already
accepts (#1269) — first `? key = value`, then `& key = value` per further
parameter, indented one level in the buffer's indent style — and merges
already-folded spellings into the same canonical one-parameter-per-line
shape. Keys and values are re-emitted **byte-identical**: the formatter never
percent-encodes or decodes anything (#1601 is why that matters). The rest of
the block normalizes conservatively: single spaces on the request line,
`Name: value` header spacing, exactly one blank line before the body; bodies,
comments and `###` separators pass through verbatim. Anything the parser
would reject (malformed request lines, invalid headers) and shapes whose fold
would not round-trip byte-identically (a bare trailing `?`, empty `&&`
fragments, comments interleaved with query continuations) stay untouched, and
a reparse guard aborts the whole reformat if the output would parse to
different requests than the input. Whole-file only (no range support);
registered at the built-in tier, `[format.http] builtin = false` disables it.

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
