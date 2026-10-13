# Code complexity

IKE measures the **cyclomatic** (CCN) and **cognitive** complexity, the
non-comment lines (NLOC), parameters, nesting depth and comment density of
every function in the project — from the syntax tree it already has, with no external tool
installed. Inside the IDE the **Complexity** window (`cmd+alt+shift+x`)
lists every function and, on its Changes tab, compares the working tree
against a revision. The same report is available without a terminal UI, for
CI pipelines and coding agents: `ike complexity`.

Settings live under `[complexity]` (Settings ▸ Complexity): `exclude` globs,
the warning thresholds `ccn_warn` / `cognitive_warn` (15 by default) and
`comment_density_warn` (40 % by default, `0` turns it off).

## Reading the numbers

| Column | Meaning |
|---|---|
| **CCN** | Cyclomatic complexity (McCabe): 1 + one per branch (`if`, loop, `case` arm, `catch`, ternary) + one per `&&` / `\|\|`. Roughly the number of test cases needed to cover every path. |
| **cog** | Cognitive complexity (Sonar): how hard the function is to *read*. Each branch costs 1 plus its nesting depth, `else` costs 1, a run of like boolean operators costs 1, as do jumps (`goto`, labelled `break`) and direct recursion. Deep nesting is what drives it up. |
| **nloc** | Lines that are neither blank nor comment-only. |
| **params** | Parameter count. |
| **depth** | Deepest nesting level inside the function. |
| **cmt%** | Comment density: the share of the function *body* that is comment-only lines, `comment lines / (nloc + comment lines)`. |
| **excess** | How far a function sits *above* the thresholds: `max(0, CCN − ccn_warn) + max(0, cog − cognitive_warn)`. A function at 20 / 30 with both thresholds at 15 has excess 20; anything at or below both has 0. The header and the summaries sum it — the number to drive down when optimising. |

Values at or above `ccn_warn` / `cognitive_warn` show in the warning colour,
as a hint after the function header (`CCN 12 · cog 8`) and, unless
`complexity.problems` is off, as warnings in the Problems window. A comment
density at or above `comment_density_warn` colours the `cmt%` cell, extends
the hint (`CCN 3 · cog 2 · cmt 55%` — below the threshold the hint stays
short) and adds its own Problems warning
(`function foo: comment density 55% ≥ 40%`). Which headers carry a hint is
`editor.marks.complexity` (Settings > Editor > Diagnostics): `all` (every
function), `warn` (only functions at or above a threshold) or `off`; an
older settings file with `true` / `false` keeps working as `all` / `off`.
In a file that differs from the Changes tab's base (`HEAD` by default) the
hint also shows how each function moved, live while you type:
`CCN 14 (−6) · cog 24 (−7)` — a drop in green, a rise in the warning
colour — or `(new)` for a function the base does not have; a function
whose numbers held shows no Δ. The window does not need to be open. The
optional `Δcx` status segment (`complexity.status_segment`) shows how the
working tree compares with `HEAD`.

**What 40 % means.** CCN and cognitive complexity never see comments, and
NLOC leaves them out, so a function with a comment between every statement
looks *simpler* than it reads. At the default 40 %, two of every five
non-blank lines of the body are comment: past that point the comments
usually narrate *what* the code does — a sign the code wants better names or
smaller functions instead. Only the body counts: a doc comment above the
header (Go doc comment, PHPDoc, JSDoc) is wanted and never raises the
density, and a Python docstring is a string, not a comment. A line with code
and a trailing comment counts as code. In the Complexity window,
`metric:cmt min:40` in the filter lists exactly the functions at or above
40 %.

The filter row knows more keys: `max:` bounds the sort column from above
(`min:15 max:30` lists functions with CCN 15 to 30; `metric:cog min:10
max:20` does the same on cognitive complexity), `kind:method` (or
`function`, `lambda`) keeps one kind of unit, `class:` matches the class
name, and `depth:4`, `params:5`, `nloc:50` keep functions at or above that
nesting depth, parameter count or code-line count.

Closures and lambdas are named `(anonymous)#n`, numbered per file. Languages
without a syntax grammar (and data formats like JSON or YAML) are not
measured.

## Why is this function complex?

A row says `renderSpanUncached ccn 139 cog 237` — but which `if`, which
`switch`, which `&&` chain costs what, and how much of it is the nesting
penalty? Select the function and press `space`: the row expands into one
indented line per *site*, every place the numbers came from, most expensive
first — `if +3 (nesting 2)` is an `if` two levels deep (1 for the `if`, 2
for the nesting), `else if +1` is flat, `&& +1` a run of boolean operators
(its `ccn` cell counts the operators), `switch +1` the statement with a
`case` line per arm (CCN only), `recursion +1` a call of the function by
itself. The `ccn` and `cog` cells of the site lines add up to the
function's (plus the 1 every function starts with). `enter` on a site line
jumps to it, `y` copies `path:line: if +3 (nesting 2)`, `space` again (on
the function or on any of its sites) folds it back. One function is open
at a time, and it stays open while the index re-measures the file. The
nesting penalty is the lever: the sites with the largest `(nesting n)` are
the blocks whose extraction into a helper saves the most. A closure's own
`if`s are listed under the closure; the enclosing function lists them too,
at its own depth, because Sonar counts a lambda's structure as part of the
method it sits in. On the command line, `ike complexity --sites --json`
prints the same breakdown.

The editor shows the same costs where you edit, SonarLint style: after
every line that adds to a function's cognitive complexity sits a dimmed
`+1`, `+3 (nesting 2)` or `+1 ||` (a boolean run names its operator;
several sites on one line join as `+1 · +1 &&`), in the warning colour from
`+3` on — the nesting penalty is what to look at. Which functions get them
is `editor.marks.complexity_sites` (Settings > Editor > Diagnostics, next to
the header hints): `warn` (the default — only functions at or above a
threshold), `all` or `off`. It is independent of `editor.marks.complexity`:
switching the header hints off leaves the site marks alone. On a function
header the `CCN · cog` hint wins over the site mark, and like the hint a
mark that does not fit whole at the end of the line is not shown. Press `m`
on a function row (or one of its site lines) in the Complexity window to
paint that function's marks once, whatever the mode — the file opens if it
is not open yet; the marks stay until you type in the buffer.

## Per class and per file

Press `b` in the Complexity window (or run *Complexity: Cycle Grouping*) to
answer "which class / which file is the heaviest?". The Project tab cycles
**none → class → file**, the File tab **none → class**. Grouped, each row is
one class (the methods of a Go receiver type, or of a Python, PHP,
TypeScript or JavaScript class, trait, interface or enum) or one file, with
the number of functions, NLOC, the CCN and cognitive **sum** (`Σccn`,
`Σcog`), average and maximum, the aggregate `cmt%`, how many of its
functions are over a threshold (`over`) and their summed `excess`. Free functions and lambdas belong to no
class but count toward their file. The header still sums the whole tab;
sorting and the filter work on the group columns (`fn:` matches the class
or the file). `enter` opens the group's first function. Each tab remembers
its grouping, and the layout keeps it across restarts.

## Hiding test files

Test functions are rarely what you want to simplify, yet a table-driven
test or a fake server easily tops the list. Press `t` in the Complexity
window (or run *Complexity: Toggle Test Files*) to hide test files on every
tab; the footer then reads `t tests: hidden`, the header totals and the
Changes verdict count only the remaining files, and the layout remembers
the choice. In the filter, `test:no` hides test files for one query and
`test:yes` shows only them — handy to find the heaviest test.

A file counts as a test when its language says so (Go `*_test.go`, Python
`test_*.py` / `*_test.py`, PHP `*Test.php`), when its name looks like
`*.spec.*`, `*.test.*`, `*_test.sh` or `*.bats`, or when it lies in a
`__tests__`, `tests`, `test` or `spec` directory. To drop test files
everywhere for good, use `complexity.exclude` (`**/*_test.go`) instead.

## The Changes tab

The third tab of the Complexity window compares every function in the files
that differ from a base revision (`HEAD` by default; press `R` to pick any
branch, tag or sha). The header sums it up:
`base HEAD → working tree · functions 120→123 · ΣCCN 410→416 (+6) · Σcog 380→371 (-9) · excess 42→30 (-12) · less complex`.

**How the verdict is decided.** The excess change decides; when it held,
the Σcog change decides; when that held too, the CCN change — counted
without the base 1 every function carries — decides; `unchanged` when all
three held. *This rule changed:* earlier releases read ΣCCN first, so
extracting a nested block into its own helper (one 20 / 30 function becoming
12 / 14 + 9 / 6) raised ΣCCN by one and read **more complex** although the
code got simpler — now it reads **less complex** (excess 20 → 0). Adding a
trivial helper reads **unchanged**. The `Δcx` status segment shows the
excess change as well.
Rows are grouped as **changed** (before → after with Δ, largest change
first; `cmt%` in percentage points), **added** and **removed**. Unsaved editor buffers count with their
current text. A renamed function appears as one removed and one added row.
`enter` jumps to the function, `e` exports CSV, `y` copies the row.

## Command line

```
ike complexity [--diff[=REV]] [--by class|file] [--brief[=class]] [--json] [--min N] [--ccn N] [--cog N] [--cmt N] [--no-tests] [--sites] [PATH...]
```

| Flag | Effect |
|---|---|
| *(none)* | Measure every supported file under the named paths (default: the current directory) and print a table sorted by CCN, then a summary line. |
| `PATH...` | One or more files or directories, mixed freely (`ike complexity pkg/ main.go`). A directory is walked; a file is measured as is. A file named twice, or also inside a named directory, counts once. A path that does not exist is an error (exit 2). |
| `--diff[=REV]` | Compare the working tree (including uncommitted and untracked files) against `REV` (default `HEAD`; any branch, tag or sha) and print the changed, added and removed functions, the totals and a verdict. With paths, only changes in those files and directories count. |
| `--by class`, `--by file` | Print one row per class or per file instead of per function: `FILE`, `CLASS` (class level only), `FUNCS`, `NLOC`, `CCN` (sum), `CCN_AVG`, `CCN_MAX`, `COG` (sum), `COG_AVG`, `COG_MAX`, `CMT%`, `OVER`, sorted by the CCN sum. With it, `--min N` keeps groups whose CCN or cognitive **sum** is at least `N`, and `--ccn` / `--cog` / `--cmt` keep groups whose **maximum** (or aggregate density) reaches the threshold. Not combinable with `--diff`. |
| `--brief`, `--brief=class` | Print one compact line per file (or per class), no header and no summary: `<path>  [<Class>]  funcs=12  nloc=340  ccn=48  cog=61  max=14/19  cmt=8%` — `ccn` / `cog` are sums, `max` is the largest CCN / cognitive complexity. At the class level a file's free functions and lambdas get one extra `(free)` line. Filters like `--by`; with `--json` the output is that of `--by file` / `--by class`. Not combinable with `--by` or `--diff`. |
| `--json` | Print JSON instead of text (fields below). |
| `--min N` | Only list functions whose CCN **or** cognitive complexity is at least `N`. |
| `--ccn N`, `--cog N` | Override the warning thresholds; given explicitly they also filter the rows to functions at or above either one. |
| `--cmt N` | Override the comment-density threshold (percent, 1-100); like `--ccn` / `--cog` it also filters the rows to functions at or above any threshold. |
| `--no-tests` | Leave test files out (the same rule as `t` in the Complexity window, see [Hiding test files](#hiding-test-files)): the table, the groups, `--brief`, the summary and `--diff` ignore them; a line `tests hidden: N files` follows the summary and the JSON `summary` carries `"tests_hidden": true`. A test file you name explicitly is still measured. |
| `--sites` | With `--json`: add `sites` to every function (and to the `after` side of every changed function under `--diff`), the per-site breakdown of [Why is this function complex?](#why-is-this-function-complex). The text table does not change; without the flag the JSON is exactly what it was. |

The walk skips the same files as the Complexity window: hidden directories,
`node_modules`, `vendor`, `dist`, `build`, `target`, virtualenvs, files over
512 KiB, and everything matching `complexity.exclude`. A file you name
explicitly is always measured, even inside a hidden or excluded directory.
The command reads your user settings and the project's `.ike/settings.toml`
when they exist, defaults otherwise; the project is the git repository of
the first path (else that path when it is a directory, else the current
directory), and file names print relative to it. Files of a language IKE
cannot measure (JSON, YAML, Markdown, …) are counted in a final `skipped:`
line.

```
$ ike complexity --json internal/cli/cli.go | jq '.summary.files'
1
$ ike complexity --diff internal/cli/cli.go internal/complexitycli/
```

```
$ ike complexity --min 40 .
FILE                         FUNCTION                  LINE  CCN  COG  NLOC  PARAMS  CMT%
internal/config/validate.go  validate                  262   168  220  622   1       14
internal/editor/view.go      Model.renderSpanUncached  949   139  237  368   6       21

summary: 33769 functions in 3161 files · ΣCCN 136390 (avg 4.0, max 168) · Σcog 122149 (avg 3.6, max 237) · NLOC 466863 · cmt 12% · 1223 over threshold (CCN ≥ 15 or cog ≥ 15 or cmt ≥ 40%) · excess 41870
shown: 2 of 33769 functions (CCN or cog ≥ 40)
skipped: 192 files (no complexity spec: json, markdown, yaml)

$ ike complexity --diff=main
complexity vs main (658ccf3)

changed (1):
  cmd/ike/main.go  main  L104  CCN 22→25 (+3)  cog 29→33 (+4)  NLOC 103→111 (+8)  cmt 10%→9% (-1)

added (1):
  internal/cli/cli.go  parseComplexity  L176  CCN 14  cog 17  NLOC 49  cmt 6%

totals: before 12 functions, ΣCCN 40, Σcog 38, excess 21 → after 13 functions, ΣCCN 57, Σcog 59, excess 30
verdict: MORE COMPLEX (excess +9, Σcog +21, ΣCCN +17)
```

The verdict reads the excess first, then Σcog, then ΣCCN (see
[The Changes tab](#the-changes-tab); earlier releases read ΣCCN first).

**Exit codes.** `0` whenever a report was printed — the command never fails
because a function is over a threshold; that policy is yours (see the CI
example). `2` for a usage error, a missing directory, a git error (an
unknown `--diff` revision prints git's message on stderr, outside a
repository likewise) or a build without grammars.

**Build requirement.** `ike complexity` needs the regular (cgo) build of
IKE, which carries the Tree-sitter grammars. A build made with
`CGO_ENABLED=0` prints an error and exits 2.

### Complexity-only install

When the report is all you need — on a CI runner, in a container for a
coding agent — install the **complexity-only build** instead of the IDE:

```sh
make install-complexity                        # ~/.local/bin/ike
make install-complexity BINDIR=/usr/local/bin  # or another directory
make build-complexity                          # just ./ike
```

It is the same `ike` binary, built with the `ike_complexity_only` tag
(`go build -tags ike_complexity_only ./cmd/ike`), and compiles much faster:
no terminal UI, LSP, debugger or plugin runtime, only the report and the
grammars of the measured languages (Go, Python, PHP, TypeScript/JavaScript,
Shell). `ike complexity` prints exactly what the full build prints on the
same tree, `skipped:` line included. `ike --version` ends with
`(complexity-only)`; any other invocation (`ike`, `ike file.go`, deep
links, `ike agent-hook`) prints
``ike: this ike build only provides `ike complexity`; install the full build for the IDE``
and exits 2. A later `make install` overwrites it with the full IDE. It
needs CGo like the full build.

### JSON fields

The field names are stable: new fields may be added, existing ones are never
renamed or removed. Line numbers are 1-based.

Project report:

```json
{
  "summary": {
    "files": 3161, "functions": 33769, "nloc": 466863,
    "ccn_sum": 136390, "ccn_avg": 4.04, "ccn_max": 168,
    "cog_sum": 122149, "cog_avg": 3.62, "cog_max": 237,
    "over_threshold": 1223, "excess": 41870, "ccn_warn": 15, "cog_warn": 15,
    "comment_lines": 63650, "comment_density": 0.12, "cmt_warn": 40
  },
  "functions": [
    { "file": "internal/config/validate.go", "name": "validate", "kind": "function",
      "line": 262, "end_line": 1047, "ccn": 168, "cognitive": 220,
      "nloc": 622, "params": 1, "depth": 5,
      "comment_lines": 101, "comment_density": 0.1397,
      "sites": [
        { "line": 264, "col": 1, "end_line": 270, "kind": "if", "ccn": 1, "cognitive": 1, "nesting": 0, "level": 0 },
        { "line": 264, "col": 6, "end_line": 264, "kind": "&&", "ccn": 2, "cognitive": 1, "nesting": 0, "level": 0 },
        { "line": 266, "col": 2, "end_line": 268, "kind": "switch", "ccn": 0, "cognitive": 2, "nesting": 1, "level": 1 }
      ] }
  ],
  "skipped": { "files": 192, "languages": ["json", "markdown", "yaml"] }
}
```

`summary` always covers every function; `functions` holds the rows the
filter keeps, sorted by CCN descending. `kind` is `function`, `method` or
`lambda`. `comment_density` is a ratio (0.55 = 55 %), `comment_lines` the
body's comment-only lines; `cmt_warn` is the threshold in percent (`0` =
off). `excess` is the summed mass above the CCN and cognitive thresholds
(see [Reading the numbers](#reading-the-numbers)). With `--no-tests` every
summary gains `"tests_hidden": true`; without it the field is absent.
`sites` appears with `--sites` only: one entry per increment of the
function's CCN or cognitive complexity — `line` / `end_line` 1-based,
`col` 0-based, `kind` the construct as the language spells it (`if`,
`else if`, `elif`, `for`, `switch`, `case`, `catch`, `?:`, a boolean
operator, `recursion`), `ccn` and `cognitive` the increments, `nesting`
the part of `cognitive` that is the nesting penalty, `level` the nesting
level. With `--diff --sites` every `changed` entry's `after` and every
`added` entry carry it too.

Grouped report (`--by class --json`): `functions` gives way to `groups`,
one object per row the filter keeps, with the summary's field names; `class`
is present at the class level only.

```json
{
  "summary": { "files": 3161, "functions": 33769, "…": "…" },
  "groups": [
    { "file": "internal/editor/view.go", "class": "Model", "functions": 212,
      "nloc": 9120, "ccn_sum": 2410, "ccn_avg": 11.37, "ccn_max": 139,
      "cog_sum": 2950, "cog_avg": 13.92, "cog_max": 237,
      "over_threshold": 41, "excess": 2894, "comment_lines": 1310, "comment_density": 0.1256 }
  ],
  "skipped": { "files": 192, "languages": ["json", "markdown", "yaml"] }
}
```

Diff report (`--diff --json`):

```json
{
  "rev": "main", "sha": "658ccf37…",
  "base": { "files": 12, "functions": 12, "ccn_sum": 40, "cog_sum": 38, "…": "…" },
  "head": { "files": 13, "functions": 13, "ccn_sum": 57, "cog_sum": 59, "…": "…" },
  "delta": { "ccn": 17, "cognitive": 21, "excess": 9, "comment_lines": 2, "comment_density": -0.004 },
  "changed": [
    { "file": "cmd/ike/main.go", "name": "main", "kind": "function", "line": 104,
      "before": { "ccn": 22, "cognitive": 29, "nloc": 103, "params": 0, "depth": 3, "comment_lines": 11, "comment_density": 0.0965 },
      "after":  { "ccn": 25, "cognitive": 33, "nloc": 111, "params": 0, "depth": 3, "comment_lines": 11, "comment_density": 0.0902 },
      "delta":  { "ccn": 3, "cognitive": 4, "nloc": 8, "excess": 7, "comment_lines": 0, "comment_density": -0.0063 } }
  ],
  "added":   [ { "file": "…", "name": "…", "line": 176, "ccn": 14, "…": "…" } ],
  "removed": [],
  "verdict": "more_complex",
  "skipped": { "files": 0, "languages": [] }
}
```

`base` and `head` have the same fields as the project `summary`, over the
files that differ from `REV`; both, the top-level `delta`, every changed
function's `delta` and every group carry `excess`. `verdict` is
`more_complex`, `less_complex` or `unchanged`, decided by the excess first
(the rule changed — see [The Changes tab](#the-changes-tab)). A policy that
wants the old ΣCCN reading keeps it with `jq -e '.delta.ccn <= 0' cx.json`.

### Gating a pull request in GitHub Actions

Fail the build when a pull request makes the code more complex, or adds or
worsens a function past CCN 20:

```yaml
jobs:
  complexity:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
        with:
          fetch-depth: 0          # the base revision must be in the clone
      - uses: actions/setup-go@v5
        with:
          go-version: '1.26'
      - name: Build ike
        run: |
          # cgo build (the default on ubuntu-latest, which ships a C compiler);
          # the complexity-only build is all this job needs, and builds fastest
          git clone --depth 1 https://github.com/TrueDaerk/ike.git "$RUNNER_TEMP/ike-src"
          (cd "$RUNNER_TEMP/ike-src" && go build -tags ike_complexity_only -o "$RUNNER_TEMP/bin/ike" ./cmd/ike)
          echo "$RUNNER_TEMP/bin" >> "$GITHUB_PATH"
      - name: Complexity vs base
        run: |
          ike complexity --diff=origin/${{ github.base_ref }} --json > cx.json
          jq -r '"verdict: \(.verdict) (excess \(.delta.excess), Σcog \(.delta.cognitive), ΣCCN \(.delta.ccn))"' cx.json
          # policy: no function may end up at CCN ≥ 20 through this change
          jq -e '[.changed[].after.ccn, .added[].ccn] | all(. < 20)' cx.json
```

Swap the last line for whatever policy you want — for example
`jq -e '.verdict != "more_complex"' cx.json` to refuse any net increase.
`jq -e` exits non-zero when the expression is `false`, which fails the step.

### For coding agents

An agent can ask "did I make this more complex?" with
`ike complexity --diff --json` (against `HEAD`, so it sees its own
uncommitted edits) and read `verdict`, `delta` and the `changed` entries.
