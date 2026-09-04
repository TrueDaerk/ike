package diff

// orientation_test.go covers the diff viewer's orientation cues (#2494):
// scroll-aware hunk stepping with viewport re-sync, the current-hunk gutter
// marker, the footer's hunk/progress readout, the side-label headers, the
// separator expand buttons with their o-target highlight, and the search
// jumps that must land on the match row through any collapse/expand state.

import (
	"fmt"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

// blockDiff builds a diff of n changed blocks separated by runs of `spread`
// unchanged lines, so it has n hunks and (collapsed) n-ish separators.
func blockDiff(n, spread int) (left, right string) {
	var lb, rb strings.Builder
	for b := 0; b < n; b++ {
		for i := 0; i < spread; i++ {
			fmt.Fprintf(&lb, "same-%d-%d\n", b, i)
			fmt.Fprintf(&rb, "same-%d-%d\n", b, i)
		}
		fmt.Fprintf(&lb, "old-%d\n", b)
		fmt.Fprintf(&rb, "new-%d\n", b)
	}
	return lb.String(), rb.String()
}

func TestStepHunkIsScrollAware(t *testing.T) {
	left, right := blockDiff(6, 30)
	m := testModel(t, left, right)
	m.SetContext(-1) // no folding: visual line i is row i
	if m.HunkCount() != 6 {
		t.Fatalf("setup: want 6 hunks, got %d", m.HunkCount())
	}
	// Scroll to the middle of the diff: past hunks 0-2 (rows 30, 61, 92),
	// with hunk 3 (row 123) below the viewport.
	m.scrollTo(100)
	m.stepHunk(1)
	if m.CurrentHunk() != 3 {
		t.Fatalf("F7 after scrolling must go to the first hunk below the anchor, got %d", m.CurrentHunk())
	}
	m.stepHunk(1)
	if m.CurrentHunk() != 4 {
		t.Fatalf("second F7 must continue forward, got %d", m.CurrentHunk())
	}
	// Scroll back up between hunks 0 and 1: the step must not continue from
	// hunk 4.
	m.scrollTo(40)
	m.stepHunk(1)
	if m.CurrentHunk() != 1 {
		t.Fatalf("F7 after scrolling up must re-sync, got %d", m.CurrentHunk())
	}
	m.stepHunk(-1)
	if m.CurrentHunk() != 0 {
		t.Fatalf("shift+F7 must step back from the stepped hunk, got %d", m.CurrentHunk())
	}
}

func TestScrollResyncsCurrentHunk(t *testing.T) {
	left, right := blockDiff(4, 30)
	m := testModel(t, left, right)
	m.SetContext(-1)
	m.stepHunk(1)
	stepped := m.CurrentHunk()
	// A wheel scroll deep into the document moves the current hunk with it.
	m.ScrollBy(80)
	if m.CurrentHunk() == stepped {
		t.Fatalf("scrolling must re-sync the current hunk, still %d", m.CurrentHunk())
	}
	// shift+F7 from down here goes to the last hunk above the anchor, not to
	// the hunk the earlier step pinned.
	m.stepHunk(-1)
	if got := m.CurrentHunk(); got < 2 {
		t.Fatalf("shift+F7 after scrolling must step near the viewport, got %d", got)
	}
}

func TestCurrentHunkMarkerFollowsStep(t *testing.T) {
	left, right := blockDiff(4, 5)
	m := testModel(t, left, right)
	m.SetContext(-1)
	marked := func() string {
		var out []string
		for _, line := range strings.Split(ansi.Strip(m.View()), "\n") {
			if strings.HasPrefix(line, "▎") {
				out = append(out, line)
			}
		}
		return strings.Join(out, "\n")
	}
	if marked() != "" {
		t.Fatalf("a fresh view has no current hunk to mark:\n%s", ansi.Strip(m.View()))
	}
	m.stepHunk(1)
	if !strings.Contains(marked(), "old-0") {
		t.Fatalf("the first step must mark hunk 0:\n%s", ansi.Strip(m.View()))
	}
	m.stepHunk(1)
	got := marked()
	if !strings.Contains(got, "old-1") || strings.Contains(got, "old-0") {
		t.Fatalf("marker must move to hunk 1 after a step, marked:\n%s", got)
	}
	// The marker sits on both sides of the marked row.
	row := strings.SplitN(got, "\n", 2)[0]
	if strings.Count(row, "▎") != 2 {
		t.Fatalf("side-by-side marker must mark both gutters: %q", row)
	}
}

func TestFooterShowsHunkAndProgress(t *testing.T) {
	left, right := blockDiff(5, 30)
	m := testModel(t, left, right)
	foot := func() string {
		lines := strings.Split(ansi.Strip(m.View()), "\n")
		return lines[len(lines)-1]
	}
	if f := foot(); !strings.Contains(f, "hunk –/5") || !strings.Contains(f, "0%") {
		t.Fatalf("fresh footer must show hunk –/5 and 0%%: %q", f)
	}
	// The first step targets hunk 0 (it starts just below the anchor); the
	// second walks on to hunk 1.
	m.stepHunk(1)
	m.stepHunk(1)
	if f := foot(); !strings.Contains(f, "hunk 2/5") {
		t.Fatalf("footer must follow the step: %q", f)
	}
	m.scrollTo(len(m.lines))
	if f := foot(); !strings.Contains(f, "100%") {
		t.Fatalf("footer at the end must read 100%%: %q", f)
	}
	if m.Progress() != 100 {
		t.Fatalf("Progress at the end = %d", m.Progress())
	}
}

func TestProgressComputation(t *testing.T) {
	left, right := blockDiff(2, 40)
	m := testModel(t, left, right)
	m.SetContext(-1)
	if got := m.Progress(); got != 0 {
		t.Fatalf("Progress at the top = %d, want 0", got)
	}
	maxTop := len(m.lines) - m.viewHeight()
	m.scrollTo(maxTop / 2)
	if got := m.Progress(); got < 45 || got > 55 {
		t.Fatalf("Progress at the middle = %d, want ~50", got)
	}
	// A diff that fits its viewport is always fully visible.
	small := testModel(t, "a\nb", "a\nB")
	if got := small.Progress(); got != 100 {
		t.Fatalf("Progress of a fitting diff = %d, want 100", got)
	}
}

func TestSideLabelHeaders(t *testing.T) {
	m := testModel(t, "a\nold\nc", "a\nnew\nc")
	m.SetSideLabels("HEAD", "working copy")
	v := ansi.Strip(m.View())
	head := strings.SplitN(v, "\n", 2)[0]
	if !strings.Contains(head, "HEAD") || !strings.Contains(head, "working copy") {
		t.Fatalf("side-by-side header must carry both labels: %q", head)
	}
	// The labels head their columns: HEAD left of the separator, the working
	// copy right of it.
	if strings.Index(head, "HEAD") > strings.Index(head, "│") {
		t.Fatalf("left label must head the left column: %q", head)
	}
	m.SetUnified(true)
	head = strings.SplitN(ansi.Strip(m.View()), "\n", 2)[0]
	if !strings.Contains(head, "HEAD → working copy") {
		t.Fatalf("unified header must read left → right: %q", head)
	}
	l, r := m.SideLabels()
	if l != "HEAD" || r != "working copy" {
		t.Fatalf("SideLabels = %q, %q", l, r)
	}
}

func TestSideLabelsFallBackToTitles(t *testing.T) {
	m := testModel(t, "a", "b")
	l, r := m.SideLabels()
	if l != "left.txt" || r != "right.txt" {
		t.Fatalf("labels must fall back to the titles, got %q, %q", l, r)
	}
	// Without explicit labels no header row is spent.
	if !strings.Contains(strings.SplitN(ansi.Strip(m.View()), "\n", 2)[0], "a") {
		t.Fatal("an unlabeled diff must not render a header row")
	}
}

func TestSeparatorButtonAndTarget(t *testing.T) {
	left, right := blockDiff(4, 30)
	m := testModel(t, left, right)
	v := ansi.Strip(m.View())
	if !strings.Contains(v, "▸") {
		t.Fatalf("collapsed separators must show the expand button:\n%s", v)
	}
	if !strings.Contains(v, "unchanged lines") {
		t.Fatalf("separators must keep their count:\n%s", v)
	}
	// The target is the separator nearest the viewport center — the one o
	// expands.
	target := m.targetGap()
	if target < 0 {
		t.Fatal("a collapsed view must have an o target")
	}
	before := len(m.lines)
	m.Update(key("o"))
	if !m.gaps[target].expanded {
		t.Fatalf("o must expand the targeted gap %d", target)
	}
	if len(m.lines) <= before {
		t.Fatal("expanding must add the hidden rows to the view")
	}
	// The footer names the affordance.
	lines := strings.Split(ansi.Strip(m.View()), "\n")
	if f := lines[len(lines)-1]; !strings.Contains(f, "o expands ▸ marked gap") {
		t.Fatalf("footer must name the o target: %q", f)
	}
}

func TestSeparatorClickExpands(t *testing.T) {
	left, right := blockDiff(4, 30)
	m := testModel(t, left, right)
	// Find a separator on screen and click its button cell.
	var sepY, gi int = -1, -1
	for line, g := range m.sepLines {
		if line >= m.top && line < m.top+m.viewHeight() {
			sepY, gi = line-m.top, g
			break
		}
	}
	if sepY < 0 {
		t.Fatal("setup: expected a visible separator")
	}
	before := len(m.lines)
	m.MousePress(0, sepY)
	if !m.gaps[gi].expanded {
		t.Fatal("clicking the ▸ button must expand the gap")
	}
	if len(m.lines) <= before {
		t.Fatal("the click must have re-rendered the expanded rows")
	}
	if m.HasSelection() {
		t.Fatal("the button click must not start a selection")
	}
}

func TestSeparatorClickPastButtonSelects(t *testing.T) {
	left, right := blockDiff(4, 30)
	m := testModel(t, left, right)
	var sepY, gi int = -1, -1
	for line, g := range m.sepLines {
		if line >= m.top && line < m.top+m.viewHeight() {
			sepY, gi = line-m.top, g
			break
		}
	}
	if sepY < 0 {
		t.Fatal("setup: expected a visible separator")
	}
	m.MousePress(m.w/2, sepY)
	if m.gaps[gi].expanded {
		t.Fatal("a press on the label must not expand the gap")
	}
}

func TestSearchJumpLandsOnMatchThroughGaps(t *testing.T) {
	left, right := blockDiff(4, 30)
	m := testModel(t, left, right)
	// The match is an unchanged line hidden inside a collapsed gap.
	m.OpenSearch()
	typeQuery(m, "same-2-15")
	m.Update(key("enter"))
	row, ok := m.search.Current()
	if !ok {
		t.Fatal("setup: query must match")
	}
	// The jump expanded the hiding gap and landed the row a third down.
	if line := m.rowStart(row); m.vrows[line].row != row {
		t.Fatalf("the match row must be visible after the jump, line %d shows row %d", line, m.vrows[line].row)
	}
	if want := max(0, m.rowStart(row)-m.viewHeight()/3); m.top != want {
		t.Fatalf("jump landed at top %d, want %d", m.top, want)
	}
}

func TestSearchStepAfterExpandAndCollapseToggle(t *testing.T) {
	left, right := blockDiff(6, 30)
	m := testModel(t, left, right)
	m.OpenSearch()
	typeQuery(m, "new-")
	m.Update(key("enter"))
	// Expand a gap above the later matches, then step: the landing must be
	// computed against the new layout.
	m.expandGap(0)
	m.Update(key("n"))
	row, _ := m.search.Current()
	if want := clamp(m.rowStart(row)-m.viewHeight()/3, 0, max(0, len(m.lines)-m.viewHeight())); m.top != want {
		t.Fatalf("n after o landed at top %d, want %d", m.top, want)
	}
	// Toggle full context and step again.
	m.Update(key("c"))
	m.Update(key("n"))
	row, _ = m.search.Current()
	if want := clamp(m.rowStart(row)-m.viewHeight()/3, 0, max(0, len(m.lines)-m.viewHeight())); m.top != want {
		t.Fatalf("n after c landed at top %d, want %d", m.top, want)
	}
}

func TestSearchMatchesRebuiltOnRediff(t *testing.T) {
	m := testModel(t, "alpha\nbeta\ngamma", "alpha\nbeta\ngamma")
	m.OpenSearch()
	typeQuery(m, "gamma")
	m.Update(key("enter"))
	if m.SearchMatches() != 1 {
		t.Fatalf("setup: want 1 match, got %d", m.SearchMatches())
	}
	// New contents shift the matching row; the cached match rows must follow.
	m.SetContents("intro\nalpha\nbeta\ngamma", "intro\nalpha\nbeta\ngamma")
	if m.SearchMatches() != 1 {
		t.Fatalf("matches after SetContents = %d", m.SearchMatches())
	}
	row, ok := m.search.Current()
	if !ok || m.res.Rows[row].Left != "gamma" {
		t.Fatalf("match must point at the new gamma row, got row %d", row)
	}
}
