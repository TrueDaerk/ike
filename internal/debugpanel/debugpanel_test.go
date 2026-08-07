package debugpanel

import (
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"ike/internal/dap"
)

func key(s string) tea.KeyPressMsg {
	switch s {
	case "enter":
		return tea.KeyPressMsg{Code: tea.KeyEnter}
	case "tab":
		return tea.KeyPressMsg{Code: tea.KeyTab}
	}
	r := []rune(s)[0]
	return tea.KeyPressMsg{Code: r, Text: s}
}

func frames() []dap.StackFrame {
	return []dap.StackFrame{
		{ID: 1, Name: "inner", Source: dap.Source{Path: "/p/a.py"}, Line: 7},
		{ID: 2, Name: "outer", Source: dap.Source{Path: "/p/a.py"}, Line: 20},
		{ID: 3, Name: "<module>", Source: dap.Source{Path: "/p/a.py"}, Line: 30},
	}
}

// TestFrameNavigationAndSelect verifies j/k movement and enter emitting the
// selected frame.
func TestFrameNavigationAndSelect(t *testing.T) {
	m := New(nil)
	m.SetSize(80, 10)
	m.SetFrames(frames())
	m.Update(key("j"))
	m.Update(key("j"))
	m.Update(key("j")) // wraps back to the first frame (#1666)
	m.Update(key("end"))
	cmd := m.Update(key("enter"))
	if cmd == nil {
		t.Fatal("enter on a frame must emit SelectFrameMsg")
	}
	msg, ok := cmd().(SelectFrameMsg)
	if !ok || msg.Frame.ID != 3 {
		t.Fatalf("selected frame = %+v", msg)
	}
}

// TestScopesAndVariableExpansion verifies the tree: scopes become roots, an
// unloaded node emits ExpandVarMsg, SetChildren fills and expands it, and a
// second enter collapses without refetching.
func TestScopesAndVariableExpansion(t *testing.T) {
	m := New(nil)
	m.SetSize(80, 12)
	m.SetFrames(frames())
	m.SetScopes([]dap.Scope{
		{Name: "Locals", VariablesReference: 100},
		{Name: "Globals", VariablesReference: 200},
	})
	m.SetChildren(100, []dap.Variable{
		{Name: "x", Value: "42"},
		{Name: "obj", Value: "<Obj>", VariablesReference: 101},
	})
	m.Update(key("tab")) // switch to the variables column
	// Rows: Locals(0) x(1) obj(2) Globals(3). Move onto obj.
	m.Update(key("j"))
	m.Update(key("j"))
	cmd := m.Update(key("enter"))
	if cmd == nil {
		t.Fatal("expanding an unloaded node must emit ExpandVarMsg")
	}
	if msg, ok := cmd().(ExpandVarMsg); !ok || msg.Ref != 101 {
		t.Fatalf("expand msg = %+v", cmd())
	}
	m.SetChildren(101, []dap.Variable{{Name: "field", Value: "1"}})
	view := m.View()
	if !strings.Contains(view, "field = 1") {
		t.Fatalf("expanded child missing from view:\n%s", view)
	}
	// Collapse and re-expand: loaded children need no refetch.
	if cmd := m.Update(key("enter")); cmd != nil {
		t.Fatal("collapsing must not emit")
	}
	if cmd := m.Update(key("enter")); cmd != nil {
		t.Fatal("re-expanding a loaded node must not refetch")
	}
	if !strings.Contains(m.View(), "field = 1") {
		t.Fatal("re-expanded child must render again")
	}
}

// TestViewStates covers the running and empty placeholders: the frames column
// shows them, the columns render in every state (#637).
func TestViewStates(t *testing.T) {
	m := New(nil)
	m.SetSize(60, 6)
	v := m.View()
	if !strings.Contains(v, "not paused") {
		t.Fatalf("empty panel must show the placeholder:\n%s", v)
	}
	m.SetFrames(frames())
	if !strings.Contains(m.View(), "FRAMES") || !strings.Contains(m.View(), "VARIABLES") {
		t.Fatal("panel must render the columns")
	}
	m.SetRunning()
	v = m.View()
	if !strings.Contains(v, "running") || !strings.Contains(v, "VARIABLES") {
		t.Fatalf("running state must keep the columns with a placeholder:\n%s", v)
	}
}

// TestStaleDataWhileRunning (#693): a resumed debuggee (input wait, sleep, IO)
// keeps the last stop's frames and variables on screen behind the running
// indicator, while paused-only interactions are gated off.
func TestStaleDataWhileRunning(t *testing.T) {
	m := New(nil)
	m.SetSize(120, 10)
	m.SetFrames(frames())
	m.SetScopes([]dap.Scope{{Name: "Locals", VariablesReference: 9}})
	m.SetRunning()
	v := m.View()
	if !strings.Contains(v, "running") {
		t.Fatalf("running indicator missing:\n%s", v)
	}
	if !strings.Contains(v, "inner") || !strings.Contains(v, "Locals") {
		t.Fatalf("stale frames/variables must stay visible while running:\n%s", v)
	}
	// Paused-only interactions are no-ops on stale rows.
	if cmd := m.activate(); cmd != nil {
		t.Fatal("frame activation must be gated while running")
	}
	m.col = colVars
	if cmd := m.activate(); cmd != nil {
		t.Fatal("variable expansion must be gated while running")
	}
	m.SetEditable(true)
	m.startEdit()
	if m.Editing() {
		t.Fatal("inline editing must be gated while running")
	}
	// A resume with the editor open cancels it (#640).
	m.SetFrames(frames())
	m.SetScopes([]dap.Scope{{Name: "Locals", VariablesReference: 9}})
	if cmd := m.Update(key("enter")); cmd != nil { // expand request path sanity
		_ = cmd
	}
	m.editing = true
	m.SetRunning()
	if m.Editing() {
		t.Fatal("SetRunning must cancel an open inline editor")
	}
	// The next stop replaces the stale data and re-enables interaction.
	m.SetFrames(frames())
	m.col = colFrames
	if cmd := m.activate(); cmd == nil {
		t.Fatal("a fresh stop must re-enable frame activation")
	}
}

// TestColumnResize (#691): the separator hit-tests at its rendered x,
// dragging moves it with min-width clamping, and untouched panels keep the
// built-in proportions.
func TestColumnResize(t *testing.T) {
	m := New(nil)
	m.SetSize(101, 8) // usable 100: defaults 40 | 60
	fw, vw := m.colWidths()
	if fw != 40 || vw != 60 {
		t.Fatalf("default widths = %d/%d, want 40/60", fw, vw)
	}
	if m.SeparatorHit(fw) != 0 {
		t.Fatal("separator must hit-test at its rendered column")
	}
	if m.SeparatorHit(fw-1) != -1 || m.SeparatorHit(fw+1) != -1 {
		t.Fatal("column interiors must not hit-test as separators")
	}
	// Drag the separator right: frames grow, vars shrink.
	m.ResizeSeparator(0, 60)
	fw, vw = m.colWidths()
	if fw != 60 || vw != 40 {
		t.Fatalf("widths = %d/%d after drag, want 60/40", fw, vw)
	}
	// Drag far right: vars clamp to the minimum.
	m.ResizeSeparator(0, 500)
	if fw, vw = m.colWidths(); vw != minColWidth || fw != 100-minColWidth {
		t.Fatalf("widths = %d/%d after clamped drag, want %d/%d", fw, vw, 100-minColWidth, minColWidth)
	}
	// Proportions stick across a panel resize instead of snapping back.
	m.ResizeSeparator(0, 60)
	m.SetSize(51, 8) // usable 50
	fw, vw = m.colWidths()
	if fw != 30 || vw != 20 {
		t.Fatalf("resized widths = %d/%d, want 30/20 (proportional)", fw, vw)
	}
	// A drag on a too-narrow panel is ignored rather than corrupting state.
	m.SetSize(16, 8)
	before := m.colFW
	m.ResizeSeparator(0, 12)
	if m.colFW != before {
		t.Fatal("a drag below the minimum panel width must be a no-op")
	}
}

// TestResizeNoDrift (#695): repeated motion events on the separator store the
// dragged width in exact cells — wiggling it never accumulates rounding error.
func TestResizeNoDrift(t *testing.T) {
	m := New(nil)
	m.SetSize(101, 8) // usable 100
	for i := 0; i < 50; i++ {
		m.ResizeSeparator(0, 41)
		fw, _ := m.colWidths()
		if fw != 41 {
			t.Fatalf("frames drifted to %d (iteration %d)", fw, i)
		}
	}
}

// TestFinishedState (#689): a terminated session keeps the panel usable — the
// FRAMES column shows the exit status, and a new session's ResetSession
// clears it (output review lives in the debuggee terminal pane, #1370).
func TestFinishedState(t *testing.T) {
	m := New(nil)
	m.SetSize(120, 8)
	m.SetFrames(frames())
	m.SetFinished(3, true)
	v := m.View()
	if !strings.Contains(v, "finished (exit code 3)") {
		t.Fatalf("finished state must show the exit code:\n%s", v)
	}
	if !m.Finished() {
		t.Fatal("Finished() must report the terminated session")
	}
	m.SetFinished(0, false)
	if v := m.View(); !strings.Contains(v, "finished") || strings.Contains(v, "exit code") {
		t.Fatalf("codeless termination must render plain 'finished':\n%s", v)
	}
	m.ResetSession()
	v = m.View()
	if m.Finished() || strings.Contains(v, "finished") {
		t.Fatalf("ResetSession must clear the finished marker:\n%s", v)
	}
}

// TestLeafEnterIsNoop: a plain value has nothing to expand.
func TestLeafEnterIsNoop(t *testing.T) {
	m := New(nil)
	m.SetSize(80, 10)
	m.SetFrames(frames())
	m.SetScopes([]dap.Scope{{Name: "Locals", VariablesReference: 100}})
	m.SetChildren(100, []dap.Variable{{Name: "x", Value: "42"}})
	m.Update(key("tab"))
	m.Update(key("j")) // onto x
	if cmd := m.Update(key("enter")); cmd != nil {
		t.Fatal("enter on a leaf value must be a no-op")
	}
}

// TestClickSelectsAndDoubleClickActivatesFrame verifies a single click selects
// a frame (no message) and a double-click on the same row emits SelectFrameMsg.
func TestClickSelectsAndDoubleClickActivatesFrame(t *testing.T) {
	m := New(nil)
	m.SetSize(80, 10)
	m.SetFrames(frames())
	clk := time.Unix(0, 0)
	m.now = func() time.Time { return clk }

	// Frames column: x small, y 2 = second frame (title at y0, frame0 at y1).
	if cmd := m.Click(4, 2); cmd != nil {
		t.Fatal("single click should not activate")
	}
	if m.frameSel != 1 || m.col != colFrames {
		t.Fatalf("click selection = %d col=%d, want 1/frames", m.frameSel, m.col)
	}
	// Second click on the same row within the window activates.
	clk = clk.Add(100 * time.Millisecond)
	cmd := m.Click(4, 2)
	if cmd == nil {
		t.Fatal("double click should activate")
	}
	sf, ok := cmd().(SelectFrameMsg)
	if !ok || sf.Frame.ID != 2 {
		t.Fatalf("activate emitted %#v, want frame ID 2", cmd())
	}
}

// TestClickExpandsVariable verifies a double-click in the variables column
// emits ExpandVarMsg for an unexpanded ref (#639: the old test only asserted
// selection).
func TestClickExpandsVariable(t *testing.T) {
	m := New(nil)
	m.SetSize(80, 10)
	m.SetFrames(frames())
	// Two scopes: Locals expands eagerly, Globals stays collapsed + unloaded so
	// activating it must emit ExpandVarMsg.
	m.SetScopes([]dap.Scope{
		{Name: "Locals", VariablesReference: 42},
		{Name: "Globals", VariablesReference: 200},
	})
	clk := time.Unix(0, 0)
	m.now = func() time.Time { return clk }

	// Variables column: x inside the middle column (frames|vars|output), y 2 =
	// second var row (the Globals scope; Locals has no loaded children).
	if cmd := m.Click(40, 2); cmd != nil {
		t.Fatal("single click should not activate")
	}
	if m.col != colVars || m.varSel != 1 {
		t.Fatalf("vars click col=%d sel=%d, want vars/1", m.col, m.varSel)
	}
	clk = clk.Add(100 * time.Millisecond)
	cmd := m.Click(40, 2)
	if cmd == nil {
		t.Fatal("double click on an unloaded ref must emit ExpandVarMsg")
	}
	if msg, ok := cmd().(ExpandVarMsg); !ok || msg.Ref != 200 {
		t.Fatalf("double click emitted %#v, want ExpandVarMsg{Ref: 200}", cmd())
	}
}

// TestWheelScrollsFocusedColumn verifies the wheel scrolls frames when the
// frames column is focused, clamped to the row count.
func TestWheelScrollsFocusedColumn(t *testing.T) {
	m := New(nil)
	m.SetSize(80, 3) // bodyHeight = 2, so 3 frames can scroll by 1
	m.SetFrames(frames())
	m.col = colFrames
	m.Wheel(1)
	if m.frameTop != 1 {
		t.Fatalf("frameTop = %d, want 1 after one wheel-down", m.frameTop)
	}
	m.Wheel(5) // clamp to len(frames)-bodyHeight = 1
	if m.frameTop != 1 {
		t.Fatalf("frameTop = %d, want clamp at 1", m.frameTop)
	}
	m.Wheel(-10)
	if m.frameTop != 0 {
		t.Fatalf("frameTop = %d, want 0 after wheel-up", m.frameTop)
	}
}

// TestVariableEditFlow verifies the inline editor: 'e' opens it on an editable
// child (only when the adapter supports setVariable), typing edits the value,
// and enter emits SetVarMsg with the containing ref.
func TestVariableEditFlow(t *testing.T) {
	m := New(nil)
	m.SetSize(80, 10)
	m.SetFrames(frames())
	m.SetScopes([]dap.Scope{{Name: "Locals", VariablesReference: 100}})
	m.SetChildren(100, []dap.Variable{{Name: "x", Value: "42", Type: "int"}})
	m.col = colVars
	m.varSel = 1 // the "x" child (row 0 is the Locals scope)

	// Without capability, 'e' does nothing.
	m.Update(key("e"))
	if m.Editing() {
		t.Fatal("edit must be gated on supportsSetVariable")
	}

	m.SetEditable(true)
	m.Update(key("e"))
	if !m.Editing() {
		t.Fatal("'e' should open the editor on an editable child")
	}
	// Replace "42" with "99": backspace twice, type 99.
	m.Update(tea.KeyPressMsg{Code: tea.KeyBackspace})
	m.Update(tea.KeyPressMsg{Code: tea.KeyBackspace})
	m.Update(key("9"))
	cmd := m.Update(key("9")) // still editing until enter
	if cmd != nil {
		t.Fatal("typing should not emit a command")
	}
	cmd = m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("enter should commit")
	}
	sv, ok := cmd().(SetVarMsg)
	if !ok || sv.Ref != 100 || sv.Name != "x" || sv.Value != "99" {
		t.Fatalf("commit emitted %#v, want ref100 x=99", cmd())
	}
	if m.Editing() {
		t.Fatal("editor should close after commit")
	}
}

// TestVariableEditEscapeCancels verifies esc closes the editor without a
// command and leaves the value untouched.
func TestVariableEditEscapeCancels(t *testing.T) {
	m := New(nil)
	m.SetSize(80, 10)
	m.SetFrames(frames())
	m.SetScopes([]dap.Scope{{Name: "Locals", VariablesReference: 100}})
	m.SetChildren(100, []dap.Variable{{Name: "x", Value: "42"}})
	m.SetEditable(true)
	m.col = colVars
	m.varSel = 1
	m.Update(key("e"))
	m.Update(key("7"))
	if cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEscape}); cmd != nil {
		t.Fatal("escape should not emit a command")
	}
	if m.Editing() {
		t.Fatal("escape should close the editor")
	}
}

// TestScopeRootNotEditable verifies a scope root (parentRef 0) cannot be edited.
func TestScopeRootNotEditable(t *testing.T) {
	m := New(nil)
	m.SetSize(80, 10)
	m.SetFrames(frames())
	m.SetScopes([]dap.Scope{{Name: "Locals", VariablesReference: 100}})
	m.SetEditable(true)
	m.col = colVars
	m.varSel = 0 // the Locals scope row
	m.Update(key("e"))
	if m.Editing() {
		t.Fatal("a scope root has no settable value")
	}
}

// TestScopesRefreshCancelsEdit verifies #640: an async scopes replacement
// (frame switch, step) closes an open inline editor instead of leaving it
// over the new tree.
func TestScopesRefreshCancelsEdit(t *testing.T) {
	m := New(nil)
	m.SetSize(80, 10)
	m.SetFrames(frames())
	m.SetScopes([]dap.Scope{{Name: "Locals", VariablesReference: 100}})
	m.SetChildren(100, []dap.Variable{{Name: "x", Value: "42"}})
	m.SetEditable(true)
	m.col = colVars
	m.varSel = 1
	m.Update(key("e"))
	if !m.Editing() {
		t.Fatal("precondition: editor open")
	}
	m.SetScopes([]dap.Scope{{Name: "Locals", VariablesReference: 300}})
	if m.Editing() {
		t.Fatal("SetScopes must cancel an open editor")
	}
}

// TestChildrenRefreshCancelsEdit verifies #640: a children refresh (the rows
// under the edited ref may be replaced) closes an open inline editor so enter
// cannot commit a stale ref/name.
func TestChildrenRefreshCancelsEdit(t *testing.T) {
	m := New(nil)
	m.SetSize(80, 10)
	m.SetFrames(frames())
	m.SetScopes([]dap.Scope{{Name: "Locals", VariablesReference: 100}})
	m.SetChildren(100, []dap.Variable{{Name: "x", Value: "42"}})
	m.SetEditable(true)
	m.col = colVars
	m.varSel = 1
	m.Update(key("e"))
	if !m.Editing() {
		t.Fatal("precondition: editor open")
	}
	m.SetChildren(100, []dap.Variable{{Name: "y", Value: "1"}})
	if m.Editing() {
		t.Fatal("SetChildren must cancel an open editor")
	}
}

// TestEditorRowWindowedToColumn verifies #640: the inline editor row is
// windowed to the variables column width — a long value neither overflows
// into the next column nor hides the cursor.
func TestEditorRowWindowedToColumn(t *testing.T) {
	m := New(nil)
	m.SetSize(80, 10)
	m.SetFrames(frames())
	m.SetScopes([]dap.Scope{{Name: "Locals", VariablesReference: 100}})
	long := strings.Repeat("abcdefghij", 20) // far wider than any column
	m.SetChildren(100, []dap.Variable{{Name: "x", Value: long}})
	m.SetEditable(true)
	m.col = colVars
	m.varSel = 1
	m.Update(key("e"))
	if !m.Editing() {
		t.Fatal("precondition: editor open")
	}
	_, vw := m.colWidths()
	for i, row := range m.renderVars(vw) {
		if got := lipgloss.Width(row); got > vw {
			t.Fatalf("row %d width = %d, exceeds column width %d", i, got, vw)
		}
	}
	// The cursor (at the buffer end) stays visible: the window shows the
	// value's tail followed by the cursor cell.
	rows := m.renderVars(vw)
	if got := StripANSI(rows[2]); !strings.HasSuffix(got, "abcdefghij ") {
		t.Fatalf("editor row must window to the cursor at the tail, got %q", got)
	}
}

// TestStripANSI covers the escape classes: CSI, OSC (BEL and ESC\ terminated)
// and two-byte ESC sequences; \n/\r/\t pass through.
func TestStripANSI(t *testing.T) {
	cases := []struct{ in, want string }{
		{"plain", "plain"},
		{"\x1b[1;32mbold green\x1b[0m", "bold green"},
		{"\x1b]0;title\x07after", "after"},
		{"\x1b]8;;http://x\x1b\\link", "link"},
		{"\x1bcreset", "reset"},
		{"keep\r\n\ttabs", "keep\r\n\ttabs"},
		{"cut mid\x1b[", "cut mid"},
	}
	for _, c := range cases {
		if got := StripANSI(c.in); got != c.want {
			t.Errorf("StripANSI(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}
