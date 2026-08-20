package settings

import (
	"sort"
	"strconv"
	"strings"

	tea "charm.land/bubbletea/v2"

	"ike/internal/bracket"
	"ike/internal/config"
	"ike/internal/highlight"
	"ike/internal/theme"
)

// colors_page.go is the syntax-colour editor (#1238): the settings surface for
// the `[theme.captures]` overrides wired up in #1318. A single highlight colour
// can be retuned without leaving the editor — the list shows the *effective*
// colour of every capture the active theme defines (plus the rainbow slots
// and the unmatched-bracket colour),
// marks the ones a config layer overrides, and writes `theme.captures.<name>`
// at user scope so the change survives a theme switch.
//
// Custom pages write directly rather than through the staged-apply buffer
// (#1296): a colour has to be visible while it is being chosen, and the write
// is the preview.

// rainbowSlots is the number of bracket-depth colour slots highlight cycles
// through; they are configurable captures like any other.
const rainbowSlots = 6

// ColorsPage implements PageModel.
type ColorsPage struct {
	navRows // last rendered height, the pgup/pgdn page (#1666)
	opts    config.Options
	pal     *theme.Palette

	sel int
	off int

	filter    string
	filterCur int // rune cursor inside filter (#2002)
	filtering bool

	// picking opens the colour list in the detail column; custom swaps it for
	// a free-token input (hex or ANSI index).
	picking bool
	pick    int
	custom  bool
	input   textField
	invalid string
	notice  string

	listH, listW int
	candTop      int
}

// NewColorsPage builds the capture-colour editor writing through opts.
func NewColorsPage(opts config.Options) *ColorsPage { return &ColorsPage{opts: opts} }

// SetPalette implements PageModel.
func (c *ColorsPage) SetPalette(p *theme.Palette) { c.pal = p }

// Capturing implements PageModel: the filter and the free-token input need
// every key verbatim (a colour token is text).
func (c *ColorsPage) Capturing() bool { return c.filtering || c.custom }

// theme returns the active palette, defaulting when none was threaded in.
func (c *ColorsPage) theme() *theme.Palette {
	if c.pal != nil {
		return c.pal
	}
	return theme.DefaultPalette()
}

// captures is the palette's capture table.
func (c *ColorsPage) captures() map[string]string { return c.theme().Captures }

// override returns the configured token for a capture, if a layer sets one.
func (c *ColorsPage) override(name string) (string, bool) {
	v, ok := config.Get().Flat()["theme.captures."+name]
	return v, ok && v != ""
}

// effective builds the live capture→style table the editors use, so the page's
// swatches are the colours actually painted — including rainbow derivation and
// the prefix fallback.
func (c *ColorsPage) effective() highlight.Theme {
	flat := config.Get().Flat()
	get := func(k string) (string, bool) { v, ok := flat[k]; return v, ok }
	keys := func() []string {
		out := make([]string, 0, len(flat))
		for k := range flat {
			out = append(out, k)
		}
		return out
	}
	return highlight.NewThemeKeys(c.captures(), get, keys)
}

// names lists the editable capture names, sorted: everything the active theme
// defines, the rainbow slots, and any capture a config layer names that the
// theme itself does not — an override must never become unreachable.
func (c *ColorsPage) names() []string {
	seen := map[string]bool{}
	for k := range c.captures() {
		seen[k] = true
	}
	for i := 0; i < rainbowSlots; i++ {
		seen["rainbow."+strconv.Itoa(i)] = true
	}
	// The unmatched-bracket colour (#1628) has no palette entry of its own —
	// it falls back to the theme's error slot — but must stay editable.
	seen[bracket.Unmatched] = true
	const prefix = "theme.captures."
	for k := range config.Get().Flat() {
		if name, ok := strings.CutPrefix(k, prefix); ok && name != "" {
			seen[name] = true
		}
	}
	out := make([]string, 0, len(seen))
	for k := range seen {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// rows returns the visible capture names after filtering.
func (c *ColorsPage) rows() []string {
	all := c.names()
	if c.filter == "" {
		return all
	}
	needle := strings.ToLower(c.filter)
	var out []string
	for _, n := range all {
		if strings.Contains(strings.ToLower(n), needle) {
			out = append(out, n)
		}
	}
	return out
}

// current returns the selected capture name.
func (c *ColorsPage) current() (string, bool) {
	rows := c.rows()
	if c.sel < 0 || c.sel >= len(rows) {
		return "", false
	}
	return rows[c.sel], true
}

// candidates lists the pickable colours: the named ones, then the capture's
// own theme default (so an override can be taken back to it without knowing
// the token), then the free-token entry.
func (c *ColorsPage) candidates(name string) []string {
	out := append([]string{}, theme.Names()...)
	if def, ok := c.captures()[name]; ok && def != "" && !contains(out, def) {
		out = append(out, def)
	}
	return out
}

// write persists a token for a capture at user scope and reloads, so the open
// editors recolour immediately.
func (c *ColorsPage) write(name, token string) tea.Cmd {
	c.picking, c.custom, c.invalid = false, false, ""
	c.notice = "✓ " + name + " → " + token
	return config.WriteAndReload(c.opts, config.UserScope, "theme.captures."+name, token)
}

// clear removes an override so the active theme's own colour returns.
func (c *ColorsPage) clear(name string) tea.Cmd {
	if _, has := c.override(name); !has {
		c.notice = name + " already uses the theme's own colour"
		return nil
	}
	c.picking, c.custom, c.invalid = false, false, ""
	c.notice = "✓ " + name + " back to the theme default"
	return config.RemoveAndReload(c.opts, config.UserScope, "theme.captures."+name)
}

// Update implements PageModel.
func (c *ColorsPage) Update(key tea.KeyPressMsg) tea.Cmd {
	switch {
	case c.filtering:
		return c.updateFilter(key)
	case c.custom:
		return c.updateCustom(key)
	case c.picking:
		return c.updatePicker(key)
	}
	if listNav(key.String(), &c.sel, len(c.rows()), c.navPageSize()) {
		c.notice, c.invalid = "", ""
		return nil
	}
	name, ok := c.current()
	switch key.String() {
	case "enter":
		if !ok {
			return nil
		}
		c.picking, c.pick, c.invalid = true, c.pickIndexFor(name), ""
	case "r":
		if ok {
			return c.clear(name)
		}
	case "e":
		// Straight to the free-token input for a hex or ANSI value.
		if ok {
			c.custom, c.invalid = true, ""
			cur, _ := c.override(name)
			c.input = newTextField(cur)
		}
	case "/":
		c.filtering = true
	}
	return nil
}

// pickIndexFor highlights the capture's current colour in the picker when it
// is one of the offered ones.
func (c *ColorsPage) pickIndexFor(name string) int {
	cur, _ := c.override(name)
	if cur == "" {
		cur = c.captures()[name]
	}
	for i, cand := range c.candidates(name) {
		if strings.EqualFold(cand, cur) {
			return i
		}
	}
	return 0
}

// updateFilter handles keys while the filter input is open.
func (c *ColorsPage) updateFilter(key tea.KeyPressMsg) tea.Cmd {
	switch key.Code {
	case tea.KeyEscape:
		c.filtering, c.filter, c.filterCur, c.sel = false, "", 0, 0
	case tea.KeyEnter:
		c.filtering = false
	default:
		// Shared line editing (#2002).
		if _, changed := filterKey(key, &c.filter, &c.filterCur); changed {
			c.sel = 0
		}
	}
	return nil
}

// updatePicker handles keys inside the colour list.
func (c *ColorsPage) updatePicker(key tea.KeyPressMsg) tea.Cmd {
	name, ok := c.current()
	if !ok {
		c.picking = false
		return nil
	}
	cands := c.candidates(name)
	// One past the last candidate is the custom-token row (#1666).
	if listNav(key.String(), &c.pick, len(cands)+1, c.navPageSize()) {
		return nil
	}
	switch key.String() {
	case "esc":
		c.picking = false
	case "enter":
		if c.pick >= len(cands) {
			c.picking, c.custom = false, true
			cur, _ := c.override(name)
			c.input = newTextField(cur)
			return nil
		}
		return c.write(name, cands[c.pick])
	}
	return nil
}

// updateCustom handles the free-token input, rejecting anything the resolver
// cannot parse — lipgloss would render it as the terminal default and the
// capture would silently look unstyled.
func (c *ColorsPage) updateCustom(key tea.KeyPressMsg) tea.Cmd {
	name, ok := c.current()
	if !ok {
		c.custom = false
		return nil
	}
	switch key.Code {
	case tea.KeyEscape:
		c.custom, c.invalid = false, ""
	case tea.KeyEnter:
		token := strings.TrimSpace(c.input.text)
		if token == "" {
			return c.clear(name)
		}
		if !theme.ValidToken(token) {
			c.invalid = "not a colour — use a name (" + strings.Join(theme.Names()[:3], ", ") + " …), #rrggbb or 0-255"
			return nil
		}
		return c.write(name, token)
	default:
		c.input.Handle(key)
	}
	return nil
}

// Paste implements the panel's paste routing: the free-token input, or the
// page's own capture filter while that is open (#2002).
func (c *ColorsPage) Paste(text string) bool {
	if c.filtering {
		if !filterPaste(text, &c.filter, &c.filterCur) {
			return false
		}
		c.sel = 0
		return true
	}
	if !c.custom {
		return false
	}
	return c.input.Paste(text)
}

// SearchItems implements Searchable (#886): every capture is reachable from
// the panel-wide filter.
func (c *ColorsPage) SearchItems() []SearchItem {
	var out []SearchItem
	for i, n := range c.names() {
		i, n := i, n
		out = append(out, SearchItem{
			Label:    n,
			Keywords: "colour color syntax capture highlight " + n,
			Activate: func() { c.filter, c.filterCur, c.sel = "", 0, i },
		})
	}
	return out
}

// KeyHelp implements KeyHelper (#887).
func (c *ColorsPage) KeyHelp() []string {
	return []string{
		"enter  pick a colour · e  type a token (#rrggbb or 0-255)",
		"r  back to the theme's own colour · /  filter captures",
	}
}
