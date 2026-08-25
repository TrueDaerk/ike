---
type: concept
title: Issues Tool Window
description: Singleton pane over the repository's forge listing — tabbed Issues/PRs views, fuzzy filter, label multi-picker, open/closed/all state filter, sort orders and label grouping, a full-area issue detail with the issue's paginated timeline (comments, label/state/assignee events), a full-area PR detail with per-check CI status and merge/close-with-comment behind a confirm dialog plus an offered post-merge branch cleanup, an action menu, permission-gated label/assignee/state mutations with optimistic rollback, editing your own texts and composing comments in markdown buffers, and the start-work action branching issue/<number>-<slug> off an up-to-date default branch (#1934, #2090, #2084, #2088, #2087, #2089).
resource: internal/ghissues/ghissues.go
tags: [architecture, vcs, github, gitea, issues, forge, tool-window, pane]
timestamp: 2026-08-25T12:00:00Z
---

# Issues Tool Window (#1934, #2090, #2084, #2088, #2087, #2089)

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
  with whatever its state filter selects (`open`, `closed`, `all`) plus its
  request generation (#2107), so the state filter stays a pane concern and the
  pane stays subprocess-free.
  Detection runs first: no usable backend (missing CLI, no git remote, no
  matching tea login) resolves to an explanatory `Setup` string (a state the
  user fixes outside the pane); a failing fetch resolves to `Err` (transient
  — the pane keeps its last listing). Otherwise the backend's issue listing
  (number, title, body, url, state, author, timestamps, labels, assignees;
  limit 200) and the PR listing fill the message. Pull requests are always
  fetched in **every** state and split client-side, so cycling the issue
  state never re-costs the PR tab. A failing PR listing drops only the PR
  states. `IssuesMsg.State` echoes what was asked for, and `IssuesMsg.Gen`
  echoes the request generation.
- `forge.TimelineCmd(dir, issue, page)` → `TimelineMsg` (#2084), injected as
  the `forge.TimelineFactory(dir)` closure: one page (30 entries, oldest
  first) of an issue's history in the neutral `TimelineEntry` vocabulary —
  `comment` (markdown body, stable forge comment ID, an *own-comment* flag
  matched against the authenticated user's login), `labeled`/`unlabeled`
  (label name + color), `closed`/`reopened`, `assigned`/`unassigned`. Both
  bindings map their forge's timeline onto it (GitHub's
  `issues/{n}/timeline`, Gitea's typed timeline comments) and drop event
  kinds outside the vocabulary; `More` reports whether another page follows,
  so long histories are never fetched whole. The message echoes issue and
  page, letting the pane drop a stale answer.
- `forge.MutateCmd(dir, mutation)` → `MutationMsg` and `forge.RepoMetaCmd(dir)`
  → `RepoMetaMsg` (#2088), injected as `forge.MutateFactory(dir)` and
  `forge.MetaFactory(dir)`: one neutral `Mutation` value carries a label diff,
  an assignee set, a state change and an optional comment, applied in that
  reading order; the metadata probe delivers the capabilities — including the
  authenticated login the #2087 ownership checks read — plus, only when they
  allow triage, the repository's label set and assignable users.
- `forge.PRDetailCmd(dir, pr)` → `PRDetailMsg` and `forge.PRActionCmd(dir,
  action)` → `PRActionMsg` (#2089), injected as `forge.PRDetailFactory(dir)`
  and `forge.PRActionFactory(dir)`: one full pull request (description, base
  branch, mergeability, merge method, per-check CI results) and the neutral
  merge/close-with-comment write, comment posted first. The bindings, their
  merge-method resolution and the forge-reason error surface are documented in
  [Forge Layer](/architecture/forge.md); `forge.CleanupBranchCmd(dir, branch)`
  → `CleanupDoneMsg` there is what the accepted post-merge cleanup offer runs.
- `forge.SaveTextCmd(dir, path, target, base, body, force)` → `SaveTextMsg`
  (#2087): the push a saved edit buffer runs, with the stale-base check in
  front of it. The pane never calls it — the app does, for the buffer it owns.
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
  `×` closed, `✓`/`…`/`✗` rollup) and the age. `enter` opens the full-area
  PR detail (#2089); `o` opens the pull request in the browser.

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
  closed / everything). The issue list is **cleared** for the width of that
  refetch and shows `(fetching issues…)`: the rows on screen were fetched for
  the *previous* filter, so keeping them would render an open-only set under a
  "closed" heading (#2107). The PR list is not cleared — it is fetched in
  every state and split purely client-side, so the new filter is already
  correct for it.
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
rendered through glamour with the preview pane's theme mapping (#62) and its
hanging indent for wrapped list items (`ui.HangingIndent`, #2105), under
an author/age/state line and — when one exists — the linked PR's state.

### Timeline (#2084)

Under the body, behind an `── activity ──` divider, the detail shows the
issue's history. Opening a detail (enter, the walking chords, `Reveal`) lazily
asks for page one through the injected `forge.TimelineFactory` — every path
funnels through `PendingTimelineCmd`, which only fetches when the shown issue
has no timeline yet. **Comments** render as markdown blocks through the same
glamour pipeline, headed by the accented author, a `(you)` marker on own
comments, and the relative age; everything else is one compact faint line —
actor, action, the label as a colored chip or the assignee, age
(self-assignments collapse to "self-assigned this"). The states are visible
and keyboard-reachable: a loading row while a fetch is in flight, an error row
(`r` retries) that keeps what already loaded, `(L loads more activity)` while
more pages follow — `L` appends the next page without moving the scroll — and
`(no activity yet)` on an empty finished history. `r` inside the detail
refetches the listing *and* the open issue's timeline. A `TimelineMsg` for an
issue the pane no longer waits on is dropped; entry IDs and the own-comment
flag are carried in the model for the comment-editing sub-issue.

### PR detail and actions (#2089)

`enter` (or a double-click) on the PR tab opens the selected pull request
**full area**, under the same kind of position header (`PR 2/5`,
`ctrl+j`/`ctrl+k` walk without going back, refetching as they move). The body
is the PR's markdown description under a meta line — author,
`head → base`, state, review decision, mergeability — and, when the
description carries `Closes #N` (or a fix/resolve variant), a bold link line
naming the issue with its title from the listing. Below the description a
`── checks ──` divider lists **every CI check by name** with its own
`✓`/`…`/`✗` glyph (`(no checks reported)` otherwise) — the list rows only
show the rollup. `o` opens the browser; `r` refetches the listing *and* the
open PR; `esc` returns to the list with cursor and scroll untouched.

**Merge and close, with a comment.** With **push permission** (`M` merge,
`c` close — offered on the PR list and the detail alike) a two-stage dialog
opens: first an **optional comment** (posted *before* the action, so the
timeline reads in order), then — because a merge is irreversible — an
explicit **confirm** naming the PR, its branches and the merge method
(`merge #12: issue/7-thing → main (method: squash)`); only `enter`/`y` there
dispatches, `esc` cancels at either stage, `backspace` steps back to the
comment. The method comes from the repository's settings through the detail
fetch (GitHub's allowed methods, Gitea's `default_merge_style`), defaulting
to a merge commit. Without push permission the two actions are dropped from
the footer, greyed in the action menu with the reason, and their keys explain
in the filter row — the same gating pattern as the issue mutations. A PR that
is not open refuses with `already merged`/`already closed`.

**Outcomes.** While the action runs the filter row reads
`applying the change…`. A rejection shows the **forge's own reason** — the
merge-conflict or branch-protection message GitHub/Gitea answered with — in
the filter row and as an error toast, not a generic failure. A success
refetches the whole listing (the issues too: a merged PR may have closed its
`Closes #N` issue) and the open detail. A successful **merge** additionally
raises the **cleanup offer**: delete the head branch locally and on origin,
switch to the default branch and pull — the change workflow's step 6. It is
an offer only: `enter` emits `CleanupRequestMsg` and the app runs
`forge.CleanupBranchCmd` (outcome toasted, VCS snapshot invalidated), `esc`
keeps the branch and nothing runs.

### Mutations (#2088)

With **triage permission** the pane writes as well as reads. The keys work
from the issue list and from the detail view alike, and they stay QWERTZ-safe:

| Key | Action |
| --- | --- |
| `e` | **label picker** — the repository's whole label set as colored chips, the issue's own labels preselected; `space` toggles, `backspace` clears, `enter` applies the **diff** (only what changed is written), `esc` drops it |
| `u` | **assignee picker** — the repository's assignable users, same keys; `enter` replaces the assignee set (an emptied picker clears it explicitly) |
| `c` | **close** the issue, or **reopen** it when it is closed — footer and menu word themselves after the selected issue's state |
| `C` | the same **with a comment**: a one-line prompt whose text is posted *before* the state change, so the timeline reads in order |

Both pickers list what `forge.RepoMetaCmd` fetched **once** per session
(retried by `r` after a failure). When that probe could not read them — a
token without the scope — the pickers fall back to the labels and assignees
the *listing* carries, so an issue's labels can still be removed.

**Capability gating.** Without triage permission the four actions are not
offered: the footer drops them and the action menu still lists them, greyed
and with the reason spelled out (`needs triage permission`,
`checking permissions…`) — pressing the key writes the same reason into the
filter row rather than doing nothing. Without a forge backend at all they are
absent entirely.

**Optimistic, with rollback.** A write is applied to the row immediately (the
chips, the assignees, the state glyph change at once) and the pre-mutation
issue is kept. A forge rejection restores it and shows the forge's own error
in the filter row (`label change failed: HTTP 403 …`), toasted as well so it
is not missed while the pane is unfocused (`esc` on the list dismisses it, as
it clears every other narrowing). A success drops the snapshot and
**refetches** the listing — and the open issue's timeline — because the pane
must show forge truth, not its own guess. While a write is in flight the same
row reads `applying the change…`.

### Editing your own texts (#2087)

The timeline is not read-only for texts that are yours. In the detail view:

- **`E`** edits. With exactly one editable text it opens straight away; with
  several it raises the **text-edit picker**, a centered overlay listing the
  issue body first, then each of your own comments by its first line. (Plain
  `e` is #2088's label picker; the shifted key keeps the two apart.)
- **`n`** composes a new comment on the open issue.

**What is offered** is decided by `textedit.go` against the same probed
`Capabilities` the mutations read — never guessed, and empty until the probe
answered:

| Text | Offered when |
| --- | --- |
| issue body | you opened the issue, **or** you have write access |
| a comment | it carries the timeline's own-comment flag (#2084) |
| new comment | the login resolved — commenting needs no repository permission |

An unavailable action is **absent**, not greyed out the way the mutation
actions are: it is missing from the footer, from the action menu, and its key
does nothing. "You may not edit someone else's comment" is not a permission
the user can go and fix, so there is nothing to explain. A failed capability
probe hides all of them.

**The editor is a real buffer.** The pane emits `EditTextRequestMsg` naming
the target and its current text; `internal/app/forgeedit.go` answers by
creating a **markdown scratch file** — the same store "Treat Buffer as …"
materializes into (#2056) — named after what it edits
(`issue-2087-comment-77.md`), seeded with the current text, and opened through
the ordinary funnel. The edit therefore gets the whole editor: vim motions,
markdown highlighting, the preview pane, undo, autosave, crash recovery. The
only thing on top is a binding, keyed by path, holding the target and the base
text.

**Saving pushes.** Writing the buffer (`:w`, Save All, autosave) fires
`editor.EventSave`; a bound path dispatches `forge.SaveTextCmd` with the file
the save just wrote. Three outcomes:

- **pushed** — the scratch file is deleted, its buffer closes, and the pane
  refetches the listing *and* the open issue's timeline, so the new text
  appears where it was written.
- **changed on the forge** — the stale-base check found the server text moved
  since the buffer opened. Nothing was written; a centered dialog offers
  `[o]` overwrite, `[l]` load the forge's version into the buffer (re-basing
  the binding), `[esc]` decide later.
- **failed** — the buffer and every character in it stay exactly as they are,
  and the error is shown in a dialog, not a toast: `[r]` retries the push,
  `[esc]` keeps editing and saves again whenever you want.

An empty new-comment buffer is never posted — opening one and changing your
mind costs nothing. A second edit request for a text that is already open
focuses that buffer instead of racing it with a second one. A push outcome
never steals an open overlay: with one on screen it is announced instead, and
the next save re-runs the whole check.

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

- **Actions** — `e`/`c` emit `EditTextRequestMsg` (#2087, detail view only);
  `s` emits `StartWorkRequestMsg` (the app answers with
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
- **Background refresh** (#2085) — the app's poll service pushes a fresh
  listing in every `forge.poll_interval_seconds` (default 20s, `0` off), so
  the content stays current without pressing `r`. A poll result is applied so
  it cannot fight a user mid-interaction: the **selection is restored by
  issue number** (a newer issue appearing above the cursor does not move it),
  the `/` filter line, its pattern and the label filter survive untouched,
  the open detail view stays open at the offset it was scrolled to (its
  rendered body cache is dropped only when the body actually changed on the
  forge, and the re-render keeps that offset), a partial result keeps the last
  known linked-PR states, and a poll landing mid-`r` leaves the manual refresh
  pending. The polling lifecycle itself — interval, backoff, snapshot events —
  is in [Forge Layer](/architecture/forge.md).
- **Superseded fetches** (#2107) — fetches resolve off the Update loop and out
  of order, so cycling `t` twice leaves two in flight. The pane counts its
  requests and passes the count to the injected factory, which echoes it back
  as `IssuesMsg.Gen`; **only the newest generation may write the listing**, so
  whichever fetch the network finishes last can no longer overwrite the list
  with the state filter the user already left behind. A background poll is not
  generation-tagged — it always fetches the *open* listing — so it is applied
  exactly while the pane's own filter is open and dropped otherwise; the next
  round lands normally once the filter is back. A dropped answer still records
  its `Setup`/`Err` (a missing CLI or a dead network is worth saying whichever
  filter asked), but never clears the pending fetch's loading state: the
  indicator keeps standing in for the listing until the answer the active
  filter asked for arrives. An **untagged** result (`Gen: 0`) is never read as
  stale, so the single-fetch path is unchanged.

## Prominent forge event notifications (#2086)

New forge events (the poller's `forge.EventsMsg`) do not settle for a toast:
`internal/app/forgenotify.go` gives each event kind its own style — a centered
dialog, a persistent status-line unread badge, a toast, or history only — with
a do-not-interrupt guard that turns a dialog into the badge while the user is
typing. Opening this tool window views the pending events and clears the badge;
the dialog's open action reveals the announced issue's detail view here. The
surface, its queueing rules and the `[forge.notify]` settings are documented in
[Notifications](/architecture/notifications.md).
