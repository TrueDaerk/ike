package app

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"ike/internal/jqplay"
)

// playcomplete_test.go covers the query line's completion popup (#1979): open
// on the trigger runes, filter as you type, the editor-consistent accept /
// dismiss / navigation keys, and the shadowing of the query line's own keys
// while the popup shows.

// dismissJQPopup closes an open completion popup the way a user does, so the
// next enter/↑/esc reaches the query line again.
func dismissJQPopup(m Model) Model {
	if m.play != nil && m.play.comp != nil {
		m = drainKey(m, tea.KeyPressMsg{Code: tea.KeyEscape})
	}
	return m
}

// playCompLabels flattens the open popup's labels for assertions.
func playCompLabels(m Model) []string {
	if m.play == nil || m.play.comp == nil {
		return nil
	}
	out := make([]string, len(m.play.comp.items))
	for i, it := range m.play.comp.items {
		out[i] = it.Label
	}
	return out
}

// TestJQCompletionOpensOnDotAndFilters is the issue's acceptance case: `.`
// offers the input's top-level keys, and typing narrows the list in place.
func TestJQCompletionOpensOnDotAndFilters(t *testing.T) {
	m := openJQ(t, playApp(t, `{"name":"ike","nation":"x","other":1}`))
	m.play.program, m.play.pos = "", 0
	m = typeInto(m, ".")
	if got := playCompLabels(m); strings.Join(got, " ") != "name nation other" {
		t.Fatalf("`.` offered %v, want the top-level keys", got)
	}
	m = typeInto(m, "na")
	if got := playCompLabels(m); strings.Join(got, " ") != "name nation" {
		t.Fatalf("`.na` offered %v, want the matching keys", got)
	}
	// A partial nothing matches closes the popup, like the editor's.
	m = typeInto(m, "zz")
	if m.play.comp != nil {
		t.Error("a matchless partial must close the popup")
	}
}

// TestJQCompletionAccept: enter writes the selected key over the partial,
// closes the popup and re-evaluates — and does not record-and-run, which is
// what enter means only once the popup is gone.
func TestJQCompletionAccept(t *testing.T) {
	m := openJQ(t, playApp(t, `{"name":"ike","nation":"x"}`))
	m.play.program, m.play.pos = "", 0
	m = typeInto(m, ".nam")
	if m.play.comp == nil {
		t.Fatal("the popup must be open on a matching partial")
	}
	m = drainKey(m, tea.KeyPressMsg{Code: tea.KeyEnter})
	if got := m.play.program; got != ".name" {
		t.Fatalf("accept wrote %q, want the completed path", got)
	}
	if m.play.comp != nil {
		t.Error("accepting must close the popup")
	}
	if got := m.play.result.Text(); got != `"ike"` {
		t.Errorf("result = %q, accepting must re-evaluate", got)
	}
	if m.play.hist.Len() != 0 {
		t.Error("the accept enter must not record the program in the history")
	}
}

// TestJQCompletionTabAccepts: a plain tab accepts like in the editor; only
// with the popup closed does tab move the keyboard into the result buffer.
func TestJQCompletionTabAccepts(t *testing.T) {
	m := openJQ(t, playApp(t, `{"name":"ike"}`))
	m.play.program, m.play.pos = "", 0
	m = typeInto(m, ".na")
	m = drainKey(m, tea.KeyPressMsg{Code: tea.KeyTab})
	if got := m.play.program; got != ".name" {
		t.Fatalf("tab accept wrote %q", got)
	}
	if m.play.bufFocus {
		t.Fatal("tab under the popup must accept, not switch the focus")
	}
	m = drainKey(m, tea.KeyPressMsg{Code: tea.KeyTab})
	if !m.play.bufFocus {
		t.Error("with the popup closed tab must reach the result buffer again")
	}
}

// TestJQCompletionDismiss: esc closes only the popup; the playground and the
// program on the query line stand.
func TestJQCompletionDismiss(t *testing.T) {
	m := openJQ(t, playApp(t, `{"name":"ike"}`))
	m.play.program, m.play.pos = "", 0
	m = typeInto(m, ".na")
	m = drainKey(m, tea.KeyPressMsg{Code: tea.KeyEscape})
	if m.play == nil || m.play.comp != nil {
		t.Fatal("esc must dismiss the popup and leave the mode open")
	}
	if got := m.play.program; got != ".na" {
		t.Fatalf("dismiss must keep the typed program, got %q", got)
	}
	m = drainKey(m, tea.KeyPressMsg{Code: tea.KeyEscape})
	if m.playOpen() {
		t.Error("the next esc must close the playground as before")
	}
}

// TestJQCompletionNavigation: the arrows and ctrl+n/ctrl+p step the selection
// (wrapping), shadowing the history walk while the popup shows.
func TestJQCompletionNavigation(t *testing.T) {
	m := openJQ(t, playApp(t, `{"alpha":1,"beta":2}`))
	m = runJQProgram(m, ".alpha") // a history entry ↑ would otherwise restore
	m.play.program, m.play.pos = "", 0
	m = typeInto(m, ".")
	m = drainKey(m, tea.KeyPressMsg{Code: tea.KeyDown})
	m = drainKey(m, tea.KeyPressMsg{Code: tea.KeyEnter})
	if got := m.play.program; got != ".beta" {
		t.Fatalf("↓ then accept wrote %q, want the second key", got)
	}
	if got := m.play.histIdx; got != -1 {
		t.Errorf("the popup's arrows must not walk the history, histIdx = %d", got)
	}

	// ↑ from the top wraps to the last entry, ui.StepIndex's rule.
	m.play.program, m.play.pos = "", 0
	m = typeInto(m, ".")
	m = drainKey(m, tea.KeyPressMsg{Code: tea.KeyUp})
	m = drainKey(m, tea.KeyPressMsg{Code: tea.KeyEnter})
	if got := m.play.program; got != ".beta" {
		t.Errorf("↑ wrap then accept wrote %q, want the last key", got)
	}
}

// TestJQCompletionNestedThroughArrays: the acceptance path `.items[].` offers
// the keys of the array's element objects.
func TestJQCompletionNestedThroughArrays(t *testing.T) {
	m := openJQ(t, playApp(t, `{"items":[{"id":7},{"id":8,"name":"x"}]}`))
	m.play.program, m.play.pos = "", 0
	m = typeInto(m, ".items[].")
	if got := playCompLabels(m); strings.Join(got, " ") != "id name" {
		t.Fatalf("`.items[].` offered %v, want the element keys", got)
	}
}

// TestJQCompletionQuotedKeyAccept: a key that is no identifier is inserted
// quoted, and the resulting program evaluates.
func TestJQCompletionQuotedKeyAccept(t *testing.T) {
	m := openJQ(t, playApp(t, `{"a b":42}`))
	m.play.program, m.play.pos = "", 0
	m = typeInto(m, ".")
	m = drainKey(m, tea.KeyPressMsg{Code: tea.KeyEnter})
	if got := m.play.program; got != `."a b"` {
		t.Fatalf("accept wrote %q, want the quoted access", got)
	}
	if got := m.play.result.Text(); got != "42" {
		t.Errorf("result = %q, the quoted access must evaluate", got)
	}
}

// TestJQCompletionBuiltins: an identifier partial completes the jq builtins,
// and the popup renders the selected builtin's doc line.
func TestJQCompletionBuiltins(t *testing.T) {
	m := openJQ(t, playApp(t, `{"a":1}`))
	m.play.program, m.play.pos = "", 0
	m = typeInto(m, "sel")
	labels := playCompLabels(m)
	if len(labels) == 0 || labels[0] != "select" {
		t.Fatalf("`sel` offered %v, want select first", labels)
	}
	v := ansi.Strip(m.render())
	if !strings.Contains(v, "select /1") {
		t.Errorf("the popup should render the builtin with its arity, got:\n%s", v)
	}
	// The doc row truncates to the popup width; the head is enough to prove
	// it renders.
	if !strings.Contains(v, "keep the input when") {
		t.Errorf("the popup should render the selected builtin's doc, got:\n%s", v)
	}
	m = drainKey(m, tea.KeyPressMsg{Code: tea.KeyEnter})
	if got := m.play.program; got != "select" {
		t.Fatalf("accept wrote %q", got)
	}
}

// TestJQCompletionManualRequest: ctrl+space opens the popup without a typed
// trigger — the full builtin list on an empty line.
func TestJQCompletionManualRequest(t *testing.T) {
	m := openJQ(t, playApp(t, `{"a":1}`))
	m.play.program, m.play.pos = "", 0
	if m.play.comp != nil {
		t.Fatal("an empty line must not open the popup on its own")
	}
	m = drainKey(m, tea.KeyPressMsg{Code: tea.KeySpace, Mod: tea.ModCtrl})
	if got := playCompLabels(m); len(got) == 0 {
		t.Fatal("ctrl+space must open the builtin list")
	}
}

// TestJQCompletionRenders: the popup is composited over the pane below the
// query line, with the editor completion popup's accept hint.
func TestJQCompletionRenders(t *testing.T) {
	m := openJQ(t, playApp(t, `{"name":"ike","nation":"x"}`))
	m.play.program, m.play.pos = "", 0
	m = typeInto(m, ".na")
	v := ansi.Strip(m.render())
	for _, want := range []string{"name string", "nation string", "↹/⏎ accept · esc close"} {
		if !strings.Contains(v, want) {
			t.Errorf("the render should hold %q, got:\n%s", want, v)
		}
	}
	// Dismissed, the rows disappear from the frame.
	m = drainKey(m, tea.KeyPressMsg{Code: tea.KeyEscape})
	if v := ansi.Strip(m.render()); strings.Contains(v, "↹/⏎ accept") {
		t.Error("a dismissed popup must not render")
	}
}

// TestJQCompletionClosesOnFocusAndHistory: leaving the query line — tab into
// the result buffer after a dismiss, or walking the history — never leaves a
// stale popup behind.
func TestJQCompletionClosesOnFocusAndHistory(t *testing.T) {
	m := openJQ(t, playApp(t, `{"name":"ike"}`))
	m = runJQProgram(m, ".name")
	m.play.program, m.play.pos = "", 0
	m = typeInto(m, ".na")

	// The cursor stepping off the partial closes the popup (the span it
	// would replace moved away), like the editor's stale-position drop.
	m = drainKey(m, tea.KeyPressMsg{Code: tea.KeyLeft})
	if m.play.comp != nil {
		t.Fatal("a cursor motion must close the popup")
	}

	// And a popup open when the mouse focuses the result buffer goes too.
	m = drainKey(m, tea.KeyPressMsg{Code: tea.KeyRight}) // back to the line end
	m = typeInto(m, "m")
	if m.play.comp == nil {
		t.Fatal("typing on must reopen the popup")
	}
	m.play.setBufFocus(true)
	if m.play.comp != nil {
		t.Error("moving the keyboard into the result buffer must close the popup")
	}
}

// TestJQCompletionAnchorsOnTheCaretsRow (#2038): with the multi-line view up
// the popup hangs under the *caret's* row and under the partial's column in
// it, not under the first row — the list has to point at the text it replaces
// wherever in the pipeline that is.
func TestJQCompletionAnchorsOnTheCaretsRow(t *testing.T) {
	m, _ := playMultiLineApp(t)
	r, ok := m.lay.Panes[m.play.paneKey]
	if !ok {
		t.Fatal("the hosting pane must have a rect")
	}
	paneW := r.W - paneChromeW

	// A partial typed into a later stage of the pipeline, rows down from the
	// query line's first row.
	m = typeInto(m, " | ma")
	if m.play.comp == nil {
		t.Fatal("typing a builtin's first runes must open the popup")
	}
	lines, _, start := m.playQueryWindow(paneW)
	row, _ := jqplay.RowCol(lines, m.play.pos)
	if row == 0 {
		t.Fatalf("setup: the caret must sit below the first row, rows %+v", lines)
	}
	col, got := m.playCompAnchor(paneW)
	if want := row - start; got != want {
		t.Errorf("the popup hangs under row %d, want the caret's row %d", got, want)
	}
	if want := m.play.comp.start - lines[row].Start; col != want {
		t.Errorf("the popup's column = %d, want the partial's column %d in its row", col, want)
	}

	// The one-line view puts it back on the single row.
	m = toggleJQView(m)
	if _, got := m.playCompAnchor(paneW); got != 0 {
		t.Errorf("the one-line view's popup hangs under row %d, want 0", got)
	}
}
