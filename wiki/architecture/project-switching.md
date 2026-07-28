---
type: concept
title: Project Switching
description: Roadmap 0090 — internal/project owns the switch flow end to end; recent-projects history, project.switch command, palette picker and the msg-driven re-root orchestration with an unsaved-changes guard.
resource: internal/project
tags: [architecture, project, history, switching, palette]
timestamp: 2026-07-28T00:00:00Z
---

# Project Switching (Roadmap 0090)

`internal/project` owns the "Switch Project" flow (spec: epic #37): the data
layer (#2), the command + picker (#12) and the switch orchestration (#3).

## Recent-projects history

- **Entry** (`entry.go`): `Path` (absolute, cleaned), `Name` (display name,
  default: base directory name), `LastOpened` (orders the list). Persisted as
  `[[project.history]]` with `last_opened` in RFC3339 UTC — the shape is fixed
  by `config.ProjectHistoryEntry`; the semantics live here.
- **Validation** (`validate.go`): `Validate(path)` expands `~`, resolves to an
  absolute cleaned path and rejects non-existent paths, non-directories and
  unlistable directories with actionable errors. Nothing is mutated on failure —
  a switch never happens partially.
- **Content rules** (`history.go`): on every successful open the root is
  upserted — moved (or added) to the front, deduped by path, capped at
  `project.max_history`. The finished list is handed to config's typed setter
  (`WriteKey`, list semantics: replace). History persists to the **user** layer:
  the list spans projects on this machine, so the project layer `DefaultScope`
  would pick is wrong for it.
- **Record on success only**: `RecordOpen` runs at startup (`cmd/ike/main.go`,
  before the model loads config — the initial open counts as an open) and, once
  #3 lands, after a completed switch. A failed validation returns the error and
  leaves the stored history untouched. `RecordOpenCmd` wraps it as a `tea.Cmd`
  so the Update loop never blocks on the stat or the write.
- **Restore last project** (#1000, `project.restore_last`, default off):
  before `RecordOpen`, startup asks `RestoreLastRoot` whether to re-anchor —
  with the setting on, no CLI target and no stdin buffer, the most recent
  history entry wins over the current directory and main `chdir`s there
  before anything loads (the whole IDE roots at "."). A history head equal to
  cwd or an empty history is a no-op; a deleted/invalid stored root falls
  back to cwd with an info toast. Explicit CLI targets always win, and two
  guards (#1010) keep the redirect honest: a cwd that is itself a project
  (`.git` or `.ike` marker) counts as an explicit target and never restores,
  and the home directory is never a restore target — one accidental `ike`
  in `~` would otherwise self-sustain at the history head (RecordOpen
  re-records the restored root) and point the recursive watcher at the
  whole home tree. The home directory is exempt from the marker rule
  (#1245): `~/.ike` is the user config directory, so the marker matches for
  everyone who ever wrote a setting and would suppress every restore started
  from `~` — dotfile repos make the same true of `~/.git`. Home is not a
  project on either side of the check.

## Project directory (#1348)

- **`[project] directory`** (default `~/IkeProjects`) is the default parent for
  projects IKE creates itself — JetBrains' `~/IdeaProjects`. It is *not* the
  place projects are opened from: opening and switching still take any path.
- `ProjectsDir` (`directory.go`) resolves the setting to an absolute, cleaned
  path: a leading `~` expands, a relative value resolves against the working
  directory, an empty value selects `~/IkeProjects`. The path need not exist.
- `EnsureDirectory` creates it (0755, parents included) **on demand** — only a
  feature that places a project there calls it, never startup. An existing
  directory is left alone; a file at the path is an error rather than a
  silently reused target.
- Settings UI: "Files & Session" → *Project directory* (user scope).

## Clone Repository (#1349)

JetBrains' *Get from VCS*: fetch a repository into the project directory and
open the result.

- **`project.clone`** ("Clone Repository…", global scope, no default chord —
  the chord budget is full, #711) dispatches `OpenCloneMsg`; reachable from the
  palette and File → *Clone Repository…*.
- **Dialog** (`internal/app/clone_prompt.go`) — two fields, the shell-prompt
  pattern: *Repository URL* and *Directory name*. The name follows the URL
  (`vcs.CloneName`: last segment, `.git` stripped, ssh/https/file forms) until
  it is typed by hand; `tab` switches fields, `enter` clones, `esc` closes.
  The dialog shows the resolved target (`<project directory>/<name>`).
- **Target rules** (`project.CloneTarget`): the name must be a single path
  segment, the project directory is created on demand (`EnsureDirectory`), and
  an existing target is refused — no clone lands inside an unrelated checkout.
- **The clone** (`vcs.CloneCmd`, `internal/vcs/clone.go`) is one async
  `tea.Cmd`, so the UI keeps rendering; it has its own 30-minute timeout
  instead of the 5s `gitTimeout` and runs with `GIT_TERMINAL_PROMPT=0` (no
  terminal is attached, so a credential prompt would hang forever — a private
  repository fails fast with git's own message). A failed clone removes the
  directory git created, so retrying the same name works.
- **Outcome**: success closes the dialog, toasts and hands the path to
  `SwitchTo` — the clone is opened through the regular switch transaction
  (unsaved-changes guard, history recording). A failure keeps the dialog open
  and editable with the decisive git line. If the dialog was dismissed while
  the clone ran, the outcome only toasts — the IDE is never re-rooted under
  the user.

## Command & picker (#12)

- **`project.switch`** (`command.go`): a compile-in plugin (id `project`)
  registering the command (global scope, title "Switch Project…") plus a
  default Keymap slot on `cmd+shift+p` — JetBrains' Recent Projects popup
  (macOS keymap export), with `ctrl+shift+p` as the delivered secondary; the
  canonical chord is owned by Roadmap 0080/0081. The command only dispatches
  `OpenPickerMsg`; the root model opens the palette locked to the picker mode.
  The File menu's "Switch Project" entry resolves against the same command id.
- **Picker** (`picker.go`): a palette `Mode` (prefix `#`, always opened
  locked) reusing Roadmap 0070's overlay/fuzzy list. Items are the history
  entries — fuzzy-matched on display name, falling back to the path; an empty
  query lists all, newest first — plus an `Open "<query>"…` affordance for a
  typed path outside the history. A **path-shaped** query (`/…`, `~/…`,
  `./…`, `../…`) browses the filesystem (#542): matching directories (via the
  shared `internal/pathcomplete` engine, dirs-only) render as selectable
  `Open <dir>` items ahead of the raw affordance, and `tab` extends the query
  to the longest unambiguous prefix — a single match completes with its
  trailing separator, so repeated tab descends (`~/Dev` → `~/Development/`).
  The tab plumbing is a palette-level seam: modes opt in by implementing
  `palette.Completer`. Entry details render through `compactPath`
  (home → `~`, middle-ellipsis) so long roots never crowd out the title.
  Activation emits `PickedMsg{Path}`, which the root model turns into the
  switch transaction below. Every row carries a **relative last-opened
  time** (`RelTime`, now implemented in `ui.RelTime`: "just now", "5m ago",
  … "3w ago"; empty for legacy entries without a timestamp, #842). Since
  #1114 the time renders **right-aligned** in the palette's `Item.Time`
  column — clearly separated from the name and from the `✕` control; narrow
  rows truncate the name (ellipsis) while the time and `✕` stay intact, and
  below a minimum name width the time drops. In-memory workspaces (#820)
  keep the `●` badge next to the name and their aux action (`shift+delete` /
  the `✕` zone) closes the workspace; unloaded entries' aux action instead
  **removes the entry from the history** (`RemoveFromHistoryMsg` → off-loop
  `RemoveFromHistory` write-back at user scope → config reload → the
  still-open palette re-lists). The Recent Projects column of the Recent
  Files dialog (#778) carries the same time column and removal action, and
  the Recent Files rows themselves gained the matching layout and prune
  action in #1113. `cmd+shift+p` is also in the JetBrains chord table
  (`internal/keymap/defaults.go`): the chord layer resolves modified chords
  even in a capturing editor, which the registry keymap layer does not.

## Switch orchestration (#3)

The switch is one msg-driven transaction; `internal/project` never mutates a
subsystem (it must not import editor/explorer), the root model routes:

1. `SwitchTo(path)` (`switch.go`) validates off the Update loop and yields
   `SwitchProjectMsg{Root}` (absolute) or `SwitchFailedMsg{Path, Err}` — a
   failure toasts and changes nothing.
2. The root model (`internal/app/switch.go`): the current root is a friendly
   no-op; dirty buffers emit `UnsavedChangesMsg{Root}`, which opens the
   **unsaved-changes guard** in the floating shell — `[s]` save all then
   switch, `[d]` discard and switch, `[esc]` cancel (project untouched).
   The prompt renders the root through `CompactPath`: the shell drops a box
   wider than the terminal, which a raw absolute root can force.
3. `performSwitch` re-roots: persist the old project's session + layout, stop
   the watcher, `os.Chdir(root)` (the whole IDE — explorer, config discovery,
   session/layout stores, search, watcher — is anchored at "."), then rebuild
   the model through the fresh-start path (`newWithHost`) with the **live
   host** carried over, so the program sender and the LSP bridge's editor
   emitter survive. The new project's layout/session restore exactly like a
   normal launch; the watcher restarts on the new root.
4. Afterwards `RecordOpenCmd` writes the history (success only) and
   `SwitchedMsg` toasts; the recorded write triggers a config reload so the
   picker's in-memory history is already current.

**Settings scope (0380, #795).** The config reload inside `performSwitch`
runs after the chdir, so the incoming project's `.ike/settings.toml` layer
applies and the outgoing project's overrides drop in the same step — theme,
keymap, editor behavior and the other consumers re-apply through the normal
`reloadConfig` path, no restart. Load diagnostics of the incoming layer
toast like any reload (#793). While a project is open, the file watcher
holds a dedicated watch on `<root>/.ike` (created late or present from the
start): an external edit of `settings.toml` surfaces as
`watch.ConfigChanged` and re-runs the reload pipeline; the sibling state
stores (layout/session/usage) stay silent.
