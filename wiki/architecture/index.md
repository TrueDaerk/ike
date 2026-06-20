# Architecture

Component-level concepts for the IKE codebase.

* [Foundation Slice](/architecture/foundation.md) - root model, layout, focus, message routing
* [File Explorer](/architecture/explorer.md) - directory tree pane
* [Editor](/architecture/editor.md) - vim-like modal editor: buffer, motions, operators, text objects, registers, undo/redo, search, viewport (Roadmap 0060)
* [Plugin Extension Contract](/architecture/plugins.md) - compile-in registry, extension points, host API
* [Configuration System](/architecture/config.md) - typed TOML config, defaults < user < project merge, validation, extension hook, host integration (Roadmap 0040)
* [Help Overlay](/architecture/help-overlay.md) - command & shortcut cheat sheet, responsive columns, vertical scroll
* [Floating Shell](/architecture/floating-shell.md) - reusable centered overlay component hosting any content (modals, plugin popups, help)
* [Pane Layout & Drag](/architecture/pane-layout.md) - pure split-tree layout, mouse divider-resize & title-bar move, per-project persistence (Roadmap 0036)
* [Session Restore](/architecture/session-restore.md) - per-project workspace persistence: open file + cursor, explorer expansion/hidden/cursor, saved on quit
* [Themes / Color Schemes](/architecture/themes.md) - planned semantic-slot palette system, built-in themes, the selector & registry (Roadmap 0110)
