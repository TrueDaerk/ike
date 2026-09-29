package app

import (
	"strings"

	"ike/internal/host"
	"ike/internal/layout"
	"ike/internal/pane"
)

// playsplit.go is the playground's detached result pane (#2797). The mode
// mounts inline in the pane it queries (#1970): the query header on top, the
// result buffer underneath. On a wide terminal that stacks two things that
// want width, and the result cannot be moved, resized or put beside the
// source. playground.splitResult moves the result into a layout leaf of its
// own — a pane.KindPlayResult placeholder split off to the right of the
// source pane — while the query header stays pinned on the source pane, over
// the document it queries. The pane is a real leaf: it resizes, moves, zooms
// and closes with the workspace's own machinery, and the pane switcher and
// the ctrl+arrows reach it like any other.
//
// What the split changes is *where the result is drawn and which pane the
// keyboard is in*; nothing else. The state stays one playState: the result
// editor, the folds, the strip, the table view, the search, the chain and
// the history are the same objects, so every result action works unchanged.
// The one new rule is the focus round trip — tab from the query line focuses
// the result pane and tab from there focuses the source pane again — which
// the two panes keep in step with bufFocus (syncPlayResultPane): a focus move
// into the result pane is a tab into the result, a tab is a focus move.
//
// The pane is session state and is never persisted: saveLayout and the named
// layouts prune its leaf (prunePlayResultLeaves), a restore never has to know
// the kind, and the next split re-creates it. Closing the pane (pane.close,
// a drag that empties it, a layout apply) re-attaches the result inline;
// closing the playground removes the pane. Across a project switch the leaf
// parks with the workspace tree and the key with the parked playState, so
// the resumed model finds both where it left them.

// SplitPlayResultMsg is playground.splitResult's message (#2797): the result
// moves into a pane of its own beside the source — or, already split, back
// under the query header.
type SplitPlayResultMsg struct{}

// detached reports whether the result lives in its own pane.
func (s *playState) detached() bool { return s != nil && s.resultKey != "" }

// resultHost is the pane the result is drawn in: the detached pane while
// split, else the source pane the mode mounted in.
func (s *playState) resultHost() string {
	if s.resultKey != "" {
		return s.resultKey
	}
	return s.paneKey
}

// playResultPaneIs reports whether pane key is the playground's detached
// result pane.
func (m Model) playResultPaneIs(key string) bool {
	s := m.play
	return s != nil && s.resultKey != "" && s.resultKey == key
}

// playResultShownIn reports whether pane key draws the result buffer: the
// detached pane while split, else the source pane while the inline mode
// owns it (playInlineActive). The wheel, the selection drag and the mode
// colour of the border key off it.
func (m Model) playResultShownIn(key string) bool {
	s := m.play
	if s == nil {
		return false
	}
	if s.resultKey != "" {
		return key == s.resultKey
	}
	return m.playInlineActive(key)
}

// togglePlaySplit is playground.splitResult: split the result out, or put it
// back. Without a playground there is nothing to split, and the notification
// says so rather than the command doing nothing.
func (m *Model) togglePlaySplit() {
	s := m.play
	if s == nil {
		m.host.Notify(host.Info, "no playground open — split needs one")
		return
	}
	if s.resultKey != "" {
		m.reattachPlayResult()
		return
	}
	if !m.detachPlayResult() {
		m.host.Notify(host.Warn, "the result cannot be split out of this pane")
	}
}

// detachPlayResult splits the result into its own pane to the right of the
// source pane, reporting whether the leaf made it into the tree. The keyboard
// stays where it is: on the query line the source pane keeps the focus, in
// the result buffer the focus follows the result into the new pane.
func (m *Model) detachPlayResult() bool {
	s := m.play
	ws := m.activeWS()
	if s == nil || s.resultKey != "" || ws.Tree == nil || !ws.Panes.Has(s.paneKey) {
		return false
	}
	key := ws.Panes.AddPlayResult()
	tree, ok := layout.SplitLeaf(ws.Tree, s.paneKey, key, layout.ZoneRight)
	if !ok {
		ws.Panes.Close(key)
		return false
	}
	ws.Tree = tree
	s.resultKey = key
	if s.bufFocus && ws.Panes.Focused() == s.paneKey {
		m.setFocus(key)
	}
	s.syncedFocus, s.syncedBuf = ws.Panes.Focused(), s.bufFocus
	m.layout()
	saveLayout(ws.Tree, ws.Panes)
	return true
}

// reattachPlayResult puts the result back under the query header: the
// detached pane leaves the tree and the registry, and a keyboard that was in
// it lands on the source pane, where the inline result now is. It is also
// the settled pass's answer to a pane closed from outside (pane.close, a
// layout apply): the leaf is gone already, the state catches up.
func (m *Model) reattachPlayResult() {
	s := m.play
	if s == nil || s.resultKey == "" {
		return
	}
	key := s.resultKey
	ws := m.activeWS()
	hadKeys := ws.Panes.Focused() == key || s.syncedFocus == key
	s.resultKey = ""
	m.removePlayResultPane(key, s.paneKey)
	if hadKeys && ws.Panes.Focused() != s.paneKey && ws.Panes.Has(s.paneKey) {
		m.setFocus(s.paneKey)
	}
	s.syncedFocus, s.syncedBuf = ws.Panes.Focused(), s.bufFocus
	m.layout()
}

// removePlayResultPane closes the detached result pane key: the leaf leaves
// the tree, the placeholder the registry. A focus standing on it moves to
// fallback (the source pane) — or, with that gone too, wherever a closed
// pane's focus goes. closePlayground calls it with the mode already gone, so
// the focus move sees no playground.
func (m *Model) removePlayResultPane(key, fallback string) {
	ws := m.activeWS()
	if !ws.Panes.Has(key) {
		return
	}
	focused := ws.Panes.Focused() == key
	if !m.closeKey(key) {
		ws.Panes.Close(key) // not in the tree (a layout apply replaced it): registry only
	}
	if focused {
		if ws.Panes.Has(fallback) {
			m.setFocus(fallback)
		} else {
			m.setFocus(m.focusAfterClose())
		}
	}
	m.layout()
	saveLayout(ws.Tree, ws.Panes)
}

// playFocusResult moves the keyboard into the result: tab from the query
// line, the find chord. In the detached layout that is a pane focus move
// into the result pane; inline it is the bufFocus flag alone.
func (m *Model) playFocusResult() {
	s := m.play
	if s == nil {
		return
	}
	if s.resultKey != "" && m.activeWS().Panes.Has(s.resultKey) {
		m.setFocus(s.resultKey)
	}
	s.setBufFocus(true)
}

// playFocusQuery is playFocusResult's return trip: tab from the result, esc
// out of a search the find chord opened from the query line.
func (m *Model) playFocusQuery() {
	s := m.play
	if s == nil {
		return
	}
	if s.resultKey != "" && m.activeWS().Panes.Has(s.paneKey) {
		m.setFocus(s.paneKey)
	}
	s.setBufFocus(false)
}

// syncPlayResultPane keeps the detached layout consistent on the settled
// Update pass. A result pane that left the workspace — closed with
// pane.close, replaced by a layout apply — re-attaches the result inline.
// Otherwise the pane focus and bufFocus are reconciled: a focus that moved
// (ctrl+arrows, the switcher, a click) decides bufFocus, and a bufFocus the
// mode changed without moving the focus (the strip's esc, a table's tab, an
// inserted filter) moves the focus after it. The pane focus wins when both
// changed in one pass — it is the user's own gesture.
func (m *Model) syncPlayResultPane() {
	s := m.play
	if s == nil || s.resultKey == "" {
		return
	}
	ws := m.activeWS()
	if !ws.Panes.Has(s.resultKey) || !layout.Panes(ws.Tree)[s.resultKey] {
		m.reattachPlayResult()
		return
	}
	focused := ws.Panes.Focused()
	switch {
	case focused != s.syncedFocus:
		if focused == s.resultKey && !s.bufFocus {
			s.setBufFocus(true)
		} else if focused == s.paneKey && s.bufFocus {
			s.setBufFocus(false)
		}
	case s.bufFocus != s.syncedBuf && (focused == s.paneKey || focused == s.resultKey):
		target := s.paneKey
		if s.bufFocus {
			target = s.resultKey
		}
		m.setFocus(target)
	}
	s.syncedFocus, s.syncedBuf = ws.Panes.Focused(), s.bufFocus
}

// playResultPaneTitle is the detached pane's chrome: the dialect, then the
// queried snapshot, so the pane says what it shows the result of.
func (m Model) playResultPaneTitle() string {
	s := m.play
	return strings.ToUpper(s.dialect.Name()) + " RESULT — " + s.source
}

// playResultPaneBody is the detached pane's content: the stale banner when
// the run failed, then the result buffer (or the table view) — the inline
// body's lower half, without the query header.
func (m Model) playResultPaneBody(width int) string {
	s := m.play
	if width < 20 {
		width = 20
	}
	body := ""
	if s.playStale() {
		body = m.playStaleBanner(width) + "\n"
	}
	s.resultEd.SetDimmed(s.playDimmed())
	return body + m.playResultView(width)
}

// playResultChromeRows is the vertical chrome the detached pane adds above
// the result buffer — the stale banner's row — for the mouse translation
// (contentYOff) and the buffer's height.
func (m Model) playResultChromeRows(key string) int {
	if m.playResultPaneIs(key) {
		return m.playStaleRows()
	}
	return 0
}

// playSourceLang is the language of the document the playground queries,
// for the palette's file-type ranking while the detached result pane is
// focused: the result pane has no buffer of its own, and the commands that
// apply are the ones for its source.
func (m Model) playSourceLang() string {
	s := m.play
	if s == nil {
		return ""
	}
	if s.srcInst != nil {
		if h := s.srcInst.HTTP(); h != nil {
			return h.BodyLang()
		}
		return ""
	}
	if s.srcEd != nil {
		return s.srcEd.LangID()
	}
	return ""
}

// prunePlayResultLeaves is the persisted layout's view of the tree: the
// detached result pane is session state and never saved, so its leaf comes
// out before encoding — on a clone, since layout.Close edits in place and
// the live tree keeps the pane. The neighbour takes its space, exactly as
// if the pane had been closed.
func prunePlayResultLeaves(root layout.Node, reg *pane.Registry) layout.Node {
	if root == nil || reg == nil {
		return root
	}
	var pruned layout.Node
	for _, key := range layout.Leaves(root) {
		inst := reg.Get(key)
		if inst == nil || inst.Kind() != pane.KindPlayResult {
			continue
		}
		if pruned == nil {
			pruned = layout.Clone(root)
		}
		if next, ok := layout.Close(pruned, key); ok {
			pruned = next
		}
	}
	if pruned == nil {
		return root
	}
	return pruned
}
