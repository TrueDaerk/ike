---
type: concept
title: Intention Actions
description: The alt+enter popup — LSP code actions merged with built-in caret-dependent intention actions through a plugin-registered provider seam, opened anchored at the caret, with a debounced diff preview of the highlighted action.
resource: internal/intention
tags: [architecture, intentions, code-actions, palette, plugins, shortcuts]
timestamp: 2026-08-28T00:00:00Z
---

# Intention Actions

Issues #2020, #2025, #2026, #2033, #2056, #2252. IntelliJ's alt+enter mixes server fixes with IDE intentions; IKE
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
internal/app/actionpreview.go the highlighted row's diff preview (#2252)
internal/palette/selection.go the selection debounce + footer seams (#2252)
internal/editor/intentions.go exported caret probes (diag/hunk/toggle/conceal)
plugins/lsp/codeaction.go     the offer's actionSet: lazy resolve, preview, apply
plugins/lsp                   Intentions=true offer + the position-gated rename provider
```

## The seam

A `Provider` is a pure function `Items(Context) []Item` plus an id. The
`Context` snapshots the caret at popup-open time: path, language id, 0-based
position, the caret line's text, whole-buffer line access, selection and
read-only-ness — plus **precomputed caret facts** (doc-path presence,
http-request block plus the response/environment facts beside it,
diagnostic/hunk/conflict at caret, tracked-in-repo, test at caret and its
debuggability, togglable word, conceal stand-in and its family, clipboard
occupancy). The facts come from state the editor and the app already cache, so
providers stay cheap and table-testable; the snapshot is built once per open
(`Model.intentionContext`).

An `Item` is `{Title, Kind, CommandID}` plus the optional `Preview` closure of
#2252 — **always a registered command, never new logic**. Activation funnels through `RunCommand` →
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

**Buffers with no file** (a `ctrl+t` tab, an unsaved `cmd+n` buffer, the split
pane a pasted response body lands in) take the bridge's short path: nothing to
ask a server about, so the offer is empty and only the built-ins that need no
path apply (curl line, JWT, conceal explain, value toggle, clipboard diff) —
plus, since #2033, **"Treat Buffer as …"**, which applies in any file-less
buffer and so makes that popup never empty. That answer is handed back as a
`tea.Cmd`, never `Send`, because it is produced on the Update goroutine;
`Send`ing it there froze the IDE (#2027, see
[plugins](./plugins.md#host-api)).

### Treat Buffer as … (#2033)

`bufferLangProvider` is the one entry that is about the **buffer** rather than
the caret, so it lists last. It is offered **only** in a buffer with no file
(`Context.Fileless`, carried explicitly so a zero `Context` still offers
nothing): a saved file is classified by its name, and the editor refuses the
override there — offering it would advertise a choice that cannot apply. The
current type rides in the title (`Treat Buffer as… (now markdown)`), so the
popup both shows what the buffer is treated as and changes it.

The item points at `editor.setBufferLanguage` like every other entry points at
a registered command; the picker, the status-line segment and the override
itself live in [Language Registry](./languages.md#buffer-language-override--treat-buffer-as--2033).
Because the gates that used to read "is this an `.http` *file*" now read the
buffer's *type* (`isHTTPBuffer`), a file-less buffer treated as HTTP offers the
full request block list — Run Request included — with the dispatch attributed
to the synthetic `buffer.http` source.

Once a type is chosen, two follow-up entries ride on it (#2056), both from the
same provider and both about the buffer rather than the caret:

| Entry | Gate | Command |
|---|---|---|
| "Open in jq Playground" / "Open in yq Playground" | `docpath.IsLang(LangID)` — the dialect follows the language — plus `Context.hasText()`, because the playground refuses an empty input | `json.jqPlayground` / `yaml.yqPlayground` |
| "Materialize to File" | `Context.LangExt != ""` — a language recognized by base name only (Dockerfile) has no extension to write a file under | `editor.materializeBuffer` |

`LangExt` is the new precomputed fact (the extension of the buffer's
`langPath()`), added rather than probed in the provider like every other
`Context` field. What materializing does — and where the file lands — is in
[Language Registry](./languages.md#language-tools-from-a-typed-buffer-2056).

## Diff preview of the highlighted action

Issue #2252. Intentions apply on selection, so for a non-obvious action the
only way to see what it does used to be doing it. The popup now shows, under
the result list, a **small inline diff of the row the highlight rests on** —
computed, never applied.

**Two sources, one shape.** An LSP row is resolved through the offer's
`actionSet` (`codeAction/resolve` when the action arrived edit-less, see
[lsp](./lsp.md)), converted, and rendered from the affected files' before/after
text (`previewFiles`, the multi-file rename dialog's payload builder, #2149).
A built-in row hands its edit over **through the provider seam**:
`Context.Preview(commandID)` is a pure function the app fills in
(`intentionPreview`), and `Context.PreviewFor(id)` wires it onto the item as a
lazy closure, so listing the items computes nothing and only the highlighted
row is ever asked. Both land as an `actionPreview` — a `diff.Result` or a note
— and render through `miniDiffLines`, the same inline renderer local history,
the change feed and the rename confirmation use.

**Nothing is applied to preview.** The LSP side runs the edits against copies
of the manager's document lines; the built-in probes
(`Model.ToggleValuePreview`, `Model.ConflictPreviewAtCaret`) read the buffer
and build strings — no recorder, no mutation, no dirty flag. Apply stays the
existing path: `RunCommand` for a built-in row, the bridge continuation for an
LSP one, which reuses the very action the preview resolved.

**Rows without a resolvable edit say so.** A command-style action (server
command, picker, side effect) has no edit at any point, so the footer reads
"no preview" rather than an empty diff; an edit that turns out to change
nothing reads "changes nothing here". Which built-ins opt in is deliberately
small — the entries that are pure buffer rewrites:

| Entry | Preview |
|---|---|
| `editor.toggleValue` | the caret line as it is → with the token flipped |
| `merge.acceptOurs` / `acceptTheirs` / `acceptBoth` | the whole conflict block → the kept side(s), ours first |
| `merge.keepManual` (#2258) | the whole conflict block → the same block with its marker lines stripped |

Everything else (copies, playgrounds, HTTP runs, blame, tests, the type pick)
previews nothing, which is the honest answer for an action whose effect is not
a buffer edit. A new previewable entry is one `Context.PreviewFor(id)` on the
item plus a case in `intentionPreview` — the probe itself belongs in the
editor, next to the command it mirrors, so the two cannot drift.

**Debounce.** Resolution hangs off two palette seams (`internal/palette/selection.go`):
`SelectionMode.SelectionChanged(sel, cx)` is called when the highlight
*settles* — every move schedules a `SelectionTickMsg` after
`SelectionDebounce` (120 ms) and each new move invalidates the previous tick,
the `LiveMode` pattern of #295 applied to the selection instead of the query —
and `FooterMode.Footer(sel, width)` renders the lines under the list behind
the same dim rule. Walking a long offer with a held arrow key therefore
resolves only the row one stops on; a resolved row is cached for the life of
the offer, so scrolling back costs nothing, and a reply that names another
path or an index no longer in the list is dropped rather than shown against
the wrong row. Opening the popup is a selection change too (the first row is
highlighted from the start), so `openIntentions` returns the kick. The footer
area is inert to clicks, and the anchor math budgets its height
(`actionPreviewMaxLines`, 8 diff lines) so the box does not have to move once
the first preview arrives.

The [Problems pane's quick fixes](./problems.md) (#2175) ride the same mode
and preview identically — the offer differs only in having no built-ins.

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

Gate the new entry against *everything* its command refuses on, not only the
caret fact — see the applicability rule below.

## The catalog

Each entry delegates to the existing command; applicability per caret:

| Context | Items (command ids) |
|---|---|
| JSON/YAML value (`DocPath`) | copy path as jq / yq / dotted (`editor.copyDocPath*`); `json.jqPlaygroundAtPath` (not for YAML — jq reads JSON — and not with a selection, which the caret's path does not index) |
| caret in an HTTP request block (`isHTTPBuffer` + `httpfile.RequestAt` — an `.http`/`.rest` file or a buffer treated as HTTP, #2033) | `http.run`, `http.copyAsCurl`; with a shown response `http.copyBody` / `http.copyHeaders` / `http.resend`; with an env file `http.selectEnvironment` |
| curl command line that parses (`httpfile.CurlCommandAt` + `ParseCurl`, any buffer) | `http.insertCurlAsRequest` (new, see below) |
| JWT on the line (`jwt.At`) | `editor.decodeJWT` |
| conceal stand-in under caret (`ConcealExplainAtCaret`) | `editor.explainConceal`; the family's `view.toggle*` via the `concealToggles` map |
| ignorable diagnostic on caret line (`ilsp.IgnoreRuleFor`) | `lsp.ignoreDiagnostic` |
| hunk under caret / conflict block / tracked file (+selection) | `vcs.revertHunk`; `merge.accept{Ours,Theirs,Both}` + `merge.keepManual` (the rewriting entries only in a writable buffer); `vcs.blameLine`, `vcs.historyForSelection` |
| test at/above caret (`lang.HasTests` + `NearestTestAt`) | `run.testAtCursor`; `debug.testAtCursor` only with a debug adapter and no running session |
| togglable caret word (writable buffer) / selection + non-empty clipboard | `editor.toggleValue`; `diff.compareWithClipboard` |
| buffer with no file (`Fileless`, #2033) | `editor.setBufferLanguage` — "Treat Buffer as …", naming the current type |
| typed file-less buffer (`Fileless` + a language, #2056) | `json.jqPlayground` / `yaml.yqPlayground` over the buffer's own text (JSON/YAML with text in it); `editor.materializeBuffer` (a type with an extension) |

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
(`httpfile.ParseCurl`/`FormatRequest`). In a *writable* `.http` buffer the
command lines are replaced in place as one text edit (single undo);
elsewhere — including a read-only preview, which would drop the edit through
the locked recorder — the block lands in a fresh scratch `.http` file which
opens focused. The ignored-flags warning is preserved either way.

## The applicability rule

Issue #2026. **An entry is offered only where its command would actually do
something.** "Pick it and read the error" is a bug, and so is picking it and
watching nothing happen: the popup is a claim about this caret, and every row
that cannot deliver makes the rows that can harder to find.

Applicability is therefore *all* of the preconditions the command checks, not
just the positional one — the caret situation, the buffer state (a read-only
buffer drops every edit through the locked recorder, #1762) and the external
state the command reads (a shown HTTP response, an env file, the clipboard, a
debug adapter). #2026 audited the whole catalog against that rule; what it
tightened:

| Entry | Was offered on | Now also needs |
|---|---|---|
| `editor.explainConceal` | any value the explainer resolved — i.e. every identifier | a conceal stand-in under the caret (`concealAtCaret`). The "why is this *not* masked" reading (#1930) stays on `g?` and the palette |
| `http.copyBody` / `http.copyHeaders` | the caret's request block | a visible response pane with that text |
| `http.resend` | the caret's request block | a shown response carrying its request snapshot |
| `http.selectEnvironment` | the caret's request block | an `http-client(.private).env.json` beside the buffer defining ≥ 1 environment |
| `http.insertCurlAsRequest` | the `curl ` prefix on the caret line | the gathered command to parse (`ParseCurl`) — no URL, a dangling flag value or an unterminated quote is no offer |
| `json.jqPlaygroundAtPath` | any non-YAML doc path | no selection — against one the caret's path indexes a document the input does not contain, and the seeded open silently degrades to the plain one |
| `debug.testAtCursor` | a test at the caret | `lang.SupportsDebug` for the file *and* no session running or launching |
| `vcs.blameLine` / `vcs.historyForSelection` / `vcs.revertHunk` | `snap.Status(path) != untracked`, which a file from outside the repo also satisfies | `snap.Contains(path)` |
| `editor.toggleValue`, `merge.accept*`, `vcs.revertHunk` | the caret fact alone | a writable buffer |
| `diff.compareWithClipboard` | a selection | a non-empty clipboard |
| `lsp.ignoreDiagnostic` | any diagnostic on the line | one `ilsp.IgnoreRuleFor` can build a rule from |

Verified as already exact and left alone: the three `editor.copyDocPath*`
flavours, `http.run` and `http.copyAsCurl` (the caret block *is* the
precondition; `http.run`'s "already running" refusal is a deliberate
duplicate guard, not an inapplicable caret), `editor.decodeJWT` (the gate
calls the same `jwt.At`), the `view.toggle*` family switches (always
executable), `merge.accept*` beyond the read-only gate, and
`run.testAtCursor` (`run.TestConfig` succeeds exactly when `lang.HasTests`
does). Rename is #2025's, gated in the bridge.

Where a check is too expensive to run synchronously — a server round trip —
the answer is precomputed *before* the popup queries the providers, the way
#2025 validates `prepareRename` alongside the code-action request. Everything
in the table above is a cheap local probe (a map lookup, a pure parse, two
small file reads the dispatch performs anyway), so it rides in `Context`. Two
of them are still paid only when they can matter: the env files are read only
for a caret inside a request block, and the clipboard — whose read runs a
helper process — only when there is a selection to compare.

## Testing

`internal/intention/catalog_test.go` is the table-driven applicability
matrix (one row per caret situation, want/want-not command ids);
`internal/app/codeactions_test.go` covers merge order, both activation paths,
the anchored open over a JSON buffer, the empty-merge toast, the fileless
cases (#2027: applicable built-ins open the anchored picker; since #2033 the
otherwise featureless fileless buffer still offers the type pick) and the
digit shortcuts (hints on the first nine unfiltered rows, digit runs on an empty
query, digit filters once a query is typed) plus the #2026 gates that need a
whole model (HTTP response and env-file facts, clipboard, read-only buffer);
`internal/palette/digit_test.go`
covers the `DigitPicker` seam itself (fast path, out-of-range digit, opt-out
modes, hint rendering); `internal/palette/selection_test.go` the #2252 seams
(the settled row is reported, a burst's stale ticks are dropped, a closed
palette reports nothing, a query edit schedules one, the footer renders under
the list and is click-inert); `internal/app/actionpreview_test.go` the popup
half (a built-in edit renders as a diff, the buffer and its dirty flag are
untouched, a command row says "no preview", an LSP row resolves once and its
reply renders, a reply for another offer is ignored, opening schedules the
debounce); `plugins/lsp/codeaction_test.go` the bridge half against a scripted
server (a lazy action resolves once and previews before/after, the synced
document is unchanged and no edits are dispatched, a command action previews a
note, apply reuses the preview's resolve and applies exactly it, apply without
a preview resolves on its own); `internal/editor/intentionpreview_test.go` the
two read-only probes (flipped line, kept conflict side, buffer untouched, and
that applying writes what was previewed); `internal/intention/preview_test.go`
the provider seam (rewriting entries carry a preview, it is computed only when
asked, command entries carry none, a context without the app's function wires
nothing);
`internal/editor/intentions_test.go` covers the tightened caret probes (a
plain identifier is not a concealed value, a mask and a size hint are, a
diagnostic needs an ignore rule); `internal/app/intentions_test.go` covers the
curl conversion (in-place, scratch, continuations, dropped-flag notice,
read-only fallback); `internal/httpfile` covers the shared `CurlCommandAt`
probe and `internal/vcs` the `Snapshot.Contains` distinction;
`internal/app/bufferlang_test.go` covers the #2033 entry (offered fileless,
hidden with a file, naming the current type), the picker (refusal with a file,
locked open, row list), the applied type (language resolution, status-line
segment, clearing) and the HTTP intentions it unlocks;
`internal/editor/langoverride_test.go` the override resolution itself; `plugins/lsp/renamegate_test.go`
drives the position gate against a scripted server (accepted caret, rejected
caret, no verdict, no `prepareRename` support, verdict dropped by an edit,
verdict reused on pick); `internal/registry` covers provider dedup/order.
