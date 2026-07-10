---
type: concept
title: Project Switching
description: Roadmap 0090 — internal/project owns the switch flow end to end; recent-projects history, project.switch command, palette picker and the msg-driven re-root orchestration with an unsaved-changes guard.
resource: internal/project
tags: [architecture, project, history, switching, palette]
timestamp: 2026-07-10T06:45:00Z
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

## Command & picker (#12)

- **`project.switch`** (`command.go`): a compile-in plugin (id `project`)
  registering the command (global scope, title "Switch Project…") plus a
  default Keymap slot on `alt+shift+p` — layout-safe on QWERTZ; the canonical
  chord is owned by Roadmap 0080/0081. The command only dispatches
  `OpenPickerMsg`; the root model opens the palette locked to the picker mode.
  The File menu's "Switch Project" entry resolves against the same command id.
- **Picker** (`picker.go`): a palette `Mode` (prefix `#`, always opened
  locked) reusing Roadmap 0070's overlay/fuzzy list. Items are the history
  entries — fuzzy-matched on display name, falling back to the path; an empty
  query lists all, newest first — plus an `Open "<query>"…` affordance for a
  typed path outside the history. Entry details render through `compactPath`
  (home → `~`, middle-ellipsis) so long roots never crowd out the title.
  Activation emits `PickedMsg{Path}`, which the root model turns into the
  switch transaction below. `alt+shift+p` is also in the JetBrains chord table
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
