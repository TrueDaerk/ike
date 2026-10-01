package tracepanel

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	tea "charm.land/bubbletea/v2"

	"ike/internal/agenttrace"
	"ike/internal/theme"
)

// tracepanel_test.go covers the pane half of #2840: the tree built from the
// fixture session, open-on-enter and on double click, expansion and
// selection surviving a live append, and the empty state's dialog with its
// install action.

var ansiRE = regexp.MustCompile("\x1b\\[[0-9;]*m")

func plain(s string) string { return ansiRE.ReplaceAllString(s, "") }

// fixture parses the shared basic transcript up to (exclusive) or past the
// second prompt.
func fixture(t *testing.T, whole bool) *agenttrace.Session {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("..", "agenttrace", "testdata", "basic.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	p := agenttrace.NewParser()
	p.ReadFile = func(string) ([]byte, error) { return nil, os.ErrNotExist }
	for _, l := range strings.Split(string(data), "\n") {
		if !whole && strings.Contains(l, "/verify") {
			break
		}
		p.Line([]byte(l))
	}
	return p.Session()
}

func info(s *agenttrace.Session) Info {
	return Info{ID: s.ID, Transcript: "/t/" + s.ID + ".jsonl", CWD: s.CWD, Turns: s.Turns()}
}

func panel(t *testing.T) *Model {
	t.Helper()
	m := New(theme.DefaultPalette())
	m.SetSize(100, 30)
	m.SetFocused(true)
	return &m
}

// keyMsg builds a key press whose String() is key: letters and enter/space.
func keyMsg(key string) tea.KeyPressMsg {
	switch key {
	case "enter":
		return tea.KeyPressMsg{Code: tea.KeyEnter}
	case "space":
		return tea.KeyPressMsg{Code: tea.KeySpace, Text: " "}
	case "down":
		return tea.KeyPressMsg{Code: tea.KeyDown}
	case "up":
		return tea.KeyPressMsg{Code: tea.KeyUp}
	case "left":
		return tea.KeyPressMsg{Code: tea.KeyLeft}
	case "right":
		return tea.KeyPressMsg{Code: tea.KeyRight}
	}
	return tea.KeyPressMsg{Code: rune(key[0]), Text: key}
}

// toTop moves the cursor onto the first row the way the user would, which
// ends following the newest row (#2857).
func toTop(m *Model) {
	for m.tree.Cursor() > 0 {
		send(m, "up")
	}
}

func send(m *Model, key string) tea.Msg {
	cmd := m.Update(keyMsg(key))
	if cmd == nil {
		return nil
	}
	return cmd()
}

func TestSetShowsNewestTurnWhole(t *testing.T) {
	m := panel(t)
	s := fixture(t, true)
	m.Set(agenttrace.BuildTree(s), info(s))
	rows := m.Rows()
	// Turn 1 collapsed, turn 2 (the newest) expanded whole.
	if strings.Join(rows, ",") != "t1,t2, e10, e11" {
		t.Fatalf("rows = %v", rows)
	}
	view := plain(m.View())
	if !strings.Contains(view, "11111111 · scan · 2 turns") || !strings.Contains(view, "#2 /verify main.go") {
		t.Fatalf("view =\n%s", view)
	}
	if !strings.Contains(view, "— context compacted —") {
		t.Fatalf("separator missing:\n%s", view)
	}
}

func TestEnterOpensFileNodeAndTogglesOthers(t *testing.T) {
	m := panel(t)
	s := fixture(t, false)
	m.Set(agenttrace.BuildTree(s), info(s))
	toTop(m)
	// One turn: expanded whole on the first Set.
	rows := m.Rows()
	if rows[0] != "t1" || rows[1] != " e1" || rows[2] != " e2" || rows[3] != "  e3" {
		t.Fatalf("rows = %v", rows)
	}
	// Enter on the turn row folds it.
	if msg := send(m, "enter"); msg != nil {
		t.Fatalf("enter on a turn yielded %#v", msg)
	}
	if got := m.Rows(); len(got) != 1 {
		t.Fatalf("turn did not fold: %v", got)
	}
	send(m, "enter")
	// Down to e4 (Edit main.go:3) — the tool row itself opens its single file.
	for _, k := range []string{"down", "down", "down", "down", "down"} {
		send(m, k)
	}
	if cur := m.Current(); cur == nil || cur.Key != "e4" {
		t.Fatalf("cursor on %+v", cur)
	}
	msg, ok := send(m, "enter").(OpenLocationMsg)
	if !ok || msg.Path != "/Users/dev/src/proj/main.go" || msg.Line != 2 || msg.Col != 0 {
		t.Fatalf("enter on Edit = %#v", msg)
	}
	// Its file child carries the same target; a create without a line opens
	// the file without a jump (Line -1).
	send(m, "down")
	if cur := m.Current(); cur.Key != "e4/f0" {
		t.Fatalf("cursor on %s", cur.Key)
	}
	send(m, "down")
	send(m, "down")
	if cur := m.Current(); cur.Key != "e5/f0" {
		t.Fatalf("cursor on %s", cur.Key)
	}
	msg, ok = send(m, "enter").(OpenLocationMsg)
	if !ok || msg.Path != "/Users/dev/src/proj/hello.go" || msg.Line != -1 {
		t.Fatalf("enter on create = %#v", msg)
	}
	// The rendered row shows the path without a line.
	view := plain(m.View())
	if !strings.Contains(view, "create  /Users/dev/src/proj/hello.go\n") && !strings.Contains(view, "create  /Users/dev/src/proj/hello.go ") {
		t.Fatalf("create row:\n%s", view)
	}
	if !strings.Contains(view, "edit  /Users/dev/src/proj/main.go:3") {
		t.Fatalf("edit row:\n%s", view)
	}
	if !strings.Contains(view, "Bash Build and vet the module ✗ error") {
		t.Fatalf("bash row:\n%s", view)
	}
}

func TestLiveAppendKeepsSelectionAndExpansion(t *testing.T) {
	m := panel(t)
	s := fixture(t, false)
	m.Set(agenttrace.BuildTree(s), info(s))
	toTop(m)
	// Fold the Read call's decision block partially: collapse e7 (MultiEdit)
	// and select e6.
	for _, k := range []string{"down", "down", "down", "down", "down", "down", "down", "down", "down", "down"} {
		send(m, k)
	}
	if cur := m.Current(); cur.Key != "e7" {
		t.Fatalf("cursor on %s", cur.Key)
	}
	send(m, "left") // collapse MultiEdit
	send(m, "up")
	if cur := m.Current(); cur.Key != "e6" {
		t.Fatalf("cursor on %s", cur.Key)
	}
	before := m.Rows()

	whole := fixture(t, true)
	m.Set(agenttrace.BuildTree(whole), info(whole))
	after := m.Rows()
	if cur := m.Current(); cur == nil || cur.Key != "e6" {
		t.Fatalf("selection lost: %+v", cur)
	}
	if strings.Join(after[:len(before)], ",") != strings.Join(before, ",") {
		t.Fatalf("existing rows changed:\n%v\n%v", before, after)
	}
	tail := after[len(before):]
	if strings.Join(tail, ",") != "t2, e10, e11" {
		t.Fatalf("new turn not expanded: %v", tail)
	}
	if m.Info().Turns != 2 {
		t.Fatalf("info turns = %d", m.Info().Turns)
	}
}

// findNode returns the node keyed key.
func findNode(nodes []agenttrace.Node, key string) *agenttrace.Node {
	var out *agenttrace.Node
	agenttrace.Walk(nodes, func(n *agenttrace.Node) {
		if n.Key == key {
			out = n
		}
	})
	return out
}

// TestLinkedRowsMarkDiffRevertAndSelect covers the pane half of #2838: a
// node linked to a change-feed entry shows the Δ mark and answers D / V,
// an unlinked one stays plain, and Select unfolds its way to a node.
func TestLinkedRowsMarkDiffRevertAndSelect(t *testing.T) {
	m := panel(t)
	s := fixture(t, false)
	nodes := agenttrace.BuildTree(s)
	m.Set(nodes, info(s))
	toTop(m)
	edit := findNode(nodes, "e4/f0")
	if edit == nil || edit.At.IsZero() || edit.Until.IsZero() || !edit.Until.After(edit.At) {
		t.Fatalf("edit node window = %+v", edit)
	}
	const target = "/Users/dev/src/proj/main.go"
	links := agenttrace.Link(nodes, []agenttrace.Change{{
		Path: target, First: edit.Until, Last: edit.Until, SourceKey: "term-1",
	}}, "term-1", s.CWD)
	if links.Path(target) != "e4/f0" || links.Node("e4") != target {
		t.Fatalf("links = %+v", links)
	}
	m.SetLinks(links)
	if view := plain(m.View()); !strings.Contains(view, "Edit Δ") {
		t.Fatalf("linked tool row has no mark:\n%s", view)
	}
	// The cursor starts on the turn row: unlinked, D and V do nothing.
	if msg := send(m, "D"); msg != nil {
		t.Fatalf("D on an unlinked row = %#v", msg)
	}
	// Fold the turn, then jump into it: Select unfolds the ancestors.
	send(m, "left")
	if len(m.Rows()) != 1 {
		t.Fatalf("turn did not fold: %v", m.Rows())
	}
	if !m.Select("e4/f0") || m.Current().Key != "e4/f0" {
		t.Fatalf("select landed on %+v", m.Current())
	}
	if m.Select("e999") {
		t.Fatal("select of an unknown key succeeded")
	}
	if msg, ok := send(m, "D").(ChangeDiffMsg); !ok || msg.Path != target {
		t.Fatalf("D = %#v", msg)
	}
	if msg, ok := send(m, "V").(ChangeRevertMsg); !ok || msg.Path != target {
		t.Fatalf("V = %#v", msg)
	}
	// Selection and expansion survive a relink; Reset forgets the links.
	m.SetLinks(agenttrace.Links{})
	if m.Current().Key != "e4/f0" || send(m, "D") != nil {
		t.Fatal("unlinking must keep the selection and silence D")
	}
	m.Reset()
	if m.Links().Len() != 0 || m.Nodes() != nil {
		t.Fatal("reset kept the links")
	}
}

func TestClickSelectsDoubleClickOpens(t *testing.T) {
	m := panel(t)
	now := time.Unix(1000, 0)
	m.SetNow(func() time.Time { return now })
	s := fixture(t, false)
	m.Set(agenttrace.BuildTree(s), info(s))
	// Row 5 (y = headerRows + 5) is e4, the Edit call.
	if cmd := m.Click(10, headerRows+5); cmd != nil {
		t.Fatal("first click must only select")
	}
	if cur := m.Current(); cur.Key != "e4" {
		t.Fatalf("click selected %s", cur.Key)
	}
	now = now.Add(100 * time.Millisecond)
	cmd := m.Click(10, headerRows+5)
	if cmd == nil {
		t.Fatal("double click yielded nothing")
	}
	msg, ok := cmd().(OpenLocationMsg)
	if !ok || msg.Line != 2 {
		t.Fatalf("double click = %#v", msg)
	}
	// A click on the marker cell of the turn row folds it.
	now = now.Add(time.Second)
	m.Click(0, headerRows)
	if got := m.Rows(); len(got) != 1 {
		t.Fatalf("marker click did not fold: %v", got)
	}
	// A click below the rows clears the pending click and selects nothing new.
	m.Click(3, headerRows+10)
	if cur := m.Current(); cur.Key != "t1" {
		t.Fatalf("cursor on %s", cur.Key)
	}
}

func TestEmptyStateDialogAndActions(t *testing.T) {
	m := panel(t)
	view := plain(m.View())
	if !strings.Contains(view, "locating the agent session") {
		t.Fatalf("loading view:\n%s", view)
	}
	if msg := send(m, "i"); msg != nil {
		t.Fatal("install offered while still locating")
	}
	m.SetNoSession("/Users/dev/src/proj", agenttrace.ErrNotFound)
	view = plain(m.View())
	for _, want := range []string{"No agent session", "/Users/dev/src/proj", "[Install Claude hooks] [Rescan]", "i install · r rescan", "╭"} {
		if !strings.Contains(view, want) {
			t.Errorf("dialog lacks %q:\n%s", want, view)
		}
	}
	if _, ok := send(m, "i").(InstallHooksMsg); !ok {
		t.Error("i must ask for the hook install")
	}
	if _, ok := send(m, "enter").(InstallHooksMsg); !ok {
		t.Error("enter must run the primary action")
	}
	if _, ok := send(m, "r").(RefreshMsg); !ok {
		t.Error("r must ask for a rescan")
	}
	// The dialog's buttons take clicks at the drawn position.
	d, fits := m.layoutDialog()
	if !fits {
		t.Fatal("dialog does not fit a 100x30 pane")
	}
	install := m.Click(d.x+1, d.y+d.actRow)
	if install == nil {
		t.Fatal("click on the install button did nothing")
	} else if _, ok := install().(InstallHooksMsg); !ok {
		t.Fatal("install button yields the wrong message")
	}
	rescan := m.Click(d.x+d.actions.Width()-2, d.y+d.actRow)
	if rescan == nil {
		t.Fatal("click on the rescan button did nothing")
	} else if _, ok := rescan().(RefreshMsg); !ok {
		t.Fatal("rescan button yields the wrong message")
	}
	if m.Click(d.x+1, d.y) != nil {
		t.Fatal("a click on the heading acted")
	}
	// The dialog line lands where the click test expects it.
	lines := strings.Split(view, "\n")
	if !strings.Contains(lines[d.y+d.actRow], "[Install Claude hooks]") {
		t.Fatalf("action row %d = %q", d.y+d.actRow, lines[d.y+d.actRow])
	}
	if !strings.Contains(lines[d.y], "No agent session") {
		t.Fatalf("heading row %d = %q", d.y, lines[d.y])
	}
	strip := plain(lines[d.y+d.actRow])
	col := utf8.RuneCountInString(strip[:strings.Index(strip, "[Install")])
	if col != d.x {
		t.Fatalf("strip drawn at column %d, hit test assumes %d", col, d.x)
	}

	// A read error is shown; a tiny pane falls back to the one-line notice.
	m.SetNoSession("/p", os.ErrPermission)
	if view = plain(m.View()); !strings.Contains(view, "permission denied") {
		t.Fatalf("error not shown:\n%s", view)
	}
	m.SetSize(30, 4)
	if view = plain(m.View()); strings.Contains(view, "╭") || !strings.Contains(view, "no agent session") {
		t.Fatalf("small pane:\n%s", view)
	}
}

// TestFollowKeepsNewestRowInView (#2857): the pane opens on the newest row
// and, while the cursor stays on the last row, every Set moves it onto the
// new last row — so a session growing past the pane's height stays visible
// instead of growing below the fold. Moving off the last row stops it;
// moving back resumes it.
func TestFollowKeepsNewestRowInView(t *testing.T) {
	m := panel(t)
	m.SetSize(100, 5) // header + 3 tree rows + hint
	part := fixture(t, false)
	m.Set(agenttrace.BuildTree(part), info(part))
	rows := m.Rows()
	if cur := m.Current(); !m.Following() || cur == nil || cur.Key != strings.TrimSpace(rows[len(rows)-1]) {
		t.Fatalf("the pane must open on the newest row, cursor on %+v", cur)
	}
	whole := fixture(t, true)
	m.Set(agenttrace.BuildTree(whole), info(whole))
	if cur := m.Current(); cur == nil || cur.Key != "e11" {
		t.Fatalf("follow did not move onto the newest row: %+v", cur)
	}
	if view := plain(m.View()); !strings.Contains(view, "#2 /verify main.go") {
		t.Fatalf("newest turn not on screen:\n%s", view)
	}

	send(m, "up")
	if m.Following() {
		t.Fatal("moving off the last row must stop following")
	}
	m.Set(agenttrace.BuildTree(whole), info(whole))
	if cur := m.Current(); cur == nil || cur.Key != "e10" {
		t.Fatalf("selection moved while not following: %+v", cur)
	}
	send(m, "down")
	if !m.Following() {
		t.Fatal("back on the last row must follow again")
	}
}

// TestNewNodesInRunningTurnArriveExpanded (#2857): a decision, call and
// file landing in a turn already on screen come up expanded, while a node
// the user folded stays folded.
func TestNewNodesInRunningTurnArriveExpanded(t *testing.T) {
	file := func(key string) agenttrace.Node {
		return agenttrace.Node{Kind: agenttrace.NodeFile, Key: key, Label: "edit", Path: "/p/" + key}
	}
	tool := func(key string) agenttrace.Node {
		return agenttrace.Node{Kind: agenttrace.NodeTool, Key: key, Label: "Edit", Children: []agenttrace.Node{file(key + "/f0")}}
	}
	decision := func(key string, tools ...agenttrace.Node) agenttrace.Node {
		return agenttrace.Node{Kind: agenttrace.NodeDecision, Key: key, Label: "decide " + key, Children: tools}
	}
	turn := func(ds ...agenttrace.Node) []agenttrace.Node {
		return []agenttrace.Node{{Kind: agenttrace.NodeTurn, Key: "t1", Label: "#1", Children: ds}}
	}
	m := panel(t)
	m.Set(turn(decision("e1", tool("e2"))), Info{ID: "s", Turns: 1})
	if got := strings.Join(m.Rows(), ","); got != "t1, e1,  e2,   e2/f0" {
		t.Fatalf("first set rows = %s", got)
	}
	// The user folds the first call.
	for m.Current().Key != "e2" {
		send(m, "up")
	}
	send(m, "left")
	m.Set(turn(decision("e1", tool("e2"), tool("e3")), decision("e4", tool("e5"))), Info{ID: "s", Turns: 1})
	want := "t1, e1,  e2,  e3,   e3/f0, e4,  e5,   e5/f0"
	if got := strings.Join(m.Rows(), ","); got != want {
		t.Fatalf("rows = %s, want %s", got, want)
	}
}

// TestHeaderDiagnostics (#2857): the header tells "no new lines" from "not
// reading" — the last read's time and count — and names the followed
// terminal and why.
func TestHeaderDiagnostics(t *testing.T) {
	m := panel(t)
	m.SetSize(160, 10)
	s := fixture(t, false)
	m.Set(agenttrace.BuildTree(s), info(s))
	if view := plain(m.View()); !strings.Contains(view, "· not read yet") || strings.Contains(view, "⇢") {
		t.Fatalf("header before any read:\n%s", view)
	}
	m.SetRead(time.Date(2026, 10, 1, 14, 5, 9, 0, time.Local), 3)
	m.SetFollowing("claude (focused)")
	header := strings.SplitN(plain(m.View()), "\n", 2)[0]
	if !strings.Contains(header, "· read 14:05:09 +3") || !strings.Contains(header, "· ⇢ claude (focused)") {
		t.Fatalf("header = %q", header)
	}
	m.SetRead(time.Date(2026, 10, 1, 14, 5, 10, 0, time.Local), 0)
	if header = strings.SplitN(plain(m.View()), "\n", 2)[0]; !strings.Contains(header, "· read 14:05:10 +0") {
		t.Fatalf("an empty read must still stamp the header: %q", header)
	}
}
