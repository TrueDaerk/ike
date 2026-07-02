---
type: concept
title: Syntax Highlighting
description: The Tree-sitter lexical highlighting layer — per-language grammars parsed off the event loop into capture spans, cached by document version, resolved to theme colours, and applied per cell in the editor's renderLine.
resource: internal/highlight
tags: [architecture, highlighting, tree-sitter, syntax, editor, theme, cgo]
timestamp: 2026-06-28T00:00:00Z
---

# Syntax Highlighting

Roadmap 0100. The fast lexical base layer that colours code in the editor, built
on [Tree-sitter](https://tree-sitter.github.io/). It is independent of the
[LSP client](./lsp.md) — it works with no language server running — and ships
grammars for **Go**, **PHP** and **Python**. An optional LSP semantic-token
overlay is deferred to a later increment.

## How it works

`internal/highlight` parses a document into `Span{Line, StartCol, EndCol,
Capture}` runs, where `Capture` is a Tree-sitter capture name (`keyword`,
`string`, `function`, …). A `Theme` resolves capture names to lipgloss colours,
falling back from a dotted name (`function.builtin`) to its head (`function`), and
layered over built-in defaults by the `[theme.captures.*]` config keys.

```
internal/highlight/
  span.go        Span model + a per-line Index for O(spans-on-line) cell lookup.
  theme.go       capture-name -> lipgloss style, from [theme] over built-in defaults.
  highlight.go   language detection by extension; Highlight(path, lines) dispatch.
  parse_cgo.go   //go:build cgo — the real Tree-sitter parser + embedded queries.
  parse_stub.go  //go:build !cgo — a no-op so CGO_ENABLED=0 still builds.
  queries/*.scm  vendored highlight queries for go / python / php.
```

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
