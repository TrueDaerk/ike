# 0082/01 — Undo · `Ctrl+Z`

| Field        | Value                                      |
|--------------|--------------------------------------------|
| Chords       | `ctrl+z` (all platforms)                   |
| Command id   | `editor.undo` / `explorer.undo`            |
| Context      | Editor, Explorer                           |
| Owner        | 06 (registered)                            |
| Status today | **live**                                   |

> **Note:** the binding is `ctrl+z`, not `cmd+z`. macOS terminals do not forward
> the Cmd modifier to a TUI, so a `cmd+z` chord is never delivered there; `ctrl+z`
> arrives as a normal key on every platform (raw mode disables the suspend
> signal). The same caveat applies to every `cmd+*` default in Roadmap 0080's
> table when run in a terminal.

## What it should do

Revert the last buffer change as one logical step. Repeated presses walk the
undo history backwards. Cursor moves to the location of the undone change.

## Usability checklist

- [ ] One press undoes one *logical* edit (a whole insert run, not one keystroke).
- [ ] Cursor lands at the changed location, not at buffer top/0,0.
- [ ] Undo past the oldest change is a no-op with a quiet hint, not a crash/beep loop.
- [x] Works from normal mode; in insert mode `Cmd/Ctrl+Z` flushes the open insert session then reverts it (same behaviour as normal mode).
- [ ] Status line reflects modified/saved state correctly after undo (e.g. `[+]` clears if back to saved).
- [ ] Fast repeat (holding) doesn't desync cursor vs buffer.

## Manual test protocol

1. Open a file, type a word, press `Cmd+Z` → word removed in one step.
2. Make 3 edits, undo 3× → buffer returns to start, cursor follows each step.
3. Undo once more at the oldest state → no-op, no error.
4. Edit, save, edit, undo → modified indicator clears when back at saved content.

## Verdict (you fill after testing)

- Status: ☐ pending · ☑ OK passt · ☐ needs change
- Notes:
  - Root cause: `Cmd/Ctrl+Z` mid-insert dispatched `editor.undo` against history
    while the insert-session recorder was still open and uncommitted, so it
    no-opped or reverted the *previous* change against stale state — it only
    "worked" in normal mode after Esc. Fix: `undo()`/`redo()` flush the open
    insert session first (`internal/editor/actions.go`), so undo reverts the whole
    typed run as one unit from either mode. Whole-word/whole-paste undo already
    held in normal mode (each insert/paste is one `history.Change`).
- Follow-ups:
  - Explorer file-op undo shipped alongside this: `Cmd/Ctrl+Z` in the explorer
    context reverses the last create/delete (`explorer.undo`), with create
    (`a`), folder (`A`), and delete (`d`) added in `internal/explorer/fileops.go`.
    Deletes move to `.ike-trash/` so they can be restored. See the
    [File Explorer](../../wiki/architecture/explorer.md) wiki.
