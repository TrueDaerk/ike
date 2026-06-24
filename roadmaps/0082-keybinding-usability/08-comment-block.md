# 0082/08 — Comment block · `Cmd+Shift+/`

| Field | Value |
|-------|-------|
| Chords | `cmd+shift+/` (→ `ctrl+shift+/` off macOS) |
| Command id | `editor.commentBlock` |
| Context | Editor |
| Owner | 06 (not registered) |
| Status today | **blocked: 06** |

## What it should do

Wrap the selection in a block comment (`/* … */`, `<!-- … -->`) using the
language's block syntax. Toggling removes the wrapping.

## Usability checklist

- [ ] Correct block tokens per filetype; languages without block comments fall back to line-comment-each (documented).
- [ ] Wraps exactly the selection bounds (char-precise, not whole lines unless line-selected).
- [ ] Toggle removes a previously added block comment cleanly.
- [ ] Nested/overlapping block comments handled or prevented (no broken syntax).
- [ ] Cursor/selection sensible after wrap.
- [ ] Single undo reverts.

## Manual test protocol

1. Select an expression in Go, press → wrapped in `/* */`; press → unwrapped.
2. In HTML, select text → `<!-- -->`; toggle back.
3. In a language without block comments → falls back to line comments per line.
4. Selection spanning partial lines → wrap is char-precise.

## Verdict (you fill after testing)

- Status: ☐ pending · ☐ OK passt · ☐ needs change
- Notes:
- Follow-ups:
