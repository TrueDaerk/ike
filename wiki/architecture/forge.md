---
type: concept
title: Forge Layer
description: The Forge interface behind the issues tooling — gh binding for GitHub, tea/REST binding for Gitea/Forgejo, backend detection by remote host with a per-workspace cache, the capability model (triage vs push, plus the authenticated login) both bindings probe, the issue mutations (labels, assignees, state, comments), the editable-text layer with its stale-base check, the PR detail/action layer (full PR fetch with per-check CI, merge/close with a comment, post-merge branch cleanup), and the background poll service that diffs snapshots into typed events, plus the persistent listing cache with incremental updated-since refresh (#2083, #2088, #2087, #2089, #2085, #2108).
resource: internal/forge/backend.go
tags: [architecture, vcs, forge, github, gitea, forgejo, issues]
timestamp: 2026-08-25T14:00:00Z
---

# Forge Layer (#1934, #2083, #2088, #2087, #2085)

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
`CreateComment` / `EditComment` / `EditIssueBody` with their read halves
`IssueBody` / `CommentBody`, `AddLabels` / `RemoveLabels` / `SetAssignees` /
`CloseIssue` / `ReopenIssue`, `RepoLabels` / `Collaborators`, `PRDetail` /
`CommentPR` / `MergePR(pr, method)` / `ClosePR`, and `Capabilities()`. The
interface ships complete; operations a binding has not implemented yet return
a typed `*ErrUnsupported` (backend + operation), so later sub-issues only
fill in bindings. #2083 brought the listings and `Capabilities()`; #2084 the
timeline; #2088 the issue mutations (labels, assignees, state, comment
creation) plus the two metadata listings; #2087 the editable texts and their
reads; #2089 the PR detail and the merge/close actions.

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

## Editable texts (#2087)

`text.go` is the neutral half of "edit my own issue texts": a **`TextTarget`**
— `TextIssueBody`, `TextComment` (with the forge comment ID), `TextNewComment`
— plus `FetchText` / `PushText`, which dispatch it onto the three interface
mutations, and `Label()` / `Slug()` for the human phrasing and the buffer
name. Nothing above this layer branches on the forge.

`SaveTextCmd(dir, path, target, base, body, force)` → `SaveTextMsg` is what a
saved edit buffer runs. Its order is **check, then push**:

1. Unless `force` is set (and unless the target is a new comment, which has no
   server text), it re-reads the current text through `FetchText` and compares
   it against `base` — the text the buffer was opened with.
2. A mismatch resolves to `Stale` with `Current` carrying the forge's version,
   and **nothing is written**: a concurrently changed text is never silently
   clobbered. The user answers by overwriting (`force`) or reloading.
3. A base read that *fails* is an ordinary `Err`, not an assumed-safe push.

Comparison and push both run through **`NormalizeText`** (CRLF → LF, trailing
blank space trimmed). An editor writes a trailing newline the forge never had,
so the raw text would report a conflict — and churn the forge — on every save
that changed nothing.

The permissions and the login the gating reads come from #2088's one-shot
`RepoMetaCmd` probe — no second capability call was added for this.

On the bindings: gh sends every body on **stdin** (`gh issue edit
--body-file -`, `gh issue comment --body-file -`, and `gh api --method PATCH
issues/comments/{id} --input -` for the comment edit gh has no command for) —
markdown must never travel as an argv element. tea sends JSON documents to the
Gitea endpoints (`PATCH issues/{n}`, `POST issues/{n}/comments`, `PATCH
issues/comments/{id}`). Both read their base text from the issue/comment
endpoint and decode the shared `body` field, rather than through `--jq`, whose
re-encoding would break a character-for-character comparison. A comment ID is
validated as digits before it reaches a request path.

## Mutations (`mutate.go`, #2088)

The write side is one neutral **`Mutation`** value — issue number, a label
`AddLabels`/`RemoveLabels` diff, an `Assignees` set behind an explicit
`SetAssignees` flag (so "nobody" and "do not touch" stay distinct), a
`State` (`"closed"`/`"open"`) and a `Comment`. `MutateCmd(dir, mut)` applies
its parts **in the order the user means them** — comment first (so
close-with-comment posts before the state change and the timeline reads
right), then the label removals and additions, then the assignees, then the
state — and stops at the first failure, so the caller's rollback covers a
half-applied write. It resolves to a `MutationMsg` echoing issue and kind.

`RepoMetaCmd(dir)` → `RepoMetaMsg` is the one-shot metadata probe behind the
pickers: `Capabilities()` first and, only when they allow triage, the
repository's `RepoLabels()` and `Collaborators()` — a token without the scope
must not spend two calls on listings it may not read anyway. A failing label
or user listing is not fatal: the capabilities still travel and `Err` names
what was missed. `MutateFactory(dir)` and `MetaFactory(dir)` are the closures
the issues window is injected with, mirroring `RefreshFactory`.

Bindings: gh runs `gh issue edit <n> --add-label/--remove-label a,b`,
`gh issue close|reopen <n>`, `gh issue comment <n> --body …`,
`gh label list --json name,color` and `gh api repos/{owner}/{repo}/assignees`.
The assignee *set* goes through `gh api --method PATCH .../issues/<n>
--input -` with a JSON body on stdin — `gh issue edit` only adds and removes
(it would need the current set to diff against), and stdin is the only way to
send the empty array of a cleared set. tea/Gitea uses the REST API throughout
(`POST`/`DELETE .../issues/<n>/labels`, `PATCH .../issues/<n>` for assignees
and state, `POST .../issues/<n>/comments`); Gitea addresses labels by numeric
ID, so names are resolved against the repository's label set first and an
unknown name errors rather than being dropped silently. Its collaborator
listing omits the repository owner, who is folded back in.

`RefreshCmd(dir)` is the listing `tea.Cmd`: detect the backend, fetch open
issues + all PRs, resolve to one `IssuesMsg` (`Setup` when no backend
applies, `Err` on a transient failure, `PRErr` when only the PR listing
failed — the issues are still worth showing). `PollCmd(dir)` is the same
fetch tagged `Poll: true` for the background poll service below; the tag is
what lets the consumers tell "the user asked for this" from "the timer did".

**Request tagging** (#2107). Fetches resolve off the Update loop, so several
can be in flight at once and they can finish in any order. `IssuesMsg.Gen`
echoes the requester's generation counter back untouched, and
`RefreshGenCmd(dir, state, gen)` — what `RefreshFactory(dir)` closes over — is
the tagging listing command. `Gen: 0` means *untagged*: a background poll, or
a caller that does not count its requests. Nothing in this package interprets
the tag; it exists so the consumer can recognise its own newest request and
drop the rest (the issues window does, see below).

## PR detail and actions (`pr.go`, #2089)

`PRDetail(pr)` fetches one pull request in full: the neutral **`PRDetail`**
embeds the listing's `PR` and adds the markdown body, the base branch, a
mergeability verdict (`mergeable` / `conflicting` / `unknown` / `""`), the
merge method a merge would use, and the per-check CI results as `CheckRun`
rows (name + folded `CheckState`) — the listing only carries the rollup.
`PRDetailCmd(dir, pr)` → `PRDetailMsg` wraps it, injected as
`PRDetailFactory(dir)`.

The write side is one neutral **`PRAction`** — PR number, kind (`PRMerge` /
`PRClose`), merge method, optional comment. `PRActionCmd` applies the comment
**first** (so the timeline reads in the user's order) and stops on its
failure before the irreversible half; it resolves to a `PRActionMsg` whose
`Err` carries the **forge's own reason** — a merge conflict, an unmet branch
protection — not a generic failure. `PRActionFactory(dir)` is the injected
closure.

Bindings: gh reads `gh pr view --json` (body, `baseRefName`, `mergeable`,
`statusCheckRollup` with each entry's `name`/`context`) plus a best-effort
`gh api repos/{owner}/{repo}` probe folding the `allow_*` flags into the
method (merge commit first, then squash, then rebase); it writes with
`gh pr comment --body-file -`, `gh pr merge --merge|--squash|--rebase` and
`gh pr close` — gh's stderr carries GitHub's refusal, which `cliError`
surfaces. tea/Gitea reads `GET /pulls/{n}` (mergeable bool), the repo
endpoint's `default_merge_style` and the head commit's combined status for
the per-check list (both best-effort); it writes with
`POST /pulls/{n}/merge {"Do": method}`, `PATCH /pulls/{n}` (close) and the
issue comment endpoint (Gitea serves PR comments there). Non-2xx responses
now surface the error document's `message` — the forge's reason — instead of
the bare status.

`LinkedIssue(body)` derives the issue a PR body claims to close (`Closes #N`
and the fix/resolve keyword variants), the detail view's link back into the
issues tab.

`CleanupBranchCmd(dir, branch)` → `CleanupDoneMsg` is the post-merge
change-workflow cleanup: refuse a dirty worktree and the default branch
itself, `checkout` the default branch, `pull --ff-only`, delete the issue
branch locally (`-D` — a squash/rebase merge leaves it unmerged in local
history) and on origin. The pull and the remote deletion degrade to warnings
(forges often auto-delete merged branches). It runs **only** on the user's
explicit confirmation of the pane's offer.

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
permissions object, with the login from the cached `gh api user` probe folded
in for the ownership checks (#2087).

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

`Capabilities` has two tiers, matching what the stream's actions need, plus
the identity the ownership checks need:

- **Triage** — mutate labels, assignees and issue state.
- **Push** — write access: merge pull requests, edit foreign issue texts.
- **Login** — the authenticated user's login, `""` when it could not be
  resolved. It rides along on the probe (which is cached per backend anyway),
  because "may I edit this?" is as much a question of *whose text it is* as of
  permissions.

GitHub reports `{admin, maintain, push, triage, pull}`: push-or-better sets
both tiers, bare triage sets only `Triage`. Gitea reports `{admin, push,
pull}` — no triage tier — so push-or-admin sets both. The issues window
consumes `Triage` (#2088) to gate its mutation actions: without it they stay
in the action menu, greyed and naming the reason, and are dropped from the
footer. `Push` and `Login` gate the text editing (#2087), where an
unpermitted action is *absent* instead — "not your comment" is not a
permission the user can go and fix.

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

Events leave as one `forge.EventsMsg` (root + events) for any consumer. The
types themselves live in `events.go` next to `Issue`/`PR` (#2086), so the
poller and the notification surface agree on one shape; `Diff` fills in the
author and labels the dialog shows besides the title, where the listing
carries them.

## Persistent listing cache (`cache.go`, #2108)

Every start used to fetch the full listing from scratch, so the issues window
showed a loading state for seconds. The cache closes that gap twice over:
instant content at start, and cheaper polls in steady state.

**The snapshot.** Every successful *open* listing — full or incremental,
foreground or poll — is persisted to the project's `.ike/forgecache.json`
(`IKE_CONFIG_DIR` overrides, like every state store): issues, PRs, the moment
the fetch *started*, the schema version and the **origin remote URL** as the
key. Only the open listing is ever cached — it is the one every poll and
every freshly opened pane asks for.

**Seeding.** An issues pane that has not loaded yet is seeded off the Update
loop by `LoadCacheCmd` (on open, batched with the real fetch; for a restored
pane, from `Init`): the pane renders the snapshot immediately, marked
`cached · updating…`, and the next real listing — the fetch or the first
background poll — replaces it through the ordinary `SetResult` path and drops
the marker. The seed resolves asynchronously, so `SetCached` refuses to
overwrite a pane that already holds fetched data; combined with the #2107
generation guard, cached content can never mask or outlive a real answer.

**Incremental refresh.** While the snapshot is younger than `resyncAfter`
(30 minutes), a background poll and the pane's on-open fetch ask the forge
only for the issues **updated since** the snapshot's timestamp (minus a
one-minute overlap for clock skew — the merge is idempotent) and merge them
in: an updated open issue replaces its row or is inserted, one that is no
longer open drops out. gh has no since filter, so the GitHub binding calls the
REST issues endpoint (`repos/{o}/{r}/issues?state=all&sort=updated&since=…`),
dropping the pull requests GitHub mixes in; the Gitea binding adds `since` to
its ordinary listing pages. Pull requests have no since filter on either forge
and are always listed in full.

**Consistency rule.** An incremental merge only sees what the forge reports
as updated — a deleted or transferred issue lingers until the next full
resync. Full resyncs are therefore never rare: manual `r` (and every other
user-driven refetch — state cycle, mutation, PR action) always resyncs fully,
as does any snapshot older than `resyncAfter`, any incremental error, and any
updated-since answer that may be truncated (a full page from GitHub, the
`issueLimit` cap on Gitea).

**Invalidation.** The cache is keyed to the origin remote: a repository or
backend switch changes the remote and the snapshot stops matching (the memo
is dropped alongside the detection cache in `ResetDetection`). A corrupt,
unreadable or wrong-version file reads as "no cache" — a fresh full fetch,
never an error. The `forge.cache` toggle (Settings → Forge, default on) is
pushed into the package at startup and on every config reload; off, nothing
is read or written.

## Consumers

The [GitHub Issues tool window](/architecture/github-issues.md) is the main
consumer: it injects `RefreshCmd`/`TimelineCmd` and routes `IssuesMsg`,
`TimelineMsg` and `CapabilitiesMsg`; through detection it now lists
Gitea/Forgejo repositories too, unchanged. Since #2085 it also consumes the
poll's fresh listing, so its content stays current without `r` — and a poll
result is applied without fighting the user: the selection is restored by
issue number, the filters survive, the open detail view keeps its scroll
offset across a re-render, and a poll never clears a manual refresh's pending
state. Since #2107 it also *drops* answers it has superseded: it counts its
fetches into `Gen` and applies only the newest one, and it ignores a poll's
open listing while its own filter shows closed or all. The start-work flow
(`StartWorkCmd`) is pure git and backend-independent.

The **forge edit buffers** (`internal/app/forgeedit.go`, #2087) consume the
editable-text layer: a markdown scratch buffer bound to a `TextTarget` runs
`SaveTextCmd` when it is saved and answers `SaveTextMsg` — documented with the
tool window.

The forge event surface (`internal/app/forgenotify.go`, #2086) is the poller's
consumer: it routes `EventsMsg` onto the dialog, the status-line unread badge,
a toast or the notification history alone.
