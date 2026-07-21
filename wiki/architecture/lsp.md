---
type: concept
title: LSP & Language Intelligence
description: The Language Server Protocol client — JSON-RPC over a server's stdio, a manager mapping (language, workspace root) to one server, editor-driven text sync, and diagnostics/completion/hover/signature-help/go-to-definition/find-references/document-highlight/inlay-hints/call-hierarchy/formatting/rename/code-actions rendered back into the editor.
resource: internal/lsp
tags: [architecture, lsp, language-server, jsonrpc, diagnostics, completion, hover, definition, plugins]
timestamp: 2026-07-21T00:00:00Z
---

# LSP & Language Intelligence

Roadmap 0100. IKE speaks the [Language Server Protocol](https://microsoft.github.io/language-server-protocol/)
to get real language intelligence: diagnostics, autocomplete, hover, and
go-to-definition. The first increment ships **Go (gopls)**, **PHP (intelephense)**
and **Python (pyright)**; references / rename / formatting / code actions /
signature help / semantic-token highlighting are deferred to a later increment.

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
             Outbound writes are async too (#594): callers marshal on their own
             goroutine and enqueue the framed payload onto an unbounded queue
             drained by a single dedicated writer goroutine. A caller therefore
             never blocks on the server draining its stdin — critical because the
             bubbletea Update goroutine sends didChange from here per keystroke,
             and a busy server (indexing a large workspace) that stalls its stdin
             would otherwise freeze the whole event loop.
  transport/ spawn a server over stdio (cmd/args/env/cwd), capture stderr,
             watch for exit. Pure Go — no CGo — so the client cross-compiles.
  protocol/  LSP wire types + the SINGLE position-encoding boundary (convert.go):
             editor rune columns <-> LSP UTF-16 (or negotiated UTF-8/UTF-32).
  client/    one Client per server: initialize/initialized/shutdown handshake,
             cached + feature-gated ServerCapabilities, typed request/notify calls.
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
exposes `lsp.hover` / `lsp.parameterInfo` / `lsp.diagnosticInfo` / `lsp.definition` / `lsp.references` / `lsp.callHierarchy` / `lsp.format` /
`lsp.formatRange` / `lsp.rename` / `lsp.codeAction` / `lsp.restart` as
registry commands.

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
(label when absent), which also orders the unfiltered list. Accepting an item replaces the partial identifier before the cursor (the run of letters/digits/`_`, `identifierStart`), not the request anchor — a manual trigger anchors at the cursor, so an anchor-only replace would duplicate the already-typed prefix (#330).

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
(`ConvertCompletionItems`), and the cursor/carets shift by the line delta of
edits above them. Fragment-routed completions (0300) drop additional edits —
they would target the virtual document.

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
`DiagnosticsBatchMsg`, so the storm costs one re-render. The app routes each (by
file path) to the editor leaf that owns it;
the editor caches diagnostics, opens the completion / hover popup, and the app
composites those popups at the cursor cell with `overlay.Place`. Go-to-definition
is handled by the app (navigate + place cursor); a jump that lands in a vendored
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

**Diagnostic navigation (#369).** `lsp.nextDiagnostic` / `lsp.prevDiagnostic`
(default `f2` / `shift+f2`, JetBrains' next/previous-highlighted-error keys)
step the cursor through the focused document's diagnostics. No server
round-trip: the editor already caches the set (`m.diags`), so the commands are
editor actions (`next_diagnostic` / `prev_diagnostic`, registered by the
editor plugin). The walk is document-ordered (not severity-ordered — repeated
presses stay a monotone sweep through the file) and wraps around either end;
each jump lands on the diagnostic's start position and raises a toast with
the severity label and the message's first line ("error: undefined: foo",
"(wrapped)" appended on wrap-around). No diagnostics → info toast.

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

**Formatting (#7).** `lsp.format` (default `cmd+alt+l`) sends
`textDocument/formatting`, `lsp.formatRange` sends the range variant for the
active visual selection — the editor's cursor events carry the visual anchor
(`editor.Event.Sel`/`Anchor*`, mirrored on `host.EditorEvent`), so the bridge
knows the selection without a read-back seam; without one it answers with a
how-to toast. `FormattingOptions` (tabSize / insertSpaces) come from
`editor.tab_width` / `editor.use_spaces`. The manager converts the returned
`TextEdit`s to editor rune coordinates (it owns the synced document lines) and
the app routes a `FormatEditsMsg` to the owning editor, which applies the batch
bottom-up as **one undo unit** (`editor/textedit.go`, mirroring replace.go).
Both requests are capability-gated (`documentFormattingProvider` /
`documentRangeFormattingProvider`) — gopls, for example, offers no range
formatting, so the range command is a graceful no-op there.

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

**Code actions (#8).** Code actions are *server-defined* fixes and
refactorings for the code at the cursor — "add the missing import", "organize
imports", "extract function"; what the list offers depends entirely on the
language server and the diagnostics at that spot. `lsp.codeAction` (default
`alt+enter`, fragile — option-as-meta) sends `textDocument/codeAction` for
the cursor or the active visual selection, passing the cached published
diagnostics overlapping the range so servers offer quick-fixes. The offer
opens as a locked palette list (`internal/app/codeactions.go`) — preferred
actions starred and sorted first, the kind rendered readably as the detail
chip ("quick fix", "source · organize imports"; a server that omits the kind
gets a generic "action", #309); picking an entry runs a bridge-built
continuation
(same seam as rename). The chosen action applies its inline `WorkspaceEdit`
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

**Semantic tokens (#9).** `internal/highlight/semantic` decodes the packed
relative 5-tuples against the server's legend into the same `highlight.Span`
shape Tree-sitter produces, mapping LSP token types (refined by modifiers:
readonly → constant, defaultLibrary → variable.builtin) onto the capture
names the theme system already resolves — no colours defined in LSP code.
The manager keeps per-document result state and uses
`semanticTokens/full/delta` when the server offers it (a delta answer may
also be a fresh full result); the bridge refreshes after open and every
change, coalescing via an in-flight/pending pair. The editor layers the
overlay over the Tree-sitter base in `styleAt` — base < semantic <
diagnostic underline, which `renderLine` applies on top either way — and
keeps the last result until the next one lands. Optional by construction:
no `semanticTokensProvider` (gopls needs `semanticTokens = true` under
`[lsp.servers.go.settings]`) simply means Tree-sitter-only rendering.

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
`sql` language plugin registers `sql-language-server` (also serving plain
`.sql` files) so the pipeline works out of the box.

## Design rules

- **Never block the event loop.** Requests run as goroutines; results return via
  `host.Send`. `Update`/`View` never do LSP I/O. Even notifications sent from the
  Update goroutine (didOpen/didChange/didSave/didClose) are safe: the jsonrpc
  layer enqueues them and a dedicated writer goroutine owns the blocking pipe
  write (#594), so a stalled server never stalls a caller.
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
  repeated crashes the server is disabled.
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
trigger characters and `ctrl+space` work regardless), and a per-language
`servers` table.
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
