---
type: concept
title: GitHub Issues Tool Window
description: Singleton pane listing the repository's open issues via the gh CLI — colored label chips, fuzzy + label filter, glamour-rendered detail view, linked-PR state, and the start-work action branching issue/<number>-<slug> off an up-to-date default branch (#1934).
resource: internal/ghissues/ghissues.go
tags: [architecture, vcs, github, issues, forge, tool-window, pane]
timestamp: 2026-08-24T00:00:00Z
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
call lives in a `tea.Cmd` with a timeout, results come back as messages.
Since #2083 the operations sit behind the **`Forge` interface** with two
bindings — gh for GitHub, tea + Gitea REST for Gitea/Forgejo — selected by
the origin remote's host; the full backend, detection and capability model is
its own concept: [Forge Layer](/architecture/forge.md). What the pane sees:

- `forge.RefreshCmd(dir)` → `IssuesMsg`. It first detects the backend: no
  usable backend (missing CLI, no git remote, no matching tea login)
  resolves to an explanatory `Setup` string (a state the user fixes outside
  the pane); a failing fetch resolves to `Err` (transient — the pane keeps
  its last listing). Otherwise the backend's issue listing (number, title,
  body, url, labels, assignees; limit 200) and PR listing (state, head
  branch, check rollup) fill the message; a failing PR listing drops only
  the PR states. Through detection the pane lists Gitea/Forgejo
  repositories the same way, unchanged.
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
- **Reveal** — `Reveal(number)` jumps straight to one issue's detail view,
  dropping the active filters first so a filtered-out issue stays reachable.
  It is what the forge event dialog's open action calls (#2086); when the
  listing does not carry the number yet, the app retries the reveal on the
  next `forge.IssuesMsg`.
- **States** — fetching, setup-missing (explanatory, never a hang: every
  subprocess is deadline-bounded), fetch-failed (last listing kept, `r`
  retries), no open issues, and nothing-matches-the-filter each render a
  distinct empty text.

## Prominent forge event notifications (#2086)

New forge events (the poller's `forge.EventsMsg`) do not settle for a toast:
`internal/app/forgenotify.go` gives each event kind its own style — a centered
dialog, a persistent status-line unread badge, a toast, or history only — with
a do-not-interrupt guard that turns a dialog into the badge while the user is
typing. Opening this tool window views the pending events and clears the badge;
the dialog's open action reveals the announced issue's detail view here. The
surface, its queueing rules and the `[forge.notify]` settings are documented in
[Notifications](/architecture/notifications.md).
