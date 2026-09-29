package app

import (
	"os"
	"path/filepath"
	"testing"

	"ike/internal/pane"
	"ike/internal/project"
)

// searchhistory_wiring_test.go covers #2826: every editor the app shows is
// wired to the model's query-history store (#1171), so up/down on the "/" line
// recall — after opening a file, in a new pane, after a project switch and on
// the resumed workspace after switching back.

// assertEditorsShareHistories fails unless every editor of the active
// workspace carries the model's store.
func assertEditorsShareHistories(t *testing.T, m Model, when string) {
	t.Helper()
	if m.qhist == nil {
		t.Fatalf("%s: model has no history store", when)
	}
	seen := 0
	for _, key := range m.activeWS().Panes.Keys() {
		inst := m.activeWS().Panes.Get(key)
		if inst == nil || inst.Kind() != pane.KindEditor {
			continue
		}
		for _, ed := range inst.Editors() {
			seen++
			if ed.Histories() != m.qhist {
				t.Fatalf("%s: editor %s is not wired to the model's history store", when, key)
			}
		}
	}
	if seen == 0 {
		t.Fatalf("%s: no editor to check", when)
	}
}

func TestSearchHistoryWiredAcrossOpenAndSwitch(t *testing.T) {
	base := t.TempDir()
	src, dst := filepath.Join(base, "src"), filepath.Join(base, "dst")
	for _, d := range []string{src, dst} {
		if err := os.Mkdir(d, 0o755); err != nil {
			t.Fatal(err)
		}
		for _, f := range []string{"a.txt", "b.txt"} {
			if err := os.WriteFile(filepath.Join(d, f), []byte("foo\n"), 0o644); err != nil {
				t.Fatal(err)
			}
		}
	}
	t.Chdir(src)
	m := switchModel(t)

	tm, _ := m.openPath(filepath.Join(src, "a.txt"), false)
	m = tm.(Model)
	assertEditorsShareHistories(t, m, "open a file")
	tm, _ = m.openPath(filepath.Join(src, "b.txt"), true)
	m = tm.(Model)
	assertEditorsShareHistories(t, m, "open a file in a new pane")

	out, _ := m.Update(project.SwitchProjectMsg{Root: dst})
	m = out.(Model)
	tm, _ = m.openPath(filepath.Join(dst, "a.txt"), false)
	m = tm.(Model)
	assertEditorsShareHistories(t, m, "after a project switch")

	out, _ = m.Update(project.SwitchProjectMsg{Root: src})
	m = out.(Model)
	assertEditorsShareHistories(t, m, "on the resumed workspace")
	tm, _ = m.openPath(filepath.Join(src, "a.txt"), false)
	m = tm.(Model)
	assertEditorsShareHistories(t, m, "reopening a file after switching back")
}
