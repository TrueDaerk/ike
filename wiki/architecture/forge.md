---
type: concept
title: Forge Layer
description: The Forge interface behind the issues tooling — gh binding for GitHub, tea/REST binding for Gitea/Forgejo, backend detection by remote host with a per-workspace cache, the capability model (triage vs push) both bindings probe, and the background poll service that diffs snapshots into typed events (#2083, #2085).
resource: internal/forge/backend.go
tags: [architecture, vcs, forge, github, gitea, forgejo, issues]
timestamp: 2026-08-24T00:00:00Z
---

# Forge Layer (#1934, #2083, #2085)

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
with labels + assignees), `PRs()` (every state), `Timeline(issue)`,
`CreateComment` / `EditComment` / `EditIssueBody`, `AddLabels` /
`RemoveLabels` / `SetAssignees` / `CloseIssue` / `ReopenIssue`, `MergePR` /
`ClosePR`, and `Capabilities()`. The interface ships complete; operations a
binding has not implemented yet return a typed `*ErrUnsupported` (backend +
operation), so later sub-issues only fill in bindings. In #2083 both bindings
implement the listings and `Capabilities()`; the rest are stubs.

`RefreshCmd(dir)` is the listing `tea.Cmd`: detect the backend, fetch open
issues + all PRs, resolve to one `IssuesMsg` (`Setup` when no backend
applies, `Err` on a transient failure, `PRErr` when only the PR listing
failed — the issues are still worth showing). `PollCmd(dir)` is the same
fetch tagged `Poll: true` for the background poll service below; the tag is
what lets the consumers tell "the user asked for this" from "the timer did".

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

## Background polling (`poll.go`, `internal/app/forgepoll.go`, #2085)

Forge data used to refresh only on `r` or on pane open. The poll service
closes that gap: IKE notices a new issue within the configured interval
without ever blocking the UI, and without the pane having to be open at all.

**The tick chain.** One `Poller` per workspace root lives on the app model
(`forgePollState`, the `vcsState` pattern). `Arm()` hands back a `tea.Tick`;
the `PollTickMsg` handler **only dispatches** `PollCmd` and returns, so the
Update loop never waits on the network — that is the whole design. A tick
carrying a different `Root` is a leftover from the project switched away from
and is dropped.

The chain is **self-sustaining and opened in exactly three places**:
`Model.StartForgePoll()`, each finished fetch, and a successful manual refresh
that resumed a stopped poller. Update's settled pass only reopens it on the
one edge those cannot cover — a config reload that turned polling back on,
since `reloadConfig` has no command to return — behind a one-shot flag.

`StartForgePoll` rides the **`StartWatcher` lifecycle** (`cmd/ike/main.go` at
startup, `switch.go` per project switch) and waits out the first deadline on
its own goroutine, delivering it through the host's `Send`. Two traps make
that placement, rather than `Init` or the settled pass, the load-bearing one —
both are guarded by tests:

- **Not `Init`.** The app test helpers drain `Init`'s commands *synchronously*
  (`sizedWith` calls each `cmd()` in-line), and a poll deadline is a
  `tea.Tick` — draining one blocks the caller for a full interval. Arming from
  `Init` cost every helper-built model 20 s of real time and timed the package
  out. `TestInitDoesNotArmTheForgePoll` guards it. This is the same reason the
  file watcher is main.go-only.
- **Not every settled pass.** That makes each Update pass return a pending
  tick, so the app never settles at the command level and any synchronous
  command drainer spins forever waiting out poll intervals.
  `TestForgePollUpdateSettlesWithoutAPendingTick` guards it.

**No pile-up.** `Tick()` refuses while a fetch is in flight and `Arm()`
refuses to schedule during one; `Apply()` re-arms when the result lands. A
forge slower than the interval therefore costs one outstanding subprocess,
never a growing queue.

**The interval** is `forge.poll_interval_seconds` (Settings → Forge): default
20s, floor 10s, ceiling one hour, **`0` disables polling entirely**. The
valid set has a hole between 0 and the floor, so the config validator snaps a
too-small value up (and a negative one down to "off") with a diagnostic,
while the settings form is strict — it refuses the typed value and names the
rule, and its steppers jump the hole (0 ↔ 10). Edits apply on the live config
reload. This is the one deliberately repeating tick in the app's
[idle rules](/architecture/performance.md); the HUD's armed-ticker count
shows it as a standing 1 while polling is on.

**Robustness.** A `Setup` result (no CLI, no matching remote or login) is not
transient, so polling **stops** rather than retrying on a timer — a
successful manual refresh (`Resume`) restarts it. Consecutive `Err` results
back off exponentially from the interval, capped at 5 minutes (never faster
than the configured interval), and reset on the first success. Exactly two
toasts can come out of a failure run: one when polling degrades and one when
it recovers — `PollResult.Degraded` / `.Recovered` are edges, not states.

**Snapshot diffing.** `Snapshot` is one observed listing (open issues + all
PRs); `Diff(prev, next)` produces typed `Event`s:

| Event | Fires when |
| --- | --- |
| `IssueOpened` | an issue is in the fresh open listing and was not before |
| `IssueClosed` | an issue left the open listing |
| `PROpened` | a PR is open now and was not (new, or reopened) |
| `PRMerged` | a PR reached the merged state |
| `PRClosed` | a PR closed without merging |
| `PRChecksFailing` | an open PR's CI rollup turned red (the transition only) |

The **first fetch seeds the snapshot silently** — no event storm at startup
or after a project switch, since the model (and with it the poller) is rebuilt
per root. A PR merely falling off the capped listing is not an event, and a
`PRErr` partial result carries the previous PRs forward so the next full poll
does not report every pull request as newly opened.

The **PR half seeds separately** from the issue half, because it can fail on
its own: if the *seeding* fetch's PR listing failed, the snapshot's empty PR
list is a stand-in rather than an observation, so the first real PR listing is
that half's silent seed too (`prsSeeded`). Without that, a startup that could
reach the issues but not the pull requests would announce every open PR as
just-opened one interval later.

Events leave as one `forge.EventsMsg` (root + events) for any consumer; the
prominent notification surface consuming them is its own sub-issue.

## Consumers

The [GitHub Issues tool window](/architecture/github-issues.md) is the one
consumer today: it injects `RefreshCmd` and routes `IssuesMsg`; through
detection it now lists Gitea/Forgejo repositories too, unchanged. Since #2085
it also consumes the poll's fresh listing, so its content stays current
without `r` — and a poll result is applied without fighting the user: the
selection is restored by issue number, the filters survive, the open detail
view keeps its scroll offset across a re-render, and a poll never clears a
manual refresh's pending state. The
start-work flow (`StartWorkCmd`) is pure git and backend-independent.
