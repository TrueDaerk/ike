package app

import (
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"ike/internal/editor"
	ilsp "ike/internal/lsp"
)

// peek_test.go covers peek definition (#1154): the PeekDefinitionMsg opens a
// cursor-anchored excerpt popup on the focused editor instead of navigating,
// Enter inside jumps through the shared DefinitionMsg funnel (nav history
// records), the excerpt source is the live buffer for open files and a
// bounded disk read otherwise, and a multi-target answer routes through the
// candidates picker with peek intent preserved.

// peekEditor returns the focused editor or fails the test.
func peekEditor(t *testing.T, m Model) *editor.Model {
	t.Helper()
	ed := m.focusedEditor()
	if ed == nil {
		t.Fatal("no focused editor")
	}
	return ed
}

func TestPeekDefinitionOpensPopupWithoutNavigating(t *testing.T) {
	_, files := navProject(t)
	m := newSized()
	tm, _ := m.openPath(files[0], false)
	m = tm.(Model)

	tm, _ = m.Update(ilsp.PeekDefinitionMsg{Path: files[1], Line: 5, Col: 0})
	m = tm.(Model)

	ed := peekEditor(t, m)
	if got := ed.Path(); got != files[0] {
		t.Fatalf("peek must not navigate; focused file = %s, want %s", got, files[0])
	}
	if !ed.PeekOpen() {
		t.Fatal("peek popup must be open on the focused editor")
	}
	v := ed.PeekView()
	if !strings.Contains(v, filepath.Base(files[1])+":6") {
		t.Fatalf("title must be path:line (1-based), got:\n%s", v)
	}
	// Context starts a few lines above the definition line…
	for _, want := range []string{"l2", "l5", "l9"} {
		if !strings.Contains(v, want) {
			t.Fatalf("excerpt must contain %q, got:\n%s", want, v)
		}
	}
	// …and nothing before it (bounded read from disk: start = line-3).
	if strings.Contains(v, "l1") {
		t.Fatalf("excerpt must start at l2, got:\n%s", v)
	}
}

func TestPeekEnterJumpsAndRecordsNavHistory(t *testing.T) {
	_, files := navProject(t)
	m := newSized()
	tm, _ := m.openPath(files[0], false)
	m = tm.(Model)
	tm, _ = m.Update(ilsp.PeekDefinitionMsg{Path: files[1], Line: 5, Col: 1})
	m = tm.(Model)

	// Enter inside the peek emits the shared DefinitionMsg jump.
	ed := peekEditor(t, m)
	next, cmd := ed.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	*ed = next
	if cmd == nil {
		t.Fatal("enter in the peek must emit a jump command")
	}
	msg, ok := cmd().(ilsp.DefinitionMsg)
	if !ok {
		t.Fatalf("expected an ilsp.DefinitionMsg, got %#v", cmd())
	}
	tm, _ = m.Update(msg)
	m = tm.(Model).atPosition(t, files[1], 5)

	// The jump recorded: nav.back returns to the origin file.
	tm, _ = m.Update(NavBackMsg{})
	m = tm.(Model).atPosition(t, files[0], 0)
	_ = m
}

func TestPeekUsesLiveBufferForOpenFiles(t *testing.T) {
	_, files := navProject(t)
	m := newSized()
	tm, _ := m.openPath(files[1], false)
	m = tm.(Model)
	// Unsaved edit in the open target buffer: replace "l5" with "EDITED".
	m.editorViewsForPath(files[1])[0].ApplyTextEdits([]editor.TextEdit{
		{StartLine: 5, StartCol: 0, EndLine: 5, EndCol: 2, Text: "EDITED"},
	})
	tm, _ = m.openPath(files[0], false)
	m = tm.(Model)

	tm, _ = m.Update(ilsp.PeekDefinitionMsg{Path: files[1], Line: 5, Col: 0})
	m = tm.(Model)
	if v := peekEditor(t, m).PeekView(); !strings.Contains(v, "EDITED") {
		t.Fatalf("peek must read the live buffer, not stale disk:\n%s", v)
	}
}

func TestPeekUnreadableTargetNotifies(t *testing.T) {
	root, files := navProject(t)
	m := newSized()
	tm, _ := m.openPath(files[0], false)
	m = tm.(Model)

	tm, _ = m.Update(ilsp.PeekDefinitionMsg{Path: filepath.Join(root, "gone.go"), Line: 0, Col: 0})
	m = tm.(Model)
	if peekEditor(t, m).PeekOpen() {
		t.Fatal("an unreadable target must not open a peek")
	}
	if len(m.toasts) == 0 || !strings.Contains(m.toasts[0].text, "peek definition") {
		t.Fatalf("disk read failure must surface as a notice, got %+v", m.toasts)
	}
}

func TestPeekCandidatesRouteThroughPicker(t *testing.T) {
	_, files := navProject(t)
	m := newSized()
	tm, _ := m.openPath(files[0], false)
	m = tm.(Model)

	tm, _ = m.Update(ilsp.DefinitionCandidatesMsg{Refs: []ilsp.Reference{
		{Path: files[1], Line: 2, Col: 0, Preview: "l2"},
		{Path: files[2], Line: 4, Col: 0, Preview: "l4"},
	}, Peek: true})
	m = tm.(Model)
	if !m.palette.IsOpen() {
		t.Fatal("multiple targets must open the candidates picker")
	}
	// Activating a candidate peeks instead of jumping.
	msg, ok := m.refs.items[0].Msg.(ilsp.PeekDefinitionMsg)
	if !ok {
		t.Fatalf("picker rows must carry PeekDefinitionMsg, got %#v", m.refs.items[0].Msg)
	}
	if msg.Path != files[1] || msg.Line != 2 {
		t.Fatalf("candidate target = %+v", msg)
	}
	// The non-peek picker keeps jumping via DefinitionMsg.
	tm, _ = m.Update(ilsp.DefinitionCandidatesMsg{Refs: []ilsp.Reference{
		{Path: files[1], Line: 2, Col: 0, Preview: "l2"},
		{Path: files[2], Line: 4, Col: 0, Preview: "l4"},
	}})
	m = tm.(Model)
	if _, ok := m.refs.items[0].Msg.(ilsp.DefinitionMsg); !ok {
		t.Fatalf("plain candidates must keep the jump msg, got %#v", m.refs.items[0].Msg)
	}
}
