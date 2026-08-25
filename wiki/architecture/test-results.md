---
type: concept
title: Test Results Tool Window
description: Singleton bottom-split pane showing a captured test run as a structured tree — group → test → subtest with pass/fail/skip glyphs, durations and a summary line, a detail column with the selected test's output, jump-to-failure, and re-run all / failed / single actions; fed by a per-language output-parser seam on lang.TestSpec — Go, pytest and PHPUnit (#1911, #1926).
resource: internal/testresults/testresults.go
tags: [architecture, tests, run, tool-window, pane, languages]
timestamp: 2026-08-18T00:00:00Z
---

# Test Results Tool Window (#1911)

JetBrains' test runner scaled to the terminal: a singleton tool pane
(`tests.toggle`, palette "Test Results", Tools menu) that shows the last test
run as a structured result tree instead of a raw terminal stream. Left column:
the tree (group → test → subtest) with `✓`/`✗`/`○` glyphs, per-test durations
and a `n passed · n failed · n skipped · time` summary. Right column: the
selected test's buffered output — or, toggled with `o`, the whole run's raw
output, so the terminal stream is never more than one keypress away.

## The parser seam

`lang.TestSpec` (the #1150 test-runner seam) grew three optional structured
fields (`internal/lang/test.go`):

- **`StructuredArgs`** — appended to a synthesized test argv when the run is
  captured (`go test` gains `-json`, pytest gains `-v --tb=short`). They must
  only change the output format, never which tests run.
- **`ParseOutput func(string) []lang.TestResult`** — parses the captured
  combined output into results: group (Go package / pytest file), a
  `/`-nested name (subtests, class methods), status, duration, the test's own
  buffered output, a `file:line` failure location, and a `RerunID`.
- **`FailedArgv` + `NamesJoin`** — an argv template re-running a named set of
  tests by their RerunIDs: Go joins them into one `-run '^(A|B)$'`
  alternation (`NamesJoin: "|"`), pytest into a `-k 'a or b'` expression; an
  empty `NamesJoin` expands a whole-element `{names}` to one argv element per
  id (node-id style). `{file}` resolves to the test file's base name.

Two more fields serve runners that belong to the project rather than to the
file (#1926):

- **`RunAtRoot`** — the test argv runs with cwd = the project root instead of
  the test file's directory, and `{file}` expands to the file's root-relative
  path. `run.TestConfig` reads it when it picks a configuration's cwd.
- **`Runner func(root string) []string`** — resolves a whole-element
  `{interpreter}` to the project's test binary (rather than the language
  interpreter the toolchain detection yields), with `Tool` as the fallback.

Registered parsers: **Go** (`plugins/languages/go/testoutput.go`, the
test2json event stream — per-test output buffering, elapsed times, the first
indented `file.go:line:` marker as the failure location, a synthetic
"(package failed)" node for build errors), **Python**
(`plugins/languages/python/testoutput.go`, pytest's `-v` progress lines plus
the `--tb=short` FAILURES blocks; pytest also gained its `TestSpec`, so the
`▶` gutter markers and `run.testAtCursor` now cover `test_*.py` /
`*_test.py`) and **PHP/PHPUnit**
(`plugins/languages/php/testoutput.go`, the `--teamcity` service-message
stream). A language without `ParseOutput` keeps the raw Run tool path
(#1905) untouched — the fallback is the absence of the seam, not a crash.

### PHP / PHPUnit (#1926)

Detection is PHPUnit's own convention: `*Test.php` files holding
`public function testX()` methods. The runner is the project's
`vendor/bin/phpunit` (`vendor/bin/simple-phpunit` for the Symfony bridge); a
root that only carries a `phpunit.xml` / `phpunit.xml.dist` falls back to a
global `phpunit` on PATH, and a root with neither keeps the plain `phpunit`
name in the argv. Runs happen at the project root (`RunAtRoot`), so PHPUnit
finds its configuration file and the composer autoloader by itself — no
`--configuration` plumbing.

`--teamcity` is the parsed surface: one `##teamcity[event key='value' …]`
service message per line on **stdout**, stable across PHPUnit 9, 10 and 11.
`--log-junit` is the more familiar machine format but writes a *file* the
captured run never sees, which is why the streaming-friendly TeamCity output
wins here. Values are TeamCity-escaped (`|n`, `|'`, `||`, `|[`, `|]`), so a
multi-line failure diff still stays on one line.

The parser maps `testSuiteStarted` / `testStarted` / `testFailed` /
`testIgnored` / `testFinished` onto the result tree: the group is the test's
fully qualified class (from the message's `php_qn://file::\Class::method`
locationHint, the enclosing suite as fallback), the duration comes from
`testFinished` (milliseconds), and a data-provider case
(`testSum with data set #0`) nests as the subtest `testSum/#0` — its
`RerunID` stays the bare method name, because `--filter` cannot address a
single data set. Failure locations prefer the stack frame inside the test's
own file over the PHPUnit-internal frames above it, so Enter lands on the
failing assertion. Single-test and re-run-failed runs use one anchored
filter, `--filter '/::(testA|testB)( |$)/'` — the trailing `( |$)` keeps the
data sets in and same-prefix siblings out.

## Wire

`internal/app/testrun.go` intercepts `launchRun` (`internal/app/run.go`): a
test-scope configuration whose language has a parser — and
`tests.results_window` on — runs **off-loop with captured combined output**
(`exec.Command`, not a PTY; `run.StructuredArgv` builds the argv) instead of
streaming into the Run tool. The pane opens on start (`tests.auto_open`,
focus stays where the user was), shows "running…", and a `TestRunDoneMsg`
fills it via the parser; a monotonic sequence number drops a stale run's
completion. The store is touched exactly like a terminal run, so
**run.rerun** repeats the captured run.

Re-runs go the other way: the pane emits `testresults.RerunMsg` (all /
failed-only / single), the app resolves the RerunIDs against the remembered
last run (`run.FailedArgv`) and starts another captured run — "re-run failed"
re-runs only the tests whose last status was fail.

## The pane

`internal/testresults.Model` follows the Problems pattern (value-type model,
`pane.KindTests`, singleton key `"tests"`, context id `"tests"`), opened as
an adaptive split of the active editor (`auxZone`, #1588) by the
`problems.toggle`-style state machine in `internal/app/tests_panel.go`. The
two-column render (tree │ detail) is the debug panel's row-wise join.

## Interaction

- `j`/`k` (full `ui.ListNav` semantics) move the tree cursor; the detail
  column tracks the selection. `tab`/`l` and `h` move the scroll focus
  between columns.
- `enter` / double-click jumps: to the parsed failure location (resolved
  against the run's working directory) when there is one, else to the test's
  source declaration — the app re-scans the run's test files through the
  language's detection pattern (`testresults.LocateTestMsg`). A group row
  opens its first failing descendant.
- `y` (or `cmd+c`/`super+c`) **copies the marked tree row** (#2071):
  `PASS|FAIL|SKIP <group/test path>`, plus the duration and the parsed failure
  location when there is one. The panel emits `testresults.CopyMsg`; the root
  model writes it through the shared `copyToClipboard` seam and toasts
  "copied test result". It copies the marked row in both columns — the detail
  text scrolls, it is not marked.
- `r` re-runs everything, `f` only the failed set, `t` the selected test.
- `o` toggles the detail column between the selected test's output and the
  whole run's raw output.
- The wheel scrolls the focused column; a click right of the separator moves
  the scroll focus to the detail column.

## Persistence & settings

The pane persists as identity kind `"tests"` and restores empty in its saved
slot — results are session state. It counts as a tool window for
`window.hideAllTools` (#791) and is a `[tools.layout]` assign target (id
`tests`, #1897). No default chord (the budget is full, #711) — the palette,
the Tools menu and `tests.toggle` bindings deliver it.

Settings (Settings → Tests): `tests.results_window` (off = every test run
stays in the raw Run tool terminal) and `tests.auto_open` (off = a captured
run only updates an already open pane).

## Run with coverage (#2081)

**run.testsWithCoverage** (palette) is run.testsInFile through the same
captured path with coverage collection: `lang.TestSpec` grew two more optional
hooks — `CoverArgs(profile)` returns the extra argv elements writing coverage
data to a temp profile (Go: `-coverprofile=<tmp>`), `ParseCover(profile, dir)`
parses that data into the neutral per-file line model
(`lang.FileCoverage`, covered / uncovered / partial per 1-based line) — so a
non-Go plugin (coverage.py, phpunit clover) registers coverage without engine
changes. `finishTestRun` consumes the profile: the parsed files replace the
app's per-run store (`internal/coverage.Store`, deliberately separate from the
result parsing so a plain re-run or re-run-failed — which produce no profile —
never invalidates coverage of untouched files), the run summary line gains a
`n% coverage` figure (executed lines over tracked lines, `SetCoverage`), and
every open editor of a covered file receives its gutter marks
(`coverage.MarksMsg`; files opened later are seeded from the store in
openPath). Go resolves the profile's import-qualified paths against the
module root found by walking up from the run directory to `go.mod`
(`plugins/languages/go/coverage.go`).

**coverage.toggle** (palette) hides/shows the marks without dropping the data;
the `editor.marks.coverage` setting is the persistent gate underneath. Editing
a file marks its coverage stale — visible but neutralized — in the store (for
later opens) and instantly in every live view by document version (see
/architecture/editor.md).
