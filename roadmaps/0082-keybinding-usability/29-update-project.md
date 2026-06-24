# 0082/29 — Update project · `Cmd+T`

| Field | Value |
|-------|-------|
| Chords | `cmd+t` (intercepted by macOS/terminal); needs `space g u` alias |
| Command id | `vcs.updateProject` |
| Context | Global |
| Owner | **future VCS roadmap** |
| Status today | **blocked: VCS** |

## What it should do

Pull/update the working copy from the remote (fetch + merge/rebase), JetBrains
"Update Project", with progress and a summary of what changed.

## Usability checklist

- [ ] Clear progress while fetching/merging; non-blocking UI.
- [ ] Summary of incoming changes (files, commits) on success.
- [ ] Conflicts surfaced with a path to resolution, not a silent failure.
- [ ] Dirty working tree handled (stash prompt or clear warning) before update.
- [ ] Update strategy (merge vs rebase) configurable or documented.
- [ ] Errors (no remote, auth) shown plainly.
- [ ] `cmd+t` is intercepted → `space g u` is the reachable entry; documented.

## Manual test protocol (once VCS roadmap lands)

1. With incoming remote commits, run → progress then summary of changes.
2. Local uncommitted changes → stash/warn flow, no surprise loss.
3. Force a conflict → surfaced with resolution guidance.
4. No remote configured → clear error.

## Verdict (you fill after testing)

- Status: ☐ pending (blocked on VCS roadmap) · ☐ OK passt · ☐ needs change
- Notes:
- Follow-ups:
