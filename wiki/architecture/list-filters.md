---
type: concept
title: List Filter Syntax
description: One filter expression language shared by every list pane — fielded terms plus free match text, parsed by internal/filterexpr against a per-pane schema and typed into the internal/filterbar row that Problems, Usages and the TODO index all wear; the Issues pane's saved filters are the same syntax, and each pane's single-key filters are sugar that writes into it (#2156).
resource: internal/filterexpr/filterexpr.go
tags: [architecture, filter, tool-window, pane, search, issues, problems, usages, todo]
timestamp: 2026-08-28T12:00:00Z
---

# List Filter Syntax (#2156)

Every list pane in IKE narrows the same way. The syntax the
[Issues pane](./github-issues.md) introduced for its match input and its
saved filters (#2110/#2115) is now the syntax the
[Problems](./problems.md) pane, the [Usages](./usages.md) pane and the
[TODO index](./todo-index.md) accept too — one language, one parser, one
input widget, one focus key.

## The language

An expression is a sequence of whitespace-separated tokens:

```
severity:error file:internal/**/*.go missing return
```

- A token `name:value` whose name is all ASCII letters (read
  case-insensitively) is a **fielded term**. Which names exist is per pane
  (below); an unknown one is an error naming the pane's fields, not silently
  ignored text.
- Everything else is **free match text**, joined by single spaces in written
  order and run through the shared fuzzy matcher (`internal/fuzzy`, the same
  gate and score the Issues list ranks by).
- Double quotes group a value carrying spaces — `label:"good first issue"`,
  `file:"src dir/*.go"`. Quotes are the only grouping the syntax has; there
  is no escape. A fully quoted token (`"file:x"`) is match text, so quoting
  is also how a colon is kept literal.
- Terms of **different** fields are AND'd; **repeats of one field** are OR'd
  (`file:*.ts file:*.md` widens, `severity:error file:*.go` narrows). For the
  single-valued gates (`is:`, `scope:`) the last one written wins.

## The core — `internal/filterexpr`

A leaf package: tokenizer, quoting, schema validation, formatting and the two
match helpers. It knows nothing about any pane.

- `Field{Name, Aliases, Values, ValueDoc, Doc}` — one accepted qualifier.
  `Values` is a closed vocabulary (a value outside it is an error);
  `ValueDoc` names what a free-form field wants, which is both the error's
  advice and the "this field needs a value" rule.
- `Schema{Fields}` — a pane's field set. `Schema.Parse(expr)` returns a
  `Query` (the terms with canonical field names plus the match text) or an
  error worded as advice: `severity: wants error, warning`,
  `unknown qualifier "author:", use severity:, file: or scope: — anything
  else is match text`, `unterminated quote`. The same message reaches a
  settings form, a config diagnostic and a pane's filter row.
- `Query.Value(field)` / `Values(field)` / `Has(field)` are how a pane reads
  a parse back; `Format(query)` writes one back out and round-trips.
- `MatchText(pattern, text)` is the free-text gate (fuzzy, scored).
  `MatchPath(pattern, path)` is what every `file:` field uses: a pattern with
  glob meta characters matches through `internal/pathglob` against the whole
  path *and* against any tail of it — so `*.go` means "somewhere below" —
  while a meta-free pattern is a case-insensitive substring, which is what a
  half-typed directory name is.

Parsing is strict on purpose. A whole expression — a config value, a saved
filter, a pane's filter line — has no reader who could notice that a token
did nothing, so an unknown field is an error. A *live, mid-typing* input may
be lenient instead: the Issues overlay's match row keeps its own leaf
(`internal/ghissues/qualifier.go`, the twin of `Tokenize` that tolerates a
half-typed quote and reports the trailing separator) and leaves an unknown
token as literal fuzzy text while explaining why.

## The row — `internal/filterbar`

`filterbar.Model` is the one-line input the panes render. It holds the text,
the cursor, the last successful `Query` and the current parse error; the pane
supplies the schema and nothing else about the widget differs between panes.

- The row is **permanent** (the rule #2104 set for the Issues filter row): a
  filter appearing must not shift the list by a line. Idle it renders the
  expression, or a hint naming the pane's fields; focused it renders the
  cursor, the inline completion ghost and any parse error.
- `/` focuses it — in every pane. `enter` applies and leaves the input, `esc`
  clears the filter and leaves it, `tab` accepts the ghost. Editing keys come
  from the shared `ui.EditKey` layer (#763); **navigation keys fall through**
  to the pane, so the list can be steered while typing.
- **Completion**: the rest of a field name (`sev` → `erity:`), or the rest of
  a value — closed vocabularies from the schema, free-form values from the
  pane's own `Candidates` hook (the files it currently lists). A completed
  value ends in its separating space, so accepting it applies at once.
- A **half-written expression keeps the last good query**, so the list does
  not empty out mid-token; the error explains what is wrong meanwhile.
- `SetTerm(field, value)` / `HasTerm(field, value)` are the quick-key seam:
  a single-key filter rewrites exactly its own field and leaves the rest of
  the expression — including the match text — alone.

## Per-pane fields

| Pane | Fields |
| --- | --- |
| [Issues](./github-issues.md) | `is:` (alias `state:`) open/closed/all · `label:` repeatable · `sort:` |
| [Problems](./problems.md) | `severity:` (alias `sev:`) error/warning/info/hint · `file:` · `code:` · `source:` (alias `src:`) · `scope:` file/project |
| [Usages](./usages.md) | `file:` · `text:` |
| [TODO index](./todo-index.md) | `tag:` (the configured pattern words) · `file:` · `scope:` file/project |

The Issues dialect lives in `internal/issuefilter` — the schema plus the
`Spec` shape the config validator and the settings form read; it is what
`issues.default_filter` and `issues.saved_filters` are written in (see
[Config](./config.md)).

## Single-key filters are sugar

The pre-#2156 one-key filters were separate state. They are now writes into
the shared expression, which is why the key and the typed term can never
disagree:

- Problems `f` writes `scope:file` (and removes it again). `scope:` resolves
  against the *current* active editor file on every refresh, so the scope
  still follows the editor rather than pinning the path it was pressed on.
- TODO index `ctrl+t`/`alt+t` steps the `tag:` term through the configured
  patterns and back to none; `ctrl+o`/`alt+o` toggles `scope:file`. The
  chips row above the input renders those two terms — it is a view of the
  filter, not a state beside it — and clicking a chip is the same write the
  key is.

Adding a field to a pane is: name it in that pane's `Schema`, gate the row on
it in the pane's `matches`, and — if it deserves a shortcut — have the key
call `SetTerm`.
