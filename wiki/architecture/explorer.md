---
type: concept
title: File Explorer
description: Expandable file-tree pane rooted at a fixed project base that emits an open-file message.
resource: internal/explorer/explorer.go
tags: [architecture, explorer, tree]
timestamp: 2026-09-01T00:00:00Z
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

**Resync (#1520).** `ResyncMsg` re-scans every expanded, loaded directory from
the root (`rescanSubtree(root)`) — the one-shot catch-up a workspace resume
sends, since the watcher is stopped while a workspace is parked and the poll
may be disabled. Unlike the user-facing `RefreshMsg` it ignores the selection
(no subtree narrowing) and keeps the multi-select. Every scan merge
additionally stability-snaps the cursor onto its entry (`applyScan` arms
`pendingSel` unless a deliberate snap is pending), so multi-directory rescans
cannot walk the selection off its entry one merge at a time.

The visible tree is flattened into `rows` (rebuilt on every expand/collapse) for
cursor navigation; each node carries its `depth` for indentation.

## Reveal (#1042)

`explorer.reveal` (`alt+f1`, palette) puts the cursor on the focused editor's
file, **expanding every collapsed ancestor** on the way and scrolling the row
into view — JetBrains' Select Opened File. Lazy loading makes the descent
async: `reveal` records the target in `pendingReveal` and `continueReveal`
walks from the root toward it, expanding loaded ancestors in place; the first
unloaded one dispatches its `scanCmd` and pauses the walk. Every landing scan
(`applyScan`) re-enters `continueReveal`, so each result resumes one level
deeper until the target row exists (select + scroll, state cleared). The loop
is bounded: a target that left the tree — deleted, renamed, outside the root,
or a scan error emptying an ancestor — abandons the reveal and clears the
state (`abandonReveal`); a target concealed by the hidden-files filter leaves
the cursor where it was.

With `explorer.auto_reveal = true` (default off) the reveal also fires
automatically whenever the focused editor's file changes — tab switches, pane
focus, opens — the JetBrains **autoscroll from source**. `SetActive`'s call
sites cannot dispatch Cmds, so a changed active path only *arms* the reveal
(`wantReveal`; the CLI open flow's `Reveal()` arms the same flag) and the
app's `Update` wrapper drains it once per settled pass via `PendingRevealCmd`,
mirroring the structure-view sync.

## Configuration

`Configure(host.Config)` applies the merged `[explorer]` section (owned by the
`internal/config` schema, Roadmap 0040) before the first render:

| key | meaning |
| --- | --- |
| `explorer.show_hidden` | initial visibility of dot-entries (toggleable at runtime) |
| `explorer.tree_indent` | spaces per depth level (indent-guide width) |
| `explorer.sort` | within-level ordering: `name` (default), `type` (extension, then name), `modified` (newest first) — directories always first; a live config change re-sorts the loaded tree (#1037) |
| `explorer.colors.<ext\|glob>` | per-filetype colour; `dir` and `default` are required fallbacks |
| `explorer.auto_refresh` | poll for external filesystem changes (default `true`; `"false"` disables) |
| `explorer.auto_reveal` | JetBrains "autoscroll from source" (#1042): reveal the focused editor's file (expand ancestors, select, scroll) on every focus/tab switch (default `false`) |
| `explorer.icons` | file-type marker glyphs (#1046): a one-cell class glyph between the expand marker and the name (default `false`) |
| `explorer.exclude` | exclude list (#1139): a TOML array of base-name glob patterns (`filepath.Match`: `.git`, `*.pyc`, `node_modules`) hidden at **every** depth, regardless of the show-hidden toggle — JetBrains' "Excluded files". Default: `[".git", ".idea", ".DS_Store"]`; an explicit empty list disables all exclusion. Editable on the settings panel's Explorer page (a `List` control: comma-separated text persisted as the TOML array); a live change re-filters without restart |

The exclude filter lives in the single visibility gate (`childVisible`, used
by `appendVisible` and `hasVisibleChildren`), so rows, expand markers (#1039),
speed search, multi-select and expand-all (#1043, which also skips excluded
subtrees rather than burn its scan budget invisibly) all see the same filtered
tree. It is **explorer-only**: go-to-file, find-in-path and the LSP scan walk
the filesystem themselves and never read explorer state. Malformed glob
patterns are dropped with a config diagnostic (`validate`); at match time a
pattern error simply never matches.

Colours (`colors.go`) resolve a node by checking, in order: an exact
**filename** key (`Makefile`, `Dockerfile`, `go.mod` — #1366), an exact
**glob** match (globs sorted for determinism), the `dir` fallback for
directories, a bare **extension** match, then `default`. Values are colour
names (`blue`, `cyan`, `gray`, …), hex (`#1f6feb`), or raw ANSI indices. When
no `[explorer.colors]` is configured, a built-in default table is used so the
tree is never monochrome. Every built-in theme's `Files` table is expanded
from a compact per-group spec (`internal/theme/files.go`) covering **all**
extensions and filenames the language plugins register; a drift test in
`cmd/ike` fails when a registered language lacks an entry.

## Navigation

- `j` / `k` / arrows — move the cursor over visible rows.
- `enter` — toggle a directory (expand/collapse) in place, or open a file
  (emits `OpenFileMsg{Path}`). The file lands as a **tab in the last-focused
  editor pane**, whatever its kind: a plain file as an editor tab, a data,
  image or archive file as a viewer content tab — never a split beside the
  editor (#1851, see [pane layout](./pane-layout.md)). `o` (`OpenFileMsg{…,
  NewPane: true}`) is the explicit split open.
- `l` / `right` — expand a collapsed directory, step into the first child of an
  expanded one, or open a file.
- `h` / `left` — collapse an expanded directory, otherwise jump to the parent
  node. Never moves above the root.
- `shift+j` / `shift+k` (and `shift+down` / `shift+up`) — extend a contiguous
  multi-select range from an anchor (#1044, see below).
- `space` — toggle the **selection mark** on the cursor row (#2166, see below).
- `m` / `y` — move / copy the selection into a target directory (#2166).
- `esc` — clear the multi-select: both the range and the marks.
- `/` — open the type-to-select **speed search** (#1087, see below).

## Speed search (#1087)

`/` with the tree focused opens a JetBrains-style type-to-select search. Bare
typing cannot activate it — the tree already spends its single letters on
file ops (`a`/`A`/`d`/`R`/`r`/`c`/`C`/`o`/`.`) — so `/` is the dedicated,
vim-idiomatic activation key (registered as `explorer.search`, rebindable;
the raw `/` in `Update` stays the zero-config fallback, and a palette
invocation moves focus to the tree first like the file-op prompts).

The search renders as a **one-line footer** on the pane's last row — the same
region the scan-error banner uses (#1030), never a modal box — mirroring the
editor's `/` command line: the slash prefix, the query with a block cursor
(`ui.CursorView`), and a dim match counter (`3/17`, or `no matches` in the
Error colour). It outranks the error banner while open.

Typing filters incrementally over the **currently visible rows** (no
auto-expansion): the cursor jumps to the row whose *name* contains the query,
case-insensitively — scanning forward with wrap-around from the stable anchor
(the row the search opened on), with **prefix matches ranked first** (a later
prefix match beats an earlier contains match). A miss leaves the cursor put
and flags `no matches`. The query is an ordinary single-line input
(`ui.EditKey`, #2002): the cursor moves with the arrows and word motions,
`opt`/`cmd`+backspace kill a word or the line, and a paste lands at the
cursor — `ctrl+n`/`ctrl+p` keep their meaning as match stepping. Every edit
re-resolves from the anchor; an
emptied query returns the cursor there.

Keys while open (`searchState`, `handleSearchKey` in `search.go`): the search
**owns the keyboard** — `Searching()` gives it the same raw-key capture the
file-op prompt gets via `Prompting()` (`explorerCapturing` in the app routes
keys ahead of the keymap layer), so the single-letter file-op bindings cannot
fire mid-word. `enter` accepts (cursor stays, search closes); `esc` cancels
(cursor returns to the anchor); `ctrl+n` / `down` step to the next match and
`ctrl+p` / `up` to the previous, both wrapping; every other non-printable key
is consumed without effect — no silent passthrough while the field is
visible. Mouse clicks still select rows normally (the prompt's
`PromptMouseClick` routing applies only to real prompts).

Matched rows (other than the cursor row, which keeps its full Selection
highlight) show the matching substring on the muted `SelectionMuted`
background — the multi-select recipe — so every candidate stays visible while
stepping. Non-ASCII names whose lowercase form changes byte length skip the
substring styling (the jump still works).

## Multi-select (#1044)

The explorer supports a **contiguous range selection** for file operations,
modeled as a single anchor row (`selAnchor`, `-1` = none): the selection is
always the visible-row range between the anchor and the cursor, in either
direction. `shift+j`/`shift+k` (and the shifted arrows) extend it — the first
extension anchors at the current cursor row — and a **shift+click** extends it
to the clicked row. Any plain motion or click collapses the range back to the
bare cursor; `esc` clears it explicitly. A right-click **inside** the range
keeps it untouched (cursor included), so the context menu's Delete acts on the
whole selection; outside it the selection collapses like a plain click.

Kept deliberately simple: row rebuilds only **clamp** the anchor to the row
set, while toggling hidden files or a manual refresh **collapses** the
selection outright (the row set shifts, so a stale range would cover the wrong
entries).

Visually, range members take the `rowRange` kind — the muted `SelectionMuted`
background over the row's semantic foreground, the same recipe as the
unfocused cursor — while the cursor row keeps the full Selection recipe, so it
reads as the range's active end. Range members outrank hover.

## Marked multi-select and bulk operations (#2166)

Beside the transient range sits a **sticky selection**: `space` toggles a mark
on the cursor row, and the mark stays until `esc` clears it or an operation
consumes it. The two models complement each other — the range is an "extend
from here" gesture that any plain motion collapses, marks are deliberate and
survive every motion, rebuild, sort change and hidden-files toggle.

Marks live in `Model.marks`, keyed by **absolute path**, never by row index, so
a rescan cannot silently re-point them at the wrong entries; `rebuild` prunes
only the marks whose entry left the tree (`pruneMarks`). The map is copied on
write, because `Model` travels by value and a shared map would leak marks into
stale copies. The root is never markable, and the Scratches section has no
multi-select at all (see below).

Visually, a marked row takes the `rowMarked` kind — the same muted
`SelectionMuted` recipe as a range member — and its **own indent-guide cell**
carries the mark: `guideCell` renders `✓` where the row's guide line `│` would
be, painted in the `Accent` slot so it never reads as a guide (#2380). The mark
therefore costs **no column of its own** — a marked row is exactly as wide as an
unmarked one, the tree never shifts, and clearing the last mark restores the
previous rendering exactly. A depth-0 row has no guide cell to borrow; there
the mark takes the expand marker's always-blank second cell, equally
width-neutral (unreachable through the UI, since the root is never markable,
but the rendering must not depend on that).

The earlier design prepended a two-cell marker column while anything was
marked. That changed every row's text without invalidating the memoized row
widths, so the renderer clipped and padded against widths two cells too small
and the terminal wrapped the whole pane (#2380); see *Content width memo*
below for the second half of that fix.

`opTargets` is the single resolution point every bulk-capable operation goes
through: the **marks** first, then an active **range**, then the plain **cursor
entry**. The first two report `bulk = true` (multi-entry wording, target list,
batched undo step); the last keeps the single-entry behaviour byte-for-byte
unchanged. In both multi-entry cases the root is dropped and entries nested
under another selected directory are filtered out — operating on the ancestor
already carries its subtree.

Three operations act on that selection:

- **Delete** (`d`) — one confirmation, titled `Delete N entries?` and **listing
  every target** project-relative (capped at 8 rows plus an "… and N more"
  line, `prompt.lines`). Entries are trashed individually.
- **Move** (`m`, `explorer.move`) — one target-directory prompt for the whole
  batch; each entry keeps its base name. An absolute input is taken as-is,
  anything else is read relative to the project root. The app's `file.move`
  (`f6`) picker feeds the same path: with marks present it sends
  `MoveManyMsg` instead of `MoveToMsg`. A **single** target still goes through
  `relocateEntry`, so it keeps the LSP `willRenameFiles` round trip (#1912),
  which is inherently one rename at a time.
- **Copy** (`y`, `explorer.copy`) — the same target-directory prompt, but the
  originals stay put. `copyTree` recurses into directories, preserves file
  modes and recreates symlinks as symlinks rather than following them. Each
  copy is recorded as an `opCreate`, so one undo trashes exactly the copies
  and leaves the sources alone; every created path is announced with
  `FileCreatedMsg` so the app refreshes the git status snapshot.

Whatever succeeds is recorded as **ONE undo step** (`fileOp.batch`), so a single
undo reverses the whole batch and a single redo re-applies it; a one-entry
batch is pushed as the plain operation it is. The selection **clears once the
operation has run** — it never outlives the action it was built for.

**Partial failure is expected, not exceptional.** A failing entry does not
abandon the rest of the batch: every other target is still processed,
everything that succeeded stays applied and undoable, and the failures are
reported together in the error dialog as `moved 1 of 2 — a.txt: already
exists` (`batchErr`). A half-written copy is removed, so a failed entry leaves
the destination exactly as it was.

Renaming stays single-target — it needs one new name per entry, which a
selection cannot supply.

Directories render with a `▾`/`▸` marker; a read error is retained and shown in
place of the tree.

## Root path context & file-type markers (#1046)

The **root row** shows more than the basename: a dimmed ` — ~/path` suffix
(the project root, home-abbreviated) renders in the `InlayHint` colour after
the name — JetBrains' project-path context, self-contained in the tree so no
status-line plumbing is needed. The suffix is pre-truncated (ellipsis) to the
pane width with one column reserved for a possible scrollbar, so it never
widens the content or triggers the horizontal scrollbar, and it is suppressed
entirely below 30 columns. It is part of `rowText` — the single source of
truth for row width — so clipping and scrollbars stay consistent.

With `explorer.icons = true` (default off) every row gains a **one-cell
file-type glyph** (plus a separator cell) between the expand marker and the
name (`icons.go`): directories `▪`, and files classified by extension into
code `◆`, doc `¶`, config `§`, image `▣`, other `·`. Directories get a glyph
too so names at one depth stay aligned. The glyphs are plain single-width
unicode — **no nerd font required** — and an ASCII-safe fallback set (`#`,
`*`, `"`, `=`, `%`, `-`) is kept alongside for a future capability probe or
override. Classification (`glyphClassOf`) is a small built-in extension map,
deliberately coarse — a handful of classes, not per-language icons.

## Mouse

The root model forwards mouse events that land in the explorer pane, translating
the absolute cell into the tree's content-local space (inside the pane border,
padding, and title row) before calling the explorer:

- **Left press** on a row (`MouseClick`) only **selects** it. Activating —
  opening a file or toggling a directory, mirroring `enter` — takes a
  **double-click** (two presses on the same row within `doubleClickWindow`,
  400ms; the clock is injectable via `Model.now` for tests). Exception: a
  single press on a directory's two-cell expand caret toggles it immediately,
  like the IDE tree it mimics. A **shift+click** instead extends the
  contiguous multi-select to the clicked row (#1044, `ShiftClick`); a plain
  click collapses any active range.
- **Motion** over a row (`SetHoverAt` / `ClearHover`) sets a transient hover
  highlight; leaving the pane clears it.
- **Wheel** over the pane scrolls without moving the cursor, like a real
  scrollbar: vertical by default (`ScrollBy`), horizontal with **shift** held or
  the wheel's own left/right buttons (`ScrollXBy`), `wheelLines` per notch.
- **`gg`/`G`** jump to top/bottom, **PageUp/PageDown** page, **`ctrl+u`/`ctrl+d`**
  half-page (#1032). **`C`** expands the selected subtree recursively
  (lazy levels load via continued scans, bounded at 200 directory scans,
  #1043); `c` stays collapse-all. Rows clipped at the right edge end in an
  ellipsis (#1035; a VCS status letter takes that cell instead).
- **Right-click** on a node selects it and opens a floating context menu
  (#1040, the #1020 `menu.Context` shell): New File/Directory, Rename,
  Delete, Refresh, Expand All, Reveal — entries dispatch the registered
  explorer commands, availability/shortcuts resolve like the menu bar.
- **Left press** on a scrollbar track jumps that axis proportionally; a press
  on the **vertical thumb grabs it** and dragging follows the pointer
  (#1036, `dragExplScroll`, mirroring the editor scrollbar #1022).

## Git status colouring

Epic 0320 layers git status over the per-filetype colours: entries render in
the theme VCS slots and a directory containing changes tints with its
subtree's dominant status, so pending work is visible on collapsed subtrees.
The palette follows the git workflow (#1868): **green** added/staged, **blue**
modified — including a file that was staged and then edited again — **violet**
untracked, **muted red** deleted, **red** conflicted. All five are muted tones
tuned per theme, mutually distinguishable and distinct from the plain
foreground, so an untracked file never reads like an open (underlined) one.
The app threads each vcs status snapshot into the tree via `SetVCS`; outside a
git repository nothing changes. See
[VCS / Git Integration](/architecture/vcs.md).

**Gitignored entries** render dimmed (#1045, JetBrains-style): the snapshot's
status command carries `--ignored`, so `! <path>` porcelain records (files, or
collapsed `dir/` entries for fully-ignored subtrees) land in an ignored set
queried via `Snapshot.Ignored` — a path under an ignored directory counts as
ignored. Dimmed rows take the plain foreground mixed halfway toward the
surface (`theme.Mix`); ignored ranks below every real VCS status and below the
untracked hue, the suffix tint does not apply (the row is uniformly dim), and
hidden-italic still composes.

## Errors (#1030)

File-operation errors (create/rename/move/delete/undo) open a **dismissable
dialog** over the intact tree — the project convention for actionable pane
states — with the message in the theme's Error colour; any key or click
dismisses and clears it. Scan/poll errors render as a themed one-line banner
on the pane's last row instead (a modal would re-open on every auto-refresh
poll); the next successful scan clears it. The tree is never replaced by raw
error text.

## Row highlighting

A row's **base** style is the plain foreground (#1051, suffix-tint model): the
colour channel belongs to the **VCS status** — a changed file reads entirely in
its status hue, JetBrains-style — directories take their subtree's dominant status (#1053), so an untracked-only folder reads untracked, not modified — and carries its status code
at the row's right edge as a non-colour cue for ANSI256 terminals and
colour-blind users. Files use the porcelain letters from the snapshot
(`Snapshot.Code`, #1868): `U` untracked, `A` staged, `M` modified in the
worktree, and the two-cell `AM`/`MM`/`RM` for a file that was staged and then
edited again — the code reserves as many cells at the right edge as it is
wide, and drops to the clipping ellipsis when the pane is too narrow for it.
Directories and synthetic snapshots without X/Y detail fall back to the
one-letter badge (`M`/`R`/`A`/`U`/`D`/`C`). On **clean files** only the extension
suffix takes the filetype colour (`colors.suffixColor`, resolved from the
`[explorer.colors]` ext/glob/filename keys; the legacy `dir`/`default` keys are accepted
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
(#1059). `rowParts` splits the row's **own** guide cell off from its ancestors'
so `View` can give it the `Accent` foreground when it carries a multi-select
mark (#2380). The `(empty)` placeholder uses the `InlayHint` slot instead of
terminal Faint (#1058).

Independently of `rowKind`, **every** file open in any editor pane renders its
**name underlined** (no italics — those stay reserved for hidden entries,
#1055; `rowParts` splits guides/marker/name so `View` styles them separately)
on top of whatever highlight the row carries. The app maintains
that set via `SetOpen` (`syncExplorerOpen` in `internal/app` collects each
editor pane's file after every open/close/restore); `SetOpen` also clears a
stale `active` mark whose file is no longer open.

## Commands

Tree navigation is registry-registered since #1041 (`explorer.cursorDown/Up`,
`top`, `bottom`, `pageDown/Up`, `open`, `expandOrOpen`, `collapseOrParent`,
`openInSplit`) with the raw keys documented as cheatsheet hints — rebindable
through the keymap layer, while the raw switch in `Update` stays the
zero-config fallback (a registered binding resolves first in the app's
keymap layer). `o` (open in split) is thereby documented and rebindable.

Every user action is a registry `Command` (scoped to the explorer context) with a
default `Keymap`; each only dispatches an explorer `Msg` that the root model
routes back into `Update`. The canonical binding set is owned by Roadmap 0080 —
these are defaults.

| command | default key | effect |
| --- | --- | --- |
| `explorer.toggleHidden` | `.` | show/hide dot-entries (`ToggleHiddenMsg`) |
| `explorer.refresh` | `r` | invalidate + re-scan the selected subtree (`RefreshMsg`) |
| `explorer.collapseAll` | `c` | fold the tree back to the root (`CollapseAllMsg`) |
| `explorer.reveal` | `alt+f1` (global) | reveal the open file: expand collapsed ancestors, select and scroll to its row (`RevealMsg`, #1042) |
| `explorer.newFile` | `a` | prompt for a name, create a file seeded with its [language template](./languages.md#file-templates-170), empty otherwise (`NewFileMsg`) |
| `explorer.newFolder` | `A` | prompt for a name, create a directory (`NewDirMsg`) |
| `explorer.delete` | `d` | delete the selected entry — or the whole multi-select (#1044, #2166) — after one confirmation listing every target (`DeleteMsg`) |
| `explorer.rename` | `R` | prompt (prefilled with the current name) to rename the selected entry (`RenameMsg`) |
| `explorer.move` | `m` | prompt for a target directory and move the selection there (`MoveSelectionMsg`, #2166) |
| `explorer.copy` | `y` | prompt for a target directory and copy the selection there (`CopySelectionMsg`, #2166) |
| `explorer.toggleMark` | `space` | toggle the selection mark on the cursor row (`ToggleMarkMsg`, #2166) |
| `explorer.clearMarks` | `esc` | clear the whole multi-select (`ClearMarksMsg`, #2166) |
| `explorer.search` | `/` | open the type-to-select speed search (`SearchMsg`, #1087) |
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
always at the string's end. The cursor cell itself is reverse-video
(`promptCursorStyle`) over the rune already there (a blank cell past the last
rune), not an inserted caret glyph — so it never shifts the surrounding text as
it moves. Every input prompt also renders an `enter accept · esc cancel` hint
line under the text (#1047), mirroring the confirm prompt's `[y]es  [n]o` and
the error notice's dismiss hint; like every prompt line it is truncated to the
pane width.

**Rename preselects the name stem** (#1047, JetBrains-style): the prompt opens
with the basename-without-extension marked as a selection
(`prompt.selStart`/`selEnd`, rendered on the theme's Selection/SelectionText
colours) and the cursor at its end. The first printable key replaces the whole
stem while the extension survives (`a.txt` + typing `new` → `new.txt`);
`Backspace`/`Delete` remove the stem; any other key — arrows, `Home`/`End`, a
mouse click — keeps the text, drops the selection, and edits normally from
there. Folders and dotfiles (extension-only names like `.gitignore`) preselect
the whole name.

`View` overlays the box via `overlay.Place(out, m.promptBox(), bx, by,
m.width, m.height)` — the explorer's **own** `m.width`/`m.height` (its pane
content area), not the full terminal, since `out` here is the explorer's own
rendered tree. So the box is placed within the pane, not the screen. The box
always fits and always renders (#373): `promptBox` truncates the title to the
pane width (ellipsis) and horizontally windows the input row so the cursor cell
stays visible for long prefilled names; `Place` clips a box taller than the
pane instead of dropping it, so an active prompt can never capture keys
invisibly.

**Delete and rename anchor their box to the affected row** (#1884) instead of
the pane centre, so the dialog stays visually attached to the entry it acts on.
`prompt.anchor` holds the affected entry's **path** — not a row index, since a
watcher rescan can renumber the rows while the prompt is open; a multi-select
delete anchors to the cursor row inside the range. `promptAnchorRow()` resolves
that path against the current `rows` and the live scroll offset, so the anchor
is the *visible* row. `promptBoxOrigin()` then opens the box directly **below**
that row, flips it **above** when the bottom edge is too close, and clamps the
result into the pane — the box is never partially off-screen. Horizontal
placement stays centered. Prompts without an anchor (new file/folder, the error
notice) keep the centered placement, as does an anchor whose row scrolled out
of view or vanished.

Mouse clicks must land in the same content-local space `MouseClick`
uses: `promptBoxOrigin()` is the single source of that placement math
(origin clamped at 0) with the model's own dimensions, and `PromptMouseClick(x, y)` maps a
content-local click on the input row to a `pos`, adding the input window's
scroll offset. The app computes those content-local coordinates itself
(pane rect + `paneContentX`/`paneContentY`, same as a normal pane click) and
routes mouse presses there instead of through the normal pane hit-test
whenever `explorerCapturing()` is true (explorer focused with a prompt open).

New entries are created next to the selection — inside the selected directory, or
beside the selected file. Deletes do not `os.Remove`; they move the entry into a
hidden, same-filesystem trash directory (`.ike/trash/` under the project root, so
the rename never crosses devices), which is what makes an undo able to restore
it. Completed operations are pushed onto a linear undo stack (`ops`) with a
matching redo stack (`redoOps`; a fresh operation clears it, like a text
editor's history):

- **Undo of a create** moves the entry to the trash (never `os.Remove`, so a
  redo — or a mistaken undo — loses nothing); redo moves it back.
- **Undo of a batch** (#1044, #2166 — a multi-select delete, move or copy)
  reverses every sub-operation in one step, in reverse order; redo re-applies
  them in the original order. The batch lives in `fileOp.batch` as plain
  per-entry sub-ops, all of one kind, and `undoBatch`/`redoBatch` dispatch on
  that kind: a delete restores from the trash, a move renames back, a copy's
  creations go to the trash.
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
sized and positioned by the shared `scrollbar.Thumb` (`internal/scrollbar`,
#1367), in the style of table TUIs. Bars are
hidden when the content fits.

**Per-row edge marks (#2377, `explorer/hscroll.go`).** The horizontal bar
answers "the tree is shifted"; the edge marks answer "*this row* is cut off,
and on which side". Each rendered row takes `‹` on its first cell while
`offsetX > 0` and `›` on its last while the row runs past the window, drawn
from the shared `internal/hscroll` package the editor and diff viewer use. The
right mark subsumes the older right-clip ellipsis (#1035) — same cell, same
meaning, now in the language every sideways-scrolling view speaks; `…` remains
the fallback while the marks are off (`ui.h_scroll_marks = false`). A VCS
status letter (#1051/#1868) keeps the cells it owns, so the marks work on the
window left of it. The marks overlay cells rather than adding them, so row
widths, the scrollbar column and the click-to-column mapping never move.

**Cursor-anchored clamping is intentional (#1140).** A wheel scroll
(`ScrollBy`) moves the viewport *without* the cursor, so the viewport clamp is
split in two: `clampOffset` only bounds the offset into `[0, rows − height]`
and runs from every content/geometry change — row rebuilds, watcher/poll
re-scans, VCS re-renders, config applies, `SetSize` — so a wheel-scrolled
viewport survives them while an offset past the last page still snaps back
(essential: mouse hit-testing reads the raw offset). `followCursor`
additionally pulls the window to the cursor and runs **only** where the cursor
genuinely moved: key navigation, speed-search jumps, reveal, mouse selection
(click / shift-click / context-click), `Restore`, and user-initiated
`pendingSel` snaps (`snapCursorTo`: file ops, the hidden toggle, the reveal
descent). `externalRefresh`'s stability snap — keeping the cursor on its entry
across a watcher rebuild — deliberately sets `pendingSel` without the follow
flag, so a background refresh never yanks the viewport back to an off-screen
selection.

### Content width memo (#1096, #2380)

`contentWidth` measures the widest visible row from `rowText` — the single
source of truth for clipping, padding, the horizontal scrollbar's geometry and
the mouse hit tests — and caches both the maximum and each row's own `n.rowW`
in `wcache`. `View` deliberately never re-parses the styled string, so a stale
cached width does not merely mis-scroll: it makes the renderer emit lines wider
than the pane, which the terminal then wraps.

The memo used to carry only a `valid` flag, invalidated by hand from `rebuild`,
`SetSize` and `Configure` under a *documented* assumption that every row-text
mutation funnels through those three. `toggleMark` broke it (#2380). The cache
now stores the **`widthKey` it was measured under** — the row-set epoch
(`rowsEpoch`, bumped by every `rebuild`), `indent`, the pane `width`, the mark
count and the `icons` flag, i.e. everything outside the row nodes that
`rowText` reads. A mismatch simply re-measures, so forgetting to invalidate can
no longer render a wrong width, and a new row-text input is a visible field to
add rather than an invariant to remember. `invalidateWidth` stays for the
inputs not worth fingerprinting (the exclude globs and colour table applied by
`Configure`).

## Scratches section (#1963)

`internal/explorer/scratches.go` docks the [scratch store](./scratch-files.md)
as a section at the pane's bottom edge, behind a horizontal divider
(`▾ Scratches ───`), replacing the #1932 tool pane. The section is attached by
`EnableScratches(dir, lister)` (called from `pane.newInstance`; a nil lister —
every plain `New` — means no section, so the bare tree is unchanged) and is
governed by `scratch.section` / `scratch.section_height` / `scratch.sort`
(Settings → Tools → *Scratch Files*).

**Geometry funnels through `viewport()`.** `scratchAreaRows` (divider + body;
body = min(height, content), floor-clamped so the tree keeps ≥3 rows) is
subtracted from the pane height there, so every tree consumer — scroll clamps,
mouse hit-tests, scrollbars, the prompt anchor — agrees on the tree's real
region without further special-casing. The section body scrolls internally
(`scrTop`) when the list outgrows it: the cursor pulls the window along
(`followScratchCursor`), and the wheel scrolls whichever region it sits over —
the app translates the pointer to a content-local row and calls `ScrollAt`,
which routes body rows to `ScratchScrollBy` and everything else (tree rows and
the divider) to the tree's `ScrollBy` (#1965). Like the tree, the wheel moves
the viewport without moving the cursor.

**One unified cursor.** Motions run over a virtual index space — tree rows
first, section entries after (`selCount`/`vcur`/`setVcur`) — so `j`/`k` step
across the divider, wrap-around passes both ends, `G` lands on the last
scratch and `gg` returns to the tree top. `scrCursor >= 0` marks the cursor as
being in the section; the tree's remembered row then renders as the muted idle
cursor. The section has **no multi-select** (deletes there are permanent, so
they stay one-at-a-time deliberate): a shifted motion moves plainly, and a
tree range never extends past the last tree row. Speed search stays a tree
affair — starting it exits the section.

**Same operations, different store.** `enter`/`l`/double-click open through
the standard funnel (`OpenFileMsg`), `o` opens in a split. `d` and `R` reuse
the fileops prompt machinery — the same anchored boxes (#1884) — but their
accepts call `scratch.Delete` / `scratch.Rename` (injectable via
`SetScratchOps` for tests) instead of the trash, then emit the standard
`FileDeletedMsg` / `FileMovedMsg` so the app closes or re-points tabs exactly
like a tree operation. `a` emits `ScratchNewMsg`, which the app routes to the
`scratch.new` language picker; `A` (new folder) is a no-op — the store is
flat. Rows sort by name (default) or `modified` newest-first, render with the
tree's highlight recipes (Selection/SelectionMuted/Panel, open-file underline,
suffix tint) plus a right-aligned **last-opened** age (#1965) — `ui.ShortAge`
("now", "5m", "3h", "7d", "6w") over the MRU store's last-opened time, which
the app pushes in with `SetScratchOpened` from `syncExplorerOpen`, falling
back to the file's mtime for a scratch the MRU never saw. The name field
shrinks to make room and clips with "…"; a pane too narrow to leave 8 columns
for the name drops the age instead. Rows refresh via `RefreshScratches` — called by the app on
scratch creation, by `r`, and by the poll loop: the scratch dir joins the
auto-refresh stamp set once it exists, so external changes surface like any
project-tree change.

**Divider gestures.** The app intercepts a left press on the divider
(`ScratchDividerHit`) before row clicks and starts a `dragScratchDiv` drag:
motion resizes the section (`ScratchDividerDrag`; dragging to the bottom edge
collapses), an unmoved release toggles the collapse
(`ScratchDividerRelease`). Both collapse state and dragged height persist
immediately with the explorer's session state (`State.ScratchCollapsed` /
`State.ScratchHeight` → `session.json`), while `scratch.section_height` only
seeds the height (apply-on-change, so a live config reload never clobbers a
drag — the #629 pattern).
