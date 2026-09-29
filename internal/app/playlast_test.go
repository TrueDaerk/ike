package app

import (
	"os"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"ike/internal/jqplay"
)

// playlast_test.go covers the per-file last program's persistence across a
// restart (#2774): the session-only mapping (playLastProgram) is documented
// by playhistory_test.go's history cases; these are the disk-backed ones.

// TestJQPlaygroundLastProgramPersistsAcrossRestart is the issue's acceptance
// case: a program run on a file, then closed, is offered — and evaluated —
// when a fresh process opens the playground on the same path.
func TestJQPlaygroundLastProgramPersistsAcrossRestart(t *testing.T) {
	m, _ := playWatchApp(t, `{"items":[10,20,30]}`)
	m = dismissOnboarding(m)
	m = setProgram(m, ".items[0]")
	path := m.play.srcPath
	if path == "" {
		t.Fatal("the source must be file-backed for this case")
	}
	m = drainKey(m, tea.KeyPressMsg{Code: tea.KeyEscape})
	if m.playOpen() {
		t.Fatal("esc must close the playground")
	}
	if _, err := os.Stat(jqplay.LastProgramFile()); err != nil {
		t.Fatalf("the last-program file must be written under IKE_CONFIG_DIR: %v", err)
	}

	// A fresh process, same config directory.
	fresh := New()
	tm, _ := fresh.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	m2 := dismissOnboarding(tm.(Model))
	tm2, cmd := m2.openPath(path, false)
	m2 = drainCmd(tm2.(Model), cmd)
	m2 = openJQ(t, m2)

	if got := m2.play.program.Text; got != ".items[0]" {
		t.Fatalf("seed program after a restart = %q, want .items[0]", got)
	}
	if !m2.play.haveResult {
		t.Fatal("the restored program must have been evaluated on open")
	}
	if len(m2.play.result.Outputs) != 1 || strings.TrimSpace(m2.play.result.Outputs[0]) != "10" {
		t.Errorf("restored evaluation outputs = %v, want [10]", m2.play.result.Outputs)
	}
}

// TestJQPlaygroundLastProgramDialectSeparate: yq on a YAML file and jq on a
// JSON file with the same base name keep independent entries — the dialect is
// part of the persisted key, like the in-session map.
func TestJQPlaygroundLastProgramDialectSeparate(t *testing.T) {
	m := openJQ(t, playApp(t, `{"a":1}`))
	m = dismissOnboarding(m)
	m = setProgram(m, ".a")
	jsonPath := m.play.srcPath
	m = drainKey(m, tea.KeyPressMsg{Code: tea.KeyEscape})

	m = openOtherFile(t, m, "deploy.yaml", "b: 2\n")
	m = openYQ(t, m)
	m = setProgram(m, ".b")
	yamlPath := m.play.srcPath
	m = drainKey(m, tea.KeyPressMsg{Code: tea.KeyEscape})

	if jsonPath == yamlPath {
		t.Fatal("test setup: the two sources must have distinct paths")
	}

	store := jqplay.NewLastPrograms(jqplay.LastProgramFile())
	if got, ok := store.Get("jq:file:" + jsonPath); !ok || got != ".a" {
		t.Errorf("jq entry = %q, %v; want .a, true", got, ok)
	}
	if got, ok := store.Get("yq:file:" + yamlPath); !ok || got != ".b" {
		t.Errorf("yq entry = %q, %v; want .b, true", got, ok)
	}
}
