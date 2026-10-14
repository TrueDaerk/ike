package app

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"ike/internal/complexity"
	"ike/internal/complexitypanel"
	"ike/internal/diff"
	"ike/internal/pane"
	"ike/internal/vcs"
)

// complexity_funcdiff_test.go covers the Changes tab's function diff
// (#2956) against a real repository: the cut excerpts with their line
// offsets, the added / removed one-sided views, the per-function viewer
// that refocuses in place, the jump into the head function and the focus
// hand-back on close.

// funcDiffApp is a Changes tab over gitRepo's working tree with the base
// resolved. The diffs of the files the tests use are measured here
// directly — a git read failing under a loaded test run must fail loudly
// rather than reclassify a changed unit as added.
func funcDiffApp(t *testing.T) (Model, string, complexity.Diff) {
	t.Helper()
	dir := gitRepo(t)
	root, err := vcs.DetectRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	snap, err := vcs.Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	sha, err := vcs.ResolveRev(root, "HEAD")
	if err != nil {
		t.Fatal(err)
	}
	d := complexity.Diff{Files: map[string]complexity.FileDiff{}}
	measure := func(rel string, atBase bool) []complexity.Function {
		t.Helper()
		var text string
		if atBase {
			if text, err = vcs.RevContent(root, sha, rel); err != nil {
				t.Fatalf("git show %s: %v", rel, err)
			}
		} else {
			data, err := os.ReadFile(filepath.Join(root, rel))
			if err != nil {
				t.Fatal(err)
			}
			text = string(data)
		}
		return cxMeasureText(rel, text)
	}
	d.Files[filepath.Join(root, "mod.go")] = complexity.DiffFunctions(measure("mod.go", true), measure("mod.go", false))
	d.Files[filepath.Join(root, "gone.go")] = complexity.DiffFunctions(measure("gone.go", true), nil)
	d.Files[filepath.Join(root, "fresh.go")] = complexity.DiffFunctions(nil, measure("fresh.go", false))
	for p, fd := range d.Files {
		if fd.Empty() {
			t.Fatalf("%s measured no change", p)
		}
	}
	changes := complexitypanel.Changes{Sha: sha, Diff: d, Ready: true}
	m := changesApp(t)
	m.vcs.snap = snap
	m.cxDiff.result = changes
	m.complexityPanel().SetChanges(changes)
	return m, root, d
}

// runFuncDiff opens req the way Update's FunctionDiffMsg case does, runs
// the fetch and feeds the result back through Update.
func runFuncDiff(t *testing.T, m Model, req complexitypanel.FunctionDiffMsg) (Model, cxFuncDiffMsg) {
	t.Helper()
	cmd := m.openComplexityFunctionDiff(req)
	if cmd == nil {
		t.Fatal("d must launch the fetch")
	}
	msg, ok := cmd().(cxFuncDiffMsg)
	if !ok {
		t.Fatal("the fetch must report a cxFuncDiffMsg")
	}
	out, _ := m.Update(msg)
	return out.(Model), msg
}

// funcDiffViewer finds the viewer of key.
func funcDiffViewer(m Model, key string) (*pane.Instance, bool) {
	_, _, inst, ok := m.findContent(func(c *pane.Instance) bool {
		return c.Kind() == pane.KindDiff && c.Diff().Key() == key
	})
	return inst, ok
}

func TestComplexityFunctionDiffChangedRow(t *testing.T) {
	m, root, d := funcDiffApp(t)
	mod := filepath.Join(root, "mod.go")
	c := d.Files[mod].Changed[0]
	req := complexitypanel.FunctionDiffMsg{Path: mod, Name: "f", Ordinal: 0, Change: c}
	m, msg := runFuncDiff(t, m, req)
	if msg.err != "" {
		t.Fatal(msg.err)
	}
	// f spans lines 2..7 at HEAD and 2..10 now (0-based); three lines of
	// context reach the file start above and "func same() {}" below.
	if msg.offL != 0 || msg.offR != 0 {
		t.Fatalf("offsets = %d/%d", msg.offL, msg.offR)
	}
	if !strings.Contains(msg.left, "return 0") || strings.Contains(msg.left, "a < 0") {
		t.Fatalf("left = %q", msg.left)
	}
	if !strings.Contains(msg.right, "if a < 0") || !strings.Contains(msg.right, "func same") {
		t.Fatalf("right = %q", msg.right)
	}
	key := cxFuncDiffKey(mod, "f", 0)
	inst, ok := funcDiffViewer(m, key)
	if !ok {
		t.Fatal("no function diff viewer opened")
	}
	if m.focusedContent() != inst {
		t.Fatal("the new viewer must hold the focus")
	}
	l, r := inst.Diff().Titles()
	if l != "f @ HEAD" || r != "f @ HEAD → working tree" || inst.Diff().RightPath() != mod {
		t.Fatalf("titles = %q / %q, right path %q", l, r, inst.Diff().RightPath())
	}
	if inst.Diff().Editable() {
		t.Fatal("the function diff must be read-only")
	}
	if _, ok := contentIdentity(inst); ok {
		t.Fatal("a function diff must not persist")
	}
	// The cache holds the base blob: a second open runs no git.
	if len(m.cxDiff.funcText) != 1 {
		t.Fatalf("cache = %d", len(m.cxDiff.funcText))
	}
	// Enter on the hunk jumps to the head function's line.
	jump := inst.Diff().Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if jump == nil {
		t.Fatal("enter must jump")
	}
	if j, ok := jump().(diff.JumpMsg); !ok || j.Path != mod || j.Line != 7 {
		t.Fatalf("jump = %+v", jump())
	}

	// Reopening the same function refocuses the viewer in place.
	m.setFocus(m.activeWS().Panes.Keys()[0])
	diffs := func(m Model) int {
		n := 0
		m.contentInstances(func(_ string, _ int, c *pane.Instance) bool {
			if isCxFuncDiff(c) {
				n++
			}
			return true
		})
		return n
	}
	m, _ = runFuncDiff(t, m, req)
	if diffs(m) != 1 || m.focusedContent().Kind() != pane.KindDiff || m.focusedContent().Diff().Key() != key {
		t.Fatalf("reopen: %d viewers, focus %v", diffs(m), m.focusedContent().Kind())
	}

	// Another function opens another viewer: the removed g of gone.go has
	// no head side and binds no file.
	gone := filepath.Join(root, "gone.go")
	g := d.Files[gone].Removed[0]
	m, msg = runFuncDiff(t, m, complexitypanel.FunctionDiffMsg{Path: gone, Name: "g", Change: complexity.Change{Before: g}})
	if msg.err != "" || msg.right != "" || !strings.Contains(msg.left, "func g()") {
		t.Fatalf("removed: %+v", msg)
	}
	if diffs(m) != 2 {
		t.Fatalf("a second function must open a second viewer (%d)", diffs(m))
	}
	if inst, ok := funcDiffViewer(m, cxFuncDiffKey(gone, "g", 0)); !ok || inst.Diff().RightPath() != "" {
		t.Fatal("the removed function's viewer binds no file")
	}

	// Closing a function diff returns focus to the Complexity window.
	m.closeFocused()
	if f := m.focusedContent(); f == nil || f.Kind() != pane.KindComplexity {
		t.Fatalf("after close focus is on %v", f)
	}
}

func TestComplexityFunctionDiffAddedRowAndOffsets(t *testing.T) {
	m, root, d := funcDiffApp(t)
	fresh := filepath.Join(root, "fresh.go")
	f := d.Files[fresh].Added[0]
	m, msg := runFuncDiff(t, m, complexitypanel.FunctionDiffMsg{Path: fresh, Name: "fresh", Change: complexity.Change{After: f}})
	if msg.err != "" || msg.left != "" || !strings.Contains(msg.right, "func fresh()") {
		t.Fatalf("added: %+v", msg)
	}
	if _, ok := funcDiffViewer(m, cxFuncDiffKey(fresh, "fresh", 0)); !ok {
		t.Fatal("no viewer for the added function")
	}
	// The cut keeps three lines of context and reports the lines before it.
	text := "l0\nl1\nl2\nl3\nl4\nl5\nl6\nl7\nl8\nl9\nl10"
	got, off := cxExcerpt(text, complexity.Function{Line: 5, EndLine: 6})
	if off != 2 || got != "l2\nl3\nl4\nl5\nl6\nl7\nl8\nl9" {
		t.Fatalf("excerpt = %q @ %d", got, off)
	}
}

func TestComplexityFunctionDiffRanged(t *testing.T) {
	dir := gitRepo(t)
	root, err := vcs.DetectRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	// Commit the working tree's f edit, then edit f again on disk: the
	// ranged diff's right side is the commit, not the disk (#2961).
	for _, args := range [][]string{{"add", "-A"}, {"commit", "-qm", "work"}} {
		cmd := exec.Command("git", append([]string{"-C", root, "-c", "user.email=t@t", "-c", "user.name=t"}, args...)...)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	mod := filepath.Join(root, "mod.go")
	if err := os.WriteFile(mod, []byte("package p\n\nfunc f(a int) int { return a }\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	snap, err := vcs.Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	m := changesApp(t)
	m.vcs.snap = snap
	msg := runComplexityDiff(cxDiffJob{key: cxDiffKey{rev: "main..HEAD", rangeGen: 1}, repoRoot: root, projRoot: root, res: complexity.Uniform(complexity.Thresholds{CCN: 15, Cog: 15})}).(complexityDiffMsg)
	if msg.err != "" {
		t.Fatal(msg.err)
	}
	m.cxDiff.result = msg.changes
	m.complexityPanel().SetChanges(msg.changes)
	fd := msg.changes.Diff.Files[mod]
	if len(fd.Changed) != 1 {
		t.Fatalf("mod.go = %+v", fd)
	}
	req := complexitypanel.FunctionDiffMsg{Path: mod, Name: "f", Change: fd.Changed[0]}
	m, fmsg := runFuncDiff(t, m, req)
	if fmsg.err != "" || !strings.Contains(fmsg.right, "if a < 0") || strings.Contains(fmsg.right, "return a }") {
		t.Fatalf("ranged right side must be the head commit: %+v", fmsg)
	}
	inst, ok := funcDiffViewer(m, cxFuncDiffKey(mod, "f", 0))
	if !ok {
		t.Fatal("no viewer")
	}
	l, r := inst.Diff().Titles()
	if l != "f @ main" || r != "f @ main → HEAD" || inst.Diff().RightPath() != "" {
		t.Fatalf("titles = %q / %q, right path %q", l, r, inst.Diff().RightPath())
	}
	// Both blobs are cached: base and head.
	if len(m.cxDiff.funcText) != 2 {
		t.Fatalf("cache = %d", len(m.cxDiff.funcText))
	}
}

func TestComplexityFunctionDiffNeedsABase(t *testing.T) {
	m := changesApp(t)
	if cmd := m.openComplexityFunctionDiff(complexitypanel.FunctionDiffMsg{Path: "/r/a.go", Name: "f"}); cmd != nil {
		t.Fatal("without a computed base nothing must run")
	}
	// The palette command with no Changes row selected only says so.
	if cmd := m.complexityFunctionDiff(); cmd != nil {
		t.Fatal("palette command without a row must not run")
	}
}
