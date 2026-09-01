package editor

// pemsummary.go collapses PEM blocks into a one-line summary (#1652). A
// certificate is forty lines of base64 that answer none of the questions a
// reader has about it, so the block folds down to its `-----BEGIN …-----`
// line with internal/peminfo's reading appended — subject CN, validity window,
// issuer, key type, SANs — and an expired or soon-expiring certificate draws
// that reading in the error or warning colour.
//
// The mechanic is the log repeat run's (#1650), not the conceal layer's: a
// conceal range is per-line and a PEM block is not, so the body lines ride the
// fold machinery (fold.go) — hidden for motions, scrolling, mouse mapping and
// the render loop alike. Like a repeat run and unlike a fold it has no
// open/close command: it reveals positionally, the way every stand-in family
// does (#1594). Put the cursor anywhere inside the block and all of it renders
// raw, base64 included; move out and it collapses again.
//
// The layer is language-agnostic on purpose. It reads the buffer text rather
// than the span pipeline, because the blocks worth summarising are as often
// pasted into a YAML block scalar or a config file as they are alone in a
// .pem — and an indented block decodes exactly like one at the margin
// (peminfo.ScanAt trims). Gated by editor.pem_summary /
// view.togglePemSummary, plus the large-file guard (#149): re-scanning a
// multi-megabyte buffer per document version is what that guard exists for.

import (
	"charm.land/lipgloss/v2"

	"ike/internal/peminfo"
)

// pemState caches the PEM blocks of one document version. A pointer field on
// Model, like logRunCache, so the value copies each Update returns share it.
type pemState struct {
	valid   bool
	version int
	path    string
	// head maps every line of a block *after* its BEGIN marker onto that
	// marker's line; at maps a BEGIN line onto its block.
	head map[int]int
	at   map[int]peminfo.Block
}

// pemActive reports whether the summary layer applies to this buffer.
func (m Model) pemActive() bool { return m.pemSummaryOn() && !m.largeFile }

// pemBlocks returns the PEM blocks of the current document version,
// recomputing them when the version moved. Blocks are a whole-buffer property
// (one may start above the viewport), so like the log repeat runs the scan is
// not viewport-scoped; the large-file guard bounds its cost.
func (m Model) pemBlocks() *pemState {
	st := m.pemCache
	if st == nil {
		return &pemState{}
	}
	if st.valid && st.version == m.docVersion && st.path == m.path {
		return st
	}
	st.valid, st.version, st.path = true, m.docVersion, m.path
	st.head, st.at = nil, nil
	for _, b := range peminfo.Scan(m.buf.Lines()) {
		if b.End <= b.Start {
			// A one-line block has no body to hide and nothing to collapse.
			continue
		}
		if st.head == nil {
			st.head, st.at = make(map[int]int), make(map[int]peminfo.Block)
		}
		st.at[b.Start] = b
		for l := b.Start + 1; l <= b.End; l++ {
			st.head[l] = b.Start
		}
	}
	return st
}

// pemHidden reports whether line is folded away inside a PEM block: it belongs
// to one, and the cursor is not inside that block (the positional reveal).
func (m Model) pemHidden(line int) bool {
	if !m.pemActive() {
		return false
	}
	st := m.pemBlocks()
	h, ok := st.head[line]
	if !ok {
		return false
	}
	return !m.inPemBlock(st.at[h])
}

// inPemBlock reports whether the cursor sits inside the block.
func (m Model) inPemBlock(b peminfo.Block) bool {
	return m.cursor.Line >= b.Start && m.cursor.Line <= b.End
}

// pemBlockAt returns the collapsed PEM block whose BEGIN marker is line. It
// reports false when line starts no block, or when the cursor revealed it — a
// revealed block renders as ordinary lines.
func (m Model) pemBlockAt(line int) (peminfo.Block, bool) {
	if !m.pemActive() {
		return peminfo.Block{}, false
	}
	b, ok := m.pemBlocks().at[line]
	if !ok || m.inPemBlock(b) {
		return peminfo.Block{}, false
	}
	return b, true
}

// hasPemBlocks reports whether this view collapses any PEM block at all, the
// gate hasFolds folds into the fold-aware render/motion/scroll paths.
func (m Model) hasPemBlocks() bool { return m.pemActive() && len(m.pemBlocks().at) > 0 }

// pemMinSummary is the narrowest summary worth drawing. Below it the row
// falls back to the bare marker: a certificate's facts cut to five columns say
// nothing, and the ellipsis alone would only look like damage.
const pemMinSummary = 12

// renderPemHeader renders the BEGIN marker of a collapsed block with the
// summary appended — the same budgeting as renderFoldHeader, except that the
// summary truncates rather than disappearing. A full certificate reading is
// far wider than an eighty-column pane, and dropping it there would leave the
// feature invisible at the most common width; peminfo orders the summary so
// the CN and the expiry verdict come first, and truncation eats the SANs.
// The summary draws in the error colour for an expired or not-yet-valid
// certificate, the warning colour inside peminfo.WarnWindow, and faint
// otherwise, so severity reads before the text does.
func (m Model) renderPemHeader(line int, b peminfo.Block, width int, cursorStyle, selStyle lipgloss.Style) string {
	const gap = "  "
	avail := width - lipgloss.Width(m.buf.Line(line)) - lipgloss.Width(gap)
	if avail < pemMinSummary {
		row, _ := m.renderLine(line, width, cursorStyle, selStyle)
		return row
	}
	tag := gap + truncate(b.Summary, avail)
	body, _ := m.renderLine(line, width-lipgloss.Width(tag), cursorStyle, selStyle)
	return body + m.pemStyle(b.Severity).Render(tag)
}

// pemStyle resolves the summary style for a severity.
func (m Model) pemStyle(sev peminfo.Severity) lipgloss.Style {
	switch sev {
	case peminfo.SevError:
		return lipgloss.NewStyle().Foreground(m.theme().Error).Bold(true)
	case peminfo.SevWarn:
		return lipgloss.NewStyle().Foreground(m.theme().Warning)
	}
	return lipgloss.NewStyle().Faint(true)
}

// togglePemSummary flips the PEM summary layer for this view. Like the other
// view toggles the override sticks: applyConfig stops tracking the
// editor.pem_summary default once toggled.
func (m *Model) togglePemSummary() {
	m.pemSummary = !m.pemSummary
	m.pemSummarySet = true
}
