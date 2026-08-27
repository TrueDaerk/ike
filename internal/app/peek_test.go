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

func TestPeekCandidatesPickInsideThePopup(t *testing.T) {
	_, files := navProject(t)
	m := newSized()
	tm, _ := m.openPath(files[0], false)
	m = tm.(Model)

	tm, _ = m.Update(ilsp.DefinitionCandidatesMsg{Refs: []ilsp.Reference{
		{Path: files[1], Line: 2, Col: 0, Preview: "l2"},
		{Path: files[2], Line: 4, Col: 0, Preview: "l4"},
	}, Peek: true})
	m = tm.(Model)

	// No modal list: the candidates live in the popup, the buffer stays put.
	if m.palette.IsOpen() {
		t.Fatal("a peek must not open the candidates picker (#2168)")
	}
	ed := peekEditor(t, m)
	if ed.Path() != files[0] {
		t.Fatalf("peek must not navigate; focused file = %s", ed.Path())
	}
	if !ed.PeekOpen() {
		t.Fatal("multiple targets must open the peek popup")
	}
	v := ed.PeekView()
	for _, want := range []string{
		filepath.Base(files[1]) + ":3", // header: first candidate, 1-based
		"(1/2)",                        // candidate counter
		filepath.Base(files[2]) + ":5", // the other candidate in the list
	} {
		if !strings.Contains(v, want) {
			t.Fatalf("popup must show %q, got:\n%s", want, v)
		}
	}

	// tab switches to the second candidate; enter jumps to that one.
	next, _ := ed.Update(tea.KeyPressMsg{Code: tea.KeyTab})
	*ed = next
	if v := ed.PeekView(); !strings.Contains(v, "(2/2)") {
		t.Fatalf("tab must select the next candidate, got:\n%s", v)
	}
	next, cmd := ed.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	*ed = next
	if cmd == nil {
		t.Fatal("enter must emit a jump command")
	}
	msg, ok := cmd().(ilsp.DefinitionMsg)
	if !ok || msg.Path != files[2] || msg.Line != 4 {
		t.Fatalf("enter must jump to the selected candidate, got %#v", cmd())
	}
	tm, _ = m.Update(msg)
	m = tm.(Model).atPosition(t, files[2], 4)

	// The non-peek path is untouched: candidates still open the picker and
	// keep jumping via DefinitionMsg.
	tm, _ = m.Update(ilsp.DefinitionCandidatesMsg{Refs: []ilsp.Reference{
		{Path: files[1], Line: 2, Col: 0, Preview: "l2"},
		{Path: files[2], Line: 4, Col: 0, Preview: "l4"},
	}})
	m = tm.(Model)
	if !m.palette.IsOpen() {
		t.Fatal("plain candidates must still open the picker")
	}
	if _, ok := m.refs.items[0].Msg.(ilsp.DefinitionMsg); !ok {
		t.Fatalf("plain candidates must keep the jump msg, got %#v", m.refs.items[0].Msg)
	}
}

func TestPeekCandidatesEscLeavesEverythingPut(t *testing.T) {
	_, files := navProject(t)
	m := newSized()
	tm, _ := m.openPath(files[0], false)
	m = tm.(Model)
	before := peekEditor(t, m)
	beforeLine, beforeCol := before.Cursor()

	tm, _ = m.Update(ilsp.DefinitionCandidatesMsg{Refs: []ilsp.Reference{
		{Path: files[1], Line: 2, Col: 0, Preview: "l2"},
		{Path: files[2], Line: 4, Col: 0, Preview: "l4"},
	}, Peek: true})
	m = tm.(Model)

	ed := peekEditor(t, m)
	next, cmd := ed.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	*ed = next
	if cmd != nil {
		t.Fatal("esc must not emit a jump")
	}
	if ed.PeekOpen() {
		t.Fatal("esc must close the peek")
	}
	line, col := ed.Cursor()
	if ed.Path() != files[0] || line != beforeLine || col != beforeCol {
		t.Fatalf("esc must restore the prior state: at %s:%d:%d, want %s:%d:%d",
			ed.Path(), line, col, files[0], beforeLine, beforeCol)
	}
}

func TestPeekManyCandidatesFallBackToPicker(t *testing.T) {
	_, files := navProject(t)
	m := newSized()
	tm, _ := m.openPath(files[0], false)
	m = tm.(Model)

	refs := make([]ilsp.Reference, 0, peekCandidateMax+1)
	for i := 0; i <= peekCandidateMax; i++ {
		refs = append(refs, ilsp.Reference{Path: files[1], Line: i % 9, Col: 0, Preview: "l"})
	}
	tm, _ = m.Update(ilsp.DefinitionCandidatesMsg{Refs: refs, Peek: true})
	m = tm.(Model)
	if !m.palette.IsOpen() {
		t.Fatalf("more than %d candidates must fall back to the picker", peekCandidateMax)
	}
	if peekEditor(t, m).PeekOpen() {
		t.Fatal("the picker fallback must not also open a popup")
	}
	// Picking one there still peeks rather than jumping.
	if _, ok := m.refs.items[0].Msg.(ilsp.PeekDefinitionMsg); !ok {
		t.Fatalf("fallback rows must carry PeekDefinitionMsg, got %#v", m.refs.items[0].Msg)
	}
}

func TestPeekCandidatesSkipUnreadableTargets(t *testing.T) {
	root, files := navProject(t)
	m := newSized()
	tm, _ := m.openPath(files[0], false)
	m = tm.(Model)

	tm, _ = m.Update(ilsp.DefinitionCandidatesMsg{Refs: []ilsp.Reference{
		{Path: filepath.Join(root, "gone.go"), Line: 0, Col: 0},
		{Path: files[2], Line: 4, Col: 0, Preview: "l4"},
	}, Peek: true})
	m = tm.(Model)

	ed := peekEditor(t, m)
	if !ed.PeekOpen() {
		t.Fatal("a readable candidate must still open the peek")
	}
	v := ed.PeekView()
	if strings.Contains(v, "gone.go") {
		t.Fatalf("unreadable candidates must be dropped, got:\n%s", v)
	}
	// One candidate left: no counter, no list.
	if strings.Contains(v, "(1/") {
		t.Fatalf("a single surviving candidate needs no counter, got:\n%s", v)
	}
}
