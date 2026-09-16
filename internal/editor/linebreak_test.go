package editor

// linebreak_test.go covers alt+enter in the two find/replace entry points
// (#2600): the chord itself, what the row renders, the panel's hand-off to the
// ex ":s" line, and the multi-line find / replace semantics behind them.

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"ike/internal/ui"
)

func altEnter() tea.KeyPressMsg { return tea.KeyPressMsg{Code: tea.KeyEnter, Mod: tea.ModAlt} }

// --- the chord in both entry points ----------------------------------------

func TestPanelAltEnterInsertsBreak(t *testing.T) {
	m := openPanel(t, "a; b; c\n")
	m = typeKeys(m, ";")
	m = send(m, altEnter())
	if m.replPanel == nil {
		t.Fatal("alt+enter must not run the substitute")
	}
	if m.replPanel.find.Text != ";\n" || m.replPanel.find.Cur != 2 {
		t.Fatalf("find=%q cur=%d, want %q 2", m.replPanel.find.Text, m.replPanel.find.Cur, ";\n")
	}
	// The Replace field takes the chord just the same.
	m = send(m, tea.KeyPressMsg{Code: tea.KeyTab})
	m = typeKeys(m, ";")
	m = send(m, altEnter())
	if m.replPanel.repl.Text != ";\n" {
		t.Fatalf("repl=%q, want %q", m.replPanel.repl.Text, ";\n")
	}
	// Plain enter still runs the substitute (here: into the confirm flow).
	m = send(m, tea.KeyPressMsg{Code: tea.KeyEnter})
	if m.replPanel != nil {
		t.Fatal("enter should still close the panel and run the substitute")
	}
}

func TestSearchLineAltEnterInsertsBreak(t *testing.T) {
	m, _ := loaded(t, "foo\nbar\n")
	m = send(m, key('/'))
	m = typeKeys(m, "foo")
	m = send(m, altEnter())
	m = typeKeys(m, "bar")
	if m.cmdline != "foo\nbar" || m.cmdCur != 7 {
		t.Fatalf("cmdline=%q cmdCur=%d, want %q 7", m.cmdline, m.cmdCur, "foo\nbar")
	}
	if m.mode != Command {
		t.Fatal("alt+enter must not commit the search")
	}
	// The incsearch preview keeps working over the break and lands on the match.
	if m.preview.Empty() || m.cursor.Line != 0 || m.cursor.Col != 0 {
		t.Fatalf("preview landing=%v", m.cursor)
	}
	// Plain enter still commits.
	m = send(m, tea.KeyPressMsg{Code: tea.KeyEnter})
	if m.mode != Normal || m.query.Pattern != "foo\nbar" {
		t.Fatalf("enter: mode=%v query=%q", m.mode, m.query.Pattern)
	}
}

func TestFollowFilterLineRejectsBreak(t *testing.T) {
	// The chord belongs to the *search* line; the follow filter shares the row
	// but not the semantics — its query is per line by definition.
	m, _ := loaded(t, "foo\nbar\n")
	m.mode = Command
	m.filtering = true
	m.cmdline = "foo"
	m.cmdCur = 3
	m = send(m, altEnter())
	if strings.Contains(m.cmdline, "\n") {
		t.Fatalf("filter line took a break: %q", m.cmdline)
	}
}

// --- rendering --------------------------------------------------------------

func TestBreakRendersAsOneRowMarker(t *testing.T) {
	m, _ := loaded(t, "foo\nbar\n")
	m = send(m, key('/'))
	m = typeKeys(m, "foo")
	m = send(m, altEnter())
	m = typeKeys(m, "bar")
	row := m.commandLineRow()
	if strings.Contains(row, "\n") {
		t.Fatalf("search row must stay one row: %q", row)
	}
	if !strings.Contains(row, ui.BreakMarker) {
		t.Fatalf("search row should show the marker: %q", row)
	}

	p := openPanel(t, "a; b\n")
	p = typeKeys(p, ";")
	p = send(p, altEnter())
	rows := p.replacePanelRows(120)
	for i, r := range rows {
		if strings.Contains(r, "\n") {
			t.Fatalf("panel row %d must stay one row: %q", i, r)
		}
	}
	if !strings.Contains(rows[0], ui.BreakMarker) {
		t.Fatalf("Find row should show the marker: %q", rows[0])
	}
	if !strings.Contains(rows[2], "alt+enter") {
		t.Fatalf("hint row should name the chord: %q", rows[2])
	}
}

// --- the panel's hand-off to the ex line ------------------------------------

func TestBuildSubLineEscapesBreaks(t *testing.T) {
	line, ok := buildSubLine(";", ";\n", "g")
	if !ok {
		t.Fatal("buildSubLine should find a delimiter")
	}
	if strings.Contains(line, "\n") {
		t.Fatalf("the ex line must stay one line: %q", line)
	}
	if line != `%s/;/;\n/g` {
		t.Fatalf("line=%q, want %q", line, `%s/;/;\n/g`)
	}
	// A break in the pattern survives the round trip too.
	line, _ = buildSubLine("foo\nbar", "x", "g")
	if line != `%s/foo\nbar/x/g` {
		t.Fatalf("line=%q, want %q", line, `%s/foo\nbar/x/g`)
	}
}

func TestPanelSubstituteWithBreakRoundTrips(t *testing.T) {
	m := openPanel(t, "a; b; c\n")
	m = typeKeys(m, ";")
	m = send(m, tea.KeyPressMsg{Code: tea.KeyTab})
	m = typeKeys(m, ";")
	m = send(m, altEnter())
	// ctrl+a is replace-all, so the whole run lands in one go.
	m = send(m, modKey('a', tea.ModCtrl))
	if m.replPanel != nil {
		t.Fatal("ctrl+a should close the panel")
	}
	want := []string{"a;", " b;", " c"}
	if m.buf.LineCount() != len(want) {
		t.Fatalf("lines=%d want %d (%q)", m.buf.LineCount(), len(want), m.buf.String())
	}
	for i, w := range want {
		if got := line(m, i); got != w {
			t.Fatalf("line %d = %q, want %q", i, got, w)
		}
	}
}

// --- multi-line replace -----------------------------------------------------

func TestSubstituteReplacementBreakIsOneUndo(t *testing.T) {
	m, _ := loaded(t, "a; b; c\n")
	m = runEx(m, `s/;/;\n/g`)
	if m.buf.String() != "a;\n b;\n c" {
		t.Fatalf("after substitute: %q", m.buf.String())
	}
	m = send(m, key('u'))
	if m.buf.String() != "a; b; c" {
		t.Fatalf("after undo: %q", m.buf.String())
	}
}

func TestSubstituteReplacementBreakAcrossLines(t *testing.T) {
	// Every line grows by one, and the later lines' indices must not drift
	// while the edits are applied.
	m, _ := loaded(t, "a;x\nb;y\n")
	m = runEx(m, `%s/;/;\n/g`)
	if m.buf.String() != "a;\nx\nb;\ny" {
		t.Fatalf("multi-line: %q", m.buf.String())
	}
	if m.cursor.Line != 3 {
		t.Fatalf("cursor line=%d, want 3", m.cursor.Line)
	}
}

func TestSubstitutePatternSpanningLines(t *testing.T) {
	m, _ := loaded(t, "foo\nbar\nbaz\n")
	m = runEx(m, `s/foo\nbar/joined/`)
	if m.buf.String() != "joined\nbaz" {
		t.Fatalf("spanning pattern: %q", m.buf.String())
	}
	m = send(m, key('u'))
	if m.buf.String() != "foo\nbar\nbaz" {
		t.Fatalf("after undo: %q", m.buf.String())
	}
}

func TestSubstituteSpanningConfirmFlow(t *testing.T) {
	m, _ := loaded(t, "foo\nbar\nfoo\nbar\n")
	m = runEx(m, `%s/foo\nbar/X/gc`)
	if m.subConfirm == nil {
		t.Fatal("c flag should enter confirmation mode")
	}
	// The highlight covers the match's part of its first line.
	sc := m.subConfirm
	if sc.curLine != 0 || sc.curStart != 0 || sc.curEnd != 3 {
		t.Fatalf("highlight=(%d,%d,%d)", sc.curLine, sc.curStart, sc.curEnd)
	}
	m = send(m, key('y'))
	// The second match moved up a line once the first collapsed two into one.
	if m.subConfirm == nil {
		t.Fatal("second match should be offered")
	}
	if m.subConfirm.curLine != 1 {
		t.Fatalf("second highlight line=%d, want 1", m.subConfirm.curLine)
	}
	m = send(m, key('y'))
	if m.buf.String() != "X\nX" {
		t.Fatalf("after both: %q", m.buf.String())
	}
	m = send(m, key('u'))
	if m.buf.String() != "foo\nbar\nfoo\nbar" {
		t.Fatalf("after undo: %q", m.buf.String())
	}
}

func TestSubstituteCountOnlySpanning(t *testing.T) {
	m, _ := loaded(t, "foo\nbar\nfoo\nbar\n")
	m = runEx(m, `%s/foo\nbar/X/gn`)
	if m.buf.String() != "foo\nbar\nfoo\nbar" {
		t.Fatal("the n flag must not mutate the buffer")
	}
	if m.cmdMsg != "2 matches on 4 lines" {
		t.Fatalf("report=%q", m.cmdMsg)
	}
}

// --- multi-line find --------------------------------------------------------

func TestSearchPatternSpanningLinesNavigates(t *testing.T) {
	m, _ := loaded(t, "top\nfoo\nbar\nfiller\nfoo\nbar\n")
	m = send(m, key('/'))
	m = typeKeys(m, "foo")
	m = send(m, altEnter())
	m = typeKeys(m, "bar")
	// The live tally counts each spanning match once, and incsearch has
	// already parked on the first one.
	if got := m.SearchCounter(); got != "1/2" {
		t.Fatalf("tally=%q, want %q", got, "1/2")
	}
	m = send(m, tea.KeyPressMsg{Code: tea.KeyEnter})
	if m.cursor.Line != 1 {
		t.Fatalf("committed landing line=%d, want 1", m.cursor.Line)
	}
	m = send(m, key('n'))
	if m.cursor.Line != 4 {
		t.Fatalf("n line=%d, want 4", m.cursor.Line)
	}
	m = send(m, key('N'))
	if m.cursor.Line != 1 {
		t.Fatalf("N line=%d, want 1", m.cursor.Line)
	}
	// Both lines of a match are highlighted.
	if got := m.query.LineMatches(m.buf, 2); len(got) != 1 {
		t.Fatalf("second line of the match should highlight: %v", got)
	}
}
