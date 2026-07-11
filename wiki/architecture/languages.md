---
type: concept
title: Language Registry
description: The neutral lang registry that bundles a language's file extensions, Tree-sitter grammar, LSP server spec, and toolchain detector — populated by per-language plugins so adding a language is a new package, not an engine edit.
resource: internal/lang
tags: [architecture, languages, registry, highlighting, lsp, plugins, toolchain]
timestamp: 2026-07-11T09:00:00Z
---

# Language Registry

Roadmap 0105. IKE's language set is **extensible**: a language is a plugin that
registers one `lang.Language` describing everything language-specific in one place.
The [highlight engine](./highlighting.md) and the [LSP subsystem](./lsp.md) read
from this registry instead of hardcoding a language list. Adding a language =
adding a `plugins/languages/<lang>/` package + a blank import in `cmd/ike/main.go`.

## The registry (`internal/lang`)

`lang` is a **leaf package** — pure Go, no CGo, no Tree-sitter import — so both the
highlight engine and `internal/lsp` depend on it without a cycle.

```go
type Language struct {
    ID         string       // "python"
    Extensions []string     // ["py", "pyi"]
    Filenames  []string     // optional exact base names ("Dockerfile")
    Grammar    Grammar      // opaque highlight token, or nil
    Server     *ServerSpec  // LSP launch config, or nil
    Toolchain  Toolchain    // project interpreter detector, or nil

    LineComment  string     // "//", "#" — comment-toggle marker (0120)
    BlockComment [2]string  // {"/*", "*/"}; empty = no block syntax
    IndentAfter  []string   // block-opening line suffixes (0260): ":" / "{" …
}
func Register(l Language)
func ByID(id string) (Language, bool)
func ByPath(path string) (Language, bool)   // exact base name, then extension
func Comments(path string) (line string, block [2]string, ok bool)
func IndentAfter(path string) ([]string, bool)
func All() []Language
```

`Comments` resolves the comment syntax for a buffer path (0120 comment
toggling); `ok` is false when no language matches or the language declares no
comment syntax at all (plain text) — the editor treats that as "toggling
unavailable". Go and PHP declare `//` + `/* */`, Python declares `#` only.

`IndentAfter` resolves the smart-indent openers for a buffer path (Roadmap
0260): trimmed-line **suffixes** after which the next line indents one level
deeper. Python declares `":"` plus the open brackets `( [ {`; Go and PHP
declare `{ ( [`. `ok` is false when no language matches or none are declared —
the editor then falls back to plain copy-indent.

`Grammar` is `any`: the concrete compiled Tree-sitter grammar is built by
`highlight.NewGrammar` (behind the cgo tag) and only stored/handed back here, so
`lang` stays CGo-free. Any of Grammar / Server / Toolchain may be nil — a language
can have highlighting but no server, or vice versa.

## A language plugin

`plugins/languages/<lang>/` registers from `init()` (like `registry.Register` /
`config.Register`). The grammar is build-tagged so `CGO_ENABLED=0` still builds:

```
<lang>.go        init() -> lang.Register(Language{ID, Extensions, Grammar: grammar(), Server, Toolchain})
                 //go:embed queries/<lang>.scm  (the highlights query)
grammar_cgo.go   //go:build cgo  -> grammar() = highlight.NewGrammar(ts.NewLanguage(<binding>), query)
grammar_stub.go  //go:build !cgo -> grammar() = nil
queries/<lang>.scm
toolchain.go     optional Toolchain detector
```

Ships with `go`, `python`, `php`, and `sql` (grammar from
DerekStride/tree-sitter-sql; also highlights SQL fragments injected into other
languages, see [highlighting](./highlighting.md)). The grammar/query for the
first three moved here out of the highlight engine.

## Server resolution (baseline < config)

A language plugin's `Language.Server` is the **baseline** ServerSpec (command,
args, root markers). The user's `[lsp.servers.<id>]` config is an **overlay**:
`plugins/lsp` `resolveSpec` reads `lang.ByID(id).Server` and merges the config
overlay per field (config wins where set; baseline fills the rest). `applyDefaults`
no longer hardcodes servers — it only enables the subsystem.

## Toolchain detection (version awareness)

For version-gated intelligence (e.g. a Python 3.13 feature flagged under a 3.12
project), IKE **detects the project toolchain and hands its path to the language
server** — it never reimplements the server's version logic. A `Toolchain`:

```go
type Toolchain interface { Detect(root string) (settings map[string]any, ok bool) }
```

runs at server spawn in `manager.ensureServer` (and on restart); the result is
deep-merged into the server's settings (an explicit user setting wins). The manager
then answers the server's `workspace/configuration` request from those settings, so
e.g. the resolved `python.defaultInterpreterPath` reaches pyright.

The **Python** detector (`plugins/languages/python/toolchain.go`) resolves the
interpreter in priority order: active `$VIRTUAL_ENV` → project `.venv`/`venv` →
`.python-version` (pyenv) → `python3` on `PATH`. Go relies on gopls reading
`go.mod`; **PHP** ships a PATH/install-location detector (no server injection).

## Explicit interpreters (Roadmap 0160, #94)

`[lang.<id>] interpreter = "<path>"` in the **project** config pins the
interpreter explicitly. Resolution is one function — the single source of
truth the settings page, the LSP overlay and 0170's terminal shims all share:

```go
lang.Interpreter(langID, root, explicit) (path, source) // source: config | detected
```

Two optional `Toolchain` extensions power it: `InterpreterDetector` exposes
the detected binary (python: the Detect resolution; php: PATH + common install
locations), and `ExplicitSettings` maps an explicit path into the same
settings shape `Detect` produces. The LSP plugin's `resolveSpec` injects the
explicit value into `ServerSpec.Settings`, where it wins over detection (the
manager's merge keeps spec settings on top); the settings **Toolchain page**
writes the key and triggers `lsp.restart` so servers respawn against it. See
[Settings UI](./settings-ui.md).

## Why compile-in

Tree-sitter grammars are CGo (C code compiled into the binary), so grammars are
linked at build time, not loaded at runtime. Delivery is therefore compile-in —
consistent with every other IKE plugin. Runtime/WASM grammar loading is out of
scope.

## Python environment management (Roadmap 0180, #132)

The toolchain settings page manages Python environments directly: `n` creates
a project venv (`uv venv` when uv is on PATH, `python -m venv .venv`
otherwise), `u` picks a version from `uv python list`'s download-available
entries and installs it in the background (`uv python install`, path resolved
via `uv python find`). Both run asynchronously as `tea.Cmd`s; the result
(`settings.EnvMsg`) is routed by the root model, which registers the new
interpreter as the absolute `[lang.python] interpreter` through the write-back
layer — `lang.Interpreter` stays the single source of truth — and restarts the
language servers against it. IKE shells out to `uv`/`python`; it never bundles
toolchains. The integrated terminal (Roadmap 0170, #98) injects the same
explicit choice via per-project shims — see
[Integrated Terminal](./terminal.md).
