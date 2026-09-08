---
type: concept
title: Project Groups
description: Epic 0510 — named sets of project roots opened, parked and closed as one; the [[project.groups]] data layer, the project.active_group marker, the group-aware background workspace cap, the project.group.open picker and warm-up chain, the status-line group segment, project.group.close with the aggregated busy guard, group.next/prev cycling, group.warm and the group-restricted Find in Project Group.
resource: internal/app/project_group.go
tags: [architecture, project, groups, workspace, config, palette, status-line]
timestamp: 2026-09-08T22:00:00Z
---

# Project Groups (Epic 0510)

A **project group** is a name plus an ordered list of roots. Opening a group parks every member as a
live workspace and lands on the first one; closing it tears all of them down in one action. The
single-root model is untouched: exactly one root is active, the IDE is still anchored at `.`, and a
group is nothing more than **a set of ordinary workspaces plus a marker**.

Spec: epic #2569. This page grows with each sub-issue; today it documents the data layer (#2570),
the open entry point — picker, warm-up chain, marker, status segment (#2571) — and leaving and
moving within a group: the close with its aggregated busy guard, `next` / `prev` cycling and
`warm` (#2572), the MRU integration (#2574) and the group-restricted `project.findInGroup`
(#2575).

## Persisted shape

`[[project.groups]]` lives in the **user** layer, like `project.history` — the set spans projects on
this machine, so the project layer `config.DefaultScope` would pick for a `project.*` key is wrong
for it. The list follows config's default list semantics: **replace, never append**.

```toml
[[project.groups]]
name = "web"
roots = ["/home/me/code/api", "/home/me/code/ui"]
created = "2026-09-08T12:00:00Z"

[project]
active_group = "web"
```

- **`name`** — display name and the key a group is addressed by. Unique case-insensitively, no path
  separators.
- **`roots`** — ordered member roots, absolute and cleaned. The order is the open order and the
  `group.next` / `group.prev` cycle order.
- **`created`** — RFC3339 UTC, informational.
- **`project.active_group`** — the group the session is working in, by name; empty means none.

The TOML shape is fixed by `config.ProjectGroup` and `config.Project` (`internal/config/schema.go`);
the semantics live in `internal/project/group.go`, which stays a leaf — it validates and persists,
it never mutates a subsystem.

## Content rules (`internal/project/group.go`)

- **Lookups**: `Groups(cfg)` returns the stored list in order; `FindGroup(cfg, name)` matches
  case-insensitively; `GroupContaining(cfg, root)` returns the **first** group in list order that
  has `root` among its members (roots compared as cleaned absolute paths), built on
  `g.Contains(root)` — the membership test the MRU ordering and the row badge share.
  `GroupBadge(g, root)` renders a member's marker, `⦿ web`, and `""` for a non-member or the zero
  group, so a caller can hand it every row unconditionally.
- **`ValidateGroup(cfg, g)`** is the write-time gate and returns the normalised group: the name
  trimmed, non-empty, free of path separators and not colliding case-insensitively with a
  *different* stored group (the same name is the group being edited, which upsert replaces); the
  roots expanded (`~`), resolved through `project.Validate`, deduped in list order; at least one
  root remains.
- **`UpsertGroup` / `RemoveGroup`** write the whole list back through `config.WriteKey` at
  **user scope**. Upsert replaces an existing same-name group in place, keeping its list position,
  and appends a new one. Removing the active group clears the marker too — a marker pointing at
  nothing would survive every startup check. `UpsertGroupCmd` / `RemoveGroupCmd` /
  `SetActiveGroupCmd` / `ClearActiveGroupCmd` wrap them as `tea.Cmd`s, so the write never blocks the
  Update loop (the `RecordOpen` / `RemoveFromHistory` pattern).
- **`ResolveGroupRoots(g)`** splits the members into the ones that exist on disk (in list order) and
  the ones that do not. The open chain opens what exists and reports the missing ones with **one**
  notification (`group "web": 1 of 3 roots missing`); the stored group is never edited — a checkout
  can be back tomorrow.

## The active-group marker

`SetActiveGroup` / `ClearActiveGroup` write `project.active_group` at user scope (`group.open`
writes it, `group.close` clears it). `ActiveGroup(cfg)` resolves it to the group it names.

At startup `cmd/ike/main.go` calls `ReconcileActiveGroup` right after the initial history record:
the marker is honoured **only when the process root is a member** of that group, otherwise it is
cleared. It re-establishes the status segment, the MRU ordering and find-in-group after a restart;
it does **not** re-park the other members — they warm lazily on the first visit, or eagerly via
`project.group.warm`.

## Cap protection

While a group is active the effective background workspace cap
(`maxWorkspaces()`, `internal/app/workspace_evict.go`, #780) is
`max(project.max_workspaces, len(members))` — a group parks all of its members, so a lower cap would
evict one the moment the last is opened. The eviction sweep ranks the background roots through
`evictionOrder`: LRU-first as the manager reports them, but **members last** — a parked non-member
goes before any member, and a member only goes when the cap is still exceeded afterwards. Memory
stays bounded by the background LSP idle shutdown (#1521), which applies to parked members
unchanged.

## Opening a group (`project.group.open`, #2571)

**Command**: `project.group.open` ("Open Project Group…", global scope, `internal/app/commands.go`),
default chords `cmd+alt+shift+g` with the delivered `ctrl+alt+shift+g` secondary
(`internal/keymap/defaults.go`) — the `cmd+alt+shift` layer of the other project-set entry point,
find in all projects. It is on the #805 terminal allowlist (`terminalGlobalCommands`), so it
works with a terminal pane focused, and in the File menu ("Open Project Group…").

**Picker** (`internal/project/grouppicker.go`, `GroupPickerMode`): the palette locked to the
group mode — the `#` picker pattern, the prefix rune (`\`) has no user-facing story. One row per
stored group in list order: the name as title, `3 projects · api, ui, www` (member count and the
member directory names, bounded to the detail width) as the detail chip, `⦿` as badge on the
active group. The query fuzzy-matches the **name** only; there is no raw-path fallback (a group
is created in Settings, never typed). With no group stored the single inert row reads
`no project groups · Settings → Project Groups`. `enter` emits `project.OpenGroupMsg{Name}`.

The Settings ▸ Project Groups page's `o` verb (#2573) dispatches `OpenProjectGroupMsg{Name}`, which
closes the settings panel and runs the very same chain.

**Open chain** (`internal/app/project_group.go`): a group open is **a chain of ordinary
switches**. The model rebuild is chdir-based, so the members are warmed by *visiting* them:

1. `handleOpenGroup` resolves the members through `ResolveGroupRoots`. The missing ones are
   reported once — `group "web": 1 of 3 roots missing` — and the stored group is left untouched;
   with no member on disk the open only notifies.
2. The hops run for members **N…2 in reverse, then member 1**, each through
   `handleSwitchProject` / `performSwitch` — the auto-save gate (#2186), the history record, the
   seamless resume of an already-parked member (`m.ws.Peek`), and every `project.switch`
   telemetry op exactly as for a palette-driven switch. A hop onto the root one is already
   standing in is a friendly no-op counted as done (`handleSwitchProject` would only say
   "already in").
3. The remaining chain rides each rebuild in the `groupOpening` carry-over — the allfind
   session-state block in `performSwitchOpts` — and the hop's `SwitchedMsg` runs the next one
   (`groupHopLanded`). The per-hop "switched to" toast is suppressed; the status segment counts
   instead (`opening web 2/3`).
4. A failed hop (`SwitchFailedMsg`: root gone, `chdir` error) is **skipped with one
   notification** (`group "web": skipped ui — …`) and the chain continues; the failed
   transaction left the model untouched, so the previous hop stays active. The landing is
   therefore the **first available member**.
5. Landing (`finishGroupOpen`): the model's `activeGroup` marker moves to the group at once,
   `project.active_group` is written off the loop (`SetActiveGroupCmd`; the `ActiveGroupMsg`
   reloads the config so `config.Get()` — the cap, the picker badge — catches up), and the one
   final toast reads `group web open · 3 projects` (counting the members that opened). With
   every hop failed nothing changes and the marker stays where it was.

Rules that fall out of the mechanics: opening from within a **peek** (#2136) escalates it — the
first hop is a normal switch away from the peek, which records the peeked root. Opening while
**another group is active** replaces the marker on landing; the previous group's workspaces stay
parked (the cap may then evict them, non-members first). **Re-opening the active group** re-warms
the members that are no longer parked and lands on member 1. Cancelling a hop at the
unsaved-changes prompt (`esc`) ends the chain (`abortGroupOpen`): the members visited so far stay
parked, the marker is untouched.

**Cap protection during the chain**: `enforceWorkspaceCap` asks the model for the group to
protect (`capGroup`): the group **being opened** while the chain runs, then the marker the model
carries, then the persisted marker. `maxWorkspacesFor` / `groupMembers` / `evictionOrderFor` are
the explicit-group forms of the #2570 functions, so the hops that park the members can never
evict them before `project.active_group` is even written — a parked non-member goes first. Member
roots are compared **canonically** (symlinks resolved), because the manager keys workspaces by
`os.Getwd`'s spelling while a group stores the roots as typed.

## The marker on the model

`Model.activeGroup` is the session's copy of `project.active_group`: seeded in `buildModel` from
the loaded config (main.go reconciled it against the process root first, §"The active-group
marker"), **carried across every rebuild** in the same carry-over block as the chain, replaced by a
group open's landing, and cleared by `project.group.close` (#2572). A plain switch to a non-member
keeps it — the marker names the set one is working in, not the current root.

## Status line

The `group` slot (`internal/app/statusline.go`, left list, right after `file`) renders
`⦿ web/api` — the group's name / the current root's directory name — while a group is active,
`opening web 2/3` while the open chain runs (hop in progress / present members),
`warming web 1/2` while a `project.group.warm` chain runs, and nothing without a group. It drops
with the other low-priority segments under width pressure (right after `hint` in
`statusDropOrder`). A click runs `project.group.next` (`statusSegmentCommands`) — the next
member in list order. See [Status Line](/architecture/status-line.md).

## Closing a group (`project.group.close`, #2572)

**Command**: `project.group.close` ("Close Project Group", global, `internal/app/commands.go`),
default chords `cmd+alt+shift+w` with the delivered `ctrl+alt+shift+w` secondary —
`project.close`'s `cmd+shift+w` with `alt` added, the group flavour of the close. On the #805
terminal allowlist and in the File menu ("Close Project Group"). Without an active group it
notifies `no project group open`; while an open/warm chain runs it notifies and does nothing.

**Aggregated busy guard** (`internal/app/project_group_close.go`, `collectGroupActivity`): the
close would kill live state in *several* workspaces, so the probes of the two single-workspace
closes run over every in-memory member and are folded into **one** prompt in the #821 shape:

- the **active member** is probed like `project.close` — `collectActivity` over its panes plus
  the popup terminal (unless the popup scope is global, #2406) and the project-owned floating
  panels (#1793), which live on the model rather than in `Aux`;
- every **parked member** is probed like the close-from-list (#820): `collectActivity` over the
  parked workspace, whose popup shells sit in `Aux`.

Idle members are left out. The prompt (`Close project group?`) lists the busy members one line
each — `api — 1 running shell terminal`, `ui — unsaved: f.txt` — followed by `[s]` *save all,
then close the group* (offered only when some member is dirty), `[d]` *close the group — stop
processes, discard unsaved changes* and `[esc]` *cancel — keep the group open*; enter takes
the primary (#1356). `s` writes the active member's buffers through `saveAllDirty` and each
parked member's through `saveWorkspaceDirty`, then re-probes: a member still dirty after the
write **stays open and cancels the rest** (`group not closed: save failed in ui`) — nothing
closes. `esc` leaves every member, the shell and the marker untouched.

**Order** (`performCloseGroup`):

1. With the active workspace a member, the seamless switch runs first to the **MRU
   non-member** parked workspace — the last non-member in `m.ws.Background()` — through
   `performSwitchOpts` with `record` and `closing` set, exactly `project.close`'s transaction:
   the member's session and layout persist, its `project.leave` reason is `close`, its
   workspace parks. A failed switch (chdir error) closes nothing; the `SwitchFailedMsg` toast
   names the reason. Standing in a non-member root needs no switch.
2. Every parked member — the one just parked included — is `Drop`ped from the manager and
   torn down through `closeWorkspace` (#820/#825): terminals, runs and the debug session end,
   the LSP bridge releases the root through the workspace-closed hook, the crash snapshots and
   poll-watches go, memory is handed back. Recent-projects **history entries stay**.
3. The marker clears: `Model.activeGroup` at once, `project.active_group` off the loop through
   `ClearActiveGroupCmd` (the `ActiveGroupMsg` reloads the config). The toast reads
   `closed group web · 2 projects`; telemetry brackets the whole close as the
   `project.group.close` op, with the nested `project.switch` op as the switch's own share.

**Degrading to the quit guard**: with the active workspace a member and **no non-member**
parked, closing the group would empty the IDE, so the close becomes a quit — `quitActivity`
aggregates the same probes over every workspace and the #287/#821 quit prompt (or a clean
`quit`) runs, exactly like `project.close` on the last project. The marker clears
**synchronously** when the quit goes through (`pendingClose.groupClose`, `clearGroupMarkerNow`:
no cmd runs after a quit) and stays on `esc`.

## Cycling (`project.group.next` / `project.group.prev`, #2572)

`project.group.next` / `project.group.prev` ("Next / Previous Project in Group", global),
default chords `cmd+alt+]` / `cmd+alt+[` with the delivered `ctrl+alt+]` / `ctrl+alt+[`
secondaries (spelled `right-bracket` / `left-bracket` in the table; `nav.back` / `nav.forward`
own the plain `cmd+bracket`), on the #805 terminal allowlist. `handleCycleGroup`
(`internal/app/project_group_cycle.go`) resolves the members through `ResolveGroupRoots` — a
member **missing on disk is skipped** — finds the current root among them (canonical
comparison) and steps `±1` **in list order with wrap**. From a non-member root `next` lands on
member 1 and `prev` on the last member; a single-member group only notifies. The step is an
ordinary `handleSwitchProject`: a parked member **resumes**, an unparked one is a **cold first
visit** (built from its saved layout), history records the open, the auto-save gate runs, the
marker rides the rebuild. Without an active group both notify `no project group open`. The
status line's `group` segment click runs `next`.

## Warming (`project.group.warm`, #2572)

`project.group.warm` ("Warm Project Group") is **palette only** — `group.open` is the entry
point that warms a group, so the audit ledger (`cmd/ike/keybind_audit_test.go`) carries it as
`reasonOccasional`. `handleWarmGroup` lists the members present on disk that are **neither
active nor parked** and runs the open chain over them — the cold members N…1, then one final
hop **back to the current root** — flagged `groupOpen.warm`: the segment reads
`warming web 1/2`, the landing neither moves the marker nor writes it and toasts
`group web warm · re-parked 2 projects`. Skipped hops are reported like the open's. With every
member already in memory nothing runs: `group web is warm · every project is parked`.

## MRU integration (#2574)

While a group is active, **every** recent-projects list puts its members first and marks them —
one function does it, so all of them agree:

- **`project.MRUOrder(history, current, group)`** (`internal/project/mru.go`) drops the project one
  is standing in and hoists the group's members to the front, keeping *their* MRU order among
  themselves; the rest of the history follows, newest first. `MRUTargets` is its roots. The picker
  (`#`), the peek flavour (`_`), the Recent Projects column of the recent-files dialog and
  `project.switchMRU1…9` all read it, so the N-th digit chord is the N-th row. A zero `Group` — no
  marker — leaves the plain MRU order untouched.
- **Member rows carry `⦿ <group>`** in the existing badge column, joined with the in-memory dot and
  the git context by `project.JoinBadge`: `● ⦿ web ⎇ main*`. The badge is rebuilt on every
  `Results` call, so the asynchronous git enrichment's `RefreshRows` (#2178) keeps it.
- **No digits return to the rows** (#2532): the digit means "N-th row of the picker", which is
  exactly what makes the reordering useful.
- **`project.switchLast` is not reordered**: it stays the MRU parked workspace, group or not —
  the toggle back to where one just was must not depend on the group one happens to be in.

Standing in `api` of `web = {api, ui, infra}`, the picker lists `ui`, `infra` (MRU order among
them) and then the non-members, and `ctrl+alt+1` switches to the first member row.

## Find in group (`project.findInGroup`, #2575)

`project.findInGroup` ("Find in Project Group…", `cmd+alt+shift+d`, on the #805 terminal
allowlist) is **Find in All Projects with the project list restricted to the members** — the same
form, engine, status segment, results overlay, cross-project open and retained-results stepping.
Members are listed in group order, all checked; a member missing on disk greys out and is skipped
like any other root. It shares the remembered `project.find_all.*` state (query, toggles, globs,
result cap) but its root selection is the group's, so a group run never writes
`project.find_all.excluded_roots`. The overlay header names the searched set —
`7 matches in group web · 3 projects`. Without an active group it notifies `no project group open`
and opens nothing. Full mechanics:
[Search → Find in All Projects → the group variant](search.md).

## Validation diagnostics

The config validator (`validateProjectGroups`, `internal/config/validate.go`) reports per-entry
problems as `project.groups[<index>]` diagnostics:

- **Dropped** — the entry can never be opened: an empty name, a name carrying a path separator, a
  name colliding case-insensitively with an earlier one, or no roots at all.
- **Reported and kept** — a root that is missing or is not a directory:
  `project.groups[2]: root "/home/me/code/gone" is not a directory — it is skipped when the group
  opens`. The open chain skips it; the stored group is left alone.

The validator also normalises what it keeps: names trimmed, roots `~`-expanded, made absolute and
deduped.

## Settings UI

`[[project.groups]]` is edited on the **Project Groups** page
(`internal/settings/projectgroups_page.go`, #2573), in the rail next to *Files & Session*. It is the
`tools_page.go` list-editor shape: rows read `name · N roots · <first root>` (compacted), `a` adds,
enter edits, `d` deletes behind the shared confirm sub-panel and `o` opens the selected group —
`project.group.open` (#2571), which the app dispatches for the row's name. An empty page says how to
add the first group.

The add/edit form (`projectgroups_form.go`) is a sub-panel with one `ui.Field` for the **name** and
one per **root**, because a group's roots are paths and a comma-joined line cannot hold them:

- `+` / `alt+enter` add a root row, `-` / `alt+backspace` remove an **empty** one. Both plain keys
  act only on an empty row, so `+` and `-` stay typable inside a path (a directory may be named
  `c++`); `alt+enter` adds from anywhere. Tab cycles every field, and a bracketed paste / Cmd+V
  lands in the focused one.
- A root resolves the way a newly created project does: `~` expands, an absolute path stands, and a
  bare name is a project inside the project directory (`ValidateGroupRoot` → `ProjectsDir`,
  `project.directory`).
- Validation reports **one clear message per failure** and keeps the panel open — `name is
  required`, `name already used by "web"`, `add at least one project root`,
  `root 2: … does not exist — check the path` — with `ValidateGroup` as the final gate for the
  remaining rules (path separators, case-collisions, dedupe).
- Saving writes the **whole list** (`WriteGroups`), so a rename keeps the group's list position
  where an `UpsertGroup` of the new name would append a second entry. Deleting goes through
  `RemoveGroup`, which clears the active-group marker with it. Every write is at **user scope**,
  with no scope toggle, and the normal reload re-shapes the group picker live.

The page reaches the data layer through injected functions (`settings.ProjectGroupOps`, wired in
`internal/app/projectgroups_settings.go`) rather than an import: `internal/project` imports the
palette, which imports the registry, which imports `internal/settings`, so importing it back would
close the cycle.

`project.active_group` is **read-only session state, not a form field**: IKE writes it
(`project.group.open` / `project.group.close`) and drops it at startup when the process root is not
a member. It is listed in the settings coverage guard's `internalKeys`
(`internal/settings/coverage_test.go`) with that reason.

## See also

- [Project Switching](/architecture/project-switching.md) — the single-root switch flow a group open
  is a chain of.
- [Status Line](/architecture/status-line.md) — the segment model the `group` slot plugs into.
- [Keybindings](/architecture/keybindings.md) — the default chord table and the reachability matrix
  rows for `project.group.open`, `project.group.close`, `project.group.next` and
  `project.group.prev`.
- [Workspace](/architecture/workspace.md) — the parked-workspace manager and the background cap.
- [Configuration](/architecture/config.md) — the layered config the groups persist through.
