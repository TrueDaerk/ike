package editor

import (
	tea "charm.land/bubbletea/v2"

	"ike/internal/host"
	"ike/internal/plugin"
	"ike/internal/registry"
)

// ContextID is advertised by the editor pane for context-scoped command/keymap
// resolution (it matches the root model's editor context).
const ContextID = "editor"

// commands.go is the single bridge between editor actions / ex-commands and the
// plugin registry. Each registered Command dispatches an ActionMsg, which the
// root model routes back into the focused editor's Update. The palette (07) and
// keybindings (08) invoke these by id — the editor grows no parallel dispatch.

// action builds a registry Command that runs a named editor action via ActionMsg.
// shortcut is the documentation hint shown in the help sheet: the editor's vim
// keys (ex-commands like ":w", modal keys like "u") are handled directly in the
// editor, not through the keymap layer, so they are surfaced as doc hints.
func action(id, title, name, shortcut string) plugin.Command {
	return plugin.Command{
		ID:       id,
		Title:    title,
		Scope:    plugin.PaneScope(ContextID),
		Shortcut: shortcut,
		Run: func(h host.API) tea.Cmd {
			return h.Dispatch(ActionMsg{Action: name})
		},
	}
}

// editorPlugin contributes the editor's actions and ex-commands as registry
// Commands. It is compiled in and self-registers below.
type editorPlugin struct{}

// ID implements plugin.Plugin.
func (editorPlugin) ID() string { return "editor" }

// Capabilities implements plugin.Plugin.
func (editorPlugin) Capabilities() plugin.Capabilities {
	return plugin.Capabilities{
		Commands: []plugin.Command{
			action("editor.write", "Save File", "write", ":w"),
			action("editor.quit", "Close Editor", "quit", ":q"),
			action("editor.write_quit", "Save and Close", "write_quit", ":wq"),
			action("editor.undo", "Undo", "undo", "u"),
			action("editor.redo", "Redo", "redo", "ctrl+r"),
			// Undo tree (#59): the overlay plus vim's chronological walks.
			action("editor.undoTree", "Undo Tree", "undo_tree", ""),
			action("editor.undoChrono", "Undo (Chronological)", "undo_chrono", "g-"),
			action("editor.redoChrono", "Redo (Chronological)", "redo_chrono", "g+"),
			action("editor.copy", "Copy", "copy", "y"),
			action("editor.cut", "Cut", "cut", "d"),
			action("editor.paste", "Paste", "paste", "p"),
			action("editor.lineStart", "Move to Line Start", "line_start", "0"),
			action("editor.lineEnd", "Move to Line End", "line_end", "$"),
			action("editor.find", "Find in File", "find", "/"),
			action("editor.replace", "Replace in File", "replace", ":s"),
			action("editor.duplicateLine", "Duplicate Line", "duplicate_line", ""),
			action("editor.caret.addNext", "Add Caret at Next Occurrence", "caret_add_next", ""),
			action("editor.caret.addAll", "Add Carets at All Occurrences", "caret_add_all", ""),
			action("editor.commentLine", "Toggle Line Comment", "comment_line", ""),
			action("editor.commentBlock", "Toggle Block Comment", "comment_block", ""),
			// View options (#64): per-view display toggles. They flip the
			// focused buffer's option on top of the [editor] config defaults.
			action("view.toggleWrap", "Toggle Soft Wrap", "toggle_wrap", ""),
			action("view.toggleWhitespace", "Toggle Whitespace Rendering", "toggle_whitespace", ""),
			action("view.toggleIndentGuides", "Toggle Indent Guides", "toggle_indent_guides", ""),
			// Code folding (#144): the vim z-commands, reachable from the
			// palette and rebindable through the keymap layer.
			action("editor.fold.toggle", "Toggle Fold", "fold_toggle", "za"),
			action("editor.fold.close", "Close Fold", "fold_close", "zc"),
			action("editor.fold.open", "Open Fold", "fold_open", "zo"),
			action("editor.fold.closeAll", "Close All Folds", "fold_close_all", "zM"),
			action("editor.fold.openAll", "Open All Folds", "fold_open_all", "zR"),
			// Line-ending / encoding conversion (#66): one command per choice,
			// theme-picker style — the palette's fuzzy match over
			// "Line Endings:" / "Encoding:" is the picker. Conversions mark
			// the buffer dirty and materialize on the next save.
			action("file.setLineEndings.lf", "Line Endings: LF", "eol_lf", ""),
			action("file.setLineEndings.crlf", "Line Endings: CRLF", "eol_crlf", ""),
			action("file.setEncoding.utf8", "Encoding: UTF-8", "encoding_utf8", ""),
			action("file.setEncoding.utf8bom", "Encoding: UTF-8 BOM", "encoding_utf8_bom", ""),
			action("file.setEncoding.utf16le", "Encoding: UTF-16 LE", "encoding_utf16le", ""),
			action("file.setEncoding.utf16be", "Encoding: UTF-16 BE", "encoding_utf16be", ""),
			action("file.setEncoding.latin1", "Encoding: ISO 8859-1", "encoding_latin1", ""),
			action("file.setEncoding.windows1252", "Encoding: Windows-1252", "encoding_windows1252", ""),
			// Diagnostic navigation (#369) carries lsp.* ids — it steps through
			// the LSP diagnostics — but lives here because the editor already
			// caches the set; no server round-trip is involved.
			action("lsp.nextDiagnostic", "Next Diagnostic", "next_diagnostic", ""),
			action("lsp.prevDiagnostic", "Previous Diagnostic", "prev_diagnostic", ""),
		},
	}
}

func init() { registry.Register(editorPlugin{}) }
