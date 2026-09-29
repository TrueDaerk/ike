package app

import (
	"os"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"ike/internal/jqplay"
)

// playopts_test.go covers the playground's -r / -c / -s toggles (#2784): the
// commands and their default chords, the info-row chips (render and click),
// the result form copy / open-as-scratch read, the per-source memory and the
// xmq no-op.

// toggleOpt dispatches one toggle command and drains its rerun.
func toggleOpt(m Model, o PlayOption) Model {
	tm, cmd := m.Update(TogglePlayOptionMsg{Option: o})
	return drainCmd(tm.(Model), cmd)
}

// playInfoRowPlain is the info row at the hosting pane's width, unstyled.
func playInfoRowPlain(m Model) string {
	r := m.lay.Panes[m.play.paneKey]
	return ansi.Strip(m.playInfoRow(paneInterior(r.W, paneChromeW)))
}

// playInfoLineText is the info row at width without its leading -r / -c / -s
// chips, unstyled: what the row says, for tests about the line proper. The
// width is the line's own — the chips' cells are added on top.
func playInfoLineText(m Model, width int) string {
	row := ansi.Strip(m.playInfoRow(width + playChipsW + 1))
	return strings.TrimPrefix(row, strings.Join(playChips[:], " ")+" ")
}

// TestPlayToggleRaw is the acceptance case: -r shows `alice`, off shows the
// quoted form again; the raw result is named `.txt`.
func TestPlayToggleRaw(t *testing.T) {
	m := openJQ(t, playApp(t, `{"name":"alice"}`))
	m = setProgram(m, ".name")
	if got := m.play.result.Text(); got != `"alice"` {
		t.Fatalf("default = %q", got)
	}
	m = toggleOpt(m, PlayOptRaw)
	if got := m.play.result.Text(); got != "alice" {
		t.Fatalf("raw = %q, want alice (status %q)", got, m.play.status)
	}
	if got := m.play.result.ResultPath(); !strings.HasSuffix(got, ".txt") {
		t.Errorf("raw result path = %q, want .txt", got)
	}
	if !strings.Contains(m.play.status, "raw output (-r) on") {
		t.Errorf("status = %q", m.play.status)
	}
	m = toggleOpt(m, PlayOptRaw)
	if got := m.play.result.Text(); got != `"alice"` {
		t.Fatalf("raw off = %q, want the quoted form", got)
	}
}

// TestPlayToggleCompactAndSlurp: -c puts each output on one line with no
// folds; -s runs a JSONL input as one array.
func TestPlayToggleCompactAndSlurp(t *testing.T) {
	m := openJQ(t, playApp(t, "{\"a\":{\"b\":1}}\n{\"a\":{\"b\":2}}\n"))
	m = setProgram(m, ".")
	if len(m.play.folds) == 0 {
		t.Fatal("the pretty result should fold")
	}
	m = toggleOpt(m, PlayOptCompact)
	if got := m.play.result.Text(); got != "{\"a\":{\"b\":1}}\n{\"a\":{\"b\":2}}" {
		t.Fatalf("compact = %q", got)
	}
	if len(m.play.folds) != 0 {
		t.Errorf("compact result still folds: %v", m.play.folds)
	}
	m = setProgram(m, "length")
	m = toggleOpt(m, PlayOptSlurp)
	if got := m.play.result.Text(); got != "2" {
		t.Fatalf("slurped length = %q, want 2", got)
	}
}

// TestPlayToggleChords: the default ctrl+alt+q / c / s reach the commands from
// the query line through the playground's Global fallback.
func TestPlayToggleChords(t *testing.T) {
	m := openJQ(t, playApp(t, `{"name":"alice"}`))
	if m.onboardingOpen() {
		m = m.closeOnboarding().(Model)
	}
	m = setProgram(m, ".name")
	for _, c := range []struct {
		key  rune
		want jqplay.Options
	}{
		{'q', jqplay.Options{Raw: true}},
		{'c', jqplay.Options{Raw: true, Compact: true}},
		{'s', jqplay.Options{Raw: true, Compact: true, Slurp: true}},
	} {
		m = drainKey(m, tea.KeyPressMsg{Code: c.key, Mod: tea.ModCtrl | tea.ModAlt})
		if m.play.opts != c.want {
			t.Fatalf("after ctrl+alt+%c opts = %+v, want %+v (status %q)", c.key, m.play.opts, c.want, m.play.status)
		}
	}
	if m.play.program.Text != ".name" {
		t.Errorf("a chord leaked into the query line: %q", m.play.program.Text)
	}
}

// TestPlayChipsRenderAndClick: the chips lead the info row, a click on one
// toggles it, and a click beside them does not.
func TestPlayChipsRenderAndClick(t *testing.T) {
	m := openJQ(t, playApp(t, `{"name":"alice"}`))
	m = setProgram(m, ".name")
	if row := playInfoRowPlain(m); !strings.HasPrefix(row, "-r -c -s Input:") {
		t.Fatalf("info row = %q, want the chips first", row)
	}
	// Active chips are highlighted: the raw one renders differently once on.
	before := m.playOptionChips()
	r := m.lay.Panes[m.play.paneKey]
	y := r.Y + m.contentYOff(m.play.paneKey) + m.playInfoRowY()
	click := func(x int) {
		tm, cmd := m.Update(tea.MouseClickMsg{X: r.X + paneContentX + x, Y: y, Button: tea.MouseLeft})
		m = drainCmd(tm.(Model), cmd)
	}
	click(3) // the `-c` chip
	if !m.play.opts.Compact || m.play.opts.Raw {
		t.Fatalf("click on -c: opts = %+v", m.play.opts)
	}
	click(0) // `-r`
	if !m.play.opts.Raw || m.play.result.Text() != "alice" {
		t.Fatalf("click on -r: opts = %+v result %q", m.play.opts, m.play.result.Text())
	}
	if m.playOptionChips() == before {
		t.Error("the active chips must render highlighted")
	}
	if !m.play.bufFocus && m.play.program.Text != ".name" {
		t.Errorf("a chip click must not touch the program: %q", m.play.program.Text)
	}
	click(2) // the gap between chips
	if m.play.opts != (jqplay.Options{Raw: true, Compact: true}) {
		t.Errorf("a click between chips toggled: %+v", m.play.opts)
	}
	click(7) // `-s`
	if !m.play.opts.Slurp {
		t.Errorf("click on -s: opts = %+v", m.play.opts)
	}
}

// TestPlayChipsHiddenForXMQ: xmq has no toggles — no chips, and the command
// leaves the options alone.
func TestPlayChipsHiddenForXMQ(t *testing.T) {
	fakeXMQOnPath(t)
	m := openXMQ(t, xmqApp(t, "xml", "<r/>\n"))
	if row := playInfoRowPlain(m); strings.Contains(row, "-r -c -s") {
		t.Fatalf("xmq info row shows chips: %q", row)
	}
	m = toggleOpt(m, PlayOptRaw)
	if m.play.opts != (jqplay.Options{}) {
		t.Errorf("xmq opts = %+v, want untouched", m.play.opts)
	}
}

// TestPlayTogglesCopyAndScratch: copy and open-as-scratch write the toggled
// form, the raw one into a `.txt` scratch.
func TestPlayTogglesCopyAndScratch(t *testing.T) {
	m := openJQ(t, playApp(t, `{"tags":["a","b"]}`))
	m = setProgram(m, ".tags[]")
	m = toggleOpt(m, PlayOptRaw)
	var copied string
	prev := clipboardWrite
	clipboardWrite = func(s string) { copied = s }
	t.Cleanup(func() { clipboardWrite = prev })
	m.copyPlayResult()
	if copied != "a\nb" {
		t.Errorf("copied %q, want the raw lines", copied)
	}
	tm, cmd := m.openPlayResultAsScratch()
	m = drainCmd(tm.(Model), cmd)
	ed := m.activeEditor()
	if ed == nil || !strings.HasSuffix(ed.Path(), ".txt") {
		t.Fatalf("scratch not opened as .txt: %v", ed)
	}
	data, err := os.ReadFile(ed.Path())
	if err != nil || string(data) != "a\nb\n" {
		t.Errorf("scratch = %q (%v), want the raw lines", data, err)
	}
}

// TestPlayTogglesRememberedWithLastProgram: reopening over the same file
// restores the last program together with the toggles it ran with, in the
// session and from the persisted store.
func TestPlayTogglesRememberedWithLastProgram(t *testing.T) {
	m := openJQ(t, playApp(t, `{"name":"alice"}`))
	m = setProgram(m, ".name")
	m = toggleOpt(m, PlayOptRaw)
	m.closePlayground()
	m = openJQ(t, m)
	if m.play.program.Text != ".name" || !m.play.opts.Raw {
		t.Fatalf("reopen: program %q opts %+v, want .name with -r", m.play.program.Text, m.play.opts)
	}
	if got := m.play.result.Text(); got != "alice" {
		t.Errorf("reopened result = %q, want the raw form", got)
	}
	key := m.play.srcKey
	if got := m.playLastStoreOf().Flags(key); got != "-r" {
		t.Errorf("persisted flags = %q, want -r", got)
	}
	// The persisted half alone (a restart) restores them too.
	m.closePlayground()
	delete(m.playLastProgram, key)
	delete(m.playLastOpts, key)
	m = openJQ(t, m)
	if !m.play.opts.Raw {
		t.Errorf("restart: opts = %+v, want -r from the store", m.play.opts)
	}
}
