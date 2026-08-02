package app

import (
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"ike/internal/registry"
	"ike/internal/ui"
	"ike/internal/vcs"
)

func rangeLogFixture() vcs.RangeLogMsg {
	return vcs.RangeLogMsg{
		Path: "f.go", StartLine: 2, EndLine: 4,
		Entries: []vcs.RangeLogEntry{
			{LogEntry: vcs.LogEntry{Hash: "a1", ShortHash: "a1", Author: "alice",
				Time: time.Now(), Subject: "newer"}, Patch: "@@ -2,3 +2,3 @@\n-old\n+new"},
			{LogEntry: vcs.LogEntry{Hash: "b2", ShortHash: "b2", Author: "bob",
				Time: time.Now(), Subject: "older"}, Patch: "@@ -1,3 +1,3 @@\n+init"},
		},
	}
}

// TestHistoryPickerFlow guards #1430: list opens, j moves, enter expands to
// the patch view, esc steps back to the list, esc again closes.
func TestHistoryPickerFlow(t *testing.T) {
	m := sizedWith(t, registry.New(), 100, 40)
	m.openHistoryPicker(rangeLogFixture())
	if !m.historyPickerOpen() {
		t.Fatal("picker not open after openHistoryPicker")
	}
	if !strings.Contains(m.renderHistoryList(), "newer") {
		t.Fatal("list missing commit subject")
	}

	out, _ := m.updateHistoryPicker(tea.KeyPressMsg{Text: "j", Code: 'j'})
	m = out.(Model)
	if m.histSel != 1 {
		t.Fatalf("sel = %d after j, want 1", m.histSel)
	}

	out, _ = m.updateHistoryPicker(tea.KeyPressMsg{Code: tea.KeyEnter})
	m = out.(Model)
	if !m.histPatch {
		t.Fatal("enter did not open the patch view")
	}
	if body := m.renderHistoryPatch(); !strings.Contains(body, "+init") || !strings.Contains(body, "older") {
		t.Fatalf("patch view body = %q", body)
	}

	out, _ = m.updateHistoryPicker(tea.KeyPressMsg{Code: tea.KeyEscape})
	m = out.(Model)
	if m.histPatch || !m.historyPickerOpen() {
		t.Fatal("esc from patch view should return to the list, not close")
	}

	out, _ = m.updateHistoryPicker(tea.KeyPressMsg{Code: tea.KeyEscape})
	m = out.(Model)
	if m.historyPickerOpen() {
		t.Fatal("esc from list should close the picker")
	}
}

// TestHistoryPickerRenderFollowsSelection guards the re-bind in
// setHistoryListContent: the shell body must reflect a selection move made in
// a LATER model copy (the root model is a value model — a closure bound only
// at open time renders the open-time selection forever).
func TestHistoryPickerRenderFollowsSelection(t *testing.T) {
	m := sizedWith(t, registry.New(), 100, 40)
	m.openHistoryPicker(rangeLogFixture())
	out, _ := m.Update(tea.KeyPressMsg{Text: "j", Code: 'j'})
	m2 := out.(Model)
	body := m2.shell.Content().(ui.ModelContent).Render(80)
	lines := strings.Split(body, "\n")
	if len(lines) < 2 || !strings.HasPrefix(lines[1], "▍") {
		t.Fatalf("marker not on row 2 after j:\n%s", body)
	}
}

// TestHistoryPickerEmptyAndError guards #1430: errors and empty results toast
// instead of opening the picker.
func TestHistoryPickerEmptyAndError(t *testing.T) {
	m := sizedWith(t, registry.New(), 100, 40)
	m.openHistoryPicker(vcs.RangeLogMsg{Path: "f.go", StartLine: 1, EndLine: 1})
	if m.historyPickerOpen() {
		t.Fatal("picker opened on an empty result")
	}
}

// TestHistoryForSelectionNoRepo guards #1430: outside a git repository the
// command toasts instead of running git.
func TestHistoryForSelectionNoRepo(t *testing.T) {
	m := sizedWith(t, registry.New(), 100, 40)
	m.vcs.snap = nil
	if _, cmd := m.historyForSelection(); cmd != nil {
		t.Fatal("expected no command without a repository")
	}
}
