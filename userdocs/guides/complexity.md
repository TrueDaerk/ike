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

Values at or above `ccn_warn` / `cognitive_warn` show in the warning colour,
as a hint after the function header (`CCN 12 · cog 8`) and, unless
`complexity.problems` is off, as warnings in the Problems window. A comment
density at or above `comment_density_warn` colours the `cmt%` cell, extends
the hint (`CCN 3 · cog 2 · cmt 55%` — below the threshold the hint stays
short) and adds its own Problems warning
(`function foo: comment density 55% ≥ 40%`). The
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

Closures and lambdas are named `(anonymous)#n`, numbered per file. Languages
without a syntax grammar (and data formats like JSON or YAML) are not
measured.

## Per class and per file

Press `b` in the Complexity window (or run *Complexity: Cycle Grouping*) to
answer "which class / which file is the heaviest?". The Project tab cycles
**none → class → file**, the File tab **none → class**. Grouped, each row is
one class (the methods of a Go receiver type, or of a Python, PHP,
TypeScript or JavaScript class, trait, interface or enum) or one file, with
the number of functions, NLOC, the CCN and cognitive **sum** (`Σccn`,
`Σcog`), average and maximum, the aggregate `cmt%` and how many of its
functions are over a threshold. Free functions and lambdas belong to no
class but count toward their file. The header still sums the whole tab;
sorting and the filter work on the group columns (`fn:` matches the class
or the file). `enter` opens the group's first function. Each tab remembers
its grouping, and the layout keeps it across restarts.

## The Changes tab

The third tab of the Complexity window compares every function in the files
that differ from a base revision (`HEAD` by default; press `R` to pick any
branch, tag or sha). The header sums it up:
`base HEAD → working tree · functions 120→123 · ΣCCN 410→416 (+6) · … · more complex`.
Rows are grouped as **changed** (before → after with Δ, largest change
first; `cmt%` in percentage points), **added** and **removed**. Unsaved editor buffers count with their
current text. A renamed function appears as one removed and one added row.
`enter` jumps to the function, `e` exports CSV, `y` copies the row.

## Command line

```
ike complexity [--diff[=REV]] [--by class|file] [--brief[=class]] [--json] [--min N] [--ccn N] [--cog N] [--cmt N] [PATH...]
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

summary: 33769 functions in 3161 files · ΣCCN 136390 (avg 4.0, max 168) · Σcog 122149 (avg 3.6, max 237) · NLOC 466863 · cmt 12% · 1223 over threshold (CCN ≥ 15 or cog ≥ 15 or cmt ≥ 40%)
shown: 2 of 33769 functions (CCN or cog ≥ 40)
skipped: 192 files (no complexity spec: json, markdown, yaml)

$ ike complexity --diff=main
complexity vs main (658ccf3)

changed (1):
  cmd/ike/main.go  main  L104  CCN 22→25 (+3)  cog 29→33 (+4)  NLOC 103→111 (+8)  cmt 10%→9% (-1)

added (1):
  internal/cli/cli.go  parseComplexity  L176  CCN 14  cog 17  NLOC 49  cmt 6%

totals: before 12 functions, ΣCCN 40, Σcog 38 → after 13 functions, ΣCCN 57, Σcog 59
verdict: MORE COMPLEX (ΣCCN +17, Σcog +21)
```

The verdict reads the summed CCN; when that did not move, the summed
cognitive complexity decides.

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
    "over_threshold": 1223, "ccn_warn": 15, "cog_warn": 15,
    "comment_lines": 63650, "comment_density": 0.12, "cmt_warn": 40
  },
  "functions": [
    { "file": "internal/config/validate.go", "name": "validate", "kind": "function",
      "line": 262, "end_line": 1047, "ccn": 168, "cognitive": 220,
      "nloc": 622, "params": 1, "depth": 5,
      "comment_lines": 101, "comment_density": 0.1397 }
  ],
  "skipped": { "files": 192, "languages": ["json", "markdown", "yaml"] }
}
```

`summary` always covers every function; `functions` holds the rows the
filter keeps, sorted by CCN descending. `kind` is `function`, `method` or
`lambda`. `comment_density` is a ratio (0.55 = 55 %), `comment_lines` the
body's comment-only lines; `cmt_warn` is the threshold in percent (`0` =
off).

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
      "over_threshold": 41, "comment_lines": 1310, "comment_density": 0.1256 }
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
  "delta": { "ccn": 17, "cognitive": 21, "comment_lines": 2, "comment_density": -0.004 },
  "changed": [
    { "file": "cmd/ike/main.go", "name": "main", "kind": "function", "line": 104,
      "before": { "ccn": 22, "cognitive": 29, "nloc": 103, "params": 0, "depth": 3, "comment_lines": 11, "comment_density": 0.0965 },
      "after":  { "ccn": 25, "cognitive": 33, "nloc": 111, "params": 0, "depth": 3, "comment_lines": 11, "comment_density": 0.0902 },
      "delta":  { "ccn": 3, "cognitive": 4, "nloc": 8, "comment_lines": 0, "comment_density": -0.0063 } }
  ],
  "added":   [ { "file": "…", "name": "…", "line": 176, "ccn": 14, "…": "…" } ],
  "removed": [],
  "verdict": "more_complex",
  "skipped": { "files": 0, "languages": [] }
}
```

`base` and `head` have the same fields as the project `summary`, over the
files that differ from `REV`. `verdict` is `more_complex`, `less_complex` or
`unchanged`.

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
          jq -r '"verdict: \(.verdict) (ΣCCN \(.delta.ccn), Σcog \(.delta.cognitive))"' cx.json
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
