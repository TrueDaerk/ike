package settings

// navcost_test.go guards the per-key cost of walking the filter results
// (#2616). Holding an arrow key with a "/" filter up used to re-run the fuzzy
// match over every page twice per key repeat — once in Update, once in View —
// and render every one of the hundreds of matches, each re-reading both config
// layers from disk for its origin colour. The repeats queued up faster than
// they could be drawn, so the highlight kept travelling after the key was
// released. A pure selection move must now touch neither.

import (
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"ike/internal/config"
	"ike/internal/theme"
)

// countingPage is a Searchable custom page that counts how often the panel
// rebuilt the filter's result list: searchRows asks every Searchable page for
// its items, so the counter is exactly the number of recomputations.
type countingPage struct {
	navRows
	items int
}

func (c *countingPage) Update(tea.KeyPressMsg) tea.Cmd { return nil }
func (c *countingPage) View(w, h int) string           { return "" }
func (c *countingPage) SetPalette(p *theme.Palette)    {}
func (c *countingPage) Capturing() bool                { return false }
func (c *countingPage) SearchItems() []SearchItem {
	c.items++
	return []SearchItem{{Label: "counted item", Keywords: "editor tab width"}}
}

// navModel is the panel over the real schema — a schema-sized entry list is
// what the key repeat has to keep up with — plus the counting page.
func navModel(tb testing.TB) (*Model, *countingPage) {
	tb.Setenv("IKE_CONFIG_DIR", tb.TempDir())
	cp := &countingPage{}
	pages := append(BasePages(nil, nil, nil), Page{Title: "Counted", Custom: cp})
	m := New(pages, config.Options{UserPath: filepath.Join(tb.TempDir(), "settings.toml")})
	m.SetSize(120, 40)
	m.Open()
	return m, cp
}

// filtered opens the filter on query and leaves the input (enter), the state
// the reproduction navigates in.
func filtered(m *Model, query string) {
	m.Update(key("/"))
	typeFilter(m, query)
	m.Update(key("enter"))
	m.View()
}

func TestFilterNavDoesNotRebuildTheResultList(t *testing.T) {
	m, cp := navModel(t)
	filtered(m, "tab")
	before := len(m.rows())
	if before < 2 {
		t.Fatalf("the query needs several results, got %d", before)
	}
	cp.items = 0
	m.Update(key("down"))
	m.View()
	if cp.items != 0 {
		t.Fatalf("a selection move rebuilt the result list %d time(s)", cp.items)
	}
	if m.sel != 1 {
		t.Fatalf("the move must step the selection, sel = %d", m.sel)
	}
	if got := len(m.rows()); got != before {
		t.Fatalf("the result list changed under the move: %d → %d", before, got)
	}
}

// TestFilterEditRebuildsTheResultList is the other half of the guard: the
// memo must not survive a key that can change the matches.
func TestFilterEditRebuildsTheResultList(t *testing.T) {
	m, cp := navModel(t)
	m.Update(key("/"))
	typeFilter(m, "tab")
	m.View()
	cp.items = 0
	m.Update(keyRune('w'))
	m.View()
	if cp.items == 0 {
		t.Fatal("editing the query must rebuild the result list")
	}
	if m.filter != "tabw" {
		t.Fatalf("the query must carry the edit, filter = %q", m.filter)
	}
}

// TestFilterNavKeepsResultsAndHighlighting guards that the memo and the
// windowed render change nothing the user sees: the same rows in the same
// order, with the match still marked on the row the selection landed on.
func TestFilterNavKeepsResultsAndHighlighting(t *testing.T) {
	m, _ := navModel(t)
	filtered(m, "tab")
	fresh := m.searchRows("tab")
	for i, r := range m.rows() {
		if r.entry.Key != fresh[i].entry.Key || r.label != fresh[i].label {
			t.Fatalf("row %d differs from a fresh match: %+v vs %+v", i, r, fresh[i])
		}
	}
	m.Update(key("down"))
	out := ansi.Strip(m.View())
	r, ok := m.current()
	if !ok || r.kind != rowEntry {
		t.Fatalf("expected an entry row under the selection, got %+v", r)
	}
	if !strings.Contains(out, r.entry.Title) {
		t.Fatalf("the selected match must be on screen: %q", r.entry.Title)
	}
	if !strings.Contains(out, m.pages[r.page].Title) {
		t.Fatalf("the match's page header must be on screen: %q", m.pages[r.page].Title)
	}
}

// TestFilterNavWrapsWithThePinnedList is a plain behaviour check that the
// pinned memo did not break wrapping.
func TestFilterNavWrapsWithThePinnedList(t *testing.T) {
	m, _ := navModel(t)
	filtered(m, "tab")
	n := len(m.rows())
	m.sel = n - 1
	m.Update(key("down"))
	if m.sel != 0 {
		t.Fatalf("the walk must wrap to the first result, sel = %d", m.sel)
	}
	if got := len(m.rows()); got != n {
		t.Fatalf("the result list changed while wrapping: %d → %d", n, got)
	}
}

// BenchmarkFilterNavStep is the per-key cost of one selection move with a
// filter up: Update plus the frame it draws, on the real schema.
func BenchmarkFilterNavStep(b *testing.B) {
	m, _ := navModel(b)
	filtered(m, "e")
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		m.Update(key("down"))
		_ = m.View()
	}
}

// BenchmarkNavStep is the same move without a filter — the baseline the
// filtered walk has to stay in the same league as.
func BenchmarkNavStep(b *testing.B) {
	m, _ := navModel(b)
	m.Update(key("enter")) // into the form column
	m.View()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		m.Update(key("down"))
		_ = m.View()
	}
}
