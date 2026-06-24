# 0082/24 — Navigate forward · `Cmd+]`

| Field | Value |
|-------|-------|
| Chords | `cmd+right-bracket` |
| Command id | `nav.forward` |
| Context | Global |
| Owner | 06 / app (navigation stack) |
| Status today | **blocked: 06/01** (no nav stack yet) |

## What it should do

Re-do a navigation undone by Navigate-back (23) — browser Forward. Only
meaningful after at least one Back.

## Usability checklist

- [ ] Exact inverse of Back: restores file + cursor/scroll the Back left.
- [ ] Forward stack cleared when the user navigates somewhere new after a Back.
- [ ] Forward at the newest entry → quiet no-op.
- [ ] Symmetric feel with Back; same granularity of history entries.
- [ ] Consistent with vim jumplist forward (`Ctrl+I`) or clearly separate (documented).

## Manual test protocol

1. Make jumps, Back twice, Forward twice → returns along the same path exactly.
2. After a Back, navigate somewhere new → Forward becomes a no-op (branch cleared).
3. Forward at the newest position → no-op.
4. Compare with vim `Ctrl+I`.

## Verdict (you fill after testing)

- Status: ☐ pending · ☐ OK passt · ☐ needs change
- Notes:
- Follow-ups:
