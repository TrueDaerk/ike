package app

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"

	tea "charm.land/bubbletea/v2"

	"ike/internal/complexity"
	"ike/internal/complexitypanel"
	"ike/internal/host"
	"ike/internal/pane"
	"ike/internal/vcs"
)

// complexity_funcdiff.go is the Changes tab's function diff (#2956): 'd' on
// a row opens the function's two versions — the base revision's blob
// against the working tree — cut to the function ranges plus a few lines of
// context, in the reusable diff pane. The base blob comes from git inside a
// tea.Cmd (cached per (sha, path) for the session beside the measurement
// cache), the head side from the dirty buffer or the disk; the head is
// re-measured on the way so an edit since the last diff run still cuts the
// right range. One read-only diff viewer per function, keyed
// "cxdiff:<path>:<name>#<ordinal>"; reopening it refocuses (and refreshes)
// the viewer in place, and closing it returns focus to the Complexity
// window.

// ComplexityFunctionDiffMsg runs complexity.functionDiff (#2956), d in the
// pane: diff the selected Changes row's function, base vs working tree.
type ComplexityFunctionDiffMsg struct{}

// cxFuncDiffContext is the context kept around each side's function range.
const cxFuncDiffContext = 3

// cxFuncDiffTextMax bounds the base-text cache before it is dropped whole.
const cxFuncDiffTextMax = 64

// cxFuncDiffPrefix starts every function diff's model key.
const cxFuncDiffPrefix = "cxdiff:"

// cxFuncDiffKey is the model key of one function's diff viewer.
func cxFuncDiffKey(path, name string, ordinal int) string {
	return cxFuncDiffPrefix + path + ":" + name + "#" + strconv.Itoa(ordinal)
}

// isCxFuncDiff reports a function diff viewer: it is one per function, so
// the single-diff-window slot (#513) never retargets it, and it does not
// persist in the layout.
func isCxFuncDiff(inst *pane.Instance) bool {
	return inst != nil && inst.Kind() == pane.KindDiff && strings.HasPrefix(inst.Diff().Key(), cxFuncDiffPrefix)
}

// cxFuncDiffJob is everything the off-loop half reads.
type cxFuncDiffJob struct {
	req      complexitypanel.FunctionDiffMsg
	key      string
	rev      string // the base as the title names it
	repoRoot string
	sha      string
	rel      string // repo-relative slash path of the base blob
	base     string // the cached base text, when cached
	cached   bool
	head     string // the working-tree text ("" when the unit is removed)
	// headRev / headSha / headRel name the head blob of a ranged diff
	// (#2961), read off-loop in place of the working tree; "" otherwise.
	headRev, headSha, headRel string
}

// cxFuncDiffMsg carries the cut texts back to Update.
type cxFuncDiffMsg struct {
	job         cxFuncDiffJob
	left, right string
	offL, offR  int
	// base is the fetched base text for the cache ("" when it was cached
	// or not needed); err a git failure reading it.
	base string
	// head is the fetched head blob of a ranged diff (#2961) for the same
	// cache, "" when it was cached or the head is the working tree.
	head string
	err  string
}

// complexityFunctionDiff is complexity.functionDiff from the palette: the
// pane's 'd' for the selected row.
func (m *Model) complexityFunctionDiff() tea.Cmd {
	if p := m.complexityPanel(); p != nil {
		if req, ok := p.FunctionDiff(); ok {
			return m.openComplexityFunctionDiff(req)
		}
	}
	m.host.Notify(host.Info, "complexity: select a function row of the Changes tab to diff it")
	return nil
}

// openComplexityFunctionDiff gathers the inputs — the base the Changes tab
// shows, the head text — and launches the off-loop fetch and cut.
func (m *Model) openComplexityFunctionDiff(req complexitypanel.FunctionDiffMsg) tea.Cmd {
	st := m.cxDiff
	if st == nil || m.vcs.snap == nil || st.result.Sha == "" {
		m.host.Notify(host.Info, "complexity: no base revision to diff against yet")
		return nil
	}
	job := cxFuncDiffJob{
		req:      req,
		key:      cxFuncDiffKey(req.Path, req.Name, req.Ordinal),
		rev:      st.result.BaseRev(),
		repoRoot: m.vcs.snap.Root,
		sha:      st.result.Sha,
		headRev:  st.result.HeadRev,
		headSha:  st.result.HeadSha,
	}
	rel, err := filepath.Rel(job.repoRoot, req.Path)
	inRepo := err == nil && !strings.HasPrefix(rel, "..")
	if !inRepo && (req.Change.Before.Name != "" || job.headSha != "") {
		m.host.Notify(host.Info, "complexity: "+displayPath(req.Path)+" is outside the repository")
		return nil
	}
	if req.Change.Before.Name != "" {
		job.rel = filepath.ToSlash(rel)
		job.base, job.cached = st.funcText[job.sha+"\x00"+job.rel]
	}
	if req.Change.After.Name != "" && job.headSha != "" {
		// A ranged diff (#2961): the head side is the head commit's blob.
		job.headRel = filepath.ToSlash(rel)
		job.head = st.funcText[job.headSha+"\x00"+job.headRel]
	} else if req.Change.After.Name != "" {
		if ed := m.editorForPath(canonicalPath(req.Path)); ed != nil {
			job.head = ed.Text()
		} else if data, err := os.ReadFile(req.Path); err == nil {
			job.head, _ = normalizeBufferText(data)
		}
	}
	return func() tea.Msg { return runCxFuncDiff(job) }
}

// runCxFuncDiff is the off-loop half: read the base blob unless cached,
// re-locate the head unit in the current text, cut both sides.
func runCxFuncDiff(job cxFuncDiffJob) tea.Msg {
	out := cxFuncDiffMsg{job: job}
	req := job.req
	if req.Change.Before.Name != "" {
		base := job.base
		if !job.cached {
			content, err := vcs.RevContent(job.repoRoot, job.sha, job.rel)
			if err != nil {
				out.err = err.Error()
				return out
			}
			base, _ = normalizeBufferText([]byte(content))
			out.base = base
		}
		out.left, out.offL = cxExcerpt(base, req.Change.Before)
	}
	if req.Change.After.Name != "" && job.headSha != "" && job.head == "" {
		content, err := vcs.RevContent(job.repoRoot, job.headSha, job.headRel)
		if err != nil {
			out.err = err.Error()
			return out
		}
		job.head, _ = normalizeBufferText([]byte(content))
		out.head = job.head
	}
	if req.Change.After.Name != "" {
		after := req.Change.After
		if fn, ok := cxLocate(req.Path, job.head, req.Name, req.Ordinal); ok {
			after = fn
		}
		out.right, out.offR = cxExcerpt(job.head, after)
	}
	return out
}

// cxLocate finds the unit name#ordinal in text, measured as path's
// language: the head side may have moved since the diff run measured it.
func cxLocate(path, text, name string, ordinal int) (complexity.Function, bool) {
	fns := cxMeasureText(path, text)
	ords := complexity.Ordinals(fns)
	for i, f := range fns {
		if f.Name == name && ords[i] == ordinal {
			return f, true
		}
	}
	return complexity.Function{}, false
}

// cxExcerpt cuts text to fn's 0-based line range plus the context, and
// reports how many lines precede the cut — the diff's line offset.
func cxExcerpt(text string, fn complexity.Function) (string, int) {
	lines := strings.Split(text, "\n")
	start := max(0, fn.Line-cxFuncDiffContext)
	end := min(len(lines)-1, max(fn.Line, fn.EndLine)+cxFuncDiffContext)
	if start > end {
		return "", 0
	}
	return strings.Join(lines[start:end+1], "\n"), start
}

// showComplexityFunctionDiff opens (or refreshes in place) the function's
// diff viewer. The right side is bound to the file while the unit exists
// there, so enter on a hunk lands in the editor at the head function; a
// removed unit binds no file.
func (m *Model) showComplexityFunctionDiff(msg cxFuncDiffMsg) {
	job := msg.job
	if msg.base != "" && m.cxDiff != nil {
		if m.cxDiff.funcText == nil || len(m.cxDiff.funcText) >= cxFuncDiffTextMax {
			m.cxDiff.funcText = map[string]string{}
		}
		m.cxDiff.funcText[job.sha+"\x00"+job.rel] = msg.base
	}
	if msg.head != "" && m.cxDiff != nil {
		if m.cxDiff.funcText == nil || len(m.cxDiff.funcText) >= cxFuncDiffTextMax {
			m.cxDiff.funcText = map[string]string{}
		}
		m.cxDiff.funcText[job.headSha+"\x00"+job.headRel] = msg.head
	}
	if msg.err != "" {
		m.host.Notify(host.Warn, "complexity: "+msg.err)
		return
	}
	leftLabel := job.req.Name + " @ " + job.rev
	rightLabel := "working tree"
	rightPath := ""
	if job.headRev != "" {
		// A ranged diff (#2961): the right side is a blob too, bound to no
		// file — the working tree may hold another version.
		rightLabel = job.headRev
	} else if job.req.Change.After.Name != "" {
		rightPath = job.req.Path
	}
	title := leftLabel + " → " + rightLabel
	fill := func(inst *pane.Instance) {
		d := inst.Diff()
		d.SetSideLabels(leftLabel, rightLabel)
		d.SetEditable(false) // the left side is a blob
		d.SetLineOffsets(msg.offL, msg.offR)
		d.SetContents(msg.left, msg.right)
	}
	if hostKey, tabIdx, inst, ok := m.findContent(func(c *pane.Instance) bool {
		return c.Kind() == pane.KindDiff && c.Diff().Key() == job.key
	}); ok {
		inst.StopDiffEdit()
		inst.Diff().Retarget(leftLabel, title, "", rightPath, "", "", false)
		fill(inst)
		m.focusContentAt(hostKey, tabIdx)
		return
	}
	inst, ok := m.openDiffLeaf(func() string {
		return m.activeWS().Panes.AddDiffModelKey(job.key, leftLabel, title, rightPath)
	})
	if !ok {
		return
	}
	fill(inst)
	saveLayout(m.activeWS().Tree, m.activeWS().Panes)
}

// refocusComplexityAfterDiff returns focus to the Complexity window after
// a function diff closed; with the window closed the focus stays where the
// close put it.
func (m *Model) refocusComplexityAfterDiff() {
	m.focusToolWindow(pane.KindComplexity)
}
