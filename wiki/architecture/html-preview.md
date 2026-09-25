---
type: concept
title: HTML Preview
description: "Epic 0530 — rendered reading view of .html/.htm/.xhtml buffers (and .html.gz through the gz viewer) beside the editor, text-mode-browser style. #2739 the UI-free render core internal/htmlrender; #2740 the preview pane (KindHTMLPreview), html.preview on cmd+alt+h (macOS) / cmd+alt+shift+h, debounced re-render, source-mapped line-accurate cursor sync, / search, layout and session restore; #2741 following links (tab/shift+tab/enter/y, click, #anchor/file/browser) and the reverse cursor sync; #2743 local <img> inline over Kitty graphics (Options.ImageBlock, preview.html_images); #2742 tables as bordered grids (content-sized columns, colspan/rowspan, ellipsis truncation); #2744 a minimal CSS subset (display/visibility hiding, font-weight, font-style, text-decoration, color, text-align). Stub — 0530/9 completes it."
resource: internal/htmlpreview
tags: [architecture, html, preview, pane, viewer, gzip, kitty, images, tables, css]
timestamp: 2026-09-25T20:00:00Z
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

`internal/htmlrender` is UI-free: `Render(doc, Options{Width, Palette, ImageBlock})`
parses the bytes tolerantly (`golang.org/x/net/html`), lays the block/inline
flow out word-wrapped at the width, and returns a `Document` — styled lines,
the link, image and anchor indexes, and a two-way **source map** (rendered
line ↔ source offset/line). No I/O, no bubbletea, no shared state.

## Tables (#2742)

A `<table>` renders as a bordered box-drawing grid (`internal/htmlrender/table.go`)
in the palette's border colour, the look of the markdown preview's tables:

```
┌─────────┬─────────┬──────────────┐
│ Quarter │ Revenue │ Notes        │
╞═════════╪═════════╪══════════════╡
│ Q1      │ 100     │ Start        │
└─────────┴─────────┴──────────────┘
```

- **Structure.** `<caption>`s render above the grid; `thead`/`tbody`/`tfoot`
  rows in order, footer rows moved to the end; cells outside a `<tr>` form
  an implicit row; wrappers between rows (a `<form>`) are transparent, and
  stray text is rendered before the grid, as a browser fosters it. Rows of a
  `<thead>` — or, without one, the leading rows made only of `<th>` — are the
  header: bold, with a double rule under them when body rows follow. `<th>`
  cells are bold anywhere. Body rows get a rule between them once any cell
  of the table takes more than one line.
- **Cells.** Each cell is laid out by the ordinary flow in *capture mode* at
  its column's width, so inline styling, paragraphs, lists and `<br>` work
  inside it, and its links, anchors (the cell's and row's `id` included) and
  source offsets are recorded when the grid emits the row. A word wider than
  its column is cut with an ellipsis (`…`) instead of being broken. An image
  in a cell keeps its `[alt]` placeholder. A table nested in a cell is
  flattened into the cell's flow — a row per line, `│` between its cells.
- **Column widths.** A first capture at the table's full width measures each
  cell's widest line (want) and widest unbreakable word (need). All wants
  fit: every column gets its want. Else, if the needs fit, every column gets
  its need and the rest grows columns in proportion to what they still want.
  Else every column gets a fair share: a need at most an even share is met in
  full, the others split the remainder evenly, never below the minimum
  column width (3 cells, less for narrower content). A `colspan` cell counts
  over its columns (its want and need spread evenly); a `rowspan` cell shows
  in its first row and leaves an empty cell below. Spans are capped at
  HTML's limits (1000 columns, 65534 rows; `rowspan=0` reaches the last row).
- **Overflow.** A grid still wider than the pane at the minimum widths is cut
  at the right edge with the `hscroll` overflow glyph `›`, like a long `<pre>`
  line.
- **Source map.** The top border maps to the `<table>` tag, every row line to
  the first word it shows (the row's `<tr>` when it shows none); rules and the
  bottom border take the nearest line's.
- **Cost.** Every cell is laid out twice (measure, place); a 500-row table
  renders in a few milliseconds (`TestLargeTable`, `BenchmarkRenderTable500`).
  The `gridview` building block is not used: it draws interactive data grids
  from strings, while table cells here are laid-out flow with links and a
  source map.

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

## Following links (#2741)

The markdown preview's link model (#2180) carried over, fed by the render
core's own index instead of a scan of the output.

- **Index.** `Document.Links` names each `<a href>` (label, href, first and
  last line); `Document.LinkSpans` places every piece of a label — one per
  rendered line when it wraps — as a byte range of the styled line (the
  selection highlight) and a cell range (the click hit-test).
  `Document.Anchors` maps each `id` and `<a name>` to its rendered line.
- **Keys.** Focused, `tab`/`shift+tab` walk the links in reading order,
  wrapping, scrolling the selected one into view and drawing its label in
  reverse video; `enter` follows the selection, `y` copies its destination,
  `esc` drops it. Only a document with links claims `tab` — a link-free page
  keeps the global focus cycle, and `ctrl+tab` cycles panes either way. The
  status line shows `→ <href>` for the selection, `tab: links` otherwise.
- **Mouse.** A left click on a label selects and follows it; a click
  anywhere else only focuses the pane. The wheel scrolls.
- **Follow rules** (`internal/app/htmlpreviewlinks.go`, from an
  `htmlpreview.LinkMsg`): `#anchor` scrolls the preview to the element
  whose `id`/`name` matches (percent-decoded as a fallback), an unknown one
  toasts; a destination with a scheme other than `file:` (`http(s)`,
  `mailto`, …) goes to the open-in-browser opener; anything else is a local
  path — percent-decoded, query dropped — resolved against the previewed
  page (for a gz buffer, against the archive's directory). An `.html` target
  opens in an editor with a preview of its own beside it, landing on the
  fragment's anchor; a markdown target opens at the fragment's heading;
  other kinds go through the normal open funnel. A missing target toasts.
- **Reverse cursor sync.** `enter` with no link selected emits an
  `htmlpreview.SourceLineMsg` for the rendered line at the sync row (a third
  down the viewport — where the forward sync puts the caret's line, so the
  two round-trip); the root model focuses the editor holding the buffer and
  moves its caret to that line's source line through the source map.

## Inline images (#2743)

A local `<img>` shows as pixels, the way the markdown preview's images do
(#2180), through the same Kitty graphics path (`imgview.PlacedImage`,
Unicode placeholder cells, the root model's reconcile pass).

- **Render hook.** `htmlrender.Options.ImageBlock(img, maxCols)` is asked
  for every `<img>` with the width left beside the current indent. Lines it
  returns replace the `[alt]` placeholder: the image ends the current line
  and each line is one rendered line (under the image's link, inside the
  indent), so the source map, link spans and anchors stay line-accurate
  around the block; `Image.Line`/`Image.Rows` record where it landed. A table
  cell keeps the placeholder — a block would dwarf the grid's columns. The core
  stays I/O-free; decoding is the hook's business.
- **Resolution** (`internal/htmlpreview/images.go`). A src relative to the
  document's file (URL path: query and fragment dropped, escapes decoded),
  an absolute path, or a `file://` URL (empty or `localhost` host) is local.
  Anything with another scheme (`http(s)`, `data:`, …) and the
  scheme-relative `//host/x` is remote and **never fetched**. A local file is
  decoded once (PNG, JPEG, GIF, WebP) and cached by path; failures are not
  cached, so a fixed file shows on the next render.
- **Placement.** The block is `imgview.FitGrid` into the offered width and
  the pane height minus one, then `imgview.PlaceholderGrid`. One file
  referenced twice is one placement drawn at both places. Ids come from the
  pane's own range (90000+), clear of the image pane (9000), the markdown
  preview (30000) and the notebook (60000). A height change re-renders a page
  that draws an image.
- **Fallback.** A remote src, an unsupported or missing file, a terminal
  without Kitty graphics (or support still unknown), and
  `preview.html_images = false` all render the core's `[alt]` —
  `[image: <basename>]` without alt. `<picture>` shows its `<img>` (the
  `<source>`s are skipped); an inline `<svg>` renders `[svg]`.
- **Lifecycle.** `imageSyncCmd` visits `KindHTMLPreview` like
  `KindMarkdown` (gated by `Registry.HTMLPreviewsMinted`): `SetGraphics`
  pushes support in, a decodable image fires the capability probe
  (`HasImages`), `ImageIDs` is the live set, `SyncSeqs` transmits and
  re-transmits after a resize. An id leaving the live set — the image edited
  out, images turned off, the pane closed — is deleted.
  `releaseWorkspaceImages` deletes the placements on park/teardown and
  `ResetImages` makes the resume re-send them.
- **Setting.** `preview.html_images` ("Render images in HTML preview", on by
  default) on the Settings UI's Markdown Preview page; the registry applies
  it on every construction path and `Reconfigure` pushes a change into open
  panes (`pane.applyHTMLPreviewCfg`).

## CSS subset (#2744)

The reading view applies a deliberately tiny CSS subset
(`internal/htmlrender/css.go`) so hidden content stays hidden and simple
emphasis survives. There is no layout engine: box model, fonts, positioning,
backgrounds and every other property are ignored.

- **Sources.** Inline `style=""` attributes and `<style>` blocks (those with a
  `media` attribute only when it names `screen` or `all`). External
  stylesheets are never fetched; `@media`, `@import`, `@font-face` and every
  other at-rule are skipped.
- **Supported properties.**

  | Property | Values | Effect |
  |---|---|---|
  | `display` | `none` (any other value reverts it) | element not rendered |
  | `visibility` | `hidden`, `collapse` (`visible` reverts) | element not rendered |
  | `font-weight` | `bold`, `bolder`, number ≥ 600 / `normal`, `lighter`, < 600 | bold on / off |
  | `font-style` | `italic`, `oblique` / `normal` | italic on / off |
  | `text-decoration`, `text-decoration-line` | `underline`, `line-through`, `none` | underline / strike added; `none` clears both (a link's underline too) |
  | `color` | named, `#rgb[a]`, `#rrggbb[aa]`, `rgb()`/`rgba()` | text colour |
  | `text-align` | `center`, `right`/`end` (`left`/`start`/`justify` reset) | block lines centred / flush right |

- **Colour.** CSS colours are emitted as true colour like the theme's own;
  bubbletea's screen downsamples every colour to the nearest palette entry
  on a 256- or 16-colour terminal. Fully transparent colours,
  `currentcolor`, `inherit`, `hsl()` and `var()` are ignored.
- **Cascade.** Rules sort by cascadia's specificity, then source order;
  inline style wins over any rule; `!important` declarations win over normal
  ones (inline important over stylesheet important). An invalid value leaves
  the property as it was. The look inherits along the render walk and is
  applied after the tag's own, so `b { font-weight: normal }` or
  `a { color: … }` override the built-in styling. `text-align` counts only
  on block elements and inherits into nested blocks and table cells (padded
  within the cell's column; the measuring pass ignores it).
- **Malformed CSS.** A stylesheet with unbalanced braces or an unterminated
  comment or string is ignored as a whole and the document still renders; a
  rule whose selector cascadia rejects (a pseudo-element) is dropped alone;
  dynamic pseudo-classes (`:hover`, `:focus`) never match.
- **Cost.** A document without CSS builds no cascade. Otherwise selectors are
  indexed by their subject's id, class or tag and skipped when an ancestor
  compound names a key no ancestor carries — a 256 KB page behind a
  3000-selector sheet renders in about 36 ms, 11 ms of it without the sheet
  (`BenchmarkRenderStyled`).

## Still to come

Bounded async rendering (0530/7), the browser screenshot mode (0530/8) and
the full concept doc (0530/9).
