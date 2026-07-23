# Architecture

Component-level concepts for the IKE codebase.

* [Foundation Slice](/architecture/foundation.md) - root model, layout, focus, message routing
* [File Explorer](/architecture/explorer.md) - directory tree pane
* [Editor](/architecture/editor.md) - vim-like modal editor: buffer, motions, operators, text objects, registers, undo/redo, search, viewport (Roadmap 0060)
* [Plugin Extension Contract](/architecture/plugins.md) - compile-in registry, extension points, host API
* [Configuration System](/architecture/config.md) - typed TOML config, defaults < user < project merge, validation, extension hook, host integration (Roadmap 0040)
* [Help Overlay](/architecture/help-overlay.md) - command & shortcut cheat sheet, responsive columns, vertical scroll
* [Welcome Tour](/architecture/welcome-tour.md) - passive paged first-orientation walkthrough in the floating shell
* [Notifications](/architecture/notifications.md) - toast notifications: severities, expiry, stacking, Esc dismissal
* [Command Palette](/architecture/command-palette.md) - centered overlay fronting every action: ":" registry commands (context-ranked) + "@" fuzzy file finder, prefix-dispatched modes (Roadmap 0070)
* [Keybindings & Shortcuts](/architecture/keybindings.md) - chord/key model, JetBrains-like default set, context-scoped resolution with multi-step chords + timeout, conflict detection, platform normalisation, cheatsheet (Roadmap 0080)
* [Workspace](/architecture/workspace.md) - per-project UI state unit (panes, split tree, focus) behind a Manager; the seamless project-switching seam (Roadmap 0370)
* [Floating Shell](/architecture/floating-shell.md) - reusable centered overlay component hosting any content (modals, plugin popups, help)
* [Pane Layout & Drag](/architecture/pane-layout.md) - pure split-tree layout, mouse pane-edge resize & title-bar move, split/close ops, per-project persistence (Roadmap 0036/0037)
* [Pane Registry & Multiple Editors](/architecture/pane-registry.md) - instance registry behind layout leaves, N editors, focused-leaf focus model, open-in-new-pane intent (Roadmap 0037)
* [Editor Tabs](/architecture/editor-tabs.md) - per-pane ordered document list with one active tab: open appends/activates, close peels tabs before the pane, shared buffers across tabs (Roadmap 0190)
* [Session Restore](/architecture/session-restore.md) - per-project workspace persistence: open file + cursor, explorer expansion/hidden/cursor, saved on quit
* [Performance & Diagnostics](/architecture/performance.md) - idle wake rules (demand-armed ticks, off-loop explorer poll, timers die with owners) + opt-in pprof/SIGUSR1 hooks (#1001)
* [Scratch Files](/architecture/scratch-files.md) - language-aware quick buffers under the user state dir, created from the palette, ordinary files that survive restarts (Roadmap 0280)
* [Crash Recovery](/architecture/crash-recovery.md) - vim-swapfile-style safety net: debounced full-text snapshots of dirty buffers, atomic writes to the project state dir, restore on next launch (Roadmap 0210)
* [Settings UI & Menu Bar](/architecture/settings-ui.md) - menu bar over the command registry; settings panel with schema-driven forms and config write-back (Roadmap 0160)
* [Completion Engine](/architecture/completion.md) - multi-source autocomplete: LSP + local index sources as tagged batches, editor-side merge with priority de-dup and stable selection (Roadmap 0410)
* [LSP & Language Intelligence](/architecture/lsp.md) - JSON-RPC client over a server's stdio, manager per (language, root), editor-driven sync, diagnostics/completion/hover/go-to-definition (Roadmap 0100)
* [Syntax Highlighting](/architecture/highlighting.md) - Tree-sitter lexical layer: per-language grammars parsed off-loop into theme-coloured spans, applied per cell (Roadmap 0100)
* [Project Search](/architecture/search.md) - streaming find-in-path engine: rg --json backend + pure-Go fallback, generation-based cancellation, bounded results (Roadmap 0150)
* [Language Registry](/architecture/languages.md) - neutral lang registry bundling extensions + grammar + LSP server + toolchain detector; per-language plugins make adding a language a new package (Roadmap 0105)
* [EditorConfig Support](/architecture/editorconfig.md) - .editorconfig resolution: spec glob matching, root=true upward search, watcher-invalidated cache, per-buffer override layer (#63)
* [Themes / Color Schemes](/architecture/themes.md) - semantic-slot palette system: one [theme].name recolors syntax + explorer + chrome, built-in themes, plugin registration (Roadmap 0110)
* [Project Switching](/architecture/project-switching.md) - recent-projects history data layer: typed [[project.history]] entries, root validation, upsert/dedupe/cap persisted via config; picker + switch orchestration upcoming (Roadmap 0090)
* [Integrated Terminal](/architecture/terminal.md) - PTY-spawned shell through a VT emulator as a pane: raw key routing with ctrl+tab escape hatch, coalesced output, resize propagation (Roadmap 0170)
* [Custom TUI Tool Panes](/architecture/tool-panes.md) - user-configured TUIs (lazygit, htop, k9s) as panes: tool.<name> commands, toggle focus, tool chrome, exit-closes-pane, layout restore, IKE_THEME_* env (#741)
* [Debugger](/architecture/debugger.md) - DAP debug sessions over run configurations: breakpoints hit, paused-line marker, IntelliJ stepping chords F7/F8/F9/Shift+F8, one session at a time (0350)
* [Run Configurations](/architecture/run-configurations.md) - named, persisted run/debug configurations synthesized into command lines through the language registry's RunCommandProvider seam; per-project .ike/runconfigs.json store (0350)
* [Writing WASM Plugins](/architecture/plugin-authoring.md) - plugin-author guide: Go guest SDK, build & install (wasip1 c-shared), sandbox posture, raw ABI reference for other languages (Roadmap 9900)
* [Pinned File Slots](/architecture/pinned-files.md) - harpoon-style numbered slots: pin the working set, jump by number; per-project persistence and a modal picker (#788)
* [Navigation History (Back/Forward)](/architecture/navigation-history.md) - per-jump cursor history with JetBrains Back/Forward semantics: open-funnel recording, nav.back/nav.forward (Roadmap 0220)
* [Status Line Segments](/architecture/status-line.md) - extensible left/right slot model behind the bottom status bar: mode/file/diagnostics plus toolchain interpreter and notification counter segments (#101)
* [Plugin Marketplace](/architecture/marketplace.md) - discover/install/update/remove WASM plugins in-IDE: static HTTPS JSON catalog, checksum-verified atomic installs pinning the reviewed capability list, marketplace settings page (Roadmap 0310)
* [Markdown Preview](/architecture/markdown-preview.md) - rendered live preview pane for markdown buffers: glamour ANSI output beside the editor, debounced re-render, heading-anchored cursor scroll sync, theme-aware (#62)
* [TODO Index](/architecture/todo-index.md) - project-wide comment-tag index (TODO/FIXME/HACK/XXX, configurable): overlay over the locations list, own scan service, per-file rescan on save, tag/current-file filters, status-line count (#61)
* [Diff Viewer](/architecture/diff-viewer.md) - reusable read-only diff pane: line-level Myers engine with intra-line refinement, side-by-side/unified rendering, hunk navigation with editor jump, diff.files palette command (#60)
* [VCS / Git Integration](/architecture/vcs.md) - async git status snapshot behind explorer coloring, branch segment, gutter diff markers, commit dialog, update/revert, branch picker, file-vs-HEAD diff, inline blame (Epic 0320)
