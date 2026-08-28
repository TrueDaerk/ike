package settings

import (
	"testing"

	tea "charm.land/bubbletea/v2"
)

// enumPicker opens the panel on theme.name with the option list focused —
// the state the live preview of #2181 works in.
func enumPicker(t *testing.T) (*Model, *enumEditor) {
	t.Helper()
	m := detailModel(t, 1, 0) // Appearance → theme.name, an Enum
	m.focus = detailColumn
	n, ok := m.editor.(*enumEditor)
	if !ok {
		t.Fatalf("editor = %T, want an enum editor", m.editor)
	}
	return m, n
}

// previewOf runs a command and reports the PreviewMsg it carries. A nil
// command, or one carrying anything else, reports ok = false.
func previewOf(cmd tea.Cmd) (PreviewMsg, bool) {
	if cmd == nil {
		return PreviewMsg{}, false
	}
	msg, ok := cmd().(PreviewMsg)
	return msg, ok
}

// tick resolves the debounce deadline a browse move armed, without waiting
// for the runtime timer.
func tick(m *Model) tea.Cmd {
	return m.PreviewTick(PreviewTickMsg{Key: m.browse.key, Gen: m.browse.gen})
}

// TestBrowsePreviewAppliesHighlighted guards #2181: moving the selection in
// the theme list previews the highlighted theme — without staging it, so
// nothing can reach disk from browsing alone.
func TestBrowsePreviewAppliesHighlighted(t *testing.T) {
	m, n := enumPicker(t)
	cmd := n.Update(key("down"))
	if cmd == nil {
		t.Fatal("moving the selection must arm a preview")
	}
	msg, ok := previewOf(tick(m))
	if !ok {
		t.Fatal("the debounce deadline must emit a PreviewMsg")
	}
	if msg.Key != "theme.name" || msg.Value != "tokyo-night" {
		t.Fatalf("preview = %+v, want theme.name → tokyo-night", msg)
	}
	if _, staged := m.change("theme.name"); staged {
		t.Fatal("browsing must not stage anything")
	}
	if m.Dirty() {
		t.Fatal("browsing must leave the panel clean")
	}
}

// TestBrowsePreviewRollbackOnEsc guards #2181: esc in the option list puts
// the theme that was active before the browse back.
func TestBrowsePreviewRollbackOnEsc(t *testing.T) {
	m, n := enumPicker(t)
	n.Update(key("down"))
	if _, ok := previewOf(tick(m)); !ok {
		t.Fatal("precondition: the move must have previewed")
	}
	msg, ok := previewOf(n.Update(key("esc")))
	if !ok {
		t.Fatal("esc must roll the preview back")
	}
	if msg.Key != "theme.name" || msg.Value != "default" {
		t.Fatalf("rollback = %+v, want theme.name → default", msg)
	}
	if m.PreviewActive() {
		t.Fatal("esc must end the browse")
	}
	if m.focus != formColumn {
		t.Fatalf("focus = %v, want the settings column after esc", m.focus)
	}
}

// TestBrowsePreviewRollbackOnClose guards #2181: closing the panel while a
// preview is showing restores the previous theme too — a preview must never
// outlive the list that armed it.
func TestBrowsePreviewRollbackOnClose(t *testing.T) {
	m, n := enumPicker(t)
	n.Update(key("down"))
	previewOf(tick(m))
	msg, ok := previewOf(m.CancelPreview())
	if !ok || msg.Value != "default" {
		t.Fatalf("closing must restore the previous theme, got %+v ok=%v", msg, ok)
	}
	m.Close()
	if m.PreviewActive() {
		t.Fatal("a closed panel must hold no preview")
	}
}

// TestBrowsePreviewNoRollbackBeforeDebounce guards #2181: a browse abandoned
// before its debounce deadline never applied anything, so it has nothing to
// undo — and its orphaned tick stays silent.
func TestBrowsePreviewNoRollbackBeforeDebounce(t *testing.T) {
	m, n := enumPicker(t)
	pending := PreviewTickMsg{Key: "theme.name", Gen: 1}
	n.Update(key("down"))
	if cmd := n.Update(key("esc")); cmd != nil {
		t.Fatalf("esc before the deadline must not emit a rollback, got %+v", cmd())
	}
	if cmd := m.PreviewTick(pending); cmd != nil {
		t.Fatalf("an orphaned deadline must be dropped, got %+v", cmd())
	}
}

// TestBrowsePreviewDebouncesRapidScrolling guards #2181: fast scrolling arms
// one deadline per move but only the newest one re-themes, so a held key
// coalesces into a single palette swap.
func TestBrowsePreviewDebouncesRapidScrolling(t *testing.T) {
	m, n := enumPicker(t)
	n.Update(key("down")) // → tokyo-night, generation 1
	stale := PreviewTickMsg{Key: "theme.name", Gen: m.browse.gen}
	n.Update(key("up")) // → default, generation 2
	if m.browse.gen != stale.Gen+1 {
		t.Fatalf("generation = %d, want %d — every move must supersede the last", m.browse.gen, stale.Gen+1)
	}
	if cmd := m.PreviewTick(stale); cmd != nil {
		t.Fatalf("a superseded deadline must be dropped, got %+v", cmd())
	}
	msg, ok := previewOf(tick(m))
	if !ok || msg.Value != "default" {
		t.Fatalf("the newest deadline must apply the current highlight, got %+v ok=%v", msg, ok)
	}
}

// TestBrowsePreviewEnterPersists guards #2181: enter stages the highlighted
// theme through the normal write-back — the preview becomes the staged value
// instead of being rolled back — and applying it writes the config.
func TestBrowsePreviewEnterPersists(t *testing.T) {
	m, n := enumPicker(t)
	n.Update(key("down"))
	previewOf(tick(m))
	msg, ok := previewOf(n.Update(key("enter")))
	if !ok || msg.Value != "tokyo-night" {
		t.Fatalf("enter must keep the previewed theme, got %+v ok=%v", msg, ok)
	}
	if m.PreviewActive() {
		t.Fatal("enter must end the browse instead of leaving a rollback armed")
	}
	c, staged := m.change("theme.name")
	if !staged || c.shown != "tokyo-night" || c.old != "default" {
		t.Fatalf("enter must stage default → tokyo-night, got %+v staged=%v", c, staged)
	}
	if cmd := m.applyChanges(); cmd == nil {
		t.Fatal("applying the batch must carry a write")
	}
}

// TestBrowsePreviewWheelPreviews guards #2181: the wheel browses like the
// keys do, including the debounce.
func TestBrowsePreviewWheelPreviews(t *testing.T) {
	m, n := enumPicker(t)
	if cmd := n.Wheel(1); cmd == nil {
		t.Fatal("wheeling the option list must arm a preview")
	}
	msg, ok := previewOf(tick(m))
	if !ok || msg.Value != "tokyo-night" {
		t.Fatalf("wheel preview = %+v ok=%v, want tokyo-night", msg, ok)
	}
}

// TestBrowsePreviewOnlyPreviewKeys guards #2181: entries whose value is not
// judged by looking at it browse without side effects.
func TestBrowsePreviewOnlyPreviewKeys(t *testing.T) {
	m := detailModel(t, 0, 0)
	e := Entry{Key: "ui.menu_bar"}
	if cmd := m.browsePreview(e, "false"); cmd != nil {
		t.Fatal("a non-preview key must not arm a preview")
	}
	if m.browse.key != "" {
		t.Fatalf("browse state = %+v, want none", m.browse)
	}
}

// TestBrowsePreviewRollbackOnClickAway guards #2181: leaving the entry with
// the mouse rolls the preview back like esc does — the rail click below moves
// to another page, so the browsed entry is no longer on screen.
func TestBrowsePreviewRollbackOnClickAway(t *testing.T) {
	m, n := enumPicker(t)
	n.Update(key("down"))
	if _, ok := previewOf(tick(m)); !ok {
		t.Fatal("precondition: the move must have previewed")
	}
	msg, ok := previewOf(m.Click(1, bodyTop)) // the first rail row
	if !ok || msg.Value != "default" {
		t.Fatalf("clicking away must restore the previous theme, got %+v ok=%v", msg, ok)
	}
	if m.PreviewActive() {
		t.Fatal("clicking away must end the browse")
	}
}
