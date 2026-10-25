package app

// complexity_hidden_test.go covers the app half of the hidden set (#3031):
// 'x' hides the selected function into the baseline file and every surface
// drops it, 'H' lists it so 'x' un-hides it, the baseline setting does not
// gate it, 'X' writes a complexity.exclude glob the index rewalks without,
// and the help overlay leads with the window's keys while it is focused.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"ike/internal/complexity"
	"ike/internal/complexitypanel"
	"ike/internal/config"
)

// paneLists reports whether the pane shows a row of function name.
func paneLists(p *complexitypanel.Model, name string) bool {
	for _, r := range p.Rows() {
		if r.Fn.Name == name {
			return true
		}
	}
	return false
}

// firstMsgOf runs cmd and returns the first message of type T in it,
// flattening batches.
func firstMsgOf[T any](t *testing.T, cmd tea.Cmd) T {
	t.Helper()
	var find func(msg tea.Msg) (T, bool)
	find = func(msg tea.Msg) (T, bool) {
		if v, ok := msg.(T); ok {
			return v, true
		}
		if batch, ok := msg.(tea.BatchMsg); ok {
			for _, c := range batch {
				if c == nil {
					continue
				}
				if v, ok := find(c()); ok {
					return v, true
				}
			}
		}
		var zero T
		return zero, false
	}
	if cmd == nil {
		t.Fatal("nil command")
	}
	v, ok := find(cmd())
	if !ok {
		t.Fatalf("no %T in the command's messages", v)
	}
	return v
}

// hideSelected runs complexity.hide on the pane's selected row and applies
// the resulting message.
func hideSelected(t *testing.T, m Model) Model {
	t.Helper()
	out, cmd := m.Update(ComplexityHideMsg{})
	m = out.(Model)
	if cmd == nil {
		t.Fatalf("complexity.hide must yield a message: %q", cxNotices(m))
	}
	out, next := m.Update(cmd())
	m = out.(Model)
	if next != nil {
		out, _ = m.Update(next())
		m = out.(Model)
	}
	return m
}

func TestComplexityHideFunctionReachesEverySurfaceAndUnhides(t *testing.T) {
	m, proj := baselineApp(t)
	path := complexity.BaselinePath(proj)
	m = baselineGen(t, m)
	if rows := complexityRows(m.probStore); len(rows) != 1 {
		t.Fatalf("A is listed before the hide: %v", rows)
	}
	out, _ := m.Update(ComplexityToggleMsg{})
	m = out.(Model)
	p := m.complexityPanel()
	if r, ok := p.Selected(); !ok || r.Fn.Name != "A" {
		t.Fatalf("selected row = %+v", r)
	}

	// 'x' hides A: the file gains a hidden entry, Problems and the pane
	// drop the row, the header counts it.
	m = hideSelected(t, m)
	b, err := complexity.LoadBaseline(path)
	if err != nil || b == nil || !b.HiddenOf().Has(complexity.EntryKey{File: "src/a.go", Function: "A", Ordinal: 0}) {
		t.Fatalf("saved file = %+v, %v", b, err)
	}
	if b.Len() != 0 {
		t.Fatalf("hiding is not accepting: %+v", b.Accepted)
	}
	if rows := complexityRows(m.probStore); len(rows) != 0 {
		t.Fatalf("a hidden function must leave Problems: %v", rows)
	}
	if paneLists(p, "A") || p.HiddenCount() != 1 {
		t.Fatalf("pane rows = %d hidden = %d:\n%s", len(p.Rows()), p.HiddenCount(), p.View())
	}
	if !strings.Contains(cxNotices(m), "hid A") {
		t.Fatalf("the hide must be reported: %q", cxNotices(m))
	}
	// The bulk accept has nothing to record: a hidden function is nobody's
	// debt.
	if n := len(m.overThresholdFunctions()); n != 0 {
		t.Fatalf("overThresholdFunctions = %d, want 0", n)
	}

	// 'H' shows the hidden row; 'x' on it un-hides it.
	out, _ = m.Update(ComplexityShowHiddenMsg{})
	m = out.(Model)
	if rows := p.Rows(); len(rows) != 1 || !rows[0].Hidden || rows[0].Fn.Name != "A" {
		t.Fatalf("hidden view rows = %+v", rows)
	}
	if !strings.Contains(cxNotices(m), "showing 1 hidden function") {
		t.Fatalf("the view toggle must be reported: %q", cxNotices(m))
	}
	m = hideSelected(t, m)
	if b, _ := complexity.LoadBaseline(path); b.HiddenOf().Len() != 0 {
		t.Fatalf("the second x must un-hide: %+v", b.HiddenOf().Entries())
	}
	if !strings.Contains(cxNotices(m), "un-hid A") {
		t.Fatalf("the un-hide must be reported: %q", cxNotices(m))
	}
	out, _ = m.Update(ComplexityShowHiddenMsg{})
	m = out.(Model)
	if !paneLists(p, "A") || p.HiddenCount() != 0 {
		t.Fatalf("A is back: %+v", p.Rows())
	}
	if rows := complexityRows(m.probStore); len(rows) != 1 {
		t.Fatalf("Problems lists A again: %v", rows)
	}

	// The baseline setting does not gate the hidden set: off, 'x' still
	// writes, and the file's hidden half still applies.
	m = baselineReload(t, m, func(c *config.Config) { c.Complexity.Baseline = false })
	m = hideSelected(t, m)
	if b, _ := complexity.LoadBaseline(path); b == nil || b.HiddenOf().Len() != 1 {
		t.Fatalf("baseline off must still hide: %+v", b)
	}
	if paneLists(p, "A") || len(complexityRows(m.probStore)) != 0 {
		t.Fatalf("baseline off must still apply the hidden set: rows=%d problems=%v", len(p.Rows()), complexityRows(m.probStore))
	}
	// A file edited on disk re-applies through the watcher route.
	b, _ = complexity.LoadBaseline(path)
	b.Unhide(complexity.EntryKey{File: "src/a.go", Function: "A", Ordinal: 0})
	if err := b.Save(path); err != nil {
		t.Fatal(err)
	}
	m.reloadComplexityBaseline()
	if !paneLists(p, "A") {
		t.Fatalf("the re-read file must restore the row: %+v", p.Rows())
	}
}

func TestComplexityHideFileWritesExcludeAndRewalks(t *testing.T) {
	m, proj := baselineApp(t)
	m = baselineGen(t, m)
	out, _ := m.Update(ComplexityToggleMsg{})
	m = out.(Model)
	a := filepath.Join(proj, "src", "a.go")

	// 'X' on the function row hides its file: the glob lands in the
	// project settings and the index rewalks without the file.
	out, cmd := m.Update(ComplexityHideFileMsg{})
	m = out.(Model)
	if cmd == nil {
		t.Fatalf("complexity.hideFile must yield a message: %q", cxNotices(m))
	}
	msg, ok := cmd().(complexitypanel.HideFileMsg)
	if !ok || len(msg.Globs) != 1 || msg.Globs[0] != "src/a.go" {
		t.Fatalf("hide file message = %+v", msg)
	}
	out, cmd = m.Update(msg)
	m = out.(Model)
	if cmd == nil {
		t.Fatal("the hide must write the setting")
	}
	if !strings.Contains(cxNotices(m), "hiding src/a.go (complexity.exclude, project settings") {
		t.Fatalf("the hide must be reported: %q", cxNotices(m))
	}
	reloaded := firstMsgOf[config.ConfigReloadedMsg](t, cmd)
	if len(reloaded.Diags) != 0 {
		t.Fatalf("diagnostics: %+v", reloaded.Diags)
	}
	data, err := os.ReadFile(filepath.Join(proj, ".ike", "settings.toml"))
	if err != nil || !strings.Contains(string(data), `"src/a.go"`) {
		t.Fatalf("project settings = %q, %v", data, err)
	}
	if got := reloaded.Config.Complexity.Exclude; len(got) != 1 || got[0] != "src/a.go" {
		t.Fatalf("reloaded exclude = %v", got)
	}
	out, _ = m.Update(reloaded)
	m = out.(Model)
	ix := m.ComplexityIndex()
	waitComplexity(t, ix, "the rewalk", func(v complexity.View) bool {
		_, has := v.Files[a]
		return !has && !ix.Stats().Scanning
	})
	// Hiding it again writes nothing.
	out, _ = m.Update(msg)
	m = out.(Model)
	if !strings.Contains(cxNotices(m), "already in complexity.exclude") {
		t.Fatalf("a repeated hide must say so: %q", cxNotices(m))
	}
}

func TestComplexityHelpGroupsLeadWhileFocused(t *testing.T) {
	m, _ := baselineApp(t)
	if g := m.complexityHelpGroups(); g != nil {
		t.Fatalf("an unfocused window contributes nothing: %+v", g)
	}
	out, _ := m.Update(ComplexityToggleMsg{})
	m = out.(Model)
	if !m.complexityPaneFocused() {
		t.Fatal("the toggle focuses the window")
	}
	groups := m.complexityHelpGroups()
	labels := map[string]bool{}
	shortcuts := map[string]string{}
	for _, g := range groups {
		if !g.Focused {
			t.Fatalf("group %q must be flagged Focused", g.Label)
		}
		labels[g.Label] = true
		for _, e := range g.Entries {
			shortcuts[e.Shortcut] = e.Title
		}
	}
	for _, want := range []string{"Complexity — list", "Complexity — filter row", "Complexity — filter vocabulary", "Complexity — Changes tab", "Complexity — agent"} {
		if !labels[want] {
			t.Errorf("missing group %q in %v", want, labels)
		}
	}
	// Fixed keys verbatim, bound commands by their live chord, the filter
	// vocabulary from the pane's schema.
	// A shifted binding renders as the keymap spells it (shift+x), a
	// fixed pane key as the pane spells it (S).
	for _, want := range []string{"s", "S", "e", "E", "y", "p", "d", "a", "r", "R", "enter · double click", "x", "shift+x", "shift+h", "b", "i", "space",
		"baseline:<new|accepted|worsened>", "hidden:<yes|no>", "test:<yes|no>", "metric:<ccn|cog|cmt|hot|commits>"} {
		if _, ok := shortcuts[want]; !ok {
			t.Errorf("missing key %q", want)
		}
	}
	if !strings.Contains(shortcuts["x"], "hide") || !strings.Contains(shortcuts["hidden:<yes|no>"], "un-hidden") {
		t.Errorf("titles: x=%q hidden=%q", shortcuts["x"], shortcuts["hidden:<yes|no>"])
	}
	// The overlay itself leads with the window's section.
	m.openHelp()
	v := plainView(m)
	if i, j := strings.Index(v, "Complexity — list"), strings.Index(v, "Global"); i < 0 || j >= 0 && j < i {
		t.Fatalf("the help overlay must lead with the Complexity section (list at %d, global at %d):\n%s", i, j, v)
	}
}
