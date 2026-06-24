# 0082/11 — Go to declaration · `Cmd+B`

| Field | Value |
|-------|-------|
| Chords | `cmd+b` (→ `ctrl+b` off macOS — collides with vim page-up!) |
| Command id | `editor.gotoDeclaration` |
| Context | Editor |
| Owner | 06 / 10 (LSP) |
| Status today | **blocked: 06/10** |

## What it should do

Jump to the declaration/definition of the symbol under the cursor (via LSP).
Pushes the origin onto the navigation stack so Navigate-back (23) returns.

## Usability checklist

- [ ] Jumps precisely to the symbol's definition, opening the target file if needed.
- [ ] Origin pushed to nav stack → `Cmd+[` returns to the call site.
- [ ] Definition in another file opens it in a pane sensibly (reuse vs new pane — documented).
- [ ] Multiple candidates → a picker (reuses palette list UX) rather than guessing.
- [ ] No symbol / no LSP / no result → clear hint, no silent nothing.
- [ ] Works while LSP is still indexing → "indexing…" feedback rather than wrong jump.
- [ ] **Conflict:** off macOS `Ctrl+B` (page-up) vs go-to — resolve before live.

## Manual test protocol

1. Cursor on a function call, press → lands on its definition.
2. `Cmd+[` → back at the call site.
3. Definition in another file → opens correctly.
4. Symbol with multiple defs → picker appears.
5. Press on a non-symbol → graceful hint.

## Verdict (you fill after testing)

- Status: ☐ pending · ☐ OK passt · ☐ needs change
- Notes:
- Follow-ups:
