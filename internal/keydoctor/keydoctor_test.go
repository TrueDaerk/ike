package keydoctor

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"ike/internal/keymap"
)

func runCmd(t *testing.T, cmd tea.Cmd) tea.Msg {
	t.Helper()
	if cmd == nil {
		t.Fatal("expected a command")
	}
	return cmd()
}

func TestDoctorProbesRawKeys(t *testing.T) {
	m := New()
	m.SetSize(120, 40)
	m.Open("testterm")
	if !m.IsOpen() {
		t.Fatal("Open must open")
	}
	// ctrl+p is a probe target; while probing the overlay must record it raw
	// instead of letting it mean anything.
	if cmd := m.Update(tea.KeyPressMsg{Code: 'p', Mod: tea.ModCtrl}); cmd != nil {
		t.Fatal("probing keys must not produce commands")
	}
	if got := m.sess.Hit("ctrl+p"); got != "ctrl+p" {
		t.Fatalf("ctrl+p not recorded: %q", got)
	}
	if !strings.Contains(m.View(), "ctrl+p") {
		t.Fatal("view must list targets")
	}
}

func TestDoctorSkipKeyIsNotATarget(t *testing.T) {
	m := New()
	m.Open("testterm")
	for _, target := range m.sess.Targets() {
		if target == m.skipKey {
			t.Fatalf("skip key %q collides with a probe target", m.skipKey)
		}
	}
	next := m.sess.Next()
	m.Update(tea.KeyPressMsg{Code: 'n', Mod: tea.ModCtrl}) // default skip key ctrl+n
	if !m.sess.Skipped(next) {
		t.Fatalf("skip key must skip the pending chord %q", next)
	}
}

func TestDoctorMouseNavButton(t *testing.T) {
	m := New()
	m.Open("testterm")
	k, _ := keymap.FromMouseButton(tea.MouseBackward)
	m.RecordKey(k)
	if got := m.sess.Hit(k.String()); got != k.String() {
		t.Fatalf("mouse nav button not recorded: %q", got)
	}
}

func TestDoctorSaveFlow(t *testing.T) {
	m := New()
	m.SetSize(120, 40)
	m.Open("testterm")
	m.Update(tea.KeyPressMsg{Code: 'p', Mod: tea.ModCtrl})
	// ctrl+d ends probing; enter saves.
	if cmd := m.Update(tea.KeyPressMsg{Code: 'd', Mod: tea.ModCtrl}); cmd != nil {
		t.Fatal("finishing must only switch phases")
	}
	if !strings.Contains(m.View(), "save") {
		t.Fatal("summary must offer saving")
	}
	msg := runCmd(t, m.Update(tea.KeyPressMsg{Code: tea.KeyEnter}))
	res, ok := msg.(ResultMsg)
	if !ok || !res.Save || res.Terminal != "testterm" {
		t.Fatalf("save result = %#v", msg)
	}
	if m.IsOpen() {
		t.Fatal("saving must close the overlay")
	}
	found := false
	for _, r := range res.Results {
		if r.Chord == "ctrl+p" {
			found = true
			if !r.Delivered {
				t.Fatal("pressed chord must report delivered")
			}
		}
	}
	if !found {
		t.Fatal("results must include the probed chord")
	}
}

func TestDoctorAbortWithoutSaving(t *testing.T) {
	m := New()
	m.Open("testterm")
	// esc ends probing too (it is not a probe target)…
	m.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	// …esc again resumes probing instead of discarding by accident…
	m.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	if !m.IsOpen() {
		t.Fatal("esc in the summary must resume, not close")
	}
	// …and d discards: closed, no save, nothing persisted.
	m.Update(tea.KeyPressMsg{Code: 'd', Mod: tea.ModCtrl})
	msg := runCmd(t, m.Update(tea.KeyPressMsg{Code: 'd', Text: "d"}))
	res, ok := msg.(ResultMsg)
	if !ok || res.Save {
		t.Fatalf("discard result = %#v", msg)
	}
	if m.IsOpen() {
		t.Fatal("discarding must close the overlay")
	}
}

func TestDoctorSummaryKeysAreNotProbeEvidence(t *testing.T) {
	m := New()
	m.Open("testterm")
	m.Update(tea.KeyPressMsg{Code: 'd', Mod: tea.ModCtrl}) // → summary
	// "z" is a probe target; in the summary it must not be recorded.
	m.Update(tea.KeyPressMsg{Code: 'z', Text: "z"})
	if got := m.sess.Hit("z"); got != "" {
		t.Fatalf("summary keys leaked into the session: %q", got)
	}
}
