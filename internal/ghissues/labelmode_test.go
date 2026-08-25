package ghissues

// labelmode_test.go covers the label filter's any-of/all-of switch (#2112).

import (
	"strings"
	"testing"
	"time"

	"ike/internal/forge"
)

// mixedLabels is a listing where one issue carries both labels, so OR and AND
// give different match sets.
func mixedLabels(t *testing.T) *Model {
	t.Helper()
	m := New(nil)
	m.SetSize(90, 14)
	m.now = func() time.Time { return fixedNow }
	m.SetResult(forge.IssuesMsg{
		State: forge.IssuesOpen,
		Issues: []forge.Issue{
			{Number: 1, Title: "both", URL: "https://e/1", State: "OPEN", CreatedAt: fixedNow,
				Labels: []forge.Label{{Name: "bug"}, {Name: "feature"}}},
			{Number: 2, Title: "feature only", URL: "https://e/2", State: "OPEN", CreatedAt: fixedNow,
				Labels: []forge.Label{{Name: "feature"}}},
			{Number: 3, Title: "bug only", URL: "https://e/3", State: "OPEN", CreatedAt: fixedNow,
				Labels: []forge.Label{{Name: "bug"}}},
		},
	})
	return &m
}

// TestLabelFilterSemantics (#2112): the same selection matches the union in
// any-of mode and only the intersection in all-of mode.
func TestLabelFilterSemantics(t *testing.T) {
	m := mixedLabels(t)
	m.labelSel = map[string]bool{"bug": true, "feature": true}
	m.applyFilter()
	if m.Visible() != 3 {
		t.Fatalf("any-of must match every issue carrying one of the labels, got %d", m.Visible())
	}
	m.toggleLabelMode()
	if !m.LabelMatchAll() {
		t.Fatal("the toggle must switch to all-of")
	}
	if m.Visible() != 1 {
		t.Fatalf("all-of must narrow to the intersection, got %d", m.Visible())
	}
	if is := m.Selected(); is == nil || is.Number != 1 {
		t.Fatalf("all-of must keep the issue carrying both labels, got %v", is)
	}
	m.toggleLabelMode()
	if m.LabelMatchAll() || m.Visible() != 3 {
		t.Fatalf("toggling back must restore any-of (all=%v visible=%d)", m.LabelMatchAll(), m.Visible())
	}
}

// TestHasAllLabels covers the predicate on its own, including the empty
// selection every caller gates on.
func TestHasAllLabels(t *testing.T) {
	is := &forge.Issue{Labels: []forge.Label{{Name: "bug"}, {Name: "feature"}}}
	if !hasAllLabels(is, []string{"bug", "feature"}) {
		t.Fatal("every selected label present must match")
	}
	if hasAllLabels(is, []string{"bug", "docs"}) {
		t.Fatal("a missing label must not match")
	}
	if !hasAllLabels(is, nil) {
		t.Fatal("an empty selection must match vacuously")
	}
	if !hasAnyLabel(is, []string{"docs", "bug"}) {
		t.Fatal("any-of must still match on one label")
	}
}

// TestLabelModeVisibleInOverlayAndChips (#2112): the switch reads in the
// overlay's mode row, the footer's wording and the chip row.
func TestLabelModeVisibleInOverlayAndChips(t *testing.T) {
	m := mixedLabels(t)
	m.labelSel = map[string]bool{"bug": true, "feature": true}
	m.keepSelection()
	press(m, "f", "down", "down", "down", "down") // the label mode row
	if v := m.View(); !strings.Contains(v, "● any of") || !strings.Contains(v, "labels match any selected") {
		t.Fatalf("the overlay must show the any-of mode:\n%s", v)
	}
	press(m, "space")
	v := m.View()
	if !strings.Contains(v, "● all of") || !strings.Contains(v, "labels match all selected") {
		t.Fatalf("the overlay must show the all-of mode:\n%s", v)
	}
	press(m, "enter")
	if v := m.View(); !strings.Contains(v, "labels: bug+feature (all)") {
		t.Fatalf("the chip row must spell out the all-of selection:\n%s", v)
	}
	// Clearing the mode chip widens back to any-of without dropping labels.
	for i, c := range m.filterChips() {
		if strings.HasPrefix(c.text, "labels: ") {
			m.clearChip(m.chipSpans()[i][0])
		}
	}
	if m.LabelMatchAll() || len(m.LabelFilter()) != 2 {
		t.Fatalf("clearing the mode chip must keep the labels (all=%v labels=%v)",
			m.LabelMatchAll(), m.LabelFilter())
	}
}

// TestLabelModeRevertsWithEsc (#2112): esc inside the overlay restores the
// mode along with every other dimension.
func TestLabelModeRevertsWithEsc(t *testing.T) {
	m := mixedLabels(t)
	m.labelSel = map[string]bool{"bug": true}
	press(m, "f")
	m.toggleLabelMode()
	press(m, "esc")
	if m.LabelMatchAll() {
		t.Fatal("esc must revert the label mode")
	}
}
