# 0082/05 — Paste · `Cmd+V`

| Field | Value |
|-------|-------|
| Chords | `cmd+v` (→ `ctrl+v` off macOS — collides with vim visual-block!) |
| Command id | `editor.paste` |
| Context | Editor |
| Owner | 06 (not registered) |
| Status today | **blocked: 06** |

## What it should do

Insert the register/clipboard content at the cursor. Linewise content pastes as
whole lines (below the current line, vim `p`-style); charwise pastes inline.

## Usability checklist

- [ ] Charwise paste inserts inline at cursor; linewise paste opens new line(s).
- [ ] Cursor ends at a predictable spot (end of pasted text, JetBrains-like).
- [ ] Multi-line paste preserves indentation; no doubled newlines.
- [ ] One undo removes the whole paste in one step.
- [ ] Pasting into a selection replaces it (or is documented as not).
- [ ] **Conflict risk:** off macOS `Cmd+V`→`Ctrl+V` clashes with vim visual-block — resolve before live.
- [ ] System-clipboard paste sanitises nothing unexpectedly (tabs/CRLF handled).

## Manual test protocol

1. Copy a word, move cursor, paste → inline, cursor after it.
2. Copy 3 lines linewise, paste → 3 new lines below; undo removes all 3.
3. Select a span, paste over it → replaced (or note behavior).
4. Off macOS: confirm `Ctrl+V` paste vs visual-block decision.

## Verdict (you fill after testing)

- Status: ☐ pending · ☐ OK passt · ☐ needs change
- Notes:
- Follow-ups:
