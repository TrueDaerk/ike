package app

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"ike/internal/config"
	"ike/internal/keymap"
)

// whichKeyConfigured installs a [keymap] which-key configuration for the test
// and restores the previous one afterwards.
func whichKeyConfigured(t *testing.T, on bool, delayMs int) {
	t.Helper()
	old := config.Get()
	t.Cleanup(func() { config.Set(old) })
	c := *old
	c.Keymap.WhichKey = on
	c.Keymap.WhichKeyDelayMs = delayMs
	config.Set(&c)
}

// pendPrefix feeds the cmd+k prefix (normalized ctrl+k on linux tables, raw
// meta on darwin — Feed normalizes) so the resolver holds a bare prefix. The
// popup itself waits for the which-key delay (#1909).
func pendPrefix(t *testing.T, m Model) Model {
	t.Helper()
	cmd, handled := m.resolveKeymap(keymap.Key{Base: "k", Mods: keymap.ModMeta})
	if !handled || cmd == nil {
		t.Fatalf("cmd+k must pend and arm the timeout")
	}
	if !m.keys.Pending() {
		t.Fatalf("cmd+k must leave the resolver pending")
	}
	return m
}

// elapseWhichKeyDelay delivers the delay message the pending sequence armed.
func elapseWhichKeyDelay(t *testing.T, m Model) Model {
	t.Helper()
	out, _ := m.Update(whichKeyDelayMsg{gen: m.whichKeyGen})
	return out.(Model)
}

// pendPrefixShown pends cmd+k and lets the which-key delay elapse, so the
// popup is up.
func pendPrefixShown(t *testing.T, m Model) Model {
	t.Helper()
	m = pendPrefix(t, m)
	m = elapseWhichKeyDelay(t, m)
	if len(m.whichKey) == 0 {
		t.Fatalf("which-key rows must be populated once the delay elapsed")
	}
	return m
}

func TestWhichKeyWaitsForTheDelay(t *testing.T) {
	whichKeyConfigured(t, true, 300)
	m := sized(t, 120, 40)
	m = pendPrefix(t, m)
	if len(m.whichKey) != 0 {
		t.Fatalf("the popup must stay closed until the delay elapses, got %v", m.whichKey)
	}
	m = elapseWhichKeyDelay(t, m)
	if len(m.whichKey) == 0 {
		t.Fatalf("the delay must open the popup while the prefix pends")
	}
	// The first row names the held prefix, the rest are continuations.
	if len(m.whichKey) < 2 {
		t.Fatalf("the popup must list continuations, got %v", m.whichKey)
	}
}

func TestWhichKeyStaleDelayIgnored(t *testing.T) {
	whichKeyConfigured(t, true, 300)
	m := sized(t, 120, 40)
	m = pendPrefix(t, m)
	stale := m.whichKeyGen
	// The sequence completes before the delay fires: cmd+k z resolves and
	// the in-flight timer must not open a popup afterwards.
	// cmd+k z completes the sequence (pane.maximize); whether its command is
	// registered in the test model decides only whether the key falls through.
	m.resolveKeymap(keymap.Key{Base: "z"})
	if m.keys.Pending() {
		t.Fatalf("the sequence must be complete")
	}
	out, _ := m.Update(whichKeyDelayMsg{gen: stale})
	m = out.(Model)
	if len(m.whichKey) != 0 {
		t.Fatalf("a sequence completed before the delay must not flash a popup, got %v", m.whichKey)
	}
}

func TestWhichKeyDisabled(t *testing.T) {
	whichKeyConfigured(t, false, 0)
	m := sized(t, 120, 40)
	m = pendPrefix(t, m)
	out, _ := m.Update(whichKeyDelayMsg{gen: m.whichKeyGen})
	m = out.(Model)
	if len(m.whichKey) != 0 {
		t.Fatalf("keymap.which_key = false must keep the popup closed, got %v", m.whichKey)
	}
	if !m.keys.Pending() {
		t.Fatalf("the chord itself must still pend with hints off")
	}
}

func TestWhichKeyZeroDelayShowsAtOnce(t *testing.T) {
	whichKeyConfigured(t, true, 0)
	m := sized(t, 120, 40)
	m = pendPrefix(t, m)
	if len(m.whichKey) == 0 {
		t.Fatalf("a zero delay must open the popup with the pending key")
	}
}

func TestWhichKeySurvivesTimeout(t *testing.T) {
	whichKeyConfigured(t, true, 300)
	m := sized(t, 120, 40)
	m = pendPrefixShown(t, m)
	// The 600 ms timer fires: a bare prefix (cmd+k has no exact binding)
	// keeps the popup and the pending chord (#1482).
	out, _ := m.Update(keymapTimeoutMsg{})
	m = out.(Model)
	if len(m.whichKey) == 0 {
		t.Fatalf("which-key popup must survive the timeout for a bare prefix")
	}
	if !m.keys.Pending() {
		t.Fatalf("the pending chord must survive the timeout")
	}
}

func TestWhichKeyDismissedByClick(t *testing.T) {
	whichKeyConfigured(t, true, 300)
	m := sized(t, 120, 40)
	m = pendPrefixShown(t, m)
	out, _ := m.Update(keymapTimeoutMsg{})
	m = out.(Model)
	out, _ = m.Update(tea.MouseClickMsg{X: 10, Y: 10, Button: tea.MouseLeft})
	m = out.(Model)
	if len(m.whichKey) != 0 {
		t.Fatalf("a mouse click must close the which-key popup")
	}
	if m.keys.Pending() {
		t.Fatalf("a mouse click must reset the pending chord")
	}
}

func TestWhichKeyDismissedByNonMatchingKey(t *testing.T) {
	whichKeyConfigured(t, true, 300)
	m := sized(t, 120, 40)
	m = pendPrefixShown(t, m)
	out, _ := m.Update(keymapTimeoutMsg{})
	m = out.(Model)
	// "z" matches no continuation of the cmd+k family: Feed dead-ends, the
	// popup closes and the key falls through.
	if _, handled := m.resolveKeymap(keymap.Key{Base: "z"}); handled {
		t.Fatalf("z must fall through, not resolve")
	}
	if len(m.whichKey) != 0 {
		t.Fatalf("a non-matching key must close the which-key popup")
	}
	if m.keys.Pending() {
		t.Fatalf("a non-matching key must reset the pending chord")
	}
}

func TestWhichKeyEscCancelsSequence(t *testing.T) {
	whichKeyConfigured(t, true, 300)
	m := sized(t, 120, 40)
	m = pendPrefixShown(t, m)
	// Esc abandons the sequence and is consumed (#1909) — cancelling a chord
	// must not double as an esc for the focused pane.
	if _, handled := m.resolveKeymap(keymap.Key{Base: "esc"}); !handled {
		t.Fatalf("esc must be consumed while a sequence is pending")
	}
	if len(m.whichKey) != 0 {
		t.Fatalf("esc must close the which-key popup")
	}
	if m.keys.Pending() {
		t.Fatalf("esc must cancel the pending chord")
	}
	// With nothing pending, esc goes back to falling through.
	if _, handled := m.resolveKeymap(keymap.Key{Base: "esc"}); handled {
		t.Fatalf("esc must fall through when no sequence is pending")
	}
}

func TestWhichKeyClosesOnCompletion(t *testing.T) {
	whichKeyConfigured(t, true, 300)
	m := sized(t, 120, 40)
	m = pendPrefixShown(t, m)
	// cmd+k z completes the sequence (pane.maximize); whether its command is
	// registered in the test model decides only whether the key falls through.
	m.resolveKeymap(keymap.Key{Base: "z"})
	if len(m.whichKey) != 0 {
		t.Fatalf("a completed sequence must close the popup, got %v", m.whichKey)
	}
}

func TestWhichKeyNarrowingUpdatesImmediately(t *testing.T) {
	whichKeyConfigured(t, true, 300)
	m := sized(t, 120, 40)
	// A three-step user chord (cmd+k g s / cmd+k g b) so the second step
	// narrows instead of completing.
	table := keymap.BuildTable(keymap.Defaults(keymap.PresetJetBrains), map[string]string{
		"cmd+k g s": "editor.save",
		"cmd+k g b": "editor.saveAll",
	}, keymap.GOOS)
	m.keys = keymap.NewResolver(table)
	m = pendPrefixShown(t, m)
	if !containsRow(m.whichKey, "g") {
		t.Fatalf("the popup must offer the g continuation, got %v", m.whichKey)
	}
	// The narrowing key refreshes the rows with the key press — the popup is
	// already up, so it does not wait for the delay again.
	if _, handled := m.resolveKeymap(keymap.Key{Base: "g"}); !handled {
		t.Fatalf("cmd+k g must pend")
	}
	if !containsRow(m.whichKey, "s") || !containsRow(m.whichKey, "b") {
		t.Fatalf("the narrowed popup must list s and b, got %v", m.whichKey)
	}
	if containsRow(m.whichKey, "g") {
		t.Fatalf("the narrowed popup must drop the consumed step, got %v", m.whichKey)
	}
}

// containsRow reports whether a which-key row starts with the given key label.
func containsRow(rows []string, key string) bool {
	for _, r := range rows[min(1, len(rows)):] { // row 0 is the held prefix
		if strings.HasPrefix(r, key+" ") {
			return true
		}
	}
	return false
}
