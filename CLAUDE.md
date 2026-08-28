# CLAUDE.md

Guidance for Claude Code (and humans) working in this repository.

## Project

**IKE** is a terminal IDE built with [bubbletea](https://github.com/charmbracelet/bubbletea).
It is a Jetbrains inspired TUI IDE, but with vim like controls in the editor.
It supports windowing, tabs, panes and resizing / moving any pane to another location.

## Where to look first

- **All planning lives in GitHub issues** on `TrueDaerk/ike`: specs are held verbatim in
  **epic** issues, work items are sub-issues linked from the epic's task list, and GitHub
  **milestones** (one per epic) track progress. Full structure, labels, conventions and the
  duplicate check: [wiki/process/issues.md](wiki/process/issues.md).
- **The wiki** (`wiki/`) holds the concept documentation — architecture per subsystem plus the
  process docs. Start at [wiki/index.md](wiki/index.md).

## Working agreements

- **Language:** write all code, comments, doc strings, descriptions, and commit messages in **English**,
  unless explicitly asked otherwise. (Conversational replies may be in the user's language.)
- **Testing & coverage:** new code should ship with tests.
- **Settings need UI:** every new setting must also be configurable in the **Settings UI**
  (`internal/settings/`) in the *same* change — never config-file-only. Validate it in the form
  (rejecting bad input with a clear message), persist it, show it in any list rendering, and test
  it (see [wiki/process/change-workflow.md](wiki/process/change-workflow.md)).
- **Commands need a keybind:** every new command ships with a **default keybind** in the same
  change (`internal/keymap/defaults.go`); keybind-less only with a reason recorded in the audit
  ledger `cmd/ike/keybind_audit_test.go`, whose test fails otherwise
  (see [wiki/process/change-workflow.md](wiki/process/change-workflow.md)).
- **Keep the wiki current:** when a change alters behavior the wiki documents, update the matching
  concept doc in the same change (see [wiki/process/wiki-format.md](wiki/process/wiki-format.md)
  for the OKF format rules).

## Model / effort labels on issues

Every work-item issue should carry two labels telling automation (e.g. `issue_runner.py`)
which Claude model and reasoning effort to use:

- **`model:opus`** — simple to medium tasks (mechanical changes, small features, doc work).
- **`model:fable`** — complex tasks that need a lot of reasoning (architecture, tricky bugs,
  cross-cutting changes).
- **`effort:low` / `effort:medium` / `effort:high`** — reasoning effort; `high` is the maximum
  allowed.

When creating a new issue, add one `model:*` and one `effort:*` label. If they are missing,
runners default to **fable / medium**.

## Size labels on issues

Every work-item issue should also carry one rough duration-estimate label, in a shared
purple color family distinct from `model:*` (blue), `effort:*` (orange) and `priority:*`
(red/green scale):

- **`size:1d`** — roughly one day or less.
- **`size:1-7d`** — between one and seven days.
- **`size:7d+`** — more than seven days.

When creating a new issue, add one `size:*` label alongside `model:*`/`effort:*`. If it's
missing there is no default — the issue is simply untagged for size.

## Change workflow

Every change follows the same loop (details in
[wiki/process/change-workflow.md](wiki/process/change-workflow.md)):

1. **Issue first** — before any change, make sure a GitHub issue exists (duplicate check!);
   create one otherwise.
2. **Branch per issue** — work on `issue/<number>-<slug>`, branched from an up-to-date `main`.
3. **When done, bump the patch version** — raise the last component of `Version` in
   `internal/version/version.go` (e.g. `0.1.6` → `0.1.7`) in its own `chore(version)` commit.
4. **Open a PR on GitHub** — body says `Closes #<number>`.
5. **Merge into `main`** — merging closes the issue and finishes the task; don't leave PRs open
   or issues dangling.
6. **Clean up** — delete the issue branch (local and remote), switch back to `main`, pull.
