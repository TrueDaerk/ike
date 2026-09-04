package settings

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"ike/internal/theme"
)

// actionPages builds every custom page the app registers, so the audit
// covers the real surface rather than a fixture.
func actionPages(t *testing.T) []PageModel {
	t.Helper()
	opts := testOpts(t)
	return []PageModel{
		NewColorsPage(opts),
		NewConcealPage(opts),
		NewAssocPage(opts),
		NewKeymapPage(opts, func(string) bool { return true }, func() []CommandEntry { return nil }),
		NewToolsPage(opts),
		NewDebugMapPage(opts),
		NewToolchainPage(opts, t.TempDir(), nil),
		NewPluginsPage(opts, func() []PluginInfo { return nil }, func(string, bool) tea.Cmd { return nil }),
		NewMarketplacePage(nil, nil),
		NewLSPPage(opts, func() []string { return nil }, nil, nil, nil),
		NewESPage(opts),
		NewFormatPage(opts),
	}
}

// TestActionsFollowTheCanonicalTable: every page's verbs use the canonical
// letters with their canonical meaning, so "r" is reset on every page and
// never restart, "s" is scope and never suggestions.
func TestActionsFollowTheCanonicalTable(t *testing.T) {
	restoreConfig(t)
	for _, page := range actionPages(t) {
		l, ok := page.(ActionLister)
		if !ok {
			t.Errorf("%T does not implement ActionLister", page)
			continue
		}
		seen := map[string]bool{}
		for _, a := range l.Actions() {
			if a.Verb == "" {
				t.Errorf("%T: action %q has no verb", page, a.Key)
			}
			if seen[a.Key] {
				t.Errorf("%T: key %q listed twice", page, a.Key)
			}
			seen[a.Key] = true
			verbs, known := canonicalVerbs[a.Key]
			if !known {
				t.Errorf("%T: key %q is not in the canonical table (actions.go)", page, a.Key)
				continue
			}
			if verbs == nil {
				continue
			}
			matched := false
			for _, v := range verbs {
				if strings.HasPrefix(strings.ToLower(a.Verb), strings.ToLower(v)) {
					matched = true
				}
			}
			if !matched {
				t.Errorf("%T: key %q means %v, page says %q", page, a.Key, verbs, a.Verb)
			}
		}
	}
}

// TestActionBarOverflowMenu: a page with more verbs than the bar holds gets
// "[…] More", and the menu runs the chosen verb by forwarding its key.
func TestActionBarOverflowMenu(t *testing.T) {
	restoreConfig(t)
	stub := &actionStub{}
	m := New([]Page{{Title: "Stub", Custom: stub}}, testOpts(t))
	m.SetSize(120, 20)
	m.Open()
	m.focus = formColumn
	v := stripANSI(m.View())
	if !strings.Contains(v, "[…] More") {
		t.Fatalf("eight verbs must overflow into More:\n%s", v)
	}
	if strings.Contains(v, "[h] Eighth") {
		t.Fatalf("the overflowed verb must not be on the bar:\n%s", v)
	}
	var more hintAction
	for _, h := range m.hintHits {
		if h.action == "more" {
			more = h
		}
	}
	m.Click(more.start, m.hintRowY())
	if !m.SubOpen() {
		t.Fatal("More must open the action menu")
	}
	m.Update(key("end"))
	m.Update(key("enter"))
	if m.SubOpen() || len(stub.got) != 1 || stub.got[0] != "h" {
		t.Fatalf("enter on the last row must forward h and close, got %v open=%v", stub.got, m.SubOpen())
	}
}

// TestActionBarDimsDisabledVerbs: a disabled verb renders on the bar but is
// not a click target.
func TestActionBarDimsDisabledVerbs(t *testing.T) {
	restoreConfig(t)
	stub := &actionStub{disabled: true}
	m := New([]Page{{Title: "Stub", Custom: stub}}, testOpts(t))
	m.SetSize(120, 20)
	m.Open()
	m.focus = formColumn
	m.View()
	for _, h := range m.hintHits {
		if h.action == "key:d" {
			t.Fatal("a disabled verb must not be clickable")
		}
	}
}

// actionStub is a page with eight verbs, one optionally disabled.
type actionStub struct {
	got      []string
	disabled bool
}

func (s *actionStub) Update(k tea.KeyPressMsg) tea.Cmd { s.got = append(s.got, k.String()); return nil }
func (s *actionStub) View(w, h int) string             { return "stub" }
func (s *actionStub) SetPalette(*theme.Palette)        {}
func (s *actionStub) Capturing() bool                  { return false }
func (s *actionStub) Actions() []Action {
	return []Action{
		{Key: "a", Verb: "Add"}, {Key: "d", Verb: "Delete", Enabled: func() bool { return !s.disabled }},
		{Key: "e", Verb: "Edit"}, {Key: "r", Verb: "Reset"}, {Key: "g", Verb: "Refresh"},
		{Key: "i", Verb: "Install"}, {Key: "n", Verb: "New"}, {Key: "h", Verb: "Eighth"},
	}
}
