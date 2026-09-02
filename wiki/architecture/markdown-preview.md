---
type: concept
title: Markdown Preview
description: "#62 — rendered live preview pane for markdown buffers: glamour-rendered ANSI split beside the editor, debounced re-render off the editor change seam, heading-anchored cursor scroll sync, theme-aware styling, layout persistence; #2180 — followable links and inline Kitty-rendered local images."
resource: internal/preview
tags: [architecture, markdown, preview, pane, glamour, links, kitty]
timestamp: 2026-09-03T00:00:00Z
---

# Markdown Preview (#62)

`internal/preview` renders a markdown buffer to styled terminal output as a
live pane beside its editor. IKE documents itself in markdown (this wiki), so
the feature is self-hosting.

## Opening and closing

`markdown.preview` (palette, `cmd+alt+m` in the editor)
splits the active editor's leaf to the right with a preview pane
bound to the buffer's path. The editor keeps focus — the preview follows the
typing, it does not receive it. Invoking the command again while a preview for
the buffer exists focuses that pane instead of duplicating it; a non-markdown
buffer (anything but `.md`/`.markdown`/`.mdown`/`.mkd`) is a no-op with a
toast. The pane closes like any pane (`ctrl+w`, pane close paths); no teardown
is needed.

## Pane integration

The preview is a fourth `pane.Kind` (`KindMarkdown`) wrapping a
`preview.Model`, keyed `"preview"`, `"preview:2"`, … by the registry's
monotonic minting (mirroring terminals). It advertises the `"preview"` context
id. Layout persistence saves `{kind: "markdown", path}`; restore rebuilds the
pane and re-reads the file from disk (live re-binding to an editor buffer
resumes with the first change event; a vanished file restores empty rather
than breaking the layout).

## In-pane search (#2409)

`/` — and the shared find chord `cmd+f` / `ctrl+f` — opens a one-line prompt on
the pane's last row (`internal/preview/search.go`): the slash prefix, the query
with its text cursor, and a `i/n` match counter (or `no matches`). `enter`
applies, `esc` abandons the search, and `n`/`N` walk the matches, wrapping at
both ends.

The match runs over the **plain text** of each rendered line — the styling
glamour wrapped around it is stripped first (`ansi.Strip`), so a word split by
a colour escape still matches what the reader sees. The preview is read-only
and has no cursor, so a match is expressed as a scroll: the matching line comes
to rest a third down the viewport, the landing the diff viewer uses. The prompt
costs one row of the document while it is up (`viewHeight`).

`OpenSearch` is the pane's `pane.Searchable` implementation, which is how the
Global `search.open` command reaches the prompt (see
[Keybindings](./keybindings.md)).

`NextMatch` / `PrevMatch` complete the capability (#2410): `cmd+g` /
`cmd+shift+g` do what `n`/`N` do, but while the prompt still holds the
keyboard, where those letters are query text. The counter marks the wrapped
step — `1/12 (wrapped)` — and a query edit drops the marker.

## Live updates (debounced)

Edits reach the preview through the existing shared-document seam: the editor
emitter broadcasts `editor.SyncMsg` on every `EventChange`/`EventSave`, and the
root model's SyncMsg handler pushes the originating editor's text into every
preview bound to the path. `preview.Model.SetSource` stores the text and arms a
200ms `tea.Tick` carrying a sequence number; only the newest tick renders
(`RenderTickMsg`), so a typing burst renders once. Open/restore render
synchronously via `SetSourceImmediate`.

## Rendering

Rendering goes through `charm.land/glamour/v2` with `WithWordWrap` bound to
the pane interior width — a resize re-renders. The style config is picked off
`theme.Palette.Dark` (glamour's stock dark/light styles) with heading and link
colors mapped onto the palette's `Accent`/`Info` slots, so the preview follows
the IDE theme, live on theme switch (`SetPalette` re-renders). Code blocks get
glamour's chroma highlighting. Wrapped **list items** get a hanging indent
(`ui.HangingIndent`, #2105): glamour word-wraps a list item's continuation
lines back under its bullet, because its only indentation knobs are block
level and shift the marker along with the text, so the rendered output is
post-processed — each item's lines are re-joined and re-wrapped at the width
its marker leaves, per nesting level.

## Following links (#2180)

The rendered document is not inert. `tab` / `shift+tab` walk its links in
reading order (wrapping), scrolling the chosen one into view and marking its
label in reverse video; the status line shows the selected destination.
`enter` follows it, `y` copies the raw destination to the clipboard. A
preview with no links leaves `tab` its global focus-cycling meaning; `ctrl+tab`
cycles panes either way.

The **index** comes out of the rendering itself (`links.go`): glamour wraps
every link label and printed URL in an OSC 8 hyperlink sequence carrying the
raw markdown destination, so a link's row and byte span are exact and cannot
drift from what the pane shows. The two halves of one link (label plus printed
URL) collapse into a single entry. In-document `#anchor` links are the one
exception — glamour renders those as bare styled text — and are recovered from
the source, located by label, starting from where the scroll-sync mapping puts
their source line.

The preview knows *where* a link points and nothing else; `preview.LinkMsg`
hands the destination to the root model, which owns the policy
(`internal/app/previewlinks.go`):

- **`#anchor`** scrolls the preview itself to that heading (GitHub-style
  slugs); an unresolvable anchor toasts.
- **An absolute URL** (anything carrying a scheme) goes to the platform opener
  — the same seam `Open in Browser` uses (#1429). Only on this explicit key:
  rendering never opens or fetches anything.
- **Anything else** is a path resolved against the previewed document and
  opened through the ordinary open funnel (`openPath`), so a claimed file kind
  still routes to its viewer. A `file.md#section` link opens at that heading's
  line via `openPathAt`, whose cursor move re-syncs a preview of that file in
  turn. A target that does not exist is a toast, not a silent no-op.

## Inline images (#2180)

Local images the buffer references render as pixels through the Kitty
graphics path the [image preview](./image-preview.md) already owns
(`internal/imgview`, #1479): each referenced file is decoded once and cached
by resolved path, and its rendered "Image: alt → target" line is replaced by a
block of Unicode-placeholder cells (`imgview.PlaceholderGrid`) scaled with
`imgview.FitGrid` to the pane interior. Rendered image lines pair with source
references positionally, the way headings pair with their rendering.

Substitution happens *before* the scroll-sync anchors are built, so a
heading's recorded rendered line is the line it really occupies and sync stays
correct around image blocks. Because placeholders are ordinary text cells, a
partially scrolled image block simply shows its visible rows.

Transmission reconciles like the image pane's: `imageSyncCmd` walks markdown
previews too, pushing the terminal's capability in with `SetGraphics` (which
re-renders, since pixels and fallback occupy different line counts), diffing
`ImageIDs` against `Model.liveImages` and emitting `SyncSeqs`. An image edited
out of the buffer leaves the live set and is deleted terminal-side;
`releaseWorkspaceImages` releases a preview's placements when its workspace
parks (#1547).

Without Kitty graphics — or while support is still unknown — glamour's text
form stays and a dim caption below it names the format, pixel size and file
size. **No network I/O ever happens:** a destination carrying a scheme is
remote by definition and is never opened for its bytes, so a remote image
stays exactly the text glamour rendered. Undecodable and missing targets
degrade the same way. Image *files* opened from the explorer/editor still
render in their own pane via the [image preview](./image-preview.md).

## Scroll sync

The editor emitter forwards `EventCursorMove` as `preview.CursorMsg`; the
preview maps the source line to a rendered line via heading anchors: ATX
headings (fenced code excluded) are located in the ANSI-stripped rendered
output in order, and the cursor's position interpolates proportionally within
its heading section. The mapped line is aimed a third down the viewport.
Mapping is approximate by design (v1 contract of #62). Anchors are rebuilt
against the *final* line list, image blocks included (#2180), so an image
between two headings shifts neither of them out of sync. A focused preview
also scrolls directly — `j/k`, arrows, `pgup/pgdown`, `ctrl+u/ctrl+d`, `g/G` —
and the mouse wheel scrolls it unfocused; the next cursor move in the source
re-syncs the view, including after a link selection scrolled it elsewhere.

## Future work

- A custom renderer over the Tree-sitter markdown grammar for better width
  control and full palette integration (long-term note in #62).
- An 'open preview' entry in the context menu (#30) once that lands.
- Clicking a link with the mouse; today link activation is keyboard-only.
- Animated GIFs stay first-frame stills, as in the image pane.
