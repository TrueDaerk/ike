# 0082/04 — Cut · `Cmd+X`

| Field | Value |
|-------|-------|
| Chords | `cmd+x` (→ `ctrl+x` off macOS) |
| Command id | `editor.cut` |
| Context | Editor |
| Owner | 06 (not registered) |
| Status today | **blocked: 06** |

## What it should do

Delete the selection (or current line when none) and place it in the
register/clipboard — a copy that also removes. Cursor settles sensibly after the
removed span.

## Usability checklist

- [ ] Selection cut removes exactly the highlighted span and stores it.
- [ ] No selection → cuts the whole current line (linewise), cursor on next line.
- [ ] Cut content is paste-identical to a copy of the same span.
- [ ] Single undo restores the cut in one step.
- [ ] Feedback ("cut N lines") shown.
- [ ] Cutting the last line / empty buffer is safe (no panic, leaves a valid buffer).

## Manual test protocol

1. Visual-select, cut → span gone, cursor reasonable; paste → restored.
2. No selection, cut line → line removed linewise; undo → line back in one step.
3. Cut the only line in a 1-line file → buffer becomes empty, no crash.

## Verdict (you fill after testing)

- Status: ☐ pending · ☐ OK passt · ☐ needs change
- Notes:
- Follow-ups:
