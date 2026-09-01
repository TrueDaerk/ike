package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"ike/internal/testdata"
)

// scratch_generate_test.go covers the generator dialog (#2134, #2228, DSL
// rework #2392): the single command, the one-screen dialog, the DSL editor
// with autocomplete, the debounced live preview, templates, the mouse
// operation and the end-to-end path from ctrl+g to an opened scratch.

// tdKey feeds one key press through the dialog's update path.
func tdKey(m Model, code rune) Model { return drainKey(m, tea.KeyPressMsg{Code: code}) }

// tdCtrl feeds a ctrl chord (ctrl+g, ctrl+s, …).
func tdCtrl(m Model, code rune) Model {
	return drainKey(m, tea.KeyPressMsg{Code: code, Mod: tea.ModCtrl})
}

// tdType feeds one printable key with its text.
func tdType(m Model, r rune) Model {
	return drainKey(m, tea.KeyPressMsg{Code: r, Text: string(r)})
}

// tdTypeStr types a whole string into the dialog.
func tdTypeStr(m Model, s string) Model {
	for _, r := range s {
		m = tdType(m, r)
	}
	return m
}

// newGenSized builds a tall isolated model, so the whole dialog body fits the
// shell viewport and every button is clickable. The first-start LSP
// onboarding (#301) is dismissed when the environment opened it — it shares
// the shell and its key routing outranks the dialog's, so it would swallow
// every scripted key on a machine with a missing language server.
func newGenSized() Model {
	m := newSized()
	tm, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 46})
	m = tm.(Model)
	if m.onboardingOpen() {
		m = m.closeOnboarding().(Model)
	}
	return m
}

// openGenerate opens the dialog through the real command path. The preview
// debounce is shortened so drains render it inline.
func openGenerate(t *testing.T, m Model) Model {
	t.Helper()
	tdPreviewDebounce = time.Millisecond
	t.Cleanup(func() { tdPreviewDebounce = 250 * time.Millisecond })
	m = drainCmd(m, m.RunCommand("scratch.generate"))
	if !m.generateScratchOpen() {
		t.Fatal("scratch.generate must open the dialog")
	}
	return m
}

// tdHitAt finds the recorded hit region for (kind, arg) and returns its
// screen coordinates — the coordinates a real click at that region carries.
func tdHitAt(t *testing.T, m Model, kind tdHitKind, arg int) (x, y int) {
	t.Helper()
	v := m.shell.View()
	bx, by := (m.width-lipgloss.Width(v))/2, (m.height-lipgloss.Height(v))/2
	ox, oy := m.shell.ContentOrigin()
	for _, h := range m.tdGen.hits {
		if h.kind == kind && h.arg == arg {
			return bx + ox + h.x0, by + oy + h.y - m.shell.ScrollOffset()
		}
	}
	t.Fatalf("no hit region for kind %d arg %d", kind, arg)
	return 0, 0
}

// tdClick presses the left button at the hit region for (kind, arg).
func tdClick(t *testing.T, m Model, kind tdHitKind, arg int) Model {
	t.Helper()
	x, y := tdHitAt(t, m, kind, arg)
	out, cmd := m.handleMouse(mouseEvent{Mouse: tea.Mouse{X: x, Y: y, Button: tea.MouseLeft}, action: mousePress})
	return drainCmd(out.(Model), cmd)
}

// tdSetSpec loads a DSL body and header values straight into the open dialog
// state — for tests whose point is not the typing.
func tdSetSpec(m Model, dsl, rows, seed string) Model {
	s := m.tdGen
	s.setText(dsl)
	s.hdr[tdHdrRows], s.hdr[tdHdrSeed] = rows, seed
	s.setFocus(tdFocEditor)
	m.renderGenerateScratch()
	return m
}

// TestGenerateSingleCommand: only scratch.generate remains — the per-format
// quick commands are gone (#2392).
func TestGenerateSingleCommand(t *testing.T) {
	m := newSized()
	if _, ok := m.reg.Command("scratch.generate"); !ok {
		t.Fatal("scratch.generate must be registered")
	}
	for _, f := range testdata.Formats() {
		if _, ok := m.reg.Command("scratch.generate." + string(f)); ok {
			t.Fatalf("scratch.generate.%s must no longer exist", f)
		}
	}
}

// TestGenerateDialogSingleScreen: opening shows the whole dialog at once —
// header fields, the spec editor with the default DSL, and the live preview.
func TestGenerateDialogSingleScreen(t *testing.T) {
	m := openGenerate(t, newGenSized())
	view := plainView(m)
	for _, want := range []string{
		"Generate Test Data", "Template", "Format", "Rows", "Seed", "Table",
		"Spec", "first_name = first_name()", "Preview (CSV",
	} {
		if !strings.Contains(view, want) {
			t.Fatalf("dialog must show %q:\n%s", want, view)
		}
	}
	if m.tdGen.focus != tdFocEditor {
		t.Fatalf("focus = %d, want the editor", m.tdGen.focus)
	}
}

// TestGeneratePreviewMatchesRender: the preview pane holds exactly what a
// generation with the same seed renders for the first rows.
func TestGeneratePreviewMatchesRender(t *testing.T) {
	m := openGenerate(t, newGenSized())
	m = tdSetSpec(m, "id = id()\nname = first_name()", "50", "77")
	m = drainCmd(m, m.tdGen.dirtyPreview())
	spec, err := m.tdGen.compose()
	if err != nil {
		t.Fatalf("compose: %v", err)
	}
	want, err := testdata.Preview(spec)
	if err != nil {
		t.Fatalf("Preview: %v", err)
	}
	if m.tdGen.prevText != string(want) {
		t.Fatalf("preview = %q, want %q", m.tdGen.prevText, want)
	}
	if !strings.Contains(plainView(m), "id,name") {
		t.Fatalf("preview must be visible:\n%s", plainView(m))
	}
}

// TestGeneratePreviewShowsErrors: an invalid spec shows its line-numbered
// error where the preview would be, live.
func TestGeneratePreviewShowsErrors(t *testing.T) {
	m := openGenerate(t, newGenSized())
	m = tdSetSpec(m, "id = id()\nbad = wat()", "10", "1")
	m = drainCmd(m, m.tdGen.dirtyPreview())
	if m.tdGen.specErr == "" {
		t.Fatal("an invalid spec must set the preview error")
	}
	view := plainView(m)
	if !strings.Contains(view, `line 2`) || !strings.Contains(view, `unknown generator "wat"`) {
		t.Fatalf("the error with its line must be visible:\n%s", view)
	}
}

// TestGenerateEndToEnd drives the dialog to a generated, opened scratch and
// checks the file is byte-identical to a direct render of the same spec.
func TestGenerateEndToEnd(t *testing.T) {
	m := openGenerate(t, newGenSized())
	// Cycle the format picker to JSON.
	m.tdGen.setFocus(tdFocFormat)
	for m.tdGen.formats[m.tdGen.fmtPick] != testdata.FormatJSON {
		m = drainKey(m, tea.KeyPressMsg{Code: tea.KeyRight})
	}
	m = tdSetSpec(m, "id = id()\nname = first_name()", "5", "42")
	spec, err := m.tdGen.compose()
	if err != nil {
		t.Fatalf("compose: %v", err)
	}
	want, err := testdata.Render(spec)
	if err != nil {
		t.Fatalf("Render: %v", err)
	}

	m = tdCtrl(m, 'g')
	if m.generateScratchOpen() {
		t.Fatalf("generation must close the dialog: %s", m.tdGen.err)
	}
	ed := m.activeWS().Panes.FocusedInstance().Editor()
	path := ed.Path()
	if filepath.Ext(path) != ".json" {
		t.Fatalf("path = %q, want a .json scratch", path)
	}
	if !strings.Contains(path, "scratches") {
		t.Fatalf("path = %q, want it under the scratch store", path)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("generated scratch must exist: %v", err)
	}
	if string(data) != string(want) {
		t.Fatalf("generated file differs from Render:\n%s\n---\n%s", data, want)
	}
	if ed.Dirty() {
		t.Fatal("a generated scratch must open clean")
	}
}

// TestGenerateInvalidSpecBlocked: ctrl+g on an invalid spec keeps the dialog
// open with the offending line; fixing the spec generates.
func TestGenerateInvalidSpecBlocked(t *testing.T) {
	m := openGenerate(t, newGenSized())
	m = tdSetSpec(m, "id = id(\nx = uuid()", "5", "1")
	m = tdCtrl(m, 'g')
	if !m.generateScratchOpen() {
		t.Fatal("an invalid spec must keep the dialog open")
	}
	if !strings.Contains(m.tdGen.err, "line 1") || !strings.Contains(m.tdGen.err, "missing ')'") {
		t.Fatalf("err = %q, want the line-numbered parse error", m.tdGen.err)
	}
	if !strings.Contains(plainView(m), "missing ')'") {
		t.Fatalf("the error must be visible:\n%s", plainView(m))
	}
	m = tdSetSpec(m, "id = id()\nx = uuid()", "5", "1")
	m = tdCtrl(m, 'g')
	if m.generateScratchOpen() {
		t.Fatalf("a fixed spec must generate: %s", m.tdGen.err)
	}
}

// TestGenerateBadHeaderBlocked: the header knobs validate like the wizard's
// options step did.
func TestGenerateBadHeaderBlocked(t *testing.T) {
	cases := []struct{ rows, seed, want string }{
		{"0", "1", "at least 1"},
		{"lots", "1", "not a number"},
		{"1000001", "1", "at most"},
		{"5", "-1", "non-negative"},
	}
	for _, tc := range cases {
		m := openGenerate(t, newGenSized())
		m = tdSetSpec(m, "id = id()", tc.rows, tc.seed)
		m = tdCtrl(m, 'g')
		if !m.generateScratchOpen() {
			t.Fatalf("rows %q seed %q must not generate", tc.rows, tc.seed)
		}
		if !strings.Contains(m.tdGen.err, tc.want) {
			t.Fatalf("err = %q, want %q", m.tdGen.err, tc.want)
		}
		m = drainKey(m, tea.KeyPressMsg{Code: tea.KeyEscape})
	}
}

// TestGenerateEditorTyping: the editor is a real multi-line text area —
// typing, enter, backspace joins, cursor movement.
func TestGenerateEditorTyping(t *testing.T) {
	m := openGenerate(t, newGenSized())
	m = tdSetSpec(m, "id = id()", "5", "1")
	// Move to the end and add a line.
	m.tdGen.curL, m.tdGen.curC = 0, len("id = id()")
	m = drainKey(m, tea.KeyPressMsg{Code: tea.KeyEnter})
	m = tdTypeStr(m, "color = from_list(red, green)")
	if got := m.tdGen.text(); got != "id = id()\ncolor = from_list(red, green)" {
		t.Fatalf("text = %q", got)
	}
	// Backspace at column 0 joins lines.
	m.tdGen.curL, m.tdGen.curC = 1, 0
	m.tdGen.acOpen = false
	m = drainKey(m, tea.KeyPressMsg{Code: tea.KeyBackspace})
	if got := m.tdGen.text(); got != "id = id()color = from_list(red, green)" {
		t.Fatalf("join produced %q", got)
	}
	if m.tdGen.curL != 0 || m.tdGen.curC != len("id = id()") {
		t.Fatalf("cursor after join = (%d,%d)", m.tdGen.curL, m.tdGen.curC)
	}
}

// TestGenerateAutocompleteGenerators: typing a generator prefix pops the
// catalog with descriptions; tab accepts and lands inside the parens for a
// parameterized kind.
func TestGenerateAutocompleteGenerators(t *testing.T) {
	m := openGenerate(t, newGenSized())
	m = tdSetSpec(m, "", "5", "1")
	m = tdTypeStr(m, "name = fir")
	s := m.tdGen
	if !s.acOpen || len(s.acItems) == 0 {
		t.Fatal("typing a generator prefix must open the autocomplete")
	}
	if s.acItems[0].label != "first_name()" || s.acItems[0].desc != "first name" {
		t.Fatalf("suggestion = %+v, want first_name() with its description", s.acItems[0])
	}
	if !strings.Contains(plainView(m), "first name") {
		t.Fatalf("the suggestion list must be visible:\n%s", plainView(m))
	}
	m = drainKey(m, tea.KeyPressMsg{Code: tea.KeyTab})
	if got := s.lines[0]; got != "name = first_name()" {
		t.Fatalf("accepting must complete the call, got %q", got)
	}
	if s.acOpen {
		t.Fatal("accepting must close the popup")
	}

	// A parameterized kind shows its grammar and parks the cursor inside.
	m.tdGen.setText("n = in")
	m.tdGen.curL, m.tdGen.curC = 0, len("n = in")
	m.tdGen.computeAC(false)
	var intItem *tdACItem
	for i := range m.tdGen.acItems {
		if strings.HasPrefix(m.tdGen.acItems[i].label, "int(") {
			intItem = &m.tdGen.acItems[i]
			m.tdGen.acPick = i
		}
	}
	if intItem == nil || !strings.Contains(intItem.label, "min..max") {
		t.Fatalf("int suggestion must carry its parameter grammar: %+v", m.tdGen.acItems)
	}
	m = drainKey(m, tea.KeyPressMsg{Code: tea.KeyEnter})
	if got := m.tdGen.lines[0]; got != "n = int()" {
		t.Fatalf("accepting int must complete the call, got %q", got)
	}
	if m.tdGen.curC != len("n = int(") {
		t.Fatalf("cursor = %d, want inside the parens", m.tdGen.curC)
	}
	// weighted is offered alongside the catalog.
	m.tdGen.setText("w = weigh")
	m.tdGen.curL, m.tdGen.curC = 0, len("w = weigh")
	m.tdGen.computeAC(false)
	if len(m.tdGen.acItems) != 1 || !strings.HasPrefix(m.tdGen.acItems[0].label, "weighted(") {
		t.Fatalf("weighted must be suggested: %+v", m.tdGen.acItems)
	}
}

// TestGenerateAutocompleteRefs: "{" offers the fields defined above the
// cursor, and only those.
func TestGenerateAutocompleteRefs(t *testing.T) {
	m := openGenerate(t, newGenSized())
	m = tdSetSpec(m, "domain = domain()\ndown = uuid()", "5", "1")
	m.tdGen.curL, m.tdGen.curC = 1, 0
	// Start a new line between the two fields, then reference upward.
	m = tdTypeStr(m, "host = hostname(")
	m = drainKey(m, tea.KeyPressMsg{Code: tea.KeyEnter}) // splits: host line, rest below
	m.tdGen.curL, m.tdGen.curC = 1, len("host = hostname(")
	m = tdTypeStr(m, "{do")
	s := m.tdGen
	if !s.acOpen || len(s.acItems) != 1 {
		t.Fatalf("ref completion must offer exactly the field above, got %+v", s.acItems)
	}
	if s.acItems[0].label != "{domain}" {
		t.Fatalf("suggestion = %+v, want {domain} (down is defined below)", s.acItems[0])
	}
	m = drainKey(m, tea.KeyPressMsg{Code: tea.KeyEnter})
	if got := s.lines[1]; got != "host = hostname({domain}" {
		t.Fatalf("accepting must complete the reference, got %q", got)
	}
}

// TestGenerateTemplatesBuiltin: cycling the template picker loads a built-in
// body into the editor.
func TestGenerateTemplatesBuiltin(t *testing.T) {
	m := openGenerate(t, newGenSized())
	m.tdGen.setFocus(tdFocTemplate)
	m = drainKey(m, tea.KeyPressMsg{Code: tea.KeyRight})
	if got := m.tdGen.tplName(); got != "Person" {
		t.Fatalf("first template = %q, want Person", got)
	}
	if !strings.Contains(m.tdGen.text(), `full_name  = "{first_name} {last_name}"`) {
		t.Fatalf("picking Person must load its body:\n%s", m.tdGen.text())
	}
	// The URL / Web built-in from the issue exists and loads.
	for m.tdGen.tplName() != "URL / Web" {
		m = drainKey(m, tea.KeyPressMsg{Code: tea.KeyRight})
	}
	if !strings.Contains(m.tdGen.text(), `url    = "https://{host}/api/{id}"`) {
		t.Fatalf("URL / Web body:\n%s", m.tdGen.text())
	}
	// Editing does not write back into the template.
	m.tdGen.setFocus(tdFocEditor)
	m.tdGen.curL, m.tdGen.curC = 0, 0
	m = tdType(m, 'x')
	for _, tpl := range testdata.Templates() {
		if tpl.Name == "URL / Web" && strings.HasPrefix(tpl.DSL, "x") {
			t.Fatal("editing the spec must not change the template")
		}
	}
}

// TestGenerateTemplateSaveDelete: ctrl+s saves the spec under a typed name,
// the template persists, and ctrl+d deletes it again.
func TestGenerateTemplateSaveDelete(t *testing.T) {
	m := openGenerate(t, newGenSized())
	m = tdSetSpec(m, "thing = uuid()", "5", "1")
	m = tdCtrl(m, 's')
	if !m.tdGen.savePrompt {
		t.Fatal("ctrl+s must open the name prompt")
	}
	m = tdTypeStr(m, "My things")
	m = drainKey(m, tea.KeyPressMsg{Code: tea.KeyEnter})
	if m.tdGen.savePrompt {
		t.Fatalf("enter must save and close the prompt (err %q)", m.tdGen.err)
	}
	if m.tdGen.tplName() != "My things" {
		t.Fatalf("the saved template must be selected, got %q", m.tdGen.tplName())
	}
	found := false
	for _, tpl := range testdata.Templates() {
		if tpl.Name == "My things" && strings.Contains(tpl.DSL, "thing = uuid()") {
			found = true
		}
	}
	if !found {
		t.Fatal("the template must persist in the store")
	}
	// A name shadowing a built-in is refused with the dialog still open.
	m = tdCtrl(m, 's')
	m.tdGen.saveName = "Person"
	m = drainKey(m, tea.KeyPressMsg{Code: tea.KeyEnter})
	if !m.tdGen.savePrompt || !strings.Contains(m.tdGen.err, "built-in") {
		t.Fatalf("shadowing a built-in must be refused, err %q", m.tdGen.err)
	}
	m = drainKey(m, tea.KeyPressMsg{Code: tea.KeyEscape}) // close the prompt

	m = tdCtrl(m, 'd')
	for _, tpl := range testdata.Templates() {
		if tpl.Name == "My things" {
			t.Fatal("ctrl+d must delete the selected user template")
		}
	}
	if m.tdGen.tplPick != 0 {
		t.Fatalf("after delete the picker must fall back to custom, got %d", m.tdGen.tplPick)
	}
	// Deleting a built-in refuses.
	m.tdGen.setFocus(tdFocTemplate)
	m = drainKey(m, tea.KeyPressMsg{Code: tea.KeyRight}) // Person
	m = tdCtrl(m, 'd')
	if !strings.Contains(m.tdGen.err, "built-in") {
		t.Fatalf("deleting a built-in must refuse, err %q", m.tdGen.err)
	}
}

// TestGenerateMouse drives the dialog by clicks alone: focus a header field,
// cycle a picker, place the editor cursor, accept a suggestion, generate.
func TestGenerateMouse(t *testing.T) {
	m := openGenerate(t, newGenSized())
	// Click the Format row: focused; click again: cycles to TSV.
	m = tdClick(t, m, tdHitHeader, tdFocFormat)
	if m.tdGen.focus != tdFocFormat {
		t.Fatalf("focus = %d, want the format row", m.tdGen.focus)
	}
	m = tdClick(t, m, tdHitHeader, tdFocFormat)
	if m.tdGen.formats[m.tdGen.fmtPick] != testdata.FormatTSV {
		t.Fatalf("second click must cycle the format, got %v", m.tdGen.formats[m.tdGen.fmtPick])
	}
	// Click the Seed row to focus its input.
	m = tdClick(t, m, tdHitHeader, tdFocSeed)
	if m.tdGen.focus != tdFocSeed {
		t.Fatalf("focus = %d, want the seed row", m.tdGen.focus)
	}
	m.tdGen.hdr[tdHdrRows] = "3"
	// Click into the second editor line: cursor lands there.
	m = tdClick(t, m, tdHitLine, 1)
	if m.tdGen.focus != tdFocEditor || m.tdGen.curL != 1 {
		t.Fatalf("clicking a line must focus the editor there (focus %d, line %d)", m.tdGen.focus, m.tdGen.curL)
	}
	// Autocomplete rows are clickable.
	m.tdGen.setText("x = uu")
	m.tdGen.curL, m.tdGen.curC = 0, len("x = uu")
	m.tdGen.computeAC(false)
	m.renderGenerateScratch()
	if !m.tdGen.acOpen {
		t.Fatal("setup: the popup must be open")
	}
	m = tdClick(t, m, tdHitAC, 0)
	if got := m.tdGen.lines[0]; got != "x = uuid()" {
		t.Fatalf("clicking a suggestion must accept it, got %q", got)
	}
	// [generate] writes the file in the clicked format.
	m = tdClick(t, m, tdHitButton, tdBtnGenerate)
	if m.generateScratchOpen() {
		t.Fatalf("[generate] must generate and close: %s", m.tdGen.err)
	}
	path := m.activeWS().Panes.FocusedInstance().Editor().Path()
	if filepath.Ext(path) != ".tsv" {
		t.Fatalf("path = %q, want the clicked TSV format", path)
	}
}

// TestGenerateMouseCancel: [cancel] closes without generating.
func TestGenerateMouseCancel(t *testing.T) {
	m := openGenerate(t, newGenSized())
	m = tdClick(t, m, tdHitButton, tdBtnCancel)
	if m.generateScratchOpen() {
		t.Fatal("[cancel] must close the dialog")
	}
}

// TestGenerateEscSemantics: esc closes the popup first, then the dialog.
func TestGenerateEscSemantics(t *testing.T) {
	m := openGenerate(t, newGenSized())
	m = tdSetSpec(m, "", "5", "1")
	m = tdTypeStr(m, "a = uu")
	if !m.tdGen.acOpen {
		t.Fatal("setup: the popup must be open")
	}
	m = drainKey(m, tea.KeyPressMsg{Code: tea.KeyEscape})
	if !m.generateScratchOpen() || m.tdGen.acOpen {
		t.Fatal("esc must close only the popup first")
	}
	m = drainKey(m, tea.KeyPressMsg{Code: tea.KeyEscape})
	if m.generateScratchOpen() {
		t.Fatal("the second esc must close the dialog")
	}
}

// TestGenerateSeedRepeatsByteForByte is the determinism criterion seen from
// the UI: generating twice with the same seed yields two identical scratches.
func TestGenerateSeedRepeatsByteForByte(t *testing.T) {
	read := func(m Model) (Model, string) {
		m = openGenerate(t, m)
		m = tdSetSpec(m, "id = id()\nu = uuid()", "12", "2024")
		m = tdCtrl(m, 'g')
		data, err := os.ReadFile(m.activeWS().Panes.FocusedInstance().Editor().Path())
		if err != nil {
			t.Fatalf("generated scratch must exist: %v", err)
		}
		return m, string(data)
	}
	m, a := read(newGenSized())
	_, b := read(m)
	if a != b {
		t.Fatalf("the same seed produced different files:\n%s\n---\n%s", a, b)
	}
}

// TestGenerateRemembersLastSpec: the generated spec is the next dialog's
// starting point — format, knobs and DSL body.
func TestGenerateRemembersLastSpec(t *testing.T) {
	m := openGenerate(t, newGenSized())
	m.tdGen.setFocus(tdFocFormat)
	for m.tdGen.formats[m.tdGen.fmtPick] != testdata.FormatYAML {
		m = drainKey(m, tea.KeyPressMsg{Code: tea.KeyRight})
	}
	m = tdSetSpec(m, "color = from_list(on, off)", "3", "77")
	m = tdCtrl(m, 'g')
	if m.generateScratchOpen() {
		t.Fatalf("generation failed: %s", m.tdGen.err)
	}

	m2 := openGenerate(t, m)
	s := m2.tdGen
	if s.formats[s.fmtPick] != testdata.FormatYAML {
		t.Fatalf("format = %v, want the remembered YAML", s.formats[s.fmtPick])
	}
	if s.hdr[tdHdrRows] != "3" || s.hdr[tdHdrSeed] != "77" {
		t.Fatalf("rows/seed = %q/%q, want 3/77", s.hdr[tdHdrRows], s.hdr[tdHdrSeed])
	}
	if !strings.Contains(s.text(), "from_list(on, off)") {
		t.Fatalf("DSL not remembered: %q", s.text())
	}
}

// TestGeneratePasteMultiLine: pasting a whole spec into the editor keeps its
// lines.
func TestGeneratePasteMultiLine(t *testing.T) {
	m := openGenerate(t, newGenSized())
	m = tdSetSpec(m, "", "5", "1")
	if !m.pasteGenerateScratch("id = id()\nname = first_name()") {
		t.Fatal("the editor must take the paste")
	}
	if got := m.tdGen.text(); got != "id = id()\nname = first_name()" {
		t.Fatalf("pasted text = %q", got)
	}
}
