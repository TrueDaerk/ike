# 0082/20 — Recent files · `Cmd+E`

| Field | Value |
|-------|-------|
| Chords | `cmd+e` (→ `ctrl+e` off macOS — collides with vim scroll-down!) |
| Command id | `palette.recentFiles` |
| Context | Global |
| Owner | 07 (palette) |
| Status today | **blocked: 07** |

## What it should do

Open a most-recently-used file list (newest first) for instant switching, the
JetBrains "Recent Files" switcher.

## Usability checklist

- [ ] MRU order, most recent at top; current file excluded or clearly marked.
- [ ] Opens with the *previous* file preselected so a press+Enter toggles back-and-forth fast.
- [ ] Optional hold-to-cycle: repeated `Cmd+E` steps down the list (JetBrains feel) — or documented as not.
- [ ] Type-to-filter narrows the list.
- [ ] Shows path context for disambiguation.
- [ ] Survives across sessions (recent persisted) or documented as session-only.
- [ ] Enter opens; Esc restores focus.
- [ ] **Conflict:** off macOS `Ctrl+E` is vim scroll — resolve before live.

## Manual test protocol

1. Open several files, press → MRU list, previous file preselected.
2. Press+Enter → toggles to the previous file quickly.
3. Type to filter the list.
4. Restart IKE → recent list persists (if intended).
5. Decide `Ctrl+E` conflict resolution.

## Verdict (you fill after testing)

- Status: ☐ pending · ☐ OK passt · ☐ needs change
- Notes:
- Follow-ups:
