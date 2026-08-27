package app

import (
	"os"
	"path/filepath"
	"testing"

	tea "charm.land/bubbletea/v2"

	"ike/internal/editor/buffer"
	"ike/internal/host"
	ilsp "ike/internal/lsp"
	"ike/internal/palette"
	"ike/internal/plugin"
	"ike/internal/problems"
	"ike/internal/registry"
)

// problems_quickfix_test.go covers the app half of #2175: the pane's key runs
// the bridge command, the continuation gets the marked row, the offer opens
// anchored in the pane, and an empty offer says so.

// quickFixRecorder is a stand-in for the LSP plugin's lsp.quickFixProblem: it
// answers with a continuation that records the request instead of talking to
// a server, so the app's half is testable without one.
type quickFixRecorder struct{ got *ilsp.QuickFixRequest }

func (q quickFixRecorder) ID() string { return "fakelsp" }

func (q quickFixRecorder) Capabilities() plugin.Capabilities {
	return plugin.Capabilities{Commands: []plugin.Command{{
		ID:    "lsp.quickFixProblem",
		Title: "LSP: Quick-Fix Marked Problem",
		Scope: plugin.GlobalScope(),
		Run: func(h host.API) tea.Cmd {
			return func() tea.Msg {
				return ilsp.QuickFixPromptMsg{Apply: func(req ilsp.QuickFixRequest) tea.Cmd {
					*q.got = req
					return nil
				}}
			}
		},
	}}}
}

// quickFixApp opens the Problems pane over one publish and returns the model
// plus the slot the recorded request lands in.
func quickFixApp(t *testing.T) (Model, *ilsp.QuickFixRequest) {
	t.Helper()
	t.Setenv("IKE_CONFIG_DIR", t.TempDir())
	got := &ilsp.QuickFixRequest{}
	reg := registry.New()
	reg.Add(quickFixRecorder{got: got})
	m := NewWith(reg, host.MapConfig{})
	out, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	m = out.(Model)
	out, _ = m.Update(ProblemsToggleMsg{})
	m = out.(Model)
	out, _ = m.Update(ilsp.DiagnosticsMsg{Path: "/proj/a.go", Diagnostics: []ilsp.Diagnostic{{
		Range:    buffer.Range{Start: buffer.Position{Line: 4, Col: 2}, End: buffer.Position{Line: 4, Col: 8}},
		Severity: 1,
		Message:  "undefined: fooBar",
	}}})
	return out.(Model), got
}

// quickFixOffer is a two-action server answer for the marked row.
func quickFixOffer(picked *int) ilsp.CodeActionsMsg {
	return ilsp.CodeActionsMsg{
		Path:     "/proj/a.go",
		QuickFix: true,
		Actions: []ilsp.CodeActionChoice{
			{Title: "Import fooBar", Kind: "quickfix"},
			{Title: "Declare fooBar", Kind: "quickfix", Preferred: true},
		},
		Apply: func(i int) tea.Cmd {
			*picked = i
			return nil
		},
	}
}

// TestProblemsQuickFixKeyRequestsTheMarkedRow is the end-to-end request half:
// "a" in the focused pane runs the bridge command and the continuation is
// called with exactly the marked diagnostic's path and range — no editor was
// opened, no caret moved.
func TestProblemsQuickFixKeyRequestsTheMarkedRow(t *testing.T) {
	m, got := quickFixApp(t)

	// Down onto the diagnostic under the file header, then the fix key.
	out, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	m = out.(Model)
	out, cmd := m.Update(tea.KeyPressMsg{Code: 'a', Text: "a"})
	m = out.(Model)
	if cmd == nil {
		t.Fatal(`"a" must ask the app for quick fixes`)
	}
	if _, ok := cmd().(problems.QuickFixMsg); !ok {
		t.Fatalf("pane msg = %#v", cmd())
	}
	out, cmd = m.Update(problems.QuickFixMsg{})
	m = out.(Model)
	if cmd == nil {
		t.Fatal("the app must run the bridge command")
	}
	m = drainCmd(m, cmd)
	if got.Path != "/proj/a.go" {
		t.Fatalf("request = %+v, want the marked row's file", got)
	}
	if got.Range.Start.Line != 4 || got.Range.Start.Col != 2 || got.Range.End.Col != 8 {
		t.Fatalf("request range = %+v, want the diagnostic's own range", got.Range)
	}
	// No editor was opened on the way: the fix starts from the pane.
	if m.activeEditor() != nil && m.activeEditor().Path() == "/proj/a.go" {
		t.Fatal("the quick fix must not require a jump to the location")
	}
}

// TestProblemsQuickFixAltEnterReachesThePane guards the alias against the
// keymap: alt+enter is the editor's lsp.codeAction chord, but that binding is
// Editor-scoped, so in the Problems context the key must fall through to the
// pane instead of being swallowed.
func TestProblemsQuickFixAltEnterReachesThePane(t *testing.T) {
	m, _ := quickFixApp(t)
	out, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	m = out.(Model)

	out, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter, Mod: tea.ModAlt})
	m = out.(Model)
	if cmd == nil {
		t.Fatal("alt+enter must reach the pane in the Problems context")
	}
	if _, ok := cmd().(problems.QuickFixMsg); !ok {
		t.Fatalf("alt+enter msg = %#v, want QuickFixMsg", cmd())
	}
}

// TestProblemsQuickFixOfferOpensAnchored: a non-empty offer lists the actions
// in the picker, anchored in the pane rather than centered.
func TestProblemsQuickFixOfferOpensAnchored(t *testing.T) {
	m, _ := quickFixApp(t)
	picked := -1

	out, _ := m.Update(quickFixOffer(&picked))
	m = out.(Model)
	if !m.palette.IsOpen() {
		t.Fatal("a quick-fix offer must open the picker")
	}
	if !m.palette.Anchored() {
		t.Fatal("the quick-fix popup must anchor in the pane, not center")
	}
	items := m.actions.Results("", palette.Context{})
	if len(items) != 2 || items[0].Title != "★ Declare fooBar" {
		t.Fatalf("items = %+v, want the preferred fix first", items)
	}
	// No caret exists, so no built-in intention may sneak into the list.
	for _, it := range items {
		if it.Detail != "quick fix" {
			t.Fatalf("unexpected non-LSP row %+v", it)
		}
	}
}

// TestProblemsQuickFixApplyRunsTheContinuation: picking a row runs the
// bridge's Apply for the original offer index, which is where the shared
// WorkspaceEdit path (undoable in the buffer) takes over.
func TestProblemsQuickFixApplyRunsTheContinuation(t *testing.T) {
	m, _ := quickFixApp(t)
	picked := -1

	out, _ := m.Update(quickFixOffer(&picked))
	m = out.(Model)
	items := m.actions.Results("", palette.Context{})
	out, _ = m.Update(items[0].Msg)
	m = out.(Model)
	if picked != 1 {
		t.Fatalf("picked = %d, want the preferred action's original index 1", picked)
	}
}

// TestProblemsQuickFixEmptyOfferReportsNoFixes is the no-action criterion: an
// empty answer leaves the picker shut and says so out loud.
func TestProblemsQuickFixEmptyOfferReportsNoFixes(t *testing.T) {
	m, _ := quickFixApp(t)

	out, _ := m.Update(ilsp.CodeActionsMsg{Path: "/proj/a.go", QuickFix: true})
	m = out.(Model)
	if m.palette.IsOpen() {
		t.Fatal("an empty offer must not open an empty picker")
	}
	if !noticed(m, "no quick fixes for this problem") {
		t.Fatalf("missing the no-fixes feedback: %+v", m.history)
	}
}

// TestProblemsQuickFixWithoutPaneSaysSo: the command is reachable from the
// palette too, where the pane may simply not be open.
func TestProblemsQuickFixWithoutPaneSaysSo(t *testing.T) {
	m := problemsApp(t) // the pane was never opened
	var got ilsp.QuickFixRequest

	out, _ := m.Update(ilsp.QuickFixPromptMsg{Apply: func(req ilsp.QuickFixRequest) tea.Cmd {
		got = req
		return nil
	}})
	m = out.(Model)
	if got.Path != "" {
		t.Fatalf("nothing may be requested without the pane, got %+v", got)
	}
	if !noticed(m, "open the Problems pane first") {
		t.Fatalf("missing the closed-pane feedback: %+v", m.history)
	}
}

// TestProblemsQuickFixRefreshesAfterTheFix: the pane is a pure consumer, so
// the diagnostic disappears on the publish the applied edit triggers — no
// bespoke refresh path.
func TestProblemsQuickFixRefreshesAfterTheFix(t *testing.T) {
	m, _ := quickFixApp(t)
	if p := m.problemsPanel(); p == nil || p.Rows() != 2 {
		t.Fatalf("setup: rows = %v", p)
	}
	out, _ := m.Update(ilsp.DiagnosticsMsg{Path: "/proj/a.go"})
	m = out.(Model)
	p := m.problemsPanel()
	if p == nil || p.Rows() != 0 {
		t.Fatalf("the fixed file must drop out of the pane, rows = %v", p)
	}
	if _, _, ok := p.SelectedDiagnostic(); ok {
		t.Fatal("an emptied pane has nothing left to fix")
	}
}

// TestProblemsQuickFixEditIsUndoable is the undo criterion (#2175): the fix
// reaches the buffer through the shared FormatEditsMsg route, which applies it
// as one history change — so a single "u" in the editor takes it back.
func TestProblemsQuickFixEditIsUndoable(t *testing.T) {
	m := sized(t, 100, 40)
	dir := t.TempDir()
	file := filepath.Join(dir, "main.go")
	if err := os.WriteFile(file, []byte("func main(){\nfooBar()\n}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	tm, _ := m.openPath(file, false)
	m = tm.(Model)

	// Exactly what applyAction dispatches for a file an editor holds open.
	out, _ := m.Update(ilsp.FormatEditsMsg{Path: file, Edits: []ilsp.FormatEdit{
		{StartLine: 1, StartCol: 0, EndLine: 1, EndCol: 6, Text: "FooBar"},
	}})
	m = out.(Model)
	ed := m.activeWS().Panes.Get(m.activeEditorKey()).Editor()
	if got := ed.Text(); got != "func main(){\nFooBar()\n}" {
		t.Fatalf("the fix must land in the buffer, got %q", got)
	}

	out, _ = m.Update(tea.KeyPressMsg{Code: 'u', Text: "u"})
	m = out.(Model)
	ed = m.activeWS().Panes.Get(m.activeEditorKey()).Editor()
	if got := ed.Text(); got != "func main(){\nfooBar()\n}" {
		t.Fatalf("one undo must take the whole fix back, got %q", got)
	}
}
