package hltest

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"ike/internal/highlight"
	"ike/internal/httppane"
)

// pressT sends the raw-toggle key and pumps the returned command — the
// recompose's off-loop syntax pass (#2353) — back in, as the app's update
// loop would.
func pressT(m *httppane.Model) {
	if cmd := m.Update(tea.KeyPressMsg{Code: 't', Text: "t"}); cmd != nil {
		if msg, ok := cmd().(httppane.HighlightedMsg); ok {
			m.ApplyHighlight(msg)
		}
	}
}

// TestPrettyHighlightedAndFoldable guards the whole default reading path of
// #2157: a minified JSON answer is indented, highlighted and foldable at once
// — the three things that make a response readable without a keystroke.
func TestPrettyHighlightedAndFoldable(t *testing.T) {
	if !highlight.FencedSupported("json") {
		t.Skip("no JSON grammar in this build")
	}
	m := viewer(t, "application/json", `{"name":"x","nested":{"a":1,"b":2}}`)

	if m.Raw() {
		t.Fatal("the viewer must open pretty-printed")
	}
	if !strings.Contains(m.BodyText(), "\"name\": \"x\"") {
		t.Errorf("body not indented:\n%s", m.BodyText())
	}
	if !m.Highlighted() {
		t.Error("a pretty-printed JSON body should be highlighted")
	}
	folds := 0
	for i := 0; i < m.Rows(); i++ {
		if m.Foldable(i) {
			folds++
		}
	}
	if folds < 2 {
		t.Errorf("want the outer and the nested object foldable, got %d fold headers", folds)
	}
}

// TestRawToggleKeepsHighlighting: raw is about *formatting*, not about
// styling — the bytes as received still read as JSON, they just are not
// re-indented. And the folds follow the rows they belong to.
func TestRawToggleKeepsHighlighting(t *testing.T) {
	if !highlight.FencedSupported("json") {
		t.Skip("no JSON grammar in this build")
	}
	const wire = `{"name":"x","nested":{"a":1,"b":2}}`
	m := viewer(t, "application/json", wire)
	prettyRows := m.Rows()

	pressT(m)
	if !m.Raw() {
		t.Fatal("t did not switch to raw")
	}
	if m.BodyText() != wire {
		t.Errorf("raw body = %q, want the wire bytes", m.BodyText())
	}
	if !m.Highlighted() {
		t.Error("a raw JSON body should still be highlighted")
	}
	if m.Rows() >= prettyRows {
		t.Errorf("the one-line raw body should compose fewer rows: %d vs %d", m.Rows(), prettyRows)
	}

	pressT(m)
	if m.Raw() || m.Rows() != prettyRows {
		t.Errorf("t did not restore the pretty view: raw=%v rows=%d want %d", m.Raw(), m.Rows(), prettyRows)
	}
}

// TestFoldsSurviveTheRawToggle: folding a range, switching to raw and back
// leaves the viewer foldable again — the ranges are rebuilt from the rows
// that exist now, never carried over from rows that no longer do.
func TestFoldsSurviveTheRawToggle(t *testing.T) {
	if !highlight.FencedSupported("json") {
		t.Skip("no JSON grammar in this build")
	}
	m := viewer(t, "application/json", `{"name":"x","nested":{"a":1,"b":2}}`)
	nested := -1
	for i := 0; i < m.Rows(); i++ {
		if strings.Contains(m.RowText(i), `"nested"`) {
			nested = i
			break
		}
	}
	if nested < 0 {
		t.Fatal("no nested row")
	}
	if !m.ToggleFold(nested) {
		t.Fatal("the nested object should fold")
	}
	hidden := m.VisibleCount()

	pressT(m)
	pressT(m)

	if _, folded := m.FoldedAt(nested); folded {
		t.Error("a recompose must drop the collapse state, not resurrect it on new rows")
	}
	if m.VisibleCount() <= hidden {
		t.Errorf("the restored view should show every row again: %d vs %d", m.VisibleCount(), hidden)
	}
	if !m.Foldable(nested) {
		t.Error("the nested object should be foldable again")
	}
}
