# 0082/14 — Save · `Cmd+S`

| Field | Value |
|-------|-------|
| Chords | `cmd+s` (→ `ctrl+s` off macOS — may collide with terminal flow-control XOFF!) |
| Command id | `editor.save` (reconcile → registered `editor.write`) |
| Context | Editor |
| Owner | 06 (registered as `editor.write` / `:w`) |
| Status today | **live via `:w`**, chord needs id reconciliation |

## What it should do

Write the active buffer to disk. Honors editor config (trim trailing whitespace,
insert final newline). Clears the modified indicator; brief feedback.

## Usability checklist

- [ ] Saves the active editor's buffer to its path; status line shows "saved" + clears `[+]`.
- [ ] Respects `trim_trailing_whitespace` / `insert_final_newline` config on save.
- [ ] No path (scratch buffer) → prompts for a filename (or documented behavior).
- [ ] Save with no changes → no-op or quiet "nothing to save", no spurious write.
- [ ] Write error (permissions) → visible error, buffer stays modified.
- [ ] Cursor/scroll position unchanged after save.
- [ ] Id reconciled: palette, cheatsheet, and `Cmd+S` all hit one canonical command; `:w` still works.
- [ ] **Conflict:** off macOS `Ctrl+S` flow-control (XOFF freeze) — verify terminal doesn't freeze; resolve if so.

## Manual test protocol

1. Edit, `Cmd+S` → "saved", `[+]` clears, file on disk updated.
2. Save again with no edits → no-op/quiet.
3. With trim-trailing-whitespace on, add trailing spaces, save → removed.
4. Read-only file → error shown, still modified.
5. Off macOS: `Ctrl+S` doesn't freeze the terminal.

## Verdict (you fill after testing)

- Status: ☐ pending · ☐ OK passt · ☐ needs change
- Notes:
- Follow-ups:
