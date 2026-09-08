---
type: concept
title: Project Groups
description: Epic 0510 — named sets of project roots opened, parked and closed as one; the [[project.groups]] data layer, the project.active_group marker and the group-aware background workspace cap.
resource: internal/project/group.go
tags: [architecture, project, groups, workspace, config]
timestamp: 2026-09-08T12:00:00Z
---

# Project Groups (Epic 0510)

A **project group** is a name plus an ordered list of roots. Opening a group parks every member as a
live workspace and lands on the first one; closing it tears all of them down in one action. The
single-root model is untouched: exactly one root is active, the IDE is still anchored at `.`, and a
group is nothing more than **a set of ordinary workspaces plus a marker**.

Spec: epic #2569. This page grows with each sub-issue; today it documents the data layer (#2570).

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
- [Workspace](/architecture/workspace.md) — the parked-workspace manager and the background cap.
- [Configuration](/architecture/config.md) — the layered config the groups persist through.
