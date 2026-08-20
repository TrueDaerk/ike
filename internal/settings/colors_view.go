package settings

import (
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"ike/internal/bracket"
	"ike/internal/config"
	"ike/internal/highlight"
	"ike/internal/theme"
)

// colors_view.go renders the syntax-colour page on the panel's grid (#1238):
// the capture list in the settings column, the selected capture's colour —
// where it comes from and how to change it — in the detail column. Every row
// carries a swatch painted in the colour it actually resolves to, so the list
// is legible without reading a single hex token.

// swatch is the block a colour is previewed with.
const swatch = "███"

// swatchFor renders the swatch for a capture in its effective colour.
func swatchFor(th highlight.Theme, name string) string {
	style, ok := th.Style(name)
	if !ok {
		return lipgloss.NewStyle().Faint(true).Render("···")
	}
	return lipgloss.NewStyle().Foreground(style.GetForeground()).Render(swatch)
}

// swatchToken renders the swatch for a raw colour token.
func swatchToken(token string) string {
	return lipgloss.NewStyle().Foreground(theme.Resolve(token)).Render(swatch)
}

// View implements PageModel.
func (c *ColorsPage) View(w, h int) string {
	c.setRows(h)
	listW, detailW, side := splitGrid(w)
	c.listW = 0
	list := c.renderList(listW, h)
	if !side {
		return list
	}
	c.listW = listW
	return lipgloss.JoinHorizontal(lipgloss.Top, list, columnRule(h), c.renderDetail(detailW, h))
}

// renderList renders the capture list: swatch, name, and the token in use.
func (c *ColorsPage) renderList(w, h int) string {
	pal := c.theme()
	clip := lipgloss.NewStyle().MaxWidth(w).Width(w)
	sec := lipgloss.NewStyle().Foreground(pal.Secondary)
	th := c.effective()

	head := " capture · colour"
	switch {
	case c.filtering:
		head += "   ⌕ " + filterView(c.filter, c.filterCur)
	case c.filter != "":
		head += "   ⌕ " + c.filter
	default:
		head += "   (/ to filter)"
	}

	rows := c.rows()
	list := make([]string, 0, len(rows))
	for i, name := range rows {
		token, overridden := c.override(name)
		if !overridden {
			token = c.captures()[name]
			if token == "" {
				token = "(derived)"
			}
		}
		left := " " + swatchFor(th, name) + " " + name
		gap := w - lipgloss.Width(left) - lipgloss.Width(token) - 2
		if gap < 1 {
			gap = 1
		}
		line := left + strings.Repeat(" ", gap) + token + " "
		style := lipgloss.NewStyle()
		switch {
		case i == c.sel:
			style = style.Background(pal.Selection).Foreground(pal.SelectionText).Bold(true)
		case overridden:
			style = style.Foreground(pal.Info) // an override stands out, like a schema row
		}
		list = append(list, clip.Render(style.Render(line)))
	}
	if len(rows) == 0 {
		list = append(list, clip.Render(sec.Render(" no matching captures")))
	}

	foot := wrapFooter([]footerLine{{
		text:  "   enter pick · e type a token · r theme default",
		style: sec,
	}}, w, 2)
	c.listH = h - 1 - len(foot)
	return clip.Render(sec.Render(head)) + "\n" + pinFooter(list, foot, c.sel, c.sel, h-1, &c.off)
}

// renderDetail renders the third column: what the capture is, the colour in
// use and where it comes from, and the picker or token input when open.
func (c *ColorsPage) renderDetail(w, h int) string {
	pal := c.theme()
	clip := lipgloss.NewStyle().MaxWidth(w)
	title := lipgloss.NewStyle().Foreground(pal.BorderFocus).Bold(true)
	dim := lipgloss.NewStyle().Foreground(pal.Secondary)
	errStyle := lipgloss.NewStyle().Foreground(pal.Error)

	name, ok := c.current()
	if !ok {
		lines := []string{clip.Render(title.Render(" Syntax Colors"))}
		lines = append(lines, wrapDetail(w, dim, clip,
			"One colour per Tree-sitter capture, on top of the active theme. An override survives a theme switch.")...)
		return strings.Join(padTo(lines, h), "\n")
	}

	th := c.effective()
	token, overridden := c.override(name)
	source := "the " + config.Get().Theme.Name + " theme"
	if overridden {
		source = "your config (" + config.Origin(c.opts, "theme.captures."+name) + ")"
	} else {
		token = c.captures()[name]
		if token == "" {
			token = "(derived)"
		}
	}

	lines := []string{clip.Render(title.Render(" " + name))}
	lines = append(lines, wrapDetail(w, dim, clip, captureBlurb(name))...)
	lines = append(lines, clip.Render(dim.Render(" theme.captures."+name+" · colour")))
	lines = append(lines, clip.Render(dim.Render(" "+strings.Repeat("─", maxInt(w-2, 1)))))
	lines = append(lines, clip.Render(" "+swatchFor(th, name)+"  "+token))
	lines = append(lines, wrapDetail(w, dim, clip, "from "+source)...)

	c.candTop = 0
	switch {
	case c.custom:
		lines = append(lines, "", clip.Render(" ✎ "+c.input.View()))
		if c.invalid != "" {
			lines = append(lines, wrapDetail(w, errStyle, clip, "✗ "+c.invalid)...)
		}
		lines = append(lines, wrapDetail(w, dim, clip,
			"A name, #rrggbb or an ANSI index 0-255. Empty clears the override. enter applies · esc cancels")...)
	case c.picking:
		lines = append(lines, "")
		c.candTop = len(lines)
		lines = append(lines, c.pickerLines(name, w, clip, dim)...)
	default:
		lines = append(lines, "")
		lines = append(lines, wrapDetail(w, dim, clip, "enter picks a colour · e types a token · r restores the theme's own")...)
	}

	var foot []string
	switch {
	case c.invalid != "" && !c.custom:
		foot = append(foot, clip.Render(errStyle.Render(" ✗ "+c.invalid)))
	case c.notice != "":
		foot = append(foot, clip.Render(lipgloss.NewStyle().Foreground(pal.Info).Render(" "+c.notice)))
	default:
		foot = append(foot, "")
	}
	for len(lines) < h-len(foot) {
		lines = append(lines, "")
	}
	lines = append(lines, foot...)
	return strings.Join(padTo(lines, h), "\n")
}

// pickerLines renders the colour list plus the free-token entry.
func (c *ColorsPage) pickerLines(name string, w int, clip, dim lipgloss.Style) []string {
	pal := c.theme()
	sel := lipgloss.NewStyle().Background(pal.Selection).Foreground(pal.SelectionText).Bold(true)
	cands := c.candidates(name)
	out := make([]string, 0, len(cands)+1)
	for i, cand := range cands {
		line := " " + swatchToken(cand) + " " + cand
		if i == c.pick {
			out = append(out, clip.Render(sel.Render(line)))
			continue
		}
		out = append(out, clip.Render(line))
	}
	custom := " ✎  type a token…"
	if c.pick >= len(cands) {
		out = append(out, clip.Render(sel.Render(custom)))
	} else {
		out = append(out, clip.Render(dim.Render(custom)))
	}
	return out
}

// captureBlurb explains what a capture paints. Unknown names — a grammar's own
// or a user-added override — get the generic sentence rather than nothing.
func captureBlurb(name string) string {
	if strings.HasPrefix(name, "rainbow.") {
		return "One depth slot of the rainbow bracket cycle; derived from another capture unless set."
	}
	if name == bracket.Unmatched {
		return "A bracket with no partner; the theme's error colour, underlined, unless set."
	}
	head, _, _ := strings.Cut(name, ".")
	if blurb, ok := captureBlurbs[head]; ok {
		return blurb
	}
	return "A Tree-sitter capture. Sub-captures inherit their head's colour unless set."
}

// captureBlurbs describes the capture heads the built-in themes define.
var captureBlurbs = map[string]string{
	"keyword":     "Language keywords: if, func, return, class.",
	"operator":    "Operators and their punctuation: + - == => …",
	"string":      "String literals, including their escapes' surroundings.",
	"number":      "Numeric literals.",
	"comment":     "Comments of every shape.",
	"function":    "Function and method names, at definition and at call.",
	"type":        "Type names, classes and interfaces.",
	"constant":    "Constants; constant.builtin covers the language's own (nil, true).",
	"variable":    "Variable and parameter names.",
	"property":    "Struct and object fields.",
	"label":       "Labels and goto targets.",
	"attribute":   "Attributes, annotations and decorators.",
	"punctuation": "Brackets, delimiters and separators.",
	"escape":      "Escape sequences inside strings.",
	"boolean":     "Boolean literals.",
	"tag":         "Markup tags (HTML, XML, JSX).",
	"embedded":    "Text embedded in another language's document.",
}

// Click implements the optional PageClicker seam (#674): the header opens the
// filter, a press selects a row, a press on the selection opens the picker,
// and a press on a candidate in the detail column takes it.
func (c *ColorsPage) Click(x, y int) tea.Cmd {
	if c.listW > 0 && x >= c.listW {
		if !c.picking || c.candTop == 0 {
			return nil
		}
		name, ok := c.current()
		if !ok {
			return nil
		}
		cands := c.candidates(name)
		switch opt := y - c.candTop; {
		case opt < 0 || opt > len(cands):
			return nil
		case opt == len(cands):
			c.picking, c.custom = false, true
			cur, _ := c.override(name)
			c.input = newTextField(cur)
		default:
			c.pick = opt
			return c.write(name, cands[opt])
		}
		return nil
	}
	if c.Capturing() {
		return nil
	}
	if y == 0 {
		c.filtering = true
		return nil
	}
	row := y - 1
	if row < 0 || (c.listH > 0 && row >= c.listH) {
		return nil
	}
	idx := row + c.off
	if idx >= len(c.rows()) {
		return nil
	}
	if idx == c.sel {
		name, ok := c.current()
		if !ok {
			return nil
		}
		c.picking, c.pick = true, c.pickIndexFor(name)
		return nil
	}
	c.sel = idx
	return nil
}

// Wheel implements the optional PageWheeler seam (#674).
func (c *ColorsPage) Wheel(delta int) {
	if c.Capturing() {
		return
	}
	if c.picking {
		name, ok := c.current()
		if !ok {
			return
		}
		c.pick = clamp(c.pick+delta, 0, len(c.candidates(name)))
		return
	}
	if n := len(c.rows()); n > 0 {
		c.sel = clamp(c.sel+delta, 0, n-1)
	}
}
