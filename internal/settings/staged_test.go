package settings

// staged_test.go covers the staged-apply model (0460, #1296): edits collect,
// the header counts them, the diff panel shows and edits the batch, and only
// an explicit apply writes.

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"ike/internal/config"
)

// stagedModel opens a panel on the test pages with the settings column focused.
func stagedModel(t *testing.T) *Model {
	t.Helper()
	restoreConfig(t)
	m := New(testPages(), testOpts(t))
	m.SetSize(130, 20)
	m.Open()
	m.Update(key("tab"))
	return m
}

// TestEditsStageWithoutWriting: an edit changes what the panel shows and
// nothing else — the config on disk and in memory is untouched until apply.
func TestEditsStageWithoutWriting(t *testing.T) {
	m := stagedModel(t)
	before := config.Get().UI.MenuBar

	if cmd := m.Update(key("enter")); cmd != nil {
		t.Fatal("staging an edit must not return a write command")
	}
	if !m.Dirty() || len(m.changes) != 1 {
		t.Fatalf("changes = %+v, want exactly one staged edit", m.changes)
	}
	if config.Get().UI.MenuBar != before {
		t.Fatal("staging must not touch the live config")
	}
	if got := m.value("ui.menu_bar"); got == liveValue("ui.menu_bar") {
		t.Fatalf("the panel must show the staged value, got %q", got)
	}

	commit(t, m)
	if config.Get().UI.MenuBar == before {
		t.Fatal("apply must write the staged value")
	}
	if m.Dirty() {
		t.Fatal("apply must clear the staging buffer")
	}
}

// TestEditingBackToTheOriginalDropsTheChange: a value returned to where it
// started is not a change, so the counter cannot lie.
func TestEditingBackToTheOriginalDropsTheChange(t *testing.T) {
	m := stagedModel(t)
	m.Update(key("enter"))
	if !m.Dirty() {
		t.Fatal("setup: the first toggle must stage")
	}
	m.Update(key("enter"))
	if m.Dirty() {
		t.Fatalf("toggling back must drop the change, got %+v", m.changes)
	}
}

// TestHeaderAndRailShowTheCount: the counter is in the header and the rail
// marks the pages carrying edits.
func TestHeaderAndRailShowTheCount(t *testing.T) {
	m := stagedModel(t)
	if strings.Contains(m.View(), "changes") || strings.Contains(m.View(), "1 change") {
		t.Fatal("a clean panel must not show a change counter")
	}
	m.Update(key("enter")) // Interface page
	m.Update(key("down"))
	m.Update(key("right")) // stepper on the same page

	v := m.View()
	if !strings.Contains(v, "2 changes") {
		t.Fatalf("header must count the staged edits:\n%s", v)
	}
	if !strings.Contains(v, "Interface ●2") {
		t.Fatalf("the rail must mark the page carrying them:\n%s", v)
	}
	if m.changedOnPage(1) != 0 {
		t.Fatal("an untouched page must not be marked")
	}
}

// TestApplyPanelShowsTheDiff: ctrl+s opens the diff with one line per change,
// old → new, and enter writes the batch.
func TestApplyPanelShowsTheDiff(t *testing.T) {
	m := stagedModel(t)
	m.Update(key("enter")) // ui.menu_bar
	m.Update(key("down"))
	m.Update(key("right")) // editor.tab_width

	m.Update(key("ctrl+s"))
	if !m.SubOpen() {
		t.Fatal("ctrl+s must open the apply diff")
	}
	v := m.View()
	for _, want := range []string{"Apply 2 changes", "ui.menu_bar", "editor.tab_width", "→"} {
		if !strings.Contains(v, want) {
			t.Fatalf("diff missing %q:\n%s", want, v)
		}
	}
	apply(t, m.Update(key("enter")))
	if m.SubOpen() || m.Dirty() {
		t.Fatal("writing must close the diff and clear the batch")
	}
	if config.Get().Editor.TabWidth != 5 {
		t.Fatalf("tab_width = %d, want the stepped 5", config.Get().Editor.TabWidth)
	}
}

// TestApplyPanelUndoLine: "u" drops one line and leaves the rest staged.
func TestApplyPanelUndoLine(t *testing.T) {
	m := stagedModel(t)
	m.Update(key("enter"))
	m.Update(key("down"))
	m.Update(key("right"))
	m.Update(key("ctrl+s"))

	m.Update(key("u"))
	if len(m.changes) != 1 {
		t.Fatalf("changes = %+v, want one left", m.changes)
	}
	if m.changes[0].entry.Key != "editor.tab_width" {
		t.Fatalf("the wrong line was dropped: %+v", m.changes)
	}
	// Dropping the last one closes the diff — there is nothing to apply.
	m.Update(key("u"))
	if m.SubOpen() || m.Dirty() {
		t.Fatal("an emptied batch must close the diff")
	}
}

// TestApplyPanelMovesTheScope: "s" retargets the whole batch at another layer
// before it lands.
func TestApplyPanelMovesTheScope(t *testing.T) {
	restoreConfig(t)
	opts := testOpts(t)
	opts.ProjectRoot = t.TempDir()
	m := New(testPages(), opts)
	m.SetSize(130, 20)
	m.Open()
	m.Update(key("tab"))
	m.Update(key("enter"))
	m.Update(key("ctrl+s"))

	m.Update(key("s")) // auto → user
	m.Update(key("s")) // user → project
	if got := m.changes[0].scope; got != config.ProjectScope {
		t.Fatalf("scope = %v, want project", got)
	}
	apply(t, m.Update(key("enter")))
	if got := config.Origin(opts, "ui.menu_bar"); got != "project" {
		t.Fatalf("origin = %q, want project", got)
	}
}

// TestDiscardRestoresAndPreviews: discarding drops the batch and sends the
// previous value back as a preview, so a previewed theme is undone too.
func TestDiscardRestoresAndPreviews(t *testing.T) {
	m := stagedModel(t)
	m.Update(key("down"))  // Appearance page… (rail focus is on the form here)
	m.focus = catColumn    //
	m.Update(key("down"))  // Appearance
	m.Update(key("enter")) // into the settings column
	m.Update(key("enter")) // open the theme editor
	m.Update(key("down"))
	cmd := m.Update(key("enter")) // stage tokyo-night
	if cmd == nil {
		t.Fatal("staging a previewed key must emit a preview")
	}
	if msg, ok := cmd().(PreviewMsg); !ok || msg.Value != "tokyo-night" {
		t.Fatalf("preview = %#v, want tokyo-night", cmd())
	}

	back := m.discardChanges()
	if m.Dirty() {
		t.Fatal("discard must clear the batch")
	}
	if back == nil {
		t.Fatal("discard must undo the preview")
	}
	if got := collectPreview(back); got != "default" {
		t.Fatalf("preview after discard = %q, want the original", got)
	}
	if config.Get().Theme.Name != "default" {
		t.Fatal("nothing was ever written")
	}
}

// collectPreview runs a (possibly batched) command and returns the value of
// the first PreviewMsg it produces.
func collectPreview(cmd tea.Cmd) string {
	switch msg := cmd().(type) {
	case PreviewMsg:
		return msg.Value
	case tea.BatchMsg:
		for _, c := range msg {
			if v := collectPreview(c); v != "" {
				return v
			}
		}
	}
	return ""
}

// TestEscWithChangesOffersTheDiff: esc never throws a batch away silently.
func TestEscWithChangesOffersTheDiff(t *testing.T) {
	m := stagedModel(t)
	m.Update(key("enter"))
	m.Update(key("esc"))
	if !m.SubOpen() {
		t.Fatal("esc with staged edits must offer the diff instead of closing")
	}
	if !m.IsOpen() {
		t.Fatal("the panel must stay open until the batch is resolved")
	}
	// Discarding from there closes the panel, as the esc asked for.
	m.Update(key("d"))
	if m.Dirty() || m.IsOpen() {
		t.Fatal("discard must drop the batch and complete the close")
	}
}

// TestResetStagesLikeAnyOtherEdit: "r" is staged, not written on the spot.
func TestResetStagesLikeAnyOtherEdit(t *testing.T) {
	m := stagedModel(t)
	m.Update(key("enter"))
	commit(t, m)
	if config.Origin(m.opts, "ui.menu_bar") != "user" {
		t.Fatal("setup: the value must be overridden first")
	}

	m.Update(key("r"))
	if len(m.changes) != 1 || !m.changes[0].reset {
		t.Fatalf("reset must stage a removal, got %+v", m.changes)
	}
	if config.Origin(m.opts, "ui.menu_bar") != "user" {
		t.Fatal("a staged reset must not remove the key yet")
	}
	commit(t, m)
	if got := config.Origin(m.opts, "ui.menu_bar"); got != "default" {
		t.Fatalf("origin after applied reset = %q, want default", got)
	}
}

// TestApplyWritesOneBatch: a multi-key batch reloads once, not once per key.
func TestApplyWritesOneBatch(t *testing.T) {
	m := stagedModel(t)
	m.Update(key("enter"))
	m.Update(key("down"))
	m.Update(key("right"))

	cmd := m.applyChanges()
	if cmd == nil {
		t.Fatal("apply must return the batch command")
	}
	msg, ok := cmd().(config.ConfigReloadedMsg)
	if !ok {
		t.Fatalf("apply must produce exactly one reload, got %#v", cmd())
	}
	config.Set(msg.Config)
	if config.Get().UI.MenuBar || config.Get().Editor.TabWidth != 5 {
		t.Fatalf("both keys must be written: menu_bar=%v tab_width=%d",
			config.Get().UI.MenuBar, config.Get().Editor.TabWidth)
	}
}
