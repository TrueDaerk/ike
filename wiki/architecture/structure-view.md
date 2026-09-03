---
type: concept
title: Structure View
description: "Structure tool pane (#1025) — the focused buffer's symbol tree from LSP textDocument/documentSymbol: singleton right-split pane, capability-gated request through the manager, version-stamped per-buffer cache with debounced refresh (#2319) that parks with the workspace and collapses dispatch bursts (#2401), cursor auto-follow, enter/double-click navigates via the open funnel."
resource: internal/structpanel
tags: [architecture, lsp, structure, tool-window]
timestamp: 2026-09-03T00:00:00Z
---

# Structure View (#1025)

JetBrains' Structure tool window scaled to the terminal: a singleton tool
pane showing the focused buffer's symbol tree, backed by LSP
`textDocument/documentSymbol`. This is the MVP slice of #31 — a Tree-sitter
fallback for server-less languages remains a follow-up tracked there. The
editor breadcrumbs bar (#1153, `/architecture/editor.md`) consumes the same
data: `applyDocumentSymbols` caches the hierarchical tree app-side per path
(`docSymbols`), and `structureSyncCmd` issues the request even with the pane
closed while `editor.breadcrumbs` is enabled. Sticky scroll's symbol fallback
(#2167, `app/symbolscopes.go`) is the third consumer of the same cache — it
pins enclosing declarations for languages with no Tree-sitter grammar — and
keeps the funnel alive with both the pane closed and breadcrumbs off; none of
the three costs an extra request.

## Data path

```
structure.toggle ──▶ app opens pane ──▶ RunCommand("lsp.documentSymbols")
                                              │  (registry command, LSP plugin)
                                              ▼
                              bridge.documentSymbols → manager.DocumentSymbols
                                              │  capability-gated on
                                              │  documentSymbolProvider
                                              ▼
                              client.DocumentSymbols (both reply shapes)
                                              ▼
                     ilsp.DocumentSymbolsMsg{Path, Symbols, NoProvider}
                                              ▼
                       app routes into structpanel.Model.SetSymbols
```

- **Protocol / client** (`internal/lsp/protocol`, `internal/lsp/client`): the
  request decodes both reply shapes — hierarchical `DocumentSymbol[]` and flat
  `SymbolInformation[]` (told apart per element by the `location` key). Flat
  entries normalise into childless `DocumentSymbol` nodes whose ranges are the
  location range and whose `containerName` becomes the detail. The client
  advertises `hierarchicalDocumentSymbolSupport`, so capable servers send the
  tree shape.
- **Manager** (`internal/lsp/manager.DocumentSymbols`): gates on the
  `documentSymbolProvider` capability (`ok=false` = "nobody to ask", distinct
  from "no symbols") and converts to editor rune coordinates via
  `ilsp.ConvertDocumentSymbols` using the synced document lines and the
  negotiated encoding. `SymbolNode` carries name, detail, kind, the
  selection-range start (navigation target) and the construct's end line
  (enclosing-symbol test).
- **Bridge** (`plugins/lsp/bridge.go`): the `lsp.documentSymbols` registry
  command requests for the bridge's current path, flushing pending didChange
  first. Errors stay silent — the refresh is passive; the pane keeps its last
  tree.

## The pane

`internal/structpanel` mirrors the VCS panel: a value-type `Model` embedded in
a `pane.Instance` under `pane.KindStructure` / singleton key `"structure"`
(`Registry.AddStructure`). The app's `structure.toggle` command drives the
usual tool-window state machine (open right of the active editor → focus →
return focus). Rows are the depth-first flattened tree — kind glyph
(`KindGlyph`), indent by depth, faint detail — with the standard list keys
(`j/k`, `g/G`, page keys), wheel scrolling and click/double-click mouse
handling (explorer-style 400 ms window).

Enter or a double-click emits `structpanel.NavigateMsg`; the app answers with
`openPathAt`, the same funnel definition jumps use, so navigation history
records the jump (`nav.back` returns).

## Refresh & follow (app wiring)

`internal/app/structure_panel.go` owns the triggers; the LSP manager stays
unreachable from the app:

- **Pane open** resets the request dedup and refills — from the cache when
  the buffer is unchanged, else via an immediate (undebounced) request.
- **Focused buffer change**: `structureSyncCmd` runs from the Update wrapper
  once per settled pass. Each cached reply (`docSymbols`, a `docSymEntry`) is
  stamped with the buffer `DocVersion` the request was issued at (#2319):
  switching to a buffer whose cached tree matches the current version refeeds
  the pane from the cache — no server round trip — while a missing or
  edited-past tree arms a 250 ms debounce (`structDebounceDelay`). The timer's
  tick (`structDebounceMsg`) dispatches only when its seq is still current and
  the active buffer still matches the armed path and version, so rapid
  tab-cycling or a typing burst sends one request for the buffer the user
  settles on. Outstanding requests dedup on path + version
  (`structReqPath`/`structReqVersion`); provider-less replies cache as
  `NoProvider` and stay fresh regardless of version, so such files never
  re-request per edit.
- **Edit**: every buffer change bumps `DocVersion`, invalidating the cached
  tree; the debounced refresh above updates the pane in the background once
  typing pauses.
- **Save**: the `todoSavedMsg` handler sets `structForce`, which bypasses
  dedup and debounce — but not the cache (#2401): a write whose content the
  cached tree already covers (its stamp equals the buffer's current
  `DocVersion`) consumes the flag and asks nothing, because the server sees
  the very document it answered for. A save past the cached version — the
  usual case, an edit then a write — dispatches immediately as before.
- **Burst dedup**: every dispatch passes `structBurstWindow` (150 ms, #2401):
  a repeat for the same (path, `DocVersion`) inside the window is dropped
  before `RunCommand`, so no LSP request leaves and no `internal` telemetry
  event is recorded for it. Focus and tab-switch flurries — the telemetry
  behind #2401 saw 2–4 dispatches inside one second — therefore cost one
  request; anything whose state really differs re-arms on the next settled
  pass.
- **Project switch**: the cache parks with the workspace (#2401,
  `wsExtras.docSymbols`) instead of dying with the model `performSwitch`
  discards, and the resumed model installs it back. Coming back to a project
  refeeds pane, breadcrumbs and sticky scopes from memory; only a buffer
  edited past its cached version re-requests.
- **Cursor follow**: the same settled pass hands the active editor's cursor
  line to `Model.Follow`, which highlights the enclosing symbol (last
  containing row in depth-first order; nearest preceding row as fallback) and
  scrolls it into view while the pane is unfocused.

The pane persists in the layout like the VCS/debug tool windows
(`paneIdentity{Kind: "structure"}`) and restores empty; the first settled pass
refills it. It counts as a tool window for `window.hideAllTools`.

`cmd+3` toggles the pane (#1048; JetBrains' Cmd+7 is taken by the comment
toggle, so the free numeric-family chord stands in — the palette is the
delivered fallback); the palette/menu run `structure.toggle` ("Structure").
