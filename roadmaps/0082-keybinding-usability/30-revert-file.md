# 0082/30 — Revert file · `Cmd+Shift+T`

| Field | Value |
|-------|-------|
| Chords | `cmd+shift+t`; needs `space g x` alias |
| Command id | `vcs.revertFile` |
| Context | Global |
| Owner | **future VCS roadmap** |
| Status today | **blocked: VCS** |

## What it should do

Discard local changes in the current file, restoring it to the last committed
(HEAD) version — JetBrains "Rollback". Destructive, so it must confirm.

## Usability checklist

- [ ] **Confirmation prompt** before discarding (this is destructive) — never one-press silent revert.
- [ ] Shows what will be lost (changed line count / brief diff) in the prompt.
- [ ] Reverts only the active file to HEAD; open buffer reloads to the reverted content.
- [ ] Cursor/scroll restored sensibly after reload.
- [ ] No-op with a hint when the file has no changes.
- [ ] Untracked file → clear behavior (cannot revert to HEAD; documented).
- [ ] Optional single-undo safety net or clearly-stated irreversibility.
- [ ] `cmd+shift+t` reachability checked; `space g x` documented alias.

## Manual test protocol (once VCS roadmap lands)

1. Modify a tracked file, revert → confirm prompt with change summary.
2. Confirm → buffer reloads to HEAD content; cursor sane.
3. Cancel at prompt → changes intact.
4. Revert an unchanged file → no-op hint.
5. Try on an untracked file → defined behavior.

## Verdict (you fill after testing)

- Status: ☐ pending (blocked on VCS roadmap) · ☐ OK passt · ☐ needs change
- Notes:
- Follow-ups:
