# 0082/01 — Undo · `Cmd+Z`

| Field | Value |
|-------|-------|
| Chords | `cmd+z` (→ `ctrl+z` off macOS) |
| Command id | `editor.undo` |
| Context | Editor |
| Owner | 06 (registered) |
| Status today | **live** |

## What it should do

Revert the last buffer change as one logical step. Repeated presses walk the
undo history backwards. Cursor moves to the location of the undone change.

## Usability checklist

- [ ] One press undoes one *logical* edit (a whole insert run, not one keystroke).
- [ ] Cursor lands at the changed location, not at buffer top/0,0.
- [ ] Undo past the oldest change is a no-op with a quiet hint, not a crash/beep loop.
- [ ] Works from normal mode; in insert mode `Ctrl+Z` either undoes or is reserved consistently (documented).
- [ ] Status line reflects modified/saved state correctly after undo (e.g. `[+]` clears if back to saved).
- [ ] Fast repeat (holding) doesn't desync cursor vs buffer.

## Manual test protocol

1. Open a file, type a word, press `Cmd+Z` → word removed in one step.
2. Make 3 edits, undo 3× → buffer returns to start, cursor follows each step.
3. Undo once more at the oldest state → no-op, no error.
4. Edit, save, edit, undo → modified indicator clears when back at saved content.

## Verdict (you fill after testing)

- Status: ☐ pending · ☐ OK passt · ☐ needs change
- Notes:
- Follow-ups:
