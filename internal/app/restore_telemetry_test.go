package app

import (
	"os"
	"testing"

	"ike/internal/telemetry"
)

// restore_telemetry_test.go covers the session.restore span's #2551 fields:
// how many file tabs came back and how many files were gone since the save.

// TestSessionRestoreRecordsTabsAndMissing is the acceptance criterion: the
// "ok" phase reports the reopened tab count next to the pane count, and counts
// the files that vanished under the restore — the same number the summary
// notice tells the user about.
func TestSessionRestoreRecordsTabsAndMissing(t *testing.T) {
	conf, dir := t.TempDir(), t.TempDir()
	a := writeTemp(t, dir, "a.txt", "aaa\n")
	b := writeTemp(t, dir, "b.txt", "bbb\n")
	c := writeTemp(t, dir, "c.txt", "ccc\n")

	m := fixedDirApp(t, conf)
	m = openAll(t, m, a, b, c)
	m.quit()

	if err := os.Remove(a); err != nil {
		t.Fatal(err)
	}
	m2 := fixedDirApp(t, conf)
	// A meaningful event must exist for the deferred restore span to land in
	// the file at all (#2318): the restore alone never opens one.
	m2 = dispatch(t, m2, TabSelectMsg{Index: 0})

	var ok *telemetry.Event
	for _, ev := range eventsOf(usageEvents(t, m2), telemetry.TypeOp) {
		if ev.Data["id"] == telemetry.OpSessionRestore && ev.Data["phase"] == "ok" {
			e := ev
			ok = &e
		}
	}
	if ok == nil {
		t.Fatalf("no %s ok phase recorded", telemetry.OpSessionRestore)
	}
	if ok.Data["tabs"] != "2" {
		t.Errorf("tabs = %q, want \"2\" — %s and %s came back", ok.Data["tabs"], b, c)
	}
	if ok.Data["missing"] != "1" {
		t.Errorf("missing = %q, want \"1\" — one file was deleted", ok.Data["missing"])
	}
	if ok.Data["panes"] == "" {
		t.Errorf("the pane count must survive the widening: %v", ok.Data)
	}
}

// A restore that loses nothing still reports both fields, so an export can
// tell "no files were missing" from "this version did not record it" (#2551).
func TestSessionRestoreRecordsZeroMissing(t *testing.T) {
	conf, dir := t.TempDir(), t.TempDir()
	a := writeTemp(t, dir, "a.txt", "aaa\n")

	m := fixedDirApp(t, conf)
	m = openAll(t, m, a)
	m.quit()

	m2 := fixedDirApp(t, conf)
	m2 = dispatch(t, m2, TabSelectMsg{Index: 0})

	found := false
	for _, ev := range eventsOf(usageEvents(t, m2), telemetry.TypeOp) {
		if ev.Data["id"] != telemetry.OpSessionRestore || ev.Data["phase"] != "ok" {
			continue
		}
		found = true
		if ev.Data["missing"] != "0" || ev.Data["tabs"] != "1" {
			t.Errorf("payload = %v, want tabs 1 and missing 0", ev.Data)
		}
	}
	if !found {
		t.Fatalf("no %s ok phase recorded", telemetry.OpSessionRestore)
	}
}
