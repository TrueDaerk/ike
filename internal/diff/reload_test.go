package diff

import (
	"strconv"
	"strings"
	"testing"
)

// lines builds a text of n numbered lines, with the 1-based line at replaced
// by replace — a document long enough to scroll and to collapse.
func lines(n, at int, replace string) string {
	var b strings.Builder
	for i := 1; i <= n; i++ {
		if i == at {
			b.WriteString(replace)
		} else {
			b.WriteString("line ")
			b.WriteString(strconv.Itoa(i))
		}
		b.WriteByte('\n')
	}
	return b.String()
}

// TestReloadContentsKeepsScrollAndHunk guards #2506: a file diff that follows
// its files on disk re-diffs in place — the reader is not thrown back to the
// top of the document, and the current hunk survives.
func TestReloadContentsKeepsScrollAndHunk(t *testing.T) {
	left := lines(200, 10, "OLD")
	right := lines(200, 10, "NEW")
	m := NewFiles("d1", "/tmp/l.txt", "/tmp/r.txt", nil)
	m.SetContext(-1) // no collapsing: visual lines map 1:1 onto rows
	m.SetSize(80, 20)
	m.SetContents(left, right)
	m.StepHunk(1)
	m.ScrollBy(60)
	top, cur := m.top, m.cur
	if top == 0 || cur != 0 {
		t.Fatalf("setup: top=%d cur=%d", top, cur)
	}

	// The right file gained a line far below the view.
	m.ReloadContents(left, right+"appended\n")
	if m.top != top {
		t.Fatalf("scroll offset = %d, want the retained %d", m.top, top)
	}
	if m.cur != cur {
		t.Fatalf("current hunk = %d, want the retained %d", m.cur, cur)
	}
	if !strings.Contains(m.rightText, "appended") {
		t.Fatal("the new right text did not land")
	}
	if m.HunkCount() != 2 {
		t.Fatalf("hunks = %d, want 2 (the changed line plus the appended one)", m.HunkCount())
	}
}

// TestReloadContentsClampsToShorterDocument guards the "as far as the new
// content allows" half of #2506: content that shrank under the reader scrolls
// up to the new end and clamps the hunk instead of pointing past it.
func TestReloadContentsClampsToShorterDocument(t *testing.T) {
	m := NewFiles("d1", "/tmp/l.txt", "/tmp/r.txt", nil)
	m.SetContext(-1)
	m.SetSize(80, 10)
	m.SetContents(lines(200, 5, "OLD"), lines(200, 5, "NEW"))
	m.StepHunk(1)
	m.ScrollBy(150)
	if m.top == 0 {
		t.Fatal("setup: expected a scrolled view")
	}
	m.ReloadContents("a\nb\nc\n", "a\nb\nc\n")
	if m.top != 0 {
		t.Fatalf("top = %d, want 0 for a three-line document", m.top)
	}
	if m.cur != -1 {
		t.Fatalf("current hunk = %d, want -1 with no hunks left", m.cur)
	}
}

// TestReloadContentsIdenticalIsNoOp guards the cheap path (#2506): a watcher
// event for a file whose bytes did not change disturbs nothing.
func TestReloadContentsIdenticalIsNoOp(t *testing.T) {
	m := NewFiles("d1", "/tmp/l.txt", "/tmp/r.txt", nil)
	m.SetContext(-1)
	m.SetSize(80, 10)
	m.SetContents(lines(100, 5, "OLD"), lines(100, 5, "NEW"))
	m.ScrollBy(30)
	top := m.top
	m.ReloadContents(m.leftText, m.rightText)
	if m.top != top {
		t.Fatalf("an unchanged reload moved the view: %d → %d", top, m.top)
	}
}

// TestReloadContentsKeepsExpandedGaps guards the gap half of #2506: a
// collapsed run the reader expanded stays expanded across a reload that
// leaves it in place.
func TestReloadContentsKeepsExpandedGaps(t *testing.T) {
	left := lines(120, 5, "OLD")
	right := lines(120, 5, "NEW")
	m := NewFiles("d1", "/tmp/l.txt", "/tmp/r.txt", nil)
	m.SetSize(80, 20)
	m.SetContents(left, right)
	if len(m.gaps) == 0 {
		t.Fatal("setup: expected a collapsed run")
	}
	m.gaps[0].expanded = true
	m.render()

	// A change at the very end leaves the gap's first hidden line untouched.
	m.ReloadContents(left, right+"tail\n")
	if len(m.gaps) == 0 || !m.gaps[0].expanded {
		t.Fatalf("the expanded gap folded again: %+v", m.gaps)
	}
}

// TestNoticeClaimsFooterRow guards the removed-file report (#2506): the
// notice takes the pane's last row — the row the search prompt uses — and
// clearing it gives the row back to the diff body.
func TestNoticeClaimsFooterRow(t *testing.T) {
	m := NewFiles("d1", "/tmp/l.txt", "/tmp/r.txt", nil)
	m.SetContext(-1)
	m.SetSize(40, 6)
	m.SetContents("a\nb\nc\nd\ne\nf\ng\n", "a\nb\nc\nd\ne\nf\ng\n")
	if m.viewHeight() != 6 {
		t.Fatalf("without a notice the body is the whole pane, got %d", m.viewHeight())
	}
	m.SetNotice("left file removed")
	if m.Notice() != "left file removed" {
		t.Fatalf("Notice = %q", m.Notice())
	}
	if m.viewHeight() != 5 {
		t.Fatalf("the notice must claim one row, body = %d", m.viewHeight())
	}
	if got := m.View(); !strings.Contains(got, "left file removed") {
		t.Fatalf("the notice is not rendered:\n%s", got)
	}
	m.SetNotice("")
	if m.viewHeight() != 6 {
		t.Fatalf("clearing the notice must give the row back, body = %d", m.viewHeight())
	}
	if got := m.View(); strings.Contains(got, "left file removed") {
		t.Fatalf("the cleared notice still renders:\n%s", got)
	}
}
