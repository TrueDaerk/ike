# Log

## 2026-07-06

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
