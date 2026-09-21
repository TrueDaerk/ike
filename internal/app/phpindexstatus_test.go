package app

// phpindexstatus_test.go covers the index's operations surface (0520, #2673):
// the two commands and their default chords, the status popup's rendering
// (warm, disabled, unavailable), the status line's "php-index …" slot, the
// rebuild's reset-and-rescan and the one telemetry op per scan.

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"ike/internal/config"
	"ike/internal/editor"
	"ike/internal/host"
	"ike/internal/keymap"
	"ike/internal/phpindex"
	"ike/internal/registry"
	"ike/internal/telemetry"

	_ "ike/plugins/languages/php"
)

// phpOpsModel is a model over a PHP fixture project with the index warm.
func phpOpsModel(t *testing.T) Model {
	t.Helper()
	proj := t.TempDir()
	t.Setenv("IKE_CONFIG_DIR", t.TempDir())
	t.Chdir(proj)
	writePHPFixture(t, proj)

	cfg, _ := config.Load(config.Options{})
	config.Set(cfg)
	m := NewWith(registry.New(), host.FromConfig(cfg))
	if !m.PHPIndex().Available() {
		t.Skip("no PHP grammar in this build (cgo off)")
	}
	tm, _ := m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	m = tm.(Model)
	waitPHPScan(t, m.PHPIndex())
	return m
}

// phpFixtureEditor opens the fixture's trait file and returns the focused
// editor, so the status-line tests run against a real PHP buffer.
func phpFixtureEditor(t *testing.T, m Model) (Model, *editor.Model) {
	t.Helper()
	tm, _ := m.openPath(filepath.Join(cwd(t), "src", "A.php"), false)
	m = tm.(Model)
	ed := m.activeEditor()
	if ed == nil || !ed.HasFile() {
		t.Fatal("the fixture PHP file should be open in the focused editor")
	}
	return m, ed
}

// TestPHPIndexCommandsRegisteredAndBound: both commands reach the palette
// through the app plugin's registry and carry a default chord, so the
// keybind audit has nothing to excuse.
func TestPHPIndexCommandsRegisteredAndBound(t *testing.T) {
	reg := registry.New()
	reg.Add(appCommands{})
	bound := map[string]string{}
	for _, b := range keymap.Defaults(keymap.PresetJetBrains) {
		if _, ok := bound[b.Command]; !ok {
			bound[b.Command] = b.Chord.String()
		}
	}
	for _, id := range []string{"php.traitIndex.rebuild", "php.traitIndex.status"} {
		c, ok := reg.Command(id)
		if !ok {
			t.Fatalf("%s is not registered", id)
		}
		if !strings.HasPrefix(c.Title, "PHP Index: ") {
			t.Errorf("%s title = %q, want a PHP Index prefix", id, c.Title)
		}
		if bound[id] == "" {
			t.Errorf("%s has no default chord", id)
		}
	}
}

// TestPHPIndexStatusPopupRendersStats: the command opens the floating shell
// with the Stats() numbers, and esc dismisses it like every other modal.
func TestPHPIndexStatusPopupRendersStats(t *testing.T) {
	m := phpOpsModel(t)
	tm, _ := m.Update(PHPIndexStatusMsg{})
	m = tm.(Model)
	if !m.shell.IsOpen() {
		t.Fatal("php.traitIndex.status should open the floating shell")
	}
	v := ansi.Strip(m.render())
	for _, want := range []string{"PHP Declaration Index", "files", "declarations", "edges", "last scan", "ready"} {
		if !strings.Contains(v, want) {
			t.Errorf("popup should mention %q:\n%s", want, v)
		}
	}
}

// TestPHPIndexStatusBodyStates: the two states in which the index can never
// answer say so instead of printing a row of zeroes.
func TestPHPIndexStatusBodyStates(t *testing.T) {
	if got := phpIndexStatusBody(nil); !strings.Contains(got, "unavailable") {
		t.Errorf("a missing index should read unavailable: %q", got)
	}
	off := phpindex.New(t.TempDir(), phpindex.Options{Enabled: false, ParentDepth: 3, MaxFiles: 20000})
	got := phpIndexStatusBody(off)
	if off.Available() {
		if !strings.Contains(got, "disabled") || !strings.Contains(got, "php.trait_index") {
			t.Errorf("a disabled index should name the setting: %q", got)
		}
	} else if !strings.Contains(got, "unavailable") {
		// A build without the PHP grammar reports the harder state first.
		t.Errorf("a grammar-less build should read unavailable: %q", got)
	}
}

// TestPHPIndexStatusBodyKeepsThePopupNarrow: a deeply nested project root is
// elided from the left, so the box stays inside the terminal and still names
// the project by its last segments.
func TestPHPIndexStatusBodyKeepsThePopupNarrow(t *testing.T) {
	m := phpOpsModel(t)
	for _, line := range strings.Split(phpIndexStatusBody(m.PHPIndex()), "\n") {
		if n := len([]rune(line)); n > phpStatusPathWidth+4 {
			t.Errorf("body line is %d cells wide, want at most %d: %q", n, phpStatusPathWidth+4, line)
		}
	}
	long := "/a/very/deeply/nested/" + strings.Repeat("segment/", 20) + "project"
	got := elideHead(long, phpStatusPathWidth)
	if len([]rune(got)) != phpStatusPathWidth {
		t.Errorf("elided path is %d runes, want %d", len([]rune(got)), phpStatusPathWidth)
	}
	if !strings.HasPrefix(got, "…") || !strings.HasSuffix(got, "project") {
		t.Errorf("elided path = %q, want the tail behind an ellipsis", got)
	}
	if got := elideHead("/short", phpStatusPathWidth); got != "/short" {
		t.Errorf("a short path must stay whole, got %q", got)
	}
}

// TestPHPIndexStatusSegment: the LSP slot carries "php-index …" while the
// walk runs on a PHP buffer, stays silent for another language, and clears
// once the scan finished.
func TestPHPIndexStatusSegment(t *testing.T) {
	m := phpOpsModel(t)
	m, ed := phpFixtureEditor(t, m)

	if got := phpIndexSegmentFor(true, ed); got != "php-index …" {
		t.Errorf("scanning with a PHP buffer = %q, want the warm-up label", got)
	}
	if got := phpIndexSegmentFor(false, ed); got != "" {
		t.Errorf("a finished scan should clear the slot, got %q", got)
	}
	// Another language's buffer says nothing: the index only knows PHP.
	other := writeTemp(t, t.TempDir(), "main.go", "package main\n")
	tm, _ := m.openPath(other, false)
	if got := phpIndexSegmentFor(true, tm.(Model).activeEditor()); got != "" {
		t.Errorf("a non-PHP buffer should not show the index, got %q", got)
	}
	if got := phpIndexSegmentFor(true, nil); got != "" {
		t.Errorf("no editor = no slot, got %q", got)
	}
	// Composed with the server's own state, both warm-ups share the slot.
	if got := joinLSPStatus("php: ready", "php-index …"); got != "php: ready · php-index …" {
		t.Errorf("joined slot = %q", got)
	}
	if got := joinLSPStatus("", "php-index …"); got != "php-index …" {
		t.Errorf("index alone = %q", got)
	}
	if got := joinLSPStatus("php: ready", ""); got != "php: ready" {
		t.Errorf("server alone = %q", got)
	}
}

// TestPHPIndexSegmentSilentWhenWarm: the real model's slot says nothing about
// the index once its scan is done.
func TestPHPIndexSegmentSilentWhenWarm(t *testing.T) {
	m := phpOpsModel(t)
	if m.phpIndexScanning() {
		t.Fatal("the fixture scan should be finished")
	}
	m, ed := phpFixtureEditor(t, m)
	if got := m.phpIndexSegment(ed); got != "" {
		t.Errorf("a warm index should leave the slot empty, got %q", got)
	}
}

// TestPHPIndexRebuildRescans: the command drops the walk and rescans, so a
// file written behind ike's back is in Stats() afterwards.
func TestPHPIndexRebuildRescans(t *testing.T) {
	m := phpOpsModel(t)
	before := m.PHPIndex().Stats()
	if before.Files == 0 {
		t.Fatalf("setup: the warm index should hold files: %+v", before)
	}

	fresh := filepath.Join(cwd(t), "src", "C.php")
	if err := os.WriteFile(fresh, []byte("<?php\nnamespace App;\nclass C { use A; public function xyz() {} }\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	tm, _ := m.Update(PHPIndexRebuildMsg{})
	m = tm.(Model)
	waitPHPScan(t, m.PHPIndex())

	after := m.PHPIndex().Stats()
	if after.Files <= before.Files {
		t.Fatalf("the rebuild did not rescan: %+v → %+v", before, after)
	}
	consumers := m.PHPIndex().ConsumersOf(`App\A`)
	if len(consumers) != 2 {
		t.Fatalf("after the rebuild, ConsumersOf(A) = %v, want both B and C", consumers)
	}
}

// TestPHPIndexRebuildInertWhenDisabled: with php.trait_index off the command
// scans nothing and says why, rather than silently doing nothing.
func TestPHPIndexRebuildInertWhenDisabled(t *testing.T) {
	m := phpOpsModel(t)
	off, _ := config.Load(config.Options{})
	off.PHP.TraitIndex = false
	tm, _ := m.Update(config.ConfigReloadedMsg{Config: off})
	m = tm.(Model)

	tm, _ = m.Update(PHPIndexRebuildMsg{})
	m = tm.(Model)
	if s := m.PHPIndex().Stats(); s.Enabled || s.Files != 0 {
		t.Fatalf("a disabled index must stay empty: %+v", s)
	}
	if got := phpIndexInertReason(m.PHPIndex()); !strings.Contains(got, "php.trait_index") {
		t.Errorf("the reason should name the setting, got %q", got)
	}
}

// TestPHPIndexScanRecordsTelemetryOp: one php.trait.index_scan op per scan —
// the initial walk plus the rebuild's — carrying ms, files and truncated.
func TestPHPIndexScanRecordsTelemetryOp(t *testing.T) {
	m := phpOpsModel(t)
	// The scan op alone never opens a session file (#2318's ghost rule, see
	// startsSession): it is background work that finishes on every launch. One
	// real usage event makes the session — and the held scan op — land.
	m.usage.Op(telemetry.OpProjectSwitch, telemetry.OpPhaseOK, nil)
	waitPHPOps(t, "the initial scan op", func() bool {
		return len(phpScanOps(t, m)) >= 1
	})
	evs := phpScanOps(t, m)
	if len(evs) != 1 {
		t.Fatalf("the initial scan recorded %d ops, want 1", len(evs))
	}
	d := evs[0].Data
	if d["phase"] != telemetry.OpPhaseOK {
		t.Errorf("phase = %q, want ok", d["phase"])
	}
	if _, ok := d["ms"]; !ok {
		t.Errorf("the scan op must carry ms: %v", d)
	}
	if d["files"] == "" || d["files"] == "0" {
		t.Errorf("files = %q, want the walk's file count", d["files"])
	}
	if d["truncated"] != "false" {
		t.Errorf("truncated = %q, want false for a two-file fixture", d["truncated"])
	}

	tm, _ := m.Update(PHPIndexRebuildMsg{})
	m = tm.(Model)
	waitPHPScan(t, m.PHPIndex())
	waitPHPOps(t, "the rebuild scan op", func() bool { return len(phpScanOps(t, m)) >= 2 })
	if n := len(phpScanOps(t, m)); n != 2 {
		t.Fatalf("after one rebuild %d scan ops were recorded, want 2", n)
	}
}

// waitPHPOps polls until cond holds, for the events the index's own timer
// goroutine writes after the scan finished.
func waitPHPOps(t *testing.T, what string, cond func() bool) {
	t.Helper()
	for start := time.Now(); !cond(); {
		if time.Since(start) > 10*time.Second {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// phpScanOps returns this model's php.trait.index_scan events.
func phpScanOps(t *testing.T, m Model) []telemetry.Event {
	t.Helper()
	m.usage.Flush()
	sid := m.usage.SessionID()
	entries, err := os.ReadDir(telemetryDir())
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		t.Fatal(err)
	}
	var out []telemetry.Event
	for _, e := range entries {
		raw, err := os.ReadFile(filepath.Join(telemetryDir(), e.Name()))
		if err != nil {
			t.Fatal(err)
		}
		for _, line := range strings.Split(strings.TrimSpace(string(raw)), "\n") {
			if line == "" {
				continue
			}
			var ev telemetry.Event
			if err := json.Unmarshal([]byte(line), &ev); err != nil {
				continue // another test's partially flushed line
			}
			if ev.SID == sid && ev.Type == telemetry.TypeOp && ev.Data["id"] == telemetry.OpPHPTraitIndexScan {
				out = append(out, ev)
			}
		}
	}
	return out
}
