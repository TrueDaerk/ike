# 0082/03 — Copy · `Cmd+C`

| Field | Value |
|-------|-------|
| Chords | `cmd+c` (→ `ctrl+c` off macOS — collides with quit!) |
| Command id | `editor.copy` |
| Context | Editor |
| Owner | 06 (not registered) |
| Status today | **blocked: 06** |

## What it should do

Copy the current selection (visual mode) — or the current line when there is no
selection — into the register / system clipboard, leaving the buffer unchanged.

## Usability checklist

- [ ] Visual-mode selection copies exactly the highlighted span.
- [ ] No selection → copies the whole current line (with newline), JetBrains-style.
- [ ] Brief feedback ("copied N lines / N chars") so it's not silent.
- [ ] Integrates with system clipboard where the terminal allows; falls back to the vim register otherwise (documented).
- [ ] Round-trips with Paste (04/05): copy then paste reproduces content exactly, incl. linewise vs charwise semantics.
- [ ] **Conflict risk:** off macOS `Cmd+C`→`Ctrl+C` clashes with quit — resolve (leader/alt chord) before this goes live.

## Manual test protocol

1. Visual-select a span, copy, paste elsewhere → identical text.
2. No selection, copy, paste → whole line inserted linewise.
3. Confirm feedback message appears.
4. Off macOS: verify `Ctrl+C` does not both copy and quit.

## Verdict (you fill after testing)

- Status: ☐ pending · ☐ OK passt · ☐ needs change
- Notes:
- Follow-ups:
