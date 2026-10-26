---
type: concept
title: Code Complexity
description: The code-complexity engine (Roadmap 0550, #2913), cyclomatic (McCabe) and cognitive (Sonar) complexity, NLOC, parameters, nesting depth and comment density (#2934) per function, measured on the Tree-sitter parse through each language's ComplexitySpec; the rules, the walker, the per-language golden fixtures cross-checked against lizard, gocyclo, gocognit and complexipy, the summary with its per-class and per-file groups (#2937), the project index (#2916) that measures every supported file in the background, persists it across sessions (#2967, a stamped complexity-index.gob in the state store), keeps it current from saves and watcher events, and hands consumers immutable snapshots, the Complexity tool window (#2917) and its Changes tab (#2918) diffing the working tree's functions against a base revision with a verdict, the Problems feed (#2920) publishing over-threshold functions under the complexity source, the hotspot columns (#2957) multiplying complexity by the commits per file from one git log, the accepted-debt baseline (#2958, a committed .ike/complexity-baseline.json that turns every surface into a ratchet flagging only new and worsened functions), the optimisation-session pins (#2959, functions pinned with their metrics in the state store's complexity-pins.json, a Pinned section with a since-pin delta and a session Δexcess in the header), and the headless `ike complexity` CLI (#2922) printing the table of the project or of named files and directories (#2936), per function or summed per class or file (#2937), or the diff against a revision as text or JSON for CI and agents, also as a complexity-only build of the ike binary (#2939, build tag ike_complexity_only, make install-complexity) that links no IDE; configured by the [complexity] section (Settings > Complexity) with a complexity.scan telemetry op.
resource: internal/complexity, internal/complexitypanel, internal/app/complexity_agent.go, internal/agentask/simplify.go, internal/agentask/extract.go, internal/agentask/answerdiff.go, internal/app/complexityindex.go, internal/app/complexity_diff.go, internal/app/complexity_churn.go, internal/app/complexity_baseline.go, internal/app/complexity_pins.go, internal/vcs/vcscore/churn.go, internal/app/complexity_problems.go, internal/app/complexity_segment.go, internal/complexitycli, cmd/ike/complexityonly.go
tags: [architecture, complexity, metrics, tree-sitter, languages, cyclomatic, cognitive, index, settings, telemetry, vcs, diff, problems, cli, baseline, pins]
timestamp: 2026-10-16T12:00:00Z
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
    Candidates     []Candidate // the best extraction candidates, saving descending (#2948)
}

type Candidate struct {
    Line, EndLine    int    // the node's 0-based range, header line to last line
    Kind             string // the node's site kind: "if", "for", "switch", "else", …
    CogSaving        int    // cognitive points the file loses: the unit's loss minus the new unit's cost
    NewCCN, NewCog   int    // the metrics the extracted function would have
    RestCCN, RestCog int    // the metrics the enclosing unit would keep
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

**Extraction candidates** (#2948, `candidates.go`). The classic answer to
high cognitive complexity is extracting the deepest nested block into a
function of its own, because the nesting penalty vanishes for the extracted
code; the walk knows every nesting-raising node, its level and the sites
inside it, so it computes that saving exactly. For every Branch, Selector
and else body of a unit (not an else-if, which is part of its chain and is
covered by the chain head's candidate; not a nested unit, which is already
its own function — a closure's structures are the closure's candidates, and
the enclosing unit's candidate counts the closure's sites at the enclosing
nesting) the engine records a `Candidate`:

- A **structural node** (`if`, `for`, `switch`, a nested `?:`) at level `L`
  is extracted whole, header included, and lands at level 0 of the new
  unit: the node itself goes from `1 + L` to `1`, every site inside keeps
  its flat part and loses `L` of its nesting penalty (never below 0 — a
  flat site, an else, a jump, a boolean run, loses nothing).
  `CogSaving = L + L × (nested sites inside)`; `NewCCN = 1 + node.CCN +
  Σ inside.CCN`; `NewCog = 1 + Σ inside.Cognitive − L × (nested sites
  inside)`.
- An **else body** cannot take its keyword along (`else { helper() }`
  stays), so its own flat point stays with the rest and the body's sites,
  which sat at `L + 1`, lose `L + 1`: `CogSaving = (L + 1) × (nested sites
  inside)`, `NewCCN = 1 + Σ inside.CCN`, `NewCog = Σ inside.Cognitive −
  (L + 1) × (nested sites inside)`.
- `RestCCN = CCN + 1 − NewCCN` (the file's ΣCCN rises by one, as it
  must) and `RestCog = Cognitive − CogSaving − NewCog`, so `RestCog +
  NewCog == Cognitive − CogSaving` for every candidate.

Worked example — an `if` at level 0 holding a `for` that holds two `if`s
(CCN 5, cognitive 1 + 2 + 3 + 3 = 9): the outer `if` saves nothing (new fn
ccn 5 cog 9, rest 1 / 0); the `for` (level 1) saves `1 + 1 × 2 = 3` (new
fn ccn 4 cog 5, rest ccn 2 cog 1); each inner `if` (level 2) saves 2 (new
fn ccn 2 cog 1, rest ccn 4 cog 6). The sums are O(sites) per unit: the
unit keeps running totals of the sites appended so far (CCN, cognitive,
how many carry a nesting penalty), a candidate snapshots them when its node
is entered and reads the difference when it is left; every structural site
inside a node at level `L` sits at level ≥ `L + 1` with a penalty equal to
its level, so the count is all the saving needs. A unit keeps the best
`MaxCandidates` (3) with `CogSaving ≥ MinCandidateSaving` (3), saving
descending, then range ascending; overlapping candidates (an `if` inside a
`for`) are both reported — the user chooses. `TestCandidatesAcceptanceFixture`
pins the worked example with the derivation, `TestCandidateInvariants`
re-derives every golden candidate site by site and checks the identities;
`BenchmarkMeasure` stays at about 30 ms.

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

`Summarize(fns, t Thresholds)` aggregates a list — a file or a
project — into functions, NLOC, Σ / avg / max CCN, Σ / avg / max cognitive,
the summed `CommentLines` with the aggregate `CommentDensity`, and the
number of functions **at or above** any warning threshold — CCN, cognitive,
or a comment density of `t.Cmt` percent (a non-positive threshold disables
that check). `Excess` (#2945) is the mass **above** the CCN and cognitive
thresholds: `Function.Excess(ccnWarn, cogWarn)` = `max(0, CCN − ccnWarn) +
max(0, cognitive − cogWarn)` (a unit at a threshold counts as over it but
has no excess; a non-positive threshold drops its term), summed over the
population. The over count says how many units cross a line, the excess how
far — the primary signal for optimisation and what the diff verdict reads
first.

**Per-file thresholds (#2960).** `Thresholds{CCN, Cog, Cmt, Source}` is the
set one file is judged by, and a `Resolver` (`func(path string) Thresholds`)
hands every consumer the set of each file: `ResolverFor(config.Complexity,
root)` / `ThresholdsFor(c, root, path)` take the first
`complexity.overrides` entry whose selector matches the file — a glob over
the project-relative path with the `complexity.exclude` rules (`Excluded`),
or `lang:<id>` against the language `lang.ByPath` resolves — fill the keys
it leaves out from the globals and name the selector in `Source`; no match
is the globals (`Global(c)`, `Source` empty), and without overrides the
resolver is `Uniform(global)`. A population mixing files is aggregated with
a `Summer` (`Add(f, t)` per function, then `Summary()`), which
`SummarizeFiles(files, res)`, `View.Summary(res)`, `GroupFunctions(files,
by, res)` and `DiffProject(base, head, res)` use; `Summarize` is the
single-set case. The resolver lives in `complexity`, not `config`: `config`
sits below `lang` in the import graph and cannot resolve a language, so
`config` owns the grammar (`ParseThresholdOverride`, `ThresholdOverrides`)
and `complexity` the matching.

**Distribution (#2953).** An average of 4 next to a maximum of 170 says
nothing about the shape, so `Summarize` also takes the **nearest-rank**
percentiles of both metrics — `CCNP50/P90/P99`, `CogP50/P90/P99`: the value
at rank `⌈p/100 · n⌉` of the ascending order (over `1…100` that is 50 / 90 /
99; one function gives its own value for all three; an empty list 0). It
sorts a copy per metric — fine at 34 k functions, the call runs per refresh,
not per key. `Histogram(fns, metric)` (`MetricCCN` / `MetricCognitive`)
counts the population per `HistBuckets` bucket — `1–5`, `6–10`, `11–15`,
`16–25`, `26+`, the same edges for both metrics, a cognitive `0` in the
first (`HistBucketOf`) — so the buckets sum to the population;
`Summary.CCNHist` / `CogHist` hold both.

**Groups (#2937).** `GroupFunctions(files, by, res)`
(`group.go`) folds functions keyed by file into one `Group{File, Class,
Line, Summary}` per class (`ByClass`: the units with a non-empty
`Function.Class`, keyed by file and class — Go methods of one type in two
files are two groups) or per file (`ByFile`: every unit). Each group is
`Summarize` over its direct units under its file's thresholds (`res`), so the sums of the file groups equal the
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
engine cannot measure (no tree) is not indexed. A cold walk of IKE itself
(3 187 files, 33 949 functions) takes a few seconds.

**Persistence (#2967).** The finished walk is written to
`complexity-index.gob` in the state store (`IKE_CONFIG_DIR`, else the
project's `.ike/`, which the repository ignores; `complexity.CacheFile`) by
`persist.go`, which mirrors the PHP index's cache end to end: a gob stream
of a header (`SpecVersion`, the project root, the record count) and one
record per file — absolute path, the `langindex.Stamp` (size, mtime) of the
file it was measured from, and its `[]Function` (`Baseline` cleared: a
classification, not a measurement). The write is atomic (a temporary
sibling renamed into place) and runs on the scan goroutine through the
walk's `Persist.Saved`, only while the walk is still the index's; a walk
with no measured file removes the file instead. The next session's walk
seeds itself through `Persist.Load`: a file whose stamp still matches is
taken without being read or measured, a changed or new one is measured, and
a file the walk no longer reaches (deleted, excluded) is never visited, so
the cache cannot resurrect it. A cache for another root (the shared
`IKE_CONFIG_DIR` case) or another `SpecVersion` is ignored quietly — bump
`SpecVersion` whenever a rule, a language spec or `Function` changes
(`TestCacheVersionTracksFormat` pins the shape; a rule change has none, so
that bump is by hand). A damaged cache is ignored too, reported once
(`Stats.CacheCorrupt`, the scan op's `cache=corrupt`) and overwritten by the
walk it failed to seed.

**Lifecycle.** `NewIndex(root, opts)` builds the walk but does not start it;
`Start` (called from the app's `Init`, after the program's sender is wired,
like the TODO index) kicks it off in the background. `Reconfigure(opts)`
applies a reloaded `[complexity]` section live: the master switch off drops
the walk and the snapshot, on rebuilds it (seeded from the cache), a changed
exclude list rewalks cold, unchanged options are a no-op. `Rebuild` is the
escape hatch for a tree that changed without watcher events — or a cache
gone wrong: it ignores the seed and replaces the cache once it finishes. A
project switch builds a new model and therefore a new index.

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
file and function counts, the walk's duration, whether it hit the cap,
`Cached` — how many files the finished walk took from the persisted cache
(the tool window's header reads `gen 1 (2981 cached)`) — and
`CacheCorrupt`.

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
| `complexity.overrides` | `[]` | Per-path and per-language thresholds (#2960), one `<selector> ccn=N cog=N cmt=N` entry each: the selector is a glob over the project-relative path (the `complexity.exclude` rules) or `lang:<id>`; each key is optional (the global value applies to a missing one), ranges 1-200 / 1-200 / 0-100, at least one key. The first matching entry in list order wins (`legacy/** ccn=30 cog=30`, `lang:php cog=25 cmt=0`, `*_test.go ccn=40 cog=40`). A malformed entry is refused in the Settings form with a message naming it and dropped with a diagnostic by `config.Validate`. Every threshold surface below reads the resolved set of each file. |

**Telemetry.** One `complexity.scan` op per completed walk (the initial one
and every rebuild), phase `ok` with `ms`, `files`, `functions`, `cached`
(v17, #2967), `truncated` and — for a damaged cache only — `cache=corrupt`,
or phase `error` when the build has no syntax tree at all. Like
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
[below](#changes-vs-head-2918)), `Trend` the project's sums at each of the
last N commits (#2962, [below](#trend-2962)), `Duplicates` the project's
clone groups (#3044, [below](#duplicates-3044)); `tab` / `shift+tab` and a
click on the header's tab bar switch. The pane is a pure consumer: the app seeds it with
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
and cognitive complexity — the sort metric's (cognitive while sorting by
it, CCN otherwise) with its percentiles between average and max, `CCN
Σ137301 avg 4.0 p50 2 p90 9 p99 28 max 170` (#2953), the other in the
short form so the line stays within reason — the aggregate comment density (`cmt 12%`), the
count over threshold, `excess N` (#2945) and, with a baseline in effect,
`N accepted` (#2958, [below](#baseline-2958)), with hidden functions in the
tab's population `N hidden` (#3031, [below](#hidden-set-3031)) and, on the Project tab,
`session Δexcess −120` (#2959, see Pins below). On the File tab, when a
`complexity.overrides` entry sets the followed file's thresholds, the
line names them after the generation: `thresholds ccn 30 cog 30
(legacy/**)` (the comment density joins when it differs from the global,
#2960). The grid
(`gridview.HeaderRow` / `DataRow`, widths from the longest visible value
under the column budget below) has the columns file
(project-relative; hidden on the File tab), function, line (1-based), ccn,
cog, nloc, params, depth, `cmt%` (the comment density in whole percent,
#2934). `s` cycles the sort column (default CCN
descending; a numeric column starts descending, a text column ascending),
`S` reverses, a click on a column header sorts by it (again: reverses). A
CCN, cognitive or `cmt%` cell at or above its file's threshold —
`complexity.ccn_warn` / `complexity.cognitive_warn` /
`complexity.comment_density_warn` (unless 0), or the matching
`complexity.overrides` entry (#2960) — renders in the palette's warning colour
(`gridview.Cell.Warn`); on a function the baseline accepts the same cells
render muted instead (`gridview.Cell.Muted`) and the name carries ` ✓`.
The status row shows `filtered/total rows` and the key hints.

**Column budget (#2992).** No text cell may push the metrics off the pane.
`Model.fitWidths` (`internal/complexitypanel/fit.go`) runs at the end of
`columnWidths` and `treeWidths`: the numeric columns keep their natural
width, and the text columns — file, function / tree name, class, section,
commit subject — share what is left of the pane (minus the leading space and
the two-space gaps) through `gridview.FitWidths`: they keep their width when
they fit together, otherwise the room is split proportionally to their
natural widths, never below `gridview.MinTextWidth` (12; a column already
narrower keeps its own). On a pane too narrow even for those minima every
text column is squeezed to its minimum and `DataRow` clips the line as
before. Every row goes through `Model.dataRow`, which truncates an
overflowing text cell with an ellipsis before `DataRow`: a path from the
left (`gridview.TruncLeft`, `…/complexitypanel/complexitypanel.go` keeps the
file name), every other text from the right (`gridview.TruncRight`,
`SomeLongClassName::someVeryLo…`). `gridview.TruncMarked` keeps the caret
`▸ ` and the trailing ` ✓` / ` ★` / `*` marks of a function cell and the
sort marker of a header label (`fitLabels`); the caret cell's occurrence
highlight is a cell flag and survives. When the cursor row has a truncated
cell, the status row leads with its full text (`clippedText`: the full path,
the full name, ` · ` joined). The CSV and Markdown exports read the rows,
not the cells, so they keep the full text.

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
`baseline:new|accepted|worsened` (#2958) keeps functions of that status
against the accepted-debt baseline — on a group row, groups with such a
member (`Summary.Accepted` / `Worsened` / the remaining over count).
`pinned:yes|no` (#2959) keeps the pinned functions or the rest (groups:
with a pinned member). `hidden:yes|no` (#3031, [below](#hidden-set-3031))
lists the hidden functions — which no other filter can reach, since without
the term they are dropped before the rows are built — or the rest (groups:
with such a member).
Every key takes a leading `-` to negate it (#2951): `matches` runs each
field through `filterexpr.Query.KeepsAll` (positive repeats AND, a negated
term drops what it matches) and the free text through `MatchFree`, so
`file:internal/app -file:*_test.go` lists the non-test files under
internal/app, `-fn:render` drops the renderers, `-lang:php` the PHP rows and
a bare `-word` every row whose name it fuzzy-matches.

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

**Distribution row (#2953).** `D` — the `complexity.histogram` command,
bound to `shift+d` in the `complexity` keymap context and in the palette
as "Complexity: Toggle Distribution (Histogram)" — shows one line under
the filter row (`histogram.go`) on the Project and File tabs: the tab's
`Summary.CCNHist` (or `CogHist` while sorting by cognitive) as `1–5
████████ 29310 · 6–10 ███ 3102 · 11–15 █ 812 · 16–25 ▌ 540 · 26+ ▏ 185`.
The bars are runs of `█` scaled so the widest bucket fills eight cells, in
eighths (`▏`…`▉`; a non-empty bucket shows at least `▏`), no new building
block; a bucket that reaches the metric's warning threshold renders in the
warning colour. The row counts the tab's population before the filter,
like the header, so narrowing to one bucket keeps the others readable.
While it shows, the chrome is five lines (`chromeRows()` /
`headerRows()`): the column header moves to y 3, the list to y 4. A click
on a bucket (`Click(x, 2)` → `clickHistogram`, the x ranges from
`histSegments`) writes `min:` / `max:` through `filterbar.SetTerm`, keeping
the other typed terms — `min:16 max:25`, `min:0 max:5` for the first bucket
(a cognitive 0 counts there), `min:26` for the last; a sort column other
than the counted metric moves to it so the keys read the bucket's metric.
The bucket the filter names renders bold, and a second click on it clears
both keys. The state persists as `hist:on` in the layout `Mode`
(`project:file,tests:hidden,hist:on`); the Changes tab has no row.

**Tree view (#2954, `tree.go`).** `v` — the `complexity.view` command,
bound to `v` in the `complexity` keymap context and in the palette as
"Complexity: Toggle Tree / Grid View" — switches the Project tab between
the grid and a `hiertree` directory tree: root → directory → … → file →
class → function, a class level only where functions carry a `Class`
(free functions sit directly under the file), every directory level kept
(no chain collapsing — the levels are what the user drills through). The
tree is built from the filtered function rows (`treeRoot`: the
project-relative paths split on `/`) and served by `hiertree.Static`, the
whole tree in memory; a node's key (`dir\0rel`, `file\0path`,
`class\0path\0class`, `fn\0path\0name\0change\0ordinal`) is what
`hiertree.Refresh` carries the expansion and the selection by, so a
`ComplexityChangedMsg` (`Set` → `Refresh` → `buildTree`) keeps both; the
first build opens the tree with the root expanded, and a non-empty filter
unfolds every directory, file and class so the matches show
(`expandBranches`). Each directory, file and class node is a group row
(`Row.Group`, `aggregateRow`: `complexity.Summarize` over the function
rows below it with `DepthMax` / `ParamsMax`, `Members` for `i`), computed
once per rebuild, bottom-up; the root row equals the tab's totals. The
layout (`treeLayout`, `name`, funcs, nloc, `Σccn`, ccn max, `Σcog`, cog
max, `cmt%`, over, excess, plus commits and hot inside a repository — a
file node its file's, a class node its file's commits over the class
summary, a directory the sums) is extensible for later node columns. A
function leaf shows its own metrics in nloc, `Σccn`, `Σcog`, `cmt%`,
excess and commits, the count, the maxima and over blank
(`fnTreeCell`); its name carries the grid's marks (`✓`, `★`). Children
sort by the sort column (`sortNodes`: `Σccn` descending by default, the
name column as text; `s` cycles the tree layout, `S` reverses, header
clicks sort) — a function leaf reads the aggregate columns as its own
metric (`fnTreeValue`: ccn max → CCN, over → 0 / 1, excess), which
`min:` / `max:` read too. Warning colour: the group rule on a node
(`groupWarn`), the function rule on a leaf. Keys: the list navigation
and `enter` / `l` / `h` / `space` / `tab` go through `hiertree.Key`
(`treeKeyPress`) — `enter` opens a function (`OpenLocationMsg`) or a
breakdown row's line and toggles any other node, `l` / `right` unfold,
`h` / `left` fold or climb to the parent, `tab` cycles the tab; `space`
(`ToggleSites` → `toggleTreeSites`) toggles a function's breakdown — a
function with sites or candidates carries them as children (#2946 /
#2948, in the breakdown's order), a function without reports "nothing to
expand", a breakdown row folds its parent, an aggregate node toggles.
`b` leaves the tree alone and says so in the footer. `Selected` reads
the tree cursor, so `y` (`groupCopyText`: `a: funcs=3 ccn=26 (max 20)
cog=29 (max 25)`), `i`, `p`, `m`, `d` and `a` work on the selected node; `e`
exports the visible rows (breakdown rows left out) as
`path,kind,name,functions,nloc,ccn_sum,ccn_max,cog_sum,cog_max,comment_lines,cmt_pct,over_threshold,excess`
(+ `commits,hot`), kind one of dir / file / class / function. The mouse:
a click selects, a double click acts as `enter`, the wheel scrolls the
tree window (`hiertree.Window` / `Wheel`, the shared follow). The header
totals, the tab bar, the distribution row and the hide-tests toggle are
the grid's; the Pinned section does not form (a gone pin has no node).
The state persists as `view:tree` in the layout `Mode`
(`project:file,tests:hidden,hist:on,view:tree`) and the command saves
the layout at once; the footer reads `v tree` on the grid, `v grid` on
the tree.

**Grouping (#2937).** `b` — the `complexity.group` command, bound to `b`
in the `complexity` keymap context and in the palette as "Complexity: Cycle
Grouping" — cycles the Project tab none → class → file and the File tab
none → class, and the Changes tab none → file (#2955, below). A
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
pane identity's `Mode` (`project:file,file:class,changes:file`; `GroupMode` /
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

**Extraction candidates (#2948, `candidates.go`).** Under the site rows
follow the function's `Function.Candidates`, one indented row each in the
engine's order (best saving first): the function column reads `  extract
120–160 (if): −18 cog · new fn ccn 9 cog 11 · rest ccn 12 cog 14` (a
one-line node shows one number), the line column the header line, the
other columns blank. `Row.Cand` / `Row.Parent` mark one; `Row.child()`
(site or candidate) is what every site-skipping path tests, so the totals,
the caret mark, the CSV export, the group rows and the hotspot scale skip
them too, `space` / `m` / `i` on one act on the parent, and the cursor
stays on the same candidate across a refresh (`Row.key` → `candKey`).
`enter` / a double click sends `OpenLocationMsg{Path, Line, EndLine}`; the
app opens the file through `openPathAt` and, when `EndLine` is past
`Line`, calls `editor.SelectLines(Line, EndLine)` — a linewise visual
selection over the range with the caret on the header (anchor on the last
line, framed like a jump) — so `alt+enter` can offer the language server's
*Extract function* on it. `y` copies `path:120–160: extract (if): −18 cog ·
new fn ccn 9 cog 11 · rest ccn 12 cog 14`. Triggering the
`refactor.extract.function` code action directly is a possible follow-up.

**Actions.** `enter` / a double click sends `OpenLocationMsg{Path, Line,
EndLine}` (`EndLine` set by a candidate row only) and the app opens the
file through `openPathAt`, so the caret lands on the function header and
`nav.back` returns. `r` (and the palette's
`complexity.refresh`) rebuilds the index (`Index.Rebuild`; a disabled index
or a build without a syntax tree is reported instead). `e` (and
`complexity.export`) writes the current tab as a CSV scratch
(`ExportMsg{Label, CSV}`: header `file,function,kind,line,ccn,cognitive,nloc,params,depth,comment_lines,cmt_pct`,
one row per visible function, the file column on every tab) and opens it.
`E` (and `complexity.exportMarkdown`, #2963) writes the same rows as
Markdown (`ExportMarkdownMsg{Label, Markdown}`, `Model.Markdown` in
`internal/complexitypanel/markdown.go`) to a `.md` scratch through
`complexitycli.WriteMarkdown`: the headline is the tab's totals line (the
Changes tab: `Complexity: <VERDICT>` with the totals as a note and one
table per changed / added / removed section, the section column dropped),
the header the on-screen labels without the sort marker, the rows the
visible ones in the view's order (breakdown rows left out; the tree's
indented with no-break spaces).
`y` / the copy chord puts `path:line: name ccn=.. cog=..` on the clipboard
(and the clipboard history). The wheel scrolls, a click selects. The
single keys are the pane's own (keybind audit ledger, `reasonPaneKey`).

**Pins** (#2959, `internal/complexity/pins.go`,
`internal/complexitypanel/pins.go`, `internal/app/complexity_pins.go`).
An optimisation session has a handful of targets; a pin remembers one
function *and the metrics it had when pinned*, so the panel shows what a
refactor achieved. `complexity.Pins` is the file `complexity-pins.json` in
the state store (`PinsPath(root)`: `$IKE_CONFIG_DIR/complexity-pins.json`
when set — the `bookmarks.File()` rule — else `<root>/.ike/`), entries
`{file, function, ordinal, ccn, cognitive, pinned_at}` keyed by the
baseline's matcher (`EntryKey`: project-relative path, name, ordinal among
same-named units), sorted by file, function, ordinal and written atomically
(temp file + rename) on every change; the last unpin removes the file. The
app loads it lazily (`complexityPinsState.list`, a malformed file reads as
empty) on the first panel seed or `p`. `p` (`complexity.pin`, "Complexity:
Pin / Unpin Selected Function"; the pane's own key, `reasonPaneKey`) is
`TogglePin`: a function row (a site / candidate row stands for its
function, a Changes row for its head side via `changeOrdinal`) or a gone
pin sends `PinToggleMsg{Rel, Fn, Ordinal}`; a group or Changes file row
and a removed function put the reason in the footer until the next key.
`markPins` sets `Row.Pin` on every tab (` ★` after the name, after a
baseline ` ✓`); on the ungrouped Project tab the pinned rows move into the
**Pinned** section (`Row.PinRow`), which `sortRows` puts first whatever
the sort, gone pins last, and the layout gains a `since pin` column
(`ColSincePin`, only while the section has rows): `ccn 42→30 (−12) · cog
31→20 (−11)` (`Pin.SinceText`), in the success colour
(`gridview.Cell.Good`) when a metric fell and none rose, the warning colour
when one rose. A pin the settled snapshot no longer holds stays listed as a
muted `(gone)` row (`Row.Gone`, `-` metrics, left out of the totals and the
CSV) until it is unpinned; nothing is marked gone while the walk runs.

**Session Δ** (#2959). The app records the first settled snapshot of the
index (walk finished, enabled, measurable) per project root in a
process-lifetime map (`complexitySessionBase`, on every
`ComplexityChangedMsg` and panel seed) and hands it to the pane
(`SetSessionBase`). `updateSessionDelta` measures the base under the
current thresholds (per file, #2960), baseline and hide-tests rule (cached
until one changes) and the Project tab's header appends `session Δexcess −120`
(current minus base excess), hidden while it is 0.

**Settings** (`complexity.ccn_warn`, `complexity.cognitive_warn`, Settings >
Complexity, Int 1-200, default 15; `complexity.comment_density_warn`, Int
0-100, default 40; `complexity.overrides`, List, #2960) apply live through
the same config reload that reconfigures the index: the app hands the pane
the globals and, while there are overrides, the resolver
(`SetThresholds(global, res)`, cached per path); every warning colour, over
and excess count, group row, hotspot score and session Δ judges a row by
its own file's set, a directory node of the tree view sums its files'.

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

## Baseline (#2958)

IKE has over a thousand functions above the thresholds. Without a record
of what is *accepted*, the Problems feed lists all of them, the panel's
`over` count and warning colour never change, and a CI policy over
`ike complexity --json` cannot tell old debt from a new offender. Linters
solve this with a baseline (`golangci-lint --new-from-rev`, Sonar's "new
code" focus): record what exists, flag only what is new or got worse. The
baseline turns every surface into a **ratchet**.

**Format.** `.ike/complexity-baseline.json`, project data beside
`.ike/settings.toml` and meant to be committed (not the state store —
`bookmarks.File()` is the other pattern), written and read by
`complexity.Baseline` (`internal/complexity/baseline.go`, pure: `Load`,
`Save`, no root, no setting):

```json
{"version": 1, "ccn_warn": 15, "cognitive_warn": 15, "accepted": [
  {"file": "internal/config/validate.go", "function": "validate", "ordinal": 0, "ccn": 170, "cognitive": 222}
]}
```

One `Entry` per accepted function: the project-relative slash path, the
`Function.Name`, the **ordinal** among the file's same-named units in
source order (`complexity.Ordinals` — the pairing rule of `PairUnits` /
`DiffFunctions`, so a function that moves inside its file keeps its entry
and an overload or `(anonymous)#n` lines up by position) and the metrics at
acceptance. `Save` sorts by file, function, ordinal and indents, so the
committed diff is stable; `LoadBaseline` returns `(nil, nil)` for a missing
file and an error naming the file for bad JSON, an unknown `version` or an
entry without a file / function / non-negative ordinal. The thresholds in
the file are informational (the ones the entries were accepted under),
never applied. A lookup index (`map[EntryKey]int`) is built on first use
and dropped by every mutation.

**Semantics.** `Classify(file, fn, ordinal)` is the one rule every
surface shares: **accepted** when an entry matches `(file, function,
ordinal)` and neither CCN nor cognitive exceeds the recorded value,
**worsened** when an entry matches and a metric grew, **new** otherwise
(no entry, wrong ordinal, renamed); a nil baseline classifies everything
new, so a surface with the baseline off runs the same code. `Stamp(file,
fns)` writes the status into `Function.Baseline` (`json:"-"` — a
classification, not a measurement), and `Summarize` reads it: an accepted
unit over a CCN / cognitive threshold is not in `OverThreshold` and adds
nothing to `Excess`; it is counted in `Summary.Accepted` instead, a
worsened one in `Summary.Worsened` and in `OverThreshold`. The comment
density check is not part of the baseline. `Summary.BaselineStatus` folds
a population (a group row, a JSON group): worsened if any unit worsened,
new if any over-threshold unit is not accepted, accepted if every one is,
nothing when nothing is over or accepted. `Function.Over(ccn, cog)` is
the shared "at or above a threshold" test.

**App.** `internal/app/complexity_baseline.go` holds
`complexityBaselineState` (shared by pointer like `cxDiff`): the file is
read once in `buildModel` and again on every `watch.BaselineChanged`
(`routeWatchEvent` → `reloadComplexityBaseline`) — the watcher classifies
`<root>/.ike/complexity-baseline.json` like `settings.toml`, so a `git
checkout`, a hand edit, a removal and IKE's own save all re-apply (a
re-read of the same content is harmless). `complexityBaseline()` is the
one gate: nil while `complexity.baseline` is off or the file is missing or
malformed. `applyComplexityBaseline` hands it to an open panel
(`SetBaseline`) and republishes the Problems feed; it runs on load, on
every file change, after a toggle and from `reconfigureComplexityIndex`
when the setting flips. A malformed file is reported **once**
(`reportComplexityBaseline`, from the next `ComplexityChangedMsg` or
apply, since `buildModel` cannot toast) and ignored; a later valid load
resets the flag.

**Panel** (`internal/complexitypanel/baseline.go`). `population` classifies
each file's units (`classify`: the rows copy every `Function`, so the
snapshot's slices stay untouched) and records `Row.Ordinal`; a group row
keeps its `Members` (the function rows it aggregates). The header gains
`N accepted`, the footer hint `i accept`. `i` (`complexity.baseline`, bound
in the `complexity` context; "Complexity: Accept / Drop Selected in
Baseline" in the palette) is `ToggleBaseline`: a function row (or a site
row, standing for its function) yields `BaselineToggleMsg{Items, Remove}`
— an accepted function is dropped, a new or worsened one recorded at its
current metrics (a worsened function re-accepted records the new ones); a
group row records every over-threshold member not yet accepted or, when
all of them already are, drops them all. The Changes tab, a function under
the thresholds and a group without an over-threshold member yield nothing
(the app says so). The app's `handleComplexityBaselineToggle` mutates the
state's baseline (created from the current thresholds when there is none),
saves the file (`saveComplexityBaseline`: a failed write keeps the
in-memory change and reports the error) and re-applies.

**Accept all.** `complexity.baselineAll` ("Complexity: Accept All Over
Threshold", keybind-less — `reasonOccasional` in the ledger) counts the
snapshot's over-threshold functions (`overThresholdFunctions`, keyed the
baseline's way) and raises a centered confirmation dialog in the floating
shell, like the forge event dialog (the "prominent actions" convention):
`enter` / `y` writes a **fresh** baseline of exactly those functions — the
file `ike complexity --write-baseline` produces — `esc` / `n` cancels.
With the setting off, another dialog open or nothing over threshold it
notifies instead.

**CLI.** `--baseline[=PATH]` reads a file (the project file is the default
when it exists, so a bare report already applies it; a bare `--baseline`
asks for the project file and fails when it is missing; `--baseline=PATH`
names another, relative to the working directory), `--no-baseline` reads
none, `--write-baseline` writes every over-threshold function of the
report (paths and `--no-tests` respected, row filters not) into the file
and prints `baseline: N functions over threshold (CCN ≥ 15 or cog ≥ 15)
written to .ike/complexity-baseline.json` instead of the report. A
malformed file is exit 2; `--baseline` with `--no-baseline`,
`--write-baseline` with `--diff` or `--no-baseline` are usage errors. With
a baseline in effect the units are stamped before the CCN sort (the
baseline keys on source order), the text table marks accepted rows
`name ✓`, the summary line is followed by `baseline: N accepted, N
worsened (<file>)`, and the JSON gains `baseline: "accepted" | "worsened"
| "new"` on every function, the folded status on every group (absent on a
group with nothing over threshold) and `accepted` on the summary
(`over_threshold` counts only new + worsened). `--diff` is unaffected: it
compares revisions, not debt.

**Setting.** `complexity.baseline` (Settings > Complexity, Bool, default
`true`): off hands every surface nil, on re-applies the loaded file —
live, through `reconfigureComplexityIndex`.

**Tests.** `complexity/baseline_test.go` (round trip and order, malformed
files, the classification table, `Ordinals` / `Stamp` / `Summarize`),
`complexitypanel/baseline_test.go` (statuses, muted ✓ rendering, totals and
group rows, the filter key, `i` on function / site / group rows),
`complexitycli/baseline_test.go` (`--write-baseline`, the default file,
the flags, JSON and text, the malformed file), `app/complexity_baseline_test.go`
(Problems and panel, the setting, the watcher route, a malformed file
reported once, `i`, the dialog), `watch/watch_test.go`
(`TestBaselineFileEvents`).

## Hidden set (#3031)

The baseline is the wrong tool for code that should not be looked at at
all — a downloaded library under `static/lib/*.min.js`, a vendored helper,
the anonymous module initialisers (`(anonymous)#n`) that carry a JS file's
whole complexity and drown the functions a developer owns. An accepted
function keeps its row, its metrics and its share of every aggregate; a
**hidden** one is dropped from every Complexity surface as if it had never
been measured. It still *is* measured, so it can be un-hidden.

**Two levels, two stores.** A **function** hide is a post-measure filter
recorded in the baseline file; a **file** hide is a `complexity.exclude`
glob, so the index never measures the file (`classify` drops it "without
queueing", the Settings form shows the pattern, and the index already
rewalks when the list changes) — one exclusion mechanism, not two.

**Format.** `.ike/complexity-baseline.json` gained a `hidden` section and
**version 2** (`complexity.BaselineVersion`; `LoadBaseline` still reads
version 1, writes version 2, and omits the section while it is empty so a
plain baseline keeps its shape):

```json
{"version": 2, "ccn_warn": 15, "cognitive_warn": 15, "accepted": [],
 "hidden": [
  {"file": "static/lib/app.js", "function": "(anonymous)", "ordinal": 2}
]}
```

`complexity.HiddenSet` (`internal/complexity/hidden.go`) is the section:
`HiddenEntry` is the baseline's key without the metrics — project-relative
slash path, `Function.Name`, ordinal among the file's same-named units
(`complexity.Ordinals`, the pairing rule of the baseline and `PairUnits`),
so `(anonymous)#2` hides exactly the second anonymous unit and a function
that moves inside its file stays hidden. It marshals as the entry array,
sorts and de-duplicates on save, validates like the accepted entries (file,
function, ordinal ≥ 0), and answers `Has`, `Add`, `Remove`, `Mask(file,
fns) []bool` and `Visible(file, fns)`; every method accepts a nil set as
"nothing hidden". `Baseline.Hide` / `Unhide` / `HiddenOf` keep the section
and drop it when it empties. The hidden set and the baseline are
independent: hiding an accepted function leaves its `accepted` entry,
un-hiding restores its status.

**App** (`internal/app/complexity_hidden.go`). The set rides
`complexityBaselineState` — read at build time, re-read on every
`watch.BaselineChanged` — but **not** the `complexity.baseline` setting:
`complexityHidden()` hands every surface the loaded set while the baseline
is on or off, and `x` writes the file either way (a missing file is created
from the current thresholds). `applyComplexityBaseline` hands it to the
panel (`SetHidden`) beside `SetBaseline` and republishes the Problems feed;
`complexityDiagnostics` skips a hidden unit entirely (no CCN / cognitive
and no comment-density row); `overThresholdFunctions` — the bulk accept's
population — skips it too, and both `acceptAllComplexityBaseline` and
`ike complexity --write-baseline` carry the section over when they reseed
the file. A file hide (`HideFileMsg`) appends the globs to the effective
`complexity.exclude` and writes the list through
`configcmd.WriteAndReload` into the layer that already holds the key — the
project file when none does, since a hidden vendor tree is project data —
so `reconfigureComplexityIndex` rewalks without the file; an already
excluded glob says so and writes nothing.

**Panel** (`internal/complexitypanel/hidden.go`). `population` masks each
file's units (`Row.Hidden`; a Changes row is placed in the snapshot through
`changeOrdinal`, like a pin), and `markHidden` — after `markPins` and
`markTests`, before the totals — counts them (`N hidden` in the header) and
drops them unless the filter names the `hidden:` field, so the totals, the
group rows, the tree, the histogram, the CSV / Markdown exports and the
copy text all follow the visible population. Under `hidden:yes` the hidden
rows are listed muted with ` ✕` after the name. Keys, all bound in the
`complexity` context: `x` (`complexity.hide`, "Complexity: Hide / Un-hide
Selected Function or File") is `ToggleHide` — a function row (a site or
candidate row stands for its function, a Changes row for its working-tree
side) yields `HideToggleMsg{Items}`, or `Remove: true` on a `✕` row; a
group, Changes file or tree file row yields `HideFileMsg` with the
project-relative path as the glob, a tree directory `dir/**`; the tree's
root, a Trend row, a removed function and a gone pin are refused with the
reason in the footer. `X` (`complexity.hideFile`) is `HideFile`: the same
file glob from a function row too. `H` (`complexity.showHidden`) is
`ToggleHiddenView`: sets or clears the `hidden:yes` term, so the hidden
functions can be un-hidden without being visible first — the un-hide list
the feature needs. The footer hints `x hide` always and `H hidden` once
something is hidden (`H hidden: shown` in the view); the empty hidden view
says `(nothing hidden on this tab …)`. Hidden *files* are not rows (the
index never measured them): their list is the Excluded files entry of
Settings > Complexity, where a glob is removed again.

**CLI.** `settings.hidden` is the file's section, loaded with the baseline
and — unlike it — kept under `--no-baseline` (hiding is not accepting).
`runProject` skips a hidden unit after `Stamp`, before the rows are built,
so the table, `--by` / `--brief` groups, `--md`, the summary and the JSON
leave it out; the summary line gains `hidden: N functions left out
(.ike/complexity-baseline.json)` and the JSON summary `hidden: N` (omitted
at 0). `--show-hidden` (`ComplexityArgs.ShowHidden`) keeps the rows
instead, marked ` ✕` in the text and Markdown tables and `"hidden": true`
in the JSON, with `hidden: N functions shown (--show-hidden)`. `--diff` and
`--trend` compare revisions and ignore the set, as they ignore the baseline.

**Help sheet** (`internal/app/complexity_help.go`). The window's keyboard
is mostly fixed pane keys no registry scope lists, so — the playground's
pattern (`playhelp.go`, `help.withExtraLeading`) — `complexityHelpGroups`
contributes, while the window is focused, five `Focused` groups that lead
the help overlay: *list* (tab cycle, navigation, enter, `s` / `S`, every
single-letter action, `e` / `E` / `y` / `r`, `/`), *filter row* (typing,
`tab` completion, `enter` / `esc`, the match steps), *filter vocabulary*
(every field of `complexitypanel.Schema` with its doc — `baseline:`,
`hidden:`, `test:`, `metric:`, …, generated so a new field documents
itself), *Changes tab* (`d`, `R`, `b`, `x`) and *agent* (`a`, `A` / `esc`).
A row naming a command shows the chord bound to it in the live keymap
(`shift+x` for a rebound or shifted key), a fixed key is listed verbatim.
Unfocused, it contributes nothing.

**Tests.** `complexity/hidden_test.go` (round trip, version 1 load, the
version bump, ordinal matching, malformed entries),
`complexitypanel/hidden_test.go` (rows / totals / groups / export, the
header count, `hidden:yes` marking and un-hide, `x` / `X` / `H`, the tree
directory glob and root refusal), `complexitycli/hidden_test.go` (default
omission with and without the baseline, JSON and Markdown, `--show-hidden`,
`--write-baseline` keeping the section, flag parsing),
`app/complexity_hidden_test.go` (every surface on `x`, `H` + `x` un-hide,
the setting off, the watcher route, `X` writing `complexity.exclude` and the
rewalk, the help groups and their order in the overlay).

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
descending. `DiffProject(base, head, res)` runs it per path
over two `map[path][]Function`, keeps the files with a non-empty
classification, summarises both sides (a `Summer`, each file under the
thresholds `res` resolves for it, #2960) and renders the
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
`Diff.ByFile()` (#2955) folds the diff per file: one `FileChange` per
file with a non-empty classification — `File`, the `Changed` / `Added` /
`Removed` counts and `Before` / `After`, the `Summary` of every unit
`DiffProject` measured for the file on that side (a removed unit counts on
the base side only, an added one on the head side only; a diff not built by
`DiffProject` sums the classified units alone) — sorted by ΔCCN
descending, then file; `DeltaCCN` / `DeltaCognitive` / `DeltaNLOC` /
`DeltaExcess` read after − before.

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
buffer, `complexity.exclude`, the threshold resolver (#2960: each file's
summaries, the `Δexcess` the verdict and the status segment read, judge it
by its own thresholds) — and launches one
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

**Two revisions (#2961).** `R` also takes a range `A..B`
(`main..feature`, `v1..v2`, `main@{1.week.ago}..main`):
`vcscore.ParseRevRange(spec)` splits it — a single ref is the working-tree
mode, `A..B` the ranged one, a missing end (`a..`, `..b`) or git's
symmetric difference (`a...b`) an error that keeps the previous base and
shows the message in the header without a git call. Both ends resolve
through `ResolveRev`; `vcscore.ChangedFilesBetween(root, base, head)`
(`git diff --name-status -M <base> <head>`, the `CommitFile` records of
`ChangedFiles`) lists the files, and no untracked file joins. The head side
is the head commit's blob (`RevContent`), measured through the same
`cxMeasureRev` as the base and cached per `(sha, path)` beside it; the dirty
buffers, the index snapshot and the disk play no part, while
`complexity.exclude`, the project root and the language rules gate both
sides as before. The header reads `base <A> → <B> · …`
(`Changes.HeadRev` / `HeadSha` carry the head end). A ranged diff ignores
the working tree's movement: its `cxDiffKey` zeroes the snapshot and index
generations and carries a range generation that only a new `R`, a config
reload or `r` (`rearmComplexityRange`) bumps — editing or saving a file
never re-runs it, while `R` with the same range re-resolves the refs. The
`Δcx` segment and the header hints' Δ (#2949) read the stored diff
whatever its mode, so they show the range's numbers while one is active.
The function diff (`d`) reads its right side from the head commit too
(cached like the base text), titled `<name> @ <A> → <B>`, bound to no file.

**Function diff (#2956).** `d` on a Changes function row (a site or
candidate row stands for its parent; file rows, the Project and File tabs do
nothing and their footers do not list it) sends
`complexitypanel.FunctionDiffMsg{Path, Name, Ordinal, Change}` — the
`complexity.Change` with both ranges, the missing side zero on an added or
removed row; `Ordinal` is the unit's index among the file's same-named
units (read off the index's head measurement; a removed unit sits past every
head unit of its name). The palette runs the same as `complexity.functionDiff`
(ledgered `reasonPaneKey`). The app (`complexity_funcdiff.go`) reads the head
text in `Update` (dirty buffer, else disk) and launches a `tea.Cmd` that reads
the base blob at the Changes tab's `Sha` (`vcs.RevContent`, cached per
`(sha, path)` for the session beside the measurement cache), re-measures the
head to re-locate `name#ordinal` (an edit since the last diff run still cuts
the right range), and cuts both texts to the function range ±3 lines. The
excerpt opens in a read-only diff viewer (`Registry.AddDiffModelKey`, placed
by `openDiffLeaf`) whose **model key** is `cxdiff:<path>:<name>#<ordinal>`,
titled `<name> @ <rev> → working tree`; `diff.Model.SetLineOffsets` numbers
each side the way the file does, so `enter` on a hunk opens the editor at the
head function's line (`JumpMsg` adds the right offset). The right side is
bound to the file only while the unit exists there (a removed unit binds
none). Reopening the same function refreshes and refocuses its viewer; a
different function opens another. A function viewer is never the
single-diff-window slot (`diffSlot` / `diffInPane` skip it), does not persist
in the layout (its slot restores as an editor), and closing it — pane or
content tab — hands focus back to the Complexity window.

**Changes per file (#2955).** `b` on the Changes tab cycles none → file
(`changesgroup.go`): one row per `Diff.ByFile()` entry of the visible
units, columns file, changed, added, removed, `Σccn before→after (Δ)`,
`Σcog before→after (Δ)`, `nloc before→after` and `excess
before→after (Δ)`; the section column disappears. The metric columns sort
by |Δ| and `min:` / `max:` read |Δ| of the sort column; `file:`, `fn:`
(the path), `lang:`, `test:` and the negated keys apply. A positive Δ
takes the warning colour. `enter` opens the file at its first changed or
added function (the top when it only lost functions), `y` copies `path:
changed 3 added 1 removed 0 ccn 40→52 (+12) cog 31→44 (+13)`, `e` exports
`file,changed,added,removed,ccn_before,ccn_after,ccn_delta,cog_before,
cog_after,cog_delta,nloc_before,nloc_after`; `space` / `m` have nothing to
expand. The level persists as `changes:file` in the layout `Mode`.

## Trend (#2962)

The fourth tab, **Trend**, is the history epic 0550 deferred: the
project's ΣCCN, Σcog, excess and over-threshold count at each of the last
`complexity.trend_commits` commits, so a week of merges reads as one
sparkline instead of thirty diffs. The engine half is
`internal/complexity/trend.go` over the plumbing in
`internal/vcs/vcscore/trend.go`, the app half
`internal/app/complexity_trend.go`, the rendering
`internal/complexitypanel/trend.go`, the CLI
`internal/complexitycli/trend.go`.

**Points and the incremental walk.** `complexity.ListTrendCommits(repo,
rev, n)` lists the last `n` commits of `rev` (HEAD) along the **first parent
only** (`vcscore.FirstParentLog`, `git log --first-parent -n N
--format=%H%x00%h%x00%ct%x00%s`) oldest first: a merge commit is a point,
the side branch it merged is not — the trend answers "what did `main` look
like after each landing", not "what did every commit on every branch
measure". `complexity.WalkTrend(job, commits)` then measures the project at
every point, returning a `Trend{Points []TrendPoint}` where a `TrendPoint`
is `{Sha, Short, Time, Subject, Files, Summary}` — `Short` git's own `%h`,
`Summary` the same `complexity.Summary` the Project tab and the CLI print
(`Summer` over every measurable file of the tree, each under the
thresholds `job.Res` resolves for it, #2960). The walk is incremental: the
**oldest point reads the whole tree** (`vcscore.TreeBlobs`, `git ls-tree
-r`), filtered through the index's rules — inside the project root (a
project that is a subdirectory of its repository measures its own files
only), a language with a complexity spec, not `complexity.exclude`d, and
`job.Keep` for the CLI's selection and `--no-tests` — and **every later
point applies the blob diff** against the previous one
(`vcscore.ChangedBlobsBetween`, `git diff-tree -r -M --raw`): a modified or
added file is re-measured, a deleted one dropped, a rename moves its entry
(its measurement travels with it when the blob did not change). Thirty
points therefore cost one tree plus twenty-nine small diffs. Blob contents
come through one `git cat-file --batch` process per batch
(`vcscore.BlobContents`), not one `git show` per file, measured by up to
`job.Workers` (4) goroutines; `job.Measure` is the measurement seam the
tests count calls through.

**Cache.** A `complexity.TrendCache` (safe for concurrent use) remembers
two things: the measured functions **per blob id and language** — a file
untouched between two commits keeps its blob, the same bytes under another
extension measure again — and the **file map per commit sha**. A second
walk in the session re-measures nothing it has seen: a new head after a
commit costs that commit's changed files, a shorter or longer window
(`complexity.trend_commits`) costs the points it adds, a walk after the
project moved to another branch only the blobs that branch introduced.
`Reset` drops it; the app resets on `r` and on a config reload, since the
exclude globs decide what the cached file maps hold. `Trend.Measured` counts
the blobs a walk actually read and measured — the telemetry's `files`.

**App half.** `complexity_trend.go` mirrors the Changes tab's pull model:
`cxTrend *complexityTrendState` (shared by pointer like `cxDiff`) holds the
key of the trend the pane shows — `(VCS snapshot generation,
trend_commits, project root, rebuild generation)` — and once per settled
Update pass, while the **Trend tab is on screen** inside a repository
(`complexityTrendWanted`), `complexityTrendSyncCmd` compares it with the
current key and launches `startComplexityTrend` when they differ: the pane
gets `Computing`, the walk runs in a `safego.Go` goroutine
(`runComplexityTrend`: `ListTrendCommits` + `WalkTrend` with the model's
`complexityResolver` thresholds, the index's excludes and the session
cache), one `tea.Cmd` waits for its `complexityTrendMsg`, and a 200 ms
`complexityTrendTickMsg` chain relays `job.Progress` into the pane's
`computing… 12/30` until the result lands or the key moves. A snapshot
refresh after a commit re-walks (cheaply, through the cache); a working-tree
edit refreshes the snapshot too, which costs one `git log` and N cached
points. `applyComplexityTrend` stores the points (or keeps the previous
ones and shows a git error inline), settles the key so a failure does not
retry every pass, and records the telemetry op. `rearmComplexityTrend` ('r'
via `rebuildComplexityIndex`, a config reload via `applyTheme`'s reload
path) bumps the rebuild generation and resets the cache. Outside a
repository the pane is told `NotGit` and nothing runs; a closed tab costs
nothing; a reopened pane is seeded with the session's points
(`seedComplexityTrend`).

**Panel.** `complexitypanel.Trend{Points, Window, Computing, Done, Total,
Ready, NotGit, Err}` arrives whole through `SetTrend`. The header reads
`last 30 commits · Σexcess 4120 → 3980 (-140) · ΣCCN … · Σcog …` (oldest →
newest; `last 7 commits (window 30)` on a young repository, `computing…
12/30` while the walk runs, `trend kept · <git error>` after a failure),
and a **sparkline row** under the filter row — `▁▂▃▄▅▆▇█` scaled between
the lowest and highest excess, oldest left, the selected commit's bar in
the accent colour, a click selecting that commit; it takes the
distribution row's chrome line (`chromeRows` 5), so `D` does nothing here.
The grid lists one row per commit, newest first: `commit` (`%h`), `date`,
`subject`, `funcs`, `Σccn (Δ)`, `Σcog (Δ)`, `excess (Δ)`, `over (Δ)` — the
sum and its change against the previous (older) point, the oldest row
without a Δ; a positive excess Δ takes the warning colour, a negative one
the success colour. The rows ride the shared `Row` as `Row.Trend
*TrendRow` (`Point`, `Prev`, `Index`): `trendCell` / `trendValue` render
and sort them, `trendMatches` applies the filter row — the bare words match
the short sha and the subject, `min:` / `max:` bound the sort column's
value, the function-level keys match no commit — and `trendKey` keeps the
cursor on the commit across refreshes. `s` cycles commit → date → subject
→ funcs → the four Δ columns; the sum columns sort by their **signed Δ**,
so `excess (Δ) ▼` lists the commits that cost the most first; ties keep
newest first. `enter` sends `TrendBaseMsg{Rev: short}` and the app sets the
Changes base to `<short>..HEAD` (the ranged path of #2961) and switches the
tab, so the question "what changed since that commit" is one key away;
`e` exports `commit,sha,date,subject,functions,ccn_sum,ccn_delta,cog_sum,
cog_delta,excess,excess_delta,over,over_delta`; `y` copies `43b2a00
2026-10-14 subject: fns 120 Σccn 410 (+6) Σcog 380 (-9) excess 42 (-12)
over 3 (0)`; `r` recomputes. Grouping, the breakdown, the site marks, the
baseline, the pins, the function diff and the base prompt do not apply —
`trendKey` swallows `b v D space m i p d R` and the footer says `no
grouping or breakdown on this tab`. `t` (hidden tests) is a working-tree
rule and leaves the trend alone. The tab is reachable by `tab` /
`shift+tab` and the header tab bar like the others.

**CLI.** `ike complexity --trend[=N]` (`cli.ComplexityArgs.Trend` /
`TrendCommits`, the window only after `=`, 1–500, else
`complexity.trend_commits`, else 30) walks the repository of the first path
over the named paths' selection with the report's thresholds and
`--no-tests` rule and prints the points **oldest first**: `complexity
trend: last 5 commits (first parent)`, a `COMMIT DATE SUBJECT FUNCS CCN COG
EXCESS OVER` table with `sum (Δ)` cells, and a `totals: <oldest> →
<newest>: ΣCCN a → b (Δ), Σcog …, excess …, over …` line. `--json` prints
`{"commits": N, "points": [{"sha", "short", "time" (RFC 3339 UTC),
"subject", "summary": <the project summary fields>, "delta": {"functions",
"ccn", "cognitive", "excess", "over_threshold"}}]}`. `--trend` with `--diff`,
`--by` or `--brief` is `cli.ErrTrendWithDiff` (exit 2); outside a
repository it exits 2 with git's message. `--churn` and the baseline flags
are accepted and ignored by the trend.

**Telemetry.** One `complexity.trend` op per landed walk (the app's
`applyComplexityTrend`): `ms` the walk's duration, `points` the commits it
measured, `files` the blobs it read and measured (the rest came from the
cache); phase `error` when git failed. No path travels. See
[Usage telemetry](./usage-telemetry.md).

**Why first parent.** Walking every commit would put a feature branch's
twelve intermediate states between two `main` landings and make the
sparkline jump on work that never existed on the branch the user reads; the
first-parent chain is what `git log --first-parent` shows and what a
release history means. The side effect is documented in the user guide: a
squash merge and a merge commit both read as one point, and a commit only
reachable through a side branch never appears.


## Duplicates (#3044)

The fifth tab, **Duplicates**, is the first step of epic 0560 (#3043):
duplicated code — the same logic copied into several places, exactly
what an extraction into a shared helper fixes — found project-wide on the
parse the index already makes, kept current by the index, shown as clone
groups ranked by size × occurrences. The detector is
`internal/complexity/tokens.go` + `duplicates.go`, the index half
`index.go` / `persist.go`, the app half `internal/app/complexity_duplicates.go`,
the rendering `internal/complexitypanel/duplicates.go`. It finds **type-1
and type-2 clones**: exact copies, and copies with renamed identifiers or
changed literals. Near-miss clones with inserted or deleted statements
(type-3) and semantic clones (type-4) are out of scope, as are
cross-language copies.

**Token stream.** `MeasureTokens(langID, lines)` is `Measure` plus, from
the same `highlight.SyntaxTree` snapshot, the file's normalized token
stream: every named node of the measured units in pre-order (the snapshot
keeps the grammar's named nodes and the infix operator, so the stream
encodes the tree's shape, not just its leaves), one 32-bit FNV hash per
node of its normalized spelling:

- an **identifier** leaf — `identifier` and the grammars' `*_identifier`
  variants, `name`, `*_name` — becomes one placeholder whatever its text,
  so renamed locals, parameters, fields and types still match; **except**
  the name of what is *called or accessed* (an identifier directly under a
  `call_expression`, `selector_expression`, `member_expression`,
  `attribute`, `*_call_expression`, `command_name`), which keeps its text.
  Two blocks calling different functions do the same thing only by
  accident, and anonymizing callees drowned this repository's real clones
  in table rows that merely share a shape (1 930 → 1 092 non-test groups at
  50 tokens);
- a **literal** leaf — a kind containing `literal`, `string`, `number`,
  `integer`, `float`, `escape`, `fragment`, `content`, `rune`, `char`,
  `heredoc` — becomes one placeholder per node kind: a changed constant or
  message matches, a string swapped for a number does not;
- a **comment** (any kind containing `comment`) is dropped with its
  subtree;
- every other node hashes its kind plus, for an inner node, its operator
  (`a + b` ≠ `a && b`), and for a leaf of another kind its text too
  (`break_statement`, `primitive_type`, `true`).

Only the **measured units** contribute: tokens outside every function
(package clauses, imports, top-level tables, struct types) are dropped,
and the stream is cut into one **segment per top-level unit** (a closure
is part of its enclosing unit's segment), so a clone never spans two
functions — the fix (#3045) extracts a helper from inside one.
`complexity.Tokens{Lang, Hash []uint32, Line []int32, Seg []int32}` is the
result, ~1.1 tokens per source token (IKE: 4.0 M tokens for 35 500
functions).

**Index.** The walk's per-file value is `fileEntry{Functions, Tokens}`:
one parse yields both, a saved or watcher-changed file is re-tokenized
alone through the same worker, and `View` carries `Tokens
map[string]Tokens` beside `Files`. The streams persist with the index
(`cacheRecord.Tokens`, `SpecVersion` 2): `Tokens.GobEncode` packs the
hashes as raw little-endian words and the lines and segment starts as
varint deltas, a fifth of gob's default struct encoding — the IKE cache
grew from ~9 MB to 25 MB — so a warm start has every stream without
parsing and the first detection of a session needs no walk.

**Detection.** `View.Duplicates(minTokens)` (`Duplicates(files, k)`) is a
pure pass over the streams, per language:

1. every window of `k` consecutive tokens inside one segment is
   fingerprinted with a polynomial rolling hash (base 1 000 003 mod 2⁶⁴);
   the (hash, position) pairs are sorted so equal windows form runs;
2. a run of two or more windows is a **seed**. A seed whose members all
   have the same left-neighbour window, and whose left run holds exactly
   the same members, is skipped — the run one token to the left grows into
   the same group. Members overlapping an earlier member of the same
   segment are dropped, so a periodic block (a table of similar rows)
   never matches itself; members are verified token by token against the
   first, never trusted to the hash;
3. the seed **grows** left and right while every member's next token
   agrees and no member reaches a segment edge or runs into another member
   — the maximal range of that member set;
4. groups with the same occurrences collapse (key: paths + first lines +
   length); a group whose **every occurrence lies inside an occurrence of a
   longer group** is nested and dropped (the repeated rows of a copied
   table, the inner block of a copied function). A group with one
   occurrence outside every longer group's stays: it is a clone in its own
   right, so a 60-token triple overlapping a 100-token pair survives.

The result is `[]CloneGroup{Lang, Tokens, Occurrences []Occurrence{Path,
Line, EndLine, Start, End}}` sorted by `Score()` = tokens × occurrences,
then tokens, then first occurrence; `Lines()` is the longest occurrence's
line count, `Files()` the distinct paths, `Key()` the identity across
detections. On IKE (3 373 files, 35 513 functions, 4.03 M tokens) a
detection at 50 tokens takes **~0.45 s** on the dev machine and yields
10 362 groups (1 092 outside test files); 30 tokens 0.74 s / 42 700
groups, 100 tokens 0.25 s / 1 251 groups. The cold walk with tokenizing
takes 5.7 s for the whole repository (it was ~5 s without).

**App half.** `complexity_duplicates.go` mirrors the Trend tab's pull
model: every `ComplexityChangedMsg` bumps `cxDup.seen` and marks the result
stale; once per settled Update pass, while the Complexity window is open
(any tab — the groups are the window's, not the tab's) and the result is
stale or was found with another `complexity.duplicates_min_tokens`, one
detection runs over `Index.Snapshot()` in a `safego` goroutine
(`app.complexityDuplicates.detect`), a command delivers
`complexityDuplicatesMsg{started, minTokens, groups, ms}`, and
`applyComplexityDuplicates` hands the pane `complexitypanel.Duplicates{
Groups, MinTokens, Computing, Ready}`. A generation landing mid-detection
leaves the result stale, so the same settled pass launches the next run;
a running detection is never doubled. A closed window costs nothing —
opening it (or restoring it from a layout, `seedComplexityDuplicates`)
runs the detector once over the snapshot already held. One
`complexity.duplicates` telemetry op per landed detection (`ms`, `groups`,
`min`).

**Pane.** `TabDuplicates` lists one row per group behind an expansion mark
(`▸ internal/app/app.go:11–41`, the first occurrence's location) with
`score`, `tokens`, `lines`, `occurrences` and `files` columns, sorted by
score descending by default (`fitSort`). `enter` or `space` on a group row
expands it in place into one indented row per occurrence (`path:first–last`,
its own line count); several groups can be open at once (`dupOpen`, keyed
by `CloneGroup.Key`, so a new detection keeps the expansion); `enter` on an
occurrence sends `OpenLocationMsg{Path, Line, EndLine}` — the extraction
candidates' path (#2948), so the occurrence opens with its lines selected;
`space` on an occurrence collapses its parent. `t` drops the occurrences in
test files (`complexity.IsTestFile`) and the groups that fall below two;
`Row.Test` marks a group whose every occurrence is a test file. The filter
row's `file:` matches any occurrence's path, `lang:` the group's language,
`test:` the mark, `min:` / `max:` bound the sort column, bare words match
the locations; the function-level keys (`fn:`, `kind:`, `class:`, `depth:`,
`params:`, `nloc:`, `baseline:`, `pinned:`, `hidden:`) match nothing.
`y` copies `dup 120 tokens × 2: a.go:11–41, b.go:6–36` (an occurrence:
`a.go:11–41 (dup 120 tokens × 2)`), `e` exports one CSV row per occurrence
(`group, score, tokens, lines, occurrences, files, path, line, end_line`),
`E` the Markdown table of the group rows. The header reads `3 groups · 7
occurrences · ≥ 50 tokens` (`detecting…` while the detector runs); the
empty text names the state (`detecting duplicates…`, `no duplicate of at
least 50 tokens`, `every duplicate involves a test file — t shows them`).
Grouping, the tree, the breakdown, the marks, the baseline, the pins, the
function diff, the hidden set and the base prompt do nothing here
(`dupKeyPress` swallows their keys): a row is a clone group, not a
function.

**Extract with the agent (#3045, #3046).** `a` on a group row (or one of
its occurrences) hands the group to the simplify agent
([below](#agent-2964)) instead of a function: `dupAgentRequest` builds a
`complexitypanel.AskAgentMsg` with `Dup` (every occurrence of the group,
the test-file hiding aside), `Tokens`, `Path` the first occurrence's file,
`Line` / `EndLine` spanning that file's occurrences and `Name` `2
duplicates of lines 5–7` (the first copy; a group spanning several files:
`3 duplicates of a.go:5–7 across 2 files`). `AskAgentMsg.Paths()` lists the
files the run may change — `Path`, then every other occurrence's file. The
root model reads each file once (the open buffer, else the disk) and builds
`agentask.ExtractPrompt`
(`internal/agentask/extract.go`): the ask marker, `File: <path> (<lang>)`,
`Duplicate code: N occurrences of the same T tokens`, each occurrence fenced
under `Occurrence i, lines a–b:`, the whole file for context when it fits
`MaxSimplifySource`, then `ExtractInstruction` — one unified diff that adds
a shared helper and replaces every occurrence. The run, the no-diff
follow-up, `complexity.agent_apply`, the not-applied scratch, cancel (`esc`
on the group row, `A`) and the notifications are the simplify run's; the
status segment reads `[agent: extract 2 duplicates of lines 5–7 m:ss]`,
the running group's location ends in `⟳`, the footer adds `a extract`, the
scratch is headed `# Extract …` with the occurrences' ranges, and the
applied notification names the group instead of re-measuring a function.

A group spanning **several files** (#3046) gets the multi-file prompt
(`Extract.Files`): `Files: a.go, b.go (<lang>)`, `… T tokens in K files`,
each occurrence under `Occurrence i, <path> lines a–b:`, every whole file
(`The whole file <path>, for context:`) while their texts fit
`MaxSimplifySource` together, then `ExtractFilesInstruction` — the helper
goes into one of the listed files, only those files may change, one
`--- a/<path>` / `+++ b/<path>` section per changed file. Its diff is
applied to every changed buffer **all-or-nothing**
([Apply](#agent-2964)): a section for a file outside the group refuses
(`also changes c.go, which holds no occurrence of the clone group`), a
miss in any file changes no buffer (`does not apply cleanly (hunk 1 of 1
in b.go)`) and opens the scratch in front. On success every changed file
is open with one unsaved edit (`u` in each buffer reverts its part), the
request's own file in front; the scratch lists `Occurrences: a.go:5–7,
b.go:12–14`, the notification `… across 2 files into a shared helper —
changed a.go, b.go`. Only a group of fewer than two occurrences is refused
(`DuplicateAgentRefusal`).

**Setting.** `complexity.duplicates_min_tokens` (Int, 1–10000, default 50,
Settings ▸ Complexity, `config.Complexity.DuplicatesMinTokens`) is the
smallest clone reported, in normalized tokens — about 8–10 lines at 50.
`config.Validate` falls back to 50 outside the range with a diagnostic;
the form rejects non-numeric input and clamps. A change re-runs the open
window's detection on the next settled pass.

## Problems feed (#2920)

`internal/app/complexity_problems.go` publishes every over-threshold
function — every *new or worsened* one once a baseline is in effect
(#2958, [below](#baseline-2958): the builder takes the
`complexity.Baseline` and the index root, classifies each file's units by
`Classify` and skips the accepted ones; a worsened row ends in
`(worse than the baseline)`) — into the [Problems tool window](./problems.md) through the store's
task-source channel (`Store.SetTaskSource("complexity", byPath)`), the same
seam the dependency scanner uses. On every `ComplexityChangedMsg` and every
config reload `feedComplexityProblems` reads the index snapshot and the
pure builder `complexityDiagnostics(view, resolver, baseline, root)` emits one
warning per function at or above its file's threshold (the globals or the
matching `complexity.overrides` entry, #2960 — the message names the threshold
in force), anchored at its header line:
`function <name>: CCN 40 ≥ 15`, `function <name>: cognitive 25 ≥ 15`, or
both joined by a comma when both are over; `Source` is `complexity`,
`Code` the first exceeded metric (`ccn` before `cognitive`). A body whose
comment density reaches the file's comment threshold (#2934; `0` = off)
is a separate warning on the same line, `function <name>: comment density
55% ≥ 40%` with `Code` `comments`. Each publish
replaces the source wholesale, so a raised threshold or a simplified
function drops its row and a lowered threshold — or a reloaded override —
adds rows without a restart.
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
wants hints for one state key `cxHintKey{version, gen, th, enabled, mode, sites, show, diff, diffGen}`
— the buffer's `DocVersion`, the index generation the hints came from, the
file's resolved thresholds (`th`, #2960 — so an override reload repaints
exactly the files whose set moved), the master switch, the hint and site-mark modes
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

**Hints.** `complexityHintsFor(fns, th, …)` emits one
`editor.ComplexityHint{Line, Text, Warn, Deltas}` per function at its header line
(the first unit on a line wins, so a lambda beside its header does not
replace it); `Warn` when CCN, cognitive or the comment density reaches its
threshold, which paints the hint in the warning tone. The density joins the
text — `CCN 3 · cog 2 · cmt 55%` — only at or above
the file's comment threshold (#2934), so the common hint stays short;
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
(the file's resolved thresholds, #2960), and of
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

**Intentions (#2965, `complexity_intention.go`).** `alt+enter` on a function
header — or anywhere inside a function, the innermost measured unit holding
the caret (the File tab follow's lookup) — adds the `complexity` provider's
entries behind the LSP actions: *Show complexity breakdown*
(`complexity.breakdownAtCaret`, a function with sites), *Open in Complexity
window* (`complexity.openAtCaret`), *Accept into* / *Remove from complexity
baseline* (`complexity.baselineAtCaret`; accept only for an over-threshold
function, omitted with `complexity.baseline` off), *Pin* / *Unpin in
Complexity window* (`complexity.pinAtCaret`) and *Ask agent to simplify*
(`complexity.askAtCaret`). Outside a function, in an unmeasured file or
with `complexity.enabled` off there are none. Each command opens the window
on the File tab, selects the caret's row (`complexitypanel.SelectCaret`) and
delegates to the pane's own action (`ToggleSites`, `ToggleBaseline`,
`TogglePin`, `AskAgent`). The provider reads the model's index, baseline and
pins, so `openIntentions` builds it per call instead of registering it. No
chords: the keybind ledger records them with `reasonIntention`.

## Headless CLI (#2922)

`ike complexity [--diff[=REV|A..B]] [--by class|file] [--brief[=class]] [--json | --md] [--min N] [--ccn N] [--cog N] [--cmt N] [--no-tests] [--sites] [--churn[=DAYS]] [--baseline[=PATH] | --no-baseline] [--write-baseline] [--pins] [--show-hidden] [--trend[=N]] [--width N] [PATH...]`
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

**Line width (#2992).** Every text table — the project and pins table, the
`--by` group tables, the `--brief` lines, the `--diff` sections and its
`--by file` table, the `--trend` commits — is written through
`settings.newTable` (`internal/complexitycli/table.go`) instead of a bare
`tabwriter`: it buffers the tab-separated lines and on `Flush` fits them
into `ComplexityArgs.Width` with the tool window's rule — numeric columns
keep their natural width, the text columns (paths, function and class
names, commit subjects) share the rest through `gridview.FitWidths` (minimum
12) and are truncated with the same ellipsis rules (paths from the left,
names from the right with ` ✓` / ` ★` kept, a leading indent kept), then
aligned two spaces apart with widths measured by `lipgloss.Width`. The
limit is `--width N` (`cli.WidthMin` 40 or more; `--width 0` is unlimited —
the plain `tabwriter` output of before; `--width 20` is a usage error, exit
2). Without the flag `cmd/ike` measures stdout (`charmbracelet/x/term`
`GetSize`) and `complexitycli.ResolveWidth` sets the terminal width, or
`cli.DefaultWidth` (120) when stdout is not a terminal or its size is
unknown; `Run` itself never probes, so tests pass the width. The summary,
trailer and verdict lines are prose and stay unwrapped; `--json` and `--md`
ignore the limit.

**Markdown (#2963).** `--md` (exclusive with `--json`:
`cli.ErrMarkdownWithJSON`, exit 2) renders every report through
`complexitycli.WriteMarkdown` (`internal/complexitycli/markdown.go`): a
`Markdown{Headline, Notes, Sections}` value, the headline bold, each
`MarkdownSection{Title, Header, Numeric, Rows}` a GFM table with the numeric
columns right-aligned and the rows past `MarkdownFold` (30) in a
`<details>` block; `MarkdownEscape` escapes `|`, backticks, `*` and `<` and
folds newlines. `projectMarkdown` / `groupsMarkdown` reuse the text
summary and trailer lines (`trailerLines`), `briefMarkdown` is the brief's
columns without a headline, `diffMarkdown` the PR-comment payload —
`Complexity: MORE COMPLEX (excess +2, …)`, the revisions, the totals, the
Changed / Added / Removed tables (cells `5→7 (+2)`) or the Files table under
`--by file` — and `trendMarkdown` the commit table. The types are exported
for the tool window's `E`.

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
config, `0` = off) and `complexity.overrides` (#2960): every file's rows, the
summary, the groups, the diff and `--write-baseline` judge it by its
resolved thresholds, and the summary's threshold text counts the entries
(`CCN ≥ 15 or cog ≥ 15 or cmt ≥ 40% · 2 overrides`). `--ccn` / `--cog` /
`--cmt` (percent, 1-100) override the thresholds — each beats the
overrides too, for its metric. The row filter: `--min N` keeps rows with CCN or cognitive ≥
N; an explicit `--ccn` / `--cog` / `--cmt` keeps rows at or above any
threshold; no filter flag keeps every row. The table carries a `CMT%`
column, the summary line the aggregate density, the thresholds and both
metrics' percentiles (`ΣCCN 136390 (avg 4.0, p50 2, p90 9, p99 28, max
168)`, #2953). The summary always covers every function.
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
An unknown level, or `--by class` with `--diff`, is a usage error (exit 2,
`--by class is not supported with --diff`); `--by file` with `--diff` is the
per-file diff (#2955, under **Diff**). Without `--by` the output is
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

**Baseline (#2958).** `--baseline[=PATH]` / `--no-baseline` /
`--write-baseline` (`ComplexityArgs.Baseline`, `BaselinePath`,
`NoBaseline`, `WriteBaseline`; the path only after `=`) apply or seed the
accepted-debt file — see [Baseline](#baseline-2958) for the flags, the
`✓` rows, the `baseline:` line and the JSON fields.

**Pins (#2959).** `--pins` (`ComplexityArgs.Pins`; with `--diff`, `--by`
or `--brief` a usage error, `cli.ErrPinsWithDiff`) keeps only the pinned
functions of the [pin file](#tool-window-2917) — read from
`complexity.PinsPath(root)`, a missing file is no pins, a malformed one
exit 2 — and adds `PINNED_CCN  PINNED_COG  ΔCCN  ΔCOG` columns and a
`pins: N pinned, N shown, N gone (<file>)` trailer (`pins: none` without
a file). Every report marks pinned rows `name ★` in the text table and
gives every pinned function — project `functions`, diff `changed` (head
side) and `added` — `pinned: {ccn, cognitive, pinned_at}` in the JSON
(`diffReport.head` keeps the head measurement for the ordinals).

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

**Hidden set (#3031).** The baseline file's `hidden` section applies to
every project report by default, with or without `--no-baseline`: a hidden
function is skipped after `Stamp`, so the table, the groups, `--brief`,
`--md`, the summary and the JSON leave it out, and the text adds
`hidden: N functions left out (.ike/complexity-baseline.json)` under the
summary (JSON: `summary.hidden`). `--show-hidden` (`ComplexityArgs.ShowHidden`)
lists them instead, marked ` ✕` (JSON: `"hidden": true` per function, the
line reads `shown (--show-hidden)`). `--write-baseline` keeps the section.
See [Hidden set](#hidden-set-3031).

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

**Diff.** `--diff[=REV|A..B]` (default `HEAD`) runs synchronously the same git
plumbing as the Changes tab — `vcscore.ResolveRev`, `vcscore.ChangedFiles`
(the bubbletea-free core `vcs` re-exports, #2939), the status snapshot's
untracked files (a collapsed untracked directory is walked) and
`vcscore.RevContent` per base blob — restricted to the selection (below a
named directory and measurable by the index's rules, or a named file with a
spec; a named file inside a collapsed untracked directory is added even
when the walk would skip it), then `complexity.DiffProject`; a named file
without changes yields an empty diff and the `unchanged` verdict. The head side is read
uncapped like the tab's, so a file over the walk's cap does not read as
all-removed. `--diff=A..B` (#2961) compares two commits instead —
`ParseRevRange`, both ends through `ResolveRev`,
`vcscore.ChangedFilesBetween`, the head side from `RevContent` at B — and
reads neither the working tree nor the untracked files; a malformed range
or an unknown ref exits 2 with the message. The text header reads
`complexity A (sha) → B (sha)`, and the JSON carries `head_rev` /
`head_sha` beside the base's `rev` / `sha` (`"working tree"` and `""` in
the working-tree mode). The text report prints `changed` (before→after with Δ per
metric), `added` and `removed` sections, the totals and
`verdict: LESS COMPLEX (excess -12, Σcog -7, ΣCCN +1)`; the totals line
and the project summary line carry `excess` too, the group table an
`EXCESS` column. With `--by file` (#2955) the sections give way to a
`files (n):` table of `Diff.ByFile()` — `FILE`, `CHANGED`, `ADDED`,
`REMOVED`, `CCN`, `COG`, `NLOC`, `EXCESS` as before→after (Δ), ΔCCN
descending, a file kept when the row filter keeps one of its units — and
the JSON adds `files` (`file, changed, added, removed, ccn_before,
ccn_after, ccn_delta, cog_before, cog_after, cog_delta, nloc_before,
nloc_after, excess_before, excess_after, excess_delta`) beside the
unchanged `changed` / `added` / `removed` / `verdict`; without the flag
the JSON has no `files` key.

**JSON.** Field names are snake_case and stable (documented in
`userdocs/guides/complexity.md`; add, never rename): the project report is
`{summary, functions, skipped}` — with `--by`, `{summary, groups, skipped}`,
each group `{file, class (class level only), functions, nloc, ccn_sum,
ccn_avg, ccn_max, cog_sum, cog_avg, cog_max, over_threshold, excess,
comment_lines, comment_density}` under the summary's field names — the diff `{rev, sha, head_rev, head_sha, base, head, delta,
changed, added, removed, verdict, skipped}` with `verdict` one of
`more_complex`, `less_complex`, `unchanged` (read by the #2945 rule above;
`delta.ccn` keeps the old ΣCCN policy available). `excess` (#2945) sits on
every summary (`summary`, `base`, `head`), the top-level `delta`, the
delta of every changed function and every group. Line numbers are 1-based.
`comment_lines` and `comment_density` (a ratio, four decimals) sit on every
function, both sides and the delta of a changed function, the summaries and
the top-level delta; the summaries also carry `cmt_warn` (#2934). Every
summary carries `ccn_p50` / `ccn_p90` / `ccn_p99` / `cog_p50` / `cog_p90` /
`cog_p99` and `histogram: {"ccn": [5 ints], "cognitive": [5 ints]}` over
the `1–5` … `26+` buckets (#2953); the group rows do not. The CLI
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

## Agent (#2964)

Two bridges to a coding agent, both built on epic 0540's headless runner
(`internal/agentask`) and Claude Code hooks (`agent.hooks.install`).

**Simplify on request.** `a` on a function row of any tab (a site or
candidate row stands for its parent; a group, file, removed or Trend row
refuses with a footer note) sends `complexitypanel.AskAgentMsg{Path, Line,
EndLine, Name, CCN, Cognitive, Lang, Fn}`; `complexity.askAgent` is the
palette's spelling (keybind-less, `reasonPaneKey` in the ledger). The app
(`complexity_agent.go`) cuts the function's source out of the open buffer
(the unsaved text when the file is open) or the disk, converts the row into
`agentask.Simplify` — path, language, range, metrics, the sites (#2946) and
the best extraction candidate by saving (#2948) — and builds the prompt with
`agentask.SimplifyPrompt`: the ask marker, `File: … (lang), lines a–b`,
`Function: name (CCN n, cognitive m)`, the site lines (capped at
`MaxSimplifySites`), the candidate line, the source fenced in its language
(capped at `MaxSimplifySource`), then the fixed `SimplifyInstruction`
(*reduce the cognitive complexity … without changing its behaviour; keep
the public signature; keep tests green. You have no tools and cannot read
files; the complete function source is in the fenced block above. Answer
with the unified diff against the file first, in a ```` ```diff ```` fence,
then one paragraph*). The tool-lessness is spelled out since #3009: the
CLAUDE.md files Claude Code loads automatically pushed a model into printing
a textual `Read` call as its whole answer. The argv is
`agentask.SimplifyCommand` — `agentask.Command` without `--resume` /
`--fork-session` (`FreshCommand`) and with `SimplifySystemPrompt` appended
instead of the explain-only one: a fresh `claude -p` session on the
`agent.ask.*` model and turn limit, tool-less, JSON output — run by
`agentask.RunSimplify` in the project root off the Update loop.

**Answer validation and the one retry (#3009).** `agentask.ParseAnswerDiff`
reads the first ```` ```diff ```` / ```` ```patch ```` fence, else a bare
`--- a/… / +++ b/…` block, into file sections of `agenttrace.DiffHunk`s
(`@@` starts a hunk; ` `, `-`, `+` rows; a blank line is an empty context
row; `\ No newline`, `diff` and `index` lines are skipped; the header counts
are informational for the apply). The parser also flags a diff that **leaves
lines out** (#3050, `AnswerDiff.Elided`): a row that is a placeholder for
lines — `...`, `…`, `[...]` alone or after a comment opener, or `...`
followed by a space or parenthesis (`... (whole kind switch, lines 494–639,
moved verbatim into helpers below)`; a spread like `...rest` is code) — or
a hunk clearly shorter than its `@@` header counts (fewer before or after
rows by more than a tenth and at least three lines, since models miscount by
one or two). A long function tempts the model into exactly that, and such a
diff can never match the buffer. Every diff-asking instruction therefore ends
with `agentask.DiffInFull` (*Write every line of the diff in full … never
shorten it with "...", "[...]" or an "unchanged" note*). An answer without a
hunk, or with an elided diff, gets exactly one follow-up in the same session
— `agentask.SimplifyFollowUp`, `FollowUp` resuming the run's session id with
`NoDiffFollowUp` (*Your answer contained no unified diff. Answer with the
diff now.*) or `ElidedDiffFollowUp` (*Your diff left lines out … Answer with
the complete diff now, every line written in full.*); a second diff-less or
elided answer is final — the elided one goes on to the apply, which misses
and says why (below). `RunSimplify` takes the runner as a parameter
(`agentask.Runner`), which is the seam the tests fake.

**Apply (#3009, `complexity.agent_apply`, default on).** The agent stays
tool-less; IKE applies its diff, so the change lands as **one undoable edit
in the open buffer, dirty and unsaved** — reviewed in the editor with the
gutter diff, reverted with `u`, no file written behind the user's back and
no git state touched. `cxAgentFileHunks` sorts the sections onto the
request's files (`AskAgentMsg.Paths()`: the function's file, or a clone
group's files, #3046): a section belongs to the file it names or whose
suffix it names, a section naming no file to the only file; a section for
another file — or, with several files, one naming no file or matching
several — refuses the apply. `agenttrace.Apply` is the agent-trace revert's splice run
forward (`spliceHunks`, shared with `Revert`, agent-trace.md "Revert agent
change"): each hunk's before lines (context and removed) must occur exactly
once and are replaced by its after lines, last hunk first; a miss returns a
`*agenttrace.HunkError` naming the hunk (`hunk 2 of 3`) with
`agenttrace.ErrNoMatch` (*the diff's lines do not occur in the file — it does
not match the buffer*; the revert's `ErrConflict`, *it was edited since*,
described the wrong direction, #3050) and nothing is applied. The check runs first against the buffer, or the disk when the file
is not open; only then the scratch (below) opens, the function's file opens
after it — so it is the tab in front and the editor sits on the function's
header — and the edit is made last through `applyBufferRestore`, the local
history's one-change restore. That order matters: a tab switch *after* the
edit would hand the dirty buffer to `editor.auto_save = "focus"`, and the
apply never saves. With several files (#3046) the same steps run per file
and stay all-or-nothing: every file is checked before anything opens, the
changed files open with the request's own file last (in front), every new
text is computed before the first `applyBufferRestore`, so a miss in any
file leaves every buffer as it was. A cmd off the loop re-measures the new text
(`cxMeasureText`, the function found again by name nearest its old header)
and `cxAgentAppliedMsg` notifies `complexity: applied the agent's diff to
<fn> (−n excess) — CCN a → b, cognitive c → d; u reverts, save writes it`,
the excess taken under the file's thresholds. The Complexity window follows
through its normal buffer-change path. A hunk that does not match opens the
scratch in front with `complexity: the agent's diff does not apply cleanly
(hunk 2 of 3) — opened as <scratch>`, so `git apply --3way` stays the
fallback; when the diff leaves lines out (#3050) the toast adds `, the diff
leaves lines out` and the scratch's note names them (*The diff leaves lines
out — hunk 1 stands a placeholder in for lines ("... (rest unchanged)"), also
after the follow-up asking for the complete diff — so it cannot match the
file.*); a diff-less answer after the retry opens it with `complexity: the
agent answered without a diff — opened as <scratch>`. With
`complexity.agent_apply` off the answer only opens as the scratch, as
before #3009.

**The scratch.** A `.md` scratch (`cxAgentScratch.body`): a heading naming
the function, its location and metrics, `Model: … · session log: …`, a
quoted line saying what happened to the diff, then the answer with a bare
unified diff fenced as ```` ```diff ```` (`fenceDiff`). It opens in a
background tab behind the function's file after a successful apply (the
documented choice over putting the explanation into the notification), in
front otherwise.

**Progress and cancel (#3009).** A run is a `cxAgentRun` in
`cxAgent.running` (keyed `complexitypanel.AgentKey`: path + name + header
line; a sequence id per run), carrying its `context.CancelFunc`, model and
start time. While any run is in flight the status line's `complexityagent`
segment shows `[agent: simplify <fn> m:ss]` (the oldest run named, `+n` for
the others) — also in the line a focused tool pane gets, so it is visible
while the Complexity window has the focus. Its clock is repainted by `cxAgentTickMsg`, a one-second tick
armed on demand by the run's start, re-armed only while a run is in flight
and stamped with the model generation — the idle rules of performance.md;
an idle session never wakes for it. The tick also re-syncs the pane's marks
(`SetAgentRunning`), so a window opened mid-run picks them up: the running
function's row leads its name with `⟳` and the footer adds `esc cancel
agent`. `esc` on that row (`complexitypanel.CancelAgentMsg`) or
`complexity.cancelAgent` (palette; `A` in the Complexity context; the
selected row's run, else every run) cancels: the process is killed through
the context, the slot is freed at once — `a` on the row starts a fresh run —
and `complexity: cancelled the agent for <fn>` is notified; the cancelled
run's result is dropped by its sequence id. One run per function at a time:
`a` again while it runs only notifies and names the ways to cancel.

**Failure, logs, telemetry.** A missing `claude` reuses agent.ask's install
message; a failed run notifies with the runner's error. Every notification
after a run carries a second line `model <m> · session log
~/.claude/projects/<project>/<session-id>.jsonl` (`cxSessionLog`:
`agenttrace.ProjectsDir`, `EncodeCWD` of the project root, the id from the
JSON result; the home directory shortened to `~` through `filepath`, so
Windows paths work alike), and the scratch header names the same. The
sessions the run leaves behind are tagged by the ask marker and their ids
are added to `askForks`, so trace discovery skips them. Each finished run is
one `complexity.agent` telemetry op: `outcome` applied / not_applied /
no_diff / scratch / failed / cancelled, `ms`, `retried`; the phase is error
for failed, canceled for cancelled.

**Verdict on Stop.** With `complexity.agent_verdict` on, the `Stop` hook
event — Claude Code's end-of-turn hook, now one of `agenttrace.HookEvents`
and `deeplink.AgentEvents` — makes `handleAgentEvent` schedule
`complexityVerdictCmd` (`complexity_verdict.go`) beside the terminal
binding, whether or not a terminal matched. Off the loop it detects the
session cwd's repository and runs the Changes tab's diff job
(`runComplexityDiff`, the `--diff` equivalent) against `HEAD`: the full job
of `complexityDiffJob` — snapshot, index, dirty buffers, base cache, the
per-file thresholds — when that repository is the project's own, else a
bare job on the detected root. The result is worded by
`complexityVerdictText` as `complexity: LESS COMPLEX (excess −12)` (the
excess delta always; `, cognitive ±n` when the excess held and the cognitive
sum decided) and posted as `cxVerdictMsg`: the note is filed per session id
(`traceNotes`, the last 50) and, when the trace pane shows that live
session, the tree is re-set with the note appended under the last turn as an
`agenttrace.NodeNote` row (`Warn` for a positive excess delta → the warning
colour through `hiertree.Entry.Warn`); `handleTraceRead` re-applies the
notes to every rebuilt tree (`traceWithNotes`). A pane not showing the
session gets a toast instead. The hook process itself is unchanged: it
forwards the event and exits 0 at once — the diff never blocks the agent.
Off, nothing runs. Users who installed the hooks before #2964 run
`agent.hooks.install` again to add the `Stop` entry.

## Settings

All keys live in the `[complexity]` section (Settings > Complexity, user
scope), validated in the form and by `config.Validate`:

| Key | Default | Meaning |
|---|---|---|
| `complexity.enabled` | `true` | Master switch for the index and every surface reading it. |
| `complexity.exclude` | `[]` | `internal/pathglob` patterns of files never measured; `x` / `X` on a file, group or tree row in the window append one (#3031). |
| `complexity.ccn_warn` | `15` | Cyclomatic warning threshold (1-200): tool-window colour, hints, Problems, over-threshold count. |
| `complexity.cognitive_warn` | `15` | The same for cognitive complexity. |
| `complexity.comment_density_warn` | `40` | Comment-density threshold in percent (0-100, `0` = off): `cmt%` colour, the hint's ` · cmt 55%`, the Problems `comments` row, `--cmt` default, over-threshold count. |
| `complexity.problems` | `true` | Publish over-threshold functions into Problems. |
| `complexity.status_segment` | `false` | Show the `Δcx` status segment. |
| `complexity.churn_days` | `90` | Hotspot window in days (1-3650): the commits the `commits` / `hot` columns count; `--churn`'s default. |
| `complexity.trend_commits` | `30` | Trend window in commits (1-500, #2962): how many first-parent commits the Trend tab and `--trend` measure the project at. |
| `complexity.baseline` | `true` | Apply `.ike/complexity-baseline.json` (#2958): accepted functions leave the over counts, Problems and the warning colour; off ignores the file everywhere. |
| `complexity.agent_verdict` | `false` | Diff the working tree against `HEAD` on the agent's `Stop` hook event and post the verdict into the Agent Trace window (#2964); off runs nothing. |
| `complexity.agent_apply` | `true` | Apply the simplify agent's diff (`a`) to the function's open buffer as one undoable edit and report the metrics before → after (#3009); off only opens the answer as a scratch. |
| `complexity.duplicates_min_tokens` | `50` | Smallest clone the Duplicates tab reports, in normalized tokens (1-10000, #3044): exact copies and copies with renamed identifiers / changed literals, within and across files of one language. |

## Limitations

- **Duplicates are type-1 / type-2 only** (#3044): a copy with an inserted
  or deleted statement breaks into the matching pieces (or below the
  threshold, into nothing), semantically equal code written differently is
  never found, and copies across languages are not compared. Only code
  inside measured functions takes part — duplicated top-level tables and
  type declarations do not — and a clone never spans two functions. The
  normalization keeps callee and member names, so two blocks that differ
  only in which function they call are not clones.
- **The trend follows the first parent only.** A commit reachable only
  through a merged side branch is never a point; a merge and a squash merge
  both read as one. The trend measures every file of the tree, so `t`
  (hidden tests) does not narrow it; `--no-tests` on the CLI does.
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
- **The baseline keys on name and ordinal.** A renamed function, or a
  same-named unit inserted above an accepted one, reads as new (and the
  one below it as a different ordinal) until re-accepted — the same
  positional rule as the diff.
- No trend history, no enforcement: IKE reports, policy belongs to CI (the
  baseline is the ratchet CI reads, not an enforcement of its own).

## Extending

A new language declares its `lang.ComplexitySpec` (see
[languages](./languages.md#complexity-spec-2914) and the
[change workflow](/process/change-workflow.md)) and gets a fixture directory
here: `testdata/<lang>/fixture.<ext>`, an entry in `fixtureLang`, a golden
generated with `-update` and reviewed against the language's reference tool,
with any deliberate difference recorded in `golden_test.go`.
