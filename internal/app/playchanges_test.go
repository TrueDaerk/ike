package app

import (
	"fmt"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"ike/internal/vcs"
)

// playChangesDoc has two items that differ in one field, so switching the
// program between them changes exactly one result line.
const playChangesDoc = `{"items":[{"a":1,"b":2,"c":3},{"a":1,"b":5,"c":3}]}`

// renderedRow returns the stripped screen row holding needle, "" if none.
func renderedRow(m Model, needle string) string {
	for _, row := range strings.Split(ansi.Strip(m.render()), "\n") {
		if strings.Contains(row, needle) {
			return row
		}
	}
	return ""
}

// TestPlayChangesMarkDifferingLines is the issue's first acceptance case:
// `.items[0]` → `.items[1]` marks the one differing line, and only it.
func TestPlayChangesMarkDifferingLines(t *testing.T) {
	m := openJQ(t, dismissOnboarding(playApp(t, playChangesDoc)))
	if m.play.changes != nil || m.play.resultEd.HostChanges() != nil {
		t.Fatalf("the first run has nothing to compare against, got %v", m.play.changes)
	}
	m = setProgram(m, ".items[0]")
	m = setProgram(m, ".items[1]")
	want := map[int]vcs.LineMark{2: vcs.LineChanged}
	if got := m.play.resultEd.HostChanges(); !vcs.MarksEqual(got, want) {
		t.Fatalf("changes = %v, want %v", got, want)
	}
	if row := renderedRow(m, `"b": 5`); !strings.Contains(row, "▎") {
		t.Errorf("the changed line must carry the bar, got %q", row)
	}
	for _, same := range []string{`"a": 1`, `"c": 3`} {
		if row := renderedRow(m, same); strings.Contains(row, "▎") || strings.Contains(row, "▔") {
			t.Errorf("unchanged line %s must carry no marker, got %q", same, row)
		}
	}
}

// TestPlayChangesPureDeletion: a removed line leaves only the thin marker, on
// the line after the gap.
func TestPlayChangesPureDeletion(t *testing.T) {
	m := openJQ(t, dismissOnboarding(playApp(t, `[1,2,3]`)))
	m = setProgram(m, ".")
	m = setProgram(m, "map(select(. != 2))")
	want := map[int]vcs.LineMark{2: vcs.LineDeleted}
	if got := m.play.changes; !vcs.MarksEqual(got, want) {
		t.Fatalf("changes = %v, want %v", got, want)
	}
	row := renderedRow(m, "▔")
	if !strings.HasSuffix(strings.TrimRight(row, " │"), "3") || strings.Contains(row, "▎") {
		t.Errorf("the line after the gap must carry ▔ and no bar, got %q", row)
	}
}

// TestPlayChangesKeepValueGlyphs: on a value's first line the type glyph
// keeps the sign cell (#2789) — the change recolours it rather than hiding
// the value boundary.
func TestPlayChangesKeepValueGlyphs(t *testing.T) {
	m := openJQ(t, dismissOnboarding(playApp(t, `[1,2,3]`)))
	m = setProgram(m, ".[]")
	m = setProgram(m, ".[] | . * 10")
	if got := len(m.play.changes); got != 3 {
		t.Fatalf("every value changed, got %v", m.play.changes)
	}
	if row := renderedRow(m, "2 20"); !strings.Contains(row, "#") || strings.Contains(row, "▎") {
		t.Errorf("the value glyph must keep its cell, got %q", row)
	}
}

// TestPlayChangesClearedResultHasNoMarks: ctrl+l drops the marks, and the run
// after it has nothing to compare against.
func TestPlayChangesClearedResultHasNoMarks(t *testing.T) {
	m := openJQ(t, dismissOnboarding(playApp(t, playChangesDoc)))
	m = setProgram(m, ".items[0]")
	m = setProgram(m, ".items[1]")
	if m.play.changes == nil {
		t.Fatal("setup: the second run must carry marks")
	}
	m = intoResult(m)
	m = drainKey(m, ctrlL)
	if m.play.changes != nil || m.play.resultEd.HostChanges() != nil {
		t.Fatalf("ctrl+l must clear the marks, got %v", m.play.resultEd.HostChanges())
	}
	m = setProgram(m, ".items[0]")
	if m.play.changes != nil || m.play.resultEd.HostChanges() != nil {
		t.Errorf("the first run after a clear must carry no marks, got %v", m.play.changes)
	}
}

// TestPlayChangesSurviveFocusAndDropOnNextResult: the marks hold across focus
// switches and a failed run, and the next good result replaces them.
func TestPlayChangesSurviveFocusAndDropOnNextResult(t *testing.T) {
	m := openJQ(t, dismissOnboarding(playApp(t, playChangesDoc)))
	m = setProgram(m, ".items[0]")
	m = setProgram(m, ".items[1]")
	want := m.play.changes
	m = drainKey(m, tea.KeyPressMsg{Code: tea.KeyTab}) // into the result
	m = playKeys(m, "jj")
	m = drainKey(m, tea.KeyPressMsg{Code: tea.KeyTab}) // back to the query
	if got := m.play.resultEd.HostChanges(); !vcs.MarksEqual(got, want) {
		t.Fatalf("focus switches must keep the marks, got %v want %v", got, want)
	}
	m = setProgram(m, ".items[1] | error")
	if m.play.runErr == "" {
		t.Fatal("setup: the program must fail")
	}
	if got := m.play.resultEd.HostChanges(); !vcs.MarksEqual(got, want) {
		t.Errorf("a failed run keeps the result and its marks, got %v", got)
	}
	m = setProgram(m, ".items[1]")
	if got := m.play.resultEd.HostChanges(); got != nil {
		t.Errorf("an identical next result must drop the marks, got %v", got)
	}
}

// TestPlayChangesYQ: the yq dialect marks the same way.
func TestPlayChangesYQ(t *testing.T) {
	m := openYQ(t, dismissOnboarding(yqApp(t, "items:\n  - a: 1\n    b: 2\n  - a: 1\n    b: 5\n")))
	m = setProgram(m, ".items[0]")
	m = setProgram(m, ".items[1]")
	want := map[int]vcs.LineMark{1: vcs.LineChanged}
	if got := m.play.resultEd.HostChanges(); !vcs.MarksEqual(got, want) {
		t.Fatalf("changes = %v, want %v", got, want)
	}
}

// playDiffLines is n lines "line 0" … "line n-1".
func playDiffLines(n int) string {
	var b strings.Builder
	for i := range n {
		if i > 0 {
			b.WriteByte('\n')
		}
		fmt.Fprintf(&b, "line %d", i)
	}
	return b.String()
}

// TestPlayChangeMarksBudget: the diff runs up to playDiffMaxLines per side
// and is skipped past it.
func TestPlayChangeMarksBudget(t *testing.T) {
	if overLineBudget(playDiffLines(playDiffMaxLines)) {
		t.Error("exactly the budget must still diff")
	}
	if !overLineBudget(playDiffLines(playDiffMaxLines + 1)) {
		t.Error("one line over the budget must skip")
	}
	at := playDiffLines(playDiffMaxLines)
	if got := playChangeMarks(at, strings.Replace(at, "line 7\n", "line seven\n", 1)); len(got) != 1 || got[7] != vcs.LineChanged {
		t.Errorf("within the budget the change must be marked, got %v", got)
	}
	over := playDiffLines(playDiffMaxLines + 1)
	if got := playChangeMarks(at, over); got != nil {
		t.Errorf("a side over the budget must skip the diff, got %d marks", len(got))
	}
	if got := playChangeMarks(over, at); got != nil {
		t.Errorf("a previous side over the budget must skip the diff, got %d marks", len(got))
	}
	if got := playChangeMarks(at, at); got != nil {
		t.Errorf("identical results carry no marks, got %v", got)
	}
}

// BenchmarkPlayChangeMarks is the worst case the budget admits: two
// 5,000-line results with a change every 50 lines.
func BenchmarkPlayChangeMarks(b *testing.B) {
	prev := playDiffLines(playDiffMaxLines)
	lines := strings.Split(prev, "\n")
	for i := 0; i < len(lines); i += 50 {
		lines[i] += " changed"
	}
	next := strings.Join(lines, "\n")
	for b.Loop() {
		playChangeMarks(prev, next)
	}
}

// BenchmarkPlayChangeMarksOverBudget is the skip path for a large result.
func BenchmarkPlayChangeMarksOverBudget(b *testing.B) {
	prev := playDiffLines(200_000)
	next := prev + "\nmore"
	for b.Loop() {
		playChangeMarks(prev, next)
	}
}
