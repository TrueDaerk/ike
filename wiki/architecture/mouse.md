---
type: concept
title: Mouse Gestures
description: The one convention every surface obeys for the wheel, the click and the double click, the shared list-mouse helpers behind it, and the audited surface×gesture matrix.
resource: internal/ui/listmouse.go
tags: [architecture, ui, mouse, lists, navigation, reusable]
timestamp: 2026-09-03T18:00:00Z
---

# Mouse Gestures

Issue #2259. Mouse support grew one pane at a time, so it grew unevenly: pane
resize and move worked everywhere, but whether a wheel notch scrolled, a click
selected and a second click activated depended on which pane the pointer sat
over. Two panes — the Elasticsearch console and the SFTP remote browser — had
no mouse handling at all, the merge view could not be scrolled with the wheel,
a floating picker ignored the wheel entirely, and nine list panes let a flick
scroll their last page off the screen while two clamped it.

This document is the convention, the shared helpers that carry it, and the
audit that produced both.

## The convention

For every **list-shaped surface** — anything that renders selectable rows in a
scrollable window:

- **The wheel scrolls, and only scrolls.** It moves the window, never the
  selection's meaning: the cursor is dragged along only far enough to stay
  inside the visible rows. It clamps at both ends — the last page never
  scrolls past its final row, so a flick can never leave a nearly empty pane.
- **A single click focuses and selects.** The pane under the pointer takes the
  focus (the app does that before the event reaches the pane), and the row
  under the pointer becomes the selection. Nothing else happens.
- **A double click activates** — the same thing `enter` does on that row: open
  the file, jump to the diagnostic, load the table, expand the variable. Two
  clicks on the same row within `ui.DoubleClickWindow` (400 ms) count; a
  slower second click is a fresh selection. Activating clears the tracker, so
  a third rapid click starts over rather than activating again — the explorer
  and the archive viewer already did this, the tool panels did not. A surface
  with no `enter` action (the two doctor panes) has nothing to activate, so it
  selects only.
- **Affordances answer to a single click.** A fold caret, a checkbox glyph, a
  `✕` zone or a `⧉` copy marker acts immediately and cancels the pending
  double click — waiting for a second press on a toggle would be wrong.
- **Chrome is inert.** Header lines, footers, key hints and the blank tail a
  short list leaves under its rows select nothing.

For **tabs**:

- **Left click activates** the tab (and focuses its pane), **middle click
  closes** it, **right click** opens the tab context menu with the clicked tab
  selected first, the `✕` zone closes it, **the wheel over the tab row cycles**
  tabs, and dragging a label tears the tab out. #2259 extended the middle
  click to the popup terminal box and the floating panels, whose bars had only
  the `✕` zone; a middle click on the active tab goes through the same
  unsaved-changes / busy-shell guard its `✕` does.

For **transient overlays** (the command palette, Search Everywhere, the
find-in-path finder, the settings panel, the floating-shell pickers) the
activation rule is deliberately looser: a **single click on a row picks it**,
because an overlay exists to choose exactly one thing and then close. The
finder, the settings panel and the floating-shell pickers use the press-again
variant — the first click selects, any later click on the already-selected row
opens it — which a double click satisfies too. Wheel and hit-test rules are the
same as everywhere else. A shell-hosted picker with no `enter` action (crash
recovery, whose per-file keys are `r`/`d`/`s`) selects only, and one whose rows
are checkboxes (the setup wizards) activates by toggling the box.

Horizontal panning (the horizontal wheel, and `shift`+wheel on terminals that
send no horizontal events) is a **viewer** gesture, not a list one: the editor,
the diff, the HTTP response body, the explorer, and the two grid panes pan
sideways; a plain list has nothing to pan.

## Shared helpers

`internal/ui/listmouse.go` is to the pointer what
[`listnav.go`](/architecture/list-navigation.md) is to the keyboard. Before it,
a dozen packages each carried their own copy of the same three things — and
the copies had drifted.

```go
// internal/ui/listmouse.go
const DoubleClickWindow = 400 * time.Millisecond

WheelWindow(top, cursor *int, delta, n, height int)      // clamped scroll, cursor dragged along
RowAt(y, top, headerRows, height, n int) (int, bool)     // content-local y → row index
type ClickTracker struct{ … }
    (*ClickTracker).Double(row int, now time.Time) bool  // second click on the same row?
    (*ClickTracker).Reset()                              // after an activation, or off a row

// The whole gesture in one call (#2462)
    (*ClickTracker).ClickRow(y, top, headerRows, height, n int, now time.Time,
                             cursor *int, activate func(int) tea.Cmd) tea.Cmd
SelectClick(y, top, headerRows, height, n int, cursor *int) bool
```

- `WheelWindow` is the clamp that used to differ: nine panes used
  `maxTop = n-1` (a flick could park a 40-row list with one row visible), the
  archive and data viewers used `maxTop = n-height`. The second is now the
  rule everywhere.
- `RowAt` is the hit-test. Its upper bound is what four panes were missing
  (Structure, the DOM inspector, the VCS changes list, GitHub Issues): a click
  on the footer row, or on the blank space under a short list, used to select
  whatever row index the arithmetic happened to produce.
- `ClickTracker`'s zero value is an empty tracker, so a pane no longer has to
  remember to seed `lastClickRow: -1`. Row identity is the caller's to define:
  the explorer offsets its scratch rows past the tree rows so a tree click and
  a section click never pair up.
- `ClickRow` (#2462) is the three of them composed: hit-test, select, activate
  on a second click on the same row, reset when the click lands off a row. It
  is what a pane's `Click(x, y)` now delegates to in one line — Problems,
  Usages, Dependencies and Structure had written that body out identically.
  `activate` may be nil for a list whose rows have no enter action;
  `SelectClick` is the same gesture without the double-click clock, for the
  read-only report panes (the two Doctors).

The **centered floating overlays** (the all-projects search form and its
results, the find-in-path finder, the TODO index, the undo tree) share their
hit test the same way (#2463): `overlayMouse` in
`internal/app/overlaymouse.go` closes the overlay on a click outside, routes a
left press to the overlay's own `Click` in overlay-local coordinates, scrolls
on the wheel when the overlay has a `Wheel(int)` method (the search form has no
list, so it ignores the wheel), and swallows everything else — an open overlay
never leaks a mouse event to the panes below. `handleMouse` chains them in
render order, topmost first.

Coordinates are **pane-content-local** throughout — `y 0` is the pane's first
rendered line — and the app translates screen cells into them once, in
`paneClick`.

The floating shell has the same seam for the pickers it hosts (#2275). The
shell renders its content as opaque text into a scroller, so it cannot map a
clicked line onto an item by itself; what it *can* own is the arithmetic:

```go
// internal/ui/floating.go
(*Floating).BodyPoint(x, y int) (int, int, bool)   // box-local cell → body-local x/y + scroll offset
(*Floating).BodyRow(x, y int) (int, bool)          // its row half
(*Floating).ClickRow(x, y int) (tea.Cmd, bool)     // routed to RowClickable content

type RowClickable interface { Content; ClickRow(row int) tea.Cmd }
```

`BodyPoint` subtracts the chrome origin (`ContentOrigin`: border + padding +
the heading rows) and adds `ScrollOffset()` back, so a picker sees the content
row it drew regardless of where the box sits or how far it is scrolled. The
root model calls it once in `handleMouse` and hands the result to
`shellBodyClick` (`internal/app/shell_rowclick.go`), which dispatches to the
picker that owns the shell; each picker's `…ClickRow` is the inverse of its
render loop. Content that can answer for itself implements `RowClickable`
instead — the root model is a value model, so its own pickers cannot mutate
state from inside a Content snapshot and go through the dispatcher.

## Surface × gesture matrix

The audit. "→" marks what #2259 changed; everything else was already in place
and is listed so the next change has a baseline.

### Editor and viewers

| Surface | Wheel | Click | Double click |
|---|---|---|---|
| Editor pane | ✅ vertical + horizontal + `shift` | ✅ caret, gutter breakpoint, `alt` multi-caret, `cmd` go-to-definition, scrollbar drag | ✅ word / triple line select |
| Editor tab bar | ✅ cycles tabs | ✅ activate, middle closes, `✕` closes, right-click menu, drag tears out | — |
| Pane title band | — | ✅ focuses, drag moves/docks, right-click menu | — |
| Pane divider | — | ✅ drag resizes | — |
| Markdown preview | ✅ | — (nothing selectable) | — |
| Diff viewer | ✅ vertical + horizontal + `shift` | ✅ selection anchor, drag extends | ✅ word / triple line |
| Merge view | ❌ → ✅ scrolls all three columns | ✅ side-column selection anchor | ✅ word / triple line |
| Image preview | — (nothing scrolls) | — | — |
| Terminal pane / tab | ✅ child, alt-screen, or scrollback | ✅ selection anchor, `cmd` opens links, scrollbar, dead-pane actions | ✅ word / triple line |

### Tool panes and list-shaped viewers

| Surface | Wheel | Click | Double click |
|---|---|---|---|
| Explorer | ✅ vertical + horizontal, scrollbar drag | ✅ select, `shift` range, caret folds, right-click menu | ✅ open / fold |
| VCS | ✅ → clamped | ✅ select | ✅ open diff |
| Problems | ✅ → clamped | ✅ select, → footer no longer selects | ✅ open location |
| Usages | ✅ → clamped | ✅ select, → footer no longer selects | ✅ open reference |
| Structure | ✅ → clamped | ✅ select, → footer no longer selects | ✅ navigate |
| Breakpoints | ✅ → clamped | ✅ select, glyph toggles enabled | ✅ jump |
| Test Results | ✅ → clamped (tree or detail) | ✅ select, detail column takes focus | ✅ jump to test |
| GitHub Issues | ✅ list, detail, modal | ✅ tab bar, filter chips, select, → footer no longer selects | ✅ open detail |
| DOM inspector | ✅ → clamped | ✅ select, selector line edits, fold glyph | ✅ navigate |
| Debug (frames/vars) | ✅ | ✅ select, tab bar, separator drag | ✅ frame select / expand |
| Debug (console) | ✅ routed to the terminal | ✅ terminal gestures | ✅ word / triple line |
| Archive viewer | ✅ | ✅ select, fold glyph | ✅ open read-only |
| Data viewer | ✅ vertical + horizontal, page crossing | ✅ region focus, select, grid row cursor | ✅ load table |
| Elasticsearch console | ❌ → ✅ vertical + horizontal, page crossing | ❌ → ✅ region focus, select, grid row cursor | ❌ → ✅ load index |
| Remote browser (SFTP) | ❌ → ✅ | ❌ → ✅ select, fold glyph | ❌ → ✅ open read-only |
| HTTP response | ✅ vertical + horizontal, scrollbar drag | ✅ selection anchor, `⟳` resend, `⧉` copy fold | ✅ word / triple line |
| Xdebug Doctor | ✅ → clamped | ✅ select | — (no `enter` action) |
| LSP Doctor | ✅ → clamped | ✅ select | — (no `enter` action) |

### Overlays and chrome

| Surface | Wheel | Click | Double click |
|---|---|---|---|
| Command palette / Search Everywhere | ✅ per column, moves the highlight | ✅ picks the row, `✕` zone runs the aux action | (single click already picks) |
| Find in path (finder) | ✅ | ✅ selects; a press on the selected row opens | (press-again covers it) |
| TODO index | ✅ | ✅ | (single click picks) |
| Undo tree | ✅ | ✅ | (single click picks) |
| Settings panel | ✅ | ✅ rows, hover, border resize drag | (press-again covers it) |
| Floating-shell pickers | ✅ scrolls the shell viewport | ✅ picks the row (#2275), group titles/legends inert, border resize drag | (press-again covers it) |
| Menu bar / dropdowns | — | ✅ opens, hover selects, invokes | — |
| Context menu | — | ✅ hover, invoke, press outside dismisses | — |
| Status line | — | ✅ clickable segments dispatch their command | — |
| Large-file banner | — | ✅ `✕` dismisses, body forces code insight | — |
| Popup terminal box / floating panels | ✅ scrollback per side; outside the boxes the notch falls through to the pane below, focus untouched (#2343) | ✅ tab activate, `✕` closes, ❌ → ✅ middle closes, title drag moves, border resizes | — |
| Floating windows | ✅ per layer | ✅ title-bar drag moves, border ring resizes | — |

## See also

- [Selection-List Navigation](/architecture/list-navigation.md)
- [Picker Speed Search](/architecture/speed-search.md)
- [Tool Panes](/architecture/tool-panes.md)
- [Editor Tabs](/architecture/editor-tabs.md)
- [Pane Layout](/architecture/pane-layout.md)
- [Floating Shell](/architecture/floating-shell.md)
