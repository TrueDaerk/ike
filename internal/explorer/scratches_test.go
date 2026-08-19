package explorer

// scratches_test.go covers the Scratches section (#1963): the divider-
// separated scratch list at the pane's bottom, walked with the unified
// cursor, opened through the standard funnel, deleted/renamed through the
// fileops prompts against the scratch store's guarded operations, resizable
// and collapsible via the divider, with sort order from scratch.sort.

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"ike/internal/host"
	"ike/internal/scratch"
)

// scratchModel is navModel plus an attached fake store: files tree entries
// and the given scratch entries, pane 30x12.
func scratchModel(t *testing.T, files int, scratches ...string) (Model, *[]scratch.Entry) {
	t.Helper()
	m := navModel(t, files)
	m.SetSize(30, 12)
	entries := &[]scratch.Entry{}
	for i, name := range scratches {
		*entries = append(*entries, scratch.Entry{
			Path:    filepath.Join("/scratches", name),
			ModTime: time.Unix(int64(1000-i), 0), // listed order = newest first
		})
	}
	m.EnableScratches("", func() ([]scratch.Entry, error) {
		out := append([]scratch.Entry(nil), *entries...)
		return out, nil
	})
	return m, entries
}

func runCmd(t *testing.T, cmd tea.Cmd) tea.Msg {
	t.Helper()
	if cmd == nil {
		t.Fatal("want a command, got nil")
	}
	return cmd()
}

// TestScratchSectionRenderSorted: the section renders behind a divider and
// sorts by name regardless of the store's newest-first listing.
func TestScratchSectionRenderSorted(t *testing.T) {
	m, _ := scratchModel(t, 2, "zeta.txt", "alpha.go", "mid.md")
	names := make([]string, len(m.ScratchEntries()))
	for i, e := range m.ScratchEntries() {
		names[i] = filepath.Base(e.Path)
	}
	want := []string{"alpha.go", "mid.md", "zeta.txt"}
	for i := range want {
		if names[i] != want[i] {
			t.Fatalf("sort by name: rows = %v want %v", names, want)
		}
	}
	view := ansi.Strip(m.View())
	if !strings.Contains(view, "Scratches") {
		t.Fatalf("view must show the section divider:\n%s", view)
	}
	if !strings.Contains(view, "alpha.go") || !strings.Contains(view, "zeta.txt") {
		t.Fatalf("view must list the scratches:\n%s", view)
	}
	di := strings.Index(view, "Scratches")
	if ai := strings.Index(view, "alpha.go"); ai < di {
		t.Fatalf("rows must render below the divider:\n%s", view)
	}
	// The divider must sit exactly at the tree/section boundary.
	lines := strings.Split(view, "\n")
	if got := len(lines); got != 12 {
		t.Fatalf("view height = %d want 12", got)
	}
	if !strings.Contains(lines[m.treeAreaRows()], "Scratches") {
		t.Fatalf("divider not on row %d:\n%s", m.treeAreaRows(), view)
	}
}

// TestScratchSortModified: scratch.sort = modified keeps the newest-first
// order; switching back re-sorts by name.
func TestScratchSortModified(t *testing.T) {
	m, _ := scratchModel(t, 1, "zeta.txt", "alpha.go")
	m.Configure(host.MapConfig{"scratch.sort": "modified"})
	m.RefreshScratches()
	if base := filepath.Base(m.ScratchEntries()[0].Path); base != "zeta.txt" {
		t.Fatalf("modified sort: first = %s want zeta.txt (newest)", base)
	}
	m.Configure(host.MapConfig{"scratch.sort": "name"})
	if base := filepath.Base(m.ScratchEntries()[0].Path); base != "alpha.go" {
		t.Fatalf("name sort: first = %s want alpha.go", base)
	}
}

// TestScratchCursorCrossesDivider: j walks off the last tree row into the
// section, k walks back, wrap-around passes both ends, G lands on the last
// scratch and gg returns to the tree top.
func TestScratchCursorCrossesDivider(t *testing.T) {
	m, _ := scratchModel(t, 2, "a.txt", "b.txt")
	// Rows: root, f00, f01 — walk to the last tree row.
	m = press(m, "G")
	if m.ScratchCursor() != len(m.ScratchEntries())-1 {
		t.Fatalf("G: scratch cursor = %d want last", m.ScratchCursor())
	}
	m = press(m, "j") // wrap: past the last scratch back to the tree top
	if m.ScratchCursor() != -1 || m.cursor != 0 {
		t.Fatalf("wrap down: cursor = %d scratch = %d", m.cursor, m.ScratchCursor())
	}
	m = press(m, "k") // wrap up: back onto the last scratch
	if m.ScratchCursor() != len(m.ScratchEntries())-1 {
		t.Fatalf("wrap up: scratch cursor = %d", m.ScratchCursor())
	}
	m = press(m, "k")
	if m.ScratchCursor() != 0 {
		t.Fatalf("k: scratch cursor = %d want 0", m.ScratchCursor())
	}
	m = press(m, "k") // seamlessly back into the tree
	if m.ScratchCursor() != -1 || m.cursor != len(m.rows)-1 {
		t.Fatalf("k into tree: cursor = %d scratch = %d", m.cursor, m.ScratchCursor())
	}
	m = press(m, "g")
	m = press(m, "g")
	if m.cursor != 0 || m.ScratchCursor() != -1 {
		t.Fatalf("gg: cursor = %d scratch = %d", m.cursor, m.ScratchCursor())
	}
}

// TestScratchOpenLikeAFile: enter and l on a section row emit the standard
// OpenFileMsg; o opens in a split (NewPane), exactly like a tree file.
func TestScratchOpenLikeAFile(t *testing.T) {
	m, _ := scratchModel(t, 1, "a.txt", "b.txt")
	m = press(m, "G")
	m = press(m, "k") // a.txt (name sort)
	mm, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	m = mm
	open, ok := runCmd(t, cmd).(OpenFileMsg)
	if !ok || filepath.Base(open.Path) != "a.txt" || open.NewPane {
		t.Fatalf("enter: msg = %#v", open)
	}
	mm, cmd = m.Update(tea.KeyPressMsg{Code: 'o', Text: "o"})
	m = mm
	split, ok := runCmd(t, cmd).(OpenFileMsg)
	if !ok || !split.NewPane {
		t.Fatalf("o: msg = %#v", split)
	}
	// Double-click on the row activates too, through the same funnel.
	row := m.treeAreaRows() + 1
	m, _ = m.MouseClick(2, row)
	m, cmd = m.MouseClick(2, row)
	if dbl, ok := runCmd(t, cmd).(OpenFileMsg); !ok || filepath.Base(dbl.Path) != "a.txt" {
		t.Fatalf("double-click: msg = %#v", dbl)
	}
}

// TestScratchDeleteFlow: d opens the explorer's confirm dialog anchored on
// the row; y deletes through the store seam and announces FileDeletedMsg so
// the app closes the file's tabs.
func TestScratchDeleteFlow(t *testing.T) {
	m, entries := scratchModel(t, 1, "a.txt", "b.txt")
	var removed string
	m.SetScratchOps(func(p string) error {
		removed = p
		kept := (*entries)[:0]
		for _, e := range *entries {
			if e.Path != p {
				kept = append(kept, e)
			}
		}
		*entries = kept
		return nil
	}, nil)
	m = press(m, "G") // b.txt
	mm, _ := m.Update(DeleteMsg{})
	m = mm
	if !m.Prompting() {
		t.Fatal("d must open the confirm prompt")
	}
	if m.prompt.kind != promptConfirm || !strings.Contains(m.prompt.title, "b.txt") {
		t.Fatalf("prompt = %+v", m.prompt)
	}
	if _, ok := m.promptAnchorRow(); !ok {
		t.Fatal("the dialog must anchor on the section row (#1884)")
	}
	mm, cmd := m.Update(tea.KeyPressMsg{Code: 'y', Text: "y"})
	m = mm
	if filepath.Base(removed) != "b.txt" {
		t.Fatalf("removed = %q want b.txt", removed)
	}
	del, ok := runCmd(t, cmd).(FileDeletedMsg)
	if !ok || filepath.Base(del.Path) != "b.txt" || del.IsDir {
		t.Fatalf("FileDeletedMsg = %#v", del)
	}
	if len(m.ScratchEntries()) != 1 {
		t.Fatalf("section must refresh after delete, rows = %d", len(m.ScratchEntries()))
	}
}

// TestScratchDeleteCancelKeepsFile: any key but y/enter cancels the confirm.
func TestScratchDeleteCancelKeepsFile(t *testing.T) {
	m, _ := scratchModel(t, 1, "a.txt")
	called := false
	m.SetScratchOps(func(string) error { called = true; return nil }, nil)
	m = press(m, "G")
	mm, _ := m.Update(DeleteMsg{})
	m = mm
	mm, cmd := m.Update(tea.KeyPressMsg{Code: 'n', Text: "n"})
	m = mm
	if called || cmd != nil || m.Prompting() {
		t.Fatalf("cancel must not delete (called=%v)", called)
	}
}

// TestScratchRenameFlow: R opens the prefilled rename prompt with the stem
// preselected; accepting renames through the store seam, announces
// FileMovedMsg (open tabs re-point) and keeps the cursor on the entry.
func TestScratchRenameFlow(t *testing.T) {
	m, entries := scratchModel(t, 1, "a.txt", "b.txt")
	var gotOld, gotName string
	m.SetScratchOps(nil, func(p, name string) (string, error) {
		gotOld, gotName = p, name
		np := filepath.Join(filepath.Dir(p), name)
		for i, e := range *entries {
			if e.Path == p {
				(*entries)[i].Path = np
			}
		}
		return np, nil
	})
	m = press(m, "G")
	m = press(m, "k") // a.txt
	mm, _ := m.Update(RenameMsg{})
	m = mm
	if !m.Prompting() || m.prompt.kind != promptInput || m.prompt.input != "a.txt" {
		t.Fatalf("prompt = %+v", m.prompt)
	}
	if m.prompt.selStart != 0 || m.prompt.selEnd != 1 {
		t.Fatalf("stem preselect = [%d,%d) want [0,1)", m.prompt.selStart, m.prompt.selEnd)
	}
	// Type a replacement stem, accept.
	mm, _ = m.Update(tea.KeyPressMsg{Code: 'z', Text: "z"})
	m = mm
	mm, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	m = mm
	if filepath.Base(gotOld) != "a.txt" || gotName != "z.txt" {
		t.Fatalf("rename call = (%q, %q)", gotOld, gotName)
	}
	moved, ok := runCmd(t, cmd).(FileMovedMsg)
	if !ok || filepath.Base(moved.Old) != "a.txt" || filepath.Base(moved.New) != "z.txt" {
		t.Fatalf("FileMovedMsg = %#v", moved)
	}
	if e, ok := m.scratchSelected(); !ok || filepath.Base(e.Path) != "z.txt" {
		t.Fatalf("cursor must follow the renamed entry, sel = %+v", e)
	}
}

// TestScratchRenameErrorDialog: a store refusal opens the dismissable error
// dialog (#1030) instead of renaming.
func TestScratchRenameErrorDialog(t *testing.T) {
	m, _ := scratchModel(t, 1, "a.txt")
	m.SetScratchOps(nil, func(p, name string) (string, error) {
		return "", fmt.Errorf("not a valid scratch name: %s", name)
	})
	m = press(m, "G")
	mm, _ := m.Update(RenameMsg{})
	m = mm
	mm, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	m = mm
	if m.prompt == nil || m.prompt.kind != promptNotice {
		t.Fatalf("want the error dialog, prompt = %+v", m.prompt)
	}
}

// TestScratchNewDelegates: the explorer's new-file affordance on the section
// emits ScratchNewMsg (the scratch.new picker); new-folder is a no-op.
func TestScratchNewDelegates(t *testing.T) {
	m, _ := scratchModel(t, 1, "a.txt")
	m = press(m, "G")
	mm, cmd := m.Update(NewFileMsg{})
	m = mm
	if _, ok := runCmd(t, cmd).(ScratchNewMsg); !ok {
		t.Fatal("NewFileMsg in the section must emit ScratchNewMsg")
	}
	mm, cmd = m.Update(NewDirMsg{})
	m = mm
	if cmd != nil || m.Prompting() {
		t.Fatal("NewDirMsg in the section must be a no-op")
	}
}

// TestScratchDividerCollapseAndDrag: a divider click folds the section (the
// cursor returns to the tree), a second re-expands; a drag resizes the body
// and Snapshot/Restore round-trip both.
func TestScratchDividerCollapseAndDrag(t *testing.T) {
	m, _ := scratchModel(t, 2, "a.txt", "b.txt", "c.txt")
	m = press(m, "G")
	div := m.treeAreaRows()
	m, _ = m.MouseClick(3, div)
	if !m.ScratchCollapsed() || m.inScratch() {
		t.Fatalf("divider click must collapse and exit the section (collapsed=%v)", m.ScratchCollapsed())
	}
	if m.scratchAreaRows() != 1 {
		t.Fatalf("collapsed area = %d want 1 (divider only)", m.scratchAreaRows())
	}
	m, _ = m.MouseClick(3, m.treeAreaRows())
	if m.ScratchCollapsed() {
		t.Fatal("second click must re-expand")
	}
	// A drag: press, move the divider up so the body grows to 4 rows.
	if !m.ScratchDividerHit(3, m.treeAreaRows()) {
		t.Fatal("divider must hit-test")
	}
	m.ScratchDividerPress()
	m.ScratchDividerDrag(12 - 1 - 4)
	m.ScratchDividerRelease() // moved: must NOT toggle
	if m.ScratchCollapsed() {
		t.Fatal("a moved release must not toggle the collapse")
	}
	if m.ScratchHeight() != 4 {
		t.Fatalf("dragged height = %d want 4", m.ScratchHeight())
	}
	// Snapshot/Restore round-trip of collapse + height.
	m.ToggleScratchCollapsed()
	st := m.Snapshot()
	if !st.ScratchCollapsed || st.ScratchHeight != 4 {
		t.Fatalf("snapshot = %+v", st)
	}
	m2 := New(m.Root())
	m2.Restore(st)
	if !m2.scrCollapsed || m2.scrHeight != 4 {
		t.Fatalf("restore: collapsed=%v height=%d", m2.scrCollapsed, m2.scrHeight)
	}
}

// TestScratchSectionSetting: scratch.section = false removes the section
// entirely — no divider, no reserved rows, no cursor entry.
func TestScratchSectionSetting(t *testing.T) {
	m, _ := scratchModel(t, 1, "a.txt")
	m.Configure(host.MapConfig{"scratch.section": "false"})
	if m.scratchAreaRows() != 0 {
		t.Fatalf("disabled section still reserves %d rows", m.scratchAreaRows())
	}
	if strings.Contains(ansi.Strip(m.View()), "Scratches") {
		t.Fatal("disabled section must not render")
	}
	m = press(m, "G")
	if m.inScratch() {
		t.Fatal("G must stay in the tree with the section disabled")
	}
}

// TestScratchEmptySectionHint: with no scratches the section shows a hint
// row and the cursor cannot enter it.
func TestScratchEmptySectionHint(t *testing.T) {
	m, _ := scratchModel(t, 1)
	if !strings.Contains(ansi.Strip(m.View()), "(no scratches)") {
		t.Fatalf("empty section must hint:\n%s", ansi.Strip(m.View()))
	}
	m = press(m, "G")
	if m.inScratch() {
		t.Fatal("the cursor must not enter an empty section")
	}
}

// TestScratchHeightConfig: scratch.section_height seeds the body height, and
// an unchanged config value does not clobber a runtime drag (#629 pattern).
func TestScratchHeightConfig(t *testing.T) {
	m, _ := scratchModel(t, 1, "a.txt", "b.txt", "c.txt", "d.txt")
	m.Configure(host.MapConfig{"scratch.section_height": "2"})
	if m.scratchBodyRows() != 2 {
		t.Fatalf("configured body = %d want 2", m.scratchBodyRows())
	}
	m.ScratchDividerPress()
	m.ScratchDividerDrag(12 - 1 - 3) // drag to 3 rows
	m.ScratchDividerRelease()
	m.Configure(host.MapConfig{"scratch.section_height": "2"}) // unrelated reload
	if m.ScratchHeight() != 3 {
		t.Fatalf("an unchanged reload clobbered the drag: height = %d", m.ScratchHeight())
	}
}

// TestScratchTreeOpsUntouched: with the cursor in the tree, delete still
// opens the tree's confirm prompt naming the tree entry — the section
// intercepts only its own rows.
func TestScratchTreeOpsUntouched(t *testing.T) {
	m, _ := scratchModel(t, 1, "a.txt")
	m = press(m, "j") // f00.txt in the tree
	mm, _ := m.Update(DeleteMsg{})
	m = mm
	if !m.Prompting() || !strings.Contains(m.prompt.title, "f00.txt") {
		t.Fatalf("tree delete prompt = %+v", m.prompt)
	}
}

// TestScratchListerError: a failing lister shows its error in the section
// instead of rows, and the pane keeps working.
func TestScratchListerError(t *testing.T) {
	m := navModel(t, 1)
	m.SetSize(30, 12)
	m.EnableScratches("", func() ([]scratch.Entry, error) {
		return nil, os.ErrPermission
	})
	if !strings.Contains(ansi.Strip(m.View()), "permission") {
		t.Fatalf("lister error must render:\n%s", ansi.Strip(m.View()))
	}
}

// scratchNames builds n numbered scratch file names, "s00.txt" upward.
func scratchNames(n int) []string {
	out := make([]string, n)
	for i := range out {
		out[i] = fmt.Sprintf("s%02d.txt", i)
	}
	return out
}

// sectionLines returns the rendered section body rows (everything below the
// divider), stripped of styling.
func sectionLines(m Model) []string {
	lines := strings.Split(ansi.Strip(m.View()), "\n")
	for i, l := range lines {
		if strings.Contains(l, "Scratches") {
			return lines[i+1:]
		}
	}
	return nil
}

// TestScratchSectionWheelScroll (#1965): the wheel over the section scrolls
// the section — not the tree — and clamps at both ends.
func TestScratchSectionWheelScroll(t *testing.T) {
	m, _ := scratchModel(t, 2, scratchNames(12)...)
	body := m.scratchBodyRows()
	if body >= len(m.ScratchEntries()) {
		t.Fatalf("the fixture must overflow the section: body = %d of %d rows", body, len(m.ScratchEntries()))
	}
	row := m.treeAreaRows() + 1 // the section's first body row
	m.ScrollAt(row, 3)
	if m.ScratchTop() != 3 {
		t.Fatalf("wheel down: top = %d want 3", m.ScratchTop())
	}
	if got := sectionLines(m); !strings.Contains(got[0], "s03.txt") {
		t.Fatalf("scrolled section must start at s03.txt:\n%s", strings.Join(got, "\n"))
	}
	m.ScrollAt(row, 99) // clamps to the last window
	if want := len(m.ScratchEntries()) - body; m.ScratchTop() != want {
		t.Fatalf("wheel past the end: top = %d want %d", m.ScratchTop(), want)
	}
	if got := sectionLines(m); !strings.Contains(got[body-1], "s11.txt") {
		t.Fatalf("the last window must end at s11.txt:\n%s", strings.Join(got, "\n"))
	}
	m.ScrollAt(row, -99)
	if m.ScratchTop() != 0 {
		t.Fatalf("wheel past the top: top = %d want 0", m.ScratchTop())
	}
	// The cursor never moves with the wheel — the tree's contract (#1036).
	if m.inScratch() {
		t.Fatal("the wheel must not move the unified cursor into the section")
	}
}

// TestScratchWheelOverTreeLeavesSection (#1965): the wheel above the divider
// keeps scrolling the tree, so the routing only claims the section's rows.
func TestScratchWheelOverTreeLeavesSection(t *testing.T) {
	m, _ := scratchModel(t, 2, scratchNames(12)...)
	m.ScrollAt(m.treeAreaRows()+1, 4)
	if m.ScratchTop() != 4 {
		t.Fatalf("setup: top = %d want 4", m.ScratchTop())
	}
	m.ScrollAt(0, 3)                // over the tree
	m.ScrollAt(m.treeAreaRows(), 3) // the divider row itself belongs to the tree side
	if m.ScratchTop() != 4 {
		t.Fatalf("a tree wheel moved the section: top = %d want 4", m.ScratchTop())
	}
	// A collapsed section has no body, so every row routes to the tree.
	m.ToggleScratchCollapsed()
	m.ScrollAt(m.height-1, 3)
	if m.ScratchTop() != 4 {
		t.Fatalf("a collapsed section must not scroll: top = %d want 4", m.ScratchTop())
	}
}

// TestScratchCursorScrollsSection (#1965): walking the unified cursor past
// the section's last visible row scrolls the window with it.
func TestScratchCursorScrollsSection(t *testing.T) {
	m, _ := scratchModel(t, 2, scratchNames(12)...)
	m = press(m, "G") // the last scratch row
	if got := m.ScratchCursor(); got != 11 {
		t.Fatalf("G must land on the last scratch, cursor = %d", got)
	}
	if want := 12 - m.scratchBodyRows(); m.ScratchTop() != want {
		t.Fatalf("the section must scroll to the cursor: top = %d want %d", m.ScratchTop(), want)
	}
	lines := sectionLines(m)
	if !strings.Contains(lines[len(lines)-1], "s11.txt") {
		t.Fatalf("the cursor row must be visible:\n%s", strings.Join(lines, "\n"))
	}
	for i := 0; i < 11; i++ {
		m = press(m, "k")
	}
	if m.ScratchCursor() != 0 || m.ScratchTop() != 0 {
		t.Fatalf("walking back must scroll home: cursor = %d top = %d", m.ScratchCursor(), m.ScratchTop())
	}
}

// TestScratchLastOpenedColumn (#1965): each row carries a right-aligned
// relative age — the MRU store's last-opened time where the app pushed one
// in, the file's mtime otherwise.
func TestScratchLastOpenedColumn(t *testing.T) {
	now := time.Date(2026, 8, 19, 12, 0, 0, 0, time.UTC)
	m, entries := scratchModel(t, 1, "a.txt", "b.txt")
	m.now = func() time.Time { return now }
	(*entries)[0].ModTime = now.Add(-7 * 24 * time.Hour)
	(*entries)[1].ModTime = now.Add(-3 * time.Hour)
	m.RefreshScratches()
	rows := sectionLines(m)
	if !strings.HasPrefix(rows[0], "  a.txt") || !strings.HasSuffix(rows[0], "7d") {
		t.Fatalf("a.txt row = %q want the name left and 7d right", rows[0])
	}
	if !strings.HasSuffix(rows[1], "3h") {
		t.Fatalf("b.txt row = %q want 3h right-aligned", rows[1])
	}
	if got := ansi.StringWidth(rows[0]); got != 30 {
		t.Fatalf("row width = %d want the full pane width 30 (%q)", got, rows[0])
	}
	// The store's last-opened time wins over the mtime.
	m.SetScratchOpened(map[string]time.Time{
		filepath.Join("/scratches", "a.txt"): now.Add(-5 * time.Minute),
	})
	rows = sectionLines(m)
	if !strings.HasSuffix(rows[0], "5m") {
		t.Fatalf("last-opened row = %q want 5m", rows[0])
	}
	if !strings.HasSuffix(rows[1], "3h") {
		t.Fatalf("an unknown path must keep the mtime age: %q", rows[1])
	}
}

// TestScratchLastOpenedNarrowPane (#1965): too narrow for both columns, the
// age is dropped rather than squeezing the name out of legibility.
func TestScratchLastOpenedNarrowPane(t *testing.T) {
	now := time.Date(2026, 8, 19, 12, 0, 0, 0, time.UTC)
	m, entries := scratchModel(t, 1, "a-long-scratch-name.txt")
	m.now = func() time.Time { return now }
	(*entries)[0].ModTime = now.Add(-3 * time.Hour)
	m.RefreshScratches()
	m.SetSize(12, 12)
	row := sectionLines(m)[0]
	if strings.Contains(row, "3h") {
		t.Fatalf("a 12-column pane must drop the age column: %q", row)
	}
	// Wide enough again: the name clips and the age keeps its place.
	m.SetSize(20, 12)
	row = sectionLines(m)[0]
	if !strings.HasSuffix(row, "3h") || !strings.Contains(row, "…") {
		t.Fatalf("row = %q want a clipped name and a right-aligned 3h", row)
	}
	if got := ansi.StringWidth(row); got != 20 {
		t.Fatalf("row width = %d want 20 (%q)", got, row)
	}
}
