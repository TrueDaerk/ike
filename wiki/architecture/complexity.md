---
type: concept
title: Code Complexity
description: The code-complexity engine (Roadmap 0550, #2913) — cyclomatic (McCabe) and cognitive (Sonar) complexity, NLOC, parameters and nesting depth per function, measured on the Tree-sitter parse through each language's ComplexitySpec; the rules, the walker, the per-language golden fixtures cross-checked against lizard, gocyclo, gocognit and complexipy, and the summary the later project index, diff and tool window build on.
resource: internal/complexity
tags: [architecture, complexity, metrics, tree-sitter, languages, cyclomatic, cognitive]
timestamp: 2026-10-12T23:30:00Z
---

# Code Complexity

Roadmap 0550 measures the **cyclomatic** (McCabe, CCN) and **cognitive**
(Sonar) complexity of every function in the project, shows it in a tool
window, and answers *"did this change make the project more or less
complex?"* by comparing the working tree against `HEAD`. Everything runs on
the Tree-sitter parse IKE already has — no lizard, gocyclo or gocognit on the
machine — so it works for every language with a grammar, in mixed projects,
out of the box. This page documents the **engine** (`internal/complexity`,
#2915); the language seam it reads is
[languages](./languages.md#complexity-spec-2914), and the project index, the
diff against HEAD, the tool window, the editor hints, the Problems feed, the
status segment and the CLI are the epic's later steps.

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

## Extending

A new language declares its `lang.ComplexitySpec` (see
[languages](./languages.md#complexity-spec-2914) and the
[change workflow](/process/change-workflow.md)) and gets a fixture directory
here: `testdata/<lang>/fixture.<ext>`, an entry in `fixtureLang`, a golden
generated with `-update` and reviewed against the language's reference tool,
with any deliberate difference recorded in `golden_test.go`.
