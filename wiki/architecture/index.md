# Architecture

Component-level concepts for the IKE codebase.

* [Foundation Slice](/architecture/foundation.md) - root model, layout, focus, message routing
* [File Explorer](/architecture/explorer.md) - directory tree pane
* [Editor](/architecture/editor.md) - vim-like modal editor: buffer, motions, operators, text objects, registers, undo/redo, search, viewport (Roadmap 0060)
* [Plugin Extension Contract](/architecture/plugins.md) - compile-in registry, extension points, host API
* [Configuration System](/architecture/config.md) - typed TOML config, defaults < user < project merge, validation, extension hook, host integration (Roadmap 0040)
* [Help Overlay](/architecture/help-overlay.md) - command & shortcut cheat sheet, responsive columns, vertical scroll
* [Notifications](/architecture/notifications.md) - toast notifications: severities, expiry, stacking, Esc dismissal
* [Command Palette](/architecture/command-palette.md) - centered overlay fronting every action: ":" registry commands (context-ranked) + "@" fuzzy file finder, prefix-dispatched modes (Roadmap 0070)
* [Keybindings & Shortcuts](/architecture/keybindings.md) - chord/key model, JetBrains-like default set, context-scoped resolution with multi-step chords + timeout, conflict detection, platform normalisation, cheatsheet (Roadmap 0080)
* [Floating Shell](/architecture/floating-shell.md) - reusable centered overlay component hosting any content (modals, plugin popups, help)
* [Pane Layout & Drag](/architecture/pane-layout.md) - pure split-tree layout, mouse divider-resize & title-bar move, split/close ops, per-project persistence (Roadmap 0036/0037)
* [Pane Registry & Multiple Editors](/architecture/pane-registry.md) - instance registry behind layout leaves, N editors, focused-leaf focus model, open-in-new-pane intent (Roadmap 0037)
* [Editor Tabs](/architecture/editor-tabs.md) - per-pane ordered document list with one active tab: open appends/activates, close peels tabs before the pane, shared buffers across tabs (Roadmap 0190)
* [Session Restore](/architecture/session-restore.md) - per-project workspace persistence: open file + cursor, explorer expansion/hidden/cursor, saved on quit
* [Scratch Files](/architecture/scratch-files.md) - language-aware quick buffers under the user state dir, created from the palette, ordinary files that survive restarts (Roadmap 0280)
* [Crash Recovery](/architecture/crash-recovery.md) - vim-swapfile-style safety net: debounced full-text snapshots of dirty buffers, atomic writes to the project state dir, restore on next launch (Roadmap 0210)
* [Settings UI & Menu Bar](/architecture/settings-ui.md) - menu bar over the command registry; settings panel with schema-driven forms and config write-back (Roadmap 0160)
* [LSP & Language Intelligence](/architecture/lsp.md) - JSON-RPC client over a server's stdio, manager per (language, root), editor-driven sync, diagnostics/completion/hover/go-to-definition (Roadmap 0100)
* [Syntax Highlighting](/architecture/highlighting.md) - Tree-sitter lexical layer: per-language grammars parsed off-loop into theme-coloured spans, applied per cell (Roadmap 0100)
* [Project Search](/architecture/search.md) - streaming find-in-path engine: rg --json backend + pure-Go fallback, generation-based cancellation, bounded results (Roadmap 0150)
* [Language Registry](/architecture/languages.md) - neutral lang registry bundling extensions + grammar + LSP server + toolchain detector; per-language plugins make adding a language a new package (Roadmap 0105)
* [Themes / Color Schemes](/architecture/themes.md) - semantic-slot palette system: one [theme].name recolors syntax + explorer + chrome, built-in themes, plugin registration (Roadmap 0110)
* [Project Switching](/architecture/project-switching.md) - recent-projects history data layer: typed [[project.history]] entries, root validation, upsert/dedupe/cap persisted via config; picker + switch orchestration upcoming (Roadmap 0090)
* [Integrated Terminal](/architecture/terminal.md) - PTY-spawned shell through a VT emulator as a pane: raw key routing with ctrl+tab escape hatch, coalesced output, resize propagation (Roadmap 0170)
* [Writing WASM Plugins](/architecture/plugin-authoring.md) - plugin-author guide: Go guest SDK, build & install (wasip1 c-shared), sandbox posture, raw ABI reference for other languages (Roadmap 9900)
* [Navigation History (Back/Forward)](/architecture/navigation-history.md) - per-jump cursor history with JetBrains Back/Forward semantics: open-funnel recording, nav.back/nav.forward, leader mnemonics space b / space i (Roadmap 0220)
