package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"ike/internal/backup"
	"ike/internal/pane"
)

// shell_rowclick_test.go covers the floating shell's row-click seam (#2275):
// the shared coordinate mapping in the root model's mouse handler plus each
// hosted picker's row → item inverse of its render loop.

// shellRowClick presses the left button on body row of the open shell picker —
// the screen coordinates a real click on that line carries — and drains the
// resulting command, so an activation that opens a pane has happened by the
// time the caller looks.
func shellRowClick(t *testing.T, m Model, row int) Model {
	t.Helper()
	v := m.shell.View()
	bx, by := (m.width-lipgloss.Width(v))/2, (m.height-lipgloss.Height(v))/2
	ox, oy := m.shell.ContentOrigin()
	x, y := bx+ox+2, by+oy+row-m.shell.ScrollOffset()
	out, cmd := m.handleMouse(mouseEvent{
		Mouse:  tea.Mouse{X: x, Y: y, Button: tea.MouseLeft},
		action: mousePress,
	})
	return drainCmd(out.(Model), cmd)
}

// --- pins ---

// TestPinPickerRowClickSelectsThenJumps: the four slot lines are rows 0..3; a
// click selects, a second click on the same slot jumps to its file.
func TestPinPickerRowClickSelectsThenJumps(t *testing.T) {
	_, files := navProject(t)
	m := newSized()
	for i := 0; i < 2; i++ {
		tm, _ := m.openPath(files[i], false)
		m = tm.(Model)
		m = step(m, PinSlotMsg{Slot: i + 1})
	}
	m = step(m, PinPickerMsg{})
	m = shellRowClick(t, m, 1)
	if m.pinSel != 1 {
		t.Fatalf("click on slot 2 selected %d, want 1", m.pinSel)
	}
	if !m.pinPickerOpen() {
		t.Fatal("the first click must only select")
	}
	m = shellRowClick(t, m, 1)
	if m.pinPickerOpen() {
		t.Fatal("a click on the selected slot must jump and close")
	}
	if got := m.activeFilePath(); got != canonicalPath(files[1]) {
		t.Fatalf("jumped to %q, want %q", got, files[1])
	}
}

// TestPinPickerRowClickIgnoresHints: the blank line and the key hints under
// the four slots select nothing.
func TestPinPickerRowClickIgnoresHints(t *testing.T) {
	_, files := navProject(t)
	m := newSized()
	tm, _ := m.openPath(files[0], false)
	m = tm.(Model)
	m = step(m, PinSlotMsg{Slot: 1})
	m = step(m, PinPickerMsg{})
	m = shellRowClick(t, m, 5) // the hint line
	if !m.pinPickerOpen() || m.pinSel != 0 {
		t.Fatalf("a click on the hints changed the picker (sel=%d, open=%v)", m.pinSel, m.pinPickerOpen())
	}
}

// --- local history ---

// TestLocalHistoryRowClickSelectsThenOpens: rows map onto snapshots; a click
// on the selected one opens the diff pane, the panel's enter.
func TestLocalHistoryRowClickSelectsThenOpens(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "a.txt")
	if err := os.WriteFile(path, []byte("one\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	m := newSized()
	m = openDirty(t, m, path)
	m.lhStore.Record(path, []byte("SNAP1\n"))
	m.lhStore.Record(path, []byte("SNAP2\n"))
	m.openLocalHistoryPicker()
	if len(m.lhEntries) < 2 {
		t.Fatalf("need two snapshots, got %d", len(m.lhEntries))
	}
	m = shellRowClick(t, m, 1)
	if m.lhSel != 1 || !m.localHistoryOpen() {
		t.Fatalf("first click must select row 1 only (sel=%d, open=%v)", m.lhSel, m.localHistoryOpen())
	}
	m = shellRowClick(t, m, 1)
	if m.localHistoryOpen() {
		t.Fatal("a click on the selected snapshot must open the diff pane and close")
	}
	inst := m.activeWS().Panes.FocusedInstance()
	if inst == nil || inst.Kind() != pane.KindDiff {
		t.Fatal("the activation must focus a diff pane")
	}
}

// TestLocalHistoryRowClickIgnoresTail: the blank tail and the key hints below
// the snapshot list select nothing.
func TestLocalHistoryRowClickIgnoresTail(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "a.txt")
	if err := os.WriteFile(path, []byte("one\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	m := newSized()
	m = openDirty(t, m, path)
	m.lhStore.Record(path, []byte("SNAP1\n"))
	m.openLocalHistoryPicker()
	m = shellRowClick(t, m, len(m.lhEntries)+3)
	if !m.localHistoryOpen() || m.lhSel != 0 {
		t.Fatalf("a click below the list changed the panel (sel=%d)", m.lhSel)
	}
}

// --- VCS history: the Timeline ---

// TestTimelineRowClickSelectsThenDiffs: a click selects a merged entry, a
// second click on it diffs that entry against the buffer.
func TestTimelineRowClickSelectsThenDiffs(t *testing.T) {
	m, _ := timelineRepo(t)
	m = openTimelineWithGit(t, m)
	if len(m.tl.merged) < 2 {
		t.Fatalf("need two entries, got %d", len(m.tl.merged))
	}
	m = shellRowClick(t, m, 1)
	if m.tl.sel != 1 || !m.timelineOpen() {
		t.Fatalf("first click must select row 1 only (sel=%d, open=%v)", m.tl.sel, m.timelineOpen())
	}
	m = shellRowClick(t, m, 1)
	if m.timelineOpen() {
		t.Fatal("a click on the selected entry must diff it and close the Timeline")
	}
}

// TestTimelineRowClickIgnoresHints: the key-hint block under the entries is
// inert.
func TestTimelineRowClickIgnoresHints(t *testing.T) {
	m, _ := timelineRepo(t)
	m = openTimelineWithGit(t, m)
	m = shellRowClick(t, m, len(m.tl.merged)+2)
	if !m.timelineOpen() || m.tl.sel != 0 {
		t.Fatalf("a click on the hints changed the Timeline (sel=%d)", m.tl.sel)
	}
}

// --- range history ---

// TestHistoryPickerRowClickSelectsThenShowsPatch: a click selects a commit, a
// second click on it opens the patch view.
func TestHistoryPickerRowClickSelectsThenShowsPatch(t *testing.T) {
	m := newSized()
	m.openHistoryPicker(rangeLogFixture())
	if !m.historyPickerOpen() {
		t.Fatal("picker did not open")
	}
	m = shellRowClick(t, m, 1)
	if m.histSel != 1 || m.histPatch {
		t.Fatalf("first click must select row 1 only (sel=%d, patch=%v)", m.histSel, m.histPatch)
	}
	m = shellRowClick(t, m, 1)
	if !m.histPatch {
		t.Fatal("a click on the selected commit must show its patch")
	}
	// The patch view has no rows to click.
	before := m.histSel
	m = shellRowClick(t, m, 0)
	if !m.histPatch || m.histSel != before {
		t.Fatal("clicks inside the patch view must change nothing")
	}
}

// --- crash recovery ---

// TestRecoveryRowClickSelectsOnly: the intro line and its blank spacer sit
// above the files, and the dialog has no enter action — a click selects and
// nothing else, twice over.
func TestRecoveryRowClickSelectsOnly(t *testing.T) {
	m := recoverySeed(t, func(svc *backup.Service, dir string) {
		for _, name := range []string{"one.txt", "two.txt"} {
			file := filepath.Join(dir, name)
			_ = os.WriteFile(file, []byte("on disk\n"), 0o644)
			_ = svc.Snapshot(backup.Doc{Key: file, Path: file, Text: "recovered " + name + "\n"})
		}
	})
	if !m.recoveryOpen() || len(m.recovery.items) != 2 {
		t.Fatalf("need two recoverable files, got %v", m.recoveryOpen())
	}
	m = shellRowClick(t, m, 3) // intro + spacer + first file => row 3 is the second
	if m.recovery.cursor != 1 {
		t.Fatalf("click selected cursor %d, want 1", m.recovery.cursor)
	}
	m = shellRowClick(t, m, 3)
	if !m.recoveryOpen() || m.recovery.cursor != 1 {
		t.Fatal("a second click must not restore or discard — the dialog has no enter")
	}
	if remainingSnapshots(t) != 2 {
		t.Fatal("clicking must not touch the snapshots on disk")
	}
	m = shellRowClick(t, m, 0) // the intro line
	if m.recovery.cursor != 1 {
		t.Fatal("the intro line must be inert")
	}
}

// --- the setup wizards ---

// TestOnboardingRowClickSelectsThenToggles: the three intro lines and their
// spacer are inert, a click selects a language and a click on the selected one
// toggles its checkbox (the dialog's space).
func TestOnboardingRowClickSelectsThenToggles(t *testing.T) {
	onboardLang(t, "obgo")
	m := onboardSeed(t)
	if !m.onboardingOpen() {
		t.Fatal("dialog did not open")
	}
	id := m.onboarding.items[0].ID
	m = shellRowClick(t, m, 1) // an intro line
	if !m.onboarding.checked[id] || m.onboarding.cursor != 0 {
		t.Fatal("a click on the intro text must not select or toggle anything")
	}
	// The cursor opens on the first row, so a click there is already the
	// press-again activation: it toggles the checkbox, and toggles it back.
	m = shellRowClick(t, m, onboardingHeadRows)
	if m.onboarding.checked[id] {
		t.Fatal("a click on the selected row must toggle its checkbox off")
	}
	m = shellRowClick(t, m, onboardingHeadRows)
	if !m.onboarding.checked[id] {
		t.Fatal("a second click must toggle it back on")
	}
	m = shellRowClick(t, m, onboardingHeadRows+len(m.onboarding.items)+1)
	if !m.onboarding.checked[id] {
		t.Fatal("a click on the legend must be inert")
	}
	if !m.onboardingOpen() {
		t.Fatal("toggling must keep the dialog open")
	}
}

// TestToolSetupRowClickTogglesSelected: the tool dialog is the onboarding
// dialog's twin — a click on the selected row toggles its checkbox.
func TestToolSetupRowClickTogglesSelected(t *testing.T) {
	catalogStub(t, nil, stubEntry("misstool"), stubEntry("othertool"))
	m := toolSetupSeed(t)
	if !m.toolSetupOpen() || len(m.toolSetup.rows) < 2 {
		t.Fatalf("need two offered tools, got %v", m.toolSetupOpen())
	}
	m = shellRowClick(t, m, toolSetupHeadRows+1)
	if m.toolSetup.cursor != 1 {
		t.Fatalf("click selected cursor %d, want 1", m.toolSetup.cursor)
	}
	before := m.toolSetup.rows[1].checked
	m = shellRowClick(t, m, toolSetupHeadRows+1)
	if m.toolSetup.rows[1].checked == before {
		t.Fatal("a click on the selected row must toggle its checkbox")
	}
	m = shellRowClick(t, m, toolSetupHeadRows+len(m.toolSetup.rows)+1)
	if !m.toolSetupOpen() {
		t.Fatal("a click on the legend must be inert")
	}
}

// TestThemePickRowClickPreviewsThenKeeps: a click previews the theme live, a
// click on the already-selected theme keeps it (the chooser's enter).
func TestThemePickRowClickPreviewsThenKeeps(t *testing.T) {
	m := tourSeed(t)
	m = finishTourFully(t, m)
	if !m.themePickOpen() || len(m.themePick.names) < 2 {
		t.Fatalf("need a theme chooser with two themes, open=%v", m.themePickOpen())
	}
	before := m.themePal
	m = shellRowClick(t, m, themePickHeadRows+1)
	if m.themePick == nil || m.themePick.cursor != 1 {
		t.Fatal("a click must select the second theme")
	}
	if m.themePal == before {
		t.Fatal("a click must preview the clicked theme live")
	}
	picked := m.themePick.names[1]
	m = shellRowClick(t, m, themePickHeadRows+1)
	if m.themePick != nil {
		t.Fatal("a click on the selected theme must keep it and close the chooser")
	}
	if got := userSettings(t); !strings.Contains(got, picked) {
		t.Fatalf("the activation must persist theme.name=%q, settings:\n%s", picked, got)
	}
}

// --- bookmarks overview ---

// TestBookmarkOverviewRowClickSelectsThenJumps: the file header above each
// group is inert, a click picks the bookmark under it and a click on the
// selected one jumps.
func TestBookmarkOverviewRowClickSelectsThenJumps(t *testing.T) {
	dir := t.TempDir()
	a := writeTemp(t, dir, "a.txt", "alpha\n")
	b := writeTemp(t, dir, "b.txt", "delta\nepsilon\nzeta\n")
	m := openApp(t, a)
	m.bmarks.Add(bpKey(a), 0)
	m.bmarks.Add(bpKey(b), 2)
	m = dispatch(t, m, BookmarkOverviewMsg{})
	if !m.bookmarkOverviewOpen() || len(m.bmOverview.flat) != 2 {
		t.Fatalf("need two bookmarks in the overview, got %v", m.bookmarkOverviewOpen())
	}
	// Rows: header(a), bookmark, header(b), bookmark.
	m = shellRowClick(t, m, 2) // the second group's file header
	if m.bmOverview.sel != 0 {
		t.Fatalf("a group header must select nothing, sel = %d", m.bmOverview.sel)
	}
	m = shellRowClick(t, m, 3)
	if m.bmOverview.sel != 1 || !m.bookmarkOverviewOpen() {
		t.Fatalf("first click must only select (sel=%d)", m.bmOverview.sel)
	}
	m = shellRowClick(t, m, 3)
	if m.bookmarkOverviewOpen() {
		t.Fatal("a click on the selected bookmark must jump and close")
	}
	ed := m.activeWS().Panes.FocusedInstance().Editor()
	if l, _ := ed.CursorPos(); !strings.HasSuffix(ed.Path(), "b.txt") || l != 2 {
		t.Fatalf("jump landed on %s:%d, want b.txt:2", ed.Path(), l)
	}
}

// --- change feed ---

// TestChangeFeedRowClickSelectsThenOpens: a click picks the entry on that
// line, a click on the selected one opens the file.
func TestChangeFeedRowClickSelectsThenOpens(t *testing.T) {
	dir := t.TempDir()
	a := writeTemp(t, dir, "a.txt", "alpha\n")
	b := writeTemp(t, dir, "b.txt", "beta\n")
	m := openApp(t, a)
	m = externalWrite(t, m, b, "beta changed\n")
	m = externalWrite(t, m, a, "alpha changed\n")
	m.openChangeFeed()
	if !m.changeFeedOpen() || len(m.cfEntries) != 2 {
		t.Fatalf("need two feed entries, got %d", len(m.cfEntries))
	}
	rows := m.changeFeedRows()
	row, want := -1, -1
	for i, r := range rows {
		if r.entry == 1 {
			row, want = i, 1
			break
		}
	}
	if row < 0 {
		t.Fatalf("no rendered row for the second entry in %v", rows)
	}
	m = shellRowClick(t, m, row)
	if m.cfSel != want || !m.changeFeedOpen() {
		t.Fatalf("first click must only select (sel=%d, want %d)", m.cfSel, want)
	}
	path := m.cfEntries[want].Path
	m = shellRowClick(t, m, row)
	if m.changeFeedOpen() {
		t.Fatal("a click on the selected entry must open it and close the panel")
	}
	if got := m.activeFilePath(); got != canonicalPath(path) {
		t.Fatalf("opened %q, want %q", got, path)
	}
}

// --- LSP rename preview ---

// TestLSPRenamePreviewRowClickSelectsThenApplies: the headline and its spacer
// are inert, a click picks an affected file and a click on the selected one
// applies the rename.
func TestLSPRenamePreviewRowClickSelectsThenApplies(t *testing.T) {
	m, applied := openRenamePreview(t)
	m = shellRowClick(t, m, 0) // the headline
	if m.lspRenamePreview.cursor != 0 || *applied != 0 {
		t.Fatal("the headline must be inert")
	}
	m = shellRowClick(t, m, lspRenamePreviewHeadRows+1)
	if m.lspRenamePreview == nil || m.lspRenamePreview.cursor != 1 {
		t.Fatal("a click must select the second file")
	}
	if *applied != 0 {
		t.Fatal("the first click must not apply")
	}
	m = shellRowClick(t, m, lspRenamePreviewHeadRows+1)
	if m.lspRenamePreviewOpen() {
		t.Fatal("a click on the selected file must apply and close")
	}
	if *applied != 1 {
		t.Fatalf("apply ran %d times, want 1", *applied)
	}
}

// --- the seam itself ---

// TestShellBodyClickIgnoresChrome: a press on the shell's border ring starts a
// resize instead of reaching a picker, and one on the heading rows reaches
// nothing at all.
func TestShellBodyClickIgnoresChrome(t *testing.T) {
	m := newSized()
	m.openHistoryPicker(rangeLogFixture())
	v := m.shell.View()
	w, h := lipgloss.Width(v), lipgloss.Height(v)
	bx, by := (m.width-w)/2, (m.height-h)/2
	ox, oy := m.shell.ContentOrigin()

	out, _ := m.handleMouse(mouseEvent{
		Mouse:  tea.Mouse{X: bx, Y: by, Button: tea.MouseLeft},
		action: mousePress,
	})
	m2 := out.(Model)
	if m2.floatDrag == nil {
		t.Fatal("the border ring must still start a resize drag")
	}
	if m2.histSel != 0 {
		t.Fatalf("the ring must not select a row, sel = %d", m2.histSel)
	}

	out, _ = m.handleMouse(mouseEvent{
		Mouse:  tea.Mouse{X: bx + ox, Y: by + oy - 1, Button: tea.MouseLeft},
		action: mousePress,
	})
	if m3 := out.(Model); m3.histSel != 0 || m3.histPatch {
		t.Fatal("a press on the heading rows must reach no row")
	}
}

// TestShellBodyClickHonoursScrollOffset: the row a click lands on is the
// scrolled-to content row, not the on-screen line.
func TestShellBodyClickHonoursScrollOffset(t *testing.T) {
	m := newSized()
	msg := rangeLogFixture()
	for len(msg.Entries) < 60 {
		e := msg.Entries[0]
		e.ShortHash = "h" + strings.Repeat("x", len(msg.Entries)%5+1)
		msg.Entries = append(msg.Entries, e)
	}
	m.openHistoryPicker(msg)
	_ = m.shell.View() // lay the body out so the viewport has a height
	m.shell.Wheel(10)
	if m.shell.ScrollOffset() != 10 {
		t.Fatalf("scroll offset = %d, want 10", m.shell.ScrollOffset())
	}
	m = shellRowClick(t, m, 12)
	if m.histSel != 12 {
		t.Fatalf("click selected %d, want the scrolled-to row 12", m.histSel)
	}
}
