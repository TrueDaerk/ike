package tour

import (
	"strings"
	"testing"

	"charm.land/lipgloss/v2"

	"ike/internal/keymap"
)

// mapResolver is a trivial command-id -> shortcut resolver for tests.
type mapResolver map[string]string

func (m mapResolver) Binding(id string) (string, bool) {
	s, ok := m[id]
	return s, ok
}

func TestPagingClampsAndIndicator(t *testing.T) {
	tr := New(nil)
	if tr.Page() != 0 {
		t.Fatal("tour must start on page 1")
	}
	if tr.Prev() {
		t.Fatal("prev on the first page must clamp")
	}
	if tr.Title() != "WELCOME TO IKE — 1/5" {
		t.Fatalf("title = %q", tr.Title())
	}
	for i := 0; i < tr.PageCount()-1; i++ {
		if !tr.Next() {
			t.Fatalf("next must advance from page %d", i+1)
		}
	}
	if tr.Title() != "WELCOME TO IKE — 5/5" {
		t.Fatalf("last title = %q", tr.Title())
	}
	if tr.Next() {
		t.Fatal("next on the last page must report no change (host closes)")
	}
	if !tr.Prev() || tr.Page() != tr.PageCount()-2 {
		t.Fatal("prev must step back from the last page")
	}
}

func TestPagesFitTheShellBudget(t *testing.T) {
	// Every page (body + legend) must fit ~72×16 so the shell never scrolls
	// it — scrolling would make space ambiguous with paging.
	tr := New(nil)
	for i := 0; i < tr.PageCount(); i++ {
		tr.page = i
		body := tr.Render(72)
		if w := lipgloss.Width(body); w > 90 {
			t.Errorf("page %d is %d cells wide", i+1, w)
		}
		if h := lipgloss.Height(body); h > 16 {
			t.Errorf("page %d is %d rows tall, budget 16", i+1, h)
		}
	}
}

func TestPageContent(t *testing.T) {
	tr := New(nil)
	checks := []struct{ page int; want []string }{
		{0, []string{"shift shift · cmd+shift+a", "quit IKE", "ctrl+c"}},
		{1, []string{"NORMAL mode", "Press i", "cmd+s · :w"}},
		{2, []string{"cmd+1", "cmd+shift+o", "cmd+k right"}},
		{3, []string{"alt+f12", "shift+f10", "every key goes to the shell"}},
		{4, []string{"Settings", "Next: pick language servers to install", "essentials"}},
	}
	for _, c := range checks {
		tr.page = c.page
		body := tr.Render(72)
		for _, want := range c.want {
			if !strings.Contains(body, want) {
				t.Errorf("page %d missing %q:\n%s", c.page+1, want, body)
			}
		}
	}
	// The paging legend + reopen hint is on every page.
	for i := 0; i < tr.PageCount(); i++ {
		tr.page = i
		if body := tr.Render(72); !strings.Contains(body, "Welcome Tour\" in the palette") {
			t.Errorf("page %d missing the reopen hint", i+1)
		}
	}
}

func TestRemappedBindingDisplays(t *testing.T) {
	// A resolver binding outside the curated default list is a real remap and
	// replaces the default text; a binding already in the list keeps the
	// curated preferred-order display.
	tr := New(mapResolver{
		"editor.write":             "f5",          // remapped
		"palette.searchEverywhere": "shift shift", // part of the curated list
	})
	tr.page = 1
	if body := tr.Render(72); !strings.Contains(body, "f5") || strings.Contains(body, "cmd+s") {
		t.Fatalf("remapped save must display the live chord:\n%s", body)
	}
	tr.page = 0
	if body := tr.Render(72); !strings.Contains(body, "shift shift · cmd+shift+a") {
		t.Fatalf("curated multi-chord list must survive a matching binding:\n%s", body)
	}
}

func TestRemapKeepsNonChordHints(t *testing.T) {
	// #678: on a real remap the live chord leads, replaced chord options are
	// dropped, but curated vim hints (":w" is handled by the editor, not the
	// keymap) stay valid and are kept as secondary options.
	tr := New(mapResolver{"editor.write": "f5"})
	tr.page = 1
	if body := tr.Render(72); !strings.Contains(body, "f5 · :w") {
		t.Fatalf("remapped save must keep the vim hint: %s", body)
	}
}

func TestUnboundFallsBackToCurated(t *testing.T) {
	// A resolver that knows nothing must leave every curated default in place.
	tr := New(mapResolver{})
	tr.page = 1
	if body := tr.Render(72); !strings.Contains(body, "cmd+s · :w") {
		t.Fatalf("unbound command must keep the curated default: %s", body)
	}
}

func TestHelpRowResolves(t *testing.T) {
	// #678: the help cheat-sheet row goes through the resolver like every
	// other row — a remap of palette.keymapHelp must display.
	tr := New(mapResolver{"palette.keymapHelp": "f9"})
	if body := tr.Render(72); !strings.Contains(body, "f9") {
		t.Fatalf("remapped help must display the live chord: %s", body)
	}
	// The default f1 is part of the curated list; the list survives.
	tr = New(mapResolver{"palette.keymapHelp": "f1"})
	if body := tr.Render(72); !strings.Contains(body, "? · f1") {
		t.Fatalf("default help binding must keep the curated list: %s", body)
	}
}

func TestLinuxShowsCtrlChords(t *testing.T) {
	// #678: off macOS the curated cmd+… defaults must render platform-
	// normalized (Meta→Ctrl) — never as hardcoded mac strings.
	defer func(g string) { keymap.GOOS = g }(keymap.GOOS)
	keymap.GOOS = "linux"
	tr := New(nil)
	wants := []struct {
		page int
		want string
	}{
		{0, "shift shift · ctrl+shift+a"},
		{1, "ctrl+s · :w"},
		{2, "ctrl+1"},
		{2, "ctrl+k right"},
		{4, "ctrl+,"},
	}
	for _, w := range wants {
		tr.page = w.page
		body := tr.Render(72)
		if !strings.Contains(body, w.want) {
			t.Errorf("page %d missing normalized %q:\n%s", w.page+1, w.want, body)
		}
		if strings.Contains(body, "cmd+") {
			t.Errorf("page %d still renders a mac chord:\n%s", w.page+1, body)
		}
	}
}

func TestKnownDefaultIsNotARemap(t *testing.T) {
	// #665: the resolver picks ONE default per command, and for
	// search-everywhere that is the space-space leader mnemonic — which is
	// not in the curated display list. Known defaults must keep the curated
	// display; only a chord outside all known defaults is a real remap.
	tr := New(mapResolver{"palette.searchEverywhere": "space space"})
	if body := tr.Render(72); !strings.Contains(body, "shift shift · cmd+shift+a") {
		t.Fatalf("a known default must keep the curated display:\n%s", body)
	}
	tr = New(mapResolver{"palette.searchEverywhere": "ctrl+alt+s"})
	if body := tr.Render(72); !strings.Contains(body, "ctrl+alt+s") {
		t.Fatalf("a chord outside the known defaults is a remap and must display:\n%s", body)
	}
}
