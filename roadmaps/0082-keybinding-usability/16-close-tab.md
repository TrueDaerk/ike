# 0082/16 — Close tab · `Cmd+W`

| Field | Value |
|-------|-------|
| Chords | `cmd+w` (→ `ctrl+w` off macOS); app already binds `ctrl+w` to close focused pane |
| Command id | `editor.closeTab` (reconcile → close-focused) |
| Context | Global |
| Owner | 06 / app (close exists as `CloseFocused`) |
| Status today | **live via `ctrl+w`**, id needs reconciliation |

## What it should do

Close the active editor (tab/pane). If the buffer is modified, prompt to
save/discard/cancel rather than losing edits. Focus moves to a sensible neighbour.

## Usability checklist

- [ ] Closes the focused editor; focus moves to an adjacent pane predictably.
- [ ] **Modified buffer → confirm prompt** (save / discard / cancel), no silent data loss.
- [ ] Closing the last editor leaves a valid workspace (explorer focus / empty state), no crash.
- [ ] Explorer pane is not closed by this (or documented).
- [ ] Layout re-tiles cleanly after close; persisted layout updates.
- [ ] Id reconciled across palette/cheatsheet/keymap; `ctrl+w` and `Cmd+W` agree.

## Manual test protocol

1. Open 2 editors, `Cmd+W` → active one closes, focus moves to the other.
2. Modify a buffer, close → save/discard/cancel prompt; cancel keeps it.
3. Close down to the last editor → workspace stays valid.
4. Reopen IKE → layout reflects the close.

## Verdict (you fill after testing)

- Status: ☐ pending · ☐ OK passt · ☐ needs change
- Notes:
- Follow-ups:
