---
type: concept
title: LSP & Language Intelligence
description: The Language Server Protocol client — JSON-RPC over a server's stdio, a manager mapping (language, workspace root) to one server, editor-driven text sync, and diagnostics/completion/hover/signature-help/go-to-definition/find-references/document-highlight/inlay-hints/call-hierarchy/formatting/rename/code-actions/code-lenses/folding-ranges/semantic-tokens/selection-ranges/willRenameFiles rendered back into the editor.
resource: internal/lsp
tags: [architecture, lsp, language-server, jsonrpc, diagnostics, completion, hover, definition, plugins]
timestamp: 2026-08-21T12:00:00Z
---

# LSP & Language Intelligence

Roadmap 0100. IKE speaks the [Language Server Protocol](https://microsoft.github.io/language-server-protocol/)
to get real language intelligence: diagnostics, autocomplete, hover, and
go-to-definition. The first increment shipped **Go (gopls)**, **PHP (intelephense)**
and **Python (pyright)**; later increments added references, rename, formatting,
code actions, signature help, semantic tokens, and (#1912) code lenses, server
folding ranges, selection ranges and `workspace/willRenameFiles`.

Everything async respects the bubbletea event loop: no LSP I/O ever blocks
`Update`. Server traffic runs on goroutines and results re-enter the program as
`tea.Msg`s injected through the host's `Send` (see [Plugin Extension
Contract](./plugins.md)). The companion lexical layer is [Syntax
Highlighting](./highlighting.md).

## Layers

```
internal/lsp/
  jsonrpc/   JSON-RPC 2.0 over an io.ReadWriteCloser: Content-Length framing,
             request/response/notification, async read loop, id correlation.
             Responses always carry a result or error property — a nil result
             serializes as an explicit JSON null (#991): vscode-jsonrpc-based
             servers (Intelephense) die on a response with neither.
             Outbound writes are async too (#594): callers marshal on their own
             goroutine and enqueue the framed payload onto a queue drained by a
             single dedicated writer goroutine. A caller therefore never blocks
             on the server draining its stdin — critical because the bubbletea
             Update goroutine sends didChange from here per keystroke, and a
             busy server (indexing a large workspace) that stalls its stdin
             would otherwise freeze the whole event loop. The queue is bounded
             (#1542): full-sync didChange frames coalesce per URI (a queued
             frame's payload is replaced in place, so only the newest whole
             document is buffered per file), and a queue outgrowing 32 MiB
             tears the connection down with ErrQueueFull — surfacing through
             Done/Err like a crash — instead of buffering without limit.
  transport/ spawn a server over stdio (cmd/args/env/cwd), capture stderr,
             watch for exit. Pure Go — no CGo — so the client cross-compiles.
  protocol/  LSP wire types + the SINGLE position-encoding boundary (convert.go):
             editor rune columns <-> LSP UTF-16 (or negotiated UTF-8/UTF-32).
  client/    one Client per server: initialize/initialized/shutdown handshake,
             cached + feature-gated ServerCapabilities, typed request/notify calls.
             The handshake is a hard gate (#937): notifications fired before the
             initialize response arrives are queued and flushed right after
             initialized (in order), requests wait for the gate — servers crash
             on traffic that races the handshake (Intelephense dies on an early
             didOpen/initialized).
  manager/   owns every server: maps (language, workspace root) -> Client, detects
             roots from root_markers, spawns lazily, routes ops, recovers from
             crashes (restart.go), and injects toolchain settings at spawn.
  config.go  ServerSpec (aliased from the lang registry) + Overlay: parse the
             [lsp.servers.<id>] config overlay onto the language's baseline.
  messages.go editor-facing tea.Msg types + protocol->editor conversion helpers.
```

Server baselines (command, args, root markers) come from the [language
registry](./languages.md) — each language plugin's `lang.Language.Server` — not
from LSP itself; `[lsp.servers.<id>]` config only *overlays* them. The `plugins/lsp`
compile-in plugin is the wiring layer: it enables the subsystem, owns the
`manager.Manager`, installs the editor-event bridge, and
exposes `lsp.hover` / `lsp.parameterInfo` / `lsp.diagnosticInfo` / `lsp.definition` / `lsp.peekDefinition` / `lsp.references` / `lsp.callHierarchy`
/ `lsp.goToSuper` / `lsp.implementations` / `lsp.typeHierarchy` (0480, #1448)
/ `lsp.rename` / `lsp.codeAction` / `lsp.documentSymbols`
(the [Structure pane](./structure-view.md)'s refresh, #1025) / `lsp.restart`
as registry commands. The `lsp.format` / `lsp.formatRange` commands moved to
`plugins/format` with #1401 — reformat resolves through the [formatter
registry](./format.md), with this plugin's LSP provider as one chain entry.

Navigation jumps (`lsp.definition`, a references pick — both funnel through
`DefinitionMsg` into `openPathAt`) focus the pane where the target file is
already open instead of opening a duplicate tab in the current pane (#930,
the cross-pane extension of the #272 same-pane dedupe; #509 precedent for
diffs). Only an unopened target opens as a tab in the current pane.

`lsp.definition` consults IKE-side **local definition providers** first
(#922, `ilsp.RegisterLocalDefinition`): a plugin whose navigation target no
server resolves (Ansible inventory hosts/groups) claims the jump and skips
the server round-trip — it works with no server installed. Providers claim
narrowly; a pass falls through to the server exactly as before.

The seam widened with #1629 into a **local provider family**: every provider
now receives the full document lines (the bridge's synced copy, disk when no
server tracks the file — same-file targets like a YAML anchor cannot be found
from one line), and `ilsp.RegisterLocalHover` / `ilsp.RegisterLocalReferences`
sit beside the definition registry with the same first-claim-wins contract.
`lsp.hover` (key and mouse-idle flows alike), `lsp.references` and
`lsp.referencesPanel` consult them before the server; a panel claim carries a
Refresh continuation that re-resolves local-first. First user: YAML
anchors/aliases (`plugins/languages/yaml/anchors.go`, scanner in
`internal/yamlanchor`) — goto-definition on `*name` jumps to `&name`,
find-usages on either lists every mark of the name with line previews, and
hover on an alias shows the anchored node's value as a highlighted `yaml`
fence, dedented, `<<:` merge keys spliced in recursively (cycle-guarded,
capped at 16 lines).

**Peek definition** (#1154, `lsp.peekDefinition` — palette + the editor
context menu next to Go to Definition): the same resolution as
`lsp.definition` (local providers, server request, the #279 multi-target
picker — with peek intent preserved, so picking a candidate peeks it), but a
single target delivers a `PeekDefinitionMsg` instead of navigating. The app
(`internal/app/peek.go`) reads a **bounded excerpt** — up to 15 lines
starting 3 above the definition line, from the **live buffer** when the
target file is open in any tab (unsaved edits must show; disk would be
stale), else from disk with a scan that stops after the excerpt; a failed
read surfaces as a notice — and opens the popup on the focused editor
(`internal/editor/peek.go`), a sibling of hover on the same popup surface
(#316 frame, cursor-anchored, composited by `compositeLSPPopups`), titled
`path:line` (over-long paths truncate from the left so the filename stays).
The excerpt is syntax-highlighted with the target file's language via the
standalone `highlight.Highlight` entry point, the way hover code fences are
(#379); no grammar renders plain. While open the popup owns a few keys:
**esc** closes, **enter jumps for real** through the same
`DefinitionMsg`/`openPathAt` funnel (nav history records, pane dedupe #930
applies), up/down scroll one row and ctrl+d/ctrl+u half a window through an
excerpt longer than its 8-row window; **any other key closes the peek and is
handled normally** (the hover-dismiss precedent), as does a mouse click.

## Data flow

**Edits → server.** The editor emits change / cursor-move / completion-trigger
events through its `Emitter` seam (`internal/editor/events.go`). The app installs
a stateless adapter on every editor that forwards these to the host
(`host.EmitEditor`), which fans them to the LSP bridge (registered via
`host.SetEditorEmitter`). Programmatic cursor placement (`editor.SetCursor` —
go-to-definition landings, usages picks, nav back/forward, session restore)
emits a cursor-move too, so the bridge's tracked position always matches the
visible cursor and position-based actions (rename, references, hover) right
after a jump act on the landed symbol, not the departure (#371). On a change the bridge hands the full document text to
the manager, which **respects the negotiated `TextDocumentSyncKind`** (#13): an
incremental server gets the minimal contiguous change region — recovered by
common-prefix/suffix diffing against the previously synced lines
(`manager/incremental.go`), one range + replacement text per keystroke — a
full-sync server gets the whole document, a SyncNone server nothing. Range
positions cross into the negotiated encoding through `protocol/convert.go`
only; per-document versions stay monotonic and only advance when a
notification is actually sent (an unchanged text sends nothing). The change is
**coalesced** (#595): each edit only stores the latest text and (re)arms a short
`changeDebounce` (40ms), so a typing burst collapses to one sync and the
O(document) diff runs on the debounce goroutine, not the bubbletea Update loop.
Any request (`cur()` is the choke point; completion, signature and save flush
explicitly) drains the pending change first, so a completion or hover never acts
on stale server text; a close cancels it so no sync lands after `didClose`. A
file-open hook drives `didOpen`, save drives `didSave`, close drives `didClose`.
The close side (#827) is centralised in the root model: every path that removes
an editor view (tab close, pane close, tab-limit eviction #742, tab drag)
records the file via `noteClosedFileView`, and the `Update` wrapper's
`drainClosedFileViews` fires `plugin.EventBufferClosed` only when **no** view of
the path remains in **any** in-memory workspace (active or parked) — the
close-side mirror of the `EventFileOpened` dedup over shared tabs/leaves
(#142); a dragged tab's file, re-opened elsewhere in the same pass, never
fires. The
`didOpen` is gated by large-file mode (#149): a file over the
`files.large_file_kb` / `files.large_file_lines` thresholds
(`largeFileGated`, policy in `internal/largefile`) is never opened with the
server — servers choke on huge documents too — so diagnostics and completion
are silently absent, and the editor's change events ship no text (they carry
`Large` instead; the bridge stops syncing and closes the document server-side,
covering a reload that grows an already-open file past the threshold). The
palette command
`editor.forceCodeInsight` sets a per-path override and re-fires the file-open
hook, which then didOpens normally. Files
already open at startup restore straight into editors (bypassing the interactive
open path), so the app also fires the file-open hook for each restored file from
`Model.Init` — once per file even when it is shared across tabs — so a
session-restored buffer gets its `didOpen` and diagnostics without a reopen (#332).

Completion is one source of several since Roadmap 0410 (#851): the bridge's
batches are tagged `Source: lsp` and merge with local index sources in the
editor — see [/architecture/completion.md](/architecture/completion.md).

**Completion triggering (#527).** Every typed character emits a completion
trigger carrying the character (`Event.Char`); the *bridge* decides whether it
warrants a `textDocument/completion` request: the server's advertised
`completionProvider.triggerCharacters` always fire (falling back to `.` while
no capabilities are known, e.g. before the handshake), and an
identifier-starting rune (letter or `_`) fires the as-you-type popup, gated on
the `lsp.completion_auto` config toggle (default on). Characters handled by
auto-close pairing still trigger. Identifier runes typed while the popup is
already open re-emit nothing — they only narrow the client-side prefix filter.
`ctrl+space` (Kitty `ctrl+' '` or the legacy `ctrl+@`/NUL spelling) emits a
char-less trigger the bridge honours unconditionally (#302); a re-press with
the popup open re-queries. The popup anchors at the start of the identifier
under the request position (widened past sigils like PHP's `$` while the
widened prefix still matches an item, mirroring the accept path's
`extendPrefixMatch`), so the partial word typed before the request counts into
the prefix filter. Filtering is **fuzzy** (#845): the typed prefix
subsequence-matches each item's `filterText` (label when absent) via
`internal/fuzzy`, so CamelCase/snake_case initials (`gCN` → `getClassName`)
and scattered substrings match; results rank by match score (word-boundary and
start-anchored matches win), with ties keeping the server's `sortText` order
(label when absent), which also orders the unfiltered list. The popup owns up/down, pgup/pgdn, enter/tab and esc **only while its filtered
list is non-empty**, i.e. while it actually draws (#1810): a reply landing after
the typed prefix has moved past it filters down to nothing, and such an
invisible list must not swallow the arrows — the next key routes to the buffer
and drops the stale state. Accepting an item replaces the partial identifier before the cursor (the run of letters/digits/`_`, `identifierStart`), not the request anchor — a manual trigger anchors at the cursor, so an anchor-only replace would duplicate the already-typed prefix (#330).

**Snippets (#846).** The client announces `snippetSupport`, so servers send
items whose insert text is LSP snippet syntax (`insertTextFormat: 2`).
`internal/lsp/snippet.Expand` parses tabstops (`$1`, `${2:default}`,
`${3|choices|}`, `$0`), variables (default or empty) and escapes into plain
text plus tabstop offsets; a malformed snippet falls back to inserting the raw
text. With tabstops present (and a single caret) accepting starts a
**tabstop session**: the cursor lands on the first stop (placeholder stops sit
at the end of their default text), tab/shift+tab jump between stops — the
buffer-size delta since the last jump shifts later stops, the sequential
fill-in shape — and esc (leaving insert mode) or jumping past the last stop
ends the session, returning tab to normal indentation.

**Auto-import (#848).** An accepted item's `additionalTextEdits` — the "type a
name, the import appears" behavior — apply through the same insert recorder as
the main insert (one undo step), bottom-up, before the identifier replacement;
the manager converts them to editor coordinates against the synced document
(`ConvertCompletionItems`), and the cursor/carets are **position-adjusted past
every edit** (`adjustPastEdit`, #929) — line delta for edits above, plus a
column shift when an edit ends on the cursor's own line before the cursor
(pyright inserts the import at `(0,0)` of a short file; without the column
shift the main insert spliced into the fresh import line). Fragment-routed
completions (0300) drop additional edits — they would target the virtual
document.

**Lazy resolve (#847).** Servers with `resolveProvider` ship lean completion
lists; documentation and late `additionalTextEdits` arrive per item via
`completionItem/resolve`. The editor emits a completion-select event whenever
the popup's selection rests on a doc-less item (carrying the item's reply
index, `CompletionID`); the bridge caches the raw reply, debounces 120ms so
arrowing through the list resolves only where the selection rests, and answers
with a `CompletionResolveMsg`. The resolved documentation renders dimmed under
the popup's hint row; resolve-delivered additional edits merge into the accept
path like inline ones.

**Incomplete lists (#849).** A reply flagged `isIncomplete` is a partial view:
identifier runes typed while the popup shows re-emit the completion trigger
instead of only narrowing the client-side filter, and the bridge **debounces
identifier-rune requests** (80ms, re-armed per keystroke) so a typing burst
reaches the server once, at the resting position. Complete replies keep the
filter-only behavior; server trigger characters and manual ctrl+space stay
immediate. Requests also report **why** they fired (#850): a typed character
in the server's declared trigger set sends `TriggerCharacter` with the
character; identifier runes and manual ctrl+space send `Invoked` — some
servers (e.g. Intelephense on `$`) tailor their answers to it.

**Server → editor.** Server replies and notifications arrive on the jsonrpc read
loop. The manager converts them to editor coordinates (via `protocol/convert.go`)
and the bridge wraps them as `tea.Msg`s — `DiagnosticsMsg`, `CompletionMsg`,
`HoverMsg`, `DefinitionMsg`, `ReferencesMsg`, `ServerStatusMsg` — injected with
`host.Send`. Diagnostics are **coalesced** before injection (#597): a
workspace-diagnostic server (pyright over a populated `.venv`) publishes for
hundreds of library files, and one `tea.Msg` per file would mean one Update pass
+ re-render per file, starving keystrokes. Publishes accumulate in the bridge
(latest per path) over a 50ms `diagCoalesce` window and flush as a single
`DiagnosticsBatchMsg`, so the storm costs one re-render. In the app every
published set first passes the **diagnostic ignore filter** (#1259,
`internal/app/diag_ignore.go`): the raw set is cached per path, the
`lsp.diagnostics_ignore` rules (`internal/lsp/ignore.go` — `source=`/`code=`
globs plus a trailing `msg=` pattern, bare token = code) drop suppressed
entries, and only the filtered set reaches the Problems store and the editors.
A rule edit re-filters the raw cache live on config reload — no republish
needed. Two rules ship as defaults (#1260): intelephense's P1006 TypeError
cannot infer types written through by-reference parameters (`&$param`,
[bmewburn/vscode-intelephense#3504](https://github.com/bmewburn/vscode-intelephense/issues/3504),
open as of 1.18.5) and floods by-ref-heavy PHP with bogus
`Expected type '...'. Found 'null'.` / `... Found 'unset'.` errors, so the
defaults suppress exactly those message variants — real P1006 type mismatches
still surface, and a user-set `lsp.diagnostics_ignore` replaces the defaults
wholesale. The editor's `lsp.ignoreDiagnostic` command (palette, "Ignore
Diagnostic Under Caret") appends the caret diagnostic's rule
(source+code, or exact message when the server sent no code) to the project
config. After the ignore filter, the **severity remap** (#1503,
`internal/lsp/severity.go`, `lsp.diagnostics_severity`) applies on the same
central path: each rule is the ignore-rule condition grammar plus a trailing
`error`/`warning`/`info`/`hint`/`off` keyword (`reportArgumentType warning`);
first match wins and `off` drops the diagnostic. Codeless diagnostics are
never remapped — pyright, gopls and friends publish syntax errors without a
`code`, so syntax errors always stay errors. Rule edits re-apply live from
the same raw cache. The condition grammar carries one pseudo-condition,
`partial` (#1510, `internal/lsp/partial.go`), matching only diagnostics whose
message classifies as a **union-partial** type mismatch — the first quoted
segment (inferred type) and the last (expected type) are split into top-level
union members, and the expected members must be a strict subset of the
inferred ones (`str | None` where `str` is expected), gated on the message
reading as an assignability complaint. Message phrasing is server-specific
(pyright is the tested one), so `partial` rules should carry `source=`; being
message-based they never qualify as native overrides and always apply
client-side. The keyword works in ignore rules too — the grammar is shared. Exact-code rules additionally pass through **natively**
at server initialize: a `ServerSpec` may declare `SeverityOverridesPath`
(pyright: `["python","analysis","diagnosticSeverityOverrides"]`), and
`resolveSpec` folds `NativeSeverityOverrides` — the rules whose sole
condition is an exact code, translated to the server vocabulary
(`error`/`warning`/`information`/`none`; `hint` stays client-side) — into the
spec's settings beneath the plugin baseline and the user's
`[lsp.servers.<id>]` overlay, so the server stops escalating at the source
(after its next restart; the client remap covers the interim). The app
routes each filtered set (by
file path) to the editor leaf that owns it;
the editor caches diagnostics, opens the completion / hover popup, and the app
composites those popups at the cursor cell with `overlay.Place`. Go-to-definition
is handled by the app (navigate + place cursor); an **empty answer is never
silent** (#858) — a toast says whether the server found nothing under the
cursor or no ready server could be asked at all; standing **on the definition
itself** (the answered range contains the request position, same file) the
jump would go nowhere, so F4/cmd+click show the symbol's usages instead
(#860, JetBrains parity) — declaration excluded, the list's hint carrying the
count; a jump that lands in a vendored
dependency (`.venv`/`site-packages`/`node_modules`/…) opens the file read-only —
the first edit prompts for confirmation before unlocking it (the editor's
[dependency-file edit guard](./editor.md), #565). Hover markdown is rendered,
not shown raw (#379): fence markers (```` ```go ````) are stripped, the fenced
block is syntax-highlighted through the language registry (`HighlightFenced`,
fence tag resolved as language id then extension; an unresolvable tag falls
back to an accent tint so the signature still reads as code), and a thematic
break (`---`) draws as a horizontal rule sized to the popup content.

**Diagnostic details popup** (#739, `lsp.diagnosticInfo`, default `ctrl+f1` —
the JetBrains error-description chord): shows every diagnostic covering the
caret line on the hover popup surface — per entry a severity header colored
like the gutter mark with the server attribution (`pyright ·
reportUndefinedVariable`; `Diagnostic.Code` carries the protocol's
string-or-number code as text), then the message; entries separate with a
rule, any key dismisses. Pure client state (the cached publishDiagnostics
answer, no server round trip); a clean caret line raises an info toast
instead. With source and code visible a false positive can be attributed to
its server and reported or configured away.

**Mouse-idle hover** (#1129): resting the pointer over editor content for
~600ms opens the hover popup **at the hovered cell**, not the caret. The app
tracks the pointer's resting cell (`internal/app/hover_idle.go`) with a
demand-armed `tea.Tick` (the [#1001 idle discipline](./performance.md): one
tick in flight, re-armed only while a wait is pending — no free-running
ticker). On fire, a diagnostic whose range covers the cell shows immediately
(the #739 content shape, pure client state — this works with **no LSP hover
support at all**), and a hover request goes to the bridge through a new
app-originated editor event (`host.EditorHoverRequest`) carrying the hovered
position; `bridge.requestHover` is the shared seam — the `ctrl+q` key flow
feeds the cursor, the mouse flow the hovered cell. The reply
(`HoverMsg{Mouse, Line, Col}`) is validated against the editor's pending
mouse-hover position, so a stale answer never opens a popup at a cell the
pointer has left; on a match the server content appends below the diagnostic
rows (rule-separated) and the popup anchors at the pointer (`hoverState`
carries an explicit anchor; `HoverAnchor` falls back to the cursor for the
key flow). Scope guards: focused editor pane only (MVP — JetBrains also
hovers unfocused panes, deferred), only over buffer text (`HoverTarget`
rejects the gutter, scrollbar, sticky headers, and cells past the line), and
never during a drag or while the context menu / any overlay is open. Motion
off the cell, any click, wheel, or key dismisses the mouse-anchored popup;
a key-triggered cursor-anchored popup is untouched by pointer motion.

**Diagnostic navigation (#369).** `lsp.nextDiagnostic` / `lsp.prevDiagnostic`
(default `f2` / `shift+f2`, JetBrains' next/previous-highlighted-error keys)
step the cursor through the focused document's diagnostics. No server
round-trip: the editor already caches the set (`m.diags`), so the commands are
editor actions (`next_diagnostic` / `prev_diagnostic`, registered by the
editor plugin). The walk is document-ordered (not severity-ordered — repeated
presses stay a monotone sweep through the file) and wraps around either end;
each jump lands on the diagnostic's start position and opens a **popup
anchored there** (#1420) — the #739 content shape (colored severity header
with attribution, the full multi-line message), with a dim "(wrapped)" row on
wrap-around — instead of routing through the generic toast queue. It dismisses
like any hover popup: the next key (further navigation before its own popup
re-opens, cursor motion, esc) drops it. No diagnostics → info toast.

**Request errors surface (#372).** Every user-initiated request (hover,
definition, references, formatting, code actions — rename already had its own
path) routes a failed server reply through `requestFailed`
(`plugins/lsp/bridge.go`), which raises an error `ServerStatusMsg` toast
naming the action and the server's message ("find usages failed: …"). A
failing request is therefore always distinguishable from a command that never
fired; only silent *empty* results (no hover info, zero definitions) stay
quiet or keep their existing info toasts.

**Find references (#5).** `lsp.references` (default `alt+f7`, reconciled in the
chord table like `lsp.definition`) sends `textDocument/references` (declaration
included, matching JetBrains' find-usages) from the cursor. The bridge converts
every location to editor coordinates — reading each distinct target file once,
which also supplies a trimmed preview line — and sends `ReferencesMsg`. The app
routes by count: none → info toast, one → navigate directly, more → the palette
opened locked to a references mode (`internal/app/references.go`) listing
`path:line` + preview, fuzzy-filterable; activating an entry emits the same
`DefinitionMsg` the go-to-definition path navigates with. The location→
reference conversion is shared (`locationsToRefs`), and go-to-definition
reuses it for the **multi-target picker** (#279): more than one definition
site (interface implementations, build-tag variants) opens the same palette
list — placeholder "Definitions — pick a target…" — instead of guessing the
first location; a single site still jumps directly.

**Find usages, persistently (#1155).** `lsp.referencesPanel` ("LSP: Find
Usages (Panel)") runs the same references request but delivers an
`ilsp.UsagesMsg` that fills the singleton [Usages tool window](./usages.md)
instead of the palette: grouped by file, refreshable with `r`, title carrying
the symbol captured under the cursor at request time. The palette stays the
quick mode; the pane is the worklist.

**Call hierarchy (#173).** `lsp.callHierarchy` (default `ctrl+alt+h`, also
`H` — lowercase `h` is the notification history) sends
`textDocument/prepareCallHierarchy` from the cursor and opens the prepared
items in the call-hierarchy overlay (`internal/callhier`): a centered modal
rendering callers (default) or callees as a lazily-expanding tree. Expanding a
node runs the bridge-built `Fetch` continuation (`callHierarchy/incomingCalls`
/ `outgoingCalls`); the reply arrives as a `CallHierarchyCallsMsg` keyed by
request id, so stale replies (after a direction toggle) fall on the floor.
`tab` flips callers/callees on the same roots, `enter` navigates through the
shared `DefinitionMsg` path — a caller row jumps to the call site
(`fromRanges[0]`), a callee row to its declaration. Nothing prepared (cursor
not on a callable, or the server lacks `callHierarchyProvider`) is an info
toast.

**Inheritance analysis & navigation (0480, #1448).** Rides on two request
families added M1-style beside call hierarchy: `textDocument/implementation`
(decoded like definition — `Location | [] | LocationLink[]`) and
`textDocument/prepareTypeHierarchy` + `typeHierarchy/supertypes`/`subtypes`
(opaque `data` round-trips verbatim). Capability-gated as usual
(`implementationProvider`, `typeHierarchyProvider`; missing = graceful empty,
plus `ImplementationSupported`/`TypeHierarchySupported` probes for #858
toast wording). Three surfaces:

- `lsp.goToSuper` (default `cmd+u`) — supertypes of the prepared item when
  the server offers type hierarchy, else the *bidirectional* implementation
  answer (on a concrete method gopls returns the interface method = "super"
  in Go). `lsp.implementations` (default `cmd+alt+b`) is the plain
  implementation request. Both deliver an `ImplementationsMsg`: one target
  navigates via `openPathAt`, several open the locked refs picker with a
  direction-specific placeholder; empty toasts in the bridge (#858).
- `lsp.typeHierarchy` (default `ctrl+h`) — the prepared roots open the
  type-hierarchy overlay (`internal/typehier`, the callhier pattern verbatim
  with `tab` toggling supertypes/subtypes); expansion replies arrive as
  `TypeHierarchyItemsMsg` keyed by request id, stale ones fall on the floor.
- **Gutter marks (#1453)** — the passive decoration: the bridge schedules a
  debounced (750 ms), per-path-coalesced `InheritanceMarks` batch on open and
  after edits; the manager walks `documentSymbol` filtered to
  `SymKind{Class,Method,Interface,Struct}` (a *SymbolKind* block in
  `protocol/enums.go`, deliberately distinct from the CompletionItemKind
  numbering), caps at 150 symbols, probes each with `implementation` through
  a 4-worker pool under a 5 s batch timeout, and derives ↑ (implements/
  overrides) or ↓ (has implementations) from the symbol kind. Replies are
  stamped with the manager doc version and dropped when an edit raced the
  batch. The `editor.marks.inheritance` toggle (default on) gates rendering
  *and* the probe traffic. Rendering: [editor](./editor.md) sign column.

**Workspace symbols (0250, #294/#295).** `project.goToClass` (default
`cmd+o` — off macOS `ctrl+o` is vim jump-back; palette fallback) opens the palette
locked to the **live symbol mode** (`internal/app/symbols.go`): every settled
keystroke (150 ms debounce, `palette.LiveMode`) re-sends `workspace/symbol`,
fanned out by the manager to every running server advertising
`workspaceSymbolProvider` and merged (capped at 200). Rows lead with the
symbol name (location + declaration preview as the detail chip), stale
replies are dropped by query, and activation navigates via the shared
`DefinitionMsg` path. Ranking is tiered (#377): symbols located inside the
project root always sort above dependency/stdlib symbols (a large score
malus on non-project rows), and an exact name match earns a bonus so the
project's own symbol is the top hit; the adjusted score is stored on the
palette item, so search everywhere sinks stdlib noise below commands and
files too. The same mode holds the search-everywhere seat (#236):
its first open silently primes the bridge continuation through a
`project.goToClass` run that installs the hook without opening the symbol
palette. No provider → warn toast; zero hits render as the palette's empty
list. The request continuation still arrives via `SymbolPromptMsg.Apply`
(the phase-1 message), so the manager stays unreachable from the app.

Each hit carries the server's **`SymbolKind`** (`ilsp.SymbolHit.Kind`, the full
spec set is modelled as `protocol.SymKind*`): rows render it as a short badge
("class", "struct", "func", …, `SymbolKindLabel`; unknown kinds get none), and
`ilsp.ClassLike` — class, struct, interface, enum — is the filter behind the
palette's [class category](./command-palette.md) (#1849). The kind-filtered
views share this one cache and its ranking; a query is sent at most once per
settled keystroke no matter how many views forward it, and a palette re-open
(`Refresh`) forgets the last sent query so re-typing it re-queries.

**Formatting (#7, reshaped by 0470/#1401).** The reformat commands live in
the [formatter registry](./format.md) now: `lsp.format` / `lsp.formatRange`
(ids kept, titles "Reformat File" / "Reformat Selection", owned by
`plugins/format`) resolve a provider chain in which LSP is one entry
(`plugins/lsp/provider.go`). The LSP provider is capability-gated
(`documentFormattingProvider` / `documentRangeFormattingProvider`, per-path
via `Manager.FormatSupported` / `RangeFormatSupported`); its `Format` sends
`textDocument/formatting` with `FormattingOptions` from the buffer's
effective settings, and the manager converts the returned `TextEdit`s to
editor rune coordinates (it owns the synced document lines). The app routes a
`FormatEditsMsg` to the owning editor, which applies the batch bottom-up as
**one undo unit** (`editor/textedit.go`, mirroring replace.go).

**Format / organize imports on save (#1148).** With
`editor.format_on_save` / `editor.organize_imports_on_save` enabled (default
off), a manual save defers its write behind a bridge-run chain
(`plugins/lsp/savechain.go`): the `source.organizeImports` code action
(requested with `CodeActionContext.Only`, first matching action applied
without the picker), then the format step — routed through the [formatter
registry](./format.md) since #1401, so external and built-in formatters apply
on save too, not only `textDocument/formatting` — then the write. Each
step is time-boxed (2 s) and falls through on error/timeout, edits ack back
via `FormatEditsMsg.Applied`, and `SaveChainDoneMsg` releases the editor's
parked write — see [Editor § Format & organize imports on
save](./editor.md#format--organize-imports-on-save-1148). The capability
gate parses `codeActionProvider.codeActionKinds`
(`client.Capabilities.OffersCodeActionKind`; an undeclared list counts as
offered).

**Rename (#6).** `lsp.rename` runs `prepareRename` first (when the server
offers it): a server without the rename capability at all toasts "language
server does not support rename" (`manager.ErrRenameUnsupported`, #426 —
intelephense gates rename behind its paid licence), a rejected position
toasts "cannot rename here", an accepted one
opens an input prompt (`internal/app/lsprename.go`) prefilled with the ranged
symbol text. The prompt msg carries a bridge-built `Apply` continuation, so
the manager stays unreachable from the app. Confirming sends
`textDocument/rename`; the returned `WorkspaceEdit` (both `changes` and
`documentChanges` shapes decode; when a server populates both,
`documentChanges` wins — they are alternative encodings, and merging them
applied every edit twice, #364) is applied by shared infrastructure
(`plugins/lsp/workspace_edit.go`, reused by code actions later): files the
manager tracks — open editor buffers — are edited in-buffer as one undo unit
via `FormatEditsMsg`, applied through exactly **one** view of the document
(views alias one buffer, #142, so per-view routing applied every edit once
per view, #366; the change-sync broadcast converges the other views, the
same single-view rule as replace-in-path) and stay dirty; every other file
is rewritten on disk
(bottom-up, mode-preserving). A summary toast reports the touched file count.
Gated on `renameProvider`. The 0082 sheet-13 verdict landed (#18): `shift+f6`
binds `lsp.rename` in the Editor context — JetBrains' context-aware
refactor-rename — while the Global `file.rename` row keeps the chord in the
explorer. Go-to-declaration's sheet-11
verdict made `f4` the delivered primary for `lsp.definition` (`cmd+b` stays a
secondary).

**Code actions (#8, merged into intentions #2020).** Code actions are
*server-defined* fixes and refactorings for the code at the cursor — "add the
missing import", "organize imports", "extract function"; what the list offers
depends entirely on the language server and the diagnostics at that spot.
`lsp.codeAction` (default `alt+enter`, fragile — option-as-meta; retitled
"Show Intention Actions") sends `textDocument/codeAction` for the cursor or
the active visual selection, passing the cached published diagnostics
overlapping the range so servers offer quick-fixes. The bridge always answers
with a `CodeActionsMsg` carrying `Intentions: true` — even with no server
attached, an empty reply, or a failed request (the error toast still fires) —
because the app merges the offer with the **built-in intention actions**
([intention-actions](./intention-actions.md)) and owns the "no code actions
here" toast for the *merged* list. The merged offer opens as a palette list
**anchored at the caret** (`internal/app/codeactions.go`, falling back to the
centered locked box without a laid-out editor pane) — preferred LSP actions
starred and sorted first, then the remaining LSP actions in server order,
then the built-ins grouped by kind; the kind renders readably as the detail
chip ("quick fix", "source · organize imports"; a server that omits the kind
gets a generic "action", #309). Picking an LSP entry runs a bridge-built
continuation (same seam as rename); a built-in entry dispatches its
registered command. The LSP plugin also contributes a provider of its own:
"Rename Symbol" appears in the popup when the attached server declares the
rename capability (`Manager.RenameSupported`). The chosen LSP action applies
its inline `WorkspaceEdit`
through `workspace_edit.go` and/or executes its `command` via
`workspace/executeCommand`; server-initiated `workspace/applyEdit` requests
(how gopls delivers e.g. Organize Imports) are answered by the manager off
the read loop, converted, and dispatched through the same apply path. Result
decode is lenient — bare `Command` entries wrap into command-only actions.
Every outcome reports (#309): applied edits toast "'<title>': edited N
files", a no-op edit toasts "changed nothing", an action with neither edit
nor command warns that `codeAction/resolve` is not supported yet, and
command failures surface as error toasts. Gated on `codeActionProvider` /
`executeCommandProvider`.

**Signature help (#4, #523).** Two ways in: typing one of the server's
advertised trigger characters (`signatureHelpProvider.triggerCharacters` +
retriggers) fires `textDocument/signatureHelp` off the change event — gated
on the `lsp.signature_auto` config toggle (default on) — and the
`lsp.parameterInfo` command (`cmd+p`, fallback `ctrl+p`) requests it on
demand at the cursor, in insert *and* normal mode, regardless of the toggle.
While the popup is showing, every change **and cursor move** retriggers so
the active parameter follows the cursor, and the server answering null
dismisses it (typing past `)`). The bridge extracts the just-typed character
from the change event; the editor renders a cursor-anchored popup
(`signatureState`) with the signature label (active parameter emphasised —
parameter labels arrive as substrings or UTF-16 offset pairs, both resolve to
rune ranges in `lsp.SignatureContent`), a separator, one row per parameter
with the active one marked `▶` in the accent tone (#523), the active
parameter's / signature's first doc line dimmed, an overload counter, and a
leading dim `ƒ` marking it as informational — the actionable completion list
carries an accept-keys hint row instead (#308). An automatically opened
popup lives only while the call is being typed (#315): leaving insert/replace
mode and mouse clicks (#307) dismiss it, and a server reply landing after
insert mode ended is dropped as stale — unless it answers the manual command
(`Manual` flag) or updates a popup that is already showing. Some servers
(gopls) answer null when the position sits inside a string literal — the most
common place to ask "which argument is this?" — so an empty answer retries
once at the literal's opening delimiter on the synced line
(`stringLiteralStart`, #525), which is still inside the argument and yields
the correct active parameter. Completion, when open, takes precedence in the
popup compositor. All three popups render inside a rounded
themed frame (`popupFrame`, #316) — `BorderFocus` on `Panel`, like the
floating shell — so they read as overlays rather than buffer text. With the
frame in place they clamp to the **terminal**, not the pane: a popup may
overflow the owning pane's borders when it needs the room, the placement
shifts left / flips above the anchor instead of bleeding past the screen
edge, and the app feeds the terminal-derived width cap in via
`SetPopupMaxWidth`. The #306 safety nets stay: long signatures wrap at the
popup width cap (≤ 80) and over-tall content truncates at `popupMaxRows`
with an ellipsis row. Gated on `signatureHelpProvider`.

**Document highlight (#172).** Occurrences of the symbol under the cursor are
marked automatically: every cursor move (and change) re-arms a 150 ms
debounced `time.AfterFunc` in the bridge, so a `hjkl` motion burst fires one
`textDocument/documentHighlight`, not one per step. The manager converts the
result ranges to editor coordinates (it owns the synced lines, like
formatting) and keeps the LSP kind; positions inside an embedded fragment
route to the fragment's server with ranges mapped back onto the host
(`fragmentDocumentHighlight`). The bridge sends `DocumentHighlightsMsg`
anchored at the request cursor — the editor installs the marks only while the
cursor still sits at that anchor (a raced reply clears instead) and renders
them in `renderLine` as a subtle background under the syntax colour, below
cursor/selection/search in precedence. Read and plain-text occurrences use
the `OccurrenceRead` theme slot, writes `OccurrenceWrite` (see
[themes](./themes.md)); errors stay silent — a passive decoration, not a
user action.

**Inlay hints (#171).** Inline parameter-name and inferred-type annotations
(`textDocument/inlayHint`), requested document-wide by the bridge after open
and every change, coalesced per path via an in-flight/pending pair like
semantic tokens. The manager converts positions to editor coordinates,
flattens the string-or-parts label union, sorts by position, and merges hints
from embedded fragments (each fragment's server queried over its whole
virtual document, positions mapped onto the host). The editor indexes the
`InlayHintsMsg` per line and `renderLine` injects the hint text — dimmed and
italic via the `InlayHint` theme slot (falls back to the theme's border tone)
— before the anchor cell as pure virtual text; `DisplayOffset` keeps
cursor-anchored popups aligned past injected hints and expanded tabs.
Capability-gated on `inlayHintProvider`; the `lsp.inlay_hints` config toggle
(**default off**, #523 — parameter info is on demand via `lsp.parameterInfo`
instead; the settings LSP page's `I` key flips it) both skips the traffic and
hides cached hints live. gopls ships
all hint kinds off, so the Go plugin's baseline settings enable parameter
names and inferred types (user `[lsp.servers.go] settings` still override).
Errors stay silent — a passive decoration.

**Semantic tokens (#9, refined by #1912).** `internal/highlight/semantic`
decodes the packed relative 5-tuples against the server's legend into the
same `highlight.Span` shape Tree-sitter produces, mapping LSP token types
(refined by modifiers: readonly → constant, defaultLibrary →
variable.builtin) onto the capture names the theme system already resolves —
no colours defined in LSP code. Parameters map to the dotted leaf
`variable.parameter` and namespaces to `type.namespace` (#1912): unthemed
they inherit the head capture's colour through the theme's prefix walk, and a
`theme.captures.variable.parameter` override colours parameters apart from
locals without touching any builtin theme. The manager keeps per-document
result state and uses `semanticTokens/full/delta` when the server offers it
(a delta answer may also be a fresh full result; a server's
`workspace/semanticTokens/refresh` clears the delta state and re-requests
every open document); the bridge refreshes after open and every change,
coalescing via an in-flight/pending pair. The editor layers the overlay over
the Tree-sitter base in `styleAt` — base < semantic < diagnostic underline,
which `renderLine` applies on top either way — and keeps the last result
until the next one lands. Optional by construction: no
`semanticTokensProvider` (gopls needs `semanticTokens = true` under
`[lsp.servers.go.settings]`) simply means Tree-sitter-only rendering; the
`lsp.semantic_tokens` toggle (default `true`, Settings → Language Support)
gates traffic and rendering while keeping cached spans.

**Code lenses (#1912).** `textDocument/codeLens` results ("run test",
"references" — whatever the server offers) arrive document-wide after open
and every change, coalesced per path like the other decorations, and render
as dimmed virtual annotations at the end of the anchored line (`CodeLens`
theme behavior mirrors inlay hints). `lsp.codeLens` ("LSP: Run Code Lens")
lists the cursor line's lenses — or the whole file's when the line has none —
through the code-action picker and executes the choice: unresolved lenses
(no command yet) go through `codeLens/resolve` first, then
`workspace/executeCommand` runs the command, whose edits come back as
`workspace/applyEdit`. A server-initiated `workspace/codeLens/refresh`
re-requests every open document (gopls does this when test files change).
Toggle: `lsp.code_lens` (default `true`).

**Server folding ranges (#1912).** `textDocument/foldingRange` feeds the
editor's existing fold engine as a second provider: the bridge requests
ranges after open and every change, the manager converts them to the
`highlight.Fold` shape (kinds — `imports`/`comment`/`region` — preserved),
and the editor merges them with its Tree-sitter folds, server ranges winning
on the same header line. Everything downstream (za/zc/zo/zM/zR, fold-aware
motions, the copy affordance) works off the merged set; no server support or
`lsp.folding = false` (default `true`) means pure Tree-sitter folding, the
unchanged fallback.

**Selection ranges (#1912).** `editor.selection.extend` / `.shrink` grow and
shrink the visual selection through syntactic ranges. The editor asks the
bridge (seam: `internal/lsp/selectionrange.go`, mirroring the save chain) for
the server's `textDocument/selectionRange` ladder at the cursor; no provider,
`lsp.selection_range = false`, or an empty answer falls back to a
Tree-sitter ancestor walk (`internal/highlight`), and as a last resort a
word → line → buffer ladder, so the commands always work. Shrink steps back
down the same ladder and finally restores the original cursor.

**willRenameFiles (#1912).** Renaming or moving a file/folder in the
explorer first runs `workspace/willRenameFiles` (seam:
`internal/lsp/willrename.go`): the explorer defers the FS operation, the
bridge asks every running server whose file-operation filters match, applies
the returned WorkspaceEdit through the shared `dispatchWorkspaceEdits`
plumbing (open buffers as one undo unit, closed files on disk), and reports
back with a `WillRenameDoneMsg`; only then does the explorer perform the
`os.Rename` — still recorded on its undo stack. The round trip is time-boxed
(2s), so a dead server delays a rename but can never lose it; undo/redo of
the FS op bypass the servers (the text edits are undone in the editors).
Toggle: `lsp.will_rename` (default `true`).

**Embedded fragments — virtual documents (0300, #412–#416).** SQL inside a
Python string gets real completion, hover, definition and references from an
SQL server. LSP has no
protocol-level notion of embedded fragments, so the manager mirrors each
detected fragment into a synthetic in-memory document (`ike-fragment:` URI,
`manager/fragments.go`) with the fragment's language id, served by that
language's ordinary managed server. Detection comes from Tree-sitter
*injection queries* (`highlight.Fragments`): a grammar built with
`NewGrammarInjections` ships an `injections.scm` whose captures follow the
`fragment.<lang>[.guess]` convention — `.guess` defers to a Go-side content
heuristic (SQL statement-leading keywords), so plain strings never become
fragments. Python's query captures `string_content`; the fragment text is
exactly the host text of its range, so host↔fragment position mapping is a
pure offset shift. Lifecycle follows the host document: fragments re-detect
after every open/change on a manager goroutine (generation-guarded — the
newest sync wins; `Change` runs on the UI thread and detection/spawning must
not), matching slots update in place via didChange, vanished fragments close,
crash restart re-opens them. Position-based requests (completion, hover,
definition, references) whose position falls inside a fragment route to the
fragment's server with positions mapped both ways: request positions become
fragment-relative, result edit/hover ranges return in host coordinates, and
definition/reference locations pointing into fragment documents are rewritten
to the host file (host URI + host range); locations in real files pass
through, and a fragment location that no longer resolves to a tracked
fragment is dropped rather than surfaced as an unopenable synthetic URI.
Diagnostics published on fragment documents merge into the host's (#415,
`manager/fragdiags.go`): the manager keeps the last publish per source — the
host server's per path, each fragment server's per (host, slot) — and
re-emits one merged host-path `publishDiagnostics` whenever any source
changes, so the bridge stays fragment-agnostic. Fragment diagnostics are
stored in fragment-relative coordinates and mapped through the fragment's
*current* range at publish time, so they follow the fragment when host edits
move it; a fragment that closes (or whose language is stopped) drops its
diagnostics from the merged view immediately, without waiting for a server
publish. A
fragment language with no configured server degrades silently. The
`sql` language plugin registers `sqls` (also serving plain
`.sql` files) so the pipeline works out of the box.

## File watching (workspace/didChangeWatchedFiles, #1144)

Workspace-indexing servers (Intelephense most visibly) resolve symbols against
an index built at initialize time and only refresh entries they are told
about. Open buffers sync via didOpen/didChange, but a file **created, changed
or deleted outside IKE** — a generator, a `git checkout`, another editor —
was previously never announced: a newly created PHP class kept reporting
`Undefined type` in referencing files until a manual save happened to poke the
server. The watched-files path closes that gap.

- **Capability**: `clientCapabilities()` advertises
  `workspace.didChangeWatchedFiles` with `dynamicRegistration: true`. Servers
  answer with a `client/registerCapability` request for method
  `workspace/didChangeWatchedFiles` carrying their glob watchers; the manager
  stores them per server instance (`manager/watchedfiles.go`, keyed by
  registration id) and `client/unregisterCapability` drops them. A crashed
  server's replacement re-registers naturally — watchers live on the `server`
  struct, not the manager.
- **Event source**: the 0140 external-file watcher (`internal/watch`) already
  emits debounced **per-file** `FileCreated`/`FileChanged`/`FileRemoved`
  events. The app's `watch.EventMsg` handler forwards them (after its
  remove-then-recreate fixup) as `plugin.EventExternalFileChange` hooks; the
  LSP bridge feeds them into `Manager.FileEvent`. IKE's own saves are
  watcher-suppressed (`MarkSaved`), so the bridge's `fileSaved` additionally
  emits a `Changed` event — spec-conform, and it keeps servers watching
  companion files (composer.json, go.mod) current.
- **Batching**: events accumulate for 200 ms (on top of the watcher's own
  100 ms debounce) with per-path merge (created+changed → created,
  created+deleted cancels, deleted+created → changed), then flush as one
  `workspace/didChangeWatchedFiles` per interested server.
- **Filtering**: with registered watchers, the globs (and their `kind` bits)
  decide. The matcher lives in `internal/pathglob` (shared since #1704 with
  the editor's conceal file filter) and supports `**`, `*` (never crossing
  `/`), `?`, `{a,b}` alternation and `[...]` classes — enough for what real
  servers register (`**/*.php`, `**/*.{ts,tsx}`); limits: byte-wise matching,
  no escape character. Relative patterns resolve against the RelativePattern
  base or the server root; absolute patterns match the full path. A server
  that never registers gets a **fallback**: events for files whose language
  (via the `internal/lang` registry) maps to that server and that lie under
  its root.
- **Open buffers**: external edits to files open in IKE keep the 0140
  reconciliation (auto-reload / stale flag, see
  [Editor § External file changes](./editor.md#external-file-changes-roadmap-0140))
  as the authority over buffer content; the server additionally receives the
  watched-files event so its index and the buffer sync stay coherent.
- **Coverage limits**: events originate from the recursive fsnotify watch, so
  changes under pruned directories (dot dirs, `vendor/`, `node_modules/`, or
  beyond the #1011 directory cap) are not announced.

## Design rules

- **Never block the event loop.** Requests run as goroutines; results return via
  `host.Send`. `Update`/`View` never do LSP I/O. Even notifications sent from the
  Update goroutine (didOpen/didChange/didSave/didClose) are safe: the jsonrpc
  layer enqueues them and a dedicated writer goroutine owns the blocking pipe
  write (#594), so a stalled server never stalls a caller. An answer the bridge
  already has on the Update goroutine — `lsp.codeAction` in a buffer with no
  file, a local hover/definition/references provider claiming (#922/#1629) —
  goes back as a `tea.Cmd` where the seam returns one; `host.Send` queues
  rather than blocking, so neither shape can freeze the IDE (#2027).
- **One manager owns all servers.** Spawning, routing, capability gating and
  restart live in `manager`/`client`; features never touch a raw connection.
- **Position mapping is centralised.** `protocol/convert.go` is the only place
  editor rune coordinates cross into LSP code-unit coordinates, honouring the
  server's negotiated `positionEncoding`.
- **Capabilities gate features.** A request is only issued when the server
  advertises support; a missing capability (or a missing binary) is a graceful
  no-op with a status message, never an error popup.
- **Crashes are recoverable.** `restart.go` detects an unexpected exit, respawns
  with linear backoff, re-initialises, and re-opens tracked documents; after
  repeated crashes the server is disabled. Diagnostics survive the restart
  attempts (a successful restart republishes), but a terminal disable — and a
  deliberate `StopLang`/`Shutdown` — clears the dead server's publishes from
  every affected editor (`cleardiags.go`, #994): host publishes are dropped
  and the merged set re-emitted (fragment diagnostics from servers that still
  run survive); documents that leave the manager get an explicit empty
  publish.
- **Status is classified (0130).** Every manager status carries a
  `lsp.ServerStatusKind`: persistent server state (ready, disabled, missing
  binary) renders as a status-line segment; transient events (crashed → warn,
  restarted → info, launch error / disabled-after-crashes → error) surface as
  toast notifications. See [Notifications](./notifications.md).
- **Actions are registry commands.** Hover/definition/references/restart are plain
  `plugin.Command`s reached by the palette (07) and keybindings (08) by id — no
  parallel dispatch path.
- **Baselines live with the language, config overlays.** Server command/args/root
  markers come from each language plugin's `lang.Language.Server`; `[lsp.servers.<id>]`
  overrides per field. Loader precedence (defaults < user < project) stays in
  `internal/config`.
- **Version awareness = detect + delegate.** A language's `Toolchain` detects the
  project interpreter (venv, `.python-version`, …); the manager merges its result
  into the server settings and answers `workspace/configuration` from them, so a
  version-aware server (pyright) checks against the project's real toolchain. IKE
  never reimplements the server's version logic. See [Language Registry](./languages.md).
  For the server to *ask*, the client advertises `workspace.configuration` in its
  capabilities (`client/lifecycle.go`); without it pyright never pulls the interpreter
  path and resolves venv imports against the system Python (#563). The server is
  registered before `initialize` so a `workspace/configuration` request arriving on
  `initialized` is answered rather than dropped.

## Configuration

The `[lsp]` section: `enabled` (master switch), `inlay_hints` (inline
parameter/type hints, default `false`, #523), `signature_auto` (automatic
signature popup on trigger characters, default `true`; the manual
`lsp.parameterInfo` command works regardless), `completion_auto` (as-you-type
completion popup on identifier characters, default `true`, #527; server
trigger characters and `ctrl+space` work regardless), the #1912 per-feature
toggles `code_lens`, `folding`, `semantic_tokens`, `selection_range` and
`will_rename` (all default `true`, all on Settings → Language Support), and a
per-language `servers` table.
Defaults ship for `go`, `php`, `python`; a user overrides any field in their
`settings.toml`. `[lsp.servers.<id>] enabled = false` switches one language's
server off while the subsystem stays on (#130; honored by `resolveSpec`). The
servers are external binaries the user installs
(`go install golang.org/x/tools/gopls@latest`, `npm i -g intelephense pyright`); a
missing binary disables that language with a status message. The binary is
resolved by `transport.Resolve` (#370): PATH first, then the well-known
per-toolchain install directories (`go env GOBIN` / `GOPATH/bin`, npm's global
prefix) — so a `go install`ed server works even when GOBIN is not on PATH; the
process is launched via the resolved absolute path.

All of this is editable in-IDE on the **Language Servers** settings page
(0180, #130 — see [Settings UI](./settings-ui.md)): live per-server status
(`ServerStatusMsg` now carries the language), effective command + source
layer, per-server enable and command/args/settings overrides via write-back,
and per-server restart (`Manager.StopLang`: stops one language's servers, all
roots; they respawn lazily) beside the global `lsp.restart`.

Closing a background workspace (#825) releases its LSP footprint the same
lazy-respawn way: the `EventWorkspaceClosed` hook (`lsp.wsclose`) has the
bridge drop its per-path caches under the closed root and call
`Manager.CloseRoot`, which didCloses every document inside the root and
stops every server rooted there.

## Missing-server installation (#131)

**Activation implies installation.** Each language plugin's `ServerSpec`
carries an `Install` recipe (a plain argv: `go install
golang.org/x/tools/gopls@latest`, `npm install -g pyright` / `intelephense`).
When launching a server fails with `transport.ErrNotFound` — detected on the
first file open of the language — the recipe runs automatically in the
background (`plugins/lsp/install.go`), with an "installing …" info toast, a
success/failure result, and on success an immediate re-open of the triggering
document so the fresh server starts without further interaction. Success is
claimed only after the binary actually resolves (`transport.Resolve`, #370);
a recipe that exits 0 but leaves the binary unresolvable (e.g. an unusual
install prefix outside PATH and the known toolchain dirs) reports an error
toast naming the probed directories, and counts as a failure for the backoff.

`lsp.auto_install = true|false` (default true) is the opt-out; the Language
Servers page toggles it with `A` and offers the same install manually with
`i` — the fallback, and the only retry path after a failure. Guard rails: one
install per language at a time, the automatic path backs off permanently
after a failed attempt (no install loop on every file open), and failures
surface the output tail as an error toast plus a `debug.log` line (#125,
written by the root model for every `ServerEventError`). All work runs inside
goroutines/`tea.Cmd`s, never on the Update loop (#123).

### First-start onboarding (#301)

On the very first launch — the user settings file does not exist yet — a
one-time floating dialog (`internal/app/onboarding.go`) lists every registered
language whose server ships an install recipe, each with a checkbox
(pre-checked). Enter installs the checked servers as a batch through the
existing `lsp.installMissing` command (same recipes, progress and result
notifications as above); unchecked servers persist as `[lsp.servers.<id>]
enabled = false` in the user layer so auto-install leaves them alone. Esc
skips without touching any server. Either way `lsp.onboarded = true` is
written (which creates the user settings file), so the dialog never returns
on its own — the Language Servers settings page stays the ongoing management
surface, and finishing the Welcome Tour re-opens the dialog deliberately
(the post-tour setup flow, #713, force-opens it past the `lsp.onboarded`
gate).
`lsp.auto_install = false` (e.g. from a project config) suppresses the dialog
entirely: ask me nothing, install nothing. When the crash-recovery prompt is
due on the same start, recovery wins the shell and onboarding follows once it
closes.

## Server logs & crash diagnostics (#715)

Every spawned server's **stderr is teed into a per-language log file**
(`internal/lsp/transport` `Spec.LogPath`): `$IKE_CONFIG_DIR/logs/lsp-<lang>.log`
(`~/.ike/logs` fallback, `manager.LogPath`). The transport writes a
timestamped start header and an exit footer (the exit error); the manager
appends its lifecycle markers — `server crashed`, `restarting (attempt n/3)`,
`disabled after repeated crashes` — so one file tells the whole story. Files
above 1 MiB rotate to `<path>.old` on the next start; the in-memory ring
buffer (`Process.Stderr`) is unchanged. Logging is best-effort: any file
error silently degrades to today's behaviour.

Markers always start on a fresh line (#990): both the transport's
header/footer and the manager's `appendLog` pad a newline first when the
server's last stderr write had none (`transport.FreshLine`), so a crash dump
that ends mid-line cannot swallow the `--- exited` footer or the next start's
header. On a crash the manager also extracts the decisive error from the
stderr tail (`transport.ErrorLine`: scanning backwards for the last short
non-stack-frame line naming an error — Node buries `SomeError: message`
under a megabyte minified source line) and names it in the crash toast, the
`server crashed:` log marker and the disabled-after-repeated-crashes toast.
Stopping or disabling a language also flushes its **unopened** publishes
(#1102): the manager tracks every path a language's servers published
non-empty diagnostics for and emits empty publishes on StopLang/Shutdown/
disable, so project-wide findings from workspace-diagnostic servers leave
the Problems store instead of surviving as stale entries; deleted files drop
out via the explorer's delete flow. Closing a document retracts its
published set the same way (#1543) — `Close` emits an empty publish and
drops the path from the published tracking — and `CloseRoot` flushes every
published path under the root plus, once a language has no live server
anywhere, its out-of-root publishes (module caches) that no root-scoped
prune can reach. Downstream, an empty publish deletes the path's key in the
bridge's code-action diagnostics cache, the app's raw-diagnostics cache and
the Problems store; closing a file's last view additionally releases the
bridge's per-path request-coalescing state and the cached raw completion
reply it owns.
A server that dies **during the handshake** gets the same treatment (#1062):
`startupError` folds the stderr line into the launch failure, so the toast
reads e.g. `taplo: the LSP is not part of this build …` instead of
`jsonrpc: connection closed`, and every launch-failure toast appends
`— details: "LSP: Show Server Log"`.

The palette command **`lsp.showLog`** ("LSP: Show Server Log",
`plugins/lsp/showlog.go`) opens the most recently modified log — the crashed
server's, in the common case — in a new editor pane, and points at the logs
directory when more exist. The disabled-after-repeated-crashes toast names
the command. No default chord (#711 policy).

## Testing

Pure-Go fakes throughout: an in-memory `io.ReadWriteCloser` speaking JSON-RPC
drives the client, manager, diagnostics, completion and the crash/restart path
with no real server installed. Position conversion (including UTF-16 surrogate
pairs) and the editor's diagnostics/completion/hover state are unit-tested by
feeding the `tea.Msg` contract straight into `editor.Model.Update`.
