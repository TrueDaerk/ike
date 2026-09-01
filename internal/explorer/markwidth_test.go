package explorer

// markwidth_test.go covers #2380: pressing space used to add a two-cell
// multi-select column without invalidating the memoized row widths, so every
// row rendered two cells wider than the renderer thought and the terminal
// wrapped the whole pane. The mark now lives in the row's own indent-guide
// cell (width-neutral), and the width memo re-measures whenever the state
// rowText reads has changed.

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"ike/internal/host"
	"ike/internal/theme"
	"ike/internal/vcs"
)

// measureRows recomputes every visible row's width straight from rowText,
// bypassing the memo — the ground truth n.rowW and contentWidth must match.
func measureRows(m Model) (max int, each []int) {
	for _, n := range m.rows {
		w := ansi.StringWidth(m.rowText(n))
		each = append(each, w)
		if w > max {
			max = w
		}
	}
	return max, each
}

// checkWidths asserts the memoized geometry (contentWidth and the per-node
// rowW the renderer, the scrollbars and the hit tests all run on) agrees with
// a fresh measurement.
func checkWidths(t *testing.T, m Model, what string) {
	t.Helper()
	got := m.contentWidth() // may serve the memo — that is the point
	want, each := measureRows(m)
	if got != want {
		t.Errorf("%s: contentWidth = %d, want %d", what, got, want)
	}
	for i, n := range m.rows {
		if n.rowW != each[i] {
			t.Errorf("%s: row %d (%s) rowW = %d, want %d", what, i, n.name, n.rowW, each[i])
		}
	}
}

// TestMarkKeepsRowWidth is the direct regression for #2380: toggleMark and
// clearMarks must leave rowW/contentWidth in agreement with rowText, and must
// not change the width at all.
func TestMarkKeepsRowWidth(t *testing.T) {
	m := selTree(t)
	checkWidths(t, m, "unmarked")
	before, eachBefore := measureRows(m)

	m, _ = send(m, key("j"), key("j"), spaceKey()) // mark a.txt
	checkWidths(t, m, "after toggleMark")
	after, eachAfter := measureRows(m)
	if after != before {
		t.Errorf("marking changed the content width: %d -> %d", before, after)
	}
	for i := range eachBefore {
		if eachAfter[i] != eachBefore[i] {
			t.Errorf("marking changed row %d width: %d -> %d", i, eachBefore[i], eachAfter[i])
		}
	}

	m, _ = send(m, escKey()) // clearMarks
	checkWidths(t, m, "after clearMarks")
	if w, _ := measureRows(m); w != before {
		t.Errorf("clearing marks changed the content width: %d -> %d", before, w)
	}
}

// TestMarkRestoresExactRendering guards that unmarking puts the pane back
// byte for byte — the mark swaps one glyph and nothing else.
func TestMarkRestoresExactRendering(t *testing.T) {
	m := selTree(t)
	// Park the cursor where it ends up after marking, so the comparison isolates
	// the marks (esc clears them without moving the cursor).
	m, _ = send(m, key("j"), key("j"), key("j"))
	plain := m.View()
	m, _ = send(m, key("k"), spaceKey(), key("j"), spaceKey())
	if m.View() == plain {
		t.Fatal("marked rows must render differently from unmarked ones")
	}
	m, _ = send(m, escKey())
	if got := m.View(); got != plain {
		t.Fatalf("clearing every mark must restore the previous rendering exactly:\n%q\n%q", got, plain)
	}
}

// TestMarkUsesGuideColumn guards the placement: the mark replaces the row's
// own indent-guide glyph instead of prepending a column, so the name column
// does not move.
func TestMarkUsesGuideColumn(t *testing.T) {
	m := selTree(t)
	n := m.rows[2] // a.txt, depth 1
	plain := m.rowText(n)
	if !strings.HasPrefix(plain, "│") {
		t.Fatalf("depth-1 row %q must start with its guide glyph", plain)
	}
	m, _ = send(m, key("j"), key("j"), spaceKey())
	marked := m.rowText(n)
	if want := markGlyph + strings.TrimPrefix(plain, "│"); marked != want {
		t.Fatalf("marked row = %q, want %q (guide glyph swapped in place)", marked, want)
	}
	// And the mark must be told apart from a guide by colour, not glyph alone.
	style := m.selCellStyle(m.rowStyle(2, n), n)
	if rgb(style.GetForeground()) == rgb(theme.DefaultPalette().IndentGuide) {
		t.Fatal("the mark cell must not render in the dimmed indent-guide colour")
	}
	if rgb(style.GetForeground()) != rgb(theme.DefaultPalette().Accent) {
		t.Fatalf("mark cell fg = %v, want Accent", style.GetForeground())
	}
	if g := m.selCellStyle(m.rowStyle(3, m.rows[3]), m.rows[3]); rgb(g.GetForeground()) !=
		rgb(theme.DefaultPalette().IndentGuide) {
		t.Fatalf("an unmarked row's guide cell must stay IndentGuide, got %v", g.GetForeground())
	}
}

// TestDepthZeroMarkStaysWidthNeutral guards the row class with no guide
// column: the mark lands in the expand marker's blank second cell instead of
// widening the row.
func TestDepthZeroMarkStaysWidthNeutral(t *testing.T) {
	m := New(t.TempDir())
	m.SetSize(40, 10)
	n := &node{name: "top.txt", path: "/top.txt", depth: 0}
	m.rows = []*node{n}
	plain := m.rowText(n)
	m.marks = map[string]bool{n.path: true}
	marked := m.rowText(n)
	if !strings.Contains(marked, markGlyph) {
		t.Fatalf("a depth-0 row must still show its mark, got %q", marked)
	}
	if ansi.StringWidth(marked) != ansi.StringWidth(plain) {
		t.Fatalf("depth-0 mark changed the width: %q (%d) vs %q (%d)",
			marked, ansi.StringWidth(marked), plain, ansi.StringWidth(plain))
	}
}

// viewRows returns View's tree lines without the scratch/footer tail.
func viewRows(m Model, n int) []string {
	lines := strings.Split(m.View(), "\n")
	if len(lines) > n {
		lines = lines[:n]
	}
	return lines
}

// TestMarkedViewNeverExceedsPaneWidth is the visible symptom of #2380: a line
// wider than the pane is what the terminal wrapped into blank rows, stray
// guides and displaced VCS badges. Checked with icons on and off, and in a
// pane narrow enough that every row clips.
func TestMarkedViewNeverExceedsPaneWidth(t *testing.T) {
	for _, tc := range []struct {
		name  string
		w, h  int
		icons bool
	}{
		{"plain", 40, 12, false},
		{"icons", 40, 12, true},
		{"narrow", 12, 12, false},
		{"narrow+icons", 12, 12, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := tree(t)
			m := mounted(t, root, tc.w, tc.h)
			m.SetFocused(true)
			if tc.icons {
				m.Configure(host.MapConfig{"explorer.icons": "true"})
			}
			m.SetVCS(vcs.NewSnapshot(root, map[string]vcs.FileStatus{
				"a.txt": vcs.StatusUntracked,
				"b.txt": vcs.StatusModified,
			}))
			m, _ = send(m, key("j"), key("j"), spaceKey(), key("j"), spaceKey())
			for i, line := range viewRows(m, tc.h) {
				if w := ansi.StringWidth(line); w > tc.w {
					t.Fatalf("line %d is %d cells wide in a %d-cell pane: %q",
						i, w, tc.w, stripANSI(line))
				}
			}
			checkWidths(t, m, "marked view")
		})
	}
}

// TestMarkedRowKeepsVCSBadgeRightAligned guards #1868 under a mark: the badge
// stays in the pane's last cell, one- and two-cell codes alike.
func TestMarkedRowKeepsVCSBadgeRightAligned(t *testing.T) {
	root := tree(t)
	m := mounted(t, root, 40, 12)
	m.SetFocused(true)
	m.SetVCS(vcs.NewSnapshot(root, map[string]vcs.FileStatus{"a.txt": vcs.StatusUntracked}))
	m, _ = send(m, key("j"), key("j"), spaceKey())
	line := stripANSI(viewRows(m, 12)[2])
	if !strings.HasSuffix(strings.TrimRight(line, " "), "U") {
		t.Fatalf("marked row must keep its right-aligned VCS badge, got %q", line)
	}
	if !strings.Contains(line, markGlyph) {
		t.Fatalf("marked row must show its mark, got %q", line)
	}
}

// TestWidthCacheTracksRowTextState is the class test the fix owes (#2380):
// each mutation below changes what rowText reads, and none of them calls
// invalidateWidth by hand. If a future state ever slips out of widthKey, the
// memo serves a stale width here instead of failing silently in the renderer.
func TestWidthCacheTracksRowTextState(t *testing.T) {
	root := tree(t)
	steps := []struct {
		name string
		do   func(*Model)
	}{
		{"expand a directory", func(m *Model) {
			*m, _ = send(*m, key("j"), key("enter"))
		}},
		{"mark a row", func(m *Model) {
			*m, _ = send(*m, key("j"), spaceKey())
		}},
		{"mark a second row", func(m *Model) {
			*m, _ = send(*m, key("j"), spaceKey())
		}},
		{"unmark one row", func(m *Model) {
			*m, _ = send(*m, spaceKey())
		}},
		{"prune the last mark", func(m *Model) {
			m.marks = map[string]bool{"/gone": true}
			m.pruneMarks()
		}},
		{"clear the marks", func(m *Model) { m.clearMarks() }},
		{"resize the pane", func(m *Model) { m.SetSize(60, 12) }},
		{"shrink below the root context width", func(m *Model) { m.SetSize(20, 12) }},
		{"turn icons on", func(m *Model) { m.icons = true }},
		{"widen the indent", func(m *Model) { m.indent = 4 }},
		{"collapse the indent", func(m *Model) { m.indent = 0 }},
		{"show hidden entries", func(m *Model) {
			*m, _ = send(*m, key("."))
		}},
	}
	m := mounted(t, root, 40, 12)
	m.SetFocused(true)
	checkWidths(t, m, "initial")
	for _, s := range steps {
		s.do(&m)
		checkWidths(t, m, s.name)
	}
}
