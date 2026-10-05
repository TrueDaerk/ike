package tracepanel

import (
	"image/color"
	"strings"
	"testing"

	"charm.land/lipgloss/v2"

	"ike/internal/agenttrace"
)

// graph_look_test.go covers #2901: the edge-wrap step after a full row,
// a compaction marker sharing its question's row, and the reworked look —
// frames per kind, the lifted selection, the attached drawer, the elbow
// connectors and the dotted turn rule.

// detailStyles are the styles a change box's detail row may carry.
var detailStyles = map[int]bool{stSecondary: true, stAdded: true, stRemoved: true, stTimes: true, stError: true, stPending: true}

// TestSnakeEdgeWrapContinuesSideways: a break right after a turn, or after
// a row whose last box did not sit at the edge the turn comes from, used
// to flip the direction onto the edge the box already sat at — the next
// box then dropped a row although it would have fit beside the prompt.
func TestSnakeEdgeWrapContinuesSideways(t *testing.T) {
	// 78 cells hold three 24-cell boxes exactly (24*3 + 2*3): row 0 is
	// full, flush against the right edge.
	const w = 78
	bw, _ := BoxWidth(w)
	if bw*3+2*gapW != w {
		t.Fatalf("box width %d does not fill %d cells with three boxes", bw, w)
	}
	// Turn 1: prompt, change, answer (row 0, full). Turn 2: prompt + answer.
	l := Snake(widthsFor(5, -1, w), []int{3}, w, -1, 0)
	p, a := l.Slots[3], l.Slots[4]
	if !p.Break || p.Row != 1 || p.X+p.W != w || p.Dir != -1 {
		t.Fatalf("prompt #2 = %+v", p)
	}
	if a.Row != 1 || a.X+a.W+gapW != p.X || a.Dir != -1 {
		t.Fatalf("answer #2 must sit %d beside the prompt on its row, got %+v", gapW, a)
	}
	// Mirror: a four-box turn whose second row ends flush at the left
	// edge (direction -1), then a prompt + answer turn.
	l = Snake(widthsFor(8, -1, w), []int{6}, w, -1, 0)
	last, p, a := l.Slots[5], l.Slots[6], l.Slots[7]
	if last.X != 0 || last.Dir != -1 {
		t.Fatalf("row 1 must end at the left edge, got %+v", last)
	}
	if !p.Break || p.Row != 2 || p.X != 0 || p.Dir != 1 {
		t.Fatalf("prompt #2 = %+v", p)
	}
	if a.Row != 2 || a.X != p.X+p.W+gapW || a.Dir != 1 {
		t.Fatalf("answer #2 must follow the prompt rightwards, got %+v", a)
	}
	// The real-world shape: turn 1 ends on a row of one box at the left
	// edge (its answer did not fit the row before), turn 2 is prompt +
	// answer. The prompt sits under that box, and the row runs right.
	l = Snake(widthsFor(9, -1, w), []int{7}, w, -1, 0)
	last, p, a = l.Slots[6], l.Slots[7], l.Slots[8]
	if last.Row != 2 || last.X != 0 || l.Slots[5].Row != 1 {
		t.Fatalf("turn 1 must end alone on row 2 at the left edge, got %+v", last)
	}
	if !p.Break || p.Row != 3 || p.X != 0 || p.Dir != 1 || a.Row != 3 || a.X != p.W+gapW {
		t.Fatalf("turn 2 = %+v, %+v", p, a)
	}
	// A narrow separator that turned the row alone leaves the box under it
	// at the edge: the row runs away from that edge too.
	l = Snake(widthsFor(6, 3, w), nil, w, -1, 0)
	sep, b := l.Slots[3], l.Slots[4]
	if sep.Row != 1 || sep.X+sep.W != w || b.Row != 1 || b.X+b.W+gapW != sep.X {
		t.Fatalf("separator turn = %+v then %+v", sep, b)
	}
	// Every row still alternates with the one before unless the room
	// forbids it, and a row's first box sits under the last of the row
	// before.
	for i := 1; i < len(l.Slots); i++ {
		prev, s := l.Slots[i-1], l.Slots[i]
		if s.Row == prev.Row {
			continue
		}
		if s.X < prev.X && s.X+s.W > prev.X+prev.W {
			t.Fatalf("slot %d (%+v) is not under %+v", i, s, prev)
		}
	}
}

// TestGraphSeparatorBeforeQuestionSharesItsRow: a compaction marker right
// before a question does not take a row of its own; it starts the
// question's row under the turn rule, the rule labelled with that turn.
func TestGraphSeparatorBeforeQuestionSharesItsRow(t *testing.T) {
	m := panel(t)
	m.SetSize(78, 30)
	path := []agenttrace.Stop{
		{Kind: agenttrace.StopPrompt, Key: "t1", Turn: 1, Label: "#1 go"},
		{Kind: agenttrace.StopChange, Key: "e1/f0", Turn: 1, Label: "x.go", Detail: "edit", Ref: &agenttrace.FileRef{Path: "/p/x.go", Op: agenttrace.OpEdit}},
		{Kind: agenttrace.StopAnswer, Key: "t1/end", Turn: 1, Label: "done"},
		{Kind: agenttrace.StopSeparator, Key: "e3", Turn: 1, Label: "compaction"},
		{Kind: agenttrace.StopPrompt, Key: "t2", Turn: 2, Label: "#2 more"},
		{Kind: agenttrace.StopAnswer, Key: "t2/end", Turn: 2, Label: "ok"},
	}
	m.Set(agenttrace.BuildTree(nil), Info{ID: "s", Turns: 2})
	m.SetPath(path)
	m.SetViewMode(ViewGraph)
	l := m.graphLayout()
	sep, p, a := l.Slots[3], l.Slots[4], l.Slots[5]
	if !sep.Break || sep.Row != 1 || p.Break || p.Row != 1 || a.Row != 1 {
		t.Fatalf("slots = %+v %+v %+v", sep, p, a)
	}
	if sep.X+sep.W != 78 || p.X+p.W+gapW != sep.X || a.X+a.W+gapW != p.X {
		t.Fatalf("the row must run leftwards from the marker: %+v %+v %+v", sep, p, a)
	}
	rows := strings.Split(plain(m.View()), "\n")
	rule := rows[headerRows+sep.Y-2]
	if !strings.HasPrefix(rule, "┄┄ #2 ┄┄") {
		t.Fatalf("rule = %q", rule)
	}
	// The path runs straight into the marker across the rule.
	x := sep.CenterX()
	if []rune(rows[headerRows+sep.Y-1])[x] != '│' || []rune(rows[headerRows+sep.Y+1])[x] != '◇' {
		t.Fatalf("marker column:\n%s", strings.Join(rows[headerRows+sep.Y-3:headerRows+sep.Y+2], "\n"))
	}
	// Selecting the question keeps the rule on screen although the marker
	// carries the break.
	m.SetSize(78, 8)
	m.graph.top = 0
	if !m.graphSelect("t2") || m.GraphTop() > sep.Y-2 {
		t.Fatalf("top %d hides the rule at %d", m.GraphTop(), sep.Y-2)
	}
	m.Select("t1")
	if m.GraphTop() != 0 {
		t.Fatalf("top %d after selecting the first box", m.GraphTop())
	}
}

// TestGraphElbowJoinsMisalignedTurn: when the centres of the two slots of
// a turn differ (a box under a narrow marker), the turn is an elbow rather
// than a dangling "│".
func TestGraphElbowJoinsMisalignedTurn(t *testing.T) {
	c := newCanvas(30, 12)
	prev := Slot{X: 20, Y: 0, W: 3, H: boxH, Row: 0, Dir: 1}
	s := Slot{X: 6, Y: boxH + turnH, W: 24, H: boxH, Row: 1, Dir: -1}
	drawConnector(c, prev, s)
	rows := c.lines(0, 12, make([]lipgloss.Style, stCount), make([]color.Color, bgCount))
	px, x := prev.CenterX(), s.CenterX()
	top := []rune(rows[boxH])
	if top[x] != '╭' || top[px] != '╯' || top[x+1] != '─' || []rune(rows[boxH+1])[x] != '▼' {
		t.Fatalf("elbow:\n%s", strings.Join(rows[:boxH+2], "\n"))
	}
	// The other way round, and across a break's rule.
	c = newCanvas(30, 12)
	prev, s = Slot{X: 0, Y: 0, W: 3, H: boxH}, Slot{X: 4, Y: boxH + breakH, W: 20, H: boxH, Row: 1, Break: true}
	drawConnector(c, prev, s)
	rows = c.lines(0, 12, make([]lipgloss.Style, stCount), make([]color.Color, bgCount))
	px, x = prev.CenterX(), s.CenterX()
	top = []rune(rows[boxH])
	if top[px] != '╰' || top[x] != '╮' || []rune(rows[boxH+1])[x] != '┼' || []rune(rows[boxH+2])[x] != '▼' {
		t.Fatalf("elbow across a rule:\n%s", strings.Join(rows[:boxH+3], "\n"))
	}
	// Aligned centres stay a plain "│".
	c = newCanvas(30, 12)
	prev, s = Slot{X: 0, Y: 0, W: 20, H: boxH}, Slot{X: 0, Y: boxH + turnH, W: 20, H: boxH, Row: 1}
	drawConnector(c, prev, s)
	rows = c.lines(0, 12, make([]lipgloss.Style, stCount), make([]color.Color, bgCount))
	if []rune(rows[boxH])[10] != '│' || []rune(rows[boxH+1])[10] != '▼' {
		t.Fatalf("aligned turn:\n%s", strings.Join(rows[:boxH+2], "\n"))
	}
}

// TestGraphFramesTellKindsApart: question, change, answer, pending and
// implicit stops carry different frames and glyphs, readable without
// colour.
func TestGraphFramesTellKindsApart(t *testing.T) {
	ref := func(op agenttrace.Op) *agenttrace.FileRef { return &agenttrace.FileRef{Path: "/p/x.go", Op: op} }
	cases := []struct {
		name string
		stop agenttrace.Stop
		top  string
	}{
		{"prompt", agenttrace.Stop{Kind: agenttrace.StopPrompt, Label: "#1 go"}, "╔?══"},
		{"edit", agenttrace.Stop{Kind: agenttrace.StopChange, Label: "x.go", Ref: ref(agenttrace.OpEdit)}, "┌✎──"},
		{"create", agenttrace.Stop{Kind: agenttrace.StopChange, Label: "x.go", Ref: ref(agenttrace.OpCreate)}, "┌+──"},
		{"delete", agenttrace.Stop{Kind: agenttrace.StopChange, Label: "x.go", Ref: ref(agenttrace.OpDelete)}, "┌✕──"},
		{"failed", agenttrace.Stop{Kind: agenttrace.StopChange, Label: "x.go", Error: true, Ref: ref(agenttrace.OpEdit)}, "┌✗──"},
		{"pending change", agenttrace.Stop{Kind: agenttrace.StopChange, Label: "x.go", Pending: true, Ref: ref(agenttrace.OpEdit)}, "┌…┄┄"},
		{"answer", agenttrace.Stop{Kind: agenttrace.StopAnswer, Label: "done"}, "╭✓──"},
		{"implicit", agenttrace.Stop{Kind: agenttrace.StopAnswer, Label: "(no answer)", Implicit: true}, "╭◌──"},
		{"pending answer", agenttrace.Stop{Kind: agenttrace.StopAnswer, Label: "working …", Pending: true}, "╭…┄┄"},
		{"rewind", agenttrace.Stop{Kind: agenttrace.StopRewind, Label: "↶ rewound"}, "┌↶──"},
	}
	m := panel(t)
	seen := map[string]string{}
	for _, tc := range cases {
		c := newCanvas(20, boxH)
		m.drawBox(c, tc.stop, Slot{X: 0, W: 20, H: boxH}, 0)
		rows := c.lines(0, boxH, make([]lipgloss.Style, stCount), make([]color.Color, bgCount))
		top := plain(rows[0])
		if !strings.HasPrefix(top, tc.top) {
			t.Errorf("%s: top border %q, want prefix %q", tc.name, top, tc.top)
		}
		key := string([]rune(top)[:2]) + string([]rune(plain(rows[3]))[:1])
		if prev, dup := seen[key]; dup {
			t.Errorf("%s and %s share frame+glyph %q", prev, tc.name, key)
		}
		seen[key] = tc.name
	}
	// A pending answer's frame is dashed all round; a settled one closed.
	c := newCanvas(20, boxH)
	m.drawBox(c, cases[8].stop, Slot{X: 0, W: 20, H: boxH}, 0)
	rows := c.lines(0, boxH, make([]lipgloss.Style, stCount), make([]color.Color, bgCount))
	if r := plain(rows[1]); !strings.HasPrefix(r, "┆") || !strings.HasSuffix(r, "┆") {
		t.Errorf("pending answer sides %q", r)
	}
	if r := plain(rows[3]); r != "╰"+strings.Repeat("┄", 18)+"╯" {
		t.Errorf("pending answer bottom %q", r)
	}
}

// TestGraphDrawerAttachesToItsBox: the expanded box's drawer is a framed
// block under its row, joined to the box by "┬" over "┴" at the box's
// centre, the key hint in its bottom border; the rows below move down by
// the drawer's full height.
func TestGraphDrawerAttachesToItsBox(t *testing.T) {
	m, _ := graphPanel(t, true, 80, 40)
	m.Select("e5/f0")
	send(m, "space")
	l := m.graphLayout()
	s := l.Slots[m.stopIndex("e5/f0")]
	lines := m.detailLines(m.graph.stops[m.stopIndex("e5/f0")], 80)
	if l.DetailH != len(lines)+detailFrameH || l.DetailY != s.Y+boxH {
		t.Fatalf("drawer at %d/%d for %d lines", l.DetailY, l.DetailH, len(lines))
	}
	rows := strings.Split(plain(m.View()), "\n")
	x := s.CenterX()
	bottom, top := []rune(rows[headerRows+s.Y+3]), []rune(rows[headerRows+l.DetailY])
	if bottom[x] != '┬' || top[x] != '┴' || top[0] != '╭' || top[79] != '╮' {
		t.Fatalf("drawer attachment:\n%s", strings.Join(rows[headerRows+s.Y:headerRows+l.DetailY+l.DetailH], "\n"))
	}
	for j, line := range lines {
		r := rows[headerRows+l.DetailY+1+j]
		if !strings.HasPrefix(r, "│ "+line) || !strings.HasSuffix(r, "│") {
			t.Fatalf("drawer line %d = %q, want %q framed", j, r, line)
		}
	}
	last := rows[headerRows+l.DetailY+l.DetailH-1]
	if !strings.HasPrefix(last, "╰─ space collapse · enter open") || !strings.HasSuffix(last, "─╯") {
		t.Fatalf("drawer bottom = %q", last)
	}
	// The next row's turn starts below the drawer.
	for i, sl := range l.Slots {
		if sl.Row == s.Row+1 {
			if sl.Y != l.DetailY+l.DetailH+turnH && sl.Y != l.DetailY+l.DetailH+breakH {
				t.Fatalf("slot %d = %+v does not clear the drawer ending at %d", i, sl, l.DetailY+l.DetailH)
			}
			break
		}
	}
	// A change detail's markers carry their own styles.
	c := newCanvas(30, boxH)
	st := agenttrace.Stop{Kind: agenttrace.StopChange, Key: "e1/f0", Label: "x.go", Detail: "edit :3 ×2 +12 −3 ✗", Ref: &agenttrace.FileRef{Path: "/p/x.go", Op: agenttrace.OpEdit}}
	m.graph.sel = ""
	m.drawBox(c, st, Slot{X: 0, W: 30, H: boxH}, 0)
	want := map[string]int{"edit": stSecondary, ":3": stSecondary, "×2": stTimes, "+12": stAdded, "−3": stRemoved, "✗": stError}
	pos := 1
	for _, tok := range strings.Split(st.Detail, " ") {
		if got := c.cells[2][pos].st; got != want[tok] {
			t.Errorf("token %q style %d, want %d", tok, got, want[tok])
		}
		pos += len([]rune(tok)) + 1
	}
}
