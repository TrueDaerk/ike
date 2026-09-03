package app

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"ike/internal/dap"
	"ike/internal/explorer"
	"ike/internal/host"
	"ike/internal/registry"
)

// bpLinesFor returns the 1-based lines of the newest setBreakpoints request
// the stub saw for path; ok is false while none arrived yet.
func bpLinesFor(sa *stubAdapter, path string) ([]int, bool) {
	var out []int
	var found bool
	for _, raw := range sa.breakpointRequests() {
		var args struct {
			Source struct {
				Path string `json:"path"`
			} `json:"source"`
			Breakpoints []struct {
				Line int `json:"line"`
			} `json:"breakpoints"`
		}
		if json.Unmarshal(raw, &args) != nil || args.Source.Path != path {
			continue
		}
		out, found = nil, true
		for _, b := range args.Breakpoints {
			out = append(out, b.Line)
		}
	}
	return out, found
}

// waitForBPLines blocks until the stub's newest breakpoint list for path
// matches want (1-based lines).
func waitForBPLines(t *testing.T, sa *stubAdapter, path string, want []int) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	var last []int
	for time.Now().Before(deadline) {
		lines, ok := bpLinesFor(sa, path)
		if ok {
			last = lines
			if sameInts(lines, want) {
				return
			}
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("breakpoint lines for %s = %v, want %v", path, last, want)
}

func sameInts(a, b []int) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// runToCursorModel is debugModel paused on frame line 2 (0-based 1), the
// starting point of every run-to-cursor test.
func runToCursorModel(t *testing.T) (Model, *stubAdapter, string) {
	t.Helper()
	m, sa, path := debugModel(t)
	// The first-start LSP dialog owns the keyboard ahead of every prompt
	// (#301), so scripted keys would never reach one; a no-op when it is not
	// up.
	if m.onboardingOpen() {
		m = m.closeOnboarding().(Model)
	}
	frames := []dap.StackFrame{{ID: 1, Name: "f", Source: dap.Source{Path: path}, Line: 2}}
	tm, _ := m.Update(debugStoppedMsg{threadID: 1, frames: frames})
	return tm.(Model), sa, path
}

// TestRunToCursorSetsTempBreakpointAndContinues covers the happy path: the
// cursor line reaches the adapter as an extra breakpoint, the debuggee
// resumes, and the stop that follows retires the temporary line again.
func TestRunToCursorSetsTempBreakpointAndContinues(t *testing.T) {
	m, sa, path := runToCursorModel(t)
	ed := m.editorForPath(canonicalPath(path))
	if ed == nil {
		t.Fatal("the paused file must be open")
	}
	// The stop left the cursor on the frame line; move it down to line 3
	// (0-based 2) so the run has somewhere to go.
	ed.SetCursor(2, 0)

	tm, _ := m.Update(DebugRunToCursorMsg{})
	m = tm.(Model)
	if m.dbg.tempBP == nil || m.dbg.tempBP.line != 2 {
		t.Fatalf("temporary breakpoint = %+v, want line 2", m.dbg.tempBP)
	}
	if m.dbg.paused {
		t.Fatal("run to cursor must leave the paused state")
	}
	waitForBPLines(t, sa, path, []int{3})
	waitForCommand(t, sa, "continue")
	// The temporary line never enters the persisted store.
	if m.bpts.Has(bpKey(path), 2) {
		t.Fatal("the temporary breakpoint must not be stored")
	}

	// Reaching it (any stop, really) retires it and re-pushes the real list.
	frames := []dap.StackFrame{{ID: 2, Name: "f", Source: dap.Source{Path: path}, Line: 3}}
	tm, _ = m.Update(debugStoppedMsg{threadID: 1, frames: frames})
	m = tm.(Model)
	if m.dbg.tempBP != nil {
		t.Fatalf("the stop must retire the temporary breakpoint, got %+v", m.dbg.tempBP)
	}
	waitForBPLines(t, sa, path, nil)
}

// TestRunToCursorKeepsExistingBreakpoint covers the overlap case: a user
// breakpoint on the target line means no temporary one is needed, and the
// cleanup must never remove the user's.
func TestRunToCursorKeepsExistingBreakpoint(t *testing.T) {
	m, sa, path := runToCursorModel(t)
	key := bpKey(path)
	m.bpts.Toggle(key, 2)
	ed := m.editorForPath(canonicalPath(path))
	ed.SetCursor(2, 0)

	tm, _ := m.Update(DebugRunToCursorMsg{})
	m = tm.(Model)
	if m.dbg.tempBP != nil {
		t.Fatalf("an existing breakpoint needs no temporary one, got %+v", m.dbg.tempBP)
	}
	waitForBPLines(t, sa, path, []int{3})
	waitForCommand(t, sa, "continue")

	frames := []dap.StackFrame{{ID: 2, Name: "f", Source: dap.Source{Path: path}, Line: 3}}
	tm, _ = m.Update(debugStoppedMsg{threadID: 1, frames: frames})
	m = tm.(Model)
	if !m.bpts.Has(key, 2) {
		t.Fatal("the user's breakpoint must survive the run to cursor")
	}
}

// TestRunToCursorStopBeforeHit covers the abandoned run: the session ends
// before the temporary breakpoint is reached, and nothing is left behind.
func TestRunToCursorStopBeforeHit(t *testing.T) {
	m, sa, path := runToCursorModel(t)
	ed := m.editorForPath(canonicalPath(path))
	ed.SetCursor(2, 0)
	tm, _ := m.Update(DebugRunToCursorMsg{})
	m = tm.(Model)
	if m.dbg.tempBP == nil {
		t.Fatal("run to cursor must install a temporary breakpoint")
	}
	waitForCommand(t, sa, "continue")

	tm, _ = m.Update(DebugStopMsg{})
	m = tm.(Model)
	if m.dbg != nil {
		t.Fatal("the stopped session must be gone")
	}
	if m.bpts.Has(bpKey(path), 2) {
		t.Fatal("a stopped session must leave no temporary breakpoint behind")
	}
}

// TestRunToCursorNeedsPausedSession keeps the refusals friendly: no session,
// or a running debuggee, changes nothing.
func TestRunToCursorNeedsPausedSession(t *testing.T) {
	m := NewWith(registry.New(), host.MapConfig{})
	tm, _ := m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	m = tm.(Model)
	if tm, _ = m.Update(DebugRunToCursorMsg{}); tm.(Model).dbg != nil {
		t.Fatal("run to cursor without a session must stay a no-op")
	}

	m2, sa, _ := debugModel(t)
	tm, _ = m2.Update(DebugRunToCursorMsg{}) // never paused
	m2 = tm.(Model)
	if m2.dbg.tempBP != nil {
		t.Fatal("run to cursor while running must not install a breakpoint")
	}
	for _, c := range sa.commands() {
		if c == "continue" {
			t.Fatal("run to cursor while running must not resume")
		}
	}
}

// TestRunToLinePromptRunsToTypedLine covers debug.runToLine: the prompt takes
// a 1-based line number and runs the same mechanism.
func TestRunToLinePromptRunsToTypedLine(t *testing.T) {
	m, sa, path := runToCursorModel(t)
	tm, _ := m.Update(DebugRunToLineMsg{})
	m = tm.(Model)
	if !m.runToLinePromptOpen() {
		t.Fatal("debug.runToLine must open its prompt")
	}
	for _, r := range "4" {
		tm, _ = m.Update(tea.KeyPressMsg{Code: r, Text: string(r)})
		m = tm.(Model)
	}
	if m.runToLineInput != "4" {
		t.Fatalf("input = %q", m.runToLineInput)
	}
	tm, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	m = tm.(Model)
	if m.runToLinePromptOpen() {
		t.Fatal("enter must close the prompt")
	}
	if m.dbg.tempBP == nil || m.dbg.tempBP.line != 3 {
		t.Fatalf("temporary breakpoint = %+v, want line 3 (0-based)", m.dbg.tempBP)
	}
	waitForBPLines(t, sa, path, []int{4})
	waitForCommand(t, sa, "continue")
}

// TestRunToLinePromptRejectsGarbage keeps a mistyped line harmless.
func TestRunToLinePromptRejectsGarbage(t *testing.T) {
	m, _, _ := runToCursorModel(t)
	tm, _ := m.Update(DebugRunToLineMsg{})
	m = tm.(Model)
	for _, r := range "xy" {
		tm, _ = m.Update(tea.KeyPressMsg{Code: r, Text: string(r)})
		m = tm.(Model)
	}
	tm, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	m = tm.(Model)
	if m.dbg.tempBP != nil {
		t.Fatalf("a non-numeric line must not run, got %+v", m.dbg.tempBP)
	}
	if !m.dbg.paused {
		t.Fatal("a rejected line must leave the session paused")
	}
}

// TestSessionSpecsMergesTemporaryBreakpoint pins the merge itself: the
// temporary line joins the file's enabled specs in line order, and only its
// own file's.
func TestSessionSpecsMergesTemporaryBreakpoint(t *testing.T) {
	m, _, path := runToCursorModel(t)
	key := bpKey(path)
	m.bpts.Toggle(key, 0)
	m.dbg.tempBP = &tempBreakpoint{key: key, abs: path, line: 2}
	specs := m.sessionSpecs(key)
	if len(specs) != 2 || specs[0].Line != 0 || specs[1].Line != 2 {
		t.Fatalf("merged specs = %+v, want lines 0 and 2", specs)
	}
	if other := m.sessionSpecs("elsewhere.go"); len(other) != 0 {
		t.Fatalf("another file must not see the temporary breakpoint: %+v", other)
	}
}

// TestInlineValuesRenderForPHPFrame is the #2405 acceptance case for the
// inline values: an xdebug frame's locals arrive sigil-first ("$name") and
// must annotate the frame's file, focused on the frame's own line.
func TestInlineValuesRenderForPHPFrame(t *testing.T) {
	m, _, _ := runToCursorModel(t)
	php := filepath.Join(t.TempDir(), "prog.php")
	if err := os.WriteFile(php, []byte("<?php\n$s = \"hi\";\necho $s;\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	tm, _ := m.Update(explorer.OpenFileMsg{Path: php})
	m = tm.(Model)

	tm, _ = m.Update(debugLocalsMsg{
		sess: m.dbg.sess,
		path: php,
		line: 1,
		vars: []dap.Variable{{Name: "$s", Value: "\"hi\""}},
	})
	m = tm.(Model)
	ed := m.editorForPath(canonicalPath(php))
	if ed == nil {
		t.Fatal("the PHP file must be open")
	}
	if got := ed.DebugValueAt(1); got != "$s = \"hi\"" {
		t.Fatalf("PHP inline value on the frame line = %q", got)
	}

	// The resume symmetry: the annotations leave with the paused marker.
	m.clearPausedMarker()
	if got := ed.DebugValueAt(1); got != "" {
		t.Fatalf("a resume must clear the inline values, got %q", got)
	}
}
