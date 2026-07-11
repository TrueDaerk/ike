---
type: concept
title: Syntax Highlighting
description: The Tree-sitter lexical highlighting layer — per-language grammars parsed off the event loop into capture spans, cached by document version, resolved to theme colours, and applied per cell in the editor's renderLine.
resource: internal/highlight
tags: [architecture, highlighting, tree-sitter, syntax, editor, theme, cgo]
timestamp: 2026-07-11T00:00:00Z
---

# Syntax Highlighting

Roadmap 0100 (engine); Roadmap 0105 made the language set extensible. The fast
lexical base layer that colours code in the editor, built on
[Tree-sitter](https://tree-sitter.github.io/). It is independent of the
[LSP client](./lsp.md) — it works with no language server running. `internal/highlight`
is now a pure **engine**: it owns no language list. Grammars come from the
[language registry](./languages.md); the built-in **Go/PHP/Python/SQL** grammars live
in `plugins/languages/*`. An optional LSP semantic-token overlay is deferred.

## How it works

`internal/highlight` parses a document into `Span{Line, StartCol, EndCol,
Capture}` runs, where `Capture` is a Tree-sitter capture name (`keyword`,
`string`, `function`, …). A `Theme` resolves capture names to lipgloss colours,
falling back from a dotted name (`function.builtin`) to its head (`function`), and
layered over built-in defaults by the `[theme.captures.*]` config keys.

```
internal/highlight/
  span.go         Span model + a per-line Index for O(spans-on-line) cell lookup.
  theme.go        capture-name -> lipgloss style, from [theme] over built-in defaults.
  highlight.go    Lang/Supported/Highlight — delegate to the lang registry (ByPath).
  grammar_cgo.go  //go:build cgo — NewGrammar(tsLang, query) builds the opaque token.
  grammar_stub.go //go:build !cgo — NewGrammar returns nil (highlighting off).
  parse_cgo.go    //go:build cgo — the real Tree-sitter parser over a grammar.
  parse_stub.go   //go:build !cgo — a no-op so CGO_ENABLED=0 still builds.
  fragment*.go    embedded-fragment detection via injection queries (shared with LSP).
  injection.go    layers fragment spans (parsed with the fragment's grammar) over host spans.
```

A language's grammar is an opaque `lang.Grammar` built by `highlight.NewGrammar`
in the language plugin's cgo file; the query (`highlights.scm`) is embedded there
too. `Highlight(path, lines)` looks the language up via `lang.ByPath`, type-asserts
its grammar, and parses — the engine knows no specific language.

## Language injections (issue #299)

`Highlight` also colours **embedded-language fragments** — an SQL string inside
Python renders as SQL. The host grammar's `injections.scm` (embedded in the
language plugin, capture convention `fragment.<lang>[.guess]`, shared with the
[LSP virtual-document seam](./lsp.md)) marks fragment ranges; each fragment is
parsed with its own language's registered grammar and the resulting spans are
shifted into host coordinates (`injection.go`). Injected spans are prepended to
the host span set, so inside a fragment they win over the host's enclosing
`string` capture in `Index.CaptureAt`, while gaps between injected tokens fall
back to the host colour. Fragments re-highlight with every reparse, exactly
like top-level edits (the whole buffer reparses per change, off the event
loop). One level deep: fragments inside fragments are not re-injected.
Fragment languages without a registered grammar degrade to plain host
highlighting.

## CGo isolation

Tree-sitter is a C library, so the parser needs CGo. It is isolated behind a
`cgo` build tag with a no-op stub for `!cgo` builds: `CGO_ENABLED=0 go build`
still compiles (highlighting simply off, code renders plain), keeping pure
cross-compilation possible. `internal/lsp` stays CGo-free so the LSP client
cross-compiles regardless.

## Editor integration

Parsing runs **off the event loop**. The editor owns a monotonic `docVersion`
(bumped on every buffer change). After a change — or on file open — the editor
returns a `tea.Cmd` that runs the CGo parse on a goroutine and yields a
`highlight.SpansMsg{Path, Version, Spans}`. The app routes it back to the editor
leaf owning the path; the editor caches the spans **only if the version still
matches** (a newer edit drops stale results). `renderLine` then looks up the
capture per rune cell and wraps it in the themed style — in the default branch,
so the cursor and the visual selection still win on overlap, and a diagnostic
underline composes on top.

## Testing

The span model, per-line index and theme fallback are pure-Go unit tests. The
real Tree-sitter path (behind the `cgo` tag) is exercised by parsing Go/PHP/Python
fixtures and asserting capture output, and the editor's render integration is
tested by feeding `SpansMsg` into `editor.Model.Update` and checking the rendered
ANSI.
