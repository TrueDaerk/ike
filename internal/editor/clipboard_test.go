package editor

import (
	"errors"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"ike/internal/editor/register"
	"ike/internal/host"
)

// failClipboard is a clipboard whose writes always fail — the "pbcopy is
// missing / sandboxed / broken" case from #1255.
type failClipboard struct {
	err     error
	writes  int
	lastGot string
}

func (f *failClipboard) Read() (string, error) { return "", f.err }

func (f *failClipboard) Write(s string) error {
	f.writes++
	f.lastGot = s
	return f.err
}

// TestClipboardCopyReportsWriteFailure is the #1255 regression guard: a failed
// system-clipboard write must reach the user instead of being dropped by a
// `_ =`. Before the fix, Cmd+C on a broken bridge still reported "copied 1
// line" because the internal register was filled regardless — exactly the
// symptom the bug reported (paste elsewhere yields nothing, IKE looks fine).
func TestClipboardCopyReportsWriteFailure(t *testing.T) {
	clip := &failClipboard{err: errors.New("exec: pbcopy: executable file not found in $PATH")}
	m, _ := loaded(t, "foo bar\n")
	m.SetClipboard(clip)

	m, cmd := m.runAction("copy")
	if clip.writes == 0 {
		t.Fatal("copy never attempted a system-clipboard write")
	}
	if cmd == nil {
		t.Fatal("copy must report a failed clipboard write (#1255)")
	}
	n, ok := cmd().(NoticeMsg)
	if !ok {
		t.Fatalf("copy notice = %T, want NoticeMsg", cmd())
	}
	if !strings.Contains(n.Text, "clipboard unavailable") || !strings.Contains(n.Text, "pbcopy") {
		t.Fatalf("copy notice = %q, want the clipboard failure with its cause", n.Text)
	}
	// The failure is drained: a second, now-succeeding copy reports success
	// again rather than repeating the stale error.
	clip.err = nil
	m, cmd = m.runAction("copy")
	if cmd == nil {
		t.Fatal("second copy should still report")
	}
	if n, ok := cmd().(NoticeMsg); !ok || !strings.HasPrefix(n.Text, "copied ") {
		t.Fatalf("second copy notice = %+v, want a copied-N toast", cmd())
	}
}

// TestClipboardCutReportsWriteFailure covers Cmd+X on the same broken bridge.
func TestClipboardCutReportsWriteFailure(t *testing.T) {
	clip := &failClipboard{err: errors.New("pbcopy: broken pipe")}
	m, _ := loaded(t, "foo bar\n")
	m.SetClipboard(clip)

	_, cmd := m.runAction("cut")
	if cmd == nil {
		t.Fatal("cut must report a failed clipboard write (#1255)")
	}
	if n, ok := cmd().(NoticeMsg); !ok || !strings.Contains(n.Text, "clipboard unavailable") {
		t.Fatalf("cut notice = %+v, want the clipboard failure", cmd())
	}
}

// TestClipboardFailureReportedFromKeyPath guards the other half of the seam:
// a `"+y` typed as keys reports through Update, not just through the Cmd+C
// action. Every key-driven clipboard write drains the same signal.
func TestClipboardFailureReportedFromKeyPath(t *testing.T) {
	clip := &failClipboard{err: errors.New("wl-copy: no display")}
	m, _ := loaded(t, "foo bar\n")
	m.SetClipboard(clip)

	// "+yy — yank the line into the clipboard register.
	var cmd tea.Cmd
	for _, k := range keys(`"+yy`) {
		m, cmd = m.Update(k)
	}
	if clip.writes == 0 {
		t.Fatal(`"+yy never attempted a system-clipboard write`)
	}
	if cmd == nil {
		t.Fatal(`"+yy must report the failed clipboard write (#1255)`)
	}
	if !strings.Contains(noticeIn(t, cmd), "clipboard unavailable") {
		t.Fatalf(`"+yy notice = %q, want the clipboard failure`, noticeIn(t, cmd))
	}
}

// TestYankSyncsToSystemClipboard is the #1256 guard: with the sync on (the
// default), an unnamed yank mirrors onto the system clipboard.
func TestYankSyncsToSystemClipboard(t *testing.T) {
	for _, tc := range []struct {
		name string
		keys string
		want string
	}{
		{"yy linewise", "yy", "foo bar\n"},
		{"y motion", "yw", "foo "},
		{"visual y", "vlly", "foo"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			clip := &fakeClipboard{}
			m, _ := loadedWith(t, host.MapConfig{"editor.clipboard_sync": "true"}, "f.txt", "foo bar\nsecond\n")
			m.SetClipboard(clip)
			m = typeKeys(m, tc.keys)
			if clip.text != tc.want {
				t.Fatalf("clipboard = %q, want %q", clip.text, tc.want)
			}
		})
	}
}

// TestNamedYankDoesNotSync: only the unnamed register mirrors — `"ay` stays
// internal, as #1256 specifies.
func TestNamedYankDoesNotSync(t *testing.T) {
	clip := &fakeClipboard{text: "untouched"}
	m, _ := loadedWith(t, host.MapConfig{"editor.clipboard_sync": "true"}, "f.txt", "foo bar\n")
	m.SetClipboard(clip)
	m = typeKeys(m, `"ayy`)
	if clip.text != "untouched" {
		t.Fatalf("clipboard = %q, want it untouched by a named-register yank", clip.text)
	}
}

// TestDeleteDoesNotSync: deletes and changes fill the unnamed register in vim
// too, but IKE deliberately syncs explicit yanks only — a stray dw must not
// clobber what the user has on their system clipboard.
func TestDeleteDoesNotSync(t *testing.T) {
	clip := &fakeClipboard{text: "untouched"}
	m, _ := loadedWith(t, host.MapConfig{"editor.clipboard_sync": "true"}, "f.txt", "foo bar\n")
	m.SetClipboard(clip)
	m = typeKeys(m, "dw")
	if clip.text != "untouched" {
		t.Fatalf("clipboard = %q, want it untouched by a delete", clip.text)
	}
}

// TestYankSyncDisabled: editor.clipboard_sync = false keeps yanks internal,
// leaving Cmd+C as the only route to the system clipboard.
func TestYankSyncDisabled(t *testing.T) {
	clip := &fakeClipboard{text: "untouched"}
	m, _ := loadedWith(t, host.MapConfig{"editor.clipboard_sync": "false"}, "f.txt", "foo bar\n")
	m.SetClipboard(clip)
	m = typeKeys(m, "yy")
	if clip.text != "untouched" {
		t.Fatalf("clipboard = %q, want it untouched with the sync off", clip.text)
	}
	// The register itself is still filled — only the mirroring is off.
	if got := m.regs.Get(0).Text; got != "foo bar\n" {
		t.Fatalf("unnamed register = %q, want the yanked line", got)
	}
}

// TestClipboardHistorySizeConfigured (#2061): editor.clipboard_history_size
// sizes the ring the paste-from-history picker lists, and a missing key leaves
// the store's default in place.
func TestClipboardHistorySizeConfigured(t *testing.T) {
	m, _ := loadedWith(t, host.MapConfig{"editor.clipboard_history_size": "2"}, "f.txt", "one\ntwo\nthree\n")
	m = typeKeys(m, "yyjyyjyy")
	h := m.RegisterHistory()
	if len(h) != 2 || h[0].Text != "three\n" || h[1].Text != "two\n" {
		t.Fatalf("a size of 2 must keep the newest two yanks, got %v", h)
	}

	plain, _ := loaded(t, "one\n")
	if got := plain.regs.HistoryCap(); got != register.DefaultHistoryCap {
		t.Fatalf("without the key the default cap must stand, got %d", got)
	}
}

// noticeIn extracts a NoticeMsg's text from a (possibly batched) command.
func noticeIn(t *testing.T, cmd tea.Cmd) string {
	t.Helper()
	switch msg := cmd().(type) {
	case NoticeMsg:
		return msg.Text
	case tea.BatchMsg:
		for _, c := range msg {
			if n, ok := c().(NoticeMsg); ok {
				return n.Text
			}
		}
	}
	return ""
}
