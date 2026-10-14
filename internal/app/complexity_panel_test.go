package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"ike/internal/complexity"
	"ike/internal/complexitypanel"
	"ike/internal/editor"
	"ike/internal/pane"
)

// complexity_panel_test.go covers the Complexity tool window's app half
// (#2917): the toggle state machine, the seed from the index snapshot, the
// generation-change refresh, the caret follow, the open-location jump, the
// CSV export and the copy.

func TestComplexityToggleLifecycle(t *testing.T) {
	m, _ := timeApp(t)
	before := m.activeWS().Panes.Focused()

	out, _ := m.Update(ComplexityToggleMsg{})
	m = out.(Model)
	if !m.activeWS().Panes.Has(pane.ComplexityKey) || m.activeWS().Panes.Focused() != pane.ComplexityKey {
		t.Fatalf("first toggle must open + focus the panel (focus=%q)", m.activeWS().Panes.Focused())
	}
	p := m.complexityPanel()
	if p == nil {
		t.Fatal("no panel")
	}
	if p.Tab() != complexitypanel.TabProject {
		t.Fatalf("tab = %v, want Project", p.Tab())
	}
	if col, desc := p.SortColumn(); col != complexitypanel.ColCCN || !desc {
		t.Fatalf("sort = %v desc=%v, want CCN descending", col, desc)
	}
	if !strings.Contains(m.statusLine(), "COMPLEXITY") {
		t.Errorf("status line must name the pane: %q", m.statusLine())
	}
	// tab belongs to the focused window (its Project / File switch), not
	// to the global focus cycle. (The first-start onboarding dialog would
	// swallow the key on a machine missing a language server.)
	m = dismissOnboarding(m)
	out, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyTab})
	m = out.(Model)
	if m.activeWS().Panes.Focused() != pane.ComplexityKey || m.complexityPanel().Tab() != complexitypanel.TabFile {
		t.Fatalf("tab must switch the pane's tab (focus=%q tab=%v)", m.activeWS().Panes.Focused(), m.complexityPanel().Tab())
	}

	out, _ = m.Update(ComplexityToggleMsg{})
	m = out.(Model)
	if m.activeWS().Panes.Focused() != before {
		t.Fatalf("focus = %q, want %q", m.activeWS().Panes.Focused(), before)
	}

	out, _ = m.Update(ComplexityToggleMsg{})
	m = out.(Model)
	if m.activeWS().Panes.Focused() != pane.ComplexityKey {
		t.Fatal("third toggle must re-focus the panel")
	}
}

// complexityApp builds a model whose project root holds one Go file the
// index can measure, and waits for the walk to finish.
func complexityApp(t *testing.T) (Model, string) {
	t.Helper()
	m, _ := timeApp(t)
	idx := m.ComplexityIndex()
	if idx == nil || !idx.Available() {
		t.Skip("no syntax tree in this build")
	}
	root := idx.Root()
	src := filepath.Join(root, "complexity_panel.go")
	if _, err := os.Stat(src); err != nil {
		t.Skipf("the project root %s does not hold the panel source", root)
	}
	idx.Start()
	deadline := time.Now().Add(20 * time.Second)
	for !idx.ScanDone() && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
	if !idx.ScanDone() {
		t.Fatal("the project walk did not finish")
	}
	return m, src
}

func TestComplexityPanelSeedsFollowsAndOpens(t *testing.T) {
	m, src := complexityApp(t)
	out, _ := m.Update(ComplexityToggleMsg{})
	m = out.(Model)
	p := m.complexityPanel()
	if p == nil || p.Total() == 0 {
		t.Fatalf("the panel must seed from the finished walk (total=%d)", p.Total())
	}
	// A generation change re-reads the snapshot.
	gen := m.ComplexityIndex().Snapshot().Gen
	out, _ = m.Update(ComplexityChangedMsg{Gen: gen + 1})
	m = out.(Model)
	if m.complexityPanel().Total() == 0 {
		t.Fatal("the refresh dropped the rows")
	}
	// Open the panel's own source and put the caret inside Refresh: the
	// File tab follows and marks it.
	out, _ = m.openPathAt(src, 0, 0)
	m = out.(Model)
	ed := m.editorForPath(src)
	if ed == nil {
		t.Fatal("the source did not open")
	}
	line := -1
	for i, l := range strings.Split(string(mustRead(t, src)), "\n") {
		if strings.HasPrefix(l, "func (m *Model) syncComplexityPanel() {") {
			line = i + 2
			break
		}
	}
	if line < 0 {
		t.Fatal("syncComplexityPanel not found in the source")
	}
	ed.JumpTo(line, 0)
	out, _ = m.Update(tea.WindowSizeMsg{Width: 120, Height: 40}) // a settled pass
	m = out.(Model)
	p = m.complexityPanel()
	p.SetTab(complexitypanel.TabFile)
	if got := filepath.Base(p.File()); got != "complexity_panel.go" {
		t.Fatalf("followed file = %q", p.File())
	}
	i := p.CaretRow()
	if i < 0 || p.Rows()[i].Fn.Name != "Model.syncComplexityPanel" {
		t.Fatalf("caret row = %d (%+v), want Model.syncComplexityPanel", i, rowName(p, i))
	}
	// Enter on the row jumps the editor to the function header; nav.back
	// returns to where the caret was.
	row := p.Rows()[i]
	out, _ = m.Update(complexitypanel.OpenLocationMsg{Path: row.Path, Line: row.Fn.Line})
	m = out.(Model)
	if l, _ := m.editorForPath(src).CursorPos(); l != row.Fn.Line {
		t.Fatalf("caret line = %d, want %d", l, row.Fn.Line)
	}
	out, _ = m.Update(NavBackMsg{})
	m = out.(Model)
	if l, _ := m.editorForPath(src).CursorPos(); l != line {
		t.Fatalf("after nav.back caret line = %d, want %d", l, line)
	}
}

// TestComplexityCandidateOpensSelection: an OpenLocationMsg with an EndLine
// past Line (an extraction candidate, #2948) opens the file and leaves a
// linewise visual selection over the range, caret on the header; a
// line-only message keeps the plain jump.
func TestComplexityCandidateOpensSelection(t *testing.T) {
	m, src := complexityApp(t)
	out, _ := m.Update(ComplexityToggleMsg{})
	m = out.(Model)
	out, _ = m.Update(complexitypanel.OpenLocationMsg{Path: src, Line: 20, EndLine: 28})
	m = out.(Model)
	ed := m.editorForPath(src)
	if ed == nil {
		t.Fatal("the source did not open")
	}
	if ed.ModeName() != editor.VisualLine {
		t.Fatalf("mode = %v, want a visual line selection", ed.ModeName())
	}
	if l, _ := ed.CursorPos(); l != 20 {
		t.Errorf("caret line = %d, want the header 20", l)
	}
	text, ok := ed.SelectionText()
	if !ok || strings.Count(text, "\n") < 8 {
		t.Errorf("selection = %q (ok=%v), want nine lines", text, ok)
	}
	// A plain function jump moves the caret only (the editor's mode is the
	// jump's business, as for every jump surface); the anchor stays put.
	out, _ = m.Update(complexitypanel.OpenLocationMsg{Path: src, Line: 5})
	m = out.(Model)
	ed = m.editorForPath(src)
	if l, _ := ed.CursorPos(); l != 5 {
		t.Errorf("after a plain jump: line %d, want 5", l)
	}
	if text, _ := ed.SelectionText(); strings.Count(text, "\n") < 20 {
		t.Errorf("the anchor moved: selection %q", text)
	}
}

func rowName(p *complexitypanel.Model, i int) string {
	if i < 0 || i >= len(p.Rows()) {
		return ""
	}
	return p.Rows()[i].Fn.Name
}

func mustRead(t *testing.T, path string) []byte {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func TestComplexityExportWritesCSVScratch(t *testing.T) {
	m, _ := timeApp(t)
	out, _ := m.Update(complexitypanel.ExportMsg{Label: "Project", CSV: "file,function\na.go,f\n"})
	m = out.(Model)

	ed := m.activeEditor()
	if ed == nil || !ed.HasFile() {
		t.Fatal("the export did not open the scratch file")
	}
	if filepath.Ext(ed.Path()) != ".csv" {
		t.Errorf("path = %q, want a .csv scratch", ed.Path())
	}
	data, err := os.ReadFile(ed.Path())
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "file,function\na.go,f\n" {
		t.Errorf("scratch content = %q", data)
	}
}

// TestComplexityExportMarkdownScratch: complexity.exportMarkdown runs the
// pane's 'E' (#2963), whose message writes a .md scratch and opens it.
func TestComplexityExportMarkdownScratch(t *testing.T) {
	m, _ := timeApp(t)
	out, cmd := m.Update(ComplexityExportMarkdownMsg{})
	m = out.(Model)
	if m.complexityPanel() == nil || cmd == nil {
		t.Fatal("the palette export must open the panel and run 'E'")
	}
	msg, ok := cmd().(complexitypanel.ExportMarkdownMsg)
	if !ok || msg.Label != "Project" || !strings.Contains(msg.Markdown, "| file | function |") {
		t.Fatalf("E yields %+v", msg)
	}
	out, _ = m.Update(msg)
	m = out.(Model)
	ed := m.activeEditor()
	if ed == nil || !ed.HasFile() || filepath.Ext(ed.Path()) != ".md" {
		t.Fatal("the Markdown export did not open a .md scratch")
	}
	if data := mustRead(t, ed.Path()); string(data) != msg.Markdown {
		t.Errorf("scratch content = %q", data)
	}
}

func TestComplexityCopyAndRefreshMessages(t *testing.T) {
	m, _ := timeApp(t)
	out, _ := m.Update(complexitypanel.CopyMsg{Text: "a.go:1: f ccn=2 cog=1"})
	m = out.(Model)
	if !strings.Contains(cxNotices(m), "copied a.go:1: f ccn=2 cog=1") {
		t.Fatalf("copy must notify: %q", cxNotices(m))
	}
	// 'r' rebuilds the index; a disabled index says so instead.
	out, _ = m.Update(complexitypanel.RefreshMsg{})
	m = out.(Model)
	m.ComplexityIndex().Reconfigure(complexity.Options{Enabled: false})
	out, _ = m.Update(ComplexityRefreshMsg{})
	m = out.(Model)
	if !strings.Contains(cxNotices(m), "complexity.enabled") {
		t.Fatalf("a disabled index must be reported: %q", cxNotices(m))
	}
}

// cxNotices joins the notification texts the model drained so far.
func cxNotices(m Model) string {
	var out []string
	for _, h := range m.history {
		out = append(out, h.text)
	}
	return strings.Join(out, "\n")
}
