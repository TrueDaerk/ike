---
type: concept
title: Code Complexity
description: The code-complexity engine (Roadmap 0550, #2913), cyclomatic (McCabe) and cognitive (Sonar) complexity, NLOC, parameters, nesting depth and comment density (#2934) per function, measured on the Tree-sitter parse through each language's ComplexitySpec; the rules, the walker, the per-language golden fixtures cross-checked against lizard, gocyclo, gocognit and complexipy, the summary with its per-class and per-file groups (#2937), the project index (#2916) that measures every supported file in the background, keeps it current from saves and watcher events, and hands consumers immutable snapshots, the Complexity tool window (#2917) and its Changes tab (#2918) diffing the working tree's functions against a base revision with a verdict, the Problems feed (#2920) publishing over-threshold functions under the complexity source, the hotspot columns (#2957) multiplying complexity by the commits per file from one git log, and the headless `ike complexity` CLI (#2922) printing the table of the project or of named files and directories (#2936), per function or summed per class or file (#2937), or the diff against a revision as text or JSON for CI and agents, also as a complexity-only build of the ike binary (#2939, build tag ike_complexity_only, make install-complexity) that links no IDE; configured by the [complexity] section (Settings > Complexity) with a complexity.scan telemetry op.
resource: internal/complexity, internal/complexitypanel, internal/app/complexityindex.go, internal/app/complexity_diff.go, internal/app/complexity_churn.go, internal/vcs/vcscore/churn.go, internal/app/complexity_problems.go, internal/app/complexity_segment.go, internal/complexitycli, cmd/ike/complexityonly.go
tags: [architecture, complexity, metrics, tree-sitter, languages, cyclomatic, cognitive, index, settings, telemetry, vcs, diff, problems, cli]
timestamp: 2026-10-13T23:59:30Z
---

# Code Complexity

Roadmap 0550 measures the **cyclomatic** (McCabe, CCN) and **cognitive**
(Sonar) complexity of every function in the project, shows it in a tool
window, and answers *"did this change make the project more or less
complex?"* by comparing the working tree against `HEAD`. Everything runs on
the Tree-sitter parse IKE already has — no lizard, gocyclo or gocognit on the
machine — so it works for every language with a grammar, in mixed projects,
out of the box. This page is the single concept doc for the whole stream: the language
spec and its audit ledger (#2914), the **engine** (`internal/complexity`,
#2915), the **project index** (#2916), the tool window (#2917), the diff
against a base revision (#2918), the editor hints (#2919), the Problems
feed (#2920), the status segment (#2921), the headless CLI (#2922), the
settings and the limitations. The user-facing guide is
`userdocs/guides/complexity.md`.

## Language spec and audit ledger (#2914)

Each language plugin declares `lang.Language.Complexity *lang.ComplexitySpec`
next to `ScopeNodes` / `FoldNodes`; `lang` stays a leaf package, so the spec
holds node-kind names and operator tokens only: `FunctionNodes` (kind → name
field, `""` = anonymous), `ParamsField`, `Branch`, `Else`, `Nesting`,
`BoolOps`, `Jumps`, plus the naming helpers `Selector`, `Arms`, `Calls` and
`MethodSep`. Details and the per-language specs:
[languages](./languages.md#complexity-spec-2914).

The audit ledger `cmd/ike/complexity_audit_test.go` makes every registered
language either declare a spec, verified against a probe parse, or carry a
row with a reason from a closed set — `reasonNoFunctions` (data formats:
JSON, YAML, TOML, CSV, INI, dotenv, Markdown, SQL, …), `reasonNoGrammar`
(no Tree-sitter grammar) or `reasonGap` with a tracking issue. A new
language fails the build until its row is filled in.

## Model

`Measure(langID, lines)` parses the buffer with the language's grammar
(`highlight.SyntaxTree`), walks the snapshot with the language's
`lang.ComplexitySpec` and returns one record per **unit** in source order:

```go
type Function struct {
    Name      string // "renderWeek", "Server.handle", "Class::method", "(anonymous)#3"
    Class     string // the method's qualifier, "Server"; "" for free functions and lambdas (#2937)
    Kind      string // function | method | lambda
    Line, EndLine int // 0-based header / last line
    NLOC      int    // non-blank, non-comment lines in the range
    Params    int
    CCN       int
    Cognitive int
    Depth     int    // deepest nesting level reached inside the unit
    CommentLines   int     // comment-only lines in the range (#2934)
    CommentDensity float64 // CommentLines / (NLOC + CommentLines), 0 when both are 0
    Sites          []Site  // every CCN / cognitive increment, in source order (#2946)
}

type Site struct {
    Line, Col int    // 0-based position of the node's first token (a boolean run: its first operator)
    EndLine   int    // last line of the node
    Kind      string // "if", "else if", "elif", "for", "switch", "case", "catch", "?:", "&&", "and", "goto", "recursion", …
    CCN       int    // 1 for a branch or an arm, the operator count of a boolean run, else 0
    Cognitive int    // the whole cognitive increment
    Nesting   int    // the part of Cognitive that is the nesting penalty; 0 for a flat increment
    Level     int    // the nesting level the site sits at
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
`Class` (#2937) carries the qualifier on its own — `Server` for
`Server.Handle` and `Server::handle`, the trait for a PHP trait method — so
consumers never parse the name; it is `""` for a free function, a lambda, a
function nested in a method and a method kind without an enclosing class.
`TestGoldenClasses` pins it per fixture (Go receiver, Python class, PHP class
and trait, TypeScript / JavaScript class).

**Sites** (#2946) are the unit's increments: one record per branch,
selector, arm, else, boolean run, jump and recursive call, in source order,
so that `1 + Σ Sites.CCN == CCN` and `Σ Sites.Cognitive == Cognitive` for
every unit (`TestSitesSumToTotals` asserts it over every fixture language).
`Kind` is the node kind as the language spells it — the leading keyword
when it is one (`if`, `elif`, `elseif`, `foreach`, `until`, `except`,
`match`, `select`, `goto`), `else if` for a Go or TypeScript `if` in the
else position, `?:` for a conditional expression, `case` for an arm the
keyword does not lead (shell, PHP `match`), the operator of a boolean run
(`&&`, `and`, `??`) and `recursion` for a recursive call. A nested unit's
structures are the nested unit's own sites (with their CCN); the enclosing
unit, whose cognitive total includes them at its own nesting (Sonar:
lambdas are part of the method they sit in), records them too, CCN-less,
so its sum still holds. Recording is unconditional — `Measure` keeps its
signature and `BenchmarkMeasure` stays well inside the 50 ms budget (about
29 ms for the 5 000-line file on the dev machine).

**Params** counts the named children of the `ParamsField` node, a child
declaring several names (Go's `a, b int`) once per name. **NLOC** counts the
lines of the unit's range that are neither blank nor comment-only, by the
language's `LineComment` / `BlockComment` markers; a line with code before a
trailing comment counts, a block comment hides lines up to its close. The
range is the whole unit, nested units included (lizard subtracts them).

**Comment density** (#2934). The same pass (`nloc()`) also counts the
**comment-only** lines of the range: a line that is blank once its comments
are stripped but was not blank before. A line with code and a trailing
comment is code (so the closing-brace line of `/* … */ }` is code); a block
comment spanning lines makes every covered line a comment line. The string
literal caveat of NLOC applies. `CommentDensity = CommentLines / (NLOC +
CommentLines)` — a ratio (0.55), rendered as a whole percent in the UI and
the CLI (`DensityPercent`, `Function.CommentsOver(warn)`). Only the **body**
counts: a doc comment above the header (Go doc comment, PHPDoc, JSDoc) lies
outside the unit's range, and a Python docstring is a string statement, not
a comment — neither raises the density. Cyclomatic and cognitive complexity
never see comments, and NLOC subtracts them, so a function that is mostly
comment between its statements looked simpler than it reads; the density
makes that visible. Neither lizard nor gocognit measure it (Sonar only per
file). The golden fixture of every language carries a `documented` unit (doc
comment above the header, density 0) and a `commentHeavy` / `comment_heavy`
unit over 40 %, with a block comment running into the last line where the
language has block comments.

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

- **Branch.** Every Branch records a site of its keyword (`if`, `for`,
  `while`, `catch`, `?:`) with CCN 1 and `Cognitive = 1 + Level`,
  `Nesting = Level`.
- **Else-if is flat.** A Branch in the else position — the `alternative`
  field of a Branch (of another kind, or of the same kind only where the
  language has no `Else` kinds: Go's `else if`), the single Branch child of an
  `Else` kind (TS/PHP `else if`), or shell's unfielded `elif_clause` after the
  body — counts +1 without the nesting penalty; its body nests one level like
  the `if` body. A chained ternary in TS or PHP is the parent's own kind, so
  it nests (+2) as Sonar does. The site is flat: kind `else if` (or the
  language's `elif` / `elseif`), CCN 1, Cognitive 1, Nesting 0. An `else`
  records a CCN-less flat site of kind `else`.
- **Headers do not nest.** Children in a Branch's or Selector's header fields
  (`condition`, `initializer`, `update`, `left`, `right`, `value`, `subject`,
  `parameter`, …) are walked at the node's own level: a ternary in an `if`
  condition costs 1 + the `if`'s level, not one more.
- **Switch counts once.** The Selector takes `+1 + level` and nests every arm
  once (Go's `default_case`, PHP's `default_statement` and TS's
  `switch_default` are not arms but sit inside the Selector, so an `if` in a
  default arm is at level + 1 — gocognit's number). Arms carry only CCN.
  The Selector records one site (`switch`, `select`, `match`, CCN 0,
  `1 + Level`), every arm a CCN-only site of kind `case`.
- **Boolean runs.** A boolean-operator tree is flattened in source order
  through `parenthesized_expression` nodes and stops at any other operand:
  `a && b && c` is one run, `a && b || c` two, `a && (b || c) && d` three
  (gocognit 1.3 and SonarJS, whose ESTree has no parenthesis node; complexipy
  restarts the run at a parenthesis), `a && f(b || c)` two (the call is a new
  expression). Every operator of the tree is consumed once. Each run is
  one site at its first operator, of the operator's kind, CCN the run's
  operator count, Cognitive 1: `a && b || c` is two sites, `a && b && c`
  one of CCN 2.
- **Recursion** compares the callee's last identifier segment with the unit's
  short name (`s.handle()`, `$this->handle()`, `self::m()` count; gocognit
  and complexipy count plain-identifier calls only), per call site, and
  through closures: a closure calling its enclosing `f` adds to `f`. Each
  call is a site of kind `recursion` (Cognitive 1) at the call node, in
  every unit it targets. A Jump with a target records a flat site of its
  keyword (`goto`, `break`, `continue`).
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

`Summarize(fns, ccnWarn, cogWarn, cmtWarn)` aggregates a list — a file or a
project — into functions, NLOC, Σ / avg / max CCN, Σ / avg / max cognitive,
the summed `CommentLines` with the aggregate `CommentDensity`, and the
number of functions **at or above** any warning threshold — CCN, cognitive,
or a comment density of `cmtWarn` percent (a non-positive threshold disables
that check). `Excess` (#2945) is the mass **above** the CCN and cognitive
thresholds: `Function.Excess(ccnWarn, cogWarn)` = `max(0, CCN − ccnWarn) +
max(0, cognitive − cogWarn)` (a unit at a threshold counts as over it but
has no excess; a non-positive threshold drops its term), summed over the
population. The over count says how many units cross a line, the excess how
far — the primary signal for optimisation and what the diff verdict reads
first. The thresholds stay parameters so a per-file threshold can feed
them. The thresholds come from the `[complexity]`
settings of the later steps; the engine takes them as plain integers.

**Groups (#2937).** `GroupFunctions(files, by, ccnWarn, cogWarn, cmtWarn)`
(`group.go`) folds functions keyed by file into one `Group{File, Class,
Line, Summary}` per class (`ByClass`: the units with a non-empty
`Function.Class`, keyed by file and class — Go methods of one type in two
files are two groups) or per file (`ByFile`: every unit). Each group is
`Summarize` over its direct units, so the sums of the file groups equal the
flat list's: a nested lambda is its own unit, counted in its file's group and
in no class's, exactly as the flat list counts it — nothing twice. `Line` is
the group's earliest unit (where "open" lands). The order is stable: CCN sum
descending, then file, then class. `Level` (`ByNone` / `ByClass` / `ByFile`,
`ParseLevel`, `String` = `""` / `class` / `file`) is the shared spelling of
the CLI flag and the pane's persisted mode.

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
| `complexity.comment_density_warn` | `40` | The comment-density threshold in percent (#2934): 0-100, `0` turns the check off; out of range falls back to 40 with a diagnostic. |

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
and cognitive complexity, the aggregate comment density (`cmt 12%`), the
count over threshold and `excess N` (#2945). The grid
(`gridview.HeaderRow` / `DataRow`, widths from the longest visible value,
the file column capped at half the pane) has the columns file
(project-relative; hidden on the File tab), function, line (1-based), ccn,
cog, nloc, params, depth, `cmt%` (the comment density in whole percent,
#2934). `s` cycles the sort column (default CCN
descending; a numeric column starts descending, a text column ascending),
`S` reverses, a click on a column header sorts by it (again: reverses). A
CCN, cognitive or `cmt%` cell at or above `complexity.ccn_warn` /
`complexity.cognitive_warn` / `complexity.comment_density_warn` (unless 0)
renders in the palette's warning colour
(`gridview.Cell.Warn`). The status row shows `filtered/total rows` and the
key hints.

**Filter.** The filter row is the shared `filterbar` over a `filterexpr`
schema (`/`, the find chord `cmd+f` / `ctrl+f`; `cmd+g` / `cmd+shift+g`
step the cursor through the filtered rows like the TODO index): `file:`
(substring or glob over the project-relative path), `lang:` (language id),
`fn:` (function name substring), `min:` (keeps rows at or above n on the
sort column, CCN when the sort column is text), `metric:ccn|cog|cmt|hot|commits` (sets
the sort column, and with it the column `min:` reads; `hot` / `commits` are
the [hotspot](#hotspots-2957) columns) — `metric:cmt min:40`
keeps bodies that are at least 40 % comment); bare words fuzzy-match
the function names. `file:internal/app min:20` is "functions with CCN >= 20
under internal/app"; adding `metric:cog` switches both to cognitive
complexity. `test:no` hides the test files, `test:yes` keeps only them
(#2950, `Row.Test`). `max:` is the at-most counterpart of `min:` on the
same column (`min:15 max:30` is the closed range); `kind:function|method|lambda`
matches `Function.Kind` exactly (case-insensitive; an unknown kind matches
nothing), `class:` is a substring of `Function.Class`, and `depth:`,
`params:`, `nloc:` keep rows at or above n on that attribute (#2952).

**Hide tests (#2950).** `t` — the `complexity.toggleTests` command, bound to
`t` in the `complexity` keymap context and in the palette as "Complexity:
Toggle Test Files" — hides the test files on every tab at once; the footer
hint reads `t tests: hidden`. A test file is what
`complexity.IsTestFile(root, path)` says: the language's
`lang.TestSpec.FilePattern` matches the base name (Go `_test.go`, Python
`test_*.py` / `*_test.py`, PHP `*Test.php`), else the base name matches
`*.spec.*`, `*.test.*`, `*_test.sh` or `*.bats`, else a directory of the
project-relative path is `__tests__`, `tests`, `test` or `spec` (at any
depth; `testdata` is not one). The toggle drops the rows before the totals
are taken, so the header (and the Changes header and verdict, via
`Diff.Without`, which re-runs `DiffProject` over the kept files) follows the
visible population; `test:` and the toggle combine (both narrow). The state
persists in the layout `Mode` beside the grouping
(`project:file,file:class,tests:hidden`; `Mode` / `SetMode`, unknown tokens
ignored) and the command saves the layout at once.

**Grouping (#2937).** `b` — the `complexity.group` command, bound to `b`
in the `complexity` keymap context and in the palette as "Complexity: Cycle
Grouping" — cycles the Project tab none → class → file and the File tab
none → class (the Changes tab does not group; the command says so). A
grouped tab replaces the function rows with one row per
`complexity.GroupFunctions` group: columns file (Project only), class (class
level only), funcs, nloc, `Σccn`, ccn avg, ccn max, `Σcog`, cog avg, cog max,
`cmt%` (aggregate density), over and excess (#2945); the header totals stay the whole tab's.
Sorting (`s` walks the group columns, `S`, header clicks) and the filter
work on the group columns — `fn:` and bare words read the class (class
level) or the path (file level), `min:` / `max:` the sort column, `class:`
the group's class, `nloc:` the NLOC sum, `depth:` / `params:` the maximum
over the group's functions (`Group.DepthMax` / `ParamsMax`); `kind:` matches
no group row; a sort column the
group layout lacks resets to CCN descending. Max CCN / max cog / `cmt%` and
a non-zero over or excess count take the warning colour. `enter` opens the group's
earliest unit, `y` copies `path: Class funcs=2 ccn=9 (max 8) cog=12 (max
12)`, `e` exports `file,class,functions,nloc,ccn_sum,ccn_avg,ccn_max,cog_sum,cog_avg,cog_max,comment_lines,cmt_pct,over_threshold,excess`.
Each tab keeps its own level. The levels persist in the layout as the
pane identity's `Mode` (`project:file,file:class`; `GroupMode` /
`SetGroupMode`, an unknown spelling ignored) — session layout, named
layouts and hosted tabs alike — and the command saves the layout at once.
A letter typed into the open filter row stays text: the app routes a
printable, unmodified key to a focused one-line input before the keymap
layer.

**Editor marks (#2947).** `m` on a function row (or a site row: its
parent) emits `ShowSitesMsg{Path, Line}` — the one-shot site marks in the
editor, see [Editor surface](#editor-surface-2919); the footer names it as
`m marks`.

**Breakdown (#2946, `sites.go`).** `space` — the `complexity.sites`
command, bound to `space` in the `complexity` keymap context and in the
palette as "Complexity: Expand / Collapse Sites" — expands the selected
function row of an ungrouped Project or File tab (and the Changes tab,
where it shows the *after* side; a removed function has nothing to expand)
in place into one indented child row per `Function.Site`: the function
column reads `  if +3 (nesting 2)` (`  case` for a CCN-only arm), the line
column the site's line, `ccn` / `cog` the site's increments, the other
columns blank. The children are sorted by cognitive cost descending, then
line, and are spliced under their parent *after* the filter and the sort
have placed the function rows (`Refresh` → `sortRows` → `spliceSites`), so
they stay glued to it whatever the sort column; a filter that drops the
parent drops them. One function is expanded at a time; `space` on the
parent or on one of its site rows collapses it (the cursor returns to the
parent); a row with nothing to expand (a group row, a removed function, a
function without sites) leaves the list alone and the command says so. The
expansion is remembered by file, function name and Changes section
(`Row.expandKey`), not by line, so a snapshot refresh — the index
re-measured a file, the base revision changed — rebuilds the rows with the
same function open and the cursor on the same site (`Row.key` →
`siteKey`). `Row.Site` / `Row.Parent` mark a site row and its parent's
index; the totals, the row counter's denominator, the caret mark, the CSV
export and the group rows ignore site rows. On a site row `enter` / a
double click opens the file at the site's line and `y` copies
`path:line: kind +cog (nesting n)`.

**Actions.** `enter` / a double click sends `OpenLocationMsg{Path, Line}`
and the app opens the file through `openPathAt`, so the caret lands on the
function header and `nav.back` returns. `r` (and the palette's
`complexity.refresh`) rebuilds the index (`Index.Rebuild`; a disabled index
or a build without a syntax tree is reported instead). `e` (and
`complexity.export`) writes the current tab as a CSV scratch
(`ExportMsg{Label, CSV}`: header `file,function,kind,line,ccn,cognitive,nloc,params,depth,comment_lines,cmt_pct`,
one row per visible function, the file column on every tab) and opens it.
`y` / the copy chord puts `path:line: name ccn=.. cog=..` on the clipboard
(and the clipboard history). The wheel scrolls, a click selects. The
single keys are the pane's own (keybind audit ledger, `reasonPaneKey`).

**Settings** (`complexity.ccn_warn`, `complexity.cognitive_warn`, Settings >
Complexity, Int 1-200, default 15; `complexity.comment_density_warn`, Int
0-100, default 40) apply live through the same config
reload that reconfigures the index.

## Hotspots (#2957)

Complexity alone ranks badly: a 170-point function nobody touched in a year
costs less than a 40-point one edited every week. The hotspot columns
multiply complexity by change frequency (Adam Tornhill's hotspot analysis);
git has the frequency for free.

**Churn.** `vcscore.Churn(root, since)` (`internal/vcs/vcscore/churn.go`)
counts the commits per path in one call for the whole tree:
`git log -z --format= --name-only --relative --since=<RFC 3339> -- .` run
in `root`, so the keys are slash paths relative to `root` — the project root
the panel's `Row.Rel` and the CLI's output paths use, even when the project
is a subdirectory of the repository. Renames are **not followed**: `git log
--name-only` names each path as of its commit, so a renamed file's history
splits between its old and new name (`--follow` works per path only and
would cost one git call per file). Merge commits list no files. The call
carries the package's 5 s timeout; `vcs.ChurnCmd` wraps it in a `tea.Cmd`
returning `vcs.ChurnMsg{Root, Since, Counts, Err}` like `LogCmd`.

**Fetch (`internal/app/complexity_churn.go`).** Pulled like the Changes
diff: on every settled Update pass, while the Complexity window is open,
`complexityChurnSyncCmd` compares the key of the cached counts —
(VCS snapshot generation `cxDiff.snapGen`, `complexity.churn_days`, index
root) — with the current one and launches one `vcs.ChurnCmd` when they
differ (one run at a time, `complexityChurnState.fetching`). A distinct VCS
snapshot or a changed window therefore re-fetches once, a closed window
costs nothing, and reopening the window on the same generation re-seeds the
pane from the cache (`seedComplexityChurn` in `seedComplexityPanel`)
without git. `applyComplexityChurn` stores the result under the launched
key; a git error stores `nil` under that key, so it is not retried every
pass. Outside a repository (`m.vcs.snap == nil`) the pane gets `nil`.

**Panel (`internal/complexitypanel/churn.go`).** `SetChurn(counts, days)`;
`nil` hides the columns, an empty map (no commits in the window) shows
zeros. Inside a repository the Project / File function layouts gain
`commits` (the row's file's count, `Row.Commits`) and the file-group layout
`commits` and `hot` = commits × Σexcess, falling back to commits × ΣCCN
when the file exceeds nothing (`complexity.Hot`, `Row.Hot`); class groups
and the Changes tab carry neither. `metric:hot` / `metric:commits` sort by
them and `s` cycles through them with the other columns; on the function
list `metric:hot` sorts by the function's own `Hot(commits, excess or CCN)`
without showing the column. The `hot` cell takes the warning colour on the
top decile of the visible rows (`markHot`: the ⌈n/10⌉-th highest score, at
least one row, nothing while every score is 0). The footer shows
`churn 90d`; the CSV export appends `commits` (functions) or
`commits,hot` (file groups). A sort column the layout loses when the
repository goes away resets to CCN. The directory tree view (#2954) is where
the same two columns land on directory and file nodes once it exists.

**Setting.** `complexity.churn_days` (Settings > Complexity, Int 1-3650,
default 90) is the window; a change applies live through the key.

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
classifies every pair whose metrics (CCN, cognitive, NLOC, params, depth,
comment lines) differ as `Changed` (before / after / Δ), an unpaired head unit as
`Added`, an unpaired base unit as `Removed`. A unit that only moved is
unchanged; a renamed function is removed + added (the engine has no body
hash to tell a rename from a rewrite). `Changed` is sorted by |ΔCCN|
descending. `DiffProject(base, head, ccnWarn, cogWarn, cmtWarn)` runs it per path
over two `map[path][]Function`, keeps the files with a non-empty
classification, summarises both sides (`Summarize`) and renders the
**verdict** (#2945): the first non-zero of Δexcess, Δ Σcognitive and
Δ decision points (ΣCCN minus the function count, `DeltaDecisions`) decides
— positive `more complex`, negative `less complex`, all three 0
`unchanged`. Until #2945 ΣCCN decided; but CCN gives every unit a base 1,
so extracting a nested block into its own function (the most common
simplification: one 20 / 30 function into 12 / 14 + 9 / 6) raised ΣCCN by
one and read *more complex* while the excess fell 20 → 0, and a trivial
helper read *more complex* although nothing crossed a threshold. Reading the
decision points rather than raw ΣCCN keeps that base 1 out of the last
tiebreak, so the helper reads `unchanged`. `Diff.DeltaExcess()` is
`Head.Excess − Base.Excess`; `DeltaCCN()` is unchanged.

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
once per settled Update pass, while the Changes tab is on screen (or
another consumer wants it, see `complexityDiffWanted` below), the model
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
n→m · ΣCCN a→b (Δ) · Σcog c→d (Δ) · excess e→f (Δ) · <verdict>` plus the rename note
(`computing…` while a run is in flight). The grid keeps its sections —
changed (|ΔCCN| descending), added, removed — with the section named on its
first row, and the columns change, function, file, line, `ccn
before→after (Δ)`, `cog before→after (Δ)`, `nloc before→after`, `cmt%
before→after (Δ)` in percentage points (the missing
side of an added / removed unit is `-`). The same filter row applies —
`file:`, `fn:`, `lang:`, `metric:`, and `min:` / `max:` on |Δ| of the sort column —
and `s` / `S` / the header click sort inside the sections. `enter` opens the
function (a removed one opens its file at the first line), `e` exports the
tab as CSV (`change,file,function,kind,line,ccn_before,ccn_after,ccn_delta,
cog_before,cog_after,cog_delta,nloc_before,nloc_after,cmt_before,cmt_after,
cmt_delta`), `y` copies
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
pure builder `complexityDiagnostics(view, ccnWarn, cogWarn, cmtWarn)` emits one
warning per function at or above a threshold, anchored at its header line:
`function <name>: CCN 40 ≥ 15`, `function <name>: cognitive 25 ≥ 15`, or
both joined by a comma when both are over; `Source` is `complexity`,
`Code` the first exceeded metric (`ccn` before `cognitive`). A body whose
comment density reaches `complexity.comment_density_warn` (#2934; `0` = off)
is a separate warning on the same line, `function <name>: comment density
55% ≥ 40%` with `Code` `comments`. Each publish
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
status slot: `Δcx +4` / `Δcx -2` / `Δcx ±0`, the `DeltaExcess()` of the
diff the Changes tab stores (#2945; until then `DeltaCCN()`), tinted after layout (foreground only) in the
warning, success or muted (inlay-hint) colour. It is hidden while the
setting or `complexity.enabled` is off, outside git and while the diff has
no changed files. The segment is a pure consumer; `complexityDiffWanted`
— the Changes tab on screen, the segment on, **or** (#2949) an open
editor of a measurable language whose file the VCS snapshot lists as
changed while the header hints are not `off` — is the seam that keeps
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
wants hints for one state key `cxHintKey{version, gen, ccn, cog, cmt, enabled, mode, sites, show, diff, diffGen}`
— the buffer's `DocVersion`, the index generation the hints came from, the
three warning thresholds, the master switch, the hint and site-mark modes
and a pending one-shot site request (below), and the Changes diff the Δ
comes from (#2949, below). The key last delivered (or
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

**Hints.** `complexityHintsFor(fns, ccnWarn, cogWarn, cmtWarn)` emits one
`editor.ComplexityHint{Line, Text, Warn, Deltas}` per function at its header line
(the first unit on a line wins, so a lambda beside its header does not
replace it); `Warn` when CCN, cognitive or the comment density reaches its
threshold, which paints the hint in the warning tone. The density joins the
text — `CCN 3 · cog 2 · cmt 55%` — only at or above
`complexity.comment_density_warn` (#2934), so the common hint stays short;
`0` never shows it. A threshold change on reload changes the key,
so every open buffer repaints. `editor.marks.complexity` (#2966) picks the hint mode — `all` (every
function, the default), `warn` (only the hints whose `Warn` flag is set:
CCN, cognitive or comment density at or above its threshold) or `off` — in
Settings > Editor > Diagnostics, next to the other mark toggles. The mode is
part of the sync key, so a change repaints every open buffer;
`complexity.enabled = false` and `off` both send one clearing push per
buffer that holds hints. A settings file holding the pre-#2966 bool loads
`true` as `all` and `false` as `off` without a diagnostic (`HintMode`'s
TOML decode); an unknown mode falls back to `all` with one. The per-site
marks' mode (#2947) is a separate setting: the function hint mode decides
which headers carry `CCN n · cog m`, the site mode which branch sites are
marked, and neither filters the other.

**Δ against the base (#2949).** While the Changes diff covers a file
(`Diff.BaseOf(path)`: the base measurement `DiffProject` compared, or nil
for a file only the head has), `cxMarkPushes` hands that base to
`complexityHintsFor(…, base, diffed)`. Every function pairs with a base unit
by `complexity.PairUnits` — `DiffFunctions`' `(Name, ordinal among
same-named units)` rule, not line numbers, which shift while typing — and
the CCN and cognitive that moved carry their Δ: `CCN 14 (−6) · cog 24
(−7)`, `CCN 14 · cog 24 (−7)` when only cognitive moved; an unpaired
function reads `CCN 9 · cog 11 (new)`; a function whose metrics held, or a
file outside the diff, shows nothing extra. The Δ is taken against the
**live** measurement (the dirty buffer's own, the index's for a clean one),
so it follows typing between diff runs. `editor.ComplexityHint` carries
`Deltas [2]ComplexityDelta{At, Delta, DeltaWarn}`: each Δ renders after the
first `At` bytes of `Text` (`Plain()` is the hint as it reads), in the
success colour for a fall and the warning colour (`DeltaWarn`) for a rise,
the metrics keeping the hint's own tone. The key's `diff` / `diffGen` are
the stored result's `cxDiffKey` and a generation `applyComplexityDiff` bumps
per result, set only for a covered file with the hints not `off`, so a
landed run repaints exactly its files. No extra `Measure`: an indexed
buffer is re-served from the snapshot, a dirty one from the last landed
measurement kept in `measured` (a key that differs from the sent one only
in `measureKey()` re-joins it; one still in flight joins the diff current
when it lands). The diff itself is armed for the Δ by the third
`complexityDiffWanted` condition (see Status segment) within the existing
300 ms debounce and single in-flight job.

**Site marks (#2947).** The same measurement also feeds a second editor
store, the Sonar-style per-site marks (`editor.ComplexitySiteMark{Line,
Text, Warn}` in `ComplexitySiteMarksMsg{Path, Marks, Version}`, editor side
`internal/editor/complexitysites.go`). `cxMarkPushes` derives both from one
`[]complexity.Function` — the snapshot's for an indexed buffer, one
`cxHintMeasure` (the `cxMeasureText` seam) per key for a dirty one — and
routes the hint and the mark pushes side by side; the debounce, the
version drop and `routeToEditor` are the hints'. The key grows `sites`
(`editor.marks.complexity_sites`) and `show`; `enabled` is the master
switch and either mode not `off` (or a pending `show`), so with the header
hints off the hint push is an empty clear and the marks still flow.
`complexitySiteMarksFor(fns, ccnWarn, cogWarn, mode, showLine)` takes the
cognitive sites (`Cognitive > 0`; CCN-only arms carry no mark) of every
function `all` admits, of `warn`'s functions at or above a threshold
(the per-file overrides of #2960 plug in at the same thresholds), and of
the function whose header is `showLine`; a site a nested unit shares with
its encloser is marked once (the encloser's copy, at its nesting); one mark
per line, the sites joined in column order: `+1`, `+3 (nesting 2)`,
`+1 ||` (a boolean run names its operator), `+1 · +1 &&`. `Warn` is an
increment of 3 or more. **One-shot `m`.** `m` in the Complexity window
(`complexity.showSites`, Complexity keymap context) sends
`complexitypanel.ShowSitesMsg{Path, Line}` for the selected function (a
site row's parent); the app opens the file if no view shows it and records
`cxShowSites{line, version}` in the hint state. While the buffer stays at
that version the key carries `show = line + 1` and the function's marks are
pushed whatever the mode; the first settled pass at another version spends
the request, so with both modes `off` the edit clears the marks at once.

| Key | Default | Meaning |
|---|---|---|
| `editor.marks.complexity` | `all` | Inline complexity hints on function headers: `all`, `warn` (over-threshold functions only) or `off`; a legacy `true` / `false` loads as `all` / `off`. |
| `editor.marks.complexity_sites` | `warn` | Per-site marks after every contributing line: `warn` (inside functions at or above a threshold), `all` or `off`; independent of `editor.marks.complexity`. |

## Headless CLI (#2922)

`ike complexity [--diff[=REV]] [--by class|file] [--brief[=class]] [--json] [--min N] [--ccn N] [--cog N] [--cmt N] [--no-tests] [--sites] [--churn[=DAYS]] [PATH...]`
is the terminal-free report for CI pipelines and coding agents. The grammar
lives in `internal/cli` (`ComplexityArgs`, first argument only like
`agent-hook`, so a file named `complexity` still opens as `./complexity`);
`cmd/ike` dispatches before any terminal setup to
`internal/complexitycli.Run`, which returns the exit code: **0** for a
report, **2** for a usage error, a path that does not exist (the message
names it), a git
error (an unknown `--diff` ref prints git's message on stderr) or a build
without syntax trees. The report never fails on thresholds — gating is the
caller's policy over the JSON.

**Selection (#2936).** `ComplexityArgs.Paths` holds any number of files and
directories, mixed; none means the working directory. Each path is made
absolute with symlinks resolved (macOS `/var` → `/private/var`, so paths
compare with what git reports). The **project root** — config discovery,
threshold defaults, root-relative slash paths in the output, the base of
the exclude globs — is the working directory without paths, otherwise the
git root of the first path (`vcscore.DetectRoot`), else the first path when it
is a directory, else the working directory. A directory is walked with
`complexity.WalkUnder(root, dir, …)` (the walk below, globs still relative
to the root); a named file is measured directly — no skip rule and no
`complexity.exclude` glob applies, since the user asked for it — and, when
no spec measures it, lands on the `skipped:` line under its language id or,
claimed by none, its extension. Every file is measured once (a file named
twice or also below a named directory); `summary.files` counts the measured
files of the selection.

**One walk.** The project report walks each named directory with
`complexity.WalkUnder` (`WalkProject` for the root), which runs on `langindex.WalkFiles` — the same
directory skip rules the index's `langindex` scan uses — and classifies
every file with the index's own `classify` (language with a spec, no
`complexity.exclude` glob), so the CLI and the tool window can never
disagree about which files count. `complexity.ReadSource` applies the
index's 512 KiB cap. Files a language claims but no spec measures are
counted per language in a final `skipped:` line (and the JSON `skipped`
object) rather than dropped silently; excluded files and files no language
claims are not counted.

**Settings.** `config.Load(config.Discover(root))` reads the user layer and
`<root>/.ike/settings.toml`: `complexity.exclude` and the warning thresholds
(15 without config) and `complexity.comment_density_warn` (40 without
config, `0` = off). `--ccn` / `--cog` / `--cmt` (percent, 1-100) override
the thresholds. The row filter: `--min N` keeps rows with CCN or cognitive ≥
N; an explicit `--ccn` / `--cog` / `--cmt` keeps rows at or above any
threshold; no filter flag keeps every row. The table carries a `CMT%`
column, the summary line the aggregate density and the thresholds. The summary always covers every function.
`complexity.enabled` is ignored — running the command is the request.

**Groups (#2937).** `--by class` / `--by file` (or `--by=…`) switches the
project report from the function table to `complexity.GroupFunctions` over
the measured functions keyed by their relative path: columns `FILE`, `CLASS`
(class level only), `FUNCS`, `NLOC`, `CCN` (sum), `CCN_AVG`, `CCN_MAX`, `COG`
(sum), `COG_AVG`, `COG_MAX`, `CMT%`, `OVER`, sorted by CCN sum descending; the
summary line is unchanged. The group filter: `--min N` keeps a CCN or
cognitive **sum** ≥ N; an explicit `--ccn` / `--cog` / `--cmt` keeps groups
whose **max** CCN, max cognitive or aggregate density is at or above its
threshold; the `shown:` line reads `shown: 1 of 2 classes (ΣCCN or Σcog ≥ 5)`.
An unknown level, or `--by` with `--diff`, is a usage error (exit 2,
`--by is not supported with --diff`). Without `--by` the output is
unchanged byte for byte. The flag is CLI-only (no setting, no command).

**Churn (#2957).** `--churn[=DAYS]` (`ComplexityArgs.Churn` / `ChurnDays`;
the window only after `=`, 1-3650, else `complexity.churn_days`, else 90)
runs `vcscore.Churn(root, now − DAYS)` synchronously, like `--diff`, and
adds `COMMITS` to the function table and `COMMITS` and `HOT`
(`complexity.Hot` over the group's summary; class groups take their file's
commits) to the group tables, `commits=N  hot=N` to the brief lines, a
`churn: commits in the last N days (git log, renames not followed)` line
under the summary, and JSON `commits` per function and per group, `hot` per
group and `summary.churn_days`. Outside a repository it exits 2 with
`--churn needs a git repository: <root> is not inside one`; with `--diff` it
is a usage error (`--churn is not supported with --diff`). Without the flag
the output is unchanged byte for byte.

**Brief (#2938).** `--brief` (one line per file) and `--brief=class` (one
line per class) are a second text renderer over the same groups
(`ComplexityArgs.Brief`, `writeBriefText`): no header, no summary, no
`shown:` / `skipped:` trailer, one tab-aligned line per kept group,
columns two spaces apart —
`<path>  [<Class>]  funcs=N  nloc=N  ccn=Σ  cog=Σ  max=CCN/cog  cmt=N%`, in
the group order (CCN sum descending, then path, then class). At the class
level a file's free functions and lambdas are one more line labelled
`(free)` (`groupProject` hands them that class before `GroupFunctions`);
`--by class` still leaves them out. The filter is the group filter of
`--by`. With `--json` brief changes nothing: the output is the `--by` JSON
of the same level byte for byte (no `(free)` group). A level other than
`class` / `file`, `--brief` with `--by` (`--brief and --by exclude each
other`) or with `--diff` (`--brief is not supported with --diff`) is a
usage error (exit 2). The level is taken only after `=`, so
`--brief PATH` keeps PATH as a path.

**No tests (#2950).** `--no-tests` (`ComplexityArgs.NoTests`) drops the
files `complexity.IsTestFile` calls test code (see "Hide tests" above) from
the walk, so the table, the groups, the brief lines and the summary all
leave them out; the text adds `tests hidden: N files (--no-tests)` under the
summary line and `; tests hidden` inside the `shown:` parentheses, and every
JSON `summary` (and the diff's `base` / `head`) gains `tests_hidden: true`.
Under `--diff` a test file is skipped like an excluded one (no `skipped:`
count) and the text ends with `tests hidden (--no-tests)`. A file named
explicitly is kept, as with the exclude globs. Without the flag the output
is unchanged byte for byte.

**Sites (#2946).** `--sites` (`ComplexityArgs.Sites`) adds the per-site
breakdown to the JSON only: every `functions` entry of the project report
(and every `added` entry of the diff) gains `sites`, one object per
`Function.Site` — `line` and `end_line` 1-based, `col` 0-based bytes,
`kind`, `ccn`, `cognitive`, `nesting`, `level` — and every `changed` entry's
`after` side gains the same; the `before` side and the `removed` entries
never carry it. Without the flag the JSON is byte-identical to before
(`omitempty`), and the text table, the groups and the brief lines ignore the
flag altogether.

**Diff.** `--diff[=REV]` (default `HEAD`) runs synchronously the same git
plumbing as the Changes tab — `vcscore.ResolveRev`, `vcscore.ChangedFiles`
(the bubbletea-free core `vcs` re-exports, #2939), the status snapshot's
untracked files (a collapsed untracked directory is walked) and
`vcscore.RevContent` per base blob — restricted to the selection (below a
named directory and measurable by the index's rules, or a named file with a
spec; a named file inside a collapsed untracked directory is added even
when the walk would skip it), then `complexity.DiffProject`; a named file
without changes yields an empty diff and the `unchanged` verdict. The head side is read
uncapped like the tab's, so a file over the walk's cap does not read as
all-removed. The text report prints `changed` (before→after with Δ per
metric), `added` and `removed` sections, the totals and
`verdict: LESS COMPLEX (excess -12, Σcog -7, ΣCCN +1)`; the totals line
and the project summary line carry `excess` too, the group table an
`EXCESS` column.

**JSON.** Field names are snake_case and stable (documented in
`userdocs/guides/complexity.md`; add, never rename): the project report is
`{summary, functions, skipped}` — with `--by`, `{summary, groups, skipped}`,
each group `{file, class (class level only), functions, nloc, ccn_sum,
ccn_avg, ccn_max, cog_sum, cog_avg, cog_max, over_threshold, excess,
comment_lines, comment_density}` under the summary's field names — the diff `{rev, sha, base, head, delta,
changed, added, removed, verdict, skipped}` with `verdict` one of
`more_complex`, `less_complex`, `unchanged` (read by the #2945 rule above;
`delta.ccn` keeps the old ΣCCN policy available). `excess` (#2945) sits on
every summary (`summary`, `base`, `head`), the top-level `delta`, the
delta of every changed function and every group. Line numbers are 1-based.
`comment_lines` and `comment_density` (a ratio, four decimals) sit on every
function, both sides and the delta of a changed function, the summaries and
the top-level delta; the summaries also carry `cmt_warn` (#2934). The CLI
keeps its snake_case spelling for them — the engine's own `Function` JSON
uses `commentLines` / `commentDensity`.

**No cgo.** Without cgo there are no grammars; `complexity.Available()`
(shared with the index's `Unavailable` stat) is false and the command
prints a clear error and exits 2.

**Row order.** The function table and `functions` are sorted by CCN
descending, then cognitive descending, file, line, end line and name. The
files are measured in parallel and rows arrive in finishing order, so the
last three keys matter: two units on one line (closures passed side by
side) used to swap between runs (#2939).

**Complexity-only build (#2939).** `make build-complexity` /
`make install-complexity` build the same `ike` binary with the
`ike_complexity_only` build tag. `cmd/ike` is split by that tag: `main.go`,
`agenthook.go`, `crashfallback.go` and the IDE audit tests are
`!ike_complexity_only`; `complexityonly.go` is the tagged entry point —
`runComplexityOnly` parses argv with `cli.Parse`, runs
`complexitycli.Run` for `ike complexity …`, prints
`version.FullVariant("complexity-only")` for `--version` / `-v`, and answers
everything else (`ike`, a file target, `agent-hook`, an `ike://` link, a
parse error outside `complexity`) with the one line
``ike: this ike build only provides `ike complexity`; install the full build for the IDE``
and exit 2. `complexity.go` (`complexityParseError`) is shared, so a bad
`ike complexity` invocation fails identically in both binaries. The build
links only the CLI parser, `config`, `vcs/vcscore`, `lang` / `highlight`,
the engine and the language plugins that carry a spec (Go, PHP, Python,
Shell, `web` for TypeScript/JavaScript) with their grammars. To keep that
graph clean, `config`'s reload commands moved to `internal/configcmd`,
`vcs`'s tea-free core to `internal/vcs/vcscore`, and
`plugins/languages/register` registers its plugin-manager shim only in the
full build (`shim.go` / `shim_none.go`). `TestComplexityOnlyBuildDeps`
(both builds) runs `go list -deps -tags ike_complexity_only` and fails on
bubbletea, bubbles, `internal/app`, `host`, `wasm`, `plugin`, `settings` or
the LSP / tasks / format plugins.

The report must match the full build byte for byte, including the
`skipped` line, which names languages the build does not link. The
generated `complexityonly_langs.go` registers a detection-only stand-in
(id, extensions, file names) for every language without a spec, skipping
ids a linked plugin already registered (`go.mod`, `html`, …); the patterns
are exactly those that resolve to the language in the full build, and the
stand-ins register in `package main`'s init, after every plugin, so
extension and file-name collisions resolve as in the full build.
`TestComplexityOnlyLangTable` (full build) regenerates it from the live
registry and fails on drift
(`IKE_GEN_COMPLEXITY_LANGS=1 go test ./cmd/ike -run TestComplexityOnlyLangTable`);
the file also lists the spec'd language ids, and the tagged
`TestComplexityOnlyLanguages` fails when one of them is not linked with its
spec — a new spec'd language needs its blank import in
`complexityonly.go`. The CI workflow `complexity-only.yml` builds the tagged
binary, checks the banner and the refusal, runs
`ike complexity --json .` on the repository, and vets and tests the tagged
packages. CGo stays required: without it `complexity.Available()` is false
and the command exits 2, as in the full build.

## Settings

All keys live in the `[complexity]` section (Settings > Complexity, user
scope), validated in the form and by `config.Validate`:

| Key | Default | Meaning |
|---|---|---|
| `complexity.enabled` | `true` | Master switch for the index and every surface reading it. |
| `complexity.exclude` | `[]` | `internal/pathglob` patterns of files never measured. |
| `complexity.ccn_warn` | `15` | Cyclomatic warning threshold (1-200): tool-window colour, hints, Problems, over-threshold count. |
| `complexity.cognitive_warn` | `15` | The same for cognitive complexity. |
| `complexity.comment_density_warn` | `40` | Comment-density threshold in percent (0-100, `0` = off): `cmt%` colour, the hint's ` · cmt 55%`, the Problems `comments` row, `--cmt` default, over-threshold count. |
| `complexity.problems` | `true` | Publish over-threshold functions into Problems. |
| `complexity.status_segment` | `false` | Show the `Δcx` status segment. |
| `complexity.churn_days` | `90` | Hotspot window in days (1-3650): the commits the `commits` / `hot` columns count; `--churn`'s default. |

## Limitations

- **Churn does not follow renames.** The hotspot `commits` count a path's
  commits as named at each commit; a renamed file starts again at 0 under
  its new name.
- **No grammar, no metrics.** Only languages with a Tree-sitter grammar and a
  `ComplexitySpec` are measured; the others are rows in the audit ledger and
  are absent from every surface. A build without cgo measures nothing (the
  CLI exits 2, the index reports `Unavailable`).
- **Renames are removed + added.** Units pair by name and ordinal; there is
  no body hash, so a renamed function shows as one removal and one addition
  in the Changes tab and `--diff`.
- **Anonymous naming is positional.** Closures and lambdas are
  `(anonymous)#n` numbered per file in source order, so inserting one above
  shifts the numbers of those below and the diff pairs them by position.
- **Comment markers inside strings** are not recognised, for NLOC and the
  comment density alike: a line that is nothing but a string starting with
  `//` or `#` reads as a comment line.
- **NLOC includes nested units** (lizard subtracts them), and the
  cross-check deviations per language are recorded in `golden_test.go`.
- No trend history, no enforcement: IKE reports, policy belongs to CI.

## Extending

A new language declares its `lang.ComplexitySpec` (see
[languages](./languages.md#complexity-spec-2914) and the
[change workflow](/process/change-workflow.md)) and gets a fixture directory
here: `testdata/<lang>/fixture.<ext>`, an entry in `fixtureLang`, a golden
generated with `-update` and reviewed against the language's reference tool,
with any deliberate difference recorded in `golden_test.go`.
