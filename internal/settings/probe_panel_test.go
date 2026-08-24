package settings

import (
	"os"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"ike/internal/keymap"
)

// TestProbedMissingFlagsRow pins the prominent-flag rule (#2080): a default
// whose chord probed missing in this terminal carries a visible ✗ marker in
// the keymap list, not just the generic fragility ⚠.
func TestProbedMissingFlagsRow(t *testing.T) {
	t.Cleanup(func() { keymap.SetProbeVerdicts(nil) })
	k, _ := keymapPage(t)
	b := selectChord(t, k, "cmd+p")
	keymap.SetProbeVerdicts([]keymap.ProbeResult{{Chord: "cmd+p", Delivered: false}})
	row := k.renderRow(keymapRow{Binding: b}, false, 120)
	if !strings.Contains(row, "✗ probed missing") {
		t.Fatalf("probed-missing default must be flagged, got %q", row)
	}
	// A probed-delivered chord loses the static ⚠ entirely — after the
	// config reload a doctor save triggers, which re-derives Fragile.
	keymap.SetProbeVerdicts([]keymap.ProbeResult{{Chord: "cmd+p", Delivered: true}})
	b.Fragile = keymap.Classify(b.Chord) != keymap.Delivered
	row = k.renderRow(keymapRow{Binding: b}, false, 120)
	if strings.Contains(row, "⚠") || strings.Contains(row, "✗") {
		t.Fatalf("probed-delivered chord must carry no marker, got %q", row)
	}
}

// TestCaptureWarnsFromProbe pins that the capture-time honesty warning speaks
// probed truth: a chord that probed missing warns with the probe wording.
func TestCaptureWarnsFromProbe(t *testing.T) {
	t.Cleanup(func() { keymap.SetProbeVerdicts(nil) })
	keymap.SetProbeVerdicts([]keymap.ProbeResult{{Chord: "cmd+s", Delivered: false}})
	if w := fragileWarning(keymap.MustParseChord("cmd+s")); !strings.Contains(w, "probed missing") {
		t.Fatalf("capture warning must prefer the probe verdict, got %q", w)
	}
}

// TestProbePanelViewAndClear covers the doctor's settings surface: stored
// runs render (missing chords with evidence), and clearing drops the run and
// uninstalls the running terminal's verdicts.
func TestProbePanelViewAndClear(t *testing.T) {
	t.Cleanup(func() { keymap.SetProbeVerdicts(nil) })
	t.Setenv("IKE_CONFIG_DIR", t.TempDir())
	k, _ := keymapPage(t)

	store := keymap.LoadProbeStore(keymap.ProbeStorePath())
	current := keymap.TerminalID(os.Getenv)
	store.Set(current, "2026-08-24T00:00:00Z", []keymap.ProbeResult{
		{Chord: "cmd+p", Delivered: false, Got: "p"},
		{Chord: "ctrl+p", Delivered: true},
	})
	if err := store.Save(keymap.ProbeStorePath()); err != nil {
		t.Fatal(err)
	}
	keymap.SetProbeVerdicts(store.Results(current))

	launched := false
	p := newProbePanel(k, k.host, func() tea.Cmd { launched = true; return nil })
	view := p.View(100, 20)
	if !strings.Contains(view, current) || !strings.Contains(view, "✗ cmd+p (arrives as p)") {
		t.Fatalf("panel must list the stored run with evidence:\n%s", view)
	}

	// Run Probe pops the settings stack and fires the launcher.
	p.run()
	if !launched {
		t.Fatal("Run Probe must call the launcher")
	}

	// Clear drops the stored run and uninstalls the live verdicts.
	if cmd := p.clear(); cmd == nil {
		t.Fatal("clear must reload the config to rebuild Fragile flags")
	}
	if got := keymap.LoadProbeStore(keymap.ProbeStorePath()).TerminalIDs(); len(got) != 0 {
		t.Fatalf("store not cleared: %v", got)
	}
	if _, ok := keymap.ProbeVerdict("cmd+p"); ok {
		t.Fatal("clearing the running terminal must uninstall its verdicts")
	}
}
