# Code complexity

IKE measures the **cyclomatic** (CCN) and **cognitive** complexity, the
non-comment lines (NLOC), parameters and nesting depth of every function in
the project — from the syntax tree it already has, with no external tool
installed. Inside the IDE the **Complexity** window (`cmd+alt+shift+x`)
lists every function and, on its Changes tab, compares the working tree
against a revision. The same report is available without a terminal UI, for
CI pipelines and coding agents: `ike complexity`.

Settings live under `[complexity]` (Settings ▸ Complexity): `exclude` globs,
and the warning thresholds `ccn_warn` / `cognitive_warn` (15 by default).

## Command line

```
ike complexity [--diff[=REV]] [--json] [--min N] [--ccn N] [--cog N] [DIR]
```

| Flag | Effect |
|---|---|
| *(none)* | Measure every supported file under `DIR` (default: the current directory) and print a table sorted by CCN, then a summary line. |
| `--diff[=REV]` | Compare the working tree (including uncommitted and untracked files) against `REV` (default `HEAD`; any branch, tag or sha) and print the changed, added and removed functions, the totals and a verdict. |
| `--json` | Print JSON instead of text (fields below). |
| `--min N` | Only list functions whose CCN **or** cognitive complexity is at least `N`. |
| `--ccn N`, `--cog N` | Override the warning thresholds; given explicitly they also filter the rows to functions at or above either one. |

The walk skips the same files as the Complexity window: hidden directories,
`node_modules`, `vendor`, `dist`, `build`, `target`, virtualenvs, files over
512 KiB, and everything matching `complexity.exclude`. The command reads
your user settings and `DIR/.ike/settings.toml` when they exist, defaults
otherwise. Files of a language IKE cannot measure (JSON, YAML, Markdown, …)
are counted in a final `skipped:` line.

```
$ ike complexity --min 40 .
FILE                         FUNCTION                  LINE  CCN  COG  NLOC  PARAMS
internal/config/validate.go  validate                  262   168  220  622   1
internal/editor/view.go      Model.renderSpanUncached  949   139  237  368   6

summary: 33769 functions in 3161 files · ΣCCN 136390 (avg 4.0, max 168) · Σcog 122149 (avg 3.6, max 237) · NLOC 466863 · 1223 over threshold (CCN ≥ 15 or cog ≥ 15)
shown: 2 of 33769 functions (CCN or cog ≥ 40)
skipped: 192 files (no complexity spec: json, markdown, yaml)

$ ike complexity --diff=main
complexity vs main (658ccf3)

changed (1):
  cmd/ike/main.go  main  L104  CCN 22→25 (+3)  cog 29→33 (+4)  NLOC 103→111 (+8)

added (1):
  internal/cli/cli.go  parseComplexity  L176  CCN 14  cog 17  NLOC 49

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
    "over_threshold": 1223, "ccn_warn": 15, "cog_warn": 15
  },
  "functions": [
    { "file": "internal/config/validate.go", "name": "validate", "kind": "function",
      "line": 262, "end_line": 1047, "ccn": 168, "cognitive": 220,
      "nloc": 622, "params": 1, "depth": 5 }
  ],
  "skipped": { "files": 192, "languages": ["json", "markdown", "yaml"] }
}
```

`summary` always covers every function; `functions` holds the rows the
filter keeps, sorted by CCN descending. `kind` is `function`, `method` or
`lambda`.

Diff report (`--diff --json`):

```json
{
  "rev": "main", "sha": "658ccf37…",
  "base": { "files": 12, "functions": 12, "ccn_sum": 40, "cog_sum": 38, "…": "…" },
  "head": { "files": 13, "functions": 13, "ccn_sum": 57, "cog_sum": 59, "…": "…" },
  "delta": { "ccn": 17, "cognitive": 21 },
  "changed": [
    { "file": "cmd/ike/main.go", "name": "main", "kind": "function", "line": 104,
      "before": { "ccn": 22, "cognitive": 29, "nloc": 103, "params": 0, "depth": 3 },
      "after":  { "ccn": 25, "cognitive": 33, "nloc": 111, "params": 0, "depth": 3 },
      "delta":  { "ccn": 3, "cognitive": 4, "nloc": 8 } }
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
          # cgo build (the default on ubuntu-latest, which ships a C compiler)
          git clone --depth 1 https://github.com/TrueDaerk/ike.git "$RUNNER_TEMP/ike-src"
          (cd "$RUNNER_TEMP/ike-src" && go build -o "$RUNNER_TEMP/bin/ike" ./cmd/ike)
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
