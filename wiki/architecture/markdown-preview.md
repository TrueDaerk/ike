---
type: concept
title: Markdown Preview
description: "#62 — rendered live preview pane for markdown buffers: glamour-rendered ANSI split beside the editor, debounced re-render off the editor change seam, heading-anchored cursor scroll sync, theme-aware styling, layout persistence."
resource: internal/preview
tags: [architecture, markdown, preview, pane, glamour]
timestamp: 2026-07-12T00:00:00Z
---

# Markdown Preview (#62)

`internal/preview` renders a markdown buffer to styled terminal output as a
live pane beside its editor. IKE documents itself in markdown (this wiki), so
the feature is self-hosting.

## Opening and closing

`markdown.preview` (palette, `cmd+k m` in the editor, leader `space P` /
`ctrl+k P`) splits the active editor's leaf to the right with a preview pane
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
glamour's chroma highlighting. Images degrade to their alt-text links —
terminal image protocols are out of scope.

## Scroll sync

The editor emitter forwards `EventCursorMove` as `preview.CursorMsg`; the
preview maps the source line to a rendered line via heading anchors: ATX
headings (fenced code excluded) are located in the ANSI-stripped rendered
output in order, and the cursor's position interpolates proportionally within
its heading section. The mapped line is aimed a third down the viewport.
Mapping is approximate by design (v1 contract of #62). A focused preview also
scrolls directly — `j/k`, arrows, `pgup/pgdown`, `ctrl+u/ctrl+d`, `g/G` — and
the mouse wheel scrolls it unfocused; the next cursor move in the source
re-syncs the view.

## Future work

- A custom renderer over the Tree-sitter markdown grammar for better width
  control and full palette integration (long-term note in #62).
- An 'open preview' entry in the context menu (#30) once that lands.
