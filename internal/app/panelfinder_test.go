package app

import (
	"os"
	"path/filepath"
	"testing"

	tea "charm.land/bubbletea/v2"

	"ike/internal/palette"
	"ike/internal/pane"
)

// panelfinder_test.go covers the "@" file finder from a tool panel (#3025):
// it floats over the panel, the pick lands as a tab in the panel's pane, and
// an "@" typed into the panel's open input stays text.

// pfAt types "@" the way a terminal delivers it.
func pfAt(m Model) Model {
	out, _ := m.Update(tea.KeyPressMsg{Code: '@', Text: "@"})
	return out.(Model)
}

func TestAtInToolPanelOpensAnchoredFinder(t *testing.T) {
	for _, name := range []string{"agenttrace", "complexity", "problems", "structure", "tests", "usage"} {
		t.Run(name, func(t *testing.T) {
			m := sized(t, 120, 40)
			key := focusToolWindow(t, &m, toolWindowKinds()[name])
			m = pfAt(m)
			if !m.palette.IsOpen() || !m.palette.Anchored() {
				t.Fatalf("@ in %s: open=%v anchored=%v, want the anchored finder", name, m.palette.IsOpen(), m.palette.Anchored())
			}
			x, y, _, _ := m.paneAnchor()
			r := m.lay.Panes[key]
			if x != r.X+1 || y != r.Y+1 {
				t.Fatalf("finder anchored at %d,%d, want the panel's interior %d,%d", x, y, r.X+1, r.Y+1)
			}
			if m.panelFinderHost != key {
				t.Fatalf("panelFinderHost = %q, want %q", m.panelFinderHost, key)
			}
		})
	}
}

func TestPanelFinderPickOpensTabInPanelPane(t *testing.T) {
	for _, name := range []string{"agenttrace", "complexity"} {
		t.Run(name, func(t *testing.T) {
			m := sized(t, 120, 40)
			tw := toolWindowKinds()[name]
			focusToolWindow(t, &m, tw)
			editorKey := m.activeEditorKey()
			path := filepath.Join(t.TempDir(), "pick.txt")
			if err := os.WriteFile(path, []byte("hello"), 0o644); err != nil {
				t.Fatal(err)
			}
			m = pfAt(m)
			out, _ := m.Update(palette.OpenFileMsg{Path: path})
			m = out.(Model)

			host := m.activeWS().Panes.FocusedInstance()
			if host == nil || host.Kind() != pane.KindEditor || host.TabCount() != 2 {
				t.Fatalf("focused pane should be the panel's tab host with two tabs, got %v", host)
			}
			if m.activeWS().Panes.Focused() == editorKey {
				t.Fatal("the pick landed in the editor pane, not the panel's pane")
			}
			if c := host.TabContent(0); c == nil || c.Kind() != tw.kind {
				t.Fatalf("first tab should keep the %s panel", name)
			}
			if ed := host.Editor(); ed == nil || ed.Path() != canonicalPath(path) {
				t.Fatalf("active tab should be the picked file")
			}
			if ed := m.activeWS().Panes.Get(editorKey).Editor(); ed != nil && ed.Path() == canonicalPath(path) {
				t.Fatal("the editor pane must not have loaded the pick")
			}
			if m.panelFinderHost != "" || m.fileTabHost != "" {
				t.Fatal("the panel routing must be consumed by the pick")
			}
		})
	}
}

func TestPanelFinderDismissForgetsPanel(t *testing.T) {
	m := sized(t, 120, 40)
	focusToolWindow(t, &m, toolWindowKinds()["complexity"])
	m = pfAt(m)
	out, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	m = out.(Model)
	if m.palette.IsOpen() || m.panelFinderHost != "" {
		t.Fatalf("esc must close the finder and drop the panel target: open=%v host=%q", m.palette.IsOpen(), m.panelFinderHost)
	}
}

func TestAtInOpenPanelInputIsText(t *testing.T) {
	m := sized(t, 120, 40)
	focusToolWindow(t, &m, toolWindowKinds()["complexity"])
	out, _ := m.Update(tea.KeyPressMsg{Code: '/', Text: "/"})
	m = out.(Model)
	if !m.complexityPanel().Filtering() {
		t.Fatal("precondition: / must focus the filter row")
	}
	m = pfAt(m)
	if m.palette.IsOpen() {
		t.Fatal("@ in the open filter row must not open the finder")
	}
	if got := m.complexityPanel().FilterText(); got != "@" {
		t.Fatalf("filter text = %q, want the typed @", got)
	}
}

func TestAtInPanelWithOwnInputsDoesNotOpenFinder(t *testing.T) {
	for _, name := range []string{"vcs", "issues", "http", "dom", "debug"} {
		t.Run(name, func(t *testing.T) {
			m := sized(t, 120, 40)
			focusToolWindow(t, &m, toolWindowKinds()[name])
			m = pfAt(m)
			if m.palette.IsOpen() {
				t.Fatalf("@ in %s must not open the finder", name)
			}
		})
	}
}

func TestAtInEditorInsertModeIsText(t *testing.T) {
	m := sized(t, 100, 40)
	m.cycleFocus() // focus the editor in normal mode
	out, _ := m.Update(tea.KeyPressMsg{Code: 'i', Text: "i"})
	m = out.(Model)
	m = pfAt(m)
	if m.palette.IsOpen() {
		t.Fatal("@ in insert mode must not open the finder")
	}
	if ed := m.activeEditor(); ed == nil || ed.Text() != "@" {
		t.Fatalf("@ should be inserted into the buffer")
	}
}
