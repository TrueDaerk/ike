package editor

import (
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"ike/internal/highlight"
)

// highlight.go drives the Tree-sitter syntax layer (Roadmap 0100). Parsing is
// CGo and runs off the event loop: a change schedules parseCmd, which produces a
// highlight.SpansMsg the editor caches and renderLine reads synchronously.

// maybeReparse appends a parse command when the document version advanced during
// this update (i.e. the buffer changed), so highlighting tracks every edit.
func (m Model) maybeReparse(beforeVersion int, cmd tea.Cmd) (Model, tea.Cmd) {
	if m.docVersion == beforeVersion {
		return m, cmd
	}
	if pc := m.parseCmd(); pc != nil {
		return m, tea.Batch(cmd, pc)
	}
	return m, cmd
}

// Reparse schedules a fresh parse of the whole buffer (used after Load so a file
// is highlighted as soon as it opens). Returns nil for unsupported languages.
func (m *Model) Reparse() tea.Cmd { return m.parseCmd() }

// parseCmd snapshots the buffer and version and returns a command that parses on
// a goroutine, yielding a SpansMsg. It returns nil when the file has no grammar,
// or when the document is flagged large (#149) — the CGo parse cost scales with
// file size, so skipping it is the single biggest degradation win.
func (m *Model) parseCmd() tea.Cmd {
	if m.InsightOff() || !highlight.Supported(m.path) {
		return nil
	}
	path := m.path
	version := m.docVersion
	lines := m.buf.Lines()
	return func() tea.Msg {
		spans, scopes, folds := highlight.HighlightScoped(path, lines)
		return highlight.SpansMsg{Path: path, Version: version, Spans: spans, Scopes: scopes, Folds: folds}
	}
}

// styleAt returns the syntax style for the rune at (line, col) and whether one
// applies. Called from renderLine's default branch; cursor and selection styles
// still win on overlap.
func (m Model) styleAt(line, col int) (lipgloss.Style, bool) {
	// Precedence (#9): Tree-sitter base < semantic overlay; the diagnostic
	// underline is applied on top by renderLine either way.
	capture := m.semIndex.CaptureAt(line, col)
	if capture == "" {
		capture = m.hlIndex.CaptureAt(line, col)
	}
	if capture == "" {
		return lipgloss.Style{}, false
	}
	st, ok := m.hlTheme.Style(capture)
	// markup.* captures (#881) carry terminal text attributes, not colors:
	// **bold** renders bold, *italic* italic, ~~strike~~ struck through —
	// composed over whatever color the theme resolves (usually none).
	switch capture {
	case "markup.bold":
		return st.Bold(true), true
	case "markup.italic":
		return st.Italic(true), true
	case "markup.strikethrough":
		return st.Strikethrough(true), true
	}
	return st, ok
}
