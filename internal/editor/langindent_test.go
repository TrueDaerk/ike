package editor

import (
	"testing"

	tea "charm.land/bubbletea/v2"

	"ike/internal/host"
	"ike/internal/lang"
)

// Private test languages so these tests never depend on the compiled-in
// language plugins (which live under plugins/ and are not imported here).
// "tabtest" mimics make/Go (UseTabs true, with an indent opener for the
// auto-indent test), "spacetest" a language that insists on spaces.
func init() {
	tt, st := true, false
	lang.Register(lang.Language{ID: "tabtest", Extensions: []string{"tabtest"}, Filenames: []string{"Tabfile"}, UseTabs: &tt, IndentAfter: []string{":"}})
	lang.Register(lang.Language{ID: "spacetest", Extensions: []string{"spacetest"}, UseTabs: &st})
}

// TestLangIndentDefaultOverridesConfig guards the #1137 resolution order:
// built-in < editor.use_spaces < language default. A language declaring
// UseTabs wins over the global editor.use_spaces preference in both
// directions.
func TestLangIndentDefaultOverridesConfig(t *testing.T) {
	cfg := host.MapConfig{"editor.use_spaces": "true"}
	m, _ := loadedInDir(t, cfg, "", "x.tabtest", "run:\n")
	if m.useSpaces {
		t.Error("UseTabs language must override editor.use_spaces=true")
	}
	if got := m.tabText(); got != "\t" {
		t.Errorf("tabText() = %q, want a literal tab", got)
	}

	cfg2 := host.MapConfig{"editor.use_spaces": "false"}
	m2, _ := loadedInDir(t, cfg2, "", "x.spacetest", "hi\n")
	if !m2.useSpaces {
		t.Error("UseTabs=false language must override editor.use_spaces=false")
	}
}

// TestLangIndentByFilename: the language default also applies to
// filename-matched buffers (the Makefile case — no extension).
func TestLangIndentByFilename(t *testing.T) {
	cfg := host.MapConfig{"editor.use_spaces": "true"}
	m, _ := loadedInDir(t, cfg, "", "Tabfile", "run:\n")
	if m.useSpaces {
		t.Error("filename-matched UseTabs language must override editor.use_spaces=true")
	}
}

// TestLangIndentNoOpinionKeepsConfig: a language without UseTabs (nil) leaves
// the global setting alone.
func TestLangIndentNoOpinionKeepsConfig(t *testing.T) {
	cfg := host.MapConfig{"editor.use_spaces": "true"}
	m, _ := loadedInDir(t, cfg, "", "x.itest", "pass\n") // itest: no UseTabs (indent_test.go)
	if !m.useSpaces {
		t.Error("language without UseTabs must keep editor.use_spaces")
	}
}

// TestEditorconfigOverridesLangIndent completes the resolution order: an
// explicit .editorconfig indent_style keeps the last word over the language
// default — a project that configures Makefile indentation stays in control.
func TestEditorconfigOverridesLangIndent(t *testing.T) {
	cfg := host.MapConfig{"editor.use_spaces": "false"}
	m, _ := loadedInDir(t, cfg,
		"root = true\n\n[*.tabtest]\nindent_style = space\nindent_size = 2\n",
		"x.tabtest", "run:\n")
	if !m.useSpaces {
		t.Error(".editorconfig indent_style = space must override the language's UseTabs")
	}
}

// TestLangIndentAutoIndentProducesTabs: with the language tab default, Enter
// after an opener deepens with a literal tab and `o` on an indented line
// copies it — the recipe-body scenario of #1137.
func TestLangIndentAutoIndentProducesTabs(t *testing.T) {
	cfg := host.MapConfig{"editor.use_spaces": "true", "editor.auto_indent": "true"}
	m, _ := loadedInDir(t, cfg, "", "Tabfile", "run:\n")
	m.cursor.Line = 0
	m = send(m, key('A'), special(tea.KeyEnter))
	if got := line(m, 1); got != "\t" {
		t.Fatalf("Enter after opener: line 1 = %q, want %q", got, "\t")
	}
	m = send(m, special(tea.KeyEscape), key('o'))
	if got := line(m, 2); got != "\t" {
		t.Fatalf("o must copy the tab indent, line 2 = %q, want %q", got, "\t")
	}
}
