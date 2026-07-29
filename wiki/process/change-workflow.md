---
type: process
title: Change Workflow
description: The lifecycle of every change — issue first, issue branch, version bump, PR, merge, branch cleanup.
tags: [process, git, release]
resource: internal/version/version.go
timestamp: 2026-07-29T00:00:00Z
---

# Change Workflow

Every change — feature, fix, docs — goes through the same loop. No direct commits to `main`.

1. **Issue first.** Before touching code, make sure a GitHub issue exists for the change
   (after the duplicate check in [GitHub Issue Workflow](/process/issues.md)); create one otherwise.
2. **Branch per issue.** Work on `issue/<number>-<slug>` (e.g. `issue/12-project-picker`),
   branched from an up-to-date `main`. Reference the issue in commits where useful.
3. **When done, bump the patch version.** Raise the last component of `Version` in
   `internal/version/version.go` (e.g. `0.1.6` → `0.1.7`) as part of the branch, in its own
   `chore(version)` commit.
4. **Open a PR on GitHub.** Body says `Closes #<number>` so the issue auto-closes on merge.
5. **Merge into `main`.** Merging closes the issue and finishes the task — don't leave PRs
   open or issues dangling.
6. **Clean up.** Delete the issue branch (local and remote), switch back to `main`, and pull.

Definition of done and epic bookkeeping (ticking the task list, closing milestones) are in
[GitHub Issue Workflow](/process/issues.md).
