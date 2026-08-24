---
type: concept
title: Issues Tool Window
description: Singleton pane over the repository's forge listing — tabbed Issues/PRs views, fuzzy filter, label multi-picker, open/closed/all state filter, sort orders and label grouping, a full-area issue detail that keeps the list context, an action menu, and the start-work action branching issue/<number>-<slug> off an up-to-date default branch (#1934, #2090).
resource: internal/ghissues/ghissues.go
tags: [architecture, vcs, github, gitea, issues, forge, tool-window, pane]
timestamp: 2026-08-24T00:00:00Z
---

# Issues Tool Window (#1934, #2090)

Development in this repository is issue-driven (see
[Change Workflow](/process/change-workflow.md)); this pane brings that loop
into the IDE: browse the issues and pull requests of the current project's
forge repository, read one, and start work on it — creating the
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

- `forge.RefreshStateCmd(dir, state)` → `IssuesMsg`, wrapped for the pane as
  the `forge.RefreshFactory(dir)` closure the app injects: the pane calls it
  with whatever its state filter selects (`open`, `closed`, `all`), so the
  state filter stays a pane concern and the pane stays subprocess-free.
  Detection runs first: no usable backend (missing CLI, no git remote, no
  matching tea login) resolves to an explanatory `Setup` string (a state the
  user fixes outside the pane); a failing fetch resolves to `Err` (transient
  — the pane keeps its last listing). Otherwise the backend's issue listing
  (number, title, body, url, state, author, timestamps, labels, assignees;
  limit 200) and the PR listing fill the message. Pull requests are always
  fetched in **every** state and split client-side, so cycling the issue
  state never re-costs the PR tab. A failing PR listing drops only the PR
  states. `IssuesMsg.State` echoes what was asked for.
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
`ui.ListNav` navigation): the app injects the fetch factory and routes
`forge.IssuesMsg` in; the pane never runs a subprocess. Its chrome is four
regions — tab bar, optional filter row, full-area body, footer.

### Tabbed views

Row 0 is the **tab bar**: `Issues n │ PRs n` with the filtered counts, the
active label accented, the fetch state (`fetching…`, `unavailable`,
`fetch failed`) right-aligned. `tab`/`shift+tab` — and the delivered
`ctrl+pgdown`/`ctrl+pgup` — walk the views; a click on a label switches
directly (`tabBarSpans` mirrors the rendering geometry, so hits land on what
was drawn). Each view keeps its own cursor and scroll, so switching back and
forth never loses the place.

- **Issues** — one row per issue: accented `#number`, title, then the
  metadata columns — label chips in the forge-assigned colors (black/white
  text picked by luminance; unparsable colors degrade to `[name]`),
  `@assignees`, the author, and the relative age (`ui.ShortAge`). The
  metadata shrinks in tiers before the title truncates: chips and assignee go
  first, then the author, then everything but the age. While the state filter
  shows more than the open issues, a `✔`/`●` glyph marks each row's state.
- **PRs** — the pull requests full width: `#number` in the state's color,
  title, head branch, review decision (`approved`/`changes`/`review`, blank
  on backends that report none), the state with its CI glyph (`⇌` merged,
  `×` closed, `✓`/`…`/`✗` rollup) and the age. The PR *detail* is #2089's
  scope; here `enter` opens the pull request in the browser.

### Filtering, sorting, grouping

The keys are QWERTZ-safe throughout (#48, #2064): plain letters and delivered
`ctrl+letter` chords only — no symbol that needs Shift or AltGr.

- **`f`** (with **`/`** kept as an alias) opens the filter line, the dataview
  pattern (#1777): the line owns the keyboard, esc clears + closes, enter
  keeps. It narrows live via `internal/fuzzy` over number, title, labels,
  assignees and author (head branch on the PR tab).
- **`l`** opens the **label multi-picker**, a centered overlay listing every
  distinct label with its issue count: `space` toggles and re-narrows live,
  `backspace` clears the selection, `enter` keeps it, `esc` restores what the
  picker opened with. The selection is an **OR** filter — picking `bug` and
  `feature` widens to both.
- **`t`** cycles the **state filter** open → closed → all. Closed issues are
  not part of an open listing, so the change refetches through the injected
  factory; the PR tab re-splits its already-fetched listing (open / merged +
  closed / everything).
- **`a`** cycles the **sort order**: `relevance` (fuzzy score while a pattern
  is typed, newest without one — the pane's pre-#2090 order), `newest`,
  `oldest`, `updated`, `number`. Every comparator is stable, so entries the
  order cannot separate (missing timestamps, equal scores) keep the listing
  order.
- **`g`** toggles **grouping by label**: each issue is filed under its
  alphabetically first label (unlabelled ones under `(no label)`, sorted
  last), so it appears exactly once however many labels it carries. Group
  headers are rows the cursor never rests on — key and wheel navigation snap
  off them in the direction of travel. Because `g` is spent here, the list
  runs on `ui.NavDefault|ui.NavVim` rather than `NavFull`; `home`/`end` still
  jump to the extremes.
- **`esc`** on a list clears every narrowing at once.

Whenever any of these differs from the pane's defaults — or while the filter
line is open — a **filter row** below the tab bar spells the state out
(`match: expl · labels: bug, feature · state: all · sort: number · grouped
by label`), so a narrowed list can never look like an empty repository.

### Detail as a real view

`enter` (or a double-click) opens the selected issue **full area**, above a
header line naming it and its place in the filtered listing (`issue 3/17`).
The list's cursor and scroll are untouched while it is open, so `esc` returns
to exactly the row it left. `ctrl+j`/`ctrl+k` walk to the next/previous issue
without going back through the list, moving the list cursor with them; `j/k`
and the page keys scroll the body. The body itself is the issue's markdown
rendered through glamour with the preview pane's theme mapping (#62), under
an author/age/state line and — when one exists — the linked PR's state. The
richer timeline content is #2084's scope.

### Discoverability

One table backs both the **footer** (`enter detail · s start work · o browser
· f filter · …`) and the **action menu** (`m`, with `?` as an alias), a
centered overlay listing every action of the *current* view with its key and
a full sentence; `enter` runs the selected one. A key can therefore never be
in one and missing from the other. Mouse: wheel scrolls the active view (or
the open overlay / detail), a click selects, a second click on the same row
within 400 ms activates it, and clicks on the tab bar or on an overlay row
work too.

### Settings

Two keys seed a freshly opened pane; switching by hand wins for the rest of
the session, so a live config reload never yanks the view away.

| Key | Values | Meaning |
| --- | --- | --- |
| `issues.default_tab` | `issues`, `prs` | which view the pane opens on |
| `issues.default_sort` | `relevance`, `newest`, `oldest`, `updated`, `number` | the order both lists start in |

Both are `Enum` entries on the Settings UI's **Issues Window** page and are
clamped with a diagnostic when a config file names something else.

### Actions and states

- **Actions** — `s` emits `StartWorkRequestMsg` (the app answers with
  `forge.StartWorkCmd`, toasts the outcome and invalidates the VCS
  snapshot); `o` emits `OpenURLMsg` (platform browser); `r` re-runs the
  injected refresh for the current state filter. All three work from the
  list and the detail.
- **Reveal** — `Reveal(number)` jumps straight to one issue's detail view,
  dropping the active filters first so a filtered-out issue stays reachable.
  It is what the forge event dialog's open action calls (#2086); when the
  listing does not carry the number yet, the app retries the reveal on the
  next `forge.IssuesMsg`.
- **States** — fetching, setup-missing (explanatory, never a hang: every
  subprocess is deadline-bounded), fetch-failed (last listing kept, `r`
  retries), no issues in the selected state, and nothing-matches-the-filter
  each render a distinct empty text.

## Prominent forge event notifications (#2086)

New forge events (the poller's `forge.EventsMsg`) do not settle for a toast:
`internal/app/forgenotify.go` gives each event kind its own style — a centered
dialog, a persistent status-line unread badge, a toast, or history only — with
a do-not-interrupt guard that turns a dialog into the badge while the user is
typing. Opening this tool window views the pending events and clears the badge;
the dialog's open action reveals the announced issue's detail view here. The
surface, its queueing rules and the `[forge.notify]` settings are documented in
[Notifications](/architecture/notifications.md).
