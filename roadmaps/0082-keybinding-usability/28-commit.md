# 0082/28 — Commit · `Cmd+K`

| Field | Value |
|-------|-------|
| Chords | `cmd+k` (also leader prefix base `ctrl+k`); needs `space g c` alias |
| Command id | `vcs.commit` |
| Context | Global |
| Owner | **future VCS roadmap** |
| Status today | **blocked: VCS** |

## What it should do

Open a commit UI: staged-changes overview, a message editor, and commit (with
optional push). JetBrains `Cmd+K`.

## Usability checklist (commit dialog UX)

- [ ] Lists changed files with stage/unstage toggles and per-file status (M/A/D).
- [ ] Diff preview for the selected file.
- [ ] Message editor with subject/body separation; remembers in-progress message.
- [ ] Commit disabled with a hint when nothing staged or message empty.
- [ ] Feedback on success (hash/summary); errors (hooks, conflicts) surfaced clearly.
- [ ] Esc cancels without committing; in-progress message preserved.
- [ ] `cmd+k` collides with the leader prefix — resolve (this is *the* reason 0081's leader base matters); `space g c` is the reachable entry.

## Manual test protocol (once VCS roadmap lands)

1. Stage changes, open commit → file list + diff preview.
2. Toggle staging; empty message → commit disabled with hint.
3. Commit → success feedback; reopen → clean state.
4. Esc with a typed message → message preserved next open.

## Verdict (you fill after testing)

- Status: ☐ pending (blocked on VCS roadmap) · ☐ OK passt · ☐ needs change
- Notes:
- Follow-ups:
