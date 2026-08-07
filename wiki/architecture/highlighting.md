---
type: concept
title: Syntax Highlighting
description: The Tree-sitter lexical highlighting layer — per-language grammars parsed off the event loop into capture spans, cached by document version, resolved to theme colours, and applied per cell in the editor's renderLine; plus the pure-Go bracket-pair tracker behind rainbow brackets, unmatched-bracket errors and depth-coloured indent guides.
resource: internal/highlight
tags: [architecture, highlighting, tree-sitter, syntax, editor, theme, cgo, brackets]
timestamp: 2026-08-07T12:00:00Z
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
including the **rainbow bracket** captures below,
falling back from a dotted name (`function.builtin`) to its head (`function`), and
layered over built-in defaults by the `[theme.captures]` config table (a typed
slot map since #1318; see [Themes](./themes.md) for the precedence and the
colour-token rules).

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
  regex.go        built-in regex mini-grammar for fragment.regex injections (#1631).
```

A language's grammar is an opaque `lang.Grammar` built by `highlight.NewGrammar`
in the language plugin's cgo file; the query (`highlights.scm`) is embedded there
too.

**Capture-order convention** (#724, #928): `Index.CaptureAt` is
**first-span-wins**, and the query cursor yields captures in *node position
order* — an enclosing node's span precedes its children's; only captures on
the *same* node fall back to query-pattern order. Two rules follow for every
`highlights.scm`: (1) specific patterns before broader ones on the same node
(the identifier catch-all comes last), and (2) **never capture a container
node whose children need their own colors** — capture the sigil/name parts
instead (a whole-`(decorator)` capture painted the entire argument list
monochrome, #928; same class of bug: markdown's `fenced_code_block`, CSS's
`integer_value`+`unit`, and whole-string captures over interpolating strings —
Python f-strings and PHP encapsed strings/heredocs capture their
delimiter/content parts so `{…}` expressions highlight as code, with the
braces, format spec and conversion as `punctuation.special`, #1466). `Highlight(path, lines)` looks the language up via `lang.ByPath`, type-asserts
its grammar, and parses — the engine knows no specific language.

`HighlightScoped(path, lines)` is the same single parse returning the spans
**plus the sticky-scroll scopes** (#168): every multi-line node whose kind the
language lists in `lang.Language.ScopeNodes` (e.g. `function_declaration`,
`class_definition`) becomes a `Scope{HeaderLine, EndLine}`, emitted in
pre-order so `EnclosingScopes(scopes, line)` can return the enclosing headers
outermost-first (scope.go, pure Go). Scopes travel in `SpansMsg.Scopes`, so
sticky scroll costs no second CGo pass.

The same parse also collects the **code-folding ranges** (#144): every
multi-line node whose kind the language lists in `lang.Language.FoldNodes`
(falling back to `ScopeNodes` when unset) becomes a `Fold{HeaderLine,
EndLine}` (fold.go, pure Go), emitted in pre-order with same-header nodes (a
declaration and its body block) merged into one region. Folds travel in
`SpansMsg.Folds`; the per-view collapse state lives in
`internal/editor/fold.go` (see [editor](/architecture/editor.md)).

**Grammar-less languages fold too** (#1630): a language may register
`lang.Language.Folds`, a Go producer of `FoldRange{HeaderLine, EndLine}`
values, and `HighlightScoped` appends them to the parse's folds — the seam
exists for foldable structure no Tree-sitter grammar provides, first used by
the unified-diff language whose hunks fold at their `@@` headers
(`internal/unidiff`). Producers must emit pre-order (outer before inner),
matching what the editor's containment lookups expect.

**Embedded regions fold too** (#1329): every fragment is parsed with its own
language's fold kinds and its ranges are shifted into host coordinates
(`offsetFolds`), so a JSON body inside a `.http` request collapses exactly as it
would in a `.json` buffer, nested inside the host's own folds. Tree-sitter end
positions are exclusive, so a node ending at column 0 (a delimited node such as
a `.http` `section`, which ends where the next `###` begins) folds to the row
above — without that correction a collapsed request would swallow the next
request's header line.

## Language injections (issue #299)

`Highlight` also colours **embedded-language fragments** — an SQL string inside
Python renders as SQL. The host grammar's `injections.scm` (embedded in the
language plugin, capture convention `fragment.<lang>[.guess]`, shared with the
[LSP virtual-document seam](./lsp.md)) marks fragment ranges; each fragment is
parsed with its own language's registered grammar and the resulting spans are
shifted into host coordinates (`injection.go`). Injected spans are prepended to
the host span set, so inside a fragment they win over the host's enclosing
`string` capture in `Index.CaptureAt`, while gaps between injected tokens fall
back to the host colour. Hosts shipping an `injections.scm` today: **Python**
(SQL and HTML strings, guess-gated), **PHP** (#995: single/double-quoted
strings, heredoc and nowdoc bodies, guess-gated), **Go** (#995: raw and
interpreted string literals, guess-gated), **TypeScript** (#1625: HTML and SQL
in template literals, guess-gated), **Markdown** (block injections, see below)
and **HTML** (script/style). The `.guess` suffix defers to the Go-side content
heuristic in `fragment.go` — SQL keyword leaders, HTML tag shape (must open
with a tag and, unless it is a doctype/comment, contain a closing or
self-closing marker) — so plain strings never become fragments. Guessed
captures sharing a capture name and a parent node are judged **together** over
their joined text (#1625): a template literal's chunks around `${…}`
substitutions are separate `string_fragment` nodes, and no single chunk of
`` `<ul>${items}</ul>` `` looks like HTML on its own; on a hit each chunk
injects while the substitution expressions keep their host highlighting.
Fragments re-highlight with every reparse, exactly
like top-level edits (the whole buffer reparses per change, off the event
loop). Injections resolve **recursively** (#1697): each fragment's own
language runs its injection query in turn, so HTML injected into a Python
f-string still highlights its `<script>` body as JavaScript and its `<style>`
body as CSS — down to three nesting levels below the host
(`maxInjectionDepth`), past which fragments keep their enclosing language's
styling so pathological nesting cannot blow up a parse.
Fragment languages without a registered grammar degrade to plain host
highlighting.

Hosts can also mark fragments **without a query** through the registry's
Go-level region detector (`lang.Language.Regions`, #1303) — the seam for
decisions no injection query can express. **YAML** uses it for CI pipelines
(#1625): in a buffer containing a `steps:` line, the value of every `run:` key
— a block scalar's indented lines, or the (quote-stripped, comment-cut) inline
scalar — becomes a `shell` fragment, so GitHub-Actions/CircleCI step scripts
highlight with the shell grammar. Arbitrary YAML with a `run:` key stays plain.
Region detectors run before the grammar's own injection query and replace it
(`fragmentsFor`); injected spans stack under the host's `Spans` hook exactly
like query-detected fragments, so YAML's base64 decoding and cron hints keep
working inside a workflow.

One injection target is **not a language**: `@fragment.regex` (#1631) routes to
a built-in regex mini-grammar (`regex.go`, pure Go) instead of a registered
grammar — `overlayFragments` special-cases the `regex` id before the registry
lookup. Context detection lives in the hosts' injection queries, gated by
tree-sitter text predicates (`#match?`/`#eq?`, evaluated by the go-tree-sitter
binding): **Go** injects the first argument of
`regexp.Compile/MustCompile(POSIX)`, **Python** the first string argument of
the `re` module's matchers (`re.compile`, `re.match`, `re.sub`, …), and
**TypeScript/JS** every `/…/` literal plus the first string argument of
`new RegExp(…)` / `RegExp(…)`. The tokenizer emits captures for character
classes (`regex.class` — `[…]` bodies, `\d`-style shorthands, `\p{…}`, the `.`
wildcard), escapes (`regex.escape`), quantifiers (`regex.quantifier`, including
`{m,n}` counts and lazy suffixes), anchors (`regex.anchor` — `^ $ \b \A …`),
alternation (`regex.alternation`), group names (`regex.group.name`), inline
flags (`regex.flags`) and `(?#…)` comments (`regex.comment`); literal runs emit
no span, so the host string colour shows through. All the captures derive from
the active palette via `regexSources` in `theme.go` (same mechanism as the
diff captures, overridable per `theme.captures.regex.*`). Group parens pair by
colour: open and close of the same group share a rainbow-bracket slot keyed by
nesting depth (`rainbow.N`), falling back to the flat `regex.group` capture
when rainbow brackets are disabled. On the LSP side a `regex` fragment resolves
no server and is skipped silently, so the virtual-document seam is unaffected.

Since #880 the query can also name the language **dynamically**: a pattern
capturing `@fragment.language` (a tag node, e.g. a markdown fence info string)
together with `@fragment.content` injects the language the tag's *text* names —
resolved as a language id first, then as a file extension (`\`\`\`go`,
`\`\`\`py`). Detection iterates query *matches* (not lone captures) so the pair
arrives together; unknown tags leave the host styling. Markdown is the first
user: its block grammar injects the separate `markdown_inline` grammar into
every `(inline)` node (headings, paragraphs), fenced code into the fence's
language, and YAML/TOML front matter into those grammars.

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
`highlight.SpansMsg{Path, Version, Spans, Scopes, Folds}`. The app routes it back to the editor
leaf owning the path; the editor caches the spans **only if the version still
matches** (a newer edit drops stale results). Before delivery,
`HighlightScoped` overlays any Go-computed spans the language registers
(`lang.Language.Spans`, #1585 — see `/architecture/languages.md`) by
prepending them, so they beat the grammar's coarser captures the way
injected-fragment spans do. `renderLine` then looks up the
capture per rune cell and wraps it in the themed style — in the default branch,
so the cursor and the visual selection still win on overlap, and a diagnostic
underline composes on top.

Large-file mode (#149) gates this at the source: a document flagged by the
`files.large_file_kb` / `files.large_file_lines` thresholds never schedules a
parse (`parseCmd` returns nil before the grammar check) — the CGo parse cost
scales with file size, so this is the single biggest degradation win. The
palette command `editor.forceCodeInsight` re-enables it per document; see
`/architecture/editor.md`.

## Testing

The span model, per-line index and theme fallback are pure-Go unit tests. The
real Tree-sitter path (behind the `cgo` tag) is exercised by parsing Go/PHP/Python
fixtures and asserting capture output, and the editor's render integration is
tested by feeding `SpansMsg` into `editor.Model.Update` and checking the rendered
ANSI.

Pairing, depth and the string/comment edge cases are pure-Go unit tests in
`internal/bracket` — no grammar needed. `highlight/brackets_test.go` covers
the capture mapping, the grammar mask, the heuristic fallback and the prose
exclusion; `editor/guides_test.go` covers the guide palette, the slot cycle
and the unmatched-bracket style.

## Rainbow brackets & bracket pairing (#789, #1628)

Bracket pairing lives in **`internal/bracket`** — a leaf package, pure Go, no
Tree-sitter: `Scan(lines, Syntax)` walks the buffer with a stack and returns
one `Mark{Line, Col, Depth, Open, Unmatched}` per `(`, `[`, `{`, `)`, `]`,
`}`. Both halves of a pair carry the **same** depth, so they colour alike, and
the stack is buffer-wide (a brace opened on line 1 still deepens line 400).
A closer matches the innermost open bracket **of its own kind**, so a single
typo does not cascade: in `{ ( }` the `}` still closes its `{` and only the
`(` comes back `Unmatched`. Openers left on the stack at the end, and closers
that find nothing open, are `Unmatched` too.

Brackets inside strings and comments must not pair. `Syntax` offers two ways
to skip them:

- **`Skip`** — an exact mask. `highlight/brackets.go` builds one from the
  spans the grammar just produced (captures under `string`, `comment`,
  `char`), so the grammar decides what a string is: Python docstrings, Go raw
  strings, nested comments and all.
- **The heuristics** (`LineComment`, `BlockComment`, `Quotes`) — used when no
  parse produced spans. Quotes are line-local: an opening quote with no
  partner on the same line is an ordinary rune, so `don't` and a Rust lifetime
  do not swallow the rest of the line.

`bracketSpans` turns marks into spans: depth `N` maps to capture
`rainbow.<N mod 6>` (`RainbowColors`, `RainbowCapture`), an unmatched bracket
to `bracket.unmatched`. They are prepended (`Index.CaptureAt` is
first-covering-wins, so they beat the grammar's punctuation captures) but stay
*behind* the Go-produced spans of #1585, so a language that colours a cell
itself — csv columns — keeps its colour. `HighlightFenced` scans too, so a
JSON body in the HTTP response pane nests like a buffer.

**Prose is not scanned** (`proseLangs`: markdown, plain text, log, csv/tsv).
A `(see below` in a paragraph is not a mistake, and marking it as one would
clutter the very files that want the least chrome — so those languages get
neither depth colours nor unmatched marks.

Colors derive from the active palette (`rainbowSources`: keyword, string,
function, number, type, constant), so light and dark themes both stay
legible; `theme.captures.rainbow.N` config keys override single slots. The
same palette serves [identifier colors](./editor.md) (#1626) and the
depth-coloured indent guides below. An unmatched bracket renders in the
theme's `Error` slot **underlined** (`editor/guides.go`,
`unmatchedBracketStyle`); `theme.captures.bracket.unmatched` overrides the
colour and keeps the underline.

Toggle: `editor.rainbow_brackets` (settings Editor page, **default on**) —
gated by an atomic read in the background parse, and a config-reload flip
re-parses every open editor immediately. Off means no bracket spans at all,
unmatched ones included. Cost is one linear scan of the buffer per async
parse; the render path is untouched (spans flow through the existing
per-version cache, 0400).

## YAML anchor pairing (#1629)

YAML anchors and aliases take the palette a third way: **keyed on the name**,
the `internal/idcolor` content-hash trick. `internal/yamlanchor` — a leaf
package like `internal/bracket` — scans the buffer line-based (no parser):
`&name`/`*name` count only at a node-start position (after `:`, `-`, `,`,
`[`, `{`, `?` or line start — exactly where YAML reserves the indicators),
never inside comments, quoted scalars or block-scalar bodies. Each alias
resolves to the nearest **preceding** same-name anchor of its **document**
(`---` resets, redefinition shadows — the YAML rule), and `Spans` emits
`rainbow.<hash(name) mod 6>` for every mark, so an anchor and all its aliases
colour alike across the file. An alias no anchor defines emits
`anchor.unresolved` instead: the editor renders it like an unmatched bracket —
error colour, underlined — override-able via `theme.captures.anchor.unresolved`.
The producer rides the `yaml` plugin's `Spans` hook, so it is always on where
YAML highlights at all; the same scan backs goto-anchor, find-usages and the
resolved-value hover (see [lsp.md](./lsp.md), local providers).

## Depth-coloured indent guides (#1628)

Indent guides (#64) take the same palette, one slot per indent level, which is
what makes nesting readable where there are no brackets to colour at all —
YAML, Python. `editor/guides.go` builds the styles: `guideStyles` returns one
style per rainbow slot, each mixed toward the theme's `IndentGuide` colour
(`guideTintFrac`, 0.55) so the indentation stays chrome rather than shouting
over the code; `guideSlot(abs, tabWidth, slots)` maps a guide cell to its slot
(the first stop is level 1 → slot 0, the depth its brackets would take).

Toggle: `editor.rainbow_indent_guides` (**default on**), independent of both
`editor.rainbow_brackets` and `editor.indent_guides` — the guides themselves
stay off until `editor.indent_guides` (or `view.toggleIndentGuides`) draws
them, and turning depth colouring off returns every guide to the flat theme
colour. Visible whitespace still wins over a guide cell on overlap.
