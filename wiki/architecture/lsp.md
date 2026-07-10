---
type: concept
title: LSP & Language Intelligence
description: The Language Server Protocol client — JSON-RPC over a server's stdio, a manager mapping (language, workspace root) to one server, editor-driven text sync, and diagnostics/completion/hover/go-to-definition/find-references/formatting rendered back into the editor.
resource: internal/lsp
tags: [architecture, lsp, language-server, jsonrpc, diagnostics, completion, hover, definition, plugins]
timestamp: 2026-07-10T09:30:00Z
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
exposes `lsp.hover` / `lsp.definition` / `lsp.references` / `lsp.format` /
`lsp.formatRange` / `lsp.restart` as registry commands.

## Data flow

**Edits → server.** The editor emits change / cursor-move / completion-trigger
events through its `Emitter` seam (`internal/editor/events.go`). The app installs
a stateless adapter on every editor that forwards these to the host
(`host.EmitEditor`), which fans them to the LSP bridge (registered via
`host.SetEditorEmitter`). On a change the bridge sends the full document text to
the manager (`didChange`, full-document sync for the MVP); a file-open hook drives
`didOpen`, save drives `didSave`, close drives `didClose`.

**Server → editor.** Server replies and notifications arrive on the jsonrpc read
loop. The manager converts them to editor coordinates (via `protocol/convert.go`)
and the bridge wraps them as `tea.Msg`s — `DiagnosticsMsg`, `CompletionMsg`,
`HoverMsg`, `DefinitionMsg`, `ReferencesMsg`, `ServerStatusMsg` — injected with
`host.Send`. The app routes each (by file path) to the editor leaf that owns it;
the editor caches diagnostics, opens the completion / hover popup, and the app
composites those popups at the cursor cell with `overlay.Place`. Go-to-definition
is handled by the app (navigate + place cursor).

**Find references (#5).** `lsp.references` (default `alt+f7`, reconciled in the
chord table like `lsp.definition`) sends `textDocument/references` (declaration
included, matching JetBrains' find-usages) from the cursor. The bridge converts
every location to editor coordinates — reading each distinct target file once,
which also supplies a trimmed preview line — and sends `ReferencesMsg`. The app
routes by count: none → info toast, one → navigate directly, more → the palette
opened locked to a references mode (`internal/app/references.go`) listing
`path:line` + preview, fuzzy-filterable; activating an entry emits the same
`DefinitionMsg` the go-to-definition path navigates with.

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
missing binary disables that language with a status message.

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
document so the fresh server starts without further interaction.

`lsp.auto_install = true|false` (default true) is the opt-out; the Language
Servers page toggles it with `A` and offers the same install manually with
`i` — the fallback, and the only retry path after a failure. Guard rails: one
install per language at a time, the automatic path backs off permanently
after a failed attempt (no install loop on every file open), and failures
surface the output tail as an error toast plus a `debug.log` line (#125,
written by the root model for every `ServerEventError`). All work runs inside
goroutines/`tea.Cmd`s, never on the Update loop (#123).

## Testing

Pure-Go fakes throughout: an in-memory `io.ReadWriteCloser` speaking JSON-RPC
drives the client, manager, diagnostics, completion and the crash/restart path
with no real server installed. Position conversion (including UTF-16 surrogate
pairs) and the editor's diagnostics/completion/hover state are unit-tested by
feeding the `tea.Msg` contract straight into `editor.Model.Update`.
