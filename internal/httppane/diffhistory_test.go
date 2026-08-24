package httppane

import (
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
)

// TestDiffHistoryKeyEmitsMsg: "D" asks the host to compare the shown stored
// response with another one (#1992) — the pane owns neither the history store
// nor the diff viewer.
func TestDiffHistoryKeyEmitsMsg(t *testing.T) {
	m := New(nil)
	m.SetSize(80, 20)
	m.Set("create", sample())

	cmd := m.Update(tea.KeyPressMsg{Code: 'D', Text: "D", Mod: tea.ModShift})
	if cmd == nil {
		t.Fatal("D must emit a command")
	}
	if _, ok := cmd().(DiffHistoryMsg); !ok {
		t.Fatalf("message type: %T", cmd())
	}
}

// TestFooterAdvertisesDiff: the hint appears once a second stored response
// exists — before that there is nothing to compare against (#1992).
func TestFooterAdvertisesDiff(t *testing.T) {
	m := New(nil)
	m.SetSize(160, 20)
	m.Set("create", sample())
	if view := m.View(); strings.Contains(view, "D diff") {
		t.Errorf("a single response has nothing to compare with:\n%s", view)
	}

	now := time.Now()
	m.SetHistory([]HistoryItem{
		{Resp: sample(), At: now},
		{Resp: sample(), At: now.Add(-time.Minute)},
	})
	if view := m.View(); !strings.Contains(view, "D diff") {
		t.Errorf("footer must advertise the diff once two responses are stored:\n%s", view)
	}
}

// TestDiffPreviousRunKeyEmitsMsg: "P" asks the host to diff the shown stored
// response directly against the previous run (#2060) — a shortcut around
// DiffHistoryMsg's picker.
func TestDiffPreviousRunKeyEmitsMsg(t *testing.T) {
	m := New(nil)
	m.SetSize(80, 20)
	m.Set("create", sample())

	cmd := m.Update(tea.KeyPressMsg{Code: 'P', Text: "P", Mod: tea.ModShift})
	if cmd == nil {
		t.Fatal("P must emit a command")
	}
	if _, ok := cmd().(DiffPreviousRunMsg); !ok {
		t.Fatalf("message type: %T", cmd())
	}
}

// TestFooterAdvertisesDiffPreviousRun: the direct shortcut's hint only
// appears while an earlier run actually exists below the one on show (#2060)
// — offering it while browsing the oldest entry would open nothing.
func TestFooterAdvertisesDiffPreviousRun(t *testing.T) {
	m := New(nil)
	m.SetSize(160, 20)
	now := time.Now()
	m.Set("create", sample())
	m.SetHistory([]HistoryItem{
		{Resp: sample(), At: now},
		{Resp: sample(), At: now.Add(-time.Minute)},
	})
	if view := m.View(); !strings.Contains(view, "P diff prev") {
		t.Errorf("footer must advertise the previous-run diff while a newer entry is shown:\n%s", view)
	}

	m.showHistory(m.histIdx + 1)
	if view := m.View(); strings.Contains(view, "P diff prev") {
		t.Errorf("the oldest entry has no previous run to diff against:\n%s", view)
	}
}
