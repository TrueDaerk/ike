---
type: concept
title: DOM Inspector
description: "DOM inspector tool pane (#1929) — the focused HTML buffer's parsed DOM tree with a CSS selector tester: tokenizer-based tolerant parse with source offsets, async off the UI loop, cursor auto-follow, selector matches highlighted in tree and editor, copy shortest-unique selector / outer HTML."
resource: internal/domview
tags: [architecture, html, dom, tool-window]
timestamp: 2026-08-18T00:00:00Z
---

# DOM Inspector (#1929)

A singleton tool pane for writing HTML parsers and extractors against fixture
files: it shows the parsed DOM tree of the focused HTML buffer
(`.html`/`.htm`/`.xhtml`), tests CSS selectors against it live, and maps every
node back to its exact source position. `dom.toggle` (palette: "DOM
Inspector", Tools menu) opens it at the right of the active editor with the
Structure view's toggle state machine — open → focus → return focus.

## The parser (internal/htmldom)

`htmldom.Parse` builds real `*html.Node` values — so
`github.com/andybalholm/cascadia` selectors match them directly — but not via
`html.Parse`: the tree is driven off the **`x/net/html` tokenizer** by a small
tolerant stack machine, because the tokenizer is the only layer that exposes
byte offsets, and because fixture files are messy. Consequences, all
deliberate:

- Every node carries a source `Span{Start, End, OpenEnd}` (byte offsets;
  `OpenEnd` closes the opening tag). `OuterHTML` is a verbatim source slice,
  never a re-serialization.
- Stray end tags are dropped; unclosed elements end where their ancestor
  closes or at EOF; a modest `autoClose` table covers the common omitted end
  tags (`<p>`, `<li>`, table rows/cells, `<option>`). No implied
  `<html>/<head>/<body>` nodes — the tree mirrors the source.
- `Position`/`Offset` translate byte offsets ↔ 0-based (line, rune column)
  editor coordinates; `NodeAt` finds the deepest node at an offset (the
  follow's other half).
- `SelectorPath` builds the shortest-unique CSS selector for a node: a
  document-unique `#id` anchors immediately, else `tag.classes` when
  sibling-unique, else `tag:nth-child(k)`, extending the ancestor chain until
  the whole selector re-finds exactly that node (verified by matching).

## The pane (internal/domview)

The Structure panel's UX skeleton with archview's folds: collapsible element
rows (`▾`/`▸`, `space`/`h`/`l`), text nodes as truncated quoted excerpts,
comments and doctype shown faint. `enter`/double-click emits `NavigateMsg` →
the root model's `openPathAt` funnel (nav history records the jump);
`FollowCursor` highlights the deepest node enclosing the editor cursor
without unfolding (nearest visible ancestor inside a fold), scrolling only
while the pane is unfocused.

The selector line (`/`, or click it) is a `ui.EditKey` single-line input that
re-matches on every keystroke: match rows highlight, the header shows
`current/total matches`, a cascadia compile error renders as `✗ message`
instead of matches. `n`/`N` step the current match (wrapping), select its row
and jump the editor. `c` copies the node's `SelectorPath`, `Y` its outer
HTML — both via `CopyMsg`, which the root model puts on the system clipboard
with a toast. `cmd+c` aliases `c`, and it is the one key the selector line
does *not* swallow (#2062): the node the copy acts on stays highlighted
behind the prompt, while `c`/`Y` remain plain selector input there. `ctrl+c`
is deliberately not an alias — the tree has no text selection to protect, so
that chord keeps its global quit meaning.

## App wiring (internal/app/dom_panel.go)

`domSyncCmd` runs once per settled Update pass, like `structureSyncCmd`:

- **Parse funnel**: when the pane's shown `(path, docVersion)` differs from
  the active editor's, it captures `ed.Text()` and returns a command whose
  goroutine runs `htmldom.Parse` — multi-MB fixtures never stall the UI. The
  `domReqPath`/`domReqVersion` pair dedups requests; `domParsedMsg` deliveries
  tagged with an older version are dropped. A non-HTML buffer sets the pane's
  explanatory notice instead. Edits reparse naturally: every buffer change
  bumps `editor.DocVersion`.
- **Highlight routing**: whenever the pane's `MatchesRev` moved, the matches'
  opening-tag ranges route as `editor.DOMMatchesMsg` to every editor showing
  the file (`routeToEditor`), clearing a previously highlighted file on
  buffer switch or pane close. The editor paints them like search matches
  (`SelectionMuted` background, the current match underlined) in
  `internal/editor/dommatches.go`.

The pane persists in the layout under key `dom` and restores empty (the first
sync refills it), participates in `window.hideAllTools`, and is assignable in
`[tools.layout]` slots like the other singleton tool windows.
