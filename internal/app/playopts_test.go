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

// rtManifestApp is the yq round-trip fixture (#2798): a commented, anchored
// manifest in the encoder's own layout.
const rtManifestApp = "# api deployment\nkind: Deployment # the kind\nmetadata:\n  name: api\n  labels: # inline\n    app: api\nspec:\n  replicas: 1 # scale me\n  template: &tpl\n    image: 'alpine:3'\n  other: *tpl\n"

// TestYQRoundTripToggle is the acceptance case (#2798): with the round-trip
// on, an assignment keeps every comment, anchor and the key order; off, the
// plain serializer drops them. The chip follows the three flags on the yq
// info row, and the status line names the new state.
func TestYQRoundTripToggle(t *testing.T) {
	m := openYQ(t, yqApp(t, rtManifestApp))
	m = setProgram(m, ".spec.replicas = 3")
	if got := m.play.result.Text(); strings.Contains(got, "#") || strings.Contains(got, "&tpl") {
		t.Fatalf("plain result keeps comments or anchors: %q", got)
	}
	if row := playInfoRowPlain(m); !strings.HasPrefix(row, "-r -c -s rt Input:") {
		t.Fatalf("yq info row = %q, want the rt chip after the flags", row)
	}
	m = toggleOpt(m, PlayOptRoundTrip)
	if !m.play.opts.RoundTrip {
		t.Fatalf("opts = %+v, want the round-trip on", m.play.opts)
	}
	want := strings.TrimRight(strings.Replace(rtManifestApp, "replicas: 1", "replicas: 3", 1), "\n")
	if got := m.play.result.Text(); got != want {
		t.Errorf("round-trip result = \n%s\nwant\n%s", got, want)
	}
	if !strings.Contains(m.play.status, "round-trip output (rt) on") {
		t.Errorf("status = %q", m.play.status)
	}
	if note := m.play.result.Note(); note != "" {
		t.Errorf("note = %q, want none", note)
	}
	m = toggleOpt(m, PlayOptRoundTrip)
	if got := m.play.result.Text(); strings.Contains(got, "#") {
		t.Errorf("round-trip off still keeps comments: %q", got)
	}
}

// TestYQRoundTripFallbackNote: a reshaping program renders plainly and the
// info row says why; the result is the plain form, never a crash.
func TestYQRoundTripFallbackNote(t *testing.T) {
	m := openYQ(t, yqApp(t, rtManifestApp))
	m = toggleOpt(m, PlayOptRoundTrip)
	m.play.status = "" // the toggle's status line, cleared by the next keystroke
	m = setProgram(m, "to_entries")
	if m.play.result.Err != "" {
		t.Fatalf("unexpected error %q", m.play.result.Err)
	}
	if got := m.play.result.Text(); !strings.HasPrefix(got, "- key: kind") {
		t.Errorf("fallback result = %q, want the plain form", got)
	}
	row := func() string { return ansi.Strip(m.playInfoRow(200)) }
	if got := row(); !strings.Contains(got, "round-trip skipped: the program reshaped the document") {
		t.Errorf("info row = %q, want the fallback note", got)
	}
	m = setProgram(m, ".kind, .metadata")
	if got := row(); !strings.Contains(got, "round-trip skipped: the program produced several outputs") {
		t.Errorf("info row = %q, want the several-outputs note", got)
	}
	// Back to an edit, the note goes.
	m = setProgram(m, "del(.metadata.labels)")
	if got := row(); strings.Contains(got, "round-trip skipped") {
		t.Errorf("info row = %q, want no note", got)
	}
	if got := m.play.result.Text(); strings.Contains(got, "labels") || !strings.Contains(got, "# scale me") {
		t.Errorf("delete = %q, want the mapping gone and the other comments kept", got)
	}
}

// TestYQRoundTripChipClickAndChord: the rt chip toggles on a click, the
// default ctrl+alt+shift+y reaches the command from the query line, and the
// chip beside it still toggles its own flag.
func TestYQRoundTripChipClickAndChord(t *testing.T) {
	m := openYQ(t, yqApp(t, rtManifestApp))
	if m.onboardingOpen() {
		m = m.closeOnboarding().(Model)
	}
	m = setProgram(m, ".spec.replicas = 3")
	r := m.lay.Panes[m.play.paneKey]
	y := r.Y + m.contentYOff(m.play.paneKey) + m.playInfoRowY()
	click := func(x int) {
		tm, cmd := m.Update(tea.MouseClickMsg{X: r.X + paneContentX + x, Y: y, Button: tea.MouseLeft})
		m = drainCmd(tm.(Model), cmd)
	}
	click(9) // the `rt` chip
	if !m.play.opts.RoundTrip || !strings.Contains(m.play.result.Text(), "# scale me") {
		t.Fatalf("click on rt: opts = %+v result %q", m.play.opts, m.play.result.Text())
	}
	click(6) // `-s`
	if !m.play.opts.Slurp || !m.play.opts.RoundTrip {
		t.Fatalf("click on -s: opts = %+v", m.play.opts)
	}
	click(6)
	m = drainKey(m, tea.KeyPressMsg{Code: 'y', Mod: tea.ModCtrl | tea.ModAlt | tea.ModShift})
	if m.play.opts.RoundTrip {
		t.Fatalf("after ctrl+alt+shift+y opts = %+v, want the round-trip off (status %q)", m.play.opts, m.play.status)
	}
	if m.play.program.Text != ".spec.replicas = 3" {
		t.Errorf("a chord leaked into the query line: %q", m.play.program.Text)
	}
}

// TestRoundTripIsYQOnly: the jq playground shows no rt chip and the command
// leaves its options alone.
func TestRoundTripIsYQOnly(t *testing.T) {
	m := openJQ(t, playApp(t, `{"name":"alice"}`))
	m = setProgram(m, ".name")
	if row := playInfoRowPlain(m); !strings.HasPrefix(row, "-r -c -s Input:") {
		t.Fatalf("jq info row = %q, want the three chips only", row)
	}
	m = toggleOpt(m, PlayOptRoundTrip)
	if m.play.opts != (jqplay.Options{}) {
		t.Errorf("jq opts = %+v, want untouched", m.play.opts)
	}
}

// TestYQRoundTripRememberedPerSession: reopening over the same file restores
// the toggle with the last program within the session; the persisted flags
// do not carry it.
func TestYQRoundTripRememberedPerSession(t *testing.T) {
	m := openYQ(t, yqApp(t, rtManifestApp))
	m = setProgram(m, ".spec.replicas = 3")
	m = toggleOpt(m, PlayOptRoundTrip)
	m.closePlayground()
	m = openYQ(t, m)
	if m.play.program.Text != ".spec.replicas = 3" || !m.play.opts.RoundTrip {
		t.Fatalf("reopen: program %q opts %+v, want the round-trip on", m.play.program.Text, m.play.opts)
	}
	if got := m.play.result.Text(); !strings.Contains(got, "# scale me") {
		t.Errorf("reopened result = %q, want the round-trip form", got)
	}
	if got := m.playLastStoreOf().Flags(m.play.srcKey); got != "" {
		t.Errorf("persisted flags = %q, want none — the round-trip is per session", got)
	}
}
