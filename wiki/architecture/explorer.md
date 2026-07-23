---
type: concept
title: File Explorer
description: Expandable file-tree pane rooted at a fixed project base that emits an open-file message.
resource: internal/explorer/explorer.go
tags: [architecture, explorer, tree]
timestamp: 2026-07-23T19:00:00Z
---

# File Explorer

`explorer.Model` shows the project as an expandable tree of `node`s rooted at a
**fixed base** (`explorer.New(".")` — the working directory). The root is never
replaced and the explorer never ascends above it. The root node is expanded on
startup; directory children are read lazily the first time a node is expanded,
sorted directories-first then alphabetically.

Scans never block the update loop: expanding a directory dispatches a `scanCmd`
`tea.Cmd` that reads the directory off-thread and returns a `ScanDoneMsg`;
`applyScan` then installs the children and rebuilds. `Init` kicks off the root
scan, so the tree is empty for one frame and fills in on the first message.
Re-scans **merge**: `setChildren` reuses existing child nodes (matched by path
and kind), so a refresh preserves expansion state and already-loaded subtrees
instead of collapsing everything.

## Auto-refresh

The tree keeps itself in sync with the filesystem without a manual `r`
(which stays as the escape hatch). Two mechanisms feed it:

**Watcher events (Roadmap 0140, #83).** The `internal/watch` service registers
one fsnotify watch per directory under the project root, but prunes
dot-directories (`.git`, `.venv`, `.tox`, caches) and a small deny-list of
vendored/noise names (`node_modules`, `__pycache__`, `site-packages`, `vendor`)
from the walk — and from the mid-session auto-watch of newly-created dirs, so a
`pip install` into `.venv` does not start thousands of watches (`skipWatchDir`,
#596). Without this a large Python project registered one watch per directory
across a populated `.venv`, exhausting file descriptors and flooding the event
loop. The root model routes the file
watcher's `watch.EventMsg{Kind: DirChanged}` to the explorer;
`externalRefresh` re-scans just the affected directory — not a full re-scan.
The `setChildren` merge preserves expansion state and loaded subtrees, and
`pendingSel` keeps the cursor on its entry across the rebuild (even when rows
above it vanish). Absent, never-loaded, or already-scanning nodes are skipped
(a collapsed directory picks changes up when first expanded); the hidden-files
filter applies as always at `rebuild`. Files deleted externally close their
editor pane like the explorer's own delete flow — unless the buffer is dirty,
in which case it survives, marked stale (see [editor](./editor.md)).

**mtime polling (fallback).** For filesystems where fsnotify under-reports:
each scan records the directory's mtime on its node; a poll loop (`schedulePoll`)
snapshots the mtimes of every visible loaded directory, sleeps
`pollEvery` (2s) off-thread, re-stats them, and reports drift as a `pollMsg`.
`applyPoll` re-scans only the changed directories (merging in place) and
schedules the next tick. A vanished directory reports its parent instead, so
external deletes fold away cleanly. The loop starts on the first `ScanDoneMsg`
(`startPoll`, guarded by `polling` so only one loop ever runs) — or is armed by
`Restore`, whose synchronous load means no scan message would ever arrive, and
started by `Init`. `explorer.auto_refresh = "false"` disables it.

The visible tree is flattened into `rows` (rebuilt on every expand/collapse) for
cursor navigation; each node carries its `depth` for indentation.

## Configuration

`Configure(host.Config)` applies the merged `[explorer]` section (owned by the
`internal/config` schema, Roadmap 0040) before the first render:

| key | meaning |
| --- | --- |
| `explorer.show_hidden` | initial visibility of dot-entries (toggleable at runtime) |
| `explorer.tree_indent` | spaces per depth level (indent-guide width) |
| `explorer.sort` | within-level ordering (`name`); directories are always first |
| `explorer.colors.<ext\|glob>` | per-filetype colour; `dir` and `default` are required fallbacks |
| `explorer.auto_refresh` | poll for external filesystem changes (default `true`; `"false"` disables) |

Colours (`colors.go`) resolve a node by checking, in order: an exact **glob**
match (globs sorted for determinism), the `dir` fallback for directories, a bare
**extension** match, then `default`. Values are colour names (`blue`, `cyan`,
`gray`, …), hex (`#1f6feb`), or raw ANSI indices. When no `[explorer.colors]` is
configured, a built-in default table is used so the tree is never monochrome.

## Navigation

- `j` / `k` / arrows — move the cursor over visible rows.
- `enter` — toggle a directory (expand/collapse) in place, or open a file
  (emits `OpenFileMsg{Path}`).
- `l` / `right` — expand a collapsed directory, step into the first child of an
  expanded one, or open a file.
- `h` / `left` — collapse an expanded directory, otherwise jump to the parent
  node. Never moves above the root.

Directories render with a `▾`/`▸` marker; a read error is retained and shown in
place of the tree.

## Mouse

The root model forwards mouse events that land in the explorer pane, translating
the absolute cell into the tree's content-local space (inside the pane border,
padding, and title row) before calling the explorer:

- **Left press** on a row (`MouseClick`) only **selects** it. Activating —
  opening a file or toggling a directory, mirroring `enter` — takes a
  **double-click** (two presses on the same row within `doubleClickWindow`,
  400ms; the clock is injectable via `Model.now` for tests). Exception: a
  single press on a directory's two-cell expand caret toggles it immediately,
  like the IDE tree it mimics.
- **Motion** over a row (`SetHoverAt` / `ClearHover`) sets a transient hover
  highlight; leaving the pane clears it.
- **Wheel** over the pane scrolls without moving the cursor, like a real
  scrollbar: vertical by default (`ScrollBy`), horizontal with **shift** held or
  the wheel's own left/right buttons (`ScrollXBy`), `wheelLines` per notch.
- **Left press** on a scrollbar track jumps that axis proportionally.

## Git status colouring

Epic 0320 layers git status over the per-filetype colours: entries render in
the new theme VCS slots — modified, added, untracked, conflicted — and a
directory containing changes tints with the modified colour so pending work is
visible on collapsed subtrees. The app threads each vcs status snapshot into
the tree via `SetVCS`; outside a git repository nothing changes. See
[VCS / Git Integration](/architecture/vcs.md).

## Row highlighting

A row's **base** style is the plain foreground (#1051, suffix-tint model): the
colour channel belongs to the **VCS status** — a changed file reads entirely in
its status hue, JetBrains-style — directories take their subtree's dominant status (#1053), so an untracked-only folder reads untracked, not modified — and carries a one-cell status letter
(`M`/`R`/`A`/`U`/`D`/`C`) at the row's right edge as a non-colour cue for
ANSI256 terminals and colour-blind users. On **clean files** only the extension
suffix takes the filetype colour (`colors.suffixColor`, resolved from the
`[explorer.colors]` ext/glob keys; the legacy `dir`/`default` keys are accepted
but no longer paint rows — directories stay uncoloured, caret + `/` carry the
distinction). Hidden (dot-prefixed) entries add italics. `rowKind` then classifies how the
row is highlighted, strongest first: the focused **cursor** (Selection
background + bold over the row's semantic foreground, #1052 — git status
stays readable while cursoring, matching the structure/problems/VCS lists;
while the pane is unfocused the cursor row keeps a muted `SelectionMuted`
background instead of vanishing, #1034) → the mouse **hover** (adds the grey
background only, preserving
the row's semantic foreground — the active-file accent included, #1056) → the
**open file** (`activeStyle`, a muted warm accent, deliberately not bold — the
**focused editor's** file: `app.setFocus` calls `SetActive` whenever focus lands
on an editor pane, so the accent follows pane clicks and focus cycling; it is
cleared when the file closes) → otherwise the base style (directory or plain
file colour). The classification lives in `rowKind` so it is testable independent
of the terminal colour profile.

Indent guides render in the semantic `IndentGuide` palette slot (#1050,
mirroring the editor) over the row's background, and — with the expand
marker — stay un-bold under the cursor so the caret column keeps its metrics
(#1059). The `(empty)` placeholder uses the `InlayHint` slot instead of
terminal Faint (#1058).

Independently of `rowKind`, **every** file open in any editor pane renders its
**name underlined** (no italics — those stay reserved for hidden entries,
#1055; `rowParts` splits guides/marker/name so `View` styles them separately)
on top of whatever highlight the row carries. The app maintains
that set via `SetOpen` (`syncExplorerOpen` in `internal/app` collects each
editor pane's file after every open/close/restore); `SetOpen` also clears a
stale `active` mark whose file is no longer open.

## Commands

Every user action is a registry `Command` (scoped to the explorer context) with a
default `Keymap`; each only dispatches an explorer `Msg` that the root model
routes back into `Update`. The canonical binding set is owned by Roadmap 0080 —
these are defaults.

| command | default key | effect |
| --- | --- | --- |
| `explorer.toggleHidden` | `.` | show/hide dot-entries (`ToggleHiddenMsg`) |
| `explorer.refresh` | `r` | invalidate + re-scan the selected subtree (`RefreshMsg`) |
| `explorer.collapseAll` | `c` | fold the tree back to the root (`CollapseAllMsg`) |
| `explorer.reveal` | — | move the cursor to the open file (`RevealMsg`) |
| `explorer.newFile` | `a` | prompt for a name, create a file seeded with its [language template](./languages.md#file-templates-170), empty otherwise (`NewFileMsg`) |
| `explorer.newFolder` | `A` | prompt for a name, create a directory (`NewDirMsg`) |
| `explorer.delete` | `d` | delete the selected entry after confirmation (`DeleteMsg`) |
| `explorer.rename` | `R` | prompt (prefilled with the current name) to rename the selected entry (`RenameMsg`) |
| `explorer.undo` | `Ctrl+Z` | reverse the last file operation instantly (`UndoMsg`) |
| `explorer.redo` | `Ctrl+Shift+Z` / `Cmd+Shift+Z` | re-apply the last undone file operation (`RedoMsg`) |

`explorer.toggle` (global, `cmd+1`) is the JetBrains cmd+1 state
machine (#268, `internal/app/explorer_toggle.go`): a focused tree **hides**
(the layout leaf is removed, editors reclaim the width; the pane instance
stays registered so expansion/selection/scroll survive), a visible unfocused
tree gains focus, and a hidden tree comes back as the outer-left split at its
remembered ratio, focused. The hidden state persists in the layout store —
`restoreLayout` accepts a tree without the explorer leaf — so it survives a
restart; the next toggle brings the tree back.

Hidden files are filtered from `rows` unless `show_hidden` is on; toggling just
rebuilds (no re-scan), since all children — hidden included — are cached on the
node. The runtime `.` toggle is authoritative: `Configure` re-applies
`explorer.show_hidden` only when the config value actually changed since the last
call (tracked in `hiddenCfg`), so an unrelated live reload never clobbers it.
Toggling also emits `HiddenToggledMsg`, which the app persists to the session
immediately — the state survives a kill/crash, not only a clean quit (#629).
A genuine config edit persists the same way: after `panes.Reconfigure` the app
compares the explorer's `ShowingHidden()` before/after and saves the session
only when the value actually changed, so a settings-driven change also survives
a kill/crash while unrelated reloads never touch `session.json` (#642).

## File operations

`fileops.go` adds create / rename / delete / undo on top of navigation. Each step that
mutates the filesystem is gated behind a **modal prompt** (`Model.prompt`):
`promptInput` reads a filename (Enter accepts, Esc cancels), `promptConfirm`
reads a yes/no answer (`y`/Enter accept, anything else cancels). While a prompt
is open `Prompting()` is true, and the root model routes every key straight to
the explorer (ahead of the keymap and global layers) so typed names and answers
are not stolen by other bindings.

That routing only fires while the explorer pane holds focus, so a prompt-opening
op dispatched from elsewhere — the command palette with an editor focused —
first moves focus to the explorer (`focusExplorer` in
`internal/app/explorer_toggle.go`, re-showing a hidden tree via `showExplorer`)
before the message reaches `Update` (#374). Otherwise the typed filename would
execute as vim commands against the buffer.

A `promptInput`'s text carries a rune-index cursor (`prompt.pos`), not just
append/backspace at the end: `Left`/`Right` step it, `Home`/`End` jump it,
`Delete` removes forward, and typed text/`Backspace` act at `pos` rather than
always at the string's end (rename starts with `pos` at the end of the
prefilled name). The cursor cell itself is reverse-video (`promptCursorStyle`)
over the rune already there (a blank cell past the last rune), not an inserted
caret glyph — so it never shifts the surrounding text as it moves.

`View` overlays the box via `overlay.Place(out, m.promptBox(), bx, by,
m.width, m.height)` — the explorer's **own** `m.width`/`m.height` (its pane
content area), not the full terminal, since `out` here is the explorer's own
rendered tree. So the box is centered within the pane, not the screen. The box
always fits and always renders (#373): `promptBox` truncates the title to the
pane width (ellipsis) and horizontally windows the input row so the cursor cell
stays visible for long prefilled names; `Place` clips a box taller than the
pane instead of dropping it, so an active prompt can never capture keys
invisibly. Mouse clicks must land in the same content-local space `MouseClick`
uses: `promptBoxOrigin()` recomputes that centering math (origin clamped at 0)
with the model's own dimensions, and `PromptMouseClick(x, y)` maps a
content-local click on the input row to a `pos`, adding the input window's
scroll offset. The app computes those content-local coordinates itself
(pane rect + `paneContentX`/`paneContentY`, same as a normal pane click) and
routes mouse presses there instead of through the normal pane hit-test
whenever `explorerCapturing()` is true (explorer focused with a prompt open).

New entries are created next to the selection — inside the selected directory, or
beside the selected file. Deletes do not `os.Remove`; they move the entry into a
hidden, same-filesystem trash directory (`.ike-trash/` under the project root, so
the rename never crosses devices), which is what makes an undo able to restore
it. Completed operations are pushed onto a linear undo stack (`ops`) with a
matching redo stack (`redoOps`; a fresh operation clears it, like a text
editor's history):

- **Undo of a create** moves the entry to the trash (never `os.Remove`, so a
  redo — or a mistaken undo — loses nothing); redo moves it back.
- **Undo of a delete** moves the trashed entry back to its original path; redo
  re-trashes it.
- **Undo of a rename or move** relocates the entry back; redo re-applies it.

Because every direction is recoverable, undo and redo apply **instantly** — no
confirmation prompt (only `explorer.delete` still confirms). Rename
(`promptRename` / `renameEntry`) and move (`moveEntry`, #175) share one core,
`relocateEntry`: a single `os.Rename` from the old to the new path, guarded
against name collisions and against moving a folder into itself, recorded as
one `opRename` on the undo stack, with both affected parent directories
re-scanned. The root is never renameable or movable, mirroring delete.
Rename/move can also be requested for an explicit path (`RenamePathMsg`,
`MoveToMsg`) — the app's `file.rename` (shift+f6) and `file.move` (f6)
commands use these to act on the focused editor's file; the move target comes
from the palette's directory picker mode.

Removing a path (a delete, or undo of a create) emits `FileDeletedMsg`, which
the root model handles by closing any editor still open on that file (or, for
a directory, any file beneath it). Renames and moves instead emit
`FileMovedMsg{Old, New, IsDir}` (#175): the root model **re-points** every
editor on the old path (or under an old directory prefix) via
`editor.SetPath` — buffer, cursor and undo history survive; only the path
changes, highlighting reparses (the extension may select a new grammar), and
both ends are stamped as own writes so the watcher's echo of the rename never
marks the followed buffers stale. Unlike the other explorer messages, these
two are handled by the app, not routed back into the explorer, so they
deliberately do not implement `Msg`.

`Ctrl+Z` in the explorer context resolves to `explorer.undo`, and
`Ctrl+Shift+Z` (plus `Cmd+Shift+Z` where the terminal delivers it) to
`explorer.redo`, mirroring the editor's text undo/redo but operating on files.
After any operation the
affected directory is re-scanned (`refreshDir`) and `pendingSel` snaps the cursor
onto the new or restored entry once it reappears. This file-op undo stack is
entirely separate from the editor's text history.

## Scrolling & scrollbars

The explorer keeps a vertical (`offset`) and horizontal (`offsetX`) scroll
offset. `viewport()` resolves the inner text area, reserving a right column for
a vertical scrollbar and a bottom row for a horizontal one whenever the content
overflows the pane (two passes settle the mutual dependence). Rows are clipped
to the horizontal window with `ansi.Cut`, so long names and deep nesting scroll
sideways instead of wrapping.

Each bar is a dim track (`│` / `─`) with a brighter, heavier thumb (`┃` / `━`)
sized and positioned by `scrollThumb`, in the style of table TUIs. Bars are
hidden when the content fits.
