package app

import (
	"os"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"ike/internal/config"
	"ike/internal/host"
	"ike/internal/layout"
	"ike/internal/pane"
	"ike/internal/registry"
	"ike/internal/scratch"
	"ike/internal/scratchpanel"
)

// scratch_panel_test.go covers the Scratch Files tool window wiring (#1932):
// the toggle state machine and its bottom placement, the configured height,
// the open funnel, the delete flow (store removal plus tab closing), live
// refresh on creation and layout persistence.

// scratchModel returns a sized model with the panel open and focused. Each
// model runs on its own IKE_CONFIG_DIR, so the scratch store is empty and
// private to the test.
func scratchModel(t *testing.T) Model {
	t.Helper()
	m := newSized()
	tm, _ := m.Update(ScratchPanelToggleMsg{})
	return tm.(Model)
}

// newScratch creates one scratch through the command the palette runs and
// returns the model plus the created path.
func newScratchFile(t *testing.T, m Model, ext string) (Model, string) {
	t.Helper()
	before, _ := scratch.List()
	tm, cmd := m.Update(NewScratchMsg{Ext: ext})
	m = drainCmd(tm.(Model), cmd)
	after, err := scratch.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(after) != len(before)+1 {
		t.Fatalf("scratch.new must create one file, store went %v → %v", before, after)
	}
	return m, after[0]
}

func TestScratchPanelToggleStateMachine(t *testing.T) {
	m := newSized()
	fromKey := m.activeWS().Panes.Focused()

	tm, _ := m.Update(ScratchPanelToggleMsg{})
	m = tm.(Model)
	if !m.activeWS().Panes.Has(pane.ScratchKey) {
		t.Fatal("toggle must open the scratch panel")
	}
	if m.activeWS().Panes.Focused() != pane.ScratchKey {
		t.Fatal("the fresh panel must hold focus")
	}

	// Focused → toggle returns focus without closing the panel.
	tm, _ = m.Update(ScratchPanelToggleMsg{})
	m = tm.(Model)
	if m.activeWS().Panes.Focused() != fromKey {
		t.Fatalf("focus = %q, want the pane it came from %q", m.activeWS().Panes.Focused(), fromKey)
	}
	if !m.activeWS().Panes.Has(pane.ScratchKey) {
		t.Fatal("returning focus must not close the panel")
	}

	// Unfocused → toggle focuses it again.
	tm, _ = m.Update(ScratchPanelToggleMsg{})
	m = tm.(Model)
	if m.activeWS().Panes.Focused() != pane.ScratchKey {
		t.Fatal("an unfocused panel must take focus")
	}
}

// The panel docks below the editor, at the configured height — the divider
// above it is an ordinary pane edge, so the mouse resize and the persisted
// ratio come from the split tree itself.
func TestScratchPanelOpensBelowAtConfiguredHeight(t *testing.T) {
	m := scratchModel(t)
	editorRect := m.lay.Panes[m.activeEditorKey()]
	rect, ok := m.lay.Panes[pane.ScratchKey]
	if !ok {
		t.Fatal("the panel must have geometry")
	}
	if rect.Y <= editorRect.Y {
		t.Fatalf("the panel must sit below the editor: panel %v, editor %v", rect, editorRect)
	}
	if want := config.Get().Scratch.PanelHeight; rect.H != want {
		t.Fatalf("panel height = %d, want the configured %d", rect.H, want)
	}
	split := scratchSplit(m.activeWS().Tree)
	if split == nil {
		t.Fatal("the panel must hang off a vertical split — the draggable divider")
	}
	// The divider that split owns is what a mouse drag grabs.
	found := false
	for _, d := range m.lay.Dividers {
		if d.Split == split && d.Orient == layout.Vertical {
			found = true
		}
	}
	if !found {
		t.Fatal("the panel's split must expose a horizontal divider band")
	}
}

// configured builds a sized model on a private store with the [scratch]
// section patched — New() reloads the config from disk, so the override has
// to sit between construction and the first sized Update pass.
func configured(t *testing.T, patch func(*config.Config)) Model {
	t.Helper()
	t.Setenv("IKE_CONFIG_DIR", t.TempDir())
	old := config.Get()
	t.Cleanup(func() { config.Set(old) })
	c := *old
	patch(&c)
	config.Set(&c)
	m := NewWith(registry.New(), host.MapConfig{})
	tm, _ := m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	return tm.(Model)
}

// A height configured in the settings UI decides how tall the panel opens.
func TestScratchPanelHonorsHeightSetting(t *testing.T) {
	m := configured(t, func(c *config.Config) { c.Scratch.PanelHeight = 12 })
	tm, _ := m.Update(ScratchPanelToggleMsg{})
	m = tm.(Model)
	if got := m.lay.Panes[pane.ScratchKey].H; got != 12 {
		t.Fatalf("panel height = %d, want 12", got)
	}
}

// [scratch] panel = true opens the panel on start without stealing focus.
func TestScratchPanelAutoOpensFromSetting(t *testing.T) {
	m := configured(t, func(c *config.Config) { c.Scratch.Panel = true })
	if !m.activeWS().Panes.Has(pane.ScratchKey) {
		t.Fatal("the setting must open the panel on start")
	}
	if m.activeWS().Panes.Focused() == pane.ScratchKey {
		t.Fatal("the auto-opened panel must not steal focus")
	}
	// Hiding it afterwards sticks: the one-shot auto-open never re-fires.
	m.closePane(pane.ScratchKey)
	tm, _ := m.Update(tea.WindowSizeMsg{Width: 100, Height: 32})
	m = tm.(Model)
	if m.activeWS().Panes.Has(pane.ScratchKey) {
		t.Fatal("a closed panel must stay closed for the session")
	}
}

// A scratch created while the panel is open shows up without a restart.
func TestScratchPanelListsNewScratchesLive(t *testing.T) {
	m := scratchModel(t)
	if got := m.scratchPanel().Entries(); len(got) != 0 {
		t.Fatalf("a fresh store must list nothing, got %v", got)
	}
	m, path := newScratchFile(t, m, "txt")
	entries := m.scratchPanel().Entries()
	if len(entries) != 1 || entries[0].Path != path {
		t.Fatalf("entries = %v, want the new %q", entries, path)
	}
	if !strings.Contains(m.View().Content, "SCRATCH FILES") {
		t.Fatal("the pane chrome must name the panel")
	}
}

// Enter on a row opens the scratch as a focused editor tab, through the same
// funnel scratch.list uses.
func TestScratchPanelOpensSelectedScratch(t *testing.T) {
	m := scratchModel(t)
	m, path := newScratchFile(t, m, "txt")

	tm, cmd := m.Update(scratchpanel.OpenMsg{Path: path})
	m = drainCmd(tm.(Model), cmd)
	ed := m.activeEditor()
	if ed == nil || !ed.HasFile() || ed.Path() != path {
		t.Fatalf("the scratch must open as the focused editor, got %v", ed)
	}
}

// The confirmed delete removes the file, closes its open tabs and drops the
// row — driven through the panel's own keys.
func TestScratchPanelDeleteRemovesFileAndTabs(t *testing.T) {
	m := scratchModel(t)
	m, path := newScratchFile(t, m, "txt") // creation opens it in the editor
	if ed := m.activeEditor(); ed == nil || ed.Path() != path {
		t.Fatalf("the new scratch must be open in an editor, got %v", ed)
	}
	tm, _ := m.Update(ScratchPanelToggleMsg{}) // focus the panel again
	m = tm.(Model)
	if m.activeWS().Panes.Focused() != pane.ScratchKey {
		t.Fatalf("focus = %q, want the panel", m.activeWS().Panes.Focused())
	}

	// 'd' only arms the confirmation.
	m = drainKey(m, tea.KeyPressMsg{Code: 'd', Text: "d"})
	if m.scratchPanel().Confirming() != path {
		t.Fatalf("d must arm the confirmation, got %q", m.scratchPanel().Confirming())
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("nothing may be deleted before the answer: %v", err)
	}

	m = drainKey(m, tea.KeyPressMsg{Code: 'y', Text: "y"})
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("the scratch must be gone from disk: %v", err)
	}
	if got := m.scratchPanel().Entries(); len(got) != 0 {
		t.Fatalf("the row must disappear, got %v", got)
	}
	for _, key := range m.activeWS().Panes.Keys() {
		inst := m.activeWS().Panes.Get(key)
		if inst == nil || inst.Kind() != pane.KindEditor {
			continue
		}
		for _, ed := range inst.Editors() {
			if ed.HasFile() && ed.Path() == path {
				t.Fatal("the deleted scratch must not stay open in any pane")
			}
		}
	}
}

// Cancelling the confirmation keeps the file.
func TestScratchPanelDeleteCancelKeepsFile(t *testing.T) {
	m := scratchModel(t)
	m, path := newScratchFile(t, m, "txt")
	tm, _ := m.Update(ScratchPanelToggleMsg{})
	m = tm.(Model)

	m = drainKey(m, tea.KeyPressMsg{Code: 'd', Text: "d"})
	m = drainKey(m, tea.KeyPressMsg{Code: 'n', Text: "n"})
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("a cancelled delete must keep the file: %v", err)
	}
	if got := m.scratchPanel().Entries(); len(got) != 1 {
		t.Fatalf("the row must stay, got %v", got)
	}
}

// A delete of a path the store refuses (outside the scratch dir) leaves the
// file alone and warns instead of closing anything.
func TestScratchPanelDeleteRefusesForeignPath(t *testing.T) {
	m := scratchModel(t)
	outside := t.TempDir() + "/victim.txt"
	if err := os.WriteFile(outside, []byte("keep"), 0o644); err != nil {
		t.Fatal(err)
	}
	tm, cmd := m.Update(scratchpanel.DeleteMsg{Path: outside})
	m = drainCmd(tm.(Model), cmd)
	if _, err := os.Stat(outside); err != nil {
		t.Fatalf("a file outside the scratch dir must survive: %v", err)
	}
}

// The panel survives a layout round trip as the singleton it is — that is
// what carries the dragged divider's height across a restart.
func TestScratchPanelPersists(t *testing.T) {
	t.Setenv("IKE_CONFIG_DIR", t.TempDir())

	m := NewWith(registry.New(), host.MapConfig{})
	out, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	m = out.(Model)
	out, _ = m.Update(ScratchPanelToggleMsg{})
	m = out.(Model)
	if !m.activeWS().Panes.Has(pane.ScratchKey) {
		t.Fatal("setup: panel not open")
	}
	// Drag the divider taller, exactly as the mouse resize does.
	split := scratchSplit(m.activeWS().Tree)
	if split == nil {
		t.Fatal("setup: no scratch split")
	}
	split.Ratio = 0.5
	saveLayout(m.activeWS().Tree, m.activeWS().Panes)

	m2 := NewWith(registry.New(), host.MapConfig{})
	inst := m2.activeWS().Panes.Get(pane.ScratchKey)
	if inst == nil || inst.Kind() != pane.KindScratch {
		t.Fatal("panel did not restore")
	}
	restored := scratchSplit(m2.activeWS().Tree)
	if restored == nil || restored.Ratio != 0.5 {
		t.Fatalf("restored ratio = %v, want the dragged 0.5", restored)
	}
}
