package settings

import (
	"strconv"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"ike/internal/concealfilter"
	"ike/internal/config"
	"ike/internal/theme"
)

// conceal_page.go is the Conceal & Hints control center (#2133). The conceal
// suite grew to seventeen gateable families plus three coloring layers, and
// its configuration was scattered across the generic Editor page as twenty-six
// interleaved keys — a list that says which switches exist but never which are
// on, what they draw, or where they are allowed to draw it. This page is the
// single surface: the families grouped by what they do, each with its toggle
// and the layer its value comes from, the three glob lists and the two pattern
// maps as structured list editors with per-kind validation, and a live preview
// of the selected family rendered through the very packages the editor draws
// with.
//
// The page edits the **config defaults**. A per-view toggle (Toggle Timestamp
// Decoding, …) overrides them for one buffer and is deliberately not shown
// here — view state is not configuration — which the header says out loud.
//
// Like every custom page it writes directly rather than through the staged
// buffer (#1296): a conceal setting is judged by looking at it, so the write
// is the preview.

// concealRowKind classifies a page row.
type concealRowKind int

const (
	concealHeaderRow  concealRowKind = iota // group title, not selectable
	concealToggleRow                        // a bool config key
	concealStepperRow                       // an int config key (left/right)
	concealListRow                          // a []string key, edited in a sub-panel
	concealInfoRow                          // a family with no switch at all
)

// concealListKind selects the element grammar a list editor enforces.
type concealListKind int

const (
	concealGlobs      concealListKind = iota // conceal_include / conceal_exclude
	concealRules                             // conceal_file_rules: family=pattern
	concealUnits                             // number_hint_units: pattern=unit
	concealSecretKeys                        // secret_masking_keys: [-]pattern
)

// concealRow is one rendered line of the page.
type concealRow struct {
	kind     concealRowKind
	key      string // config key, empty for headers and info rows
	title    string
	desc     string
	family   string // concealfilter family the key gates, "" when none
	min, max int    // stepper bounds
	list     concealListKind
}

// concealGroups inserts a header before the entry that opens a group, so the
// page's order comes from the schema page and only the grouping is stated
// here.
var concealGroups = []struct{ beforeKey, title string }{
	{"editor.markdown_rendering", "RENDERING LAYERS — a whole file drawn readable"},
	{"editor.timestamp_decoding", "DECODERS — an encoded value drawn as what it encodes"},
	{"editor.cron_hints", "VALUE HINTS — a stand-in appended after the literal"},
	{"editor.secret_masking", "SECRETS & FIELD UNITS"},
	{"editor.conceal_include", "FILE RULES — where the families above may draw at all"},
	{"editor.rainbow_brackets", "COLORING LAYERS — no columns hidden, so the file rules do not gate them"},
}

// concealListKinds names the element grammar of each list key.
var concealListKinds = map[string]concealListKind{
	"editor.secret_masking_keys": concealSecretKeys,
	"editor.number_hint_units":   concealUnits,
	"editor.conceal_include":     concealGlobs,
	"editor.conceal_exclude":     concealGlobs,
	"editor.conceal_file_rules":  concealRules,
}

// concealCatalog builds the page rows from the schema page of the same title:
// titles, descriptions and stepper bounds have one definition (the Entry), and
// docgen keeps documenting the keys even though the panel renders this model.
func concealCatalog() []concealRow {
	var entries []Entry
	for _, p := range BasePages(nil, nil, nil) {
		if p.Title == ConcealPageTitle {
			entries = p.Entries
			break
		}
	}
	rows := make([]concealRow, 0, len(entries)+len(concealGroups)+1)
	for _, e := range entries {
		for _, g := range concealGroups {
			if g.beforeKey == e.Key {
				rows = append(rows, concealRow{kind: concealHeaderRow, title: g.title})
			}
		}
		r := concealRow{key: e.Key, title: e.Title, desc: e.Description, min: e.Min, max: e.Max}
		if fam := strings.TrimPrefix(e.Key, "editor."); concealfilter.IsFamily(fam) {
			r.family = fam
		}
		switch e.Type {
		case Bool:
			r.kind = concealToggleRow
		case Int:
			r.kind = concealStepperRow
		case List:
			r.kind = concealListRow
			r.list = concealListKinds[e.Key]
		default:
			continue
		}
		rows = append(rows, r)
	}
	rows = append(rows,
		concealRow{kind: concealHeaderRow, title: "NO SWITCH"},
		concealRow{kind: concealInfoRow, title: "JWT decoding",
			desc: "A JWT in a buffer always decodes its header and payload into the explain popover — the layer draws no stand-in of its own, so there is nothing to switch off"},
	)
	return rows
}

// ConcealPageTitle is the page's title, shared with the schema page it takes
// its entries from and with the app's registration.
const ConcealPageTitle = "Conceal & Hints"

// ConcealPage implements PageModel.
type ConcealPage struct {
	navRows // last rendered height, the pgup/pgdn page (#1666)
	opts    config.Options
	pal     *theme.Palette

	rows []concealRow
	nav  []int // row indices that can be selected (headers are skipped)

	sel  int // index into nav
	off  int
	note string
	host SubPanelHost

	listH int // list-window height of the last render (mouse hit-testing)
}

// NewConcealPage builds the conceal control center writing through opts.
func NewConcealPage(opts config.Options) *ConcealPage {
	c := &ConcealPage{opts: opts, rows: concealCatalog()}
	for i, r := range c.rows {
		if r.kind != concealHeaderRow {
			c.nav = append(c.nav, i)
		}
	}
	return c
}

// SetSubPanelHost implements the hostAware injection seam (#883).
func (c *ConcealPage) SetSubPanelHost(h SubPanelHost) { c.host = h }

// SetPalette implements PageModel.
func (c *ConcealPage) SetPalette(p *theme.Palette) { c.pal = p }

// Capturing implements PageModel: every editor is a sub-panel, so the page
// itself never captures.
func (c *ConcealPage) Capturing() bool { return false }

// theme returns the active palette, defaulting when none was threaded in.
func (c *ConcealPage) theme() *theme.Palette {
	if c.pal != nil {
		return c.pal
	}
	return theme.DefaultPalette()
}

// selected returns the selected row.
func (c *ConcealPage) selected() concealRow {
	if c.sel < 0 || c.sel >= len(c.nav) {
		return concealRow{}
	}
	return c.rows[c.nav[c.sel]]
}

// boolValue reads a bool key's effective value.
func concealBool(key string) bool {
	cfg := config.Get()
	if cfg == nil {
		return false
	}
	return cfg.Flat()[key] == "true"
}

// intValue reads an int key's effective value.
func concealInt(key string) int {
	cfg := config.Get()
	if cfg == nil {
		return 0
	}
	n, _ := strconv.Atoi(cfg.Flat()[key])
	return n
}

// concealList reads a list key's effective value off the typed config — Flat
// joins the elements, which is not what a list editor may split back.
func concealList(key string) []string {
	cfg := config.Get()
	if cfg == nil {
		return nil
	}
	switch key {
	case "editor.secret_masking_keys":
		return cfg.Editor.SecretMaskingKeys
	case "editor.number_hint_units":
		return cfg.Editor.NumberHintUnits
	case "editor.conceal_include":
		return cfg.Editor.ConcealInclude
	case "editor.conceal_exclude":
		return cfg.Editor.ConcealExclude
	case "editor.conceal_file_rules":
		return cfg.Editor.ConcealFileRules
	}
	return nil
}

// Update implements PageModel.
func (c *ConcealPage) Update(key tea.KeyPressMsg) tea.Cmd {
	// Shared list semantics (#1666): steps wrap, page jumps clamp.
	if listNav(key.String(), &c.sel, len(c.nav), c.navPageSize()) {
		c.note = ""
		return nil
	}
	row := c.selected()
	switch key.String() {
	case "enter", " ", "space":
		return c.activate(row)
	case "left", "h":
		return c.step(row, -1)
	case "right", "l":
		return c.step(row, +1)
	case "r":
		if row.key == "" {
			return nil
		}
		c.note = row.title + " reset to its built-in default"
		return config.RemoveAndReload(c.opts, config.UserScope, row.key)
	}
	return nil
}

// activate applies the row's primary action: flip a toggle, open a list
// editor. An info row has nothing to activate.
func (c *ConcealPage) activate(row concealRow) tea.Cmd {
	switch row.kind {
	case concealToggleRow:
		on := !concealBool(row.key)
		c.note = row.title + " " + onOff(on)
		return config.WriteAndReload(c.opts, config.UserScope, row.key, on)
	case concealListRow:
		if c.host != nil {
			c.note = ""
			c.host.Push(newConcealListPanel(c, c.host, row))
		}
	}
	return nil
}

// step moves a stepper row's value inside its bounds.
func (c *ConcealPage) step(row concealRow, delta int) tea.Cmd {
	if row.kind != concealStepperRow {
		return nil
	}
	v := clamp(concealInt(row.key)+delta, row.min, row.max)
	if v == concealInt(row.key) {
		return nil
	}
	c.note = row.title + " = " + strconv.Itoa(v)
	return config.WriteAndReload(c.opts, config.UserScope, row.key, v)
}

// onOff words a bool for the notice line.
func onOff(on bool) string {
	if on {
		return "on"
	}
	return "off"
}

// View implements PageModel.
func (c *ConcealPage) View(w, h int) string {
	c.setRows(h)
	pal := c.theme()
	sec := lipgloss.NewStyle().Foreground(pal.Secondary)
	head := sec.Render(pad(" state  setting", w-12) + "layer")
	list := make([]string, len(c.rows))
	selRow := -1
	if c.sel >= 0 && c.sel < len(c.nav) {
		selRow = c.nav[c.sel]
	}
	for i, r := range c.rows {
		list[i] = c.renderRow(i, r, i == selRow, w, pal)
	}
	footer := c.footer(w, pal)
	c.listH = h - 1 - len(footer)
	if selRow < 0 {
		selRow = 0
	}
	return head + "\n" + pinFooter(list, footer, selRow, selRow, h-1, &c.off)
}

// renderRow draws one line: the state marker, the title and the config layer
// the value comes from.
func (c *ConcealPage) renderRow(_ int, r concealRow, sel bool, w int, pal *theme.Palette) string {
	sec := lipgloss.NewStyle().Foreground(pal.Secondary)
	if r.kind == concealHeaderRow {
		return sec.Bold(true).MaxWidth(w).Render(" " + r.title)
	}
	state, layer := "", ""
	switch r.kind {
	case concealToggleRow:
		if concealBool(r.key) {
			state = "◉ on "
		} else {
			state = "○ off"
		}
	case concealStepperRow:
		state = "‹" + pad(strconv.Itoa(concealInt(r.key)), 3) + "›"
	case concealListRow:
		state = "≡" + pad(strconv.Itoa(len(concealList(r.key))), 4)
	case concealInfoRow:
		state = "  —  "
	}
	if r.key != "" {
		layer = config.Origin(c.opts, r.key)
	}
	body := "  " + state + "  " + r.title
	if lw := w - 12; lw > 0 {
		body = pad(body, lw) + layer
	}
	style := lipgloss.NewStyle().MaxWidth(w)
	if sel {
		style = style.Background(pal.Selection).Foreground(pal.SelectionText).Bold(true)
	} else if r.kind == concealInfoRow {
		style = style.Foreground(pal.Secondary)
	}
	return style.Render(body)
}

// footer is the preview panel plus the key hint: the selected family's sample
// line drawn both ways, with the state the toggle currently picks marked.
func (c *ConcealPage) footer(w int, pal *theme.Palette) []string {
	sec := lipgloss.NewStyle().Foreground(pal.Secondary)
	var lines []footerLine
	if c.note != "" {
		lines = append(lines, footerLine{text: "   " + c.note, style: sec})
	}
	for _, l := range c.previewLines(pal) {
		lines = append(lines, footerLine{text: l.text, style: l.style})
	}
	lines = append(lines, footerLine{
		text:  "   enter toggle/edit · ←→ adjust · r reset to default — config defaults; a per-view toggle wins in that buffer",
		style: sec,
	})
	return wrapFooter(lines, w, 8)
}

// previewLines renders the selected row's preview block.
func (c *ConcealPage) previewLines(pal *theme.Palette) []footerLine {
	sec := lipgloss.NewStyle().Foreground(pal.Secondary)
	acc := lipgloss.NewStyle().Foreground(pal.Accent)
	row := c.selected()
	if row.key == "" {
		if row.kind == concealInfoRow {
			return []footerLine{{text: "   " + row.desc, style: sec}}
		}
		return nil
	}
	// The glob and rule lists decide *where*, not *what*: their preview is the
	// verdict on the sample paths, which is the question they answer.
	if row.kind == concealListRow && (row.list == concealGlobs || row.list == concealRules) {
		rules := concealList("editor.conceal_file_rules")
		out := []footerLine{{text: "   preview · " + row.key, style: sec}}
		fam := ""
		if row.list == concealRules {
			// Per-family rules only say anything about the families they name;
			// previewing the first one is what shows the override at work.
			fam = concealRuleFamily(rules)
			out = append(out, footerLine{text: "     for " + fam + ":", style: sec})
		}
		for _, l := range concealFilterPreview(fam, concealList("editor.conceal_include"),
			concealList("editor.conceal_exclude"), rules) {
			out = append(out, footerLine{text: "     " + l, style: acc})
		}
		return out
	}
	sample, ok := concealSampleFor(row.key)
	if !ok {
		return []footerLine{{text: "   " + row.desc, style: sec}}
	}
	on := concealBool(row.key)
	if row.kind != concealToggleRow {
		// A list or stepper row has no switch of its own; it previews the
		// family it feeds, which is on unless that family's toggle is off.
		on = concealBool(concealOwnerKey(row.key))
	}
	shown := sample.Shown
	if sample.Paint != nil {
		shown = sample.Paint(pal, shown)
	}
	mark := func(active bool) string {
		if active {
			return " ❯ "
		}
		return "   "
	}
	rawStyle, shownStyle := sec, sec
	if on {
		shownStyle = acc
	} else {
		rawStyle = acc
	}
	return []footerLine{
		{text: "   preview · " + row.key, style: sec},
		{text: "  " + mark(!on) + "raw   " + sample.Raw, style: rawStyle},
		{text: "  " + mark(on) + "drawn " + shown, style: shownStyle},
	}
}

// concealRuleFamily names the family the rule preview reports on: the first
// one a configured rule names, or secret masking while there are none — the
// family the rules are most often written about.
func concealRuleFamily(rules []string) string {
	for _, raw := range rules {
		fam, _, ok := strings.Cut(raw, "=")
		fam = strings.TrimPrefix(strings.ToLower(strings.TrimSpace(fam)), "editor.")
		if ok && concealfilter.IsFamily(fam) {
			return fam
		}
	}
	return concealfilter.SecretMasking
}

// concealOwnerKey names the toggle a non-toggle key's preview depends on: a
// pattern list is only visible while the family reading it is on.
func concealOwnerKey(key string) string {
	switch key {
	case "editor.secret_masking_keys":
		return "editor.secret_masking"
	case "editor.id_color_min_length":
		return "editor.id_colors"
	case "editor.number_hint_units":
		return "editor.byte_size_hints"
	}
	return key
}

// Click implements the optional PageClicker seam (enter semantics on the
// selected row).
func (c *ConcealPage) Click(_, y int) tea.Cmd {
	line := y - 1
	if line < 0 || (c.listH > 0 && line >= c.listH) {
		return nil
	}
	idx := line + c.off
	if idx < 0 || idx >= len(c.rows) || c.rows[idx].kind == concealHeaderRow {
		return nil
	}
	for i, ri := range c.nav {
		if ri != idx {
			continue
		}
		if i == c.sel {
			return c.activate(c.rows[idx])
		}
		c.sel, c.note = i, ""
		return nil
	}
	return nil
}

// Wheel implements the optional PageWheeler seam.
func (c *ConcealPage) Wheel(delta int) {
	if n := len(c.nav); n > 0 {
		c.sel = clamp(c.sel+delta, 0, n-1)
	}
}

// KeyHelp implements KeyHelper (#887).
func (c *ConcealPage) KeyHelp() []string {
	return []string{
		"enter  toggle a family / open a list editor · ←→  adjust a number · r  reset to default",
		"the page edits the config defaults; a per-view toggle (Toggle Timestamp Decoding, …) wins in that buffer",
	}
}

// SearchItems implements Searchable (#886): every family and list is reachable
// from the panel-wide filter.
func (c *ConcealPage) SearchItems() []SearchItem {
	out := make([]SearchItem, 0, len(c.nav))
	for i, ri := range c.nav {
		r := c.rows[ri]
		i, r := i, r
		out = append(out, SearchItem{
			Label:    r.title,
			Keywords: r.key + " " + r.family + " conceal hints",
			Activate: func() { c.sel = i },
		})
	}
	return out
}
