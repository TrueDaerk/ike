package testresults

// copy_test.go covers #2071: the marked tree row goes to the clipboard
// through a CopyMsg, with "y" and the modified copy chord.

import (
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
)

func copyChordKey() tea.KeyPressMsg { return tea.KeyPressMsg{Code: 'c', Mod: tea.ModSuper} }

func TestCopyFailedRowCarriesLocation(t *testing.T) {
	m := filled(t)
	m.cursor = 3 // example/pkg → TestTable → case_a, the parsed failure
	cmd := m.Update(key("y"))
	if cmd == nil {
		t.Fatal("y produced no command")
	}
	msg, ok := cmd().(CopyMsg)
	if !ok {
		t.Fatalf("message = %T, want CopyMsg", cmd())
	}
	want := "FAIL example/pkg/TestTable/case_a  " + filepath.Join("/proj/pkg", "x_test.go") + ":7"
	if msg.Text != want {
		t.Errorf("copied %q, want %q", msg.Text, want)
	}
	if msg.What != "test result" {
		t.Errorf("what = %q, want \"test result\"", msg.What)
	}
}

func TestCopyPassedRowCarriesDuration(t *testing.T) {
	m := filled(t)
	m.cursor = 1 // example/pkg → TestOK
	msg := m.Update(key("y"))().(CopyMsg)
	if want := "PASS example/pkg/TestOK  120ms"; msg.Text != want {
		t.Errorf("copied %q, want %q", msg.Text, want)
	}
}

// TestCopyGroupRow: a synthesized parent copies its aggregated outcome.
func TestCopyGroupRow(t *testing.T) {
	m := filled(t)
	m.cursor = 0
	msg := m.Update(key("y"))().(CopyMsg)
	if msg.Text != "FAIL example/pkg" {
		t.Errorf("group copy = %q", msg.Text)
	}
}

func TestCopyChordAliasesY(t *testing.T) {
	m := filled(t)
	m.cursor = 1
	bare := m.Update(key("y"))().(CopyMsg)
	chord := m.Update(copyChordKey())
	if chord == nil {
		t.Fatal("cmd+c produced no command")
	}
	if got := chord().(CopyMsg); got != bare {
		t.Errorf("chord copied %+v, bare copied %+v", got, bare)
	}
}

func TestCopyEmptyTreeDoesNothing(t *testing.T) {
	m := New(nil)
	m.SetSize(100, 12)
	if cmd := m.Update(key("y")); cmd != nil {
		t.Errorf("empty tree produced %+v, want no copy", cmd())
	}
	if cmd := m.Update(copyChordKey()); cmd != nil {
		t.Errorf("empty tree chord produced %+v, want no copy", cmd())
	}
}

func TestCopyKeyInFooter(t *testing.T) {
	m := filled(t)
	if !strings.Contains(m.View(), "y copy") {
		t.Error("footer must name the copy key")
	}
}
