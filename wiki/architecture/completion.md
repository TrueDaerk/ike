---
type: concept
title: Completion Engine
description: Multi-source autocomplete (Roadmap 0410) — the LSP server plus local index sources answer each trigger as independent tagged batches; the editor merges them into one popup with priority-based de-dup and stable selection. Identifier-rune triggers wait lsp.completion_delay_ms and one dispatch's local batches travel as a single message (#2541). The popup and the local sources filter with the JetBrains-style hump matcher under completion.case_sensitivity (#2650). The word and symbol indexes hold code tokens of one language at a time, scanned lazily per language, and every source resolves the effective language at the cursor — an embedded fence's inside one (#2652).
resource: internal/complete
tags: [architecture, completion, autocomplete, lsp, sources, postfix]
timestamp: 2026-09-21T12:00:00Z
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

**Two pass-count rules (#2541).** A bubbletea message is an Update plus a
full render, so the protocol also says *when* and *how many*:

- **Identifier runes wait.** An auto-trigger on a letter, digit or `_` waits
  `lsp.completion_delay_ms` (default 100, 0 = immediate, Settings UI →
  Language Support) for the next keystroke before either producer asks
  anything; a typing burst asks once, at its resting position. The bridge
  already debounced this (#849, formerly a fixed 80ms); the local engine now
  does too (`Engine.Delay`, read per trigger so a reload applies live). Server
  trigger characters (`.`), sources' own trigger characters (#1913) and the
  manual ctrl+space request never wait — the user asked, or the character
  itself is the position of interest — and any of them cancels an armed wait.
- **One dispatch, one message.** The engine's sources answer concurrently
  and each used to send its own `CompletionMsg` — with five sources that was
  five Update+View passes for a popup that opens once, empty answers
  included. The dispatch now gathers what lands within `gatherWindow` (15ms
  after the first answer, or as soon as every source answered) into a single
  `lsp.CompletionBatchMsg`; the app routes each batch exactly as it routes
  a lone `CompletionMsg`, so the editor's merge is unchanged. A source
  slower than the window still sends on its own when it arrives — a slow
  index never holds the popup back. The LSP bridge is not a Source and
  keeps its own message (it already drops empty replies).

Measured in the #2541 typing trace: 96 `CompletionMsg` per 42 keys became
12 `CompletionBatchMsg` (see [performance](performance.md)).

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
answers a `.` while the word index still stays out of it. The `.http` source
claims `{` the same way (#2158), so a `{{` opens the request file's variable
popup without waiting for a letter to follow the braces; a claim never reaches
past an exclusive claim on the path, so a claiming source that does not own the
buffer stays out of it.

**Observers that are not sources (#2667).** `Engine.RegisterObserver(o)` adds
an `EventObserver` / `FileObserver` that receives every editor event and
file-change notification exactly like an observing source but never takes
part in a dispatch. The [PHP trait index](php-trait-index.md) registers this
way: it keeps its declarations fresh from the same buffer edits and watcher
events the symbol index sees, and the trait completion source (#2668)
queries it instead of extracting anything itself.

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

The exclusive claim is also what lets a `.http` buffer complete from something
other than its own text: inside a `GRAPHQL` block's query section (#2423) the
source answers from the schema `http.graphqlIntrospect` cached for that
endpoint — fields, arguments and types, resolved by a caret walk over the
unfinished query rather than by a parse of it. Nothing is dispatched while
typing; the cache is the whole input (see
[HTTP Client](/architecture/http-client.md)).

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

**Effective language at the cursor (#2652).** A fourth field, `Lang`, is the
language id completion actually happens in: the id of the innermost embedded
fragment covering the position — a ```` ```python ```` fence in Markdown, the
`<script>` body of an HTML page, HTML inside a Python string — or the buffer
language's id otherwise (`""` for a buffer no language claims). The engine
computes it once per dispatch, off the UI goroutine, from the text it stashes
per buffer on every `EditorChange`: `highlight.Embedded` resolves the
fragments recursively (cached per text generation) and
`highlight.InnermostAt` picks the deepest one whose language is a *buffer
language* — a registered language with extensions or file names. The
Markdown grammar's `markdown_inline` pass and the regex mini-grammar are
injection targets, not languages a buffer is in, so prose stays `markdown`
and a regex literal stays JavaScript. The end of a fragment counts as inside
it, since that is where typing appends. Sources read it through
`req.LangID()` (which falls back to `lang.ByPath(LangName())` for a request
built without it) and `req.Langs()` — the id plus the language's
`lang.Language.CompletionPeers`, an optional list for families that share
identifiers (JS/TS, C and its headers); the default is empty, i.e. strict
same-language, and no registered language populates it yet. The word and
symbol indexes filter by it, and snippets (`snippets.ForLang`), postfix
(`lang.PostfixForLang`) and Emmet resolve their templates through it, so a
PHP fence in `README.md` gets PHP templates instead of Markdown's. Postfix
expression detection still parses the buffer under its own grammar and takes
the token fallback inside a fragment.

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
  echo of the same identifier); within one source, items sharing the insert
  text but differing in detail all stay (#2610: the same symbol importable
  from two modules is two auto-import candidates the user picks between by
  the module shown),
- **auto-import variants collapse** (#2653, `foldImportVariants`): items of
  one source with equal label, insert text and kind whose `ImportModule`
  (the `labelDetails.description` the bridge carries as its own field)
  differs form one group. The **canonical** item — module equal to the
  label (`logging` from `logging`), else the shortest module path (fewest
  `.`/`/` segments, then shortest string), else server order — stays a
  normal entry; the rest fold into **one** trailing entry rendered
  `<label>   +N modules` directly behind it, carrying them in `Variants`.
  A group of exactly two skips the folded entry and shows both, canonical
  first (two rows read faster than a row plus a picker). Items without an
  `ImportModule` — servers naming the module only in free-text detail —
  never group; free text is not parsed.

A batch for a *different* position replaces the popup outright; an empty
merge batch clears only its source's contribution (the popup closes when
every batch is empty); an empty non-merging batch is ignored so it can never
clobber another source's popup. The **selection is stable across merges**:
the selected item is re-located by identity (source + label + insert text)
after each rebuild, so a late-arriving batch never yanks the highlight while
the user is arrowing.

The folded entry has its own selection identity (`completionItemKey`
appends a marker) and never resolves — it inserts nothing. **Accepting it
opens the picker**: the popup itself switches to a variants mode
(`completionState.variants`), listing the variants by module under the same
filter, ranking, keys and rendering as the list, with its own hint row;
accepting a variant runs the ordinary accept (resolve cache,
`additionalTextEdits`, pending late import), Esc returns to the list on the
folded entry. A batch merge while the picker is open drops back to the
merged list — its items may belong to a superseded reply.

Hump filtering (#2650, see "Unified ranking") runs on the merged list; `completionItem/resolve`
(#847) and its documentation/auto-import merge apply to `SourceLSP` items
only — local items never resolve, and resolve IDs cannot collide across
sources. Every selected server item resolves, documented or not, and an
accept that outruns its resolve records a pending import the late reply
applies to the accepted text (#2610) — the ordering, the `Seq` stamp that
ties a reply to its popup, and the per-server auto-import options are in
[LSP](./lsp.md) ("Resolve-before-accept", "Auto-import server options").

## Word index (#852)

`internal/complete/words` is the first local source (name `words`, priority
`lsp.PriorityWords`): vim-keyword-level completion from identifier words. Two
feeds: **open buffers** — the engine forwards every `EditorChange` event (the
optional `EventObserver` extension) and the buffer's word set re-extracts
lazily on the next query (large-file buffers drop out); re-extraction runs
**outside the source's lock** (#2193) — texts snapshot under the lock,
tokenize unlocked, re-install generation-guarded — so a keystroke's `Observe`
(reached synchronously from `Update`) never blocks behind tokenizing dirty
buffers — and the **per-language project index** (below). A query
computes the partial identifier at the cursor from the observed buffer text,
pre-filters with the popup's hump matcher (`fuzzy.MatchHumpsCase` under
`completion.case_sensitivity`, #2650 — so `gur` reaches `GotoURLResolver`
from the local index, not only from a server), excludes the word being typed,
caps at 200 items, and encodes the locality tier (current buffer < other buffers <
project) into `SortText` so nearer words list first. Words shorter than 3
runes or starting with a digit are noise and never indexed.

**Code tokens of the same language only (#2652).** The index used to
tokenize raw text — string contents, comments, Markdown prose, JSON keys and
lock-file noise all became candidates, from every file type into one pool.
Now a word is an identifier in *code* of the request's language family
(`req.Langs()`): the tokenizer runs over `highlight.Segments`, the highlight
layer's per-language view of a text, which masks the grammar's `string` /
`comment` / `char` captures (the same mask the bracket scanner uses) and
attributes every embedded fragment to its own language, recursively. So a
Go buffer with `// commentWord`, `"stringWord"` and the identifier
`codeWord` offers only `codeWord`; `README.md` prose is not indexed at all
(Markdown is a prose language — `highlight.CodeLanguage` says which
languages have code tokens of their own: a registered file language with a
grammar that is not prose); and `$myObj` in the README's ```` ```php ````
fence lands in the PHP index, offered in a `.php` buffer and inside the fence
itself, never in a `.py` buffer. Every buffer's and file's words are stored
**per language id**, and the query reads only the ids in `req.Langs()`.
A buffer whose language has **no grammar** — Plain Text, a plugin disabled
at build time (#908), a no-cgo build — keeps the plain tokenizer over its
own text under its language id (`""` for Plain Text), so a plain-text buffer
still completes from itself and from other plain-text buffers; such files
contribute **nothing** to the project tier.

**Per-language lazy project index (#2652).** `internal/complete/langindex`
is the walk both indexes share: the first request from a buffer of language
*X* starts the scan for *X* in the background — `*.x` files plus the *X*
fragments embedded in other files — and a Go buffer opened later triggers the
Go scan; nothing is scanned for a language nobody completes in, and the
language filter at query time is a map lookup. A file another language owns
is read for *X* only when it can embed anything (its language has a grammar
or a region detector); every file read records the languages of its
embedded fragments, so the next language's scan skips the files that cannot
contain it without parsing them again. The walk keeps the caps and skips
(dot-dirs, `node_modules`, `vendor` & co.; 256KB/file and 10k files per
language for words, 128KB/2000 for symbols; binaries by NUL sniff), and the
watcher's `Engine.NotifyFileChanged` re-extracts a file for every scanned
language through the index's **single worker** with per-path dedup (#2176)
— the word index now refreshes on-disk edits too. Until a language's scan
finishes, its buffer tiers answer alone.

## Symbol index (#853)

`internal/complete/symbols` (name `symbols`, priority `lsp.PrioritySymbols`)
indexes project-wide identifiers through the **tree-sitter highlight layer**:
the captures the language grammars already produce (`function`,
`function.method`, `constructor`, `type`, `constant`) become completion items
with proper kinds — no server round-trip, no per-language extraction code.
Without cgo the grammar layer answers nothing and the source stays silent
(the word index covers those builds). **Same language only (#2652):** the
captures come from `highlight.Segments`, so every symbol is stored under the
language of the segment that declared it — the host's own code, or an
embedded fragment's language — and a query reads only `req.Langs()`: a Go
function is not offered in a Python buffer, and `func FencedFunc` in a
```` ```go ```` fence of a README is a Go symbol, offered in Go buffers and
inside the fence. **CSS files** contribute selector class
names and IDs (regex over the stylesheet's code text — a `<style>` fragment
in an HTML page counts too), offered inside HTML
`class="…"`/`id="…"` attribute values — detected on the current line, with
`data-class` & co. excluded — the cross-file case language servers are
structurally weak at; an HTML request also starts the stylesheet
languages' scan, and a stylesheet no plugin claims still indexes under
`css` by extension, so the feature survives a build without the web plugin
or cgo. Freshness mirrors the word index (observed buffers
override the disk index; lazy re-extraction outside the lock, #2193) plus
**watcher invalidation**:
the app forwards file-change events through `Engine.NotifyFileChanged` to
sources implementing `FileObserver`, which re-extract off-goroutine — queued
behind the shared index's **single worker** with per-path dedup (#2176), so
a mass checkout cannot fan out into hundreds of concurrent disk readers. The
per-language scan (see "Per-language lazy project index" above) is capped
tighter (2000 files, 128KB) since each file costs a parse.

## Unified ranking (#854)

**Which candidates survive** is decided first, by the JetBrains-style **hump
matcher** `fuzzy.MatchHumps` (#2650): every typed rune must either directly
continue the previous matched rune or sit at the start of a word segment of
the filter text — index 0, after `_`/`-`/`.`, a camelCase hump, the last
capital of an acronym run (`URLResolver`), or a letter↔digit change. So `my`
offers `mycelium` and `MY_CONSTANT` but no longer `empty` or `summary`,
`log` no `dialogBox`/`catalogOf`, while `gur` → `GotoURLResolver` and
`dacco` → `DataAccessObject`. The matcher is the same dynamic program as the
pickers' permissive `fuzzy.Match`, with a transition to position *j* only
legal when *j* continues the previous match or `isBoundary(j)`; `Match`
itself is unchanged for the palette, finder and settings search. The case
rule is **`completion.case_sensitivity`** (Settings → Language Support):
`first_letter` (default, IntelliJ's rule) lets a lowercase typed rune match
either case while an uppercase rune only matches an uppercase one (`DataA`
cannot match `database`), `none` folds every rune, `all` compares exactly.
The word and symbol sources pre-filter with the same matcher and setting
(read live from `config.Get()` once per query), and `Result.Prefix` reports
whether the filter text starts with the pattern so a ranking tier "exact
prefix > hump" needs no second pass.

The popup ranks the survivors by **match tier** (#2651, replacing the
single additive score of #854), and tiers never mix:

1. exact filter text (case-sensitive), then equal ignoring case
2. case-exact prefix of the filter text
3. prefix under the case rule (`Result.Prefix`)
4. every other hump match

So `data` lists `database` and `DataAccessObject` above `DumpAllTablesAgain`
even though the scattered CamelCase hit scores more fuzzy points, `dao`
lists the label `dao` above `DataAccessObject`, and `my` lists `mycelium`
above `MY_CONSTANT`. Inside a tier the comparator (`completionRank`, a small
struct compared field by field rather than a folded integer, so further keys
such as an auto-import duplicate can slot in) orders by:

1. **MRU** — the last-accepted labels first (rank 0 → 10 fading to 0 past
   rank 10), fed by `internal/complete/mru`; a recent accept tops its own
   tier but cannot climb into a better one
2. **locality** — the item's `LocalityTier` (0 current file and everything a
   server answers, 1 other open buffers, 2 project scan), which the
   word/symbol sources stamp
3. **length** — shorter filter text first (fewer unmatched runes)
4. **source priority** — the batch's priority (LSP 100 > symbols > … >
   words), so server order is only compared among one source's items
5. **server order** — `sortText` (label when absent), which gopls/pyright use
   to encode expected type, scope distance and deprecation; this survives as
   a real key now instead of only as the stable-sort tie-break
6. **fuzzy score** — the matcher's points, which in practice only settle the
   hump tier

An empty prefix (manual trigger at a word boundary) puts every item in tier 4
and skips the length key, so a fresh popup keeps its shape: recently used and
near items first, then server order. Ties stay deterministic: the sort is
stable over the merged base order (#851).

The MRU store is a per-project, most-recent-first
label store persisted atomically at `.ike/completion-mru.json` and bumped on
every accept. Since #2146 the store is **scoped per language**: the editor
bumps and ranks under the buffer's resolved language id (`lang.ByPath` over
`langPath()`, so a file-less "Treat Buffer as …" buffer scopes like a real
file), an accept in a Go buffer boosts Go popups only, and buffers no
language claims share the `""` scope. A named scope with no hit falls back
to `""`, so a pre-scope flat-array store file keeps boosting after the
migration.

## Emmet subset (#856)

`internal/complete/emmet` (name `emmet`, priority `lsp.PriorityEmmet`) covers
the high-frequency Emmet muscle memory as **snippet items** (#846) with an
expansion preview in the item detail: CSS property shorthands (`m10` →
`margin: 10px;`, `bg` → `background: $1;`, fixed forms like `df` →
`display: flex;`) in CSS/SCSS/LESS buffers, and HTML tag snippets (`div` →
`<div>$1</div>`, list/img/input/link special shapes) in HTML buffers, outside
attribute values. The buffer kind is the effective language at the cursor
(#2652): a `<style>` body in a page gets the CSS shorthands, a `<script>`
body gets neither, and a path no plugin claims still resolves by extension. Full Emmet abbreviations (`ul>li*3`) contain
non-identifier characters the popup's identifier-replace accept path cannot
span and are deliberately out of scope.

## Live templates (#1152)

`internal/snippets` (name `snippets`, priority `lsp.PrioritySnippets` = 40 —
below symbols, above Emmet) offers the user's `[[snippets]]` config templates
plus the built-in examples as **snippet items** (kind snippet, detail
`template <preview>`), scoped to the effective language at the cursor
(`req.LangID()`, #2652 — the buffer's language, or the embedded fragment's
inside one; `snippets.ForLang`) with global entries everywhere. The source returns every matching template and
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
whole call, `xs.range` writes a range loop. It implements `TriggerSource` (as
does the `.http` source for `{`, #2158), so typing the `.` opens the popup even
where no language server answers; typing further narrows it through the
ordinary client-side filter.

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

## Completion context (#2654)

Not every position deserves the popup. The editor classifies the cursor at
trigger time into a **completion context** and both producers gate on it —
the local engine through `complete.Request.Context`, the LSP bridge through
`host.EditorEvent.Context` (the same `lang.CompletionContext` value, a
string). The zero value is ordinary code, so a request built without one
behaves as before.

| context | auto-trigger | ordinary local sources | LSP |
|---|---|---|---|
| code | as always | all | asked |
| comment | withheld; ctrl+space opens | word index only, **current buffer only** | not asked |
| string literal | withheld | none (sources claiming strings stay) | asked — servers answer import paths and the like |
| declaration position | withheld; ctrl+space opens | all | asked |
| import line | as always | none | asked |

**Detection** (`lang.CompletionContextAt`, `internal/lang/complctx.go`) needs
no cgo and never parses the line:

- *comment* / *string*: the syntax highlighter's capture (`comment…`,
  `string…`) at the character **before the current word** — the `/` of `//`,
  the opening quote, a blank inside the comment. Not the capture at the
  cursor: the highlight index lags one parse behind the keystroke, so the
  just-typed rune is never inside a span yet. Without a grammar there is no
  capture and the position reads as code.
- *declaration position*: the identifier-delimited word left of the current
  word on the same line, separated by whitespace, is one of the language's
  **`DeclKeywords`** (case-insensitive): `func type var const package` for Go,
  `def class` for Python, `function class let const var interface type enum`
  for TypeScript/JavaScript, `function class interface trait enum const
  namespace` for PHP, `function local` for shell, `ARG ENV` for Dockerfile,
  `define` for make, `module` for go.mod, `keyframes` for CSS. A bracket or
  comma between the words is not whitespace, so `func(x` is a parameter list
  and `f(a, b` an argument list; a receiver's closing `)` likewise keeps
  `func (r *T) Na` in code, where gopls offers interface methods to implement.
- *import line*: the language's **`ImportLine`** regex matches the line
  (`^\s*import\b` for Go and TypeScript, `^\s*(import|from)\b` for Python,
  `^\s*use\b` for PHP).

Every registered language either lists its `DeclKeywords` or carries a
justified entry in the audit ledger `cmd/ike/declkeyword_audit_test.go`,
whose test fails otherwise — the same discipline as the span-family audit
(see [Change workflow](/process/change-workflow.md)).

**Gating.** The editor withholds the auto-trigger (`maybeAutoComplete`,
`internal/editor/keys_insert.go`) wherever the table says so; ctrl+space
always dispatches, tagged with the context. In the engine, a code or
declaration position keeps every source; any other context keeps only the
sources declaring it through the optional extension

```go
type ContextSource interface{ CompletesIn(ctx lang.CompletionContext) bool }
```

The word index claims comments and, seeing the context on the request,
answers with the current buffer's tier alone. The exclusive `.http` and
ES-query sources and the Ansible hosts source claim every context: their
answers are position-specific by construction and typed inside what the
grammar calls a string (a JSON key, a YAML plain scalar). The bridge skips
the server request in a comment and honours every other context.

## Adding a source

Implement `Source`, register it on the app's engine (`completeEngine` in
`internal/app`) at build time. A source that owns a language's files
end-to-end should also implement `ExclusiveSource` (see above), or the generic
indexes will merge their identifiers into its popup; one that needs a
punctuation trigger implements `TriggerSource`; one with something to say
inside a comment or a string literal implements `ContextSource` (#2654),
without which it runs in code and declaration positions only. All Phase-2
sources have landed.

Not every popup routes through the engine: the protocol is wired to *editor*
events, so a non-editor input rolls a self-contained aid in the same look and
keys — the [jq playground](./jq-playground.md)'s query line (#1979) is the
precedent, with synchronous candidates (snapshot keys, gojq builtins) and its
own popup mirroring the editor's accept/dismiss/navigation.
