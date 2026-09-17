package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

// playgroundtabbar_test.go covers the title row of a pane running the inline
// playground (#2606): the tab bar keeps the row whenever the pane would show
// one, so the pane's other documents stay visible and clickable, and the
// dialect and source move into the playground's own info row.

// playTabApp opens notes.txt and data.json into the focused pane — data.json
// active — and runs the jq playground over the JSON tab. It returns the model
// and both paths.
func playTabApp(t *testing.T) (Model, string, string) {
	t.Helper()
	noDebounce(t)
	dir := t.TempDir()
	notes := writeTemp(t, dir, "notes.txt", "notes\n")
	data := filepath.Join(dir, "data.json")
	if err := os.WriteFile(data, []byte(`{"name":"ike"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	m := dismissOnboarding(openApp(t, notes, data))
	return openJQ(t, m), notes, data
}

// TestPlaygroundKeepsTabBar is the issue's acceptance case: with two tabs open
// the title row renders the tab bar, not the playground title.
func TestPlaygroundKeepsTabBar(t *testing.T) {
	m, _, _ := playTabApp(t)
	inst := m.activeWS().Panes.FocusedInstance()
	if !m.playInlineActive(m.activeWS().Panes.Focused()) {
		t.Fatal("the playground must be inline in the focused pane")
	}
	if bar, ok := m.tabBar(inst, 80); !ok || !strings.Contains(ansi.Strip(bar), "notes.txt") {
		t.Fatalf("a two-tab pane must render the bar while the playground runs, ok=%v bar=%q", ok, ansi.Strip(bar))
	}
	v := stripped(m)
	if !strings.Contains(v, "notes.txt ✕ │ data.json ✕") {
		t.Fatalf("the title row must show the tab bar, frame:\n%s", v)
	}
	// The mode and its source are not lost with the title: the info row names
	// them instead.
	if !strings.Contains(v, "JQ — data.json") {
		t.Fatalf("the playground chrome must still name dialect and source, frame:\n%s", v)
	}
}

// TestPlaygroundSingleTabKeepsTitle: with one tab and always_show off there is
// no bar to make room for, so the playground title renders as before.
func TestPlaygroundSingleTabKeepsTitle(t *testing.T) {
	noDebounce(t)
	m := openJQ(t, playApp(t, `{"name":"ike"}`))
	inst := m.activeWS().Panes.FocusedInstance()
	if m.paneTabBarShown(inst) {
		t.Fatal("a single tab without always_show shows no bar")
	}
	if seg := m.playModeSegment(); seg != "" {
		t.Fatalf("the info row must not repeat the title, got %q", ansi.Strip(seg))
	}
	if v := stripped(m); !strings.Contains(v, "JQ — data.json") {
		t.Fatalf("the single-tab pane keeps the playground title, frame:\n%s", v)
	}
}

// TestPlaygroundTabBarClickSwitchesTab guards the mouse path: the bar is
// hit-testable while the playground runs, and a click reaches the same tab
// switch the keyboard already reached.
func TestPlaygroundTabBarClickSwitchesTab(t *testing.T) {
	m, notes, _ := playTabApp(t)
	key := m.activeWS().Panes.Focused()
	inst := m.activeWS().Panes.FocusedInstance()
	x, y := barCell(t, m, 1) // inside the first segment, " notes.txt ✕ "
	gotKey, idx, _, ok := m.tabBarHit(x, y)
	if !ok || gotKey != key || idx != 0 {
		t.Fatalf("tabBarHit must resolve the first segment, got key=%q idx=%d ok=%v", gotKey, idx, ok)
	}
	m = step(m, tea.MouseClickMsg{X: x, Y: y, Button: tea.MouseLeft})
	if inst.Editor().Path() != notes {
		t.Fatalf("the click must switch to the clicked tab, got %q", inst.Editor().Path())
	}
}
