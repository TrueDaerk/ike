// Package filterbar is the one-line filter input every list pane wears since
// #2156: the Problems, Usages and TODO index panes all render this row, all
// bind "/" to focus it, and all read the parsed result back as a
// filterexpr.Query. The pane supplies the schema — which fields it
// understands and what they take — and nothing else about the widget differs
// between panes, which is the point: the syntax the Issues pane taught
// (#2110/#2115) is typed the same way everywhere.
//
// The row is permanent, like the Issues pane's filter row (#2104): a filter
// appearing must not shift the list by a line. While unfocused it renders the
// current expression (or a hint naming the pane's fields); while focused it
// renders the text cursor, an inline completion ghost tab accepts, and the
// parse error of a half-written expression.
//
// The bar owns no list state. A pane calls Key while the bar is focused,
// re-derives its rows when Key reports a change, and gates each row through
// its own matcher over Query.
package filterbar

import (
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"ike/internal/filterexpr"
	"ike/internal/theme"
	"ike/internal/ui"
)

// Model is one pane's filter row.
type Model struct {
	schema filterexpr.Schema

	text   string
	cur    int
	active bool

	query filterexpr.Query
	err   string

	// status is the match counter the focused row shows in front of its key
	// hints (#2410): "3/17" after a cmd+g step, "1/12 (wrapped)" for the one
	// that came back around. The pane owns the count — the bar holds no list
	// state — and every text change drops it, so a stale number can never
	// outlive the rows it counted.
	status string

	// Candidates supplies a field's completion values when the schema has no
	// closed vocabulary for it (tag names the pane configured, the paths it
	// currently lists). Unset falls back to the schema's own vocabulary.
	Candidates func(field string) []string
}

// New returns an empty, unfocused bar over schema.
func New(schema filterexpr.Schema) Model { return Model{schema: schema} }

// Schema returns the field set the bar parses against.
func (m *Model) Schema() filterexpr.Schema { return m.schema }

// SetSchema replaces the field set and re-parses (the TODO index rebuilds its
// tag vocabulary when the configured patterns change).
func (m *Model) SetSchema(s filterexpr.Schema) {
	m.schema = s
	m.parse()
}

// Active reports whether the input has focus and is consuming keys.
func (m *Model) Active() bool { return m.active }

// Focus moves the cursor into the input, at the end of the text.
func (m *Model) Focus() {
	m.active = true
	m.cur = len([]rune(m.text))
	m.status = ""
}

// SetStatus records the match counter the focused row shows (#2410); the pane
// writes it from its NextMatch/PrevMatch step. "" hides it again.
func (m *Model) SetStatus(s string) { m.status = s }

// ShowStep is SetStatus in the shape a pane's NextMatch/PrevMatch already
// has, and returns it unchanged so the pane can `return m.filter.ShowStep(…)`.
// A step over nothing shows no counter — the caller's hint says why.
func (m *Model) ShowStep(st ui.MatchStep) ui.MatchStep {
	if st.Total == 0 {
		m.status = ""
		return st
	}
	m.status = ui.MatchCounter(st.Index, st.Total, st.Wrapped)
	return st
}

// Status returns the counter currently shown (tests).
func (m *Model) Status() string { return m.status }

// Blur leaves the input, keeping the filter.
func (m *Model) Blur() { m.active = false }

// Text is the raw expression.
func (m *Model) Text() string { return m.text }

// Query is the last successful parse. A half-written expression keeps the
// previous query, so the list does not empty out mid-token.
func (m *Model) Query() filterexpr.Query { return m.query }

// Err is the parse error of the current text, "" when it parses.
func (m *Model) Err() string { return m.err }

// Empty reports whether the filter narrows nothing.
func (m *Model) Empty() bool { return m.query.Empty() }

// SetText replaces the expression, reporting whether anything changed. It is
// the seam the single-key shortcuts write through, so a quick filter and a
// typed one are the same filter (#2156).
func (m *Model) SetText(s string) bool {
	if s == m.text {
		return false
	}
	m.text = s
	m.cur = len([]rune(m.text))
	m.parse()
	return true
}

// Clear empties the filter, reporting whether anything changed.
func (m *Model) Clear() bool { return m.SetText("") }

// SetTerm rewrites the expression so field carries exactly value, dropping
// every other term of that field and leaving the rest of the expression (and
// the match text) alone. An empty value removes the field. It reports whether
// anything changed — the shape a single-key toggle wants.
//
// It rewrites from the last *parsed* query, so pressing a quick key while an
// expression is half-written normalizes the line back to what last parsed —
// the only reading under which the key's own term is guaranteed to land.
func (m *Model) SetTerm(field, value string) bool {
	q := m.query
	terms := make([]filterexpr.Term, 0, len(q.Terms)+1)
	for _, t := range q.Terms {
		if t.Field != field {
			terms = append(terms, t)
		}
	}
	if value != "" {
		terms = append(terms, filterexpr.Term{Field: field, Value: value})
	}
	q.Terms = terms
	return m.SetText(filterexpr.Format(q))
}

// HasTerm reports whether the current filter carries field:value.
func (m *Model) HasTerm(field, value string) bool {
	for _, t := range m.query.Terms {
		if t.Field == field && t.Value == value {
			return true
		}
	}
	return false
}

// Key applies one key while the bar is focused. handled is false for a key
// the bar does not own, so the pane can act on it; changed reports that the
// filter moved and the rows need re-deriving.
//
// enter applies and leaves the input; esc clears the filter and leaves it —
// one predictable escape, the same in every pane.
func (m *Model) Key(msg tea.KeyPressMsg) (handled, changed bool) {
	if !m.active {
		return false, false
	}
	switch msg.String() {
	case "enter":
		m.Blur()
		return true, false
	case "esc":
		m.Blur()
		return true, m.Clear()
	case "tab":
		if ghost := m.Completion(); ghost != "" {
			m.text += ghost
			m.cur = len([]rune(m.text))
			m.parse()
			return true, true
		}
		return true, false
	}
	text, cur, ok, ch := ui.EditKey(msg, m.text, m.cur)
	if !ok {
		return false, false
	}
	m.text, m.cur = text, cur
	if ch {
		m.parse()
	}
	return true, ch
}

// Paste inserts a pasted block at the cursor (#1273's single-line rule),
// reporting whether the filter changed.
func (m *Model) Paste(paste string) bool {
	if !m.active {
		return false
	}
	text, cur, changed := ui.PasteText(m.text, m.cur, paste)
	m.text, m.cur = text, cur
	if changed {
		m.parse()
	}
	return changed
}

// parse re-reads the text. A failing parse keeps the last good query and
// records the message — the row explains it, the list stays put.
func (m *Model) parse() {
	m.status = "" // an edited filter starts a fresh walk (#2410)
	q, err := m.schema.Parse(m.text)
	if err != nil {
		m.err = err.Error()
		return
	}
	m.err = ""
	m.query = q
}

// Completion is the inline ghost after the cursor that tab accepts: the rest
// of a field name ("sev" → "erity:"), or the rest of a value. Only offered
// with the cursor at the end of the input, on the first candidate the typed
// prefix matches. A completed value ends in the terminating space.
func (m *Model) Completion() string {
	if !m.active || m.cur != len([]rune(m.text)) || m.text == "" {
		return ""
	}
	tok, ok := lastToken(m.text)
	if !ok {
		return ""
	}
	if i := strings.IndexByte(tok, ':'); i > 0 {
		return m.completeValue(strings.ToLower(tok[:i]), tok[i+1:])
	}
	low := strings.ToLower(tok)
	for _, name := range m.schema.Names() {
		if strings.HasPrefix(name, low) && len(low) < len(name) {
			return name[len(low):]
		}
	}
	return ""
}

// lastToken is the token the cursor sits at the end of — the one being typed
// — keeping quoted spans together the way filterexpr.Tokenize does, and with
// the quotes left in so completeValue can see whether the value was opened
// with one. ok is false when the line ends in a separator (nothing is being
// typed) or is empty.
func lastToken(text string) (string, bool) {
	start, inQuote := 0, false
	for i, r := range text {
		switch {
		case r == '"':
			inQuote = !inQuote
		case !inQuote && (r == ' ' || r == '\t'):
			start = i + 1
		}
	}
	if start >= len(text) {
		return "", false
	}
	return text[start:], true
}

// candidates lists a field's completion values: the pane's own source when it
// injected one, else the schema's vocabulary.
func (m *Model) candidates(name string) []string {
	f, ok := m.schema.Lookup(name)
	if !ok {
		return nil
	}
	if m.Candidates != nil {
		if out := m.Candidates(f.Name); len(out) > 0 {
			return out
		}
	}
	return f.Values
}

// completeValue is the ghost for a value prefix. A candidate with spaces is
// only offered once the value was opened with a quote — the ghost extends the
// input at the cursor and cannot retro-insert the opening quote.
func (m *Model) completeValue(name, val string) string {
	typed := strings.ReplaceAll(val, `"`, "")
	quoted := strings.HasPrefix(val, `"`)
	for _, cand := range m.candidates(name) {
		if !strings.HasPrefix(cand, typed) || cand == typed {
			continue
		}
		rest := cand[len(typed):]
		if quoted {
			return rest + `" `
		}
		if strings.ContainsRune(cand, ' ') {
			continue
		}
		return rest + " "
	}
	return ""
}

// Hint is the idle row's advice: the fields this pane understands, canonical
// names only — the aliases are for the completion to offer, not for a hint to
// spend width on.
func (m *Model) Hint() string {
	names := make([]string, 0, len(m.schema.Fields))
	for _, f := range m.schema.Fields {
		names = append(names, f.Name+":")
	}
	if len(names) == 0 {
		return "/ filter"
	}
	return "/ filter — " + strings.Join(names, " ") + " or plain text"
}

// View renders the row, clipped to width. The focused row shows the cursor,
// the ghost and any parse error; the idle row shows the expression, or the
// hint when there is none.
func (m *Model) View(width int, pal *theme.Palette) string {
	if pal == nil {
		pal = theme.DefaultPalette()
	}
	dim := lipgloss.NewStyle().Faint(true)
	if !m.active {
		if m.text == "" {
			return clip(dim.Render(" "+m.Hint()), width)
		}
		body := lipgloss.NewStyle().Foreground(pal.Accent).Render(" filter: " + m.text)
		if m.err != "" {
			body += dim.Render("  " + m.err)
		}
		return clip(body, width)
	}
	body := " filter: " + ui.CursorView(m.text, m.cur)
	if ghost := m.Completion(); ghost != "" {
		body += dim.Render(ghost)
	}
	if m.err != "" {
		body += lipgloss.NewStyle().Foreground(pal.Error).Render("  " + m.err)
	} else {
		hints := "enter apply · esc clear · tab complete"
		if m.status != "" {
			// Where the cmd+g walk stands, wrap included (#2410) — ahead of
			// the hints, which are the first thing a narrow pane clips.
			hints = m.status + " · " + hints
		}
		body += dim.Render("  " + hints)
	}
	return clip(body, width)
}

// clip hard-caps a styled row to width cells. ansi.Truncate, not lipgloss
// MaxWidth — MaxWidth wraps overlong content onto a second line, which would
// shift every row below it.
func clip(s string, width int) string {
	if width <= 0 {
		return s
	}
	return ansi.Truncate(s, width, "…")
}
