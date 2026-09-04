---
type: concept
title: Project Switching
description: Roadmap 0090 — internal/project owns the switch flow end to end; recent-projects history, project.switch command, palette picker and the msg-driven re-root orchestration with an unsaved-changes guard.
resource: internal/project
tags: [architecture, project, history, switching, palette]
timestamp: 2026-09-04T00:00:00Z
---

# Project Switching (Roadmap 0090)

`internal/project` owns the "Switch Project" flow (spec: epic #37): the data
layer (#2), the command + picker (#12) and the switch orchestration (#3).

## Recent-projects history

- **Entry** (`entry.go`): `Path` (absolute, cleaned), `Name` (display name,
  default: base directory name), `LastOpened` (orders the list), and
  `Remotes` (#2396: the repository's canonical git-remote keys,
  `host/owner/repo`, read from the checkout's git config on every recorded
  open — the index [ike:// deep links](/architecture/deep-links.md) resolve
  against; worktrees resolve through gitdir/commondir). Persisted as
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
  Bracketed paste and Cmd+V insert into the focused field at its cursor
  (`overlayCapturesKeyboard`/`routeOverlayPaste`, #1273 pattern) — a paste
  into the URL field re-derives the name like typing does. While the name
  merely follows the URL it renders dimmed (`cloneGhostStyle`, faint) rather
  than as live text, so typing the URL no longer reads as writing into both
  fields at once (#1873); the save-as, new-project, save-layout and
  JetBrains-import prompts take the same paste routing.
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

## New Project (#1718)

JetBrains' *New Project* wizard: pick a project type and toolchain, name the
directory, and open the scaffolded result.

- **`project.new`** ("New Project…", global scope, no default chord — the
  chord budget is full, #711) dispatches `OpenNewProjectMsg`; reachable from
  the palette and File → *New Project…*.
- **Wizard** (`internal/app/newproject_prompt.go`) — a three-step shell
  dialog. Step 1 lists the **project types**: **Plain** first (#1721 — a
  registry-independent entry, not a language), then the registered languages
  whose toolchain implements `lang.ProjectScaffolder`
  ([Language Registry](./languages.md)). Step 2 lists that language's
  **toolchain options** (`lang.ProjectOptions`) — for Python the guided venv
  choice between uv and pip/venv; unavailable options render disabled with
  the reason, never hidden. Step 3 is the **directory name** with the
  resolved target shown; `enter` advances/creates, `esc` steps back (from the
  first step: closes).
- **Plain** (#1721) skips step 2 in both directions: `enter` on the type goes
  straight to the name, `esc` from the name returns to the type list. Its
  create only makes the directory — no toolchain, no scaffolding — so the
  wizard also works in a build where no language offers scaffolding.
- **Target rules**: `project.Target` — the same resolution as a clone (single
  path segment under the project directory, created on demand, existing
  targets refused). `CloneTarget` is now a thin wrapper over it.
- **The create** is one async `tea.Cmd`: make the directory, run the
  language's `ScaffoldProject` (subprocesses like `uv init`/`uv sync`,
  `python -m venv`, `go mod init` are fine there). A failed scaffold removes
  the partial directory — the name can be retried — and keeps the wizard
  open with the tool's reason.
- **Outcome**: success closes the wizard, toasts and hands the path to
  `SwitchTo` — the fresh project opens through the regular switch
  transaction. If the wizard was dismissed while the scaffold ran, the
  outcome only toasts.

## Switch to last project (#2398)

Telemetry showed project switching is a **ping-pong**: most switches bounce
between the same two projects, and users reached them through the recent-files
palette (`cmd+e`) instead of the picker. One chord removes the detour.

- **`project.switchLast`** ("Switch to last project", global scope, default
  chord `cmd+shift+e` with `ctrl+shift+e` as the delivered secondary — the
  project.switch pattern, and deliberately next to `cmd+e`) dispatches
  `SwitchLastMsg`; also reachable from the palette. The chord is on the #805
  terminal allowlist (`terminalGlobalCommands`), like the other project-level
  entry points.
- **The pick** (`handleSwitchLastProject`, `internal/app/switch.go`) is the
  **last element of `m.ws.Background()`** — the most recently used parked
  workspace, the same pick `project.close` uses. Since the project just left
  becomes the new MRU parked one, invoking the command again switches straight
  back: an alt+tab between two projects.
- **A normal switch, not a peek**: the request is routed through
  `handleSwitchProject`, so the recent-projects history records the open and
  the auto-save gate (#2186) runs exactly as for a palette-driven switch. The
  resumed workspace comes back with its tabs, cursors and running terminals
  intact (#777).
- **No background workspace**: nothing changes and a `no previous project`
  notification says why.

## Direct MRU switching by digit (#2489)

Three days of telemetry counted 183 project switches, 115 of them through the
recent-files palette; 28 of 61 recorded stays were shorter than 30 seconds.
`project.switchLast` removed the dialog for *one* target — these chords remove
it for the nine most recent ones.

- **`project.switchMRU1` … `project.switchMRU9`** ("Switch to Recent Project
  N", global scope, registered in `internal/app/commands.go` next to the
  `pane.focusN` family) dispatch `SwitchProjectMRUMsg{Index}`. Default chords
  are `ctrl+alt+1` … `ctrl+alt+9`: the only digit family free on **both**
  platforms — `cmd+digit` is the tool windows, `alt+digit` the editor tabs,
  `ctrl+digit` the macOS-only pane numbers (#2407, and off macOS the Cmd→Ctrl
  fold puts the tool windows there), `cmd+alt+0` the time report. The chords
  are spelled with a literal Ctrl, so they ship identically everywhere, and
  they are on the #805 terminal allowlist like the other project entry points:
  the hop is usually made while looking at a shell.
- **One numbering, three renderings** (`internal/project/mru.go`):
  `MRUTargets(history, current)` is the recent-projects history in MRU order
  with the project one is standing in dropped, and `MRUHint(i)` labels its
  first `MaxMRU` (9) entries "1"…"9". The handler resolves against that list,
  and both project lists render the same digit as the row's `Item.Hint` — the
  picker's rows (`picker.go`) and the Recent Projects column of the
  recent-files dialog (`app.go`'s injected items). The digit is the entry's
  rank in the *unfiltered* history, not its row number, so neither a typed
  query nor the column's frecency ranking (#2399) ever renumbers a project:
  `ctrl+alt+4` always means the same one.
- **Number one is `project.switchLast`'s target**: the history's newest entry
  after the current project is dropped is the project one came from, which is
  also the MRU parked workspace. The digits simply generalize that toggle.
  (They part company only after `project.close`, which leaves the closed
  project in the history but not in the workspace set — the list is
  deliberately "recent", not "still open".)
- **The switch is the ordinary transaction**: `handleSwitchMRUProject`
  (`internal/app/project_mru.go`) hands the root to `project.SwitchTo`, so it
  is validated off the Update loop — a history entry whose directory has since
  vanished reports the usual switch failure — and then runs through
  `handleSwitchProject`/`performSwitch` like a palette pick: the auto-save gate
  (#2186), the history record, and a still-parked workspace resuming with its
  tabs and terminals (#777).
- **A digit past the end of the list** changes nothing and notifies
  `no recent project N`: the chord family is fixed at nine, the list is not.

## Close Project (#1355)

The inverse of a quick visit: instead of switching back and closing the
visited workspace from the list (#820), one action closes the **current**
project.

- **`project.close`** ("Close Project", global scope, default chord
  `cmd+shift+w` with `ctrl+shift+w` as the delivered secondary, #1358 —
  the project.switch pattern) dispatches `CloseProjectMsg`; also reachable
  from the palette and File → *Close Project*. The chord is on the #805
  terminal allowlist (`terminalGlobalCommands`), so it escapes a focused
  live terminal or tool pane like the other IDE-level entry points (#1360).
- **With background workspaces** (`internal/app/project_close.go`): the
  active workspace closes — session + layout persist first (reopening later
  restores), terminals/runs/debug tear down like a close-from-list
  (#820/#825) — and the **most recently used** parked workspace resumes in
  place through the seamless-switch path (#777). The recent-projects history
  entry stays.
- **Busy guard**: a dirty or running active workspace prompts first with the
  #821 shape — `[s]` save all then close (offered only when buffers are
  dirty; a failed write keeps the project open), `[d]` close discarding,
  `[esc]` cancel. `[enter]` confirms the primary option (#1356).
- **Last project**: with no background workspace the request degrades to an
  app quit through the existing quit guard (#287/#821).
- Mechanically the close is `performSwitch` to the MRU root (which parks the
  closing workspace) followed by `Drop` + teardown of exactly that parked
  unit — a failed switch (chdir error) parks nothing and closes nothing.

## Quick-peek switch (#2136)

A ten-second look-up in another project without the three-step dance (switch,
switch back, close from the list): open a project **temporarily**, return with
one action that also unloads it.

- **Enter**: `alt+enter` on any row of the normal picker (the palette's
  `Item.Alt` alternate activation, added for this feature), or `project.peek`
  ("Peek Project…", no default chord — the picker is the entry point), which
  opens the same list in a peek flavour (prefix `_`, locked-open only) where
  plain activation peeks and `alt+enter` switches for real. Both run
  `PeekTo(path)` — `SwitchTo`'s twin, yielding `PeekProjectMsg{Root}` — and
  the root model performs the normal seamless switch (#777) with two
  differences (`internal/app/project_peek.go` + `switchOpts` in `switch.go`):
  `RecordOpenCmd` is **skipped** (the peeked project never enters
  `project.history` and never becomes the #1000 restore head), and the fresh
  model carries a `peekState` marker — the origin root plus a snapshot of the
  peeked project's would-be session/layout payloads.
- **Indicator**: while peeking, a status-line segment shows
  `peek ⇢ <origin> (<chord>)` with the live `project.peek.return` binding
  (resolver truth), and the switch toast reads "peeking … `<chord>` returns".
- **Return**: `project.peek.return` ("Return From Peek", default
  `cmd+shift+b` with the delivered `ctrl+shift+b` secondary) switches back to
  the origin — recording the origin open like any switch-back — then drops
  and tears down the peeked workspace (`Drop` + `closeWorkspace`, the
  #820/#825 path: terminals, LSP, memory). The peeked project's session and
  layout are **not written** when they still equal the peek-enter snapshot,
  so an untouched peek plants no `.ike` directory in a repo it only read.
  A busy peek (dirty buffers, running processes, popup/floating shells)
  prompts first with the #821 shape (`[s]` save all then return / `[d]`
  discard and return / `[esc]` stay); a failed switch back (origin root gone)
  keeps the peek intact with the usual failure toast. An origin workspace
  evicted while peeking (#780 — a peek counts toward the cap) simply resumes
  as a cold first visit.
- **Escalation**: deciding to stay converts the peek into a normal workspace.
  `project.peek.keep` ("Keep Peeked Project") records the open and clears the
  marker in place; a plain switch elsewhere from within a peek records the
  peeked root (sequentially before the target, one cmd — two concurrent
  history writes would race) and the marker dies with the model rebuild.
  Peeking from within a peek escalates the current one first. `project.close`
  from a peek discards it: the resume-MRU path runs without escalation.
- **Lifetime**: peek state is model state, deliberately unpersisted — after a
  quit the peeked workspace is simply gone and the origin restores normally.

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
  typed path outside the history.
  The **currently open project is not listed** (#2317), the way the
  recent-files mode drops the active file: the history's newest entry is
  always the project you are standing in, so listing it would put a row that
  can only answer "already in …" on top. Dropping it makes the first row the
  *previous* project, so the switch chord plus `enter` bounces between the two
  projects you alternate between — the whole picker is an MRU switcher, and
  the MRU order is the persisted history, so it survives a restart. The
  exclusion is not an empty-query special case: a query cannot surface the
  current project either, and the peek flavour hides it for the same reason
  (peeking where you already are is the same no-op). It resolves the palette
  `Context.Root` (always `"."` — the IDE is anchored at the process working
  directory) with `filepath.Abs` and compares cleaned paths against the
  stored, absolute history paths; an unresolvable root filters nothing.
  Any non-empty query also browses the
  filesystem (#542): matching directories (via the shared
  `internal/pathcomplete` engine, dirs-only) render as selectable
  `Open <dir>` items ahead of the raw affordance, and `tab` extends the query
  to the longest unambiguous prefix — a single match completes with its
  trailing separator, so repeated tab descends (`Dev` → `Development/`).
  The tab plumbing is a palette-level seam: modes opt in by implementing
  `palette.Completer`. An absolute (`/…`) or home-relative (`~/…`) query
  browses and completes as typed; anything else — a bare name, `./…`, `../…`
  — resolves against the configured projects directory
  (`project.ProjectsDir()`) instead of the process working directory (#1808),
  matching `newproject_prompt.go` / `clone_prompt.go`, which already default
  new projects there. Entry details render through `compactPath`
  (home → `~`, middle-ellipsis) so long roots never crowd out the title.
  Activation emits `PickedMsg{Path}`, which the root model turns into the
  switch transaction below. Every row carries a **relative last-opened
  time** (`RelTime`, now implemented in `ui.RelTime`: "just now", "5m ago",
  … "3w ago"; empty for legacy entries without a timestamp, #842). Since
  #1114 the time renders **right-aligned** in the palette's `Item.Time`
  column — clearly separated from the name and from the `✕` control; narrow
  rows truncate the name (ellipsis) while the time and `✕` stay intact, and
  below a minimum name width the time drops. In-memory workspaces (#820)
  keep the `●` badge next to the name and their aux action (`shift+delete` or
  `cmd+backspace` — the forward-delete-free alias, #1418 — / a click on the
  aux zone, rendered as `⏏` on these rows to set the close apart from
  removal) closes the workspace; unloaded entries' aux action instead
  **removes the entry from the history** (`RemoveFromHistoryMsg` → off-loop
  `RemoveFromHistory` write-back at user scope → config reload → the
  still-open palette re-lists). Since #2489 each of the first nine rows also
  carries its **MRU digit** as a leading hint — the `ctrl+alt+N` chord that
  switches there without the picker. The Recent Projects column of the Recent
  Files dialog (#778) carries the same time column, removal action and digit
  hints, and the Recent Files rows themselves gained the matching layout and
  prune action in #1113. `cmd+shift+p` is also in the JetBrains chord table
  (`internal/keymap/defaults.go`): the chord layer resolves modified chords
  even in a capturing editor, which the registry keymap layer does not.
- **Branch + dirty marker per row (#2178)**: each git project's row carries
  its current branch and a `*` when the checkout has uncommitted changes —
  `⎇ main*`, in the same badge column as the `●` in-memory dot (`● ⎇ main*`).
  Reading that costs a subprocess per row, so it is **fully asynchronous**
  (`gitinfo.go`): opening the picker lists the history immediately and returns
  `EnrichCmd(history)`, which fires one probe per entry — capped at
  `maxGitProbes` (24) entries, at most `gitProbeParallel` (4) subprocesses at
  a time. Each probe is a single `git status --porcelain=v2 --branch -z`
  (branch header plus "is anything changed at all"; `--ignored` deliberately
  not requested, unlike `vcs.Load`, which needs it for explorer dimming),
  bounded by `gitProbeTimeout` (1s — tighter than `vcs`'s 5s `gitTimeout`: a
  row that has not answered by then is better left plain than filled in after
  the eye has moved on). Untracked files count as dirty; a detached HEAD shows
  the short commit hash. Results arrive one `GitInfoMsg` at a time; the root
  model files each in the shared `GitCache` — one cache serves both picker
  flavours, so a probe made for the switch list is already there when the peek
  list opens — and calls `palette.RefreshRows`, the in-place re-list that keeps
  selection and scroll (plain `Refresh` resets to the top like a query edit and
  would yank the cursor from under a user already arrowing down). Everything
  that is not a clean answer — a non-git root, a missing git binary, a timeout,
  an unreadable path — degrades to the plain row: the badge stays empty and
  nothing is toasted. A cached result survives the palette closing, so a
  re-open starts from the last known state and re-probes on top of it.

## Switch orchestration (#3)

The switch is one msg-driven transaction; `internal/project` never mutates a
subsystem (it must not import editor/explorer), the root model routes:

1. `SwitchTo(path)` (`switch.go`) validates off the Update loop and yields
   `SwitchProjectMsg{Root}` (absolute) or `SwitchFailedMsg{Path, Err}` — a
   failure toasts and changes nothing.
2. The root model (`internal/app/switch.go`): the current root is a friendly
   no-op; dirty buffers emit `UnsavedChangesMsg{Root}`, which opens the
   **unsaved-changes guard** in the floating shell — `[s]` (or `[enter]`,
   #1356) save all then switch, `[d]` discard and switch, `[esc]` cancel
   (project untouched).
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

**Auto-save on switch (#2186).** With `project.auto_save_on_switch` on (the
default, JetBrains' behaviour) an orderly switch first writes the departing
project's edits — `handleSwitchProject` runs the gate in
`internal/app/switch_autosave.go` before `performSwitch` ever chdirs:

- Every dirty **file-backed** buffer of the active workspace (background tabs
  included, shared documents once) is saved through the *manual* save action
  (`write`), so **format-on-save**, organize-imports-on-save and the other
  save hooks run exactly as on a `:w`. That path is asynchronous when the save
  chain applies (#1148), and a parked workspace's editors are cut loose from
  the model's services — a chain completing after the swap would never write.
  The gate therefore parks the switch (`autoSaveSwitch`) until the last
  `ilsp.SaveChainDoneMsg` lands, with a 3s backstop that writes whatever is
  still pending raw (format skipped) so a wedged server can never strand the
  switch.
- Buffers with no writable home never block it at all (#2396): **untitled**
  ones, a **read-only** buffer (#1762) and one **changed on disk** since the
  edits (a stale buffer, whose overwrite is the conflict guard's decision,
  not the switch's) — plus anything whose write failed — simply stay dirty
  and **park with the departing workspace**, announced by one notification.
  They come back exactly as left on the next visit; nothing is lost. The
  former "Cannot save every buffer" dialog is gone — a project switch never
  prompts; the quit guard at exit remains the single place that asks about
  unsaved buffers.
- `false` restores the pre-#2186 behaviour: dirty buffers simply park with
  their workspace (#777). The gate is on the plain `project.switch` path only;
  a quick peek (#2136), `project.close` and the peek-return keep their own
  busy guards, whose explicit save/discard answer must not be second-guessed.
- Settings UI: "Files & Session" → *Auto-save on project switch* (user scope).

**Global tool panes follow the switch (#1890, #1903, #2042).** A
`[[tools.custom]]` tool marked `global = true` detaches from the departing
workspace right after the chdir (`detachGlobalTools`) — its one process-wide
session parks on the workspace manager — and at the end of `performSwitch`,
after the rebuild, `attachOpenGlobalTools` splices it into the incoming
workspace — the project's own saved placement first (`savedToolHosts`,
#2141), then the normal open path (slot rule with tab-join, home placement
with tab-join, tab-join into an existing tool pane/tool host, and only then
the adaptive split) — without moving focus, so the pane is visible
in every project view while the tool is open — and several arriving tools
group at their configured position instead of scattering as separate splits
over the editor area (#2042). A workspace whose own `layout.json` places the
tools re-attaches them during the restore at those saved positions; the
attach only handles what the layout left parked, and it too follows that
record: the pane key the layout named for the tool, else the live pane a
**saved co-tenant** already took — the host of a tabbed group closes with
its last global tab (#1901), so returning to a project reforms the group
around whichever member arrives first instead of dissolving it into the tool
pane next door (#2141). Only a tool the layout never placed groups by the
#2042 rule. A restore that already re-attached the session from the saved
layout wins; explicitly closing the pane in any project ends the tool
everywhere (no resurrection from stale layouts), and a session whose process
exited while parked arrives showing the #810 exited overlay with
Restart/Close. Details: [tool-panes](tool-panes.md).

**The active tab stays per project (#1906).** The attach appends its tabs and
activates each one, which would otherwise hand every project the tab selection
of the project just left. `performSwitch` therefore snapshots every pane's
active tab index right before `attachOpenGlobalTools` and re-applies it right
after (`snapshotActiveTabs` / `restoreActiveToolTabs`); on top of that, each
workspace remembers in `ActiveTools` which global tools were its host pane's
active tab when it was last left — recorded by `detachGlobalTools`, keyed by
tool name because a host carrying nothing but global tools closes with the
detach — and those reclaim their tab wherever it landed. A remembered tool
that did not come back (closed everywhere, or no longer global here)
contributes nothing, so the pre-attach index stands and no pane is ever left
pointing at a tab that is gone. The corrected selection is persisted over the
attach's own `saveLayout`.

**Buffer reconciliation on resume (#1515).** While a workspace is parked its
watcher is stopped, so external edits in that window (a coding agent's
atomic-rename saves, git operations) never arrive as events. `performSwitch`
therefore sends every resumed editor tab an `editor.ReconcileMsg`: a clean
buffer whose file changed on disk reloads in place (identical content is a
no-op, undo history survives), a dirty buffer whose file provably changed
(disk hash differs) is marked stale so the next save runs the conflict
guard; a deleted file leaves the buffer untouched — it is the only copy.

**Explorer resync on resume (#1520).** The same catch-up covers the tree:
`performSwitch` sends the resumed explorer one `explorer.ResyncMsg`, which
re-scans every expanded, loaded directory from the root (the auto-refresh
poll would get there eventually, and not at all with
`explorer.auto_refresh = "false"`). Expansion survives because scan results
merge over existing children; the selection stays on its entry via the
per-merge stability snap in `applyScan`. Git status needs no extra step —
`StartWatcher` sends a `vcsInvalidateMsg` on every switch.

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
