package palette

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
)

// selection_test.go covers the highlighted-row seams of #2252: the debounce
// that tells a mode which row settled, and the footer such a mode renders
// under the list.

// selMode is a SelectionMode + FooterMode stub: it records the rows it was
// told about and renders a fixed footer for the selected one.
type selMode struct {
	titles []string
	seen   []string
	footer []string
}

func (s *selMode) Prefix() rune        { return '!' }
func (s *selMode) Placeholder() string { return "" }

func (s *selMode) Results(query string, _ Context) []Item {
	var out []Item
	for _, t := range s.titles {
		if query != "" && !strings.Contains(t, query) {
			continue
		}
		out = append(out, Item{Title: t, Msg: RunCommandMsg{ID: t}})
	}
	return out
}

func (s *selMode) SelectionChanged(sel Item, _ Context) tea.Cmd {
	s.seen = append(s.seen, sel.Title)
	return nil
}

func (s *selMode) Footer(sel Item, width int) []string {
	if s.footer == nil {
		return nil
	}
	return append([]string{"footer for " + sel.Title}, s.footer...)
}

// selPalette opens a locked palette over the stub.
func selPalette(t *testing.T, mode *selMode) *Palette {
	t.Helper()
	p := New(Config{}, mode)
	p.SetSize(80, 24)
	p.OpenLocked(Context{}, '!')
	return p
}

// tick runs a scheduled debounce command and feeds the resulting tick back.
func tick(t *testing.T, p *Palette, cmd tea.Cmd) {
	t.Helper()
	if cmd == nil {
		t.Fatal("a highlight move must schedule the selection debounce")
	}
	msg, ok := cmd().(SelectionTickMsg)
	if !ok {
		t.Fatalf("scheduled %#v, want a SelectionTickMsg", cmd())
	}
	p.SelectionTick(msg)
}

// TestSelectionTickReportsTheSettledRow: the mode learns which row the
// highlight rests on, once the debounce fires.
func TestSelectionTickReportsTheSettledRow(t *testing.T) {
	mode := &selMode{titles: []string{"Alpha", "Beta", "Gamma"}}
	p := selPalette(t, mode)

	tick(t, p, p.SelectionKick())
	tick(t, p, p.Update(tea.KeyPressMsg{Code: tea.KeyDown}))
	if got := strings.Join(mode.seen, ","); got != "Alpha,Beta" {
		t.Fatalf("reported rows = %q, want the opened row then the moved-to one", got)
	}
}

// TestSelectionDebounceDropsRowsScrolledPast is the debounce contract: a burst
// of moves resolves only the row the highlight ends on — the ticks scheduled
// by the earlier steps are stale and answer nothing.
func TestSelectionDebounceDropsRowsScrolledPast(t *testing.T) {
	mode := &selMode{titles: []string{"Alpha", "Beta", "Gamma", "Delta"}}
	p := selPalette(t, mode)

	first := p.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	second := p.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	// The stale tick fires after the second move: it must be ignored.
	tick(t, p, first)
	if len(mode.seen) != 0 {
		t.Fatalf("a stale tick reported %v — only the settled row may resolve", mode.seen)
	}
	tick(t, p, second)
	if got := strings.Join(mode.seen, ","); got != "Gamma" {
		t.Fatalf("reported rows = %q, want only the row the burst ended on", got)
	}
}

// TestSelectionTickIgnoredAfterClose: a tick arriving once the popup is gone
// resolves nothing.
func TestSelectionTickIgnoredAfterClose(t *testing.T) {
	mode := &selMode{titles: []string{"Alpha", "Beta"}}
	p := selPalette(t, mode)

	cmd := p.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	p.Close()
	tick(t, p, cmd)
	if len(mode.seen) != 0 {
		t.Fatalf("a closed palette reported %v", mode.seen)
	}
}

// TestQueryEditSchedulesSelectionTick: typing reshuffles the list, so the row
// under the highlight changed and its mode is told about the new one.
func TestQueryEditSchedulesSelectionTick(t *testing.T) {
	mode := &selMode{titles: []string{"Alpha", "Beta", "Gamma"}}
	p := selPalette(t, mode)

	cmd := p.Update(tea.KeyPressMsg{Code: 'G', Text: "G"})
	if cmd == nil {
		t.Fatal("a query edit must schedule the selection debounce")
	}
	// The batch holds the (nil) live kick and the selection tick.
	for _, m := range flatten(cmd()) {
		if st, ok := m.(SelectionTickMsg); ok {
			p.SelectionTick(st)
		}
	}
	if got := strings.Join(mode.seen, ","); got != "Gamma" {
		t.Fatalf("reported rows = %q, want the row the filtered list highlights", got)
	}
}

// flatten runs a possibly batched command down to the messages it produced.
func flatten(msg tea.Msg) []tea.Msg {
	batch, ok := msg.(tea.BatchMsg)
	if !ok {
		return []tea.Msg{msg}
	}
	var out []tea.Msg
	for _, c := range batch {
		if c == nil {
			continue
		}
		out = append(out, flatten(c())...)
	}
	return out
}

// TestFooterModeRendersUnderTheList: a FooterMode's lines appear in the box,
// under the results, for the selected row.
func TestFooterModeRendersUnderTheList(t *testing.T) {
	mode := &selMode{titles: []string{"Alpha", "Beta"}, footer: []string{"- was", "+ is"}}
	p := selPalette(t, mode)

	view := p.View()
	if !strings.Contains(view, "footer for Alpha") || !strings.Contains(view, "+ is") {
		t.Fatalf("view must render the selected row's footer:\n%s", view)
	}
	rows := strings.Split(view, "\n")
	listAt, footAt := -1, -1
	for i, r := range rows {
		if strings.Contains(r, "Beta") {
			listAt = i
		}
		if strings.Contains(r, "footer for Alpha") {
			footAt = i
		}
	}
	if listAt < 0 || footAt < 0 || footAt < listAt {
		t.Fatalf("the footer must render under the result rows (list %d, footer %d):\n%s", listAt, footAt, view)
	}
}

// TestFooterAreaIsInertToClicks: a press in the footer must not activate the
// row a scrolled list happens to hold at that offset.
func TestFooterAreaIsInertToClicks(t *testing.T) {
	mode := &selMode{
		titles: []string{"Alpha", "Beta", "Gamma", "Delta", "Epsilon", "Zeta"},
		footer: []string{"- was", "+ is"},
	}
	p := New(Config{MaxResults: 3}, mode)
	p.SetSize(80, 24)
	p.OpenLocked(Context{}, '!')

	// Three visible rows at y == 2..4, so y == 6 lies in the footer — while
	// the unscrolled list still *has* a row at that offset, which is exactly
	// what a press there must not activate.
	if cmd := p.Click(4, 6+1); cmd != nil {
		t.Fatalf("a click in the footer emitted %#v", cmd())
	}
	if !p.IsOpen() {
		t.Fatal("a click in the footer must not close the popup")
	}
}
