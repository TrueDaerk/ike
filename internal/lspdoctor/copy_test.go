package lspdoctor

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
)

// copy_test.go covers the pane's clipboard route (#2487): the plain-text
// rendering carries the whole report and no styling, and the chord is a
// no-op before the first run.

func reportWithResults() *Report {
	r := NewReport()
	r.SetServers([]Server{{Lang: "go", Command: "gopls"}, {Lang: "toml", Command: "taplo"}})
	r.Finish([]Result{
		{
			Lang:    "go",
			Command: "gopls",
			Path:    "/usr/local/bin/gopls",
			Checks:  []Check{{Name: "binary", Status: StatusOK, Detail: "found on PATH"}},
			Class:   ClassOK,
		},
		{
			Lang:      "toml",
			Command:   "taplo",
			Checks:    []Check{{Name: "binary", Status: StatusFail, Detail: "not found"}},
			Class:     ClassMissing,
			Diagnosis: "the binary is nowhere on PATH",
			Fix:       "cargo install taplo-cli",
		},
	})
	return r
}

func TestPlainTextRendersWholeReport(t *testing.T) {
	m := New(nil)
	m.SetSize(20, 10) // narrow on purpose: the copy must not be clipped
	m.SetReport(reportWithResults())

	text := m.PlainText()
	if strings.ContainsRune(text, '\x1b') {
		t.Fatalf("plain text carries ANSI escapes: %q", text)
	}
	for _, want := range []string{
		"LSP Doctor — 2 servers · 1 failing",
		"go — gopls  (/usr/local/bin/gopls)",
		"binary: found on PATH",
		"toml — taplo",
		"diagnosis [binary missing]: the binary is nowhere on PATH",
		"fix: cargo install taplo-cli",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("plain text misses %q\n---\n%s", want, text)
		}
	}
	if got := strings.Count(text, "…"); got != 0 {
		t.Errorf("plain text is width-clipped (%d ellipses)\n---\n%s", got, text)
	}
	// Header line plus every rendered row.
	if got, want := strings.Count(strings.TrimSuffix(text, "\n"), "\n")+1, m.Rows()+1; got != want {
		t.Errorf("plain text has %d lines, want %d", got, want)
	}
}

func TestPlainTextEmptyWithoutRun(t *testing.T) {
	m := New(nil)
	if got := m.PlainText(); got != "" {
		t.Errorf("no report: PlainText = %q, want empty", got)
	}
	if cmd := m.CopyKeyCmd(); cmd != nil {
		t.Error("no report: CopyKeyCmd should be a no-op")
	}
	m.SetReport(NewReport())
	if cmd := m.CopyKeyCmd(); cmd != nil {
		t.Error("no finished run: CopyKeyCmd should be a no-op")
	}
}

func TestCopyKeyEmitsCopyMsg(t *testing.T) {
	m := New(nil)
	m.SetSize(80, 10)
	m.SetReport(reportWithResults())

	cmd := m.Update(tea.KeyPressMsg{Code: 'c', Text: "c"})
	if cmd == nil {
		t.Fatal("'c' produced no command")
	}
	msg, ok := cmd().(CopyMsg)
	if !ok {
		t.Fatalf("'c' produced %T, want CopyMsg", cmd())
	}
	if msg.What != "LSP Doctor report" {
		t.Errorf("What = %q", msg.What)
	}
	if msg.Text != m.PlainText() {
		t.Errorf("CopyMsg text differs from PlainText")
	}
}
