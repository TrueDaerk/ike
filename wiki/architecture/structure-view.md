---
type: concept
title: Structure View
description: "Structure tool pane (#1025) — the focused buffer's symbol tree from LSP textDocument/documentSymbol: singleton right-split pane, capability-gated request through the manager, cursor auto-follow, enter/double-click navigates via the open funnel."
resource: internal/structpanel
tags: [architecture, lsp, structure, tool-window]
timestamp: 2026-07-23T00:00:00Z
---

# Structure View (#1025)

JetBrains' Structure tool window scaled to the terminal: a singleton tool
pane showing the focused buffer's symbol tree, backed by LSP
`textDocument/documentSymbol`. This is the MVP slice of #31 — breadcrumbs and
a Tree-sitter fallback for server-less languages remain follow-ups tracked
there.

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

- **Pane open** resets the request dedup and refreshes.
- **Focused buffer change**: `structureSyncCmd` runs from the Update wrapper
  once per settled pass; when the shown tree belongs to another file it issues
  one `lsp.documentSymbols` run, deduplicated per path (`structReqPath`) so a
  provider-less file never re-requests every pass.
- **Save**: the `todoSavedMsg` handler sets `structForce`, which bypasses the
  dedup for the unchanged path.
- **Cursor follow**: the same settled pass hands the active editor's cursor
  line to `Model.Follow`, which highlights the enclosing symbol (last
  containing row in depth-first order; nearest preceding row as fallback) and
  scrolls it into view while the pane is unfocused.

The pane persists in the layout like the VCS/debug tool windows
(`paneIdentity{Kind: "structure"}`) and restores empty; the first settled pass
refills it. It counts as a tool window for `window.hideAllTools`.

No default chord is bound (JetBrains' Cmd+7 is taken by the comment toggle);
the palette/menu run `structure.toggle` ("Structure").
