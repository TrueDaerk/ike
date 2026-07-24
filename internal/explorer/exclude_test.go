package explorer

// exclude_test.go covers the explorer.exclude setting (#1139): base-name glob
// patterns hidden from the tree at every depth, regardless of the show-hidden
// toggle. The filter lives in the single visibility gate (childVisible, used
// by appendVisible and hasVisibleChildren), so rows, markers, search and
// multi-select all see the same filtered tree.

import (
	"os"
	"path/filepath"
	"testing"

	"ike/internal/host"
)

// excludeTree builds root/{.git/config, .idea/w.xml, .DS_Store, .env,
// keep.txt, sub/{.git/x, gen.pyc, ok.txt}}.
func excludeTree(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	for _, d := range []string{".git", ".idea", "sub", filepath.Join("sub", ".git")} {
		if err := os.MkdirAll(filepath.Join(root, d), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	mustWrite(t, filepath.Join(root, ".git", "config"), "x")
	mustWrite(t, filepath.Join(root, ".idea", "w.xml"), "x")
	mustWrite(t, filepath.Join(root, ".DS_Store"), "x")
	mustWrite(t, filepath.Join(root, ".env"), "x")
	mustWrite(t, filepath.Join(root, "keep.txt"), "x")
	mustWrite(t, filepath.Join(root, "sub", ".git", "x"), "x")
	mustWrite(t, filepath.Join(root, "sub", "gen.pyc"), "x")
	mustWrite(t, filepath.Join(root, "sub", "ok.txt"), "x")
	return root
}

// rowNames returns the base names of the current visible rows (root included).
func rowNames(m Model) []string {
	out := make([]string, len(m.rows))
	for i, n := range m.rows {
		out[i] = n.name
	}
	return out
}

func hasRowNamed(m Model, name string) bool {
	for _, n := range m.rows {
		if n.name == name {
			return true
		}
	}
	return false
}

// TestDefaultExcludeHidesNoiseEvenWithHiddenShown: the built-in default list
// (.git, .idea, .DS_Store) filters entries even when the show-hidden toggle
// is on, while other dot-entries (.env) appear normally.
func TestDefaultExcludeHidesNoiseEvenWithHiddenShown(t *testing.T) {
	m := mounted(t, excludeTree(t), 60, 40)
	m, _ = m.Update(ToggleHiddenMsg{}) // show hidden ON
	for _, name := range []string{".git", ".idea", ".DS_Store"} {
		if hasRowNamed(m, name) {
			t.Errorf("default-excluded %q visible with show_hidden on; rows=%v", name, rowNames(m))
		}
	}
	if !hasRowNamed(m, ".env") {
		t.Errorf(".env should be visible with show_hidden on; rows=%v", rowNames(m))
	}
	if !hasRowNamed(m, "keep.txt") {
		t.Errorf("keep.txt missing; rows=%v", rowNames(m))
	}
}

// TestExcludeAppliesAtEveryDepth: an excluded name is filtered inside
// subdirectories too, not only at the root level.
func TestExcludeAppliesAtEveryDepth(t *testing.T) {
	m := mounted(t, excludeTree(t), 60, 40)
	m, _ = m.Update(ToggleHiddenMsg{})
	// Expand sub.
	for i, n := range m.rows {
		if n.name == "sub" {
			m.cursor = i
			break
		}
	}
	m, _ = pumpScans(m.Update(ActivateMsg{}))
	if hasRowNamed(m, ".git") {
		t.Errorf("sub/.git visible despite exclude; rows=%v", rowNames(m))
	}
	if !hasRowNamed(m, "ok.txt") {
		t.Errorf("sub/ok.txt missing; rows=%v", rowNames(m))
	}
}

// TestExcludeGlobPatternsAndLiveChange: Configure applies a changed
// explorer.exclude (comma-joined globs) live — the tree rebuilds without a
// restart, glob patterns (*.pyc) match, and entries excluded only by the
// previous list reappear.
func TestExcludeGlobPatternsAndLiveChange(t *testing.T) {
	m := mounted(t, excludeTree(t), 60, 40)
	// Expand sub while the default list is active.
	for i, n := range m.rows {
		if n.name == "sub" {
			m.cursor = i
			break
		}
	}
	m, _ = pumpScans(m.Update(ActivateMsg{}))
	if !hasRowNamed(m, "gen.pyc") {
		t.Fatalf("gen.pyc should be visible before the config change; rows=%v", rowNames(m))
	}

	m.Configure(host.MapConfig{"explorer.exclude": "*.pyc, .git"})
	if hasRowNamed(m, "gen.pyc") {
		t.Errorf("*.pyc pattern not applied live; rows=%v", rowNames(m))
	}
	if !hasRowNamed(m, "ok.txt") {
		t.Errorf("ok.txt vanished; rows=%v", rowNames(m))
	}

	// Emptying the list un-hides everything the exclude filtered; hidden
	// filtering still applies independently (.git stays dot-hidden until the
	// toggle).
	m.Configure(host.MapConfig{"explorer.exclude": ""})
	if !hasRowNamed(m, "gen.pyc") {
		t.Errorf("empty exclude list should restore gen.pyc; rows=%v", rowNames(m))
	}
	m, _ = m.Update(ToggleHiddenMsg{})
	if !hasRowNamed(m, ".git") {
		t.Errorf("with an empty exclude list and hidden shown, .git should appear; rows=%v", rowNames(m))
	}
}

// TestReconfigureUnchangedExcludeSkipsRebuild: an unrelated live reload with
// the same exclude value must not churn (guarded by excludeCfg, mirroring
// hiddenCfg #629). Observable via the width cache: an unchanged value leaves
// pendingSel/cursor alone and performs no extra rebuild-driven cursor snap.
func TestReconfigureUnchangedExcludeSkipsRebuild(t *testing.T) {
	m := mounted(t, excludeTree(t), 60, 40)
	before := m.excludeCfg
	m.Configure(host.MapConfig{"explorer.exclude": before})
	if m.excludeCfg != before {
		t.Fatalf("excludeCfg changed on an identical value: %q -> %q", before, m.excludeCfg)
	}
}

// TestExcludedOnlyDirShowsNoExpander: a loaded directory whose children are
// all excluded reads as empty — no expand caret (#1039 semantics extend to
// the exclude filter).
func TestExcludedOnlyDirShowsNoExpander(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "d", ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	m := mounted(t, root, 60, 40)
	m, _ = m.Update(ToggleHiddenMsg{}) // show hidden on: only the exclude filter is left
	var d *node
	for _, n := range m.rows {
		if n.name == "d" {
			d = n
		}
	}
	if d == nil {
		t.Fatalf("d missing; rows=%v", rowNames(m))
	}
	// Load d's children.
	for i, n := range m.rows {
		if n == d {
			m.cursor = i
		}
	}
	m, _ = pumpScans(m.Update(ActivateMsg{}))
	if got := m.marker(d); got != "  " {
		t.Errorf("marker = %q, want blank for a dir with only excluded children", got)
	}
	if m.hasVisibleChildren(d) {
		t.Error("hasVisibleChildren must be false when every child is excluded")
	}
}

// TestInvalidPatternNeverMatches: a malformed glob (config validation drops
// these, but belt-and-braces) matches nothing instead of erroring.
func TestInvalidPatternNeverMatches(t *testing.T) {
	m := New(t.TempDir())
	m.exclude = []string{"[", "*.log"}
	if m.isExcluded("x") {
		t.Error("malformed pattern must not match")
	}
	if !m.isExcluded("a.log") {
		t.Error("valid pattern alongside a malformed one must still match")
	}
}

// TestParseExclude covers the comma-split parsing rules.
func TestParseExclude(t *testing.T) {
	if got := parseExclude(""); got != nil {
		t.Errorf("empty value = %v, want nil", got)
	}
	got := parseExclude(" .git , *.pyc ,, node_modules ")
	want := []string{".git", "*.pyc", "node_modules"}
	if len(got) != len(want) {
		t.Fatalf("parseExclude = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("parseExclude = %v, want %v", got, want)
		}
	}
}
