package editor

// replace_panel_edit_test.go guards the find/replace panel's shared line
// editing (#2002). Both fields were append-only — no cursor, no word ops, no
// macOS chords — and now route through ui.EditKey, each field keeping its own
// cursor across a tab switch.

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
)

func openPanel(t *testing.T, text string) Model {
	t.Helper()
	m, _ := loaded(t, text)
	m, _ = m.runAction("replace")
	if m.replPanel == nil {
		t.Fatal("replace panel should be open")
	}
	return m
}

func TestPanelCursorInsertsMidField(t *testing.T) {
	m := openPanel(t, "alpha beta\n")
	m = typeKeys(m, "apha")
	m = send(m, tea.KeyPressMsg{Code: tea.KeyLeft})
	m = send(m, tea.KeyPressMsg{Code: tea.KeyLeft})
	m = send(m, tea.KeyPressMsg{Code: tea.KeyLeft})
	m = typeKeys(m, "l")
	if m.replPanel.find != "alpha" {
		t.Fatalf("find = %q, want %q", m.replPanel.find, "alpha")
	}
	if m.replPanel.findCur != 2 {
		t.Fatalf("cursor = %d, want 2", m.replPanel.findCur)
	}
}

func TestPanelWordAndLineKills(t *testing.T) {
	m := openPanel(t, "alpha beta\n")
	m = typeKeys(m, "alpha beta")
	m = send(m, tea.KeyPressMsg{Code: tea.KeyBackspace, Mod: tea.ModAlt})
	if m.replPanel.find != "alpha " {
		t.Fatalf("alt+backspace: find = %q", m.replPanel.find)
	}
	m = send(m, tea.KeyPressMsg{Code: tea.KeyBackspace, Mod: tea.ModSuper})
	if m.replPanel.find != "" || m.replPanel.findCur != 0 {
		t.Fatalf("super+backspace: find = %q cur = %d", m.replPanel.find, m.replPanel.findCur)
	}
}

func TestPanelWordMotionAndHomeEnd(t *testing.T) {
	m := openPanel(t, "alpha beta\n")
	m = typeKeys(m, "alpha beta")
	m = send(m, tea.KeyPressMsg{Code: tea.KeyLeft, Mod: tea.ModAlt})
	if m.replPanel.findCur != 6 {
		t.Fatalf("alt+left: cur = %d, want 6", m.replPanel.findCur)
	}
	m = send(m, tea.KeyPressMsg{Code: tea.KeyLeft, Mod: tea.ModSuper})
	if m.replPanel.findCur != 0 {
		t.Fatalf("super+left: cur = %d, want 0", m.replPanel.findCur)
	}
	m = send(m, tea.KeyPressMsg{Code: tea.KeyRight, Mod: tea.ModSuper})
	if m.replPanel.findCur != 10 {
		t.Fatalf("super+right: cur = %d, want 10", m.replPanel.findCur)
	}
}

// Each field owns its cursor: tabbing away and back does not lose the spot.
func TestPanelCursorsArePerField(t *testing.T) {
	m := openPanel(t, "alpha beta\n")
	m = typeKeys(m, "abc")
	m = send(m, tea.KeyPressMsg{Code: tea.KeyHome})
	m = send(m, tab())
	m = typeKeys(m, "xyz")
	if m.replPanel.replCur != 3 {
		t.Fatalf("replace cursor = %d, want 3", m.replPanel.replCur)
	}
	m = send(m, tab())
	if m.replPanel.findCur != 0 {
		t.Fatalf("find cursor = %d, want 0 (kept across the tab)", m.replPanel.findCur)
	}
	m = typeKeys(m, "Z")
	if m.replPanel.find != "Zabc" {
		t.Fatalf("find = %q, want %q", m.replPanel.find, "Zabc")
	}
}

// A mid-field edit still re-runs the incremental preview.
func TestPanelMidFieldEditRefreshesPreview(t *testing.T) {
	m := openPanel(t, "alpha beta\n")
	m = typeKeys(m, "bta")
	m = send(m, tea.KeyPressMsg{Code: tea.KeyLeft})
	m = send(m, tea.KeyPressMsg{Code: tea.KeyLeft})
	m = typeKeys(m, "e")
	if m.replPanel.find != "beta" {
		t.Fatalf("find = %q", m.replPanel.find)
	}
	if m.preview.Empty() {
		t.Fatal("preview must recompile after a mid-field edit")
	}
	if len(m.preview.AllMatches(m.buf)) != 1 {
		t.Fatalf("preview matches = %d, want 1", len(m.preview.AllMatches(m.buf)))
	}
}

// ctrl+u still clears the whole field, ahead of the shared editor.
func TestPanelCtrlUClears(t *testing.T) {
	m := openPanel(t, "alpha beta\n")
	m = typeKeys(m, "alpha")
	m = send(m, tea.KeyPressMsg{Code: 'u', Mod: tea.ModCtrl})
	if m.replPanel.find != "" || m.replPanel.findCur != 0 {
		t.Fatalf("ctrl+u: find = %q cur = %d", m.replPanel.find, m.replPanel.findCur)
	}
}

// Typing still replaces a preselected prefill wholesale (#292).
func TestPanelTypingReplacesPreselect(t *testing.T) {
	m, _ := loaded(t, "alpha beta\n")
	m = send(m, key('/'))
	m = typeKeys(m, "alpha")
	m = send(m, tea.KeyPressMsg{Code: tea.KeyEnter})
	m, _ = m.runAction("replace")
	if !m.replPanel.preselect {
		t.Fatal("panel should open with a preselected prefill")
	}
	m = typeKeys(m, "b")
	if m.replPanel.find != "b" || m.replPanel.findCur != 1 {
		t.Fatalf("find = %q cur = %d, want %q/1", m.replPanel.find, m.replPanel.findCur, "b")
	}
}

// A paste lands at the cursor rather than at the end of the field.
func TestPanelPasteAtCursor(t *testing.T) {
	m := openPanel(t, "alpha beta\n")
	m = typeKeys(m, "ala")
	m = send(m, tea.KeyPressMsg{Code: tea.KeyLeft})
	m.PasteText("ph")
	if m.replPanel.find != "alpha" {
		t.Fatalf("find = %q, want %q", m.replPanel.find, "alpha")
	}
	if m.replPanel.findCur != 4 {
		t.Fatalf("cursor = %d, want 4", m.replPanel.findCur)
	}
}

// The active field renders its cursor where the edit position is.
func TestPanelRendersCursorMidField(t *testing.T) {
	m := openPanel(t, "alpha beta\n")
	m = typeKeys(m, "alpha")
	m = send(m, tea.KeyPressMsg{Code: tea.KeyHome})
	rows := m.replacePanelRows(60)
	if len(rows) != 3 {
		t.Fatalf("rows = %d, want 3", len(rows))
	}
	if !strings.Contains(rows[0], "lpha") {
		t.Fatalf("find row lost its text: %q", rows[0])
	}
}
