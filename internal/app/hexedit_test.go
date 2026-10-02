package app

import (
	"bytes"
	"os"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"ike/internal/host"
	"ike/internal/pane"
)

// hexedit_test.go covers the app side of the hex viewer's overwrite edit
// mode (#2876): hex.save on ctrl+s with the watcher stamp, the dirty markers,
// and the unsaved-changes guard on close — for a hex pane of its own and for
// a hex content tab.

// hexEditOpen opens a sniffed-binary fixture in a hex viewer — a pane of its
// own (split off the explorer) or a content tab of the editor pane — and
// returns the model with the viewer focused, plus the file path.
func hexEditOpen(t *testing.T, asTab bool) (Model, string) {
	t.Helper()
	m := hexApp(t, host.MapConfig{})
	path := writeTestBinary(t, "blob.bin")
	if asTab {
		m.setFocus(m.fileEditorKey())
	} else {
		m.setFocus("explorer")
	}
	m = paletteOpen(t, m, path)
	c := m.focusedContent()
	if c == nil || c.Kind() != pane.KindHex {
		t.Fatalf("the open must focus a hex viewer, got %v", c)
	}
	if isTab := m.activeWS().Panes.FocusedInstance().Kind() != pane.KindHex; isTab != asTab {
		t.Fatalf("hex viewer as tab = %v, want %v", isTab, asTab)
	}
	return m, path
}

// hexEditType feeds printable keys through the app's key dispatch.
func hexEditType(m Model, keys string) Model {
	for _, r := range keys {
		out, cmd := m.Update(tea.KeyPressMsg{Code: r, Text: string(r)})
		m = drainCmd(out.(Model), cmd)
	}
	return m
}

// hexEditCtrl presses ctrl+<r> through the app's key dispatch.
func hexEditCtrl(m Model, r rune) Model {
	out, cmd := m.Update(tea.KeyPressMsg{Code: r, Mod: tea.ModCtrl})
	return drainCmd(out.(Model), cmd)
}

// hexEditCmdYields reports whether the cmd tree produces a HexSaveMsg.
func hexEditCmdYields(cmd tea.Cmd) bool {
	pending := []tea.Cmd{cmd}
	for len(pending) > 0 {
		c := pending[0]
		pending = pending[1:]
		if c == nil {
			continue
		}
		switch msg := c().(type) {
		case HexSaveMsg:
			return true
		case tea.BatchMsg:
			pending = append(pending, msg...)
		}
	}
	return false
}

// hexEditTitle is the label the user sees for the focused hex viewer: the
// pane title of a dedicated pane, the active tab label of a tab.
func hexEditTitle(m Model) string {
	inst := m.activeWS().Panes.FocusedInstance()
	if inst.Kind() == pane.KindHex {
		return contentPaneTitle(inst)
	}
	return tabLabels(inst)[inst.ActiveTab()]
}

// TestHexEditSaveInPlace: typed hex digits reach the pane through the app's
// dispatch, the title shows the dirty marker, ctrl+s writes just the changed
// byte in place, clears the marker and stamps the watcher.
func TestHexEditSaveInPlace(t *testing.T) {
	for _, asTab := range []bool{false, true} {
		name := "pane"
		if asTab {
			name = "tab"
		}
		t.Run(name, func(t *testing.T) {
			m, path := hexEditOpen(t, asTab)
			orig, _ := os.ReadFile(path)
			m = hexEditType(m, "6bd4")
			hv := m.focusedContent().Hex()
			if hv.Cursor() != 2 || hv.Modified() != 2 {
				t.Fatalf("typing 6bd4 must edit two bytes, cursor %d modified %d", hv.Cursor(), hv.Modified())
			}
			if !strings.Contains(hexEditTitle(m), "●") {
				t.Fatalf("a dirty hex viewer must show ● in its title, got %q", hexEditTitle(m))
			}
			// ctrl+s resolves to hex.save; the stamp is checked right after
			// HexSaveMsg lands, before the follow-up cmds run — the
			// suppression window is short and the VCS refresh is not.
			out, cmd := m.Update(tea.KeyPressMsg{Code: 's', Mod: tea.ModCtrl})
			m = out.(Model)
			if !hexEditCmdYields(cmd) {
				t.Fatal("ctrl+s in the hex viewer must dispatch hex.save")
			}
			out, cmd = m.Update(HexSaveMsg{})
			m = out.(Model)
			if !m.watcher.SavedRecently(hv.Path()) {
				t.Fatal("the save must be stamped as IKE's own write")
			}
			m = drainCmd(m, cmd)
			if hv.Dirty() {
				t.Fatal("ctrl+s must save and clear the overlay")
			}
			if strings.Contains(hexEditTitle(m), "●") {
				t.Fatalf("the saved viewer must drop its dirty marker, got %q", hexEditTitle(m))
			}
			disk, _ := os.ReadFile(path)
			want := append([]byte{0x6b, 0xd4}, orig[2:]...)
			if !bytes.Equal(disk, want) {
				t.Fatalf("disk = % x, want % x", disk, want)
			}
		})
	}
}

// TestHexEditClosePrompts: closing a dirty hex viewer opens the unsaved-
// changes guard; d discards, s saves — both then close.
func TestHexEditClosePrompts(t *testing.T) {
	for _, asTab := range []bool{false, true} {
		for _, answer := range []rune{'d', 's'} {
			name := "pane/" + string(answer)
			if asTab {
				name = "tab/" + string(answer)
			}
			t.Run(name, func(t *testing.T) {
				m, path := hexEditOpen(t, asTab)
				orig, _ := os.ReadFile(path)
				m = hexEditType(m, "ff")
				m.guardedCloseFocused()
				if !m.closePromptOpen() {
					t.Fatal("closing a dirty hex viewer must open the guard")
				}
				out, cmd := m.updateClosePrompt(tea.KeyPressMsg{Code: answer, Text: string(answer)})
				m = drainCmd(out.(Model), cmd)
				if hasKind(m, pane.KindHex) {
					t.Fatal("the guard's answer must close the hex viewer")
				}
				disk, _ := os.ReadFile(path)
				switch answer {
				case 'd':
					if !bytes.Equal(disk, orig) {
						t.Fatalf("discard must leave the file alone, got % x", disk)
					}
				case 's':
					if disk[0] != 0xff || !bytes.Equal(disk[1:], orig[1:]) {
						t.Fatalf("save must write the edit in place, got % x", disk)
					}
				}
			})
		}
	}
}

// TestHexEditCleanCloseNoPrompt: an unedited hex viewer closes at once.
func TestHexEditCleanCloseNoPrompt(t *testing.T) {
	m, _ := hexEditOpen(t, false)
	m.guardedCloseFocused()
	if m.closePromptOpen() {
		t.Fatal("a clean hex viewer must close without the guard")
	}
	if hasKind(m, pane.KindHex) {
		t.Fatal("the hex pane must be closed")
	}
}

// TestHexEditQuitGuardListsAndSaves: a dirty hex viewer counts as unsaved
// work for the quit guard, and the save-all path writes it.
func TestHexEditQuitGuardListsAndSaves(t *testing.T) {
	m, path := hexEditOpen(t, false)
	m = hexEditType(m, "00")
	dirty, _ := m.quitActivity()
	if len(dirty) != 1 || dirty[0] != "blob.bin" {
		t.Fatalf("quit activity dirty = %v, want the hex file", dirty)
	}
	if cmds := m.saveAllDirty(); len(cmds) != 1 {
		t.Fatalf("save-all must report the hex save, got %d cmds", len(cmds))
	}
	if dirty, _ := m.quitActivity(); len(dirty) != 0 {
		t.Fatalf("after save-all nothing is dirty, got %v", dirty)
	}
	if disk, _ := os.ReadFile(path); disk[0] != 0 {
		t.Fatalf("save-all must write the edit, got % x", disk)
	}
}

// TestHexEditKeysReachPane: the edit keys — tab, i, esc, u, ctrl+r — reach
// the hex viewer through the app's keymap dispatch rather than being taken
// by a global binding.
func TestHexEditKeysReachPane(t *testing.T) {
	m, _ := hexEditOpen(t, false)
	hv := m.focusedContent().Hex()
	m = hexEditType(m, "ff")
	m = hexEditType(m, "u")
	if hv.Dirty() {
		t.Fatal("u must undo the hex byte")
	}
	m = hexEditCtrl(m, 'r')
	if !hv.Dirty() {
		t.Fatal("ctrl+r must redo the hex byte")
	}
	out, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyTab})
	m = drainCmd(out.(Model), cmd)
	if hv.ColumnLabel() != "TEXT" {
		t.Fatalf("tab must switch the column, got %s", hv.ColumnLabel())
	}
	m = hexEditType(m, "iZ?q")
	if hv.ColumnLabel() != "TEXT INSERT" || hv.Modified() != 4 {
		t.Fatalf("i then Z?q must type in the text column (no help, no quit), column %s modified %d", hv.ColumnLabel(), hv.Modified())
	}
	out, cmd = m.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	m = drainCmd(out.(Model), cmd)
	if hv.ColumnLabel() != "TEXT" {
		t.Fatalf("esc must leave text insertion, got %s", hv.ColumnLabel())
	}
	_ = m
}
