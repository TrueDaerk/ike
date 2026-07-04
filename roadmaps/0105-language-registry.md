# Roadmap 0105 — Extensible Language System (Registry + Toolchain)

Follow-up to [0100 (LSP + highlighting)](0100-lsp.md). That increment hardcoded the
language set in three places (`highlight.langByExt`, `parse_cgo.initGrammars`,
`plugins/lsp applyDefaults`). This roadmap makes languages **plugin-registered** and
adds **toolchain/version awareness** driven by IKE-side interpreter detection.

## Goals

- One registration point per language bundling file extensions + Tree-sitter
  grammar/query (highlight) + LSP server spec + toolchain detector.
- Adding a language = a new `plugins/languages/<lang>/` package + a blank import;
  no engine edits.
- Version-gated intelligence (e.g. Python 3.13 vs 3.12) via **LSP server config**
  fed by IKE detecting the project interpreter and passing its path to the server.
  Tree-sitter highlighting stays version-agnostic.

## Decisions

- **Delivery: compile-in** (Go blank-import + `init()` `Register`, matching every
  other IKE plugin). Tree-sitter is CGo, so runtime/WASM grammar loading is out of
  scope.
- **Unit: full language bundle** (`lang.Language`).
- **Versions: detect + delegate.** IKE resolves the interpreter path; the language
  server owns all version semantics.

## Milestones

- [x] `internal/lang` registry — pure-Go leaf package: `Language`, `ServerSpec`
      (moved here), `Toolchain`, `Register`/`ByPath`/`ByExt`/`ByID`/`All`,
      `MergeSettings`.
- [x] `internal/highlight` becomes an engine: `NewGrammar(tsLang, query)` (cgo) +
      nil stub (!cgo); `Highlight`/`Lang`/`Supported` delegate to `lang.ByPath`;
      hardcoded grammar map + embedded queries removed.
- [x] Per-language plugins `plugins/languages/{go,php,python}` — grammar (cgo/stub),
      embedded `highlights.scm`, server spec, `lang.Register` in `init()`.
- [x] Python toolchain detector — `$VIRTUAL_ENV` → `.venv`/`venv` →
      `.python-version` (pyenv) → `python3` on PATH → `python.defaultInterpreterPath`.
- [x] LSP rewired — `ServerSpec` aliased from `lang`; `resolveSpec` merges plugin
      baseline `<` `[lsp.servers.<id>]` overlay; `manager.ensureServer` injects
      `Toolchain.Detect(root)` into settings; `workspace/configuration` answered from
      settings; `bridge` uses `lang.ByPath`.
- [x] Wiring + tests — blank imports in `cmd/ike/main.go`; unit tests for the
      registry, spec merge, toolchain detection, and toolchain-reaches-server;
      CGO on/off builds; real gopls smoke test still green.
- [x] Docs — `wiki/architecture/languages.md`; refreshed highlighting/lsp docs;
      `log.md` entry; this roadmap + PROGRESS tick.

## Deferred

Runtime/WASM grammar loading; per-version Tree-sitter grammars; an in-house
version→feature rule engine (the LSP server owns version semantics). PHP/Go ship
without a toolchain detector (gopls reads `go.mod`; PHP baseline is enough for now).
