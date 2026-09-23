package app

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"ike/internal/changefeed"
	"ike/internal/explorer"
	"ike/internal/forge"
	"ike/internal/host"
	"ike/internal/vcs"
	"ike/internal/watch"
)

// renderreuse_idle_test.go covers the idle-churn half of the render-reuse
// rule (#2693): the background wakes an idle session still gets — the git
// status debounce tick and its unchanged snapshot, a forge poll deadline, a
// watcher batch over files nobody has open, an explorer rescan that found the
// same listing, a change-feed capture landing behind a closed picker — reuse
// the previous frame; the same wakes render when they do change something.
// The motion half through the real coalescer is here too. All go through
// pass(), the Update+View round bubbletea runs, and read the two view pass
// counters.

// idleModel is a sized, onboarding-free model with one frame composed, as
// the loop has before the first background wake.
func idleModel(t *testing.T) Model {
	t.Helper()
	m := dismissOnboarding(sized(t, 100, 40))
	m.View()
	return m
}

func TestCoalescerMotionWithoutHoverChangeNeverRenders(t *testing.T) {
	m := idleModel(t)
	c := NewMouseCoalescer()
	send, snapshot := collectSender()
	c.SetSender(send)
	renders := 0
	const bursts = 5
	for b := 0; b < bursts; b++ {
		// One burst: a handful of motion steps over the empty editor body,
		// folded by the real filter and flushed as one coalescedInputMsg.
		for i := 0; i < 4; i++ {
			x, y := editorBodyCell(t, m, b*3+i, i%2)
			if out := c.Filter(nil, tea.MouseMotionMsg{X: x, Y: y}); out != nil {
				t.Fatalf("the filter must absorb motion, got %T", out)
			}
		}
		burst := waitForFlush(t, snapshot)
		if burst.motion == nil || len(burst.wheels) != 0 || len(burst.termKeys) != 0 {
			t.Fatalf("flushed burst = %+v, want motion alone", burst)
		}
		var composed bool
		m, composed = pass(t, m, burst)
		if composed {
			renders++
		}
		// Let the coalescer disarm before the next round so each burst is
		// a fresh flush rather than a re-arm of the previous one.
		time.Sleep(coalesceCeiling + 10*time.Millisecond)
		c.mu.Lock()
		c.armed = false
		c.mu.Unlock()
		send, snapshot = collectSender()
		c.SetSender(send)
	}
	if renders != 0 {
		t.Fatalf("%d coalesced motion bursts over an empty editor composed %d frames, want 0", bursts, renders)
	}
}

func TestVCSTickReusesFrame(t *testing.T) {
	m := idleModel(t)
	m.vcs.tickArmed = true
	m, composed := pass(t, m, vcsTickMsg{})
	if composed {
		t.Fatal("the vcs debounce tick launches a subprocess and moves nothing on screen: must reuse the frame")
	}
	if m.vcs.tickArmed || !m.vcs.refreshing {
		t.Fatalf("the tick must still run the refresh: armed=%v refreshing=%v", m.vcs.tickArmed, m.vcs.refreshing)
	}
}

func TestVCSSnapshotUnchangedReusesFrame(t *testing.T) {
	m := idleModel(t)
	first := vcs.NewSnapshot("/r", map[string]vcs.FileStatus{"a.go": vcs.StatusModified})
	m, composed := pass(t, m, vcs.SnapshotMsg{Snap: first})
	if !composed || m.VCSSnapshot() != first {
		t.Fatalf("a snapshot that differs from the last one must render and land (composed=%v)", composed)
	}
	same := vcs.NewSnapshot("/r", map[string]vcs.FileStatus{"a.go": vcs.StatusModified})
	m, composed = pass(t, m, vcs.SnapshotMsg{Snap: same})
	if composed {
		t.Fatal("a refresh that found the same status must reuse the frame")
	}
	if m.VCSSnapshot() != first {
		t.Fatal("an unchanged refresh keeps the snapshot the consumers already hold")
	}
	changed := vcs.NewSnapshot("/r", map[string]vcs.FileStatus{"a.go": vcs.StatusModified, "b.go": vcs.StatusUntracked})
	m, composed = pass(t, m, vcs.SnapshotMsg{Snap: changed})
	if !composed || m.VCSSnapshot() != changed {
		t.Fatalf("a changed status must render and land (composed=%v)", composed)
	}
	// Leaving the repository (nil snapshot) is a change too.
	if _, composed = pass(t, m, vcs.SnapshotMsg{Snap: nil}); !composed {
		t.Fatal("a nil snapshot after a real one must render")
	}
}

func TestForgePollTickReusesFrame(t *testing.T) {
	m := dismissOnboarding(pollApp(t, host.MapConfig{}))
	m.View()
	root := m.forgeRoot()
	m, composed := pass(t, m, forge.PollTickMsg{Root: root})
	if composed {
		t.Fatal("a poll deadline only dispatches the fetch: must reuse the frame")
	}
	if !m.forgePoller().InFlight() {
		t.Fatal("the tick must still dispatch the fetch")
	}
	// A dropped tick (one arriving mid-flight) changes even less.
	if _, composed = pass(t, m, forge.PollTickMsg{Root: root}); composed {
		t.Fatal("a dropped poll tick must reuse the frame")
	}
}

func TestChangeFeedCaptureBehindClosedPickerReusesFrame(t *testing.T) {
	m := idleModel(t)
	entry := func(name string) changefeed.Entry {
		return changefeed.Entry{Path: filepath.Join(t.TempDir(), name), Time: time.Now(), Kind: changefeed.Changed}
	}
	m, composed := pass(t, m, changeFeedCapturedMsg{entries: []changefeed.Entry{entry("a.txt")}})
	if composed {
		t.Fatal("a capture landing while the picker is closed shows nowhere: must reuse the frame")
	}
	if m.feed.Len() != 1 {
		t.Fatalf("the capture must still be recorded, feed = %d", m.feed.Len())
	}
	// An empty capture (every entry already known) reuses too.
	if _, composed = pass(t, m, changeFeedCapturedMsg{}); composed {
		t.Fatal("an empty capture must reuse the frame")
	}
	// With the picker open the same landing is a visible list change.
	m.openChangeFeed()
	m.View()
	if !m.cfPicker {
		t.Fatal("setup: the picker did not open")
	}
	if _, composed = pass(t, m, changeFeedCapturedMsg{entries: []changefeed.Entry{entry("b.txt")}}); !composed {
		t.Fatal("a capture landing in the open picker must render")
	}
}

func TestWatchBatchOverUnviewedPathsReusesFrame(t *testing.T) {
	dir := t.TempDir()
	a := filepath.Join(dir, "a.txt")
	if err := os.WriteFile(a, []byte("one\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	m := idleModel(t)
	m, composed := pass(t, m, watch.EventBatchMsg{Events: []watch.EventMsg{
		{Kind: watch.FileChanged, Path: a},
		{Kind: watch.FileCreated, Path: filepath.Join(dir, "b.txt")},
		{Kind: watch.DirChanged, Path: dir},
		{Kind: watch.GitChanged, Path: filepath.Join(dir, ".git", "index")},
	}})
	if composed {
		t.Fatal("a batch over files nobody has open must reuse the frame")
	}
	if !m.vcs.tickArmed {
		t.Fatal("the batch must still arm the git status refresh")
	}
	// The same file, once open, is a visible change: the buffer reloads.
	tm, _ := m.openPath(a, false)
	m = tm.(Model)
	m.View()
	if err := os.WriteFile(a, []byte("AGENT\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	m, composed = pass(t, m, watch.EventBatchMsg{Events: []watch.EventMsg{{Kind: watch.FileChanged, Path: a}}})
	if !composed {
		t.Fatal("a batch touching an open file must render")
	}
	if ed := m.editorForPath(a); ed == nil || ed.Text() != "AGENT" {
		t.Fatal("the batch did not route the reload to the open buffer")
	}
}

func TestExplorerRescanUnchangedReusesFrame(t *testing.T) {
	m := idleModel(t)
	exp := m.explorer()
	root := exp.Root()
	// A watcher-driven refresh of the root: the scan runs off-loop and its
	// result lands as a ScanDoneMsg.
	_, cmd := exp.Update(watch.EventMsg{Kind: watch.DirChanged, Path: root})
	if cmd == nil {
		t.Fatal("setup: the directory event must launch a rescan")
	}
	sd, ok := cmd().(explorer.ScanDoneMsg)
	if !ok || sd.Err != nil {
		t.Fatalf("setup: rescan = %T %v", sd, sd.Err)
	}
	m, composed := pass(t, m, sd)
	if composed {
		t.Fatal("a rescan that listed the same entries must reuse the frame")
	}
	// A listing that lost an entry is a row change.
	_, cmd = m.explorer().Update(watch.EventMsg{Kind: watch.DirChanged, Path: root})
	sd = cmd().(explorer.ScanDoneMsg)
	if len(sd.Entries) == 0 {
		t.Fatal("setup: the root scan listed nothing")
	}
	sd.Entries = sd.Entries[:len(sd.Entries)-1]
	if _, composed = pass(t, m, sd); !composed {
		t.Fatal("a rescan with a changed listing must render")
	}
}

func TestNotificationDuringQuietPassRenders(t *testing.T) {
	m := idleModel(t)
	m.vcs.tickArmed = true
	// Something queued a toast during the pass (a hook, a consumer): the
	// settled pass drains it, and a proof of "nothing changed" is withdrawn.
	m.host.Notify(host.Info, "hello")
	m, composed := pass(t, m, vcsTickMsg{})
	if !composed {
		t.Fatal("a pass that drained a notification must render however quiet its handler was")
	}
	if len(m.toasts) == 0 {
		t.Fatal("setup: the notification did not become a toast")
	}
}
