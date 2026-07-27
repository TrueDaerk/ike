package settings

import (
	"os"
	"strconv"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
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
	case List:
		return newListEditor(m, e)
	case Path:
		ed := &pathEditor{m: m, e: e, tf: newTextField(m.value(e.Key))}
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
	m      *Model
	e      Entry
	idx    int
	filter string
	off    int
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

func (n *enumEditor) Update(key tea.KeyPressMsg) tea.Cmd {
	opts := n.matches()
	switch key.String() {
	case "esc":
		if n.filter != "" {
			n.filter, n.idx = "", optionIndex(n.e, n.m.value(n.e.Key))
			return nil
		}
		n.m.leaveEditor()
		return nil
	case "enter":
		if n.idx >= 0 && n.idx < len(opts) {
			return n.m.writeValue(n.e, opts[n.idx])
		}
		return nil
	case "backspace":
		if n.filter != "" {
			r := []rune(n.filter)
			n.filter = string(r[:len(r)-1])
			n.idx = clamp(n.idx, 0, len(n.matches())-1)
		}
		return nil
	}
	// Arrows only: in a type-to-filter list the letters belong to the filter,
	// so "j"/"k" cannot double as motion (a theme called "tokyonight" would
	// be untypeable).
	switch key.String() {
	case "up", "down", "pgup", "pgdown", "home", "end":
		listNav(key.String(), &n.idx, len(opts), navPage)
		return nil
	}
	if key.Text != "" && key.Text != " " {
		n.filter += key.Text
		n.idx = 0
		n.off = 0
	}
	return nil
}

func (n *enumEditor) View(w, h int) []string {
	pal := n.m.theme()
	dim := lipgloss.NewStyle().Foreground(pal.Secondary)
	clip := lipgloss.NewStyle().MaxWidth(w)
	opts := n.matches()
	cur := n.m.value(n.e.Key)

	head := strconv.Itoa(len(n.e.Options)) + " options · type to filter"
	if n.filter != "" {
		head = "⌕ " + n.filter + "▌ · " + strconv.Itoa(len(opts)) + " of " + strconv.Itoa(len(n.e.Options))
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
	next := n.clampToBounds(cur + delta)
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
		n.err = ""
		v = n.clampToBounds(v)
		n.tf.Set(strconv.Itoa(v))
		return n.m.writeValue(n.e, v)
	}
	n.tf.Handle(key)
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

func (p *pathEditor) Value() any      { return strings.TrimSpace(p.tf.text) }
func (p *pathEditor) Capturing() bool { return true }
func (p *pathEditor) Dirty() bool     { return p.tf.text != p.m.value(p.e.Key) }

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
		if path != "" {
			if _, err := os.Stat(expandHome(path)); err != nil {
				p.err = "path does not exist"
				return nil
			}
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
// to re-type to change one element.
type listEditor struct {
	m       *Model
	e       Entry
	items   []string
	idx     int // == len(items) selects the "+ add value…" row
	editing bool
	tf      textField
}

func newListEditor(m *Model, e Entry) *listEditor {
	return &listEditor{m: m, e: e, items: splitList(m.value(e.Key))}
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

func (l *listEditor) Value() any      { return append([]string{}, l.items...) }
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

// write persists the current items as a TOML string array (#1139).
func (l *listEditor) write() tea.Cmd {
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
			l.editing = false
			return nil
		case tea.KeyEnter:
			l.editing = false
			text := strings.TrimSpace(l.tf.text)
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
	listNav(key.String(), &l.idx, len(l.items)+1, navPage)
	return nil
}

func (l *listEditor) View(w, h int) []string {
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
	if len(out) < h {
		out = append(out, clip.Render(dim.Render(" enter edit · d remove")))
	}
	return out
}
