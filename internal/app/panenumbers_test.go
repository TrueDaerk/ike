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

	"ike/internal/host"
	"ike/internal/layout"
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

// splitOrderApp builds the nested layout the ordering test asserts on:
// explorer left, and a right-hand column split into two rows below a second
// editor column — four panes whose tree walk order and reading order differ.
func splitOrderApp(t *testing.T) Model {
	t.Helper()
	m := numberedApp(t, host.MapConfig{})
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

	off := numberedApp(t, host.MapConfig{"layout.pane_numbers": "off"})
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
	m := numberedApp(t, host.MapConfig{"layout.pane_numbers": "focus-only"})
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
	m := dismissOnboarding(newSized())
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
