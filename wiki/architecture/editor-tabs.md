---
type: concept
title: Editor Tabs
description: The per-pane tab model — each editor pane hosts an ordered tab list (documents and embedded terminals) with one active tab; opening routes into the focused pane's tab list, closing peels tabs before the pane.
resource: internal/pane/instance.go
tags: [architecture, panes, tabs, editors, terminals, shared-documents, close]
timestamp: 2026-07-18T00:00:00Z
---

# Editor Tabs

Roadmap 0190 (#156). An editor pane no longer hosts exactly one document: each
`pane.Instance` of `KindEditor` holds an **ordered tab list** (`[]*pane.Tab`)
plus an **active index**, JetBrains-style. The layout tree is untouched — a leaf
is still one pane; tabs live entirely inside the instance. Since 0350 (#573) a
tab slot holds **either a document editor or an embedded terminal** (`pane.Tab`
is a sum type over `*editor.Model` / `*terminal.Model`), so run output and
shells can live next to the files they belong to.

## The tab model (`internal/pane`)

- Every editor instance starts with a single scratch tab and **never has zero
  tabs** — closing the only tab is refused (`CloseTab` returns false) and the
  caller closes the pane instead.
- `Editor()` returns the **active tab's** model, so the pane surface (render,
  key routing, status/title) keeps its one-document view of the world.
  `Editors()`, `TabEditor(i)`, `TabCount()`, `ActiveTab()`, `TabForPath(path)`
  and `EditorForPath(path)` expose the full list for "all documents of this
  pane" sweeps.
- Operations: `AddTab` (appends at the end, inheriting size/config/palette and
  activating), `ActivateTab`, `MoveTab` (reorder, the moved tab stays active),
  `CloseTab` (the right neighbour slides in and takes over; the last position
  falls back left).
- Focus lives on exactly one tab: `SetFocused`/`ActivateTab` re-assert per-tab
  focus flags so background tabs are always blurred. `SetSize` sizes every tab,
  so switching never renders through a stale viewport.
- Message routing: `Update` targets the active tab; `UpdateForPath(path, skip,
  msg)` reaches every tab showing path (background tabs included) and
  `UpdateTab(i, msg)` a specific one — the seams path-routed messages (sync,
  highlight, LSP results, save-all) travel through.

### Terminal tabs (0350, #573)

- `AddTerminalTab(term)` appends a tab hosting a `terminal.Model` and activates
  it; `terminal.newTab` (Tools menu / palette) opens one in the active editor
  pane, falling back to the classic bottom-split terminal when no editor pane
  exists. Session keys are minted via `Registry.MintTerminalKey()` so tab
  sessions never collide with terminal panes.
- While a terminal tab is active, `Editor()` is **nil** — app code nil-checks —
  and `ActiveTerminal()` returns the hosted terminal; `ContextID()` flips to
  `terminal` so terminal keybindings and the raw-key routing
  (`terminalFocused`) apply exactly like in a terminal pane, mouse included
  (selection drag, wheel scrollback/child routing).
- Document sweeps (`Editors()`, `TabForPath`, autosave, backups, dirty guards)
  skip terminal tabs; `TabEditor(i)` returns nil for them, `TabTerminal(i)`
  the terminal.
- The tab bar labels a terminal tab `⌨ ` + its OSC title (else the shell's base
  name); no dirty/stale markers.
- Closing a terminal tab (middle-click, `editor.closeTab`) **ends its shell
  session**; a pane close, `Registry.Close`, and a project switch end every
  hosted session the same way. Terminal tabs are session-local: like scratch
  tabs they are not persisted, and layout restore drops them.

## Opening routes into the tab list (`internal/app`)

All open seams (explorer, palette `@`, find-in-path, `host.API`) already funnel
through `openPath`; it now lands the file in the focused pane's tab list via
`openInTab`:

- a tab already showing the file is **activated** (no duplicate tab) —
  `openPath`/`openPathAt` canonicalise the incoming path to its cleaned
  absolute form first (#272), so the explorer's absolute spelling and the
  palette's root-relative one land on the same tab;
- an **empty** scratch tab is **filled in place** (fresh panes keep the old
  feel). Empty means no file *and* no text — `editor.Model.IsEmpty`, the same
  predicate `Instance.IsEmptyEditor` and the diff path use (#628, #641). A
  pathless tab that already holds typed text is *not* reused, so its content
  is never clobbered;
- otherwise a **new tab is appended**, after autosaving the document being left
  (#174) — tab switches autosave the same way (`activateTab`).
- **Open-in-new-pane splits** — unless the active editor is an empty scratch
  pane, which is reused in place instead of stranding a blank pane beside the
  new one (#641), mirroring the diff viewer's `placeDiffLeaf` behavior.

Shared documents (#142) are reused verbatim: `loadOrShare` scans all tabs of all
panes, so one file open in two tabs — same pane or different panes — aliases one
buffer and one undo history. `editorKeysForPath` matches any tab;
`editorViewsForPath`/`editorForPath` resolve per-view editor models; sync
broadcasts skip only the originating tab, reaching background tabs of the same
pane.

## Closing peels tabs before the pane

`editor.closeTab` (cmd+w, `:q`, `CloseFocused`) closes the **active tab** and
only closes the pane when the last tab goes — single-tab panes feel exactly like
before. **Dirty buffers open the unsaved-changes guard first** (#259,
`internal/app/closeguard.go`): a floating-shell prompt offers `[s]` save then
close, `[d]` discard, `[esc]` cancel — same pattern as the project-switch
guard. A pane close checks every tab; documents still shown by another pane
(#142) close without a prompt (nothing is lost), `:q!` forces the close
vim-style, and a failed save (read-only file) keeps the tab open with an
error toast. **App quit runs through the same guard** (#287): `q` (normal-mode
editor or explorer focus) and `ctrl+c` collect every dirty document across all
panes (`guardedQuit`/`dirtyEverywhere`, deduped by path — shared documents
count once) and prompt `[s]` save all then quit, `[d]` discard and quit,
`[esc]` cancel; a failed write keeps the app running. A closing tab drops its crash-backup snapshot (#165) unless
another tab or pane still shows the document. Externally deleted files close
their tab (the pane survives while other tabs remain); moved files re-path
every tab that holds them.

## Tab bar rendering (#157)

The bar occupies the **pane's top row** — the same line the single-document
title used (`internal/app/tabbar.go`, hooked in `renderPane`), so showing tabs
costs no editor row. With one tab the classic title renders; with two or more
(or `editor.tabs.always_show = true` under `[editor.tabs]`, live-reloadable,
with a settings-page toggle) the tab list does:

- **Labels**: file basename (`untitled` for scratch tabs); duplicates
  disambiguate with the display-relative directory (`main.go — cmd/ike`); a
  dirty tab carries ` ●`, a stale one `!` (file changed on disk while dirty,
  0140 — externally deleted dirty files surface the same way).
- **Highlighting** reuses theme slots: the active tab renders `Accent` + bold,
  inactive tabs `Foreground`, separators (`│`) and end ellipses `Border`.
- **Overflow** never wraps: `tabWindow` grows a run of tabs around the active
  one (rightward, then leftward) while separators and end ellipses still fit;
  hidden tabs are marked with `…` at that end, and a lone oversized active
  label truncates.

## Commands & keybindings (#158)

Tab operations are registered `Command` capabilities (`internal/app/commands.go`,
handlers in `internal/app/tabs.go`), so the palette, the cheatsheet and the
keymap all see them; they act on the focused editor pane (else the most recent
one):

| Command | Default chord | Behaviour |
|---|---|---|
| `editor.tab.next` / `editor.tab.prev` | `ctrl+cmd+right` / `ctrl+cmd+left` (also `ctrl+alt+right` / `ctrl+alt+left`) — JetBrains' macOS keymap; palette is the delivered fallback | cycle the active tab, wrapping |
| `editor.tab.select1…9` | `alt+1`…`alt+9` | jump straight to tab N |
| `editor.tab.moveLeft` / `editor.tab.moveRight` | `ctrl+shift+pgup/pgdown` | reorder the active tab |
| `editor.tab.reopenClosed` | `cmd+shift+t` (JetBrains) / `alt+shift+t` | pop the reopen ring |
| `editor.closeTab` | `cmd+w` / `ctrl+w` / `:q` | close the active tab, the pane on its last tab |

Tab cycling now mirrors JetBrains' macOS keymap export: `ctrl+cmd+arrow`
primaries with `ctrl+alt+arrow` secondaries. These Cmd/Option chords only reach
a TUI in a terminal that forwards the modifiers (Ghostty with the Kitty
protocol) — accepted per user preference; the palette is the delivered fallback.
Reorder stays on the `ctrl+shift+pgup/pgdown` page keys, whose modifiers survive
the legacy CSI encoding everywhere. Select-tab digits sit identically on QWERTZ
(layout-safe), and tab cycling stays distinct from the `ctrl+tab` pane switcher.

The **reopen ring** keeps the last 10 closed tabs (path + caret), fed by both
tab closes and pane closes; `editor.tab.reopenClosed` pops entries, skipping
files deleted since, and restores the caret via the standard open flow. A
"Reopen Closed Tab" item joins the File menu.

## Mouse on the bar (#159)

`tabAt` mirrors the renderer's geometry exactly (window, padding, separators,
ellipses), so clicks land on what is drawn; `tabBarHit` maps an absolute cell
to (pane, tab) — only on the bar row of editor panes actually showing a bar.

- **Left-click** on an inactive segment focuses the pane and activates that
  tab. The active tab's own segment — and the row outside the segments —
  still starts a **pane move**, keeping the title row as the drag handle.
- **Middle-click** closes the clicked tab with the same guard as
  `editor.closeTab` (backup snapshot kept while the document is open
  elsewhere; the reopen ring is fed); a single-tab pane closes entirely.
- **Wheel** over the bar row cycles tabs (up = previous, down = next) instead
  of scrolling the viewport; below the bar the wheel scrolls as before.

## Session persistence (#160)

The layout store's per-leaf identity table (`internal/app/store.go`) grows the
tab list: `tabs` holds every file-backed tab's path in order, `active` indexes
the active one within that list; `path` stays the active tab's file so older
builds keep working. Scratch tabs are not persisted — their unsaved text is
the crash-recovery side's job (#165).

Restore (`restoreLayout`) rebuilds each pane's tab list tolerantly: identities
without `tabs` (pre-#160 files) restore as single-tab panes; files missing on
disk are skipped without leaving an empty tab (the saved active index maps to
the surviving tab); a pane whose every file vanished restores as one scratch
tab. The same file across several tabs or panes restores as one shared
document (#142). `session.json` is unchanged — it still frames the focused
editor's cursor/scroll, which lands on the restored active tab.

## Open ends

Drag-reorder on the bar may follow later (keyboard reorder shipped with #158);
the reopen ring stays session-local.
