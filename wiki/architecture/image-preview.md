---
type: concept
title: Image Preview
description: "#1479 — image files render in a preview pane via the Kitty graphics protocol (Unicode placeholders): capability probe with metadata fallback, per-pass transmit/delete reconcile, decode of PNG/JPEG/GIF/WebP, layout persistence; #2688 zoom and pan via a crop-based placement."
resource: internal/imgview
tags: [architecture, image, preview, pane, kitty, graphics, zoom]
timestamp: 2026-09-23T00:00:00Z
---

# Image Preview (#1479)

`internal/imgview` renders image files (PNG, JPEG, GIF first frame, WebP —
decoders registered via the stdlib and `golang.org/x/image/webp`) in a pane
of kind `KindImage`, using the Kitty graphics protocol on terminals that
support it (Ghostty, Kitty, WezTerm) and a metadata summary everywhere else.

## Routing

The compile-in `images` plugin (`internal/app/images.go`) claims the files
via a `FileHandler` — extensions plus a magic-byte sniff on the `readHead`
buffer — so `openPath` routes an explorer/editor open to the preview instead
of failing UTF-8 decode into an error toast. The handler dispatches
`OpenImageMsg`; `openImagePreview` opens as a content tab in the pane the open
asked for — the focused pane for a palette pick (#1825), the last-focused
editor for the explorer's default open (#1851, see
[data viewer](./data-viewer.md)) — and otherwise splits the leaf
`viewerSplitTarget` picks (see [pane layout](./pane-layout.md)), refocusing an
existing pane already bound to the same path instead of duplicating. Keys mint
as `image`, `image:2`, …; persistence records
`{Kind: "image", Path}` and restore re-decodes the file (a vanished file
restores as the pane's decode-error fallback).

## Rendering: Unicode placeholders

IKE uses the protocol's Unicode-placeholder flavour (`U=1`), not absolute
positioning: the image is transmitted once as a *virtual placement* (PNG
payload, base64, 4096-byte chunks — `kitty.go`) scaled to a cell grid that
`FitGrid` fits into the pane preserving pixel aspect (cells counted 2:1
tall). `View` then renders ordinary text: rows of U+10EEEE placeholder cells
carrying row/column diacritics, with the image id encoded in the foreground
colour. The terminal composites the image over those cells. Because
placeholders are plain cells, bubbletea's renderer, the pane box cache,
overlays and zoom all stay correct — a repaint can never leave ghost
graphics, and an untransmitted placeholder renders as blank cells, never
garbage.

## Zoom & pan (#2688)

The pane opens at **fit** (the whole picture inside the body) and can zoom
into a detail. Zoom is a factor relative to the fit size, stepped
multiplicatively (×1.25 per step, `imgview.ZoomStep`); zooming out below fit
is not possible and zooming in stops at the larger of 8× fit
(`MaxZoomFactor`) and the **1:1** level, where one image pixel maps to one
terminal pixel assuming `CellPxW`-pixel-wide cells — so the loop always ends.
Zoom and pan are view state only (never persisted; a restore opens at fit),
and on a terminal without Kitty graphics the keys do nothing and the metadata
card is unchanged.

| Input | Effect |
|---|---|
| `+` / `=` | zoom in one step around the view centre |
| `-` | zoom out one step (no-op at fit) |
| `0` | back to fit |
| `alt+wheel` | zoom in/out one step per tick around the pointer cell |
| wheel | pan vertically (when zoomed) |
| `shift+wheel`, horizontal wheel | pan horizontally (when zoomed) |
| primary-button drag | pan the picture with the pointer (when zoomed) |
| `h`/`j`/`k`/`l`, arrows | pan by a few cells |
| `ctrl+d` / `ctrl+u` | pan by half the body height |

At fit nothing pans — the whole image is on screen. Pan is clamped so the
visible rectangle never leaves the image. The keys are pane-internal (the
notebook viewer's pattern, `imgview.Model.Update` switching on the key
string), not registry commands, so no keybind entries exist for them. The
mouse gestures route from the root model's mouse dispatch (`handleMouse`):
the wheel branch for `KindImage` reads the modifiers, and a left press arms a
`dragImagePan` drag whose motion events call `MouseDrag`.

**Crop-based placement.** The pane keeps one placement filling its body and
shows a *source rectangle* of the image. `geometry()` derives, from the zoom
factor, the virtual grid the whole image would occupy (`fit × zoom`), clips
it to the body, and maps the clipped part back to a crop in image pixels
(`Crop()`); the pan offset is the crop origin. The placement command carries
that crop (`x=`, `y=`, `w=`, `h=`) whenever it is smaller than the image.
Pixels are transmitted **once** per lifecycle with `a=t` (`TransmitData`,
store only) and every geometry change — resize, zoom, pan — deletes the old
placement with `a=d,d=i` (`DeletePlacements`: placements only, data kept)
and issues a new `a=p,U=1` placement (`Place`) under the **same image id**.
This was chosen over a fresh id per crop because the app's reconcile
(`liveImages`, `releaseWorkspaceImages`, the pane-close delete) keys on one id
per pane; a new id per crop would have needed an id history per pane just to
free the previous placements. A crop change is therefore delete + re-place
without a retransmission; an unchanged crop emits nothing.

**Footer.** The last row of the body is a status line — file name, format,
dimensions and the zoom label (`fit`, `2.4×`, `1:1`) — so the user always
knows where they are; the fit grid is computed over the rows above it.

## Capability probe & fallback

Support is detected lazily: when the first image pane opens, the reconcile
pass emits one `a=q` probe (`imgview.Query`, id 424242). A supporting
terminal answers with an APC response that ultraviolet delivers as
`uv.KittyGraphicsEvent` straight into `Update`; `QueryResponseOK` flips
`Model.kittyGfx` and the same pass transmits every open image pane. A
terminal without support never answers — nothing waits on it, and the panes
keep rendering the fallback: file name, format, dimensions, size, and the
reason. Decode failures show the same card with the error.

## Lifecycle reconcile

`imageSyncCmd` runs at the end of every root `Update` pass (next to the
structure/breadcrumb hooks): it walks the active workspace's image panes and
diffs desired placements against `Model.liveImages` (a pointer-shared map
like `toolchainSeg`). First show transmits the pixels and places them; a pane
resize, zoom or pan deletes the placement and re-places at the new grid and
crop without retransmitting (`SyncSeqs` is idempotent while the geometry is
unchanged, see [Zoom & pan](#zoom--pan-2688)); a closed pane emits `a=d,d=I` for its id, freeing the
terminal-side data. All sequences leave through one `tea.Raw`, bypassing the
renderer. A workspace switch or teardown releases its placements separately
(#1547): `releaseWorkspaceImages` emits the deletes and resets each pane's
transmission state — `liveImages` is re-initialized empty in `buildModel`,
so the per-pass diff could never reach a parked workspace's ids — and the
reset makes the resume's reconcile pass transmit again.

## Boundaries

- This pane handles image *files*. The markdown preview and the notebook
  viewer render the images they reference/embed through the same protocol
  layer (#2180, #2425) — `FitGrid`, `PlaceholderGrid`, `Transmit`, `Delete`
  and `HumanSize` are shared, and `imageSyncCmd` / `releaseWorkspaceImages`
  reconcile all three kinds — but each owns its own placements; see
  [markdown-preview](./markdown-preview.md) and
  [notebook-viewer](./notebook-viewer.md). Since #2464, both features' own
  per-image record embeds `imgview.PlacedImage` (id plus the wanted-vs-sent
  grid) and their `SyncSeqs` delegates to `imgview.SyncSeqs` over it, so the
  transmit/delete diff itself is one implementation, not two.
- No animation: a GIF shows its first frame (stdlib decode).
- Sixel / iTerm2 inline-image protocols are out of scope; terminals without
  Kitty graphics get the metadata card.
