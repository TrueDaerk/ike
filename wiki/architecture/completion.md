---
type: concept
title: Completion Engine
description: Multi-source autocomplete (Roadmap 0410) — the LSP server plus local index sources answer each trigger as independent tagged batches; the editor merges them into one popup with priority-based de-dup and stable selection.
resource: internal/complete
tags: [architecture, completion, autocomplete, lsp, sources, postfix]
timestamp: 2026-08-27T00:00:00Z
---

# Completion Engine

Roadmap 0410's hybrid completion: autocomplete is no longer a single LSP
round-trip but a **fan-in of independent sources**. Instant local answers open
the popup; the server's answer merges in when it arrives. A slow or dead
server degrades the popup instead of blocking it.

## The merge protocol

The unit of exchange is the tagged `lsp.CompletionMsg` batch: `Source` (a
name — `"lsp"`, `"words"`, `"symbols"`) plus `SourcePriority`. Anything that
listens to editor completion triggers and sends such batches is a completion
source at the protocol level. Today there are two producers:

- the **LSP bridge** (`plugins/lsp`), which keeps its own gating (server
  trigger characters, debounce, `isIncomplete` re-query, resolve) and tags its
  batches `Source: lsp.SourceLSP`, priority `lsp.PriorityLSP`;
- the **local engine** (`internal/complete`), which hosts in-process
  `Source` implementations.

Both are registered as named editor-event sinks (`host.SetEditorEmitter(name,
e)`); the host fans every editor event out to all sinks in deterministic name
order. Named registration is idempotent across project switches.

## The local engine (`internal/complete`)

`Source` is the in-process provider contract:

```go
type Source interface {
    Name() string     // batch tag; one popup shows one batch per name
    Priority() int    // merge order + de-dup winner (higher wins)
    Complete(ctx context.Context, req Request) ([]lsp.CompletionItem, error)
}
```

The `Engine` dispatches every registered source concurrently per completion
trigger, each on its own goroutine under a shared context: the engine timeout
(default 2s) bounds a dispatch, and a **new trigger cancels the previous
dispatch**, so late results are dropped rather than delivered stale. Identifier
runes and manual requests dispatch every local source — punctuation trigger
characters (`.`, `->`, `$`) are the LSP bridge's business; a local index has
nothing position-specific to say after a `.`.

**Trigger characters (#1913).** A source that *does* have something to say after
a punctuation character declares it:

```go
type TriggerSource interface{ TriggerChar(ch string) bool }
```

Such a character dispatches **only the claiming sources**, so the postfix source
answers a `.` while the word index still stays out of it.

**Exclusive sources (#1302).** A language source that fully owns completion for
its own files implements the optional extension

```go
type ExclusiveSource interface{ Exclusive(path string) bool }
```

The engine hands it the request's `LangName()` (below), so a file-less buffer
switched to the source's language is claimed like a real file of it (#2048).
While a source claims a path, the engine dispatches **only claiming sources**
for it. Without it every source answers every buffer: a `.http` header line
offered `Content-Type` next to `contentYOff` and every other identifier the
buffer-word and project-scan tiers had seen, and a request body offered nothing
but buffer words. No source claims anything by default, so every other language
keeps the full merged popup; the LSP bridge is not a `Source` and is unaffected.
The ES console's query buffers (#1927) use the same claim: `esq.CompletionSource`
owns `*.es.json` outright and offers Query-DSL keys plus the index mapping's
field names (see [Elasticsearch Console](/architecture/elasticsearch-console.md)).

## Buffer identity and language (#2048)

A `Request` carries three names, not one:

```go
type Request struct {
    Path     string // the file; empty for a buffer with no file
    Key      string // buffer identity — editor.ParseKey
    LangPath string // the name language lookups resolve
    ...
}
```

`Path` alone was enough while every completing buffer was a file. A **buffer
with no file** — a fresh tab, a split, a paste target — has no path at all, so
it had neither: every language-gated source (snippets, Emmet, postfix, the
symbol index's grammar extraction) resolved "no language", every path-keyed
text store folded all such buffers into the single empty key, and the answer
batch was routed to "every editor showing the path ''", which is no editor at
all. Since #2033 those buffers *do* have a language — the alt+enter intention
**"Treat Buffer as …"** — and completion has to follow it.

The two extra names split the two jobs `Path` was doing:

- **`Key` — identity.** `editor.ParseKey`: the file path, or the view's own
  tag (`\x00buffer/<n>`) when it has none. Sources key observed buffer text by
  `req.BufKey()` / `ev.BufKey()`, so two file-less buffers keep separate
  indexes, and the engine stamps it on every batch as `CompletionMsg.Key`.
  The app routes a batch by `RouteKey()` (`routeToEditorKey` →
  `pane.Instance.UpdateForParseKey`, the same route an async parse takes) and
  the editor accepts one only under its own `ParseKey`. For a file the key
  *is* the path, so every view of a shared document is still served.
- **`LangPath` — classification.** The buffer's `langPath()`: the file path,
  or the chosen language's **synthetic name** (`buffer.go`, `buffer.md`,
  `Dockerfile`). Sources resolve the language through `req.LangName()`, so a
  file-less buffer treated as Go is offered the Go templates and postfix
  transformations, the symbol index extracts it with the Go grammar, Emmet
  answers in a buffer treated as HTML, and an `ExclusiveSource` claims it the
  way it claims one of its files. Back on **Plain Text** the name is empty and
  every language-gated source falls silent again.

Both fields are empty for an ordinary file buffer and both accessors then
answer with `Path`, so no source needed a behaviour change for files. The
synthetic name is a *classification* name only — never opened, never written.

**Deliberately out of scope: LSP.** The bridge (`plugins/lsp`) speaks
`file://` URIs to a real server and needs a document that exists on disk; it
keeps reading `Path` and stays silent on a file-less buffer, exactly as
before. The ES console's query source is gated the same way for the same
reason: a query buffer's index name is encoded in its *file name*
(`esq.QueryRef`), which a synthetic name cannot supply.

## Editor-side merge (`internal/editor/lsp_state.go`)

The popup state keeps **one batch per source** for one request position
(`reqLine`/`reqCol`). A batch for the same position replaces that source's
previous contribution and the merged list is rebuilt:

- sources ordered by priority descending (name ascending on ties),
- items within a source in server order (`sortText`, label fallback),
- **de-dup by insert text** — the first occurrence, i.e. the
  highest-priority source's item, wins (the LSP item beats the word-index
  echo of the same identifier).

A batch for a *different* position replaces the popup outright; an empty
merge batch clears only its source's contribution (the popup closes when
every batch is empty); an empty non-merging batch is ignored so it can never
clobber another source's popup. The **selection is stable across merges**:
the selected item is re-located by identity (source + label + insert text)
after each rebuild, so a late-arriving batch never yanks the highlight while
the user is arrowing.

Fuzzy filtering (#845) runs on the merged list; `completionItem/resolve`
(#847) and its documentation/auto-import merge apply to `SourceLSP` items
only — local items never resolve, and resolve IDs cannot collide across
sources.

## Word index (#852)

`internal/complete/words` is the first local source (name `words`, priority
`lsp.PriorityWords`): vim-keyword-level completion from identifier words. Two
feeds: **open buffers** — the engine forwards every `EditorChange` event (the
optional `EventObserver` extension) and the buffer's word set re-extracts
lazily on the next query (large-file buffers drop out) — and a **one-shot
background project scan** at construction (skips dot-dirs, `node_modules`,
`vendor` & co.; 256KB/file, 10k files, binaries by NUL sniff). A query
computes the partial identifier at the cursor from the observed buffer text,
pre-filters by case-insensitive prefix, excludes the word being typed, caps at
200 items, and encodes the locality tier (current buffer < other buffers <
project) into `SortText` so nearer words list first. Words shorter than 3
runes or starting with a digit are noise and never indexed. Edits to files not
open in a buffer are not re-scanned — the buffer feed covers what the user
actually types in.

## Symbol index (#853)

`internal/complete/symbols` (name `symbols`, priority `lsp.PrioritySymbols`)
indexes project-wide identifiers through the **tree-sitter highlight layer**:
the captures the language grammars already produce (`function`,
`function.method`, `constructor`, `type`, `constant`) become completion items
with proper kinds — no server round-trip, no per-language extraction code.
Without cgo the grammar layer answers nothing and the source stays silent
(the word index covers those builds). **CSS files** contribute selector class
names and IDs (regex over `.css`/`.scss`/`.less`), offered inside HTML
`class="…"`/`id="…"` attribute values — detected on the current line, with
`data-class` & co. excluded — the cross-file case language servers are
structurally weak at. Freshness mirrors the word index (observed buffers
override the disk index; lazy re-extraction) plus **watcher invalidation**:
the app forwards file-change events through `Engine.NotifyFileChanged` to
sources implementing `FileObserver`, which re-extract off-goroutine — queued
behind a **single worker** with per-path dedup (#2176), so a mass checkout
cannot fan out into hundreds of concurrent disk readers. The
one-shot background scan is capped tighter (2000 files, 128KB) since each
file costs a parse.

## Unified ranking (#854)

The popup ranks the merged list with one score:

    score = fuzzy·4 + priority + locality + MRU

Fuzzy match quality (#845) dominates — the boosts top out well under a single
word-boundary bonus, so they only settle comparable matches. Priority is the
batch's source priority scaled down (LSP 100 → +4); locality reads the item's
`LocalityTier` (0 current file — and everything a server answers — +4,
1 other open buffers +2, 2 project scan +0), which the word/symbol sources
stamp; MRU boosts the last-accepted labels (rank 0 → +10 fading to 0 past
rank 10), fed by `internal/complete/mru` — a per-project, most-recent-first
label store persisted atomically at `.ike/completion-mru.json` and bumped on
every accept. Since #2146 the store is **scoped per language**: the editor
bumps and ranks under the buffer's resolved language id (`lang.ByPath` over
`langPath()`, so a file-less "Treat Buffer as …" buffer scopes like a real
file), an accept in a Go buffer boosts Go popups only, and buffers no
language claims share the `""` scope. A named scope with no hit falls back
to `""`, so a pre-scope flat-array store file keeps boosting after the
migration. An empty prefix ranks the same way with fuzzy 0, so a fresh
popup already prefers near and recently used items. Ties stay deterministic:
the sort is stable over the merged base order (#851).

## Emmet subset (#856)

`internal/complete/emmet` (name `emmet`, priority `lsp.PriorityEmmet`) covers
the high-frequency Emmet muscle memory as **snippet items** (#846) with an
expansion preview in the item detail: CSS property shorthands (`m10` →
`margin: 10px;`, `bg` → `background: $1;`, fixed forms like `df` →
`display: flex;`) in CSS/SCSS/LESS buffers, and HTML tag snippets (`div` →
`<div>$1</div>`, list/img/input/link special shapes) in HTML buffers, outside
attribute values. Full Emmet abbreviations (`ul>li*3`) contain
non-identifier characters the popup's identifier-replace accept path cannot
span and are deliberately out of scope.

## Live templates (#1152)

`internal/snippets` (name `snippets`, priority `lsp.PrioritySnippets` = 40 —
below symbols, above Emmet) offers the user's `[[snippets]]` config templates
plus the built-in examples as **snippet items** (kind snippet, detail
`template <preview>`), scoped to the buffer's language via `lang.ByPath`
(global entries everywhere). The source returns every matching template and
lets the popup's fuzzy prefix filter narrow the list, so it needs no buffer
text of its own; entries are read live from `config.Get()` per request, so a
config reload needs no re-wiring. Because the local engine answers triggers
independently of the LSP bridge, template items complete in plain buffers
with no server. On accept the editor recognises the `snippets` source name
and re-indents the body to the cursor's line before expansion — the same
shape the insert-mode Tab trigger produces (see
[editor](./editor.md)).

## Postfix completion (#1913)

`internal/complete/postfix` (name `lsp.SourcePostfix` = `postfix`, priority
`lsp.PriorityPostfix` = 20 — below every member the server offers on the same
dot) is the JetBrains habit of writing the expression first and the construct
after it: `err.nil` completes to `if err == nil { | }`, `foo(bar).if` wraps the
whole call, `xs.range` writes a range loop. It is the one local source that
implements `TriggerSource`, so typing the `.` opens the popup even where no
language server answers; typing further narrows it through the ordinary
client-side filter.

**It does not insert at the cursor.** The item carries
`CompletionItem.ReplacePrefix` — the `<expr>.` text — and the editor's accept
path widens the replacement span leftwards over it, so the whole
`<expr>.<template>` is rewritten. The widening only fires when the buffer really
carries that text immediately before the identifier start; otherwise the item
degrades to a plain insert (a secondary caret elsewhere, an edit since the
request). Bodies are LSP snippet syntax, so `$1`/`$0` run the usual tabstop
session (#846), and the source name is recognised for the same **re-indent to
the cursor's line** live templates get — a literal tab in a body becomes the
buffer's indent unit (tab width, spaces, EditorConfig) and continuation lines
inherit the current line's indentation.

**Expression detection** is Tree-sitter first: `highlight.ExpressionEndingAt`
parses the buffer and takes the **widest node ending exactly at the dot** whose
kind the language declares in `lang.Language.PostfixExprNodes`. The buffer is
syntactically broken while typing (`err.` is not a Go statement), but error
recovery parks the dot and the partial trigger word in a trailing `ERROR` node
and leaves the expression itself intact — that is exactly the case the tests
pin. The kind filter is what keeps "widest" honest: `x := foo(bar).` also has a
`short_var_declaration` ending at the dot, and only the expression kinds exclude
it. The declared kinds are the member-access chain plus literals, deliberately
*not* binary/unary expressions: on `a + b.if` the widest node would be the whole
sum; `(a + b).if` says so explicitly through `parenthesized_expression`.
Without a tree (no cgo, no grammar, no usable node) a bracket-aware token scan
walks left over identifier runes, chained dots and balanced bracket groups —
narrower than the tree answer rather than wrong.

**Templates are the language's.** `lang.Language.Postfix` is a list of
`lang.PostfixTemplate{Trigger, Body, Detail, ErrorLike}` where `EXPR`
(`lang.ExprPlaceholder`) marks the detected expression; a plugin contributes its
set exactly like `ScopeNodes`/`FoldNodes`, and a language registering none makes
the feature inert for its files. `ErrorLike` restricts a template to expressions
that read as an error value (`err`, `myErr`, `read_error`, `f().err`) — Go's
`.err` guard. Go ships `if`, `nil`, `err`, `for`, `range`, `ret`, `var`,
`print`; Python ships `if`, `for`, `ret`, `print`, `not`, `len`. Items are
snippet items marked `postfix <preview>` in the popup detail.

The source can be switched off with **`editor.postfix_completion`** (Settings →
Typing Assistance); the flag is read per query, so a config reload applies with
no re-wiring.

## Adding a source

Implement `Source`, register it on the app's engine (`completeEngine` in
`internal/app`) at build time. A source that owns a language's files
end-to-end should also implement `ExclusiveSource` (see above), or the generic
indexes will merge their identifiers into its popup; one that needs a
punctuation trigger implements `TriggerSource`. All Phase-2 sources have
landed.

Not every popup routes through the engine: the protocol is wired to *editor*
events, so a non-editor input rolls a self-contained aid in the same look and
keys — the [jq playground](./jq-playground.md)'s query line (#1979) is the
precedent, with synchronous candidates (snapshot keys, gojq builtins) and its
own popup mirroring the editor's accept/dismiss/navigation.
