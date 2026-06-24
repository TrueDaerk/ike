# 0082/06 — Duplicate line · `Cmd+D`

| Field | Value |
|-------|-------|
| Chords | `cmd+d` (→ `ctrl+d` off macOS — collides with vim half-page-down!) |
| Command id | `editor.duplicateLine` |
| Context | Editor |
| Owner | 06 (not registered) |
| Status today | **blocked: 06** |

## What it should do

Duplicate the current line (or selection) directly below, JetBrains-style.
Cursor moves to the duplicate so repeated presses stack copies.

## Usability checklist

- [ ] No selection → duplicates the current line below; cursor on the copy.
- [ ] Selection → duplicates the whole selected block below it.
- [ ] Indentation preserved; no trailing-whitespace artifacts.
- [ ] Repeated presses produce N stacked duplicates predictably.
- [ ] Single undo removes one duplication step.
- [ ] **Conflict risk:** off macOS `Ctrl+D` is vim half-page-down — pick a non-clashing default (leader) before live.

## Manual test protocol

1. On a line, press → identical line appears below, cursor on it.
2. Press 3× → 3 copies stacked; undo 3× → back to original.
3. Visual-select 2 lines, duplicate → both lines copied below.
4. Decide `Cmd+D` vs vim `Ctrl+D` conflict resolution.

## Verdict (you fill after testing)

- Status: ☐ pending · ☐ OK passt · ☐ needs change
- Notes:
- Follow-ups:
