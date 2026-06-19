# Roadmap 0090 — Project Switching

Let a user change which project IKE is working on without restarting. A
"Switch Project" command opens a picker that lists recently opened projects
(most-recent-first) and accepts a direct filesystem path. Choosing one re-roots
the whole IDE: the explorer is re-rooted, the project config layer is
re-resolved, editor buffers are reset (with a guard for unsaved changes), and
the window title / status line update.

This roadmap owns the **switch action, the recent-projects history content, and
the re-root orchestration**. It reuses the command-palette UI for the picker
(07), the config loader and `[project]` schema for persistence (04), and only
*triggers* the explorer re-root (05) rather than reaching into it.

## Prerequisites / Dependencies

- **01 Foundation** — the root model in `internal/app` owns global state,
  including the current **project root** (the absolute directory IKE is anchored
  to; the explorer is rooted here and config discovery resolves the project layer
  from `{project_root}/.ike/settings.toml`). This roadmap defines a single
  `SwitchProjectMsg` (and result/confirmation msgs) the root model handles to
  re-initialize panes. There is exactly one project root at a time.
- **02 Plugins registry** — `internal/plugin` (Command, Keymap, Pane,
  FileHandler, Hook), `internal/registry`, `internal/host` (`host.API`). The
  switch action is a registered `registry.Command` (`project.switch`) invoked
  through the normal command path; it ships a default `Keymap` slot but the
  canonical binding is owned by 08.
- **04 Settings** — `internal/config` owns load/merge/precedence and the
  `[project]` section (`history`, `max_history`, `restore_last`). This roadmap
  reads `project.history` and writes it back through the **typed setter API that
  04 exposes** (04 owns the write mechanism; we own *what* is stored and *when*).
  Switching re-resolves the project-level config layer for the new root.
- **05 File Explorer** — exposes a re-root entry point (a `tea.Msg` the explorer
  consumes, or `host.API`) so a new project root rebuilds the tree. We trigger
  it; we never import explorer internals.
- **07 Command Palette** — the overlay + fuzzy-list component. We reuse it to
  render the project picker; we do not build a second fuzzy list.

## Architecture

```
internal/project/
  switch.go        orchestration: SwitchTo(root) -> validate -> re-root sequence
  history.go       recent-projects record: load/update/dedupe/cap via config (04)
  entry.go         ProjectEntry type (path, display name, last-opened time)
  picker.go        builds palette items from history + path-entry; feeds 07's UI
  validate.go      path resolution: abs/expand ~, exists, is-dir, has read access
  command.go       registry Command `project.switch` + default Keymap slot (02)
  msgs.go          SwitchProjectMsg, SwitchedMsg, SwitchFailedMsg,
                   UnsavedChangesMsg (confirm-before-switch)
  project_test.go  table-driven tests
```

Flow:

```
project.switch (Command)
        │
        ▼
picker.go ──uses──► 07 palette overlay (history items + path input)
        │ selection (existing entry | typed path)
        ▼
validate.go ─ ok ─► switch.go ──► [unsaved? -> UnsavedChangesMsg -> confirm]
        │ fail                         │ proceed
        ▼                              ▼
   SwitchFailedMsg            SwitchProjectMsg (to internal/app root model)
                                       │
        ┌──────────────────────────────┼───────────────────────────────┐
        ▼                ▼              ▼               ▼                 ▼
  re-root explorer  re-resolve     reset/save      update title    history.go:
  (05 re-root msg)  project config editor buffers   + status line   record + persist
                    layer (04)     (01 panes)                        via config (04)
```

`internal/project` depends on `internal/config`, `internal/registry`/`host`, and
the palette component; it does **not** import `internal/editor` or
`internal/explorer` directly — all pane effects happen by emitting msgs the root
model in `internal/app` routes.

## Design rules

- **Project root is absolute and single.** Resolve to an absolute path (expand
  `~`, clean the path) before doing anything. Reject non-directories and
  unreadable paths in `validate.go` with an actionable `SwitchFailedMsg`; never
  partially switch.
- **Switch is one orchestrated transaction, driven by msgs.** `switch.go`
  produces a `SwitchProjectMsg` that the root model turns into the re-root
  sequence. No subsystem is mutated directly from here. If a step is unsafe
  (unsaved buffers), the whole switch is gated behind confirmation first.
- **Guard unsaved changes.** Before re-rooting, if any editor buffer is dirty,
  emit `UnsavedChangesMsg` so the user can save-all, discard, or cancel. Only on
  confirm does the switch proceed; cancel leaves the current project untouched.
- **History is content we own, persistence we delegate.** A `ProjectEntry`
  stores `path` (absolute), `display name` (default: base dir name, overridable),
  and `last opened` (RFC3339). On open: upsert by path, move to front, dedupe,
  cap at `project.max_history`. We build the new list and hand it to 04's typed
  setter; 04 writes the TOML. List semantics follow 04 (replace, not append).
- **Record on successful switch only.** History is updated after a switch
  completes (including the *initial* project open at startup), never on a failed
  or cancelled attempt.
- **Reuse the palette, don't fork it.** The picker is a thin adapter producing
  palette items (recent entries, newest first) plus a path-entry affordance; all
  fuzzy/overlay behaviour stays in 07.
- **Expose the command, don't bind it.** Register `project.switch` with a stable
  id and a default `Keymap` *slot*; the canonical key is assigned by 08.
- **No blocking IO in Update.** Path validation, directory stat, and config
  read/write run as `tea.Cmd`s returning result msgs.

## `[project]` history entry shape

This roadmap defines what each `project.history` entry stores (persisted via 04):

```toml
[[project.history]]
path        = "/Users/me/code/ike"   # absolute, cleaned
name        = "ike"                  # display name (default: base dir)
last_opened = "2026-06-19T10:00:00Z" # RFC3339, used for ordering
```

Entries are kept most-recent-first, deduped by `path`, and capped at
`project.max_history`.

## Milestones

- [ ] `ProjectEntry` type + `validate.go`: abs/`~`-expand/clean, exists, is-dir, readable, actionable errors.
- [ ] `history.go`: load from `project.history`, upsert-by-path, move-to-front dedupe, cap at `max_history`, persist via 04's typed setter.
- [ ] Record current project into history at startup (initial open counts as an open).
- [ ] `command.go`: register `project.switch` `registry.Command` + default `Keymap` slot via `internal/registry`.
- [ ] `picker.go`: build palette items (history newest-first) + direct path-entry; reuse 07's overlay/fuzzy list (no new UI).
- [ ] `msgs.go` + `switch.go`: `SwitchProjectMsg`/`SwitchedMsg`/`SwitchFailedMsg` and the orchestration sequence.
- [ ] Unsaved-changes guard: detect dirty buffers, emit `UnsavedChangesMsg`, gate switch on save-all/discard/cancel.
- [ ] Root-model re-root handling in `internal/app`: re-root explorer (05 msg), reset/save editor buffers, update window title + status line.
- [ ] Re-resolve project config layer (04) for the new root as part of the switch.
- [ ] Tests: path validation cases, history upsert/dedupe/cap/order, record-on-success-only, command registration, picker item construction, full switch sequence (incl. unsaved-cancel and failed-path paths).
- [ ] Wiki: add/refresh the project-switching concept doc under `wiki/` (frontmatter `type`, `resource: internal/project`), document the `project.switch` command and the `project.history` entry shape, bump `timestamp`, add a `log.md` entry.

## Out of scope

The command-palette/fuzzy-list internals — **07** (we reuse it). The config
loader, merge/precedence, and the history write mechanism — **04** (we supply
content via its setter). The canonical default keybinding for `project.switch` —
**08** (we expose the command). Explorer tree internals — **05** (we trigger a
re-root). Multi-root workspaces, a project-creation/scaffolding wizard, git
clone-to-open, and per-project session restore beyond `restore_last` are later
work. **Open project in a new window vs the current one** is deferred until
windowing exists; when it does, `project.switch` gains an "in new window" variant
that opens the chosen root in a fresh window instead of re-rooting the current
one.
