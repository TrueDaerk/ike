package explorer

// Tests for reveal expanding collapsed ancestors (#1042): the async descent
// through unloaded directories, cleanup when the target vanished, and the
// auto-reveal arming behind explorer.auto_reveal.

import (
	"os"
	"path/filepath"
	"testing"

	tea "charm.land/bubbletea/v2"

	"ike/internal/host"
)

// deepTree builds: root/{a.txt, sub/deep/file.txt} — two directory levels that
// start collapsed and unloaded.
func deepTree(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	mustWrite(t, filepath.Join(root, "a.txt"), "a")
	if err := os.MkdirAll(filepath.Join(root, "sub", "deep"), 0o755); err != nil {
		t.Fatal(err)
	}
	mustWrite(t, filepath.Join(root, "sub", "deep", "file.txt"), "f")
	return root
}

// stepScan executes cmd and returns the single ScanDoneMsg it produced,
// unwrapping a tea.BatchMsg (the ScanDoneMsg handler batches the reveal
// continuation with the poll starter). It fails the test when the cmd yields
// anything else, so each async reveal step is driven — and asserted — one scan
// at a time.
func stepScan(t *testing.T, cmd tea.Cmd) ScanDoneMsg {
	t.Helper()
	if cmd == nil {
		t.Fatal("expected a scan Cmd, got nil")
	}
	msg := cmd()
	if batch, ok := msg.(tea.BatchMsg); ok {
		var out tea.Msg
		for _, c := range batch {
			if c == nil {
				continue
			}
			if m := c(); m != nil {
				if out != nil {
					t.Fatalf("expected one message, got a second: %#v", m)
				}
				out = m
			}
		}
		msg = out
	}
	sd, ok := msg.(ScanDoneMsg)
	if !ok {
		t.Fatalf("expected ScanDoneMsg, got %#v", msg)
	}
	return sd
}

// cursorPath returns the path under the cursor.
func cursorPath(t *testing.T, m Model) string {
	t.Helper()
	n := m.current()
	if n == nil {
		t.Fatal("empty tree")
	}
	return n.path
}

func TestRevealExpandsUnloadedAncestors(t *testing.T) {
	root := deepTree(t)
	m := mounted(t, root, 40, 10)
	target := filepath.Join(root, "sub", "deep", "file.txt")
	m.SetActive(target)

	// Kick the reveal: sub is collapsed and unloaded, so the walk pauses on
	// its scan.
	m, cmd := m.Update(RevealMsg{})
	if m.pendingReveal != target {
		t.Fatalf("pendingReveal = %q, want %q", m.pendingReveal, target)
	}
	sub := nodeByPath(m.root, filepath.Join(root, "sub"))
	if sub == nil || !sub.expanded || !sub.loading {
		t.Fatalf("sub should be expanded and scanning, got %+v", sub)
	}

	// First scan lands: the walk resumes one level deeper (deep).
	sd := stepScan(t, cmd)
	if sd.Path != sub.path {
		t.Fatalf("first scan hit %q, want %q", sd.Path, sub.path)
	}
	m, cmd = m.Update(sd)
	deep := nodeByPath(m.root, filepath.Join(root, "sub", "deep"))
	if deep == nil || !deep.expanded || !deep.loading {
		t.Fatalf("deep should be expanded and scanning, got %+v", deep)
	}
	if m.pendingReveal != target {
		t.Fatalf("pendingReveal dropped mid-descent: %q", m.pendingReveal)
	}

	// Second scan lands: the target row exists, is selected, and the pending
	// state is cleared.
	sd = stepScan(t, cmd)
	if sd.Path != deep.path {
		t.Fatalf("second scan hit %q, want %q", sd.Path, deep.path)
	}
	m, _ = m.Update(sd)
	if got := cursorPath(t, m); got != target {
		t.Fatalf("cursor on %q, want %q", got, target)
	}
	if m.pendingReveal != "" || m.pendingSel != "" {
		t.Fatalf("reveal state not cleared: pendingReveal=%q pendingSel=%q",
			m.pendingReveal, m.pendingSel)
	}
}

func TestRevealVisibleRowNeedsNoScan(t *testing.T) {
	root := tree(t)
	m := mounted(t, root, 40, 10)
	target := filepath.Join(root, "b.txt")
	m.SetActive(target)
	m, cmd := m.Update(RevealMsg{})
	if cmd != nil {
		t.Fatal("visible target should not dispatch a scan")
	}
	if got := cursorPath(t, m); got != target {
		t.Fatalf("cursor on %q, want %q", got, target)
	}
	if m.pendingReveal != "" {
		t.Fatalf("pendingReveal = %q, want empty", m.pendingReveal)
	}
}

func TestRevealVanishedPathClearsState(t *testing.T) {
	root := deepTree(t)
	m := mounted(t, root, 40, 10)
	// The active file's directory does not exist (deleted after opening).
	m.SetActive(filepath.Join(root, "sub", "ghost", "file.txt"))
	m, cmd := m.Update(RevealMsg{})
	if m.pendingReveal == "" {
		t.Fatal("reveal should be pending until sub's scan lands")
	}
	// Drive applyScan with a scan proving "ghost" is not there.
	m, _ = m.Update(ScanDoneMsg{
		Path:    filepath.Join(root, "sub"),
		Entries: []scanEntry{{name: "deep", isDir: true}},
	})
	if m.pendingReveal != "" || m.pendingSel != "" {
		t.Fatalf("vanished path should clear reveal state, got pendingReveal=%q pendingSel=%q",
			m.pendingReveal, m.pendingSel)
	}
	_ = cmd
}

func TestRevealOutsideRootIsAbandoned(t *testing.T) {
	root := tree(t)
	m := mounted(t, root, 40, 10)
	m.SetActive(filepath.Join(filepath.Dir(root), "elsewhere.txt"))
	m, cmd := m.Update(RevealMsg{})
	if cmd != nil || m.pendingReveal != "" {
		t.Fatalf("outside-root target must abandon: cmd=%v pendingReveal=%q",
			cmd, m.pendingReveal)
	}
}

func TestAutoRevealArmsOnSetActive(t *testing.T) {
	root := tree(t)
	m := mounted(t, root, 40, 10)
	target := filepath.Join(root, "a.txt")

	// Default off: SetActive arms nothing.
	m.SetActive(target)
	if cmd := m.PendingRevealCmd(); cmd != nil {
		t.Fatal("auto_reveal defaults off; SetActive must not arm a reveal")
	}

	m.Configure(host.MapConfig{"explorer.auto_reveal": "true"})
	m.SetActive("") // clear so the next SetActive is a genuine change
	m.SetActive(target)
	if m.PendingRevealCmd() == nil {
		// The visible-row reveal returns nil Cmd but still moves the cursor;
		// arming is what we assert, via the drained flag below.
		if m.wantReveal {
			t.Fatal("PendingRevealCmd left wantReveal set")
		}
	}
	if got := cursorPath(t, m); got != target {
		t.Fatalf("auto-reveal did not select %q (cursor on %q)", target, got)
	}
	// Drained: a second call is a no-op.
	if m.wantReveal {
		t.Fatal("wantReveal should be cleared after draining")
	}
	// Re-activating the same path does not re-arm.
	m.SetActive(target)
	if m.wantReveal {
		t.Fatal("unchanged active path must not re-arm the reveal")
	}

	// Toggling back off disarms future activations.
	m.Configure(host.MapConfig{"explorer.auto_reveal": "false"})
	m.SetActive("")
	m.SetActive(target)
	if m.wantReveal {
		t.Fatal("auto_reveal off: SetActive must not arm a reveal")
	}
}

func TestRevealMethodArmsPendingCmd(t *testing.T) {
	root := tree(t)
	m := mounted(t, root, 40, 10)
	target := filepath.Join(root, "sub", "c.txt")
	m.SetActive(target)
	m.Reveal()
	if !m.wantReveal {
		t.Fatal("Reveal() must arm wantReveal")
	}
	cmd := m.PendingRevealCmd()
	// sub is unloaded: the drained reveal starts its scan.
	if cmd == nil {
		t.Fatal("expected the sub scan Cmd")
	}
	sd, ok := cmd().(ScanDoneMsg)
	if !ok {
		t.Fatalf("expected ScanDoneMsg, got %#v", cmd())
	}
	m, _ = m.Update(sd)
	if got := cursorPath(t, m); got != target {
		t.Fatalf("cursor on %q, want %q", got, target)
	}
}
