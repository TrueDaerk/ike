# Roadmap 0095 — Session Restore (Workspace State Persistence)

Reopen the IDE **as it was left**. Roadmap 0036 already persists pane geometry
and split structure (`layout.json`); this roadmap persists the state *inside* the
panes so a relaunch restores the full working context, not just the frame:

- **Editor** — the open file and the cursor position within it.
- **Explorer** — which directories are expanded, the show-hidden toggle, and the
  cursor row.

It is additive and best-effort: a missing, partial, or stale session file always
falls back to a clean default workspace and never crashes or blocks startup.

## Prerequisites / Dependencies

- **Roadmap 0036 (Pane Drag / Layout Persistence):** provides the persistence
  pattern this reuses — a per-project JSON state store under `.ike/` with the
  `IKE_CONFIG_DIR` discovery override, separate from `settings.toml`. Session
  state is a sibling store (`session.json`), restored in `NewWith` right after
  the layout restore.
- **Roadmap 0040 (Settings):** unaffected. Session state is *runtime UI state*,
  not user-authored configuration, so it stays out of `settings.toml` (matching
  0036's reasoning). The future `[project].restore_last` flag (Roadmap 0090)
  would later gate *whether* to restore, but the mechanism lives here.
- **Roadmap 0010 (Foundation):** the root model owns the editor + explorer panes
  and all I/O; save routes through the quit path, restore through `NewWith`.

## Design

- `internal/app/session.go` — the store: `sessionState` schema (optional
  `editor` + `explorer` sections), `sessionFile()` discovery, `loadSession` /
  `saveSession` (errors swallowed, like the layout store).
- `internal/explorer/state.go` — `Snapshot()` collects expanded dir paths +
  show-hidden + cursor path; `Restore(State)` re-applies them. Restore loads
  directories **synchronously** (`loadSync`), shallowest-first, because the
  normal async scan path would replace restored children. `explorer.Init` skips
  its startup scan once the root is restored.
- `internal/editor` — `CursorPos()` (0-based getter) and `SetCursor(line, col)`
  (clamped, scrolls into view) for save/restore.
- `internal/app/app.go` — `snapshotSession()` captures state, `quit()` saves then
  exits (every quit path routes through it), `restoreSession()` reapplies on
  launch and focuses the editor when a file was reopened.

## Out of scope

Multiple editors / tabs (Roadmap 0037 extends the layout store with per-leaf file
identity), undo history, visual/selection state, scroll offsets beyond what the
cursor implies, and cross-project session history (Roadmap 0090 `restore_last`).

## Milestones

- [x] Session store (`session.json`) with `IKE_CONFIG_DIR`/`.ike` discovery and
  tolerant load.
- [x] Explorer `Snapshot`/`Restore` (expanded dirs, show-hidden, cursor) with
  synchronous restore and `Init` scan-skip.
- [x] Editor `CursorPos`/`SetCursor`.
- [x] Save on every quit path; restore in `NewWith` with editor focus.
- [x] Tests: explorer round-trip + stale/missing dirs + Init interplay; app-level
  persist-and-restore.
- [x] Wiki concept doc + log entry.
