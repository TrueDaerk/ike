package app

import (
	"os"

	tea "charm.land/bubbletea/v2"

	"ike/internal/host"
	"ike/internal/undostore"
	"ike/internal/watch"
)

// playwatch.go keeps an open playground's input in step with the file it was
// snapshotted from (#2356). The snapshot principle itself stands — the input
// is not re-read per keystroke — but a *detected external change* is exactly
// the event the snapshot was never meant to survive: a result describing a
// document that is no longer on disk answers a question nobody asked, and
// says nothing about being stale.
//
// Three decisions this file encodes:
//
//   - The refresh is **automatic**, not an offered hint. The one argument for
//     a hint is cost, and the cost is already bounded: parses past
//     jqplay.AsyncThreshold run off the event loop, the watcher debounces
//     bursts into one flush, and a superseded parse or run is dropped on its
//     generation stamp. A "press r to reload" banner would trade that for a
//     question the user has to answer before every look.
//   - Only a **whole-file** editor source is followed (playInputSource.path).
//     An HTTP response is not a file; a selection's character range means
//     something else after an edit; an unsaved buffer has no path.
//   - The source of truth is the **buffer**, not the file. The event triggers
//     the refresh, but the text comes from the editor showing the path — so a
//     dirty buffer, which the editor deliberately does not auto-reload
//     (internal/editor/reload.go), does not have foreign content pushed under
//     its own unsaved edits either. The digest check makes that a no-op rather
//     than a wasted parse.
//
// IKE's own saves never arrive here: the watcher suppresses them at ingest
// and at flush through its save epoch (Roadmap 0140), so a save from inside
// the playground's own pane cannot masquerade as a foreign change.

// playWatchEvent re-reads the playground's input when the file it follows
// changed, and ends the mode in a defined state when that file disappears.
// It is called from routeWatchEvent *after* the event reached the editors, so
// the buffer this reads from has already applied its own reload.
func (m *Model) playWatchEvent(msg watch.EventMsg) tea.Cmd {
	s := m.play
	if s == nil || s.srcPath == "" || canonicalPath(s.srcPath) != canonicalPath(msg.Path) {
		return nil
	}
	switch msg.Kind {
	case watch.FileRemoved:
		m.playSourceRemoved()
		return nil
	case watch.FileChanged, watch.FileCreated:
		// A rename-in-place save (write temp + rename) coalesces to either
		// kind; both are content changes of the followed file.
	default:
		return nil
	}
	text, ok := m.playSourceText(s)
	if !ok || undostore.Hash([]byte(text)) == s.srcHash {
		// Nothing readable, or the bytes the parser already saw: a stale
		// dirty buffer, auto-reload switched off, or a touch that wrote the
		// same content. Silence is the honest answer — claiming a reload that
		// changed nothing would make the info row's stamp meaningless.
		return nil
	}
	return m.playRefreshInput(text)
}

// playSourceText is the followed document's current text: the **queried
// editor model** itself (#2355 pins it on the state, so this is the document
// the playground was opened over even when its pane now shows another tab),
// any other buffer holding the path if that model is gone, and the file on
// disk as the last resort.
func (m Model) playSourceText(s *playState) (string, bool) {
	if s.srcEd != nil {
		return s.srcEd.Text(), true
	}
	if ed := m.editorForPath(s.srcPath); ed != nil {
		return ed.Text(), true
	}
	data, err := os.ReadFile(s.srcPath)
	if err != nil {
		return "", false
	}
	return string(data), true
}

// playRefreshInput re-parses the input and re-runs the current program against
// it. It is a program change with a new input, not a reopen: query, cursor,
// history position, the expanded view and the result buffer's focus all stay
// exactly as they were, and only what the program is evaluated *over* moves.
//
// The generation bump is what keeps a burst honest: it abandons the run in
// flight and invalidates any debounce tick already scheduled, so five saves in
// a row leave one result on screen — the last one's.
func (m *Model) playRefreshInput(text string) tea.Cmd {
	s := m.play
	s.cancelRun()
	s.gen++
	s.pending = true
	s.reloadedAt = m.clock()
	s.status, s.statusWarn = "input reloaded — the source file changed", false
	return m.parsePlayInput(text)
}

// playSourceRemoved ends the playground when the file it follows is deleted or
// renamed away. With the buffer still open and dirty it holds the only copy of
// the document, so the mode stays up over content that still exists and says
// what happened; otherwise the root model is about to close the hosting pane
// (routeWatchEvent), and a playground whose input, file and pane are all gone
// has nothing left to be — it closes with a message naming the file, rather
// than vanishing with the pane unexplained.
func (m *Model) playSourceRemoved() {
	s := m.play
	ed := s.srcEd
	if ed == nil {
		ed = m.editorForPath(s.srcPath)
	}
	if ed != nil && ed.Dirty() {
		s.status = "source file removed on disk — the result is the unsaved buffer"
		s.statusWarn = true
		return
	}
	name, dialect := baseName(s.srcPath), s.dialect.Name()
	m.closePlayground()
	m.host.Notify(host.Warn, dialect+" playground closed: "+name+" was removed")
}
