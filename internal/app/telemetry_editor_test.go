package app

import (
	"os"
	"path/filepath"
	"testing"

	tea "charm.land/bubbletea/v2"

	"ike/internal/host"
	"ike/internal/telemetry"
)

// telemetryEditorModel opens a file in an editor pane on an isolated model, so
// key presses land in a focused editor.
func telemetryEditorModel(t *testing.T) Model {
	t.Helper()
	m := telemetryModel(t, host.MapConfig{})
	path := filepath.Join(t.TempDir(), "a.txt")
	if err := os.WriteFile(path, []byte("alpha bravo charlie\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	tm, cmd := m.openPath(path, false)
	return drainCmd(tm.(Model), cmd)
}

// unboundChords lists the chords logged as unbound in m's usage log.
func unboundChords(t *testing.T, m Model) []string {
	t.Helper()
	var out []string
	for _, ev := range eventsOf(usageEvents(t, m), telemetry.TypeKey) {
		if ev.Data["status"] == "unbound" {
			out = append(out, ev.Data["chord"])
		}
	}
	return out
}

// TestTelemetryEditorOwnedChordNotUnbound (#2303): a chord the editor handles
// itself never reaches the keymap table, but it is bound as far as the user is
// concerned — it must not be logged as a missing keybind. alt+delete (the
// macOS word kill) was the top "unbound" chord in the telemetry before this.
func TestTelemetryEditorOwnedChordNotUnbound(t *testing.T) {
	for _, mode := range []string{"normal", "insert"} {
		t.Run(mode, func(t *testing.T) {
			m := telemetryEditorModel(t)
			if mode == "insert" {
				m = drainKey(m, tea.KeyPressMsg{Code: 'i', Text: "i"})
			}
			m = drainKey(m, tea.KeyPressMsg{Code: tea.KeyDelete, Mod: tea.ModAlt})

			if got := unboundChords(t, m); len(got) != 0 {
				t.Fatalf("editor-owned alt+delete logged as unbound: %v", got)
			}
			if ed := m.focusedEditor(); ed == nil || ed.Text() != "bravo charlie" {
				t.Fatalf("alt+delete did not kill the word in %s mode", mode)
			}
		})
	}
}

// TestTelemetryUnboundChordInEditorStillRecorded keeps the missing-keybind
// signal alive: a chord neither the keymap nor the editor wants is still
// reported, even with an editor focused.
func TestTelemetryUnboundChordInEditorStillRecorded(t *testing.T) {
	m := telemetryEditorModel(t)
	// ctrl+alt+0 is bound to nothing in any preset and means nothing to the
	// editor either.
	m = drainKey(m, tea.KeyPressMsg{Code: '0', Mod: tea.ModCtrl | tea.ModAlt})

	found := false
	for _, c := range unboundChords(t, m) {
		if c == "ctrl+alt+0" {
			found = true
		}
	}
	if !found {
		t.Fatalf("unbound chord not recorded in an editor; got %v", unboundChords(t, m))
	}
}
