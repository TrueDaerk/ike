---
type: concept
title: Notebook Viewer
description: "#2425 — .ipynb files open read-only as their cells: markdown through the preview renderer, code highlighted under the notebook language, outputs (stream, text/plain, degraded text/html, PNG/JPEG via Kitty graphics, errors) below each cell, with cell navigation, output folding, source search, copy, open-in-scratch and image saving."
resource: internal/nbview
tags: [architecture, notebook, jupyter, ipynb, pane, viewer]
timestamp: 2026-09-03T00:00:00Z
---

# Notebook Viewer (#2425)

`internal/nbview` renders a Jupyter notebook read-only as the cells its author
wrote, in a pane of kind `KindNotebook` — instead of the JSON document the
file actually is. Execution and editing are out of scope; the model is shaped
so a later edit mode grows on it rather than replacing it.

The package splits along that line:

| file | owns |
| --- | --- |
| `notebook.go` | the nbformat 4 model and its parse — no rendering |
| `nbview.go` | the pane: state, keys, search, actions, chrome |
| `render.go` | cells → rows, once per size/theme/fold/content change |
| `images.go` | the Kitty placements of the image outputs |
| `html.go` | the text/html → text degradation |

## The model

`Parse` reads nbformat 4. Both source spellings — the line array notebooks
normally store and the plain string some tools write — decode to one string
(`multiline`), and `execution_count: null` is "never run", rendered `[ ]`
rather than `[0]`. A JSON syntax error comes back verbatim; well-formed JSON
that is not a notebook is refused with its own reason (no `cells` array, or
an nbformat 3 `worksheets` document), and the pane shows that reason next to
the way out: **Open File As… → Text editor** opens the raw JSON.

Every output is reduced to what the viewer can show *at parse time*, so
rendering never re-inspects the MIME bundle. The bundle preference is
image → `text/plain` → `text/html`: the pixels are what the author saw, and
`text/plain` is Jupyter's own fallback, so HTML only wins where nothing else
exists. An output the viewer can render nothing of (a custom
`application/vnd.*` bundle) is dropped rather than left as an empty block.

The notebook language comes from `metadata.language_info.name`, falling back
to `metadata.kernelspec.language`; the scratch extension from
`language_info.file_extension`, falling back to the registered extension of
that language id, then `txt`.

## Rendering

`render()` builds a flat row list — the layout is done once per size, theme,
fold or content change, never per frame: a markdown cell costs a glamour
render and a code cell a tree-sitter parse.

* **Gutter.** Every cell's first row carries `<index> <type>` and, for code
  cells, the execution count: `2 code [1]`, `6 code [ ]`, `1 md`. Later rows
  of the cell carry only the bar. The cursor cell's gutter is accented.
* **Markdown cells** go through `preview.Render` — the pane-free half of the
  [markdown preview's](./markdown-preview.md) renderer, extracted for this —
  so a notebook's prose reads exactly like a previewed `.md` file.
* **Code cells** are highlighted with `highlight.HighlightFenced` under the
  notebook's language id, the same path the HTTP response pane uses for its
  bodies. No language, no compiled-in grammar or a cell past 128 KiB renders
  plain, which keeps every notebook readable.
* **Outputs** follow the source, each introduced by a dim label row: the
  stream channel (`stdout`, `stderr` — the latter in the warning colour),
  `Out[3]` for an execute result, `text/html as text` for a degraded HTML
  output, or the image's `image/png · 640×480 px · 12.3 KB`. Error outputs
  render `ename: evalue` and the traceback in the error colour, with the
  traceback's ANSI colouring stripped rather than passed through.
* **Empty cells** still render one placeholder row, so their gutter label is
  there instead of the cell vanishing.

### Images

Image outputs use the Kitty graphics path of the [image
pane](./image-preview.md) (#1479) and the preview's inline images (#2180): the
base64 payload is decoded once, placed as a block of Unicode placeholder
cells, and reconciled by the app's `imageSyncCmd` — `HasImages`, `ImageIDs`,
`TransmittedIDs`, `SyncSeqs`, `ResetImages`, `SetGraphics` are the same
contract the preview implements. Ids mint from 60000, above `imgview` (9000)
and the preview (30000), so the three can never collide in one terminal's
graphics memory. Where the terminal has no graphics support the metadata
label *is* the output. Folding a cell releases its placements, so no ghost
graphics survive.

## Navigation, folding, search

`j`/`k` step the **cell** cursor and reveal the cell; `g`/`G` jump to the
ends; arrows, `pgup`/`pgdn`, `ctrl+d`/`ctrl+u` and the mouse wheel scroll
rows without moving the cursor — a reader scrolling past a cell should not
silently retarget `e` or `y`.

`enter` folds and unfolds the cursor cell's outputs, replacing them with a
`▸ 2 outputs folded` marker. A cell with no outputs has nothing to fold.

`/` (and cmd+f through the shared `Searchable` capability, #2409/#2410)
searches the **cell sources** — what the author wrote, not what a run
happened to print — case-insensitively. `n`/`N` and cmd+g/cmd+shift+g step
the matches, moving the cell cursor with them; matching rows highlight and
`esc` drops the set.

## Actions

| key | action |
| --- | --- |
| `e` | open the cursor cell's source as a scratch in the notebook's language (`nbview.ScratchMsg`) |
| `y`, cmd+c | copy the cursor cell's source (`nbview.CopyMsg`) |
| `o` | save the cell's first image output next to the notebook (`nbview.SaveImageMsg`) |

All three are messages, not actions the pane takes: the clipboard, the
scratch store and the file system belong to the root model
(`internal/app/nbfiles.go`). A saved image never overwrites — a taken name
gets a `-2`, `-3`, … suffix, so saving the outputs of a re-run notebook keeps
every version.

## Routing and lifecycle

The compile-in `notebook` plugin claims `.ipynb` by extension only — a
notebook is JSON, and no magic distinguishes it without parsing the whole
file. It dispatches `OpenNotebookMsg`; `openNotebookPane` opens as a content
tab in the pane the open asked for (#1825/#1851) and otherwise splits the
leaf `viewerSplitTarget` picks, refocusing an existing pane bound to the same
path. Keys mint as `notebook`, `notebook:2`, …; persistence records
`{Kind: "notebook", Path}` and restore re-reads the file.

The pane is added to the watcher's poll set on open, and
`routeWatchEvent` calls `refreshNotebooks` on a change: the viewer holds no
editor buffer, so nothing else in the routing would notice a kernel rewriting
the file. A reload keeps the fold set and clamps the cursor into the new
document; a file that turns malformed becomes the error pane rather than
stale rows.

The [Open File As…](./hex-viewer.md) chooser gained a **Notebook** row, which
validates by parsing before a pane exists — so a notebook saved as `.json`
opens as cells, and a plain JSON document refuses with a notification instead
of opening a pane that shows only its own error.
