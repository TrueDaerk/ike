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
	page   int
}

// railRows interleaves section headers (#890) with the page rows.
func (m *Model) railRows() []railRow {
	var out []railRow
	for i, p := range m.pages {
		if p.Section != "" {
			out = append(out, railRow{header: p.Section, page: -1})
		}
		out = append(out, railRow{page: i})
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
