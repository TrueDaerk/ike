---
type: concept
title: HTML Preview
description: "Epic 0530 — a rendered reading view of .html/.htm/.xhtml buffers (and .html.gz through the gz viewer), in the file's own tab (Source/Preview view modes, #2766) or beside the editor, text-mode-browser style: a UI-free render core (internal/htmlrender) with tables, a minimal CSS subset and a two-way source map; the pane (KindHTMLPreview, html.preview) with link following, inline Kitty images, off-loop generation-cancelled rendering under a size budget, and an optional headless-browser screenshot mode."
resource: internal/htmlpreview
tags: [architecture, html, preview, pane, view-modes, viewer, gzip, kitty, images, tables, css, async, performance, browser, screenshot, security]
timestamp: 2026-09-26T00:30:00Z
---

# HTML Preview (Epic 0530)

An HTML document opened in IKE can show a **rendered reading view** next to
its source, the way a text-mode browser (w3m, lynx) shows a page: headings,
paragraphs, lists, links, tables, images — theme-aware, scrollable,
searchable, without leaving the terminal. It is the sibling of the
[markdown preview](./markdown-preview.md) and keeps its seams: same split
semantics, same debounced re-render, same [image preview](./image-preview.md)
Kitty-graphics path, its own render core instead of glamour. A `.html.gz`
opens through the [gz viewer](./gz-viewer.md) and previews like a plain page;
writing HTML parsers or extractors against a page's structure is the job of
the [DOM inspector](./dom-inspector.md) tool pane instead, not this preview.
Splitting, moving and resizing the preview pane itself follows the general
[pane layout](./pane-layout.md) model.

The epic shipped in nine parts: the render core (#2739), the pane (#2740),
link following (#2741), tables (#2742), inline images (#2743), a CSS subset
(#2744), off-loop rendering (#2745), browser mode (#2746) and this
consolidated doc (#2747). A follow-up (#2766) made HTML files open rendered
in their own tab, with Source/Preview [view modes](#view-modes-2766).

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
  newest `htmlpreview.RenderTickMsg` renders. Open and restore skip the
  debounce via `SetSourceImmediate`.
- **Off the loop.** No render runs on the update loop; see
  [Off-loop render](#off-loop-render-2745).
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

## Off-loop render (#2745)

A multi-MB report must not freeze the IDE, so the render never runs on the
update loop and never reads more than a budget of the page — the Tree-sitter
`docVersion` and project-search pattern.

- **Owe, then dispatch.** Every input change — source (the debounce tick,
  `SetSourceImmediate` on open and restore), width, height with an image on
  the page, theme, Kitty support, `preview.html_images`, the budget — only
  marks the pane as owing a render. `RenderCmd` dispatches it as a `tea.Cmd`
  tagged with the next generation over a snapshot of those inputs. The
  debounce tick returns it directly; everything else is picked up by
  `htmlPreviewRenderCmd` on Update's settled pass, right after the Kitty
  reconcile (which may itself owe one), since resize, theme and setting call
  sites cannot return a Cmd.
- **Generation and cancellation.** The Cmd runs
  `htmlrender.RenderContext` and returns an `htmlpreview.RenderedMsg{Key,
  Gen, Took}`; the pane adopts it only when `Gen` is still the newest
  dispatched. Generations are minted process-wide, so a result of a parked
  workspace's pane still queued never matches the rebuilt pane that took
  over its key. A newer dispatch cancels the older render's context, as do
  closing the pane (`Instance.releaseContent` → `Model.Close`) and closing
  the last view of the source buffer (`drainClosedFileViews` →
  `CancelRender`, which keeps the shown page). The render core polls the
  context every 256 nodes and a cancelled render returns no message — no
  wake. A workspace that parks interrupts its previews' renders and owes
  them again, so the resume renders the newest source.
- **Budget.** `htmlrender.Options.Budget` cuts the source at
  `preview.html_render_budget_kb` KiB (default 2048, 64–65536) before
  parsing — backing off to a character boundary and off a half-open tag —
  and ends the document with a blank line and a faint
  `… truncated after N KB` line mapped to the cut; `Document.Truncated`
  reports it. The focused pane's status line says `truncated at N KB`. The
  editor still holds the whole file.
- **While pending.** The pane keeps drawing the previous document with a
  static ` rendering… ` notice right-aligned on its first row (no spinner
  tick: a pending render must not add wakes), and the status line shows
  `rendering…`. A followed link's `#fragment` (`LandOnAnchor`) waits for the
  document to land.
- **Images off the loop.** The `ImageBlock` hook runs on the render
  goroutine against an `imageRun`: a copy of the decoded-image cache (pixels
  and ids never change), its own decodes and a private grid map. The loop
  adopts cache, placements and grids with the document, so the reconcile
  pass's placement state is only ever touched on the loop; decoding a new
  image no longer blocks it either.
- **Cost.** A 5 MB page opens its preview in a ~1.5 ms update pass; the
  off-loop render of its first 2 MB takes ~100 ms
  (`TestBudgetBoundsLargePage`, `BenchmarkRenderBudget`). The performance
  HUD books each delivered render under `<pane key> render` (see
  [Performance](./performance.md#the-performance-hud-internalperfhud-1999)).
- **Setting.** `preview.html_render_budget_kb` ("HTML preview render budget
  (KB)") on the Settings UI's Markdown Preview page; out-of-range typed
  values clamp to the range, and `pane.applyHTMLPreviewCfg` pushes a change
  into open panes, which re-render.

## Browser screenshot mode (#2746)

The text rendering is a reading view. For CSS- and JS-accurate output the
pane has an optional **browser mode**: a headless Chrome, Chromium or Edge
screenshots the page and the pane shows the PNG — the mermaid PNG renderer's
pattern (`internal/preview/diagrams.go`, #2421): an external binary from the
settings, `exec.LookPath`, off the loop, cached, with a fallback
(`internal/htmlpreview/browser.go`).

- **Toggle.** `b` in the focused pane, or `html.preview.browser` ("HTML
  preview: render in browser", palette, language-gated to `html` like
  `html.preview`; it switches the focused HTML preview, else the one bound to
  the active editor's buffer). It ships keybind-less with a pane-key ledger
  entry: the chord the spec suggested, `cmd+alt+shift+h`, is `html.preview`'s.
  Text mode is always the default; the mode is remembered per pane in the
  layout state (`paneIdentity.Mode = "browser"`, content tabs included),
  written on the settled pass after every flip (`TakeModeChange`), and a
  restored pane in browser mode takes its screenshot on the first render
  pass.
- **Render.** The current buffer text (unbudgeted — the timeout bounds the
  browser) is written as `page.html` into a fresh `shot-*` directory under
  the scratch area's hidden `.html-preview/` folder (`$IKE_CONFIG_DIR` or
  `~/.ike/scratches`; the scratch listing skips directories), with a
  `<base href>` naming the document's own directory injected after `<head>`
  so its relative stylesheets, scripts and images still resolve. The browser
  runs as

  ```
  <browser> --headless=new --disable-gpu --no-first-run --no-default-browser-check
            --user-data-dir=<dir>/profile --hide-scrollbars --screenshot=<dir>/shot.png
            --window-size=<w>,4096 file://<dir>/page.html
  ```

  with `w` the pane width × 16 px (800–1920). The PNG is decoded on the
  Cmd goroutine, the page background below the last content row is trimmed
  (a 16 px margin stays), and the whole directory — page copy, profile, PNG
  — is removed before the Cmd returns, on success, error, timeout and
  cancellation alike.
- **Display.** The screenshot becomes an `imgview.Model`
  (`imgview.NewFromImage`) opened zoomed to the pane width at the top
  (`ZoomWidth`), so the page is one tall image the user pans: `j/k`, arrows,
  `ctrl+d/ctrl+u` and the wheel pan, `h/l` pan sideways, `+`/`-` zoom,
  `0` fits the whole page — the image pane's keys (#2688). `r` re-renders
  and keeps the zoom and pan (`SetViewState`). The footer names
  `<file> (browser)`, the status line shows `browser`, `screenshot…` while
  one is taken, `stale — r re-renders` after an edit, and the zoom level.
  While the screenshot shows, the pane's Kitty lifecycle (`ImageIDs`,
  `SyncSeqs`, `TransmittedIDs`, `ResetImages`) is the screenshot's one
  placement; switching mode forgets the sent state of whichever side leaves,
  so it transmits again when it comes back. Links, `tab` and search belong to
  text mode.
- **Async and cache.** The screenshot runs as a `tea.Cmd` tagged with a
  generation from the render counter; `htmlpreview.ShotMsg` lands only when
  still the newest, and leaving the mode or closing the pane cancels it (the
  context kills the browser's whole process group); a workspace that parks
  interrupts it and owes it to the resume, like the text render. A
  screenshot is cached
  under the hash of the document text, the browser binary and the pixel
  width: toggling back with an unchanged key shows the cached shot without
  running the browser. Edits never re-run the browser on their own (a
  browser launch costs a second or more); `r` does, unconditionally. Until
  the first screenshot lands the pane keeps drawing the text rendering under
  a ` screenshot… ` notice. The performance HUD books it as
  `<pane key> screenshot`.
- **Fallbacks.** No browser resolves → the toggle stays in text mode and
  toasts a warning naming `preview.html_browser`; a render error (the last
  non-empty line of the browser's stderr) or the timeout → a toast and back
  to text mode (`htmlpreview.NoticeMsg`). Browser mode is never entered
  implicitly.
- **Browser lookup.** `preview.html_browser` when set (`~` expands, a bare
  name resolves on PATH; a configured browser that does not resolve is
  reported, never replaced by auto-detection); else the first of
  `google-chrome`, `google-chrome-stable`, `chromium`, `chromium-browser`,
  `chrome`, `microsoft-edge`, `microsoft-edge-stable`, `msedge`,
  `chrome-headless-shell` on PATH, then on macOS the Chrome, Chromium and Edge
  app bundles under `/Applications`. Chrome's `chrome-headless-shell` (the
  Playwright/Chrome-for-Testing download) is the lightest option; a full
  Chrome needs a window-server session on macOS and otherwise runs into the
  timeout.
- **Security.** The page runs with JavaScript in a real browser engine —
  that is the point of the mode — so it is sandboxed as far as the command
  line allows: a fresh `--user-data-dir` per render (no cookies, logins,
  extensions or history of the user's own profile, deleted afterwards),
  `--no-first-run --no-default-browser-check` (no first-run UI, no
  default-browser prompt), `--disable-gpu` (no GPU process), headless (no
  window), the process group killed on timeout and after exit so no helper
  outlives the render, and the temp files readable only by the user
  (`0700`/`0600`). Unlike text mode, which never fetches anything, the
  browser loads whatever the page references — remote stylesheets, scripts,
  images and requests its scripts make. Use text mode for untrusted pages.
- **Settings.** `preview.html_browser` ("HTML preview browser", Path) and
  `preview.html_browser_timeout_s` ("HTML preview browser timeout (s)",
  Int, default 20, 1–300) on the Settings UI's Markdown Preview page, next to
  the diagram renderer they mirror; `pane.applyHTMLPreviewCfg` threads both
  into every open pane (`Model.SetBrowser`). See
  [Settings UI](./settings-ui.md).

## View modes (#2766)

An HTML file opens **rendered, in its own tab** — the JetBrains experience —
rather than as source with a chord to get the preview. The tab has two
views, **Source** (the editor) and **Preview** (the rendered page), and a
one-row button strip in the **bottom-left corner of the pane body**:

```
│ Some bold text and a link.                              │
│                                                         │
│ [Source] [Preview] [Browser]                            │
╰─────────────────────────────────────────────────────────╯
```

- **One tab, one buffer.** The Preview view is the pane's
  `htmlpreview.Model` — live updates, cursor sync, links, images, browser
  mode, off-loop render exactly as above — held as a nested
  `KindHTMLPreview` instance *beside* the editor in the same tab slot
  (`internal/pane/tabview.go`), not a content tab of its own. The app mints
  it through the registry on the first switch (`ensureTabView`) and the tab
  attaches it (`Instance.AttachTabView`). Switching keeps the buffer, undo
  history, LSP session, the pane's tab identity and each view's own scroll;
  closing the tab closes both. While the page shows, the tab answers the
  body-level questions like a preview content tab (`ActiveContent`,
  `ContextID` = `preview`, `Searchable`, key routing, mouse, title band
  `HTML PREVIEW <file>`, status line), while `Editor()`, `TabPath`, the
  dirty sweeps, save and persistence keep seeing the editor. Keys go to the
  preview — nothing typed in Preview view reaches the hidden buffer — and the
  editor's own async results still reach it. The breadcrumbs row hides.
- **Hidden views park.** Leaving Preview interrupts the preview's render or
  screenshot in flight (owed again, `Model.Interrupt`) and forgets its sent
  Kitty images (`ResetImages`), and the app's content walks
  (`forEachContent`) skip a hidden view — it neither renders nor receives the
  buffer sync while the editor shows. Showing it again runs
  `Model.Resync(text, caretLine)`: a changed text, or a debounce tick the
  hidden pane never received, owes a render; a moved caret re-syncs the
  scroll; with neither, the page is exactly where it was left.
- **The strip.** Drawn by `ui.Segmented` (see
  [Shared Building Blocks](./shared-building-blocks.md#structured-views)):
  `[Source] [Preview]`, the current one in the palette's accent style, plus
  `[Browser]` in Preview view, lit while browser mode is on and toggling it
  exactly like `b`. It takes the pane body's bottom row of an HTML document
  tab (loaded or still deferred) in either view — both bodies are sized one
  row shorter, the text is padded so the strip sits on the bottom row — and
  hides below `pane.ViewStripMinHeight` (6 rows) rather than steal a text
  row from a small pane. A tab that turns out to be HTML after it was sized
  (a file loaded into a scratch tab) re-sizes on its next View or Update.
  A left click on a button acts (`Instance.ViewStripAt` → `viewStripClick`,
  ahead of the body's own click routing); any other press on the row is
  swallowed. Hover is not needed.
- **Opening.** An open of an `.html`/`.htm`/`.xhtml` file — explorer, palette,
  go-to-file, a CLI path, a followed `.html` link — and an `.html.gz` through
  the gz viewer lands a *new* tab in the view `preview.html_open_mode` names
  (`preview` by default, `source`); a tab already open keeps its view. "Open
  File As… → Text editor" stays in Source. A navigation that targets a
  source position — go to definition, a search result, the Problems pane, a
  `file:line` CLI target or deep link, go to line, the preview's reverse
  cursor sync (`enter` on no link) — always lands in **Source**, switching a
  tab that shows the page back, so a jump never ends on a rendered page
  without the caret (`openPathAtWith`: a line ≥ 0 is a jump; a line-less
  target is a plain open). A followed `.html` link that opened in Preview
  view lands on its `#fragment` there instead of splitting a preview beside
  it.
- **Commands.** `html.view.toggle` ("HTML: toggle Source/Preview",
  `cmd+alt+shift+v` / `ctrl+alt+shift+v` in the editor and preview contexts
  — the spec's `cmd+alt+shift+p` is `scratch.promote`'s, see
  [Keybindings](./keybindings.md#html-toggle-sourcepreview-2766)), plus the
  palette entries `html.view.source` / `html.view.preview` ("HTML: show
  Source" / "HTML: show Preview", keybind-less as flavours of the toggle).
  All three are language-gated to `html` and act on the focused pane's
  active tab (else the active editor's); a non-HTML tab toasts. The tab
  context menu of an HTML tab offers "Toggle Source/Preview" next to "HTML
  Preview".
- **With `html.preview`.** The split keeps its meaning: it opens a preview
  pane beside the tab even while the tab shows its own Preview view (its
  dedupe skips tab views, `Instance.IsTabView`). Two renderings of one page
  side by side are allowed but pointless; use the split with the tab in
  Source view for the source-beside-page layout.
- **Persistence.** A document tab's view is part of its layout identity:
  `paneIdentity.Views` lists `{index, mode}` for tabs not in Source view,
  `mode` `preview` or `browser` (the preview in browser screenshot mode),
  indexes into `Tabs` like `Pinned`. Restore attaches the preview right away,
  rendered from the file on disk even while the tab is still deferred
  (#2177) — the restored content-tab preview's path — and older builds
  ignore the key and restore Source. A named saved layout carries no tab
  lists; applying one moves the live editor pane, tabs and views included,
  into its slot, so the view survives it the same way.

## Keybinds and settings

| Command | Default keybind | Where |
|---|---|---|
| `html.preview` | `cmd+alt+h` (macOS) / `cmd+alt+shift+h` (`ctrl+alt+shift+h` off macOS) | palette, editor tab context menu |
| `html.preview.browser` | keybind-less (ledger entry: the spec's suggested chord is `html.preview`'s) | palette |
| `html.view.toggle` | `cmd+alt+shift+v` / `ctrl+alt+shift+v` (Editor and Preview contexts) | the tab's `[Source] [Preview]` strip, palette, tab context menu |
| `html.view.source`, `html.view.preview` | keybind-less (ledger: flavours of the toggle) | palette |

| Setting | Default | Page |
|---|---|---|
| `preview.html_open_mode` | `preview` (`preview` / `source`) | Settings UI → Markdown Preview |
| `preview.html_images` | on | Settings UI → Markdown Preview |
| `preview.html_render_budget_kb` | 2048 (64–65536) | Settings UI → Markdown Preview |
| `preview.html_browser` | auto-detected | Settings UI → Markdown Preview |
| `preview.html_browser_timeout_s` | 20 (1–300) | Settings UI → Markdown Preview |

See [Settings UI](./settings-ui.md) for the form itself.

## Related

- [Markdown Preview](./markdown-preview.md) — the sibling preview this pane's
  pane semantics, debounce and cursor sync are modeled on.
- [Image Preview](./image-preview.md) — the Kitty graphics path inline
  `<img>`s and the browser screenshot both reuse.
- [Gz Viewer](./gz-viewer.md) — how `.html.gz` reaches this preview as a
  decompressed buffer.
- [DOM Inspector](./dom-inspector.md) — the tool pane for exploring an HTML
  buffer's parsed structure and testing CSS selectors, rather than reading it.
- [Pane Layout & Drag](./pane-layout.md) — the split/resize/persistence model
  the preview pane lives in.
