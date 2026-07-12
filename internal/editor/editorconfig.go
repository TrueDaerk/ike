package editor

import (
	"path/filepath"

	"ike/internal/editorconfig"
	"ike/internal/textenc"
)

// editorconfig.go wires the EditorConfig standard into the editor (#63). The
// resolved settings are a per-buffer override layer between IKE's own config
// and explicit per-buffer user actions:
//
//	built-in defaults < IKE config < .editorconfig < explicit user action
//
// applyConfig reads IKE's config first and then lets the buffer's resolved
// .editorconfig settings override indent style/width, trim-on-save and
// final-newline. Explicit per-buffer actions win because they run after (and
// independent of) the per-Update refresh: file.setLineEndings /
// file.setEncoding change eol/enc directly, which resolveEditorconfig only
// touches at Load/NewFile time. editor.editorconfig = false in IKE's config
// disables the whole layer.

// resolveEditorconfig (re-)resolves the buffer path's effective EditorConfig
// settings. Called wherever the buffer's identity changes (Load, NewFile,
// SetPath, save-as) and when a watched .editorconfig changes.
func (m *Model) resolveEditorconfig() {
	if m.path == "" || !m.editorconfigEnabled() {
		m.ec = nil
		return
	}
	m.ec = editorconfig.Resolve(m.path)
}

// editorconfigEnabled reads editor.editorconfig; the layer is on by default.
func (m Model) editorconfigEnabled() bool {
	if m.cfg == nil {
		return true
	}
	return boolOr(m.cfg, "editor.editorconfig", true)
}

// applyEditorconfig overlays the buffer's resolved settings onto the values
// applyConfig just read from IKE's config. Only keys the section set are
// touched, so everything else keeps following the config live.
func (m *Model) applyEditorconfig() {
	if len(m.ec) == 0 || !m.editorconfigEnabled() {
		return
	}
	if v, ok := m.ec.UseSpaces(); ok {
		m.useSpaces = v
	}
	if w, ok := m.ec.IndentWidth(); ok {
		m.tabWidth = w
	}
	if v, ok := m.ec.TrimTrailingWhitespace(); ok {
		m.trimTrailing = v
	}
	if v, ok := m.ec.InsertFinalNewline(); ok {
		m.insertFinalNewline = v
	}
}

// editorconfigEOL maps an end_of_line setting onto the stored line-ending
// flavor (#66); ok is false when unset, disabled, or unrepresentable ("cr").
func (m Model) editorconfigEOL() (textenc.LineEnding, bool) {
	if !m.editorconfigEnabled() {
		return "", false
	}
	switch v, _ := m.ec.EndOfLine(); v {
	case "lf":
		return textenc.LF, true
	case "crlf":
		return textenc.CRLF, true
	}
	return "", false
}

// editorconfigCharset maps a charset setting onto a supported encoding; ok is
// false when unset, disabled, or unsupported. Load uses it as the decode
// fallback (a BOM or valid UTF-8 content still wins — re-interpreting
// readable bytes would corrupt them), NewFile as the new file's encoding.
func (m Model) editorconfigCharset() (textenc.Encoding, bool) {
	if !m.editorconfigEnabled() {
		return "", false
	}
	cs, ok := m.ec.Charset()
	if !ok {
		return "", false
	}
	return textenc.Lookup(cs)
}

// handleEditorconfigChange reacts to a watcher event (#53) on any
// .editorconfig: the shared cache drops the changed directory and this
// buffer's settings re-resolve, so the next applyConfig pass picks them up.
func (m *Model) handleEditorconfigChange(path string) bool {
	if filepath.Base(path) != editorconfig.FileName {
		return false
	}
	editorconfig.Invalidate(path)
	m.resolveEditorconfig()
	return true
}

// IndentInfo reports the effective indent style and width for the status
// line's indent segment ("Spaces: 2" / "Tab: 4", #63).
func (m Model) IndentInfo() (useSpaces bool, width int) {
	return m.useSpaces, m.tabWidth
}
