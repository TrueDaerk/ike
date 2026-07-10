---
type: concept
title: Project Switching
description: Roadmap 0090 — internal/project owns the recent-projects history data layer, the project.switch command and the palette picker; the switch orchestration is the remaining sub-issue.
resource: internal/project
tags: [architecture, project, history, switching, palette]
timestamp: 2026-07-10T05:00:00Z
---

# Project Switching (Roadmap 0090)

`internal/project` owns the "Switch Project" flow (spec: epic #37). Landed so
far are the **data layer** (#2) and the **command + picker** (#12); the switch
orchestration msgs (#3) complete the flow.

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
  Activation emits `PickedMsg{Path}`; until #3 lands the root model surfaces
  the selection as an informational toast instead of switching.
