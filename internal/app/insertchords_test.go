package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"ike/internal/editor"
	"ike/internal/explorer"
	"ike/internal/keymap"
	ilsp "ike/internal/lsp"
)

// insertchords_test.go guards #2622: while the focused editor is in insert
// mode, a chord that can never be text input — a function key, or a chord
// carrying cmd/ctrl/alt — resolves through the keymap layer instead of being
// swallowed by the buffer, exactly as it does from normal mode. Typing keys
// and the chords the editor itself consumes in insert mode are untouched.

// insertEditor opens a file with the given content in a fresh model and leaves
// the focused editor in insert mode at the buffer start. The settings file
// pre-sets lsp.onboarded so the first-start LSP dialog (#301) does not open
// over the editor and eat the scripted keys.
func insertEditor(t *testing.T, content string) (Model, string) {
	t.Helper()
	dir := t.TempDir()
	cfg := t.TempDir()
	t.Setenv("IKE_CONFIG_DIR", cfg)
	if err := os.WriteFile(filepath.Join(cfg, "settings.toml"), []byte("[lsp]\nonboarded = true\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "x.txt")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	m := New()
	out, _ := m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	m = out.(Model)
	out, _ = m.Update(explorer.OpenFileMsg{Path: path})
	m = out.(Model)
	if m.floats.IsOpen() {
		t.Fatalf("startup must leave the editor clear of modals (onboarding=%v tour=%v)", m.onboardingOpen(), m.tourOpen())
	}
	m = drainKey(m, tea.KeyPressMsg{Text: "i", Code: 'i'})
	if md := m.activeEditor().ModeName(); md != editor.Insert {
		t.Fatalf("editor mode = %v, want Insert", md)
	}
	return m, path
}

// editorText is the focused editor's logical text, with the cursor's
// reverse-video escapes stripped.
func editorText(m Model) string { return ansi.Strip(m.activeEditor().View()) }

// TestInsertModeNonTypingChordsResolve is the issue's core claim: the
// JetBrains navigation keys fire from insert mode, dispatch their command, and
// insert nothing into the buffer.
func TestInsertModeNonTypingChordsResolve(t *testing.T) {
	oldGOOS := keymap.GOOS
	keymap.GOOS = "darwin"
	defer func() { keymap.GOOS = oldGOOS }()
	cases := []struct {
		name string
		key  tea.KeyPressMsg
		cmd  string
	}{
		{"f2", tea.KeyPressMsg{Code: tea.KeyF2}, "lsp.nextDiagnostic"},
		{"shift+f2", tea.KeyPressMsg{Code: tea.KeyF2, Mod: tea.ModShift}, "lsp.prevDiagnostic"},
		{"f4", tea.KeyPressMsg{Code: tea.KeyF4}, "lsp.definition"},
		{"alt+f7", tea.KeyPressMsg{Code: tea.KeyF7, Mod: tea.ModAlt}, "lsp.references"},
		{"cmd+g", tea.KeyPressMsg{Code: 'g', Mod: tea.ModMeta}, "search.nextMatch"},
		{"cmd+shift+g", tea.KeyPressMsg{Code: 'g', Mod: tea.ModMeta | tea.ModShift}, "search.prevMatch"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m, _ := insertEditor(t, "alpha beta\n")
			out, cmd := m.Update(tc.key)
			m = out.(Model)
			var fired bool
			for _, msg := range cmdMsgs(cmd) {
				if ex, ok := msg.(CommandExecutedMsg); ok && ex.ID == tc.cmd {
					fired = true
				}
			}
			if !fired {
				t.Fatalf("%s in insert mode must dispatch %s, got %v", tc.name, tc.cmd, cmdMsgs(cmd))
			}
			// The chord is consumed, not typed: the buffer is unchanged.
			if got := editorText(m); !strings.Contains(got, "alpha beta") || strings.Contains(got, tc.name) {
				t.Fatalf("%s must not edit the buffer; text = %q", tc.name, got)
			}
		})
	}
}

// TestInsertModeTypingKeysUnchanged guards the other half of #627/#2622: the
// keys the editor needs for text entry keep reaching the buffer, and esc still
// leaves insert mode.
func TestInsertModeTypingKeysUnchanged(t *testing.T) {
	m, _ := insertEditor(t, "alpha\n")
	// Printable keys insert.
	for _, k := range []tea.KeyPressMsg{
		{Text: "z", Code: 'z'}, {Text: "9", Code: '9'}, {Text: "!", Code: '!'},
	} {
		m = drainKey(m, k)
	}
	if got := editorText(m); !strings.Contains(got, "z9!alpha") {
		t.Fatalf("printable keys must still insert; text = %q", got)
	}
	// Backspace deletes the last one.
	m = drainKey(m, tea.KeyPressMsg{Code: tea.KeyBackspace})
	if got := editorText(m); !strings.Contains(got, "z9alpha") {
		t.Fatalf("backspace must still delete; text = %q", got)
	}
	// Enter splits the line.
	m = drainKey(m, tea.KeyPressMsg{Code: tea.KeyEnter})
	if got := editorText(m); !strings.Contains(got, "z9") || !strings.Contains(got, "alpha") || strings.Contains(got, "z9alpha") {
		t.Fatalf("enter must still break the line; text = %q", got)
	}
	// Esc leaves insert mode.
	m = drainKey(m, tea.KeyPressMsg{Code: tea.KeyEscape})
	if md := m.activeEditor().ModeName(); md != editor.Normal {
		t.Fatalf("esc must leave insert mode, mode = %v", md)
	}
}

// TestInsertModeEditorChordStillReachesEditor guards the precedence rule: a
// non-typing chord the editor itself consumes in insert mode and the keymap
// has no Editor binding for — ctrl+w, the vim-native word kill (#246) — still
// reaches the editor rather than being eaten by the keymap layer.
func TestInsertModeEditorChordStillReachesEditor(t *testing.T) {
	m, _ := insertEditor(t, "alpha beta\n")
	// Move to the line end and type a word, then kill it with ctrl+w.
	m = drainKey(m, tea.KeyPressMsg{Code: tea.KeyRight})
	for _, r := range "gamma" {
		m = drainKey(m, tea.KeyPressMsg{Text: string(r), Code: r})
	}
	if got := editorText(m); !strings.Contains(got, "agammalpha") {
		t.Fatalf("setup typing failed; text = %q", got)
	}
	m = drainKey(m, tea.KeyPressMsg{Code: 'w', Mod: tea.ModCtrl})
	if got := editorText(m); strings.Contains(got, "gamma") {
		t.Fatalf("ctrl+w must still kill the word in insert mode; text = %q", got)
	}
	if md := m.activeEditor().ModeName(); md != editor.Insert {
		t.Fatalf("ctrl+w must keep insert mode, mode = %v", md)
	}
}

// TestInsertModeCompletionNavigationKeepsWorking guards the completion popup's
// own insert-mode keys through the whole app: with the popup showing, down
// moves the selection and tab accepts the item under it — none of the popup's
// keys is a function key, so the keymap layer must stay out of its way.
func TestInsertModeCompletionNavigationKeepsWorking(t *testing.T) {
	// The completion MRU (shared across the package's models) decides which
	// item the popup opens on, so the test compares accepting straight away
	// against accepting after one "down" instead of naming an item: the two
	// must differ, which is exactly what "down navigated" means.
	accept := func(t *testing.T, nav bool) (string, bool) {
		t.Helper()
		m, path := insertEditor(t, "alpha\n")
		out, _ := m.Update(ilsp.CompletionMsg{Path: path, Line: 0, Col: 0, Items: []ilsp.CompletionItem{
			{Label: "alphaOne", InsertText: "alphaOne"},
			{Label: "alphaTwo", InsertText: "alphaTwo"},
		}})
		m = out.(Model)
		if !m.activeEditor().CompletionOpen() {
			t.Fatal("completion popup should be open")
		}
		if nav {
			m = drainKey(m, tea.KeyPressMsg{Code: tea.KeyDown})
			if !m.activeEditor().CompletionOpen() {
				t.Fatal("down must navigate the popup, not dismiss it")
			}
		}
		m = drainKey(m, tea.KeyPressMsg{Code: tea.KeyTab})
		return editorText(m), m.activeEditor().CompletionOpen()
	}
	first, stillOpen := accept(t, false)
	if stillOpen {
		t.Fatal("tab must accept and close the popup")
	}
	if !strings.Contains(first, "alphaOnealpha") && !strings.Contains(first, "alphaTwoalpha") {
		t.Fatalf("tab must accept the selected item; text = %q", first)
	}
	moved, _ := accept(t, true)
	if moved == first {
		t.Fatalf("down must move the selection; both accepts inserted %q", first)
	}
}
