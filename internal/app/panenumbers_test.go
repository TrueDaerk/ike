package app

import (
	"image/color"
	"runtime"
	"strconv"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"ike/internal/config"
	"ike/internal/host"
	"ike/internal/layout"
	"ike/internal/pane"
	"ike/internal/registry"
)

// panenumbers_test.go covers the pane numbers and the focus-by-number
// commands (#2407): the reading order across nested splits, the badge in the
// chrome, the three modes of layout.pane_numbers, and the commands (chord,
// message and prompt) that address a number.

// numberedApp is a sized app on cfg, so a test can pick the pane-number mode.
func numberedApp(t *testing.T, cfg host.Config) Model {
	t.Helper()
	t.Setenv("IKE_CONFIG_DIR", t.TempDir())
	reg := registry.New()
	reg.Add(appCommands{})
	m := NewWith(reg, cfg)
	out, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	m = out.(Model)
	// The first-start LSP dialog owns the keyboard while it is up (#301) and
	// would swallow the keys these tests type; esc is what a user presses too.
	return dismissOnboarding(m)
}

// dismissOnboarding closes the first-start LSP dialog if this machine offers
// one, so a scripted key reaches the pane prompt under test.
func dismissOnboarding(m Model) Model {
	for m.onboardingOpen() {
		tm, _ := m.updateOnboarding(tea.KeyPressMsg{Code: tea.KeyEscape})
		m = tm.(Model)
	}
	return m
}

// noSlots empties layout.pane_slots (#2592): with no tool window holding a
// reserved number the panes are numbered 1…N in plain reading order, which is
// what the geometry, badge and prompt tests are about. The reserved numbering
// has its own tests below.
func noSlots() host.MapConfig { return host.MapConfig{"layout.pane_slots": ""} }

// splitOrderApp builds the nested layout the ordering test asserts on:
// explorer left, and a right-hand column split into two rows below a second
// editor column — four panes whose tree walk order and reading order differ.
func splitOrderApp(t *testing.T) Model {
	t.Helper()
	m := numberedApp(t, noSlots())
	m.SplitFocused(layout.ZoneRight)  // explorer | A | B
	m.SplitFocused(layout.ZoneBottom) // …with B split into B (top) and C (bottom)
	m.layout()
	return m
}

// TestPaneNumberOrderIsReadingOrder: the numbers run left-to-right,
// top-to-bottom over the computed rectangles, across nested splits.
func TestPaneNumberOrderIsReadingOrder(t *testing.T) {
	m := splitOrderApp(t)
	order := m.paneNumberOrder()
	if len(order) != 4 {
		t.Fatalf("panes = %d (%v), want 4", len(order), order)
	}
	for i := 1; i < len(order); i++ {
		a, b := m.lay.Panes[order[i-1]], m.lay.Panes[order[i]]
		if b.Y < a.Y || (b.Y == a.Y && b.X < a.X) {
			t.Fatalf("pane %d at (%d,%d) precedes pane %d at (%d,%d): not reading order",
				i, a.X, a.Y, i+1, b.X, b.Y)
		}
	}
	for i, key := range order {
		if got := m.paneNumberOf(key); got != i+1 {
			t.Errorf("paneNumberOf(%s) = %d, want %d", key, got, i+1)
		}
	}
}

// TestPaneNumbersFollowLayoutChanges: closing a pane renumbers the survivors
// on the spot — the numbering is derived from the live layout, never cached.
func TestPaneNumbersFollowLayoutChanges(t *testing.T) {
	m := splitOrderApp(t)
	before := m.paneNumberOrder()
	last := before[len(before)-1]
	m.closeKey(last)
	m.layout()
	after := m.paneNumberOrder()
	if len(after) != len(before)-1 {
		t.Fatalf("after close: %d panes, want %d", len(after), len(before)-1)
	}
	for _, k := range after {
		if k == last {
			t.Fatal("the closed pane still carries a number")
		}
	}
	if n := m.paneNumberOf(last); n != 0 {
		t.Errorf("closed pane numbered %d, want none", n)
	}
	for i, key := range after {
		if got := m.paneNumberOf(key); got != i+1 {
			t.Errorf("after close paneNumberOf(%s) = %d, want %d", key, got, i+1)
		}
	}
}

// TestPaneNumberBadgeInChrome: every visible pane draws its number in the
// title bar as the inverted pill (#2496) matching its focus state, and the
// badge disappears with layout.pane_numbers = off.
func TestPaneNumberBadgeInChrome(t *testing.T) {
	m := splitOrderApp(t)
	for i, key := range m.paneNumberOrder() {
		box := m.renderPane(key, m.lay.Panes[key])
		focused := m.activeWS().Panes.Focused() == key
		want := paneNumberBadge(" "+string(rune('0'+i+1))+" ", focused, m.pal())
		if !strings.Contains(box, want) {
			t.Errorf("pane %s chrome has no %d pill (focused=%v):\n%s", key, i+1, focused, box)
		}
		if got := lipgloss.Width(m.paneNumberBadgeText(key)); got != paneNumberBadgeWidth {
			t.Errorf("badge width for %s = %d, want %d", key, got, paneNumberBadgeWidth)
		}
	}

	off := numberedApp(t, host.MapConfig{"layout.pane_numbers": "off", "layout.pane_slots": ""})
	off.SplitFocused(layout.ZoneRight)
	off.layout()
	for _, key := range off.paneNumberOrder() {
		if off.paneNumberBadgeText(key) != "" {
			t.Errorf("pane_numbers = off still drew a badge for %s", key)
		}
		box := off.renderPane(key, off.lay.Panes[key])
		for _, n := range []string{" 1 ", " 2 "} {
			for _, focused := range []bool{true, false} {
				if strings.Contains(box, paneNumberBadge(n, focused, off.pal())) {
					t.Errorf("pane_numbers = off still drew a pill for %s:\n%s", key, box)
				}
			}
		}
	}
}

// TestPaneNumberBadgeGap: one plain cell separates the pill from a plain
// title, and a tab bar — whose segments open with their own padding space —
// gets no extra one, so both kinds of title sit the same distance from the
// badge.
func TestPaneNumberBadgeGap(t *testing.T) {
	m := splitOrderApp(t)
	pal := m.pal()
	badge := paneNumberBadge(" 1 ", true, pal)
	plain := ansi.Strip(paneBox(badge, "⚙ CLAUDE", "", 30, 3, pal.Border))
	if !strings.Contains(plain, " 1  ⚙ CLAUDE") {
		t.Errorf("plain title is glued to the badge:\n%s", plain)
	}
	bar := renderTabBar([]string{"⚙ lazygit", "b.go"}, 0, 20, pal)
	tabs := ansi.Strip(paneBox(badge, bar, "", 30, 3, pal.Border))
	if !strings.Contains(tabs, " 1  ⚙ lazygit ") || strings.Contains(tabs, " 1   ⚙") {
		t.Errorf("tab bar is not exactly one cell from the badge:\n%s", tabs)
	}
	none := ansi.Strip(paneBox("", "⚙ CLAUDE", "", 30, 3, pal.Border))
	if !strings.Contains(none, "│ ⚙ CLAUDE") {
		t.Errorf("a badge-less title grew a separator:\n%s", none)
	}
}

// TestPaneNumberBadgeIsInverted: the focused pane's pill uses the accent
// slots, the others the muted pair, and both differ from the border colour the
// badge used to borrow — the dim digit was the bug (#2496).
func TestPaneNumberBadgeIsInverted(t *testing.T) {
	m := splitOrderApp(t)
	pal := m.pal()
	if got := paneNumberBadge(" 1 ", true, pal); !strings.Contains(got, ansiOf(pal.PaneBadge)) {
		t.Errorf("focused pill %q does not paint the accent badge background", got)
	}
	if got := paneNumberBadge(" 2 ", false, pal); !strings.Contains(got, ansiOf(pal.PaneBadgeMuted)) {
		t.Errorf("unfocused pill %q does not paint the muted badge background", got)
	}
	if paneNumberBadge("", true, pal) != "" {
		t.Error("an empty badge must render nothing at all")
	}
}

// ansiOf renders c as the ANSI parameters lipgloss writes for it, so a test can
// assert which palette slot a styled string was painted with.
func ansiOf(c color.Color) string {
	r, g, b, _ := c.RGBA()
	return strconv.Itoa(int(r>>8)) + ";" + strconv.Itoa(int(g>>8)) + ";" + strconv.Itoa(int(b>>8))
}

// TestPaneNumbersFocusOnlyFollowsTheHint: in focus-only mode the badges are
// hidden until a pane switch raises the which-pane hint, and the hint's own
// timer message takes them down again.
func TestPaneNumbersFocusOnlyFollowsTheHint(t *testing.T) {
	m := numberedApp(t, host.MapConfig{"layout.pane_numbers": "focus-only", "layout.pane_slots": ""})
	m.SplitFocused(layout.ZoneRight)
	m.layout()
	if m.paneNumbersShown() {
		t.Fatal("focus-only should hide the numbers while no switch is happening")
	}
	tm, cmd := m.Update(CyclePaneFocusMsg{})
	m = tm.(Model)
	if !m.paneNumbersShown() {
		t.Fatal("a pane switch should raise the which-pane hint")
	}
	if cmd == nil {
		t.Fatal("the hint should come with the command that takes it down again")
	}
	// The hint's expiry message ends it; a stale generation does not.
	tm, _ = m.Update(paneNumberHintMsg{gen: m.paneNumHintGen - 1})
	if !tm.(Model).paneNumbersShown() {
		t.Error("an outrun hint timer must not take the badges down")
	}
	tm, _ = m.Update(paneNumberHintMsg{gen: m.paneNumHintGen})
	if tm.(Model).paneNumbersShown() {
		t.Error("the hint should expire with its own timer message")
	}
}

// TestPaneFocusIndexFocusesThatPane: pane.focus<n>'s message focuses the pane
// carrying that number, and an out-of-range number leaves focus alone.
func TestPaneFocusIndexFocusesThatPane(t *testing.T) {
	m := splitOrderApp(t)
	order := m.paneNumberOrder()
	for i, want := range order {
		tm, _ := m.Update(PaneFocusIndexMsg{Index: i + 1})
		m = tm.(Model)
		if got := m.activeWS().Panes.Focused(); got != want {
			t.Errorf("focus pane %d = %s, want %s", i+1, got, want)
		}
	}
	focused := m.activeWS().Panes.Focused()
	tm, _ := m.Update(PaneFocusIndexMsg{Index: len(order) + 1})
	m = tm.(Model)
	if got := m.activeWS().Panes.Focused(); got != focused {
		t.Errorf("out-of-range number moved focus to %s", got)
	}
}

// TestPaneFocusChordFocusesThatPane guards the ctrl+digit defaults: the chord
// must resolve through the keymap layer to pane.focus<n>. macOS only — off
// macOS the Cmd→Ctrl fold owns these chords, and the prompt is the doorway.
func TestPaneFocusChordFocusesThatPane(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("ctrl+digit pane focus ships on macOS only (#2407)")
	}
	m := numberedApp(t, noSlots())
	m.SplitFocused(layout.ZoneRight)
	m.layout()
	order := m.paneNumberOrder()
	if len(order) < 2 {
		t.Fatalf("precondition: %d panes, want at least 2", len(order))
	}
	m = drainKey(m, tea.KeyPressMsg{Code: '1', Text: "1", Mod: tea.ModCtrl})
	if got := m.activeWS().Panes.Focused(); got != order[0] {
		t.Errorf("ctrl+1 focused %s, want %s", got, order[0])
	}
	m = drainKey(m, tea.KeyPressMsg{Code: '2', Text: "2", Mod: tea.ModCtrl})
	if got := m.activeWS().Panes.Focused(); got != order[1] {
		t.Errorf("ctrl+2 focused %s, want %s", got, order[1])
	}
}

// TestPaneFocusByIndexPrompt: the palette flavour opens a shell prompt and
// focuses the typed number on enter.
func TestPaneFocusByIndexPrompt(t *testing.T) {
	m := splitOrderApp(t)
	order := m.paneNumberOrder()
	tm, _ := m.Update(PaneFocusByIndexMsg{})
	m = tm.(Model)
	if !m.paneNumPromptOpen() {
		t.Fatal("pane.focusByIndex should open the pane-number prompt")
	}
	for _, k := range []tea.KeyPressMsg{{Code: '3', Text: "3"}, {Code: tea.KeyEnter}} {
		tm, _ = m.Update(k)
		m = tm.(Model)
	}
	if m.paneNumPromptOpen() {
		t.Error("enter should close the prompt")
	}
	if got := m.activeWS().Panes.Focused(); got != order[2] {
		t.Errorf("prompt focused %s, want pane 3 (%s)", got, order[2])
	}
}

// TestTabBarHitStartsAfterTheBadge: the tab bar is rendered into what the
// pane-number pill leaves of the title row, so a click must be resolved
// against the same origin — a cell inside the pill is not tab 0 (#2496).
func TestTabBarHitStartsAfterTheBadge(t *testing.T) {
	dir := t.TempDir()
	m := openApp(t, writeTemp(t, dir, "a.txt", "a\n"), writeTemp(t, dir, "b.txt", "b\n"))
	m.layout()
	key := m.activeWS().Panes.Focused()
	r, ok := m.lay.Panes[key]
	if !ok {
		t.Fatal("focused pane has no rect")
	}
	if m.paneNumberBadgeText(key) == "" {
		t.Fatal("precondition: the focused pane should carry a badge")
	}
	y := r.Y + 1
	for dx := 0; dx < paneNumberBadgeWidth; dx++ {
		if _, _, _, hit := m.tabBarHit(r.X+paneContentX+dx, y); hit {
			t.Errorf("cell %d of the pill must not resolve to a tab", dx)
		}
	}
	gotKey, idx, _, hit := m.tabBarHit(r.X+paneContentX+paneNumberBadgeWidth+1, y)
	if !hit || gotKey != key || idx != 0 {
		t.Errorf("the first bar cell after the pill = (%q, %d, hit=%v), want (%q, 0, true)", gotKey, idx, hit, key)
	}
}

// --- Reserved pane numbers (#2592) ---------------------------------------
//
// The numbers below are pinned by layout.pane_slots: the explorer is always 1,
// an assigned tool always carries its number — open or closed — and the
// document panes take what is left above the highest reserved one.

// slotApp is a sized app whose reserved-number table is exactly slots. The
// short toast timeout keeps drainCmd from running a four-second expiry tick
// inline when a chord notifies.
func slotApp(t *testing.T, slots string) Model {
	t.Helper()
	return numberedApp(t, host.MapConfig{
		"layout.pane_slots":             slots,
		"notifications.timeout_seconds": "1",
	})
}

// TestReservedNumbersIgnoreTheEditorCount: with vcs=3 the explorer is 1, the
// VCS window is 3 whether zero, one or three editors are open, and the editors
// start at 4 — the number after the highest reserved one.
func TestReservedNumbersIgnoreTheEditorCount(t *testing.T) {
	m := slotApp(t, "vcs=3")
	vcs := focusToolWindow(t, &m, toolWindowKinds()["vcs"])
	editor := m.activeEditorKey()
	if editor == "" {
		t.Fatal("precondition: the default layout should include an editor pane")
	}
	check := func(what string, editors int) {
		t.Helper()
		if got := m.paneNumberOf(pane.ExplorerKey); got != 1 {
			t.Errorf("%s: explorer numbered %d, want 1", what, got)
		}
		if got := m.paneNumberOf(vcs); got != 3 {
			t.Errorf("%s: VCS numbered %d, want its reserved 3", what, got)
		}
		var docs []int
		for _, key := range m.paneNumberOrder() {
			if key == vcs || key == pane.ExplorerKey {
				continue
			}
			docs = append(docs, m.paneNumberOf(key))
		}
		if len(docs) != editors {
			t.Fatalf("%s: %d document panes, want %d", what, len(docs), editors)
		}
		for i, n := range docs {
			if want := 4 + i; n != want {
				t.Errorf("%s: document pane %d numbered %d, want %d", what, i, n, want)
			}
		}
	}
	check("one editor", 1)

	m.setFocus(editor)
	m.SplitFocused(layout.ZoneRight)
	m.SplitFocused(layout.ZoneRight)
	m.layout()
	check("three editors", 3)

	for _, key := range m.paneNumberOrder() {
		if key != vcs && key != pane.ExplorerKey {
			m.closeKey(key)
		}
	}
	m.layout()
	check("no editors", 0)
}

// TestReservedNumberGapStaysWhenTheToolCloses: closing an assigned tool leaves
// every other number exactly where it was — the reserved number becomes a gap
// rather than shifting the panes below it up.
func TestReservedNumberGapStaysWhenTheToolCloses(t *testing.T) {
	m := slotApp(t, "vcs=3,problems=4")
	vcs := focusToolWindow(t, &m, toolWindowKinds()["vcs"])
	problems := focusToolWindow(t, &m, toolWindowKinds()["problems"])
	before := map[string]int{}
	for _, key := range m.paneNumberOrder() {
		before[key] = m.paneNumberOf(key)
	}
	if before[vcs] != 3 || before[problems] != 4 {
		t.Fatalf("precondition: vcs=%d problems=%d, want 3 and 4", before[vcs], before[problems])
	}
	m.closeKey(problems)
	m.layout()
	for key, want := range before {
		if key == problems {
			continue
		}
		if got := m.paneNumberOf(key); got != want {
			t.Errorf("after closing Problems, %s numbered %d, want an unchanged %d", key, got, want)
		}
	}
	if got := m.paneNumberOf(problems); got != 0 {
		t.Errorf("the closed Problems pane still carries %d", got)
	}
}

// TestReservedChordOpensAClosedTool: the chord on a reserved number whose tool
// is not open opens it through the tool's own toggle route and focuses it —
// the number addresses the tool, not merely a pane that happens to exist.
func TestReservedChordOpensAClosedTool(t *testing.T) {
	m := slotApp(t, "problems=3")
	if m.activeWS().Panes.Has(pane.ProblemsKey) {
		t.Fatal("precondition: the Problems window should start closed")
	}
	tm, cmd := m.Update(PaneFocusIndexMsg{Index: 3})
	m = drainCmd(tm.(Model), cmd)
	m.layout()
	if !m.activeWS().Panes.Has(pane.ProblemsKey) {
		t.Fatal("the reserved chord did not open the Problems window")
	}
	if got := m.activeWS().Panes.Focused(); got != pane.ProblemsKey {
		t.Errorf("the reserved chord focused %s, want the Problems window", got)
	}
	if got := m.paneNumberOf(pane.ProblemsKey); got != 3 {
		t.Errorf("the opened Problems window is numbered %d, want its reserved 3", got)
	}
	// Pressing it again focuses the pane that is now open (and does not
	// toggle it away — the chord is "go there", not "toggle").
	m.setFocus(m.activeEditorKey())
	tm, cmd = m.Update(PaneFocusIndexMsg{Index: 3})
	m = drainCmd(tm.(Model), cmd)
	if got := m.activeWS().Panes.Focused(); got != pane.ProblemsKey {
		t.Errorf("the chord on the open window focused %s, want the Problems window", got)
	}
}

// TestExplorerIsAlwaysPaneOne: 1 belongs to the explorer whatever the table
// says, and the chord brings a hidden explorer back rather than dying.
func TestExplorerIsAlwaysPaneOne(t *testing.T) {
	m := slotApp(t, "vcs=3")
	if got := m.paneNumberOf(pane.ExplorerKey); got != 1 {
		t.Fatalf("explorer numbered %d, want 1", got)
	}
	m.setFocus(pane.ExplorerKey)
	m.toggleExplorer()
	m.layout()
	if m.explorerVisible() {
		t.Fatal("precondition: the explorer should be hidden")
	}
	for _, key := range m.paneNumberOrder() {
		if got := m.paneNumberOf(key); got == 1 {
			t.Errorf("%s took the explorer's 1 while the tree was hidden", key)
		}
	}
	tm, cmd := m.Update(PaneFocusIndexMsg{Index: 1})
	m = drainCmd(tm.(Model), cmd)
	m.layout()
	if !m.explorerVisible() {
		t.Fatal("ctrl+1 did not bring the hidden explorer back")
	}
	if got := m.activeWS().Panes.Focused(); got != pane.ExplorerKey {
		t.Errorf("ctrl+1 focused %s, want the explorer", got)
	}
}

// TestUnassignedToolIsNumberedLikeAnEditor: a tool with no entry in the table
// takes a flowing number after the reserved ones, in reading order, exactly
// like a document pane.
func TestUnassignedToolIsNumberedLikeAnEditor(t *testing.T) {
	m := slotApp(t, "vcs=3")
	problems := focusToolWindow(t, &m, toolWindowKinds()["problems"])
	n := m.paneNumberOf(problems)
	if n < 4 {
		t.Fatalf("the unassigned Problems window is numbered %d, want a flowing number past the reserved 3", n)
	}
	// It flows with the document panes: its number is the reading-order
	// position among the panes holding no reserved number.
	want := 4
	for _, key := range m.paneNumberOrder() {
		if key == pane.ExplorerKey {
			continue
		}
		if key == problems {
			break
		}
		want++
	}
	if n != want {
		t.Errorf("Problems numbered %d, want %d — its place in the flowing order", n, want)
	}
}

// TestReservedNumberWithoutAnOpenerNotifies: the two windows that have no
// toggle command of their own (the debug area, the HTTP viewer) keep their
// gap — the chord says so instead of silently doing nothing (#275).
func TestReservedNumberWithoutAnOpenerNotifies(t *testing.T) {
	m := numberedApp(t, host.MapConfig{
		"layout.pane_slots": "debug=3",
		// drainKey runs the toast's expiry tick inline; keep it short.
		"notifications.timeout_seconds": "1",
	})
	focused := m.activeWS().Panes.Focused()
	tm, cmd := m.Update(PaneFocusIndexMsg{Index: 3})
	m = drainCmd(tm.(Model), cmd)
	if got := m.activeWS().Panes.Focused(); got != focused {
		t.Errorf("the chord moved focus to %s, want no move", got)
	}
	if m.activeWS().Panes.Has(pane.DebugKey) {
		t.Error("the chord opened a debug area it has no command for")
	}
	if !notifiedAbout(m, "focus pane 3") {
		t.Errorf("the chord must notify, history = %v", m.history)
	}
}

// TestUnassignedNumberNotifies: a number no pane carries and no tool reserves
// is the pre-existing no-op with a notification.
func TestUnassignedNumberNotifies(t *testing.T) {
	m := numberedApp(t, host.MapConfig{
		"layout.pane_slots":             "vcs=3",
		"notifications.timeout_seconds": "1",
	})
	focused := m.activeWS().Panes.Focused()
	tm, cmd := m.Update(PaneFocusIndexMsg{Index: paneNumberMax})
	m = drainCmd(tm.(Model), cmd)
	if got := m.activeWS().Panes.Focused(); got != focused {
		t.Errorf("an unassigned number moved focus to %s", got)
	}
	if !notifiedAbout(m, "focus pane "+strconv.Itoa(paneNumberMax)) {
		t.Errorf("an unassigned number must notify, history = %v", m.history)
	}
}

// notifiedAbout reports whether text was toasted, live or in the history ring.
func notifiedAbout(m Model, text string) bool {
	for _, h := range m.history {
		if strings.Contains(h.text, text) {
			return true
		}
	}
	for _, n := range m.toasts {
		if strings.Contains(n.text, text) {
			return true
		}
	}
	return false
}

// TestReservedNumbersFollowTheBadge: the badge a tool draws is its reserved
// number, and pane.focusByIndex resolves the same table the chords do.
func TestReservedNumbersFollowTheBadge(t *testing.T) {
	m := slotApp(t, "vcs=3")
	vcs := focusToolWindow(t, &m, toolWindowKinds()["vcs"])
	if got := m.paneNumberBadgeText(vcs); got != " 3 " {
		t.Errorf("VCS badge = %q, want \" 3 \"", got)
	}
	m.setFocus(m.activeEditorKey())
	tm, _ := m.Update(PaneFocusByIndexMsg{})
	m = tm.(Model)
	for _, k := range []tea.KeyPressMsg{{Code: '3', Text: "3"}, {Code: tea.KeyEnter}} {
		tm, cmd := m.Update(k)
		m = drainCmd(tm.(Model), cmd)
	}
	if got := m.activeWS().Panes.Focused(); got != vcs {
		t.Errorf("Focus Pane by Number 3 focused %s, want the VCS window", got)
	}
}

// TestPaneSlotDefsCoverEveryConfigurableTool keeps the app's slot table and
// the config id list from drifting apart: every tool the settings form accepts
// must have a definition here (or the chord would address nothing), and no
// definition may name an id the form rejects. Only the debug area and the HTTP
// viewer may lack an opener — they have no toggle command of their own.
func TestPaneSlotDefsCoverEveryConfigurableTool(t *testing.T) {
	defs := map[string]paneSlotDef{}
	for _, d := range paneSlotDefs {
		defs[d.id] = d
	}
	if _, ok := defs[config.PaneSlotExplorer]; !ok {
		t.Error("the explorer must have a slot definition — it always carries 1")
	}
	for _, id := range config.PaneSlotTools() {
		d, ok := defs[id]
		if !ok {
			t.Errorf("layout.pane_slots accepts %q but no pane slot defines it", id)
			continue
		}
		if d.open == nil && id != "debug" && id != "http" {
			t.Errorf("%q has no opener: a reserved chord could not open it", id)
		}
	}
	if len(defs) != len(config.PaneSlotTools())+1 {
		t.Errorf("%d slot definitions, want the %d configurable tools plus the explorer",
			len(defs), len(config.PaneSlotTools()))
	}
}
