package app

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"ike/internal/host"
	"ike/internal/lang"
	ilsp "ike/internal/lsp"
	"ike/internal/registry"
	"ike/internal/watch"
)

// notify raises a notification and runs one Update pass so the root model
// drains it, returning the model and the drain's cmd (the expiry ticks).
func notify(m Model, sev host.Severity, text string) (Model, tea.Cmd) {
	m.host.Notify(sev, text)
	tm, cmd := m.Update(tea.WindowSizeMsg{Width: m.width, Height: m.height})
	return tm.(Model), cmd
}

func TestToastExpiry(t *testing.T) {
	m := newSized()
	m, cmd := notify(m, host.Info, "saved")
	if len(m.toasts) != 1 {
		t.Fatalf("toasts = %d want 1", len(m.toasts))
	}
	if cmd == nil {
		t.Fatal("info toast must schedule an expiry tick")
	}
	tm, _ := m.Update(toastExpireMsg{id: m.toasts[0].id})
	if m = tm.(Model); len(m.toasts) != 0 {
		t.Fatalf("toast should be gone after expiry, have %d", len(m.toasts))
	}
}

func TestErrorToastPersistsUntilEsc(t *testing.T) {
	m := newSized()
	m, _ = notify(m, host.Error, "server crashed")
	// A foreign expiry must not remove it, and no tick targets it anyway.
	tm, _ := m.Update(toastExpireMsg{id: -1})
	m = tm.(Model)
	if len(m.toasts) != 1 {
		t.Fatal("error toast must persist")
	}
	tm, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	if m = tm.(Model); len(m.toasts) != 0 {
		t.Fatal("Esc should dismiss error toasts")
	}
}

func TestToastStackingNewestOnTopCappedAtThree(t *testing.T) {
	m := newSized()
	for _, s := range []string{"one", "two", "three", "four"} {
		m, _ = notify(m, host.Info, s)
	}
	if len(m.toasts) != 4 {
		t.Fatalf("all toasts kept in the store, got %d", len(m.toasts))
	}
	if m.toasts[0].text != "four" {
		t.Fatalf("newest first, got %q", m.toasts[0].text)
	}
	frame := m.render()
	if !strings.Contains(frame, "four") || !strings.Contains(frame, "two") {
		t.Fatal("visible toasts missing from the frame")
	}
	if strings.Contains(frame, "● one ") {
		t.Fatal("fourth-oldest toast must not render (cap 3)")
	}
}

// TestServerStatusRouting guards the 0130 call-site migration: persistent
// server state is tracked per language (#380), transient events become toasts
// of the matching severity.
func TestServerStatusRouting(t *testing.T) {
	m := newSized()
	tm, _ := m.Update(ilsp.ServerStatusMsg{Lang: "go", Text: "go language server ready", Kind: ilsp.ServerState})
	m = tm.(Model)
	if got := m.lspStatus["go"]; got != "go language server ready" {
		t.Fatalf("persistent state should be tracked for its language, got %q", got)
	}
	if got := m.host.Status(); got != "" {
		t.Fatalf("per-language state must not write the global host status (#380), got %q", got)
	}
	if len(m.toasts) != 0 {
		t.Fatalf("persistent state must not toast, have %d toasts", len(m.toasts))
	}

	events := []struct {
		kind ilsp.ServerStatusKind
		sev  host.Severity
	}{
		{ilsp.ServerEventInfo, host.Info},
		{ilsp.ServerEventWarn, host.Warn},
		{ilsp.ServerEventError, host.Error},
	}
	for _, ev := range events {
		tm, _ = m.Update(ilsp.ServerStatusMsg{Text: "event", Kind: ev.kind})
		m = tm.(Model)
		if len(m.toasts) == 0 || m.toasts[0].sev != ev.sev {
			t.Fatalf("kind %d should raise a severity-%d toast", ev.kind, ev.sev)
		}
	}
	if got := m.lspStatus["go"]; got != "go language server ready" {
		t.Fatalf("events must not overwrite the persistent per-language state, got %q", got)
	}
}

// TestStatusLineScopesServerSegmentToBuffer guards #380: the server segment
// follows the focused buffer's language — a plain-text buffer shows no server
// text, a buffer of the reporting language shows its current state.
func TestStatusLineScopesServerSegmentToBuffer(t *testing.T) {
	lang.Register(lang.Language{ID: "statest", Extensions: []string{"stt"}, Server: &lang.ServerSpec{Language: "statest", Command: "x"}})
	m := newSized()
	tm, _ := m.Update(ilsp.ServerStatusMsg{Lang: "statest", Text: "statest language server ready", Kind: ilsp.ServerState})
	m = tm.(Model)

	dir := t.TempDir()
	code := filepath.Join(dir, "main.stt")
	notes := filepath.Join(dir, "notes.txt")
	for _, p := range []string{code, notes} {
		if err := os.WriteFile(p, []byte("hello\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	// The temp path in the file segment is long; widen the bar so the #659
	// truncation guard cannot clip the segment under test.
	tm, _ = m.Update(tea.WindowSizeMsg{Width: 400, Height: 30})
	m = tm.(Model)

	tm, _ = m.openPath(code, false)
	m = tm.(Model)
	if line := m.statusLine(); !strings.Contains(line, "statest language server ready") {
		t.Fatalf("focused buffer's language state missing from the status line: %q", line)
	}

	tm, _ = m.openPath(notes, false)
	m = tm.(Model)
	if line := m.statusLine(); strings.Contains(line, "language server") {
		t.Fatalf("non-LSP buffer must show no server text (#380): %q", line)
	}
}

// TestStatusLineKeepsSegmentsWithHostStatus guards the 0130 defect fix: a
// persistent host status renders as one more segment, never replacing the
// mode/cursor segments.
func TestStatusLineKeepsSegmentsWithHostStatus(t *testing.T) {
	m := newSized()
	m.setFocus(m.activeEditorKey()) // mode/cursor segments render for a focused editor (#381)
	m.host.SetStatus("go language server ready")
	line := m.statusLine()
	if !strings.Contains(line, "NORMAL") || !strings.Contains(line, "Ln 1") {
		t.Fatalf("host status must not cover the mode/cursor segments: %q", line)
	}
	if !strings.Contains(line, "go language server ready") {
		t.Fatalf("persistent host status missing from the status line: %q", line)
	}
}

// TestSaveAllNotifiesSavedCount guards editor.saveAll feedback: SaveAllMsg
// reports 'saved N files' as an info toast, and hints 'nothing to save' when
// no buffer is dirty (0082 review, #275) — a silent chord reads as dead.
func TestSaveAllNotifiesSavedCount(t *testing.T) {
	m := newSized()
	tm, _ := m.Update(SaveAllMsg{})
	if m = tm.(Model); len(m.toasts) != 1 || m.toasts[0].text != "nothing to save" {
		t.Fatalf("save-all with no dirty editors should hint 'nothing to save', toasts=%+v", m.toasts)
	}
	tm, _ = m.Update(toastExpireMsg{id: m.toasts[0].id})
	m = tm.(Model)

	dir := t.TempDir()
	path := filepath.Join(dir, "a.txt")
	if err := os.WriteFile(path, []byte("one\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	tm, _ = m.openPath(path, false)
	m = tm.(Model)
	for _, k := range []tea.KeyPressMsg{
		{Code: 'i', Text: "i"},
		{Code: 'X', Text: "X"},
		{Code: tea.KeyEscape},
	} {
		m = drainKey(m, k)
	}
	tm, _ = m.Update(SaveAllMsg{})
	m = tm.(Model)
	if len(m.toasts) != 1 || m.toasts[0].text != "saved 1 file" || m.toasts[0].sev != host.Info {
		t.Fatalf("expected a 'saved 1 file' info toast, toasts=%+v", m.toasts)
	}
}

// TestThemeSelectNotifies guards the theme migration: selecting a theme
// confirms via toast (warn on unknown names) and leaves the persistent status
// segment untouched.
func TestThemeSelectNotifies(t *testing.T) {
	m := newSized()
	tm, _ := m.Update(SelectThemeMsg{Name: "no-such-theme"})
	m = tm.(Model)
	if len(m.toasts) != 1 || m.toasts[0].sev != host.Warn {
		t.Fatalf("unknown theme should raise a warn toast, toasts=%+v", m.toasts)
	}
	if m.host.Status() != "" {
		t.Fatalf("theme selection must not write the status line, got %q", m.host.Status())
	}
}

// TestHistoryCapturesNewestFirstAndCaps guards the #78 ring: every
// notification is recorded newest-first with its severity, capped at
// historyCap entries.
func TestHistoryCapturesNewestFirstAndCaps(t *testing.T) {
	m := newSized()
	for i := 0; i < historyCap+5; i++ {
		m, _ = notify(m, host.Info, "n"+strconv.Itoa(i))
	}
	if len(m.history) != historyCap {
		t.Fatalf("history = %d entries, want cap %d", len(m.history), historyCap)
	}
	if m.history[0].text != "n"+strconv.Itoa(historyCap+4) {
		t.Fatalf("newest first, got %q", m.history[0].text)
	}
	if m.history[0].at.IsZero() {
		t.Fatal("history entries must carry a timestamp")
	}
}

// TestMinSeverityFiltersToastsNotHistory guards notifications.min_severity:
// below-floor notifications are history-only, at/above-floor still toast.
func TestMinSeverityFiltersToastsNotHistory(t *testing.T) {
	m := NewWith(registry.New(), host.MapConfig{"notifications.min_severity": "warn"})
	tm, _ := m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	m = tm.(Model)
	m, _ = notify(m, host.Info, "quiet")
	if len(m.toasts) != 0 {
		t.Fatal("info below the warn floor must not toast")
	}
	m, _ = notify(m, host.Warn, "loud")
	if len(m.toasts) != 1 || m.toasts[0].text != "loud" {
		t.Fatalf("warn at the floor must toast, toasts=%+v", m.toasts)
	}
	if len(m.history) != 2 || m.history[1].text != "quiet" {
		t.Fatalf("both notifications belong in the history, got %+v", m.history)
	}
}

// TestNotificationHistoryCommand guards the notifications.history command: it
// is registered and opens the floating shell with the recorded entries.
func TestNotificationHistoryCommand(t *testing.T) {
	m := newSized()
	m, _ = notify(m, host.Error, "server exploded")
	if _, ok := m.reg.Command("notifications.history"); !ok {
		t.Fatal("notifications.history must be a registry command")
	}
	tm, _ := m.Update(ShowNotificationHistoryMsg{})
	m = tm.(Model)
	if !m.shell.IsOpen() {
		t.Fatal("history command should open the floating shell")
	}
	if v := m.shell.View(); !strings.Contains(v, "server exploded") {
		t.Fatalf("history view missing the entry: %q", v)
	}
}

// TestSaveStampsWatcherEpoch guards the 0140 self-event suppression wiring:
// an editor save flows through the emitter adapter into watcher.MarkSaved, so
// IKE's own writes never report back as external changes.
func TestSaveStampsWatcherEpoch(t *testing.T) {
	m := newSized()
	dir := t.TempDir()
	path := filepath.Join(dir, "w.txt")
	if err := os.WriteFile(path, []byte("one\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	tm, _ := m.openPath(path, false)
	m = tm.(Model)
	for _, k := range []tea.KeyPressMsg{
		{Code: 'i', Text: "i"},
		{Code: 'X', Text: "X"},
		{Code: tea.KeyEscape},
	} {
		m = drainKey(m, k)
	}
	tm, _ = m.Update(SaveAllMsg{})
	m = tm.(Model)
	if !m.watcher.SavedRecently(path) {
		t.Fatal("editor save must stamp the watcher's save epoch")
	}
	// Routing smoke: watch events dispatch without panicking whether or not a
	// pane consumes them yet (#81-#83).
	tm, _ = m.Update(watch.EventMsg{Kind: watch.FileChanged, Path: path})
	m = tm.(Model)
	tm, _ = m.Update(watch.EventMsg{Kind: watch.DirChanged, Path: dir})
	_ = tm
}

func TestToastNeverCoversStatusLine(t *testing.T) {
	m := newSized()
	m, _ = notify(m, host.Info, "hello toast")
	lines := strings.Split(m.render(), "\n")
	last := lines[len(lines)-1]
	if strings.Contains(last, "hello toast") {
		t.Fatal("toast rendered on the status line row")
	}
	if !strings.Contains(strings.Join(lines, "\n"), "hello toast") {
		t.Fatal("toast missing from frame")
	}
}
