package settings

// pagehelp.go holds the three shapes the add·edit·delete settings pages and
// their forms used to hand-roll once per page (#2466, roadmap 0500):
//
//   - fieldNav — the tab/shift-tab/↑/↓ field cursor every sub-panel form
//     repeated verbatim, together with the caret it parks at the end of the
//     newly focused field,
//   - pageClick — the "a press on a row selects it, a press on the selected
//     row opens it" hit-test, now over the shared ui.RowAt (#2259) instead of
//     four private copies of the same arithmetic,
//   - pageActionKey — the a/enter/d action switch, with the delete-confirm
//     sentence left to the page as a callback.
//
// esc and enter stay with the individual forms: what a form saves, and what
// it validates before saving, is not shared behaviour.

import (
	tea "charm.land/bubbletea/v2"

	"ike/internal/theme"
	"ike/internal/ui"
)

// --- form field navigation ---

// fieldNav is the focused-field cursor of a sub-panel form. Forms embed it, so
// its field and cur members read exactly like the two members every form used
// to declare itself, and the renderers keep working unchanged.
//
// text reports the current text of field i; the nav uses it to park the caret
// at the end of a field it moves onto — a caret left mid-way through a longer
// neighbour would point into nothing.
type fieldNav struct {
	field int // index of the focused field
	cur   int // caret position inside that field, in runes
	n     int // number of fields
	text  func(i int) string
}

// newFieldNav builds the nav for an n-field form reading its text through get.
func newFieldNav(n int, get func(i int) string) fieldNav {
	return fieldNav{n: n, text: get}
}

// Update handles the field-motion keys — shift+tab and ↑ step back, tab and ↓
// step forward, both wrapping — and reports whether it consumed the key. A
// form calls it before its own text handling: everything it does not consume
// is field input.
func (f *fieldNav) Update(key tea.KeyPressMsg) bool {
	if f.n < 1 {
		return false
	}
	switch {
	case key.Code == tea.KeyTab && key.Mod&tea.ModShift != 0, key.Code == tea.KeyUp:
		f.Focus((f.field + f.n - 1) % f.n)
	case key.Code == tea.KeyTab, key.Code == tea.KeyDown:
		f.Focus((f.field + 1) % f.n)
	default:
		return false
	}
	return true
}

// Focus moves onto field i (ignoring an out-of-range index) and parks the
// caret at the end of its text. It is what a click on a field row does.
func (f *fieldNav) Focus(i int) {
	if i < 0 || i >= f.n {
		return
	}
	f.field, f.cur = i, 0
	if f.text != nil {
		f.cur = len([]rune(f.text(i)))
	}
}

// --- page row hit-test ---

// pageHeadRows is how many lines the add·edit·delete pages draw above their
// list — one head line, in every one of them.
const pageHeadRows = 1

// pageClick maps a content-local click row onto a list row for the
// add·edit·delete pages: a press on a row selects it, a press on the already
// selected row opens it (enter semantics). Presses on the head line, on the
// footer, and on the blank rows a short list leaves at the bottom do nothing.
//
// listH is the list-window height of the last render. Zero means nothing has
// been drawn yet — ui.RowAt then reports no row at all, where the hand-rolled
// guards this replaces read an unrendered page as unbounded and selected a row
// from a window that was never on screen.
func pageClick(y, off, listH, n int, sel *int, open func(int)) tea.Cmd {
	idx, ok := ui.RowAt(y, off, pageHeadRows, listH, n)
	if !ok {
		return nil
	}
	if idx == *sel {
		if open != nil {
			open(idx)
		}
		return nil
	}
	*sel = idx
	return nil
}

// --- page action switch ---

// pageActions describes the a/enter/d behaviour of one add·edit·delete page.
// open is called with -1 to add and with a row index to edit; confirm supplies
// the sentence the delete confirmation asks ("delete the mapping x → y"), and
// remove performs it once confirmed.
type pageActions struct {
	host    SubPanelHost
	pal     *theme.Palette
	sel     int
	n       int
	open    func(idx int)
	confirm func(idx int) string
	remove  func(idx int) tea.Cmd
}

// pageActionKey runs the add/edit/delete keys of an add·edit·delete page and
// reports whether it consumed the key. Delete always goes through the shared
// confirm sub-panel (#891), so with no host — nowhere to push the
// confirmation — d does nothing.
func pageActionKey(key string, a pageActions) bool {
	inRange := a.sel >= 0 && a.sel < a.n
	switch key {
	case "a":
		if a.open != nil {
			a.open(-1)
		}
	case "enter":
		if inRange && a.open != nil {
			a.open(a.sel)
		}
	case "d":
		if inRange && a.host != nil && a.confirm != nil && a.remove != nil {
			idx := a.sel
			a.host.Push(newConfirm(a.host, a.confirm(idx), "Delete", a.pal, func() tea.Cmd {
				return a.remove(idx)
			}))
		}
	default:
		return false
	}
	return true
}
