package settings

// search.go makes the panel filter reach the whole product (0420, #886):
// custom pages export their items through the Searchable seam, category
// titles match as jump rows, and activating a result navigates there —
// "/python interpreter" lands on Toolchain › python instead of "no matching
// settings".
//
// Since #2179 the match itself is fuzzy and reads every piece of schema
// metadata: the config key, the title and the description, so a setting is
// findable by what it does ("/wrap long lines") and not only by what it is
// called. Matching is schema-driven, so a new Entry is searchable the moment
// it exists.

import (
	"sort"
	"strings"

	"ike/internal/fuzzy"
)

// searchField is one haystack a row exposes to the query. bonus lifts a hit
// in a name above the same hit in prose, so "tab width" ranks the entry above
// every description that happens to mention tabs; prose marks free text, where
// a scattered subsequence of one or two runes is noise rather than a match.
type searchField struct {
	text  string
	bonus int
	prose bool
}

// Field weights and the prose gate. The numbers only have to order fields
// against each other — the fuzzy scorer's own bonuses (16 for a word
// boundary) set the scale.
const (
	bonusKey    = 24 // the dotted config key: the most precise thing to type
	bonusTitle  = 20 // the form label
	bonusPage   = 8  // the category title, so "appearance theme" narrows
	bonusProse  = 0  // description / keywords
	proseMinLen = 3  // shorter patterns must appear literally in prose
)

// scoreFields matches query against fields. Whitespace-separated terms are
// ANDed — every term must hit at least one field, so "editor tab" narrows
// instead of widening — and the row's score is the sum of each term's best
// field score. ok is false when a term matches nothing.
func scoreFields(query string, fields ...searchField) (int, bool) {
	terms := strings.Fields(strings.ToLower(query))
	if len(terms) == 0 {
		return 0, true
	}
	total := 0
	for _, term := range terms {
		best, hit := 0, false
		for _, f := range fields {
			if f.text == "" {
				continue
			}
			res, ok := fuzzy.Match(term, f.text)
			if !ok {
				continue
			}
			if f.prose && len([]rune(term)) < proseMinLen &&
				!strings.Contains(strings.ToLower(f.text), term) {
				// "e" scattered through a sentence is not a hit.
				continue
			}
			if s := res.Score + f.bonus; !hit || s > best {
				best, hit = s, true
			}
		}
		if !hit {
			return 0, false
		}
		total += best
	}
	return total, true
}

// searchCache memoizes one query's result rows (#2179). rows() runs many
// times per frame — the form, the rail's hit pages, the selection, the
// editor — and fuzzy-matching every entry of every page on each call is far
// too expensive to repeat. The cache lives for exactly one input event: every
// entry point that can change the query or a page's items drops it, so a
// result list can never outlive the state it was built from.
type searchCache struct {
	query string
	rows  []row
	valid bool
}

// invalidateSearch drops the memoized result list.
func (m *Model) invalidateSearch() { m.search = searchCache{} }

// scored is one match with the score that ordered it.
type scored struct {
	row   row
	score int
}

// searchRows builds the filter's flat result list: every page title, schema
// entry and custom-page item the query matches, across all categories.
//
// The rows stay grouped by their page — the rail lists "pages with hits"
// (#1297) and a group has to be contiguous for that to read — but both levels
// are ordered by score: the page with the best hit comes first, and inside a
// page the best row does. So the strongest match is always the first row of
// the first group, and the rail is still a map of where the matches are.
func (m *Model) searchRows(query string) []row {
	type group struct {
		rows []scored
		best int
	}
	groups := make([]group, 0, len(m.pages))
	for pi, p := range m.pages {
		var g group
		add := func(r row, score int) {
			g.rows = append(g.rows, scored{row: r, score: score})
			if len(g.rows) == 1 || score > g.best {
				g.best = score
			}
		}
		// Category titles match as jump rows (#886).
		if s, ok := scoreFields(query, searchField{text: p.Title, bonus: bonusTitle}); ok {
			add(row{page: pi, kind: rowPage, label: p.Title}, s)
		}
		if p.Custom != nil {
			// Custom pages export their items through the Searchable seam
			// (#886); enter navigates there.
			if sp, ok := p.Custom.(Searchable); ok {
				for _, it := range sp.SearchItems() {
					s, ok := scoreFields(query,
						searchField{text: it.Label, bonus: bonusTitle},
						searchField{text: p.Title, bonus: bonusPage},
						searchField{text: it.Keywords, bonus: bonusProse, prose: true})
					if !ok {
						continue
					}
					add(row{page: pi, kind: rowItem, label: it.Label, activate: it.Activate}, s)
				}
			}
		}
		for _, e := range p.Entries {
			s, ok := scoreFields(query,
				searchField{text: e.Key, bonus: bonusKey},
				searchField{text: e.Title, bonus: bonusTitle},
				searchField{text: p.Title, bonus: bonusPage},
				searchField{text: e.Description, bonus: bonusProse, prose: true})
			if !ok {
				continue
			}
			add(row{page: pi, entry: e}, s)
		}
		if len(g.rows) > 0 {
			sort.SliceStable(g.rows, func(i, j int) bool { return g.rows[i].score > g.rows[j].score })
			groups = append(groups, g)
		}
	}
	sort.SliceStable(groups, func(i, j int) bool { return groups[i].best > groups[j].best })
	var out []row
	for _, g := range groups {
		for _, s := range g.rows {
			out = append(out, s.row)
		}
	}
	return out
}

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
	out := []SearchItem{{
		Label:    "New Python environment",
		Keywords: "venv uv virtualenv create python environment",
		Activate: func() { t.selectRow(func(r tcRow) bool { return r.action == "newenv" }) },
	}}
	for _, l := range t.languages() {
		id := l.ID
		out = append(out, SearchItem{
			Label:    id,
			Keywords: "interpreter toolchain version " + id,
			// Search reaches into the folded not-installed group (#1299):
			// activating unfolds it so the row is actually there.
			Activate: func() {
				t.showMissing = true
				t.selectRow(func(r tcRow) bool { return r.header == "" && r.action == "" && r.lang.ID == id })
			},
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

// --- search in the grid (0460, #1297) ---

// hitPage is one page carrying filter matches: where it sits in m.pages, how
// many rows matched, and the index of its first row in rows().
type hitPage struct {
	page     int
	count    int
	firstRow int
}

// hitPages groups the current filter's rows by their owning page, in row
// order — the search-mode content of the nav column: "which pages is this
// query even about".
func (m *Model) hitPages() []hitPage {
	var out []hitPage
	for i, r := range m.rows() {
		found := false
		for j := range out {
			if out[j].page == r.page {
				out[j].count++
				found = true
				break
			}
		}
		if !found {
			out = append(out, hitPage{page: r.page, count: 1, firstRow: i})
		}
	}
	return out
}

// hitSummary is the header's match count: "7 hits · 5 pages".
func (m *Model) hitSummary() string {
	n := len(m.rows())
	if n == 0 {
		return "no matches"
	}
	return plural(n, "hit") + " · " + plural(len(m.hitPages()), "page")
}

// jumpToHitPage moves the match list to the first row of the hit page at
// index i, and remembers the rail selection there.
func (m *Model) jumpToHitPage(i int) {
	pages := m.hitPages()
	if i < 0 || i >= len(pages) {
		return
	}
	m.hitSel = i
	m.sel = pages[i].firstRow
	m.followForm = true
}

// syncHitSel keeps the rail's search selection on the page owning the
// highlighted match, so moving down the match list walks the rail with it.
func (m *Model) syncHitSel() {
	rows := m.rows()
	if m.sel < 0 || m.sel >= len(rows) {
		return
	}
	for i, h := range m.hitPages() {
		if h.page == rows[m.sel].page {
			m.hitSel = i
			return
		}
	}
}

// openHitPage leaves the search on the highlighted match's own page (the
// footer's "tab open page"): the filter clears and the page opens positioned
// on that row.
func (m *Model) openHitPage() {
	r, ok := m.current()
	if !ok {
		return
	}
	if r.kind != rowEntry {
		m.activateResult(r)
		return
	}
	page, key := r.page, r.entry.Key
	m.filter, m.filtering = "", false
	m.cat, m.sel, m.focus = page, 0, formColumn
	for i, e := range m.pages[page].Entries {
		if e.Key == key {
			m.sel = i
			break
		}
	}
	m.followCat, m.followForm = true, true
	m.rebuildEditor()
}
