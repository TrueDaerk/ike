package app

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"ike/internal/agenttrace"
	"ike/internal/tracepanel"
)

// agenttrace_diff_test.go covers the trace's per-change diff view (#2859):
// D on a change opens the reconstructed diff with its provenance, esc
// closes it, and 1 / 2 / 3 switch the base between the session's before,
// git HEAD (in a temp repository) and the working file — HEAD disabled with
// a reason for an untracked file and outside a repository.

// runTraceDiff feeds a DiffMsg through the model and runs the off-loop
// reconstruction it starts.
func runTraceDiff(t *testing.T, m Model, msg tracepanel.DiffMsg) Model {
	t.Helper()
	out, cmd := m.Update(msg)
	m = out.(Model)
	if cmd == nil {
		t.Fatal("D started no reconstruction")
	}
	out, _ = m.Update(cmd())
	return out.(Model)
}

// diffTranscript is a session that reads repo/main.go whole, edits it
// without a structured result, creates repo/new.go and edits plain/other.go
// (outside any repository) with only a patch recorded.
func diffTranscript(t *testing.T, id, cwd, main, created, other string) string {
	t.Helper()
	var sb strings.Builder
	n := 0
	add := func(typ string, msg map[string]any, result any) {
		n++
		l := map[string]any{
			"isSidechain": false, "cwd": cwd, "sessionId": id, "type": typ, "message": msg,
			"uuid": "x" + string(rune('a'+n)), "timestamp": "2026-09-30T14:00:0" + string(rune('0'+n)) + ".000Z",
		}
		if result != nil {
			l["toolUseResult"] = result
		}
		b, err := json.Marshal(l)
		if err != nil {
			t.Fatal(err)
		}
		sb.Write(b)
		sb.WriteByte('\n')
	}
	call := func(id, name string, input map[string]any) {
		add("assistant", map[string]any{"role": "assistant", "content": []any{map[string]any{"type": "tool_use", "id": id, "name": name, "input": input}}}, nil)
	}
	result := func(id, text string, structured any) {
		add("user", map[string]any{"role": "user", "content": []any{map[string]any{"type": "tool_result", "tool_use_id": id, "content": text}}}, structured)
	}
	add("user", map[string]any{"role": "user", "content": "Change the files"}, nil)
	call("t_read", "Read", map[string]any{"file_path": main})
	result("t_read", "     1\ta\n     2\tb\n     3\tc\n", map[string]any{"type": "text", "file": map[string]any{"filePath": main, "startLine": 1, "numLines": 3, "totalLines": 3}})
	call("t_edit", "Edit", map[string]any{"file_path": main, "old_string": "c", "new_string": "d"})
	result("t_edit", "ok", nil)
	call("t_write", "Write", map[string]any{"file_path": created, "content": "x\n"})
	result("t_write", "created", map[string]any{"type": "create", "filePath": created, "content": "x\n", "structuredPatch": []any{}})
	call("t_other", "Edit", map[string]any{"file_path": other, "old_string": "1", "new_string": "2"})
	result("t_other", "ok", map[string]any{"filePath": other, "structuredPatch": []any{map[string]any{"oldStart": 4, "oldLines": 1, "newStart": 4, "newLines": 1, "lines": []any{"-1", "+2"}}}})
	return sb.String()
}

func TestTraceDiffViewProvenanceAndBases(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	m, dir := traceApp(t)
	if m.onboardingOpen() {
		m = m.closeOnboarding().(Model)
	}
	repo := t.TempDir()
	git := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", append([]string{"-C", repo}, args...)...)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	git("init", "-b", "main")
	main := filepath.Join(repo, "main.go")
	if err := os.WriteFile(main, []byte("a\nb\nc\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	git("add", "main.go")
	git("-c", "user.email=t@t", "-c", "user.name=t", "commit", "-m", "init")
	// The agent's edit is on disk; HEAD still has the old line.
	if err := os.WriteFile(main, []byte("a\nb\nd\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	created := filepath.Join(repo, "new.go")
	if err := os.WriteFile(created, []byte("x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	other := filepath.Join(t.TempDir(), "other.go")
	if err := os.WriteFile(filepath.Join(dir, "sess-1.jsonl"), []byte(diffTranscript(t, "sess-1", projectRoot(), main, created, other)), 0o644); err != nil {
		t.Fatal(err)
	}
	m = openTrace(t, m)
	p := m.agentTracePanel()
	if !p.HasSession() {
		t.Fatalf("no session:\n%s", p.View())
	}

	// The tree row shows the reconstructed counts.
	if !p.Select("e2/f0") || p.Current().Detail != "+1 −1" {
		t.Fatalf("edit row = %+v", p.Current())
	}
	msg, ok := p.Update(tea.KeyPressMsg{Code: 'D', Text: "D"})().(tracepanel.DiffMsg)
	if !ok || msg.Key != "e2/f0" || msg.Linked != "" {
		t.Fatalf("D = %#v", msg)
	}
	m = runTraceDiff(t, m, msg)
	if !m.traceDiffOpen() {
		t.Fatal("D must open the diff view")
	}
	view := ansi.Strip(m.shell.Content().Render(100))
	for _, want := range []string{"edit (Edit) · turn #1 · +1 −1", "from old/new string", "- c", "+ d", "esc close"} {
		if !strings.Contains(view, want) {
			t.Errorf("session view lacks %q:\n%s", want, view)
		}
	}
	// HEAD still holds the old line: the change is not in it.
	out, _ := m.Update(tea.KeyPressMsg{Code: '2', Text: "2"})
	m = out.(Model)
	if m.traceDiff.base != baseHead {
		t.Fatalf("2 must switch to HEAD: %q", notices(m))
	}
	if view := ansi.Strip(m.shell.Content().Render(100)); !strings.Contains(view, "- c") || !strings.Contains(view, "+ d") {
		t.Errorf("HEAD view:\n%s", view)
	}
	// The working file is exactly the change's result.
	out, _ = m.Update(tea.KeyPressMsg{Code: '3', Text: "3"})
	m = out.(Model)
	if view := ansi.Strip(m.shell.Content().Render(100)); m.traceDiff.base != baseWork || !strings.Contains(view, "working file holds exactly this change's result") {
		t.Errorf("working view:\n%s", view)
	}
	out, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	m = out.(Model)
	if m.traceDiffOpen() || m.shell.IsOpen() {
		t.Fatal("esc must close the diff view")
	}

	// A created file HEAD does not track: HEAD is disabled with the reason.
	p = m.agentTracePanel()
	p.Select("e3/f0")
	m = runTraceDiff(t, m, p.Update(tea.KeyPressMsg{Code: 'D', Text: "D"})().(tracepanel.DiffMsg))
	view = ansi.Strip(m.shell.Content().Render(100))
	if !strings.Contains(view, "new file") || !strings.Contains(view, "git HEAD: not tracked in HEAD") || !strings.Contains(view, "+ x") {
		t.Errorf("create view:\n%s", view)
	}
	out, _ = m.Update(tea.KeyPressMsg{Code: '2', Text: "2"})
	m = out.(Model)
	if m.traceDiff.base != baseSession || !strings.Contains(strings.Join(notices(m), "\n"), "git HEAD: not tracked in HEAD") {
		t.Fatalf("a disabled base must not be picked: base=%d notices=%q", m.traceDiff.base, notices(m))
	}
	out, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	m = out.(Model)

	// Outside a repository, with only the patch recorded: HEAD says so, the
	// hunk renders through the markdown fallback.
	p = m.agentTracePanel()
	p.Select("e4/f0")
	m = runTraceDiff(t, m, p.Update(tea.KeyPressMsg{Code: 'D', Text: "D"})().(tracepanel.DiffMsg))
	view = ansi.Strip(m.shell.Content().Render(100))
	for _, want := range []string{"from structuredPatch", "git HEAD: not a git repository", "@@ -4,1 +4,1 @@", "-1", "+2"} {
		if !strings.Contains(view, want) {
			t.Errorf("patch-only view lacks %q:\n%s", want, view)
		}
	}
	out, _ = m.Update(tea.KeyPressMsg{Code: '3', Text: "3"})
	m = out.(Model)
	if m.traceDiff.base != baseSession {
		t.Fatal("the working-file base needs the whole after content")
	}
}

// TestTraceAskFallsBackToReconstructedHunk: without a feed link the ask
// context carries the transcript's hunk (#2859).
func TestTraceAskFallsBackToReconstructedHunk(t *testing.T) {
	m, dir := traceApp(t)
	target := filepath.Join(t.TempDir(), "main.go")
	if err := os.WriteFile(filepath.Join(dir, "sess-1.jsonl"), []byte(transcriptLines("sess-1", projectRoot(), target, 1)), 0o644); err != nil {
		t.Fatal(err)
	}
	m = openTrace(t, m)
	sess, err := agenttrace.Load(filepath.Join(dir, "sess-1.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	node := &agenttrace.Node{Key: "e2/f0"}
	if m.traceAskHunk(node) != "" {
		t.Fatal("no feed link: the on-loop hunk is empty")
	}
	if h := askSessionHunk(sess, node, ""); !strings.Contains(h, "-c\n+d") {
		t.Fatalf("reconstructed hunk = %q", h)
	}
	if h := askSessionHunk(sess, node, "feed"); h != "feed" {
		t.Fatalf("the feed's hunk must win, got %q", h)
	}
}
