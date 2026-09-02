---
type: concept
title: Problems Tool Window
description: Singleton bottom-split pane aggregating LSP diagnostics project-wide — grouped by file, errors first, enter/double-click jumps to the location, 'a' applies a code action without leaving the pane, '/' filters with the shared list-filter syntax and 'f' is its current-file sugar; consumes the publishDiagnostics flow plus the Go-computed lint notes and per-run task-matcher findings (#1024, part of #33; notes #1654; tasks #1915; quick fixes #2175; shared filter #2156).
resource: internal/problems/problems.go
tags: [architecture, lsp, diagnostics, tool-window, pane, tasks, code-actions, filter]
timestamp: 2026-09-02T00:00:00Z
---

# Problems Tool Window (#1024)

JetBrains' Problems view scaled to the terminal: a singleton tool pane
(`problems.toggle`, palette "Problems") that lists every current LSP
diagnostic in the project, live-updating as servers publish. Part of the
umbrella idea #33.

## Data flow — a pure consumer

Diagnostics already arrive as `lsp.DiagnosticsMsg` / `lsp.DiagnosticsBatchMsg`
(coalesced, #597) in the root model's Update. Two consumers now share that
seam:

- the **editor route** (unchanged): `routeToEditor` feeds each open buffer's
  gutter/underline cache; unopened paths route to nothing;
- the **Problems store** (`problems.Store`, held as `Model.probStore` in
  `internal/app`): a session-wide `path → []Diagnostic` map replaced
  wholesale per publish. It keeps sets for files *no editor has open*, so the
  pane aggregates project-wide — how wide depends on the server (workspace
  -diagnostic servers report the whole project; per-document servers only
  files that were opened at some point). An empty publish deletes the path,
  so fixed files drop out.

The store carries a second, independent channel (#1654): the Go-computed lint
notes (#1623 language linters, #1654 Unicode hygiene) arrive with every
`highlight.SpansMsg`, converted via `editor.NoteDiagnostics` (source `lint`)
and stored per path through `Store.SetNotes`. The two channels replace
independently — a server publish never clobbers the lint findings, nor the
reverse — and `Get`/`Paths`/`Len` merge them, so the pane lists both. No LSP
traffic is involved in that channel at all.

A third channel holds task-run findings (#1915): problem matchers parse a
run's terminal output (see [Tasks & Problem Matchers](/architecture/tasks.md))
into diagnostics stored per run source through `Store.SetTaskSource(source,
byPath)` — keyed by the run configuration's name, replaced wholesale per
publish and cleared at every launch (`ClearTaskSource`), so a re-run
replaces its previous problems instead of duplicating them and never touches
another run's or the LSP's findings. `Get` appends task findings after the
server and lint sets; rows navigate like any diagnostic.

Both LSP consumers sit behind the app's diagnostic ignore filter (#1259,
`internal/app/diag_ignore.go`): a diagnostic matching an
`lsp.diagnostics_ignore` rule is dropped before either sees it, so the pane
and the editor decorations always agree. The per-severity decoration toggles
(`editor.marks.lsp_*`) are deliberately different: they only gate the
*painting* (scrollbar/gutter/underline) — the Problems pane keeps showing
every non-ignored diagnostic regardless, so nothing is silently lost.

No diagnostic traffic originates in the pane: it never asks for findings, it
only consumes what the flow publishes. The one request that *does* start here
is the quick-fix code action (#2175, below) — user-initiated, one round trip
per keypress, and routed through the bridge rather than issued by the pane.

## The pane

`internal/problems.Model` follows the VCS tool-window pattern
(`internal/vcspanel`): a value-type model embedded in a `pane.Instance`
(`pane.KindProblems`, singleton key `"problems"`, context id `"problems"`),
opened as an adaptive split of the active editor (`auxZone`, #1588 — below,
or right of a wide landscape host) by the `vcs.panel`-style toggle
state machine in `internal/app/problems_panel.go` (open → focus → return
focus).

Rows are the flattened grouping: one accented header per file, its
diagnostics beneath — severity glyph (`●` error, `▲` warning, `ℹ` info, `✦`
hint, colored from the theme's diagnostic slots), 1-based `line:col`, the
message's first line, plus the server's rule code in parentheses when sent
(#739). Files sort worst-severity-first then by path; within a file severity,
then line, then column. Unspecified severity counts as error, matching the
gutter. A refresh keeps the cursor on the same diagnostic where possible.

A diagnostic's **related information** (#2147) hangs under it as its own
faint, indented `↳ note  file:line` row — one per entry a server attached
("declared here", the competing branch of a type conflict), rendered from
`ilsp.RelatedInfo.Label()`, the same wording the
[diagnostic details popup](./lsp.md) uses. A related row carries its *own*
location, which routinely names another file: `enter` (and double-click)
opens *that* file at *that* position through the same
`problems.OpenLocationMsg` funnel, and `y` copies the entry rather than its
parent. Related rows are context, not findings — they never count toward the
header's error/warning totals — and the refresh cursor keep matches them by
their rendered text, since they share their parent's path and position.

## Interaction

- `j`/`k`/arrows move, `g`/`G` home/end; `enter` opens the file with the
  cursor on the diagnostic (a header row opens the file's first diagnostic)
  via `problems.OpenLocationMsg` → `openPathAt`, the same navigation seam
  go-to-definition uses (0-based coordinates).
- Mouse mirrors the VCS panel (#514): click selects, double-click within
  400 ms activates, wheel scrolls dragging the cursor along.
- `y` (or `cmd+c`/`super+c`) **copies the marked row** (#2071): a diagnostic
  as the line renders it — `path:line:col: message (code)`, the path
  project-relative — a file header as its path. The panel only emits
  `problems.CopyMsg`; the root model writes it through the shared
  `copyToClipboard` seam (system clipboard + clipboard history, #2061) and
  confirms with a "copied problem" toast. `ctrl+c` stays the global quit: the
  list has no text selection that could claim it (#2062).
- `/` — or the shared find chord `cmd+f` / `ctrl+f` (#2409) — focuses the
  **filter row**, see below.
- `f` toggles **current file** vs **project** scope (named in the footer).
  Since #2156 it is sugar over the filter: it writes `scope:file` into it and
  removes it again. The active path tracks the focused editor via
  `syncProblemsActive`, hooked into `setFocus` and tab switching like the
  explorer's active-file accent, and `scope:` resolves against it on every
  refresh — so the scope still follows the editor rather than pinning the
  path the key was pressed on.
- `a` (or `alt+enter`, the editor's intention key) **applies a quick fix** to
  the marked problem — see below.

## Filtering (#2156)

The row under the header is the shared filter line
([List Filter Syntax](./list-filters.md), `internal/filterbar` over
`internal/filterexpr`) — the same widget, the same syntax and the same `/`
focus key as the Usages pane, the TODO index and the Issues pane's saved
filters. It is permanent, so a filter appearing never shifts the list.

The pane's fields (`problems.Schema`):

| Field | Takes |
| --- | --- |
| `severity:` (alias `sev:`) | `error`, `warning`, `info`, `hint` — repeatable (OR) |
| `file:` | a path glob (`internal/**/*.go`, `*.ts`) or a substring |
| `code:` | a substring of the server's rule code (#739) |
| `source:` (alias `src:`) | a substring of the reporting server, linter or task |
| `scope:` | `file` (the active editor's file, what `f` writes) or `project` |

Bare words are the fuzzy match text, run over the message and the code.
Terms of different fields AND, repeats of one field OR. A file the filter
empties drops out entirely — header and all — and the header's error/warning
totals count what is *visible*, so the counts always describe the list under
them. `file:` completion offers the paths the pane currently lists.

## Quick fixes from the pane (#2175)

Fixing a listed problem used to mean jumping to it first and pressing
alt+enter there. `a` on a row does it where the problem is listed instead,
through the same seam the [intention popup](./lsp.md) uses — only entered
from outside an editor:

1. The pane resolves what the marked row stands for and emits
   `problems.QuickFixMsg`. A diagnostic row means itself; a file header its
   first (most severe) diagnostic, like `enter`; a **related-information row
   its parent** — the finding is what a server offers fixes for, its
   "declared here" note is not. An empty list emits nothing.
2. The root model runs `lsp.quickFixProblem`. Like `project.goToClass`, that
   command asks nothing by itself: it answers with `ilsp.QuickFixPromptMsg`,
   the bridge continuation. The app calls it with the marked row's path and
   the diagnostic's own range (`ilsp.QuickFixRequest`) — so the request
   carries its location rather than reading one off a caret, which is the
   whole reason no jump is needed. The command is in the palette too, where
   it means the same thing: fix the marked Problems row.
3. `bridge.quickFixAt` issues the ordinary `textDocument/codeAction` request,
   with the cached diagnostics overlapping the range as `CodeActionContext`
   — the same context alt+enter sends — and answers with an
   `ilsp.CodeActionsMsg` carrying `QuickFix`.
4. The app fills the *same* `actionsMode` the intention popup uses (LSP
   actions only: with no caret, no intention provider could apply) and opens
   the picker anchored under the marked row, `caretPopupAnchor`'s math for a
   list pane. Picking a row runs the bridge's `Apply` continuation.
5. Applying goes through the shared WorkspaceEdit path
   (`plugins/lsp/workspace_edit.go`): an open buffer gets one
   `FormatEditsMsg`, applied as a single undo unit, so `u` takes the fix
   back; a file no editor holds is rewritten on disk, exactly as the
   intention path does it.
6. Nothing refreshes the list by hand. The edit makes the server republish,
   the publish feeds the store, and the pane re-derives its rows — the pure
   consumer rule holds for fixes too.

The "no fixes" verdict is explicit: an offer with no actions closes nothing
and toasts `no quick fixes for this problem`. That is the honest answer for a
lint note (#1654), a task-matcher finding (#1915) or a file no language
server tracks — none of which any server has an action for.

## Persistence

The pane persists as identity kind `"problems"` and restores empty in its
saved slot — diagnostics are session state; the live store re-feeds it as
servers publish. It counts as a tool window for `window.hideAllTools` (#791).

`cmd+8` toggles the pane (#1048; JetBrains' cmd+6 is taken by the TODO
index #61, so the free numeric-family chord stands in — the palette is the
delivered fallback). Next/previous-diagnostic navigation stays with the existing
`lsp.nextDiagnostic` / `lsp.prevDiagnostic` commands.
