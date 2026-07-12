# Log

## 2026-07-12

- Plugin marketplace (Roadmap 0310, #444-#446): new `internal/market` package
  (static HTTPS `index.json` catalog with strict per-entry validation, install
  engine with SHA-256 verification and atomic .wasm+manifest writes pinning
  the reviewed capability list) and a Marketplace settings page (browse,
  capability review before install, install/update/remove, restart notice).
  New config key `marketplace.catalog_url`; new doc
  [marketplace.md](/architecture/marketplace.md).

- Code folding (#144): collapse a function body, block, import list or
  multi-line comment behind its header line. Fold ranges come from the same
  Tree-sitter parse as the highlight spans (`SpansMsg.Folds`, kinds via
  `lang.Language.FoldNodes`, `ScopeNodes` fallback); the collapsed set is
  per-view (#142). A closed fold renders as one row (header + dimmed
  `⋯ N lines` placeholder) and counts as one row for `j`/`k`, clicks and
  wheel scrolling; jumps into a fold auto-unfold, overlapping edits dissolve
  it, reparses reconcile it. Keys `za zc zo zM zR` + `editor.fold.*` palette
  commands. See [editor](/architecture/editor.md),
  [highlighting](/architecture/highlighting.md).

- Multi-caret editing (#145): a primary caret plus secondary carets fan every
  edit out — insert-mode typing/kills, `x r`, `d c y` with motions and text
  objects, `dd cc yy`, `p/P`, `o/O`, completion — as **one undo unit**.
  Created via `ctrl+g` (add next occurrence), `ctrl+shift+g` / `space G`
  (all occurrences), `alt+click` (toggle), visual block `I`/`A`; Esc
  collapses. Carets are per-view (#142) and re-clamp on reload/sync.
  See [editor](/architecture/editor.md).

- Persistent undo (#148, vim's `undofile`): undo/redo stacks survive a
  restart. New `internal/undostore` keeps one hash-keyed JSON file per
  document under `.ike/undo/`, written on save/close/quit (clean buffers
  only) and adopted on `Load` when the stored content hash matches the
  just-read file; any mismatch discards silently. Shared documents load
  once; `files.persistent_undo` (default on) toggles it; 1 MiB per-file and
  200-file LRU caps. See [editor](/architecture/editor.md),
  [session-restore](/architecture/session-restore.md).

- Large-file mode (#149): files over `files.large_file_kb` (default 1024) or
  `files.large_file_lines` (default 100000) degrade gracefully instead of
  stalling — highlighting off, LSP `didOpen` skipped, change events without
  text, watcher poll never content-hashes — with a warn toast, a
  `[large file]` status segment, and the `editor.forceCodeInsight` palette
  override per document (policy shared via new `internal/largefile`). See
  [editor](/architecture/editor.md),
  [highlighting](/architecture/highlighting.md),
  [lsp](/architecture/lsp.md).

- Sticky scroll (#168): the header lines of the declarations enclosing the
  first visible line pin as the pane's top rows (JetBrains-style), collected
  by the same Tree-sitter parse as the highlight spans via new
  `lang.Language.ScopeNodes` per language; clicking a pinned row jumps to the
  declaration, `editor.sticky_scroll` toggles it and
  `editor.sticky_scroll_depth` caps the nesting. See
  [editor](/architecture/editor.md),
  [highlighting](/architecture/highlighting.md).

- File templates (#170): newly created files start with language-aware
  content — `package ${PACKAGE}` for Go, `<?php` for PHP — rendered by
  `lang.TemplateFor` with `${FILENAME}/${NAME}/${DIR}/${PACKAGE}/${DATE}/${YEAR}`
  variables and overridable per language via `[lang.<id>] template`. Applies
  to explorer creates (written to disk), `:e` on a new path, and CLI opens of
  missing files (seeded, unmodified buffers). See
  [languages](/architecture/languages.md).

- Inlay hints (#171): inline parameter-name and inferred-type hints
  (`textDocument/inlayHint`) render as dimmed italic virtual text via the new
  `InlayHint` theme slot, refreshed document-wide on open/change and merged
  from embedded fragments; `lsp.inlay_hints` toggles them (default on), and
  the Go plugin enables gopls's parameter/type hint kinds by default. See
  [lsp](/architecture/lsp.md).

## 2026-07-11

- Document highlight (#172): occurrences of the symbol under the cursor are
  marked with a subtle background (read cool / write warm via the new
  `OccurrenceRead`/`OccurrenceWrite` theme slots), debounced 150 ms on cursor
  moves; fragment positions route to the fragment's server. See
  [lsp](/architecture/lsp.md).

- Fragment diagnostics (#415): diagnostics published on fragment documents
  now map back onto the host buffer and merge with the host server's own
  diagnostics into a single host-path publish (`manager/fragdiags.go`).
  Fragment diagnostics follow the fragment when host edits move it and clear
  immediately when the fragment closes or its language is stopped. See
  [lsp](/architecture/lsp.md).

- Fragment references/definition (#416): definition and references requests
  inside an embedded fragment now route to the fragment's server; result
  locations in fragment documents are rewritten to host-file locations
  (fragment URIs never reach the editor), real-file locations pass through,
  stale fragment locations are dropped. See [lsp](/architecture/lsp.md).

- Floating shell stale body (#409): `ui.Floating.View()` now re-renders its
  content body on every call (scroll offset preserved via `scroller.Refresh`),
  so modals that mutate state in place — the crash-recovery prompt's cursor and
  item removal — update on the next frame. The onboarding dialog's per-key
  `SetSize` workaround (#301) is removed. See
  [floating-shell](/architecture/floating-shell.md).

- Rename feedback (#426): a server that lacks the rename capability
  (intelephense free) now toasts "language server does not support rename"
  (`manager.ErrRenameUnsupported`) instead of the misleading "cannot rename
  here". See [lsp](/architecture/lsp.md).

- Comment toggling (#428): line-comment markers align with the comment on
  the line above the range (fallback: min indent, clamped to each line's
  indent), and blank lines now get a padded bare marker instead of being
  skipped — uncommenting empties them again. See
  [editor](/architecture/editor.md).

- Finder mouse support (#424): the find/replace-in-path overlay is now
  mouse-operable — click outside dismisses, clicks focus input rows, flip
  the Case/Word/Regex toggles, select result rows (press again to open),
  and the wheel scrolls the list. New `locations.List` seams: `ItemAt`,
  `SetCursor`, `Cursor`. See [search](/architecture/search.md).

- Finder ctrl chords (#422): every alt binding in the find/replace-in-path
  overlay gained a ctrl primary (`ctrl+c/w/x` toggles, `ctrl+f`/`ctrl+a`
  batch replace, `ctrl+enter` navigate, `ctrl+up/down` history) — on macOS
  Option composes characters, so the alt chords never arrived. Alt stays as
  secondary. See [search](/architecture/search.md).

- LSP call hierarchy (#173): `lsp.callHierarchy` (`ctrl+alt+h`, leader `H`)
  prepares the symbol under the cursor and opens a lazily-expanding
  callers/callees tree overlay (`internal/callhier`); `tab` flips the
  direction, `enter` jumps to the call site / declaration. See
  [LSP](/architecture/lsp.md).

- Tree-sitter language injections (#299): embedded fragments (SQL in Python
  strings) now highlight with the fragment language's own grammar. The SQL
  plugin gained a Tree-sitter grammar (DerekStride/tree-sitter-sql), so .sql
  files highlight too. See [highlighting](/architecture/highlighting.md).

- Embedded-language LSP via virtual documents (0300, #412–#414): Tree-sitter
  injection queries detect fragments (SQL in Python strings, capture
  convention `fragment.<lang>[.guess]`), the manager mirrors each into an
  `ike-fragment:` document on the fragment language's server and routes
  completion/hover inside the fragment there, mapping positions both ways.
  New `sql` language plugin (sql-language-server). Diagnostics (#415) and
  references (#416) follow. See [LSP](/architecture/lsp.md).

- First-start LSP onboarding (#301): a one-time dialog on the very first
  launch (no user config yet) lists the servers with install recipes as
  checkboxes; enter batch-installs the checked ones via `lsp.installMissing`,
  unchecked ones persist disabled, esc skips, `lsp.onboarded = true` keeps it
  from returning; `lsp.auto_install = false` suppresses it entirely. See
  [LSP](/architecture/lsp.md).

- Diagnostic navigation: `lsp.nextDiagnostic` / `lsp.prevDiagnostic` (f2 /
  shift+f2, JetBrains parity) step the cursor through the focused document's
  diagnostics in document order with wrap-around, toasting the message (#369).
  See [LSP](/architecture/lsp.md) and
  [Keybindings](/architecture/keybindings.md).

- New built-in theme `dracula` (official Dracula spec), AA-contrast checked
  (#385). See [Themes](/architecture/themes.md).

- New built-in themes `solarized-dark` / `solarized-light` (Ethan
  Schoonover's Solarized), AA-contrast checked (#386). See
  [Themes](/architecture/themes.md).

- New built-in theme `one-dark` (Atom's One Dark), AA-contrast checked
  (#387). See [Themes](/architecture/themes.md).

- New built-in theme `kanagawa` (wave variant of rebelot/kanagawa.nvim),
  AA-contrast checked (#388). See [Themes](/architecture/themes.md).

- f3/shift+f3 repeat a committed in-file search (`/`, `?`, cmd+f) like `n`/`N`
  while it is the most recent search; a new find-in-path scan reclaims them
  (#376). See [Project Search](/architecture/search.md).

- Go-to-symbol / search everywhere ranks project symbols above
  dependency/stdlib results, exact name match on top (#377). See
  [LSP](/architecture/lsp.md).

- `lsp.hover` (quick documentation) gets a delivered default chord (#378):
  `ctrl+q` (JetBrains Windows/Linux quick doc; XON is disabled in raw mode)
  plus the `space k` / `ctrl+k k` leader path (vim's K keyword lookup). See
  [Keybindings](/architecture/keybindings.md).

- Hover popup renders LSP markdown instead of showing it raw (#379): ```` ```go ````
  fence markers are stripped, the fenced signature block is syntax-highlighted
  via the language registry (accent tint when the fence tag has no grammar),
  and `---` draws as a horizontal rule. See [LSP](/architecture/lsp.md).

- Status line's LSP server segment is scoped to the focused buffer's language
  (#380): `ServerState` messages are tracked per language and the segment shows
  only the focused buffer's entry — non-LSP buffers show no server text, and
  stale event text no longer sticks globally. `host.SetStatus` stays as the
  plugin-facing global segment. See
  [Notifications](/architecture/notifications.md).

- Status line names the focused pane kind (#381): a focused terminal shows
  `TERMINAL │ shell · dir` (`[exited]` when dead), the explorer shows
  `EXPLORER`; editor mode/file/cursor render only while an editor holds
  focus. See [Integrated Terminal](/architecture/terminal.md).

- Settings window QoL pass (#383): ←→/h/l switch between the category column
  and the form (arrow-left only on custom pages, `h` stays page filter text);
  both columns scroll with the selection instead of truncating; enum entries
  open a picker list on enter (←/→ still quick-cycle on the row); the
  unfocused column dims its selection so focus is unambiguous; filtered
  results name the custom pages the filter cannot search. See
  [Settings UI](/architecture/settings-ui.md).

- Auto-installed language servers start without PATH surgery (#370):
  `transport.Resolve` probes `go env GOBIN` / `GOPATH/bin` and npm's global
  prefix after `exec.LookPath` fails and launches the server via absolute
  path, so a fresh `go install`ed gopls works immediately; the install
  success toast now fires only after the binary actually resolves, otherwise
  an error toast names the probed directories. See
  [LSP](/architecture/lsp.md).

- LSP actions no longer use a stale cursor after programmatic jumps (#371):
  `editor.SetCursor` now emits a cursor-move event, so go-to-definition /
  usages-pick / nav back-forward landings update the LSP bridge's tracked
  position and rename/references immediately act on the landed symbol. See
  [LSP](/architecture/lsp.md).

- LSP request errors surface as toasts (#372): a failing hover / definition /
  references / formatting / code-action request now raises an error toast with
  the server's message ("find usages failed: …") instead of silently doing
  nothing, via the shared `requestFailed` seam in `plugins/lsp/bridge.go`. See
  [LSP](/architecture/lsp.md).

- Explorer prompts never render invisibly (#373): a rename/delete prompt box
  wider than the pane used to be silently dropped while still capturing keys
  (blind renames/deletes). `promptBox` now truncates the title and windows the
  input row to the pane width, and `View` overlays via `overlay.Place` (clips)
  instead of `overlay.Center` (drops). See
  [explorer — file operations](/architecture/explorer.md).

- Palette-invoked explorer file ops focus the explorer (#374): dispatching
  `NewFileMsg`/`NewDirMsg`/`DeleteMsg`/`RenameMsg` from the command palette now
  moves focus to the explorer pane first (re-showing a hidden tree), so the
  modal prompt captures every typed key instead of leaking vim commands into
  the focused editor buffer. See
  [explorer — file operations](/architecture/explorer.md).

- Theme contrast audit (#384): all built-in themes now pass WCAG AA (≥ 4.5:1)
  on the rendered fg/bg slot pairs, enforced by the new table-driven
  `TestBuiltinThemeContrast`. Light themes (gruvbox-light, rose-pine-dawn,
  catppuccin-latte) had their accent/diagnostic slots darkened; the default
  theme's near-invisible `Error`/`Info`/`Hint`/`Warning` were lifted.
  Selected-row renderers (settings pages, pickers) now always set
  `Foreground(SelectionText)` with a `Selection` background instead of
  inheriting the terminal default. See
  [themes — contrast rule](/architecture/themes.md).

- 0082 sheet 11+13 verdicts (#18): `f4` is the delivered primary for
  `lsp.definition` (JetBrains jump-to-source; `cmd+b` stays secondary), and
  `shift+f6` is context-aware refactor-rename — `lsp.rename` in an editor,
  `file.rename` in the explorer. Label/matrix primary selection now prefers
  fewer keystrokes before shorter strings, so single-step chords beat leader
  sequences. Matrix regenerated. See
  [keybindings](/architecture/keybindings.md).

- Workspace edits (rename/format/code actions) apply once per document, not
  once per view (#366): `FormatEditsMsg` now goes through exactly one view of
  a shared document; per-view routing hit the aliased buffer N times when the
  file was open in a second tab/split. See [lsp](/architecture/lsp.md).

- Rename no longer applies edits twice (#364): `WorkspaceEdit.AllChanges`
  prefers `documentChanges` over `changes` per spec instead of merging both —
  pylsp sends the same edits in both fields, corrupting the buffer
  (`z` → `match1` became `match1atch1`). See [lsp](/architecture/lsp.md).

- `readStdin` folded back into `cmd/ike/main.go` (#362) so the single-file
  invocation `go run cmd/ike/main.go` compiles again; `cmd/ike/stdin.go`
  deleted. See [foundation](/architecture/foundation.md).

- Zen mode (#359, Roadmap 0290 — epic complete): `view.zenMode`
  (`cmd+k shift+z`, View menu — the dormant entry is live now) maximizes the
  active editor and hides the tab bar + status line; leaving zen restores
  both, tree mutations drop it like the zoom. See
  [Pane Layout](/architecture/pane-layout.md).

- Pane maximize (#358, Roadmap 0290): `pane.maximize` (`cmd+k z`, View menu,
  palette) zooms the focused pane tmux-style — it renders alone over the body
  while the split tree survives untouched; any leaf-set change (split, close,
  relocation) auto-unzooms via a signature check in `layout()`. Not
  persisted. Documented in [Pane Layout](/architecture/pane-layout.md);
  zen mode follows as #359.

- Paste from History (#57): every yank/delete feeds a bounded 20-entry
  register history; `editor.pasteFromHistory` (`cmd+shift+v`, Edit menu,
  palette) picks an entry from a palette list (first line + size, fuzzy
  filter) — it becomes the current clipboard and pastes with Cmd+V
  semantics. See [Editor](/architecture/editor.md); matrix regenerated.

- Scratch picker (#352, Roadmap 0280 — epic complete): `scratch.list`
  ("Open Scratch File…", palette + File menu) locks the palette to a new
  scratch mode — newest-first, fuzzy filter, enter opens; empty store shows
  an inert hint row. See [Scratch Files](/architecture/scratch-files.md).

- Scratch files land (#350/#351, Roadmap 0280): `scratch.new` and per-language
  "New Scratch File: <Lang>" palette commands (File menu too) create
  `scratch-N.<ext>` under `$IKE_CONFIG_DIR/scratches` / `~/.ike/scratches`
  (new `internal/scratch` store) and open it focused through the standard
  funnel — highlighting/LSP/session flow from the extension. Documented in
  [Scratch Files](/architecture/scratch-files.md); picker follows as #352.

- Split view (#147): `editor.splitViewRight`/`Down` (`cmd+k shift+right`/
  `shift+down`, View menu, palette) split the focused editor into a second
  live shared view of the same document (#142), cursor/scroll copied from the
  source, new view focused; file-less editors no-op with a toast. Documented
  in [Editor](/architecture/editor.md); keybinding matrix regenerated.

- `command | ike -` pipes stdin into a scratch buffer (#344, Roadmap 0270):
  read to EOF before the UI starts, opened focused after any file targets,
  dirty + never-saved so the quit guard prompts and `:w <path>` names it; the
  keyboard re-points at /dev/tty. `ike -` on a TTY fails fast. Roadmap 0270
  is complete.

- CLI open targets (#343, Roadmap 0270): `ike file.go:42`, `file.go:42:7` and
  vim-style `+42 file.go` open files as tabs at startup — first target focused
  with the cursor placed, explorer revealing it; a nonexistent path opens as
  an unsaved buffer. Session restore still runs first; CLI files win focus.
  Documented in [Foundation](/architecture/foundation.md); README usage updated.

- Shift+Tab in insert mode dedents the whole current line one indent unit
  (#337, Roadmap 0260) — the same unit `<<` removes — wherever the cursor
  sits, inside the open insert's undo unit; plain Tab keeps inserting one
  unit at the cursor (and still accepts an open completion popup).

- Enter and `o` gain language-aware smart indent (#336, Roadmap 0260): with
  `editor.auto_indent` on, a line whose trimmed text ends with a block opener
  (`lang.IndentAfter` — Python `:`, Go/PHP braces) opens the next line one
  `tabText()` unit deeper; Enter keys off the text left of the cursor, `O` and
  unknown languages keep plain copy-indent. Documented in
  [Editor](/architecture/editor.md).

- The language registry gains smart-indent metadata (#335, Roadmap 0260):
  `Language.IndentAfter` lists trimmed-line suffixes that open a block, resolved
  per buffer path via `lang.IndentAfter`. Python registers `":"` + open
  brackets, Go and PHP register `{ ( [`. Documented in
  [Language Registry](/architecture/languages.md).

- Files already open at startup now receive an LSP `didOpen` (#332): the session
  restore paths load editors directly (bypassing the interactive open), so
  `Model.Init` fires the file-open hook for each restored file — deduped per path
  for buffers shared across tabs (#142) — instead of leaving them without a
  server until reopened.

- Accepting an LSP completion no longer duplicates the already-typed identifier
  prefix (#330): the insert now replaces the identifier run before the cursor
  (`identifierStart`) rather than the request anchor, which is empty for a
  manual `ctrl+space` trigger (`xyz.__` + `__dict__` had produced `xyz.____dict__`).

- Tab cycling gains an `alt+home`/`alt+end` default pair, `alt+shift+home/end`
  to move the active tab (#328): on Macs without physical PgUp/PgDn keys,
  `fn+ctrl+arrows` is claimed by macOS globals, while `fn+option+left/right`
  arrives as exactly these chords. `ctrl+pgup/pgdown` stay bound.

- Shift+arrow selections stop on unshifted navigation (#326): a selection
  started with `Shift+arrows` is now GUI-style — releasing Shift and pressing
  a plain navigation key (arrows, `Home`/`End`, word/paragraph/page keys)
  drops the selection and moves the caret (vim's `keymodel=stopsel`), instead
  of extending it. Vim motions and `v`/`V`/`Ctrl+V` selections keep extending.
  Documented in [Editor](/architecture/editor.md).

- Center drop zone merges as tab (#318): during a move or tab drag an editor
  target now shows five zones — the four edges split/relocate as before, the
  interior center merges JetBrains-style: a whole-pane drop moves all source
  files into the target's tab list (deduped) and closes the source pane, a
  tab drop joins the list with that file; edge drops of a tab on an editor
  now split next to it. Feedback distinguishes the center (`⧉ merge as tab`
  marker, full-pane ghost). Documented in
  [Pane Layout & Drag](/architecture/pane-layout.md).

- LSP popups framed and freed from the pane clamp (#316): completion,
  signature and hover render inside a rounded themed border (like the
  floating shell) and may now overflow the owning pane — clamped to the
  terminal edges instead, still shifting left / flipping above the anchor;
  the width/height caps and ellipsis row stay as safety nets. Documented in
  [LSP](/architecture/lsp.md).

- Word-wise alt+arrow cursor motion (#303): `alt/opt+←/→` (and the delivered
  `ctrl+←/→` fallback) now move word-wise within the current line with `.` as
  a stop point; `shift+alt/ctrl+←/→` extend the selection the same way. The
  alt+arrow tab-cycling secondaries were freed for this — tab cycling keeps
  `ctrl+pgup/pgdown`. Documented in [Editor](/architecture/editor.md),
  [Editor Tabs](/architecture/editor-tabs.md) and
  [Keybindings](/architecture/keybindings.md).

- Tab drop next to terminals (#317): a dragged tab released on a non-editor
  pane's edge zone now splits that pane and opens the file in the fresh
  editor leaf; interior drops stay a no-op and the drag feedback (zone
  arrow, ghost) reflects it. Documented in
  [Pane Layout & Drag](/architecture/pane-layout.md).

- Terminal duplication on project switch fixed (#320): when layout restore
  already recreated a terminal under the carried session's key, the live
  session now takes over that pane instead of splitting a duplicate leaf
  (which mirrored one instance in two panels). Documented in
  [Terminal](/architecture/terminal.md).

- Signature popup lifecycle (#315): leaving insert mode and insert-mode
  arrow motion dismiss the popup, stale replies after esc are dropped — it
  no longer trails normal-mode cursor motion. Documented in
  [LSP](/architecture/lsp.md).

- Code-action clarity (#309): readable kind chips in the palette list, an
  explainer in the wiki, and feedback for every apply outcome — edited-N,
  changed-nothing, unresolved-action warning, command errors. Documented in
  [LSP](/architecture/lsp.md).

- ctrl+space triggers completion manually (#302): both the Kitty and the
  legacy NUL spelling emit the same trigger event the "." auto-path uses.
  Documented in [LSP](/architecture/lsp.md).

- LSP popup fixes from live use (#306, #307, #308): signature/hover popups
  clamp to the owning pane (width wrap, ellipsis row, shift/flip placement),
  mouse clicks dismiss cursor-anchored popups, the completion list shows an
  accept-keys hint and the signature popup a dim `ƒ` marker. Documented in
  [LSP](/architecture/lsp.md).

- Live workspace-symbol palette mode (#295, Epic 0250 phase 2): cmd+o now
  opens the palette locked to a live symbol mode — 150 ms debounced
  `workspace/symbol` re-query per keystroke (`palette.LiveMode` plumbing),
  symbol-name rows, stale replies dropped — and the same mode fills the
  search-everywhere seat (#236) via silent hook-priming. Replaces the
  phase-1 prompt. Documented in [LSP](/architecture/lsp.md) and
  [Command Palette](/architecture/command-palette.md).

- Workspace-symbol search (#294, Epic 0250 promoted from idea #146):
  `project.goToClass` (cmd+o / leader `S`) prompts for a query and lists
  `workspace/symbol` hits in the references palette (Enter navigates like
  go-to-definition); capability-gated with honest no-provider/zero-hit
  toasts. The last non-VCS blocked-ledger entry is gone. Documented in
  [LSP](/architecture/lsp.md); status matrix regenerated in
  [Keybindings](/architecture/keybindings.md).

- Find/replace panel (#283, Epic 0240 phase 2): `editor.replace` (cmd+r /
  leader `R`) now opens a two-field panel — Find with live incremental
  preview and match tally, Replace, tab to switch — finishing through the
  one substitute engine: ctrl+a replaces all ("N substitutions" report),
  enter enters the y/n/a/q/l confirm flow, esc cancels and restores the
  origin. The panel counts as capturing so global keys keep out of its
  inputs. Replaces the phase-1 ex-line prefill (#282). Documented in
  [Editor](/architecture/editor.md).

- 0082-review fixes documented (#289): blocked-chord toast (#267) and
  bracket-glyph canonicalisation (#284) in
  [Keybindings](/architecture/keybindings.md); canonical open paths / tab
  dedupe (#272) and the app-quit unsaved-changes guard (#287) in
  [Editor Tabs](/architecture/editor-tabs.md); visual-mode counts (#265) in
  [Editor](/architecture/editor.md); finder query preselect (#277) in
  [Project Search](/architecture/search.md); save-all no-op hint (#275) in
  [Notifications](/architecture/notifications.md).

- In-file replace, phase 1 (#282, Epic 0240 promoted from idea #49):
  `editor.replace` (cmd+r, leader `R`) opens the ex line prefilled with
  `%s/<pattern>/` (seeded from the committed search when literal and
  slash-free) and runs through the existing `:substitute` engine — flags,
  per-match confirm and single-undo included. The chord left the blocked
  ledger. Documented in [Editor](/architecture/editor.md); status matrix
  regenerated in [Keybindings](/architecture/keybindings.md).

- Multi-target go-to-definition picker (#279): several definition sites open
  the references-style palette list ("Definitions — pick a target…") instead
  of silently jumping to the first; one site still jumps directly. The
  location→reference conversion is now shared with find-references. From the
  0082 sheet 11 protocol (#18). Documented in [LSP](/architecture/lsp.md).

- Cheatsheet live filter (#271): typing in the help overlay narrows the
  bindings (titles + shortcuts, empty groups drop, title echoes the filter);
  `q`/`?` stay dismiss keys only while the filter is empty, esc clears then
  closes. Implements the last open item on 0082 sheet 27 (#21). Documented in
  [Help Overlay](/architecture/help-overlay.md).

- Explorer hide/show (#268): `explorer.toggle` (`space e` / cmd+1) now runs
  the JetBrains cmd+1 state machine — focused tree hides and editors reclaim
  the width, a hidden tree comes back at its remembered ratio and takes
  focus; the hidden state persists across restarts. Found running the 0082
  sheet 25 protocol (#21). Documented in
  [Explorer](/architecture/explorer.md).

- Search-everywhere follow-ups (#263): `space space` opens the mode (the
  terminal stand-in for JetBrains' double-shift, via the leader engine), and
  an empty query now lists the recent files first (active file excluded)
  before the command listing. From 0082 sheet 17 (#20). Documented in
  [Command Palette](/architecture/command-palette.md).

- Save feedback on the ex line (#261): `:w` / `cmd+s` report `"file" written`
  on success and a vim-style `E: <error>` on failure (previously silent);
  a failed write keeps the buffer dirty, aborts `:wq`, and a nameless
  scratch `:w` reports "E: no file name". Found running the 0082 sheet 14
  protocol (#19). Documented in [Editor](/architecture/editor.md).

- Unsaved-changes guard on close (#259): `cmd+w` / `ctrl+w` / `:q` on a dirty
  buffer now prompt save/discard/cancel instead of silently dropping the
  edits; `:q!` forces, shared documents skip the prompt, a failed save keeps
  the tab open. Found running the 0082 sheet 16 protocol (#19). Documented in
  [Editor Tabs](/architecture/editor-tabs.md).

- Smartcase search (#257): `/` `?` (and the incremental preview, `n`/`N`,
  ex `/pat/` addresses) fold case for all-lowercase patterns and stay exact
  once the pattern contains an uppercase rune; `*`/`#` remain exact-word,
  `:s` keeps its explicit `i`/`I` flags. Closes the last behavior item on
  0082 sheet 09 (#18). Documented in [Editor](/architecture/editor.md).

- Incremental in-file search (#255): the `/` line now previews live — per-
  keystroke jump to the nearest match, match-count tally on the input line,
  full-buffer match highlighting (current match underlined), exact
  cursor/viewport restore on Esc, and "no matches" / "search wrapped"
  ex-line hints. Normal-mode Esc clears highlights (`:noh`-style). Found
  running the 0082 sheet 09 protocol (#18). Documented in
  [Editor](/architecture/editor.md).

- Copy/cut feedback toasts (#252): `editor.copy` / `editor.cut` report what
  landed in the clipboard ("copied 1 line", "cut 5 chars") through the
  existing `NoticeMsg` toast path; vim-native `y`/`d` stay silent. Found
  running the 0082 sheet 03/04 protocols (#17). Documented in
  [Editor](/architecture/editor.md).

- Undo tracks the save point (#251): the history pins a checkpoint on save,
  and undo/redo clear the modified flag when they land exactly on it — `[+]`
  no longer sticks after undoing back to the saved content. Crash-restored
  buffers never read as clean. Found running the 0082 sheet 01/02 protocols
  (#17). Documented in [Editor](/architecture/editor.md).

## 2026-07-10

- Search-everywhere palette mode (#236): `cmd+shift+a` / double-shift (leader
  `A`) rank one query across commands and files by composing the existing
  command and file modes — per-kind cap, score interleave, kind glyph per row.
  `palette.searchEverywhere` left the blocked ledger. Documented in
  [Command Palette](/architecture/command-palette.md).

- Delivered tab chords (#248): `ctrl+pgup`/`ctrl+pgdown` cycle tabs and
  `ctrl+shift+pgup/pgdown` reorder them — delivered primaries for the
  alt-arrow chords that never arrive on macOS (Option composes characters).
  The reachability rules now exempt CSI-parameter keys from the ctrl+shift
  collapse. Documented in [Keybindings](/architecture/keybindings.md) and
  [Editor Tabs](/architecture/editor-tabs.md).

- Insert-mode backward kills (#246): `option+backspace` / `ctrl+w` delete the
  previous word, `cmd+backspace` / `ctrl+u` delete to the line start, all
  through the open insert session's recorder (one undo unit). Documented in
  [Editor](/architecture/editor.md).

- Defaults for palette-only commands (#242): `f3`/`shift+f3` step retained
  search matches, `alt+f1` reveals the open file in the explorer (fragile,
  palette fallback), leader `T` opens a new terminal and leader `h` the
  notification history. Status matrix regenerated in
  [Keybindings](/architecture/keybindings.md).

- Theme override survives config reloads (#241): `reloadConfig` no longer
  unconditionally re-resolves `[theme].name` — a palette-selected runtime
  theme now survives unrelated settings edits; an explicit `[theme].name`
  change still wins and clears the override. Documented in
  [Themes](/architecture/themes.md).

- Terminal word/line kill chords (#240): `motionKey` extends the #225 natural
  text editing set — `option+backspace` sends `ESC DEL` (backward-kill-word),
  `cmd+backspace` sends `ctrl+u` (kill to line start). Documented in
  [Integrated Terminal](/architecture/terminal.md).

- Wheel-event coalescing (#238): queued mouse-wheel events accumulate in the
  root model and apply in a single update pass via a scheduled `wheelFlushMsg`,
  so fast scroll bursts cost one render instead of replaying every stale event;
  any non-wheel message flushes the batch first to preserve input ordering.
  Documented in [Pane Layout & Drag](/architecture/pane-layout.md).

- Recent-files palette mode (#235, Roadmap 0230): `palette.recentFiles`
  (cmd+e / leader m / Navigate menu) opens the palette locked to an MRU file
  list — touched on every open and tab activation, persisted as
  `recent_files` in .ike/session.json, active file excluded so enter jumps
  to the previous file. The binding left the keymap blocked ledger.

- Editor horizontal scrolling (#230): horizontal wheel and shift+wheel scroll
  the editor viewport sideways (`editor.ScrollXBy`, wired in `app.handleMouse`
  like the explorer's), clamped so the longest visible line keeps its last
  character on screen; the cursor stays put.
- Counted undo/redo (#231): `{count}u` / `{count}ctrl+r` undo/redo count
  changes at once, stopping early when the history runs out.

- Terminal mouse selection + copy (#227): left-drag selects text on the grid
  (virtual-anchored, survives scrollback paging), cmd+c copies it to the
  system clipboard; clicks forward to mouse-reporting children instead.
- Terminal focus escape (#228): the spatial focus moves (default
  ctrl+arrows, keymap.bindings.focus_* overrides) now work from a focused
  terminal; the reserved-set table grew accordingly.

- Terminal wheel routing (#226): the mouse wheel now reaches the running
  application — forwarded as encoded mouse events when the child enabled
  mouse reporting, as arrow keys on the alt screen (less/man/vim), and it
  keeps paging ike's scrollback at the plain prompt.

- Terminal macOS editing chords (#225): option+left/right word-jump
  (ESC b / ESC f), cmd+left/right line start/end (ctrl+a / ctrl+e) — the
  iTerm "natural text editing" convention, translated in
  internal/terminal/model.go (motionKey).

- Terminal shifted-input fix (#224): the vt encoder drops non-special keys
  that still carry a modifier, so uppercase/caps-lock characters never
  reached the shell; the pane now replays shift/caps-lock/num-lock-only
  text presses as their produced text (`toVTKeys` in
  internal/terminal/model.go).

- Navigation history cross-pane polish (#220, Roadmap 0220, closes the
  epic): stale-entry skipping via BackWhere/ForwardWhere validity filter
  (deleted/renamed files are dropped silently, traversal continues, no
  duplicate departures on the opposite stack); back/forward acts in the
  active editor pane with split layouts. TUI usability pass over
  finder/definition jump chains incl. deleting a mid-chain file.
- In-editor jump sources (#219, Roadmap 0220): the editor emits EventJump
  (departure position) for large motions (gg, G, {count}G via
  motion.Result.Jump) and search landings (initial //? jump, n/N, */#
  via jumpTo); the app's editorEmitter records it into the shared
  history and swallows the event. Small motions and operator-composed
  motions (dG) never record. navigation-history.md refreshed.
- Navigation history core (#218, Roadmap 0220 — promoted from idea #51):
  internal/nav History (per-jump entries, forward-truncation on fresh
  jumps, same-line dedup, 100-entry cap) recorded at the open funnel
  (openPath file switches, openPathAt same-file line jumps — covers
  definition/references/search/file opens at two choke points); nav.back /
  nav.forward appCommands unblock the cmd+bracket defaults (removed from
  the 0081 blocked ledger) with new leader mnemonics space b / space i;
  status matrix regenerated. New concept doc
  architecture/navigation-history.md.
- Sandbox limits + plugin manifest (#27, Roadmap 9900, closes the epic):
  per-module memory cap (64 MiB default) and call deadlines (5 s default,
  wazero CloseOnContextDone) on every guest call incl. _initialize; a
  runaway callback closes the module and the bridge unloads it with an
  error toast. Optional sidecar <plugin>.manifest.json (name/version/
  capabilities) validated strictly at load — invalid manifests reject the
  module; a present manifest gates registration kinds (bridge drops
  undeclared ones with diagnostics) and host calls (gated "ike" module →
  no-ops). Docs in plugins.md, plugin-authoring.md, sdk/README.md; example
  plugin ships a least-privilege manifest.
- Go guest SDK + example plugin (#26, Roadmap 9900): sdk/ (nested module
  ike/sdk) wraps the raw ABI in a typed guest API — Command/Keymap/Hook
  declarations plus Notify/SetStatus/OpenFile/Dispatch/ConfigGet host
  calls; sdk/example is a buildable reference plugin; new authoring guide
  wiki/architecture/plugin-authoring.md (SDK, build via GOOS=wasip1
  -buildmode=c-shared, ABI reference for other languages). Full-pipeline
  test builds the example and drives it through scan → register → invoke.
- WASM capability bridge (#25, Roadmap 9900): internal/wasm/bridge adapts
  loaded modules into plugin.Plugin — register() descriptors become
  registry commands/keymaps/hooks (guest callbacks run inside tea.Cmds,
  faults toast as warnings), HostAdapter binds abi.Host onto the live
  host.API late (main.go instantiates the "ike" module before guests load,
  SetAPI after app.New). A WASM plugin is now palette-reachable and
  indistinguishable from a compile-in plugin; parity is pinned by tests
  against a real Go wasip1 guest.
- WASM ABI (#24, Roadmap 9900): internal/wasm/abi fixes the host↔guest
  contract — JSON payloads over (ptr,len) regions, packed-u64 returns,
  guest ike_alloc for host→guest buffers; guest entry points register/
  on_command/on_key/on_hook; host imports open_file/dispatch/notify/
  set_status/config_get as thin shims over the narrow abi.Host interface
  (malformed payloads dropped). Verified end to end against a real Go
  wasip1 c-shared guest exercising every shim.
- WASM plugin runtime (#23, Roadmap 9900): internal/wasm embeds wazero —
  plugins-dir scan (diagnostic-and-skip on faults), load/instantiate/unload
  lifecycle supporting both WASI conventions (command _start incl. clean
  proc_exit, reactor _initialize with callable exports), no ambient FS/net,
  guest stdio sunk. Tests build real Go wasip1 fixtures (including a
  c-shared reactor whose add export is called through the sandbox).
  main.go scans at startup; the capability bridge is #25.
- Per-binding status matrix (#16, Roadmap 0081): generated acceptance
  ledger (keymap.StatusMatrix/MatrixMarkdown) — one row per default-bound
  command with primary chord, reachability class, reachable fallback and
  resolution status; the final-gate test in cmd/ike runs against the
  shipped plugin set and fails on any unresolved row. All 60 rows resolve:
  live, live-via-fallback, or honestly blocked. Table persisted in
  architecture/keybindings.md. This closes Roadmap 0081 — epic #39 and its
  milestone.
- Keybinding discoverability (#15, Roadmap 0081): which-key panel for held
  chord prefixes (live continuations, letters first); keymap.LiveBindings
  gives the cheatsheet and the palette shortcut column honest labels from
  the effective table across reloads (delivered chord plain, fragile with
  warning + escape route, blocked with dependency); the cheatsheet gains a
  never-hidden blocked section.
- Leader key & terminal-safe defaults (#14, Roadmap 0081): space-leader
  (outside the editor, [keymap] leader tunable) plus universal ctrl+k
  mnemonics through the existing chord resolver — go-to-file, grep,
  project switch, terminal, LSP actions, tabs and more get delivered
  two-keystroke paths. Fragile flags now derive from the reachability
  table instead of hand-maintained booleans; a completeness test enforces
  an escape route (leader / delivered chord / documented exception) for
  every fragile default.
- Terminal reality probe & reachability table (#10, Roadmap 0081):
  cmd/keyprobe (interactive chord probe with machine-parseable PROBE lines
  and shift-collapse evidence) plus internal/keymap/reachability.go — every
  default chord classified delivered/fragile/undetectable with an honest
  note; ground truth recorded against tmux/macOS (ctrl+tab eaten,
  ctrl+shift+z collapses to ctrl+z, alt+* and Kitty-encoded cmd+* arrive).
  Downstream 0081 work (#14–#16) keys off these classes. Table persisted in
  architecture/keybindings.md.
- Toolchain environment injection (#98, Roadmap 0170): per-project shim dir
  (.ike/shims) with exec scripts for php/python/python3 targeting the
  settings-page interpreters (silent detection never injects); terminal
  spawns prepend it to PATH, venv choices set VIRTUAL_ENV + venv bin; the
  pane title indicates the mapping; shims regenerate on config reload and
  retarget running sessions (exec by absolute path). No setting → untouched
  env. Windows .cmd note documented. This closes Roadmap 0170 — epic #88
  and its milestone.
- Terminal commands & UX (#97, Roadmap 0170): terminal.toggle (alt+f12,
  JetBrains state machine: create → focus → return-focus, also reserved
  inside a focused terminal), terminal.clear (canonical 2J+3J wipe — 2J
  alone pushes lines into the scrollback — plus ctrl+l prompt repaint),
  Tools-menu entries, and OSC 0/2 titles appended to the pane title.
  Chord and commands land together, so no blocked-ledger entry.
- Terminal workspace integration (#96, Roadmap 0170): pane titles show
  shell + origin dir; the reserved key set is documented and minimal
  (ctrl+tab only — everything else reaches the shell); scrollback paging
  via shift+pgup/pgdn and the mouse wheel (styled history lines, marker
  row, snap-back on typing); layout persistence restores terminals as
  fresh shells in their saved position with the origin cwd; live sessions
  survive a project switch (adopted below the new active editor, titled
  with their origin root). Spawn dirs are pinned absolute.
- Integrated terminal core (#95, Roadmap 0170): new internal/terminal —
  creack/pty spawns the shell (terminal.shell → $SHELL → /bin/sh) in the
  project root, charmbracelet/x/vt emulates the screen, output notifications
  are coalesced. pane.KindTerminal + terminal.new (splits below the active
  editor); focused terminals take every key raw with ctrl+tab as the escape
  hatch; shell exit closes the pane; terminal leaves prune from layout
  restore until #96. Quality bar verified: vim, less, resize, colors. New
  doc architecture/terminal.md.
- Python environment management (#132, Roadmap 0180): the toolchain page
  creates a project venv (uv, python -m venv fallback) and installs managed
  Pythons picked from `uv python list`; results register the absolute
  interpreter via write-back ([lang.python] interpreter) and restart the
  servers. Async cmds with fake-runner tests; uv-absent fallback covered.
  This closes Roadmap 0180 — epic #129 and its milestone.
- Plugin manager page (#133, Roadmap 0180): settings panel gains a Plugins
  page — every registered plugin with live enabled state, capability
  summary and expandable inspection; `e` toggles plugins.<id>.enabled via
  write-back (new real [plugins] config section; applyPluginConfig is now
  symmetric and runs on reload). Language packages register a `lang-<id>`
  plugin shim (plugins/languages/register), so a disabled language plugin
  takes its LSP server with it and enabling one kicks the missing-server
  install (new lsp.installMissing command). Registry.Describe lists
  disabled plugins' capabilities for inspection.
- LSP semantic-token highlighting (#9, Roadmap 0100): new
  internal/highlight/semantic decodes legend-based 5-tuples into highlight
  spans (modifier-refined capture mapping, UTF-16 via convert.go); manager
  requests full/delta with per-document result state; bridge refreshes on
  open + change (coalesced); editor layers the overlay over the
  Tree-sitter base (base < semantic < diagnostics). Verified against gopls
  in a CGO-free build (no Tree-sitter): the whole file renders from the
  overlay alone. **This closes Roadmap 0100 — epic #38 and its milestone.**
- LSP incremental didChange sync (#13, Roadmap 0100): the manager now
  respects the negotiated TextDocumentSyncKind — incremental servers get
  the minimal change region (common-prefix/suffix diff against the
  previously synced lines, manager/incremental.go) instead of the whole
  document on every keystroke; full-sync servers keep the old behaviour,
  SyncNone servers get nothing. UTF-16/UTF-32 offsets via convert.go,
  monotonic versions that only advance on a real send. Verified against
  gopls (negotiates incremental): diagnostics track correctly through
  inserts, newline splits and line deletes.
- LSP signature help (#4, Roadmap 0100): typing a server-advertised trigger
  character opens a cursor-anchored popup with the active signature, the
  active parameter emphasised (substring and UTF-16 offset-pair labels both
  resolve), first doc line and overload counter. While showing, every change
  retriggers so the parameter follows the cursor; a null answer (past ")")
  or esc dismisses. Capability-gated; completion popup takes precedence.
  Verified against gopls: popup on "(", highlight moves on ",", gone on ")".
- LSP code actions (#8, Roadmap 0100): `lsp.codeAction` (alt+enter) lists
  quick-fixes/refactors for the cursor or visual selection in a locked
  palette picker (preferred first), passing cached diagnostics as context.
  Chosen actions apply their WorkspaceEdit via workspace_edit.go and/or run
  workspace/executeCommand; the manager now answers server-initiated
  workspace/applyEdit (off the read loop — inline responding can deadlock
  against a flushing server) through the new Callbacks.ApplyEdit seam.
  Verified against gopls: Organize Imports removes an unused import through
  the full executeCommand → applyEdit round trip.
- LSP rename symbol (#6, Roadmap 0100): `lsp.rename` — prepareRename
  validation (reject toast), name prompt prefilled with the symbol
  (bridge-built Apply continuation keeps the manager out of the app), and
  WorkspaceEdit application through new shared infrastructure
  (`plugins/lsp/workspace_edit.go`): open buffers in-editor as one undo
  unit, closed files rewritten on disk; both WorkspaceEdit shapes decode.
  Manager splits edits by open/closed and converts positions (UTF-16 in
  convert.go). Verified against gopls across an open and a closed file.
- LSP document & range formatting (#7, Roadmap 0100): `lsp.format`
  (`cmd+alt+l`) and `lsp.formatRange` apply server `TextEdit`s to the buffer
  as one undo unit via the new `editor.ApplyTextEdits` (bottom-up, clamped,
  multi-line). Editor events now carry the visual anchor so the bridge knows
  the selection for range requests; `FormattingOptions` honour
  `editor.tab_width`/`use_spaces`; UTF-16 conversion stays in
  protocol/convert.go (manager converts, owning the synced lines).
  Capability-gated both ways; file-open now primes the bridge's current file
  so formatting works before the first cursor move.
- LSP find references (#5, Roadmap 0100): `textDocument/references` through
  client/manager (capability-gated on `referencesProvider`, UTF-16 conversion
  via protocol/convert.go), new `lsp.references` command ("LSP: Find
  Usages"), `alt+f7` reconciled in the chord table (blocked-ledger entry
  removed). Results route by count: toast / direct navigation / palette
  locked to a new references mode with `path:line` + preview and fuzzy
  filter; activation reuses the DefinitionMsg navigation path. Tests across
  client, manager (fake server echoes includeDeclaration), and app routing.
- Project switching complete (#3, Roadmap 0090): msg-driven switch
  transaction — `SwitchTo` validation, unsaved-changes guard (save-all /
  discard / cancel in the floating shell), root-model re-root via chdir +
  model rebuild with the live host carried over (LSP bridge and program
  sender survive), session/layout persisted per project, history recorded on
  success with a config reload. `alt+shift+p` added to the JetBrains chord
  table so the picker opens from a capturing editor. Fixed: the floating
  shell drops boxes wider than the terminal — prompt paths render through
  `CompactPath`. Epic #37 closes with this.
- project.switch command + picker (#12, Roadmap 0090): `internal/project`
  registers the `project.switch` command (default slot `alt+shift+p`) and a
  palette picker mode listing recent projects newest-first with fuzzy match
  on name/path, compacted path details and an `Open "<query>"…` affordance.
  The root model opens the picker locked and routes `PickedMsg` (stub toast
  until the switch orchestration #3 lands). File-menu "Switch Project" now
  resolves. Doc `architecture/project-switching.md` updated.
- Project-switching data layer (#2, Roadmap 0090): new `internal/project`
  package — `Entry` (path/name/last_opened), `Validate` (expand `~`, absolute,
  exists/is-dir/readable with actionable errors) and history content rules
  (upsert-by-path, move-to-front dedupe, cap at `project.max_history`),
  persisted to the user layer via config's typed setter. `project.history`
  becomes a `[[project.history]]` table array (`config.ProjectHistoryEntry`;
  `config.PushHistory` removed). Startup records the initial open before the
  model loads config. New doc `architecture/project-switching.md`; config doc
  updated.
- Missing-server install helper (#131, Roadmap 0180): `lang.ServerSpec` grows
  an `Install` recipe (argv; gopls via `go install`, pyright/intelephense via
  `npm -g`). A server launch failing with ErrNotFound on file open triggers
  the recipe automatically in the background (`plugins/lsp/install.go`) —
  progress/result toasts, on success the triggering document re-opens so the
  server starts immediately. New config `lsp.auto_install` (default true);
  the Language Servers page toggles it (`A`) and runs the install manually
  (`i`) — the retry path after a failure. Guards: one install per language,
  auto path backs off after a failure, failures log the output tail to
  debug.log (root model, every ServerEventError). Tests in `plugins/lsp`
  (fake runner: recipe, opt-out, backoff, concurrency, no-recipe warn) and
  `internal/settings`; wiki (lsp.md, settings-ui.md) updated.

- Language-server settings page (#130, Roadmap 0180): new custom settings
  page "Language Servers", contributed by the LSP plugin via SettingsPages
  (`internal/settings/lsp_page.go`). One row per language with a server:
  live status (ready/idle/crashed/missing/disabled/off-master — from
  language-tagged `ServerStatusMsg`s, which now carry `Lang`, plus the new
  `Manager.RunningLangs`), effective command line and source layer. Controls:
  `E` master switch, `e` per-server enable (new `[lsp.servers.<id>] enabled`
  key, honored by `resolveSpec`), inline `c`/`a`/`s` command/args/settings
  overrides (project-scope write-back, empty = reset), `x` reset all, `r`
  per-server restart (new `Manager.StopLang`, async per #123), `R` restart
  all. Missing binaries render the launch-failure reason with an install-
  helper hint (#131). Tests across `internal/settings`, `internal/lsp/manager`
  and `plugins/lsp`; wiki (settings-ui.md, lsp.md) updated.

- Editor tabs — session persistence (#160, Roadmap 0190, closes the epic):
  `layout.json`'s per-leaf identity grows `tabs` (ordered file-backed tab
  paths) and `active` (index within that list); `path` stays the active tab's
  file for older builds. Restore rebuilds tab lists tolerantly: pre-tabs
  identities become single-tab panes, missing files are skipped (active index
  maps to survivors), all-gone panes restore as a scratch tab, and one file in
  several tabs/panes restores as one shared document. Scratch-tab text remains
  crash recovery's job. Tests in `internal/app/tabpersist_test.go`; wiki
  updated. **Epic 0190 complete** — tab model (#156), bar (#157), commands
  (#158), mouse (#159), persistence (#160).

- Editor tabs — mouse on the bar (#159, Roadmap 0190): `tabAt`/`tabBarHit`
  (in `internal/app/tabbar.go`) hit-test the rendered bar geometry exactly.
  Left-click focuses/activates the clicked tab (the active segment still
  starts a pane move, preserving the title-row drag handle), middle-click
  closes it with the editor.closeTab guard (reopen ring fed; a single-tab
  pane closes entirely), and the wheel over the bar row cycles tabs instead
  of scrolling. Tests in `internal/app/tabmouse_test.go`; wiki updated.

- Editor tabs — commands & keybindings (#158, Roadmap 0190): new registry
  commands `editor.tab.next`/`prev` (alt+right/left, wrapping),
  `editor.tab.select1…9` (alt+1…9), `editor.tab.moveLeft`/`moveRight`
  (alt+shift+arrows) and `editor.tab.reopenClosed` (alt+shift+t) — handlers in
  `internal/app/tabs.go`, acting on the focused (else most recent) editor
  pane. A 10-entry reopen ring records path + caret of closed tabs (tab and
  pane closes both feed it); reopen skips files deleted since and restores the
  caret. Chords are QWERTZ-safe and distinct from the ctrl+tab pane switcher;
  alt+arrow rows are marked fragile (option-as-meta). "Reopen Closed Tab"
  joins the File menu; palette/cheatsheet entries come via the registry.
  Tests in `internal/app/tabcommands_test.go`; wiki concept doc updated.

## 2026-07-09

- Editor tabs — tab bar rendering (#157, Roadmap 0190): editor panes with ≥ 2
  tabs render a tab bar on the pane's top row, replacing the single-document
  title (zero extra vertical cost; `internal/app/tabbar.go`). Labels show the
  basename with directory disambiguation for duplicates, a dirty ● and stale
  `!` marker; the active tab is highlighted via theme slots (Accent/bold,
  separators in Border). Overflow elides around the active tab with `…` at the
  hidden end — never wraps. New config key `editor.tabs.always_show` (default
  false, `[editor.tabs]`, settings-page toggle) forces the bar for single-tab
  panes. Tests in `internal/app/tabbar_test.go`; wiki concept doc updated.

- Editor tabs — tab model (#156, Roadmap 0190): each editor pane
  (`pane.Instance`) now hosts an **ordered tab list** (`[]*editor.Model`) with
  one active index; `Editor()` stays the active tab so the pane surface is
  unchanged. New ops `AddTab`/`ActivateTab`/`MoveTab`/`CloseTab` (+
  `TabForPath`, `Editors`, `UpdateForPath`, `UpdateTab`). `openPath` routes all
  open seams into the focused pane's tab list (`openInTab`): activate an
  existing tab, fill a scratch tab in place, else append a tab (autosaving the
  document being left, #174); open-in-new-pane keeps splitting. Shared
  documents (#142) span tabs: `loadOrShare`/sync/highlight/LSP routing reach
  background tabs. `editor.closeTab` (cmd+w, `:q`) closes the active tab and
  the pane only on its last tab; backup snapshots (#165), save-all, external
  delete/move, conflicts and replace-in-buffer are all tab-aware. New concept
  doc [Editor Tabs](/architecture/editor-tabs.md); tests in
  `internal/pane/tabs_test.go` and `internal/app/tabs_test.go`.

- Backup config & GC (#167, Roadmap 0210): new `[backup]` config section —
  `enable` (default true; `false` fully disables the subsystem and **purges**
  existing snapshots, at startup and on live reload), `debounce_ms` (default
  2000, clamped ≥ 100), `max_age_days` (default 7, clamped ≥ 1) — plus the
  write-side wiring (`internal/app/backup.go`): the `editor.SyncMsg` change
  seam marks dirty buffers on the `Debouncer`, one armed `tea.Tick` snapshots
  the quiet ones off the Update loop, and save / close-with-discard / clean
  quit remove their snapshots. Age-based GC (`Service.Prune`) runs at startup
  only after the restore prompt closes. New settings **Backup** page
  (`backup.enable` / `backup.debounce_ms` / `backup.max_age_days`). Tests
  across `internal/backup`, `internal/config`, `internal/settings`,
  `internal/app`. Wiki updated (crash-recovery config table + privacy note,
  config schema/clamps, settings pages).

- Restore flow (#166, Roadmap 0210): crash recovery reads leftover snapshots at
  launch (`internal/app/recovery.go`). `scanRecovery` runs in the constructor;
  once the window is sized, `maybeOpenRecovery` shows a floating prompt (reusing
  the save-conflict UX) listing every recoverable file with a per-file
  base-changed warning. `r` restores the recovered text as a dirty buffer (new
  `editor.RestoreText`: onto the base file for titled buffers, a fresh untitled
  editor otherwise), `d` discards, `s` skips, `esc` skips all. Base-change
  detection compares the on-disk hash/mtime against the snapshot header. Crash-
  simulated tests (`recovery_test.go`). Wiki updated.

- Backup service (#165, Roadmap 0210): new `internal/backup` subsystem for
  crash recovery — `Service` writes/reads/removes one full-text snapshot per
  dirty buffer (`<sha256(key)>.ikebak`: magic + header with key/path/base
  mtime+hash/timestamp, blank line, verbatim text) with **atomic** temp→fsync→
  rename writes to `<state dir>/backups`; untitled buffers are marked "no base
  file". `Debouncer` (injectable clock) collapses edit bursts into one pending
  snapshot ~2s after the last edit, so clean buffers cause zero writes.
  `BaseInfo` stats+hashes the on-disk base for change detection. Fully unit-
  tested (fake clock + temp dirs). App event-loop wiring + restore UI land with
  #166/#167. New concept doc `architecture/crash-recovery.md`.

- Substitute confirm mode (#163, Roadmap 0200): the `c` flag
  (`:s/pat/repl/gc`) drives an interactive match-by-match walk in a mode-machine
  sub-state (`internal/editor/substitute_confirm.go`) — `y`/`n`/`a`/`q`/`l`
  (+ `Esc`), the current match highlighted, the prompt on the command-line row.
  Accepted replacements share one open recorder (a single undo unit; cancel keeps
  what was applied), and a per-line rune-delta keeps multiple matches on a line
  aligned as lengths change. Completes epic 0200.
- Range companions (#164, Roadmap 0200): `internal/editor/excmd_ops.go` adds
  `:[range]d [reg]` (delete into register), `:[range]y [reg]` (yank; cursor
  stays), and `:[range]>` / `:[range]<` (indent/outdent, `:>>` repeats) over the
  shared #161 resolver, reusing the operator/register/indent logic. Each is one
  undo unit with vim-matching cursor behavior (verified against vim).
- `:substitute` core (#162, Roadmap 0200): `internal/editor/substitute.go`
  implements `:[range]s/pat/repl/[flags]` on top of the #161 parser/resolver —
  flags `g`/`i`/`I`/`n`, any delimiter (`:s#a#b#`), pattern via the search-regex
  convention (`\v`, empty pattern reuses the last search), vim-style capture-group
  replacements (`&`, `\0`-`\9`). All replacements form a single undo unit, the
  cursor lands on the last changed line, and the result is reported as *N
  substitutions on M lines* (or a clear error for unknown flags / pattern not
  found).
- Ex parser & range resolver (#161, Roadmap 0200): `internal/editor/excmd` now
  parses the `:` line into a typed `Command{Range, Name, Bang, Args}` AST with a
  full address grammar — `%`, line numbers, `.`, `$`, `'<` / `'>`, `/pat/` /
  `?pat?`, and signed offsets — and a single `Range.Resolve` shared by all
  range-taking commands. Existing `:w :q :wq :e` and bare line jumps keep
  working; `:g` / `:v` / `:s` are reserved with a *not implemented* message.
  Entering `:` from Visual pre-fills `'<,'>`. Ex-command errors/reports now show
  on a transient command-line message row.
- F6 move / Shift+F6 rename (#175): `file.move` (f6) moves the explorer
  selection or the focused editor's file into a folder picked from a new
  palette directory mode; `file.rename` (shift+f6) renames it (explorer inline
  prompt, or a shell prompt for the focused editor). Renames/moves now emit
  `FileMovedMsg` and open editors **follow the new path** (buffer, cursor,
  undo history intact) instead of being closed; undo/redo of the operation
  re-points them back. shift+f6 was reclaimed from the blocked LSP
  rename-symbol placeholder (#6 needs a new chord when it lands).

- Auto-save on focus switch (#174): `editor.auto_save = focus` (default; `off`
  disables) saves a dirty buffer when focus leaves its pane or its document is
  replaced by opening another file. Saves ride the normal path (watcher epoch,
  LSP didSave, shared-view sync); undo history is untouched, and undo/redo now
  re-dirty the buffer so post-save undos persist on the next blur. Stale
  buffers are skipped (conflict guard unchanged). Settings entry under Editor.

## 2026-07-08

- Replace in path (#86, Roadmap 0150): `project.replaceInPath` (cmd+shift+r)
  adds a replacement input + before/after preview to the finder; enter/alt+f/
  alt+a apply per match/file/all. Dirty buffers are edited in place (one undo
  unit per file), other files rewritten on disk (clean open buffers reload
  via the 0140 watcher); stale lines are skipped and counted; `$1` capture
  groups expand. Epic #73 (Find in Path) is complete.

- Find-in-path UI (#85, Roadmap 0150): `project.findInPath` (cmd+shift+f) is
  live — modal overlay with query, case/word/regex toggles, include/exclude
  globs, query history, and live-streamed results grouped by file (the new
  reusable `internal/locations` list). Enter jumps to the match;
  `search.nextMatch`/`prevMatch` step retained results with the overlay
  closed. Blocked-ledger entry for the binding removed.

- Project-search scanner engine (#84, Roadmap 0150): `internal/search` streams
  matches in batches with generation-based cancellation and a truncation
  bound; `rg --json` backend (with `--no-require-git`) and a pure-Go
  walker+matcher fallback with a small gitignore matcher, guarded by a
  backend-parity test. UI lands with #85.

- Shared documents (#142): the same file open in several panes is one document
  with multiple views — shared buffer and undo stack, per-pane cursor/scroll;
  unsaved edits, dirty/stale flags, saves and reloads mirror live across the
  views via an emitter-driven SyncMsg broadcast. Async per-path messages
  (highlight, LSP, watch) now route to all owning panes.

- Explorer auto-refresh on watcher events (#83, Roadmap 0140): directory
  events re-scan just the affected subtree, preserving expansion state and
  cursor; externally deleted files close their editor pane (dirty buffers
  survive, marked stale); manual `r` and the mtime poll stay as fallbacks.
  This completes epic #72.

- Stale marking + save conflict guard (#82, Roadmap 0140): an external change
  to a dirty buffer marks it stale (tab `!`, status `[disk changed]`) instead
  of reloading; saving a stale buffer opens a floating prompt — keep mine /
  reload (discard edits) / cancel. Keep-mine stamps the watcher's save epoch;
  reload reuses the clean-reload path.

- Clean-buffer auto-reload (#81, Roadmap 0140): a non-dirty editor buffer whose
  file changed on disk reloads in place, preserving cursor and scroll (clamped
  like session restore); undo history restarts; highlighting and LSP re-sync
  via the ordinary change event. Config `files.auto_reload = clean|never`
  (default `clean`). Dirty buffers stay untouched until #82.

- Menu bar polish (#137): the open dropdown gets a rounded border, mouse
  hover selects entries (disabled ones skipped), and a title's first letter
  jumps to that menu while open — the bar underlines each first letter as
  the hint.

- Help overlay polish: the cheat sheet is now scoped to the focused pane
  (global commands plus the focused context's group; `Snapshot` takes a
  context id), shortcuts are right-aligned to the column edge, columns carry a
  fixed slack beyond their widest cell so the pane is wider than its text, and
  the floating shell's title row is underlined with a blank spacer row beneath
  it (budget reserves `titleRows = 2`).

- Settings mouse control (#127): category clicks switch pages, form-entry
  clicks select, a second click activates (enter semantics — bool toggles,
  enum cycles, text opens the inline edit).

- Slow-update diagnostics (#125): Update passes over 200ms log message type +
  duration to `.ike/debug.log`, so UI stalls (like the #123 restart deadlock)
  are attributable after the fact. Fixed #123 itself: `lsp.restart` now runs
  Shutdown asynchronously and returns its status message instead of calling
  `host.Send` from the Update goroutine (which deadlocks bubbletea's
  unbuffered message channel).

- Click-outside dismiss (#116): a mouse press outside an open floating
  overlay — settings panel, floating shell (help/modals/history), command
  palette (centered and anchored) — closes it; clicks inside never leak to
  the panes below. The menu dropdown already behaved this way.

- Settings panel floats (#115): the settings window renders as a centered
  rounded-border box (capped ~110×32) above the workspace instead of covering
  the whole terminal; overlong form rows clip instead of wrapping the frame.

- Split keybindings (#114, #121): `pane.splitDown` / `pane.splitUp` /
  `pane.splitRight` / `pane.splitLeft` (cmd+k + arrow) split the focused leaf
  with a fresh empty editor and move focus onto it — no drag or file open
  needed (wires the existing `Model.SplitFocused`).

- Toolchain settings page (#94, closes epic #87 / roadmap 0160): per-language
  interpreter rows with source badge and async version probe; discovery
  pickers (Python: venv/.venv/uv/pyenv/PATH; PHP: PATH + install locations) +
  custom path; choices land in the project config (`[lang.<id>] interpreter`)
  and trigger an LSP restart. New `lang.Interpreter` resolution (explicit
  beats detection) with `InterpreterDetector`/`ExplicitSettings` toolchain
  extensions — the single source of truth 0170's terminal shims will reuse.

- Keymap editor page (#93, roadmap 0160): settings pages can now be custom
  models (`settings.PageModel`); the Keymap page lists the effective binding
  table (layer badges, blocked-with-reason, fragile ⚠, type-to-filter) and
  rebinds by chord capture — conflict confirmation, cmd-chord honesty warning,
  `u` unbind, `r` reset-to-preset — all as `keymap.bindings.*` overrides via
  write-back; the app rebuilds its resolver on config reload so edits apply
  live.

- Core settings pages (#92, roadmap 0160): Editor (all live `applyConfig`
  keys), Appearance (theme enum from the registry with immediate preview,
  menu bar, palette chord), Files & Session (restore-last, files.watch) and
  Notifications. No-dead-keys test guards every entry against the typed
  schema.

- Settings panel framework (#91, roadmap 0160): new `internal/settings` —
  full-window panel (cmd+, / `settings.open`), left category list, right
  schema-driven form (bool/int/string/enum/path/chord). Apply-on-change
  through the write-back layer + live reload; per-entry layer badge
  (`config.Origin`) and `r` reset-to-default; `/` filter across all pages;
  plugin-contributable pages via `Capabilities.SettingsPages`.

- Menu bar (#90, roadmap 0160): new `internal/menu` — File · Edit · View ·
  Navigate · Tools · Settings · Help above the panes (`ui.menu_bar`, default
  true). Menus are data over the command registry: entries show cheatsheet
  shortcuts, unregistered ids render disabled with their dependency hint;
  selection dispatches `menu.RunMsg` → `RunCommand`. f10 toggles; arrows/
  enter/esc navigate; mouse clicks open/run. New concept doc
  `architecture/settings-ui.md`.

## 2026-07-07

- Config write-back layer (#89, roadmap 0160): `config.WriteKey`/`RemoveKey`
  persist one dotted key to the user or project settings file via a TOML
  round-trip (unknown keys survive; broken files are refused, never
  destroyed); `DefaultScope` routes keys to their conventional layer;
  `WriteAndReload`/`RemoveAndReload` chain into the existing reload path so
  changes apply live. Foundation for the 0160 settings UI.

- File-watcher service (#80, roadmap 0140): new `internal/watch` — fsnotify on
  the project root (recursive, `.git` skipped), ~100ms debounce with per-path
  coalescing, `watch.EventMsg` routed to the owning editor / explorer.
  Self-event suppression via a save epoch (new editor `EventSave` → emitter
  adapter → `MarkSaved`); mtime+size poll fallback with hash-on-suspicion for
  tracked (open) files; config `files.watch` (default true).

- Block-comment toggling (#76, closes epic #70 / roadmap 0120):
  `editor.commentBlock` (cmd+shift+7) wraps a charwise selection inline
  (`/* sel */`), a linewise selection or the current line on its own marker
  lines; exactly-wrapped targets unwrap; python falls back to line comments.
  One undo unit, dot-repeatable. Blocked-ledger entry removed.

- Line-comment toggling (#75, roadmap 0120): `editor.commentLine` (cmd+7,
  cmd+k cmd+c) toggles the registry language's line marker on the current line
  or visual selection at the minimal indent — mixed ranges comment the
  uncommented lines, fully commented ranges uncomment, blank lines skipped.
  Single-line toggle advances the cursor (JetBrains); selections survive the
  toggle. One undo unit, dot-repeatable; buffers without comment syntax raise
  an info toast (`editor.NoticeMsg`). Blocked-ledger entry removed.

- Notification history (#78, closes epic #71 / roadmap 0130): ring of the
  newest 100 notifications with timestamp + severity; `notifications.history`
  palette command lists them in the floating shell. New typed config section
  `[notifications]` (`timeout_seconds`, `min_severity` — below the floor is
  history-only, no toast), live-reloaded; the host's config view now refreshes
  on config reload (`Host.SetConfig`).

- Event-like `SetStatus` call sites migrated to `Notify` (#79, closes roadmap
  0130's migration milestone): example plugin messages, save-all ("saved N
  files"), theme select/warnings and transient LSP events (crashed → warn,
  restarted → info, launch failure / disabled after repeated crashes → error)
  are toasts now; `lsp.ServerStatusMsg` carries a `ServerStatusKind` assigned in
  the manager. Persistent LSP server state stays on the status line, which
  renders `SetStatus` as an extra segment instead of replacing the whole line.

- Command line moved into the editor pane (#99): the ":" / "/" / "?" input
  renders as the pane's bottom row (vim-style) instead of replacing the app
  status line. Status line shows the focused file's project-relative path
  (absolute outside the project) instead of just the base name (#100).

- Notification toasts (#77, roadmap 0130): `host.Notify(severity, text)` queues
  event messages the root model drains each Update pass and renders as
  severity-colored toasts bottom-right above the status line — info/warn expire
  (`notifications.timeout_seconds`, default 4s), errors persist until Esc
  (pass-through). New concept doc `architecture/notifications.md`.

- Language registry comment metadata (#74, roadmap 0120): `lang.Language` grows
  `LineComment`/`BlockComment`, `lang.Comments(path)` resolves the syntax per
  buffer path; go/php declare `//` + `/* */`, python `#`. Consumed by the
  upcoming comment-toggle actions (#75/#76).

- Command coverage & id reconciliation, no inert bindings (#11): new blocked
  ledger (`keymap/blocked.go`) documents every intentionally-unregistered
  default binding with its unblocking dependency; thin commands registered for
  `editor.find`, `editor.duplicateLine`, `editor.saveAll` and
  `explorer.toggle`; `cmd+b` reconciled onto the registered `lsp.definition`
  id; coverage test `TestNoSilentlyDeadDefaultBindings` guards the invariant.

- Conventional selection & clipboard (#47): word navigation moved from
  `shift+←/→` to `alt/option+←/→` (paragraph jumps to `alt+↑/↓`; ctrl variants
  stay); `shift+arrows` now start/extend a charwise visual selection;
  `editor.copy/cut/paste` registered (cmd+c/x/v live, acting on the selection
  or current line through the system clipboard — new `internal/clipboard`
  package wired into `register.SetClipboard`); `cmd+left`/`cmd+right` bound to
  new `editor.lineStart`/`editor.lineEnd` commands.

- Cheatsheet, pane switcher, go-to-file as registered commands (#44, #45, #46):
  `palette.keymapHelp` (f1 / cmd+k cmd+s) opens the help overlay via the new
  `openHelp` helper (hardcoded `?`/`f1` kept as fallback), `pane.switcher`
  (ctrl+tab, fragile) cycles focus like the hardcoded tab, and
  `project.goToFile` (cmd+shift+o) opens the centered palette locked to the `@`
  file mode via the new `palette.OpenLocked`.
- Cmd+W closes the focused tab (#43): new compile-in `app` plugin
  (`internal/app/commands.go`) exposes root-model actions as registry commands;
  `editor.closeTab` dispatches `CloseTabMsg` → `CloseFocused`, so the default
  `cmd+w` binding is live and the action is palette-invokable.

- Ctrl/Cmd+S saves the file (#42): the default `cmd+s` binding now targets
  `editor.write` (the registered `:w` command) instead of the never-registered
  `editor.save`, and a `ctrl+s` fallback chord was added because macOS terminals
  never forward `Cmd` — mirroring the undo/redo pattern. Works from insert mode
  (modified chords stay keymap-eligible). `cmd+shift+s`/`editor.saveAll` stays
  inert until a save-all command exists (#11/#19).

- Planning moved from the `roadmaps/` directory to GitHub issues on
  `TrueDaerk/ike`: specs live verbatim in epic issues #37–#41 (0090 Project
  Switching, 0100 LSP deferred, 0081 Keybinding Audit, 0082 Usability Review,
  9900 WASM Plugins), work items are sub-issues tracked via one milestone per
  epic. `roadmaps/` was deleted (history remains in git); wiki links to roadmap
  files were rewritten as "former Roadmap NNNN" notes.
- Theme switching from the command palette: the `themes` plugin now registers
  one global command per built-in scheme (`themes.select.<name>`, "Theme:
  <name>"), dispatching `app.SelectThemeMsg` → `selectTheme` (resolve over
  built-ins + plugin themes, re-thread via `applyTheme`, status confirmation).
  The runtime pick persists in the session store (`session.json` `theme` field,
  `Model.themeOverride`) and is re-applied on restore, overriding the
  config-derived theme; only explicit picks are recorded, so `settings.toml`
  stays untouched and config edits keep working until a runtime pick overrides.
  `settings.toml` write-back remains with 0040/0090.

- Roadmap 0110 (Themes / Color Schemes) implemented: new leaf package
  `internal/theme` (semantic `UI` slots + `Captures` + `Files` per Textual/
  sqlit model, `Palette` with resolved colors, single `theme.Resolve` token
  resolver, `theme.Select` lookup with fallback-to-default). Built-ins:
  `default` (pixel-identical to the old hard-coded colors), `tokyo-night`,
  `nord`, `gruvbox`(+light), `rose-pine`(+dawn), `catppuccin-mocha`/`-latte`,
  shipped via the compile-in `themes` plugin (`Capabilities.Themes`, merged by
  `registry.Themes()`). `[theme].name` now selects the palette;
  `highlight.NewTheme` and the explorer color table take their defaults from
  it (per-key `theme.captures.*` / `[explorer.colors]` still win). Every hex
  chrome literal in `app`, `editor`, `explorer`, `ui`, `palette`, and `help`
  was replaced by a palette slot; the screen background/foreground are set at
  the renderer level from the palette. Live re-theme: the app now consumes
  `config.ConfigReloadedMsg` (re-resolve palette, re-thread, reconfigure
  panes). Updated the Themes concept doc from planned to implemented.

## 2026-07-06

- Explorer UX pass (four features). **Open-file marking:** every file open in
  any editor pane renders its name underlined + italic (`Model.SetOpen`, kept
  current by `app.syncExplorerOpen` on open/close/restore; a stale `active`
  mark is cleared; `rowParts` keeps guides/padding undecorated). The active
  accent is muted (no bold) and tracks the **focused** editor's file
  (`app.setFocus` → `SetActive`), so switching panes moves the highlight. **Double-click to open:** a single mouse press now only selects a
  row; opening a file / toggling a directory takes a double-click (400ms
  window, injectable clock) — except a single click on a directory's expand
  caret, which still toggles. **Auto-refresh:** directory mtimes are polled
  every 2s off-thread (`schedulePoll`/`pollMsg`); only changed directories are
  re-scanned, and `setChildren` now merges by path so any re-scan (auto or
  manual `r`, which also became a deep re-scan of expanded children) preserves
  expansion state. Disable with `explorer.auto_refresh = "false"`.
  **Undo/redo:** file operations gained a redo stack (`explorer.redo`,
  `Ctrl+Shift+Z`/`Cmd+Shift+Z`) and rename is now undoable; undo of a create
  moves the entry to `.ike-trash/` instead of removing it, so undo/redo apply
  instantly without confirmation prompts. `editor.redo` additionally binds
  `ctrl+shift+z`, which — unlike `cmd+shift+z` — macOS terminals can deliver.

- Editor: mouse wheel now scrolls the viewport (`editor.ScrollBy`, wired in
  `app.handleMouse`'s `mouseWheel` case for `pane.KindEditor`), independent of
  vim mode — it moves `view.Top` directly instead of the cursor, so it works
  the same in Normal/Insert/Visual/etc. Previously only the explorer pane
  handled the wheel.
- Roadmap 0050 (File Explorer) file-operations milestone completed: added
  `explorer.rename` (prompt prefilled with the current name, `R` default key) to
  the existing create/delete/undo set. Rename is not on the undo stack (rename
  back to undo); it reuses `FileDeletedMsg` for the old path so the app closes any
  editor open on it, since the editor can't follow a path change in place. Along
  the way fixed a latent test-helper bug: `pumpScans` didn't unwrap `tea.BatchMsg`
  (used by delete/rename's `tea.Batch(refreshDir, deletedCmd)`), so it silently
  skipped the rescan — invisible before because no test asserted post-delete
  row/cursor state. Roadmap 0050 is now fully checked off.
- Roadmap 0110 (Themes) reworked to match landed reality. Syntax highlight (0100)
  and explorer file colors (0050) already ship config-driven color models with
  duplicated resolvers, and `[theme].name` is inert. 0110 now: activate
  `[theme].name` so a **named palette** recolors syntax + explorer + chrome at
  once; new leaf `internal/theme` holds built-in palettes + one shared color
  resolver (collapsing the `highlight`/`explorer` copies) and feeds the
  **defaults** of the existing `highlight.Theme` / explorer `colorTable` (per-key
  config still overrides); chrome hex literals move onto ui slots. Naming caution
  recorded: color pkg is `internal/theme`, not `internal/palette` (that's 0070's
  command palette). Also captured the **background-bleed bug**: `app.render`
  paints `appBackground` once around the whole screen (`app.go:1512`), so pane
  interiors, the floating shell, the palette, and LSP popups still show the raw
  terminal background (lipgloss won't repaint occupied cells). 0110 mandates
  painting backgrounds **per surface** (pane bodies fill `surface` + pad to full
  size; overlays paint an opaque surface before compositing). Updated
  `roadmaps/0110-themes.md` + `architecture/themes.md`.

## 2026-07-02

- **Extensible language system (Roadmap 0105).** The hardcoded three-language set
  became a plugin registry. New leaf package `internal/lang` is the single source
  of truth for a language: `Language{ID, Extensions, Filenames, Grammar, Server,
  Toolchain}`, registered from a plugin's `init()` like `registry.Register`. The
  highlight engine (`internal/highlight`) no longer knows any language — it exposes
  `NewGrammar(tsLang, query)` (cgo) and resolves grammars via `lang.ByPath`; the
  Go/PHP/Python grammars + `highlights.scm` queries moved into
  `plugins/languages/{go,php,python}` (grammar behind a cgo build tag, nil stub for
  `CGO_ENABLED=0`). LSP server baselines now come from each language's
  `Language.Server`; `[lsp.servers.<id>]` config only *overlays* them
  (`resolveSpec` merge; `applyDefaults` no longer hardcodes servers). New
  `lang.Toolchain` seam: `manager.ensureServer` runs the language's detector against
  the workspace root and merges the result into server settings, and the manager now
  answers `workspace/configuration` from those settings — so the Python detector's
  resolved interpreter (`$VIRTUAL_ENV` → `.venv` → `.python-version` → PATH) reaches
  pyright as `python.defaultInterpreterPath`, giving version-aware diagnostics
  without IKE reimplementing any version logic. Tree-sitter highlighting stays
  version-agnostic. Adding a language = new `plugins/languages/<lang>/` package + a
  blank import in `cmd/ike/main.go`. See [Language Registry](/architecture/languages.md).

## 2026-06-28

- **LSP + syntax highlighting (Roadmap 0100, MVP slice).** IKE gained language
  intelligence for **Go / PHP / Python**. A pure-Go JSON-RPC 2.0 client
  (`internal/lsp/{jsonrpc,transport,protocol,client,manager}`) speaks LSP over a
  server's stdio; a `manager` maps each `(language, workspace root)` to one server,
  spawns lazily, routes ops, and recovers from crashes (backoff respawn + re-open
  tracked docs). Editor edits flow out through the existing `Emitter` seam — now
  forwarded via a new `host.EditorEmitter` + `host.Send` (async injection wired
  from `main.go`'s `program.Send`) — and the `plugins/lsp` compile-in plugin drives
  `didOpen`/`didChange`/`didSave`/`didClose`. Results return as `tea.Msg`s
  (`DiagnosticsMsg`/`CompletionMsg`/`HoverMsg`/`DefinitionMsg`/`ServerStatusMsg`)
  routed by path to the owning editor leaf: diagnostics colour the gutter + underline
  inline + count in the status line; completion shows a cursor-anchored, prefix-
  filtered popup; hover shows a popup; go-to-definition navigates. `lsp.hover` /
  `lsp.definition` / `lsp.restart` are registry commands. Server defaults
  (gopls/intelephense/pyright) ship via `config/extend.go`; a missing binary is a
  graceful no-op. Separately, **Tree-sitter syntax highlighting** (`internal/highlight`)
  parses Go/PHP/Python off the event loop into theme-coloured spans applied per cell
  in `renderLine`; it is CGo, isolated behind a build tag with a no-op stub so
  `CGO_ENABLED=0` still builds. Deferred to a later increment: references, rename,
  formatting, code actions, signature help, and the LSP semantic-token overlay. See
  [LSP](/architecture/lsp.md) and [Syntax Highlighting](/architecture/highlighting.md).

## 2026-06-25

- **Upgraded to Bubble Tea v2 (Roadmap 0085).** The whole charm stack moved to
  `charm.land/bubbletea/v2 v2.0.7`, `charm.land/lipgloss/v2 v2.0.4`, and
  `charm.land/bubbles/v2 v2.1.0`. The driver is the **kitty keyboard protocol**:
  keyboard enhancements are now requested on the root model's `tea.View`
  (`ReportEventTypes`), unlocking disambiguated chords (ctrl+i vs tab, shift+enter).
  Key handling moved from `key.Type`/`key.Runes` to `key.Code`/`key.Text`/`key.Mod`
  (the in-house keymap still funnels everything through `fromkeymsg.go`'s `String()`);
  the single `tea.MouseMsg` split into four messages normalised into one `mouseEvent`;
  `Model.View()` now returns a `tea.View` (alt-screen/mouse declared there, not via
  program options); and lipgloss v2 is "pure" so rendered-output tests `ansi.Strip`
  first. See [Foundation](/architecture/foundation.md) and
  [Keybindings](/architecture/keybindings.md).

## 2026-06-24

- **Editor undo fixed for insert mode.** `editor.undo`/`redo` now flush an open
  insert session before walking history, so `Ctrl+Z` while typing reverts the
  whole typed run as one unit and behaves the same from insert and normal mode
  (previously it ran against history with the in-progress insert still
  uncommitted, so it no-opped or desynced). See
  [Editor](/architecture/editor.md).

- **Explorer file operations (create / delete / undo).** New `fileops.go` adds
  `explorer.newFile` (`a`), `explorer.newFolder` (`A`), `explorer.delete` (`d`),
  and `explorer.undo` (`Ctrl+Z`). Every destructive step is gated behind a
  modal prompt; deletes move entries to a same-filesystem `.ike-trash/` so undo
  can restore them, and a linear op stack reverses the last create (delete it) or
  delete (restore it). The root model routes keys straight to a prompting
  explorer so typed names/answers are not stolen by other bindings. Removing a
  file (delete or undo-create) emits `FileDeletedMsg`, which the app handles by
  closing any editor still open on that path. See
  [File Explorer](/architecture/explorer.md).

- **Keybindings layer (Roadmap 0080).** New `internal/keymap` package: a
  chord/key model (`Key` + `Mod` bitset, multi-step `Chord`), canonical
  parse/format, the JetBrains-flavoured default set as data, context-scoped
  resolution (pane-scoped shadows Global), build-time conflict detection,
  platform normalisation (Cmd→Ctrl off macOS), a `tea.KeyMsg` adapter, a
  partial-chord resolver with 600ms timeout, and a cheatsheet view. Wired into
  `internal/app` dispatch: IDE-level chords resolve to registered command ids
  before pane routing (only modified chords in a capturing editor); inert/unbound
  chords fall through. Bindings reference command ids owned elsewhere and define
  no commands; `vcs.*` ids stay inert pending a future VCS roadmap. See
  [Keybindings & Shortcuts](/architecture/keybindings.md).

- **Pane focus: directional, geometry-aware.** `FocusDir` (Ctrl+arrow) now routes
  through a pure `focusTarget` scorer over the computed leaf rectangles. It ranks
  candidates in the travel direction by perpendicular-span overlap, then travel-
  axis distance, then perpendicular alignment — so focus-right lands on the pane
  beside you, not a wide full-width pane below whose centre happened to be closer
  by raw Manhattan distance.

## 2026-06-21

- **Editor: expand tabs when rendering.** `renderLine` now budgets by display
  cells and expands each tab to `tab_width` spaces. Previously it emitted raw
  tabs counted as one rune each; the terminal expanded them past the line's width
  budget, wrapping the line and pushing a split editor pane's bottom border off
  screen. Fixes the "split pane has no bottom border" bug on tab-indented files.

- **Command palette refinements (Roadmap 0070).** Box is now compact (half-width
  centered / pane-width anchored, each with a floor). Key bindings render as a
  highlighted chip pinned right of each row (title truncates first). Two new entry
  points: **esc-esc** opens the centered palette from a non-capturing context, and
  **`@` in an editor's normal mode** opens a slimmed, file-only palette *anchored*
  over the editor pane (`OpenAnchored` + `overlay.Place`, locked to `@` so no mode
  switching).

- **Command palette (Roadmap 0070).** New `internal/palette` overlay fronts every
  action: a leading prefix rune selects a `Mode` — `:` runs registry commands
  (snapshot per open, ranked context-first/global/off-context), `@` fuzzy-finds
  files by relative path (directory segments included). New `internal/fuzzy`
  matcher returns an optimal-alignment score + matched rune spans shared by
  ranking and highlighting. The palette is its own modal tea-model (the read-only
  floating shell can't take typed input); it dispatches `RunCommandMsg` /
  `OpenFileMsg` and executes nothing. Root model hosts it, toggles on `ctrl+p`
  (config `palette.toggle_key`), forwards keys, composites it centered. New
  `[palette]` config section (`max_results`, `default_mode`, `off_context`,
  `toggle_key`). The `plugin.Command.Scope` field it ranks by was already present.
  New concept doc [Command Palette](/architecture/command-palette.md).

- **Pane splitting & multiple editors (Roadmap 0037).** The fixed two-component
  root becomes a dynamic pane set. New `internal/pane` registry maps each layout
  leaf to a live instance (explorer singleton + N editors); focus is now the
  focused leaf. `internal/layout` gains `SplitLeaf`/`Close` (the create/close
  half, reusing `insert`/`remove`) and `DecodeTree`/`Leaves`. Binding-agnostic
  ops `SplitFocused`/`CloseFocused`/`FocusDir` + tab focus-cycle; mouse self-edge
  drag spawns a split. Open-in-new-pane rides an additive `NewPane` flag on
  `explorer.OpenFileMsg` / `host.OpenFileRequest` (+ `host.API.OpenFileIn`),
  defaulting to Replace. The layout store grows a per-leaf identity table
  (`{kind, path}`) so editors restore their files best-effort (missing file →
  empty editor); old bare-tree files still load. New concept doc
  [Pane Registry & Multiple Editors](/architecture/pane-registry.md); Pane Layout
  doc updated.

- **Pane-split rendering fixes.** `paneBox` now hard-clamps to its rect
  (`MaxWidth`/`MaxHeight` + title truncation) so a narrow split column can no
  longer wrap its title and overflow by a row — the overflow had pushed the whole
  tiling up (cut-off pane titles) and desynced mouse hit-testing from `m.lay`.
  Open-in-new-pane now splits the **active editor's** leaf rather than the focused
  explorer, so a file opened from the explorer lands in the editor area instead of
  shrinking the explorer.

- **Pane focus/close keybinds.** `Ctrl+W` closes the focused editor pane
  (`CloseFocused`; no-op on the explorer / last leaf). Spatial focus moves
  (`FocusDir`) get default **Ctrl+arrow** bindings, overridable via
  `keymap.bindings.focus_{left,right,up,down}`. Cmd is intentionally avoided —
  terminals don't deliver it to a TUI. Both are core keys; Roadmap 0080 owns the
  final keymap.

## 2026-06-20

- `F1` now opens the help overlay as an alias for `?`, and dismisses it as well
  (added to the floating shell's dismiss key set).

- Help overlay is now a **full reference**: it snapshots every registered command
  (`registry.Commands()`) regardless of focus, so the Editor section shows
  alongside Global and Explorer. Added a documentation-only `plugin.Command.Shortcut`
  hint — help falls back to it when no keymap resolves — so the editor's vim
  ex-commands (`:w`/`:q`/`:wq`) and modal keys (`u`/`ctrl+r`) display their
  shortcuts. Scope groups are now separated by a blank line for readability.

- Fixed explorer mouse-click desync after restoring a session with expanded
  directories: `clampScroll` now also clamps `offset` to `len(rows)-textH`.
  Restore runs at height 0 and parked an offset past the last page; `View`
  clamped it for display but `MouseClick`/hover read the raw offset, so clicks
  landed on rows far below the ones shown until the user scrolled.

- Session restore now also persists the editor's **viewport framing** (scroll
  `top`/`left`), not just the cursor. `Top` is sticky during editing, so cursor-
  only restore reframed the file and made mouse clicks land on the wrong lines.
  Saved offset is applied after the editor is first sized (`Model.pendingScroll`
  → `editor.SetScroll`). New `editor.ScrollOffset`/`SetScroll`.

- Added **session restore**: a per-project `session.json` (beside `layout.json`,
  same `IKE_CONFIG_DIR`/`.ike` discovery) saves the open file + cursor and the
  explorer's expanded dirs + show-hidden + cursor on quit, reapplied on launch.
  Explorer restore loads directories synchronously and `Init` skips its async
  scan once the root is restored. New `internal/app/session.go`,
  `internal/explorer/state.go`, `editor.SetCursor`/`CursorPos`,
  `app.quit()`/`restoreSession`. See `/architecture/session-restore.md`.

- `q` now quits the app from the editor too, when it is focused in normal mode
  (previously only from the explorer). Insert/command mode still routes `q` to
  the buffer. See `app.quitKey`/`app.isCoreKey`.

- Editor follow-ups: the visual selection is now rendered (per-cell highlight in
  `view.go`, cursor wins on overlap) and visual mode gained `i`/`a` text-object
  selection, `>`/`<` indent, and register-replace `p`. Added word navigation on
  `Shift+←/→` (+ `Ctrl+←/→`), paragraph jumps on `Shift+↑/↓`, page scrolling via
  `PgUp`/`PgDn` + `Ctrl-f/b/d/u`, screen jumps `H M L`, plus `~` toggle-case and
  `*`/`#` word search. Arrow/Home/End and the new motions also work mid-insert.
  Mouse click focuses the editor and positions the cursor.

- Editor (Roadmap 0060): the foundation's minimal modal editor is rebuilt into a
  full vim-like editor across focused sub-packages under `internal/editor/`:
  `buffer` (line slice + rune/byte `Position` + reversible `Apply(Edit)`),
  `mode`, `motion` (`h j k l w b e W B E 0 ^ $ gg G { } f t F T ; , %`),
  `textobject` (`iw aw`, bracket/quote pairs), `operator` (`d c y p gp`, doubled
  `dd cc yy`, `Compose`), `register` (`" a-z 0 - 1-9 +`), `history` (undo/redo +
  `.` repeat), `viewport` (scroll/scrolloff/gutter), `search` (`/ ? n N`, `\v`
  regex), and `excmd` (`:w :q :wq :q! :e`, `:<n>`). The `editor.Model` keeps its
  pane API (so the root is unchanged but for routing `ActionMsg` and using
  `Capturing()`); `commands.go` registers actions/ex-commands as plugin
  `Command`s dispatched via a single `ActionMsg` path; `events.go` is the LSP
  hook seam. `[editor]` config (tab width, expandtab, line numbers, scrolloff…)
  is read live via `Configure`.

- Help: command shortcuts now render. `plugin.Keymap` gained a `CommandID`
  field; `*registry.Registry` implements `help.BindingResolver` via a new
  `Binding(cmdID)` reverse-lookup, and the root wires it (was `nil`). Explorer
  default keymaps link to their command ids, so the cheat sheet shows e.g.
  `Explorer: Toggle Hidden Files  .`. Full keymap layer still owned by 0080.

- Explorer (Roadmap 0050): config-driven per-filetype colours (`colors.go`,
  glob→ext→`dir`/`default` resolution from `[explorer.colors]`), italic hidden
  entries with a `explorer.toggleHidden` runtime toggle (default off via
  `explorer.show_hidden`), indent guides sized by `explorer.tree_indent`, and
  async directory scans (`scanCmd`/`ScanDoneMsg`, no blocking IO in `Update`).
  Added registry commands + default keymaps (`toggleHidden` `.`, `refresh` `r`,
  `collapseAll` `c`, `reveal`) that dispatch explorer `Msg`s the root routes
  back. `host.Config` gained `Keys()` so the explorer can enumerate the dynamic
  `[explorer.colors]` section. Only the optional file-ops milestone remains.

- Explorer: hover highlight (mouse motion), an "open file" highlight distinct
  from cursor/hover (`SetActive`, set on open and cleared on editor close), and
  shift-wheel / horizontal-wheel sideways scrolling (`ScrollXBy`). Row styling is
  now resolved through a testable `rowKind` precedence: cursor > hover > active >
  dir > plain.

- Explorer (Roadmap 0050, partial): mouse navigation and scrollbars. The root
  model forwards in-pane mouse events to the explorer — left-press selects/
  activates a row, wheel scrolls without moving the cursor, scrollbar-track press
  jumps an axis. The explorer gained a horizontal scroll offset and renders
  conditional right/bottom scrollbars (dim track + heavier thumb, sized by
  `scrollThumb`) whenever content overflows the pane; rows are clipped with
  `ansi.Cut` so long names scroll sideways instead of wrapping.

- Roadmap 0040 (Settings / Configuration) implemented: new leaf-level
  `internal/config` package — typed `Config` sections (`schema.go`), in-code
  defaults (`defaults.go`), `~/.ike` + `{root}/.ike` discovery with
  `IKE_CONFIG_DIR` override (`discovery.go`), TOML decode isolated behind the
  package (`load.go`), deep map merge with scalar-replace / table-merge /
  list-replace semantics (`merge.go`), clamp-and-warn validation with non-fatal
  `Diagnostic`s and parse-error layer isolation (`validate.go`), an idempotent
  `Extension` registration hook (`extend.go`), `Load`/`Get`/`Set` accessors plus
  `Config.Flat` (`config.go`), a `ConfigReloadedMsg` reload seam (`watch.go`),
  and a typed setter seam `PushHistory` (`write.go`). `internal/host` now depends
  on `internal/config` via `host.FromConfig` (flat read-only view backing the
  plugin API); `internal/app.New` loads the merged config at startup. Backed by
  `BurntSushi/toml`. Tests cover precedence, table/list merge, clamp-and-warn,
  parse-error isolation, and extend round-trip (config 87% coverage).


- Roadmap 0036 (Pane Drag) implemented: new pure `internal/layout` split-tree
  (`tree.go` types + `Compute`/`Rects` exact tiling, `rect.go` hit-testing +
  drop zones, `resize.go` clamped divider drag, `move.go` drop-zone re-parent,
  `state.go` tolerant encode/decode). `internal/app` replaces hard-coded
  `explorerWidth`/`JoinHorizontal` with tree-driven `Rects`, adds a `tea.MouseMsg`
  drag state machine (press hit-test → resize/move, release commit), and a
  per-project layout store (`store.go`, `IKE_CONFIG_DIR`/`.ike/layout.json`,
  save-on-release, default fallback on stale state). `cmd/ike` enables
  `tea.WithMouseCellMotion`. New concept doc `architecture/pane-layout.md`.

- Roadmap 0110 (Themes) planned: added `roadmaps/0110-themes.md` and a stub
  concept doc `architecture/themes.md`. Semantic-slot theme model mirroring
  sqlit/Textual; built-in palettes (tokyo-night, nord, gruvbox, rose-pine,
  catppuccin); selector behind 0040's `[theme]`, registration via 0020. Stub is
  marked planned — not implemented yet.
- Roadmap 0035 (Floating Shell) implemented: extracted the one-off help overlay
  chrome into a reusable component. New `internal/overlay` (pure ANSI-aware
  `Center` compositing, moved out of `internal/app`) and `internal/ui`
  (`Floating` shell hosting any `ui.Content`; `sizing.go` content budget;
  `scroll.go` generalised scroller wrapping `bubbles/viewport`; `ModelContent`
  adapter to float a view-only model). `internal/help` refactored to a
  `ui.Content` provider (snapshot + column layout only); its local chrome,
  sizing, and scroll deleted. Root model now hosts one active `*ui.Floating`,
  forwards size + keys, and composites via `overlay.Center`. Added an additive
  in-process plugin seam, `host.OpenModalRequest{Title, View}`, so a plugin can
  present its pane as a floating modal; optional `overlay.*` config tuning
  (margin, max width/height fraction). Added the Floating Shell concept doc and
  updated Help Overlay.

## 2026-06-19

- Roadmap 0030 (Help Overlay) implemented: `internal/help` (`source.go` snapshot
  + binding join + scope grouping, `layout.go` responsive column-major packing,
  `viewport.go` vertical scroll wrapping `bubbles/viewport` with a position
  indicator, `help.go` overlay `tea.Model`). Root model hosts the overlay, opens
  it on `?`, forwards size + keys, and renders it on top. Binding resolver
  (roadmap 0080) consumed through a `BindingResolver` interface; not wired yet,
  so commands render title-only. The overlay renders as a content-sized floating
  pane centered over the layout (max two columns), composited via an ANSI-aware
  splice (`x/ansi`) so the base stays visible around it. Added the Help Overlay
  concept doc and the `bubbles` dependency.

- Roadmap 0020 (Plugins: Compile-in Registry) implemented: `internal/plugin`
  (Plugin interface + Command/Keymap/Pane/FileHandler/Hook capability types,
  Scope, ContextProvider), `internal/registry` (Register, conflict detection,
  deterministic ordering, enable/disable, lookups), `internal/host` (host.API +
  in-process impl). Root model now routes file opens through handlers, fires
  lifecycle hooks, resolves layered plugin keymaps, and exposes `RunCommand`.
  Added `plugins/example` reference plugin and the Plugin Extension Contract
  concept doc.

- Explorer reworked into an expandable tree rooted at a fixed project base:
  folders expand/collapse in place (`▾`/`▸`) instead of replacing the listing,
  and the explorer can no longer ascend above the root.
- Roadmap 0010 (Foundation) implemented: file explorer pane, modal vim editor
  pane, root model routing/focus/status line. Added concept docs for the
  foundation slice, explorer, and editor.
