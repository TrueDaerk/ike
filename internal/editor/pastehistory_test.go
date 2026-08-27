package editor

// pastehistory_test.go covers the editor half of paste-from-history (#2250):
// the picked entry pastes with the linewise/charwise mode it was captured
// with, and the per-entry size cap is configurable.

import (
	"strings"
	"testing"

	"ike/internal/editor/register"
	"ike/internal/host"
)

// echoClipboard is a working system clipboard: writes are kept and read back,
// like pbcopy/pbpaste. It matters here because reading `+` re-derives linewise
// from a trailing newline — the mode the ring entry carries would be lost if
// the paste went through the clipboard round-trip.
type echoClipboard struct{ text string }

func (c *echoClipboard) Read() (string, error) { return c.text, nil }

func (c *echoClipboard) Write(s string) error { c.text = s; return nil }

func TestPasteHistoryEntryPreservesCharwiseMode(t *testing.T) {
	m, _ := loaded(t, "ab\ncd\n")
	m.SetClipboard(&echoClipboard{})
	// A charwise span that happens to end in a newline — the case a
	// clipboard round-trip would misread as linewise.
	m.regs.PushHistory(register.Entry{Text: "X\n"})
	// Mid-line: at column 0 both modes would insert in the same place, so the
	// caret has to sit inside a line for the distinction to show.
	m.SetCursor(0, 1)

	m.PasteHistoryEntry(0)

	if got := m.Text(); got != "aX\nb\ncd" {
		t.Fatalf("charwise entry must splice inline, text = %q", got)
	}
}

func TestPasteHistoryEntryPreservesLinewiseMode(t *testing.T) {
	m, _ := loaded(t, "ab\ncd\n")
	m.SetClipboard(&echoClipboard{})
	m.regs.PushHistory(register.Entry{Text: "X\n", Linewise: true})
	m.SetCursor(0, 1)

	m.PasteHistoryEntry(0)

	if got := m.Text(); got != "X\nab\ncd" {
		t.Fatalf("linewise entry must open a whole line, text = %q", got)
	}
	// The pick becomes the current clipboard, JetBrains-style.
	if got := m.regs.Get(0).Text; got != "X\n" {
		t.Fatalf("unnamed register = %q, want the picked entry", got)
	}
}

// TestPasteHistoryEntryIsOneUndoStep guards the "pastes via the normal paste
// path" half of the acceptance: the insert is undoable in one step.
func TestPasteHistoryEntryIsOneUndoStep(t *testing.T) {
	m, _ := loaded(t, "ab\ncd\n")
	m.regs.PushHistory(register.Entry{Text: "one\ntwo\n", Linewise: true})
	m.PasteHistoryEntry(0)
	if got := m.Text(); got != "one\ntwo\nab\ncd" {
		t.Fatalf("paste = %q", got)
	}
	m = typeKeys(m, "u")
	if got := m.Text(); got != "ab\ncd" {
		t.Fatalf("one undo must remove the whole paste, text = %q", got)
	}
}

// TestPasteHistoryEntryOutOfRange: a stale picker index is a no-op, not a panic.
func TestPasteHistoryEntryOutOfRange(t *testing.T) {
	m, _ := loaded(t, "ab\n")
	m.PasteHistoryEntry(0)
	m.PasteHistoryEntry(-1)
	if got := m.Text(); got != "ab" {
		t.Fatalf("an out-of-range pick must not edit, text = %q", got)
	}
}

// TestClipboardHistoryEntryCapConfigured (#2250): a yank above
// editor.clipboard_history_max_kb stays out of the ring but still pastes in
// full from the registers, and a missing key leaves the store default.
func TestClipboardHistoryEntryCapConfigured(t *testing.T) {
	big := strings.Repeat("x", 2048)
	m, _ := loadedWith(t, host.MapConfig{"editor.clipboard_history_max_kb": "1"}, "f.txt", big+"\nsmall\n")
	m = typeKeys(m, "yyj yy")
	h := m.RegisterHistory()
	if len(h) != 1 || h[0].Text != "small\n" {
		t.Fatalf("the oversized line must be skipped by the ring, got %v", h)
	}
	if got := m.regs.EntryMaxBytes(); got != 1024 {
		t.Fatalf("entry cap = %d bytes, want 1 KiB", got)
	}

	plain, _ := loaded(t, "one\n")
	if got := plain.regs.EntryMaxBytes(); got != register.DefaultEntryMaxBytes {
		t.Fatalf("without the key the default cap must stand, got %d", got)
	}
}
