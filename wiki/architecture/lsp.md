---
type: concept
title: LSP & Language Intelligence
description: The Language Server Protocol client — JSON-RPC over a server's stdio, a manager mapping (language, workspace root) to one server, editor-driven text sync, and diagnostics/completion/hover/signature-help/go-to-definition/find-references/call-hierarchy/formatting/rename/code-actions rendered back into the editor.
resource: internal/lsp
tags: [architecture, lsp, language-server, jsonrpc, diagnostics, completion, hover, definition, plugins]
timestamp: 2026-07-11T21:30:00Z
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
exposes `lsp.hover` / `lsp.definition` / `lsp.references` / `lsp.callHierarchy` / `lsp.format` /
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
notification is actually sent (an unchanged text sends nothing). A file-open
hook drives `didOpen`, save drives `didSave`, close drives `didClose`. Files
already open at startup restore straight into editors (bypassing the interactive
open path), so the app also fires the file-open hook for each restored file from
`Model.Init` — once per file even when it is shared across tabs — so a
session-restored buffer gets its `didOpen` and diagnostics without a reopen (#332). `ctrl+space` (Kitty `ctrl+' '` or the legacy `ctrl+@`/NUL spelling) emits the same completion trigger manually (#302), so completion opens without typing a trigger character; a re-press with the popup open re-queries. Accepting an item replaces the partial identifier before the cursor (the run of letters/digits/`_`, `identifierStart`), not the request anchor — a manual trigger anchors at the cursor, so an anchor-only replace would duplicate the already-typed prefix (#330).

**Server → editor.** Server replies and notifications arrive on the jsonrpc read
loop. The manager converts them to editor coordinates (via `protocol/convert.go`)
and the bridge wraps them as `tea.Msg`s — `DiagnosticsMsg`, `CompletionMsg`,
`HoverMsg`, `DefinitionMsg`, `ReferencesMsg`, `ServerStatusMsg` — injected with
`host.Send`. The app routes each (by file path) to the editor leaf that owns it;
the editor caches diagnostics, opens the completion / hover popup, and the app
composites those popups at the cursor cell with `overlay.Place`. Go-to-definition
is handled by the app (navigate + place cursor). Hover markdown is rendered,
not shown raw (#379): fence markers (```` ```go ````) are stripped, the fenced
block is syntax-highlighted through the language registry (`HighlightFenced`,
fence tag resolved as language id then extension; an unresolvable tag falls
back to an accent tint so the signature still reads as code), and a thematic
break (`---`) draws as a horizontal rule sized to the popup content.

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

**Call hierarchy (#173).** `lsp.callHierarchy` (default `ctrl+alt+h`, leader
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
`cmd+o`, leader `S` — off macOS `ctrl+o` is vim jump-back) opens the palette
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
explorer; `space n` stays as the leader path. Go-to-declaration's sheet-11
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

**Signature help (#4).** No command: typing one of the server's advertised
trigger characters (`signatureHelpProvider.triggerCharacters` + retriggers)
fires `textDocument/signatureHelp` off the change event; while the popup is
showing, *every* change retriggers so the active parameter follows the cursor,
and the server answering null dismisses it (typing past `)`). The bridge
extracts the just-typed character from the change event; the editor renders a
cursor-anchored popup (`signatureState`) with the active parameter emphasised
(parameter labels arrive as substrings or UTF-16 offset pairs — both resolve
to rune ranges in `lsp.SignatureContent`), the first doc line dimmed, and an
overload counter, and a leading dim `ƒ` marking it as informational — the
actionable completion list carries an accept-keys hint row instead (#308).
The popup lives only while the call is being typed (#315): leaving
insert/replace mode, insert-mode arrow motion, and mouse clicks (#307) all
dismiss it — anything that moves the anchoring cursor without a change event
would otherwise drag the popup along — and a server reply landing after
insert mode ended is dropped as stale. Completion, when open, takes
precedence in the popup compositor. All three popups render inside a rounded
themed frame (`popupFrame`, #316) — `BorderFocus` on `Panel`, like the
floating shell — so they read as overlays rather than buffer text. With the
frame in place they clamp to the **terminal**, not the pane: a popup may
overflow the owning pane's borders when it needs the room, the placement
shifts left / flips above the anchor instead of bleeding past the screen
edge, and the app feeds the terminal-derived width cap in via
`SetPopupMaxWidth`. The #306 safety nets stay: long signatures wrap at the
popup width cap (≤ 80) and over-tall content truncates at `popupMaxRows`
with an ellipsis row. Gated on `signatureHelpProvider`.

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
fragment is dropped rather than surfaced as an unopenable synthetic URI. A
fragment language with no configured server degrades silently; fragment
diagnostics are dropped until #415. The
`sql` language plugin registers `sql-language-server` (also serving plain
`.sql` files) so the pipeline works out of the box.

## Design rules

- **Never block the event loop.** Requests run as goroutines; results return via
  `host.Send`. `Update`/`View` never do LSP I/O.
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

## Configuration

The `[lsp]` section: `enabled` (master switch) and a per-language `servers` table.
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
written (which creates the user settings file), so the dialog never returns —
the Language Servers settings page stays the ongoing management surface.
`lsp.auto_install = false` (e.g. from a project config) suppresses the dialog
entirely: ask me nothing, install nothing. When the crash-recovery prompt is
due on the same start, recovery wins the shell and onboarding follows once it
closes.

## Testing

Pure-Go fakes throughout: an in-memory `io.ReadWriteCloser` speaking JSON-RPC
drives the client, manager, diagnostics, completion and the crash/restart path
with no real server installed. Position conversion (including UTF-16 surrogate
pairs) and the editor's diagnostics/completion/hover state are unit-tested by
feeding the `tea.Msg` contract straight into `editor.Model.Update`.
