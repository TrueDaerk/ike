package app

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	tea "charm.land/bubbletea/v2"

	"ike/internal/host"
	"ike/internal/keymap"
	"ike/internal/ui"
)

// pins.go implements harpoon-style pinned file slots (#788): four numbered
// slots holding files of the current working set. Unlike Recent Files (an
// MRU history whose order shifts constantly), slots are stable — pin once,
// jump by number, muscle memory does the rest. Slots persist per project in
// the .ike state store, like layout and window sizes.

// pinSlotCount is the fixed number of slots. Four keeps every slot a single
// reachable chord (ctrl+1..4) and mirrors harpoon's default working-set size.
const pinSlotCount = 4

// pinsFile returns the per-project slot store path, following the layout
// store's IKE_CONFIG_DIR redirection seam.
func pinsFile() string {
	if d := os.Getenv("IKE_CONFIG_DIR"); d != "" {
		return filepath.Join(d, "pins.json")
	}
	return filepath.Join(".ike", "pins.json")
}

// pinStore holds the slot paths (absolute, canonical) and persists on every
// mutation. The zero slot value "" means empty.
type pinStore struct {
	path  string
	Slots [pinSlotCount]string `json:"slots"`
}

// loadPins reads the store, tolerating a missing or malformed file (all
// slots empty) — failing to read must never disrupt the session.
func loadPins() *pinStore {
	p := &pinStore{path: pinsFile()}
	data, err := os.ReadFile(p.path)
	if err != nil {
		return p
	}
	var onDisk pinStore
	if json.Unmarshal(data, &onDisk) == nil {
		p.Slots = onDisk.Slots
	}
	return p
}

// save persists the store; errors are swallowed (never disrupt the session).
func (p *pinStore) save() {
	data, err := json.Marshal(p)
	if err != nil {
		return
	}
	if dir := filepath.Dir(p.path); dir != "." {
		_ = os.MkdirAll(dir, 0o755)
	}
	_ = os.WriteFile(p.path, data, 0o644)
}

// Get returns slot i's path ("" when empty); out-of-range is empty.
func (p *pinStore) Get(i int) string {
	if p == nil || i < 0 || i >= pinSlotCount {
		return ""
	}
	return p.Slots[i]
}

// Set pins path to slot i (replacing any occupant) and unpins the path from
// any other slot it occupied — a file lives in at most one slot.
func (p *pinStore) Set(i int, path string) {
	if p == nil || i < 0 || i >= pinSlotCount || path == "" {
		return
	}
	for j := range p.Slots {
		if j != i && p.Slots[j] == path {
			p.Slots[j] = ""
		}
	}
	p.Slots[i] = path
	p.save()
}

// Clear empties slot i.
func (p *pinStore) Clear(i int) {
	if p == nil || i < 0 || i >= pinSlotCount {
		return
	}
	p.Slots[i] = ""
	p.save()
}

// Swap exchanges two slots (picker reorder).
func (p *pinStore) Swap(i, j int) {
	if p == nil || i < 0 || j < 0 || i >= pinSlotCount || j >= pinSlotCount || i == j {
		return
	}
	p.Slots[i], p.Slots[j] = p.Slots[j], p.Slots[i]
	p.save()
}

// pinCurrent pins the active editor's file to slot (1-based).
func (m *Model) pinCurrent(slot int) {
	path := m.activeFilePath()
	if path == "" {
		m.host.Notify(host.Info, "no active file to pin")
		return
	}
	path = canonicalPath(path)
	m.pins.Set(slot-1, path)
	m.host.Notify(host.Info, fmt.Sprintf("pinned %s to slot %d", filepath.Base(path), slot))
}

// pinJump opens the file pinned to slot (1-based). A vanished file keeps the
// slot but raises the picker with it selected, so unpinning is one keystroke
// away (the "offers to unpin" of #788).
func (m Model) pinJump(slot int) (tea.Model, tea.Cmd) {
	path := m.pins.Get(slot - 1)
	if path == "" {
		// Chord rendered platform-normalized (#981): ctrl+2 off macOS.
		picker := keymap.NormalizeChord(keymap.MustParseChord("cmd+2"), keymap.GOOS).String()
		m.host.Notify(host.Info, fmt.Sprintf("slot %d is empty — pin via the picker (%s)", slot, picker))
		return m, nil
	}
	if _, err := os.Stat(path); err != nil {
		m.host.Notify(host.Warn, fmt.Sprintf("pinned file missing: %s — x unpins it", displayPath(path)))
		m.openPinPicker(slot - 1)
		return m, nil
	}
	return m.openPath(path, false)
}

// openPinPicker shows the slot picker in the modal shell with sel selected.
func (m *Model) openPinPicker(sel int) {
	if sel < 0 || sel >= pinSlotCount {
		sel = 0
	}
	m.pinSel = sel
	m.pinPicker = true
	m.shell.SetContent(ui.ModelContent{
		Heading: "PINNED FILES",
		Body:    m.renderPinPicker,
	})
	m.shell.Open()
}

// pinPickerOpen reports whether the shell currently shows the slot picker —
// the content check guards against another overlay having taken the shell
// over without the picker's own close path running.
func (m Model) pinPickerOpen() bool {
	if !m.pinPicker || !m.shell.IsOpen() {
		return false
	}
	c, ok := m.shell.Content().(ui.ModelContent)
	return ok && c.Heading == "PINNED FILES"
}

// renderPinPicker draws the four slots plus the key hints.
func (m *Model) renderPinPicker() string {
	var b strings.Builder
	for i := 0; i < pinSlotCount; i++ {
		marker := "  "
		if i == m.pinSel {
			marker = "▍ "
		}
		entry := "(empty)"
		if p := m.pins.Get(i); p != "" {
			entry = filepath.Base(p) + "  —  " + displayPath(filepath.Dir(p))
			if _, err := os.Stat(p); err != nil {
				entry += "  (missing)"
			}
		}
		fmt.Fprintf(&b, "%s%d  %s\n", marker, i+1, entry)
	}
	b.WriteString("\nenter open · p pin active file · x unpin · shift+j/k reorder · 1-4 jump · esc close")
	return b.String()
}

// updatePinPicker consumes every key while the picker is open: navigation,
// reorder, unpin, jump. Everything else is swallowed (the picker is modal).
func (m Model) updatePinPicker(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	closePicker := func() {
		m.pinPicker = false
		m.shell.Close()
	}
	switch key := msg.String(); key {
	case "esc", "q":
		closePicker()
		return m, nil
	case "j", "down":
		if m.pinSel < pinSlotCount-1 {
			m.pinSel++
		}
		return m, nil
	case "k", "up":
		if m.pinSel > 0 {
			m.pinSel--
		}
		return m, nil
	case "J", "shift+down":
		if m.pinSel < pinSlotCount-1 {
			m.pins.Swap(m.pinSel, m.pinSel+1)
			m.pinSel++
		}
		return m, nil
	case "K", "shift+up":
		if m.pinSel > 0 {
			m.pins.Swap(m.pinSel, m.pinSel-1)
			m.pinSel--
		}
		return m, nil
	case "x", "d", "backspace":
		m.pins.Clear(m.pinSel)
		return m, nil
	case "p":
		// Pin the active file to the selected slot — the picker-side twin of
		// nav.pinSlotN, since the pin commands have no default chord (the
		// ctrl+shift+digit family is taken by the jumps).
		m.pinCurrent(m.pinSel + 1)
		return m, nil
	case "1", "2", "3", "4":
		closePicker()
		return m.pinJump(int(key[0] - '0'))
	case "enter":
		slot := m.pinSel + 1
		closePicker()
		return m.pinJump(slot)
	}
	return m, nil
}
