package editor

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"ike/internal/editor/buffer"
	ilsp "ike/internal/lsp"
	"ike/internal/vcs"
)

// conflictText is a plain two-sided conflict framed by context lines.
const conflictText = "top\n" +
	"<<<<<<< HEAD\n" +
	"ours1\n" +
	"ours2\n" +
	"=======\n" +
	"theirs1\n" +
	">>>>>>> feature\n" +
	"bottom\n"

// diff3Text carries the optional ||||||| base section.
const diff3Text = "a\n" +
	"<<<<<<< HEAD\n" +
	"ours\n" +
	"||||||| merged common ancestors\n" +
	"base\n" +
	"=======\n" +
	"theirs\n" +
	">>>>>>> feature\n" +
	"z\n"

// --- detection --------------------------------------------------------------

func TestScanConflictsBasic(t *testing.T) {
	blocks := scanConflicts(strings.Split(strings.TrimSuffix(conflictText, "\n"), "\n"))
	if len(blocks) != 1 {
		t.Fatalf("blocks = %d, want 1", len(blocks))
	}
	c := blocks[0]
	if c.start != 1 || c.sep != 4 || c.end != 6 || c.base != -1 {
		t.Fatalf("block = %+v, want start 1 sep 4 end 6 base -1", c)
	}
}

func TestScanConflictsBaseSection(t *testing.T) {
	blocks := scanConflicts(strings.Split(strings.TrimSuffix(diff3Text, "\n"), "\n"))
	if len(blocks) != 1 {
		t.Fatalf("blocks = %d, want 1", len(blocks))
	}
	c := blocks[0]
	if c.start != 1 || c.base != 3 || c.sep != 5 || c.end != 7 {
		t.Fatalf("block = %+v, want start 1 base 3 sep 5 end 7", c)
	}
	if c.oursEnd() != 3 {
		t.Fatalf("oursEnd = %d, want 3 (the base marker)", c.oursEnd())
	}
}

// TestScanConflictsIgnoresLookalikes: an 8-rune run (a markdown heading
// underline, a decorative `<<<<<<<<`), markers out of order, and a truncated
// block must produce no blocks.
func TestScanConflictsIgnoresLookalikes(t *testing.T) {
	cases := [][]string{
		{"title", "========", "text"},                   // 8 equals: not a separator
		{"<<<<<<<<", "=======", ">>>>>>> x"},            // 8-rune opener: no block start
		{"=======", ">>>>>>> x"},                        // separator without opener
		{"<<<<<<< HEAD", "ours", "======="},             // truncated at EOF
		{"<<<<<<< HEAD", "ours", ">>>>>>> x"},           // closer before separator
		{"a", "||||||| b", "=======", ">>>>>>> x", "c"}, // base without opener
	}
	for i, lines := range cases {
		if got := scanConflicts(lines); len(got) != 0 {
			t.Fatalf("case %d: blocks = %v, want none", i, got)
		}
	}
}

// TestScanConflictsRestartsOnSecondOpener: a half block abandoned by a fresh
// `<<<<<<<` yields only the completed block.
func TestScanConflictsRestartsOnSecondOpener(t *testing.T) {
	lines := []string{
		"<<<<<<< stale", "junk",
		"<<<<<<< HEAD", "ours", "=======", "theirs", ">>>>>>> b",
	}
	blocks := scanConflicts(lines)
	if len(blocks) != 1 || blocks[0].start != 2 {
		t.Fatalf("blocks = %v, want one starting at line 2", blocks)
	}
}

// TestConflictCachePerVersion guards the testmarks-style caching (#1150
// pattern): repeated queries at one document version reuse the scan (epoch
// stable); an edit bumps the version and the next query rescans (epoch moves).
func TestConflictCachePerVersion(t *testing.T) {
	m, _ := loaded(t, conflictText)
	if len(m.conflicts()) != 1 {
		t.Fatal("setup: conflict not detected")
	}
	e1 := m.conflictsEpoch()
	if e2 := m.conflictsEpoch(); e2 != e1 {
		t.Fatalf("epoch moved without an edit: %d -> %d", e1, e2)
	}
	m = send(m, key('x')) // delete one rune: docVersion bumps
	if e3 := m.conflictsEpoch(); e3 == e1 {
		t.Fatal("epoch must move after an edit")
	}
}

// --- roles ------------------------------------------------------------------

func TestConflictRoleOf(t *testing.T) {
	m, _ := loaded(t, diff3Text)
	want := map[int]conflictRole{
		0: conflictNone,
		1: conflictMarker, // <<<<<<<
		2: conflictOurs,
		3: conflictMarker, // |||||||
		4: conflictBase,
		5: conflictMarker, // =======
		6: conflictTheirs,
		7: conflictMarker, // >>>>>>>
		8: conflictNone,
	}
	for line, role := range want {
		if got := m.conflictRoleOf(line); got != role {
			t.Fatalf("line %d role = %d, want %d", line, got, role)
		}
	}
}

func TestConflictAtCursor(t *testing.T) {
	m, _ := loaded(t, conflictText)
	if m.ConflictAtCursor() {
		t.Fatal("line 0 is outside the block")
	}
	m.SetCursor(3, 0)
	if !m.ConflictAtCursor() {
		t.Fatal("line 3 is inside the block")
	}
}

// --- accepts ----------------------------------------------------------------

// acceptOn loads text, puts the cursor on line, runs the accept action and
// returns the model.
func acceptOn(t *testing.T, text string, line int, action string) Model {
	t.Helper()
	m, _ := loaded(t, text)
	m.SetCursor(line, 0)
	m, _ = m.Update(ActionMsg{Action: action})
	return m
}

func TestAcceptOurs(t *testing.T) {
	m := acceptOn(t, conflictText, 2, "merge_accept_ours")
	if got := m.Text(); got != "top\nours1\nours2\nbottom" {
		t.Fatalf("text = %q", got)
	}
	if m.cursor.Line != 1 {
		t.Fatalf("cursor line = %d, want 1 (block start)", m.cursor.Line)
	}
	if !m.Dirty() {
		t.Fatal("accept must mark the buffer dirty")
	}
}

func TestAcceptTheirs(t *testing.T) {
	m := acceptOn(t, conflictText, 6, "merge_accept_theirs")
	if got := m.Text(); got != "top\ntheirs1\nbottom" {
		t.Fatalf("text = %q", got)
	}
	if m.cursor.Line != 1 {
		t.Fatalf("cursor line = %d, want 1", m.cursor.Line)
	}
}

func TestAcceptBoth(t *testing.T) {
	m := acceptOn(t, conflictText, 4, "merge_accept_both")
	if got := m.Text(); got != "top\nours1\nours2\ntheirs1\nbottom" {
		t.Fatalf("text = %q", got)
	}
}

// TestAcceptDropsBaseSection: the diff3 base is never kept, whichever side is
// accepted.
func TestAcceptDropsBaseSection(t *testing.T) {
	m := acceptOn(t, diff3Text, 4, "merge_accept_both")
	if got := m.Text(); got != "a\nours\ntheirs\nz" {
		t.Fatalf("text = %q", got)
	}
}

// TestAcceptSingleUndo guards the one-undo-unit requirement: a single `u`
// restores the whole conflict block.
func TestAcceptSingleUndo(t *testing.T) {
	m := acceptOn(t, conflictText, 2, "merge_accept_ours")
	m = send(m, key('u'))
	if got := m.Text(); got != strings.TrimSuffix(conflictText, "\n") {
		t.Fatalf("one undo must restore the block, got %q", got)
	}
}

// TestAcceptOutsideConflictNotices: no conflict under the cursor leaves the
// buffer untouched and answers with the ex-line notice command.
func TestAcceptOutsideConflictNotices(t *testing.T) {
	m, _ := loaded(t, conflictText)
	m.SetCursor(0, 0)
	m, cmd := m.Update(ActionMsg{Action: "merge_accept_ours"})
	if m.Text() != strings.TrimSuffix(conflictText, "\n") {
		t.Fatal("buffer must stay untouched outside a conflict")
	}
	if cmd == nil {
		t.Fatal("expected the no-conflict notice command")
	}
	if m.Dirty() {
		t.Fatal("nothing changed; the buffer must stay clean")
	}
}

// TestAcceptEmptySideAtEOF: a block at EOF whose kept side is empty vanishes
// without leaving a blank line behind.
func TestAcceptEmptySideAtEOF(t *testing.T) {
	text := "keep\n<<<<<<< HEAD\n=======\ngone\n>>>>>>> b"
	m, _ := loaded(t, text)
	m.SetCursor(2, 0)
	m, _ = m.Update(ActionMsg{Action: "merge_accept_ours"})
	if got := m.Text(); got != "keep" {
		t.Fatalf("text = %q, want %q", got, "keep")
	}
}

// --- navigation -------------------------------------------------------------

// twoConflicts holds two blocks, starting at lines 1 and 8.
const twoConflicts = "l0\n" +
	"<<<<<<< a\no1\n=======\nt1\n>>>>>>> b\n" + // 1..5
	"l6\nl7\n" +
	"<<<<<<< a\no2\n=======\nt2\n>>>>>>> b\n" + // 8..12
	"l13\n"

func TestConflictJumpNextPrevAndWrap(t *testing.T) {
	m, _ := loaded(t, twoConflicts)
	m, _ = m.Update(ActionMsg{Action: "merge_next_conflict"})
	if m.cursor.Line != 1 {
		t.Fatalf("first next lands on %d, want 1", m.cursor.Line)
	}
	m, _ = m.Update(ActionMsg{Action: "merge_next_conflict"})
	if m.cursor.Line != 8 {
		t.Fatalf("second next lands on %d, want 8", m.cursor.Line)
	}
	// Past the last block: wrap to the first.
	m, _ = m.Update(ActionMsg{Action: "merge_next_conflict"})
	if m.cursor.Line != 1 {
		t.Fatalf("wrap next lands on %d, want 1", m.cursor.Line)
	}
	// Standing on the first block's start: prev wraps to the last.
	m, _ = m.Update(ActionMsg{Action: "merge_prev_conflict"})
	if m.cursor.Line != 8 {
		t.Fatalf("wrap prev lands on %d, want 8", m.cursor.Line)
	}
}

func TestConflictJumpNoConflicts(t *testing.T) {
	m, _ := loaded(t, "just\ntext\n")
	m, cmd := m.Update(ActionMsg{Action: "merge_next_conflict"})
	if cmd == nil {
		t.Fatal("expected the no-conflicts notice command")
	}
	if m.cursor.Line != 0 {
		t.Fatal("cursor must not move without conflicts")
	}
}

// --- rendering --------------------------------------------------------------

// TestConflictRenderInvalidation guards the line-cache discipline (#614): the
// marker renders while the block exists, and an accept — which changes the
// document version through Update, bumping the render epoch — drops the cached
// bodies so the next frame shows the resolved text.
func TestConflictRenderInvalidation(t *testing.T) {
	m, _ := loaded(t, conflictText)
	before := ansi.Strip(m.View())
	if !strings.Contains(before, "<<<<<<<") {
		t.Fatal("marker line missing from the initial render")
	}
	m.SetCursor(2, 0)
	m, _ = m.Update(ActionMsg{Action: "merge_accept_ours"})
	after := ansi.Strip(m.View())
	if strings.Contains(after, "<<<<<<<") || strings.Contains(after, "=======") {
		t.Fatalf("stale conflict chrome after accept:\n%s", after)
	}
	if !strings.Contains(after, "ours1") {
		t.Fatal("kept side missing after accept")
	}
}

// TestConflictSectionsStyled: the ours/theirs sections carry a background
// tint and the marker lines render styled (dim bold) rather than plain.
func TestConflictSectionsStyled(t *testing.T) {
	m, _ := loaded(t, conflictText)
	m.SetCursor(0, 0)
	out := m.View()
	rows := strings.Split(out, "\n")
	if len(rows) < 7 {
		t.Fatalf("short render: %d rows", len(rows))
	}
	// Row 2 is "ours1": a background tint means an SGR 48;2 sequence.
	if !strings.Contains(rows[2], "48;2;") {
		t.Fatalf("ours row lacks a background tint: %q", rows[2])
	}
	if !strings.Contains(rows[5], "48;2;") {
		t.Fatalf("theirs row lacks a background tint: %q", rows[5])
	}
	// The ours and theirs tints must differ (VCSAdded- vs VCSModified-mixed).
	if oursBG, theirsBG := bgOf(rows[2]), bgOf(rows[5]); oursBG == theirsBG {
		t.Fatalf("ours and theirs tint identically: %q", oursBG)
	}
	// Marker rows carry styling (faint bold), so escapes are present.
	if !strings.Contains(rows[1], "\x1b[") {
		t.Fatalf("marker row renders unstyled: %q", rows[1])
	}
}

// bgOf extracts the first 48;2;R;G;B background sequence of a rendered row.
func bgOf(row string) string {
	i := strings.Index(row, "48;2;")
	if i < 0 {
		return ""
	}
	j := strings.IndexByte(row[i:], 'm')
	if j < 0 {
		return row[i:]
	}
	return row[i : i+j]
}

// --- scrollbar --------------------------------------------------------------

// conflictSbEditor builds a 200-line buffer in a 10-row pane with one conflict
// block at lines 100..104.
func conflictSbEditor(t *testing.T) Model {
	t.Helper()
	var b strings.Builder
	for i := 0; i < 200; i++ {
		switch i {
		case 100:
			b.WriteString("<<<<<<< HEAD\n")
		case 101:
			b.WriteString("ours\n")
		case 102:
			b.WriteString("=======\n")
		case 103:
			b.WriteString("theirs\n")
		case 104:
			b.WriteString(">>>>>>> feature\n")
		default:
			b.WriteString("line\n")
		}
	}
	m, _ := loaded(t, b.String())
	m.SetSize(20, 10)
	return m
}

// TestScrollbarConflictMarks guards the overview-ruler source (#1131
// mechanism): conflict rows mark in the stripe, click-to-jump targets the
// block start, conflicts outrank git on a shared cell, and diagnostics
// outrank conflicts.
func TestScrollbarConflictMarks(t *testing.T) {
	m := conflictSbEditor(t)
	track, total, _, _, ok := m.scrollbarGeometry()
	if !ok {
		t.Fatal("setup: no scrollbar")
	}
	y := 100 * track / total
	// A git mark inside the block shares the row; the conflict claims the
	// click target (block start), outranking the git line.
	m.gitMarks = map[int]vcs.LineMark{102: vcs.LineChanged}
	m.marksEpoch++
	_, _, conf, lines := m.stripesFor(track, total)
	if !conf[y] {
		t.Fatalf("row %d must carry a conflict mark", y)
	}
	if lines[y] != 100 {
		t.Fatalf("click line for row %d = %d, want 100 (block start over git)", y, lines[y])
	}
	// The overlay draws the conflict glyph outside the thumb.
	rows := make([]string, track)
	for i := range rows {
		rows[i] = "x"
	}
	out := strings.Join(m.overlayScrollbar(rows), "\n")
	if !strings.Contains(out, "◆") {
		t.Fatal("conflict glyph missing from the overlay")
	}
	// Diagnostics outrank conflicts on the shared cell.
	m.setDiagnostics([]ilsp.Diagnostic{{
		Range: buffer.Range{Start: buffer.Position{Line: 100}}, Severity: 1, Message: "boom",
	}})
	_, _, _, lines = m.stripesFor(track, total)
	if lines[y] != 100 {
		t.Fatalf("diag line for row %d = %d, want 100", y, lines[y])
	}
	rows2 := make([]string, track)
	for i := range rows2 {
		rows2[i] = "x"
	}
	out2 := m.overlayScrollbar(rows2)
	if !strings.Contains(out2[y], "■") {
		t.Fatalf("diagnostic must win the shared cell, got %q", out2[y])
	}
}

// TestScrollbarConflictClickJumps: pressing the marked track cell centres the
// viewport on the block start (#1131 click-to-jump).
func TestScrollbarConflictClickJumps(t *testing.T) {
	m := conflictSbEditor(t)
	track, total, _, _, _ := m.scrollbarGeometry()
	y := 100 * track / total
	if drag := m.ScrollbarPress(y); drag {
		t.Fatal("mark press must not start a drag")
	}
	want := clampInt(100-track/2, 0, total-track)
	if m.view.Top != want {
		t.Fatalf("view.Top = %d, want %d (centred on the block)", m.view.Top, want)
	}
}

// TestScrollbarConflictEpochInvalidation: resolving the block invalidates the
// stripe memo through the conflicts epoch — no diag/git epoch moves.
func TestScrollbarConflictEpochInvalidation(t *testing.T) {
	m := conflictSbEditor(t)
	track, total, _, _, _ := m.scrollbarGeometry()
	if _, _, conf, _ := m.stripesFor(track, total); len(conf) == 0 {
		t.Fatal("setup: no conflict marks")
	}
	m.SetCursor(101, 0)
	m, _ = m.Update(ActionMsg{Action: "merge_accept_ours"})
	track, total, _, _, _ = m.scrollbarGeometry()
	if _, _, conf, _ := m.stripesFor(track, total); len(conf) != 0 {
		t.Fatal("stale conflict marks after the block was resolved")
	}
}
