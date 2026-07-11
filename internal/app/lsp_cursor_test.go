package app

import (
	"testing"

	"ike/internal/host"
	ilsp "ike/internal/lsp"
)

// lsp_cursor_test.go pins #371: programmatic cursor placement (go-to-definition,
// usages picks, nav back/forward — everything funneled through openPathAt) must
// reach the host's editor-emitter seam as a cursor-move, so the LSP bridge's
// tracked position matches the visible cursor and rename/references right after
// a jump act on the landed symbol, not the departure.

// recordingEmitter captures host editor events like the LSP bridge would.
type recordingEmitter struct{ events []host.EditorEvent }

func (r *recordingEmitter) Emit(ev host.EditorEvent) { r.events = append(r.events, ev) }

// lastCursorMove returns the most recent cursor-move event, failing if none arrived.
func (r *recordingEmitter) lastCursorMove(t *testing.T) host.EditorEvent {
	t.Helper()
	for i := len(r.events) - 1; i >= 0; i-- {
		if r.events[i].Kind == host.EditorCursorMove {
			return r.events[i]
		}
	}
	t.Fatal("no cursor-move event emitted")
	return host.EditorEvent{}
}

func (r *recordingEmitter) assertAt(t *testing.T, path string, line, col int) {
	t.Helper()
	ev := r.lastCursorMove(t)
	if ev.Path != path || ev.Line != line || ev.Col != col {
		t.Fatalf("bridge position = %s:%d:%d, want %s:%d:%d",
			ev.Path, ev.Line, ev.Col, path, line, col)
	}
}

func TestProgrammaticJumpEmitsCursorMove(t *testing.T) {
	_, files := navProject(t)
	m := newSized()
	tm, _ := m.openPath(files[0], false)
	m = tm.(Model)

	// Installed after the first open: the LSP bridge registers itself as the
	// host emitter on file-open, and would otherwise displace the recorder.
	rec := &recordingEmitter{}
	m.host.SetEditorEmitter(rec)

	// Go-to-definition into another file: the bridge must learn the landing.
	tm, _ = m.Update(ilsp.DefinitionMsg{Path: files[1], Line: 5, Col: 1})
	m = tm.(Model).atPosition(t, files[1], 5)
	rec.assertAt(t, files[1], 5, 1)

	// Same-file jump (usages pick, search result).
	tm, _ = m.Update(ilsp.DefinitionMsg{Path: files[1], Line: 8, Col: 1})
	m = tm.(Model).atPosition(t, files[1], 8)
	rec.assertAt(t, files[1], 8, 1)

	// Nav back and forward retrace positions; each landing reaches the seam.
	tm, _ = m.Update(NavBackMsg{})
	m = tm.(Model).atPosition(t, files[1], 5)
	rec.assertAt(t, files[1], 5, 1)
	tm, _ = m.Update(NavForwardMsg{})
	m = tm.(Model).atPosition(t, files[1], 8)
	rec.assertAt(t, files[1], 8, 1)
}
