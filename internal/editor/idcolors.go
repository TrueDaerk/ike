package editor

import (
	"charm.land/lipgloss/v2"

	"ike/internal/highlight"
	"ike/internal/idcolor"
)

// idcolors.go is the editor half of identifier color hashing (#1626): UUIDs
// and long hex hashes (git SHAs, request/trace IDs) render in a color derived
// from the identifier's own hash, so every occurrence of the same identifier
// shares one color and the lines belonging to one request stand out without
// reading a hex digit. Detection lives in `internal/idcolor`; the color comes
// from the shared rainbow palette (#789/#1589), so a `theme.captures.rainbow.N`
// override moves identifier colors along with bracket colors.
//
// Scope is language-keyed, like the color-preview scope of #1622: only the
// formats where an opaque identifier is the point — logs (#1621), JSON/NDJSON
// and `.http` files (the response pane colors its body through the same
// package). Everywhere else a hex run is far more often a literal than an ID.
//
// Detection runs per rendered line inside the (line-cached) render path, so
// only visible lines are ever scanned.

// idColorLangs are the buffer languages identifier coloring applies to.
var idColorLangs = map[string]bool{
	"log": true, "json": true, "ndjson": true, "http": true,
}

// toggleIDColors flips identifier coloring for this view
// (view.toggleIdentifierColors). The override sticks like the #64 view
// toggles: applyConfig stops tracking editor.id_colors once toggled.
func (m *Model) toggleIDColors() {
	m.idColors = !m.idColors
	m.idColorsSet = true
}

// lineIDColors returns the identifier spans of one line when coloring is
// enabled for this buffer; nil otherwise.
func (m Model) lineIDColors(line int) []idcolor.Span {
	if !m.idColors || !idColorLangs[highlight.Lang(m.langPath())] {
		return nil
	}
	return idcolor.Scan(m.buf.Line(line), m.idColorMin)
}

// idColorStyle resolves the rainbow slot of a span into the style its cells
// render with; not ok when the theme has no such capture.
func (m Model) idColorStyle(s idcolor.Span) (lipgloss.Style, bool) {
	return m.hlTheme.Style(idcolor.Capture(s.Slot))
}
