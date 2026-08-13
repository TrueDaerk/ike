---
type: process
title: GitHub Issue Workflow
description: How planning is structured on TrueDaerk/ike — epics, sub-issues, milestones, labels, conventions, and the duplicate check.
tags: [process, github, planning]
timestamp: 2026-07-29T00:00:00Z
---

# GitHub Issue Workflow

All planning and work tracking happens as GitHub issues on `TrueDaerk/ike` (use the `gh` CLI).
There is no roadmap directory anymore — the structure is:

- **Epic issue** (label `epic` + `roadmap:NNNN`): holds the full spec verbatim (architecture,
  design rules, milestones) and a `- [ ] #N` task list of its sub-issues. One epic per work stream.
  Current epics: #37 (0090 Project Switching), #38 (0100 LSP deferred), #39 (0081 Keybinding Audit),
  #40 (0082 Usability Review), #41 (9900 WASM Plugins).
- **Sub-issue**: one independently completable, reviewable task, linked from its epic's task list.
- **GitHub milestone** (one per epic): assigned to the epic and all its sub-issues; its progress
  bar is the progress tracking. Close the milestone when the epic is done.

## Labels

| Label | Meaning |
|---|---|
| `epic` | Umbrella issue holding a full spec + sub-issue task list. |
| `roadmap:NNNN` | Work-stream tag (e.g. `roadmap:0090`); shared by an epic and all its sub-issues. New stream → new label + new milestone. |
| `idea` | Gap-analysis proposal, not yet planned. Promoting an idea = write the spec into a new **epic** issue, create its `roadmap:NNNN` label + milestone, split into sub-issues, close the idea with a link to the epic. |
| `bug` | Defect in shipped behavior. |
| `enhancement` | New feature or improvement (usually combined with `roadmap:*` or `idea`). |
| `documentation` | Wiki / README work only. |
| `model:opus` / `model:fable` | Which Claude model a runner (e.g. `issue_runner.py`) should use — opus for simple/medium tasks, fable for complex, reasoning-heavy ones. |
| `effort:low` / `effort:medium` / `effort:high` | Reasoning effort for the runner; `high` is the maximum allowed. |
| `priority:lowest` … `priority:highest` | Relative priority for picking work. |
| `size:1d` / `size:1-7d` / `size:7d+` | Rough duration estimate — 1 day or less, 1-7 days, or more than 7 days. Every work-item issue should carry one; no default if missing. |

## Issue conventions

- **Title:** sub-issues are prefixed with their stream number — `0090: <what>` (sub-doc form
  `0081/30: <what>`); epics use `Epic NNNN: <name> (spec)`; ideas use `idea: <what>`.
  Titles and bodies in English.
- **Body:** link the spec (`spec: #<epic>`), list concrete acceptance criteria as a `- [ ]`
  checklist, name dependencies by issue number (`Depends on #12`), and include tests + wiki updates
  in the checklist when they apply.
- **Scope:** one issue = one independently completable, reviewable task. Split rather than batch.

## Before creating an issue: check for duplicates

1. Search open **and** closed issues: `gh issue list --state all --search "<keywords>"` (try both
   feature terms and the stream number).
2. Check the stream's label: `gh issue list --state all --label "roadmap:NNNN"`.
3. Skim the matching epic's task list.
4. If a matching issue exists, extend/comment on it instead of opening a new one; if it exists but
   is closed and the problem is back, reopen it.

## Working an issue

The step-by-step change workflow (branch, version bump, PR, merge, cleanup) lives in
[Change Workflow](/process/change-workflow.md). Issue-side rules on top of it:

1. Pick an issue whose dependencies (`Depends on #N`) are closed; comment briefly that work starts
   (or assign yourself).
2. Definition of done: acceptance checklist ticked, tests pass, wiki updated where behavior
   changed, the epic's task-list box for this issue ticked. When the last sub-issue closes,
   close the epic and its milestone.
3. Discoveries out of scope while working an issue become **new** issues (after the duplicate
   check), not scope creep on the current one. New sub-issues get added to their epic's task
   list and milestone.
