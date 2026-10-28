package diff

import (
	"fmt"
	"strings"
	"testing"
)

// sourceModel builds a long, uncollapsed diff with three one-line hunks on
// right-side lines 10, 30 and 50 — far enough apart that the viewport
// anchor can sit inside, between and after them (#3081).
func sourceModel(t *testing.T) *Model {
	t.Helper()
	var l, r []string
	for i := 1; i <= 60; i++ {
		line := fmt.Sprintf("line %d", i)
		l = append(l, line)
		if i == 10 || i == 30 || i == 50 {
			line = "changed " + line
		}
		r = append(r, line)
	}
	m := testModel(t, strings.Join(l, "\n"), strings.Join(r, "\n"))
	m.SetContext(-1)
	if m.HunkCount() != 3 {
		t.Fatalf("fixture: want 3 hunks, got %d", m.HunkCount())
	}
	return m
}

// anchorAt scrolls so the viewport anchor sits on visual line v.
func anchorAt(m *Model, v int) {
	m.scrollTo(v - m.viewHeight()/3)
	if got := m.anchorLine(); got != v {
		panic(fmt.Sprintf("anchor = %d, want %d", got, v))
	}
}

func TestSourceTargetCursorInsideHunk(t *testing.T) {
	m := sourceModel(t)
	anchorAt(m, m.rowStarts[m.res.Hunks[1].Start])
	path, line, ok := m.SourceTarget()
	if !ok || path != "/tmp/right.txt" || line != 30 {
		t.Fatalf("inside hunk 2: got %q:%d ok=%v, want /tmp/right.txt:30", path, line, ok)
	}
}

func TestSourceTargetBetweenHunksTakesFollowing(t *testing.T) {
	m := sourceModel(t)
	// Visual line 19 is row 20, between the hunks on rows 10 and 30.
	anchorAt(m, 19)
	if _, line, _ := m.SourceTarget(); line != 30 {
		t.Fatalf("between hunks: got line %d, want the following hunk's 30", line)
	}
}

func TestSourceTargetAfterLastHunkTakesLast(t *testing.T) {
	m := sourceModel(t)
	m.scrollTo(len(m.lines))
	if m.anchorLine() <= m.rowStarts[m.res.Hunks[2].Start] {
		t.Fatal("fixture: the anchor should sit below the last hunk")
	}
	if _, line, _ := m.SourceTarget(); line != 50 {
		t.Fatalf("after the last hunk: got line %d, want 50", line)
	}
}

func TestSourceTargetFollowsSteppedHunk(t *testing.T) {
	m := sourceModel(t)
	m.StepHunk(1)
	m.StepHunk(1)
	if _, line, _ := m.SourceTarget(); line != 30 {
		t.Fatalf("after two steps: got line %d, want 30", line)
	}
}

func TestSourceTargetWithoutHunksLandsOnFirstLine(t *testing.T) {
	m := testModel(t, "a\nb", "a\nb")
	if _, line, ok := m.SourceTarget(); !ok || line != 1 {
		t.Fatalf("no hunks: got line %d ok=%v, want 1", line, ok)
	}
}

func TestSourceTargetWithoutFile(t *testing.T) {
	m := New("diff", "HEAD", "buffer", "", nil)
	m.SetSize(80, 10)
	m.SetContents("a", "b")
	if _, _, ok := m.SourceTarget(); ok {
		t.Fatal("a diff without a right-side file has no source to jump to")
	}
}
