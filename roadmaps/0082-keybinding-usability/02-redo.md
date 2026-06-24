# 0082/02 — Redo · `Cmd+Shift+Z`

| Field | Value |
|-------|-------|
| Chords | `cmd+shift+z` (→ `ctrl+shift+z` off macOS); editor also `Ctrl+R` (vim) |
| Command id | `editor.redo` |
| Context | Editor |
| Owner | 06 (registered) |
| Status today | **live** |

## What it should do

Re-apply the last undone change. Mirrors Undo: repeated presses walk forward;
cursor follows the redone edit. A new edit after undo discards the redo branch.

## Usability checklist

- [ ] Redo re-applies exactly what Undo reverted (symmetry, same granularity).
- [ ] Cursor lands at the redone change.
- [ ] After undo + a fresh edit, redo is a no-op (branch discarded) — no stale redo.
- [ ] Redo past the newest state is a quiet no-op.
- [ ] `Cmd+Shift+Z` and vim `Ctrl+R` agree (no divergent histories).
- [ ] Modified/saved indicator stays correct.

## Manual test protocol

1. Type, `Cmd+Z` (undo), `Cmd+Shift+Z` (redo) → text restored at same spot.
2. Undo 3×, redo 3× → exact round-trip.
3. Undo 2×, type a char, press redo → no-op (branch gone).
4. Compare with vim `Ctrl+R`: same result.

## Verdict (you fill after testing)

- Status: ☐ pending · ☐ OK passt · ☐ needs change
- Notes:
- Follow-ups:
