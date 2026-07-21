---
type: concept
title: Completion Engine
description: Multi-source autocomplete (Roadmap 0410) — the LSP server plus local index sources answer each trigger as independent tagged batches; the editor merges them into one popup with priority-based de-dup and stable selection.
resource: internal/complete
tags: [architecture, completion, autocomplete, lsp, sources]
timestamp: 2026-07-21T00:00:00Z
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
dispatch**, so late results are dropped rather than delivered stale. Only
identifier runes and manual requests dispatch the local sources — punctuation
trigger characters (`.`, `->`, `$`) are the LSP bridge's business; a local
index has nothing position-specific to say after a `.`.

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

## Adding a source

Implement `Source`, register it on the app's engine (`completeEngine` in
`internal/app`) at build time. Planned sources: the tree-sitter symbol index (#853, including CSS class
names offered in HTML), Emmet (#856). Unified ranking across sources (locality, MRU) is #854.
