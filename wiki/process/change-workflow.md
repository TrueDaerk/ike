---
type: process
title: Change Workflow
description: The lifecycle of every change — issue first, issue branch, version bump, PR, merge, branch cleanup, plus the settings-UI, default-keybind, shared-building-block and span-family-ledger obligations.
tags: [process, git, release]
resource: internal/version/version.go
timestamp: 2026-10-27T07:30:00Z
---

# Change Workflow

Every change — feature, fix, docs — goes through the same loop. No direct commits to `main`.

1. **Issue first.** Before touching code, make sure a GitHub issue exists for the change
   (after the duplicate check in [GitHub Issue Workflow](/process/issues.md)); create one otherwise.
2. **Branch per issue.** Work on `issue/<number>` (e.g. `issue/12`), branched from an
   up-to-date `main`. Reference the issue in commits where useful.
3. **When done, bump the patch version.** Raise the last component of `Version` in
   `internal/version/version.go` (e.g. `0.1.6` → `0.1.7`) as part of the branch, in its own
   `chore(version)` commit.
4. **Open a PR on GitHub.** Body says `Closes #<number>` so the issue auto-closes on merge.
5. **Merge into `main`.** Merging closes the issue and finishes the task — don't leave PRs
   open or issues dangling.
6. **Clean up.** Delete the issue branch (local and remote), switch back to `main`, and pull.

Definition of done and epic bookkeeping (ticking the task list, closing milestones) are in
[GitHub Issue Workflow](/process/issues.md).

## New settings ship with UI support

Every new configuration setting must also be **configurable in the Settings UI, in the same
change** that introduces it — a setting reachable only by hand-editing the config file is
considered incomplete. Concretely: extend the matching settings page/form
(`internal/settings/`), validate the input there, persist it through the write-back layer,
surface it in any list rendering, and cover it with tests.

Where the config validator has to be lenient (downgrading a bad combination to a diagnostic,
because a config file on disk must still load), the form is free to be **strict** and reject
the input outright with a clear message — that is the preferred behavior, since the user is
editing interactively and can fix it immediately.

## New commands ship with a default keybind

Commands are driven by keybind far more often than through the palette, so a command that
ships **palette-only is effectively invisible** to everyday use. Every new command therefore
ships with a **default keybind in the same change** — a row in `jetbrainsRows`
(`internal/keymap/defaults.go`), following the existing conventions: JetBrains' own chord
where one exists and is free on both platforms, otherwise the closest family in the table
(`ctrl+alt+*` for view/tool toggles, the numeric `cmd+<n>` tool windows, the F-key run/debug
scheme), single modifier chords only, and conflict-free after the Cmd→Ctrl fold off macOS.
A fragile (Cmd/Alt-modified) primary also needs its escape route recorded in
`reachableAlternatives` (`internal/keymap/matrix.go`).

Shipping **without** a keybind stays allowed, but only **with a recorded reason** — the
command already has a vim-native key or a pane-local single key, it is one entry of a picker,
it is a second flavour of a command that already has a chord, `alt+enter`'s intention popup
offers it where it applies, or it is a genuine one-off. The reason goes into the audit ledger
in `cmd/ike/keybind_audit_test.go`, whose test fails the build for any registered command that
is neither bound nor justified — and for any ledger entry that has gone stale.

After changing the default table, refresh the generated documentation in the same change:
`go run ./cmd/docgen` (the `userdocs/reference` pages) and
`IKE_GEN_MATRIX=<file> go test ./cmd/ike -run TestGenerateMatrixMarkdown` for the status
matrix embedded in [Keybindings & Shortcuts](/architecture/keybindings.md). New bindings show
up in the cheatsheet (`f1`) and the palette's shortcut column automatically — both read the
live binding table.

## New surfaces reuse the shared building blocks

A pane, prompt, list, search line or tool window is built from the pieces in
[Shared Building Blocks](/architecture/shared-building-blocks.md) — `ui.Field` for a
one-line input, `ui.LineSearch` for an in-pane `/` search, `ui.ClampWindow` and the
list-mouse helpers for a cursor-and-window list, `togglePanel` / `openToolPane` for a tool
window, and so on. The 0500 sweep (#2458) exists because each of those had been copied a
dozen times, and each copy drifted: a missing paste path here, a missing caret there, four
meanings of `esc`.

Three sweep tests in `internal/ui` (`inputsweep_test.go`, `scrollsweep_test.go`,
`searchsweep_test.go`) fail the build on the shape of a hand-rolled copy outside
`internal/ui`. A surface the block genuinely does not fit carries an allowlist entry **with a
reason**; a companion test fails when the entry has gone stale. Adopting a block that changes
what the user sees is recorded in the pane's concept doc and in `wiki/log.md`.

## Languages account for every span family

Language plugins wire their stand-in and hint families — unicode-escape decoding, entity
decoding, base64 decoding, secret masking, network/permission/cron/number hints — by hand in
their `lang.Language.Spans` hook, so a family a language *could* offer but does not is
invisible until someone notices (that is how the unicode decoding of #1620 missed Python,
PHP, YAML and TOML until #2334). The audit ledger in `cmd/ike/spanfamily_audit_test.go`
(#2337) closes that hole: every registered language must, for every audited family, either
**offer it** (verified behaviourally — the test runs the hook over a per-family probe buffer
and looks for the family's capture) or carry a **ledger entry with a reason** from the
ledger's small named set (no syntax for such literals, no marking convention in the format,
foreign data rendered verbatim, injection helper, or a genuine gap linked to its tracking
issue).

Concretely: a **new language** fails the build until its row is filled in; **wiring a
family** means moving its cell to `offeredSpanFamilies` (and extending the family's probe if
the new producer needs a shape the probe lacks); a reason entry for a family the language
has since started offering is **stale and fails**. Real gaps are never recorded as a bare
reason — they are closed or linked as a follow-up issue (`reasonGap`; the initial set the
audit surfaced was closed wholesale in #2345, so the ledger currently carries none).

## Languages declare their complexity spec or a reason

The complexity engine (Roadmap 0550) measures a language only through its
`lang.Language.Complexity` spec — the node kinds that start a function, branch, nest and the
boolean operator tokens — so a language added without one would silently read as "no
functions" in every metric. The audit ledger in `cmd/ike/complexity_audit_test.go` (#2914)
closes that hole the same way the span-family ledger does: every registered language either
**declares a spec** (verified behaviourally — the test parses a per-language probe with the
registered grammar and requires the named function at the expected cyclomatic count) or
carries a row in `unmeasuredLanguages` with a reason from the named set (`reasonNoFunctions`
for data and markup formats, `reasonNoGrammar` for a language with functions but no
Tree-sitter grammar, `reasonSpecGap(issue)` for a real gap linked to its tracking issue).

Concretely: a **new language** fails the build until its row is filled in; **declaring a
spec** means adding a `complexityProbes` entry and a `complexity_test.go` in the plugin whose
probe parses to every kind and operator the spec names (the kinds are verified by a parse,
never guessed), plus a golden fixture under `internal/complexity/testdata/<lang>/` whose
`fixture.golden.json` was reviewed against the language's reference tool (#2915, see
[complexity](/architecture/complexity.md) § Fixtures); a reason row for a language that has since gained a spec, or whose
`reasonNoGrammar` claim no longer holds, is **stale and fails**. See
[languages](/architecture/languages.md) § Complexity spec and
[complexity](/architecture/complexity.md).

## The test suite stays fast and green (#2997)

`go test ./...` is the gate: the `Test` workflow (`.github/workflows/test.yml`)
runs `go vet ./...` and `go test ./...` on every PR and every push to `main`.
The budget is the whole run **under 2 minutes** on a ~10-core developer
machine with a warm build cache, **no package over 60 s**, nothing excluded
from the default run, and the workflow **under 4 minutes** wall clock (#3054).
A red suite or a package that has grown slow fails the PR, not the next
developer's afternoon.

The workflow spreads the suite over jobs that start together — vet, the bulk
of the packages, the wasm packages, and `internal/app` in three shards per OS
(`scripts/ci/shard.sh` splits a package's tests by position in `go test -list`
order) — and restores the Go build cache saved by the last push to `main`
(`.github/actions/setup-go`), so a PR compiles only what it changed. Two
consequences for tests:

- **A test leaves the process as it found it.** A shard runs a different third
  of `internal/app` than the full run does, so a test that depends on an
  earlier one's leftover — a changed working directory, an environment
  variable, a persisted store under the previous test's `IKE_CONFIG_DIR`, a
  package-level seam — passes locally and fails in one shard. In
  `internal/app`, change directory through `chdir(t, dir)` (never `t.Chdir`:
  the cwd cache in `cwdcache.go` would outlive the test; a sweep test enforces
  it) and give a model built with `NewWith` its own `IKE_CONFIG_DIR`.
- **No test installs a tool.** The app `TestMain` calls `lsp.NoInstalls()`
  (`plugins/lsp`): opening a `.go` file where gopls is missing — every CI
  runner — would otherwise run `go install …gopls@latest` in the background
  of the whole suite, as would the npm/brew recipes of the other languages
  (#3056). A test of the install path stubs the installer's runner.
- **A hosted runner stalls a goroutine for tens of milliseconds.** A test that
  reasons about real time leaves that much slack: a 60 ms debounce probed with
  10 ms pauses fired between two keystrokes of a "burst" on CI.

What keeps the budget, and what a new test must follow:

- **No test waits on real time where a seam exists.** `internal/app` runs
  commands synchronously (`drainCmd`, `settle`), so a `tea.Tick` executed in a
  drain *sleeps its delay*. Every timer the app schedules goes through
  `tickCmd` (`internal/app/tick.go`); the package tests install a hook
  (`tick_test.go`) that lets debounces under 500 ms run for real and **defers**
  longer timers — toast expiry, grace periods, idle timeouts — as a command
  that delivers nothing. A test that needs such a timer to lapse calls
  `fireDeferredTicks`; a live test that drives a real loop against a fake
  agent or shell opts back into real timers with `realTicks(t)`. Per-buffer
  debounces have the same shape in `debounceTick`.
- **No test model walks the package directory.** `Init` starts the TODO scan
  and the complexity index walk, and the first completion request scans the
  word and symbol indexes' root; with the test cwd being `internal/app` that
  meant ~1000 Go files through tree-sitter per model — seven busy cores for
  the whole run. `TestMain` turns the Init walks off (`initProjectWalks`) and
  points the completion indexes at an empty root (`completionIndexRoot`);
  a test that asserts on a walk calls `withProjectWalks(t)`. A project switch
  changes the process cwd: a test that switches wraps it in `t.Chdir`, and
  the model-construction guard (`cwdguard_test.go`) puts a vanished cwd back.
- **Polling loops are bounded by the work, not by hope.** A remaining
  poll-and-sleep loop has a deadline of **≤ 2 s** and a comment saying why the
  operation cannot take longer; fixtures are small (the complexity fixture
  walks a one-file temp root, not the package directory).
- **Fixtures do not leak into later tests.** `lang.Register` replaces an
  entry wholesale, so a grammar-less stand-in strips the real plugin's grammar
  from every later test in the binary: register through `registerTestLang`
  (restores the previous entry), `ensureLang` (keeps an existing one) or
  `withTestExtension`. The app tests start with the welcome tour and the
  first-start LSP dialog off (`tourAutoOpen`, `onboardingAutoOpen` in
  `TestMain`) — both would swallow scripted keys — and with `SHELL=/bin/sh`,
  so a terminal test never depends on the developer's login shell.
- **Pty tests pass or skip fast.** `internal/terminal` probes once per binary
  whether the test shell reaches a prompt on a pty (`requirePTY`, called from
  `waitFor`); without one every pty test skips with the reason in
  microseconds instead of timing out one by one. The interactive tests drive
  **readline line editing** (cursor motion, history, mid-line completion
  edits), so the shell is `testShell` (`testmain_test.go`, #3000): `/bin/sh`
  where that is bash (macOS), otherwise bash from PATH through a symlink named
  `sh` — POSIX mode, no rc files, the stock `sh-5.2$ ` prompt — since Debian
  and Ubuntu's `/bin/sh` is dash, which swallows every editing key. The
  package's `TestMain` gives the shells an empty `HOME` and no `$ENV`. Type
  into a shell only after `waitPrompt`: `Busy()` clears before readline is up,
  and bash 5 loses a ctrl+d sent that early, as it loses the byte typed in
  the millisecond after a ctrl+c (`TestSendKeyCtrlCDiscardsSpooledBacklog`
  waits for the interrupt's prompt first).
- **A test waits on the thing it asserts, never on a proxy of it (#3006).**
  The hosted runners are slow and loaded, and three proxies broke there: a
  `runtime.NumGoroutine` snapshot (every earlier test's goroutine still
  winding down moves it — the producer leak check reads
  `jqplay.Producer.Done()` / `Wait` instead), a measure landing before the
  walk has finished (`baselineApp` waits for `!Stats().Scanning`, which is
  what the session base keys on), and a substring of the expected output
  that the echoed command line also contains (the multi-click tests wait for
  the output *rows* and assemble their marker from two printf arguments, so
  the kernel echo of keys typed before the shell is up — at column 0 —
  cannot match). A pane test about what the grid does with a reference
  injects it as child output (`terminal.Model.InjectOutput`, `termRefRow`)
  instead of typing a printf through the shell: no spawn, no deadline sized
  for a loaded runner. Two more of the kind fell on the Linux runner (#3018):
  a 62-column echo typed *before* the prompt (the line discipline echoes the
  keys at column 0, readline draws prompt and line again behind them, and the
  wrapped line never matches — `TestSessionResizeWidthRoundTrip` now
  `waitPrompt`s first), and a bridge test disconnecting right behind the
  "accepted" trace, which precedes the run the bridge sends from a goroutine
  (`TestDoctorAcceptedTrace` waits for the request's `continued` first).
- **A test's git leaves no daemon behind (#3018).** Since git 2.47 a commit
  (as a fetch, a merge, a rebase) ends by spawning `git maintenance run
  --auto --detach`, a child that outlives the commit; git 2.52 made the
  geometric strategy its default, whose repack fires once the sampled bucket
  `.git/objects/17` holds two loose objects — for a tiny fixture, the moment
  a commit hash starts with `17`. The daemon's `git repack` then recreates
  `.git/objects/pack` while `t.TempDir`'s cleanup is deleting the repository
  ("unlinkat …/.git: directory not empty", `TestChurnFunctionTextAndBrief`
  on ubuntu-latest; reproducible under CPU load with git 2.55). Every package whose
  tests commit into temp trees — `app`, `complexity`, `complexitycli`,
  `forge`, `project`, `vcs`, `vcscore` — calls
  `gittest.DisableAutoMaintenance()` from `TestMain` (`internal/gittest`): it
  sets `maintenance.auto=false` and `gc.auto=0` through `GIT_CONFIG_COUNT`,
  which reaches the fixtures' gits and the code's alike. A new package with
  git fixtures does the same.
- **The suite runs on macOS and Linux, pinned to the macOS keymap.** The
  tests feed cmd chords and expect the macOS-only default rows, so the
  `keymap`, `settings`, `tour` and `app` packages pin `keymap.GOOS =
  "darwin"` in their `TestMain` (#3000); a test of the Cmd→Ctrl fold sets
  `GOOS` to `"linux"` itself and restores it. A command whose only chord is a
  `darwinRows` row carries a `reasonPlatformChord` entry in the keybind
  audit ledger, which reads the running platform's table. A test that
  genuinely needs macOS skips with a reason naming the dependency; CI
  (`test.yml`) runs both platforms as a matrix with fail-fast off.
- **Measurements run on request.** A test that only logs timings over a huge
  fixture (the 100 MB `TestNDJSONOpenPhases` case) skips unless
  `IKE_SLOW_TESTS=1` is set; it is the one thing the default run leaves out,
  and CONTRIBUTING.md says how to run it.
- **Deterministic ledgers are part of the change.** A new default chord needs
  its `noCounterpart` row in `internal/keymap/jbimport/coverage_test.go` (or
  an `actionMap` entry), and a new tool window its id in
  `config.PaneSlotTools()`; the keydoctor grid and the settings capture tests
  read the real default table, so they move when it does.

Measure before claiming: `go test -count=1 -json ./internal/app | jq -r
'select(.Action=="pass" or .Action=="fail") | select(.Test!=null) |
"\(.Elapsed)\t\(.Test)"' | sort -rn | head` lists a package's slowest tests;
`go test -count=1 -json ./... | jq -r 'select(.Test==null) |
select(.Action=="pass" or .Action=="fail") | "\(.Elapsed)\t\(.Package)"' |
sort -rn | head` the slowest packages.
