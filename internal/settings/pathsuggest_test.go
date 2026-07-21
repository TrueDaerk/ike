package settings

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"ike/internal/config"
	"ike/internal/lang"
)

// suggestTree builds a fixture: <root>/Development/, <root>/Downloads/,
// <root>/python3 (file).
func suggestTree(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	for _, d := range []string{"Development", "Downloads"} {
		if err := os.Mkdir(filepath.Join(root, d), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(root, "python3"), []byte("#!"), 0o755); err != nil {
		t.Fatal(err)
	}
	return root
}

// typeString feeds s rune-by-rune into the toolchain custom input.
func typeString(p *ToolchainPage, s string) {
	for _, r := range s {
		p.Update(tea.KeyPressMsg{Text: string(r), Code: r})
	}
}

func TestToolchainCustomPathTabCompletes(t *testing.T) {
	restoreConfig(t)
	root := suggestTree(t)
	lang.Register(lang.Language{ID: "tcsuggest", Extensions: []string{"tcsuggest"}, Toolchain: fakeTC{detected: ""}})
	p := NewToolchainPage(testOpts(t), root, nil)
	p.run = func(string, ...string) string { return "" }
	p.look = func(string) string { return "" }
	for i, l := range p.languages() {
		if l.ID == "tcsuggest" {
			p.sel = i
		}
	}
	p.custom = true

	// Typing an ambiguous prefix surfaces both directory candidates.
	typeString(p, filepath.Join(root, "D"))
	if len(p.suggest.candidates) != 2 {
		t.Fatalf("candidates = %v, want the two directories", p.suggest.candidates)
	}
	view := p.View(120, 40)
	if !strings.Contains(view, "Development"+string(filepath.Separator)) || !strings.Contains(view, "Downloads"+string(filepath.Separator)) {
		t.Fatalf("view must list the candidates:\n%s", view)
	}

	// Disambiguate and tab: the input extends to the full directory.
	typeString(p, "ev")
	p.Update(tea.KeyPressMsg{Code: tea.KeyTab})
	if want := filepath.Join(root, "Development") + string(filepath.Separator); p.inputField.text != want {
		t.Fatalf("input after tab = %q, want %q", p.inputField.text, want)
	}

	// esc clears the suggestions with the input.
	p.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	if p.suggest.candidates != nil {
		t.Fatalf("esc must clear suggestions, got %v", p.suggest.candidates)
	}
}

func TestToolchainCustomPathTabCompletesFile(t *testing.T) {
	restoreConfig(t)
	root := suggestTree(t)
	lang.Register(lang.Language{ID: "tcsuggest2", Extensions: []string{"tcsuggest2"}, Toolchain: fakeTC{detected: ""}})
	p := NewToolchainPage(testOpts(t), root, nil)
	p.run = func(string, ...string) string { return "" }
	p.look = func(string) string { return "" }
	for i, l := range p.languages() {
		if l.ID == "tcsuggest2" {
			p.sel = i
		}
	}
	p.custom = true
	typeString(p, filepath.Join(root, "py"))
	p.Update(tea.KeyPressMsg{Code: tea.KeyTab})
	if want := filepath.Join(root, "python3"); p.inputField.text != want {
		t.Fatalf("input after tab = %q, want %q", p.inputField.text, want)
	}
}

func TestPanelPathEntryTabCompletes(t *testing.T) {
	restoreConfig(t)
	root := suggestTree(t)
	pages := []Page{{Title: "Test", Entries: []Entry{
		{Key: "test.path", Type: Path, Title: "Some path", Description: "d", Scope: config.UserScope},
	}}}
	m := New(pages, testOpts(t))
	m.Open()
	m.SetSize(100, 30)
	m.focus = formColumn
	m.Update(key("enter")) // start editing
	if !m.editing {
		t.Fatal("enter must start the inline edit")
	}
	for _, r := range filepath.Join(root, "Dev") {
		m.Update(tea.KeyPressMsg{Text: string(r), Code: r})
	}
	if len(m.suggest.candidates) != 1 {
		t.Fatalf("candidates = %v, want Development only", m.suggest.candidates)
	}
	if v := m.View(); !strings.Contains(v, "Development"+string(filepath.Separator)) {
		t.Fatalf("view must show the suggestion:\n%s", v)
	}
	m.Update(key("tab"))
	if want := filepath.Join(root, "Development") + string(filepath.Separator); m.edit.text != want {
		t.Fatalf("input after tab = %q, want %q", m.edit.text, want)
	}
	m.Update(key("esc"))
	if m.suggest.candidates != nil {
		t.Fatalf("esc must clear suggestions, got %v", m.suggest.candidates)
	}
}

func TestPathSuggestLinesCap(t *testing.T) {
	s := pathSuggest{}
	for i := 0; i < maxSuggestLines+3; i++ {
		s.candidates = append(s.candidates, "c")
	}
	lines := s.lines()
	if len(lines) != maxSuggestLines+1 {
		t.Fatalf("lines = %d, want %d rows plus the more-tail", len(lines), maxSuggestLines+1)
	}
	if !strings.Contains(lines[len(lines)-1], "+3 more") {
		t.Fatalf("tail = %q", lines[len(lines)-1])
	}
}
