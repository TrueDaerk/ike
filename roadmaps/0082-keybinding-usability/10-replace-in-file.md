# 0082/10 — Replace in file · `Cmd+R`

| Field | Value |
|-------|-------|
| Chords | `cmd+r` (→ `ctrl+r` off macOS — collides with vim redo!); editor `:s` |
| Command id | `editor.replace` |
| Context | Editor |
| Owner | 06 (`:substitute` exists internally) |
| Status today | **blocked: 06** (chord) |

## What it should do

Open a find + replace UI: a search field and a replacement field, with
per-match confirm and replace-all. Live highlight of matches like Find (09).

## Usability checklist

- [ ] Two clearly-labelled fields (Find / Replace), Tab moves between them.
- [ ] Matches highlight live as the Find field changes (reuses Find UX).
- [ ] Actions visible: Replace (current), Replace All, Skip/Next, Cancel — with their keys shown.
- [ ] Per-match confirm flow: jumps to match, preview of the result, y/n/all.
- [ ] Match counter + "N replaced" feedback at the end.
- [ ] Replacement preserves surrounding text; supports capture refs or documents that it doesn't.
- [ ] Esc cancels with no partial mutation left behind (or undoable in one step).
- [ ] Consistent with `:s///` semantics (no divergent engine).
- [ ] **Conflict:** off macOS `Ctrl+R` is vim redo — resolve before live.

## Manual test protocol

1. `Cmd+R`, type find + replace, Tab between fields.
2. Replace-all → counter shows "N replaced"; undo reverts all in one step.
3. Per-match confirm: y replaces, n skips, all finishes.
4. Esc mid-flow → no leftover partial changes.
5. Cross-check with `:s/foo/bar/g`.

## Verdict (you fill after testing)

- Status: ☐ pending · ☐ OK passt · ☐ needs change
- Notes:
- Follow-ups:
