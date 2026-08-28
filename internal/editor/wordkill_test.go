package editor

import (
	"testing"

	tea "charm.land/bubbletea/v2"
)

// TestNormalWordKill (#2303): the macOS word-kill chords also work outside
// insert mode — alt+delete / ctrl+delete delete forward like `dw`,
// alt+backspace backward like `db`, both honoring a count. Before this the
// chords fell through as unknown normal-mode keys and the keymap layer logged
// them as unbound.
func TestNormalWordKill(t *testing.T) {
	forward := func(k tea.KeyPressMsg) func(*testing.T) {
		return func(t *testing.T) {
			m, _ := loaded(t, "alpha bravo charlie\n")
			m = send(m, k)
			if line(m, 0) != "bravo charlie" {
				t.Fatalf("forward kill=%q want %q", line(m, 0), "bravo charlie")
			}
			if m.cursor.Col != 0 {
				t.Fatalf("caret at col %d, want 0", m.cursor.Col)
			}
			m = typeKeys(m, "u")
			if line(m, 0) != "alpha bravo charlie" {
				t.Fatalf("undo=%q", line(m, 0))
			}
		}
	}
	t.Run("alt+delete", forward(tea.KeyPressMsg{Code: tea.KeyDelete, Mod: tea.ModAlt}))
	t.Run("ctrl+delete", forward(tea.KeyPressMsg{Code: tea.KeyDelete, Mod: tea.ModCtrl}))
	t.Run("alt+shift+delete", forward(tea.KeyPressMsg{Code: tea.KeyDelete, Mod: tea.ModAlt | tea.ModShift}))

	// The backward kill mirrors the insert-mode alt+backspace of #246.
	t.Run("alt+backspace", func(t *testing.T) {
		m, _ := loaded(t, "alpha bravo charlie\n")
		m = typeKeys(m, "$") // caret on the last rune of "charlie"
		m = send(m, tea.KeyPressMsg{Code: tea.KeyBackspace, Mod: tea.ModAlt})
		if line(m, 0) != "alpha bravo e" {
			t.Fatalf("backward kill=%q want %q", line(m, 0), "alpha bravo e")
		}
	})

	// A count multiplies the kill, like 2dw.
	t.Run("count", func(t *testing.T) {
		m, _ := loaded(t, "alpha bravo charlie\n")
		m = typeKeys(m, "2")
		m = send(m, tea.KeyPressMsg{Code: tea.KeyDelete, Mod: tea.ModAlt})
		if line(m, 0) != "charlie" {
			t.Fatalf("2 alt+delete=%q want %q", line(m, 0), "charlie")
		}
	})

	// A read-only buffer refuses the kill like every other destructive key.
	t.Run("read-only", func(t *testing.T) {
		m, _ := loaded(t, "alpha bravo\n")
		m.readOnly = true
		m = send(m, tea.KeyPressMsg{Code: tea.KeyDelete, Mod: tea.ModAlt})
		if line(m, 0) != "alpha bravo" {
			t.Fatalf("read-only buffer changed: %q", line(m, 0))
		}
	})
}

// TestHandledLastKey (#2303): the editor reports whether a routed key did
// anything, so the host can tell an editor-owned chord apart from a genuinely
// unbound one before logging telemetry.
func TestHandledLastKey(t *testing.T) {
	m, _ := loaded(t, "alpha bravo\n")
	m = send(m, tea.KeyPressMsg{Code: tea.KeyDelete, Mod: tea.ModAlt})
	if !m.HandledLastKey() {
		t.Fatal("normal-mode alt+delete must count as handled")
	}
	m = send(m, tea.KeyPressMsg{Code: 'q', Mod: tea.ModCtrl})
	if m.HandledLastKey() {
		t.Fatal("unknown normal-mode chord ctrl+q must count as unhandled")
	}
	m = typeKeys(m, "i")
	m = send(m, tea.KeyPressMsg{Code: tea.KeyDelete, Mod: tea.ModAlt})
	if !m.HandledLastKey() {
		t.Fatal("insert-mode alt+delete must count as handled")
	}
	m = send(m, tea.KeyPressMsg{Code: 'q', Mod: tea.ModCtrl})
	if m.HandledLastKey() {
		t.Fatal("unknown insert-mode chord ctrl+q must count as unhandled")
	}
}
