package app

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"ike/internal/ansiblevault"
	"ike/internal/lang"
	"ike/internal/layout"
	ilsp "ike/internal/lsp"
	"ike/internal/registry"
	"ike/internal/telemetry"
)

// intention_anchor_test.go guards the placement of the anchored intention
// popup: whatever the layout, the caret's pane and the scroll state, the box
// lands on screen and the composited frame actually shows it.

// vaultIntentionModel opens a plaintext vault.yml (ansible) in a 7-pane
// layout with a password source configured, so "Treat as Vault File" is on
// offer, then hides a popup terminal again — the user's layout.
func vaultIntentionModel(t *testing.T, content string) Model {
	t.Helper()
	ensureLang(lang.Language{ID: "ansible", Extensions: []string{"yml"}})
	t.Setenv(ansiblevault.EnvPassword, "secret")
	t.Setenv(ansiblevault.EnvPasswordFile, "")
	m := sizedWith(t, registry.Global(), 140, 40)
	path := filepath.Join(t.TempDir(), "vault.yml")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	out, _ := m.openPath(path, false)
	m = out.(Model)
	for i, zone := range []layout.Zone{layout.ZoneRight, layout.ZoneBottom, layout.ZoneRight, layout.ZoneBottom, layout.ZoneLeft, layout.ZoneTop} {
		out, _ = m.Update(SplitFocusedMsg{Zone: zone})
		m = out.(Model)
		// A path already open lands on its tab, so each pane gets its own copy.
		copy := filepath.Join(filepath.Dir(path), "vault"+strconv.Itoa(i)+".yml")
		if err := os.WriteFile(copy, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
		out, _ = m.openPath(copy, false)
		m = out.(Model)
	}
	m = openTestPopupWith(t, m)
	out, _ = m.Update(TerminalPopupMsg{})
	m = out.(Model)
	if m.popup.open {
		t.Fatal("the popup terminal must be hidden again")
	}
	return m
}

// editorKeys lists the editor panes of the active workspace in layout order.
func editorKeys(m Model) []string {
	var keys []string
	for _, key := range m.leafOrder() {
		if inst := m.activeWS().Panes.Get(key); inst != nil && inst.Editor() != nil {
			keys = append(keys, key)
		}
	}
	return keys
}

// focusPaneKey moves the keyboard focus onto the pane key.
func focusPaneKey(t *testing.T, m Model, key string) Model {
	t.Helper()
	m.setFocus(key)
	if m.activeWS().Panes.Focused() != key {
		t.Fatalf("focus %q failed", key)
	}
	return m
}

// assertIntentionPopupVisible opens the intention picker through the bridge
// reply and checks the box rectangle is on screen and its first row is in the
// composited frame at that very position.
func assertIntentionPopupVisible(t *testing.T, m Model, label string, wantAnchored bool) Model {
	t.Helper()
	out, _ := m.Update(ilsp.CodeActionsMsg{Path: m.activeEditor().Path(), Intentions: true})
	m = out.(Model)
	if !m.palette.IsOpen() {
		t.Fatalf("%s: the picker must open", label)
	}
	if m.palette.Anchored() != wantAnchored {
		t.Fatalf("%s: anchored = %v, want %v", label, m.palette.Anchored(), wantAnchored)
	}
	v := m.palette.View()
	if v == "" {
		t.Fatalf("%s: the open picker renders nothing", label)
	}
	w, h := lipgloss.Width(v), lipgloss.Height(v)
	x, y := m.palette.Origin(m.width, m.height, w, h)
	if x < 0 || y < 0 || x+w > m.width || y+h > m.height {
		t.Fatalf("%s: popup rect (%d,%d %dx%d) leaves the %dx%d screen (anchored=%v)", label, x, y, w, h, m.width, m.height, m.palette.Anchored())
	}
	first := ansi.Strip(strings.SplitN(v, "\n", 2)[0])
	frame := strings.Split(ansi.Strip(m.render()), "\n")
	if y >= len(frame) {
		t.Fatalf("%s: frame has %d rows, popup row %d", label, len(frame), y)
	}
	row := frame[y]
	if got := ansi.Cut(row, x, x+w); got != first {
		t.Fatalf("%s: frame row %d col %d = %q, want popup top %q (anchored=%v)", label, y, x, got, first, m.palette.Anchored())
	}
	offered := false
	for _, it := range m.actions.Results("", m.paletteContext()) {
		if it.Title == "Treat as Vault File" {
			offered = true
		}
	}
	if !offered {
		t.Fatalf("%s: Treat as Vault File missing", label)
	}
	m.palette.Close()
	return m
}

// TestIntentionPopupVisibleInEveryPane walks every editor pane of the
// multi-pane layout with the caret at the top, at the bottom, at the right
// edge and scrolled out of view, and demands a visible popup each time.
func TestIntentionPopupVisibleInEveryPane(t *testing.T) {
	var b strings.Builder
	for i := 0; i < 200; i++ {
		b.WriteString("key_" + strings.Repeat("x", 150) + ": value\n")
	}
	m := vaultIntentionModel(t, b.String())
	keys := editorKeys(m)
	if len(keys) < 4 {
		t.Fatalf("layout has only %d editor panes", len(keys))
	}
	for _, key := range keys {
		m = focusPaneKey(t, m, key)
		ed := m.activeEditor()
		if ed == nil || !strings.HasSuffix(ed.Path(), ".yml") {
			t.Fatalf("pane %s does not show a vault file", key)
		}
		ed.SetCursor(0, 0)
		m = assertIntentionPopupVisible(t, m, key+" top", true)
		ed.SetCursor(ed.LineCount()-1, 0)
		m = assertIntentionPopupVisible(t, m, key+" bottom", true)
		ed.SetCursor(ed.LineCount()-1, 150)
		m = assertIntentionPopupVisible(t, m, key+" right edge", true)
		ed.SetCursor(100, 10)
		ed.SetScroll(0, 0)
		m = assertIntentionPopupVisible(t, m, key+" scrolled below view", false)
		ed.SetCursor(0, 10)
		ed.SetScroll(150, 0)
		m = assertIntentionPopupVisible(t, m, key+" scrolled above view", false)
		ed.SetCursor(5, 0)
		ed.SetScroll(0, 100)
		m = assertIntentionPopupVisible(t, m, key+" scrolled right of view", false)
	}
}

// TestIntentionPopupDrawsAboveBlurredPopupTerminal: a popup terminal that is
// open but blurred (the editor has the keyboard) must not hide the picker.
func TestIntentionPopupDrawsAboveBlurredPopupTerminal(t *testing.T) {
	m := vaultIntentionModel(t, "secret: plaintext\n")
	out, _ := m.Update(TerminalPopupMsg{})
	m = out.(Model)
	if !m.popup.open {
		t.Fatal("the popup terminal must be open again")
	}
	m.blurPopupLayer()
	key := editorKeys(m)[0]
	m = focusPaneKey(t, m, key)
	m.activeEditor().SetCursor(0, 3)
	if !m.popupLayerVisible() {
		t.Fatal("the blurred layer must stay visible")
	}
	assertIntentionPopupVisible(t, m, "blurred popup terminal", true)
}

// TestIntentionPopupCentersWithoutAnchor: with the caret scrolled out of
// the viewport no on-screen anchor exists, so the picker opens centered,
// and its telemetry says so.
func TestIntentionPopupCentersWithoutAnchor(t *testing.T) {
	var b strings.Builder
	for i := 0; i < 200; i++ {
		b.WriteString("key: value\n")
	}
	m := vaultIntentionModel(t, b.String())
	m = focusPaneKey(t, m, editorKeys(m)[0])
	ed := m.activeEditor()
	ed.SetCursor(150, 2)
	ed.SetScroll(0, 0)
	if _, _, _, ok := m.caretPopupAnchor(9); ok {
		t.Fatal("a caret below the viewport must yield no anchor")
	}
	out, _ := m.Update(ilsp.CodeActionsMsg{Path: ed.Path(), Intentions: true})
	m = out.(Model)
	if !m.palette.IsOpen() || m.palette.Anchored() {
		t.Fatalf("open=%v anchored=%v, want an open centered picker", m.palette.IsOpen(), m.palette.Anchored())
	}
	if !m.paletteOnScreen() {
		t.Fatal("the centered picker must lie on screen")
	}
	out, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	m = out.(Model)
	got := eventsOf(usageEvents(t, m), telemetry.TypePaletteDismiss)
	if len(got) != 1 || got[0].Data["anchored"] != "false" {
		t.Fatalf("want one dismissal with anchored=false, got %v", got)
	}
}

// TestIntentionPopupAnchoredTelemetry: the ordinary anchored open reports
// anchored=true on its pick.
func TestIntentionPopupAnchoredTelemetry(t *testing.T) {
	m := vaultIntentionModel(t, "secret: plaintext\n")
	m = focusPaneKey(t, m, editorKeys(m)[0])
	m.activeEditor().SetCursor(0, 3)
	out, _ := m.Update(ilsp.CodeActionsMsg{Path: m.activeEditor().Path(), Intentions: true})
	m = out.(Model)
	if !m.palette.Anchored() {
		t.Fatal("want the anchored picker")
	}
	out, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	m = out.(Model)
	got := eventsOf(usageEvents(t, m), telemetry.TypePaletteDismiss)
	if len(got) != 1 || got[0].Data["anchored"] != "true" {
		t.Fatalf("want one dismissal with anchored=true, got %v", got)
	}
}

// TestFitPopupAnchorRejectsOffScreen: a box that cannot be flipped or
// clamped onto a terminal shorter than itself is rejected.
func TestFitPopupAnchorRejectsOffScreen(t *testing.T) {
	m := Model{width: 80, height: 8}
	if _, _, _, ok := m.fitPopupAnchor(10, 5, 9); ok {
		t.Fatal("an 8-row terminal cannot hold the budgeted box")
	}
	m.height = 40
	x, y, w, ok := m.fitPopupAnchor(70, 38, 9)
	if !ok || x+w > 80 || y < 0 || y+min(9, 12)+5+actionPreviewMaxLines > 40 {
		t.Fatalf("fit = (%d,%d w%d ok=%v), want on screen", x, y, w, ok)
	}
}

// TestIntentionPopupPickEncryptsVaultFile is the user's path end to end:
// alt+enter over the plaintext vault.yml, pick "Treat as Vault File", and the
// file on disk is vault ciphertext.
func TestIntentionPopupPickEncryptsVaultFile(t *testing.T) {
	m := vaultIntentionModel(t, "secret: plaintext\n")
	m = focusPaneKey(t, m, editorKeys(m)[0])
	m.activeEditor().SetCursor(0, 3)
	path := m.activeEditor().Path()
	m = pressAltEnterForIntentions(t, m)
	m = assertIntentionPopupVisible(t, m, "pick", true)
	out, _ := m.Update(ilsp.CodeActionsMsg{Path: path, Intentions: true})
	m = out.(Model)
	for _, r := range "Treat as Vault" {
		out, _ = m.Update(tea.KeyPressMsg{Code: r, Text: string(r)})
		m = out.(Model)
	}
	out, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	m = drainCmd(out.(Model), cmd)
	if ed := m.activeEditor(); ed == nil || !ed.Vault() {
		t.Fatal("the pick must mark the buffer as a vault document")
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(string(raw), "$ANSIBLE_VAULT;") {
		t.Fatalf("file on disk = %q, want vault ciphertext", raw)
	}
}
