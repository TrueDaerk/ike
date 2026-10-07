package app

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"ike/internal/agentask"
	"ike/internal/agenttrace"
	"ike/internal/diff"
	"ike/internal/host"
	"ike/internal/tracepanel"
	"ike/internal/ui"
	"ike/internal/vcs"
)

// agenttrace_diff.go is the trace's per-change diff view (#2859): D on a
// change box or file node shows what the agent changed, in the floating
// shell. The diff is the change feed's exact one when the node links to an
// entry (#2838), else the reconstruction from the transcript
// (agenttrace.Diffs — structuredPatch, old/new string, a Write's content);
// the header names its provenance. 1 / 2 / 3 switch the left side between
// the content before the change, git HEAD and the working file; the right
// side stays the change's after content, so the HEAD and working-file bases
// answer "what of this change is still there".

// traceDiffBase is the left side of the diff view.
type traceDiffBase int

const (
	baseSession traceDiffBase = iota
	baseHead
	baseWork
)

// traceDiffState is the open diff view.
type traceDiffState struct {
	diff agenttrace.ChangeDiff
	base traceDiffBase
	// head and work are the comparison bases; a non-empty *Why disables
	// the base and says why.
	head, headWhy string
	work, workWhy string
	// linked is the change-feed path the node links to, for f.
	linked string
	// key is the node the view was opened or stepped to — where ← / → and
	// n / p step from (#2910).
	key string
}

// traceDroppedNotice explains a stored change whose hunks the record cap
// dropped (#2882); its counts stay in the view's header.
const traceDroppedNotice = "hunks of this change were dropped when the session was stored"

// traceDiffReadyMsg is the off-loop reconstruction's result.
type traceDiffReadyMsg struct {
	gen   int64
	req   tracepanel.DiffMsg
	diff  agenttrace.ChangeDiff
	found bool
	// record reports a diff from a stored session's record (#2882).
	record bool
	err    error
	head   string
	headOK string // "" when head is valid, else why not
	work   string
	workOK string
	// step marks a reconstruction ← / → / n / p started (#2910): it moves
	// the pane's selection onto the node and keeps base when it applies.
	step bool
	base traceDiffBase
}

// traceDiffCmd reconstructs the node's diff off the loop: the transcript is
// parsed again (subagents included — the node may be one of their calls),
// and both comparison bases are read.
func (m *Model) traceDiffCmd(req tracepanel.DiffMsg) tea.Cmd {
	src, ok := m.traceDiffSource()
	if !ok {
		return nil
	}
	m.traceDiffGen++
	gen := m.traceDiffGen
	return func() tea.Msg {
		out := traceDiffReadyMsg{gen: gen, req: req}
		out.diff, out.found, out.record, out.err = src.load(req.Key)
		path := req.Path
		if out.found {
			path = out.diff.Path
		}
		path = traceAbs(path, src.cwd)
		out.work, out.workOK = traceWorkingFile(path)
		out.head, out.headOK = traceHeadFile(path)
		return out
	}
}

// traceDiffSrc is where D and V find a node's diff: the live session's
// transcript, or — while the pane shows a stored session (#2882) — the
// record's diffs, with the record's transcript as a second source while
// Claude Code still keeps it.
type traceDiffSrc struct {
	transcript, cwd string
	stored          bool
	diffs           []agenttrace.ChangeDiff
}

// traceDiffSource is the shown session's diff source and working directory
// (the project root when it has none); false while the pane is not there.
// A stored session never consults the live reader: its keys are not the
// live transcript's.
func (m Model) traceDiffSource() (traceDiffSrc, bool) {
	p := m.agentTracePanel()
	if p == nil {
		return traceDiffSrc{}, false
	}
	src := traceDiffSrc{transcript: p.Info().Transcript, cwd: p.Info().CWD}
	switch {
	case m.traceShowingHistory():
		src.stored, src.diffs = true, m.traceHistoryDiffs
	case m.traceReader != nil:
		src.transcript = m.traceReader.Path()
	}
	if src.cwd == "" {
		src.cwd = projectRoot()
	}
	return src, true
}

// load finds the node key's diff; record reports that it is the stored
// record's (hunks and counts only). Off the loop only: it may parse the
// transcript and read files.
func (s traceDiffSrc) load(key string) (d agenttrace.ChangeDiff, found, record bool, err error) {
	if !s.stored {
		d, found, err = loadTraceDiff(s.transcript, key)
		return d, found, false, err
	}
	d, found = agenttrace.DiffFor(s.diffs, key)
	// The transcript, when it is still there, recovers the whole contents
	// the record never keeps; a parse failure just leaves the record's.
	if s.transcript != "" {
		if _, statErr := os.Stat(s.transcript); statErr == nil {
			if td, ok, _ := loadTraceDiff(s.transcript, key); ok && (td.HasAfter || len(td.Hunks) > 0) {
				return td, true, false, nil
			}
		}
	}
	return d, found, found, nil
}

// loadTraceDiff parses the transcript again (subagents included — the node
// may be one of their calls) and reconstructs the diff the node key shows.
// Off the loop only: it reads the transcript and the files.
func loadTraceDiff(transcript, key string) (agenttrace.ChangeDiff, bool, error) {
	if transcript == "" {
		return agenttrace.ChangeDiff{}, false, nil
	}
	sess, err := agenttrace.Load(transcript)
	if sess == nil {
		return agenttrace.ChangeDiff{}, false, err
	}
	d, found := agenttrace.DiffFor(agenttrace.Diffs(sess), key)
	return d, found, err
}

// traceAbs resolves a transcript path against the session's directory.
func traceAbs(path, cwd string) string {
	if path != "" && !filepath.IsAbs(path) {
		path = filepath.Join(cwd, path)
	}
	return path
}

// traceWorkingFile is the file on disk now, in the buffer's native form.
func traceWorkingFile(path string) (string, string) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return "", "the file no longer exists"
		}
		return "", "unreadable: " + err.Error()
	}
	text, err := normalizeBufferText(data)
	if err != nil {
		return "", "undecodable: " + err.Error()
	}
	return text, ""
}

// traceHeadFile is the file's blob at git HEAD; the reason when there is
// none — no repository, or a file HEAD does not track.
func traceHeadFile(path string) (string, string) {
	root, err := vcs.DetectRoot(filepath.Dir(path))
	if err != nil || root == "" {
		return "", "not a git repository"
	}
	text, err := vcs.HeadContent(root, path)
	if err != nil {
		return "", "not tracked in HEAD"
	}
	text, err = normalizeBufferText([]byte(text))
	if err != nil {
		return "", "undecodable at HEAD: " + err.Error()
	}
	return text, ""
}

// openTraceDiff shows a finished reconstruction. A linked node takes the
// change feed's exact diff when the entry still holds its pre-change
// content; a node with neither says so.
func (m *Model) openTraceDiff(msg traceDiffReadyMsg) {
	if msg.gen != m.traceDiffGen {
		return
	}
	d, found := msg.diff, msg.found
	if !found {
		d = agenttrace.ChangeDiff{Key: msg.req.Key, Path: msg.req.Path}
	}
	if msg.req.Linked != "" {
		if e, ok := m.feed.Get(msg.req.Linked); ok && e.HasBefore() {
			if after, problem := m.changeFeedAfter(e); problem == "" {
				d.Source, d.Note = agenttrace.DiffFeed, "the feed's capture before the write, against the file now"
				d.Before, d.HasBefore = e.Before, true
				d.After, d.HasAfter = after, true
				d.Hunks = nil
				found = true
			}
		}
	}
	if !found {
		why := "no diff for this change in the transcript"
		if msg.err != nil {
			why = "agent trace: " + msg.err.Error()
		}
		m.host.Notify(host.Info, why)
		return
	}
	if d.Dropped && len(d.Hunks) == 0 && !d.HasAfter {
		m.host.Notify(host.Info, traceDroppedNotice)
	}
	st := &traceDiffState{diff: d, head: msg.head, headWhy: msg.headOK, work: msg.work, workWhy: msg.workOK, linked: msg.req.Linked, key: msg.req.Key}
	if !d.HasAfter {
		why := "the transcript holds only the changed hunks — the whole after content is unknown"
		if msg.record {
			why = "stored session — the record kept only the changed hunks, the whole after content is unknown"
		}
		if st.headWhy == "" {
			st.headWhy = why
		}
		if st.workWhy == "" {
			st.workWhy = why
		}
	}
	if msg.step {
		if p := m.agentTracePanel(); p != nil {
			p.Select(msg.req.Key)
		}
		if why := st.disabled(msg.base); why != "" {
			m.host.Notify(host.Info, traceBaseLabel(msg.base)+": "+why)
		} else {
			st.base = msg.base
		}
	}
	m.traceDiff = st
	m.shell.SetAccent(nil)
	m.shell.SetContent(&traceDiffContent{m: m, st: st})
	m.shell.SetSize(m.width, m.height)
	m.shell.Open()
}

// traceDiffOpen reports whether the shell shows the diff view.
func (m Model) traceDiffOpen() bool {
	if m.traceDiff == nil || !m.shell.IsOpen() {
		return false
	}
	_, ok := m.shell.Content().(*traceDiffContent)
	return ok
}

// closeTraceDiff dismisses the view.
func (m *Model) closeTraceDiff() {
	m.traceDiff = nil
	m.shell.Close()
}

// updateTraceDiff handles a key while the view is open: the base switch,
// f for the linked feed entry, ← / → and n / p to step through the
// changes, esc; the rest scrolls.
func (m Model) updateTraceDiff(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	st := m.traceDiff
	switch key := msg.String(); key {
	case "esc", "q":
		m.closeTraceDiff()
	case "1", "2", "3":
		base := traceDiffBase(key[0] - '1')
		if why := st.disabled(base); why != "" {
			m.host.Notify(host.Info, traceBaseLabel(base)+": "+why)
			return m, nil
		}
		st.base = base
		m.shell.SetContent(&traceDiffContent{m: &m, st: st})
	case "f":
		if st.linked == "" {
			return m, nil
		}
		m.closeTraceDiff()
		if _, ok := m.traceChangeEntry(st.linked); ok {
			m.openChangeFeedAt(st.linked)
		}
	case "right", "n", "left", "p":
		return m, m.stepTraceDiff(key == "right" || key == "n")
	default:
		m.shell.Update(msg)
	}
	return m, nil
}

// stepTraceDiff loads the diff of the change after (or before) the shown
// one along the trace (#2910), keeping the picked base; the view stays
// open on the current diff until the new one is ready, and at an end of
// the trace says so.
func (m *Model) stepTraceDiff(next bool) tea.Cmd {
	st := m.traceDiff
	p := m.agentTracePanel()
	if p == nil {
		return nil
	}
	dir, end := 1, "no next change"
	if !next {
		dir, end = -1, "no previous change"
	}
	req, ok := p.NextChange(st.key, dir)
	if !ok {
		m.host.Notify(host.Info, end)
		return nil
	}
	cmd := m.traceDiffCmd(req)
	if cmd == nil {
		return nil
	}
	base := st.base
	return func() tea.Msg {
		out, ok := cmd().(traceDiffReadyMsg)
		if ok {
			out.step, out.base = true, base
		}
		return out
	}
}

// disabled is why a base cannot be picked, "" when it can.
func (st *traceDiffState) disabled(b traceDiffBase) string {
	switch b {
	case baseHead:
		return st.headWhy
	case baseWork:
		return st.workWhy
	}
	return ""
}

func traceBaseLabel(b traceDiffBase) string {
	switch b {
	case baseHead:
		return "git HEAD"
	case baseWork:
		return "working file"
	}
	return "session before"
}

// traceDiffContent renders the view at the shell's width budget.
type traceDiffContent struct {
	m  *Model
	st *traceDiffState
	// cache holds the last rendering by width — the engine's diff is not
	// free, and a base switch installs a fresh content.
	cacheW int
	cache  string
}

// Title implements ui.Content.
func (c *traceDiffContent) Title() string { return "Change diff — " + filepath.Base(c.st.diff.Path) }

// Render implements ui.Content.
func (c *traceDiffContent) Render(width int) string {
	width = max(30, width)
	if c.cacheW != width || c.cache == "" {
		c.cache, c.cacheW = c.render(width), width
	}
	return c.cache
}

func (c *traceDiffContent) render(width int) string {
	pal := c.m.pal()
	dim := lipgloss.NewStyle().Foreground(pal.Hint)
	d := c.st.diff
	// The file, then the op, the turn, the counts and the provenance.
	what := d.Op.String()
	if d.Tool != "" {
		what += " (" + d.Tool + ")"
	}
	if d.Turn > 0 {
		what += " · turn #" + strconv.Itoa(d.Turn)
	}
	if d.Counted || d.HasBefore && d.HasAfter {
		what += " · " + c.counts()
	}
	lines := []string{
		lipgloss.NewStyle().Bold(true).Render(clipLeft(displayPath(d.Path), width)),
		ansi.Truncate(what+" · "+lipgloss.NewStyle().Foreground(pal.Accent).Render(d.Source.String()), width, "…"),
	}
	if d.Note != "" {
		lines = append(lines, dim.Render(ansi.Truncate(d.Note, width, "…")))
	}
	seg := ui.Segmented{}
	for b := baseSession; b <= baseWork; b++ {
		label := strconv.Itoa(int(b)+1) + " " + traceBaseLabel(b)
		if c.st.disabled(b) != "" {
			label += " ✗"
		}
		seg.Segments = append(seg.Segments, ui.Segment{Label: label, On: b == c.st.base})
	}
	lines = append(lines, "", seg.View(width, pal))
	for b := baseHead; b <= baseWork; b++ {
		if why := c.st.disabled(b); why != "" {
			lines = append(lines, dim.Render(ansi.Truncate(traceBaseLabel(b)+": "+why, width, "…")))
		}
	}
	lines = append(lines, "")
	body := c.body(width)
	hint := "1 session before · 2 git HEAD · 3 working file"
	if c.st.linked != "" {
		hint += " · f change feed"
	}
	hint += " · ←/→ n/p prev/next change · esc close"
	return strings.Join(lines, "\n") + "\n" + body + "\n\n" + dim.Render(ansi.Truncate(hint, width, "…"))
}

// clipLeft cuts a path to width cells from the left — the file name is the
// part that must survive.
func clipLeft(s string, width int) string {
	if w := lipgloss.Width(s); w > width {
		return ansi.TruncateLeft(s, w-width+1, "…")
	}
	return s
}

// counts is the header's "+N −M": the recorded hunks' when there are any,
// else the whole contents' diff.
func (c *traceDiffContent) counts() string {
	d := c.st.diff
	added, removed := d.Added, d.Removed
	if !d.Counted || d.Source == agenttrace.DiffFeed {
		added, removed = 0, 0
		res := diff.Compute(d.Before, d.After)
		for _, r := range res.Rows {
			switch r.Kind {
			case diff.RowAdded:
				added++
			case diff.RowRemoved:
				removed++
			case diff.RowChanged:
				added++
				removed++
			}
		}
	}
	return "+" + strconv.Itoa(added) + " −" + strconv.Itoa(removed)
}

// body is the diff itself against the picked base.
func (c *traceDiffContent) body(width int) string {
	pal := c.m.pal()
	dim := lipgloss.NewStyle().Foreground(pal.Hint)
	d := c.st.diff
	switch c.st.base {
	case baseHead, baseWork:
		left := c.st.head
		if c.st.base == baseWork {
			left = c.st.work
		}
		return c.whole(left, d.After, width, "no differences — "+traceBaseLabel(c.st.base)+" holds exactly this change's result")
	}
	switch {
	case d.HasBefore && d.HasAfter:
		return c.whole(d.Before, d.After, width, "no differences — the change left the file as it was")
	case len(d.Hunks) > 0:
		// Only hunks: the ask overlay's markdown renderer, diff-fenced.
		return strings.TrimRight(agentask.RenderMarkdown("```diff\n"+d.Unified()+"\n```", width, pal), "\n")
	case d.HasAfter:
		return dim.Render("before unknown — the whole after content:") + "\n" + c.whole("", d.After, width, "(empty file)")
	case d.Dropped:
		return dim.Render(ansi.Truncate(traceDroppedNotice, width, "…"))
	}
	return dim.Render("nothing known about this change")
}

// whole renders the engine's diff of two whole contents the way the change
// feed's mini-diff does.
func (c *traceDiffContent) whole(left, right string, width int, same string) string {
	pal := c.m.pal()
	dim := lipgloss.NewStyle().Foreground(pal.Hint)
	res := diff.Compute(left, right)
	switch {
	case res.TooLarge:
		return dim.Render("too large to diff")
	case len(res.Hunks) == 0:
		return dim.Render(ansi.Truncate(same, width, "…"))
	}
	return strings.Join(miniDiffLines(pal, res, width), "\n")
}
