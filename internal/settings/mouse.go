package settings

import (
	"strings"

	tea "charm.land/bubbletea/v2"

	"ike/internal/config"
)

// mouse.go is the settings panel's pointer affordance layer (0420, #885):
// hover highlighting (menu-bar parity), viewport-scrolling wheel semantics
// for the panel's own lists, and the clickable chrome — the scope chip, the
// hint-row keys, and path-completion suggestion rows.

// PageHoverer is an optional PageModel extension (#885): pages implementing
// it receive pointer positions at page-local coordinates (-1,-1 clears).
type PageHoverer interface {
	Hover(x, y int)
}

// Hover records the pointer position for the hover highlight: the category
// row or settings row under it. Panel-local coordinates like Click.
func (m *Model) Hover(x, y int) {
	m.hoverCat, m.hoverRow = -1, -1
	if !m.open || m.SubOpen() {
		m.pageHover(-1, -1)
		return
	}
	row := y - bodyTop
	if row < 0 || row >= m.height-chromeRows {
		m.pageHover(-1, -1)
		return
	}
	if x >= 1 && x < 1+catWidth {
		if m.filter != "" {
			m.pageHover(-1, -1)
			return // search-mode rail rows have no hover state of their own
		}
		rows := m.railRows()
		if idx := row + m.catOff; idx >= 0 && idx < len(rows) && rows[idx].header == "" {
			m.hoverCat = rows[idx].page
		}
		m.pageHover(-1, -1)
		return
	}
	if page := m.customPage(); page != nil && m.filter == "" {
		if x >= formX() {
			m.pageHover(x-formX(), row)
		}
		return
	}
	g := m.gridFor()
	if x < formX() || row >= g.listH {
		return
	}
	if g.side && x >= m.detailX() {
		return // the detail column has its own cursor, not a hover row
	}
	if idx := m.rowAtLine(row + m.formOff); idx >= 0 && idx < len(m.rows()) {
		m.hoverRow = idx
	}
}

// pageHover forwards the pointer to a hovering custom page.
func (m *Model) pageHover(x, y int) {
	if page := m.customPage(); page != nil {
		if h, ok := page.(PageHoverer); ok {
			h.Hover(x, y)
		}
	}
}

// hintAction is one clickable segment of the hint row.
type hintAction struct {
	start, end int
	action     string
}

// hintRowY is the hint row's panel-local y (inside the bottom border).
func (m *Model) hintRowY() int { return m.height - 2 }

// clickChrome hit-tests the non-body chrome: the scope chip on the title row
// and the hint-row keys. Handled reports whether the press was consumed.
func (m *Model) clickChrome(x, y int) (tea.Cmd, bool) {
	if y == 1 && m.chipSpan.end > m.chipSpan.start && x >= m.chipSpan.start && x < m.chipSpan.end {
		m.writeScope = (m.writeScope + 1) % 3
		return nil, true
	}
	if y == 1 && m.countSpan.end > m.countSpan.start && x >= m.countSpan.start && x < m.countSpan.end {
		// The change counter is the mouse's apply button (#1296).
		return m.openApply(), true
	}
	if y == m.hintRowY() {
		for _, h := range m.hintHits {
			if x >= h.start && x < h.end {
				return m.runHintAction(h.action), true
			}
		}
		return nil, true // dead hint-row cells swallow the press
	}
	return nil, false
}

// runHintAction executes a clicked hint-row key.
func (m *Model) runHintAction(action string) tea.Cmd {
	switch action {
	case "edit":
		if m.focus == catColumn {
			m.focus = formColumn
			m.sel = 0
			m.syncEditor()
			return nil
		}
		return m.activate()
	case "reset":
		if r, ok := m.current(); ok && m.focus == formColumn {
			return config.RemoveAndReload(m.opts, m.scopeFor(r.entry), r.entry.Key)
		}
	case "scope":
		m.writeScope = (m.writeScope + 1) % 3
	case "filter":
		m.filtering = true
		m.focus = formColumn
	case "help":
		m.openKeyHelp()
	case "apply":
		return m.openApply()
	case "openpage":
		m.openHitPage()
	case "toggle":
		if r, ok := m.current(); ok && m.focus == formColumn && r.kind == rowEntry && r.entry.Type == Bool {
			return m.writeValue(r.entry, m.value(r.entry.Key) != "true")
		}
	default:
		// A page verb on the bar ("key:a") reaches the page as its key would.
		if k, ok := strings.CutPrefix(action, "key:"); ok {
			if page := m.customPage(); page != nil {
				m.focus = formColumn
				return page.Update(keyPress(k))
			}
		}
	case "close":
		cmd := m.CancelPreview()
		m.Close()
		return cmd
	}
	return nil
}
