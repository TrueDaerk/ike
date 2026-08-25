package settings

import (
	"image/color"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

// editor.go is the typed-editor layer (0460, #1295). The wireframes' rule is
// "every value has a type, and every type has a picker": an enum is a list, a
// number is a stepper, a boolean is a pair of radio rows, a chord is a key
// capture, a list is an indexed multi-value editor — free text stays only
// where there is no domain. Each type implements Editor and renders into the
// detail column, so a new setting needs a type plus documentation, never new
// UI code.

// Editor edits one entry's value inside the detail column.
type Editor interface {
	// View renders the editor body into w columns and at most h lines.
	View(w, h int) []string
	// Update handles one key press; a returned command carries the write-back.
	Update(key tea.KeyPressMsg) tea.Cmd
	// Value is the editor's current value — what a commit would write.
	Value() any
	// Dirty reports that Value differs from the value in the live config.
	Dirty() bool
	// Capturing reports that the editor needs every key verbatim (a text
	// input), so the host must not intercept chrome chords.
	Capturing() bool
}

// pasteEditor is an optional Editor extension: editors backed by a text input
// accept a bracketed paste (#1273).
type pasteEditor interface {
	Paste(text string) bool
}

// clickEditor and wheelEditor are the detail column's pointer seams (#1325):
// an editor that can map its rendered rows onto an action implements Click
// (coordinates are editor-body-local: y counts its own View lines), and one
// with more content than room implements Wheel. Editors without a meaningful
// pointer target — a text field, say — implement neither and a press there
// only focuses the column.
type clickEditor interface {
	Click(x, y int) tea.Cmd
}

type wheelEditor interface {
	Wheel(delta int)
}

// newEditor builds the editor for an entry's type.
func newEditor(m *Model, e Entry) Editor {
	switch e.Type {
	case Bool:
		return newBoolEditor(m, e)
	case Enum:
		return newEnumEditor(m, e)
	case Int:
		return &intEditor{m: m, e: e, tf: newTextField(m.value(e.Key))}
	case Chord:
		return &chordEditor{m: m, e: e}
	case List, IntList:
		return newListEditor(m, e)
	case Path:
		ed := &pathEditor{m: m, e: e, tf: newTextField(m.value(e.Key))}
		ed.suggest.dirs = e.Dirs
		ed.suggest.refresh(ed.tf.text)
		return ed
	default:
		return &textEditor{m: m, e: e, tf: newTextField(m.value(e.Key))}
	}
}

// writeValue stages an edit for e (#1296): nothing reaches disk until the
// batch is applied. Every editor commits through here, so scope resolution,
// the change counter and the live preview live in one place.
func (m *Model) writeValue(e Entry, v any) tea.Cmd { return m.stage(e, v) }

// leaveEditor returns the focus to the settings column (esc from an editor).
func (m *Model) leaveEditor() { m.focus = formColumn }

// editorRow renders one selectable editor line: marker, label and an optional
// right-aligned tail, highlighted when it is the editor's cursor row.
func (m *Model) editorRow(w int, selected bool, left, right string) string {
	pal := m.theme()
	line := " " + left
	if right != "" {
		gap := w - lipgloss.Width(line) - lipgloss.Width(right) - 1
		if gap < 1 {
			gap = 1
		}
		line += strings.Repeat(" ", gap) + right
	}
	style := lipgloss.NewStyle().Foreground(pal.Foreground)
	if selected {
		style = lipgloss.NewStyle().Background(pal.Selection).Foreground(pal.SelectionText).Bold(true)
	}
	return lipgloss.NewStyle().MaxWidth(w).Render(style.Render(line))
}

// themedEditorRow renders one enum option row in the option's own colors
// (#1688): the two-cell selection marker in the panel's colors, then the label
// block painted with the option's background/foreground — that block is the
// mini-preview — and finally the already-styled tail (the palette swatch strip
// of #1664) outside it, on the panel's own background.
//
// The selection stays readable on top of arbitrary theme colors because it is
// carried by the panel-coloured chevron plus a bold, underlined label, never by
// a background band that would hide the very colors being previewed.
func (m *Model) themedEditorRow(w int, selected bool, left, right string, fg, bg color.Color) string {
	pal := m.theme()
	mark := rowMarker(pal, selected, m.focus == detailColumn, markerBG(selected, lipgloss.NewStyle().Background(pal.Selection)))

	body := lipgloss.NewStyle().Foreground(fg).Background(bg)
	if selected {
		body = body.Bold(true).Underline(true)
	}
	avail := maxInt(w-markerWidth, 1)
	tailW := lipgloss.Width(right)
	if right != "" {
		tailW++ // a blank keeps the strip off the themed block
	}
	textW := maxInt(avail-tailW, 1)
	label := ansi.Truncate(" "+left, textW, "…")
	label += strings.Repeat(" ", maxInt(textW-lipgloss.Width(label), 0))

	line := mark + body.Render(label)
	if right != "" {
		line += " " + right
	}
	return lipgloss.NewStyle().MaxWidth(w).Render(line)
}

// --- bool ---

// boolEditor is the ◉/○ toggle: two radio rows instead of an opaque [x].
type boolEditor struct {
	m   *Model
	e   Entry
	idx int // 0 = on, 1 = off
}

func newBoolEditor(m *Model, e Entry) *boolEditor {
	b := &boolEditor{m: m, e: e}
	if m.value(e.Key) != "true" {
		b.idx = 1
	}
	return b
}

func (b *boolEditor) Value() any      { return b.idx == 0 }
func (b *boolEditor) Capturing() bool { return false }
func (b *boolEditor) Dirty() bool     { return (b.m.value(b.e.Key) == "true") != (b.idx == 0) }

func (b *boolEditor) Update(key tea.KeyPressMsg) tea.Cmd {
	switch key.String() {
	case "esc":
		b.m.leaveEditor()
	case "up", "k", "down", "j", "left", "h", "right", "l", "space":
		b.idx = 1 - b.idx
		return b.m.writeValue(b.e, b.idx == 0)
	case "enter":
		return b.m.writeValue(b.e, b.idx == 0)
	}
	return nil
}

// Click picks the clicked radio row (#1325): a toggle is exactly the control
// a pointer expects to operate in one press, and the write is staged like
// every other edit, so it is undone by discarding the batch.
func (b *boolEditor) Click(_, y int) tea.Cmd {
	if y != 0 && y != 1 {
		return nil
	}
	b.idx = y
	return b.m.writeValue(b.e, b.idx == 0)
}

func (b *boolEditor) View(w, h int) []string {
	on := b.m.value(b.e.Key) == "true"
	rows := []struct {
		label string
		set   bool
	}{{"on", true}, {"off", false}}
	out := make([]string, 0, 2)
	for i, r := range rows {
		mark := "○"
		if r.set == on {
			mark = "●"
		}
		out = append(out, b.m.editorRow(w, i == b.idx, mark+" "+r.label, ""))
	}
	return out
}

// --- enum ---

// enumEditor is the filterable option list. The current value is marked ●;
// typing narrows the list instead of scrolling a long one (18 themes).
type enumEditor struct {
	navRows   // last rendered height, the pgup/pgdn page (#1666)
	m         *Model
	e         Entry
	idx       int
	filter    string
	filterCur int // rune cursor inside filter (#2002)
	off       int
}

func newEnumEditor(m *Model, e Entry) *enumEditor {
	return &enumEditor{m: m, e: e, idx: optionIndex(e, m.value(e.Key))}
}

// matches returns the options passing the filter, in schema order.
func (n *enumEditor) matches() []string {
	if n.filter == "" {
		return n.e.Options
	}
	needle := strings.ToLower(n.filter)
	var out []string
	for _, o := range n.e.Options {
		if strings.Contains(strings.ToLower(o), needle) {
			out = append(out, o)
		}
	}
	return out
}

func (n *enumEditor) Value() any {
	opts := n.matches()
	if n.idx < 0 || n.idx >= len(opts) {
		return n.m.value(n.e.Key)
	}
	return opts[n.idx]
}

// Capturing is true only while a filter is being typed: with an empty filter
// esc must fall through to the panel and return to the settings column.
func (n *enumEditor) Capturing() bool { return n.filter != "" }
func (n *enumEditor) Dirty() bool     { return n.Value() != n.m.value(n.e.Key) }

// Paste inserts a pasted block into the type-to-filter line at its cursor
// (#2002).
func (n *enumEditor) Paste(text string) bool {
	if !filterPaste(text, &n.filter, &n.filterCur) {
		return false
	}
	n.idx, n.off = 0, 0
	return true
}

func (n *enumEditor) Update(key tea.KeyPressMsg) tea.Cmd {
	opts := n.matches()
	switch key.String() {
	case "esc":
		if n.filter != "" {
			n.filter, n.filterCur = "", 0
			n.idx = optionIndex(n.e, n.m.value(n.e.Key))
			return nil
		}
		n.m.leaveEditor()
		return nil
	case "enter":
		if n.idx >= 0 && n.idx < len(opts) {
			return n.m.writeValue(n.e, opts[n.idx])
		}
		return nil
	}
	// Arrows only: in a type-to-filter list the letters belong to the filter,
	// so "j"/"k" cannot double as motion (a theme called "tokyonight" would
	// be untypeable).
	switch key.String() {
	case "up", "down", "pgup", "pgdown", "home", "end":
		listNav(key.String(), &n.idx, len(opts), n.navPageSize())
		return nil
	}
	if key.Text == " " {
		// Space stays inert: no option name needs it and it would silently
		// empty the list.
		return nil
	}
	// Everything else is shared line editing (#2002).
	if _, changed := filterKey(key, &n.filter, &n.filterCur); changed {
		n.idx = 0
		n.off = 0
		if n.filter != "" {
			n.idx = clamp(n.idx, 0, len(n.matches())-1)
		}
	}
	return nil
}

// Click picks the option under the pointer (#1325). Row 0 is the filter head,
// the rows below are the visible window of the option list.
func (n *enumEditor) Click(_, y int) tea.Cmd {
	opts := n.matches()
	idx := n.off + y - 1
	if y < 1 || idx < 0 || idx >= len(opts) {
		return nil
	}
	n.idx = idx
	return n.m.writeValue(n.e, opts[idx])
}

// Wheel moves the selection, which the option window follows (#1325): the
// window offset is derived from the selection on every render, so scrolling it
// on its own would be undone by the next frame.
func (n *enumEditor) Wheel(delta int) {
	n.idx = clamp(n.idx+delta, 0, len(n.matches())-1)
}

func (n *enumEditor) View(w, h int) []string {
	n.setRows(h)
	pal := n.m.theme()
	dim := lipgloss.NewStyle().Foreground(pal.Secondary)
	clip := lipgloss.NewStyle().MaxWidth(w)
	opts := n.matches()
	cur := n.m.value(n.e.Key)

	head := strconv.Itoa(len(n.e.Options)) + " options · type to filter"
	if n.filter != "" {
		head = "⌕ " + filterView(n.filter, n.filterCur) + " · " + strconv.Itoa(len(opts)) + " of " + strconv.Itoa(len(n.e.Options))
	}
	out := []string{clip.Render(dim.Render(" " + head))}

	listH := h - len(out)
	if listH < 1 {
		listH = 1
	}
	n.idx = clamp(n.idx, 0, len(opts)-1)
	n.off = follow(n.off, n.idx, n.idx, len(opts), listH)
	for i := n.off; i < len(opts) && len(out) < h; i++ {
		mark, tail := "○", ""
		if opts[i] == cur {
			mark, tail = "●", "current"
		}
		if n.e.Preview != nil {
			// The option's inline preview (#1664: theme palette swatches)
			// replaces the tail text — the ● mark already says "current".
			if sw := n.e.Preview(opts[i]); sw != "" {
				tail = sw
			}
		}
		if n.e.RowColors != nil {
			// #1688: the row up to the swatch strip is painted in the option's
			// own colors. Options the callback cannot resolve fall back to the
			// panel's colors but keep the themed layout, so the marker column
			// stays aligned down the whole list.
			fg, bg, ok := n.e.RowColors(opts[i])
			if !ok {
				fg, bg = pal.Foreground, pal.Background
			}
			out = append(out, n.m.themedEditorRow(w, i == n.idx, mark+" "+opts[i], tail, fg, bg))
			continue
		}
		out = append(out, n.m.editorRow(w, i == n.idx, mark+" "+opts[i], tail))
	}
	return out
}

// --- int ---

// intEditor is the ‹ n › stepper: ←→ and +/- step and write, digits type a
// value that enter commits. Both paths clamp to the entry's bounds.
type intEditor struct {
	m   *Model
	e   Entry
	tf  textField
	err string
}

func (n *intEditor) Value() any {
	v, err := strconv.Atoi(strings.TrimSpace(n.tf.text))
	if err != nil {
		return n.m.value(n.e.Key)
	}
	return v
}
func (n *intEditor) Capturing() bool        { return true }
func (n *intEditor) Dirty() bool            { return strings.TrimSpace(n.tf.text) != n.m.value(n.e.Key) }
func (n *intEditor) Paste(text string) bool { return n.tf.Paste(text) }

// snapInt walks v on in delta's direction until the entry's ValidateInt hook
// accepts it (#2085), so a stepper jumps a hole in the valid set — 0 steps up
// to 10 for the forge poll interval, and back down to 0 — instead of stopping
// inside it. Entries without the hook, and a zero delta, return v unchanged;
// the walk is bounded by the entry's own range.
func snapInt(e Entry, v, delta int) int {
	if e.ValidateInt == nil || delta == 0 {
		return v
	}
	span := e.Max - e.Min
	if span < 0 {
		span = 0
	}
	for i := 0; i <= span && e.ValidateInt(v) != ""; i++ {
		if v < e.Min || v > e.Max {
			break
		}
		v += delta
	}
	return v
}

// clampToBounds applies the entry's inclusive range, noting a silent clamp.
func (n *intEditor) clampToBounds(v int) int {
	if n.e.Min == 0 && n.e.Max == 0 {
		return v
	}
	out := clamp(v, n.e.Min, n.e.Max)
	if out != v {
		n.m.notice = "clamped to " + strconv.Itoa(out)
	}
	return out
}

// step adds delta to the current number and writes the result.
func (n *intEditor) step(delta int) tea.Cmd {
	cur, err := strconv.Atoi(strings.TrimSpace(n.tf.text))
	if err != nil {
		cur, _ = strconv.Atoi(strings.TrimSpace(n.m.value(n.e.Key)))
	}
	next := n.clampToBounds(snapInt(n.e, cur+delta, delta))
	n.err = ""
	n.tf.Set(strconv.Itoa(next))
	if next == cur {
		return nil
	}
	return n.m.writeValue(n.e, next)
}

func (n *intEditor) Update(key tea.KeyPressMsg) tea.Cmd {
	switch key.String() {
	case "esc":
		n.m.leaveEditor()
		return nil
	case "left", "-":
		return n.step(-1)
	case "right", "+", "=":
		return n.step(1)
	case "enter":
		v, err := strconv.Atoi(strings.TrimSpace(n.tf.text))
		if err != nil {
			n.err = "not a number"
			return nil
		}
		v = n.clampToBounds(v)
		// A typed value is refused outright rather than snapped (#2085): the
		// user is editing interactively and can fix it, so naming the valid
		// set beats silently writing something else.
		if n.e.ValidateInt != nil {
			if msg := n.e.ValidateInt(v); msg != "" {
				n.err = msg
				return nil
			}
		}
		n.err = ""
		n.tf.Set(strconv.Itoa(v))
		return n.m.writeValue(n.e, v)
	}
	n.tf.Handle(key)
	return nil
}

// Click operates the stepper (#1325): the ‹ and › glyphs step the value, a
// press on the number itself only focuses the field.
func (n *intEditor) Click(x, y int) tea.Cmd {
	if y != 0 {
		return nil
	}
	switch {
	case x <= 2: // " ‹ "
		return n.step(-1)
	case x >= lipgloss.Width(" ‹ "+n.tf.View()+" "):
		return n.step(1)
	}
	return nil
}

func (n *intEditor) View(w, h int) []string {
	pal := n.m.theme()
	dim := lipgloss.NewStyle().Foreground(pal.Secondary)
	clip := lipgloss.NewStyle().MaxWidth(w)
	out := []string{clip.Render(" ‹ " + n.tf.View() + " ›")}
	if n.e.Min != 0 || n.e.Max != 0 {
		out = append(out, clip.Render(dim.Render(" range "+strconv.Itoa(n.e.Min)+"–"+strconv.Itoa(n.e.Max))))
	}
	if n.err != "" {
		out = append(out, clip.Render(lipgloss.NewStyle().Foreground(pal.Error).Render(" ✗ "+n.err)))
	}
	return out
}

// --- text ---

// textEditor is the free-text fallback: the last resort for values without a
// domain to pick from.
type textEditor struct {
	m  *Model
	e  Entry
	tf textField
}

func (t *textEditor) Value() any             { return t.tf.text }
func (t *textEditor) Capturing() bool        { return true }
func (t *textEditor) Dirty() bool            { return t.tf.text != t.m.value(t.e.Key) }
func (t *textEditor) Paste(text string) bool { return t.tf.Paste(text) }

func (t *textEditor) Update(key tea.KeyPressMsg) tea.Cmd {
	switch key.Code {
	case tea.KeyEscape:
		t.tf = newTextField(t.m.value(t.e.Key))
		t.m.leaveEditor()
		return nil
	case tea.KeyEnter:
		return t.m.writeValue(t.e, t.tf.text)
	}
	t.tf.Handle(key)
	return nil
}

func (t *textEditor) View(w, h int) []string {
	clip := lipgloss.NewStyle().MaxWidth(w)
	dim := lipgloss.NewStyle().Foreground(t.m.theme().Secondary)
	return []string{
		clip.Render(" ✎ " + t.tf.View()),
		clip.Render(dim.Render(" enter apply · esc cancel")),
	}
}

// --- path ---

// pathEditor is free text with live completion from the real filesystem, and
// an existence check on commit.
type pathEditor struct {
	m       *Model
	e       Entry
	tf      textField
	suggest pathSuggest
	err     string
}

// resolvablePath reports whether a Path entry's value names something that
// exists. A value with a separator must be a real file or directory; a bare
// name (#1663: terminal.shell = "fish") is accepted when it resolves on PATH,
// because that is exactly how the consumer spawns it.
func resolvablePath(path string) bool {
	if _, err := os.Stat(expandHome(path)); err == nil {
		return true
	}
	if strings.ContainsRune(path, os.PathSeparator) {
		return false
	}
	_, err := exec.LookPath(path)
	return err == nil
}

// resolvableDir is the Dirs-entry check (#1720). A directory-valued setting
// names a folder, so a file is never acceptable and the PATH fallback does not
// apply; a path that does not exist yet passes when its parent directory does,
// because such a value is a target the consumer creates on first use
// (project.directory).
func resolvableDir(path string) bool {
	real := expandHome(path)
	if st, err := os.Stat(real); err == nil {
		return st.IsDir()
	}
	parent := filepath.Dir(strings.TrimRight(real, string(os.PathSeparator)))
	st, err := os.Stat(parent)
	return err == nil && st.IsDir()
}

func (p *pathEditor) Value() any      { return strings.TrimSpace(p.tf.text) }
func (p *pathEditor) Capturing() bool { return true }
func (p *pathEditor) Dirty() bool     { return p.tf.text != p.m.value(p.e.Key) }

// valid is the entry's commit check: directories only for a Dirs entry, any
// resolvable path otherwise.
func (p *pathEditor) valid(path string) bool {
	if p.e.Dirs {
		return resolvableDir(path)
	}
	return resolvablePath(path)
}

func (p *pathEditor) Paste(text string) bool {
	if !p.tf.Paste(text) {
		return false
	}
	p.suggest.refresh(p.tf.text)
	return true
}

func (p *pathEditor) Update(key tea.KeyPressMsg) tea.Cmd {
	switch key.Code {
	case tea.KeyEscape:
		p.tf = newTextField(p.m.value(p.e.Key))
		p.suggest.clear()
		p.m.leaveEditor()
		return nil
	case tea.KeyTab:
		p.tf.Set(p.suggest.complete(p.tf.text))
		return nil
	case tea.KeyEnter:
		path := strings.TrimSpace(p.tf.text)
		if path != "" && !p.valid(path) {
			p.err = "path does not exist"
			if p.e.Dirs {
				p.err = "not a directory"
			}
			return nil
		}
		p.err = ""
		p.suggest.clear()
		return p.m.writeValue(p.e, path)
	}
	if _, changed := p.tf.Handle(key); changed {
		p.suggest.refresh(p.tf.text)
	}
	return nil
}

// Click takes a completion suggestion (#1325): row 0 is the input, an error
// line may follow it, and the suggestion rows come after — the same order
// View renders them in.
func (p *pathEditor) Click(_, y int) tea.Cmd {
	first := 1
	if p.err != "" {
		first = 2
	}
	if idx := y - first; idx >= 0 && idx < len(p.suggest.candidates) && idx < maxSuggestLines {
		p.tf.Set(p.suggest.candidates[idx])
		p.suggest.refresh(p.tf.text)
	}
	return nil
}

func (p *pathEditor) View(w, h int) []string {
	pal := p.m.theme()
	clip := lipgloss.NewStyle().MaxWidth(w)
	dim := lipgloss.NewStyle().Foreground(pal.Secondary)
	out := []string{clip.Render(" ✎ " + p.tf.View())}
	if p.err != "" {
		out = append(out, clip.Render(lipgloss.NewStyle().Foreground(pal.Error).Render(" ✗ "+p.err)))
	}
	for _, l := range p.suggest.lines() {
		if len(out) >= h-1 {
			break
		}
		out = append(out, clip.Render(dim.Render(l)))
	}
	if len(out) < h {
		out = append(out, clip.Render(dim.Render(" tab complete · enter apply · esc cancel")))
	}
	return out
}

// --- chord ---

// chordEditor shows the bound chord and hands the actual recording to the
// shared capture sub-panel, which disables the panel's own keys so esc and tab
// can be recorded too.
type chordEditor struct {
	m *Model
	e Entry
}

func (c *chordEditor) Value() any      { return c.m.value(c.e.Key) }
func (c *chordEditor) Capturing() bool { return false }
func (c *chordEditor) Dirty() bool     { return false }

func (c *chordEditor) Update(key tea.KeyPressMsg) tea.Cmd {
	switch key.String() {
	case "esc":
		c.m.leaveEditor()
	case "enter":
		c.m.Push(newChordCapture(c.m, c.m.opts, c.m.scopeFor(c.e), c.e.Key, c.e.Title, c.m.pal))
	}
	return nil
}

// Click opens the capture dialog from the chord row (#1325) — the same thing
// enter does; the hint line below it is not a target.
func (c *chordEditor) Click(_, y int) tea.Cmd {
	if y != 0 {
		return nil
	}
	c.m.Push(newChordCapture(c.m, c.m.opts, c.m.scopeFor(c.e), c.e.Key, c.e.Title, c.m.pal))
	return nil
}

func (c *chordEditor) View(w, h int) []string {
	clip := lipgloss.NewStyle().MaxWidth(w)
	dim := lipgloss.NewStyle().Foreground(c.m.theme().Secondary)
	cur := c.m.value(c.e.Key)
	if cur == "" {
		cur = "(unbound)"
	}
	return []string{
		clip.Render(" ⌨ " + cur),
		clip.Render(dim.Render(" enter record a new chord")),
	}
}

// --- list ---

// listEditor is the indexed multi-value editor: one row per value with a
// remove key, plus an add row — instead of a comma-joined string the user has
// to re-type to change one element. An IntList entry (#1663) runs the same
// editor over a []int config field: elements are rejected unless they parse as
// numbers, and the commit writes a TOML integer array.
type listEditor struct {
	navRows // last rendered height, the pgup/pgdn page (#1666)
	m       *Model
	e       Entry
	items   []string
	idx     int // == len(items) selects the "+ add value…" row
	editing bool
	tf      textField
	// numeric marks the IntList variant; err carries its rejection message.
	numeric bool
	err     string
}

func newListEditor(m *Model, e Entry) *listEditor {
	return &listEditor{m: m, e: e, items: splitList(m.value(e.Key)), numeric: e.Type == IntList}
}

// splitList parses the flat rendering of a list value ("[a b]" from the typed
// schema, or a comma-separated string) into its elements.
func splitList(v string) []string {
	v = strings.TrimSpace(v)
	v = strings.TrimPrefix(v, "[")
	v = strings.TrimSuffix(v, "]")
	sep := ","
	if !strings.Contains(v, ",") {
		sep = " "
	}
	var out []string
	for _, p := range strings.Split(v, sep) {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

func (l *listEditor) Value() any {
	if l.numeric {
		return toInts(l.items)
	}
	return append([]string{}, l.items...)
}
func (l *listEditor) Capturing() bool { return l.editing }
func (l *listEditor) Dirty() bool {
	return strings.Join(l.items, ",") != strings.Join(splitList(l.m.value(l.e.Key)), ",")
}

func (l *listEditor) Paste(text string) bool {
	if !l.editing {
		return false
	}
	return l.tf.Paste(text)
}

// toInts converts the item strings to the TOML integer array an IntList entry
// persists (#1663). Elements are validated on entry, so a stray unparseable one
// is dropped rather than failing the whole write.
func toInts(items []string) []int {
	out := make([]int, 0, len(items))
	for _, it := range items {
		if n, err := strconv.Atoi(strings.TrimSpace(it)); err == nil {
			out = append(out, n)
		}
	}
	return out
}

// write persists the current items as a TOML string array (#1139), or as an
// integer array for an IntList entry (#1663).
func (l *listEditor) write() tea.Cmd {
	if l.numeric {
		return l.m.writeValue(l.e, toInts(l.items))
	}
	items := l.items
	if items == nil {
		items = []string{}
	}
	return l.m.writeValue(l.e, items)
}

func (l *listEditor) Update(key tea.KeyPressMsg) tea.Cmd {
	if l.editing {
		switch key.Code {
		case tea.KeyEscape:
			l.editing, l.err = false, ""
			return nil
		case tea.KeyEnter:
			text := strings.TrimSpace(l.tf.text)
			// An IntList element must be a number: reject in place instead of
			// staging a value the typed decode would refuse (#1663).
			if l.numeric && text != "" {
				if _, err := strconv.Atoi(text); err != nil {
					l.err = "not a number"
					return nil
				}
			}
			// A schema-declared element check (#1946, tools.layout.assign):
			// reject in place with the message naming the valid values.
			if l.e.ValidateEntry != nil && text != "" {
				if msg := l.e.ValidateEntry(l.m.value, text); msg != "" {
					l.err = msg
					return nil
				}
			}
			l.err = ""
			l.editing = false
			switch {
			case text == "" && l.idx < len(l.items):
				l.items = append(l.items[:l.idx], l.items[l.idx+1:]...)
			case text == "":
				return nil
			case l.idx < len(l.items):
				l.items[l.idx] = text
			default:
				l.items = append(l.items, text)
			}
			return l.write()
		}
		l.tf.Handle(key)
		return nil
	}
	switch key.String() {
	case "esc":
		l.m.leaveEditor()
		return nil
	case "enter":
		l.editing = true
		if l.idx < len(l.items) {
			l.tf = newTextField(l.items[l.idx])
		} else {
			l.tf = newTextField("")
		}
		return nil
	case "d":
		if l.idx < len(l.items) {
			l.items = append(l.items[:l.idx], l.items[l.idx+1:]...)
			l.idx = clamp(l.idx, 0, len(l.items))
			return l.write()
		}
		return nil
	}
	listNav(key.String(), &l.idx, len(l.items)+1, l.navPageSize())
	return nil
}

// Click selects the value row under the pointer, and a press on the row that
// is already selected starts editing it — the settings column's own
// select-then-activate rule (#1325), so a stray click never opens an input.
// The "+ add value…" row is the last one.
func (l *listEditor) Click(_, y int) tea.Cmd {
	if l.editing || y < 0 || y > len(l.items) {
		return nil
	}
	if y == l.idx {
		l.editing = true
		if l.idx < len(l.items) {
			l.tf = newTextField(l.items[l.idx])
		} else {
			l.tf = newTextField("")
		}
		return nil
	}
	l.idx = y
	return nil
}

// Wheel walks the value rows, the add row included.
func (l *listEditor) Wheel(delta int) {
	if !l.editing {
		l.idx = clamp(l.idx+delta, 0, len(l.items))
	}
}

func (l *listEditor) View(w, h int) []string {
	l.setRows(h)
	pal := l.m.theme()
	clip := lipgloss.NewStyle().MaxWidth(w)
	dim := lipgloss.NewStyle().Foreground(pal.Secondary)
	out := make([]string, 0, len(l.items)+2)
	for i, it := range l.items {
		text := it
		if l.editing && i == l.idx {
			text = l.tf.View()
		}
		label := strconv.Itoa(i+1) + "  " + text
		tail := ""
		if i == l.idx && !l.editing {
			tail = "d remove"
		}
		out = append(out, l.m.editorRow(w, i == l.idx, label, tail))
	}
	add := "+  add value…"
	if l.editing && l.idx == len(l.items) {
		add = "+  " + l.tf.View()
	}
	out = append(out, l.m.editorRow(w, l.idx == len(l.items), add, ""))
	// Value hints while typing (#1946): the schema-declared candidates for
	// the element under edit, re-narrowed on every keystroke.
	if l.editing && l.e.EntryHints != nil {
		for _, row := range hintRows(l.e.EntryHints(l.m.value, strings.TrimSpace(l.tf.text))) {
			if len(out) >= h {
				break
			}
			out = append(out, clip.Render(dim.Render(row)))
		}
	}
	if l.err != "" && len(out) < h {
		out = append(out, clip.Render(lipgloss.NewStyle().Foreground(pal.Error).Render(" ✗ "+l.err)))
	}
	if len(out) < h {
		out = append(out, clip.Render(dim.Render(" enter edit · d remove")))
	}
	return out
}
