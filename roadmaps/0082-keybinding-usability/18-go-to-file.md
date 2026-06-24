# 0082/18 — Go to file · `Cmd+Shift+O`

| Field | Value |
|-------|-------|
| Chords | `cmd+shift+o`; editor `@` opens the file finder today |
| Command id | `project.goToFile` |
| Context | Global |
| Owner | 09 (palette `@` file mode partially exists) |
| Status today | **partial** (`@` finder works; dedicated command/chord pending 09) |

## What it should do

Open a fuzzy file finder over the project; type part of a path/name, pick, open.
Already partly present via the palette `@` mode and the anchored `@` finder.

## Usability checklist (file finder UX)

- [ ] Fuzzy path matching with highlighted matched chars.
- [ ] Results ranked: filename matches over path matches; recent/open boosted.
- [ ] Shows path context to disambiguate same-named files.
- [ ] Respects ignore rules (.gitignore / hidden) consistent with the explorer.
- [ ] Incremental, no lag on large trees.
- [ ] Enter opens (honoring open-in-new-pane intent where applicable); Esc restores focus.
- [ ] Remembers/preselects last pick or current file sensibly.
- [ ] Dedicated `Cmd+Shift+O` and `@` give the same finder (no two divergent UIs).

## Manual test protocol

1. Open finder, type a fragment → fuzzy ranked results with highlights.
2. Two files same name → path context distinguishes them.
3. Hidden/ignored files behave per explorer settings.
4. Enter opens; Esc restores focus.
5. Compare `@` finder vs `Cmd+Shift+O` — consistent.

## Verdict (you fill after testing)

- Status: ☐ pending · ☐ OK passt · ☐ needs change
- Notes:
- Follow-ups:
