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
// re-arms for the newer one. complexity.enabled = false or
// editor.marks.complexity = off clears every buffer's hints once; the mode
// (#2966) is part of the key, so all <-> warn repaints.
//
// The same measurement feeds the per-site marks (#2947): one Measure (or
// one snapshot lookup) per key derives both the header hints and the
// Sonar-style `+3 (nesting 2)` marks, pushed side by side. The site mode
// (editor.marks.complexity_sites) is part of the key and independent of the
// hint mode; a panel `m` (complexitypanel.ShowSitesMsg) pins one function's
// marks into the key until the buffer's version moves.

import (
	"sort"
	"strconv"
	"time"

	tea "charm.land/bubbletea/v2"

	"ike/internal/complexity"
	"ike/internal/complexitypanel"
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
	version int             // the buffer's DocVersion
	gen     int             // index generation the hints came from; 0 when measured
	ccn     int             // complexity.ccn_warn
	cog     int             // complexity.cognitive_warn
	cmt     int             // complexity.comment_density_warn
	enabled bool            // complexity.enabled and anything to show
	mode    config.HintMode // editor.marks.complexity (#2966)
	sites   config.HintMode // editor.marks.complexity_sites (#2947)
	show    int             // a panel `m` function's header line + 1; 0 none (#2947)
}

// complexityHintState is the sync's bookkeeping, shared by pointer across
// value-model copies like cxDiff: the key last delivered (or launched) per
// path, the key a debounce tick is armed for, and the panel's one-shot
// site requests (#2947).
type complexityHintState struct {
	sent    map[string]cxHintKey
	pending map[string]cxHintKey
	show    map[string]cxShowSites
}

// cxShowSites is a panel `m` (#2947): paint the sites of the function whose
// header is at line while the buffer stays at version.
type cxShowSites struct {
	line    int
	version int
}

// cxHintMeasure is the off-loop measurement seam; tests count calls.
var cxHintMeasure = cxMeasureText

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
// keep the first — the header's. warnOnly (editor.marks.complexity = warn,
// #2966) keeps only the Warn hints.
func complexityHintsFor(fns []complexity.Function, ccnWarn, cogWarn, cmtWarn int, warnOnly bool) []editor.ComplexityHint {
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
		warn := fn.CCN >= ccnWarn || fn.Cognitive >= cogWarn || overCmt
		if warnOnly && !warn {
			continue
		}
		out = append(out, editor.ComplexityHint{Line: fn.Line, Text: text, Warn: warn})
	}
	return out
}

// cxSiteWarn is the cognitive increment from which a site mark takes the
// warning tone: a nesting penalty of 2 or more on top of the structure.
const cxSiteWarn = 3

// complexitySiteMarksFor converts a file's functions into per-site marks
// (#2947): the cognitive sites of every function mode admits — all, or
// warn's functions at or above a threshold — plus the function whose
// header sits at showLine (a panel `m`, -1 for none), one mark per line.
// A site shared by a nested unit and its encloser keeps the first copy
// (the encloser's, at its nesting, as Sonar counts it); several sites on a
// line join in column order (`+1 · +1 &&`). CCN-only sites (switch arms)
// carry no cognitive cost and no mark.
func complexitySiteMarksFor(fns []complexity.Function, ccnWarn, cogWarn int, mode config.HintMode, showLine int) []editor.ComplexitySiteMark {
	type pos struct {
		line, col int
		kind      string
	}
	seen := map[pos]bool{}
	byLine := map[int][]complexity.Site{}
	for _, fn := range fns {
		over := fn.CCN >= ccnWarn || fn.Cognitive >= cogWarn
		if !(mode == config.HintAll || (mode == config.HintWarn && over) || fn.Line == showLine) {
			continue
		}
		for _, s := range fn.Sites {
			p := pos{s.Line, s.Col, s.Kind}
			if s.Cognitive == 0 || seen[p] {
				continue
			}
			seen[p] = true
			byLine[s.Line] = append(byLine[s.Line], s)
		}
	}
	if len(byLine) == 0 {
		return nil
	}
	out := make([]editor.ComplexitySiteMark, 0, len(byLine))
	for line, sites := range byLine {
		sort.Slice(sites, func(i, j int) bool { return sites[i].Col < sites[j].Col })
		mark := editor.ComplexitySiteMark{Line: line}
		for i, s := range sites {
			if i > 0 {
				mark.Text += " · "
			}
			mark.Text += cxSiteText(s)
			mark.Warn = mark.Warn || s.Cognitive >= cxSiteWarn
		}
		out = append(out, mark)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Line < out[j].Line })
	return out
}

// cxSiteText is one site's mark: `+1`, `+3 (nesting 2)`, or `+1 ||` for a
// boolean run, whose operator says which part of the line it is.
func cxSiteText(s complexity.Site) string {
	t := "+" + strconv.Itoa(s.Cognitive)
	if s.Nesting > 0 {
		t += " (nesting " + strconv.Itoa(s.Nesting) + ")"
	}
	switch s.Kind {
	case "&&", "||", "??", "and", "or":
		t += " " + s.Kind
	}
	return t
}

// cxMarkPushes routes one measurement of path at version to every view as
// both annotation stores: the header hints (none when the hint mode is
// off) and the site marks, each derived per the key.
func (m *Model) cxMarkPushes(path string, version int, fns []complexity.Function, key cxHintKey) tea.Cmd {
	var hints []editor.ComplexityHint
	if key.mode != config.HintOff {
		hints = complexityHintsFor(fns, key.ccn, key.cog, key.cmt, key.mode == config.HintWarn)
	}
	marks := complexitySiteMarksFor(fns, key.ccn, key.cog, key.sites, key.show-1)
	return tea.Batch(
		m.routeToEditor(path, editor.ComplexityHintsMsg{Path: path, Hints: hints, Version: version}),
		m.routeToEditor(path, editor.ComplexitySiteMarksMsg{Path: path, Marks: marks, Version: version}),
	)
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
func (m Model) cxHintKeyFor(ed *editor.Model, view complexity.View, c config.Complexity, marks config.Marks) (key cxHintKey, indexed bool) {
	key = cxHintKey{version: ed.DocVersion(), ccn: c.CCNWarn, cog: c.CognitiveWarn, cmt: c.CommentDensityWarn,
		mode: marks.Complexity, sites: marks.ComplexitySites}
	if req, ok := m.cxHints.show[ed.Path()]; ok {
		if req.version == key.version {
			key.show = req.line + 1
		} else {
			// The buffer moved on: the one-shot request is spent.
			delete(m.cxHints.show, ed.Path())
		}
	}
	key.enabled = cxHintsOn(c, marks) || c.Enabled && key.show > 0
	if !key.enabled {
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

// cxHintsOn reports whether hints or site marks are fed at all: the index
// is enabled and editor.marks.complexity (#2966) or
// editor.marks.complexity_sites (#2947) is not off.
func cxHintsOn(c config.Complexity, marks config.Marks) bool {
	return c.Enabled && (marks.Complexity != config.HintOff || marks.ComplexitySites != config.HintOff)
}

// complexityHintsSyncCmd runs once per settled Update pass: for every open
// document it compares the wanted key with the delivered one and either
// serves the hints from the index, clears them (switch off), or arms the
// debounced off-loop measurement.
func (m *Model) complexityHintsSyncCmd() tea.Cmd {
	if m.complexityIndex == nil || m.cxHints == nil {
		return nil
	}
	cfg := config.Get()
	c, marks := cfg.Complexity, cfg.Editor.Marks
	var view complexity.View
	if cxHintsOn(c, marks) || len(m.cxHints.show) > 0 {
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
			key, indexed := m.cxHintKeyFor(ed, view, c, marks)
			if st.sent[path] == key || st.pending[path] == key {
				continue
			}
			switch {
			case !key.enabled:
				st.sent[path] = key
				delete(st.pending, path)
				if ed.HasComplexityHints() {
					cmds = append(cmds, m.routeToEditor(path, editor.ComplexityHintsMsg{Path: path, Version: key.version}))
				}
				if ed.HasComplexitySiteMarks() {
					cmds = append(cmds, m.routeToEditor(path, editor.ComplexitySiteMarksMsg{Path: path, Version: key.version}))
				}
			case indexed:
				st.sent[path] = key
				delete(st.pending, path)
				cmds = append(cmds, m.cxMarkPushes(path, key.version, view.Files[canonicalPath(path)], key))
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
	for path := range st.show {
		if !seen[path] {
			delete(st.show, path)
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
		return complexityHintMeasuredMsg{path: path, version: version, fns: cxHintMeasure(path, text), key: key}
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
	return m.cxMarkPushes(msg.path, msg.version, msg.fns, msg.key)
}

// showComplexitySites handles a panel `m` (#2947): pin the function's site
// marks into its buffer's key at the current version — opening the file
// at the function when no view shows it — so the next settled pass paints
// them whatever editor.marks.complexity_sites says. An edit moves the
// version and spends the request.
func (m Model) showComplexitySites(msg complexitypanel.ShowSitesMsg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd
	if len(m.editorViewsForPath(msg.Path)) == 0 {
		var out tea.Model
		out, cmd = m.openPathAt(msg.Path, msg.Line, 0)
		m = out.(Model)
	}
	for _, ed := range m.editorViewsForPath(msg.Path) {
		if m.cxHints != nil {
			m.cxHints.show[ed.Path()] = cxShowSites{line: msg.Line, version: ed.DocVersion()}
		}
		break
	}
	return m, cmd
}
