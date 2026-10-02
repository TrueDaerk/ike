package app

import (
	"fmt"

	tea "charm.land/bubbletea/v2"

	"ike/internal/agenttrace"
	"ike/internal/host"
	"ike/internal/tracepanel"
	"ike/internal/ui"
)

// agenttrace_revert.go is V in the agent trace (#2877): revert the change
// the selected change box or file node made. A node linked to a change-feed
// entry that still holds its pre-change content takes the feed's own revert
// (#2838). Everything else falls back to the transcript's reconstruction of
// the change (#2859): its hunks are turned around against the file as it is
// now (agenttrace.Revert), behind the same confirmation, into the buffer as
// one undoable edit. A change that cannot be reverted — a read, a create, a
// write whose previous content is unknown, a file edited since — says why;
// V is never silent.

// traceRevert is the transcript-based revert awaiting its confirmation.
type traceRevert struct {
	path string // absolute
	diff agenttrace.ChangeDiff
}

// traceRevertReadyMsg is the off-loop reconstruction's result.
type traceRevertReadyMsg struct {
	gen   int64
	cwd   string
	diff  agenttrace.ChangeDiff
	found bool
	err   error
}

// traceRevertNothing prefixes every "cannot revert" notice.
const traceRevertNothing = "nothing to revert: "

// traceRevertCmd answers V: the feed's prompt for a revertible linked
// entry, a notice for a row that is no change, else the transcript's diff,
// reconstructed off the loop.
func (m *Model) traceRevertCmd(req tracepanel.ChangeRevertMsg) tea.Cmd {
	if req.Linked != "" {
		if e, ok := m.feed.Get(req.Linked); ok && e.HasBefore() {
			m.openChangeFeedRevertPrompt(e)
			return nil
		}
	}
	switch {
	case req.Path == "":
		m.host.Notify(host.Info, traceRevertNothing+"the selected row is not a change")
		return nil
	case req.Read:
		m.host.Notify(host.Info, traceRevertNothing+"a read does not change the file")
		return nil
	}
	src, ok := m.traceDiffSource()
	if !ok {
		return nil
	}
	m.traceRevertGen++
	gen := m.traceRevertGen
	return func() tea.Msg {
		d, found, _, err := src.load(req.Key)
		return traceRevertReadyMsg{gen: gen, cwd: src.cwd, diff: d, found: found, err: err}
	}
}

// openTraceRevert checks the reconstruction against the file now and asks
// for the confirmation, or says why the change cannot be reverted.
func (m *Model) openTraceRevert(msg traceRevertReadyMsg) {
	if msg.gen != m.traceRevertGen {
		return
	}
	if !msg.found {
		why := traceRevertNothing + agenttrace.ErrNoSnapshot.Error() + " — the transcript records no diff for this change"
		if msg.err != nil {
			why = "agent trace: " + msg.err.Error()
		}
		m.host.Notify(host.Info, why)
		return
	}
	d := msg.diff
	path := traceAbs(d.Path, msg.cwd)
	if err := agenttrace.Revertible(d); err != nil {
		m.host.Notify(host.Info, traceRevertNothing+baseName(path)+": "+err.Error())
		return
	}
	now, why := m.traceRevertCurrent(path)
	if why != "" {
		m.host.Notify(host.Info, traceRevertNothing+baseName(path)+": "+why)
		return
	}
	if _, err := agenttrace.Revert(d, now); err != nil {
		m.host.Notify(host.Warn, "cannot revert "+baseName(path)+": "+err.Error())
		return
	}
	m.cfRevertTrace = &traceRevert{path: path, diff: d}
	what := d.Op.String()
	if d.Tool != "" {
		what += " (" + d.Tool + ")"
	}
	m.shell.SetContent(ui.ModelContent{
		Heading: "Revert agent change",
		Body: func() string {
			return fmt.Sprintf("%s: the agent's %s is undone, reconstructed %s.\n\n"+
				"The change is turned around in the buffer as one undoable edit — undo brings\n"+
				"it back, and the file on disk is untouched until you save.\n\n  [enter] revert\n  [esc]   cancel",
				displayPath(path), what, d.Source)
		},
	})
	m.shell.SetSize(m.width, m.height)
	m.shell.Open()
}

// traceRevertCurrent is the file's content now — its open buffer, else the
// disk — or why there is none.
func (m Model) traceRevertCurrent(path string) (string, string) {
	if ed := m.editorForPath(path); ed != nil {
		return ed.Text(), ""
	}
	text, why := traceWorkingFile(path)
	if why != "" {
		return "", why + " — " + agenttrace.ErrNoSnapshot.Error()
	}
	return text, ""
}

// applyTraceRevert is the confirmed revert: the buffer (opened when the
// file is not) is checked again — it may have changed behind the prompt —
// and replaced through the local-history restore path. A conflict leaves
// the file untouched.
func (m Model) applyTraceRevert(tr traceRevert) (tea.Model, tea.Cmd) {
	m, openCmd, ok := m.openForRevert(tr.path)
	if !ok {
		m.host.Notify(host.Warn, "could not open "+baseName(tr.path)+" to revert it")
		return m, openCmd
	}
	text, err := agenttrace.Revert(tr.diff, m.editorForPath(tr.path).Text())
	if err != nil {
		m.host.Notify(host.Warn, "cannot revert "+baseName(tr.path)+": "+err.Error())
		return m, openCmd
	}
	cmd, _ := m.applyBufferRestore(tr.path, text)
	m.host.Notify(host.Info, "reverted the agent's change to "+baseName(tr.path)+" — undo brings it back, save writes it")
	return m, tea.Batch(openCmd, cmd)
}
