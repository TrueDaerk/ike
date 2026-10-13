package app

// complexity_hints.go feeds the editor's inline complexity hints (0550,
// #2919): `CCN 12 · cog 8` at the end of every function header. The sync
// runs on the settled pass of every Update like the Structure pane's
// (#1025): every open document of a measurable language wants hints for one
// state key — its document version, the index generation (for a clean
// buffer, whose functions the index already holds), the warning thresholds
// and the master switch. A clean buffer the index knows is served on the
// spot from the snapshot: no parse, so opening a file or a finished walk
// shows hints in the same pass. A dirty buffer (or one the index has not
// reached) arms the 250 ms debounce; on the tick the buffer text is measured
// off-loop with complexity.Measure, stamped with the DocVersion it was taken
// at, and routed to every view of the path like vcs.MarksMsg — the editor
// drops a push whose version moved meanwhile, and the next settled pass
// re-arms for the newer one. complexity.enabled = false clears every
// buffer's hints once.

import (
	"strconv"
	"time"

	tea "charm.land/bubbletea/v2"

	"ike/internal/complexity"
	"ike/internal/config"
	"ike/internal/editor"
	"ike/internal/lang"
	"ike/internal/largefile"
	"ike/internal/pane"
)

// cxHintDebounce is how long a typing burst settles before the dirty buffer
// is re-measured, the structure-pane cadence (#2319).
const cxHintDebounce = 250 * time.Millisecond

// cxHintKey is the state one hint set answers for; a differing key means
// the editor's hints are (or will be) out of date.
type cxHintKey struct {
	version int  // the buffer's DocVersion
	gen     int  // index generation the hints came from; 0 when measured
	ccn     int  // complexity.ccn_warn
	cog     int  // complexity.cognitive_warn
	cmt     int  // complexity.comment_density_warn
	enabled bool // complexity.enabled
}

// complexityHintState is the sync's bookkeeping, shared by pointer across
// value-model copies like cxDiff: the key last delivered (or launched) per
// path and the key a debounce tick is armed for.
type complexityHintState struct {
	sent    map[string]cxHintKey
	pending map[string]cxHintKey
}

// complexityHintTickMsg is the debounce expiring for one path; a stale key
// (the buffer moved on since) is dropped.
type complexityHintTickMsg struct {
	path string
	key  cxHintKey
}

// complexityHintMeasuredMsg carries an off-loop measurement back: the
// functions of path's buffer text at version.
type complexityHintMeasuredMsg struct {
	path    string
	version int
	fns     []complexity.Function
	key     cxHintKey
}

// complexityHintsFor converts a file's functions into editor hints: one per
// function at its header line, Warn when a metric reaches its threshold.
// The comment density (#2934) joins the text only at or above cmtWarn
// percent (" · cmt 55%"), so the common hint stays short; 0 never shows it.
// Several functions on one line (lambdas beside their enclosing header)
// keep the first — the header's.
func complexityHintsFor(fns []complexity.Function, ccnWarn, cogWarn, cmtWarn int) []editor.ComplexityHint {
	if len(fns) == 0 {
		return nil
	}
	seen := make(map[int]bool, len(fns))
	out := make([]editor.ComplexityHint, 0, len(fns))
	for _, fn := range fns {
		if seen[fn.Line] {
			continue
		}
		seen[fn.Line] = true
		text := "CCN " + strconv.Itoa(fn.CCN) + " · cog " + strconv.Itoa(fn.Cognitive)
		overCmt := fn.CommentsOver(cmtWarn)
		if overCmt {
			text += " · cmt " + strconv.Itoa(complexity.DensityPercent(fn.CommentDensity)) + "%"
		}
		out = append(out, editor.ComplexityHint{
			Line: fn.Line,
			Text: text,
			Warn: fn.CCN >= ccnWarn || fn.Cognitive >= cogWarn || overCmt,
		})
	}
	return out
}

// cxHintMeasurable gates a buffer into the hint feed: a file of a language
// with a complexity spec, not excluded by complexity.exclude, not a
// degraded large file.
func (m Model) cxHintMeasurable(ed *editor.Model) bool {
	if ed == nil || !ed.HasFile() || ed.FeatureOff(largefile.FeatureLSP) {
		return false
	}
	l, ok := lang.ByPath(ed.Path())
	if !ok || l.Complexity == nil {
		return false
	}
	if x := m.complexityIndex; x != nil {
		return !complexity.Excluded(x.Root(), x.Options().Exclude, canonicalPath(ed.Path()))
	}
	return true
}

// cxHintKeyFor is the state key an editor's hints should answer for right
// now; indexed reports whether the index snapshot already holds the
// buffer's functions (clean buffer, file walked), the no-parse case.
func (m Model) cxHintKeyFor(ed *editor.Model, view complexity.View, c config.Complexity) (key cxHintKey, indexed bool) {
	key = cxHintKey{version: ed.DocVersion(), ccn: c.CCNWarn, cog: c.CognitiveWarn, cmt: c.CommentDensityWarn, enabled: c.Enabled}
	if !c.Enabled {
		return key, false
	}
	if !ed.Dirty() {
		if _, ok := view.Files[canonicalPath(ed.Path())]; ok {
			key.gen = view.Gen
			return key, true
		}
	}
	return key, false
}

// complexityHintsSyncCmd runs once per settled Update pass: for every open
// document it compares the wanted key with the delivered one and either
// serves the hints from the index, clears them (switch off), or arms the
// debounced off-loop measurement.
func (m *Model) complexityHintsSyncCmd() tea.Cmd {
	if m.complexityIndex == nil || m.cxHints == nil {
		return nil
	}
	c := config.Get().Complexity
	var view complexity.View
	if c.Enabled {
		view = m.complexityIndex.Snapshot()
	}
	st := m.cxHints
	var cmds []tea.Cmd
	seen := map[string]bool{}
	for _, paneKey := range m.activeWS().Panes.Keys() {
		inst := m.activeWS().Panes.Get(paneKey)
		if inst == nil || inst.Kind() != pane.KindEditor {
			continue
		}
		for _, ed := range inst.Editors() {
			if !m.cxHintMeasurable(ed) || seen[ed.Path()] {
				continue
			}
			seen[ed.Path()] = true
			path := ed.Path()
			key, indexed := m.cxHintKeyFor(ed, view, c)
			if st.sent[path] == key || st.pending[path] == key {
				continue
			}
			switch {
			case !c.Enabled:
				st.sent[path] = key
				delete(st.pending, path)
				if ed.HasComplexityHints() {
					cmds = append(cmds, m.routeToEditor(path, editor.ComplexityHintsMsg{Path: path, Version: key.version}))
				}
			case indexed:
				st.sent[path] = key
				delete(st.pending, path)
				fns := view.Files[canonicalPath(path)]
				cmds = append(cmds, m.routeToEditor(path, editor.ComplexityHintsMsg{
					Path: path, Hints: complexityHintsFor(fns, c.CCNWarn, c.CognitiveWarn, c.CommentDensityWarn), Version: key.version,
				}))
			default:
				st.pending[path] = key
				cmds = append(cmds, tea.Tick(cxHintDebounce, func(time.Time) tea.Msg {
					return complexityHintTickMsg{path: path, key: key}
				}))
			}
		}
	}
	// Documents closed since their last push: forget them, so a reopen
	// starts fresh.
	for path := range st.sent {
		if !seen[path] {
			delete(st.sent, path)
			delete(st.pending, path)
		}
	}
	if len(cmds) == 0 {
		return nil
	}
	return tea.Batch(cmds...)
}

// complexityHintTick launches the off-loop measurement when the tick's key
// is still what the buffer wants; a buffer that moved on (or was served
// from the index meanwhile) drops it — the settled pass re-armed.
func (m *Model) complexityHintTick(msg complexityHintTickMsg) tea.Cmd {
	st := m.cxHints
	if st == nil || st.pending[msg.path] != msg.key {
		return nil
	}
	delete(st.pending, msg.path)
	var ed *editor.Model
	for _, e := range m.editorViewsForPath(msg.path) {
		ed = e
		break
	}
	if ed == nil || ed.DocVersion() != msg.key.version || !msg.key.enabled {
		return nil
	}
	st.sent[msg.path] = msg.key
	path, version, text := msg.path, msg.key.version, ed.Text()
	key := msg.key
	return func() tea.Msg {
		return complexityHintMeasuredMsg{path: path, version: version, fns: cxMeasureText(path, text), key: key}
	}
}

// applyComplexityHints routes a landed measurement to every view of the
// path; the editor drops it when its version moved. A measurement that is
// no longer the sent key (a newer launch overtook it) is dropped here.
func (m *Model) applyComplexityHints(msg complexityHintMeasuredMsg) tea.Cmd {
	st := m.cxHints
	if st == nil || st.sent[msg.path] != msg.key {
		return nil
	}
	c := config.Get().Complexity
	return m.routeToEditor(msg.path, editor.ComplexityHintsMsg{
		Path: msg.path, Hints: complexityHintsFor(msg.fns, c.CCNWarn, c.CognitiveWarn, c.CommentDensityWarn), Version: msg.version,
	})
}
