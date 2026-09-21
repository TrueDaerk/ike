package phpindex

// rebuild_test.go covers the operations half of the index (#2673): the
// rebuild the status command's neighbour triggers, and the per-scan callback
// the telemetry op rides on.

import (
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"ike/internal/host"
)

// TestRebuildRescansAndPicksUpDiskChanges: a file written behind the index's
// back is invisible until Rebuild, which drops the walk, rescans and leaves
// the stats filled again.
func TestRebuildRescansAndPicksUpDiskChanges(t *testing.T) {
	x, dir := fixture(t, defaultOpts())
	before := x.Stats()
	if before.Files == 0 || before.Declarations == 0 {
		t.Fatalf("setup: warm index should hold something: %+v", before)
	}
	if before.Scanning {
		t.Fatalf("setup: the fixture scan should be finished: %+v", before)
	}

	// The change no watcher event described: a whole file appears.
	fresh := filepath.Join(dir, "src", "Models", "Rebuilt.php")
	text := "<?php\nnamespace App\\Models;\nclass Rebuilt { public function rebuilt() {} }\n"
	if err := os.WriteFile(fresh, []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := x.DeclarationsNamed(`App\Models\Rebuilt`); len(got) != 0 {
		t.Fatalf("the index should not know the file yet: %v", got)
	}

	if !x.Rebuild() {
		t.Fatal("Rebuild on an enabled index should start a scan")
	}
	waitFor(t, "the rebuild scan", func() bool { return x.ScanDone() })
	if got := x.DeclarationsNamed(`App\Models\Rebuilt`); len(got) != 1 {
		t.Fatalf("the rebuilt index should know the new class: %v", got)
	}
	after := x.Stats()
	if after.Files <= before.Files || after.Declarations <= before.Declarations {
		t.Fatalf("stats did not grow: %+v → %+v", before, after)
	}
	if after.Scanning || !after.Enabled || after.Unavailable {
		t.Fatalf("after the rebuild, stats = %+v", after)
	}
}

// TestRebuildKeepsObservedBuffers: the walk is dropped, the editor's unsaved
// truth is not — a rebuild must not un-know what the user is typing.
func TestRebuildKeepsObservedBuffers(t *testing.T) {
	x, dir := fixture(t, defaultOpts())
	bPath := filepath.Join(dir, "src", "Models", "B.php")
	text, err := os.ReadFile(bPath)
	if err != nil {
		t.Fatal(err)
	}
	edited := string(text[:len(text)-len("}\n")]) + " public function unsavedMember() {} }\n"
	x.Observe(host.EditorEvent{Kind: host.EditorChange, Path: bPath, Text: edited})
	x.Flush()
	if !hasDeclMember(x, `App\Models\B`, "unsavedMember") {
		t.Fatal("setup: the buffer edit should be in the index")
	}

	if !x.Rebuild() {
		t.Fatal("Rebuild should start a scan")
	}
	waitFor(t, "the rebuild scan", func() bool { return x.ScanDone() })
	if !hasDeclMember(x, `App\Models\B`, "unsavedMember") {
		t.Fatal("the rebuild dropped the observed buffer's unsaved member")
	}
}

// hasDeclMember reports whether fqn declares a member of that name.
func hasDeclMember(x *Index, fqn, name string) bool {
	for _, d := range x.DeclarationsNamed(fqn) {
		if d.FQN != fqn {
			continue
		}
		for _, m := range d.Members {
			if m.Name == name {
				return true
			}
		}
	}
	return false
}

// TestRebuildInertWhileDisabled: with php.trait_index off there is nothing to
// rescan, and the command must be able to say so.
func TestRebuildInertWhileDisabled(t *testing.T) {
	opts := defaultOpts()
	opts.Enabled = false
	x := New(t.TempDir(), opts)
	if x.Rebuild() {
		t.Fatal("a disabled index must refuse the rebuild")
	}
	if s := x.Stats(); s.Enabled || s.Files != 0 {
		t.Fatalf("disabled stats = %+v", s)
	}
}

// TestOnScanFiresOncePerScan: the telemetry callback reports the initial walk
// exactly once — the settle poll keeps running for buffer edits — and once
// more for a rebuild's walk.
func TestOnScanFiresOncePerScan(t *testing.T) {
	x, dir := fixture(t, defaultOpts())
	var scans atomic.Int64
	var last atomic.Value
	x.SetOnScan(func(s Stats) {
		scans.Add(1)
		last.Store(s)
	})
	waitFor(t, "the initial scan report", func() bool { return scans.Load() > 0 })
	if got := scans.Load(); got != 1 {
		t.Fatalf("the initial scan reported %d times, want 1", got)
	}
	s, _ := last.Load().(Stats)
	if s.Files == 0 || s.Scanning {
		t.Fatalf("the reported stats should describe a finished walk: %+v", s)
	}

	// A buffer edit keeps the settle poll busy but is not a scan.
	bPath := filepath.Join(dir, "src", "Models", "B.php")
	text, err := os.ReadFile(bPath)
	if err != nil {
		t.Fatal(err)
	}
	x.Observe(host.EditorEvent{Kind: host.EditorChange, Path: bPath, Text: string(text) + "\n// touched\n"})
	x.Flush()
	time.Sleep(4 * changeSettle)
	if got := scans.Load(); got != 1 {
		t.Fatalf("a buffer edit reported %d scans, want the initial 1", got)
	}

	// A rebuild is a new walk and earns its own event.
	if !x.Rebuild() {
		t.Fatal("Rebuild should start a scan")
	}
	waitFor(t, "the rebuild scan report", func() bool { return scans.Load() > 1 })
	waitFor(t, "the settle poll to quiesce", func() bool {
		n := scans.Load()
		time.Sleep(3 * changeSettle)
		return scans.Load() == n
	})
	if got := scans.Load(); got != 2 {
		t.Fatalf("after one rebuild, %d scans were reported, want 2", got)
	}
}
