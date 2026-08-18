package editor

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

// labeljump_test.go pins the label-jump motion (#787): visible-match
// collection, conflict-free label assignment (incl. two-character labels past
// the alphabet), the jump itself, and cancel behavior.

func TestLabelJumpCollectsVisibleMatches(t *testing.T) {
	// "fo" occurs at (0,0) — under the cursor, skipped — (1,4), (2,0), (3,0).
	m, _ := loaded(t, "foo one\ntwo foo three\nfour\nfoo five\n")
	m = typeKeys(m, "gsfo")
	if m.leap == nil {
		t.Fatal("no live session after gsfo")
	}
	if m.wait != awaitLabel {
		t.Fatalf("wait = %v, want awaitLabel", m.wait)
	}
	want := []struct{ line, col int }{{1, 4}, {2, 0}, {3, 0}}
	if len(m.leap.targets) != len(want) {
		t.Fatalf("targets = %+v, want %d", m.leap.targets, len(want))
	}
	for i, w := range want {
		if p := m.leap.targets[i].pos; p.Line != w.line || p.Col != w.col {
			t.Errorf("target %d = %d:%d, want %d:%d", i, p.Line, p.Col, w.line, w.col)
		}
	}
	// The continuations of the matches ('o' of foo, 'u' of four) may not be
	// labels — a key must never be ambiguous between narrowing and picking.
	for _, tg := range m.leap.targets {
		if strings.ContainsAny(tg.label, "ou") {
			t.Errorf("label %q collides with a query continuation", tg.label)
		}
	}
	// The nearest target gets the alphabet's first free key.
	if m.leap.targets[0].label != "a" {
		t.Errorf("nearest label = %q, want %q", m.leap.targets[0].label, "a")
	}
}

func TestLabelJumpLabelMovesCaret(t *testing.T) {
	m, _ := loaded(t, "foo one\ntwo foo three\nfour\nfoo five\n")
	m = typeKeys(m, "gsfo")
	second := m.leap.targets[1]
	m = typeKeys(m, second.label)
	if l, c := m.CursorPos(); l != second.pos.Line || c != second.pos.Col {
		t.Fatalf("cursor = %d:%d, want %d:%d", l, c, second.pos.Line, second.pos.Col)
	}
	if m.leap != nil || m.wait != awaitNone {
		t.Fatal("session must end on a label pick")
	}
}

func TestLabelJumpAutojumpOnUniqueMatch(t *testing.T) {
	m, _ := loaded(t, "alpha\nbeta gamma\n")
	m = typeKeys(m, "gsg")
	if m.leap != nil {
		t.Fatal("unique match must autojump without labels")
	}
	if l, c := m.CursorPos(); l != 1 || c != 5 {
		t.Fatalf("cursor = %d:%d, want 1:5", l, c)
	}
}

func TestLabelJumpSecondCharNarrows(t *testing.T) {
	// After "g" both targets continue with 'a' / 'u', so those runes are
	// excluded from the labels and typing 'a' narrows to the unique "ga".
	m, _ := loaded(t, "xgax\nxgux\n")
	m = typeKeys(m, "gsg")
	if m.leap == nil || len(m.leap.targets) != 2 {
		t.Fatalf("targets after gsg = %+v", m.leap)
	}
	for _, tg := range m.leap.targets {
		if strings.ContainsAny(tg.label, "au") {
			t.Errorf("label %q collides with a query continuation", tg.label)
		}
	}
	m = typeKeys(m, "a")
	if m.leap != nil {
		t.Fatal("narrowed unique match must autojump")
	}
	if l, c := m.CursorPos(); l != 0 || c != 1 {
		t.Fatalf("cursor = %d:%d, want 0:1", l, c)
	}
}

func TestLabelJumpEscCancels(t *testing.T) {
	m, _ := loaded(t, "foo one\ntwo foo three\n")
	m = typeKeys(m, "jll") // park the cursor away from the origin
	wantL, wantC := m.CursorPos()
	m = typeKeys(m, "gsfo")
	if !m.Capturing() {
		t.Fatal("a live session must capture keys from the app layer")
	}
	m = send(m, special(tea.KeyEscape))
	if m.leap != nil || m.wait != awaitNone {
		t.Fatal("esc must drop the session")
	}
	if m.Capturing() {
		t.Fatal("capture must end with the session")
	}
	if m.cmdMsg != "" {
		t.Fatalf("cmdMsg = %q, want empty", m.cmdMsg)
	}
	if l, c := m.CursorPos(); l != wantL || c != wantC {
		t.Fatalf("cursor = %d:%d, want %d:%d (unchanged)", l, c, wantL, wantC)
	}
}

func TestLabelJumpNoMatchCancels(t *testing.T) {
	m, _ := loaded(t, "foo one\ntwo foo three\n")
	m = typeKeys(m, "gsz")
	if m.leap != nil || m.wait != awaitNone {
		t.Fatal("a query without matches must cancel the session")
	}
	if !strings.Contains(m.cmdMsg, "no match") {
		t.Fatalf("cmdMsg = %q, want a no-match notice", m.cmdMsg)
	}
	if l, c := m.CursorPos(); l != 0 || c != 0 {
		t.Fatalf("cursor = %d:%d, want 0:0", l, c)
	}
}

func TestLabelJumpPendingOperatorCancels(t *testing.T) {
	m, _ := loaded(t, "foo one\ntwo foo three\n")
	m = typeKeys(m, "dgs")
	if m.leap != nil {
		t.Fatal("gs must not start a session under a pending operator")
	}
	if m.pending.HasOperator() {
		t.Fatal("the pending operator must cancel")
	}
	if line(m, 0) != "foo one" {
		t.Fatalf("buffer changed: %q", line(m, 0))
	}
}

func TestLeapLabelsPrefixFreeOverflow(t *testing.T) {
	alpha := []rune(labelAlphabet)
	labels := leapLabels(alpha, 40)
	if len(labels) != 40 {
		t.Fatalf("labels = %d, want 40", len(labels))
	}
	seen := map[string]bool{}
	singles := map[string]bool{}
	for _, l := range labels {
		if seen[l] {
			t.Fatalf("duplicate label %q", l)
		}
		seen[l] = true
		if len(l) == 1 {
			singles[l] = true
		}
	}
	for _, l := range labels {
		if len(l) == 2 && singles[string(l[0])] {
			t.Fatalf("label set not prefix-free: %q shadows single %q", l, string(l[0]))
		}
	}
	// One reserved prefix suffices for 40: 25 singles + pairs, home row first.
	if labels[0] != "a" || labels[25] != "ma" {
		t.Fatalf("labels[0]=%q labels[25]=%q, want a / ma", labels[0], labels[25])
	}
}

func TestLabelJumpTwoCharLabelPick(t *testing.T) {
	// Six lines with six "qq" each: 36 matches, minus the one under the
	// cursor — more than the 26-key alphabet, so the tail gets pair labels.
	row := "qq qq qq qq qq qq"
	m, _ := loaded(t, strings.Repeat(row+"\n", 6))
	m = typeKeys(m, "gsqq")
	if m.leap == nil || len(m.leap.targets) != 35 {
		t.Fatalf("targets = %v", m.leap)
	}
	var pair leapTarget
	for _, tg := range m.leap.targets {
		if tg.label == "" {
			t.Fatalf("target %v unlabeled", tg.pos)
		}
		if len(tg.label) == 2 {
			pair = tg
			break
		}
	}
	if pair.label == "" {
		t.Fatal("no two-character label assigned")
	}
	m = typeKeys(m, string(pair.label[0]))
	if m.leap == nil || m.leap.prefix != rune(pair.label[0]) {
		t.Fatalf("prefix not armed after %q", string(pair.label[0]))
	}
	// With the prefix typed, only its targets keep an overlay — showing the
	// remaining key.
	if r, ok := m.leapLabelAt(pair.pos.Line, pair.pos.Col); !ok || r != rune(pair.label[1]) {
		t.Fatalf("overlay at target = %q %v, want %q", string(r), ok, string(pair.label[1]))
	}
	if _, ok := m.leapLabelAt(m.leap.targets[0].pos.Line, m.leap.targets[0].pos.Col); ok {
		t.Fatal("single-label target must lose its overlay under a foreign prefix")
	}
	m = typeKeys(m, string(pair.label[1]))
	if l, c := m.CursorPos(); l != pair.pos.Line || c != pair.pos.Col {
		t.Fatalf("cursor = %d:%d, want %d:%d", l, c, pair.pos.Line, pair.pos.Col)
	}
}

func TestLabelJumpViewOverlaysLabels(t *testing.T) {
	// Buffer text avoids the letters a/s so the overlays are attributable:
	// the label replaces the match's first cell ("fo" renders as "ao"/"so").
	m, _ := loaded(t, "xxx foo\nyyy foo\n")
	m = typeKeys(m, "gsfo")
	rows := strings.Split(ansi.Strip(m.View()), "\n")
	var first, second string
	for _, r := range rows {
		if strings.Contains(r, "xxx") {
			first = r
		}
		if strings.Contains(r, "yyy") {
			second = r
		}
	}
	if !strings.Contains(first, "ao") {
		t.Fatalf("row 0 lacks the a label: %q", first)
	}
	if !strings.Contains(second, "so") {
		t.Fatalf("row 1 lacks the s label: %q", second)
	}
	m = send(m, special(tea.KeyEscape))
	rows = strings.Split(ansi.Strip(m.View()), "\n")
	for _, r := range rows {
		if strings.Contains(r, "ao") || strings.Contains(r, "so") {
			t.Fatalf("labels survive the cancel: %q", r)
		}
	}
}

func TestLabelJumpOffscreenExcluded(t *testing.T) {
	// 40 lines of "needle …": only the visible window (height 20 at loaded's
	// SetSize) collects targets — off-screen lines get none.
	m, _ := loaded(t, strings.Repeat("needle line\n", 40))
	m = typeKeys(m, "gsne")
	if m.leap == nil {
		t.Fatal("no live session")
	}
	bottom := m.view.Bottom(m.buf.LineCount())
	for _, tg := range m.leap.targets {
		if tg.pos.Line >= bottom {
			t.Fatalf("off-screen target at line %d (bottom %d)", tg.pos.Line, bottom)
		}
	}
}

func TestLabelJumpEmitsJumpEvent(t *testing.T) {
	// The landing records in the navigation history: the departure position
	// is emitted as an EventJump, like a search landing (nav_events_test.go).
	jumps := jumpEvents(t, "gsmas")
	if len(jumps) != 1 || jumps[0].Line != 0 || jumps[0].Col != 0 {
		t.Fatalf("jumps = %+v, want one departure from 0:0", jumps)
	}
	// A canceled session leaves no history entry (two matches, so the "ma"
	// query holds the session open instead of autojumping).
	m, _ := loaded(t, "zero\nthree match\nnine match\n")
	var count int
	m.SetEmitter(EmitterFunc(func(e Event) {
		if e.Kind == EventJump {
			count++
		}
	}))
	m = typeKeys(m, "gsma")
	m = send(m, special(tea.KeyEscape))
	if count != 0 {
		t.Fatalf("cancel emitted %d jump events", count)
	}
}

func TestLabelJumpActionStartsSession(t *testing.T) {
	m, _ := loaded(t, "foo one\ntwo foo three\nfoo four\n")
	m, _ = m.Update(ActionMsg{Action: "label_jump"})
	if m.leap == nil || m.wait != awaitLabel {
		t.Fatal("label_jump action must open a session")
	}
	m = typeKeys(m, "fo")
	if m.leap == nil || len(m.leap.targets) != 3 {
		t.Fatalf("targets = %v", m.leap)
	}
}
