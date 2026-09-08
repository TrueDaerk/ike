---
type: concept
title: Project Groups
description: Epic 0510 — named sets of project roots opened, parked and closed as one; the [[project.groups]] data layer, the project.active_group marker, the group-aware background workspace cap, the project.group.open picker and warm-up chain, and the status-line group segment.
resource: internal/app/project_group.go
tags: [architecture, project, groups, workspace, config, palette, status-line]
timestamp: 2026-09-08T18:00:00Z
---

# Project Groups (Epic 0510)

A **project group** is a name plus an ordered list of roots. Opening a group parks every member as a
live workspace and lands on the first one; closing it tears all of them down in one action. The
single-root model is untouched: exactly one root is active, the IDE is still anchored at `.`, and a
group is nothing more than **a set of ordinary workspaces plus a marker**.

Spec: epic #2569. This page grows with each sub-issue; today it documents the data layer (#2570)
and the open entry point — picker, warm-up chain, marker, status segment (#2571).

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
  has `root` among its members (roots compared as cleaned absolute paths).
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
`opening web 2/3` while the chain runs (hop in progress / present members), and nothing without
a group. It drops with the other low-priority segments under width pressure (right after
`hint` in `statusDropOrder`). A click is wired to `project.group.next` in
`statusSegmentCommands`; until #2572 registers that command the click notifies that it is not
available yet. See [Status Line](/architecture/status-line.md).

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

- `[[project.groups]]` is edited on the **Project Groups** page
  (`internal/settings/projectgroups_page.go`, #2573), the `tools_page.go` list-editor shape, writing
  at user scope only.
- `project.active_group` is **read-only session state, not a form field**: IKE writes it
  (`project.group.open` / `project.group.close`) and drops it at startup when the process root is
  not a member. It is listed in the settings coverage guard's `internalKeys`
  (`internal/settings/coverage_test.go`) with that reason.

## See also

- [Project Switching](/architecture/project-switching.md) — the single-root switch flow a group open
  is a chain of.
- [Status Line](/architecture/status-line.md) — the segment model the `group` slot plugs into.
- [Keybindings](/architecture/keybindings.md) — the default chord table and the reachability matrix
  row for `project.group.open`.
- [Workspace](/architecture/workspace.md) — the parked-workspace manager and the background cap.
- [Configuration](/architecture/config.md) — the layered config the groups persist through.
