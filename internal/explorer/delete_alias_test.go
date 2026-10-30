package explorer

import (
	"os"
	"path/filepath"
	"testing"

	"ike/internal/plugin"
)

// TestDeleteAliasKeymaps pins the JetBrains delete aliases (#3087): each
// chord is explorer-scoped and dispatches explorer.delete. Whether it skips
// the prompt (#3107) is dispatch behaviour, guarded by
// TestDeleteNowSkipsPrompt here and the app-level chord tests.
func TestDeleteAliasKeymaps(t *testing.T) {
	kms := corePlugin{}.Capabilities().Keymaps
	for _, chord := range []string{"d", "cmd+backspace", "ctrl+backspace", "delete"} {
		t.Run(chord, func(t *testing.T) {
			for _, km := range kms {
				if km.Keys == chord {
					if km.CommandID != "explorer.delete" {
						t.Fatalf("%s -> %s, want explorer.delete", chord, km.CommandID)
					}
					if km.Scope != plugin.PaneScope(ctxID) {
						t.Fatalf("%s scope = %v, want explorer pane", chord, km.Scope)
					}
					return
				}
			}
			t.Fatalf("no keymap for %s", chord)
		})
	}
}

// TestDeleteNowSkipsPrompt: DeleteMsg{Now: true} — the backspace chords'
// message (#3107) — trashes the cursor entry without a confirmation, keeps
// the undo step, and stays a no-op on the root row.
func TestDeleteNowSkipsPrompt(t *testing.T) {
	root := tree(t)
	m := mounted(t, root, 40, 20)
	m.SetFocused(true)

	m, cmd := m.Update(DeleteMsg{Now: true}) // cursor on the root
	m, _ = pumpScans(m, cmd)
	if m.Prompting() {
		t.Fatal("DeleteMsg{Now} on the root must not prompt")
	}

	m, _ = send(m, key("j"), key("j")) // root, sub, a.txt
	if c := m.current(); c == nil || c.name != "a.txt" {
		t.Fatalf("cursor not on a.txt: %v", names(m))
	}
	m, cmd = m.Update(DeleteMsg{Now: true})
	m, _ = pumpScans(m, cmd)
	if m.Prompting() {
		t.Fatal("DeleteMsg{Now} must not leave a prompt open")
	}
	if _, err := os.Stat(filepath.Join(root, "a.txt")); !os.IsNotExist(err) {
		t.Fatalf("a.txt should be deleted, err=%v", err)
	}
	m, cmd = m.Update(UndoMsg{})
	pumpScans(m, cmd)
	if _, err := os.Stat(filepath.Join(root, "a.txt")); err != nil {
		t.Fatalf("undo should restore a.txt: %v", err)
	}
}
