# 0082/21 — Find in path · `Cmd+Shift+F`

| Field | Value |
|-------|-------|
| Chords | `cmd+shift+f` |
| Command id | `project.findInPath` |
| Context | Global |
| Owner | 09 |
| Status today | **blocked: 09** |

## What it should do

Project-wide text search: a query field plus a results view listing every match
(file:line + snippet); selecting a result jumps there.

## Usability checklist (project search UX)

- [ ] Query field with live or on-Enter search (documented); progress while scanning.
- [ ] Results grouped by file with match counts and code-context snippets.
- [ ] Matched text highlighted within each snippet.
- [ ] Options: case sensitivity, whole-word, regex, include/exclude globs.
- [ ] Respects .gitignore / hidden settings; large repos stay responsive (streaming results).
- [ ] Keyboard-navigable; Enter opens at the match and pushes nav stack.
- [ ] Empty/no-results state is clear; cancel mid-search stops work.
- [ ] Reuses palette/list UX where possible for consistency.

## Manual test protocol

1. Open, search a common token → grouped results with counts + highlighted snippets.
2. Toggle case/regex → results update correctly.
3. Apply an exclude glob → matching files drop out.
4. Enter on a result → jumps there; `Cmd+[` returns.
5. Search nonsense → clear empty state; cancel a long search.

## Verdict (you fill after testing)

- Status: ☐ pending · ☐ OK passt · ☐ needs change
- Notes:
- Follow-ups:
