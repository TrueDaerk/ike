package app

import (
	"os"
	"path/filepath"
	"testing"

	tea "charm.land/bubbletea/v2"

	"ike/internal/keymap"
	"ike/internal/telemetry"
)

// deleteChordApp builds an explorer-focused app over a fresh project with the
// cursor on a.txt, and returns the file's path.
func deleteChordApp(t *testing.T) (Model, string) {
	t.Helper()
	root := t.TempDir()
	file := filepath.Join(root, "a.txt")
	writeFile(t, file, "one\n")
	writeFile(t, filepath.Join(root, "b.txt"), "two\n")
	m := packTestModel(t, root)
	return selectEntry(t, m, file), file
}

// TestExplorerBackspaceChordsDeleteWithoutPrompt guards #3107: cmd+backspace
// (super+ under Kitty, meta+ under the legacy encoding) and ctrl+backspace
// trash the selected entry straight away — the cmd+ spelling used to never
// match the press — and the chord is not logged as unbound.
func TestExplorerBackspaceChordsDeleteWithoutPrompt(t *testing.T) {
	pinDarwin(t)
	for _, tc := range []struct {
		name string
		mod  tea.KeyMod
	}{
		{"super", tea.ModSuper},
		{"meta", tea.ModMeta},
		{"ctrl", tea.ModCtrl},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m, file := deleteChordApp(t)
			m = runMsg(m, tea.KeyPressMsg{Code: tea.KeyBackspace, Mod: tc.mod})
			if m.explorer().Prompting() {
				t.Fatal("the backspace chord must not open the confirmation")
			}
			if _, err := os.Stat(file); !os.IsNotExist(err) {
				t.Fatalf("a.txt must be trashed, stat err = %v", err)
			}
			if m.pendUnbound != nil {
				t.Fatalf("the chord left a pending unbound event: %+v", *m.pendUnbound)
			}
			for _, ev := range usageEvents(t, m) {
				if ev.Type == telemetry.TypeKey && ev.Data["status"] == "unbound" &&
					ev.Data["context"] == string(keymap.Explorer) {
					t.Fatalf("the chord was recorded unbound: %+v", ev.Data)
				}
			}
		})
	}
}

// TestExplorerDeleteKeysStillConfirm: d and delete keep the prompt (#3107);
// only the deliberate backspace chords skip it.
func TestExplorerDeleteKeysStillConfirm(t *testing.T) {
	for _, tc := range []struct {
		name string
		key  tea.KeyPressMsg
	}{
		{"d", tea.KeyPressMsg{Code: 'd', Text: "d"}},
		{"delete", tea.KeyPressMsg{Code: tea.KeyDelete}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m, file := deleteChordApp(t)
			m = runMsg(m, tc.key)
			if !m.explorer().Prompting() {
				t.Fatal("the key must open the delete confirmation")
			}
			if _, err := os.Stat(file); err != nil {
				t.Fatalf("a.txt must survive until confirmed: %v", err)
			}
		})
	}
}
