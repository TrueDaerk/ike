package settings

import (
	"testing"

	tea "charm.land/bubbletea/v2"
)

// pagehelp_test.go covers the three shared page/form helpers of #2466.

func keyOf(code rune, mod tea.KeyMod) tea.KeyPressMsg {
	return tea.KeyPressMsg{Code: code, Mod: mod}
}

func TestFieldNavWrapsAndParksCaret(t *testing.T) {
	form := [3]string{"alpha", "", "long value"}
	nav := newFieldNav(3, func(i int) string { return form[i] })

	if !nav.Update(keyOf(tea.KeyTab, 0)) {
		t.Fatal("tab should be consumed")
	}
	if nav.field != 1 || nav.cur != 0 {
		t.Fatalf("after tab: field=%d cur=%d, want 1/0", nav.field, nav.cur)
	}
	nav.Update(keyOf(tea.KeyDown, 0))
	if nav.field != 2 || nav.cur != len("long value") {
		t.Fatalf("after down: field=%d cur=%d, want 2/10", nav.field, nav.cur)
	}
	// Forward wraps past the last field.
	nav.Update(keyOf(tea.KeyTab, 0))
	if nav.field != 0 || nav.cur != len("alpha") {
		t.Fatalf("after wrap: field=%d cur=%d, want 0/5", nav.field, nav.cur)
	}
	// Backward wraps the other way; shift+tab and up agree.
	nav.Update(keyOf(tea.KeyTab, tea.ModShift))
	if nav.field != 2 {
		t.Fatalf("after shift+tab: field=%d, want 2", nav.field)
	}
	nav.Update(keyOf(tea.KeyUp, 0))
	if nav.field != 1 {
		t.Fatalf("after up: field=%d, want 1", nav.field)
	}
}

func TestFieldNavCaretIsRuneCounted(t *testing.T) {
	form := [2]string{"", "äöü"}
	nav := newFieldNav(2, func(i int) string { return form[i] })
	nav.Update(keyOf(tea.KeyTab, 0))
	if nav.cur != 3 {
		t.Fatalf("cur=%d, want 3 runes (not 6 bytes)", nav.cur)
	}
}

func TestFieldNavIgnoresOtherKeys(t *testing.T) {
	nav := newFieldNav(2, func(int) string { return "" })
	if nav.Update(keyOf('x', 0)) {
		t.Fatal("a rune key must be left to the form's text handling")
	}
	if nav.Update(keyOf(tea.KeyEnter, 0)) {
		t.Fatal("enter stays with the form")
	}
	// A nav over no fields must not divide by zero.
	empty := fieldNav{}
	if empty.Update(keyOf(tea.KeyTab, 0)) {
		t.Fatal("an empty nav consumes nothing")
	}
}

func TestFieldNavFocusClampsAndParks(t *testing.T) {
	form := [2]string{"one", "two"}
	nav := newFieldNav(2, func(i int) string { return form[i] })
	nav.Focus(1)
	if nav.field != 1 || nav.cur != 3 {
		t.Fatalf("Focus(1): field=%d cur=%d, want 1/3", nav.field, nav.cur)
	}
	nav.Focus(-1)
	nav.Focus(5)
	if nav.field != 1 {
		t.Fatalf("out-of-range Focus moved the field to %d", nav.field)
	}
}

func TestPageClickSelectsThenOpens(t *testing.T) {
	sel, opened := 0, -1
	open := func(i int) { opened = i }

	// Head line: no row.
	if pageClick(0, 0, 5, 4, &sel, open); sel != 0 || opened != -1 {
		t.Fatalf("head line acted: sel=%d opened=%d", sel, opened)
	}
	// A press on another row selects it, it does not open it.
	pageClick(3, 0, 5, 4, &sel, open)
	if sel != 2 || opened != -1 {
		t.Fatalf("select: sel=%d opened=%d, want 2/-1", sel, opened)
	}
	// A press on the selected row opens it.
	pageClick(3, 0, 5, 4, &sel, open)
	if opened != 2 {
		t.Fatalf("open: opened=%d, want 2", opened)
	}
}

func TestPageClickHonoursScrollAndBounds(t *testing.T) {
	sel, opened := 0, -1
	open := func(i int) { opened = i }

	// The offset shifts the mapping: row 0 of the window is entry off.
	pageClick(1, 7, 5, 20, &sel, open)
	if sel != 7 {
		t.Fatalf("scrolled click: sel=%d, want 7", sel)
	}
	// Below the list window (the footer) does nothing.
	sel = 0
	pageClick(6, 0, 5, 20, &sel, open)
	if sel != 0 || opened != -1 {
		t.Fatalf("footer click acted: sel=%d opened=%d", sel, opened)
	}
	// The blank rows a short list leaves inside the window do nothing.
	pageClick(4, 0, 5, 2, &sel, open)
	if sel != 0 || opened != -1 {
		t.Fatalf("blank row acted: sel=%d opened=%d", sel, opened)
	}
}

// An unrendered page has no rows: listH 0 must not be read as unbounded, which
// is what the hand-rolled guards did before #2466.
func TestPageClickUnrenderedPageHasNoRows(t *testing.T) {
	sel, opened := 0, -1
	pageClick(1, 0, 0, 4, &sel, func(i int) { opened = i })
	if sel != 0 || opened != -1 {
		t.Fatalf("click on an unrendered page acted: sel=%d opened=%d", sel, opened)
	}
}

func TestPageActionKey(t *testing.T) {
	host := &stubHost{}
	opened := -99
	acts := func(sel, n int) pageActions {
		return pageActions{
			host: host, sel: sel, n: n,
			open:    func(i int) { opened = i },
			confirm: func(int) string { return "delete row" },
			remove:  func(int) tea.Cmd { return nil },
		}
	}

	if !pageActionKey("a", acts(0, 3)) || opened != -1 {
		t.Fatalf("a should add: opened=%d, want -1", opened)
	}
	if !pageActionKey("enter", acts(2, 3)) || opened != 2 {
		t.Fatalf("enter should edit row 2: opened=%d", opened)
	}
	// Out of range: enter does nothing.
	opened = -99
	pageActionKey("enter", acts(3, 3))
	if opened != -99 {
		t.Fatalf("enter on an out-of-range selection opened %d", opened)
	}
	// Unknown keys are left to the page.
	if pageActionKey("x", acts(0, 3)) {
		t.Fatal("x must not be consumed")
	}
}

func TestPageActionKeyDeleteConfirms(t *testing.T) {
	host := &stubHost{}
	removed := -99
	acts := pageActions{
		host: host, sel: 1, n: 3,
		open:    func(int) {},
		confirm: func(i int) string { return "delete the mapping" },
		remove:  func(i int) tea.Cmd { removed = i; return nil },
	}
	if !pageActionKey("d", acts) {
		t.Fatal("d should be consumed")
	}
	if removed != -99 {
		t.Fatal("delete must wait for the confirmation")
	}
	cp, ok := host.top().(*confirmPanel)
	if !ok {
		t.Fatalf("d pushed %T, want a confirm panel", host.top())
	}
	// Confirming runs the removal for the row that was selected at press time.
	cp.Update(tea.KeyPressMsg{Code: 'y'})
	if removed != 1 {
		t.Fatalf("confirmed delete removed %d, want 1", removed)
	}

	// Without a host there is nowhere to confirm, so d does nothing.
	acts.host, removed = nil, -99
	pageActionKey("d", acts)
	if removed != -99 {
		t.Fatal("hostless delete ran unconfirmed")
	}
}
