---
type: concept
title: EditorConfig Support
description: Per-buffer .editorconfig resolution — spec-conformant glob matching, upward search with root=true, watcher-invalidated cache, override layer between IKE config and explicit user actions.
resource: internal/editorconfig
tags: [architecture, editor, config, editorconfig]
timestamp: 2026-07-12T00:00:00Z
---

# EditorConfig Support

IKE honours [.editorconfig](https://editorconfig.org) files (#63): open a file
in a repo that carries one and indent style/width, trim-on-save,
final-newline, line endings and charset follow that file's matching sections —
no IKE configuration needed. The support is invisible except for the status
line's `indent` segment ("Spaces: 2" / "Tab: 4"), which shows the effective
value.

## Precedence

```
built-in defaults < IKE config ([editor]) < .editorconfig < explicit user action
```

Resolved `.editorconfig` settings override the `[editor]` values for their
buffer only; buffers no section matches keep following IKE's config live.
Explicit per-buffer actions still win — `file.setLineEndings` /
`file.setEncoding` change the stored flavor directly and are not re-clobbered
(resolution only touches EOL/charset at load time). `editor.editorconfig =
false` (settings UI: "EditorConfig") disables the whole layer.

## The package: `internal/editorconfig`

- **Parsing** (`parse`): the INI-flavored syntax — `root = true` preamble,
  `[glob]` sections, `key = value` pairs, `;`/`#` full-line comments. Lenient:
  malformed lines are skipped, unknown keys carried but unused.
- **Glob matching** (`glob.go`): section patterns translate to anchored
  regexps — `*` (non-separator), `**`, `?`, `[seq]`/`[!seq]`,
  `{a,b}` (nestable; comma-less braces are literal), `{n1..n2}` numeric
  ranges, `\` escapes. Patterns containing `/` anchor to the `.editorconfig`'s
  directory; bare names match at any depth. Compiled patterns are memoized.
- **Resolution** (`Resolver.Resolve`): walks from the file's directory upward,
  stopping at the first `root = true` file (or the filesystem root); files
  closer to the target and later sections within a file override earlier ones
  key by key; the value `unset` removes a key.
- **Caching**: parsed files are cached per directory on the shared default
  resolver (absent files cached as absent, so unchanged directories are never
  re-stat'd). `Invalidate(path)` drops one directory's entry.

## Editor integration (`internal/editor/editorconfig.go`)

The buffer's resolved settings live on the model (`ec`), re-resolved whenever
the buffer's identity changes (`Load`, `NewFile`, `SetPath`, `:w other`) and
when the file watcher (#53) reports a changed `.editorconfig` (the editor
invalidates the shared cache and re-resolves). Each `applyConfig` pass reads
IKE's config first, then overlays the buffer's settings.

Consumed keys map onto existing behaviour:

| Key | Effect |
|---|---|
| `indent_style` | `useSpaces` (space/tab) |
| `indent_size` / `tab_width` | `tabWidth` (tab_width wins; `indent_size = tab` defers to tab_width) |
| `trim_trailing_whitespace` | trim on save |
| `insert_final_newline` | final newline on save |
| `end_of_line` | stored EOL flavor (#66): applied on save, so an lf/crlf mismatch converts on the next write; `cr` is unsupported and ignored |
| `charset` | decode fallback on load (a BOM or valid UTF-8 still wins — readable bytes are never re-interpreted) and the initial encoding of new files; outranks `files.encoding` |
