---
type: concept
title: Run Configurations
description: Work stream 0350 — named, persisted run/debug configurations synthesized into command lines through the language registry; per-project store in .ike/runconfigs.json; output in the dedicated Run tool pane (#1905), placed in the layout's tool region rather than the outermost bottom and remembering a user-moved position per project (#2191); run.select picker merging .vscode/launch.json imports (#1914); literal-argv task configurations, the Run Task picker and problem matchers (#1915); the run.editConfig environment form and the kind-faithful rerun-last chord (#2173).
resource: internal/run
tags: [architecture, run, debug, toolchain, languages, vscode, tasks]
timestamp: 2026-08-27T18:00:00Z
---

# Run Configurations (0350)

Epic #572. `internal/run` holds JetBrains-style run configurations: named,
persisted descriptions of how to run (or debug) a file. A configuration is
**data, not a shell string** — the command line is synthesized at launch, so
interpreter changes (venv switch, explicit `[lang.<id>] interpreter`) apply
to every later run automatically.

## The model (`internal/run`)

```go
type Config struct {
    Name   string            // unique; base name, or relative path on collision
    Kind   Kind              // "run" | "debug" — a debug launch reuses the run's data
    Lang   string            // language id in the registry
    File   string            // project-relative target file
    Module string            // language module spelling (Python -m), optional
    Args   []string          // program arguments
    Env    map[string]string // extra environment
    Cwd    string            // project-relative working dir; "" = root
    Tests  bool              // test-scope config (#1150): argv via the TestSpec seam
    TestName, TestKind string // one test function; empty name = whole file scope
    Argv     []string        // literal command line (#1915, task configs): skips synthesis
    Matchers []string        // problem matchers over the run's output (#1915)
}
```

- **Store**: `run.Load()` / `run.Save(store)` persist `.ike/runconfigs.json`
  (`IKE_CONFIG_DIR` override like session/layout). `Store` keeps the ordered
  config list plus `LastUsed` and `LastKind` — the rerun-last target *and the
  way it was started* (`Touch(name, kind)` / `Last` / `LastLaunch`, #2173).
  Missing or malformed files load as empty — run configs are convenience
  state, never a startup error; a failed save must not abort the run. A store
  written before #2173 carries no `last_kind`; that reads back as `KindRun`.
- **Default synthesis**: `Store.EnsureFor(root, file)` returns the config for
  a file, creating and remembering the default on first run: kind `run`, no
  env, cwd = project root, the language's module form when the file lies in
  a package, name = base name (relative path on collision).
- **Launch**: `run.Argv(root, cfg, explicitInterpreter)` resolves the argv
  through the language seam below — unless the configuration carries a
  literal `Argv` (#1915, task configurations), which is returned as-is;
  `Config.Dir(root)` and `Config.EnvSlice()` feed the terminal spawn.
- **Environment rows** (#2173): `run.EnvRows(env)` renders the overlay as the
  sorted key/value rows the form edits, `run.ValidateEnvKey` and `run.EnvMap`
  are the validation seam (empty keys, keys carrying `=` or whitespace, and
  duplicate keys are rejected with a user-facing message), and
  `Config.SetEnv(rows)` validates a whole set before it replaces `Env` — an
  invalid set leaves the configuration untouched, an empty one clears the
  overlay. The rules live next to the store, not in the dialog, so the store
  is never handed an unchecked map.
- **Tasks**: `run.TaskConfig(lang.Task)` renders a discovered build-tool
  target (Makefile/npm/just, #1915) as a configuration named
  `"source: name"` with the literal argv and the task's default problem
  matchers — see [Tasks & Problem Matchers](/architecture/tasks.md).

## The language seam (`internal/lang/run.go`)

Language plugins contribute run behavior via optional `Toolchain` extensions:

- `RunCommandProvider.RunCommand(root, RunSpec{File, Module, Args},
  interpreter) (argv, ok)` — the interpreter arrives pre-resolved via
  `lang.Interpreter` (explicit config beats detection, one source of truth
  with the LSP/terminal shims).
- `ModuleResolver.Module(root, file) (module, ok)` — the file's module
  spelling for default configs.

Registered providers:

| Language | Command | Module form |
|---|---|---|
| Python | `<interpreter> file.py` / `<interpreter> -m pkg.mod` | dotted path when every directory from root to the file is a package (`__init__.py` chain); `__main__.py` maps to its package |
| PHP | `<php> file.php` | — |
| Go | `<go> run file.go` | — |
| Shell | `<shell> file.sh` — explicit `[lang.shell] interpreter` > the file's shebang shell (only when that binary is on PATH) > the extension's natural shell (`.bash` → bash, `.zsh` → zsh, `.sh` → sh); never executes via the shebang directly (no chmod) | — |

## Running (#576)

`internal/app/run.go` wires the commands end to end:

- **`run.file`** (shift+f10 — JetBrains' Windows-keymap Run; macOS ctrl+r
  would shadow vim redo — Run menu, palette) ensures a configuration for the
  active file (`EnsureFor`; the first run persists the default and says so in
  the toast) and launches it. **`run.rerun`** (cmd+f5 / ctrl+f5) repeats the
  last-used config **the way it was started** (#2173): every launch funnel
  touches the store with its kind, so a configuration last run under the
  debugger reruns through `startDebugConfig` and everything else rides
  `launchRun`. The marker is part of the per-project store, so the chord still
  repeats yesterday's run after a restart; nothing ever run yet is a toast
  (`run: nothing to rerun yet`), never a silent no-op.
- The command runs as a **terminal command session** (#574) — interactive
  stdin, exit code shown on completion — with the toolchain shim env plus the
  config's env overlay, in the config's cwd; the terminal is labelled with
  the config name.
- **The Run tool** (#1905) is where the output goes: a dedicated tool pane
  (`tool` identity `run`, per project) exactly like a `[[tools.custom]]` one —
  see [Custom TUI Tool Panes](/architecture/tool-panes.md). Before #1905 a run
  took over the *first reusable terminal* (`ReusableRunTerminal`: any terminal
  never typed into or already finished), which dropped run output into an
  unrelated terminal pane or, worse, into an open tool pane's tab list. Runs
  now target their own tool identity only; a terminal the user opened is never
  touched, and the Run tool is never used for anything but runs.

### The Run tool (#1905)

`internal/app/run.go` places run output through the shared tool-pane machinery
(`Model.placeTool`, `internal/app/tools.go`), so the Run tool behaves like
every other tool window:

- **One instance, reused** — `startInRunTool` finds the open Run tool wherever
  it lives (dedicated pane or hosted tab, `toolLocations("run")`) and starts
  the new command **in place** (`StartCommand`), relabelling the pane after the
  configuration and focusing it. A still-running program is replaced: the Run
  tool is the run's one home, so runs never pile up panes. Only with no Run
  tool open does `openRunTool` create one.
- **Placement** — `run.placement` (settings page "Run", default `bottom`) names
  the Run tool's **home position**: `bottom`/`left`/`right`/`top` dock the pane
  at that workspace edge with the ordinary `#1889` dock rules (a tab-capable
  dock occupant takes the session as a focused tab), `in_pane` keeps the
  pre-#1905 shape — a terminal tab in the focused editor pane (#573), falling
  back to the adaptive split when no editor pane exists. A `[tools.layout]`
  slot assigned to `run` (#1897) overrides the setting, exactly as it does for
  a configured tool. The removed `new_terminal` value lives on as an alias for
  `bottom` — where it always put the output — and migrates silently
  (`internal/config/validate.go`). Where the placement actually lands in a
  nested layout, and what happens once the user moves the pane, is below.
- **Lifecycle** — the pane closes with `ctrl+w` like any pane, and the
  program's exit leaves it open with the standard #810 overlay
  (`run exited (code N)` plus `Restart`/`Close`); `Restart` reruns the same
  command line in place.
- **Session state, never restored** — `saveLayout` records the pane as
  `{kind: "runTool"}` and the restore **prunes that leaf** (the debuggee
  terminal's precedent, #1370): a program must not re-run itself at startup
  just because its output was on screen. A saved *window* layout (#1175) that
  captured the Run tool re-slots a live one and otherwise restores an empty
  editor there.
- **Reserved name** — the tool identity is `run`; a `[[tools.custom]]` entry of
  that name would be indistinguishable from the Run tool.

### Where run output lands (#2191)

The placement setting names an *edge*, but "bottom" only means the bottom of
the whole workspace while the workspace is one column. In a nested layout —
the explorer beside an editor column whose own bottom carries Problems / Test
Results — docking at the outermost bottom re-roots the tree and stretches the
run output under the explorer, away from the tool area the user actually
works with. Placement therefore resolves in three steps, sharing the
tool-pane machinery (`Model.dockNewPane`, `internal/app/tools.go`):

1. **The workspace-edge dock slot**, exactly as before (`layout.EdgeLeaf`):
   free → full-span dock, tab-capable occupant → focused tab, other occupant
   → perpendicular split.
2. **The layout's own tool region** (`Model.toolRegionLeaf`) when that edge is
   no slot at all: the active editor's ancestor path is walked outwards
   (`layout.Hops`) and the first sibling region on the placement's side that
   holds *nothing but* tool windows — Problems, Test Results, tool panes,
   tool-tab hosts — is the tool area. The Run pane splits **beside** that
   region's edge leaf (`layout.EdgeLeafIn`), joining the strip instead of
   re-rooting the tree, and never tabs into it (#1905: a tool's or shell's
   tab list is not run output's home). This applies to every home-docked tool
   pane, not just the Run tool. See
   [Tool Panes](/architecture/tool-panes.md) § "Home positions".
3. **The full-span workspace dock** when there is no tool region — an editor
   beside the editor is editor area, not a tool strip.

A **rerun never places anything**: `startInRunTool` finds the open Run tool
first and restarts the command in its session, so runs cannot pile up splits.

**A moved Run pane keeps its place** (`internal/app/runhome.go`). Once a
layout drag relocates the Run tool, its position is recorded per project in
`.ike/runhome.json` (`{anchor, zone, ratio, tab, root, placement}`, the
`IKE_CONFIG_DIR` seam like every other state file) and the next run re-opens
there — split off the recorded anchor pane at the recorded side and share, as
a tab of it when that is where it was dragged, or as the full-span workspace
dock (#811) it was docked to (`root`: the pane hung straight off the root
split, so `layout.DockNew` re-docks it at its remembered share instead of
splitting whichever leaf happened to touch it). This is deliberately
*unlike* a configured tool, whose placement stays pure intent (#1889): run
output reappears on every run, so where the user put it is where it belongs.
The record is what makes the position survive both the pane's close and a
restart — the layout's `runTool` leaf prunes on restore, so `layout.json`
alone would forget it. It is written **only** when a drag actually moves the
pane, and it carries the placement it overruled: a slot assignment (#1897)
still wins, and editing `run.placement` re-asserts the setting instead of
being shadowed by a stale drag. An anchor pane that is gone from the
workspace is ignored, falling back to the rules above.

## Test runner (#1150)

A language can declare **test detection + command templates** as data
(`lang.TestSpec` in `internal/lang/test.go`): a line-anchored regexp with
named groups `name` (the runnable test's full name) and optional `kind`, a
`FilePattern` restricting detection to test files, per-kind argv templates
with `{interpreter}`/`{name}` placeholders, a `FileArgv` template for the
whole file scope, a `Tool` fallback binary and an `Exclude` name list.
Detection is deliberately regex-based (not documentSymbol/Tree-sitter): it
works without a language server and in CGo-free builds, and test declarations
are strictly line-anchored in the supported languages. The synthesized argv is
executed directly — no shell ever parses it, so quoting is shell-agnostic by
construction.

Registered specs:

| Language | Detection | Commands |
|---|---|---|
| Go | `^func (Test\|Benchmark\|Fuzz)X(` in `_test.go` files (bare `Test` counts, `Testify` and `TestMain` do not) | `go test -run '^TestX$'`; benchmarks `go test -bench '^BenchmarkX$' -run '^$'`; file scope plain `go test` — all with cwd = the file's directory (its package) |
| Python | `def test*(` (async too) in `test_*.py` / `*_test.py` files (#1911) | single test `python -m pytest FILE -k NAME`; file scope `python -m pytest FILE` — the resolved project interpreter, so the venv's pytest runs |
| PHP | `public function testX(` in `*Test.php` files (#1926) | single test `phpunit --filter '/::testX( \|$)/' FILE`; file scope `phpunit FILE` — the project's `vendor/bin/phpunit`, run with cwd = the project root (`TestSpec.RunAtRoot`), so phpunit.xml and the composer autoloader resolve |

A test-scope run whose language also declares an **output parser**
(`TestSpec.ParseOutput` — Go's `go test -json`, pytest's `-v`, PHPUnit's
`--teamcity`) is captured
and lands in the **Test Results tool window** instead of the Run tool; see
[Test Results Tool Window](/architecture/test-results.md). The
`tests.results_window` setting turns the capture off entirely.

Wiring:

- The **editor** caches the detected declarations per document version
  (`internal/editor/testmarks.go` — one `O(lines)` scan at most per edit,
  never per frame; per-view pointer store like the line cache) and renders a
  `▶` **gutter run marker** in the success tone on each test line. Sign
  precedence: debugger paused line > breakpoint `●` > test `▶` >
  diagnostic/git colouring.
- **`run.testAtCursor`** (Run menu, palette, editor context menu) runs the
  test at or nearest **above** the cursor; a notice appears when none is
  there. **`run.testsInFile`** runs the file's whole scope. Neither has a
  default chord (the budget is full, #711).
- **Gutter clicks**: a plain left click keeps toggling the breakpoint on
  *every* line — including test lines, so breakpoints on test functions stay
  reachable. **ctrl+click or cmd+click on a marker line runs that test**; on
  other gutter lines the modified click still toggles the breakpoint.
- Both commands synthesize a **test-scope configuration**
  (`run.TestConfig`: `Tests: true`, `TestName`/`TestKind`, cwd = the file's
  dir, stable name `TestX (pkg/dir)` / `tests: pkg/dir` so repeats fold into
  one config) and launch it through the ordinary placement rules above —
  which also registers it with **run.rerun's last-used memory**, so rerun
  repeats the exact test.

## The picker and launch.json (#1914)

**`run.select`** (Run menu, palette) opens the run-configuration picker: a
palette-locked mode (`internal/app/run_picker.go`, the stored-HTTP-requests
pattern) listing every stored configuration plus, when `run.vscode_launch` is
on (default), the compatible entries of the project's `.vscode/launch.json`.

`internal/vscodelaunch` is the leaf parser: JSONC-tolerant (comments,
trailing commas), it maps `launch`-request entries of types `go` /
`python` / `debugpy` / `php` onto `run.Config{Kind: KindDebug}` —
`${workspaceFolder}`/`${workspaceRoot}` expanded; entries with other
variables, attach requests, unknown types, or programs outside the root are
skipped silently (launch.json is someone else's file; a partial import beats
an error). Go's `"mode": "test"` maps to a test-scope config.

Merging happens at open time and nothing is written back — the store stays
authoritative and wins name collisions; imported rows name their source in
the detail column. Picking a run-kind row rides the ordinary `launchRun`
funnel; a debug-kind row (all launch.json imports, and the picker is how a
stored debug config is launched by name) goes through `startDebugConfig`,
the same guard funnel as `debug.start`.

**`run.editConfig`** (Run menu, palette, #2173) opens the same picker in
**edit mode** — stored configurations only, since a `launch.json` import is
someone else's file and is never written back — and a picked row opens the
**run-configuration form** (`internal/app/runconfig_form.go`): the shell
dialog editing that configuration's environment as rows. `↑↓`/`jk` select,
`a` adds a row, `enter` opens the two-field row editor (`tab` switches key and
value), `d` removes, `ctrl+s` saves and `esc` discards. A row only joins the
list once it validates (`run.ValidateEnvKey` plus a duplicate check against
the other rows), so a typo is corrected in place instead of being dropped, and
the save re-validates the whole set before writing `.ike/runconfigs.json`.
From there every later launch spawns its process with those variables
(`Config.EnvSlice` → `terminal.MergeEnv`). The form is checked **ahead of the
popup/terminal key routing** in `internal/app/app.go`: it is typically opened
right after a launch, when the Run tool's terminal holds the focus, and a
modal dialog must not lose its keys to it.

Two sibling commands ride the same locked-palette pattern (#1915):
**`run.task`** lists the build-tool targets the task providers discover
(Makefile targets, package.json scripts, justfile recipes) and runs a picked
one as an ephemeral configuration; **`run.taskPromote`** stores the picked
task as a normal configuration instead. Details in
[Tasks & Problem Matchers](/architecture/tasks.md).

## Breakpoints (#577)

`internal/debug` holds the per-project breakpoint store: line breakpoints
keyed by project-relative path (0-based lines), persisted at
`.ike/breakpoints.json` on toggle and on file save; missing/malformed files
load empty.

- **Toggling**: `debug.toggleBreakpoint` (ctrl+f8, Run menu, palette) on the
  focused editor's cursor line, or a **left click in the gutter**
  (`editor.GutterHit` maps the click through folds/wrap/sticky headers).
- **Rendering**: the editor queries an injected breakpoint source per frame
  (`SetBreakpointSource` — no push bookkeeping; shared documents and every
  view stay current) and renders the line number bold in the error tone,
  winning over diagnostic and VCS gutter colours.
- **Edit adjustment**: the editor reports line-count deltas at the edit site
  (`SetBreakpointAdjuster`, same pattern as fold shifting in
  `dissolveFoldsAtEdit`); the store shifts breakpoints below insertions and
  deletions, collapsing ones inside a removed range. Wholesale buffer
  replacements (load, share, remote sync) re-baseline instead of shifting.

## DAP client (#578)

`internal/dap` is the Debug Adapter Protocol client: the LSP base-protocol
framing (`jsonrpc.WriteFrame`/`ReadFrame`, shared with the language servers)
carrying DAP's `seq`/`type` envelope. `Conn` correlates requests with
responses (bounded by a call timeout) and dispatches events (stopped,
continued, terminated, output, initialized) to a handler; reverse requests
(runInTerminal) are refused so adapters fall back. `Session` types the
vocabulary IKE uses: `Initialize`, `LaunchAsync` (adapters like debugpy
answer launch only after `ConfigurationDone`), `SetBreakpoints` (0-based in,
1-based on the wire), stepping (`Next`/`StepIn`/`StepOut`/`Continue`),
`Threads`/`StackTrace`/`Scopes`/`Variables`, `Disconnect`. Adapter processes
spawn through `internal/lsp/transport` exactly like language servers.

Languages contribute adapters via `lang.DebugAdapterProvider`
(`DebugAdapter` argv + `DebugLaunchArgs`): Python uses debugpy
(`<interpreter> -m debugpy.adapter`; module or program launch form matching
the run config). Go's `dlv dap` only speaks DAP over a socket, so it rides
the in-process connect seam instead (#1914) — see
[Debugger](/architecture/debugger.md) § "Go: delve over a socket".

## Consumers

- The debug session orchestration (#579) drives Session from a run
  configuration with kind `debug`, stopping at the stored breakpoints.
