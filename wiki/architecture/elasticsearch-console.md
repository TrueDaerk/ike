---
type: concept
title: Elasticsearch Console
description: "#1927 — a read-only console per configured cluster: index sidebar with doc counts, paged hit grid over from/size, per-index Query-DSL buffers as real files with mapping-aware completion; every cluster request asynchronous"
resource: internal/espane
tags: [architecture, elasticsearch, pane, read-only, grid, completion, async]
timestamp: 2026-08-18T00:00:00Z
---

# Elasticsearch Console (#1927)

A tool pane for browsing an Elasticsearch cluster and running Query-DSL
queries: a sidebar listing the cluster's indices and aliases with doc counts,
a paged read-only grid of search hits, and one Query-DSL buffer per index —
an ordinary JSON file edited in the normal editor, with completion fed from
the index mapping. The console is strictly read-only against the cluster: the
client can only form `_cat`, `_mapping`, `_search` requests.

## Three packages, one HTTP-aware

- **`internal/esq`** is the backend: a small typed client over the cluster's
  read APIs, the mapping→field derivation completion builds on, the
  search-response→grid mapping, and the query-buffer naming. No raw JSON or
  HTTP leaks past it.
- **`internal/espane`** is the pane model: regions, cursors, paging, render —
  the data viewer's layout and keys (see [Data Viewer](/architecture/data-viewer.md)),
  bound to one configured endpoint by name.
- **`internal/app/es.go`** wires it in: per-endpoint palette commands, result
  routing by pane key, the read-only JSON views, and `es.run`.

## Why not the data viewer's Source seam

The obvious move — implement `datasrc.Source` over ES and reuse
`internal/dataview` wholesale — was considered and rejected: the data viewer
fetches pages *synchronously* in `Update`, which is correct for local files
and a UI stall for a network round trip. The console copies the data viewer's
patterns instead and applies the background-open discipline (#1795) to
**every** fetch: connect, index listing, each page, each mapping — all
`tea.Cmd` goroutines funneling into one key-routed `espane.ResultMsg`. A page
fetch is stamped with a sequence number; a result superseded by a newer fetch
(or a switched index) is dropped on landing. One page fetch is in flight at a
time — holding `n` down must not fan out a request per keypress — while an
index switch or a query run supersedes the flight rather than waiting.

## Connections

Endpoints live in config as the `[[elasticsearch.endpoints]]` table array
(name, URL, basic auth or API key — mutually exclusive), edited on the
Settings → Elasticsearch page (strict form validation; the config validator
leniently drops unusable entries with a diagnostic, `config.ESURLError` being
the shared URL check). Each configured endpoint appears in the palette as
`ES Console: <name>` (`es.console.<slug>`), opening one console per cluster —
keyed `es:<endpoint>`, refocused rather than duplicated, persisted by
endpoint name and reconnected in the background on restore. The endpoint is
re-resolved against the live config on every open, so a config edit heals a
restored pane; a dead endpoint degrades to a notice with `r` to retry.

## The pane

Two regions with `tab` (and `h` at the grid's left edge); the focused console
claims `tab` before the global focus cycle, like the data viewer. The sidebar
lists indices (doc counts from `_cat/indices` — the cluster's own metadata,
exact and cheap) and aliases (`?` count, `a` tag). `enter`/`l` loads an index:
its first hit page plus, in the background, its mapping — which primes
completion and the `s` view.

The grid shows hits as rows: `_id`, `_score`, then the union of `_source`
keys across the page, sorted. Scalars render bare; nested objects and arrays
render as compact JSON in the cell, with the full pretty document one
keypress away (`v`/`enter`) in a read-only tab at the virtual path
`<endpoint>!<index>.hit-N.json` — the schema-buffer convention (#1764), so
JSON highlighting and folding apply. `s` shows the mapping the same way;
`a` the aggregations, when the query asked for any.

Paging is `from`/`size` behind the grid keys (`n`/`p` whole pages, `j`/`k`
across page edges, `g`/`G` first/last): every search forces
`track_total_hits`, so totals are exact, `G` has a number to aim at, and no
separate count request ever runs. Page size is 100 — every page is a network
fetch of full documents. A query error (the cluster's own root-cause reason)
renders in the grid while the last good page's rows are kept.

## Query buffers

`q` opens the selected index's Query-DSL buffer: a real JSON file at
`<state>/es/<endpoint>/<index>.es.json` (the scratch-file philosophy — no
special buffer type), seeded with a runnable match-all body. Being a real
file buys the full editor, JSON highlighting, folding, session restore and
the completion engine. `es.run` (palette, editor scope) sends the buffer:
the text becomes the index's active query, the console loads a fresh first
page on it — opening the console first and queueing the query when it was not
open — and the header marks the index as filtered. The body's own
`from`/`size` are overridden by the grid's paging.

## Completion

`esq.CompletionSource` claims `*.es.json` exclusively (#1302): inside a JSON
string it offers the common Query-DSL keys (badge `dsl`) and the index's
fields with their mapping types (`keyword`, `date`, …), derived by
flattening the mapping's `properties` recursively — object children and
multi-fields as dotted paths (`seller.address.city`, `title.keyword`).
Accepting a dotted field replaces the whole typed prefix via
`CompletionItem.ReplacePrefix` (#1913). Fields come from a package-level
cache the pane primes when an index loads; a cache miss fetches the mapping
under the engine's dispatch context (success cached 5 min, errors 10 s, a
hiccup keeps the last good list). The buffer's endpoint+index resolve through
the in-run path registry, else the directory layout (session restore).

## Testing

`internal/esq` and `internal/espane` test against `httptest` fake clusters:
client parsing and auth headers, from/size/track_total_hits injection,
mapping→field flattening, response→grid mapping, and the pane flow end to
end (open lists indices, select loads hits, paging past the first page, dead
endpoint degrades and retries, query errors keep the page, doc/mapping/aggs
messages). The settings page tests cover the strict form validation and both
write scopes; the config validator's endpoint checks live with the other
clamp-and-warn tests.
