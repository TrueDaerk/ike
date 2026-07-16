package pane

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"ike/internal/editor"
)

// TestInstanceViewCacheNeverStale is the safety net for the pane-level View cache
// (#615): after every render-affecting mutation, the cached Instance.View must
// equal a freshly recomputed one. A failure means RenderVersion is incomplete —
// i.e. the cache could serve a stale frame.
func TestInstanceViewCacheNeverStale(t *testing.T) {
	setup := func() (*Instance, *editor.Model) {
		i := newInstance("editor", KindEditor, nil, nil)
		ed := i.tabs[0].Editor()
		ed.RestoreText(strings.Repeat("some line of code here = value(x, y)\n", 40))
		ed.SetSize(60, 20)
		ed.SetFocused(true)
		return i, ed
	}

	bpLines := []int{}
	mutations := []struct {
		name string
		do   func(i *Instance, ed *editor.Model)
	}{
		{"vertical-scroll", func(i *Instance, ed *editor.Model) { ed.ScrollBy(6) }},
		{"vertical-scroll-back", func(i *Instance, ed *editor.Model) { ed.ScrollBy(-4) }},
		{"horizontal-scroll", func(i *Instance, ed *editor.Model) { ed.ScrollXBy(5) }},
		{"cursor", func(i *Instance, ed *editor.Model) { ed.SetCursor(7, 3) }},
		{"resize-width", func(i *Instance, ed *editor.Model) { ed.SetSize(48, 20) }},
		{"resize-height", func(i *Instance, ed *editor.Model) { ed.SetSize(60, 12) }},
		{"blur", func(i *Instance, ed *editor.Model) { ed.SetFocused(false) }},
		{"edit", func(i *Instance, ed *editor.Model) { i.Update(tea.KeyPressMsg{Text: "x", Code: 'x'}) }},
		{"paused", func(i *Instance, ed *editor.Model) { ed.SetPausedLine(5) }},
		{"blame", func(i *Instance, ed *editor.Model) { ed.ToggleBlame() }},
		{"breakpoint-change", func(i *Instance, ed *editor.Model) {
			bpLines = append(bpLines, 4) // the external store gained a breakpoint
		}},
	}

	for _, mut := range mutations {
		t.Run(mut.name, func(t *testing.T) {
			i, ed := setup()
			bpLines = []int{2}
			ed.SetBreakpointSource(func(string) []int { return bpLines })
			_ = i.View() // warm the pane view cache

			mut.do(i, ed)

			a := i.View() // cached path
			i.cvValid = false
			b := i.View() // forced fresh
			if a != b {
				t.Fatalf("%s: cached Instance.View differs from fresh — RenderVersion is incomplete\n--- cached ---\n%s\n--- fresh ---\n%s", mut.name, a, b)
			}
		})
	}
}

// TestInstanceViewCacheHits verifies the cache actually short-circuits when
// nothing changed (an untouched pane during another pane's scroll).
func TestInstanceViewCacheHits(t *testing.T) {
	i := newInstance("editor", KindEditor, nil, nil)
	ed := i.tabs[0].Editor()
	ed.RestoreText("alpha\nbeta\ngamma\n")
	ed.SetSize(40, 10)
	first := i.View()
	if !i.cvValid {
		t.Fatal("view cache not populated")
	}
	ver := i.cvVer
	// A second render with no mutation must reuse the cache (same version).
	second := i.View()
	if second != first || i.cvVer != ver {
		t.Fatal("unchanged pane recomputed its View instead of reusing the cache")
	}
}
