# 0082/12 — Find usages · `Alt+F7`

| Field | Value |
|-------|-------|
| Chords | `alt+f7` (fragile in some emulators) |
| Command id | `editor.findUsages` |
| Context | Editor |
| Owner | 06 / 10 (LSP) |
| Status today | **blocked: 06/10** |

## What it should do

List every reference to the symbol under the cursor (via LSP) in a navigable
results view; selecting a result jumps there. Origin pushed to nav stack.

## Usability checklist

- [ ] Results view lists file:line + a code-context snippet per usage.
- [ ] Grouped by file, counts shown ("12 usages in 4 files").
- [ ] Keyboard-navigable (next/prev result, Enter jumps, Esc closes); reuses palette/list UX.
- [ ] Jumping to a result opens the file and pushes nav stack (23 returns).
- [ ] Live filter box to narrow results.
- [ ] No usages / no LSP → clear empty-state, not a blank panel.
- [ ] Large result sets paginate/scroll smoothly, no UI freeze.
- [ ] `Alt+F7` fragility: a delivered alias (leader) exists per 0081.

## Manual test protocol

1. Cursor on a symbol, press → results grouped by file with counts.
2. Navigate + Enter → jumps to that usage; `Cmd+[` returns.
3. Filter box narrows the list live.
4. Symbol with zero usages → empty-state message.
5. Confirm leader alias works where `Alt+F7` doesn't arrive.

## Verdict (you fill after testing)

- Status: ☐ pending · ☐ OK passt · ☐ needs change
- Notes:
- Follow-ups:
