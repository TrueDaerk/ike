package settings

// pages_mouse_test.go covers #674: the optional PageClicker/PageWheeler seams
// and their implementations on the custom pages (Toolchain, Keymap, LSP,
// Plugins, Marketplace).

import (
	"os"
	"path/filepath"
	"testing"

	tea "charm.land/bubbletea/v2"

	"ike/internal/config"
	"ike/internal/lang"
	"ike/internal/market"
)

// mouseStub is a custom page recording forwarded mouse events.
type mouseStub struct {
	stubPage
	clicks [][2]int
	wheels []int
}

func (s *mouseStub) Click(x, y int) tea.Cmd { s.clicks = append(s.clicks, [2]int{x, y}); return nil }
func (s *mouseStub) Wheel(delta int)        { s.wheels = append(s.wheels, delta) }

func TestPanelForwardsMouseToCustomPage(t *testing.T) {
	restoreConfig(t)
	stub := &mouseStub{}
	pages := append(testPages(), Page{Title: "Custom", Custom: stub})
	m := New(pages, testOpts(t))
	m.SetSize(90, 20)
	m.Open()
	m.cat = 2 // the custom page

	formX := 1 + catWidth + 3
	// Click in the form column: forwarded with page-local coordinates
	// ((0,0) = top-left of the page's render area, body top is row 2).
	m.Click(formX+5, 2+4)
	if len(stub.clicks) != 1 || stub.clicks[0] != [2]int{5, 4} {
		t.Fatalf("click must forward page-locally, got %v", stub.clicks)
	}
	if m.focus != formColumn {
		t.Fatal("a form-column click must focus the form column")
	}
	// Click in the category column: stays with the panel (selects a page).
	m.Click(2, 2)
	if len(stub.clicks) != 1 || m.cat != 0 {
		t.Fatalf("category click must not forward, clicks=%v cat=%d", stub.clicks, m.cat)
	}
	m.cat = 2

	// Wheel over the form column: forwarded as a delta.
	m.Wheel(formX+5, 3)
	if len(stub.wheels) != 1 || stub.wheels[0] != 3 {
		t.Fatalf("wheel must forward the delta, got %v", stub.wheels)
	}
	// Wheel over the category column: stays with the panel.
	m.Wheel(2, -1)
	if len(stub.wheels) != 1 || m.cat != 1 {
		t.Fatalf("category wheel must not forward, wheels=%v cat=%d", stub.wheels, m.cat)
	}

	// A page without the seams is simply inert (no panic, no selection).
	plain := &stubPage{}
	m2 := New(append(testPages(), Page{Title: "Plain", Custom: plain}), testOpts(t))
	m2.SetSize(90, 20)
	m2.Open()
	m2.cat = 2
	m2.Click(formX+5, 2+4)
	m2.Wheel(formX+5, 3)
}

func TestToolchainPageMouse(t *testing.T) {
	restoreConfig(t)
	lang.Register(lang.Language{ID: "tcmouse", Extensions: []string{"tcmouse"}, Toolchain: fakeTC{detected: "/detected/bin/x"}})
	proj := t.TempDir()
	opts := config.Options{UserPath: filepath.Join(t.TempDir(), "settings.toml"), ProjectRoot: proj}
	p := NewToolchainPage(opts, proj, nil)
	p.run = func(string, ...string) string { return "X 9.9" }
	p.look = func(string) string { return "" }

	idx := -1
	for i, l := range p.languages() {
		if l.ID == "tcmouse" {
			idx = i
		}
	}
	p.View(120, 40) // sets the list-window height; off stays 0 (tall window)

	// Click selects the row (header is line 0, rows start at 1).
	p.Click(3, 1+idx)
	if p.sel != idx {
		t.Fatalf("click must select row %d, sel=%d", idx, p.sel)
	}
	// Second click opens the picker.
	p.Click(3, 1+idx)
	if !p.picking {
		t.Fatal("click on the selection must open the picker")
	}
	// With no candidates the only picker line is "custom path…": clicking it
	// opens the custom input.
	p.Click(3, 1+idx+1)
	if p.picking || !p.custom {
		t.Fatalf("clicking custom path… must open the input, picking=%v custom=%v", p.picking, p.custom)
	}
	p.custom = false

	// A click on a real candidate chooses it.
	interp := filepath.Join(proj, "bin-x")
	if err := os.WriteFile(interp, []byte("#!"), 0o755); err != nil {
		t.Fatal(err)
	}
	p.candidates, p.picking, p.pick = []string{interp}, true, 0
	cmd := p.Click(3, 1+idx+1)
	if cmd == nil || p.picking {
		t.Fatal("clicking a candidate must choose it")
	}
	drainBatch(t, p, cmd)
	if got := config.Get().Lang["tcmouse"]["interpreter"]; got != interp {
		t.Fatalf("interpreter = %q, want %q", got, interp)
	}

	// A click outside the picker closes it without choosing.
	p.candidates, p.picking, p.pick = []string{interp}, true, 0
	if cmd := p.Click(3, 1+idx+5); cmd != nil || p.picking {
		t.Fatalf("outside click must close the picker, picking=%v", p.picking)
	}

	// Wheel moves the selection; in the picker it moves the highlight.
	p.sel = 0
	p.Wheel(1)
	if p.sel != 1 && len(p.languages()) > 1 {
		t.Fatalf("wheel must move the selection, sel=%d", p.sel)
	}
	p.candidates, p.picking, p.pick = []string{interp}, true, 0
	p.Wheel(5)
	if p.pick != 1 { // clamped one past the end = "custom path…"
		t.Fatalf("picker wheel must clamp to the custom entry, pick=%d", p.pick)
	}
	p.picking = false
}

func TestKeymapPageMouse(t *testing.T) {
	k, _ := keymapPage(t)
	k.View(120, 80) // sets the list-window height; off stays 0

	// Click selects a row (header is line 0).
	k.Click(3, 1+2)
	if k.sel != 2 {
		t.Fatalf("click must select row 2, sel=%d", k.sel)
	}
	// Second click starts the chord capture (enter semantics).
	k.Click(3, 1+2)
	if !k.capturing {
		t.Fatal("click on the selection must start the capture")
	}
	// A click during capture cancels it.
	k.Click(3, 1+4)
	if k.capturing || k.sel != 2 {
		t.Fatalf("click during capture must cancel it, capturing=%v sel=%d", k.capturing, k.sel)
	}

	// The header row opens the filter input; a further click closes it.
	k.Click(3, 0)
	if !k.filtering {
		t.Fatal("header click must open the filter input")
	}
	k.Click(3, 1+1)
	if k.filtering {
		t.Fatal("click while filtering must return to the list")
	}

	// Wheel moves the selection, clamped.
	k.sel = 0
	k.Wheel(3)
	if k.sel != 3 {
		t.Fatalf("wheel must move the selection, sel=%d", k.sel)
	}
	k.Wheel(-99)
	if k.sel != 0 {
		t.Fatalf("wheel must clamp, sel=%d", k.sel)
	}
}

func TestLSPPageMouse(t *testing.T) {
	p, _, _ := lspPageFixture(t)
	p.View(120, 40) // sets the list-window height; off stays 0

	idx := -1
	var id string
	for i, l := range p.servers() {
		if l.ID == "lsptest" {
			idx, id = i, l.ID
		}
	}

	// Click selects the row (three pinned header lines above the list).
	p.sel = -1
	p.Click(3, lspHeadLines+idx)
	if p.sel != idx {
		t.Fatalf("click must select row %d, sel=%d", idx, p.sel)
	}
	// Second click toggles the per-server enable (the page's `e` action).
	cmd := p.Click(3, lspHeadLines+idx)
	if cmd == nil {
		t.Fatal("click on the selection must toggle the server enable")
	}
	drainLSP(t, p, cmd)
	if serverOn(id) {
		t.Fatal("second click must disable the server")
	}

	// A click while the override input is open cancels it.
	p.startEdit(lspEditCommand, "x")
	p.Click(3, lspHeadLines+idx)
	if p.editing != lspEditNone {
		t.Fatal("click must cancel the inline override input")
	}

	// Wheel moves the selection, clamped.
	p.Wheel(-99)
	if p.sel != 0 {
		t.Fatalf("wheel must clamp at the top, sel=%d", p.sel)
	}
}

func TestPluginsPageMouse(t *testing.T) {
	p := pluginsFixture(nil)

	// Rows sort example < lang-go < zeta; header is line 0.
	p.Click(3, 2)
	if p.sel != 1 {
		t.Fatalf("click must select row 1, sel=%d", p.sel)
	}
	// Second click expands the capability inspection (enter semantics).
	p.Click(3, 2)
	if !p.expanded["lang-go"] {
		t.Fatal("click on the selection must expand the row")
	}
	// The next row shifted down by the expansion: clicking its new line
	// selects it.
	span := 1 + len(inspect(p.rows()[1]))
	p.Click(3, 2+span)
	if p.sel != 2 {
		t.Fatalf("click below the expansion must select row 2, sel=%d", p.sel)
	}

	// Wheel moves the selection, clamped.
	p.Wheel(-99)
	if p.sel != 0 {
		t.Fatalf("wheel must clamp at the top, sel=%d", p.sel)
	}
}

func TestMarketplacePageMouse(t *testing.T) {
	p := loadedPage(t, &fakeMarketEngine{installed: map[string]market.Installed{}})

	// One catalog row below the status line; it is already selected, so a
	// click toggles the detail expansion (enter semantics).
	p.Click(3, 1)
	if !p.expanded["example"] {
		t.Fatal("click on the selection must expand the detail")
	}
	// A click on the expanded detail keeps the selection (no toggle).
	p.Click(3, 2)
	if !p.expanded["example"] || p.sel != 0 {
		t.Fatalf("detail click must not collapse, expanded=%v sel=%d", p.expanded["example"], p.sel)
	}
	// Clicking the row line again collapses.
	p.Click(3, 1)
	if p.expanded["example"] {
		t.Fatal("click on the selection must collapse the detail")
	}

	// Wheel clamps on the single row.
	p.Wheel(5)
	if p.sel != 0 {
		t.Fatalf("wheel must clamp, sel=%d", p.sel)
	}
}
