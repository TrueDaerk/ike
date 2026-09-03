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
			// Select All (#1861): a linewise visual selection over the whole
			// buffer, equivalent to the vim "ggVG" sequence.
			action("editor.selectAll", "Select All", "select_all", "ggVG"),
			action("editor.paste", "Paste", "paste", "p"),
			action("editor.lineStart", "Move to Line Start", "line_start", "0"),
			action("editor.lineEnd", "Move to Line End", "line_end", "$"),
			// The line and document family (#2400): JetBrains' Move Line
			// Up/Down, Delete Line and the document/line-edge motions. They
			// existed as vim gestures only, so the JetBrains chords resolved
			// to nothing; the doc hints name the gesture each one mirrors.
			action("editor.moveLineUp", "Move Line Up", "move_line_up", ""),
			action("editor.moveLineDown", "Move Line Down", "move_line_down", ""),
			action("editor.deleteLine", "Delete Line", "delete_line", "dd"),
			action("editor.deleteWordBackward", "Delete Word Backward", "delete_word_backward", "db"),
			action("editor.docStart", "Go to Document Start", "doc_start", "gg"),
			action("editor.docEnd", "Go to Document End", "doc_end", "G"),
			action("editor.selectLineStart", "Select to Line Start", "select_line_start", "v0"),
			action("editor.selectLineEnd", "Select to Line End", "select_line_end", "v$"),
			action("editor.find", "Find in File", "find", "/"),
			action("editor.replace", "Replace in File", "replace", ":s"),
			// Label jump (#787): easymotion/leap-style motion — type 1-2
			// target characters, every visible match gets a home-row label,
			// and the label key moves the caret there.
			action("editor.labelJump", "Jump to Visible Text (Label Jump)", "label_jump", "gs"),
			action("editor.duplicateLine", "Duplicate Line", "duplicate_line", ""),
			// Line-set commands (#2417): the selection's lines — or the whole
			// buffer without a selection — reordered or thinned in one undo
			// step, the palette/keybind half of the ":sort" family. The doc
			// hints name the ex-command each flavour mirrors.
			action("editor.sortLines", "Sort Lines", "sort_lines", ":sort"),
			action("editor.sortLinesIgnoreCase", "Sort Lines (Ignore Case)", "sort_lines_ignore_case", ":sort i"),
			action("editor.sortLinesNatural", "Sort Lines (Natural Order)", "sort_lines_natural", ""),
			action("editor.sortLinesDescending", "Sort Lines (Descending)", "sort_lines_descending", ":sort!"),
			action("editor.sortLinesByLength", "Sort Lines by Length", "sort_lines_by_length", ""),
			action("editor.uniqueLines", "Remove Duplicate Lines", "unique_lines", ""),
			action("editor.reverseLines", "Reverse Lines", "reverse_lines", ""),
			action("editor.shuffleLines", "Shuffle Lines", "shuffle_lines", ""),
			action("editor.caret.addNext", "Add Caret at Next Occurrence", "caret_add_next", ""),
			action("editor.caret.addAll", "Add Carets at All Occurrences", "caret_add_all", ""),
			action("editor.caret.addAbove", "Clone Caret Above", "caret_add_above", ""),
			action("editor.caret.addBelow", "Clone Caret Below", "caret_add_below", ""),
			// Increment / decrement / value toggle (#1658): the vim ctrl+a and
			// ctrl+x keys plus g!, surfaced as commands so they are rebindable
			// through the keymap layer and reachable from the palette.
			action("editor.increment", "Increment Number", "increment", "ctrl+a"),
			action("editor.decrement", "Decrement Number", "decrement", "ctrl+x"),
			action("editor.toggleValue", "Toggle Value Under Caret", "toggle_value", "g!"),
			// Case conversion (#2418): the command half of the gu/gU/g~
			// operators — the doc hints name the vim gesture each mirrors —
			// plus the identifier-style rotation JetBrains has no equivalent
			// of. All four take the selection, or the token under every
			// caret when there is none.
			action("editor.case.lower", "Lower Case", "case_lower", "gu"),
			action("editor.case.upper", "Upper Case", "case_upper", "gU"),
			action("editor.case.toggle", "Toggle Case", "case_toggle", "g~"),
			action("editor.case.cycle", "Cycle Case (camel/snake/kebab)", "case_cycle", ""),
			// Escape / unescape the selection (#2338): the writing direction
			// of the #1620 escape decoding — the selection (or the string
			// literal under the caret) is rewritten in the buffer language's
			// escape form, and back.
			action("editor.escapeSelection", "Escape Selection as Unicode", "escape_selection", ""),
			action("editor.unescapeSelection", "Unescape Selection", "unescape_selection", ""),
			// JSON/YAML path breadcrumb (#1660): the status line shows where
			// the caret sits, these copy it — plain, or as an expression the
			// matching CLI tool takes.
			action("editor.copyDocPath", "Copy JSON/YAML Path", "copy_doc_path", ""),
			action("editor.copyDocPathJQ", "Copy JSON/YAML Path as jq Expression", "copy_doc_path_jq", ""),
			action("editor.copyDocPathYQ", "Copy JSON/YAML Path as yq Expression", "copy_doc_path_yq", ""),
			action("editor.commentLine", "Toggle Line Comment", "comment_line", ""),
			action("editor.commentBlock", "Toggle Block Comment", "comment_block", ""),
			// View options (#64): per-view display toggles. They flip the
			// focused buffer's option on top of the [editor] config defaults.
			action("view.toggleWrap", "Toggle Soft Wrap", "toggle_wrap", ""),
			action("view.toggleWhitespace", "Toggle Whitespace Rendering", "toggle_whitespace", ""),
			action("view.toggleIndentGuides", "Toggle Indent Guides", "toggle_indent_guides", ""),
			// Markdown rich rendering (#1599): per-view toggle over the
			// editor.markdown_rendering config default, like the #64 toggles.
			action("view.toggleMarkdownRendering", "Toggle Markdown Rendering", "toggle_markdown_rendering", ""),
			// Log rich rendering (#1621): per-view toggle over the
			// editor.log_rendering config default.
			action("view.toggleLogRendering", "Toggle Log Rendering", "toggle_log_rendering", ""),
			// Follow mode (#1928): tail -f for the open file — streams
			// appended content, read-only, viewport stuck to the end.
			action("view.toggleFollow", "Toggle Follow (Tail -f)", "toggle_follow", ""),
			// Follow filter (#2255): a live pattern over the tailed stream —
			// hiding non-matching lines, or only colouring the matches.
			action("view.followFilter", "Filter Followed Output", "follow_filter", ""),
			action("view.followHighlight", "Highlight in Followed Output", "follow_highlight", ""),
			action("view.clearFollowFilter", "Clear Follow Filter", "follow_filter_clear", ""),
			// Inline epoch-timestamp decoding (#1618): per-view toggle over
			// the editor.timestamp_decoding config default.
			action("view.toggleTimestampDecoding", "Toggle Timestamp Decoding", "toggle_timestamp_decoding", ""),
			// Escaped-text decoding (#1620): per-family view toggles over the
			// editor.*_decoding config defaults — unicode escapes in string
			// literals, HTML/XML entities, base64 values in k8s Secret data.
			action("view.toggleUnicodeEscapeDecoding", "Toggle Unicode Escape Decoding", "toggle_unicode_escape_decoding", ""),
			action("view.toggleEntityDecoding", "Toggle Entity Decoding", "toggle_entity_decoding", ""),
			action("view.toggleBase64Decoding", "Toggle Base64 Decoding", "toggle_base64_decoding", ""),
			// Cron hints (#1624): per-view toggle over the editor.cron_hints
			// config default.
			action("view.toggleCronHints", "Toggle Cron Hints", "toggle_cron_hints", ""),
			// PEM summaries (#1652): per-view toggle over the
			// editor.pem_summary config default.
			action("view.togglePemSummary", "Toggle PEM Summary", "toggle_pem_summary", ""),
			// Number-readability hints (#1627): per-view toggles over the
			// editor.byte_size_hints / duration_hints / digit_grouping /
			// radix_hints config defaults.
			action("view.toggleByteSizeHints", "Toggle Byte Size Hints", "toggle_byte_size_hints", ""),
			action("view.toggleDurationHints", "Toggle Duration Hints", "toggle_duration_hints", ""),
			action("view.toggleDigitGrouping", "Toggle Digit Grouping", "toggle_digit_grouping", ""),
			action("view.toggleRadixHints", "Toggle Radix Hints", "toggle_radix_hints", ""),
			// Permission hints (#1656): per-view toggle over the
			// editor.permission_hints config default.
			action("view.togglePermissionHints", "Toggle Permission Hints", "toggle_permission_hints", ""),
			// Network-literal hints (#1653): per-view toggles over the
			// editor.cidr_hints / idn_hints config defaults.
			action("view.toggleCIDRHints", "Toggle CIDR Hints", "toggle_cidr_hints", ""),
			action("view.toggleIDNHints", "Toggle IDN Hints", "toggle_idn_hints", ""),
			// Secret masking (#1623): dotenv values whose key names a secret
			// render as ••••; per-view toggle over editor.secret_masking.
			action("view.toggleSecretMasking", "Toggle Secret Masking", "toggle_secret_masking", ""),
			// Inline color preview (#790/#1622): per-view toggle over the
			// editor.color_preview config default.
			action("view.toggleColorPreview", "Toggle Color Preview", "toggle_color_preview", ""),
			// Identifier colors (#1626): UUIDs and hex hashes colored by their
			// own hash; per-view toggle over the editor.id_colors default.
			action("view.toggleIdentifierColors", "Toggle Identifier Colors", "toggle_identifier_colors", ""),
			// JWT decoding (#1619): the caret's token opens as decoded header
			// and payload JSON in the hover popup.
			action("editor.decodeJWT", "Decode JWT at Caret", "decode_jwt", ""),
			// Syntax-aware extend/shrink selection (#1912): JetBrains'
			// Expand/Shrink Selection over the LSP selection-range ladder,
			// with a Tree-sitter fallback (selrange.go).
			action("editor.selection.extend", "Extend Selection", "selection_extend", ""),
			action("editor.selection.shrink", "Shrink Selection", "selection_shrink", ""),
			// Code folding (#144): the vim z-commands, reachable from the
			// palette and rebindable through the keymap layer.
			action("editor.fold.toggle", "Toggle Fold", "fold_toggle", "za"),
			action("editor.fold.close", "Close Fold", "fold_close", "zc"),
			action("editor.fold.open", "Open Fold", "fold_open", "zo"),
			action("editor.fold.closeAll", "Close All Folds", "fold_close_all", "zM"),
			action("editor.fold.openAll", "Open All Folds", "fold_open_all", "zR"),
			// Copy the fold under the cursor whole (#1787) — the keyboard
			// pendant of the collapsed header's ⧉ affordance.
			action("editor.fold.copy", "Copy Folded Range", "fold_copy", "zy"),
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
			// Git hunk navigation (#1170): the ]c/[c vim sequences, surfaced
			// as doc hints like the fold z-commands.
			action("vcs.nextChange", "Next Change (Editor)", "next_hunk", "]c"),
			action("vcs.prevChange", "Previous Change (Editor)", "prev_hunk", "[c"),
			action("lsp.nextDiagnostic", "Next Diagnostic", "next_diagnostic", ""),
			action("lsp.prevDiagnostic", "Previous Diagnostic", "prev_diagnostic", ""),
			// Diagnostic ignore rules (#1259): suppress the caret diagnostic's
			// rule (source+code, or exact message) project-wide via
			// lsp.diagnostics_ignore.
			action("lsp.ignoreDiagnostic", "Ignore Diagnostic Under Caret", "ignore_diagnostic", ""),
			// Conceal/secret explain popover (#1998): why the value at the
			// caret is masked or read as it is, with the reclassify keys. A
			// vim-style sequence like the fold commands, so it is surfaced as
			// a doc hint and stays rebindable through the keymap layer.
			action("editor.explainConceal", "Explain Concealed Value", "explain_conceal", "g?"),
			// Merge-conflict resolution (#1149, #2258): vim-style sequences
			// rather than cmd chords (that budget is full, #711) — go/gt/gb
			// resolve the block at the caret, gm keeps a hand-merged one, and
			// ]n/[n cycle the remaining blocks like ]c/[c cycle git hunks.
			// The accepts also surface contextually in the editor context
			// menu when the cursor is inside a block.
			action("merge.acceptOurs", "Merge: Accept Ours", "merge_accept_ours", "go"),
			action("merge.acceptTheirs", "Merge: Accept Theirs", "merge_accept_theirs", "gt"),
			action("merge.acceptBoth", "Merge: Accept Both", "merge_accept_both", "gb"),
			action("merge.keepManual", "Merge: Keep Manual Edit", "merge_keep_manual", "gm"),
			action("merge.nextConflict", "Merge: Next Conflict", "merge_next_conflict", "]n"),
			action("merge.prevConflict", "Merge: Previous Conflict", "merge_prev_conflict", "[n"),
		},
	}
}

func init() { registry.Register(editorPlugin{}) }
