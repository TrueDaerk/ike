---
type: concept
title: Code Complexity
description: The code-complexity engine (Roadmap 0550, #2913), cyclomatic (McCabe) and cognitive (Sonar) complexity, NLOC, parameters and nesting depth per function, measured on the Tree-sitter parse through each language's ComplexitySpec; the rules, the walker, the per-language golden fixtures cross-checked against lizard, gocyclo, gocognit and complexipy, the summary, the project index (#2916) that measures every supported file in the background, keeps it current from saves and watcher events, and hands consumers immutable snapshots, the Complexity tool window (#2917) and its Changes tab (#2918) diffing the working tree's functions against a base revision with a verdict, the Problems feed (#2920) publishing over-threshold functions under the complexity source, and the headless `ike complexity` CLI (#2922) printing the project table or the diff against a revision as text or JSON for CI and agents; configured by the [complexity] section (Settings > Complexity) with a complexity.scan telemetry op.
resource: internal/complexity, internal/complexitypanel, internal/app/complexityindex.go, internal/app/complexity_diff.go, internal/app/complexity_problems.go, internal/app/complexity_segment.go, internal/complexitycli
tags: [architecture, complexity, metrics, tree-sitter, languages, cyclomatic, cognitive, index, settings, telemetry, vcs, diff, problems, cli]
timestamp: 2026-10-13T12:00:00Z
---

# Code Complexity

Roadmap 0550 measures the **cyclomatic** (McCabe, CCN) and **cognitive**
(Sonar) complexity of every function in the project, shows it in a tool
window, and answers *"did this change make the project more or less
complex?"* by comparing the working tree against `HEAD`. Everything runs on
the Tree-sitter parse IKE already has — no lizard, gocyclo or gocognit on the
machine — so it works for every language with a grammar, in mixed projects,
out of the box. This page documents the **engine** (`internal/complexity`,
#2915) and the **project index** on top of it (#2916, below); the language
seam it reads is [languages](./languages.md#complexity-spec-2914); the tool
window (#2917), the diff against a base revision (#2918), the Problems
feed, the status segment, the editor hints and the headless CLI (#2922)
follow below.

## Model

`Measure(langID, lines)` parses the buffer with the language's grammar
(`highlight.SyntaxTree`), walks the snapshot with the language's
`lang.ComplexitySpec` and returns one record per **unit** in source order:

```go
type Function struct {
    Name      string // "renderWeek", "Server.handle", "Class::method", "(anonymous)#3"
    Kind      string // function | method | lambda
    Line, EndLine int // 0-based header / last line
    NLOC      int    // non-blank, non-comment lines in the range
    Params    int
    CCN       int
    Cognitive int
    Depth     int    // deepest nesting level reached inside the unit
}
```

A **unit** is any node of the spec's `FunctionNodes`: a declaration, a method,
a closure, a lambda, an arrow function. Nested units are units of their own.
A language without a spec, an unregistered id or a build without cgo (no
tree) yields `ErrUnsupported`, never a panic — the caller reads it as "not
measured".

**Naming.** A unit with a name field keeps it; a method is qualified by its
scope — a Go receiver type (`Server.Handle`, pointer and type arguments
stripped), or the `name` of the nearest enclosing `Nesting` kind that is not
itself a unit (a class, trait, interface or enum body): `Class.method` in
Python and TypeScript, `Class::method` in PHP (`MethodSep`). A function nested
in a method keeps its bare name. Anonymous units are `(anonymous)#n`, numbered
per file in source order. `Kind` is `lambda` for anonymous units, `method` for
a qualified unit or a kind whose name says method, `function` otherwise.

**Params** counts the named children of the `ParamsField` node, a child
declaring several names (Go's `a, b int`) once per name. **NLOC** counts the
lines of the unit's range that are neither blank nor comment-only, by the
language's `LineComment` / `BlockComment` markers; a line with code before a
trailing comment counts, a block comment hides lines up to its close. The
range is the whole unit, nested units included (lizard subtracts them).

## Rules

The spec names the kinds; the engine owns the two algorithms, once:

| Spec list | CCN | Cognitive | Nesting |
|---|---|---|---|
| `Branch` (if, elif, loops, catch, ternary) | +1 | +1 + level, or +1 flat in the else position | body nests |
| `Selector` (switch, type switch, select, match) | — | +1 + level | every arm nests, the default included |
| `Arms` (case / match arms) | +1 | — | — |
| `Else` (else clause) | — | +1 flat when it is a Branch's alternative | body nests |
| `Nesting` (lambda, class body) | — | — | body nests |
| nested unit | own unit | counts toward every enclosing unit at the enclosing level, toward itself from zero | enclosing units nest |
| `BoolOps` (`&&` `\|\|` `and` `or` `??`) | +1 per operator | +1 per run of like operators, parentheses flattened | — |
| `Jumps` (goto, labelled break / continue) | — | +1 when the node carries a target | — |
| `Calls` | — | +1 per direct recursive call | — |

**Cyclomatic** is therefore `1 + branches + arms + boolean operators` per
unit, the number lizard and gocyclo print, except that a closure is its own
unit: gocyclo folds a closure's branches into the enclosing function, lizard
splits Go and TS/JS closures but folds Python lambdas and PHP closures.

**Cognitive** follows Sonar's white paper as gocognit and complexipy implement
it. The details the walker gets right:

- **Else-if is flat.** A Branch in the else position — the `alternative`
  field of a Branch (of another kind, or of the same kind only where the
  language has no `Else` kinds: Go's `else if`), the single Branch child of an
  `Else` kind (TS/PHP `else if`), or shell's unfielded `elif_clause` after the
  body — counts +1 without the nesting penalty; its body nests one level like
  the `if` body. A chained ternary in TS or PHP is the parent's own kind, so
  it nests (+2) as Sonar does.
- **Headers do not nest.** Children in a Branch's or Selector's header fields
  (`condition`, `initializer`, `update`, `left`, `right`, `value`, `subject`,
  `parameter`, …) are walked at the node's own level: a ternary in an `if`
  condition costs 1 + the `if`'s level, not one more.
- **Switch counts once.** The Selector takes `+1 + level` and nests every arm
  once (Go's `default_case`, PHP's `default_statement` and TS's
  `switch_default` are not arms but sit inside the Selector, so an `if` in a
  default arm is at level + 1 — gocognit's number). Arms carry only CCN.
- **Boolean runs.** A boolean-operator tree is flattened in source order
  through `parenthesized_expression` nodes and stops at any other operand:
  `a && b && c` is one run, `a && b || c` two, `a && (b || c) && d` three
  (gocognit 1.3 and SonarJS, whose ESTree has no parenthesis node; complexipy
  restarts the run at a parenthesis), `a && f(b || c)` two (the call is a new
  expression). Every operator of the tree is consumed once.
- **Recursion** compares the callee's last identifier segment with the unit's
  short name (`s.handle()`, `$this->handle()`, `self::m()` count; gocognit
  and complexipy count plain-identifier calls only), per call site, and
  through closures: a closure calling its enclosing `f` adds to `f`.
- **Loop else.** Python's `for … else` / `while … else` is an `Else` kind in
  the loop's `alternative` field and counts +1 (complexipy does not); a `try`'s
  `else` is no Branch's alternative and is free, as is `finally`.
- **Depth** is the deepest nesting level a unit's walk reaches — the body of
  an `if` inside an `else` is 2, closures included.

The walk is **iterative**: one explicit frame stack for the pre-order walk,
one for the boolean flattening, a stack of the open units each with its own
nesting counter. A 5 000-term `&&` chain or a 5 000-line file is a loop, not
a recursion.

## Fixtures and cross-check

`internal/complexity/testdata/<lang>/fixture.<ext>` with a
`fixture.golden.json` next to it, one per language with a spec — Go, Python,
PHP, TypeScript, JavaScript (measured through the `typescript` id; one TSX
grammar serves both) and shell — each covering an else-if chain, switch /
match arms with a default, nested loops, try / catch, ternaries, boolean
sequences, nested closures, recursion and labelled jumps.
`go test ./internal/complexity -update` rewrites the goldens from the engine.

The values were cross-checked on the dev machine against **gocyclo 0.6.0**,
**lizard 1.24.1** (CCN), **gocognit 1.3.0** (Go) and **complexipy 8.0.1**
(Python) for cognitive complexity; Go agrees with gocyclo and gocognit on
every function, Python with lizard and complexipy wherever the rules above
coincide. Every remaining difference is a documented property of the tool
(`golden_test.go`): lizard counts `finally` and every `?`-shaped token (`?.`,
`??` twice) and skips PHP's `and` / `or` / `??` / `?:` and `match`; its Go
reader misreads a type switch, its TypeScript reader ends a function a line
late. Shell has no reference tool; its values follow the rules the Go and
Python fixtures verify. `TestGoldenAcceptance` spells out the epic's criteria
on the Go fixture — a closure's branches are not its parent's CCN but raise
the parent's cognitive nesting; `a && b && c` is CCN +2 / cognitive +1,
`a && b || c` CCN +2 / cognitive +2 — so a golden regeneration cannot move
them quietly. `BenchmarkMeasure` measures a synthetic 5 000-line Go file:
about 29 ms on the dev machine (budget 50 ms).

## Summary

`Summarize(fns, ccnWarn, cogWarn)` aggregates a list — a file or a project —
into functions, NLOC, Σ / avg / max CCN, Σ / avg / max cognitive and the
number of functions **at or above** either warning threshold (a non-positive
threshold disables that check). The thresholds come from the `[complexity]`
settings of the later steps; the engine takes them as plain integers.

## Project index (#2916)

The engine measures one buffer; the tool window, the Problems feed and the
status segment need the whole project, measured once and kept current. That
is `complexity.Index` (`index.go`), one per project root, built in
`buildModel` beside the PHP declaration index and wired by
`internal/app/complexityindex.go`.

**Storage** is the shared per-language project walk
(`internal/complete/langindex`, the [PHP trait index](./php-trait-index.md)'s
template): the lazy walk with its built-in skips (`.git`, hidden directories,
`node_modules`, `vendor`, `dist`, `build`, `target`, virtualenvs), a 512 KiB
per-file cap and a 50 000-file cap, one re-extraction worker for watcher
events, and binaries skipped by a NUL in the head. The walk is keyed by
language, but the index wants one pass over every file of every language
with a spec, so it registers a single pseudo language: the classifier claims
a path when `lang.ByPath` names a language with a `ComplexitySpec` and no
`complexity.exclude` glob matches; the extractor re-derives the language
from the path and runs `Measure`. Nothing else is read, and a file the
engine cannot measure (no tree) is not indexed. A 50k-line project walks in
a few seconds; nothing is persisted across sessions yet (the walk's
`Persist` seam is there when it is worth it).

**Lifecycle.** `NewIndex(root, opts)` builds the walk but does not start it;
`Start` (called from the app's `Init`, after the program's sender is wired,
like the TODO index) kicks it off in the background. `Reconfigure(opts)`
applies a reloaded `[complexity]` section live: the master switch off drops
the walk and the snapshot, on rebuilds it, a changed exclude list rewalks,
unchanged options are a no-op. `Rebuild` is the escape hatch for a tree that
changed without watcher events. A project switch builds a new model and
therefore a new index.

**Freshness.** `InvalidateFile(path)` re-measures one file through the
walk's worker, off the Update goroutine: the app calls it from the buffer
save path (`todoSavedMsg`, so every save flow reaches it; IKE's own writes
are watcher-suppressed) and from `routeWatchEvent` for every file event that
is not repository metadata, a settings file or a directory. A path no spec
language claims, or an excluded one, is dropped without queueing; a removed
file drops out of the snapshot. Nothing in `Update` parses.

**Snapshot.** `Snapshot()` returns an immutable `View{Gen, Files}`: the
measured functions per absolute path, derived from the walk once per content
generation and reused until it moves; a consumer keeps a View as long as it
likes and never holds the walk's internals. `View.Summary(ccnWarn, cogWarn)`
is `Summarize` over every function, `Paths` the sorted file list. `Stats`
reports enabled / unavailable (no syntax tree in the build) / scanning, the
file and function counts, the walk's duration and whether it hit the cap.

**Change notification.** `SetOnChange(fn)` runs off the UI goroutine on a
settle timer (250 ms, re-armed while a walk or a queued re-extraction is
still landing): when the walk's content moved since the last notification,
or the index was dropped or rebuilt, the generation `Gen` moves **once** and
the callback fires with it. The app sends `ComplexityChangedMsg{Gen}` to the
root model, so a save storm or a branch switch arrives as one wake; the
surfaces of the later steps re-read `Snapshot` on it. An enable + disable
inside one settle window collapses into one notification.

**Settings** (`[complexity]`, Settings > Languages > Complexity, user scope):

| Key | Default | Meaning |
|---|---|---|
| `complexity.enabled` | `true` | The master switch: off stops the walk at once and drops the snapshot every complexity surface reads; on restarts the walk without a restart. |
| `complexity.exclude` | `[]` | Glob patterns of files the index never measures, matched against the project-relative slash path (`vendor/**`, `**/*_test.go`, `src/gen/**`) and, for a pattern without a slash, against the base name at any depth (`*_test.go`). The shared `internal/pathglob` vocabulary (`**`, `*`, `?`, `{a,b}`, `[...]`). A malformed pattern is refused in the Settings form and dropped with a diagnostic by `config.Validate`. |
| `complexity.ccn_warn` | `15` | The cyclomatic threshold (#2917): the tool window paints a CCN cell at or above it in the warning colour and `Summary.OverThreshold` counts the function. 1-200; out of range falls back to the default with a diagnostic. |
| `complexity.cognitive_warn` | `15` | The same for cognitive complexity. |

**Telemetry.** One `complexity.scan` op per completed walk (the initial one
and every rebuild), phase `ok` with `ms`, `files`, `functions` and
`truncated`, or phase `error` when the build has no syntax tree at all. Like
`php.trait.index_scan` it never opens a session file on its own (the walk
finishes after every launch); it is held and written once a real usage
event starts the session. No path travels. See
[usage telemetry](./usage-telemetry.md).

## Tool window (#2917)

The **Complexity** tool window (`complexity.toggle`, `cmd+alt+shift+x`,
Tools menu, `internal/complexitypanel`) is the index's first surface: a
singleton bottom-split pane built like the [Usage report](./usage-report.md)
(`pane.KindComplexity`, key `complexity`, context id `complexity`, the usual
`togglePanel` state machine - open at the bottom, focus, return focus - and
every tool-window wiring site: layout persistence, slot numbering, tab
hosting, status line, paste routing, mouse). The chord sits on the shift
layer because `cmd+alt+x` folds onto `ctrl+alt+x`, the xmq playground, off
macOS.

**Tabs.** `Project` lists every function of the index snapshot, `File` the
focused editor's file, `Changes` the diff against a base revision (#2918,
[below](#changes-vs-head-2918)); `tab` / `shift+tab` and a click on the
header's tab bar switch. The pane is a pure consumer: the app seeds it with
`Index.Snapshot()` and `Index.Stats()` when it opens or is restored from a
layout (`wireToolWindow`), re-reads the snapshot on every
`ComplexityChangedMsg`, and on every settled Update pass tells it the
active editor's file and caret line (`syncComplexityPanel`, beside the
Structure pane's follow). On the File tab the innermost function whose
range contains the caret is marked (`▸` prefix and the occurrence
highlight on its name cell); a moved caret is an in-place re-mark, a buffer
switch rebuilds the tab from the snapshot already held. Nothing is parsed
in `Update`.

**Header and columns.** The header line names the pane, draws the tab bar
and the tab's `Summary` over its whole population: the index generation (or
`scanning…` while the walk runs), functions, NLOC, sum / average / max CCN
and cognitive complexity, and the count over threshold. The grid
(`gridview.HeaderRow` / `DataRow`, widths from the longest visible value,
the file column capped at half the pane) has the columns file
(project-relative; hidden on the File tab), function, line (1-based), ccn,
cog, nloc, params, depth. `s` cycles the sort column (default CCN
descending; a numeric column starts descending, a text column ascending),
`S` reverses, a click on a column header sorts by it (again: reverses). A
CCN or cognitive cell at or above `complexity.ccn_warn` /
`complexity.cognitive_warn` renders in the palette's warning colour
(`gridview.Cell.Warn`). The status row shows `filtered/total rows` and the
key hints.

**Filter.** The filter row is the shared `filterbar` over a `filterexpr`
schema (`/`, the find chord `cmd+f` / `ctrl+f`; `cmd+g` / `cmd+shift+g`
step the cursor through the filtered rows like the TODO index): `file:`
(substring or glob over the project-relative path), `lang:` (language id),
`fn:` (function name substring), `min:` (keeps rows at or above n on the
sort column, CCN when the sort column is text), `metric:ccn|cog` (sets the
sort column, and with it the column `min:` reads); bare words fuzzy-match
the function names. `file:internal/app min:20` is "functions with CCN >= 20
under internal/app"; adding `metric:cog` switches both to cognitive
complexity.

**Actions.** `enter` / a double click sends `OpenLocationMsg{Path, Line}`
and the app opens the file through `openPathAt`, so the caret lands on the
function header and `nav.back` returns. `r` (and the palette's
`complexity.refresh`) rebuilds the index (`Index.Rebuild`; a disabled index
or a build without a syntax tree is reported instead). `e` (and
`complexity.export`) writes the current tab as a CSV scratch
(`ExportMsg{Label, CSV}`: header `file,function,kind,line,ccn,cognitive,nloc,params,depth`,
one row per visible function, the file column on every tab) and opens it.
`y` / the copy chord puts `path:line: name ccn=.. cog=..` on the clipboard
(and the clipboard history). The wheel scrolls, a click selects. The
single keys are the pane's own (keybind audit ledger, `reasonPaneKey`).

**Settings** (`complexity.ccn_warn`, `complexity.cognitive_warn`, Settings >
Complexity, Int 1-200, default 15) apply live through the same config
reload that reconfigures the index.

## Changes vs HEAD (#2918)

The third tab, **Changes**, answers the epic's question: it measures the
**base revision** of every file that differs from it and diffs that against
the working tree. The pure half is `internal/complexity/diff.go`, the git
and app half `internal/app/complexity_diff.go`, the rendering
`internal/complexitypanel/changes.go`.

**Diff engine (pure).** `DiffFunctions(base, head)` pairs the units of one
file by `(Name, ordinal among same-named units)` — the k-th `handle` of the
base meets the k-th `handle` of the head, so overloads, nested closures
(`(anonymous)#2`) and repeated local helpers line up in source order — and
classifies every pair whose five metrics (CCN, cognitive, NLOC, params,
depth) differ as `Changed` (before / after / Δ), an unpaired head unit as
`Added`, an unpaired base unit as `Removed`. A unit that only moved is
unchanged; a renamed function is removed + added (the engine has no body
hash to tell a rename from a rewrite). `Changed` is sorted by |ΔCCN|
descending. `DiffProject(base, head, ccnWarn, cogWarn)` runs it per path
over two `map[path][]Function`, keeps the files with a non-empty
classification, summarises both sides (`Summarize`) and renders the
**verdict**: `more complex` when ΣCCN rose, `less complex` when it fell,
Σcognitive breaking the tie, `unchanged` otherwise.

**Base revision and file list.** `vcs.ResolveRev(root, rev)` turns the
typed base — `HEAD` by default; any `git rev-parse`-able ref: a branch, a
tag, a sha, `HEAD~3` — into its commit sha (git's own message for a ref
that does not exist); `vcs.ChangedFiles(root, sha)` lists the tracked files
whose working tree differs from it (`git diff --name-status -M`, staged and
unstaged edits alike, renames with their old path), and the status
snapshot's untracked entries join as all-added files. Comparing against
`main` therefore includes the branch's commits, not only the uncommitted
edits. The status snapshot's rename records keep their origin
(`FileEntry.From`) for the same reason. Both helpers run inside a
`tea.Cmd`; see [VCS](./vcs.md).

**App wiring** (`complexity_diff.go`). The diff is **pulled**, not pushed:
once per settled Update pass, while the Changes tab is on screen, the model
compares the key of the diff the pane shows — `(VCS snapshot generation,
complexity index generation, base revision)` — with the current one and,
when they differ, arms a 300 ms debounce tick (once). The snapshot
generation moves in `applyVCSSnapshot` for every snapshot that differs from
the last (an `Equal` refresh moves nothing), the index generation with every
`ComplexityChangedMsg` (a saved file re-measured), the base with `R`. A
hidden tab costs nothing; one computation runs at a time; a result whose
inputs moved meanwhile is followed by a fresh run. The tick gathers the
inputs in `Update` — the snapshot, the index `View`, the text of every dirty
buffer, `complexity.exclude`, the warning thresholds — and launches one
`tea.Cmd` that resolves the base, lists the files, and measures both sides
of each with four workers: the base side by `git show <sha>:<path>` +
`Measure` (every git call under the package's 5 s timeout; a blob the base
lacks measures as nothing), the head side from the **dirty buffer** when the
file is open and unsaved (its rows carry the editor tabs' `*`), else from
the index snapshot, else from disk. Measured base blobs are cached per
`(sha, path)` for the session, so the next run against the same base skips
their git calls; unchanged files are never measured — only the files git
lists are. Files outside the project root (the project may be a
subdirectory of the repository), of a language without a spec, or matched
by `complexity.exclude` are skipped. A deleted file is all removed, an added
or untracked one all added, a renamed one removed under its old path and
added under the new one with a one-line note in the header. Outside a
repository the tab says `not a git repository` and nothing runs. Nothing in
`Update` runs git or parses.

**Changes tab.** The header reads `base <rev> → working tree · functions
n→m · ΣCCN a→b (Δ) · Σcog c→d (Δ) · <verdict>` plus the rename note
(`computing…` while a run is in flight). The grid keeps its sections —
changed (|ΔCCN| descending), added, removed — with the section named on its
first row, and the columns change, function, file, line, `ccn
before→after (Δ)`, `cog before→after (Δ)`, `nloc before→after` (the missing
side of an added / removed unit is `-`). The same filter row applies —
`file:`, `fn:`, `lang:`, `metric:`, and `min:` on |Δ| of the sort column —
and `s` / `S` / the header click sort inside the sections. `enter` opens the
function (a removed one opens its file at the first line), `e` exports the
tab as CSV (`change,file,function,kind,line,ccn_before,ccn_after,ccn_delta,
cog_before,cog_after,cog_delta,nloc_before,nloc_after`), `y` copies
`path:line: name changed ccn=a→b (Δ) cog=c→d (Δ)`. **`R`** prompts the base
revision in the filter row's place (`ui.Field`, seeded with the current
base, `enter` applies, `esc` cancels); an invalid ref keeps the previous
base and shows git's message in the header. The base revision is session
state, not a setting.

## Problems feed (#2920)

`internal/app/complexity_problems.go` publishes every over-threshold
function into the [Problems tool window](./problems.md) through the store's
task-source channel (`Store.SetTaskSource("complexity", byPath)`), the same
seam the dependency scanner uses. On every `ComplexityChangedMsg` and every
config reload `feedComplexityProblems` reads the index snapshot and the
pure builder `complexityDiagnostics(view, ccnWarn, cogWarn)` emits one
warning per function at or above a threshold, anchored at its header line:
`function <name>: CCN 40 ≥ 15`, `function <name>: cognitive 25 ≥ 15`, or
both joined by a comma when both are over; `Source` is `complexity`,
`Code` the first exceeded metric (`ccn` before `cognitive`). Each publish
replaces the source wholesale, so a raised threshold or a simplified
function drops its row and a lowered threshold adds rows without a restart.
`complexity.problems = false` or `complexity.enabled = false` calls
`ClearTaskSource("complexity")`, leaving LSP, lint and other task findings
untouched. Rows navigate like any diagnostic and filter with
`source:complexity`; the task channel is read only by the pane, so the
editor paints no gutter mark or underline for them.

| Key | Default | Meaning |
|---|---|---|
| `complexity.problems` | `true` | Publish over-threshold functions into Problems (Settings > Complexity). |

## Status segment (#2921)

`internal/app/complexity_segment.go` renders the opt-in `complexity`
status slot: `Δcx +4` / `Δcx -2` / `Δcx ±0`, the `DeltaCCN()` of the diff
the Changes tab stores, tinted after layout (foreground only) in the
warning, success or muted (inlay-hint) colour. It is hidden while the
setting or `complexity.enabled` is off, outside git and while the diff has
no changed files. The segment is a pure consumer; `complexityDiffWanted`
— the Changes tab on screen **or** the segment on — is the seam that keeps
the debounced diff armed on VCS refreshes and re-measures with the window
closed, so a save shows up within one refresh. A click toggles the
Complexity window and selects its Changes tab.

| Key | Default | Meaning |
|---|---|---|
| `complexity.status_segment` | `false` | Show the `Δcx` status segment (Settings > Complexity). |

## Editor surface (#2919)

`internal/app/complexity_hints.go` feeds the editor's inline complexity
hints — `CCN 12 · cog 8` as trailing virtual text on every function header
(editor side: `internal/editor/complexityhints.go`, see
[editor](./editor.md)). It is a third per-line annotation store beside code
lenses and inline debugger values, fed per open document and routed like
`vcs.MarksMsg` through `routeToEditor` to every view of the path.

**Sync.** `complexityHintsSyncCmd` runs on the settled pass of every
`Update`, the Structure pane's pattern (#1025): every open document of a
language with a complexity spec (not excluded, not a degraded large file)
wants hints for one state key `cxHintKey{version, gen, ccn, cog, enabled}`
— the buffer's `DocVersion`, the index generation the hints came from, the
two warning thresholds and the master switch. The key last delivered (or
launched) per path is remembered in `sent`, the key a debounce is armed for
in `pending`; a document whose wanted key matches either costs nothing. A
**clean buffer the index holds** is served on the spot from the snapshot —
no parse, so opening a file, a finished walk or a `ComplexityChangedMsg`
shows hints in the same pass. A **dirty buffer** (or one the walk has not
reached) arms the 250 ms `cxHintDebounce` tick; a typing burst re-arms with
a newer key and every older tick is dropped at fire time
(`complexityHintTick`). The surviving tick takes the buffer text and runs
`complexity.Measure` off-loop (`cxMeasureText`, shared with the Changes
tab); the result returns as `complexityHintMeasuredMsg`, stamped with the
version it was taken at, and is routed only while it is still the sent key.
The editor drops a push whose version moved meanwhile; the next settled pass
re-arms. Closed documents are forgotten so a reopen starts fresh.

**Hints.** `complexityHintsFor(fns, ccnWarn, cogWarn)` emits one
`editor.ComplexityHint{Line, Text, Warn}` per function at its header line
(the first unit on a line wins, so a lambda beside its header does not
replace it); `Warn` when CCN or cognitive reaches its threshold, which paints
the hint in the warning tone. A threshold change on reload changes the key,
so every open buffer repaints. `complexity.enabled = false` sends one
clearing push per buffer that holds hints; `editor.marks.complexity = false`
hides them in the editor without a push (Settings > Editor > Diagnostics,
next to the other mark toggles).

| Key | Default | Meaning |
|---|---|---|
| `editor.marks.complexity` | `true` | Render the inline complexity hints on function headers. |

## Headless CLI (#2922)

`ike complexity [--diff[=REV]] [--json] [--min N] [--ccn N] [--cog N] [DIR]`
is the terminal-free report for CI pipelines and coding agents. The grammar
lives in `internal/cli` (`ComplexityArgs`, first argument only like
`agent-hook`, so a file named `complexity` still opens as `./complexity`);
`cmd/ike` dispatches before any terminal setup to
`internal/complexitycli.Run`, which returns the exit code: **0** for a
report, **2** for a usage error, a directory that does not exist, a git
error (an unknown `--diff` ref prints git's message on stderr) or a build
without syntax trees. The report never fails on thresholds — gating is the
caller's policy over the JSON.

**One walk.** The project report walks `DIR` with
`complexity.WalkProject`, which runs on `langindex.WalkFiles` — the same
directory skip rules the index's `langindex` scan uses — and classifies
every file with the index's own `classify` (language with a spec, no
`complexity.exclude` glob), so the CLI and the tool window can never
disagree about which files count. `complexity.ReadSource` applies the
index's 512 KiB cap. Files a language claims but no spec measures are
counted per language in a final `skipped:` line (and the JSON `skipped`
object) rather than dropped silently; excluded files and files no language
claims are not counted.

**Settings.** `config.Load(config.Discover(DIR))` reads the user layer and
`DIR/.ike/settings.toml`: `complexity.exclude` and the warning thresholds
(15 without config). `--ccn` / `--cog` override the thresholds. The row
filter: `--min N` keeps rows with CCN or cognitive ≥ N; an explicit `--ccn`
/ `--cog` alone keeps rows at or above either threshold; no filter flag
keeps every row. The summary always covers every function.
`complexity.enabled` is ignored — running the command is the request.

**Diff.** `--diff[=REV]` (default `HEAD`) runs synchronously the same git
plumbing as the Changes tab — `vcs.ResolveRev`, `vcs.ChangedFiles`, the
status snapshot's untracked files (a collapsed untracked directory is walked)
and `vcs.RevContent` per base blob — restricted to files under `DIR` the
index would measure, then `complexity.DiffProject`. The head side is read
uncapped like the tab's, so a file over the walk's cap does not read as
all-removed. The text report prints `changed` (before→after with Δ per
metric), `added` and `removed` sections, the totals and
`verdict: MORE COMPLEX (ΣCCN +4, Σcog +2)`.

**JSON.** Field names are snake_case and stable (documented in
`userdocs/guides/complexity.md`; add, never rename): the project report is
`{summary, functions, skipped}`, the diff `{rev, sha, base, head, delta,
changed, added, removed, verdict, skipped}` with `verdict` one of
`more_complex`, `less_complex`, `unchanged`. Line numbers are 1-based.

**No cgo.** Without cgo there are no grammars; `complexity.Available()`
(shared with the index's `Unavailable` stat) is false and the command
prints a clear error and exits 2.

## Extending

A new language declares its `lang.ComplexitySpec` (see
[languages](./languages.md#complexity-spec-2914) and the
[change workflow](/process/change-workflow.md)) and gets a fixture directory
here: `testdata/<lang>/fixture.<ext>`, an entry in `fixtureLang`, a golden
generated with `-update` and reviewed against the language's reference tool,
with any deliberate difference recorded in `golden_test.go`.
