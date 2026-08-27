package terminal

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

// waitPlainView is waitView over the *stripped* view: reference spans carry
// the underline affordance per rune (#1168), so the raw view never contains
// a path literally.
func waitPlainView(t *testing.T, m *Model, want string) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if strings.Contains(ansi.Strip(m.View()), want) {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("view never showed %q:\n%s", want, ansi.Strip(m.View()))
}

// hintKey builds the key press hint mode reads: a label character.
func hintKey(r rune) tea.KeyPressMsg {
	return tea.KeyPressMsg{Code: r, Text: string(r)}
}

// feedRefs writes n existing files into dir and prints one absolute
// `path:line` reference per line into a pipe model (a pipe session has no
// cwd, so the references are absolute).
func feedRefs(t *testing.T, m *Model, dir string, names ...string) []string {
	t.Helper()
	paths := make([]string, 0, len(names))
	var b strings.Builder
	for _, n := range names {
		p := filepath.Join(dir, n)
		if err := os.WriteFile(p, []byte("x\nx\nx\nx\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		paths = append(paths, p)
		b.WriteString("ref " + p + ":3\n")
	}
	m.FeedText(b.String())
	waitPlainView(t, m, filepath.Join(dir, names[len(names)-1]))
	return paths
}

// TestLinkHintsAssignLabels (#2254): entering hint mode labels every visible
// resolvable reference in reading order, and the labels render over the
// reference's first cell.
func TestLinkHintsAssignLabels(t *testing.T) {
	m := NewPipe("hints-labels", 200, 8, nil)
	t.Cleanup(m.Close)
	dir := t.TempDir()
	paths := feedRefs(t, &m, dir, "a.go", "b.go")

	if !m.StartLinkHints() {
		t.Fatal("hint mode must open over visible references")
	}
	if !m.Hinting() {
		t.Fatal("Hinting must report the open mode")
	}
	if len(m.hints.items) != 2 {
		t.Fatalf("got %d hints, want 2", len(m.hints.items))
	}
	for i, want := range []struct {
		label byte
		path  string
	}{{'a', paths[0]}, {'s', paths[1]}} {
		it := m.hints.items[i]
		if it.label != want.label || it.path != want.path || it.line != 2 {
			t.Errorf("hint %d = %c %q line %d, want %c %q line 2",
				i, it.label, it.path, it.line, want.label, want.path)
		}
		if it.col != 4 { // the "ref " prefix
			t.Errorf("hint %d starts at col %d, want 4", i, it.col)
		}
	}
	// The labels are stamped over the reference's first cell, replacing it.
	view := ansi.Strip(m.View())
	if !strings.Contains(view, "ref a") || !strings.Contains(view, "ref s") {
		t.Fatalf("labels must be stamped on the rows:\n%s", view)
	}
	if !strings.Contains(view, "type a label to open") {
		t.Fatalf("the bottom row must carry the mode prompt:\n%s", view)
	}
}

// TestLinkHintsActivate (#2254): typing a label yields that reference's
// target and closes the mode; esc and unknown keys close without opening.
func TestLinkHintsActivate(t *testing.T) {
	m := NewPipe("hints-activate", 200, 8, nil)
	t.Cleanup(m.Close)
	dir := t.TempDir()
	paths := feedRefs(t, &m, dir, "a.go", "b.go")

	m.StartLinkHints()
	p, line, col, ok := m.LinkHintKey(hintKey('s'))
	if !ok || p != paths[1] || line != 2 || col != 0 {
		t.Fatalf("label s = %q %d %d %v, want %q 2 0 true", p, line, col, ok, paths[1])
	}
	if m.Hinting() {
		t.Fatal("activating a label must close the mode")
	}

	m.StartLinkHints()
	if _, _, _, ok := m.LinkHintKey(tea.KeyPressMsg{Code: tea.KeyEscape}); ok || m.Hinting() {
		t.Fatal("esc must close the mode without opening anything")
	}
	m.StartLinkHints()
	if _, _, _, ok := m.LinkHintKey(hintKey('z')); ok || m.Hinting() {
		t.Fatal("an unassigned key must close the mode without opening anything")
	}
	if v := m.View(); strings.Contains(ansi.Strip(v), "type a label to open") {
		t.Fatal("a closed mode must not keep its prompt")
	}
}

// TestLinkHintsSkipMissingFiles (#2254): the existence gate runs at
// activation, so references to files that do not exist get no label — and a
// screen without any resolvable reference leaves the chord alone.
func TestLinkHintsSkipMissingFiles(t *testing.T) {
	m := NewPipe("hints-missing", 200, 8, nil)
	t.Cleanup(m.Close)
	dir := t.TempDir()

	m.FeedText("ghost " + filepath.Join(dir, "nope.go") + ":1\n")
	waitPlainView(t, &m, "nope.go")
	if m.StartLinkHints() || m.Hinting() {
		t.Fatal("a screen without resolvable references must not open hint mode")
	}
	feedRefs(t, &m, dir, "real.go")
	if !m.StartLinkHints() {
		t.Fatal("a resolvable reference must open hint mode")
	}
	if len(m.hints.items) != 1 {
		t.Fatalf("got %d hints, want only the existing file", len(m.hints.items))
	}
}

// TestLinkHintsOverScrollback (#2254): hint mode operates on the visible
// viewport, so a reference paged into view from history gets a label.
func TestLinkHintsOverScrollback(t *testing.T) {
	m := NewPipe("hints-scroll", 200, 8, nil)
	t.Cleanup(m.Close)
	dir := t.TempDir()
	paths := feedRefs(t, &m, dir, "deep.go")
	for i := 0; i < 30; i++ {
		m.FeedText("filler\n")
	}
	waitPlainView(t, &m, "filler")
	if m.StartLinkHints() {
		t.Fatal("the reference scrolled out of view must not be labelled")
	}
	m.ScrollBy(30) // page it back in
	if !m.StartLinkHints() {
		t.Fatal("a reference windowed into the viewport must be labelled")
	}
	if got := m.hints.items[0].path; got != paths[0] {
		t.Fatalf("hint target = %q, want %q", got, paths[0])
	}
	if !strings.Contains(ansi.Strip(m.View()), "type a label to open") {
		t.Fatal("the scrolled view must carry the mode prompt too")
	}
}

// TestLinkHintsStatOnlyAtActivation guards the #1168 performance posture the
// keyboard route inherits: rendering never stats, activation stats once per
// visible reference.
func TestLinkHintsStatOnlyAtActivation(t *testing.T) {
	var stats atomic.Int64
	orig := linkStat
	linkStat = func(p string) (fs.FileInfo, error) {
		stats.Add(1)
		return orig(p)
	}
	t.Cleanup(func() { linkStat = orig })

	m := NewPipe("hints-stat", 200, 8, nil)
	t.Cleanup(m.Close)
	dir := t.TempDir()
	feedRefs(t, &m, dir, "a.go", "b.go")

	stats.Store(0)
	for i := 0; i < 5; i++ {
		_ = m.View()
	}
	if n := stats.Load(); n != 0 {
		t.Fatalf("rendering performed %d stat calls, want 0", n)
	}
	m.StartLinkHints()
	if n := stats.Load(); n != 2 {
		t.Fatalf("activation performed %d stat calls, want one per reference (2)", n)
	}
	stats.Store(0)
	for i := 0; i < 5; i++ {
		_ = m.View()
	}
	if n := stats.Load(); n != 0 {
		t.Fatalf("rendering the hint overlay performed %d stat calls, want 0", n)
	}
}

// TestLinkHintsDropOnResizeAndBlur (#2254): the labels describe a captured
// viewport, so a reflow or a focus change closes the mode.
func TestLinkHintsDropOnResizeAndBlur(t *testing.T) {
	m := NewPipe("hints-drop", 200, 8, nil)
	t.Cleanup(m.Close)
	dir := t.TempDir()
	feedRefs(t, &m, dir, "a.go")

	m.StartLinkHints()
	m.SetSize(60, 8)
	if m.Hinting() {
		t.Fatal("a resize must close hint mode")
	}
	m.StartLinkHints()
	m.SetFocused(false)
	if m.Hinting() {
		t.Fatal("losing the focus must close hint mode")
	}
}

// TestLinkHintsKeyNeverReachesShell (#2254): a key that reaches Model.Update
// while the mode is open closes it instead of typing into the child.
func TestLinkHintsKeyNeverReachesShell(t *testing.T) {
	m := NewPipe("hints-update", 200, 8, nil)
	t.Cleanup(m.Close)
	dir := t.TempDir()
	feedRefs(t, &m, dir, "a.go")

	m.StartLinkHints()
	m.Update(hintKey('a'))
	if m.Hinting() {
		t.Fatal("a key routed through Update must close the mode")
	}
}
