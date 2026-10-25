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
`comment_density_warn` (40 % by default, `0` turns it off), per-path and
per-language `overrides` of those thresholds (see
[Different thresholds per path or language](#different-thresholds-per-path-or-language)),
the hotspot window `churn_days` (90 by default), the trend window
`trend_commits` (30 by default, see [The Trend tab](#the-trend-tab)) and the `baseline` switch
(on by default) that applies the project's accepted-debt file (see
[Accepted debt: the baseline](#accepted-debt-the-baseline)).

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

**How bad is it overall?** An average says little next to a maximum, so
the window header adds the percentiles of the sort metric:
`CCN Σ137301 avg 4.0 p50 2 p90 9 p99 28 max 170` reads "half the functions
are at 2 or below, nine in ten at 9 or below, one in a hundred above 28".
Press `D` for the distribution row under the filter:
`1–5 ████████ 29310 · 6–10 ███ 3102 · 11–15 █ 812 · 16–25 ▌ 540 · 26+ ▏ 185`
— how many functions fall in each bucket of the sort metric (cognitive
while you sort by it, CCN otherwise), the buckets at or above the warning
threshold in the warning colour. Click a bucket to list just those
functions (it sets `min:16 max:25` in the filter and keeps what else you
typed); click it again to drop the range. `D` again hides the row; the
window remembers it across restarts.

The filter row knows more keys: `max:` bounds the sort column from above
(`min:15 max:30` lists functions with CCN 15 to 30; `metric:cog min:10
max:20` does the same on cognitive complexity), `kind:method` (or
`function`, `lambda`) keeps one kind of unit, `class:` matches the class
name, and `depth:4`, `params:5`, `nloc:50` keep functions at or above that
nesting depth, parameter count or code-line count.

Put a `-` in front of any key to exclude instead of keep:
`file:internal/app -file:*_test.go` lists everything under `internal/app`
except the tests, `-fn:render` drops the renderers and `-lang:php` the PHP
files. A bare `-word` drops every function whose name the word matches. To
search for a literal leading dash, write `--x` (or quote it: `"-x"`). The
same `-` works in every filter row — Problems, Usages, the TODO index, the
archive viewer and the other tool windows.

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

Below the sites, the expanded function proposes **what to extract**: up to
three lines like `extract 120–160 (if): −18 cog · new fn ccn 9 cog 11 ·
rest ccn 12 cog 14`, best saving first. Each names a nested block — an
`if`, a `for`, a `switch`, an `else` body — and what moving it into a
function of its own would do: the file loses 18 cognitive points, because
the block and everything inside it shed their nesting penalty; the new
function would measure ccn 9 / cog 11, what remains of the old one ccn 12 /
cog 14 (the two cyclomatic numbers add up to one more than before — a
function always starts at 1). Only blocks saving at least 3 points are
listed; a block at the top level saves nothing and never appears, and
blocks nest, so an `if` inside a listed `for` may be listed too — pick the
one that reads best. `enter` on a proposal opens the file with those lines
selected and the caret on the block's first line, so `alt+enter` offers
the language server's *Extract function* right away; `y` copies the
proposal. The same proposals ride `ike complexity --sites --json` as
`candidates`.

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

## Let an agent simplify it

Press `a` on a function row — on any tab; on a site or candidate row it
means the function above it — and IKE asks Claude Code to simplify that
function: a fresh `claude -p` session (no tools, the model and turn limit of
`agent.ask.*`) gets the function's source, its metrics, its complexity
sites and its best extraction candidate, and the instruction to reduce the
cognitive complexity without changing the behaviour, keep the public
signature, keep the tests green and answer with a unified diff plus one
paragraph on what changed and why. The prompt tells the agent it has no
tools and must answer from the source it was given; an answer without a
diff gets one follow-up asking for it.

When the answer arrives, IKE applies the diff to the function's file in the
editor — opening it if needed — as **one undoable edit**: the buffer is
modified but not saved, the editor jumps to the function, and a
notification reports the change, e.g. `complexity: applied the agent's diff
to render (−3 excess) — CCN 12 → 6, cognitive 20 → 9`. Review it with the
gutter diff, press `u` to throw the whole change away, or save to keep it.
Nothing is written to disk and git is never touched. The agent's
explanation opens as a Markdown scratch in a tab behind the file. If a hunk
of the diff does not match your file (it changed meanwhile, or the agent
invented lines), nothing is applied and the scratch opens in front, naming
the hunk that failed, so you can still apply it by hand (for example with
`git apply --3way`). Turn off **Apply the agent's diff**
(`complexity.agent_apply`, Settings ▸ Complexity) to only get the scratch.

While the agent works, the status line shows `[agent: simplify render
0:42]` and the function's row in the Complexity window starts with `⟳`.
Press `esc` on that row, `A` in the Complexity window, or run **Complexity:
Cancel Agent** from the palette to stop it; `a` then starts a fresh run.
One run per function at a time — `a` again while it runs only reminds you.
Every notification after a run, and the scratch header, name the model and
the session log (`~/.claude/projects/<project>/<session-id>.jsonl`), so a
strange answer can be inspected. Without the `claude` command on `PATH` the
notification tells you to install Claude Code; a failed run shows the
error.

## Different thresholds per path or language

One set of limits rarely fits a whole project: legacy code, generated files
and tests can live with more, a strict core wants less. `complexity.overrides`
(Settings ▸ Complexity ▸ Threshold overrides) is a list of entries
`<selector> ccn=N cog=N cmt=N`:

```toml
[complexity]
overrides = ["legacy/** ccn=30 cog=30", "lang:php cog=25 cmt=0", "*_test.go ccn=40 cog=40"]
```

- The **selector** is a glob over the project-relative path, with the same
  rules as `exclude` (`legacy/**`; a pattern without a slash also matches the
  file name anywhere, `*_test.go`), or `lang:<id>` for every file of a
  language (`lang:php`, `lang:go`).
- Each of `ccn=` (1–200), `cog=` (1–200) and `cmt=` (0–100, `0` off) is
  optional — a key the entry leaves out keeps the global value — but an
  entry needs at least one.
- The **first** matching entry, in list order, decides; a file no entry
  matches keeps the global thresholds.

The file's thresholds apply everywhere a threshold does: the Complexity
window's warning colours and `over` / `excess` totals (the File tab's header
names the thresholds in force, `thresholds ccn 30 cog 30 (legacy/**)`), the
inline hints and site marks, the Problems feed, the Changes tab and its
status segment, the baseline's *accept all*, and `ike complexity` — where an
explicit `--ccn` / `--cog` / `--cmt` still beats every override. The
Settings form refuses a malformed entry with a message naming it; a bad
entry in a settings file is dropped with a warning. A change applies at
once — the Problems rows and the hints follow without a restart.

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

## The directory tree

Press `v` in the Complexity window (or run *Complexity: Toggle Tree /
Grid View*) to turn the Project tab into a tree: the project root, its
directories (every level — this is what you drill through), the files,
the classes and the functions. Each directory, file and class row shows
the same aggregates as the file grouping — the number of functions, NLOC,
`Σccn`, ccn max, `Σcog`, cog max, `cmt%`, `over` and `excess` (and
`commits` / `hot` in a git repository) — summed over everything below it,
so "which subsystem is the heaviest, and inside it which file" is a few
`l` presses: children sort by `Σccn` (or by the column `s` picks). A
function row shows its own metrics in the matching columns. `enter` (or a
double click) opens a function and unfolds or folds anything else; `l` /
`h` unfold / fold (a second `h` climbs to the parent); `space` on a
function shows its breakdown. The filter works on the functions and
prunes the branches that end up empty, unfolding the rest so the matches
are visible. `y` copies the selected node's aggregate line, `e` exports
the rows you can see as CSV with a `path` column, `i` accepts a whole
directory's over-threshold functions into the baseline. The header, the
distribution row and the test-file toggle are the grid's; `v` again
returns to it, and the layout remembers the view (the footer reads `v
tree` or `v grid`).

## Hotspots: complexity × change frequency

A complex function nobody has touched in a year costs less than a simpler
one edited every week. Inside a git repository the Complexity window adds a
`commits` column to the function rows — how many commits touched the
function's file in the last 90 days — and, grouped per file (`b` twice),
`commits` and `hot`: commits × the file's `excess`, or commits × its
`Σccn` when nothing in it exceeds a threshold. Sort by it with
`metric:hot` in the filter (or `s` until the header marks `hot`): the top
rows are where simplifying pays off first. The top tenth of the visible
`hot` values is painted in the warning colour, and the footer names the
window (`churn 90d`). Change the window in **Settings > Complexity >
Hotspot churn window** (`complexity.churn_days`, 1–3650 days); outside a
git repository the columns are hidden. The counts come from one `git log`
per refresh of the git status. A renamed file is not followed: its commits
before the rename count under the old name.

## Accepted debt: the baseline

A project that adopts complexity checks late has hundreds of functions over
the thresholds, and none of them is going to be rewritten this week. Without
a record of what is *accepted*, the Problems window lists all of them, the
`over` count never changes, and a CI check cannot tell old debt from a new
offender. The **baseline** fixes that the way linters do (`golangci-lint
--new-from-rev`, Sonar's "new code"): record what exists, flag only what is
new or got worse.

The baseline is the file `.ike/complexity-baseline.json`, meant to be
committed:

```json
{"version": 1, "ccn_warn": 15, "cognitive_warn": 15, "accepted": [
  {"file": "internal/config/validate.go", "function": "validate", "ordinal": 0, "ccn": 170, "cognitive": 222}
]}
```

One entry per accepted function — the project-relative path, the name, the
ordinal among same-named functions of that file (0 for the first `init`,
1 for the second) and the metrics it was accepted at; the thresholds are
noted for the reader. A function is

- **accepted** when its entry matches and neither CCN nor cognitive
  complexity is above the recorded value — it no longer counts as over
  threshold anywhere: not in the Problems window, not in the `over` count
  or `excess` of the window header and the group rows, not in the CLI
  summary. The window still shows it, with a `✓` after the name and its
  over-threshold cells muted instead of coloured;
- **worsened** when its entry matches but a metric grew — listed again,
  the Problems row ends in `(worse than the baseline)`;
- **new** otherwise: no entry, a renamed function, or an inserted
  same-named function that shifted the ordinals.

The comment-density check is not part of the baseline.

**Seeding it.** In the Complexity window, `i` accepts the selected function
into the file (or drops it again when it is already accepted — a worsened
function re-accepted records its new metrics); on a class or file row `i`
accepts every over-threshold function of that group, or drops them all when
they are all accepted already. `Complexity: Accept All Over Threshold` in the
command palette writes every function currently over the thresholds in one
go, after a confirmation; it replaces the file. The same file comes out of
`ike complexity --write-baseline` (below), which is how a CI job or a coding
agent seeds it. Commit the file: IKE re-reads it whenever it changes on disk,
so a `git checkout` of another branch applies that branch's baseline at
once. The filter key `baseline:new`, `baseline:accepted` and
`baseline:worsened` narrows the window to one status (on group rows: groups
with such a member).

**Switching it off.** `complexity.baseline = false` (Settings ▸ Complexity
▸ Accepted-debt baseline) ignores the file everywhere without deleting it.
A malformed file is reported once and ignored.

## Hiding functions and files

The baseline is for debt you own but live with. Some rows are not debt at
all: a downloaded library under `static/lib/`, a vendored helper, the
anonymous module initialisers (`(anonymous)#2`) that carry a JavaScript
file's whole complexity and bury the functions you wrote. Those should not
be looked at — and **hidden** is the stronger action for them. A hidden
function or file disappears from every Complexity surface as if it had
never been measured: all tabs, the grid and the tree, the groups, the
histogram, the Problems window, the exports and `ike complexity`. The
header counts what left (`3 hidden`).

- **`x` on a function row** hides that function. It is matched like a
  baseline entry (path, name, position among same-named functions), so
  `(anonymous)#2` hides exactly that unit and survives edits above it. The
  entry lands in the `hidden` section of `.ike/complexity-baseline.json`
  (format version 2; commit the file and a fresh checkout hides the same
  set). This works whether or not the baseline setting is on.
- **`x` on a file, group or tree row** hides the whole file — a directory
  node in the tree hides the directory (`vendor/**`). This adds a pattern
  to **Settings > Complexity > Excluded files** (`complexity.exclude`, in
  the project settings), so the index stops measuring the file at once;
  remove the pattern there to bring it back.
- **`X` on any row** hides the row's file, from a function row too.
- **`H`** shows the hidden functions instead — the filter term
  `hidden:yes`, listed muted with ` ✕` — so you can un-hide one with `x`
  without it being visible first. `H` again leaves the view. `hidden:no`
  is the complement; without either term hidden functions are simply gone.

Hiding and accepting are independent: hiding an accepted function keeps
its baseline entry, un-hiding restores its status. The accept-all action
and `--write-baseline` keep the hidden section when they reseed the file.

Press `f1` while the window is focused: the help overlay leads with every
key the window handles — the list, the filter row and its vocabulary, the
Changes tab's `d` and `R`, the agent keys — including the fixed ones no
keymap lists.

## Pinning the functions you are working on

An optimisation session usually has a handful of targets. Select one in the
Complexity window and press `p` (or run **Complexity: Pin / Unpin Selected
Function**): the function is pinned together with its CCN and cognitive
complexity *at that moment*. On the Project tab a **Pinned** section leads
the list, whatever the sort, with a `since pin` column:

```
render ★   …   ccn 42→30 (−12) · cog 31→20 (−11)
```

The column is green when a metric fell and none rose, and in the warning
colour when one rose. A pinned function that no longer exists (renamed,
deleted) stays listed as `(gone)` until you unpin it. Pinned functions
carry `★` after their name on the File and Changes tabs too, and
`pinned:yes` / `pinned:no` filters on them. `p` on a group row only
explains that pins are per function. Press `p` again to unpin.

Pins are personal state, not project data: they live in
`.ike/complexity-pins.json` (or `$IKE_CONFIG_DIR/complexity-pins.json`) and
survive restarts. A pin follows its function when it moves inside its file
(it is matched by file, name and position among functions of the same
name).

**Session Δ.** The Project tab's header also shows how far the whole
session moved: `session Δexcess −120` compares the current excess with the
first fully scanned snapshot of this IDE run. It is hidden while nothing
changed.

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

**Pasting into a pull request.** `E` (or `Complexity: Export Current Tab as
Markdown`) writes the current tab as Markdown to a scratch file and opens
it: on the Changes tab a bold verdict, the totals and one table each for
changed, added and removed; on the other tabs the totals line and the
table. The rows are the ones you see — filter and sort apply — and rows past
the 30th fold into a `<details>` block. It is the same rendering as
`ike complexity --diff --md` (see [Posting the verdict as a pull-request
comment](#posting-the-verdict-as-a-pull-request-comment)).

**Comparing two revisions.** `R` also takes a range `A..B` — review a pull
request branch without checking it out (`main..feature`), or ask whether
`main` got more complex this week (`main@{1.week.ago}..main`). The header
then reads `base main → feature · …`, both sides come from the commits, and
your working tree — unsaved buffers, uncommitted and untracked files — plays
no part, so editing does not re-run the comparison; `R`, `r` or a settings
change does. A range with a missing end (`main..`) or three dots
(`main...feature`) is rejected with a message and the previous base stays.
The `Δcx` status segment and the inline hint Δ follow whichever comparison
the tab shows.

**Which edit cost the points?** Press `d` on a function row to open a diff
of just that function: its version at the base revision on the left, the
working tree (unsaved edits included) on the right, each with three lines of
context and numbered like the file — titled `handle @ HEAD → working tree`.
An added function shows an empty left side, a removed one an empty right
side. `enter` on a change jumps to that line in the editor. Each function
gets its own diff; pressing `d` on it again brings the open one back, and
closing it returns you to the Complexity window. The diff is read-only (the
left side is a stored revision).

**Per file.** Press `b` to see the change per file instead: one row per
file with how many functions changed, were added and were removed, and the
file's CCN, cognitive, NLOC and excess sums before → after with the
change — `internal/app/app.go  3  1  0  40→52 (+12)  31→44 (+13) …`. A
file that got heavier shows its numbers in the warning colour. Sorting and
`min:` / `max:` read the size of the change; `enter` opens the file at its
first changed function, `y` copies `path: changed 3 added 1 removed 0 ccn
40→52 (+12) cog 31→44 (+13)`, `e` exports one CSV row per file. Press `b`
again for the function rows; the choice is kept with the layout.

## The Trend tab

The fourth tab answers "is the project getting more complex?" over the
last commits instead of against one base: it measures the whole project at
each of the last 30 commits (`complexity.trend_commits`) and lists them,
newest first, with their sums and how much each commit moved them. The
header reads `last 30 commits · Σexcess 4120 → 3980 (-140) · ΣCCN … ·
Σcog …` (oldest → newest), and a sparkline under the filter row draws the
excess of every commit, oldest on the left:

```
 excess ▃▃▄▄▅▅▆▇█▇▇▆▅▄▃▂▁ 3980–4210 · oldest → newest
 commit ▼  date        subject                     funcs  Σccn (Δ)     Σcog (Δ)     excess (Δ)   over (Δ)
 43b2a00    2026-10-14  per-path thresholds          34942  144055 (+220) 129431 (+201) 18193 (+42)  1404 (+3)
 777a23d    2026-10-14  tree view of the Project tab 34891  143835 (+319) 129230 (+282) 18151 (-6)   1401 (-1)
```

Each row is one commit: its short sha, date and subject, the function
count, and the summed CCN, summed cognitive complexity, excess and
over-threshold count with the change against the previous (older) commit.
A commit that **added** excess shows it in the warning colour, one that
removed some in the success colour; the oldest row has no change. Only the
**first parent** is followed: a merge commit is one point (the state of the
branch after the merge landed), the commits of the branch it merged are
not listed on their own, and a squash merge reads the same as a merge
commit. The thresholds are the current ones, including the per-path and
per-language overrides.

- `s` cycles the sort column; the sum columns sort by their change, so
  `excess (Δ) ▼` puts the commits that cost the most at the top. `S`
  reverses.
- The filter row matches the subject and the short sha (`render`,
  `43b2a0`); `min:` / `max:` bound the sort column's change (`min:10` on
  `excess (Δ)` keeps the commits that added ten or more).
- `enter` switches to the [Changes tab](#the-changes-tab) comparing that
  commit against `HEAD` (`43b2a00..HEAD`) — "what changed since this
  commit" is one key away; `R` there takes you anywhere else.
- A click on a sparkline bar selects that commit's row.
- `e` exports one CSV row per commit (`commit, sha, date, subject,
  functions, ccn_sum, ccn_delta, cog_sum, cog_delta, excess, excess_delta,
  over, over_delta`); `y` copies the row as `43b2a00 2026-10-14 subject:
  fns 120 Σccn 410 (+6) Σcog 380 (-9) excess 42 (-12) over 3 (0)`.
- `r` measures again from scratch.

Grouping (`b`), the tree view, the breakdown (`space`, `m`), the
distribution row, the baseline and the pins do not apply here — a row is a
commit, not a function — and the footer says so. The first walk reads the
whole tree at the oldest commit and then only the files each later commit
changed, so it takes a few seconds on a large project; `computing… 12/30`
counts it down. Afterwards the measurements are kept for the session: a
new commit, a changed window or a return to the tab re-measures only what
is new. Outside a git repository the tab says so.

**Trend window** (`complexity.trend_commits`, Settings ▸ Complexity, 1–500,
default 30) sets how many commits the tab and `ike complexity --trend`
measure by default; a change recomputes the open tab.


## Command line

```
ike complexity [--diff[=REV|A..B]] [--by class|file] [--brief[=class]] [--json | --md] [--min N] [--ccn N] [--cog N] [--cmt N] [--no-tests] [--sites] [--churn[=DAYS]] [--baseline[=PATH] | --no-baseline] [--write-baseline] [--pins] [--show-hidden] [--trend[=N]] [PATH...]
```

| Flag | Effect |
|---|---|
| *(none)* | Measure every supported file under the named paths (default: the current directory) and print a table sorted by CCN, then a summary line. |
| `PATH...` | One or more files or directories, mixed freely (`ike complexity pkg/ main.go`). A directory is walked; a file is measured as is. A file named twice, or also inside a named directory, counts once. A path that does not exist is an error (exit 2). |
| `--diff[=REV]` | Compare the working tree (including uncommitted and untracked files) against `REV` (default `HEAD`; any branch, tag or sha) and print the changed, added and removed functions, the totals and a verdict. With paths, only changes in those files and directories count. |
| `--diff=A..B` | Compare two revisions instead: the files that differ between commits `A` and `B`, both sides read from git; the working tree and untracked files are ignored. `--diff=origin/main..HEAD` gates a merge commit in CI. A malformed range (`A..`, `A...B`) or an unknown ref exits 2 with the message. |
| `--by class`, `--by file` | Print one row per class or per file instead of per function: `FILE`, `CLASS` (class level only), `FUNCS`, `NLOC`, `CCN` (sum), `CCN_AVG`, `CCN_MAX`, `COG` (sum), `COG_AVG`, `COG_MAX`, `CMT%`, `OVER`, sorted by the CCN sum. With it, `--min N` keeps groups whose CCN or cognitive **sum** is at least `N`, and `--ccn` / `--cog` / `--cmt` keep groups whose **maximum** (or aggregate density) reaches the threshold. With `--diff`, `--by file` prints the change per file instead of per function (`FILE`, `CHANGED`, `ADDED`, `REMOVED`, then `CCN`, `COG`, `NLOC`, `EXCESS` as before→after (Δ), the biggest CCN increase first) and the JSON gains a `files` array; `--by class` is not combinable with `--diff`. |
| `--brief`, `--brief=class` | Print one compact line per file (or per class), no header and no summary: `<path>  [<Class>]  funcs=12  nloc=340  ccn=48  cog=61  max=14/19  cmt=8%` — `ccn` / `cog` are sums, `max` is the largest CCN / cognitive complexity. At the class level a file's free functions and lambdas get one extra `(free)` line. Filters like `--by`; with `--json` the output is that of `--by file` / `--by class`. Not combinable with `--by` or `--diff`. |
| `--json` | Print JSON instead of text (fields below). |
| `--md` | Print GitHub-flavoured Markdown instead of text, ready for a pull-request description or comment: a bold summary line (the verdict under `--diff`), the notes, and a table per section with the numbers right-aligned, before→after cells as `5→7 (+2)` and every row past the 30th folded into a `<details>` block. Works with `--diff` (changed / added / removed tables, or the per-file table with `--by file`), `--by`, `--brief` and `--trend`. `|`, backticks, `*` and `<` in names and paths are escaped. Not combinable with `--json` (exit 2). |
| `--min N` | Only list functions whose CCN **or** cognitive complexity is at least `N`. |
| `--ccn N`, `--cog N` | Override the warning thresholds — the global ones and every `complexity.overrides` entry; given explicitly they also filter the rows to functions at or above either one. |
| `--cmt N` | Override the comment-density threshold (percent, 1-100); like `--ccn` / `--cog` it also filters the rows to functions at or above any threshold. |
| `--no-tests` | Leave test files out (the same rule as `t` in the Complexity window, see [Hiding test files](#hiding-test-files)): the table, the groups, `--brief`, the summary and `--diff` ignore them; a line `tests hidden: N files` follows the summary and the JSON `summary` carries `"tests_hidden": true`. A test file you name explicitly is still measured. |
| `--sites` | With `--json`: add `sites` and `candidates` to every function (and to the `after` side of every changed function under `--diff`), the per-site breakdown and the extraction proposals of [Why is this function complex?](#why-is-this-function-complex). The text table does not change; without the flag the JSON is exactly what it was. |
| `--churn[=DAYS]` | Add the hotspot numbers (see [Hotspots](#hotspots-complexity--change-frequency)) from the commits of the last `DAYS` days (default: `complexity.churn_days`, 90): a `COMMITS` column in the function table, `COMMITS` and `HOT` in the `--by` tables, `commits=N  hot=N` on `--brief` lines, and in the JSON `commits` per function and group, `hot` per group and `summary.churn_days`. `ike complexity --churn=30 --by file` ranks the files to simplify first. Needs a git repository (exit 2 otherwise); not combinable with `--diff`. Without the flag the output is unchanged. |
| `--baseline[=PATH]`, `--no-baseline` | Apply the [baseline](#accepted-debt-the-baseline). The project's `.ike/complexity-baseline.json` applies **by default** when it exists; a bare `--baseline` insists on it (exit 2 when it is missing), `--baseline=PATH` reads another file (relative to the current directory), `--no-baseline` reads none. With a baseline the text table marks accepted rows `name ✓`, a line `baseline: N accepted, N worsened (<file>)` follows the summary, the summary's over-threshold count and `excess` leave accepted functions out, and the JSON gains `baseline` per function and group and `accepted` in the summary. A malformed file is exit 2. `--diff` ignores the baseline (it compares revisions, not debt). |
| `--pins` | List only the [pinned](#pinning-the-functions-you-are-working-on) functions, with `PINNED_CCN`, `PINNED_COG`, `ΔCCN` and `ΔCOG` columns and a `pins: N pinned, N shown, N gone (<file>)` line. Without a pin file the table is empty (`pins: none`); a malformed file is exit 2. Not combinable with `--diff`, `--by` or `--brief`. Every report marks pinned rows `name ★`. |
| `--show-hidden` | List the functions the baseline file's `hidden` section hides (see [Hiding functions and files](#hiding-functions-and-files)). By default they are left out of every project report — table, groups, `--brief`, `--md`, summary and JSON, with or without `--no-baseline` — and a line `hidden: N functions left out (.ike/complexity-baseline.json)` follows the summary (JSON: `summary.hidden`). With the flag they are listed, marked `name ✕` (JSON: `"hidden": true`). Files hidden through `complexity.exclude` are never measured either way. `--diff` and `--trend` ignore the set, as they ignore the baseline. |
| `--write-baseline` | Write every function at or above the CCN or cognitive threshold — of the named paths, `--no-tests` respected, the row filters not — into the baseline file (the project file, or `--baseline=PATH`), replacing it, and print `baseline: N functions over threshold (CCN ≥ 15 or cog ≥ 15) written to .ike/complexity-baseline.json` instead of the report. The same file `Complexity: Accept All Over Threshold` writes in the IDE. Not combinable with `--diff` or `--no-baseline`. |
| `--trend[=N]` | Measure the project at each of the last `N` commits (default: `complexity.trend_commits`, 30; first parent only, so a merge is one point) and print them oldest first: `COMMIT`, `DATE`, `SUBJECT`, `FUNCS`, then `CCN`, `COG`, `EXCESS`, `OVER` as `sum (Δ against the previous commit)`, and a `totals: <oldest> → <newest>: …` line — the [Trend tab](#the-trend-tab) on the command line. With paths, only files in those files and directories count; `--no-tests` and the thresholds apply. `--json` prints `{"commits": N, "points": [...]}` with the project summary fields per point. Needs a git repository (exit 2 otherwise); not combinable with `--diff`, `--by` or `--brief`. |

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

summary: 33769 functions in 3161 files · ΣCCN 136390 (avg 4.0, p50 2, p90 9, p99 28, max 168) · Σcog 122149 (avg 3.6, p50 1, p90 9, p99 37, max 237) · NLOC 466863 · cmt 12% · 1223 over threshold (CCN ≥ 15 or cog ≥ 15 or cmt ≥ 40%) · excess 41870
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
    "comment_lines": 63650, "comment_density": 0.12, "cmt_warn": 40,
    "accepted": 0,
    "ccn_p50": 2, "ccn_p90": 9, "ccn_p99": 28,
    "cog_p50": 1, "cog_p90": 9, "cog_p99": 37,
    "histogram": { "ccn": [29130, 3102, 812, 540, 185], "cognitive": [29954, 2010, 776, 590, 439] }
  },
  "functions": [
    { "file": "internal/config/validate.go", "name": "validate", "kind": "function",
      "line": 262, "end_line": 1047, "ccn": 168, "cognitive": 220,
      "nloc": 622, "params": 1, "depth": 5,
      "comment_lines": 101, "comment_density": 0.1397,
      "baseline": "accepted",
      "sites": [
        { "line": 264, "col": 1, "end_line": 270, "kind": "if", "ccn": 1, "cognitive": 1, "nesting": 0, "level": 0 },
        { "line": 264, "col": 6, "end_line": 264, "kind": "&&", "ccn": 2, "cognitive": 1, "nesting": 0, "level": 0 },
        { "line": 266, "col": 2, "end_line": 268, "kind": "switch", "ccn": 0, "cognitive": 2, "nesting": 1, "level": 1 }
      ],
      "candidates": [
        { "line": 266, "end_line": 268, "kind": "switch", "cog_saving": 4, "new_ccn": 3, "new_cog": 2, "rest_ccn": 166, "rest_cog": 214 }
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
level. `candidates` comes with the same flag: the function's extraction
proposals, best first — `line` / `end_line` the block's 1-based range,
`kind` the block (`if`, `for`, `switch`, `else`, …), `cog_saving` the
cognitive points the file would lose, `new_ccn` / `new_cog` the metrics
the extracted function would have, `rest_ccn` / `rest_cog` what the
function would keep (`rest_cog + new_cog == cognitive − cog_saving`,
`rest_ccn + new_ccn == ccn + 1`); a function without a block saving at
least 3 points has no `candidates` key. With `--diff --sites` every
`changed` entry's `after` and every `added` entry carry both. `baseline`
is `accepted`, `worsened` or `new`
against the [baseline](#accepted-debt-the-baseline) in effect and absent
without one; `pinned` (`{"ccn", "cognitive", "pinned_at"}`, the metrics at
pin time) is present on every pinned function — project `functions`, the
diff's `changed` and `added` — and absent otherwise; `summary.accepted` counts the over-threshold functions the
baseline accepts (always present, `0` without a baseline), while
`over_threshold` and `excess` count only the new and worsened ones.
`ccn_p50` … `cog_p99` are nearest-rank percentiles over the summarized
functions (0 when there are none), and `histogram` counts them per bucket
— `1–5`, `6–10`, `11–15`, `16–25`, `26+` in that order, a cognitive 0 in
the first — so each array sums to `functions`. Every summary carries them
(`summary`, and `base` / `head` under `--diff`); group rows do not.

Grouped report (`--by class --json`): `functions` gives way to `groups`,
one object per row the filter keeps, with the summary's field names; `class`
is present at the class level only. With `--churn`, every group gains `commits` and
`hot`, every function `commits`, and the summary `churn_days`. With a
baseline every group with an over-threshold or accepted function gains
`baseline`: `worsened` when any of its functions worsened, `accepted` when
every over-threshold function is accepted, `new` when one is not.

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
  "rev": "main", "sha": "658ccf37…", "head_rev": "working tree", "head_sha": "",
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

`rev` / `sha` name the base, `head_rev` / `head_sha` the head: `B` and its
commit with `--diff=A..B`, `"working tree"` and `""` otherwise.
`base` and `head` have the same fields as the project `summary`, over the
files that differ from `REV`; both, the top-level `delta`, every changed
function's `delta` and every group carry `excess`. `verdict` is
`more_complex`, `less_complex` or `unchanged`, decided by the excess first
(the rule changed — see [The Changes tab](#the-changes-tab)). A policy that
wants the old ΣCCN reading keeps it with `jq -e '.delta.ccn <= 0' cx.json`.

With `--diff --by file --json` the report also carries `files`, one entry
per file, the biggest CCN increase first:

```json
"files": [
  { "file": "cmd/ike/main.go", "changed": 1, "added": 1, "removed": 0,
    "ccn_before": 22, "ccn_after": 39, "ccn_delta": 17,
    "cog_before": 29, "cog_after": 50, "cog_delta": 21,
    "nloc_before": 103, "nloc_after": 140,
    "excess_before": 21, "excess_after": 30, "excess_delta": 9 }
]
```

Trend (`--trend --json`), the points oldest first, every `summary` with the
fields of the project report, `delta` against the previous point (all zero
on the oldest):

```json
{
  "commits": 30,
  "points": [
    { "sha": "600390f7…", "short": "600390f7", "time": "2026-10-14T09:12:40Z", "subject": "persist the index",
      "summary": { "files": 3161, "functions": 34811, "ccn_sum": 143365, "cog_sum": 128806, "excess": 18093, "over_threshold": 1380, "...": "…" },
      "delta": { "functions": 0, "ccn": 0, "cognitive": 0, "excess": 0, "over_threshold": 0 } },
    { "sha": "fd45e55c…", "short": "fd45e55c", "time": "2026-10-14T10:03:11Z", "subject": "diff between two revisions",
      "summary": { "files": 3163, "functions": 34828, "ccn_sum": 143516, "cog_sum": 128948, "excess": 18120, "over_threshold": 1384, "...": "…" },
      "delta": { "functions": 17, "ccn": 151, "cognitive": 142, "excess": 27, "over_threshold": 4 } }
  ],
  "skipped": { "files": 0, "languages": [] }
}
```

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

**Gating the merge commit.** On a `push` to a branch, or for the merge
commit GitHub builds for a pull request, compare two commits instead of the
working tree — nothing the job writes into the checkout can leak into the
numbers:

```yaml
      - name: Complexity of the merge
        run: |
          ike complexity --diff=origin/main..HEAD --json > cx.json
          jq -r '"\(.rev)..\(.head_rev) (\(.sha[:8])..\(.head_sha[:8])): \(.verdict)"' cx.json
          jq -e '.verdict != "more_complex"' cx.json
```

The same works locally without a checkout: `ike complexity
--diff=main..feature` reviews a branch, `ike complexity
--diff=main@{1.week.ago}..main` the last week of `main`.

**The ratchet.** With a committed [baseline](#accepted-debt-the-baseline)
the policy no longer needs a diff: every function that is not accepted is
either new debt or old debt that got worse, and the build fails on exactly
those — the accepted set can only shrink.

```yaml
      - name: Seed the baseline once (then commit .ike/complexity-baseline.json)
        if: ${{ false }}   # run by hand the first time, or when the thresholds change
        run: ike complexity --write-baseline
      - name: Complexity ratchet
        run: |
          ike complexity --json > cx.json
          jq -r '"\(.summary.over_threshold) over threshold, \(.summary.accepted) accepted"' cx.json
          jq -e '[.functions[] | select(.baseline != "accepted")] | length == 0' cx.json
```

The last line fails the step when any function is `new` or `worsened`; to
allow new functions under a higher bar, select on `.baseline == "worsened"
or (.baseline == "new" and .ccn >= 20)` instead. The job runs on the
checkout's own baseline, so a pull request that accepts more debt shows
that as a diff of the JSON file for review.

### Posting the verdict as a pull-request comment

`--diff --md` renders the comparison as Markdown — the verdict in bold, the
revisions and totals, then the changed, added and removed functions — so a
job can keep one comment on the pull request up to date:

```yaml
    permissions:
      pull-requests: write
    steps:
      # … checkout with fetch-depth: 0 and the ike build as above …
      - name: Complexity comment
        env:
          GH_TOKEN: ${{ github.token }}
        run: |
          ike complexity --diff=origin/${{ github.base_ref }} --md > cx.md
          # edit the job's last comment; post a new one the first time
          gh pr comment ${{ github.event.pull_request.number }} --edit-last --body-file cx.md \
            || gh pr comment ${{ github.event.pull_request.number }} --body-file cx.md
```

An action such as `marocchino/sticky-pull-request-comment` with `path: cx.md`
does the same. A report of more than 30 functions per section keeps the
rest inside a collapsed `<details>` block.

### For coding agents

An agent can ask "did I make this more complex?" with
`ike complexity --diff --json` (against `HEAD`, so it sees its own
uncommitted edits) and read `verdict`, `delta` and the `changed` entries.

IKE can run that check for the agent. Turn on **Agent verdict on Stop**
(`complexity.agent_verdict`, Settings ▸ Complexity, off by default) and
install the Claude Code hooks ("Claude Hooks: Install" — run it again if
you installed them before, so the `Stop` hook is added): at the end of
every agent turn IKE diffs the working tree against `HEAD` in its own
process and posts a row into the Agent Trace window of that session —
`complexity: LESS COMPLEX (excess −12)`, in the warning colour when the
excess rose, so you see at a glance whether the turn made things worse.
When the trace window is not showing that session the verdict arrives as a
notification instead. The hook itself never waits for the diff, so the
agent is not slowed down. Pressing `a` on a function asks the agent to
simplify it — see [Let an agent simplify it](#let-an-agent-simplify-it).
With a baseline, `ike complexity --json | jq '[.functions[] | select(.baseline
!= "accepted")]'` lists exactly the functions it has to simplify before the
ratchet passes, and `ike complexity --write-baseline` is how it records the
debt a maintainer asked it to accept.
