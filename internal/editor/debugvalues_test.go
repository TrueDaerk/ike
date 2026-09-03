package editor

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

// debugLoaded opens a small document in an 80-column view.
func debugLoaded(t *testing.T, content string) Model {
	t.Helper()
	path := filepath.Join(t.TempDir(), "f.txt")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	m := New()
	if err := m.Load(path); err != nil {
		t.Fatal(err)
	}
	m.SetSize(80, 10)
	m.SetFocused(true)
	return m
}

const debugDoc = "count := 0\nmax := count + x2\ntotal := count + x\nplain line\n"

// TestDebugValuesRenderOnMentioningLines covers #1914: every line mentioning
// a local as a whole word carries its value, joined in push order, and lines
// without a mention stay clean — including near-miss identifiers (x must not
// match max or x2).
func TestDebugValuesRenderOnMentioningLines(t *testing.T) {
	m := debugLoaded(t, debugDoc)
	m.SetDebugLocals([]DebugLocal{{Name: "count", Value: "3"}, {Name: "x", Value: "7"}})

	rows := strings.Split(ansi.Strip(m.View()), "\n")
	if !strings.Contains(rows[0], "▏ count = 3") {
		t.Errorf("line 0 mentions count: %q", rows[0])
	}
	if !strings.Contains(rows[1], "count = 3") || strings.Contains(rows[1], "x =") {
		t.Errorf("line 1 mentions count but x2/max must not match x: %q", rows[1])
	}
	if !strings.Contains(rows[2], "count = 3, x = 7") {
		t.Errorf("line 2 joins both locals in push order: %q", rows[2])
	}
	if strings.Contains(rows[3], "▏") {
		t.Errorf("a line without mentions must stay unannotated: %q", rows[3])
	}
}

// TestDebugValuesDedupeRepeatedName: a name occurring twice on one line — or
// pushed twice — annotates once.
func TestDebugValuesDedupeRepeatedName(t *testing.T) {
	m := debugLoaded(t, "x = x + 1\n")
	m.SetDebugLocals([]DebugLocal{{Name: "x", Value: "7"}, {Name: "x", Value: "9"}})

	if got := m.DebugValueAt(0); got != "x = 7" {
		t.Errorf("one mention despite two occurrences and a duplicate push: %q", got)
	}
}

// TestDebugValuesClear: a nil push removes every annotation and invalidates
// the render cache so the view actually loses them.
func TestDebugValuesClear(t *testing.T) {
	m := debugLoaded(t, debugDoc)
	m.SetDebugLocals([]DebugLocal{{Name: "count", Value: "3"}})
	before := m.RenderVersion()
	m.SetDebugLocals(nil)

	if got := m.DebugValueAt(0); got != "" {
		t.Errorf("the annotation must be gone: %q", got)
	}
	if m.RenderVersion() == before {
		t.Error("clearing must invalidate the render cache, or the view keeps the old frame")
	}
	if strings.Contains(ansi.Strip(m.View()), "▏") {
		t.Errorf("view must be free of annotations:\n%s", m.View())
	}
}

// TestDebugValuesRepaintOnChange: an identical push costs no repaint, a moved
// value does — mirroring the http-flight render-cache contract.
func TestDebugValuesRepaintOnChange(t *testing.T) {
	m := debugLoaded(t, debugDoc)
	m.SetDebugLocals([]DebugLocal{{Name: "count", Value: "3"}})
	stable := m.RenderVersion()
	m.SetDebugLocals([]DebugLocal{{Name: "count", Value: "3"}})
	if m.RenderVersion() != stable {
		t.Error("an identical push must not invalidate the cache")
	}
	m.SetDebugLocals([]DebugLocal{{Name: "count", Value: "4"}})
	if m.RenderVersion() == stable {
		t.Error("a moved value must invalidate the cache")
	}
}

// TestDebugValuesSkippedWhenTight: the annotation never truncates code — a
// line too wide to leave room renders unchanged.
func TestDebugValuesSkippedWhenTight(t *testing.T) {
	long := "x := " + strings.Repeat("y", 70)
	m := debugLoaded(t, long+"\n")
	m.SetDebugLocals([]DebugLocal{{Name: "x", Value: "7"}})

	rows := strings.Split(ansi.Strip(m.View()), "\n")
	if strings.Contains(rows[0], "▏") {
		t.Errorf("no room means no annotation: %q", rows[0])
	}
	if !strings.Contains(rows[0], strings.Repeat("y", 70)) {
		t.Errorf("the code must survive untruncated: %q", rows[0])
	}
}

// TestDebugValuesFollowEdit: the per-version rescan (the testmarks
// discipline) keeps annotations correct across an edit while paused — a new
// line mentioning a local picks up its value without a re-push.
func TestDebugValuesFollowEdit(t *testing.T) {
	m := debugLoaded(t, "plain line\n")
	m.SetDebugLocals([]DebugLocal{{Name: "count", Value: "3"}})
	if got := m.DebugValueAt(0); got != "" {
		t.Fatalf("nothing mentions count yet: %q", got)
	}

	m = typeKeys(m, "Ocount") // open a line above, type "count"
	rows := strings.Split(ansi.Strip(m.View()), "\n")
	if !strings.Contains(rows[0], "count = 3") {
		t.Errorf("the fresh mention must carry the value after the edit: %q", rows[0])
	}
	if got := m.DebugValueAt(0); got != "count = 3" {
		t.Errorf("DebugValueAt must rescan on version change: %q", got)
	}
}

// TestDebugValuesCapLength: the assembled annotation caps at 120 runes with
// an ellipsis.
func TestDebugValuesCapLength(t *testing.T) {
	m := debugLoaded(t, "x\n")
	m.SetDebugLocals([]DebugLocal{{Name: "x", Value: strings.Repeat("ä", 300)}})

	got := m.DebugValueAt(0)
	r := []rune(got)
	if len(r) != 120 {
		t.Fatalf("annotation must cap at 120 runes, got %d: %q", len(r), got)
	}
	if r[len(r)-1] != '…' {
		t.Errorf("a capped annotation must end in an ellipsis: %q", got)
	}
}

// TestDebugValuesSkipNonIdentifiers: synthesized names cannot match a whole
// word and must be dropped instead of scanned.
func TestDebugValuesSkipNonIdentifiers(t *testing.T) {
	m := debugLoaded(t, "(*Struct).field and x\n")
	m.SetDebugLocals([]DebugLocal{
		{Name: "(*Struct).field", Value: "1"},
		{Name: "", Value: "2"},
		{Name: "x", Value: "7"},
	})

	if got := m.DebugValueAt(0); got != "x = 7" {
		t.Errorf("only the plain identifier may annotate: %q", got)
	}
}

// TestDebugValuesPHPSigilNames covers #2405: xdebug reports PHP locals as
// "$name", and those used to be dropped as non-identifiers — a PHP session
// showed no inline values at all. The sigil is part of the name and part of
// the match, so "$s" annotates "$s" without matching "$sum".
func TestDebugValuesPHPSigilNames(t *testing.T) {
	m := debugLoaded(t, "$s = \"hi\";\n$sum = 1;\n")
	m.SetDebugLocals([]DebugLocal{{Name: "$s", Value: "\"hi\""}})

	if got := m.DebugValueAt(0); got != "$s = \"hi\"" {
		t.Errorf("the PHP local must annotate its line: %q", got)
	}
	if got := m.DebugValueAt(1); got != "" {
		t.Errorf("$sum must not match $s: %q", got)
	}
}

// TestDebugValuesFocusedLineTruncates covers the #2405 focus window: on the
// paused frame's line (and the two above it) a hint that does not fit shrinks
// instead of disappearing, which is exactly where a stepping user looks.
func TestDebugValuesFocusedLineTruncates(t *testing.T) {
	long := "x := " + strings.Repeat("y", 50)
	m := debugLoaded(t, long+"\n"+long+"\n")
	m.SetDebugLocals([]DebugLocal{{Name: "x", Value: strings.Repeat("v", 40)}})

	rows := strings.Split(ansi.Strip(m.View()), "\n")
	if strings.Contains(rows[0], "\u258f") {
		t.Fatalf("without a focus the tight line stays unannotated: %q", rows[0])
	}

	m.SetDebugFocus(0)
	rows = strings.Split(ansi.Strip(m.View()), "\n")
	if !strings.Contains(rows[0], "x = vvv") || !strings.Contains(rows[0], "\u2026") {
		t.Errorf("the focused line must carry a truncated hint: %q", rows[0])
	}
	if strings.Contains(rows[1], "\u258f") {
		t.Errorf("a line below the focus stays unannotated: %q", rows[1])
	}
}

// TestDebugValuesFocusWindowSpansTwoLinesAbove pins the window itself: the
// frame line and the two above it, nothing further.
func TestDebugValuesFocusWindowSpansTwoLinesAbove(t *testing.T) {
	m := debugLoaded(t, "a\nb\nc\nd\ne\n")
	m.SetDebugLocals([]DebugLocal{{Name: "a", Value: "1"}})
	m.SetDebugFocus(3)
	for line, want := range map[int]bool{0: false, 1: true, 2: true, 3: true, 4: false} {
		if got := m.debugValueFocused(line); got != want {
			t.Errorf("focus at line %d = %v, want %v", line, got, want)
		}
	}
	m.SetDebugFocus(-1)
	if m.debugValueFocused(3) {
		t.Error("clearing the focus must empty the window")
	}
}
