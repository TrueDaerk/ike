---
type: concept
title: Language Registry
description: The neutral lang registry that bundles a language's file extensions, Tree-sitter grammar, LSP server spec, and toolchain detector — populated by per-language plugins so adding a language is a new package, not an engine edit.
resource: internal/lang
tags: [architecture, languages, registry, highlighting, lsp, plugins, toolchain]
timestamp: 2026-07-24T00:00:00Z
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
    ServerLanguage string   // delegate documents to this language's server (#1063); "" = own Server
    Toolchain  Toolchain    // project interpreter detector, or nil

    LineComment  string     // "//", "#" — comment-toggle marker (0120)
    BlockComment [2]string  // {"/*", "*/"}; empty = no block syntax
    IndentAfter  []string   // block-opening line suffixes (0260): ":" / "{" …
    ScopeNodes   []string   // sticky-scroll scope node kinds (#168); empty = inert
    Template     string     // initial content for new files (#170); "" = start empty
}
func Register(l Language)
func ByID(id string) (Language, bool)
func ByPath(path string) (Language, bool)   // sniffed path association, exact base name, then extension
func ForShebang(firstLine string) (Language, bool) // interpreter on the #! line (#893)
func AssociatePath(path, id string)         // record a content-sniffed language for one path
func Comments(path string) (line string, block [2]string, ok bool)
func IndentAfter(path string) ([]string, bool)
func TemplateFor(path string) string        // rendered new-file content (#170)
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

`ScopeNodes` names the Tree-sitter node kinds that define **sticky-scroll
scopes** (#168) — declarations whose header line pins at the top of the editor
while their body scrolls (see [highlighting](./highlighting.md)). Go declares
functions, methods, func literals and type declarations; Python declares
function and class definitions; PHP declares functions, methods, anonymous
functions, class/interface/trait/enum declarations and namespaces. An empty
list leaves sticky scroll inert for the language.

### Context sniffers (#897)

The generalisation of the shebang seam: a language whose files the static
indexes cannot identify registers a `Sniffer` (`lang.RegisterSniffer`) that
inspects the path and project context on open. The editor runs `lang.Sniff`
**before** the static verdict counts — a sniffer may override the extension —
and records hits via `AssociatePath`. **Ansible** is the first user: `.yml`
under `roles/<r>/(tasks|handlers|defaults|vars|meta)/`, `playbooks/`,
`group_vars/`, `host_vars/` or `inventory/`, or inside a project with
`ansible.cfg` / `galaxy.yml` / `requirements.yml`+`roles/` up the tree,
resolves to the `ansible` id (sharing yaml's grammar) and gets
`@ansible/ansible-language-server` instead of plain yaml-language-server;
everything else stays `yaml`.

**Inventory navigation & completion (#922).** The server resolves modules but
not inventory hosts/groups, so IKE indexes them itself
(`plugins/languages/ansible/inventory.go`): INI inventories (`inventory/`,
root `hosts`/`inventory` — `[group]` headers, host lines, `:children`
entries), YAML inventories (keys under `hosts:` / `children:`), and
`group_vars/` / `host_vars/` base names. Two seams consume the index:

- `lang`-side **local definition** (`ilsp.RegisterLocalDefinition`): the LSP
  bridge consults registered providers before the server, so goto-definition
  on a `hosts:` / `delegate_to:` value or a `groups['...']` reference jumps to
  the defining inventory line — even with no server installed. Providers claim
  only on a positive hit; everything else still reaches the server.
- a **completion source** (`complete.RegisterSource`, the plugin seam): in a
  `hosts:`/`delegate_to:` value position it offers the project's group/host
  names (groups sorted first, prefix-filtered, priority between symbols and
  the server).

The index is best-effort text scanning with a 2s cache — no ansible
invocation; dynamically computed names stay unknown.

### Shebang fallback (#893)

Files with no extension and no known base name (`deploy`, `run-tests`) resolve
via the `#!` line: a language declares `Interpreters` (base names, e.g. python
→ `python`, `python3`), and the **editor** — only when the static lookups both
miss — reads the first buffer line on open and calls `lang.ForShebang`. The
parser handles the plain form (`#!/bin/bash`), the env form
(`#!/usr/bin/env python3`) and env `-S` (`#!/usr/bin/env -S deno run`: first
non-flag, non-assignment word after env); trailing version digits are stripped
on a miss so `python3.12` matches `python`. A hit is recorded with
`lang.AssociatePath(path, id)` — a per-path override consulted first by
`ByPath` — so highlighting, the LSP bridge and the statusline (all path-keyed)
follow without any extra plumbing.

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

Ships with `go`, `python`, `php`, `sql` (grammar from
DerekStride/tree-sitter-sql; also highlights SQL fragments injected into other
languages, see [highlighting](./highlighting.md)), and `json`/`ndjson` (#878,
official tree-sitter-json grammar — its document rule is `repeat(_value)`, so
one grammar covers both a `.json` document and an ndjson/jsonl stream; ndjson
has no server on purpose, the JSON server would flag every stream line), and
`toml` (#895, tree-sitter-grammars/tree-sitter-toml; `[table]` headers are
sticky-scroll scopes), and `dockerfile` (#896, camdencheek/tree-sitter-dockerfile
— **vendored C source** under the plugin's `grammar/`: upstream's Go binding
carries a nested go.mod with the repo-root module path, so it is not
importable; matches `Dockerfile`/`Containerfile` by exact base name plus the
`.dockerfile` extension), and `yaml` (#879, tree-sitter-grammars/tree-sitter-yaml;
multi-line mapping pairs are sticky-scroll scopes, and `IndentAfter` is limited
to `":"` + block-scalar introducers so sibling keys never get auto-indented
wrongly — YAML is indentation-sensitive), and `shell` (#894, official
tree-sitter-bash — covers sh/zsh for highlighting; matches `.sh`/`.bash`/`.zsh`,
the rc-file base names `.bashrc` `.zshrc` `.bash_profile` `.profile`
`.zprofile`, and extensionless scripts via interpreters `sh` `bash` `zsh`
`dash`), and `markdown` (#880, two vendored grammars — block + inline — wired
through the injection seam; fenced code blocks render in the fence's language,
front matter as YAML/TOML; see [highlighting](./highlighting.md)), and the
web languages (#925): `typescript` highlights via the **TSX grammar** — the
permissive superset that parses plain JS, JSX and TS annotations alike, so
the single language id (and with it the single vtsls instance per project)
stays intact; the one casualty is legacy `<T>x` type assertions. `html` uses
the official grammar with `<script>`/`<style>` injections into
typescript/css; `css` uses the official grammar (scss/less parse best-effort
— error-tolerant spans still color the shared subset).
The grammar/query for the first three moved here out of the highlight engine.

### Server delegation (#1063)

A language may set `ServerLanguage` to run its documents on **another
language's server**: the LSP manager resolves the spec and keys the server
instance by `Language.ServerLang()` (the delegate), while the `didOpen`
languageId stays the delegating language's own ID. First user: the go plugin
registers `go.mod`, `go.work` and `go.sum` as filename-matched languages
delegating to `go` — a `go.mod` buffer attaches to the very gopls instance
(same root, same process) that serves the module's `.go` files, with the wire
languageIds `go.mod`/`go.work`/`go.sum` gopls documents, so hover on require
lines and go.mod diagnostics work. They carry no grammar (no gomod
Tree-sitter grammar is vendored — plain text), and they register with plain
`lang.Register`, not `register.Language`: the `lang-go` plugin toggle governs
the shared server, and a dotted plugin id would splinter the config key.
Gating helpers: `Language.HasServer()` is true for a language with its own
`Server` **or** a delegate that has one — the LSP bridge gates on it instead
of `Server != nil`.

## Server resolution (baseline < config)

A language plugin's `Language.Server` is the **baseline** ServerSpec (command,
args, root markers). The user's `[lsp.servers.<id>]` config is an **overlay**:
`plugins/lsp` `resolveSpec` reads `lang.ByID(id).Server` and merges the config
overlay per field (config wins where set; baseline fills the rest). `applyDefaults`
no longer hardcodes servers — it only enables the subsystem.

### Companion tools (#1067)

Some servers delegate a capability to another binary and stay silent when it is
missing (bash-language-server runs fine without shellcheck, it just never
publishes a diagnostic). A spec declares those as optional **companions**:

```go
Companions: []lang.Companion{
    {Binary: "shellcheck", Purpose: "shell diagnostics", Install: "brew install shellcheck"},
}
```

When a server first becomes ready, the LSP manager probes PATH for each declared
companion and raises a one-time warn status per missing one — e.g. "shellcheck
not found — shell diagnostics disabled (brew install shellcheck)" — deduplicated
per language for the session, never per file. Declared today: shell → shellcheck;
ansible → ansible (module data) and ansible-lint (lint diagnostics).

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
`.python-version` (pyenv) → `python3` on `PATH`. The **PHP** detector (#1079)
resolves the project's PHP *version* for intelephense
(`intelephense.environment.phpVersion`): the `composer.json` `require.php`
constraint's minimum bound wins (a PHP 7.3 project flags 8.x syntax as errors),
the detected interpreter's `php -v` is the fallback; without either the server
keeps its default. The **TypeScript** detector (#1079,
`plugins/languages/web/toolchain.go`) points vtsls at a vendored workspace
TypeScript via `typescript.tsdk` when `node_modules/typescript/lib` exists —
VS Code's "use workspace version". **Go** (gopls reads the `go.mod` directive
itself) ships a PATH/install-location detector only (no server injection):
PATH first, then the common install locations — for Go `/opt/homebrew/bin`, `/usr/local/bin`,
`/usr/local/go/bin`, `/usr/bin` (#538), since a GUI-launched process often
misses homebrew's bin on PATH. The toolchain settings page's generic
interpreter picker probes the same well-known directories after PATH
(`defaultCandidates` in `internal/settings/toolchain_discover.go`), and every
picker additionally globs the **versioned install directories** (#675):
Homebrew `opt/<formula>[@*]/bin/<bin>` under `/opt/homebrew` and `/usr/local`
(unversioned formula first, then newest version first), pyenv
`~/.pyenv/versions/*/bin/python` and Go `~/sdk/go*/bin/go` — deduplicated by
symlink-resolved path, so switching to e.g. `php@7.4` no longer needs a typed
custom path. Opening the picker pre-selects the currently effective
interpreter and probes every candidate's version eagerly (async `VersionMsg`s),
so the list shows versions without pressing `p`.

### Version-manager shim resolution (#650)

pyenv, mise and asdf put dispatcher scripts on PATH (`~/.pyenv/shims/python`,
`…/mise/shims/php`, `~/.asdf/shims/go`), so a plain `LookPath` reports the
shim, hiding which interpreter version is actually active. Like JetBrains,
IKE resolves shims to the real executable: `lang.ResolveShim(root, path)`
(`internal/lang/shims.go`) detects the owning manager by path component and
asks it — `pyenv|mise|asdf which <bin>` — running the command with the project
root as working directory so per-project pins (`.python-version`,
`.tool-versions`, `mise.toml`) resolve to that project's version. Resolution
is best-effort: a non-shim path, a missing manager binary, a failing command
or output that is not an existing file all return the input unchanged.

The three plugin `Interpreter()` detectors resolve their PATH hits through it
(venv and pyenv-versions paths are already real and skip it), which covers
every consumer of `lang.Interpreter` — settings page, statusline, terminal
PATH shims, debug launch and LSP injection. Discovery
(`internal/settings/toolchain_discover.go`) resolves shim candidates before
listing too (including the hardcoded pyenv shim entry) and dedupes identical
resolutions, so the picker shows versioned paths.

## File templates (#170)

A language may register a `Template`: the initial content seeded into newly
created files — the explorer's `explorer.newFile`, `:e` on a nonexistent path,
and a CLI open of a missing file all go through it. `lang.TemplateFor(path)`
(`internal/lang/template.go`) matches the path's language, applies any user
override, and substitutes the variables:

| variable | value |
| --- | --- |
| `${FILENAME}` | base name with extension (`main.go`) |
| `${NAME}` | base name without extension (`main`) |
| `${DIR}` | containing directory's name |
| `${PACKAGE}` | `${DIR}` sanitised to an identifier (`my-pkg` → `mypkg`, fallback `main`) |
| `${DATE}` / `${YEAR}` | today as `YYYY-MM-DD` / `YYYY` |

Built-ins: Go seeds `package ${PACKAGE}`, PHP seeds `<?php`. Users override per
language with `[lang.<id>] template = "..."` in the config (TOML multiline
strings work); an explicitly **empty** override disables the template. Explorer
creates write the rendered template to disk; editor-side new buffers
(`NewFile`) are seeded but stay unmodified, so quitting without `:w` loses
nothing.

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

The toolchain settings page manages Python environments directly: `n` opens a
guided create wizard (#569, PyCharm-style): step 1 picks the tool — uv or the
stdlib `venv` module, filtered by what's on PATH and skipped when only one is
available; step 2 picks the Python — for uv a version from `uv python list`
(default first; uv downloads a missing version on demand, so the env lands as
`uv venv --python <version>`), for venv a base interpreter from the discovery
candidates (`<base> -m venv`); step 3 asks for the target directory (#547) in
a path-completed input pre-filled with `.venv`. Relative targets resolve
against the project root, absolute and `~` targets are honored, so a shared
env directory outside the project works.
On the uv path the project is scaffolded too (#548): a missing
`pyproject.toml` is generated via `uv init --bare` (manifest only, no sample
sources) and a missing `uv.lock` via `uv lock` — best effort, existing files
are never touched, and the result toast names what was created.
`u` picks a version from `uv python list`'s download-available
entries and installs it in the background (`uv python install`, path resolved
via `uv python find`). Both run asynchronously as `tea.Cmd`s; the result
(`settings.EnvMsg`) is routed by the root model, which registers the new
interpreter as the absolute `[lang.python] interpreter` through the write-back
layer — `lang.Interpreter` stays the single source of truth — and restarts the
language servers against it. IKE shells out to `uv`/`python`; it never bundles
toolchains. The integrated terminal (Roadmap 0170, #98) injects the same
explicit choice via per-project shims — see
[Integrated Terminal](./terminal.md).

The page also answers *what is this environment* (#569): each python row and
picker candidate carries a **provenance** badge — `uv venv` / `venv` (from
`pyvenv.cfg`, where uv stamps a `uv = <version>` key), `uv managed`, `pyenv`
or `system` (path heuristics) — and `i` lists the effective interpreter's
installed packages with versions (async; `uv pip list --python <interp>` when
uv is present — it works even in envs without pip — else
`<interp> -m pip list --format=freeze`), scrollable inline with `j`/`k`.
Since #571 the view also installs/uninstalls/upgrades packages and marks
available upgrades (`↑ <latest>`), preferring `uv add`/`uv remove` in uv
projects so pyproject.toml and uv.lock stay in sync — see
[Settings UI](./settings-ui.md) for the key flow.

## Default language servers & why (#855)

| Language | Default server | Rationale / alternative |
|---|---|---|
| Go | gopls | Reference server, no contest. Also serves `go.mod`/`go.work`/`go.sum` (#1063): filename-matched languages delegating to the same instance, languageIds `go.mod`/`go.work`/`go.sum`. |
| Python | pyright (via server spec in `plugins/languages/python`) | Fast, precise; venv-aware via `workspace/configuration`. |
| PHP | Intelephense | Free tier beats phpactor on completion quality and speed; cross-file rename & advanced refactors are premium (paid). Prefer those? Override to phpactor via `[lsp.servers.php]`. |
| TS/JS | vtsls | Wraps the same tsserver VS Code uses but speaks LSP far more faithfully than typescript-language-server (streaming/isIncomplete completions, lower memory churn). Override via `[lsp.servers.typescript]`. |
| HTML | vscode-html-language-server (`vscode-langservers-extracted`) | The extracted VS Code server; unmatched tag/attribute data. |
| CSS/SCSS/LESS | vscode-css-language-server (`vscode-langservers-extracted`) | Same package, property/value data included. |
| SQL | sqls (`go install github.com/sqls-server/sqls@latest`) | Maintained Go binary, LSP over stdio by default, no Node dependency; keyword/function completion and formatting without a DB, richer completion with a `.sqls/config.yml` connection. Replaced sql-language-server, which crashes on startup under Node ≥ 26 (#1066). |
| JSON | vscode-json-language-server (`vscode-langservers-extracted`) | Same npm package as HTML/CSS — no new install step; JSON-Schema-store + `$schema` completion for free. ndjson/jsonl: no server (multi-document streams are an error to it). |
| TOML | taplo (`taplo lsp stdio`, via `@taplo/cli`) | Schema-store completion (Cargo.toml, pyproject.toml, … by filename), formatting, diagnostics. IKE's own config is TOML. **Caveat:** the Homebrew `taplo` formula is built *without* the LSP feature and dies at startup; IKE recognizes that failure and points at `npm install -g @taplo/cli` (or `cargo install taplo-cli --features lsp`), #1065. |
| Dockerfile | docker-langserver (`dockerfile-language-server-nodejs`) | Completes instructions, flags, image tags; diagnostics for common mistakes. |
| YAML | yaml-language-server (Red Hat) | Schema-store completion auto-detected by filename (Kubernetes, GitHub Actions, docker-compose, …), hover, diagnostics. |
| Shell | bash-language-server (`bash-language-server start`) | Completes commands from PATH, variables, functions; shellcheck diagnostics automatic when shellcheck is on PATH (declared companion — a one-time hint fires when it is missing, #1067). |
| Markdown | marksman (`marksman server`, `brew install marksman`) | Single static binary; completes link targets, heading anchors, wiki-links, reference labels. Prose keeps the word-index source. |
| Ansible | ansible-language-server (`@ansible/ansible-language-server`) | Module FQCN/option/keyword completion, hover docs, ansible-lint diagnostics when installed. Needs `ansible` on PATH for full module data (both declared companions with one-time missing hints, #1067). Detected by context sniffer, not extension. |

Every default is a plain `ServerSpec` and can be replaced per project or user
via the `[lsp.servers.<id>]` config table (command/args/settings) — editable
in the Settings → LSP page, which lists each registered language's effective
command line, its config layer, and offers install/restart.
