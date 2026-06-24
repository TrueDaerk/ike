# 0082/09 — Find in file · `Cmd+F`

| Field | Value |
|-------|-------|
| Chords | `cmd+f` (→ `ctrl+f` off macOS — collides with vim page-down!); editor also `/` (vim) |
| Command id | `editor.find` |
| Context | Editor |
| Owner | 06 (editor has `/` search internally) |
| Status today | **blocked: 06** (chord), search engine exists |

## What it should do

Open an incremental in-buffer search field. As the user types, matches highlight
live and the view jumps to the nearest match; Enter/`n` go next, `N` previous,
Esc cancels and restores the pre-search cursor.

## Usability checklist (the search field is the point)

- [ ] Search box appears in a clear, fixed spot (status line / floating), with the cursor in it.
- [ ] **Incremental**: highlights update on every keystroke, view scrolls to first match.
- [ ] All matches highlighted; current match visually distinct from the rest.
- [ ] Match counter ("3/17") shown.
- [ ] `n`/Enter = next, `N`/Shift+Enter = previous; wraps around with a subtle "wrapped" hint.
- [ ] Esc cancels → cursor returns to where it was before the search.
- [ ] Empty query / no matches → clear "no matches", no jump, no error.
- [ ] Case sensitivity behavior defined (smartcase?) and toggleable or documented.
- [ ] Consistent with vim `/` (same engine, no two divergent searches).
- [ ] **Conflict:** off macOS `Ctrl+F` (page-down) vs find — resolve before live.

## Manual test protocol

1. `Cmd+F`, type a substring → live highlight + jump + counter.
2. `n`/`N` cycle through matches, wrap at ends.
3. Esc → cursor back at origin.
4. Search a non-existent string → "no matches", nothing moves.
5. Compare with vim `/` for the same query.

## Verdict (you fill after testing)

- Status: ☐ pending · ☐ OK passt · ☐ needs change
- Notes:
- Follow-ups:
