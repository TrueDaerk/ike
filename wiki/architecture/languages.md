---
type: concept
title: Language Registry
description: The neutral lang registry that bundles a language's file extensions, Tree-sitter grammar, LSP server spec, and toolchain detector — populated by per-language plugins so adding a language is a new package, not an engine edit.
resource: internal/lang
tags: [architecture, languages, registry, highlighting, lsp, plugins, toolchain]
timestamp: 2026-07-02T00:00:00Z
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
}
func Register(l Language)
func ByID(id string) (Language, bool)
func ByPath(path string) (Language, bool)   // exact base name, then extension
func All() []Language
```

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

Ships with `go`, `python`, `php`. The grammar/query for each moved here out of the
highlight engine.

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
`go.mod`; PHP ships without a detector for now.

## Why compile-in

Tree-sitter grammars are CGo (C code compiled into the binary), so grammars are
linked at build time, not loaded at runtime. Delivery is therefore compile-in —
consistent with every other IKE plugin. Runtime/WASM grammar loading is out of
scope.
