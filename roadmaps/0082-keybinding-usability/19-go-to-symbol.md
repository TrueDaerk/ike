# 0082/19 — Go to symbol · `Cmd+O`

| Field | Value |
|-------|-------|
| Chords | `cmd+o` (→ `ctrl+o` off macOS — collides with vim jump-back!) |
| Command id | `project.goToClass` |
| Context | Global |
| Owner | 09 / 10 (LSP symbols) |
| Status today | **blocked: 09/10** |

## What it should do

Fuzzy-find a symbol (class/function/type) across the project via LSP workspace
symbols; pick to jump to its definition.

## Usability checklist

- [ ] Fuzzy symbol search with kind icons/labels (func/type/const…).
- [ ] Shows containing file + line for each symbol.
- [ ] Ranking favours exact/prefix and shorter names; recent boosted.
- [ ] Optional scope toggle: current-file symbols vs whole-project.
- [ ] Enter jumps + pushes nav stack (23 returns); Esc restores focus.
- [ ] No LSP / still indexing → clear feedback, not empty silence.
- [ ] Reuses the palette list UX (consistent with go-to-file).
- [ ] **Conflict:** off macOS `Ctrl+O` is vim jump-back — resolve before live.

## Manual test protocol

1. Open, type a symbol fragment → ranked symbols with kind + location.
2. Enter → jumps to definition; `Cmd+[` returns.
3. Toggle current-file vs project scope.
4. Without LSP ready → feedback shown.
5. Decide `Ctrl+O` conflict resolution.

## Verdict (you fill after testing)

- Status: ☐ pending · ☐ OK passt · ☐ needs change
- Notes:
- Follow-ups:
