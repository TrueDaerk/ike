package editor

import (
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"ike/internal/bracket"
	"ike/internal/highlight"
	"ike/internal/jwt"
	"ike/internal/lang"
	"ike/internal/nethint"
	"ike/internal/secret"
	"ike/internal/unidiff"
	"ike/internal/unihint"
	"ike/internal/yamlanchor"
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
// a goroutine, yielding a SpansMsg. It returns nil only when the document is
// flagged large (#149) — the parse cost scales with file size, so skipping it is
// the single biggest degradation win. Files without a language still get a pass:
// the Unicode hygiene scan (#1654) is language-agnostic, and its ASCII fast path
// keeps the cost of a plain buffer at one byte scan per line.
func (m *Model) parseCmd() tea.Cmd {
	if m.InsightOff() {
		return nil
	}
	supported := highlight.Supported(m.path)
	path := m.path
	version := m.docVersion
	lines := m.buf.Lines()
	return func() tea.Msg {
		var spans []highlight.Span
		var scopes []highlight.Scope
		var folds []highlight.Fold
		var notes []lang.Note
		if supported {
			spans, scopes, folds = highlight.HighlightScoped(path, lines)
			// The Go-computed linter (#1623) rides the same off-loop pass as
			// the parse: same snapshot, same version guard, no second schedule.
			notes = highlight.Lint(path, lines)
		}
		// Invisible/deceptive Unicode findings (#1654) merge into the same
		// note stream, so every buffer — with or without a language — marks
		// zero-width characters, bidi controls and confusable identifiers.
		notes = append(notes, unihint.Notes(lines)...)
		return highlight.SpansMsg{Path: path, Version: version, Spans: spans, Scopes: scopes, Folds: folds, Notes: notes}
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
	// Log-layer captures (#1621) resolve through the palette-aware table in
	// logrender.go and honor the log-rendering toggle: off means raw text.
	if logCapture(capture) {
		if !m.logRender {
			return lipgloss.Style{}, false
		}
		return m.logStyle(capture)
	}
	st, ok := m.hlTheme.Style(capture)
	// An unmatched bracket (#1628) is an error, not a nesting level: it takes
	// the theme's error colour and an underline unless a capture override
	// names its own colour.
	if capture == bracket.Unmatched {
		return m.unmatchedBracketStyle(st, ok), true
	}
	// A YAML alias without a matching anchor (#1629) is broken the same way an
	// unmatched bracket is: error colour plus underline, override-able via a
	// theme.captures.anchor.unresolved key.
	if capture == yamlanchor.Unresolved {
		return m.unmatchedBracketStyle(st, ok), true
	}
	// A JWT's signature segment (#1619) carries no meaning for a reader, so it
	// renders faint unless a theme.captures.jwt.signature key says otherwise —
	// which the Style lookup above already honoured.
	if capture == jwt.Capture && !ok {
		return lipgloss.NewStyle().Faint(true), true
	}
	// A masked secret (#1623) is a value like any other: without a
	// theme.captures.secret.value key it styles as the language's string
	// colour, so the mask sits where the value sat and the revealed value
	// under the caret reads normally.
	if capture == secret.Capture && !ok {
		return m.hlTheme.Style("string")
	}
	// A punycode host whose decoded form carries the homograph shape (#1653)
	// is a phishing tell, not a readability win: it takes the theme's warning
	// colour unless a theme.captures.net.idn.mixed key names its own.
	if capture == nethint.IDNMixedCapture && !ok {
		return lipgloss.NewStyle().Foreground(m.theme().Warning), true
	}
	// The word-level changed range of a paired removed/added diff line
	// (#1630) takes the diff viewer's changed-range background under the
	// line's own foreground (resolved above via the dotted-prefix fallback),
	// so a .diff buffer reads like the diff panes.
	if capture == unidiff.PlusEmph || capture == unidiff.MinusEmph {
		return st.Background(m.theme().DiffChanged), true
	}
	// markup.* captures (#881) carry terminal text attributes, not colors:
	// **bold** renders bold, *italic* italic, ~~strike~~ struck through —
	// composed over whatever color the theme resolves (usually none). Gated
	// by the markdown-rendering toggle (#1599) like the conceal layer, so
	// toggling off shows plain raw source.
	if m.mdRender {
		switch capture {
		case "markup.bold":
			return st.Bold(true), true
		case "markup.italic":
			return st.Italic(true), true
		case "markup.strikethrough":
			return st.Strikethrough(true), true
		}
	}
	return st, ok
}
