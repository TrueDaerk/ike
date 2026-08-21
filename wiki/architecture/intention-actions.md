---
type: concept
title: Intention Actions
description: The alt+enter popup — LSP code actions merged with built-in caret-dependent intention actions through a plugin-registered provider seam, opened anchored at the caret.
resource: internal/intention
tags: [architecture, intentions, code-actions, palette, plugins, shortcuts]
timestamp: 2026-08-21T12:00:00Z
---

# Intention Actions

Issues #2020, #2025. IntelliJ's alt+enter mixes server fixes with IDE intentions; IKE
does the same: one caret-anchored popup that merges **LSP code actions**
([lsp](./lsp.md), #8) with **built-in intention actions** whose applicability
is decided from the caret context. Before this slice the popup was
LSP-only — almost always empty — while the context-specific capabilities
(copy jq path, run request, revert hunk, decode JWT, …) sat invisible in the
command palette exactly when they applied.

## Structure

```
internal/intention/           the seam: Context, Item, Provider + the built-in catalog
internal/plugin/plugin.go     Capabilities.Intentions — providers as a plugin capability
internal/registry/            IntentionProviders() — dedup + deterministic order
internal/app/intentions.go    caret snapshot, merge, caret-anchored open, curl insert
internal/app/codeactions.go   the palette mode over the merged entry list
internal/editor/intentions.go exported caret probes (diag/hunk/toggle/conceal)
plugins/lsp                   Intentions=true offer + the position-gated rename provider
```

## The seam

A `Provider` is a pure function `Items(Context) []Item` plus an id. The
`Context` snapshots the caret at popup-open time: path, language id, 0-based
position, the caret line's text, whole-buffer line access, selection — plus
**precomputed caret facts** (doc-path presence, http-request block,
diagnostic/hunk/conflict at caret, tracked-in-repo, test at caret, togglable
word, conceal explanation and its family). The facts come from state the
editor and the app already cache, so providers stay cheap and table-testable;
the snapshot is built once per open (`Model.intentionContext`).

An `Item` is `{Title, Kind, CommandID}` — **always a registered command,
never new logic**. Activation funnels through `RunCommand` →
`dispatchCommand`, so `EventCommandExecuted` fires (#679) and every entry is
also reachable the ordinary ways (palette, chords, menus). A provider
therefore contributes *visibility at the caret*, nothing else.

**Registration is a plugin capability** (`Capabilities.Intentions`),
mirrored from Commands: the registry dedups provider ids (first owner by
sorted plugin order wins) and preserves registration order within a plugin.
This was chosen over an app-assembled provider list so any plugin — the LSP
bridge today, third-party/Wasm plugins later — can contribute without an app
change; the built-in catalog simply ships through the `app` plugin
(`intention.Builtins()`), the editor-state probes travel inside `Context`
rather than through provider-side reach-ins. The right-click context menu
(`editorContextItems`, #1020) is deliberately compatible: it could be fed
from the same providers later.

## The flow

`alt+enter` keeps the `lsp.codeAction` command id (JetBrains keymap import
maps `ShowIntentionActions` to it; the blocked-key matrix references it) but
is retitled "Show Intention Actions". The bridge sends its offer as
`CodeActionsMsg{Intentions: true}` on **every** path — no manager, empty
reply, failed request — and no longer toasts. The app merges:

1. preferred LSP actions (★, stable),
2. remaining LSP actions in server order,
3. built-in items grouped by kind (kinds in first-appearance order, stable
   within one).

Empty merged list → the "no code actions here" toast (only honest verdict
lives here now). Otherwise the picker opens **anchored one row below the
caret** (`caretPopupAnchor` — the `compositeLSPPopups` math: pane rect +
gutter + `DisplayOffset`/`DisplayRow`, shifted left at the right edge,
flipped above when it would cross the bottom), falling back to the centered
locked box without a laid-out editor pane. Fuzzy filtering works as before;
built-in kinds ("copy", "http", "vcs", "test", …) pass through
`actionKindLabel` unchanged. The code-lens picker (#1912) leaves
`Intentions` false and keeps the centered list.

## Digit shortcuts

Issue #2023. The common case is "pop the list, run the first or second
entry", so the popup numbers its rows: the **first nine listed entries carry
a `1`–`9` hint** in front of the title, and while the query is **empty** that
digit runs the entry directly — the same dispatch as selecting the row and
pressing enter. Arrow keys, enter and fuzzy filtering are unchanged; a digit
past the last row is swallowed (the popup stays open) so the hints and the
accepted keys never disagree.

As soon as a filter query is typed the digits are ordinary query text again
(titles like "Run test 2" stay reachable) and **the hints disappear** rather
than renumbering against the filtered list — a visible number that no longer
runs anything would lie. This is the chosen half of the #2023 design
alternative.

The seam is `palette.DigitPicker`, an optional Mode extension
(`DigitShortcuts() bool`) the palette consults only for the **locked** mode
and only on an empty query; `actionsMode` is its single implementer, so every
other palette mode keeps plain digit typing. The hint itself is `Item.Hint`,
a dim leading column rendered before the title (it does not shift the fuzzy
`Spans`); `actionsMode.Results` assigns it by result index, so the numbers
follow the kind-grouped merge order (★ preferred LSP, LSP, built-ins by
kind). No setting — this is fixed UX.

## Adding new actions

**When a new action or command is added to IKE and its applicability is
caret- or context-dependent, surface it as an intention item too.** The
alt+enter list is only as useful as its coverage: a command that is only
meaningful "here" (at this caret, in this file type, on this hunk) but is
reachable only through the palette is invisible exactly when it applies.

The cost is small — add an `Item{Title, Kind, CommandID}` to an existing
provider in `internal/intention` when the caret fact already exists, or write
a new `Provider` (plus, for a plugin, register it via the `Intentions`
capability) when it needs a new one. Precompute any new caret fact into
`Context` in `Model.intentionContext` instead of reaching into editor state
from the provider, keep the item pointing at a **registered command**, and
extend the `catalog_test.go` applicability table. Globally applicable
commands (save all, open settings, …) stay out — they belong in the palette.

## The catalog

Each entry delegates to the existing command; applicability per caret:

| Context | Items (command ids) |
|---|---|
| JSON/YAML value (`DocPath`) | copy path as jq / yq / dotted (`editor.copyDocPath*`); `json.jqPlaygroundAtPath` (not for YAML — jq reads JSON) |
| caret in `.http` request (`httpfile.RequestAt`) | `http.run`, `http.copyAsCurl`, `http.copyBody`, `http.copyHeaders`, `http.resend`, `http.selectEnvironment` |
| curl command line (`httpfile.IsCurlCommand`, any buffer) | `http.insertCurlAsRequest` (new, see below) |
| JWT on the line (`jwt.At`) | `editor.decodeJWT` |
| explainable value (`ConcealExplainAtCaret`) | `editor.explainConceal`; the family's `view.toggle*` via the `concealToggles` map |
| diagnostic on caret line | `lsp.ignoreDiagnostic` |
| hunk under caret / conflict block / tracked file (+selection) | `vcs.revertHunk`; `merge.accept{Ours,Theirs,Both}`; `vcs.blameLine`, `vcs.historyForSelection` |
| test at/above caret (`lang.HasTests` + `NearestTestAt`) | `run.testAtCursor`, `debug.testAtCursor` |
| togglable caret word / selection | `editor.toggleValue`; `diff.compareWithClipboard` |

The LSP plugin adds "Rename Symbol" (`lsp.rename`), gated **twice** (#2025):
the attached server must declare rename at all (`Manager.RenameSupported`) and
`prepareRename` must accept this exact caret. The position check cannot live in
the provider — `Items` is synchronous, the check is a server round trip — so
the bridge validates instead (`plugins/lsp/renamegate.go`): `codeAction` fires
`prepareRename` *concurrently* with the code-action request, so the popup waits
on one round trip rather than two, and records the verdict for that one
`(path, line, col)` before sending the message that makes the app query the
providers. An unvalidated caret is not offered, and an edit to the buffer drops
the verdict. Picking the entry reuses it instead of asking again — which is what
makes the "cannot rename here" toast unreachable from the popup; before #2025
the entry showed up in every Markdown paragraph and led there. Servers without
`prepareRename` keep the #426 contract: the manager answers ok without asking,
so the entry stays offered and the rename attempt decides.

**`http.insertCurlAsRequest`** is the one intention without a pre-existing
command: the caret line's curl command (plus backslash continuations,
flattened like a pasted import) runs through the #1994 parser
(`httpfile.ParseCurl`/`FormatRequest`). In an `.http` buffer the command
lines are replaced in place as one text edit (single undo); elsewhere the
block lands in a fresh scratch `.http` file which opens focused. The
ignored-flags warning is preserved either way.

## Testing

`internal/intention/catalog_test.go` is the table-driven applicability
matrix (one row per caret situation, want/want-not command ids);
`internal/app/codeactions_test.go` covers merge order, both activation paths,
the anchored open over a JSON buffer, the empty-merge toast and the digit
shortcuts (hints on the first nine unfiltered rows, digit runs on an empty
query, digit filters once a query is typed); `internal/palette/digit_test.go`
covers the `DigitPicker` seam itself (fast path, out-of-range digit, opt-out
modes, hint rendering);
`internal/app/intentions_test.go` covers the curl conversion (in-place,
scratch, continuations, dropped-flag notice); `plugins/lsp/renamegate_test.go`
drives the position gate against a scripted server (accepted caret, rejected
caret, no verdict, no `prepareRename` support, verdict dropped by an edit,
verdict reused on pick); `internal/registry` covers provider dedup/order.
