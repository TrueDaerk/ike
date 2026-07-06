# Progress

One box per roadmap. Tick a roadmap once all its milestones are done.

## Build order

- [x] [01 — Foundation: File Explorer + Vim Editor](0010-foundation.md)
- [x] [02 — Plugins: Compile-in Registry](0020-plugins-compile-in.md)
- [x] [03 — Help Overlay (Command & Shortcut Cheat Sheet)](0030-help-overlay.md)
- [x] [03.5 — Floating Shell (Reusable Overlay / Modal Component)](0035-floating-shell.md)
- [x] [03.6 — Pane Drag: Mouse Move, Resize & Layout Persistence](0036-pane-drag-layout.md)
- [x] [03.7 — Pane Splitting, Multiple Editors & Open-in-New-Pane](0037-pane-splitting-multi-editor.md)
- [x] [04 — Settings / Configuration](0040-settings.md)
- [ ] [05 — File Explorer (full)](0050-file-explorer.md)
- [x] [06 — Vim-Like Editor (full)](0060-vim-editor.md)
- [x] [07 — Command Palette](0070-command-palette.md)
- [x] [08 — Keybindings & Shortcuts](0080-keybindings.md)
- [ ] [08.1 — Keybinding Audit & User-Friendly Activation](0081-keybindings-audit/index.md)
- [ ] [08.2 — Per-Keybinding Usability Review](0082-keybinding-usability/index.md)
- [x] [08.5 — Bubble Tea v2 Upgrade (Kitty Keyboard Protocol)](0085-bubbletea-v2.md)
- [ ] [09 — Project Switching](0090-project-switching.md)
- [x] [09.5 — Session Restore (Workspace State Persistence)](0095-session-restore.md)
- [ ] [10 — LSP Support](0100-lsp.md)
- [x] [10.5 — Extensible Language System (Registry + Toolchain)](0105-language-registry.md)
- [ ] [11 — Themes / Color Schemes](0110-themes.md)
- [ ] [99 — Plugins: WASM (Runtime, Sandboxed)](9900-plugins-wasm.md)

## Dependency notes

- **02** defines the extension contract (registry: Command/Keymap/Pane/FileHandler/Hook,
  `host.API`). Everything that adds a command, key binding, or pane goes through it.
  `Command.Scope` is an additive field needed by 07 + 08.
- **04** owns config loading + precedence (defaults < user < project). 05/06/08/10 fill
  their own schema sections (`[explorer]`, `[editor]`, `[keymap]`, `[lsp]`); 09 stores
  project history under `[project]`.
- **03** help overlay is a read-only consumer: it joins **02** registry Commands
  with **08** binding strings and renders them responsively. Opened by `?` /
  `:help` (binding/command owned by 08/07). Owns no command or shortcut data.
- **03.5** floating shell generalises **03**'s one-off overlay into a reusable
  centered-pane component (`internal/overlay` compositing + `internal/ui.Floating`
  shell) hosting any `tea.Model`. **03** is refactored to consume it; modals and
  plugin popups reuse it. Owns no content, only the shell.
- **03.6** pane drag lifts the root's hard-coded tiling into a pure layout tree
  (`internal/layout`) driven by `tea.MouseMsg`: divider drag resizes, title-bar
  drag moves/swaps panes. Geometry/structure persist per project in a dedicated
  layout **state store** (not `settings.toml`). It is the first step of the
  broader pane manager **03.5** deferred; **03.5** overlays still composite above
  the tiling. Reuses **04**'s discovery/write seam for the store when present.
- **03.7** completes the pane manager **03.5/03.6** deferred: the *create/close*
  half. It extends **03.6**'s pure `internal/layout` tree (new `Split`/`Close` ops
  reusing `insert`/`remove`) and its layout state store (richer per-leaf identity:
  kind + file, restored best-effort). It replaces the root's two hard-coded
  component fields with a `internal/pane` registry of N instances and turns focus
  into "the focused leaf"; multiple editors tile side by side. Open-in-new-pane
  rides an additive `OpenTarget` on `explorer.OpenFileMsg` / `host.OpenFileRequest`
  / `host.API` (defaults to today's replace, so **02**'s `FileHandler` contract
  stays compatible). Split/close/focus-move are binding-agnostic ops **08** binds
  later (mirroring **03.6**'s resize/move); **04** supplies optional tuning
  read-only. **03.5** overlays still composite above the tiling.
- **07** palette is the shared fuzzy-list UI; **09** reuses it for the project picker.
- **08** binds keys to commands owned elsewhere; vim normal-mode keys stay inside **06**.
- **08.1** is a controlled audit of **08**'s bindings: it consumes `internal/keymap`
  (no new engine) and makes each binding genuinely usable — a terminal-reachability
  probe (which chords actually arrive), command-id reconciliation/registration
  (kill inert presses), a leader key (`space` outside the editor, `Ctrl+K …`
  universal) for fragile/intercepted chords, discoverability (live cheatsheet,
  which-key, palette shortcut column), and a per-binding verification matrix.
  Real semantic commands stay owned by 05/06/07/09/VCS; 08.1 owns the *binding
  experience* and records blocked-by dependencies. Lives as a directory roadmap
  (`0081-keybindings-audit/`).
- **08.2** is a directory roadmap with one file per existing keybinding
  (`0082-keybinding-usability/01-undo.md` … `30-revert-file.md`). Each file
  reviews the *usability of the action behind the chord* — search-field behavior,
  picker UX, confirm prompts, feedback — via a per-binding checklist + manual test
  protocol. Verdicts are filled by the user ("OK passt" / change requests); a
  binding is done when its verdict is OK. Blocked commands are spec'd for intended
  UX now, verified once their owner roadmap lands. Feeds concrete change requests
  back to 05/06/07/09/VCS.
- **11** activates **04**'s inert `[theme].name`: a named palette recolors
  syntax, explorer, and chrome in one move. New leaf `internal/theme` (lipgloss
  only; **not** `internal/palette`, which is **07**'s command palette) holds the
  built-in palettes + one shared color resolver, collapsing the duplicated
  `namedColors`/resolver in **10**'s `highlight` and **05**'s explorer. A palette
  bundles ui slots + capture defaults + file-color defaults; it feeds the
  **defaults** of the existing `highlight.Theme` and explorer `colorTable` while
  `theme.captures.*` / `[explorer.colors]` still override. Chrome literals in
  app/explorer/editor move onto ui slots. Themes register via **02**
  (`Capabilities.Themes []Theme`, compile-in plugin); live re-theme rides **04**'s
  `ConfigReloadedMsg`. Model mirrors sqlit/Textual's semantic-slot `Theme`.

## Known gaps / future roadmaps

- **VCS / Git** (unplanned): Roadmap 0080's JetBrains defaults bind Cmd+K → Commit and
  Cmd+T → Update Project. Those commands have no owner yet — they bind to placeholder
  command ids until a Git roadmap registers them.
