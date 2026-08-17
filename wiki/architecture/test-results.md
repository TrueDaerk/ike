---
type: concept
title: Test Results Tool Window
description: Singleton bottom-split pane showing a captured test run as a structured tree — group → test → subtest with pass/fail/skip glyphs, durations and a summary line, a detail column with the selected test's output, jump-to-failure, and re-run all / failed / single actions; fed by a per-language output-parser seam on lang.TestSpec (#1911).
resource: internal/testresults/testresults.go
tags: [architecture, tests, run, tool-window, pane, languages]
timestamp: 2026-08-17T00:00:00Z
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

Registered parsers: **Go** (`plugins/languages/go/testoutput.go`, the
test2json event stream — per-test output buffering, elapsed times, the first
indented `file.go:line:` marker as the failure location, a synthetic
"(package failed)" node for build errors) and **Python**
(`plugins/languages/python/testoutput.go`, pytest's `-v` progress lines plus
the `--tb=short` FAILURES blocks; pytest also gained its `TestSpec`, so the
`▶` gutter markers and `run.testAtCursor` now cover `test_*.py` /
`*_test.py`). A language without `ParseOutput` keeps the raw Run tool path
(#1905) untouched — the fallback is the absence of the seam, not a crash.

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
