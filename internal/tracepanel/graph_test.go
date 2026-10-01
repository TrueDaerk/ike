package tracepanel

import (
	"strings"
	"testing"
	"time"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"ike/internal/agenttrace"
)

// graph_test.go covers the graph view (#2858): the snake layout at several
// widths, the rendered boxes and connectors, the path keys, click /
// double-click / wheel, selection and expansion surviving a live append,
// the newest-turn auto-scroll and the tree toggle.

// graphPanel is a panel in graph view showing the fixture's path.
func graphPanel(t *testing.T, whole bool, w, h int) (*Model, *agenttrace.Session) {
	t.Helper()
	m := panel(t)
	m.SetSize(w, h)
	s := fixture(t, whole)
	m.Set(agenttrace.BuildTree(s), info(s))
	m.SetPath(agenttrace.BuildPath(s))
	m.SetViewMode(ViewGraph)
	return m, s
}

// widthsFor is the slot width list of n boxes with a separator at sep (-1
// for none) at pane width w.
func widthsFor(n, sep, w int) []int {
	bw, _ := BoxWidth(w)
	out := make([]int, n)
	for i := range out {
		out[i] = bw
		if i == sep {
			out[i] = sepW
		}
	}
	return out
}

func TestSnakeLayoutBoustrophedon(t *testing.T) {
	for _, w := range []int{40, 80, 120} {
		widths := widthsFor(12, 5, w)
		l := Snake(widths, nil, w, -1, 0)
		if l.Single {
			t.Fatalf("width %d must not be a single column", w)
		}
		bw, _ := BoxWidth(w)
		if l.BoxW != bw || bw < minBoxW || bw > maxBoxW {
			t.Fatalf("width %d: box width %d", w, l.BoxW)
		}
		perRow := map[int][]Slot{}
		for i, s := range l.Slots {
			if s.X < 0 || s.X+s.W > w {
				t.Fatalf("width %d: slot %d (%+v) is cut at the border", w, i, s)
			}
			if s.H != boxH || (s.W != bw && s.W != sepW) {
				t.Fatalf("width %d: slot %d = %+v", w, i, s)
			}
			perRow[s.Row] = append(perRow[s.Row], s)
		}
		if len(perRow) < 2 {
			t.Fatalf("width %d: %d boxes fit one row", w, len(widths))
		}
		for r := 0; r < len(perRow); r++ {
			row := perRow[r]
			wantDir := 1
			if r%2 == 1 {
				wantDir = -1
			}
			for i, s := range row {
				if s.Dir != wantDir {
					t.Fatalf("width %d row %d: dir %d, want %d", w, r, s.Dir, wantDir)
				}
				if s.Y != r*(boxH+turnH) {
					t.Fatalf("width %d row %d: y %d", w, r, s.Y)
				}
				if i == 0 {
					continue
				}
				prev := row[i-1]
				// Boxes on a row are gapW apart, in the row's direction.
				if wantDir > 0 && s.X != prev.X+prev.W+gapW {
					t.Fatalf("width %d row %d: %+v does not follow %+v", w, r, s, prev)
				}
				if wantDir < 0 && s.X+s.W+gapW != prev.X {
					t.Fatalf("width %d row %d: %+v does not precede %+v", w, r, s, prev)
				}
			}
			if r == 0 {
				continue
			}
			// The row starts under the last box of the row before, sharing
			// the edge the path came from.
			last, first := perRow[r-1][len(perRow[r-1])-1], row[0]
			if wantDir < 0 && last.X+last.W != first.X+first.W {
				t.Fatalf("width %d row %d: right edges %d vs %d", w, r, last.X+last.W, first.X+first.W)
			}
			if wantDir > 0 && last.X != first.X {
				t.Fatalf("width %d row %d: left edges %d vs %d", w, r, last.X, first.X)
			}
		}
		if l.Height != len(perRow)*(boxH+turnH)-turnH {
			t.Fatalf("width %d: height %d for %d rows", w, l.Height, len(perRow))
		}
		// j/k neighbours are the nearest box on the next / previous row.
		if below := l.Below(0, nil); below < 0 || l.Slots[below].Row != 1 {
			t.Fatalf("width %d: below(0) = %d", w, below)
		}
		if above := l.Above(len(widths)-1, nil); above < 0 || l.Slots[above].Row != l.Slots[len(widths)-1].Row-1 {
			t.Fatalf("width %d: above(last) = %d", w, above)
		}
		if l.Above(0, nil) != -1 {
			t.Fatalf("width %d: nothing is above the first row", w)
		}
	}
}

func TestSnakeSingleColumnAndExpanded(t *testing.T) {
	// Narrower than two boxes: one column, full width, stacked.
	l := Snake(widthsFor(4, -1, 30), nil, 30, -1, 0)
	if !l.Single || l.BoxW != 30 {
		t.Fatalf("single column = %+v", l)
	}
	for i, s := range l.Slots {
		if s.X != 0 || s.W != 30 || s.Y != i*(boxH+turnH) || s.Row != i {
			t.Fatalf("slot %d = %+v", i, s)
		}
	}
	// An expanded box pushes the rows below it down by its detail height.
	l = Snake(widthsFor(6, -1, 40), nil, 40, 1, 3)
	if l.DetailY != boxH || l.DetailH != 3 {
		t.Fatalf("detail at %d/%d", l.DetailY, l.DetailH)
	}
	if l.Slots[2].Y != boxH+3+turnH || l.Slots[2].Row != 1 {
		t.Fatalf("row after the expanded box = %+v", l.Slots[2])
	}
	if l.At(l.Slots[2].X+1, l.Slots[2].Y+1) != 2 || l.At(0, boxH+1) != -1 {
		t.Fatal("hit test disagrees with the slots")
	}
	if BoxWidth(0); Snake(nil, nil, 40, -1, 0).Height != 0 {
		t.Fatal("empty path has no height")
	}
}

func TestGraphViewRendersBoxesAndConnectors(t *testing.T) {
	m, _ := graphPanel(t, true, 80, 30)
	view := plain(m.View())
	for _, want := range []string{"┌?", "│#1 Add a greeting to …│", "✎", "┌+", "hello.go", "✕", "notes.ipynb", "┌✓", "Done: main.go greets,", "──▶", "◀──", "▼", "◇", "edit :3 +1 −0", "create", "t tree"} {
		if !strings.Contains(view, want) {
			t.Errorf("graph lacks %q:\n%s", want, view)
		}
	}
	// Every row fits the pane.
	for i, l := range strings.Split(view, "\n") {
		if n := len([]rune(l)); n > 80 {
			t.Errorf("row %d is %d cells wide:\n%s", i, n, l)
		}
	}
	// The pane opens on the newest box (the answer of turn 2).
	if cur := m.CurrentStop(); cur == nil || cur.Key != "t2/end" || !m.graph.follow {
		t.Fatalf("selection = %+v", cur)
	}
	// Current() shows the box as a node for the host.
	if n := m.Current(); n == nil || n.Kind != agenttrace.NodeDecision || n.Event != 11 || n.Turn != 2 {
		t.Fatalf("current node = %+v", m.Current())
	}
	// The tree toggle keeps both views' state.
	send(m, "t")
	if m.ViewMode() != ViewTree || !strings.Contains(plain(m.View()), "t graph") {
		t.Fatal("t must switch to the tree")
	}
	send(m, "t")
	if m.ViewMode() != ViewGraph || m.CurrentStop().Key != "t2/end" {
		t.Fatal("t must switch back with the selection kept")
	}
}

func TestGraphKeysMoveOpenAndExpand(t *testing.T) {
	m, _ := graphPanel(t, true, 80, 30)
	// 9 stops at 80 cells (3 boxes per row): row 0 = t1, e4/f0, e5/f0;
	// row 1 (right-to-left) = e7/f0, e8/f0, t1/end; row 2 = t2, (sep), t2/end.
	send(m, "g")
	if m.CurrentStop().Key != "t1" {
		t.Fatalf("g → %s", m.CurrentStop().Key)
	}
	send(m, "l")
	send(m, "l")
	send(m, "l")
	if m.CurrentStop().Key != "e7/f0" {
		t.Fatalf("l l l → %s", m.CurrentStop().Key)
	}
	send(m, "h")
	if m.CurrentStop().Key != "e5/f0" {
		t.Fatalf("h → %s", m.CurrentStop().Key)
	}
	// j goes to the box directly below (e7/f0 sits under e5/f0), k back up.
	send(m, "j")
	if m.CurrentStop().Key != "e7/f0" {
		t.Fatalf("j → %s", m.CurrentStop().Key)
	}
	send(m, "j")
	if m.CurrentStop().Key != "t2/end" {
		t.Fatalf("j j → %s", m.CurrentStop().Key)
	}
	// k climbs to the nearest box of the row above: the notes.ipynb box
	// sits under e4/f0, not under e5/f0.
	send(m, "k")
	send(m, "k")
	if m.CurrentStop().Key != "e4/f0" {
		t.Fatalf("k k → %s", m.CurrentStop().Key)
	}
	send(m, "l")
	// Enter on a change box opens the file at the line (create: no line).
	msg, ok := send(m, "enter").(OpenLocationMsg)
	if !ok || msg.Path != "/Users/dev/src/proj/hello.go" || msg.Line != -1 {
		t.Fatalf("enter on create = %#v", msg)
	}
	// A second enter expands it in place; space collapses; only one box is
	// expanded at a time.
	if send(m, "enter") != nil || m.Expanded() != "e5/f0" {
		t.Fatalf("second enter must expand, expanded = %q", m.Expanded())
	}
	view := plain(m.View())
	for _, want := range []string{"Write · /Users/dev/src/proj/hello.go · ok", "hello.go · +1 −0", "space collapse", "┴"} {
		if !strings.Contains(view, want) {
			t.Errorf("detail block lacks %q:\n%s", want, view)
		}
	}
	send(m, "h")
	send(m, "space")
	if m.Expanded() != "e4/f0" {
		t.Fatalf("space must move the expansion, expanded = %q", m.Expanded())
	}
	if view = plain(m.View()); !strings.Contains(view, "Edit · /Users/dev/src/proj/main.go · ok") || !strings.Contains(view, "› I'll read main.go") || !strings.Contains(view, "main.go:3 · +1 −0") {
		t.Fatalf("detail block:\n%s", view)
	}
	send(m, "space")
	if m.Expanded() != "" {
		t.Fatal("space again must collapse")
	}
	// Space on a prompt does nothing; enter shows its text.
	send(m, "h")
	send(m, "space")
	if m.Expanded() != "" {
		t.Fatal("a prompt box has nothing to expand")
	}
	text, ok := send(m, "enter").(ShowTextMsg)
	if !ok || text.Title != "Prompt #1" || !strings.Contains(text.Text, "Add a greeting") {
		t.Fatalf("enter on the prompt = %#v", text)
	}
	send(m, "G")
	text, ok = send(m, "enter").(ShowTextMsg)
	if !ok || text.Title != "Answer #2" || text.Text != "Done: main.go greets, hello.go added." {
		t.Fatalf("enter on the answer = %#v", text)
	}
	// Separators are skipped along the path.
	send(m, "h")
	if m.CurrentStop().Key != "t2" {
		t.Fatalf("h over the separator → %s", m.CurrentStop().Key)
	}
}

func TestGraphLinkedBoxesDiffRevertAndSelect(t *testing.T) {
	m, s := graphPanel(t, false, 80, 30)
	nodes := m.Nodes()
	edit := findNode(nodes, "e4/f0")
	const target = "/Users/dev/src/proj/main.go"
	links := agenttrace.Link(nodes, []agenttrace.Change{{Path: target, First: edit.Until, Last: edit.Until, SourceKey: "term-1"}}, "term-1", s.CWD)
	m.SetLinks(links)
	if view := plain(m.View()); !strings.Contains(view, "Δ┐") {
		t.Fatalf("linked box has no mark:\n%s", view)
	}
	// Unlinked box: D and V stay silent.
	if msg := send(m, "D"); msg != nil {
		t.Fatalf("D on an unlinked box = %#v", msg)
	}
	// The feed's back-link selects by file node key — or by the tool key.
	if !m.Select("e4") || m.CurrentStop().Key != "e4/f0" {
		t.Fatalf("select landed on %+v", m.CurrentStop())
	}
	if m.Select("e999") || m.Select("e10") {
		t.Fatal("unknown or separator keys must not select")
	}
	if msg, ok := send(m, "D").(DiffMsg); !ok || msg.Linked != target || msg.Path != target {
		t.Fatalf("D = %#v", msg)
	}
	if msg, ok := send(m, "V").(ChangeRevertMsg); !ok || msg.Path != target {
		t.Fatalf("V = %#v", msg)
	}
	if _, ok := send(m, "a").(AskMsg); !ok {
		t.Fatal("a must ask")
	}
	if n := m.Current(); n == nil || n.Kind != agenttrace.NodeFile || n.Ref == nil || n.Ref.Line != 3 || n.Path != target || n.Label != "edit" {
		t.Fatalf("current node = %+v", n)
	}
}

func TestGraphLiveAppendKeepsSelectionAndExpansion(t *testing.T) {
	m, _ := graphPanel(t, false, 80, 30)
	// The running turn's answer is pending.
	if cur := m.CurrentStop(); cur.Key != "t1/end" || !cur.Pending {
		t.Fatalf("newest box = %+v", cur)
	}
	if view := plain(m.View()); !strings.Contains(view, "┌…") || !strings.Contains(view, "working …") {
		t.Fatalf("pending answer:\n%s", view)
	}
	// The user selects and expands the first edit.
	m.Select("e4/f0")
	send(m, "space")
	if m.graph.follow {
		t.Fatal("moving off the newest box must stop following")
	}
	whole := fixture(t, true)
	m.Set(agenttrace.BuildTree(whole), info(whole))
	m.SetPath(agenttrace.BuildPath(whole))
	if cur := m.CurrentStop(); cur == nil || cur.Key != "e4/f0" || m.Expanded() != "e4/f0" {
		t.Fatalf("append lost the state: sel=%+v expanded=%q", cur, m.Expanded())
	}
	if len(m.Path()) != 9 || m.Path()[5].Pending {
		t.Fatalf("path after append = %v", stopKeys(m.Path()))
	}
	// Back on the newest box, following resumes and the next append moves
	// the selection on.
	send(m, "G")
	if !m.graph.follow {
		t.Fatal("the newest box must resume following")
	}
	// An ended session settles a pending answer on SetPath.
	part := fixture(t, false)
	m.Reset()
	in := info(part)
	in.Ended = true
	m.Set(agenttrace.BuildTree(part), in)
	m.SetPath(agenttrace.BuildPath(part))
	if cur := m.CurrentStop(); cur.Pending || cur.Detail != "ended on a tool call" {
		t.Fatalf("ended session's answer = %+v", cur)
	}
}

func stopKeys(path []agenttrace.Stop) []string {
	out := make([]string, len(path))
	for i := range path {
		out[i] = path[i].Key
	}
	return out
}

// TestGraphAutoScrollsToNewestUnlessScrolledUp: a pane too short for the
// path shows the newest turn after every append; a wheel up parks the
// window until the user returns to the end.
func TestGraphAutoScrollsToNewestUnlessScrolledUp(t *testing.T) {
	m, _ := graphPanel(t, false, 40, 8) // header + 6 rows + hint; 2 boxes per row
	l := m.graphLayout()
	if l.Height <= m.treeHeight() {
		t.Fatalf("the path must overflow the body: %d rows", l.Height)
	}
	if m.GraphTop() != m.graphMaxTop(l) {
		t.Fatalf("top = %d, want the end %d", m.GraphTop(), m.graphMaxTop(l))
	}
	if view := plain(m.View()); !strings.Contains(view, "working …") {
		t.Fatalf("newest box not on screen:\n%s", view)
	}
	m.Wheel(-2)
	if m.graph.follow || m.GraphTop() != m.graphMaxTop(l)-2 {
		t.Fatalf("wheel up: follow=%v top=%d", m.graph.follow, m.GraphTop())
	}
	whole := fixture(t, true)
	m.Set(agenttrace.BuildTree(whole), info(whole))
	m.SetPath(agenttrace.BuildPath(whole))
	if m.GraphTop() != m.graphMaxTop(l)-2 || m.CurrentStop().Key != "t1/end" {
		t.Fatalf("append while scrolled up moved the window: top=%d sel=%s", m.GraphTop(), m.CurrentStop().Key)
	}
	// Wheeling back to the end resumes following only once the newest box
	// is selected again.
	m.Wheel(100)
	if m.graph.follow {
		t.Fatal("an old selection at the end must not follow")
	}
	send(m, "G")
	if !m.graph.follow || m.CurrentStop().Key != "t2/end" {
		t.Fatalf("G: follow=%v sel=%s", m.graph.follow, m.CurrentStop().Key)
	}
	// Moving the selection off screen scrolls it into view.
	send(m, "g")
	if m.GraphTop() != 0 {
		t.Fatalf("top = %d after g", m.GraphTop())
	}
	send(m, "pgdown")
	if m.GraphTop() == 0 {
		t.Fatal("pgdown must scroll")
	}
}

func TestGraphClickSelectsDoubleClickOpensWheelScrolls(t *testing.T) {
	m, _ := graphPanel(t, true, 80, 30)
	now := time.Unix(1000, 0)
	m.SetNow(func() time.Time { return now })
	l := m.graphLayout()
	// Click inside the second box (e4/f0) selects it.
	s := l.Slots[1]
	if cmd := m.Click(s.X+2, headerRows+s.Y+1); cmd != nil {
		t.Fatal("first click must only select")
	}
	if m.CurrentStop().Key != "e4/f0" {
		t.Fatalf("click selected %s", m.CurrentStop().Key)
	}
	now = now.Add(100 * time.Millisecond)
	cmd := m.Click(s.X+2, headerRows+s.Y+1)
	if cmd == nil {
		t.Fatal("double click yielded nothing")
	}
	if msg, ok := cmd().(OpenLocationMsg); !ok || msg.Line != 2 {
		t.Fatalf("double click = %#v", msg)
	}
	// A click on the connector between boxes, or on the hint row, selects
	// nothing new; a click on the separator marker neither.
	now = now.Add(time.Second)
	m.Click(s.X-2, headerRows+s.Y+1)
	m.Click(3, headerRows+m.treeHeight())
	sep := l.Slots[7]
	m.Click(sep.X+1, headerRows+sep.Y+1)
	if m.CurrentStop().Key != "e4/f0" {
		t.Fatalf("stray clicks moved the selection to %s", m.CurrentStop().Key)
	}
	// The hit test reads the drawn geometry: the box's label is on the row
	// and columns the slot says.
	row := []rune(plain(strings.Split(m.View(), "\n")[headerRows+s.Y+1]))
	if got := strings.TrimSpace(string(row[s.X+1 : s.X+s.W-1])); got != "main.go" {
		t.Fatalf("label at the slot = %q", got)
	}
	// The connector points at the label row, not the detail row (#2872).
	if row[s.X-1] != '▶' {
		t.Fatalf("connector on the label row = %q", string(row))
	}
	// The wheel scrolls once the path is taller than the pane.
	m.SetSize(40, 9)
	m.Wheel(1)
	if m.GraphTop() != 1 {
		t.Fatalf("wheel top = %d", m.GraphTop())
	}
	// Clicks land on the scrolled geometry.
	l = m.graphLayout()
	s = l.Slots[2]
	m.Click(s.X+1, headerRows+s.Y+1-m.GraphTop())
	if m.CurrentStop().Key != "e5/f0" {
		t.Fatalf("scrolled click selected %s", m.CurrentStop().Key)
	}
}

func TestGraphThemesTellKindsApartByGlyph(t *testing.T) {
	m, _ := graphPanel(t, true, 120, 30)
	view := plain(m.View())
	for _, glyph := range []string{"┌?", "┌✎", "┌+", "┌✕", "┌✓"} {
		if !strings.Contains(view, glyph) {
			t.Errorf("glyph %q missing without colour:\n%s", glyph, view)
		}
	}
	// A failed call's box carries ✗.
	path := []agenttrace.Stop{
		{Kind: agenttrace.StopPrompt, Key: "t1", Turn: 1, Label: "#1 go"},
		{Kind: agenttrace.StopChange, Key: "e2/f0", Turn: 1, Label: "x.go", Detail: "edit ✗", Error: true, Ref: &agenttrace.FileRef{Path: "/p/x.go", Op: agenttrace.OpEdit}},
		{Kind: agenttrace.StopAnswer, Key: "t1/end", Turn: 1, Label: "done"},
	}
	m.SetPath(path)
	if view = plain(m.View()); !strings.Contains(view, "┌✗") {
		t.Fatalf("failed call glyph missing:\n%s", view)
	}
}

func TestSnakeBreaksStartNewRowWithRule(t *testing.T) {
	// Six boxes fit one row at 160 cells; a break at 3 forces a turn.
	const w = 160
	l := Snake(widthsFor(6, -1, w), []int{0, 3}, w, -1, 0)
	if l.Slots[2].Row != 0 || l.Slots[3].Row != 1 || l.Slots[4].Row != 1 {
		t.Fatalf("rows = %+v", l.Slots)
	}
	if l.Slots[0].Break || !l.Slots[3].Break || l.Slots[4].Break {
		t.Fatal("only the forced turn is a break; a break at 0 is ignored")
	}
	// The break leaves room for the "│", the rule and the "▼", and the
	// snake turns as before: right-to-left, under the last box.
	prev, s := l.Slots[2], l.Slots[3]
	if s.Y != boxH+breakH || s.Dir != -1 || s.X+s.W != prev.X+prev.W {
		t.Fatalf("break slot = %+v after %+v", s, prev)
	}
	if n := l.Slots[4]; n.X+n.W+gapW != s.X {
		t.Fatalf("the row after a break continues leftwards: %+v", n)
	}
	// Nothing on the gap rows is clickable; j/k cross the break.
	for y := boxH; y < s.Y; y++ {
		if l.At(s.X+1, y) != -1 {
			t.Fatalf("gap row %d hits a box", y)
		}
	}
	if l.Below(2, nil) != 3 || l.Above(3, nil) != 2 {
		t.Fatalf("below(2)=%d above(3)=%d", l.Below(2, nil), l.Above(3, nil))
	}
	if l.Height != 2*boxH+breakH {
		t.Fatalf("height = %d", l.Height)
	}
	// A single column takes the break's extra row too.
	l = Snake(widthsFor(3, -1, 30), []int{2}, 30, -1, 0)
	if l.Slots[1].Y != boxH+turnH || l.Slots[2].Y != 2*boxH+turnH+breakH || !l.Slots[2].Break {
		t.Fatalf("single column with a break = %+v", l.Slots)
	}
}

func TestGraphNewQuestionBreaksTheRow(t *testing.T) {
	// 200 cells hold seven boxes: turn 2's prompt would fit on the first
	// row, but a new question starts a new one under a "── #2 ──" rule.
	m, _ := graphPanel(t, true, 200, 30)
	l := m.graphLayout()
	p2 := m.stopIndex("t2")
	if p2 < 1 {
		t.Fatal("no prompt for turn 2")
	}
	s, prev := l.Slots[p2], l.Slots[p2-1]
	if !s.Break || s.Row != prev.Row+1 {
		t.Fatalf("prompt #2 = %+v after %+v", s, prev)
	}
	for i := 0; i < p2; i++ {
		if l.Slots[i].Row != 0 || l.Slots[i].Break {
			t.Fatalf("turn 1 slot %d = %+v", i, l.Slots[i])
		}
	}
	rows := strings.Split(plain(m.View()), "\n")
	rule := []rune(rows[headerRows+s.Y-2])
	if !strings.HasPrefix(string(rule), "── #2 ──") || len(rule) != 200 {
		t.Fatalf("turn rule = %q", string(rule))
	}
	x := s.CenterX()
	if rule[x] != '┼' || []rune(rows[headerRows+s.Y-3])[x] != '│' || []rune(rows[headerRows+s.Y-1])[x] != '▼' {
		t.Fatalf("the answer must connect down across the rule at column %d:\n%s", x, strings.Join(rows[headerRows+s.Y-3:headerRows+s.Y], "\n"))
	}
	// Selecting the prompt keeps its rule on screen.
	m.SetSize(200, 8)
	if !m.graphSelect("t2") || m.GraphTop() > s.Y-2 {
		t.Fatalf("top %d hides the rule at %d", m.GraphTop(), s.Y-2)
	}
}

func TestGraphTwoRowsFitATwelveRowPane(t *testing.T) {
	for _, w := range []int{40, 80, 120} {
		m, _ := graphPanel(t, true, w, 12)
		send(m, "g")
		l := m.graphLayout()
		var second *Slot
		for i := range l.Slots {
			if l.Slots[i].Row == 1 {
				second = &l.Slots[i]
				break
			}
		}
		if second == nil {
			t.Fatalf("width %d: one row only", w)
		}
		if gap := second.Y - boxH; gap <= 1 {
			t.Fatalf("width %d: rows %d apart, want more breathing room", w, gap)
		}
		if m.GraphTop() != 0 || second.Y+boxH > m.treeHeight() {
			t.Fatalf("width %d: second row ends at %d, body is %d", w, second.Y+boxH, m.treeHeight())
		}
		// A click on the second row's box lands on it.
		m.Click(second.X+1, headerRows+second.Y+1)
		if cur := m.CurrentStop(); cur == nil || m.stopIndex(cur.Key) != l.At(second.X+1, second.Y+1) {
			t.Fatalf("width %d: click selected %+v", w, cur)
		}
	}
}

func TestGraphBoxFrameClosedAtEveryWidth(t *testing.T) {
	widths := []int{4, 6, 7, 8, 12}
	for w := minBoxW; w <= maxBoxW; w++ {
		widths = append(widths, w)
	}
	widths = append(widths, 30, 37, 60) // single-column full width
	details := []string{
		"", "16:00", "+12 −3", "✗ error", "…", "create +1 −0",
		"edit :123 ×2 +12 −3 ✗ error", "working …", "ended on a tool call",
		"編集した変更の詳細", "x編集した変更の詳細",
	}
	m := panel(t)
	styles := m.graphStyles(m.theme())
	corners := [boxH][2]string{{"┌", "┐"}, {"│", "│"}, {"│", "│"}, {"└", "┘"}}
	for _, w := range widths {
		for _, d := range details {
			for _, variant := range []string{"plain", "linked", "expanded", "selected"} {
				st := agenttrace.Stop{Kind: agenttrace.StopChange, Key: "e1/f0", Label: "a very long file name.go", Detail: d,
					Ref: &agenttrace.FileRef{Path: "main.go", Op: agenttrace.OpEdit}}
				m.links = agenttrace.Links{}
				m.graph.expanded, m.graph.sel = "", ""
				switch variant {
				case "linked":
					m.links = agenttrace.Links{ByNode: map[string]string{st.Key: "/p/main.go"}}
				case "expanded":
					m.graph.expanded = st.Key
				case "selected":
					m.graph.sel = st.Key
				}
				const x0 = 2
				c := newCanvas(w+2*x0, boxH)
				m.drawBox(c, st, Slot{X: x0, W: w, H: boxH}, 0)
				name := variant + "/" + d + "/w" + itoa(w)
				rows := c.lines(0, boxH, styles)
				for y, row := range rows {
					if got := ansi.StringWidth(row); got != c.w {
						t.Fatalf("%s: row %d is %d cells, want %d", name, y, got, c.w)
					}
					if c.cells[y][x0].ch != corners[y][0] || c.cells[y][x0+w-1].ch != corners[y][1] {
						t.Fatalf("%s: row %d frame %q…%q", name, y, c.cells[y][x0].ch, c.cells[y][x0+w-1].ch)
					}
				}
				// The bottom border is a solid run (#2872): no text in it, only
				// the expanded box's "┴" at the left.
				bottom := c.cells[3]
				for x := x0 + 1; x < x0+w-1; x++ {
					want := "─"
					if variant == "expanded" && x == x0+1 {
						want = "┴"
					}
					if bottom[x].ch != want {
						t.Fatalf("%s: bottom border %q at %d, want %q", name, plain(rows[3]), x-x0, want)
					}
				}
				// The detail sits on the third row, left-aligned and cut to
				// the inner width like the label.
				var inner strings.Builder
				for x := x0 + 1; x < x0+w-1; x++ {
					inner.WriteString(c.cells[2][x].ch)
				}
				want := fitCells(d, w-2)
				want += strings.Repeat(" ", w-2-ansi.StringWidth(want))
				if inner.String() != want {
					t.Fatalf("%s: detail row %q, want %q", name, inner.String(), want)
				}
				// The selection covers both content rows.
				if variant == "selected" {
					for _, y := range []int{1, 2} {
						if c.cells[y][x0+1].st != stSelectedMuted && c.cells[y][x0+1].st != stSelected {
							t.Fatalf("%s: row %d not highlighted", name, y)
						}
					}
				} else if d != "" && c.cells[2][x0+1].st != stFaint {
					t.Fatalf("%s: detail not faint", name)
				}
			}
		}
	}
}

func TestCanvasWideRunes(t *testing.T) {
	c := newCanvas(5, 1)
	// A wide rune that would straddle the right edge is left out.
	c.put(2, 0, "ab編", stPlain, 0)
	c.put(0, 0, "編", stPlain, 0)
	// Overwriting either half of a wide rune blanks the other half.
	c.put(1, 0, "x", stPlain, 0)
	if got := plain(c.lines(0, 1, make([]lipgloss.Style, stCount))[0]); got != " xab " {
		t.Fatalf("row = %q", got)
	}
}
