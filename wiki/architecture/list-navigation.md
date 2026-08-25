---
type: concept
title: Selection-List Navigation
description: The shared cursor semantics every selectable list obeys — single steps wrap around, page keys jump one visible page and clamp, home/end go to the extremes, and the scroll window follows the selection.
resource: internal/ui/listnav.go
tags: [architecture, ui, lists, navigation, keys, reusable]
timestamp: 2026-08-25T18:00:00Z
---

# Selection-List Navigation

Issue #1666. IKE renders dozens of selectable lists — the command palette and
Search Everywhere, the recent-projects column, find-in-path results, the
locations-backed tool windows (Usages, Problems, TODO index), the explorer
tree, every settings page, the marketplace, the undo tree, the debugger's
frame and variable columns, the completion popup, and a family of modal
pickers (pins, local history, VCS history, crash recovery, the setup wizards).
Each of them used to roll its own cursor arithmetic, so paging existed in some
views and not others and nothing wrapped.

`internal/ui/listnav.go` is the one place that defines what a list cursor
does. Views keep their own state and rendering; they take the *semantics* from
here.

## Semantics

Two rules, applied everywhere:

- **Single steps wrap.** Down on the last entry lands on the first, up on the
  first lands on the last. This covers `↓`/`↑`, the vim aliases `j`/`k` where
  a view can spare them, and `ctrl+n`/`ctrl+p`.
- **Page jumps clamp.** `pgdn`/`pgup` move by one *visible page* — the list's
  own rendered height, not a fixed row count — and stop at the ends. This is
  the vim/fzf convention: wrapping a page jump is disorienting because the
  jump distance already varies with the window size.

`home`/`end` (and `g`/`G` where the view's single letters are free) go to the
first/last entry. Lists that sit behind a text query — the palette, the finder
— leave `home`/`end` to the query cursor; their page keys still page the list.

The mouse wheel keeps **clamped** semantics everywhere: a wheel flick past the
end must not teleport to the other end of the list.

## Scrolloff (#2041)

`ScrollToShowOff` is `ScrollToShow` with vim's `scrolloff`: it keeps `off`
rows visible **above and below** the selection wherever the list allows it, so
navigating downwards moves the window on as soon as the cursor reaches the
`off`-th-last visible row instead of only at the very edge — the next entry is
always on screen before the user asks for it. Two clamps keep it quiet: the
margin is capped at `(height-1)/2` so it can be honoured on both sides of a
short window, and it is clipped against the list ends, so the first and last
entries still sit flush against the window edge and no empty row is ever
scrolled into view. `ScrollToShow` is `off = 0`. The command palette passes
`off = 1` for both of its columns.

## API

```go
// internal/ui/listnav.go
ClampIndex(i, n) int                  // confine to [0, n-1]
StepIndex(i, delta, n) int            // wrapping single step
PageIndex(i, delta, n, page) int      // clamping page jump
ScrollToShow(top, sel, height, n) int // viewport follows the selection
ScrollToShowOff(top, sel, height, n, off) int // …with a scrolloff margin

ListNav(key string, sel *int, n, page int, keys NavKeys) bool
```

`ListNav` is the router most views call: it moves `*sel` and reports whether
it consumed the key, so the caller's own `switch` runs only for keys the list
did not claim. `NavKeys` is a bitmask because views differ in what they can
spare:

| Flag             | Keys                  |
|------------------|-----------------------|
| `NavArrows`      | `↑` `↓` `pgup` `pgdn` |
| `NavEmacs`       | `ctrl+p` `ctrl+n`     |
| `NavVim`         | `j` `k`               |
| `NavVimExtremes` | `g` `G`               |
| `NavHomeEnd`     | `home` `end`          |

`NavDefault` is `NavArrows | NavEmacs | NavHomeEnd` — a list that owns its keys
but has no text query. `NavFull` adds both vim sets. `NavVimExtremes` is split
off `NavVim` because pages whose single letters are actions (the settings
pages: `a` add, `d` delete, `e` toggle) can spare `j`/`k` but not every
letter.

An empty list consumes nothing; a one-entry list consumes the key and stays at
0.

## Page size

The page-jump distance is the list's visible height, which every view already
knows one way or another:

- `locations.List` records the height its last `Render` was given and exposes
  `Step`/`Page`/`Home`/`End`; a page jump moves the cursor to the item nearest
  the target *render* row, so file-header rows count towards the screenful.
- Panes with a body area (Problems, Usages, Structure, Breakpoints, the
  debugger columns, the VCS changes list) pass their `bodyHeight()`.
- Overlays derive it from the terminal height (`undotree`, `callhier`,
  `typehier`) — known before the first render, so a page key works on a list
  that has never been drawn.
- Settings pages embed `navRows`, which records the height their `View` was
  asked to render into; `navPage` (10) is the fallback until the first render.
- Pickers hosted in the floating shell read `ui.Floating.ViewportRows()` via
  the `app.pickerNav` adapter.

## Keys are not remappable (yet)

The navigation keys are hard-coded in each view, as they were before #1666 —
no in-list binding goes through `internal/keymap` today, and the `Palette`
context exists but carries no default bindings. The change here unified the
semantics, not the binding layer; making list navigation remappable is a
separate piece of work on the keymap contexts.

## Typing into a list

A list that narrows as you type is a separate, composable layer:
[Picker Speed Search](/architecture/speed-search.md) (#2111). It matters here
because the two share the keyboard — a speed-searchable picker must run on
`NavDefault` rather than `NavFull`, since `j`/`k`/`g`/`G` would swallow the
query's first rune.

## See also

- [Picker Speed Search](/architecture/speed-search.md)
- [Command Palette](/architecture/command-palette.md)
- [Floating Shell](/architecture/floating-shell.md)
- [Settings UI & Menu Bar](/architecture/settings-ui.md)
- [Keybindings & Shortcuts](/architecture/keybindings.md)
