package diff

import (
	"strings"
	"testing"

	"charm.land/lipgloss/v2"

	"ike/internal/theme"
)

// whitespace_test.go covers the ignore-whitespace mode (#2170): what drops out
// of the diff, what survives it, and that the emphasis lands on the changed
// content rather than on the re-indentation around it.

func TestIgnoreWhitespaceDropsWhitespaceOnlyHunks(t *testing.T) {
	left := "func f() {\n\treturn 1\n}\n"
	right := "func f() {\n        return 1\n}\n"
	if res := Compute(left, right); len(res.Hunks) != 1 {
		t.Fatalf("whitespace-significant diff should see the re-indentation: %d hunks", len(res.Hunks))
	}
	res := ComputeWith(left, right, Options{IgnoreWhitespace: true})
	if len(res.Hunks) != 0 {
		t.Fatalf("ignoring whitespace should leave no hunks, got %d: %+v", len(res.Hunks), res.Rows)
	}
	for _, r := range res.Rows {
		if r.Kind != RowSame {
			t.Fatalf("row %+v should be unchanged", r)
		}
	}
	// Both columns keep their own raw text — the diff is comparison-only.
	if got := res.Rows[1]; got.Left != "\treturn 1" || got.Right != "        return 1" {
		t.Fatalf("unchanged row lost a side's raw text: %+v", got)
	}
}

func TestIgnoreWhitespaceKeepsRealChanges(t *testing.T) {
	left := "a = 1\n  b   =   2\nc = 3\n"
	right := "a = 1\n\tb = 9\nc = 3\n"
	res := ComputeWith(left, right, Options{IgnoreWhitespace: true})
	if len(res.Hunks) != 1 {
		t.Fatalf("the changed value must stay a hunk, got %d", len(res.Hunks))
	}
	row := res.Rows[1]
	if row.Kind != RowChanged {
		t.Fatalf("row 1 = %v, want RowChanged", row.Kind)
	}
	// The emphasis covers the changed digit only, not the collapsed spacing.
	if got := spanText(row.Left, row.LeftSpans); got != "2" {
		t.Errorf("left spans cover %q, want %q", got, "2")
	}
	if got := spanText(row.Right, row.RightSpans); got != "9" {
		t.Errorf("right spans cover %q, want %q", got, "9")
	}
}

func TestIgnoreWhitespaceSpansSkipIndentation(t *testing.T) {
	// Re-indented *and* renamed: whitespace-significant refinement flags the
	// leading run too, ignoring whitespace it marks the identifier alone.
	left := "  value := 1"
	right := "\t\tresult := 1"
	plain := ComputeWith(left, right, Options{})
	if got := spanText(plain.Rows[0].Left, plain.Rows[0].LeftSpans); !strings.HasPrefix(got, " ") {
		t.Fatalf("whitespace-significant left span %q should start in the indentation", got)
	}
	res := ComputeWith(left, right, Options{IgnoreWhitespace: true})
	row := res.Rows[0]
	if row.Kind != RowChanged {
		t.Fatalf("row = %v, want RowChanged", row.Kind)
	}
	for _, s := range row.LeftSpans {
		runes := []rune(row.Left)
		if strings.TrimSpace(string(runes[s.Start:s.End])) == "" {
			t.Errorf("left span %v is whitespace-only", s)
		}
	}
	// The rune diff keeps whatever the two identifiers share ("e"), so the
	// spans cover the differing parts — never the indentation before them.
	if got := spanText(row.Left, row.LeftSpans); got != "valu" {
		t.Errorf("left spans cover %q, want %q", got, "valu")
	}
	if got := spanText(row.Right, row.RightSpans); got != "rsult" {
		t.Errorf("right spans cover %q, want %q", got, "rsult")
	}
	if row.LeftSpans[0].Start < 2 {
		t.Errorf("left span starts at %d, inside the indentation", row.LeftSpans[0].Start)
	}
}

func TestIgnoreWhitespaceToggleIsLive(t *testing.T) {
	m := testModel(t, "a\n\tb\nc\n", "a\n    b\nc\n")
	if m.HunkCount() != 1 {
		t.Fatalf("expected the re-indentation as one hunk, got %d", m.HunkCount())
	}
	cmd := m.Update(key("w"))
	if !m.IgnoreWhitespace() {
		t.Fatal("w should switch ignore-whitespace on")
	}
	if m.HunkCount() != 0 {
		t.Fatalf("the whitespace-only hunk should be gone, got %d", m.HunkCount())
	}
	if cmd == nil {
		t.Fatal("the toggle must report itself for persistence")
	}
	msg, ok := cmd().(IgnoreWhitespaceMsg)
	if !ok || msg.Key != "diff" || !msg.On {
		t.Fatalf("cmd produced %#v, want IgnoreWhitespaceMsg{Key: diff, On: true}", msg)
	}
	// And back: the hunk returns without new contents.
	m.Update(key("w"))
	if m.IgnoreWhitespace() || m.HunkCount() != 1 {
		t.Fatalf("toggling back should restore the hunk: on=%v hunks=%d", m.IgnoreWhitespace(), m.HunkCount())
	}
}

func TestIgnoreWhitespaceSurvivesRediff(t *testing.T) {
	m := testModel(t, "a\n\tb\n", "a\n\tb\n")
	m.SetIgnoreWhitespace(true)
	m.Rediff("a\n        b\n")
	if m.HunkCount() != 0 {
		t.Fatalf("a re-diff must keep ignoring whitespace, got %d hunks", m.HunkCount())
	}
	m.SetContents("a\n\tb\n", "a\n        b\n")
	if m.HunkCount() != 0 {
		t.Fatalf("new contents must keep ignoring whitespace, got %d hunks", m.HunkCount())
	}
}

// TestEmphasisPerSide: the intra-line emphasis of a changed pair uses the
// added/removed emphasis slots, so a changed range never leaves the colour of
// the line it sits in — in both layouts.
func TestEmphasisPerSide(t *testing.T) {
	pal := theme.DefaultPalette()
	m := NewFiles("diff", "/tmp/left.txt", "/tmp/right.txt", pal)
	m.SetSize(40, 10)
	m.SetContents("value = 1\n", "value = 2\n")
	emph := func(bg lipgloss.Style, s string) string { return bg.Bold(true).Render(s) }
	wantL := emph(lipgloss.NewStyle().Background(pal.DiffRemovedEmph), "1")
	wantR := emph(lipgloss.NewStyle().Background(pal.DiffAddedEmph), "2")
	for _, unified := range []bool{false, true} {
		m.SetUnified(unified)
		v := m.View()
		if !strings.Contains(v, wantL) {
			t.Errorf("unified=%v: removed side should use the removed emphasis:\n%q", unified, v)
		}
		if !strings.Contains(v, wantR) {
			t.Errorf("unified=%v: added side should use the added emphasis:\n%q", unified, v)
		}
	}
}

// spanText concatenates the rune ranges spans cover in line.
func spanText(line string, spans []Span) string {
	runes := []rune(line)
	var b strings.Builder
	for _, s := range spans {
		b.WriteString(string(runes[clamp(s.Start, 0, len(runes)):clamp(s.End, 0, len(runes))]))
	}
	return b.String()
}
