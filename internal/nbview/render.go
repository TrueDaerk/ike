package nbview

// render.go turns the parsed notebook into the pane's rows (#2425). Rendering
// is done once per size/theme/fold/content change rather than per frame: a
// markdown cell costs a full glamour render and a code cell a tree-sitter
// parse, neither of which belongs in View.

import (
	"fmt"
	"image/color"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"ike/internal/highlight"
	"ike/internal/imgview"
	"ike/internal/preview"
)

// tabWidth is what a tab in cell source expands to. Notebooks carry no
// indent settings of their own, and 4 is the near-universal convention of the
// languages notebooks are written in.
const tabWidth = 4

// render rebuilds the row list for the current width, theme and fold state.
func (m *Model) render() {
	if m.w <= 0 || m.err != nil {
		return
	}
	m.ensureTheme()
	gw := m.gutterWidth()
	width := max(10, m.w-gw)
	m.rows = m.rows[:0]
	placed := map[imgKey]bool{}
	for ci, c := range m.nb.Cells {
		if ci > 0 {
			m.rows = append(m.rows, row{kind: rowGap, cell: -1, src: -1})
		}
		m.appendCell(ci, c, width, placed)
	}
	m.forgetUnplaced(placed)
	m.clampScroll()
}

// ensureTheme (re)builds the capture→style table for the active palette.
func (m *Model) ensureTheme() {
	pal := m.palette()
	if m.hlOK && m.hlName == pal.Name {
		return
	}
	m.hl = highlight.NewTheme(pal.Captures, nil)
	m.hlName, m.hlOK = pal.Name, true
}

// appendCell renders one cell: its source, then its outputs (or the fold
// marker standing in for them). Every cell contributes at least one row, so
// an empty cell still shows its gutter label instead of vanishing.
func (m *Model) appendCell(ci int, c Cell, width int, placed map[imgKey]bool) {
	before := len(m.rows)
	switch c.Type {
	case CellMarkdown:
		m.appendMarkdown(ci, c, width)
	case CellCode:
		m.appendCode(ci, c, width)
	default:
		m.appendPlain(ci, c, width)
	}
	if len(m.rows) == before {
		m.note(ci, width, "(empty "+strings.TrimSuffix(m.cellKindWord(c), " ")+" cell)")
	}
	m.appendOutputs(ci, c, width, placed)
	if len(m.rows) > before {
		m.rows[before].first = true
	}
}

// cellKindWord names a cell type in prose, for the empty-cell placeholder.
func (m *Model) cellKindWord(c Cell) string {
	switch c.Type {
	case CellMarkdown:
		return "markdown"
	case CellCode:
		return "code"
	}
	return "raw"
}

// appendMarkdown renders a markdown cell through the markdown preview's
// renderer, so a notebook's prose reads exactly like a previewed .md file.
// A renderer failure degrades to the raw source rather than to nothing.
func (m *Model) appendMarkdown(ci int, c Cell, width int) {
	if strings.TrimSpace(c.Source) == "" {
		return
	}
	out, err := preview.Render(c.Source, width, m.palette())
	if err != nil {
		m.appendPlain(ci, c, width)
		return
	}
	for _, line := range strings.Split(strings.Trim(out, "\n"), "\n") {
		m.rows = append(m.rows, row{
			text:  clipTo(line, width),
			plain: clipTo(ansi.Strip(line), width),
			kind:  rowSource, cell: ci, src: -1,
		})
	}
}

// appendCode renders a code cell's source highlighted under the notebook's
// language. A notebook that declares no language, one with no compiled-in
// grammar, or a cell too large to be worth parsing renders plain — the
// fallback that keeps every notebook readable.
func (m *Model) appendCode(ci int, c Cell, width int) {
	if c.Source == "" {
		return
	}
	lines := strings.Split(c.Source, "\n")
	for i, l := range lines {
		lines[i] = expandTabs(l)
	}
	var ix highlight.Index
	if tag := strings.ToLower(m.nb.Lang); tag != "" && len(c.Source) <= maxHighlightBytes && highlight.FencedSupported(tag) {
		ix = highlight.NewIndex(highlight.HighlightFenced(tag, lines))
	}
	for i, l := range lines {
		plain := clipTo(l, width)
		m.rows = append(m.rows, row{
			text:  m.styleCode(ix, i, plain),
			plain: plain,
			kind:  rowSource, cell: ci, src: i,
		})
	}
}

// appendPlain renders a cell's source verbatim: raw cells, and the fallback
// for a markdown cell the renderer refused.
func (m *Model) appendPlain(ci int, c Cell, width int) {
	if c.Source == "" {
		return
	}
	st := lipgloss.NewStyle().Foreground(m.palette().Foreground)
	for i, l := range strings.Split(c.Source, "\n") {
		plain := clipTo(expandTabs(l), width)
		m.rows = append(m.rows, row{
			text: st.Render(plain), plain: plain,
			kind: rowSource, cell: ci, src: i,
		})
	}
}

// styleCode paints one source line from the capture index, segment by
// segment. An uncaptured run — or a build without the grammar — renders as
// plain foreground text.
func (m *Model) styleCode(ix highlight.Index, ln int, text string) string {
	runes := []rune(text)
	plainStyle := lipgloss.NewStyle().Foreground(m.palette().Foreground)
	var b strings.Builder
	for col := 0; col < len(runes); {
		capture := ix.CaptureAt(ln, col)
		end := col + 1
		for end < len(runes) && ix.CaptureAt(ln, end) == capture {
			end++
		}
		seg := string(runes[col:end])
		if st, ok := m.hl.Style(capture); ok {
			b.WriteString(st.Render(seg))
		} else {
			b.WriteString(plainStyle.Render(seg))
		}
		col = end
	}
	return b.String()
}

// appendOutputs renders a code cell's outputs below its source, or the fold
// marker when they are collapsed. Nothing but code cells carries outputs.
func (m *Model) appendOutputs(ci int, c Cell, width int, placed map[imgKey]bool) {
	if len(c.Outputs) == 0 {
		return
	}
	if m.folded[ci] {
		m.note(ci, width, fmt.Sprintf("▸ %s folded — enter to expand", plural(len(c.Outputs), "output")))
		return
	}
	for oi, o := range c.Outputs {
		m.appendOutput(ci, oi, o, width, placed)
	}
}

// appendOutput renders one output: a label row naming what it is, then its
// body — text, error lines, or an image placement.
func (m *Model) appendOutput(ci, oi int, o Output, width int, placed map[imgKey]bool) {
	pal := m.palette()
	switch {
	case o.HasImage():
		m.appendImage(ci, oi, o, width, placed)
	case o.Type == OutError:
		head := strings.TrimSpace(o.Ename + ": " + o.Evalue)
		m.label(ci, width, pal.Error, head)
		st := lipgloss.NewStyle().Foreground(pal.Error)
		for _, l := range o.Traceback {
			for _, tl := range strings.Split(ansi.Strip(l), "\n") {
				plain := clipTo(expandTabs(tl), width)
				m.rows = append(m.rows, row{text: st.Render(plain), plain: plain,
					kind: rowError, cell: ci, src: -1})
			}
		}
	default:
		m.label(ci, width, pal.Ghost, outputLabel(o))
		fg := pal.Foreground
		if o.Type == OutStream && o.Name == "stderr" {
			fg = pal.Warning
		}
		st := lipgloss.NewStyle().Foreground(fg)
		for _, l := range strings.Split(o.Text, "\n") {
			plain := clipTo(expandTabs(l), width)
			m.rows = append(m.rows, row{text: st.Render(plain), plain: plain,
				kind: rowOutput, cell: ci, src: -1})
		}
	}
}

// outputLabel names a textual output in the dim label row above it.
func outputLabel(o Output) string {
	switch {
	case o.Type == OutStream && o.Name != "":
		return o.Name
	case o.Type == OutStream:
		return "stream"
	case o.FromHTML && o.ExecCount > 0:
		return fmt.Sprintf("Out[%d] · text/html as text", o.ExecCount)
	case o.FromHTML:
		return "text/html as text"
	case o.ExecCount > 0:
		return fmt.Sprintf("Out[%d]", o.ExecCount)
	}
	return "output"
}

// label appends one dim marker row introducing an output.
func (m *Model) label(ci, width int, fg color.Color, text string) {
	plain := clipTo(text, width)
	m.rows = append(m.rows, row{
		text:  lipgloss.NewStyle().Foreground(fg).Faint(true).Render(plain),
		plain: plain, kind: rowNote, cell: ci, src: -1,
	})
}

// note appends one dim viewer-generated row (fold markers, placeholders).
func (m *Model) note(ci, width int, text string) {
	m.label(ci, width, m.palette().Ghost, text)
}

// appendImage renders an image output: its metadata label, then the Kitty
// placeholder block on a supporting terminal. Everywhere else the label is
// the whole output — the same degradation the image pane and the markdown
// preview apply.
func (m *Model) appendImage(ci, oi int, o Output, width int, placed map[imgKey]bool) {
	key := imgKey{cell: ci, out: oi}
	im := m.image(key, o)
	if im == nil {
		m.note(ci, width, o.MIME+" · undecodable image output")
		return
	}
	placed[key] = true
	m.label(ci, width, m.palette().Ghost, im.meta(o.MIME))
	if !m.gfx {
		im.cols, im.rows = 0, 0
		return
	}
	im.cols, im.rows = imgview.FitGrid(im.imgW, im.imgH, max(1, width), max(1, m.bodyRows()-2))
	for _, grid := range imgview.PlaceholderGrid(im.id, im.cols, im.rows) {
		m.rows = append(m.rows, row{text: grid, plain: "", kind: rowImage, cell: ci, src: -1})
	}
}

// plural formats a count with its noun.
func plural(n int, noun string) string {
	if n == 1 {
		return "1 " + noun
	}
	return fmt.Sprintf("%d %ss", n, noun)
}

// expandTabs replaces tabs with spaces to the next tab stop, so a column
// index in the highlight spans lines up with the rendered cell.
func expandTabs(s string) string {
	if !strings.ContainsRune(s, '\t') {
		return s
	}
	var b strings.Builder
	col := 0
	for _, r := range s {
		if r != '\t' {
			b.WriteRune(r)
			col++
			continue
		}
		n := tabWidth - col%tabWidth
		b.WriteString(strings.Repeat(" ", n))
		col += n
	}
	return b.String()
}
