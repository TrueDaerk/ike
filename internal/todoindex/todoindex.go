// Package todoindex is the TODO/FIXME index tool window (#61): JetBrains' TODO
// view as a centered overlay listing every comment tag (TODO, FIXME, HACK, XXX
// — configurable via [todo] patterns) in the project, grouped by file through
// the reusable locations component. It drives its own search.Service (the
// find-in-path scanner, so gitignore/binary/hidden rules match, #29) whose
// messages arrive wrapped in ScanMsg so the finder can never mistake them for
// its own generation. A full scan runs at startup and on demand; a buffer save
// rescans just that file (FileScanMsg). Filters — tag kind and current file
// only — are applied in-memory over the retained entry set, so toggling them
// never rescans. Selecting an entry dispatches OpenLocationMsg.
package todoindex

import (
	"bufio"
	"bytes"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"ike/internal/locations"
	"ike/internal/search"
	"ike/internal/theme"
)

// OpenLocationMsg asks the root model to open Path at the (1-based) Line and
// (0-based rune) Col of a selected tag.
type OpenLocationMsg struct {
	Path string
	Line int
	Col  int
}

// ScanMsg wraps this index's streamed search messages (search.BatchMsg /
// search.DoneMsg). The finder consumes the bare types and filters only by
// generation — two independent search.Services count generations separately,
// so an unwrapped todo batch could collide with a live finder scan. The root
// model unwraps Inner into Apply.
type ScanMsg struct{ Inner tea.Msg }

// FileScanMsg carries one file's freshly scanned tags (the buffer-save rescan
// path). Gen is the index generation the scan was started under; a full rescan
// in between invalidates it.
type FileScanMsg struct {
	Gen   int
	Path  string
	Items []Entry
}

// Entry is one indexed tag: its location plus the canonical tag word
// ("TODO", "FIXME", …) the filter groups by.
type Entry struct {
	Item locations.Item
	Tag  string
}

// DefaultPatterns is the tag set used when [todo] patterns is empty.
var DefaultPatterns = []string{"TODO", "FIXME", "HACK", "XXX"}

// Model is the overlay state. The root model routes keys here while open,
// feeds wrapped scan messages through Apply, and file rescans through
// ApplyFileScan.
type Model struct {
	svc      *search.Service
	root     string
	patterns []string

	open bool

	gen       int // generation of the full scan whose results we accept
	scanning  bool
	scanned   bool // at least one full scan finished (status segment gate)
	truncated bool
	errText   string

	entries []Entry // full unfiltered set, scan order
	list    locations.List

	tagIdx   int    // 0 = all tags, else patterns[tagIdx-1]
	fileOnly bool   // restrict to curPath
	curPath  string // active editor file when opened (absolute)

	// lay records, during View, which content rows the mouse can hit; Click
	// hit-tests against it (same scheme as the finder).
	lay layoutInfo

	width, height int
	pal           *theme.Palette

	// displayPath shortens paths for the group headers (the root model injects
	// its project-relative formatter).
	displayPath func(string) string
}

// New returns a closed index rooted at root, driving svc with the given tag
// patterns (nil/empty falls back to DefaultPatterns).
func New(svc *search.Service, root string, patterns []string) *Model {
	cleaned := make([]string, 0, len(patterns))
	for _, p := range patterns {
		if p = strings.TrimSpace(p); p != "" {
			cleaned = append(cleaned, p)
		}
	}
	if len(cleaned) == 0 {
		cleaned = append(cleaned, DefaultPatterns...)
	}
	return &Model{svc: svc, root: root, patterns: cleaned}
}

// SetPalette threads the active theme in.
func (m *Model) SetPalette(p *theme.Palette) { m.pal = p }

// SetDisplayPath injects the header path formatter.
func (m *Model) SetDisplayPath(f func(string) string) { m.displayPath = f }

// SetSize records the terminal size.
func (m *Model) SetSize(w, h int) { m.width, m.height = w, h }

// Patterns returns the configured tag words (the filter cycles through them).
func (m *Model) Patterns() []string { return m.patterns }

// Open shows the index. curPath is the active editor's file (used by the
// current-file-only filter), "" when none. Results of the last scan are shown
// immediately; the first open triggers the initial scan if none ran yet.
func (m *Model) Open(curPath string) {
	m.open = true
	m.curPath = absPath(curPath)
	if !m.scanned && !m.scanning {
		m.Rescan()
		return
	}
	m.rebuildList()
}

// Close hides the overlay. The entry set is kept: the status-line count and
// the next open stay warm.
func (m *Model) Close() { m.open = false }

// IsOpen reports whether the overlay is shown.
func (m *Model) IsOpen() bool { return m.open }

// Count returns the total indexed tag count (unfiltered), for the status line.
func (m *Model) Count() int { return len(m.entries) }

// Total returns the visible (filtered) tag count; Files its file-group count.
func (m *Model) Total() int { return m.list.Total() }
func (m *Model) Files() int { return m.list.Files() }

// Scanned reports whether a full scan has completed at least once.
func (m *Model) Scanned() bool { return m.scanned }

// pattern is the scanner regex: the alternation of the (quoted) tag words.
// WholeWord on the query adds the \b guards on both backends, so the match
// range is exactly the tag — Apply classifies entries from it.
func (m *Model) pattern() string {
	quoted := make([]string, len(m.patterns))
	for i, p := range m.patterns {
		quoted[i] = regexp.QuoteMeta(p)
	}
	return "(?:" + strings.Join(quoted, "|") + ")"
}

// Rescan starts a full project scan, superseding any running one and
// invalidating in-flight file rescans.
func (m *Model) Rescan() {
	m.entries = nil
	m.truncated = false
	m.errText = ""
	m.scanning = true
	m.gen = m.svc.Scan(search.Query{
		Pattern:   m.pattern(),
		Root:      m.root,
		Regex:     true,
		WholeWord: true,
	})
	m.rebuildList()
}

// Apply consumes one unwrapped scan message, dropping stale generations.
func (m *Model) Apply(msg tea.Msg) {
	switch msg := msg.(type) {
	case search.BatchMsg:
		if msg.Gen != m.gen {
			return
		}
		for _, hit := range msg.Matches {
			m.entries = append(m.entries, m.entry(hit.Path, hit.Line, hit.StartCol, hit.EndCol, hit.Text))
		}
		m.rebuildList()
	case search.DoneMsg:
		if msg.Gen != m.gen {
			return
		}
		m.scanning = false
		m.scanned = true
		m.truncated = msg.Truncated
		if msg.Err != nil {
			m.errText = msg.Err.Error()
		}
	}
}

// entry builds one Entry, canonicalizing the path (scanner paths are
// root-relative, editor paths absolute — the save-rescan splice and the
// current-file filter need one spelling) and classifying the tag from the
// match range.
func (m *Model) entry(path string, line, startCol, endCol int, text string) Entry {
	tag := strings.ToUpper(runeSlice(text, startCol, endCol))
	for _, p := range m.patterns {
		if strings.EqualFold(p, tag) {
			tag = strings.ToUpper(p)
			break
		}
	}
	return Entry{
		Item: locations.Item{
			Path:     absPath(path),
			Line:     line,
			StartCol: startCol,
			EndCol:   endCol,
			Text:     text,
		},
		Tag: tag,
	}
}

// RescanFile returns a command that rescans one saved file off the update
// loop and reports its tags as a FileScanMsg. Files outside the project root
// or hidden (dot-path) files are skipped, mirroring the project scan's rules;
// gitignored files are not re-checked here (a saved buffer is almost always
// project content — the next full scan settles any drift).
func (m *Model) RescanFile(path string) tea.Cmd {
	abs := absPath(path)
	if abs == "" || !m.inRoot(abs) {
		return nil
	}
	gen := m.gen
	scan := *m // capture patterns/pattern() without racing the model
	return func() tea.Msg {
		return FileScanMsg{Gen: gen, Path: abs, Items: scan.scanFile(abs)}
	}
}

// ApplyFileScan splices one file's rescanned tags into the entry set: its old
// entries are replaced in place (keeping the file's position in the list), new
// files append at the end.
func (m *Model) ApplyFileScan(msg FileScanMsg) {
	if msg.Gen != m.gen {
		return
	}
	out := make([]Entry, 0, len(m.entries)+len(msg.Items))
	inserted := false
	for _, e := range m.entries {
		if e.Item.Path == msg.Path {
			if !inserted {
				out = append(out, msg.Items...)
				inserted = true
			}
			continue
		}
		out = append(out, e)
	}
	if !inserted {
		out = append(out, msg.Items...)
	}
	m.entries = out
	m.rebuildList()
}

// inRoot reports whether abs lives under the index root and no path component
// below the root is hidden (the project scan's dot-entry rule).
func (m *Model) inRoot(abs string) bool {
	root := absPath(m.root)
	rel, err := filepath.Rel(root, abs)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return false
	}
	for _, part := range strings.Split(filepath.ToSlash(rel), "/") {
		if strings.HasPrefix(part, ".") {
			return false
		}
	}
	return true
}

// maxFileSize mirrors the project scanner's single-file bound.
const maxFileSize = 4 << 20 // 4 MiB

// scanFile matches one file line by line with the same regex semantics as the
// project scan (case-folded, whole-word alternation).
func (m Model) scanFile(abs string) []Entry {
	re, err := regexp.Compile(`(?i)\b` + m.pattern() + `\b`)
	if err != nil {
		return nil
	}
	fi, err := os.Stat(abs)
	if err != nil || !fi.Mode().IsRegular() || fi.Size() > maxFileSize {
		return nil
	}
	f, err := os.Open(abs)
	if err != nil {
		return nil
	}
	defer f.Close()
	head := make([]byte, 1024)
	n, _ := f.Read(head)
	if bytes.IndexByte(head[:n], 0) >= 0 {
		return nil // binary sniff, like the scanner
	}
	if _, err := f.Seek(0, 0); err != nil {
		return nil
	}
	var out []Entry
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), maxFileSize)
	line := 0
	for sc.Scan() {
		line++
		text := strings.TrimRight(sc.Text(), "\r")
		for _, loc := range re.FindAllStringIndex(text, -1) {
			out = append(out, m.entry(abs, line,
				len([]rune(text[:loc[0]])), len([]rune(text[:loc[1]])), text))
		}
	}
	return out
}

// tagFilter returns the active tag word, "" when all tags show.
func (m *Model) tagFilter() string {
	if m.tagIdx == 0 {
		return ""
	}
	return strings.ToUpper(m.patterns[m.tagIdx-1])
}

// rebuildList re-derives the visible list from the entry set and the filters.
func (m *Model) rebuildList() {
	tag := m.tagFilter()
	m.list.Reset()
	for _, e := range m.entries {
		if tag != "" && e.Tag != tag {
			continue
		}
		if m.fileOnly && e.Item.Path != m.curPath {
			continue
		}
		m.list.Append([]locations.Item{e.Item})
	}
}

// Update handles one key while the overlay is open.
func (m *Model) Update(msg tea.KeyPressMsg) tea.Cmd {
	switch msg.String() {
	case "esc":
		m.Close()
		return nil
	case "enter":
		return m.openCurrent()
	case "down", "j":
		m.list.Move(1)
	case "up", "k":
		m.list.Move(-1)
	case "pgdown":
		m.list.Move(10)
	case "pgup":
		m.list.Move(-10)
	// ctrl doubles every alt binding: on macOS Option is a composition key,
	// so alt chords never reach the terminal (#422); ctrl is the delivered
	// primary, alt stays for terminals where it works.
	case "alt+t", "ctrl+t":
		m.tagIdx = (m.tagIdx + 1) % (len(m.patterns) + 1)
		m.rebuildList()
	case "alt+o", "ctrl+o":
		m.fileOnly = !m.fileOnly
		m.rebuildList()
	case "alt+r", "ctrl+r":
		m.Rescan()
	}
	return nil
}

// openCurrent dispatches the selected tag as a navigation and closes.
func (m *Model) openCurrent() tea.Cmd {
	it, ok := m.list.Current()
	if !ok {
		return nil
	}
	m.Close()
	return func() tea.Msg {
		return OpenLocationMsg{Path: it.Path, Line: it.Line, Col: it.StartCol}
	}
}

// layoutInfo maps content rows (0 = first row inside the border) to click
// targets; View fills it in each render, -1 marks an absent row.
type layoutInfo struct {
	filters           int
	listTop, listRows int
}

// The filter row's fixed pieces; filterSpans derives the click ranges from
// them, so keep the render and the constants in sync.
const (
	tagLabelPrefix = "Tag: "
	tagHint        = " (ctrl+t)"
	fileLabel      = "Current file (ctrl+o)"
)

// filterSpans mirrors filtersRow's layout: the half-open x ranges of the tag
// cycle and the current-file checkbox within the content row.
func (m *Model) filterSpans() (tag, file [2]int) {
	w := len(tagLabelPrefix) + len(m.tagName()) + len(tagHint)
	tag = [2]int{0, w}
	file = [2]int{w + 2, w + 2 + 4 + len(fileLabel)} // "  " gap, "[x] " + label
	return
}

// tagName is the active tag filter's display name.
func (m *Model) tagName() string {
	if m.tagIdx == 0 {
		return "All"
	}
	return strings.ToUpper(m.patterns[m.tagIdx-1])
}

// Click handles a left press at panel-local coordinates (0,0 = the box's
// top-left border cell): the tag label cycles the tag filter, the checkbox
// toggles current-file-only, a result row selects its entry, and a press on
// the already-selected entry opens it.
func (m *Model) Click(x, y int) tea.Cmd {
	if !m.open || m.lay.filters <= 0 {
		return nil
	}
	cx, cy := x-2, y-1 // border + horizontal padding
	if cx < 0 || cy < 0 {
		return nil
	}
	if cy == m.lay.filters {
		tagSp, fileSp := m.filterSpans()
		switch {
		case cx >= tagSp[0] && cx < tagSp[1]:
			m.tagIdx = (m.tagIdx + 1) % (len(m.patterns) + 1)
			m.rebuildList()
		case cx >= fileSp[0] && cx < fileSp[1]:
			m.fileOnly = !m.fileOnly
			m.rebuildList()
		}
		return nil
	}
	if m.lay.listTop >= 0 && cy >= m.lay.listTop && cy < m.lay.listTop+m.lay.listRows {
		if idx, ok := m.list.ItemAt(cy - m.lay.listTop); ok {
			if idx == m.list.Cursor() {
				return m.openCurrent()
			}
			m.list.SetCursor(idx)
		}
	}
	return nil
}

// Wheel scrolls the list by delta items.
func (m *Model) Wheel(delta int) { m.list.Move(delta) }

// theme returns the active palette, defaulting when none was threaded in.
func (m *Model) theme() *theme.Palette {
	if m.pal != nil {
		return m.pal
	}
	return theme.DefaultPalette()
}

// View renders the centered overlay box.
func (m *Model) View() string {
	if !m.open || m.width <= 0 {
		return ""
	}
	pal := m.theme()
	boxW := m.width - 12
	if boxW > 100 {
		boxW = 100
	}
	if boxW < 40 {
		boxW = min(40, m.width-2)
	}
	innerW := boxW - 4 // border + padding

	title := lipgloss.NewStyle().Bold(true).Underline(true).Render("TODO Index")
	lay := layoutInfo{listTop: -1}
	rows := []string{title, ""}
	lay.filters = len(rows)
	rows = append(rows, m.filtersRow(innerW), "")

	listH := m.height/2 - 6
	if listH < 4 {
		listH = 4
	}
	if body := m.list.Render(innerW, listH, pal, m.displayPath); body != "" {
		lay.listTop = len(rows)
		lay.listRows = strings.Count(body, "\n") + 1
		rows = append(rows, body)
	}
	m.lay = lay
	rows = append(rows, "", m.statusRow(innerW))

	box := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(pal.BorderFocus).
		Padding(0, 1).
		Width(boxW - 2)
	return box.Render(strings.Join(rows, "\n"))
}

// filtersRow renders the tag cycle and the current-file checkbox.
func (m *Model) filtersRow(width int) string {
	pal := m.theme()
	on := lipgloss.NewStyle().Foreground(pal.BorderFocus).Bold(true)
	dim := lipgloss.NewStyle().Faint(true)
	tag := tagLabelPrefix + m.tagName()
	if m.tagIdx == 0 {
		tag = dim.Render(tag)
	} else {
		tag = on.Render(tag)
	}
	tag += dim.Render(tagHint)
	file := dim.Render("[ ] " + fileLabel)
	if m.fileOnly {
		file = on.Render("[x] " + fileLabel)
	}
	return lipgloss.NewStyle().MaxWidth(width).Render(tag + "  " + file)
}

// statusRow summarizes the index: live progress, counts, truncation, errors.
func (m *Model) statusRow(width int) string {
	pal := m.theme()
	dim := lipgloss.NewStyle().Faint(true)
	switch {
	case m.errText != "":
		return lipgloss.NewStyle().Foreground(pal.Error).Render(
			lipgloss.NewStyle().MaxWidth(width).Render("error: " + m.errText))
	case m.scanning && m.list.Total() == 0:
		return dim.Render("scanning…")
	case m.list.Total() == 0:
		return dim.Render("no tags — enter opens, esc closes, ctrl+t tag, ctrl+o file, ctrl+r rescan")
	}
	s := plural(m.list.Total(), "tag", "tags") + " in " + plural(m.list.Files(), "file", "files")
	if m.truncated {
		s += " (truncated)"
	} else if m.scanning {
		s += "…"
	}
	return dim.Render(s + " — enter opens, ctrl+t tag, ctrl+o file, ctrl+r rescan")
}

// plural renders "1 tag" / "3 tags" style counts.
func plural(n int, one, many string) string {
	if n == 1 {
		return "1 " + one
	}
	return strconv.Itoa(n) + " " + many
}

// runeSlice returns text[start:end] in rune offsets, clamped.
func runeSlice(text string, start, end int) string {
	r := []rune(text)
	if start < 0 {
		start = 0
	}
	if end > len(r) {
		end = len(r)
	}
	if start >= end {
		return ""
	}
	return string(r[start:end])
}

// absPath canonicalizes a path to its cleaned absolute form ("" stays "").
func absPath(path string) string {
	if path == "" {
		return ""
	}
	if abs, err := filepath.Abs(path); err == nil {
		return abs
	}
	return filepath.Clean(path)
}
