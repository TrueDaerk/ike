package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
)

// pins_test.go covers harpoon-style pinned file slots (#788): the store
// (pin/replace/remove/swap/persistence), the jump commands, and the picker.

func TestPinStorePersistence(t *testing.T) {
	t.Setenv("IKE_CONFIG_DIR", t.TempDir())
	p := loadPins()
	p.Set(0, "/a.go")
	p.Set(2, "/c.go")
	if p.Get(0) != "/a.go" || p.Get(1) != "" || p.Get(2) != "/c.go" {
		t.Fatalf("slots = %v", p.Slots)
	}
	// Replace and single-slot invariant: re-pinning a path elsewhere moves it.
	p.Set(1, "/a.go")
	if p.Get(0) != "" || p.Get(1) != "/a.go" {
		t.Fatalf("re-pin must move the path: %v", p.Slots)
	}
	p.Swap(1, 2)
	if p.Get(1) != "/c.go" || p.Get(2) != "/a.go" {
		t.Fatalf("swap failed: %v", p.Slots)
	}
	p.Clear(1)
	if p.Get(1) != "" {
		t.Fatal("clear failed")
	}
	// A fresh load sees the persisted state.
	q := loadPins()
	if q.Get(2) != "/a.go" || q.Get(1) != "" {
		t.Fatalf("reload = %v", q.Slots)
	}
	// Bounds are inert.
	p.Set(9, "/x")
	p.Clear(-1)
	p.Swap(0, 9)
	if _, err := os.Stat(pinsFile()); err != nil {
		t.Fatal("store file must exist after saves")
	}
}

func TestPinAndJumpCommands(t *testing.T) {
	_, files := navProject(t)
	m := newSized()
	tm, _ := m.openPath(files[0], false)
	m = tm.(Model)
	m = step(m, PinSlotMsg{Slot: 1})
	if got := m.pins.Get(0); got != canonicalPath(files[0]) {
		t.Fatalf("slot 1 = %q, want %q", got, files[0])
	}
	// Open another file, jump back via the slot.
	tm, _ = m.openPath(files[1], false)
	m = tm.(Model)
	tm, _ = m.Update(PinJumpMsg{Slot: 1})
	m = tm.(Model)
	if got := m.activeFilePath(); got != canonicalPath(files[0]) {
		t.Fatalf("jump landed on %q, want %q", got, files[0])
	}
	// Jumping to an empty slot is a friendly no-op.
	tm, _ = m.Update(PinJumpMsg{Slot: 3})
	m = tm.(Model)
	if got := m.activeFilePath(); got != canonicalPath(files[0]) {
		t.Fatal("empty-slot jump must not navigate")
	}
}

func TestPinJumpMissingFileOpensPicker(t *testing.T) {
	_, files := navProject(t)
	m := newSized()
	tm, _ := m.openPath(files[0], false)
	m = tm.(Model)
	m = step(m, PinSlotMsg{Slot: 2})
	if err := os.Remove(files[0]); err != nil {
		t.Fatal(err)
	}
	tm, _ = m.Update(PinJumpMsg{Slot: 2})
	m = tm.(Model)
	if !m.pinPickerOpen() {
		t.Fatal("a missing pinned file must open the picker (offer to unpin)")
	}
	if m.pinSel != 1 {
		t.Fatalf("picker selection = %d, want the missing slot 1", m.pinSel)
	}
	// x unpins the missing slot.
	m = step(m, tea.KeyPressMsg{Text: "x", Code: 'x'})
	if m.pins.Get(1) != "" {
		t.Fatal("x must unpin the selected slot")
	}
}

func TestPinPickerNavigateReorderJump(t *testing.T) {
	_, files := navProject(t)
	m := newSized()
	for i := 0; i < 2; i++ {
		tm, _ := m.openPath(files[i], false)
		m = tm.(Model)
		m = step(m, PinSlotMsg{Slot: i + 1})
	}
	m = step(m, PinPickerMsg{})
	if !m.pinPickerOpen() || m.pinSel != 0 {
		t.Fatal("picker must open at slot 1")
	}
	body := m.renderPinPicker()
	if !strings.Contains(body, filepath.Base(files[0])) || !strings.Contains(body, filepath.Base(files[1])) {
		t.Fatalf("picker must list pinned names, got %q", body)
	}
	// j moves, J swaps downward.
	m = step(m, tea.KeyPressMsg{Text: "J", Code: 'J', Mod: tea.ModShift})
	if m.pins.Get(0) != canonicalPath(files[1]) || m.pins.Get(1) != canonicalPath(files[0]) {
		t.Fatalf("reorder failed: %v", m.pins.Slots)
	}
	if m.pinSel != 1 {
		t.Fatalf("selection must follow the moved slot, sel = %d", m.pinSel)
	}
	// enter jumps to the selected slot (files[0], now slot 2) and closes.
	m = step(m, tea.KeyPressMsg{Code: tea.KeyEnter})
	if m.pinPickerOpen() {
		t.Fatal("enter must close the picker")
	}
	if got := m.activeFilePath(); got != canonicalPath(files[0]) {
		t.Fatalf("enter landed on %q, want %q", got, files[0])
	}
	// esc closes without navigating.
	m = step(m, PinPickerMsg{})
	m = step(m, tea.KeyPressMsg{Code: tea.KeyEscape})
	if m.pinPickerOpen() {
		t.Fatal("esc must close the picker")
	}
}

func TestPinPickerPinsActiveFile(t *testing.T) {
	_, files := navProject(t)
	m := newSized()
	tm, _ := m.openPath(files[2], false)
	m = tm.(Model)
	m = step(m, PinPickerMsg{})
	m = step(m, tea.KeyPressMsg{Text: "j", Code: 'j'}) // slot 2
	m = step(m, tea.KeyPressMsg{Text: "p", Code: 'p'})
	if got := m.pins.Get(1); got != canonicalPath(files[2]) {
		t.Fatalf("p must pin the active file to the selected slot, got %q", got)
	}
}

func TestPinCommandsRegistered(t *testing.T) {
	m := newSized()
	for _, id := range []string{
		"nav.pins",
		"nav.pinSlot1", "nav.pinSlot2", "nav.pinSlot3", "nav.pinSlot4",
		"nav.pinGoto1", "nav.pinGoto2", "nav.pinGoto3", "nav.pinGoto4",
	} {
		if _, ok := m.reg.Command(id); !ok {
			t.Errorf("%s must be registered", id)
		}
	}
}
