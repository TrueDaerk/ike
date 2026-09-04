package settings

import (
	"encoding/json"
	"os"
	"path/filepath"
)

// railstate.go persists the panel's last-visited page per project (#890),
// next to the other .ike state. Losing it is harmless — the panel just opens
// on its first page again.

// lastPageFile is the per-project state location (IKE_CONFIG_DIR redirects
// like the window-size store).
func lastPageFile() string {
	if dir := os.Getenv("IKE_CONFIG_DIR"); dir != "" {
		return filepath.Join(dir, "settings-last.json")
	}
	return filepath.Join(".ike", "settings-last.json")
}

// loadLastPage reads the remembered page title ("" when none).
func loadLastPage() string {
	data, err := os.ReadFile(lastPageFile())
	if err != nil {
		return ""
	}
	var state struct {
		Page string `json:"page"`
	}
	if json.Unmarshal(data, &state) != nil {
		return ""
	}
	return state.Page
}

// saveLastPage writes the remembered page title, best effort.
func saveLastPage(title string) {
	data, err := json.Marshal(struct {
		Page string `json:"page"`
	}{Page: title})
	if err != nil {
		return
	}
	path := lastPageFile()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return
	}
	_ = os.WriteFile(path, data, 0o644)
}

// railRow is one rendered rail line: a section header or a page.
type railRow struct {
	header string // non-empty = a section header line
	page   int    // the page, or for a header the group's first page
	// collapsed marks a header whose pages are folded away; count is how many.
	collapsed bool
	count     int
}

// railGroup is one run of pages under a section header: [start, end).
type railGroup struct {
	name       string
	start, end int
}

// railGroups splits the pages into their sections (a page with an empty
// Section joins the previous one; leading pages without any section form an
// unnamed group).
func (m *Model) railGroups() []railGroup {
	var out []railGroup
	for i, p := range m.pages {
		// A page naming the section the rail is already in joins it rather
		// than opening a second header of the same name.
		if (p.Section != "" && (len(out) == 0 || p.Section != out[len(out)-1].name)) || len(out) == 0 {
			if len(out) > 0 {
				out[len(out)-1].end = i
			}
			out = append(out, railGroup{name: p.Section, start: i})
		}
	}
	if len(out) > 0 {
		out[len(out)-1].end = len(m.pages)
	}
	return out
}

// railHeight is the category rail's visible row count — the panel body height
// under the chrome. It is the rail's pgup/pgdn page size (#1666).
func (m *Model) railHeight() int { return max(1, m.height-chromeRows) }

// railRows is the accordion rail: every section is one header row, and only
// the section holding the current page lists its pages under it — so some
// forty pages read as eight groups plus the dozen that matter right now. A
// collapsed header carries its page count; a section with a single page
// renders as that page alone, a header over one row would only repeat it.
// (Before the 2026-09 overhaul every page was always listed, with headers
// as dim dividers.)
func (m *Model) railRows() []railRow {
	var out []railRow
	for _, g := range m.railGroups() {
		n := g.end - g.start
		if n <= 0 {
			continue
		}
		open := m.cat >= g.start && m.cat < g.end
		if g.name == "" || n == 1 && g.name != "" && m.pages[g.start].Title == g.name {
			// Unnamed leading pages, or a one-page group named like its page.
			for i := g.start; i < g.end; i++ {
				out = append(out, railRow{page: i})
			}
			continue
		}
		out = append(out, railRow{header: g.name, page: g.start, collapsed: !open, count: n})
		if !open {
			continue
		}
		for i := g.start; i < g.end; i++ {
			out = append(out, railRow{page: i})
		}
	}
	return out
}

// railRowOf returns the rail-row index of a page.
func (m *Model) railRowOf(page int) int {
	for i, r := range m.railRows() {
		if r.header == "" && r.page == page {
			return i
		}
	}
	return 0
}
