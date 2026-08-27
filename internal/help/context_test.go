package help

import (
	"strings"
	"testing"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"ike/internal/version"
)

// labels lists the group labels of a snapshot in order.
func labels(groups []Group) []string {
	out := make([]string, 0, len(groups))
	for _, g := range groups {
		out = append(out, g.Label)
	}
	return out
}

// TestContextSnapshotLeadsWithFocusedContext (#2182): the focused context's
// own group comes first — flagged as focused — then global, then the remaining
// contexts in the usual alphabetical order. Nothing is dropped.
func TestContextSnapshotLeadsWithFocusedContext(t *testing.T) {
	groups := ContextSnapshot(testRegistry(), nil, "explorer")
	if got, want := strings.Join(labels(groups), ","), "explorer,global,editor"; got != want {
		t.Fatalf("context groups = %q, want %q", got, want)
	}
	if !groups[0].Focused {
		t.Fatal("the leading group must be flagged as the focused context")
	}
	for _, g := range groups[1:] {
		if g.Focused {
			t.Fatalf("only the focused context may carry the flag, %q does too", g.Label)
		}
	}

	// Focus the editor instead and the order follows the focus.
	if got, want := strings.Join(labels(ContextSnapshot(testRegistry(), nil, "editor")), ","), "editor,global,explorer"; got != want {
		t.Fatalf("editor-focused groups = %q, want %q", got, want)
	}
}

// TestContextSnapshotDegradesWithoutFocus verifies the plain ordering is kept
// when there is no focused context, or when the focused one owns no commands.
func TestContextSnapshotDegradesWithoutFocus(t *testing.T) {
	for _, ctx := range []string{"", "global", "terminal"} {
		groups := ContextSnapshot(testRegistry(), nil, ctx)
		if got, want := strings.Join(labels(groups), ","), "global,editor,explorer"; got != want {
			t.Fatalf("context %q groups = %q, want %q", ctx, got, want)
		}
		for _, g := range groups {
			if g.Focused {
				t.Fatalf("context %q must not flag a focused group", ctx)
			}
		}
	}
}

// TestHelpOpensOnContextView (#2182): opening help with a pane focused shows
// that pane's bindings first, under a heading naming the context, followed by
// the global ones.
func TestHelpOpensOnContextView(t *testing.T) {
	h := New(testRegistry(), nil, 0)
	h.Snapshot("explorer")

	if h.view != viewContext {
		t.Fatalf("view = %v, want the context view", h.view)
	}
	if title := h.Title(); !strings.HasPrefix(title, "HELP "+version.Short()) || !strings.HasSuffix(title, "— Explorer context") {
		t.Fatalf("context view title = %q", title)
	}
	body := ansi.Strip(h.Render(80))
	if !strings.Contains(body, "Explorer — focused pane") {
		t.Fatalf("focused section heading missing:\n%s", body)
	}
	explorer := strings.Index(body, "Explorer — focused pane")
	global := strings.Index(body, "Global")
	editor := strings.Index(body, "Editor")
	if explorer < 0 || global < 0 || editor < 0 {
		t.Fatalf("expected all three sections:\n%s", body)
	}
	if !(explorer < global && global < editor) {
		t.Fatalf("want explorer, then global, then the other contexts:\n%s", body)
	}
	if !strings.Contains(body, "press tab for the full list") {
		t.Fatalf("context footer must advertise the toggle:\n%s", body)
	}

	// The same overlay focused on the editor leads with the editor instead.
	h.Snapshot("editor")
	body = ansi.Strip(h.Render(80))
	if !strings.Contains(body, "Editor — focused pane") {
		t.Fatalf("editor focus should lead with the editor section:\n%s", body)
	}
	if strings.Index(body, "Editor — focused pane") > strings.Index(body, "Global") {
		t.Fatalf("editor section must precede global:\n%s", body)
	}
}

// TestContextViewTogglesToFullList (#2182): tab cycles context -> full flat
// list -> essentials -> context, and the flat list keeps the classic ordering
// (global first, focused context after) without a focused-pane heading.
func TestContextViewTogglesToFullList(t *testing.T) {
	h := New(essentialsRegistry(), nil, 0)
	h.Snapshot("editor")
	if h.view != viewContext {
		t.Fatalf("expected the context view on open, got %v", h.view)
	}

	if !h.HandleKey("tab") {
		t.Fatal("tab must be consumed")
	}
	if h.view != viewFlat {
		t.Fatalf("first tab = %v, want the flat list", h.view)
	}
	body := ansi.Strip(h.Render(80))
	if !strings.HasSuffix(h.Title(), "— commands & shortcuts") {
		t.Fatalf("flat view title = %q", h.Title())
	}
	if strings.Contains(body, "— focused pane") {
		t.Fatalf("the flat list keeps the classic headings:\n%s", body)
	}
	if strings.Index(body, "Global") > strings.Index(body, "Editor") {
		t.Fatalf("flat list is global-first:\n%s", body)
	}

	h.HandleKey("tab")
	if h.view != viewEssentials {
		t.Fatalf("second tab = %v, want essentials", h.view)
	}
	h.HandleKey("tab")
	if h.view != viewContext {
		t.Fatalf("third tab = %v, want back to the context view", h.view)
	}
}

// TestContextViewSkippedWithoutFocus verifies the cycle degrades to the
// pre-#2182 two-view toggle when no context is focused.
func TestContextViewSkippedWithoutFocus(t *testing.T) {
	h := New(essentialsRegistry(), nil, 0)
	h.Snapshot("")
	if h.view != viewEssentials {
		t.Fatalf("no focus should open on essentials, got %v", h.view)
	}
	h.HandleKey("tab")
	if h.view != viewFlat {
		t.Fatalf("tab without a focused context = %v, want the flat list", h.view)
	}
	h.HandleKey("tab")
	if h.view != viewEssentials {
		t.Fatalf("tab should return to essentials, got %v", h.view)
	}
}

// TestContextViewLayoutStaysResponsive (#2182): the two-column cap and the
// column packing apply to the context view exactly as to the flat one.
func TestContextViewLayoutStaysResponsive(t *testing.T) {
	h := New(testRegistry(), nil, 0)
	h.Snapshot("explorer")
	colW := MinColumnWidth(h.allCells(h.ctxGroups), 0) + colSlack
	wide := ansi.Strip(h.Render(400))
	// The footer legend is a free-running line; the packed sections are what
	// the two-column cap governs.
	for _, line := range strings.Split(wide, "\n") {
		if strings.Contains(line, "press tab") {
			continue
		}
		if w := lipgloss.Width(strings.TrimRight(line, " ")); w > 2*colW+gutter {
			t.Fatalf("context view line width %d exceeds the two-column bound %d: %q", w, 2*colW+gutter, line)
		}
	}
	narrow := ansi.Strip(h.Render(20))
	if !strings.Contains(narrow, "New File") {
		t.Fatalf("narrow render should still list the focused context's commands:\n%s", narrow)
	}
}

// TestFilterSearchesEveryContextFromContextView (#2182): a filter typed in the
// context view searches all scopes, including contexts other than the focused
// one.
func TestFilterSearchesEveryContextFromContextView(t *testing.T) {
	h := New(testRegistry(), nil, 0)
	h.Snapshot("explorer")
	h.SetFilter("save") // an editor command while the explorer is focused
	if body := ansi.Strip(h.Render(80)); !strings.Contains(body, "Save") {
		t.Fatalf("filter must reach other contexts:\n%s", body)
	}
}
