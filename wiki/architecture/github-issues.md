---
type: concept
title: GitHub Issues Tool Window
description: Singleton pane listing the repository's open issues via the gh CLI — colored label chips, fuzzy + label filter, glamour-rendered detail view, linked-PR state, and the start-work action branching issue/<number>-<slug> off an up-to-date default branch (#1934).
resource: internal/ghissues/ghissues.go
tags: [architecture, vcs, github, issues, forge, tool-window, pane]
timestamp: 2026-08-18T00:00:00Z
---

# GitHub Issues Tool Window (#1934)

Development in this repository is issue-driven (see
[Change Workflow](/process/change-workflow.md)); this pane brings that loop
into the IDE: browse the open issues of the current project's GitHub
repository, read one, and start work on it — creating the
`issue/<number>-<slug>` branch — without leaving the terminal.

`issues.toggle` ("GitHub Issues", Tools menu) drives the shared singleton
state machine (no pane → open + first fetch; unfocused → focus; focused →
return focus), key `issues`, adaptive `auxZone` placement, slot-template
assignable as `issues`, hidden by `window.hideAllTools`, persisted as
`{kind: "issues"}` and restored empty with its refresh armed.

## The forge layer (`internal/forge`)

The subprocess side mirrors `internal/vcs`: nothing runs from `Update`, every
call lives in a `tea.Cmd` with a timeout, results come back as messages. The
types (`Issue`, `Label`, `PR`, `CheckState`) are forge-agnostic — a Gitea
binding via `tea` can produce them later — while the shipped implementation
shells out to the **gh CLI** with `--json`, never scraping human output:

- `forge.RefreshCmd(dir)` → `IssuesMsg`. It first checks the setup: no `gh`
  on PATH, no git remote, or a non-GitHub origin resolve to an explanatory
  `Setup` string (a state the user fixes outside the pane); a failing fetch
  resolves to `Err` (transient — the pane keeps its last listing). Otherwise
  `gh issue list --state open --json …` (number, title, body, url, labels,
  assignees; limit 200) and `gh pr list --state all --json …` (state,
  headRefName, statusCheckRollup) fill the message; a failing PR listing
  drops only the PR states.
- `forge.PRForIssue(prs, n)` joins PRs to issues by the branch convention:
  head `issue/<n>` or `issue/<n>-…`, preferring open over merged over closed.
  Each PR's `statusCheckRollup` (CheckRun and StatusContext shapes both)
  folds into one `CheckState`: failing beats pending beats passing.
- `forge.BranchName(n, title)` derives the branch: lower-cased title,
  non-alphanumeric runs collapsed to one dash, capped at 50 characters —
  exactly the convention of the existing branches.
- `forge.StartWorkCmd(dir, n, title)` → `StartWorkDoneMsg`. The flow refuses
  a dirty worktree with a clear message; resolves the default branch
  (`origin/HEAD`, falling back to `main`/`master`); fetches it (30s
  timeout); then `checkout -b` off `origin/<default>` — or, when the fetch
  fails (offline), off the local default with a warning instead of blocking.

## The pane (`internal/ghissues`)

A pure consumer in the Usages mold (value model, pointer-receiver mutators,
`ui.ListNav` navigation): the app injects the refresh command and routes
`forge.IssuesMsg` in; the pane never runs a subprocess.

- **List** — one row per issue: accented `#number`, title, right-aligned
  label chips in the forge-assigned colors (black/white text picked by
  luminance; unparsable colors degrade to `[name]`), `@assignees`, and the
  linked PR as `PR#n` with a state glyph (⇌ merged, × closed, ✓/…/✗ CI).
- **Filtering** — `/` opens a filter line (the dataview pattern: the line
  owns the keyboard, esc clears + closes, enter keeps) narrowing live via
  `internal/fuzzy` over number, title, labels and assignees, score-ranked;
  `l` cycles a hard label filter through the distinct labels and back to
  none. Active filters show in the header.
- **Detail** — enter (or double-click) renders the issue body through
  glamour with the preview pane's theme mapping; j/k and page keys scroll,
  esc returns. A linked PR renders as a bold state line above the body.
- **Actions** — `s` emits `StartWorkRequestMsg` (the app answers with
  `forge.StartWorkCmd`, toasts the outcome and invalidates the VCS
  snapshot); `o` emits `OpenURLMsg` (platform browser); `r` re-runs the
  injected refresh. All three work from list and detail.
- **States** — fetching, setup-missing (explanatory, never a hang: every
  subprocess is deadline-bounded), fetch-failed (last listing kept, `r`
  retries), no open issues, and nothing-matches-the-filter each render a
  distinct empty text.
