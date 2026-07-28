package settings

// colors_page_test.go covers the syntax-colour editor (#1238): the capture
// list and its override marking, the picker and the free-token input, and the
// write/clear round-trip through the real config layer.

import (
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"ike/internal/config"
	"ike/internal/theme"
)

// colorsPage builds a page over a real user-scope config file.
func colorsPage(t *testing.T) (*ColorsPage, config.Options) {
	t.Helper()
	restoreConfig(t)
	opts := config.Options{UserPath: filepath.Join(t.TempDir(), "settings.toml")}
	p := NewColorsPage(opts)
	p.SetPalette(theme.DefaultPalette())
	return p, opts
}

// reload re-runs the config pipeline the way the app does after a write.
func reload(t *testing.T, opts config.Options, cmd tea.Cmd) {
	t.Helper()
	if cmd == nil {
		t.Fatal("expected a write-reload command")
	}
	msg, ok := cmd().(config.ConfigReloadedMsg)
	if !ok {
		t.Fatalf("expected ConfigReloadedMsg, got %#v", cmd())
	}
	config.Set(msg.Config)
}

// selectCapture positions the page on a capture by name.
func selectCapture(t *testing.T, p *ColorsPage, name string) {
	t.Helper()
	for i, n := range p.rows() {
		if n == name {
			p.sel = i
			return
		}
	}
	t.Fatalf("no row for capture %q", name)
}

// TestColorsPageListsCapturesAndRainbowSlots: every capture the theme defines
// is editable, and so is every rainbow slot.
func TestColorsPageListsCapturesAndRainbowSlots(t *testing.T) {
	p, _ := colorsPage(t)
	names := p.names()
	for _, want := range []string{"keyword", "string", "comment", "rainbow.0", "rainbow.5"} {
		if !contains(names, want) {
			t.Errorf("capture %q must be listed", want)
		}
	}
	if len(names) < len(theme.DefaultPalette().Captures) {
		t.Fatalf("listed %d captures, want at least the theme's %d", len(names), len(theme.DefaultPalette().Captures))
	}
	for i := 1; i < len(names); i++ {
		if names[i-1] > names[i] {
			t.Fatalf("names must be sorted, got %v", names)
		}
	}
}

// TestColorsPageShowsEffectiveColourAndOverride: the row shows the colour in
// use, and an override is visibly marked.
func TestColorsPageShowsEffectiveColourAndOverride(t *testing.T) {
	p, opts := colorsPage(t)
	selectCapture(t, p, "keyword")

	if _, overridden := p.override("keyword"); overridden {
		t.Fatal("setup: keyword must start on the theme colour")
	}
	v := p.View(130, 24)
	if !strings.Contains(v, "keyword") || !strings.Contains(v, swatch) {
		t.Fatalf("the list must show a swatch per capture:\n%s", v)
	}
	if !strings.Contains(v, "from the ") {
		t.Fatalf("the detail column must name where the colour comes from:\n%s", v)
	}

	reload(t, opts, p.write("keyword", "#ff8800"))
	token, overridden := p.override("keyword")
	if !overridden || token != "#ff8800" {
		t.Fatalf("override = %q,%v", token, overridden)
	}
	if v := p.View(130, 24); !strings.Contains(v, "#ff8800") || !strings.Contains(v, "your config") {
		t.Fatalf("an override must be visible and attributed:\n%s", v)
	}
}

// TestColorsPagePickerWritesAndClears: the picker writes a named colour, and
// "r" takes the capture back to the theme's own.
func TestColorsPagePickerWritesAndClears(t *testing.T) {
	p, opts := colorsPage(t)
	selectCapture(t, p, "string")

	p.Update(key("enter"))
	if !p.picking {
		t.Fatal("enter must open the colour picker")
	}
	p.pick = 0 // the first named colour
	want := p.candidates("string")[0]
	reload(t, opts, p.Update(key("enter")))
	if p.picking {
		t.Fatal("a pick must close the picker")
	}
	if got := config.Get().Theme.Captures["string"]; got != want {
		t.Fatalf("captures[string] = %q, want %q", got, want)
	}
	if got := config.Origin(opts, "theme.captures.string"); got != "user" {
		t.Fatalf("origin = %q, want user", got)
	}

	reload(t, opts, p.Update(key("r")))
	if _, still := config.Get().Theme.Captures["string"]; still {
		t.Fatal("r must clear the override")
	}
	if got := config.Origin(opts, "theme.captures.string"); got != "default" {
		t.Fatalf("origin after clear = %q, want default", got)
	}
}

// TestColorsPageRejectsInvalidToken: a token the resolver cannot parse is
// refused with feedback instead of silently un-styling the capture.
func TestColorsPageRejectsInvalidToken(t *testing.T) {
	p, _ := colorsPage(t)
	selectCapture(t, p, "number")

	p.Update(key("e"))
	if !p.custom {
		t.Fatal("e must open the token input")
	}
	p.input.Set("nosuchcolour")
	if cmd := p.Update(key("enter")); cmd != nil {
		t.Fatal("an invalid token must not write")
	}
	if p.invalid == "" {
		t.Fatal("an invalid token must be reported")
	}
	if v := p.View(130, 24); !strings.Contains(v, "not a colour") {
		t.Fatalf("the rejection must be visible:\n%s", v)
	}
	// A valid one goes through.
	p.input.Set("#00ff00")
	if cmd := p.Update(key("enter")); cmd == nil {
		t.Fatal("a valid token must write")
	}
	if p.invalid != "" {
		t.Fatalf("the error must clear on success, got %q", p.invalid)
	}
}

// TestColorsPageEmptyTokenClears: clearing the input is the same as "r".
func TestColorsPageEmptyTokenClears(t *testing.T) {
	p, opts := colorsPage(t)
	selectCapture(t, p, "comment")
	reload(t, opts, p.write("comment", "red"))

	p.Update(key("e"))
	p.input.Set("")
	reload(t, opts, p.Update(key("enter")))
	if _, still := config.Get().Theme.Captures["comment"]; still {
		t.Fatal("an empty token must clear the override")
	}
}

// TestColorsPageOverrideOfAnUnknownCaptureStaysReachable: a capture the theme
// does not define — set by hand or by an older theme — must still be listed,
// or the user could never take it back.
func TestColorsPageOverrideOfAnUnknownCaptureStaysReachable(t *testing.T) {
	p, opts := colorsPage(t)
	if err := config.WriteKey(opts, config.UserScope, "theme.captures.function.builtin", "orange"); err != nil {
		t.Fatal(err)
	}
	cfg, _ := config.Load(opts)
	config.Set(cfg)

	if !contains(p.names(), "function.builtin") {
		t.Fatalf("an override outside the theme must stay listed, got %v", p.names())
	}
	selectCapture(t, p, "function.builtin")
	reload(t, opts, p.Update(key("r")))
	if _, still := config.Get().Theme.Captures["function.builtin"]; still {
		t.Fatal("the dotted capture must be clearable")
	}
}

// TestColorsPageFilterAndSearch: "/" narrows the list, and every capture is
// reachable from the panel-wide search.
func TestColorsPageFilterAndSearch(t *testing.T) {
	p, _ := colorsPage(t)
	p.Update(key("/"))
	for _, r := range "rain" {
		p.Update(keyRune(r))
	}
	rows := p.rows()
	if len(rows) != rainbowSlots {
		t.Fatalf("filter 'rain' matched %d rows, want the %d rainbow slots", len(rows), rainbowSlots)
	}
	items := p.SearchItems()
	if len(items) != len(p.names()) {
		t.Fatalf("SearchItems = %d, want one per capture (%d)", len(items), len(p.names()))
	}
}

// TestColorsPageDetailClicksTakeACandidate: the picker is mouse-complete.
func TestColorsPageDetailClicksTakeACandidate(t *testing.T) {
	p, opts := colorsPage(t)
	selectCapture(t, p, "type")
	p.Update(key("enter"))
	p.View(130, 24) // records the column widths and the candidate top
	if p.candTop == 0 {
		t.Fatal("the picker must record where its candidates start")
	}
	want := p.candidates("type")[1]
	reload(t, opts, p.Click(p.listW+2, p.candTop+1))
	if got := config.Get().Theme.Captures["type"]; got != want {
		t.Fatalf("captures[type] = %q, want the clicked %q", got, want)
	}
}

// TestInsertAfterPlacesThePage: the page sits behind Appearance, inside CORE.
func TestInsertAfterPlacesThePage(t *testing.T) {
	pages := InsertAfter(BasePages([]string{"default"}), "Appearance", Page{Title: "Syntax Colors"})
	idx := -1
	for i, p := range pages {
		if p.Title == "Appearance" {
			idx = i
		}
	}
	if idx < 0 || idx+1 >= len(pages) || pages[idx+1].Title != "Syntax Colors" {
		t.Fatalf("page order = %v", titlesOf(pages))
	}
	if pages[idx+1].Section != "" {
		t.Fatal("a page inserted mid-section must not start a new one")
	}
	// An unknown anchor appends rather than dropping the page.
	pages = InsertAfter(BasePages([]string{"default"}), "Nope", Page{Title: "X"})
	if pages[len(pages)-1].Title != "X" {
		t.Fatal("an unknown anchor must append")
	}
}

func titlesOf(pages []Page) []string {
	out := make([]string, len(pages))
	for i, p := range pages {
		out[i] = p.Title
	}
	return out
}
