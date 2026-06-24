# 0082/13 — Rename symbol · `Shift+F6`

| Field | Value |
|-------|-------|
| Chords | `shift+f6` |
| Command id | `editor.rename` |
| Context | Editor |
| Owner | 06 / 10 (LSP) |
| Status today | **blocked: 06/10** |

## What it should do

Rename the symbol under the cursor across the project (via LSP). Prompt for the
new name pre-filled with the old, then apply edits in all files atomically.

## Usability checklist

- [ ] Inline prompt pre-filled with current name, cursor selecting it for quick overwrite.
- [ ] Preview / count of affected occurrences before applying ("rename 9 occurrences in 3 files?").
- [ ] Applies atomically; partial failure rolls back, doesn't leave half-renamed code.
- [ ] Open buffers update in place; unopened files saved or marked modified consistently.
- [ ] Invalid identifier rejected with a hint (no broken code written).
- [ ] Esc cancels with zero changes.
- [ ] One undo reverts the whole rename (or documented multi-file undo behavior).
- [ ] No LSP / non-symbol → clear hint.

## Manual test protocol

1. Cursor on a symbol, press → prompt pre-filled, old name selected.
2. Type new name, confirm → all occurrences across files updated.
3. Open one affected file → reflects the rename.
4. Try an illegal name (e.g. `1foo`) → rejected.
5. Esc before confirm → nothing changed; undo after rename → reverted.

## Verdict (you fill after testing)

- Status: ☐ pending · ☐ OK passt · ☐ needs change
- Notes:
- Follow-ups:
