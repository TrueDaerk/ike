# 0082/27 — Keymap cheatsheet · `F1` / `Cmd+K Cmd+S`

| Field | Value |
|-------|-------|
| Chords | `f1` (delivered); `cmd+k cmd+s` (chord); app also `?` opens help overlay |
| Command id | `palette.keymapHelp` |
| Context | Global |
| Owner | this roadmap line / 07 (help overlay exists) |
| Status today | **partial** (help overlay exists; live keymap cheatsheet pending) |

## What it should do

Show a cheatsheet of all bindings grouped by context, with each chord's status
(live / blocked / macOS-only) and its working fallback — the discoverability
surface from 0081/40.

## Usability checklist

- [ ] Opens an overlay listing bindings grouped by context (Global first).
- [ ] Each row: chord(s), action, and a **status** badge (live / blocked:<roadmap> / macOS-only → fallback).
- [ ] Fragile/intercepted chords show their working alternative inline.
- [ ] Searchable/filterable; scrollable for the full set; responsive column reflow.
- [ ] Esc / same key closes; focus restored.
- [ ] Reflects config overrides (shows the *effective* binding, not just defaults).
- [ ] `F1`, `?`, and `Cmd+K Cmd+S` all reach the same sheet (consistent).

## Manual test protocol

1. `F1` → cheatsheet grouped by context with status badges.
2. A blocked binding shows "needs Roadmap NNNN"; a fragile one shows its fallback.
3. Filter/scroll through the list; reflow on resize.
4. Override a binding in config, reopen → effective chord shown.
5. `?` and `Cmd+K Cmd+S` open the same sheet.

## Verdict (you fill after testing)

- Status: ☐ pending · ☐ OK passt · ☐ needs change
- Notes:
- Follow-ups:
