---
type: concept
title: Forge Layer
description: The Forge interface behind the issues tooling — gh binding for GitHub, tea/REST binding for Gitea/Forgejo, backend detection by remote host with a per-workspace cache, and the capability model (triage vs push) both bindings probe (#2083).
resource: internal/forge/backend.go
tags: [architecture, vcs, forge, github, gitea, forgejo, issues]
timestamp: 2026-08-24T00:00:00Z
---

# Forge Layer (#1934, #2083)

`internal/forge` talks to the project's code forge. Since #2083 the operation
surface is the **`Forge` interface** (`backend.go`), so everything later in
stream 0470 — timeline, mutations, polling, PR actions — programs against one
contract while each forge gets its own binding. The types (`Issue`, `Label`,
`PR`, `CheckState`, `TimelineEntry`, `Capabilities`) are forge-agnostic; each
binding maps its forge's JSON onto them.

Package rule (unchanged from #1934): nothing runs from `Update`. Every
subprocess **and HTTP** call is deadline-bounded (30s network, 5s local git)
inside a `tea.Cmd`, and parsing works on JSON only — `gh --json`, Gitea REST
responses — never on a human rendering.

## The interface

`Forge` covers the whole 0470 stream: `Issues(state)` (open/closed listing
with labels + assignees), `PRs()` (every state), `Timeline(issue, page)`,
`CreateComment` / `EditComment` / `EditIssueBody`, `AddLabels` /
`RemoveLabels` / `SetAssignees` / `CloseIssue` / `ReopenIssue`, `MergePR` /
`ClosePR`, and `Capabilities()`. The interface ships complete; operations a
binding has not implemented yet return a typed `*ErrUnsupported` (backend +
operation), so later sub-issues only fill in bindings. #2083 brought the
listings and `Capabilities()`; #2084 the timeline; the rest are stubs.

`Timeline(issue, page)` (#2084) fetches one 30-entry page of an issue's
history, oldest first, and reports whether more pages follow — long
histories are never fetched whole. Entries use the neutral kind vocabulary
(`comment`, `labeled`/`unlabeled`, `closed`/`reopened`,
`assigned`/`unassigned`; anything else is dropped by the parsers): a comment
carries its markdown body, the stable forge comment ID a later edit needs,
and an own-comment flag matched against the authenticated user's login (gh:
one cached `gh api user` probe; tea: the login's user, falling back to one
`/user` probe). The gh binding reads GitHub's `issues/{n}/timeline` endpoint
(comments arrive inline as `commented` events); the tea binding reads
Gitea's typed timeline comments, where a label event's body distinguishes
add (`"1"`) from remove. `TimelineCmd(dir, issue, page)` wraps it into a
`TimelineMsg` echoing issue and page.

`RefreshCmd(dir)` is the listing `tea.Cmd`: detect the backend, fetch open
issues + all PRs, resolve to one `IssuesMsg` (`Setup` when no backend
applies, `Err` on a transient failure, a failing PR listing drops only the
PR states).

## Backend detection (`detect.go`)

`Detect(dir)` picks the binding by the **origin remote's host**:

- **github.com** (or a `github.*` enterprise host) → the **gh binding**,
  provided `gh` is on PATH — otherwise the setup message names the missing
  CLI.
- **Any other host** → the **tea binding**, provided the `tea` CLI is
  installed and `tea logins list --output json` has a login whose URL matches
  the host; the login's API token is then read from tea's config file
  (`os.UserConfigDir()/tea/config.yml`, falling back to
  `~/.config/tea/config.yml`), because the login listing does not print
  tokens. Each missing piece — tea itself, a matching login, the token —
  resolves to a setup message naming exactly it.
- **No remote / unparsable remote** → a setup message.

A **successful** detection is cached per workspace root (absolute path), so
refreshes do not re-probe CLIs and logins; failures are never cached —
installing the missing CLI and pressing `r` recovers. `ResetDetection(dir)`
drops one cache entry (tests, changed remote). Remote parsing handles all
three git URL shapes (https, ssh://, scp-like `git@host:owner/repo`) and
keeps the last two path segments, since Gitea can serve under a path prefix.

## The gh binding (`gh.go`)

The #1934 code moved behind the interface unchanged: `gh issue list --json` /
`gh pr list --json` (limit 200), the mixed CheckRun/StatusContext rollup
folded into one `CheckState` (failing beats pending beats passing beats
none). `Capabilities()` runs `gh api repos/{owner}/{repo} --jq .permissions`
(gh fills the placeholders from the remote) and folds GitHub's five-tier
permissions object.

## The tea binding (`tea.go`)

**Decision:** the listings and the capability probe call the **Gitea REST
API directly** (`/api/v1/repos/{owner}/{repo}/issues|pulls`, the repo
endpoint for permissions), authenticated with the tea login's token. tea's
own `--output json` runs through its table layer, which flattens labels to
comma-joined names and drops their colors — not enough for the pane's label
chips. The tea CLI still owns authentication: detection matches its login
list, the token comes from its config. Forgejo serves the same API.

Listings page in 50s up to the shared 200 cap. Gitea's PR listing carries no
check rollup, so `Checks` stays `ChecksNone` for now (a later sub-issue can
read the commit-status API); merged PRs map `state:closed` + `merged:true`
onto the shared `MERGED` vocabulary so `PRForIssue`'s open>merged>closed
ranking works identically.

## Capability model

`Capabilities` has two tiers, matching what the stream's actions need:

- **Triage** — mutate labels, assignees and issue state.
- **Push** — write access: merge pull requests.

GitHub reports `{admin, maintain, push, triage, pull}`: push-or-better sets
both, bare triage sets only `Triage`. Gitea reports `{admin, push, pull}` —
no triage tier — so push-or-admin sets both. Later sub-issues consume this to
hide or explain unavailable actions.

## Event types (#2086)

`events.go` fixes the vocabulary of the snapshot-diff events the background
poller (#2085) emits: `EventKind` (`IssueOpened`, `IssueClosed`, `PROpened`,
`PRMerged`, `PRClosed`, `PRChecksFailing`), the `Event` payload (number, title,
author, labels, url) and `EventsMsg`, which carries one poll round's events plus
the workspace root they were diffed for. Each kind names its own config leaf
(`EventKind.ConfigKey()` → `forge.notify.<kind>`), which is what the prominent
notification surface reads to decide between dialog, badge, toast and off — see
[Notifications](/architecture/notifications.md). The types are producer- and
consumer-agnostic: nothing in this file talks to a forge.

## Consumers

The [GitHub Issues tool window](/architecture/github-issues.md) is the one
consumer today: it injects `RefreshCmd` and routes `IssuesMsg`; through
detection it now lists Gitea/Forgejo repositories too, unchanged. The
start-work flow (`StartWorkCmd`) is pure git and backend-independent.

The forge event surface (`internal/app/forgenotify.go`) is the second consumer:
it routes `EventsMsg` onto the dialog, the status-line unread badge, a toast or
the notification history alone.
