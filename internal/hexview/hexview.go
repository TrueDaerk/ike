// Package hexview is the read-only hex viewer pane (#2420): the classic
// offset | hex | ASCII layout over windowed reads, so a multi-gigabyte file
// opens as fast as a small one — only the visible bytes are ever read. It
// carries a byte cursor with an inspector row (integer/float/rune decodings
// of the bytes under the cursor), a range selection with copy-as-hex or
// copy-as-raw, and a search for ASCII/UTF-8 strings or hex byte sequences.
//
// The buffer is deliberately a (path, size, window) triple rather than a byte
// slice: a later write mode can grow an overlay of edited ranges on top
// without touching the read path.
package hexview

import (
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"ike/internal/theme"
	"ike/internal/ui"
)

// windowSize is the size of the cached read window. It comfortably covers a
// screenful at every row width plus scroll headroom, while staying far below
// anything a multi-gigabyte file could make expensive.
const windowSize = 256 * 1024

// maxCopy caps a selection copy at 1 MiB of raw bytes: the clipboard is a
// text channel, not a file transfer.
const maxCopy = 1 << 20

// maxMatches caps how many search-match offsets are enumerated; beyond it the
// counter reports the cap with a "+" and stepping keeps working over the
// enumerated set.
const maxMatches = 100_000

// CopyMsg asks the root model to place text on the system clipboard (#2420):
// the pane emits it from the copy menu and the app owns the clipboard, like
// the data viewer's export does.
type CopyMsg struct {
	Text string
	What string // human label for the "copied …" notice
}

// Model is one hex viewer pane bound to a file path.
type Model struct {
	key  string
	path string
	pal  *theme.Palette

	f    *os.File
	size int64
	err  error

	w, h    int
	focused bool

	perRow int   // bytes per row: 8, 16 or 32, from the pane width
	top    int64 // first visible byte offset, always a multiple of perRow
	cur    int64 // cursor byte offset
	anchor int64 // selection anchor offset, -1 while nothing is selected

	// search line state (#2409/#2410 shape shared with the other panes).
	sEditing bool
	sInput   string
	sCur     int
	sErr     string
	needle   []byte  // applied byte sequence, nil while no search applies
	matches  []int64 // enumerated match offsets, capped at maxMatches
	capped   bool    // true when the enumeration hit the cap
	mIdx     int     // index of the current match in matches, -1 none
	wrapped  bool

	// copy menu state: a two-row picker over the selection's copy format.
	copyMenu bool
	copyCur  int

	// cached read window.
	win    []byte
	winOff int64
}

// New opens the file at path for windowed reading. Open/stat errors are kept
// for View — the pane opens either way and explains itself.
func New(key, path string, pal *theme.Palette) Model {
	m := Model{key: key, path: path, pal: pal, perRow: 16, anchor: -1, mIdx: -1}
	f, err := os.Open(path)
	if err != nil {
		m.err = err
		return m
	}
	st, err := f.Stat()
	if err != nil {
		f.Close()
		m.err = err
		return m
	}
	m.f = f
	m.size = st.Size()
	return m
}

// Key returns the pane's stable registry key.
func (m *Model) Key() string { return m.key }

// Path returns the viewed file's path.
func (m *Model) Path() string { return m.path }

// Err returns the open error, nil when the file is readable (tests).
func (m *Model) Err() error { return m.err }

// Cursor returns the byte cursor's offset (tests).
func (m *Model) Cursor() int64 { return m.cur }

// PerRow returns the current bytes-per-row width (tests).
func (m *Model) PerRow() int { return m.perRow }

// Selection returns the selected inclusive byte range and whether one exists.
func (m *Model) Selection() (from, to int64, ok bool) {
	if m.anchor < 0 {
		return 0, 0, false
	}
	from, to = m.anchor, m.cur
	if from > to {
		from, to = to, from
	}
	return from, to, true
}

// Matches returns the enumerated search-match offsets (tests).
func (m *Model) Matches() []int64 { return m.matches }

// Searching reports whether the search line is open (tests).
func (m *Model) Searching() bool { return m.sEditing }

// Close releases the file handle. A zero-value model (after a tab detach
// moved the live one) holds nothing and releases nothing.
func (m *Model) Close() {
	if m.f != nil {
		m.f.Close()
		m.f = nil
	}
}

// SetSize records the pane interior and re-derives the row width from it.
func (m *Model) SetSize(w, h int) {
	m.w, m.h = w, h
	m.perRow = rowBytes(w, m.offsetWidth())
	m.clampScroll()
}

// SetFocused records focus for the chrome.
func (m *Model) SetFocused(f bool) { m.focused = f }

// SetPalette re-threads the theme palette.
func (m *Model) SetPalette(p *theme.Palette) { m.pal = p }

// offsetWidth is the hex-digit width of the offset column: 8 digits covers
// 4 GiB and stays the classic look; larger files widen it to fit.
func (m *Model) offsetWidth() int {
	w := 8
	for max := int64(1) << (4 * 8); m.size > max && w < 16; w++ {
		max = int64(1) << (4 * (w + 1))
	}
	return w
}

// rowBytes picks the widest classic row (8, 16 or 32 bytes) whose rendered
// line — offset, two-space gutters, hex column with a mid gap every 8 bytes,
// ASCII column — fits in w columns.
func rowBytes(w, offW int) int {
	for _, n := range []int{32, 16, 8} {
		if rowWidth(n, offW) <= w {
			return n
		}
	}
	return 8
}

// rowWidth is the rendered width of an n-byte row with an offW-digit offset.
func rowWidth(n, offW int) int {
	gaps := n/8 - 1 // extra space between each 8-byte group
	if gaps < 0 {
		gaps = 0
	}
	return offW + 2 + (3*n - 1 + gaps) + 2 + n
}

// bodyRows is the number of hex rows the pane shows: the interior minus the
// inspector row and the footer (search line or hints).
func (m *Model) bodyRows() int {
	h := m.h - 2
	if h < 1 {
		h = 1
	}
	return h
}

// clampScroll keeps the cursor in range and the viewport around it.
func (m *Model) clampScroll() {
	if m.size == 0 {
		m.cur, m.top = 0, 0
		return
	}
	if m.cur < 0 {
		m.cur = 0
	}
	if m.cur >= m.size {
		m.cur = m.size - 1
	}
	rows := int64(m.bodyRows())
	per := int64(m.perRow)
	if m.top%per != 0 {
		m.top -= m.top % per
	}
	curRow := m.cur / per
	topRow := m.top / per
	if curRow < topRow {
		topRow = curRow
	}
	if curRow >= topRow+rows {
		topRow = curRow - rows + 1
	}
	lastTop := (m.size - 1) / per
	if maxTop := lastTop - rows + 1; topRow > maxTop && maxTop >= 0 {
		topRow = maxTop
	}
	if topRow < 0 {
		topRow = 0
	}
	m.top = topRow * per
}

// readAt returns up to n bytes at off through the cached window, reading a
// fresh window from the file when off falls outside the cached one. Reads
// never touch more than windowSize bytes, whatever the file size.
func (m *Model) readAt(off int64, n int) []byte {
	if m.f == nil || off < 0 || off >= m.size || n <= 0 {
		return nil
	}
	if n > windowSize {
		n = windowSize
	}
	if off < m.winOff || off+int64(n) > m.winOff+int64(len(m.win)) {
		start := off - windowSize/4
		if start < 0 {
			start = 0
		}
		buf := make([]byte, windowSize)
		read, err := m.f.ReadAt(buf, start)
		if err != nil && err != io.EOF {
			return nil
		}
		m.win, m.winOff = buf[:read], start
	}
	lo := off - m.winOff
	hi := lo + int64(n)
	if max := int64(len(m.win)); hi > max {
		hi = max
	}
	if lo < 0 || lo >= hi {
		return nil
	}
	return m.win[lo:hi]
}

// Update handles one message; the pane is read-only, so every key is
// navigation, selection, search or the copy menu.
func (m *Model) Update(msg tea.Msg) tea.Cmd {
	key, ok := msg.(tea.KeyPressMsg)
	if !ok {
		return nil
	}
	if m.err != nil {
		return nil
	}
	if m.copyMenu {
		return m.copyMenuKey(key)
	}
	if m.sEditing {
		return m.searchKey(key)
	}
	per := int64(m.perRow)
	page := int64(m.bodyRows()) * per
	switch key.String() {
	case "j", "down":
		m.cur += per
	case "k", "up":
		m.cur -= per
	case "l", "right":
		m.cur++
	case "h", "left":
		m.cur--
	case "pgdown", "ctrl+f":
		m.cur += page
	case "pgup", "ctrl+b":
		m.cur -= page
	case "ctrl+d":
		m.cur += page / 2
	case "ctrl+u":
		m.cur -= page / 2
	case "g", "home":
		m.cur = 0
	case "G", "end":
		if m.size > 0 {
			m.cur = m.size - 1
		}
	case "v":
		if m.anchor >= 0 {
			m.anchor = -1
		} else {
			m.anchor = m.cur
		}
	case "esc":
		switch {
		case m.anchor >= 0:
			m.anchor = -1
		case m.needle != nil:
			m.needle, m.matches, m.capped, m.mIdx = nil, nil, false, -1
		}
	case "y", "cmd+c", "super+c":
		if m.size > 0 {
			m.copyMenu, m.copyCur = true, 0
		}
	case "/":
		m.startSearch()
	case "n":
		m.stepMatch(1)
	case "N":
		m.stepMatch(-1)
	}
	m.clampScroll()
	return nil
}

// PasteText inserts a paste into the open search line (#2002); with the line
// closed the text is dropped rather than leaking into navigation keys.
func (m *Model) PasteText(text string) {
	if !m.sEditing || text == "" {
		return
	}
	r := []rune(m.sInput)
	m.sInput = string(r[:m.sCur]) + text + string(r[m.sCur:])
	m.sCur += len([]rune(text))
	m.sErr = ""
}

// Wheel scrolls the viewport by delta rows, moving the cursor along so it
// stays visible.
func (m *Model) Wheel(delta int) {
	m.cur += int64(delta) * int64(m.perRow)
	m.clampScroll()
}

// startSearch opens the search line, seeded with the applied query.
func (m *Model) startSearch() {
	if m.err != nil || m.size == 0 {
		return
	}
	m.sEditing = true
	m.sCur = len([]rune(m.sInput))
	m.sErr = ""
}

// OpenSearch implements the pane's Searchable capability (#2409): cmd+f opens
// the same line "/" does.
func (m *Model) OpenSearch() bool {
	m.startSearch()
	return m.sEditing
}

// NextMatch implements the pane's match-step capability (#2410).
func (m *Model) NextMatch() ui.MatchStep { return m.searchStep(1) }

// PrevMatch steps backwards; see NextMatch.
func (m *Model) PrevMatch() ui.MatchStep { return m.searchStep(-1) }

// searchStep serves cmd+g / cmd+shift+g while the search line is open: it
// applies the typed query if it changed and steps the current match.
func (m *Model) searchStep(delta int) ui.MatchStep {
	if !m.sEditing {
		return ui.NoStep
	}
	if !m.applySearch() {
		return ui.NoMatches()
	}
	if len(m.matches) == 0 {
		return ui.NoMatches()
	}
	m.stepMatch(delta)
	total := len(m.matches)
	return ui.Stepped(m.mIdx, total, m.wrapped)
}

// searchKey feeds one key to the open search line, which owns the keyboard.
func (m *Model) searchKey(key tea.KeyPressMsg) tea.Cmd {
	switch key.String() {
	case "esc":
		m.sEditing, m.sErr = false, ""
	case "enter":
		if m.applySearch() {
			m.sEditing = false
			if len(m.matches) == 0 {
				m.sErr = ""
			}
		}
	default:
		if out, ncur, handled, changed := ui.EditKey(key, m.sInput, m.sCur); handled {
			m.sInput, m.sCur = out, ncur
			if changed {
				m.sErr = ""
				m.wrapped = false
			}
		}
	}
	m.clampScroll()
	return nil
}

// applySearch parses the typed query, enumerates its matches and jumps to the
// first one at or after the cursor. It reports false on a malformed query.
func (m *Model) applySearch() bool {
	needle, err := ParseQuery(m.sInput)
	if err != nil {
		m.sErr = err.Error()
		return false
	}
	if len(needle) == 0 {
		m.sErr = "empty search"
		return false
	}
	if !bytesEqual(needle, m.needle) || m.matches == nil {
		m.needle = needle
		m.matches, m.capped = m.findAll(needle)
		m.mIdx = -1
		m.wrapped = false
	}
	if len(m.matches) == 0 {
		m.sErr = "no matches"
		return true
	}
	m.sErr = ""
	if m.mIdx < 0 {
		m.mIdx = 0
		for i, off := range m.matches {
			if off >= m.cur {
				m.mIdx = i
				break
			}
		}
		m.cur = m.matches[m.mIdx]
	}
	return true
}

// stepMatch moves the current match by delta, wrapping over the enumerated
// set, and puts the cursor on it.
func (m *Model) stepMatch(delta int) {
	if len(m.matches) == 0 {
		return
	}
	if m.mIdx < 0 {
		m.mIdx = 0
	} else {
		m.mIdx, m.wrapped = ui.StepWrap(m.mIdx, len(m.matches), delta)
	}
	m.cur = m.matches[m.mIdx]
	m.clampScroll()
}

// findAll streams over the file in overlapping chunks and collects the
// offsets where needle occurs, up to maxMatches. Only an explicit search pays
// this scan; opening the pane never does.
func (m *Model) findAll(needle []byte) (offs []int64, capped bool) {
	if m.f == nil || len(needle) == 0 {
		return nil, false
	}
	const chunk = 1 << 20
	buf := make([]byte, chunk+len(needle)-1)
	for off := int64(0); off < m.size; off += chunk {
		read, err := m.f.ReadAt(buf, off)
		if read == 0 {
			break
		}
		data := buf[:read]
		for i := 0; ; {
			j := indexBytes(data[i:], needle)
			if j < 0 {
				break
			}
			at := off + int64(i+j)
			// The overlap re-reads len(needle)-1 bytes, so a match there
			// would be found twice; only the first chunk owns it.
			if len(offs) == 0 || offs[len(offs)-1] < at {
				if len(offs) >= maxMatches {
					return offs, true
				}
				offs = append(offs, at)
			}
			i += j + 1
		}
		if err != nil {
			break
		}
	}
	return offs, false
}

// copyMenuKey drives the two-row copy-format picker.
func (m *Model) copyMenuKey(key tea.KeyPressMsg) tea.Cmd {
	switch key.String() {
	case "esc", "q":
		m.copyMenu = false
	case "j", "down", "k", "up", "tab":
		m.copyCur = 1 - m.copyCur
	case "1":
		m.copyMenu = false
		return m.copyCmd(false)
	case "2":
		m.copyMenu = false
		return m.copyCmd(true)
	case "enter", "y":
		m.copyMenu = false
		return m.copyCmd(m.copyCur == 1)
	}
	return nil
}

// copyCmd reads the selected range (the cursor byte without a selection) and
// emits the CopyMsg — as a spaced hex string, or as the raw bytes.
func (m *Model) copyCmd(raw bool) tea.Cmd {
	from, to, ok := m.Selection()
	if !ok {
		from, to = m.cur, m.cur
	}
	n := to - from + 1
	truncated := false
	if n > maxCopy {
		n, truncated = maxCopy, true
	}
	data := make([]byte, 0, n)
	for int64(len(data)) < n {
		part := m.readAt(from+int64(len(data)), int(n-int64(len(data))))
		if len(part) == 0 {
			break
		}
		data = append(data, part...)
	}
	var text, what string
	if raw {
		text = string(data)
		what = fmt.Sprintf("%d raw bytes", len(data))
	} else {
		parts := make([]string, len(data))
		for i, b := range data {
			parts[i] = fmt.Sprintf("%02x", b)
		}
		text = strings.Join(parts, " ")
		what = fmt.Sprintf("%d bytes as hex", len(data))
	}
	if truncated {
		what += " (selection capped at 1 MiB)"
	}
	return func() tea.Msg { return CopyMsg{Text: text, What: what} }
}

// bytesEqual avoids importing bytes for one comparison next to the manual
// index below.
func bytesEqual(a, b []byte) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// indexBytes is bytes.Index without the import gravity; needles are short.
func indexBytes(data, needle []byte) int {
	if len(needle) == 0 || len(data) < len(needle) {
		return -1
	}
	first := needle[0]
	for i := 0; i <= len(data)-len(needle); i++ {
		if data[i] != first {
			continue
		}
		if bytesEqual(data[i:i+len(needle)], needle) {
			return i
		}
	}
	return -1
}

// View renders the pane interior: hex rows, the inspector row, and the
// footer (hints, the open search line, or the copy menu).
func (m *Model) View() string {
	if m.w <= 0 || m.h <= 0 {
		return ""
	}
	if m.err != nil {
		dim := lipgloss.NewStyle().Foreground(m.pal.Ghost)
		body := lipgloss.NewStyle().Bold(true).Foreground(m.pal.Foreground).Render(filepath.Base(m.path)) +
			"\n" + dim.Render("cannot open: "+m.err.Error())
		return lipgloss.Place(m.w, m.h, lipgloss.Center, lipgloss.Center, body)
	}
	rows := m.bodyRows()
	lines := make([]string, 0, m.h)
	per := int64(m.perRow)
	for r := 0; r < rows; r++ {
		off := m.top + int64(r)*per
		if off >= m.size {
			lines = append(lines, "")
			continue
		}
		lines = append(lines, m.renderRow(off))
	}
	lines = append(lines, m.inspectorRow())
	lines = append(lines, m.footer())
	return strings.Join(lines, "\n")
}

// byteClass says how one visible byte is highlighted.
type byteClass int

const (
	classPlain byteClass = iota
	classMatch
	classSelected
	classCursor
)

// classify ranks the highlights of the byte at off: the cursor wins over the
// selection, which wins over a search match.
func (m *Model) classify(off int64) byteClass {
	if off == m.cur {
		return classCursor
	}
	if from, to, ok := m.Selection(); ok && off >= from && off <= to {
		return classSelected
	}
	if m.inMatch(off) {
		return classMatch
	}
	return classPlain
}

// inMatch reports whether off lies inside any enumerated match. Matches are
// sorted, so a binary search over the starts bounds the check.
func (m *Model) inMatch(off int64) bool {
	if len(m.matches) == 0 || m.needle == nil {
		return false
	}
	lo, hi := 0, len(m.matches)
	for lo < hi {
		mid := (lo + hi) / 2
		if m.matches[mid] <= off {
			lo = mid + 1
		} else {
			hi = mid
		}
	}
	if lo == 0 {
		return false
	}
	start := m.matches[lo-1]
	return off < start+int64(len(m.needle))
}

// renderRow renders one offset|hex|ASCII line starting at off.
func (m *Model) renderRow(off int64) string {
	offW := m.offsetWidth()
	data := m.readAt(off, m.perRow)
	styles := map[byteClass]lipgloss.Style{
		classPlain:    lipgloss.NewStyle().Foreground(m.pal.Foreground),
		classMatch:    lipgloss.NewStyle().Background(m.pal.OccurrenceRead).Foreground(m.pal.Foreground),
		classSelected: lipgloss.NewStyle().Background(m.pal.SelectionMuted).Foreground(m.pal.Foreground),
		classCursor:   lipgloss.NewStyle().Background(m.pal.Primary).Foreground(m.pal.SelectionText).Bold(true),
	}
	dim := lipgloss.NewStyle().Foreground(m.pal.Ghost)
	var hex, asc strings.Builder
	for i := 0; i < m.perRow; i++ {
		if i > 0 {
			hex.WriteByte(' ')
			if i%8 == 0 {
				hex.WriteByte(' ')
			}
		}
		if i >= len(data) {
			hex.WriteString("  ")
			continue
		}
		b := data[i]
		st := styles[m.classify(off+int64(i))]
		hex.WriteString(st.Render(fmt.Sprintf("%02x", b)))
		ch := "."
		if b >= 0x20 && b < 0x7f {
			ch = string(rune(b))
		}
		asc.WriteString(st.Render(ch))
	}
	return dim.Render(fmt.Sprintf("%0*x", offW, off)) + "  " + hex.String() + "  " + asc.String()
}

// inspectorRow decodes the bytes at the cursor into the classic value
// readings: fixed-width integers little- and big-endian, floats, and the
// UTF-8 rune starting there.
func (m *Model) inspectorRow() string {
	if m.size == 0 {
		return lipgloss.NewStyle().Faint(true).Render(clipTo(" empty file", m.w))
	}
	line := " " + Inspect(m.readAt(m.cur, 8), m.cur)
	if from, to, ok := m.Selection(); ok {
		line += fmt.Sprintf(" · sel %d B", to-from+1)
	}
	return lipgloss.NewStyle().Faint(true).Render(clipTo(line, m.w))
}

// footer renders the bottom line: the copy menu or search line while one is
// open, the offset/size status and key hints otherwise.
func (m *Model) footer() string {
	if m.copyMenu {
		rows := []string{"1 hex string", "2 raw bytes"}
		for i := range rows {
			if i == m.copyCur {
				rows[i] = "▸ " + rows[i]
			} else {
				rows[i] = "  " + rows[i]
			}
		}
		return lipgloss.NewStyle().Foreground(m.pal.Accent).Render(
			clipTo(" copy as: "+strings.Join(rows, "   ")+"   · enter copy · esc cancel", m.w))
	}
	if m.sEditing {
		if m.sErr != "" {
			return lipgloss.NewStyle().Foreground(m.pal.Error).Render(clipTo(" /"+m.sInput+" — "+m.sErr, m.w))
		}
		hint := " enter search · esc close · text, 0x…, or \\xNN bytes"
		if len(m.matches) > 0 && m.needle != nil {
			total := fmt.Sprintf("%d", len(m.matches))
			if m.capped {
				total += "+"
			}
			hint = fmt.Sprintf(" %d/%s ·%s", m.mIdx+1, total, hint)
		}
		return lipgloss.NewStyle().Faint(true).Render(clipTo(" /"+m.sInput+" ·"+hint, m.w))
	}
	status := fmt.Sprintf(" offset %d (0x%x) of %d", m.cur, m.cur, m.size)
	if len(m.matches) > 0 {
		total := fmt.Sprintf("%d", len(m.matches))
		if m.capped {
			total += "+"
		}
		status += fmt.Sprintf(" · match %d/%s", m.mIdx+1, total)
	}
	hints := "j/k/h/l move · g/G ends · ctrl+d/u half page · v select · y copy · / search · n/N match"
	return lipgloss.NewStyle().Faint(true).Render(clipTo(status+" · "+hints, m.w))
}

// clipTo cuts s to width cells; the footer and inspector never wrap.
func clipTo(s string, w int) string {
	if w <= 0 {
		return ""
	}
	r := []rune(s)
	if len(r) <= w {
		return s
	}
	return string(r[:w])
}

// Inspect renders the inspector readings for data, the bytes starting at the
// cursor offset off. Readings a short tail cannot fill are shown as "—".
func Inspect(data []byte, off int64) string {
	var b strings.Builder
	fmt.Fprintf(&b, "0x%x", off)
	if len(data) == 0 {
		b.WriteString(" · —")
		return b.String()
	}
	u8 := data[0]
	fmt.Fprintf(&b, " · u8 %d · i8 %d", u8, int8(u8))
	if len(data) >= 2 {
		le := uint16(data[0]) | uint16(data[1])<<8
		be := uint16(data[1]) | uint16(data[0])<<8
		fmt.Fprintf(&b, " · u16 %d/%d", le, be)
	} else {
		b.WriteString(" · u16 —")
	}
	if len(data) >= 4 {
		le := le32(data)
		be := be32(data)
		fmt.Fprintf(&b, " · u32 %d/%d · f32 %s", le, be, floatText(float64(f32(le))))
	} else {
		b.WriteString(" · u32 — · f32 —")
	}
	if len(data) >= 8 {
		le := le64(data)
		be := be64(data)
		fmt.Fprintf(&b, " · u64 %d/%d · f64 %s", le, be, floatText(f64(le)))
	} else {
		b.WriteString(" · u64 — · f64 —")
	}
	if r, size := utf8.DecodeRune(data); r != utf8.RuneError || size > 1 {
		fmt.Fprintf(&b, " · %q", r)
	} else {
		b.WriteString(" · rune —")
	}
	return b.String()
}

func le32(d []byte) uint32 {
	return uint32(d[0]) | uint32(d[1])<<8 | uint32(d[2])<<16 | uint32(d[3])<<24
}

func be32(d []byte) uint32 {
	return uint32(d[3]) | uint32(d[2])<<8 | uint32(d[1])<<16 | uint32(d[0])<<24
}

func le64(d []byte) uint64 {
	return uint64(le32(d)) | uint64(le32(d[4:]))<<32
}

func be64(d []byte) uint64 {
	return uint64(be32(d[4:])) | uint64(be32(d))<<32
}

// f32/f64 reinterpret the little-endian integer readings as IEEE floats.
func f32(u uint32) float32 { return math.Float32frombits(u) }
func f64(u uint64) float64 { return math.Float64frombits(u) }

// floatText keeps the inspector row compact: %g, but NaN/Inf spelled out.
func floatText(f float64) string { return fmt.Sprintf("%g", f) }

// ParseQuery turns the search input into the byte sequence to find: a "0x"
// prefix reads hex digits (spaces allowed), "\xNN" escapes mix bytes into a
// literal, and anything else is the string's UTF-8 bytes.
func ParseQuery(s string) ([]byte, error) {
	if strings.HasPrefix(s, "0x") || strings.HasPrefix(s, "0X") {
		digits := strings.Map(func(r rune) rune {
			if r == ' ' {
				return -1
			}
			return r
		}, s[2:])
		if len(digits) == 0 || len(digits)%2 != 0 {
			return nil, fmt.Errorf("hex search needs an even number of digits")
		}
		out := make([]byte, len(digits)/2)
		for i := 0; i < len(out); i++ {
			hi, ok1 := hexVal(digits[2*i])
			lo, ok2 := hexVal(digits[2*i+1])
			if !ok1 || !ok2 {
				return nil, fmt.Errorf("invalid hex digit in %q", digits)
			}
			out[i] = hi<<4 | lo
		}
		return out, nil
	}
	if strings.Contains(s, `\x`) {
		var out []byte
		for i := 0; i < len(s); {
			if i+3 < len(s) && s[i] == '\\' && s[i+1] == 'x' {
				hi, ok1 := hexVal(s[i+2])
				lo, ok2 := hexVal(s[i+3])
				if !ok1 || !ok2 {
					return nil, fmt.Errorf(`\x needs two hex digits`)
				}
				out = append(out, hi<<4|lo)
				i += 4
				continue
			}
			if s[i] == '\\' && i+1 < len(s) && s[i+1] == 'x' {
				return nil, fmt.Errorf(`\x needs two hex digits`)
			}
			out = append(out, s[i])
			i++
		}
		return out, nil
	}
	return []byte(s), nil
}

func hexVal(c byte) (byte, bool) {
	switch {
	case c >= '0' && c <= '9':
		return c - '0', true
	case c >= 'a' && c <= 'f':
		return c - 'a' + 10, true
	case c >= 'A' && c <= 'F':
		return c - 'A' + 10, true
	}
	return 0, false
}
