# 0082/23 — Navigate back · `Cmd+[`

| Field | Value |
|-------|-------|
| Chords | `cmd+left-bracket` |
| Command id | `nav.back` |
| Context | Global |
| Owner | 06 / app (navigation stack) |
| Status today | **blocked: 06/01** (no nav stack yet) |

## What it should do

Jump to the previous cursor location in the navigation history — across files —
like a browser Back. Pairs with Navigate-forward (24).

## Usability checklist

- [ ] Maintains a cross-file history of meaningful jumps (go-to-def, usages, search jumps, big cursor moves).
- [ ] Back restores both file and exact cursor/scroll position.
- [ ] Distinguishes navigation jumps from every tiny cursor move (no noisy 1-line-per-entry history).
- [ ] Reopens a closed file if the history points into it.
- [ ] Back at the oldest entry → quiet no-op.
- [ ] Forward (24) is the exact inverse; new navigation truncates the forward branch.
- [ ] Consistent with vim jumplist (`Ctrl+O`) or clearly separate (documented).

## Manual test protocol

1. Go-to-definition into another file, `Cmd+[` → back at the origin, exact spot.
2. Several jumps, back repeatedly → walks the history in order.
3. Close the origin file, then back → it reopens at the right spot.
4. Back past the start → no-op.
5. Compare with vim `Ctrl+O` expectation.

## Verdict (you fill after testing)

- Status: ☐ pending · ☐ OK passt · ☐ needs change
- Notes:
- Follow-ups:
