---
type: concept
title: Navigation History (Back/Forward)
description: Cursor-position history across jumps — per-jump entries with JetBrains Back/Forward semantics, recorded at the open funnel, traversed by nav.back / nav.forward — plus the edit-location ring behind nav.lastEdit and the Recent Locations picker.
resource: internal/nav/history.go
tags: [architecture, navigation, editor, keybindings]
timestamp: 2026-09-08T00:00:00Z
---

# Navigation History (Back/Forward)

Roadmap 0220, promoted from idea #51. `nav.back` / `nav.forward` return the
caret to where it was before a jump and re-traverse after going back —
JetBrains Navigate Back/Forward semantics. The commands back the
`cmd+left-bracket` / `cmd+right-bracket` defaults (fragile on many
terminals and awkward on QWERTZ, where the bracket keys need option-combos —
hence the macOS-only `cmd+alt+left` / `cmd+alt+right` aliases, IntelliJ's
classic Back/Forward chords, #2361), the **mouse back/forward buttons** (#816:
buttons 4/5 arrive as the synthetic single-step chords `mouse-back` /
`mouse-forward` and resolve through the normal keymap, so they rebind like
keys; terminals without SGR extended buttons simply never deliver them, and
an unbound press is swallowed rather than leaked into a pane), the Navigate
menu entries, and the palette.

## Semantics

- **Per-jump entries, not per-keystroke.** An entry is recorded when the
  caret jumps through the open funnel — switching files (explorer, finder,
  palette, `host.OpenFileRequest`), go-to-definition, a references-list
  pick, a find-in-path result — and for in-file jumps (#219): large
  motions (`gg`, `G`, `{count}G`), search landings (the initial `/`/`?`
  jump, `n`/`N`, `*`/`#`), vim-mark jumps (#1151: `'{x}` / `` `{x} ``
  and the `nav.bookmarks` picker — global marks route through the open
  funnel, local ones emit the same jump event), and label-jump landings
  (#787: `gs`, which lands through the same `jumpTo` seam as a search
  landing). Small motions (hjkl, w/b,
  paragraphs, page scrolls) never record, and an operator composed over a
  large motion (`dG`) is an edit, not a jump.
- **Tab switches record too** (#816): activating another tab — the tab bar,
  `editor.tab.next`/`prev`, `editor.tab.select1…9`, the wheel over the tab
  bar — leaves one file for another and records the departure, so Back
  returns to the tab just left. Recorded in `switchTab` from the *switching*
  pane (a tab click can hit an unfocused pane) and only when the file
  actually changes; the open funnel's own tab activation (`activateTab`)
  stays out of it, so history navigation never records its own arrival.
- **Back** returns to the departure point; **forward** re-traverses after a
  back. A fresh jump while back in history truncates the forward tail.
- **Dedup**: consecutive entries on the same file+line collapse (keeping
  the freshest column); column-only drift is not a jump.
- **Bounded**: 100 entries per direction, oldest fall off. Session-scoped —
  not persisted, and a project switch starts fresh.
- **Exhausted direction** → info toast ("no earlier/later position"), no-op.

## Architecture

```
internal/nav/        pure data structure: Position{Path,Line,Col} (0-based),
                     History{RecordJump, Back, Forward, CanBack, CanForward}
internal/app/nav.go  integration: currentNavPos (active editor file+caret),
                     navPosOfPane (a given pane's, for tab switches),
                     NavBackMsg/NavForwardMsg handling, navigateHistory
internal/app/tabs.go switchTab records the departure on a tab change (#816)
internal/nav/edits.go       EditRing{Record, Step, Recent} + Location{Position,Root},
                            History.Recent — the edit-location ring (#2545)
internal/app/recentlocations.go  nav.lastEdit / nav.recentLocations integration:
                            NavLastEditMsg, the recentLocationsMode palette mode,
                            jumpToLocation (cross-project switch via allPendingOpen)
```

- Recording sits at the root model's open funnel: `openPath` records when
  the target *file* differs, `openPathAt` additionally records same-file
  jumps to another *line* — so every jump source (definition, references,
  search results, file switches) is covered at two choke points instead of
  per-feature hooks.
- **Landing framing** (#996, #1373): `openPathAt` places the caret through
  `editor.JumpTo`. Off-screen targets frame `jumpTopMargin` (3) rows below
  the viewport's top edge (JetBrains-like context margin). Targets already
  comfortably visible move only the caret — no viewport yank — and targets
  within `jumpEdgeMargin` (5, widened by `editor.scroll_off` when larger)
  rows of an edge scroll minimally onto that margin line, vim
  `scrolloff`-style. Panes too short for the margin zones always use the
  near-top framing; `SetScroll`'s clamp keeps end-of-buffer jumps sane.
  Interactive cursor motion is untouched.
- In-file jumps come through the editor's event seam (#219): the editor
  emits `EventJump` carrying the *departure* position immediately before a
  large motion or search landing moves the caret (`motion.Result.Jump` for
  `gg`/`G`, `jumpTo` for search sites); the app's `editorEmitter` adapter
  records it into the shared history and swallows the event — the landing
  follows as an ordinary cursor-move, so the LSP bridge sees nothing new.
- `Back(current)` / `Forward(current)` take the caret's current position so
  the opposite stack stays consistent; navigation itself goes through
  `openPathAt` with recording suppressed (`navSkip`), reusing the standard
  open flow (tab reuse, focus, hooks) — no remembered pane identities,
  which keeps entries valid across layout changes. With split layouts, a
  jump target already open in *any* editor pane focuses that pane's tab
  (#930) — so back/forward returns to the pane a file lives in instead of
  duplicating it into the active pane; a target open nowhere lands in the
  active editor pane as before.
- **Stale entries** (#220): traversal passes a validity filter
  (`BackWhere`/`ForwardWhere` with an `os.Stat` check) — an entry whose
  file was deleted or renamed is silently dropped and traversal continues
  in the same direction; the current position lands on the opposite stack
  only when a real target is found, so skipped attempts leave no
  duplicates.
- `nav.back` / `nav.forward` are `appCommand`s (compile-in `app` plugin)
  dispatching `NavBackMsg` / `NavForwardMsg`; the Navigate menu was already
  wired to these ids.

## Last Edit Location and Recent Locations (#2545)

The jump history tracks jumps, not edits: after a terminal or issue detour the
way back to the code was `nav.back` ×10. JetBrains' *Last Edit Location* and
*Recent Locations* close that gap with an **edit-location ring** beside the
history.

- **Ring** (`internal/nav/edits.go`, `EditRing`): every buffer mutation
  records the caret's file+position (`Location` = `Position` + project
  `Root`) from the editor emitter's `EventChange` — the same seam the LSP
  sync rides — so every edit path (insert, delete, paste, substitute,
  multi-caret, reload) records at one choke point. Newest last, **deduped by
  line proximity**: an edit within 3 lines of an existing entry in the same
  file replaces it (moved to the newest slot), so typing across a few
  adjacent lines is one place; **capped** at 50, oldest fall off. Pathless
  scratch buffers record nothing.
- **`nav.lastEdit`** (`cmd+shift+backspace`, JetBrains' chord;
  `ctrl+shift+backspace` as its Windows/Linux form and the cmd→ctrl fold)
  jumps to the newest edit site; a **repeat walks back** through the ring
  (`EditRing.Step` keeps the walk index and continues from it while the
  caret still sits where the previous step landed — any edit or move
  elsewhere restarts from the newest). Entries on the caret's own line are
  skipped, so invoking it at the edit site goes to the previous edit; a
  deleted or renamed file is walked past like a stale history entry.
  Exhausted → info toast, no-op. The jump goes through `openPathAt`
  *with recording*, so `nav.back` returns to where the detour ended.
- **`nav.recentLocations`** (`alt+shift+e`: JetBrains' `cmd+shift+e` is
  `project.switchLast` here, #2398, and `cmd+alt+e` folds onto the jq/yq
  query view) opens a locked palette mode (`recentLocationsMode`,
  `internal/app/recentlocations.go`) listing the ring newest first (rows
  marked `✎`), then the jump history's positions newest first (`↷`,
  `History.Recent`: back stack then forward tail), each file+line once —
  an edit site that was also a jump departure keeps its edit row. Every row
  carries a one-line **code preview** chip (the open buffer's line when the
  file is loaded, disk otherwise) plus the palette's code column (#2053).
  An empty query keeps the recency order; typing fuzzy-filters on path and
  preview text. Both commands sit in the Navigate menu.
- **Cross-project entries**: the ring is **session state** — it rides across
  project switches (the carry-over block in `performSwitchOpts`, which also
  re-wires the fresh model's emitters onto the carried ring) — so a
  location recorded in another project stays reachable. Activating one
  parks the open on `allPendingOpen` and runs the standard switch
  transaction (`project.SwitchTo`); the `SwitchedMsg` handler finishes the
  open, exactly the all-projects search's path (#2394). The jump history
  itself stays per-project (fresh on switch), so its rows never carry a
  root.

## Diagnosing the mouse buttons

A press that does nothing has two very different causes, and the silent
degrade hides which one it is (#816):

- **The terminal never reports buttons 4/5.** Run `go run ./cmd/keyprobe`
  and click the buttons: the probe enables the same mouse mode the editor
  uses (`MouseModeCellMotion` + SGR extended coordinates) and lists
  `mouse-back` / `mouse-forward` among its targets, so a terminal that stays
  silent shows them as `missing`. The translation from button to chord is
  `keymap.FromMouseButton`, shared by the probe and the app.
- **The press arrived but did nothing.** Every delivered navigation button
  leaves a `mouse: navigation button delivered as …` line in the per-project
  `.ike/debug.log`. A line with no jump means the keymap or the history is
  at fault, not the terminal.

While a modal overlay owns the input (palette, settings, finder, …) the
buttons are swallowed: navigating the editor hidden underneath would be
invisible.

## Keybinding status

With the commands registered, both ids left the 0081 blocked ledger
(`internal/keymap/blocked.go`); the status matrix rows read "live via
palette". See [Keybindings & Shortcuts](/architecture/keybindings.md).
