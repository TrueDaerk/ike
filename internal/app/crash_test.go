package app

import (
	"os"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"ike/internal/crashlog"
	"ike/internal/host"
	"ike/internal/safego"
)

// crash_test.go covers the crash guards of #2836: a panic inside Update or
// View or a Cmd leaves a crash report and a notification and the session
// keeps running; a guarded goroutine's panic reaches the UI the same way;
// the next launch announces an unacknowledged report once, with the open
// command as its action.

// crashDir points the crash writer at a fresh directory for one test.
func crashDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	crashlog.SetDir(dir)
	crashlog.ResetForTest()
	t.Cleanup(func() { crashlog.SetDir(""); crashlog.ResetForTest() })
	return dir
}

// crashNotifications drains the host and returns the notifications whose
// text contains want.
func crashNotifications(m Model, want string) []host.Notification {
	var out []host.Notification
	for _, n := range m.host.DrainNotifications() {
		if strings.Contains(n.Text, want) {
			out = append(out, n)
		}
	}
	return out
}

// TestUpdatePanicWritesCrashLogAndKeepsRunning is the acceptance case: the
// injected panic produces a report with the version, the panic value, the
// goroutine dumps, the key context, the focused pane and the recent telemetry
// events; the model survives and the next message is handled normally.
func TestUpdatePanicWritesCrashLogAndKeepsRunning(t *testing.T) {
	dir := crashDir(t)
	m := dismissOnboarding(newSized())
	m.host.DrainNotifications()
	m.usage.Key("cmd+shift+f", "editor", "project.findInPath", "resolved")

	tm, cmd := m.Update(crashPanicMsg{value: "boom in update"})
	mm, ok := tm.(Model)
	if !ok {
		t.Fatalf("Update after a panic returned %T, want Model", tm)
	}
	logs := crashlog.List(dir)
	if len(logs) != 1 {
		t.Fatalf("crash logs = %v, want one", logs)
	}
	body, err := os.ReadFile(logs[0])
	if err != nil {
		t.Fatal(err)
	}
	s := string(body)
	for _, want := range []string{
		"version:  ", "where:    update", "message:  app.crashPanicMsg", "panic:    boom in update",
		"context:  ", "pane:     ", "chord=cmd+shift+f", "command=project.findInPath",
		"--- panicking goroutine ---", "TestUpdatePanicWritesCrashLogAndKeepsRunning",
		"--- all goroutines ---",
	} {
		if !strings.Contains(s, want) {
			t.Errorf("crash log lacks %q:\n%s", want, s)
		}
	}
	notes := crashNotifications(mm, "recovered from a crash in update")
	if len(notes) != 1 || notes[0].Severity != host.Error || !strings.Contains(notes[0].Text, logs[0]) {
		t.Fatalf("notification = %+v, want one error naming %s", notes, logs[0])
	}
	if len(notes[0].Actions) != 1 || notes[0].Actions[0].Command != "crash.openLastLog" {
		t.Fatalf("notification actions = %+v, want crash.openLastLog", notes[0].Actions)
	}
	// The follow-up command runs a settled pass; the session goes on.
	mm = drainCmd(mm, cmd)
	tm, _ = mm.Update(OpenFindInPathMsg{})
	mm = tm.(Model)
	if !mm.finder.IsOpen() {
		t.Fatal("the IDE must keep working after a recovered panic")
	}
	_ = mm.View()
}

// TestCmdPanicIsCaught: a panicking Cmd — alone or as a batch member —
// resolves to a crashRecoveredMsg with its report written, and the message
// raises the toast when it lands.
func TestCmdPanicIsCaught(t *testing.T) {
	dir := crashDir(t)
	m := dismissOnboarding(newSized())
	m.host.DrainNotifications()

	msg := guardCmd(func() tea.Msg { panic("cmd boom") })()
	rec, ok := msg.(crashRecoveredMsg)
	if !ok || rec.where != "cmd" || rec.path == "" || !rec.notify {
		t.Fatalf("guarded cmd returned %#v", msg)
	}
	if got := crashlog.List(dir); len(got) != 1 || got[0] != rec.path {
		t.Fatalf("crash logs = %v, want %s", got, rec.path)
	}
	tm, _ := m.Update(msg)
	m = tm.(Model)
	// The pass that carries the message drains the toast into the model.
	if texts := toastTexts(m); len(texts) != 1 || !strings.Contains(texts[0], "recovered from a crash in cmd") || !strings.Contains(texts[0], rec.path) {
		t.Fatalf("toasts = %v, want the cmd crash naming %s", texts, rec.path)
	}

	batch := guardCmd(tea.Batch(
		func() tea.Msg { return OpenFindInPathMsg{} },
		func() tea.Msg { panic("member boom") },
	))()
	members, ok := batch.(tea.BatchMsg)
	if !ok || len(members) != 2 {
		t.Fatalf("guarded batch resolved to %#v", batch)
	}
	var caught, plain int
	for _, c := range members {
		switch c().(type) {
		case crashRecoveredMsg:
			caught++
		case OpenFindInPathMsg:
			plain++
		}
	}
	if caught != 1 || plain != 1 {
		t.Fatalf("batch members: caught=%d plain=%d", caught, plain)
	}
	if guardCmd(nil) != nil {
		t.Fatal("a nil Cmd stays nil")
	}
}

// TestViewPanicRendersFallback: a View panic writes the report and draws the
// fallback frame; the toast lands on the next pass and a fixed View renders
// again.
func TestViewPanicRendersFallback(t *testing.T) {
	dir := crashDir(t)
	m := dismissOnboarding(newSized())
	m.host.DrainNotifications()
	m.viewPanic = "view boom"
	v := m.View()
	if !strings.Contains(v.Content, "could not render") || !strings.Contains(v.Content, "view boom") {
		t.Fatalf("fallback frame = %q", v.Content)
	}
	logs := crashlog.List(dir)
	if len(logs) != 1 || !strings.Contains(v.Content, logs[0]) {
		t.Fatalf("crash logs = %v, frame names %q", logs, v.Content)
	}
	if notes := crashNotifications(m, "recovered from a crash in view"); len(notes) != 1 {
		t.Fatalf("notifications = %+v, want one", notes)
	}
	m.viewPanic = ""
	if v := m.View(); strings.Contains(v.Content, "could not render") {
		t.Fatal("a healthy View must render the real frame again")
	}
}

// TestGoroutinePanicNotifiesUI: a guarded goroutine's panic reaches the
// running model's host as an error notification naming the report — the
// process is still here to assert it.
func TestGoroutinePanicNotifiesUI(t *testing.T) {
	dir := crashDir(t)
	m := dismissOnboarding(newSized())
	tm, _ := m.Update(tea.WindowSizeMsg{Width: 100, Height: 30}) // publishes the live host
	m = tm.(Model)
	m.host.DrainNotifications()
	safego.Go("test.worker", func() { panic("worker boom") })
	deadline := time.Now().Add(5 * time.Second)
	var notes []host.Notification
	for time.Now().Before(deadline) && len(notes) == 0 {
		notes = crashNotifications(m, "background task test.worker failed")
		time.Sleep(10 * time.Millisecond)
	}
	if len(notes) != 1 || notes[0].Severity != host.Error {
		t.Fatalf("notifications = %+v, want one error", notes)
	}
	logs := crashlog.List(dir)
	if len(logs) != 1 || !strings.Contains(notes[0].Text, logs[0]) {
		t.Fatalf("crash logs = %v, notification %q", logs, notes[0].Text)
	}
	body, _ := os.ReadFile(logs[0])
	if !strings.Contains(string(body), "where:    goroutine:test.worker") {
		t.Errorf("report lacks the goroutine name:\n%s", body)
	}
}

// TestNoticeLastCrashOnceAndOpen: the next-launch notice fires once per
// unacknowledged report, and crash.openLastLog opens the newest report.
func TestNoticeLastCrashOnceAndOpen(t *testing.T) {
	crashDir(t)
	m := dismissOnboarding(newSized())
	m.host.DrainNotifications()
	if m = m.NoticeLastCrash(); len(crashNotifications(m, "crashed last time")) != 0 {
		t.Fatal("no crash log, no notice")
	}
	path, err := crashlog.Write(crashlog.Report{Where: "test", Value: "earlier"})
	if err != nil {
		t.Fatal(err)
	}
	m = m.NoticeLastCrash()
	notes := crashNotifications(m, "crashed last time")
	if len(notes) != 1 || notes[0].Severity != host.Warn || !strings.Contains(notes[0].Text, path) {
		t.Fatalf("notice = %+v, want one warning naming %s", notes, path)
	}
	if len(notes[0].Actions) != 1 || notes[0].Actions[0].Command != "crash.openLastLog" {
		t.Fatalf("notice actions = %+v", notes[0].Actions)
	}
	if m = m.NoticeLastCrash(); len(crashNotifications(m, "crashed last time")) != 0 {
		t.Fatal("an acknowledged crash log must not be announced twice")
	}
	tm, cmd := m.Update(OpenCrashLogMsg{})
	m = drainCmd(tm.(Model), cmd)
	if ed := m.activeEditor(); ed == nil || ed.Path() != canonicalPath(path) {
		t.Fatalf("crash.openLastLog must open %s, active editor = %v", path, ed)
	}
}

// TestCrashFocusPublished: the settled pass publishes the key context and the
// focused pane for reports written off the loop.
func TestCrashFocusPublished(t *testing.T) {
	crashDir(t)
	m := dismissOnboarding(newSized())
	path := tempFileWith(t, "a.py", "x = 1\n")
	m = openInEditor(m, path)
	f := crashlog.CurrentFocus()
	if f == nil || !strings.HasPrefix(f.KeyContext, "editor") || f.PaneKind != "editor" || f.PanePath != canonicalPath(path) {
		t.Fatalf("published focus = %+v", f)
	}
}
