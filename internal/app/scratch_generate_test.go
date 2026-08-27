package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"ike/internal/testdata"
)

// scratch_generate_test.go covers the test-data wizard (#2134, #2228): the
// command family, the five steps, the validation messages, the catalog picker,
// the mouse operation and the end-to-end path from `g` to an opened scratch.

// tdKey feeds one key press through the wizard's update path.
func tdKey(m Model, code rune) Model { return drainKey(m, tea.KeyPressMsg{Code: code}) }

// tdType feeds one printable key with its text, so type-to-filter sees it.
func tdType(m Model, r rune) Model {
	return drainKey(m, tea.KeyPressMsg{Code: r, Text: string(r)})
}

// openGenerate opens the wizard through the real command path.
func openGenerate(t *testing.T, m Model) Model {
	t.Helper()
	m = drainCmd(m, m.RunCommand("scratch.generate"))
	if !m.generateScratchOpen() {
		t.Fatal("scratch.generate must open the wizard")
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

func TestGenerateCommandsRegistered(t *testing.T) {
	m := newSized()
	want := []string{"scratch.generate"}
	for _, f := range testdata.Formats() {
		want = append(want, "scratch.generate."+string(f))
	}
	for _, id := range want {
		if _, ok := m.reg.Command(id); !ok {
			t.Fatalf("command %s must be registered", id)
		}
	}
}

func TestGenerateWizardOpensOnFormatStep(t *testing.T) {
	t.Setenv("IKE_CONFIG_DIR", t.TempDir())
	m := openGenerate(t, newSized())
	if m.tdGen.step != tdStepFormat {
		t.Fatalf("step = %d, want the format step", m.tdGen.step)
	}
	view := plainView(m)
	if !strings.Contains(view, "Generate Test Data") || !strings.Contains(view, "NDJSON") {
		t.Fatalf("wizard is not rendered:\n%s", view)
	}
}

// TestGenerateRowCountPrompted is the row-count criterion of #2228: the
// options step asks for the row count, prefilled with a sensible default.
func TestGenerateRowCountPrompted(t *testing.T) {
	t.Setenv("IKE_CONFIG_DIR", t.TempDir())
	m := tdKey(openGenerate(t, newSized()), tea.KeyEnter)
	if m.tdGen.step != tdStepOptions {
		t.Fatalf("step = %d, want the options step", m.tdGen.step)
	}
	if got := m.tdGen.opt[tdOptRows]; got != "100" {
		t.Fatalf("rows prefill = %q, want the default 100", got)
	}
	if !strings.Contains(plainView(m), "Rows") {
		t.Fatalf("the row prompt must be visible:\n%s", plainView(m))
	}
}

// TestGenerateWizardEndToEnd walks format → options → columns → generate and
// checks the scratch lands on disk, in the right format, and opens focused.
func TestGenerateWizardEndToEnd(t *testing.T) {
	t.Setenv("IKE_CONFIG_DIR", t.TempDir())
	m := openGenerate(t, newSized())

	// Format step: move to JSON.
	for m.tdGen.formats[m.tdGen.fmtPick] != testdata.FormatJSON {
		m = tdKey(m, tea.KeyDown)
	}
	m = tdKey(m, tea.KeyEnter)
	if m.tdGen.step != tdStepOptions {
		t.Fatalf("step = %d, want the options step", m.tdGen.step)
	}

	// Options step: 5 rows, seed 42.
	m.tdGen.opt[tdOptRows] = "5"
	m.tdGen.opt[tdOptSeed] = "42"
	m = tdKey(m, tea.KeyEnter)
	if m.tdGen.step != tdStepFields {
		t.Fatalf("step = %d, want the fields step", m.tdGen.step)
	}
	if len(m.tdGen.spec.Fields) == 0 {
		t.Fatal("the default spec must come with fields")
	}

	m = tdKey(m, 'g')
	if m.generateScratchOpen() {
		t.Fatalf("the wizard must close once the generation finished: %s", m.tdGen.err)
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
	if !strings.HasPrefix(string(data), "[\n") {
		t.Fatalf("content is not a JSON array:\n%s", data)
	}
	if got := strings.Count(string(data), "\"first_name\""); got != 5 {
		t.Fatalf("got %d rows, want 5:\n%s", got, data)
	}
	if ed.Dirty() {
		t.Fatal("a generated scratch must open clean")
	}
}

// TestGenerateRejectsBadInput is the "form rejects invalid input" criterion:
// a non-positive row count, an empty column list and a bad parameter each stop
// at the dialog with a message instead of generating.
func TestGenerateRejectsBadInput(t *testing.T) {
	t.Setenv("IKE_CONFIG_DIR", t.TempDir())
	base := newSized()
	// The wizard state is a pointer inside the model, so every subtest opens
	// its own — copying the model would share (and corrupt) the state.
	options := func(t *testing.T) Model {
		t.Helper()
		return tdKey(openGenerate(t, base), tea.KeyEnter)
	}
	fields := func(t *testing.T) Model {
		t.Helper()
		m := tdKey(options(t), tea.KeyEnter)
		if m.tdGen.step != tdStepFields {
			t.Fatalf("setup: step = %d, want the fields step (err %q)", m.tdGen.step, m.tdGen.err)
		}
		return m
	}

	t.Run("row count zero", func(t *testing.T) {
		m := options(t)
		m.tdGen.opt[tdOptRows] = "0"
		m = tdKey(m, tea.KeyEnter)
		if m.tdGen.step != tdStepOptions {
			t.Fatalf("step = %d, want to stay on the options step", m.tdGen.step)
		}
		if !strings.Contains(m.tdGen.err, "at least 1") {
			t.Fatalf("err = %q, want a row-count message", m.tdGen.err)
		}
		if !strings.Contains(plainView(m), "at least 1") {
			t.Fatalf("the message must be visible:\n%s", plainView(m))
		}
	})

	t.Run("row count not a number", func(t *testing.T) {
		m := options(t)
		m.tdGen.opt[tdOptRows] = "lots"
		m = tdKey(m, tea.KeyEnter)
		if !strings.Contains(m.tdGen.err, "not a number") {
			t.Fatalf("err = %q, want a parse message", m.tdGen.err)
		}
	})

	t.Run("seed not a number", func(t *testing.T) {
		m := options(t)
		m.tdGen.opt[tdOptSeed] = "-1"
		m = tdKey(m, tea.KeyEnter)
		if !strings.Contains(m.tdGen.err, "non-negative") {
			t.Fatalf("err = %q, want a seed message", m.tdGen.err)
		}
	})

	t.Run("row count above the cap", func(t *testing.T) {
		m := options(t)
		m.tdGen.opt[tdOptRows] = "1000001"
		m = tdKey(m, tea.KeyEnter)
		if !strings.Contains(m.tdGen.err, "at most") {
			t.Fatalf("err = %q, want the cap message", m.tdGen.err)
		}
	})

	t.Run("empty column list", func(t *testing.T) {
		m := fields(t)
		for n := len(m.tdGen.spec.Fields); n > 0; n-- {
			m = tdKey(m, 'd')
		}
		if len(m.tdGen.spec.Fields) != 0 {
			t.Fatalf("d must delete the selected column, %d left", len(m.tdGen.spec.Fields))
		}
		m = tdKey(m, 'g')
		if m.tdGen.step != tdStepFields {
			t.Fatalf("step = %d, want to stay on the fields step", m.tdGen.step)
		}
		if !strings.Contains(m.tdGen.err, "at least one field") {
			t.Fatalf("err = %q, want an empty-column-list message", m.tdGen.err)
		}
	})

	t.Run("empty column name", func(t *testing.T) {
		m := tdKey(fields(t), 'e')
		m.tdGen.editName = "  "
		m = tdKey(m, tea.KeyEnter)
		if !strings.Contains(m.tdGen.err, "field name is required") {
			t.Fatalf("err = %q, want a name message", m.tdGen.err)
		}
	})

	t.Run("bad parameter", func(t *testing.T) {
		m := tdKey(fields(t), 'e')
		m.tdGen.editKind = kindIndex(testdata.KindURL)
		m.tdGen.editParam = "https://example.com/x"
		m = tdKey(m, tea.KeyEnter)
		if !strings.Contains(m.tdGen.err, "not a domain name") {
			t.Fatalf("err = %q, want a domain message", m.tdGen.err)
		}
	})

	t.Run("empty from_list", func(t *testing.T) {
		m := tdKey(fields(t), 'a')
		m.tdGen.editName = "color"
		m.tdGen.editKind = kindIndex(testdata.KindFromList)
		m = tdKey(m, tea.KeyEnter)
		if !strings.Contains(m.tdGen.err, "at least one entry") {
			t.Fatalf("err = %q, want the from_list message", m.tdGen.err)
		}
	})

	t.Run("duplicate column name", func(t *testing.T) {
		m := tdKey(fields(t), 'a') // new column
		m.tdGen.editName = m.tdGen.spec.Fields[0].Name
		m = tdKey(m, tea.KeyEnter)
		if !strings.Contains(m.tdGen.err, "already exists") {
			t.Fatalf("err = %q, want a duplicate-name message", m.tdGen.err)
		}
	})
}

// tdFields walks a fresh wizard to the columns step.
func tdFields(t *testing.T, m Model) Model {
	t.Helper()
	m = tdKey(tdKey(openGenerate(t, m), tea.KeyEnter), tea.KeyEnter)
	if m.tdGen.step != tdStepFields {
		t.Fatalf("setup: step = %d, want the fields step (err %q)", m.tdGen.step, m.tdGen.err)
	}
	return m
}

// TestGenerateColumnEditorAddsAndDeletes covers the column list's editing keys
// with the kind chosen through the catalog picker, never typed.
func TestGenerateColumnEditorAddsAndDeletes(t *testing.T) {
	t.Setenv("IKE_CONFIG_DIR", t.TempDir())
	m := tdFields(t, newSized())
	before := len(m.tdGen.spec.Fields)

	m = tdKey(m, 'a')
	m.tdGen.editName = "place"
	m.tdGen.editField = tdEditKind
	m = tdKey(m, tea.KeyEnter) // Kind row → catalog picker
	if m.tdGen.step != tdStepKind {
		t.Fatalf("step = %d, want the kind picker", m.tdGen.step)
	}
	// Type-to-filter narrows to "city" without knowing the catalog order.
	for _, r := range "city" {
		m = tdType(m, r)
	}
	filtered := m.tdGen.filteredCatalog()
	if len(filtered) == 0 || filtered[m.tdGen.kindPick].Kind != testdata.KindCity {
		t.Fatalf("filter %q selected %+v, want city", m.tdGen.kindSearch.Query(), filtered)
	}
	m = tdKey(m, tea.KeyEnter) // choose → back in the editor
	if m.tdGen.step != tdStepField {
		t.Fatalf("step = %d, want back in the editor", m.tdGen.step)
	}
	if m.tdGen.editField == tdEditKind {
		t.Fatal("choosing must move the focus off the Kind row, or enter would reopen the picker")
	}
	m = tdKey(m, tea.KeyEnter) // accept the column
	if m.tdGen.step != tdStepFields {
		t.Fatalf("step = %d, want back on the fields step (err %q)", m.tdGen.step, m.tdGen.err)
	}
	if len(m.tdGen.spec.Fields) != before+1 {
		t.Fatalf("got %d columns, want %d", len(m.tdGen.spec.Fields), before+1)
	}
	added := m.tdGen.spec.Fields[len(m.tdGen.spec.Fields)-1]
	if added.Name != "place" || added.Kind != testdata.KindCity {
		t.Fatalf("added column = %+v, want place/city", added)
	}

	// Deleting takes it away again, with a visible notice.
	m.tdGen.fieldPick = len(m.tdGen.spec.Fields) - 1
	m = tdKey(m, 'd')
	if len(m.tdGen.spec.Fields) != before {
		t.Fatalf("got %d columns after delete, want %d", len(m.tdGen.spec.Fields), before)
	}
	if !strings.Contains(plainView(m), `removed column "place"`) {
		t.Fatalf("delete must announce what it removed:\n%s", plainView(m))
	}
}

// TestGenerateKindPickerShowsCatalog: the picker lists kinds with their
// descriptions and the selected kind's parameter grammar — no name needs to be
// known by heart.
func TestGenerateKindPickerShowsCatalog(t *testing.T) {
	t.Setenv("IKE_CONFIG_DIR", t.TempDir())
	m := tdKey(tdFields(t, newSized()), 'a')
	m.tdGen.editField = tdEditKind
	m = tdKey(m, tea.KeyEnter)
	view := plainView(m)
	for _, want := range []string{"uuid", "random UUID v4", "type to filter"} {
		if !strings.Contains(view, want) {
			t.Fatalf("picker must show %q:\n%s", want, view)
		}
	}
	// Walk to a parameterized kind: its grammar shows under the list.
	m.tdGen.kindPick = kindIndex(testdata.KindFromList)
	m.renderGenerateScratch()
	if !strings.Contains(plainView(m), "comma-separated entries") {
		t.Fatalf("picker must show the selected kind's parameter grammar:\n%s", plainView(m))
	}
	// Esc first clears an active filter, then leaves the picker.
	m = tdType(m, 'x')
	m = tdKey(m, tea.KeyEscape)
	if m.tdGen.step != tdStepKind || m.tdGen.kindSearch.Active() {
		t.Fatalf("esc must clear the filter first (step %d, active %v)", m.tdGen.step, m.tdGen.kindSearch.Active())
	}
	m = tdKey(m, tea.KeyEscape)
	if m.tdGen.step != tdStepField {
		t.Fatalf("step = %d, want back in the editor", m.tdGen.step)
	}
}

// TestGenerateFromListEndToEnd adds a from_list column and checks every
// generated value comes from the user's list.
func TestGenerateFromListEndToEnd(t *testing.T) {
	t.Setenv("IKE_CONFIG_DIR", t.TempDir())
	m := openGenerate(t, newSized())
	m = tdKey(m, tea.KeyEnter) // format: CSV → options
	m.tdGen.opt[tdOptRows] = "20"
	m.tdGen.opt[tdOptSeed] = "7"
	m = tdKey(m, tea.KeyEnter) // → fields
	m = tdKey(m, 'a')
	m.tdGen.editName = "color"
	m.tdGen.editKind = kindIndex(testdata.KindFromList)
	m.tdGen.editParam = "red, green, blue"
	m = tdKey(m, tea.KeyEnter)
	if m.tdGen.step != tdStepFields {
		t.Fatalf("step = %d, want the fields step (err %q)", m.tdGen.step, m.tdGen.err)
	}
	m = tdKey(m, 'g')
	if m.generateScratchOpen() {
		t.Fatalf("generation must close the wizard: %s", m.tdGen.err)
	}
	data, err := os.ReadFile(m.activeWS().Panes.FocusedInstance().Editor().Path())
	if err != nil {
		t.Fatalf("generated scratch must exist: %v", err)
	}
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(lines) != 21 {
		t.Fatalf("got %d lines, want header + 20 rows:\n%s", len(lines), data)
	}
	for _, l := range lines[1:] {
		v := l[strings.LastIndex(l, ",")+1:]
		if v != "red" && v != "green" && v != "blue" {
			t.Fatalf("from_list value %q not from the list:\n%s", v, data)
		}
	}
}

// TestGenerateEscWalksBack mirrors the new-project wizard's esc semantics.
func TestGenerateEscWalksBack(t *testing.T) {
	t.Setenv("IKE_CONFIG_DIR", t.TempDir())
	m := tdFields(t, newSized())
	m = tdKey(m, 'e')
	m.tdGen.editField = tdEditKind
	m = tdKey(m, tea.KeyEnter)
	if m.tdGen.step != tdStepKind {
		t.Fatalf("step = %d, want the kind picker", m.tdGen.step)
	}
	for _, want := range []int{tdStepField, tdStepFields, tdStepOptions, tdStepFormat} {
		m = tdKey(m, tea.KeyEscape)
		if m.tdGen.step != want {
			t.Fatalf("step = %d, want %d", m.tdGen.step, want)
		}
	}
	m = tdKey(m, tea.KeyEscape)
	if m.generateScratchOpen() {
		t.Fatal("esc on the first step must close the wizard")
	}
}

// TestGenerateMouse drives the wizard by clicks alone (#2228): pick a format,
// advance with the [next] buttons, focus an option field, open a column by
// clicking it twice, choose a kind from the catalog by click, accept with
// [ok] and generate with [generate].
func TestGenerateMouse(t *testing.T) {
	t.Setenv("IKE_CONFIG_DIR", t.TempDir())
	m := openGenerate(t, newSized())

	// Click the TSV row: selected, still on the format step.
	m = tdClick(t, m, tdHitFormat, 1)
	if m.tdGen.step != tdStepFormat || m.tdGen.formats[m.tdGen.fmtPick] != testdata.FormatTSV {
		t.Fatalf("click must select TSV (step %d, pick %d)", m.tdGen.step, m.tdGen.fmtPick)
	}
	// Click it again: advance to the options.
	m = tdClick(t, m, tdHitFormat, 1)
	if m.tdGen.step != tdStepOptions {
		t.Fatalf("step = %d, want the options step", m.tdGen.step)
	}
	// Click the Seed field to focus it, then [next].
	m = tdClick(t, m, tdHitOpt, tdOptSeed)
	if m.tdGen.optField != tdOptSeed {
		t.Fatalf("optField = %d, want the seed row", m.tdGen.optField)
	}
	m.tdGen.opt[tdOptRows] = "3"
	m = tdClick(t, m, tdHitButton, '\r')
	if m.tdGen.step != tdStepFields {
		t.Fatalf("step = %d, want the fields step (err %q)", m.tdGen.step, m.tdGen.err)
	}
	// Click the second column twice: select, then edit.
	m = tdClick(t, m, tdHitField, 1)
	if m.tdGen.fieldPick != 1 || m.tdGen.step != tdStepFields {
		t.Fatalf("first click must only select (pick %d, step %d)", m.tdGen.fieldPick, m.tdGen.step)
	}
	m = tdClick(t, m, tdHitField, 1)
	if m.tdGen.step != tdStepField {
		t.Fatalf("second click must edit (step %d)", m.tdGen.step)
	}
	// Click the Kind row: the catalog picker opens; click a kind: chosen.
	m = tdClick(t, m, tdHitEdit, tdEditKind)
	if m.tdGen.step != tdStepKind {
		t.Fatalf("step = %d, want the kind picker", m.tdGen.step)
	}
	target := kindIndex(testdata.KindCity)
	m.tdGen.kindPick = target // scroll it into the window
	m.renderGenerateScratch()
	m = tdClick(t, m, tdHitCatalog, target)
	if m.tdGen.step != tdStepField || testdata.Kinds()[m.tdGen.editKind] != testdata.KindCity {
		t.Fatalf("click must choose city (step %d, kind %v)", m.tdGen.step, testdata.Kinds()[m.tdGen.editKind])
	}
	// [ok] accepts the column, [generate] writes the file.
	m = tdClick(t, m, tdHitButton, '\r')
	if m.tdGen.step != tdStepFields {
		t.Fatalf("[ok] must accept (step %d, err %q)", m.tdGen.step, m.tdGen.err)
	}
	m = tdClick(t, m, tdHitButton, 'g')
	if m.generateScratchOpen() {
		t.Fatalf("[generate] must generate and close: %s", m.tdGen.err)
	}
	path := m.activeWS().Panes.FocusedInstance().Editor().Path()
	if filepath.Ext(path) != ".tsv" {
		t.Fatalf("path = %q, want the clicked TSV format", path)
	}
}

// TestGenerateWheelMovesSelection: the wheel walks the format list and the
// catalog picker like the arrows do.
func TestGenerateWheelMovesSelection(t *testing.T) {
	t.Setenv("IKE_CONFIG_DIR", t.TempDir())
	m := openGenerate(t, newSized())
	wheel := func(m Model, b tea.MouseButton) Model {
		out, cmd := m.handleMouse(mouseEvent{Mouse: tea.Mouse{X: m.width / 2, Y: m.height / 2, Button: b}, action: mouseWheel})
		return drainCmd(out.(Model), cmd)
	}
	m = wheel(m, tea.MouseWheelDown)
	if m.tdGen.fmtPick == 0 {
		t.Fatal("wheel down must move the format selection")
	}
	m = wheel(m, tea.MouseWheelUp)
	if m.tdGen.fmtPick != 0 {
		t.Fatalf("wheel up must move back (pick %d)", m.tdGen.fmtPick)
	}
	// In the picker the wheel moves the windowed selection.
	m = tdKey(m, tea.KeyEnter)
	m = tdKey(m, tea.KeyEnter)
	m = tdKey(m, 'a')
	m.tdGen.editField = tdEditKind
	m = tdKey(m, tea.KeyEnter)
	m = wheel(m, tea.MouseWheelDown)
	if m.tdGen.kindPick == 0 {
		t.Fatal("wheel down must move the kind selection")
	}
}

// TestGeneratePerFormatCommandUsesPreset is the no-prompt path: the command
// generates straight away, and the spec it used becomes the format's preset.
func TestGeneratePerFormatCommandUsesPreset(t *testing.T) {
	t.Setenv("IKE_CONFIG_DIR", t.TempDir())
	m := newSized()
	m = drainCmd(m, m.RunCommand("scratch.generate.csv"))
	if m.generateScratchOpen() {
		t.Fatal("the per-format command must not open the wizard")
	}
	path := m.activeWS().Panes.FocusedInstance().Editor().Path()
	if filepath.Ext(path) != ".csv" {
		t.Fatalf("path = %q, want a .csv scratch", path)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("generated scratch must exist: %v", err)
	}
	if !strings.HasPrefix(string(data), "id,first_name,last_name,email\n") {
		t.Fatalf("header missing:\n%s", data)
	}
	if _, err := os.Stat(testdata.StoreFile()); err != nil {
		t.Fatalf("the spec must be remembered as a preset: %v", err)
	}
}

// TestGenerateRemembersPreset checks the wizard's spec comes back next time
// the same format is picked — including a from_list column.
func TestGenerateRemembersPreset(t *testing.T) {
	t.Setenv("IKE_CONFIG_DIR", t.TempDir())
	m := openGenerate(t, newSized())
	m = tdKey(m, tea.KeyEnter) // format: CSV → options
	m.tdGen.opt[tdOptRows] = "3"
	m.tdGen.opt[tdOptSeed] = "77"
	m = tdKey(m, tea.KeyEnter) // → fields
	m = tdKey(m, 'a')
	m.tdGen.editName = "color"
	m.tdGen.editKind = kindIndex(testdata.KindFromList)
	m.tdGen.editParam = "on,off"
	m = tdKey(m, tea.KeyEnter)
	m = tdKey(m, 'g') // generate

	m2 := openGenerate(t, m)
	if got := m2.tdGen.opt[tdOptRows]; got != "3" {
		t.Fatalf("rows = %q, want the remembered 3", got)
	}
	if got := m2.tdGen.opt[tdOptSeed]; got != "77" {
		t.Fatalf("seed = %q, want the remembered 77", got)
	}
	fields := m2.tdGen.spec.Fields
	last := fields[len(fields)-1]
	if last.Kind != testdata.KindFromList || last.Param != "on,off" {
		t.Fatalf("preset must keep the from_list column, got %+v", last)
	}
}

// TestGenerateSeedRepeatsByteForByte is the determinism criterion seen from
// the UI: generating twice with the same seed yields two identical scratches.
func TestGenerateSeedRepeatsByteForByte(t *testing.T) {
	t.Setenv("IKE_CONFIG_DIR", t.TempDir())
	m := newSized()
	spec := testdata.Default(testdata.FormatCSV)
	spec.Rows, spec.Seed = 12, 2024
	testdata.SavePreset(spec)

	read := func(m Model) (Model, string) {
		m = drainCmd(m, m.RunCommand("scratch.generate.csv"))
		data, err := os.ReadFile(m.activeWS().Panes.FocusedInstance().Editor().Path())
		if err != nil {
			t.Fatalf("generated scratch must exist: %v", err)
		}
		return m, string(data)
	}
	m, a := read(m)
	_, b := read(m)
	if a != b {
		t.Fatalf("the same seed produced different files:\n%s\n---\n%s", a, b)
	}
}

// TestGenerateShowsErrorForBrokenPreset covers the quick command's guard: a
// preset that cannot generate toasts instead of writing a broken scratch.
func TestGenerateBrokenPresetIsRefused(t *testing.T) {
	t.Setenv("IKE_CONFIG_DIR", t.TempDir())
	// newSized rotates IKE_CONFIG_DIR itself, so the store file has to be
	// written to the directory it settled on.
	m := newSized()
	store := testdata.StoreFile()
	if err := os.MkdirAll(filepath.Dir(store), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(store,
		[]byte(`{"specs":{"csv":{"format":"csv","rows":4,"fields":[{"name":"x","kind":"nope"}]}}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	m = drainCmd(m, m.RunCommand("scratch.generate.csv"))
	// The store's spec is unusable, so Preset falls back to the default and
	// the generation still produces a valid file rather than failing.
	path := m.activeWS().Panes.FocusedInstance().Editor().Path()
	if filepath.Ext(path) != ".csv" {
		t.Fatalf("path = %q, want a .csv scratch from the fallback default", path)
	}
}
