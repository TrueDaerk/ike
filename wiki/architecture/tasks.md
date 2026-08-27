---
type: concept
title: Tasks & Problem Matchers
description: Task discovery (Makefile targets, package.json scripts, justfile recipes) behind the lang.TaskProvider seam, run through the Run Task picker as ephemeral or promoted run configurations; named regex problem matchers tee the run terminal's output into per-run Problems entries (#1915).
resource: internal/matcher
tags: [architecture, tasks, run, problems, matchers, languages, config]
timestamp: 2026-08-17T18:00:00Z
---

# Tasks & Problem Matchers (#1915)

Build-tool targets become runnable without hand-written configurations, and
compiler output becomes clickable diagnostics. Two halves, joined by the run
configuration:

- **Task discovery** enumerates what a project's build tooling already
  defines — Makefile targets, package.json scripts, justfile recipes — and
  lists it in the **Run Task** picker (`run.task`).
- **Problem matchers** parse the run's terminal output line by line into
  file/line/col/severity/message records that stream into the
  [Problems tool window](/architecture/problems.md) under a per-run source.

## Task discovery (`internal/lang/tasks.go`)

The provider seam lives in the language registry, so plugins contribute
discovery exactly like languages:

```go
type Task struct {
    Name     string   // target as the tool spells it ("build")
    Source   string   // providing tool ("make", "npm", "just")
    Argv     []string // literal command line ({"make", "build"})
    Dir      string   // project-relative cwd, "" = root
    Matchers []string // default problem matchers for this task's output
}
type TaskProvider interface {
    Source() string
    Tasks(root string) []Task
}
```

`lang.RegisterTaskProvider` registers (last-writer-wins per Source);
`lang.Tasks(root)` aggregates every provider in registration order, each
provider's tasks sorted by name. Discovery is best-effort: a missing or
malformed manifest yields nil, never an error.

The built-in providers live in `plugins/tasks` (blank-imported by `cmd/ike`
and `cmd/docgen`): **make** parses `Makefile`/`makefile`/`GNUmakefile` rule
lines (dot-targets, pattern rules and variable machinery skipped), **npm**
reads `package.json` `scripts` (run as `npm run <name>`), **just** reads
`justfile`/`Justfile`/`.justfile` recipes (underscore-private recipes
skipped). Every built-in task defaults to the full built-in matcher set —
overlap is deduplicated and unmatched output costs nothing.

## Running and promoting (`internal/app/task_picker.go`)

`run.task` opens the palette locked to the tasks mode (prefix `)`, the
runConfigsMode pattern). Picking a row builds `run.TaskConfig` — a run
configuration named `"source: name"` carrying the task's **literal argv**
(`Config.Argv`, a #1915 field that short-circuits `run.Argv` past the
language synthesis) and its default matchers — and launches it through the
ordinary `launchRun` funnel into the Run tool. The run is **ephemeral**:
nothing is written, and `last_used` only moves for stored configurations.

`run.taskPromote` opens the same list but **stores** the picked task in
`.ike/runconfigs.json` instead of running it; re-promotions of the same task
fold into one entry (Upsert by name). A promoted task is a completely normal
run configuration afterwards — the run.select picker lists it, `run.rerun`
repeats it, and a picker run of the same task uses the stored entry, so
hand-edits (narrowed matchers, extra env) apply.

## Problem matchers (`internal/matcher`)

A matcher is a named parser over output lines:

```go
type Matcher interface { Name() string; NewState() State }
type State interface { Feed(line string) []Problem; Flush() []Problem }
```

Single-line matchers are `matcher.Rule` values — a compiled regex plus
1-based capture-group indexes for file/line/col/severity/message. Built-ins:
**go** (`./x.go:5:2: msg`, column optional), **generic**
(`file:line[:col]: [severity:] msg`; the file part needs a dot or slash so
timestamps never match), **tsc** (`src/a.ts(12,5): error TS2304: msg`) and
**python** — a multi-line state machine: `File "x.py", line N` frames set
the location (deepest frame wins) and the first following non-indented line
(`ZeroDivisionError: …`) is the message.

`matcher.Engine` is the streaming front: it assembles raw PTY chunks into
lines, strips ANSI, feeds every referenced matcher and deduplicates the
problems over the whole run (overlapping matchers, repeated errors).

Custom matchers are project config (see
[Configuration System](/architecture/config.md)):

```toml
[[tasks.matcher]]
name = "mylint"
pattern = "^(\\S+) at line (\\d+): (.+)$"
file = 1
line = 2
message = 3
default_severity = "warning"   # optional; col/severity groups optional too
```

`matcher.Compile` validates entries — pattern compiles, file/line/message
groups present, indexes inside the pattern — and config validation drops a
broken or duplicate entry with the compiler's message as a diagnostic. A
custom matcher may shadow a built-in name; resolution prefers the project's
entry.

## The output tee (`internal/app/taskproblems.go`)

The run terminal already streams output; the matcher taps it rather than
re-running anything. `terminal.Session` grew an optional tap
(`SetTap`/`Model.SetOutputTap`): the feed loop hands every replayed chunk to
the tap on the feed goroutine — **off the render loop** — before writing the
emulator. `launchRun` builds a `taskCollector` for configurations with
matchers and installs it on the fresh Run-tool session right after the
spawn.

The collector feeds the engine, resolves relative paths against the run's
cwd, converts to 0-based `ilsp.Diagnostic`s (Source = the configuration
name) and publishes the accumulated set as a `TaskProblemsMsg` snapshot **per
~50ms quiet window** (#2176) — not per matching chunk: a broken build
spraying thousands of findings used to deep-copy the whole map on the feed
goroutine and re-sort the whole store per chunk, O(n²) on both sides. The
timer always fires within one window of the last match, so a finished run's
final state is never withheld. The Update loop stores it via
`problems.Store.SetTaskSource` — a third channel next to the server and
lint-note maps, keyed source → path — and refreshes the panel. Every launch
first runs `ClearTaskSource(cfg.Name)`, so **a re-run replaces its previous
problems instead of duplicating them**, and one task's findings never
clobber another's or the LSP's. Entries navigate like any diagnostic
(`OpenLocationMsg` → open at file:line); unmatched output stays untouched in
the terminal.
