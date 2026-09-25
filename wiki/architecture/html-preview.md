---
type: concept
title: HTML Preview
description: "Epic 0530 — rendered reading view of .html/.htm/.xhtml buffers (and .html.gz through the gz viewer) beside the editor, text-mode-browser style. #2739 the UI-free render core internal/htmlrender; #2740 the preview pane (KindHTMLPreview), html.preview on cmd+alt+h (macOS) / cmd+alt+shift+h, debounced re-render, source-mapped line-accurate cursor sync, / search, layout and session restore. Stub — 0530/9 completes it."
resource: internal/htmlpreview
tags: [architecture, html, preview, pane, viewer, gzip]
timestamp: 2026-09-25T12:00:00Z
---

# HTML Preview (Epic 0530)

An HTML document opened in IKE can show a **rendered reading view** next to
its source, the way a text-mode browser (w3m, lynx) shows a page: headings,
paragraphs, lists, links, code blocks — theme-aware, scrollable, searchable,
without leaving the terminal. It is the sibling of the
[markdown preview](./markdown-preview.md) and keeps its seams.

This page is a stub written with the pane (#2740); the complete concept doc
is 0530/9 (#2747). Until then the render core is described by the package
doc comment of `internal/htmlrender`.

## Render core (#2739)

`internal/htmlrender` is UI-free: `Render(doc, Options{Width, Palette})`
parses the bytes tolerantly (`golang.org/x/net/html`), lays the block/inline
flow out word-wrapped at the width, and returns a `Document` — styled lines,
the link, image and anchor indexes, and a two-way **source map** (rendered
line ↔ source offset/line). No I/O, no bubbletea, no shared state.

## The pane (#2740)

`internal/htmlpreview.Model` wraps the core as a pane of kind
`pane.KindHTMLPreview`, keyed `htmlpreview`, `htmlpreview:2`, … by the
registry's minting. It is a sibling package rather than a renderer switch
inside `preview.Model`: the markdown model is built around glamour, heading
anchors, diagram fences and markdown image substitution, none of which
apply, while the HTML pane's later features (tables, CSS, the off-loop
render, the browser screenshot mode) would all have had to branch inside
it.

- **Opening.** `html.preview` ("HTML Preview") — `cmd+alt+h` on macOS,
  `cmd+alt+shift+h` everywhere (`ctrl+alt+shift+h` off macOS; the fold of
  `cmd+alt+h` is `ctrl+alt+h`, which `lsp.callHierarchy` owns), the palette
  (language-gated to `html` buffers) and the editor tab context menu on an
  HTML tab. The semantics are `markdown.preview`'s: the first press splits
  the active editor's leaf to the right with a preview bound to the buffer
  path, the editor keeps focus; a second press focuses the existing preview
  (dedicated pane or content tab) instead of duplicating it; the pane closes
  like any pane. A buffer that is not `.html`/`.htm`/`.xhtml` (optionally
  `.gz`) is a no-op with a toast.
- **`.html.gz`.** The gz viewer (#1763) opens a compressed page as a
  read-only buffer at `<file>.html.gz!<inner>.html` holding the decompressed
  text under the inner name's language; `htmlpreview.IsHTMLPath` accepts
  that path, so the preview renders the buffer like a plain page. The gz
  viewer's decompressed-byte cap is the size guard. Its watcher refresh
  pushes the new text into a bound preview, since a read-only buffer never
  emits the change sync.
- **Live updates.** Every `editor.SyncMsg` for the path hands the
  originating editor's text to `SetSource`, which arms a
  `preview.Debounce` (200ms) tick carrying a sequence number; only the
  newest `htmlpreview.RenderTickMsg` renders. Open and restore render
  synchronously via `SetSourceImmediate`.
- **Single render seam.** Every render goes through `htmlpreview.Render`,
  synchronous on the update loop for now; the bounded, off-loop render of
  0530/7 (#2745) replaces that one function.
- **Cursor sync.** The editor emitter's `preview.CursorMsg` reaches HTML
  previews too (the `previewBound` gate of #2540 counts them). The caret's
  source line maps through `Document.LineForSourceLine` — content starting
  on that line wins, markup-only lines take the content before them — and
  the mapped line is aimed a third down the viewport. Line-accurate, unlike
  the markdown preview's heading interpolation. A width change or theme
  switch re-renders and re-applies the sync.
- **Scrolling and search.** Focused: `j/k`, arrows, `pgup/pgdown`,
  `ctrl+u/ctrl+d`, `g/G`; the wheel scrolls it unfocused. `/` (and the find
  chord) opens the shared `ui.LineSearch` prompt over the ANSI-stripped
  rendered lines — `n`/`N`, `cmd+g` stepping, the match landing a third
  down; the pane is `pane.Searchable`.
- **Context and chrome.** The pane advertises the shared `preview` context
  id; title band `HTML PREVIEW <file>`, status line with the page's
  `<title>`, the `◫` content-tab glyph.
- **Persistence.** Layout persistence saves `{kind: "htmlpreview", path}` for
  a dedicated pane and a content tab alike; restore rebuilds the pane and
  re-reads the source — decompressing it for a gz buffer path — and a
  vanished file restores an empty preview. Named saved layouts treat it as
  an anonymous content slot like every viewer (`pane.KindViewer`); it is
  tabbable (`pane.KindTabbable`).

## Still to come

Links and navigation (0530/3), tables (0530/4), inline images (0530/5), the
minimal CSS subset (0530/6), bounded async rendering (0530/7), the browser
screenshot mode (0530/8) and the full concept doc (0530/9).
