package app

import (
	"context"
	"errors"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"ike/internal/format"
	ilsp "ike/internal/lsp"
)

// reformatApp opens content in an app with a channel-backed host sender, so
// the async provider results (FormatEditsMsg, status toasts) are observable.
func reformatApp(t *testing.T, name, content string) (Model, string, chan tea.Msg) {
	t.Helper()
	m := sized(t, 100, 40)
	file := writeTemp(t, t.TempDir(), name, content)
	tm, _ := m.openPath(file, false)
	m = tm.(Model)
	msgs := make(chan tea.Msg, 16)
	m.host.SetSender(func(msg tea.Msg) { msgs <- msg })
	format.ResetForTest()
	t.Cleanup(format.ResetForTest)
	return m, file, msgs
}

// awaitMsg pulls host messages until one matches (or fails after a timeout).
func awaitMsg(t *testing.T, msgs chan tea.Msg, match func(tea.Msg) bool) tea.Msg {
	t.Helper()
	deadline := time.After(5 * time.Second)
	for {
		select {
		case msg := <-msgs:
			if match(msg) {
				return msg
			}
		case <-deadline:
			t.Fatal("timed out waiting for host message")
			return nil
		}
	}
}

// TestReformatFileRunsProviderAndApplies: the Reformat File request resolves
// the registry provider, applies its text result to the buffer as edits, and
// names the source in a status toast (#1401).
func TestReformatFileRunsProviderAndApplies(t *testing.T) {
	m, _, msgs := reformatApp(t, "notes.sql", "select 1\n")
	format.Register(format.Provider{
		Name: "built-in", Tier: format.TierBuiltin, Languages: nil,
		Format: func(ctx context.Context, req format.Request) (format.Result, error) {
			return format.TextResult("SELECT 1\n"), nil
		},
	})

	out, cmd := m.Update(format.FileRequestMsg{})
	m = out.(Model)
	if cmd == nil {
		t.Fatal("a resolvable provider must yield a command")
	}
	_ = cmd()

	edits := awaitMsg(t, msgs, func(msg tea.Msg) bool { _, ok := msg.(ilsp.FormatEditsMsg); return ok }).(ilsp.FormatEditsMsg)
	out, _ = m.Update(edits)
	m = out.(Model)
	ed := m.activeWS().Panes.Get(m.activeEditorKey()).Editor()
	if got := ed.Text(); got != "SELECT 1\n" {
		t.Fatalf("provider text must land in the buffer, got %q", got)
	}
	status := awaitMsg(t, msgs, func(msg tea.Msg) bool { _, ok := msg.(ilsp.ServerStatusMsg); return ok }).(ilsp.ServerStatusMsg)
	if status.Text != "reformat: built-in" {
		t.Fatalf("status must name the provider, got %q", status.Text)
	}
}

// TestReformatProviderErrorLeavesBufferUntouched: a failing provider reports
// an error toast and changes nothing (#1401 — failure is never destructive).
func TestReformatProviderErrorLeavesBufferUntouched(t *testing.T) {
	m, _, msgs := reformatApp(t, "notes.sql", "select 1\n")
	format.Register(format.Provider{
		Name: "broken", Tier: format.TierBuiltin,
		Format: func(ctx context.Context, req format.Request) (format.Result, error) {
			return format.Result{}, errors.New("boom")
		},
	})

	_, cmd := m.Update(format.FileRequestMsg{})
	if cmd == nil {
		t.Fatal("provider resolves, command expected")
	}
	_ = cmd()

	status := awaitMsg(t, msgs, func(msg tea.Msg) bool { _, ok := msg.(ilsp.ServerStatusMsg); return ok }).(ilsp.ServerStatusMsg)
	if status.Kind != ilsp.ServerEventError {
		t.Fatalf("failure must surface as an error toast, got %#v", status)
	}
	ed := m.activeWS().Panes.Get(m.activeEditorKey()).Editor()
	if got := ed.Text(); got != "select 1" && got != "select 1\n" {
		t.Fatalf("buffer must stay untouched on error, got %q", got)
	}
	select {
	case msg := <-msgs:
		if _, ok := msg.(ilsp.FormatEditsMsg); ok {
			t.Fatal("no edits may be delivered on error")
		}
	case <-time.After(100 * time.Millisecond):
	}
}

// TestReformatNoProviderIsClearMessage: without any provider the command is a
// warn toast, not a silent no-op (#1401).
func TestReformatNoProviderIsClearMessage(t *testing.T) {
	m, _, _ := reformatApp(t, "notes.sql", "select 1\n")

	out, _ := m.Update(format.FileRequestMsg{})
	m = out.(Model)
	found := false
	for _, n := range m.toasts {
		if n.text == "no formatter for this file type" {
			found = true
		}
	}
	if !found {
		t.Fatal(`expected the "no formatter for this file type" notification`)
	}
}

// TestReformatSelectionWholeFileOnly: providers exist but none does ranges —
// the user is told only whole-file reformat is available instead of the whole
// file being formatted silently (#1401).
func TestReformatSelectionWholeFileOnly(t *testing.T) {
	m, _, _ := reformatApp(t, "notes.sql", "select 1\nselect 2\n")
	called := false
	format.Register(format.Provider{
		Name: "built-in", Tier: format.TierBuiltin,
		Format: func(ctx context.Context, req format.Request) (format.Result, error) {
			called = true
			return format.TextResult("SELECT"), nil
		},
	})
	// Enter visual mode so a selection exists.
	m = drainKey(m, tea.KeyPressMsg{Code: 'v', Text: "v"})

	out, _ := m.Update(format.RangeRequestMsg{})
	m = out.(Model)
	if called {
		t.Fatal("whole-file-only must not run any provider")
	}
	found := false
	for _, n := range m.toasts {
		if n.text == "no range formatter for this file type — only Reformat File is available" {
			found = true
		}
	}
	if !found {
		t.Fatal("expected the whole-file-only notification")
	}
}

// TestReformatSelectionRunsRangeProvider: an active selection reaches the
// range-capable provider with the normalized span.
func TestReformatSelectionRunsRangeProvider(t *testing.T) {
	m, file, msgs := reformatApp(t, "notes.sql", "select 1\nselect 2\n")
	var gotStart, gotEnd format.Pos
	format.Register(format.Provider{
		Name: "built-in", Tier: format.TierBuiltin,
		Format: func(ctx context.Context, req format.Request) (format.Result, error) {
			return format.Result{}, nil
		},
		FormatRange: func(ctx context.Context, req format.Request, start, end format.Pos) (format.Result, error) {
			gotStart, gotEnd = start, end
			return format.Result{Edits: []format.Edit{{StartLine: 0, StartCol: 0, EndLine: 0, EndCol: 6, Text: "SELECT"}}}, nil
		},
	})
	m = drainKey(m, tea.KeyPressMsg{Code: 'V', Text: "V"}) // line-wise selection on line 0

	_, cmd := m.Update(format.RangeRequestMsg{})
	if cmd == nil {
		t.Fatal("range provider must run")
	}
	_ = cmd()

	edits := awaitMsg(t, msgs, func(msg tea.Msg) bool { _, ok := msg.(ilsp.FormatEditsMsg); return ok }).(ilsp.FormatEditsMsg)
	if edits.Path != file || len(edits.Edits) != 1 || edits.Edits[0].Text != "SELECT" {
		t.Fatalf("unexpected edits %#v", edits)
	}
	if gotStart != (format.Pos{Line: 0, Col: 0}) || gotEnd != (format.Pos{Line: 1, Col: 0}) {
		t.Fatalf("line-wise selection must normalize to [0:0, 1:0), got %v..%v", gotStart, gotEnd)
	}
}

// runCmd executes a command, unpacking nested batches — a toast alongside
// the reformat run wraps them into one tea.BatchMsg.
func runCmd(c tea.Cmd) {
	if c == nil {
		return
	}
	if batch, ok := c().(tea.BatchMsg); ok {
		for _, inner := range batch {
			runCmd(inner)
		}
	}
}

// TestReformatFileWithSelectionFormatsRange: the plain reformat command is
// context-sensitive (#1603) — an active visual selection routes to the
// range-capable provider instead of the whole file.
func TestReformatFileWithSelectionFormatsRange(t *testing.T) {
	m, _, msgs := reformatApp(t, "notes.sql", "select 1\nselect 2\n")
	wholeFile := false
	var gotStart, gotEnd format.Pos
	format.Register(format.Provider{
		Name: "built-in", Tier: format.TierBuiltin,
		Format: func(ctx context.Context, req format.Request) (format.Result, error) {
			wholeFile = true
			return format.Result{}, nil
		},
		FormatRange: func(ctx context.Context, req format.Request, start, end format.Pos) (format.Result, error) {
			gotStart, gotEnd = start, end
			return format.Result{Edits: []format.Edit{{StartLine: 0, StartCol: 0, EndLine: 0, EndCol: 6, Text: "SELECT"}}}, nil
		},
	})
	m = drainKey(m, tea.KeyPressMsg{Code: 'V', Text: "V"}) // line-wise selection on line 0

	_, cmd := m.Update(format.FileRequestMsg{})
	if cmd == nil {
		t.Fatal("range provider must run")
	}
	_ = cmd()

	awaitMsg(t, msgs, func(msg tea.Msg) bool { _, ok := msg.(ilsp.FormatEditsMsg); return ok })
	if wholeFile {
		t.Fatal("an active selection must not format the whole file")
	}
	if gotStart != (format.Pos{Line: 0, Col: 0}) || gotEnd != (format.Pos{Line: 1, Col: 0}) {
		t.Fatalf("selection span must reach the provider, got %v..%v", gotStart, gotEnd)
	}
}

// TestReformatFileWithSelectionFallsBackToWholeFile: selection active but no
// range-capable provider — the context-sensitive command widens to the whole
// file with a notice instead of doing nothing (#1603).
func TestReformatFileWithSelectionFallsBackToWholeFile(t *testing.T) {
	m, _, msgs := reformatApp(t, "notes.sql", "select 1\nselect 2\n")
	called := false
	format.Register(format.Provider{
		Name: "built-in", Tier: format.TierBuiltin,
		Format: func(ctx context.Context, req format.Request) (format.Result, error) {
			called = true
			return format.TextResult("SELECT 1\nSELECT 2\n"), nil
		},
	})
	m = drainKey(m, tea.KeyPressMsg{Code: 'v', Text: "v"})

	out, cmd := m.Update(format.FileRequestMsg{})
	m = out.(Model)
	if cmd == nil {
		t.Fatal("fallback must run the whole-file provider")
	}
	runCmd(cmd)
	awaitMsg(t, msgs, func(msg tea.Msg) bool { _, ok := msg.(ilsp.FormatEditsMsg); return ok })
	if !called {
		t.Fatal("whole-file provider must run as the fallback")
	}
	found := false
	for _, n := range m.toasts {
		if n.text == "no range formatter for this file type — reformatting the whole file" {
			found = true
		}
	}
	if !found {
		t.Fatal("the widened scope must be announced")
	}
}

// TestReformatWithoutSelectionExplains: reformat-selection without a visual
// selection tells the user what to do.
func TestReformatWithoutSelectionExplains(t *testing.T) {
	m, _, _ := reformatApp(t, "notes.sql", "select 1\n")
	format.Register(format.Provider{
		Name: "built-in", Tier: format.TierBuiltin,
		Format: func(ctx context.Context, req format.Request) (format.Result, error) {
			return format.Result{}, nil
		},
	})

	out, _ := m.Update(format.RangeRequestMsg{})
	m = out.(Model)
	found := false
	for _, n := range m.toasts {
		if n.text == "select a range first (visual mode), or use Reformat File" {
			found = true
		}
	}
	if !found {
		t.Fatal("expected the select-a-range hint")
	}
}
