---
type: concept
title: Problems Tool Window
description: Singleton bottom-split pane aggregating LSP diagnostics project-wide — grouped by file, errors first, enter/double-click jumps to the location, 'f' toggles current-file vs project scope; pure consumer of the publishDiagnostics flow (#1024, part of #33).
resource: internal/problems/problems.go
tags: [architecture, lsp, diagnostics, tool-window, pane]
timestamp: 2026-07-23T00:00:00Z
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

No new LSP traffic originates in the pane.

## The pane

`internal/problems.Model` follows the VCS tool-window pattern
(`internal/vcspanel`): a value-type model embedded in a `pane.Instance`
(`pane.KindProblems`, singleton key `"problems"`, context id `"problems"`),
opened as a bottom split of the active editor by the `vcs.panel`-style toggle
state machine in `internal/app/problems_panel.go` (open → focus → return
focus).

Rows are the flattened grouping: one accented header per file, its
diagnostics beneath — severity glyph (`●` error, `▲` warning, `ℹ` info, `✦`
hint, colored from the theme's diagnostic slots), 1-based `line:col`, the
message's first line, plus the server's rule code in parentheses when sent
(#739). Files sort worst-severity-first then by path; within a file severity,
then line, then column. Unspecified severity counts as error, matching the
gutter. A refresh keeps the cursor on the same diagnostic where possible.

## Interaction

- `j`/`k`/arrows move, `g`/`G` home/end; `enter` opens the file with the
  cursor on the diagnostic (a header row opens the file's first diagnostic)
  via `problems.OpenLocationMsg` → `openPathAt`, the same navigation seam
  go-to-definition uses (0-based coordinates).
- Mouse mirrors the VCS panel (#514): click selects, double-click within
  400 ms activates, wheel scrolls dragging the cursor along.
- `f` toggles **current file** vs **project** scope (named in the footer).
  The active path tracks the focused editor via `syncProblemsActive`, hooked
  into `setFocus` and tab switching like the explorer's active-file accent.

## Persistence

The pane persists as identity kind `"problems"` and restores empty in its
saved slot — diagnostics are session state; the live store re-feeds it as
servers publish. It counts as a tool window for `window.hideAllTools` (#791).

`cmd+8` toggles the pane (#1048; JetBrains' cmd+6 is taken by the TODO
index #61, so the free numeric-family chord stands in — the palette is the
delivered fallback). Next/previous-diagnostic navigation stays with the existing
`lsp.nextDiagnostic` / `lsp.prevDiagnostic` commands.
