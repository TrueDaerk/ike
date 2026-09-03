// Package codepreview is the shared code-preview column: the source excerpt a
// picker shows next to its result list, so one sees where the selection leads
// before jumping there. It arrived with the find-in-path overlay and the
// find-usages popup (#2047) and is now the single implementation every picker
// pointing at file positions uses (#2053) — the symbol and class pickers, the
// '@' file finder, the bookmarks list and the call-hierarchy overlay.
//
// Since #2327 the column is a read-only mini editor rather than a text dump:
// the excerpt is syntax-highlighted through internal/highlight, carries a
// line-number gutter, marks the hit line with a full-width current-line
// background (the match ranges keeping their emphasis on top of the syntax
// colors), and — while focused — scrolls vertically through the whole file and
// horizontally along long lines. Nothing about it edits: it is a viewport.
//
// Four pieces make up the component: Target, the per-row source location a
// picker stores; the geometry (SplitWidth with Cache.Natural, so the column
// adapts to the code around the hit within [MinPreviewWidth, MaxPreviewWidth]);
// Cache, which owns the viewport — Render for the raw rows, Columns for the
// whole two-column body (list, vertical rule, excerpt); and Cache.Key, the
// editor-like scroll keys a focused preview consumes.
//
// It is the plain-text sibling of internal/preview, which is the live markdown
// preview pane; nothing is shared between the two.
//
// It reads only the window it needs, caches the last one — raw and styled — so
// following the cursor through a list does not re-read or re-parse the file for
// every frame, and turns unreadable or deleted files into a dim notice instead
// of an error.
package codepreview

import (
	"bufio"
	"image/color"
	"os"
	"strconv"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"ike/internal/highlight"
	"ike/internal/theme"
	"ike/internal/ui"
)

// maxLineBytes caps one scanned line; longer lines are cut rather than
// failing the whole read (minified sources).
const maxLineBytes = 1 << 20

// Unavailable is the notice rendered in place of the excerpt when the target
// file cannot be read (deleted, unreadable, a directory).
const Unavailable = "preview unavailable"

// tabWidth is what one tab expands to before the excerpt is parsed, so the
// highlight spans, the match ranges and the rendered cells share one
// coordinate system. hStep, one horizontal scroll step, is the same width.
const (
	tabWidth = 4
	hStep    = tabWidth
)

// Range is one match range within a line: 0-based rune offsets, half-open.
// It mirrors locations.Range, which is where the finder's ranges come from —
// the component keeps its own type so it stays independent of the list.
type Range struct {
	Start int
	End   int
}

// Target is a source location a preview column points at. Line is 1-based,
// matching what Render expects; the zero value (empty Path) renders an empty
// column. Ranges are the match ranges on Line (#2327) — they keep their match
// emphasis on top of the syntax colors; leaving them empty simply renders the
// hit line highlighted without an inner mark.
//
// It is the row-side half of the component: every picker that offers a preview
// stores one per row and hands the selected one to Render.
type Target struct {
	Path   string
	Line   int
	Ranges []Range
}

// TargetFrom builds a Target from a row's path, line and its own range type,
// via bounds — so a picker's list component (locations.Item's Ranges, or
// whatever a future one uses) never has to hand codepreview its own Range
// type. Every range whose bounds is not a proper (End > Start) span is
// dropped: a picker that recorded an empty or reversed range has nothing to
// mark, and this is the one place that filter needs writing. Shared by the
// find-in-path overlay (allfind) and the find-usages popup (finder), whose
// previewTarget was otherwise byte-for-byte the same function.
func TargetFrom[T any](path string, line int, ranges []T, bounds func(T) (start, end int)) Target {
	out := make([]Range, 0, len(ranges))
	for _, rg := range ranges {
		start, end := bounds(rg)
		if end > start {
			out = append(out, Range{Start: start, End: end})
		}
	}
	return Target{Path: path, Line: line, Ranges: out}
}

// same reports whether two targets point at the same line of the same file —
// the test Render uses to decide whether the selection moved, which resets the
// viewport. Ranges do not participate: they describe one line's content, not
// which line is shown.
func (t Target) same(o Target) bool { return t.Path == o.Path && t.Line == o.Line }

// Cache is the preview's viewport: the last window read from disk, the styled
// rows built from it, and the scroll offsets a focused preview walks the file
// with. Repeated renders of the same target — every frame while the selection
// sits on one row — hit memory instead of the filesystem and the parser. The
// zero value is ready to use.
type Cache struct {
	// The raw window last read: lines [from, to] of path.
	path     string
	from, to int
	lines    []string
	ok       bool
	loaded   bool

	// The styled rows built from that window, keyed by everything that
	// changes their appearance (window, hit line, ranges, palette).
	stKey  string
	stRows []string

	// eof is the last line of eofPath, once a short read proved where the
	// file ends; 0 means not known yet. It bounds the scroll so walking down
	// stops at the last line instead of running into blank rows — and costs
	// no full-file scan, except on the explicit end-of-file jump.
	eofPath string
	eof     int

	// Natural's memo: the adaptive width of one target's window.
	natPath string
	natLine int
	natH    int
	natW    int

	// The viewport proper. top is the first visible line; 0 means "derive it
	// from the anchor on the next render", which is how a moved selection
	// re-centers. hoff is the horizontal offset in display cells.
	anchor  Target
	top     int
	hoff    int
	focused bool
	viewH   int // rows of the last render — the page size of the scroll keys
	viewW   int // content cells of the last render

	// The capture→style theme, rebuilt when the palette changes.
	hl     highlight.Theme
	hlName string
	hlOK   bool
}

// Reset drops the cached window and the viewport, forcing the next Render to
// re-read and re-center.
func (c *Cache) Reset() { *c = Cache{} }

// Focused reports whether the preview owns the scroll keys (see Key).
func (c *Cache) Focused() bool { return c.focused }

// SetFocus focuses or blurs the preview. A blurred preview ignores Key, so a
// host can hand every key to it unconditionally.
func (c *Cache) SetFocus(on bool) { c.focused = on }

// ToggleFocus flips the focus and reports the new state.
func (c *Cache) ToggleFocus() bool { c.focused = !c.focused; return c.focused }

// FocusKey and FocusKeyAlt are the chord every host binds to "hand the
// keyboard to the excerpt" (#2327). alt+p reads as "preview"; ctrl+e is its
// alias for macOS, where Option is a composition key and alt chords never
// reach the terminal (#422). Both are free in every overlay that carries the
// column — the palette already spends ctrl+p on moving the selection — so the
// toggle is the same everywhere.
const (
	FocusKey    = "alt+p"
	FocusKeyAlt = "ctrl+e"
)

// IsFocusKey reports whether a key string is the focus toggle.
func IsFocusKey(key string) bool { return key == FocusKey || key == FocusKeyAlt }

// Key consumes one editor-like scroll key while the preview is focused,
// reporting whether it took it (#2327). The motions are the editor's own,
// without any of its editing: j/k (and the arrows) one line, ctrl+d/ctrl+u
// half a page, ctrl+f/ctrl+b (and pgup/pgdown) a full page, h/l one tab of
// columns, 0 back to column one, $ to the end of the widest visible line, g/G
// to the file's head and tail, and z back to the hit the list selected.
func (c *Cache) Key(key string) bool {
	if !c.focused || c.anchor.Path == "" {
		return false
	}
	page := max(1, c.viewH)
	switch key {
	case "j", "down":
		c.Scroll(1)
	case "k", "up":
		c.Scroll(-1)
	case "ctrl+d":
		c.Scroll(max(1, page/2))
	case "ctrl+u":
		c.Scroll(-max(1, page/2))
	case "ctrl+f", "pgdown":
		c.Scroll(page)
	case "ctrl+b", "pgup":
		c.Scroll(-page)
	case "l", "right":
		c.hoff += hStep
	case "h", "left":
		c.hoff = max(0, c.hoff-hStep)
	case "0", "home":
		c.hoff = 0
	case "$", "end":
		c.hoff = max(0, c.widest()-max(1, c.viewW))
	case "g":
		c.top, c.hoff = 1, 0
	case "G":
		c.top, c.hoff = max(1, c.lineCount(c.anchor.Path)-page+1), 0
	case "z":
		// Back to the hit: the same window the list's selection anchors.
		c.top, c.hoff = 0, 0
	default:
		return false
	}
	return true
}

// Scroll moves the window by delta lines, materialising the anchored top
// first so the first scroll after a selection change starts from what is
// shown. It is the mouse wheel's seam into the viewport; the keys go through
// Key.
func (c *Cache) Scroll(delta int) {
	if c.top < 1 {
		c.top = centerTop(c.anchor.Line, max(1, c.viewH))
	}
	c.top = c.clampTop(c.anchor.Path, c.top+delta, max(1, c.viewH))
}

// widest is the display width of the widest line in the current window — how
// far right scrolling has anything to show.
func (c *Cache) widest() int {
	w := 0
	for _, l := range c.lines {
		if n := ansi.StringWidth(expandTabs(l)); n > w {
			w = n
		}
	}
	return w
}

// window returns lines [from, to] (1-based, inclusive) of path; the slice is
// shorter than the range when the file ends first. ok is false when the file
// could not be read at all.
func (c *Cache) window(path string, from, to int) (lines []string, ok bool) {
	if c.loaded && c.path == path && c.from == from && c.to == to {
		return c.lines, c.ok
	}
	lines, ok = readWindow(path, from, to)
	c.path, c.from, c.to, c.lines, c.ok, c.loaded = path, from, to, lines, ok, true
	return lines, ok
}

// readWindow reads lines [from, to] of path without slurping the whole file.
func readWindow(path string, from, to int) (lines []string, ok bool) {
	if path == "" || from < 1 || to < from {
		return nil, false
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, false
	}
	defer f.Close()
	if st, err := f.Stat(); err != nil || st.IsDir() {
		return nil, false
	}
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), maxLineBytes)
	for n := 1; n <= to; n++ {
		if !sc.Scan() {
			// A read error mid-window still yields what we already have;
			// only a file that gave us nothing counts as unavailable.
			if sc.Err() != nil && len(lines) == 0 {
				return nil, false
			}
			break
		}
		if n >= from {
			lines = append(lines, sc.Text())
		}
	}
	return lines, true
}

// lineCount counts path's lines, memoising the answer as the known end of
// file. It is the one full-file read the component ever does — the explicit
// jump to the tail (G) is the only caller.
func (c *Cache) lineCount(path string) int {
	if c.eofPath == path && c.eof > 0 {
		return c.eof
	}
	f, err := os.Open(path)
	if err != nil {
		return 1
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), maxLineBytes)
	n := 0
	for sc.Scan() {
		n++
	}
	if n == 0 {
		return 1
	}
	c.noteEOF(path, n)
	return n
}

// noteEOF records where path ends, so clampTop can bound the scroll.
func (c *Cache) noteEOF(path string, line int) {
	if line < 1 {
		line = 1
	}
	c.eofPath, c.eof = path, line
}

// clampTop bounds a first-visible line to the file: never above line one,
// never so far down that the known last line scrolls off the top.
func (c *Cache) clampTop(path string, top, height int) int {
	if c.eofPath == path && c.eof > 0 {
		if maxTop := c.eof - height + 1; top > maxTop {
			top = maxTop
		}
	}
	return max(1, top)
}

// centerTop is the window start that centers line in a height-row viewport,
// never scrolling past the top of the file — a match on line 2 shows the
// file's head, not blank rows.
func centerTop(line, height int) int {
	return max(1, line-(height-1)/2)
}

// Render lays a width×height excerpt of t out — syntax-highlighted, with a
// line-number gutter, the hit line highlighted across the full width and the
// scroll offsets a focused preview accumulated applied — returning exactly
// height rows (blank-padded) so the caller's popup keeps a stable size.
//
// A target on a different line than the last one re-centers the viewport, so
// moving the selection in the result list always lands the new hit in view. An
// empty path or an unreadable file renders the Unavailable notice; nothing
// here ever fails.
func (c *Cache) Render(t Target, width, height int, pal *theme.Palette) []string {
	if height < 1 || width < 4 {
		return nil
	}
	if pal == nil {
		pal = theme.DefaultPalette()
	}
	pad := func(rows []string) []string {
		for len(rows) < height {
			rows = append(rows, "")
		}
		return rows[:height]
	}
	if t.Path == "" {
		c.anchor, c.top, c.hoff = Target{}, 0, 0
		return pad(nil)
	}
	if t.Line < 1 {
		t.Line = 1
	}
	if !t.same(c.anchor) {
		// The selection moved: re-center on the new hit and drop the offsets.
		c.top, c.hoff = 0, 0
	}
	c.anchor = t
	c.viewH = height

	from := c.top
	if from < 1 {
		from = centerTop(t.Line, height)
	}
	from = c.clampTop(t.Path, from, height)
	lines, ok := c.window(t.Path, from, from+height-1)
	if ok && len(lines) < height {
		// A short read proves where the file ends: clamp onto its tail so a
		// scrolled viewport shows lines instead of blank rows.
		c.noteEOF(t.Path, from+len(lines)-1)
		if want := c.clampTop(t.Path, from, height); want < from {
			from = want
			lines, ok = c.window(t.Path, from, from+height-1)
		}
	}
	c.top = from
	dim := lipgloss.NewStyle().Foreground(pal.Border)
	if !ok {
		return pad([]string{dim.Render(ansi.Truncate(Unavailable, width, "…"))})
	}

	gw := len(strconv.Itoa(max(from+height-1, 1)))
	cw := width - gw - 1
	if cw < 1 {
		gw, cw = 0, width
	}
	c.viewW = cw
	if c.hoff > 0 {
		// Never scroll further right than the window has content.
		c.hoff = min(c.hoff, max(0, c.widest()-1))
	}

	hit := lipgloss.NewStyle().Background(pal.SelectionMuted)
	styled := c.styled(t, lines, from, pal)
	rows := make([]string, 0, height)
	for i, body := range styled {
		n := from + i
		gutter := ""
		if gw > 0 {
			num := strconv.Itoa(n)
			gutter = strings.Repeat(" ", gw-len(num)) + num + " "
		}
		body = ansi.Cut(body, c.hoff, c.hoff+cw)
		if n != t.Line {
			rows = append(rows, dim.Render(gutter)+body)
			continue
		}
		// The hit line carries the current-line background across the whole
		// column, gutter included, whatever the excerpt's own width.
		if gap := cw - ansi.StringWidth(body); gap > 0 {
			body += hit.Render(strings.Repeat(" ", gap))
		}
		rows = append(rows, hit.Render(gutter)+body)
	}
	return pad(rows)
}

// styled returns the window's rows with syntax colors, the hit line's
// background and the match emphasis baked in — unclipped, so the horizontal
// offset can slice them per frame. The parse is memoised: only a changed
// window, hit line, match set or palette re-runs it.
func (c *Cache) styled(t Target, lines []string, from int, pal *theme.Palette) []string {
	key := styleKey(t, from, len(lines), pal)
	if c.stKey == key && len(c.stRows) == len(lines) {
		return c.stRows
	}
	c.ensureTheme(pal)
	exp := make([]string, len(lines))
	for i, l := range lines {
		exp[i] = expandTabs(l)
	}
	// The excerpt is parsed standalone, exactly like the definition peek and
	// the hover code fences do (#379): a file whose language has no grammar —
	// or a build without tree-sitter — yields no spans and renders plain.
	ix := highlight.NewIndex(highlight.Highlight(t.Path, exp))
	rows := make([]string, len(lines))
	for i, l := range exp {
		var ranges []Range
		var bg color.Color
		if from+i == t.Line {
			ranges = expandRanges(lines[i], t.Ranges)
			bg = pal.SelectionMuted
		}
		rows[i] = c.styleLine(ix, i, l, ranges, bg)
	}
	c.stKey, c.stRows = key, rows
	return rows
}

// styleKey spells everything that changes a window's styled rows.
func styleKey(t Target, from, n int, pal *theme.Palette) string {
	var b strings.Builder
	b.WriteString(t.Path)
	b.WriteByte(0)
	for _, v := range []int{from, n, t.Line} {
		b.WriteString(strconv.Itoa(v))
		b.WriteByte(':')
	}
	for _, r := range t.Ranges {
		b.WriteString(strconv.Itoa(r.Start) + "-" + strconv.Itoa(r.End) + ",")
	}
	b.WriteByte(0)
	b.WriteString(pal.Name)
	return b.String()
}

// styleLine renders one source line: the capture colors from ix, the match
// ranges bold and underlined on top of them (the list's own match emphasis),
// all over bg when the line is the hit. A capture the theme does not style,
// with no background and no match, renders as plain text — the fallback that
// keeps unsupported languages readable.
func (c *Cache) styleLine(ix highlight.Index, ln int, text string, ranges []Range, bg color.Color) string {
	runes := []rune(text)
	var b strings.Builder
	for col := 0; col < len(runes); {
		capture := ix.CaptureAt(ln, col)
		hot := inRanges(ranges, col)
		end := col + 1
		for end < len(runes) && ix.CaptureAt(ln, end) == capture && inRanges(ranges, end) == hot {
			end++
		}
		seg := string(runes[col:end])
		st, styled := c.hl.Style(capture)
		if !styled {
			st = lipgloss.NewStyle()
		}
		if bg != nil {
			st, styled = st.Background(bg), true
		}
		if hot {
			st, styled = st.Bold(true).Underline(true), true
		}
		if styled {
			seg = st.Render(seg)
		}
		b.WriteString(seg)
		col = end
	}
	return b.String()
}

// ensureTheme (re)builds the capture→style table for pal.
func (c *Cache) ensureTheme(pal *theme.Palette) {
	if c.hlOK && c.hlName == pal.Name {
		return
	}
	c.hl = highlight.NewTheme(pal.Captures, nil)
	c.hlName, c.hlOK = pal.Name, true
}

// inRanges reports whether col falls inside any (half-open) range.
func inRanges(ranges []Range, col int) bool {
	for _, r := range ranges {
		if col >= r.Start && col < r.End {
			return true
		}
	}
	return false
}

// expandTabs flattens one source line for display: tabs become tabWidth
// spaces, stray carriage returns drop out and an embedded newline — a match
// text spanning lines — becomes a space, so one source line stays one row.
func expandTabs(s string) string {
	if !strings.ContainsAny(s, "\t\r\n") {
		return s
	}
	return strings.NewReplacer("\t", strings.Repeat(" ", tabWidth), "\r", "", "\n", " ").Replace(s)
}

// expandRanges maps match ranges of the raw line onto the tab-expanded one, so
// an indented hit keeps its emphasis over the right columns.
func expandRanges(raw string, ranges []Range) []Range {
	if len(ranges) == 0 || !strings.Contains(raw, "\t") {
		return ranges
	}
	runes := []rune(raw)
	// shift[i] is the display column the i-th rune starts at.
	shift := make([]int, len(runes)+1)
	col := 0
	for i, r := range runes {
		shift[i] = col
		if r == '\t' {
			col += tabWidth
			continue
		}
		col++
	}
	shift[len(runes)] = col
	at := func(i int) int {
		if i < 0 {
			return 0
		}
		if i >= len(shift) {
			return col
		}
		return shift[i]
	}
	out := make([]Range, 0, len(ranges))
	for _, r := range ranges {
		out = append(out, Range{Start: at(r.Start), End: at(r.End)})
	}
	return out
}

// Column geometry, shared by every picker that carries the preview (#2053).
// Below MinSplitWidth the box is too narrow to hold two columns and the list
// keeps the whole width.
//
// Above it the preview adapts to the code around the hit (#2327): it wants
// Cache.Natural cells — the widest line of the window around the hit plus its
// gutter — bounded to [MinPreviewWidth, MaxPreviewWidth] and then clamped to
// what the box has to spare, which is the smaller of half the content and
// whatever leaves the list MinListWidth cells. A column that would end up
// below MinColumnWidth is dropped entirely.
const (
	MinSplitWidth   = 64
	MinPreviewWidth = 50
	MaxPreviewWidth = 120
	MinColumnWidth  = 20
	MinListWidth    = 40
)

// dividerWidth is what the vertical rule plus its two spaces of air cost.
const dividerWidth = 3

// SplitWidth divides an inner content width into the list column and a
// preview column that wants natural cells. previewW is 0 when the box is too
// narrow to split, in which case listW is the whole width and the caller
// renders its list alone.
func SplitWidth(inner, natural int) (listW, previewW int) {
	if inner < MinSplitWidth {
		return inner, 0
	}
	previewW = min(max(natural, MinPreviewWidth), MaxPreviewWidth)
	// The list keeps the larger share, and never less than MinListWidth.
	room := min((inner-dividerWidth)/2, inner-dividerWidth-MinListWidth)
	previewW = min(previewW, room)
	if previewW < MinColumnWidth {
		return inner, 0
	}
	return inner - previewW - dividerWidth, previewW
}

// Split is SplitWidth for a caller with no target at hand: the preview takes
// its minimum width.
func Split(inner int) (listW, previewW int) { return SplitWidth(inner, MinPreviewWidth) }

// Natural is the width the preview would like for t: the widest line of the
// height-row window around the hit plus the line-number gutter, bounded to
// [MinPreviewWidth, MaxPreviewWidth]. It is measured around the hit, not
// around the scrolled window, so scrolling a focused preview never resizes the
// columns under the cursor. The answer is memoised per target.
func (c *Cache) Natural(t Target, height int) int {
	if t.Path == "" || height < 1 {
		return MinPreviewWidth
	}
	line := max(1, t.Line)
	if c.natPath == t.Path && c.natLine == line && c.natH == height {
		return c.natW
	}
	from := centerTop(line, height)
	to := from + height - 1
	w := MinPreviewWidth
	if lines, ok := readWindow(t.Path, from, to); ok {
		longest := 0
		for _, l := range lines {
			if n := ansi.StringWidth(expandTabs(l)); n > longest {
				longest = n
			}
		}
		w = len(strconv.Itoa(to)) + 1 + longest
	}
	w = min(max(w, MinPreviewWidth), MaxPreviewWidth)
	c.natPath, c.natLine, c.natH, c.natW = t.Path, line, height, w
	return w
}

// SplitFor is the geometry for one target: SplitWidth fed with the width the
// preview of t wants. It is what a host calls in View and in its click
// mapping, so both agree on where the columns lie.
func (c *Cache) SplitFor(inner, height int, t Target) (listW, previewW int) {
	return SplitWidth(inner, c.Natural(t, height))
}

// Columns is the whole two-column body of a picker (#2053): the list rows
// blank-padded to height on the left, a vertical rule — accented while the
// preview holds the scroll keys (#2327) — and the excerpt of t on the right. A
// previewW of 0 (a box too narrow to split, see SplitWidth) returns the padded
// list alone, so a caller can pass the geometry straight through without
// branching.
func (c *Cache) Columns(left []string, listW, previewW, height int, t Target, pal *theme.Palette) string {
	left = ui.PadRows(left, height)
	if previewW <= 0 {
		return strings.Join(left, "\n")
	}
	if pal == nil {
		pal = theme.DefaultPalette()
	}
	ruleColor := pal.Border
	if c.focused {
		ruleColor = pal.BorderFocus
	}
	rule := lipgloss.NewStyle().Foreground(ruleColor).Render("│")
	return ui.JoinColumns(left, listW, rule, c.Render(t, previewW, height, pal))
}
