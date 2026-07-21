package settings

// search.go makes the panel filter reach the whole product (0420, #886):
// custom pages export their items through the Searchable seam, category
// titles match as jump rows, and activating a result navigates there —
// "/python interpreter" lands on Toolchain › python instead of "no matching
// settings".

// SearchItem is one filterable item a custom page exports.
type SearchItem struct {
	// Label is the display text (and primary match text).
	Label string
	// Keywords are additional match terms (space-separated).
	Keywords string
	// Activate positions the page on the item (select the row, open the
	// picker, …); the panel navigates to the page first.
	Activate func()
}

// Searchable is an optional PageModel extension: pages implementing it are
// covered by the "/" filter.
type Searchable interface {
	SearchItems() []SearchItem
}

// rowKind classifies a filter-result row.
type rowKind int

const (
	rowEntry rowKind = iota // a schema entry (editable in place)
	rowPage                 // a category-title match (enter jumps there)
	rowItem                 // a custom page's SearchItem (enter navigates)
)

// activateResult runs a non-entry filter result: clear the filter, select the
// page, and let the item position itself.
func (m *Model) activateResult(r row) {
	m.filter, m.filtering = "", false
	m.cat, m.sel = r.page, 0
	m.focus = formColumn
	m.followCat, m.followForm = true, true
	if r.activate != nil {
		r.activate()
	}
}

// --- Searchable implementations (#886) ---

// SearchItems implements Searchable for the toolchain page: one item per
// language (interpreter keywords included) plus the create-environment
// action.
func (t *ToolchainPage) SearchItems() []SearchItem {
	var out []SearchItem
	for i, r := range t.rows() {
		i := i
		if r.action == "newenv" {
			out = append(out, SearchItem{
				Label:    "New Python environment",
				Keywords: "venv uv virtualenv create python environment",
				Activate: func() { t.sel = i },
			})
			continue
		}
		out = append(out, SearchItem{
			Label:    r.lang.ID,
			Keywords: "interpreter toolchain version " + r.lang.ID,
			Activate: func() { t.sel = i },
		})
	}
	return out
}

// SearchItems implements Searchable for the tools page: one item per
// configured tool.
func (t *ToolsPage) SearchItems() []SearchItem {
	var out []SearchItem
	for i, e := range t.entries() {
		i := i
		out = append(out, SearchItem{
			Label:    e.Name,
			Keywords: "tool custom pane " + e.Command,
			Activate: func() { t.sel = i },
		})
	}
	return out
}

// SearchItems implements Searchable for the plugins page: one item per
// plugin.
func (p *PluginsPage) SearchItems() []SearchItem {
	var out []SearchItem
	for i, r := range p.rows() {
		i := i
		out = append(out, SearchItem{
			Label:    r.ID,
			Keywords: "plugin enable disable",
			Activate: func() { p.sel = i },
		})
	}
	return out
}

// SearchItems implements Searchable for the keymap page: every binding row by
// command id and title.
func (k *KeymapPage) SearchItems() []SearchItem {
	var out []SearchItem
	for i, b := range k.rows() {
		i, b := i, b
		out = append(out, SearchItem{
			Label:    b.Command,
			Keywords: "keybinding shortcut chord " + b.Title + " " + b.Chord.String(),
			Activate: func() { k.sel = i },
		})
	}
	return out
}

// SearchItems implements Searchable for the LSP page: one item per language
// server row.
func (p *LSPPage) SearchItems() []SearchItem {
	var out []SearchItem
	for i, l := range p.servers() {
		i, l := i, l
		out = append(out, SearchItem{
			Label:    l.ID,
			Keywords: "lsp language server " + l.ID,
			Activate: func() { p.sel = i },
		})
	}
	return out
}

// SearchItems implements Searchable for the marketplace page: the catalog
// entries by name.
func (p *MarketplacePage) SearchItems() []SearchItem {
	var out []SearchItem
	for i, e := range p.rows() {
		i, e := i, e
		out = append(out, SearchItem{
			Label:    e.Name,
			Keywords: "marketplace plugin " + e.Description,
			Activate: func() { p.sel = i },
		})
	}
	return out
}

// SearchItems implements Searchable for the PHP debug-mapping page.
func (t *DebugMapPage) SearchItems() []SearchItem {
	var out []SearchItem
	for i, e := range t.entries() {
		i, e := i, e
		out = append(out, SearchItem{
			Label:    e.Server + " → " + e.Local,
			Keywords: "debug php path mapping xdebug",
			Activate: func() { t.sel = i },
		})
	}
	return out
}
