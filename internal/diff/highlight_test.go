package diff

// Tests for syntax highlighting inside the diff view (#1699). The language is
// a registry-level fake with a Go span producer (no grammar, no CGo): every
// line starting with "func" gets a keyword capture over its first four runes,
// so the tests observe capture styling without a compiled Tree-sitter grammar.

import (
	"strings"
	"sync"
	"testing"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"ike/internal/highlight"
	"ike/internal/lang"
	"ike/internal/theme"
)

var registerHlLang = sync.OnceFunc(func() {
	lang.Register(lang.Language{
		ID:         "difftest",
		Extensions: []string{"zzhl"},
		Spans: func(lines []string) []lang.Span {
			var out []lang.Span
			for i, l := range lines {
				if strings.HasPrefix(l, "func") {
					out = append(out, lang.Span{Line: i, StartCol: 0, EndCol: 4, Capture: "keyword"})
				}
			}
			return out
		},
	})
})

// hlModel builds a sized diff whose right path resolves to the fake language.
func hlModel(t *testing.T, left, right string) *Model {
	t.Helper()
	registerHlLang()
	m := NewFiles("diff", "/tmp/left.zzhl", "/tmp/right.zzhl", nil)
	m.SetSize(80, 10)
	m.SetContents(left, right)
	return &m
}

// keywordFg is the foreground colour the default capture theme resolves for
// the fake language's "keyword" spans.
func keywordFg(t *testing.T) lipgloss.Style {
	t.Helper()
	th := highlight.NewTheme(nil, nil)
	st, ok := th.Style("keyword")
	if !ok {
		t.Fatal("default theme should style the keyword capture")
	}
	return st
}

func TestSyntaxHighlightBothSides(t *testing.T) {
	// A changed pair: both sides carry the keyword line, so both columns
	// must style "func" — the left column is the virtual (HEAD) document.
	m := hlModel(t, "func a()\nplain", "func b()\nplain")
	plainM := testModel(t, "func a()\nplain", "func b()\nplain") // .txt: no language
	if ansi.Strip(m.View()) != ansi.Strip(plainM.View()) {
		t.Fatalf("highlighting must not change the text:\n%s\nvs\n%s", ansi.Strip(m.View()), ansi.Strip(plainM.View()))
	}
	if m.View() == plainM.View() {
		t.Fatal("highlighted view should differ from the unhighlighted one in styling")
	}
	pal := theme.DefaultPalette()
	fg := keywordFg(t).GetForeground()
	wantLeft := lipgloss.NewStyle().Background(pal.DiffRemoved).Foreground(fg).Render("func")
	wantRight := lipgloss.NewStyle().Background(pal.DiffAdded).Foreground(fg).Render("func")
	v := m.View()
	if !strings.Contains(v, wantLeft) {
		t.Fatalf("left (removed) side should compose keyword foreground with the removed background:\n%q", v)
	}
	if !strings.Contains(v, wantRight) {
		t.Fatalf("right (added) side should compose keyword foreground with the added background:\n%q", v)
	}
}

func TestSyntaxHighlightUnified(t *testing.T) {
	m := hlModel(t, "func a()\nplain", "func b()\nplain")
	m.SetUnified(true)
	pal := theme.DefaultPalette()
	fg := keywordFg(t).GetForeground()
	v := m.View()
	for _, want := range []string{
		lipgloss.NewStyle().Background(pal.DiffRemoved).Foreground(fg).Render("func"),
		lipgloss.NewStyle().Background(pal.DiffAdded).Foreground(fg).Render("func"),
	} {
		if !strings.Contains(v, want) {
			t.Fatalf("unified view should keep diff backgrounds under syntax foregrounds:\n%q", v)
		}
	}
}

func TestSyntaxHighlightVirtualLeftOnly(t *testing.T) {
	// A removed-only keyword line exists in no buffer — its spans must come
	// from the virtual left document's own parse.
	m := hlModel(t, "func gone()\nplain", "plain")
	pal := theme.DefaultPalette()
	fg := keywordFg(t).GetForeground()
	want := lipgloss.NewStyle().Background(pal.DiffRemoved).Foreground(fg).Render("func")
	if !strings.Contains(m.View(), want) {
		t.Fatalf("removed-only line should highlight from the left-side parse:\n%q", m.View())
	}
}

func TestSyntaxHighlightUnchangedRows(t *testing.T) {
	// Unchanged rows keep syntax colouring too (no diff background).
	m := hlModel(t, "func same()\nx", "func same()\ny")
	fg := keywordFg(t).GetForeground()
	want := lipgloss.NewStyle().Foreground(fg).Render("func")
	if !strings.Contains(m.View(), want) {
		t.Fatalf("unchanged row should carry plain syntax foreground:\n%q", m.View())
	}
}

func TestEditModeDefersRightReparse(t *testing.T) {
	m := hlModel(t, "func a()", "func b()")
	if got := m.rightIx.CaptureAt(0, 0); got != "keyword" {
		t.Fatalf("right side should be indexed after SetContents, got %q", got)
	}
	m.SetEditMode(true)
	// Per-keystroke rediff while the embedded editor renders the right
	// column: the right index intentionally stays stale.
	m.Rediff("nope()")
	if got := m.rightIx.CaptureAt(0, 0); got != "keyword" {
		t.Fatalf("edit-mode rediff should not re-parse the right side, got %q", got)
	}
	// Leaving edit mode re-parses once from the final buffer state.
	m.SetEditMode(false)
	if got := m.rightIx.CaptureAt(0, 0); got != "" {
		t.Fatalf("leaving edit mode should re-parse the right side, got %q", got)
	}
	// The left column keeps its own (virtual-document) spans throughout.
	if got := m.leftIx.CaptureAt(0, 0); got != "keyword" {
		t.Fatalf("left side spans should survive edit mode, got %q", got)
	}
}

func TestSideCapsTabExpansion(t *testing.T) {
	ix := highlight.NewIndex([]highlight.Span{{Line: 0, StartCol: 1, EndCol: 5, Capture: "keyword"}})
	caps := sideCaps(ix, 1, "\tfunc")
	if len(caps) != tabWidth+4 {
		t.Fatalf("want %d expanded cells, got %d", tabWidth+4, len(caps))
	}
	for i := 0; i < tabWidth; i++ {
		if caps[i] != "" {
			t.Fatalf("tab cells should carry the tab's capture, got %q at %d", caps[i], i)
		}
	}
	for i := tabWidth; i < tabWidth+4; i++ {
		if caps[i] != "keyword" {
			t.Fatalf("expanded cell %d should keep the keyword capture, got %q", i, caps[i])
		}
	}
}

func TestNoLanguageStaysPlain(t *testing.T) {
	// A .txt diff resolves no language: renderSegment must behave exactly as
	// before (guards the caps == nil path).
	m := testModel(t, "func a()", "func b()")
	if !m.leftIx.Empty() || !m.rightIx.Empty() {
		t.Fatal("a language-less diff should hold no span indexes")
	}
}
