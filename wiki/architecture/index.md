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
* [Session Restore](/architecture/session-restore.md) - per-project workspace persistence: open file + cursor, explorer expansion/hidden/cursor, saved on quit
* [LSP & Language Intelligence](/architecture/lsp.md) - JSON-RPC client over a server's stdio, manager per (language, root), editor-driven sync, diagnostics/completion/hover/go-to-definition (Roadmap 0100)
* [Syntax Highlighting](/architecture/highlighting.md) - Tree-sitter lexical layer: per-language grammars parsed off-loop into theme-coloured spans, applied per cell (Roadmap 0100)
* [Language Registry](/architecture/languages.md) - neutral lang registry bundling extensions + grammar + LSP server + toolchain detector; per-language plugins make adding a language a new package (Roadmap 0105)
* [Themes / Color Schemes](/architecture/themes.md) - semantic-slot palette system: one [theme].name recolors syntax + explorer + chrome, built-in themes, plugin registration (Roadmap 0110)
