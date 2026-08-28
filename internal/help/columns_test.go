package help

import (
	"fmt"
	"strings"
	"testing"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"ike/internal/plugin"
	"ike/internal/registry"
)

func TestTypicalColumnWidth(t *testing.T) {
	// One outlier among short cells: the 90% width ignores it, the max does not.
	cells := make([]string, 0, 10)
	for i := 0; i < 9; i++ {
		cells = append(cells, strings.Repeat("a", 25))
	}
	cells = append(cells, strings.Repeat("b", 90))

	if got := TypicalColumnWidth(cells, 0, 90); got != 25 {
		t.Errorf("TypicalColumnWidth(90%%) = %d, want 25 (the outlier ignored)", got)
	}
	if got := MinColumnWidth(cells, 0); got != 90 {
		t.Errorf("MinColumnWidth = %d, want 90 (the outlier counted)", got)
	}
	// Full coverage is MinColumnWidth again, and the configured floor applies.
	if got := TypicalColumnWidth(cells, 0, 100); got != 90 {
		t.Errorf("TypicalColumnWidth(100%%) = %d, want 90", got)
	}
	if got := TypicalColumnWidth([]string{"x"}, 40, 90); got != 40 {
		t.Errorf("configured floor = %d, want 40", got)
	}
	if got := TypicalColumnWidth(nil, 0, 90); got != defaultMinColWidth {
		t.Errorf("empty cells = %d, want the default floor", got)
	}
	// Out-of-range percentages clamp instead of panicking.
	if got := TypicalColumnWidth(cells, 0, -5); got != 25 {
		t.Errorf("pct below 1 = %d, want the smallest cell width 25", got)
	}
}

func TestColumnLayout(t *testing.T) {
	cases := []struct {
		name                       string
		width, natural, floor, max int
		wantCols, wantW            int
	}{
		// The budget affords two natural columns: no need to shrink them.
		{"wide", 200, 60, 40, 2, 2, 60},
		// The natural width alone would collapse the sheet (#2215) — the columns
		// shrink to their fair share instead, as long as they stay above the floor.
		{"shrink to fit", 120, 77, 45, 2, 2, 59},
		// Below the floor a second column is not worth having.
		{"too narrow", 80, 77, 45, 2, 1, 77},
		// A single column never exceeds the budget.
		{"clamped to budget", 40, 77, 45, 2, 1, 40},
		// maxCols governs the cap; a nonsensical one degrades to one column.
		{"three columns", 200, 60, 40, 3, 3, 60},
		{"max below one", 200, 60, 40, 0, 1, 60},
	}
	for _, c := range cases {
		cols, w := ColumnLayout(c.width, c.natural, c.floor, c.max)
		if cols != c.wantCols || w != c.wantW {
			t.Errorf("%s: ColumnLayout(%d,%d,%d,%d) = (%d,%d), want (%d,%d)",
				c.name, c.width, c.natural, c.floor, c.max, cols, w, c.wantCols, c.wantW)
		}
	}
}

// TestRenderEntryTruncatesOverlongTitle (#2215): a title too long for its
// column is ellipsised so the row stays one line and keeps its shortcut, while
// a column too narrow for a meaningful title overflows instead.
func TestRenderEntryTruncatesOverlongTitle(t *testing.T) {
	h := New(registry.New(), nil, 0)
	got := ansi.Strip(h.renderEntry(Entry{Title: "Toggle the verbose diagnostics panel", Shortcut: "ctrl+d"}, 30))
	if lipgloss.Width(got) != 30 {
		t.Fatalf("entry = %q (width %d), want exactly the column width", got, lipgloss.Width(got))
	}
	if !strings.Contains(got, "…") || !strings.HasSuffix(got, "ctrl+d") {
		t.Fatalf("entry = %q, want an ellipsised title keeping its shortcut", got)
	}
	// Unbound entries may use the whole column.
	unbound := ansi.Strip(h.renderEntry(Entry{Title: strings.Repeat("x", 40)}, 20))
	if lipgloss.Width(unbound) != 20 {
		t.Fatalf("unbound entry width = %d, want 20", lipgloss.Width(unbound))
	}
	// Too little room for a readable title: the row overflows rather than
	// showing an ellipsis and nothing else.
	tight := ansi.Strip(h.renderEntry(Entry{Title: "Toggle diagnostics", Shortcut: "ctrl+d"}, 12))
	if tight != "Toggle diagnostics  ctrl+d" {
		t.Fatalf("tight entry = %q, want the untruncated overflow", tight)
	}
}

// verboseRegistry mimics the real app as the context view (#2182) sees it:
// several pane contexts, each with a handful of commands, plus one unusually
// verbose global command title.
func verboseRegistry() *registry.Registry {
	r := registry.New()
	var cmds []plugin.Command
	for _, ctx := range []string{"editor", "explorer", "terminal", "vcs", "debug", "problems"} {
		for i := 0; i < 6; i++ {
			cmds = append(cmds, plugin.Command{
				ID:       fmt.Sprintf("%s.c%d", ctx, i),
				Title:    fmt.Sprintf("%s command number %d", ctx, i),
				Scope:    plugin.PaneScope(ctx),
				Shortcut: "ctrl+alt+shift+x",
			})
		}
	}
	for i := 0; i < 10; i++ {
		cmds = append(cmds, plugin.Command{
			ID:       fmt.Sprintf("global.c%d", i),
			Title:    fmt.Sprintf("Global thing %d", i),
			Scope:    plugin.GlobalScope(),
			Shortcut: "ctrl+g",
		})
	}
	cmds = append(cmds, plugin.Command{
		ID:       "global.verbose",
		Title:    "Toggle the extremely verbose diagnostics side panel",
		Scope:    plugin.GlobalScope(),
		Shortcut: "ctrl+alt+shift+d",
	})
	r.Add(stubPlugin{id: "p", cmd: cmds})
	return r
}

// sectionRows counts the entry rows rendered for the section headed by heading,
// i.e. the lines up to the blank line that separates it from the next section.
func sectionRows(body, heading string) int {
	lines := strings.Split(body, "\n")
	for i, l := range lines {
		if strings.TrimSpace(l) != heading {
			continue
		}
		n := 0
		for _, row := range lines[i+1:] {
			if strings.TrimSpace(row) == "" {
				break
			}
			n++
		}
		return n
	}
	return -1
}

// TestSectionsRenderMultiColumnWhenWide (#2215): the sectioned context view
// packs each section into columns just like the flat sheet — a single verbose
// command title must not collapse the whole sheet into one endless column.
func TestSectionsRenderMultiColumnWhenWide(t *testing.T) {
	h := New(verboseRegistry(), nil, 0)
	h.Snapshot("editor")

	wide := ansi.Strip(h.Render(120))
	// Six editor commands in two columns are three rows, not six.
	if got := sectionRows(wide, "Editor — focused pane"); got != 3 {
		t.Fatalf("focused section rows = %d, want 3 (two columns):\n%s", got, wide)
	}
	// The global section (11 entries) likewise halves to six rows.
	if got := sectionRows(wide, "Global"); got != 6 {
		t.Fatalf("global section rows = %d, want 6 (two columns):\n%s", got, wide)
	}
	// The body uses the width it was given instead of one narrow column.
	if w := lipgloss.Width(wide); w < 100 || w > 120 {
		t.Fatalf("body width = %d, want it to fill most of the 120-cell budget", w)
	}
	// The flat sheet gets the same treatment.
	h.HandleKey("tab")
	if h.view != viewFlat {
		t.Fatalf("expected the flat view after tab, got %v", h.view)
	}
	flat := ansi.Strip(h.Render(120))
	if got := sectionRows(flat, "Global"); got != 6 {
		t.Fatalf("flat global section rows = %d, want 6 (two columns):\n%s", got, flat)
	}
}

// TestSectionsCollapseToOneColumnWhenNarrow (#2215): a budget too tight for two
// readable columns still renders every section, single column, within the
// budget.
func TestSectionsCollapseToOneColumnWhenNarrow(t *testing.T) {
	h := New(verboseRegistry(), nil, 0)
	h.Snapshot("editor")

	narrow := ansi.Strip(h.Render(50))
	if got := sectionRows(narrow, "Editor — focused pane"); got != 6 {
		t.Fatalf("focused section rows = %d, want 6 (single column):\n%s", got, narrow)
	}
	for _, line := range strings.Split(narrow, "\n") {
		if strings.Contains(line, "press tab") {
			continue // the footer legend is a free-running line
		}
		if w := lipgloss.Width(strings.TrimRight(line, " ")); w > 50 {
			t.Fatalf("line width %d exceeds the 50-cell budget: %q", w, line)
		}
	}
	// Every section is still there, focused one first.
	for _, want := range []string{"Editor — focused pane", "Global", "Explorer", "Terminal"} {
		if !strings.Contains(narrow, want) {
			t.Fatalf("narrow render dropped the %q section:\n%s", want, narrow)
		}
	}
}
