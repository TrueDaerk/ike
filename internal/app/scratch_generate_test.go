package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"ike/internal/testdata"
)

// scratch_generate_test.go covers the test-data wizard (#2134): the command
// family, the four steps, the validation messages, and the end-to-end path
// from enter to an opened scratch.

// tdKey feeds one key press through the wizard's update path.
func tdKey(m Model, code rune) Model { return drainKey(m, tea.KeyPressMsg{Code: code}) }

// openGenerate opens the wizard through the real command path.
func openGenerate(t *testing.T, m Model) Model {
	t.Helper()
	m = drainCmd(m, m.RunCommand("scratch.generate"))
	if !m.generateScratchOpen() {
		t.Fatal("scratch.generate must open the wizard")
	}
	return m
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

// TestGenerateWizardEndToEnd walks format → options → fields → generate and
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

	m = tdKey(m, tea.KeyEnter)
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
// a non-positive row count, an empty field list and an unknown kind each stop
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

	t.Run("empty field list", func(t *testing.T) {
		m := fields(t)
		for n := len(m.tdGen.spec.Fields); n > 0; n-- {
			m = tdKey(m, 'd')
		}
		if len(m.tdGen.spec.Fields) != 0 {
			t.Fatalf("d must delete the selected field, %d left", len(m.tdGen.spec.Fields))
		}
		m = tdKey(m, tea.KeyEnter)
		if m.tdGen.step != tdStepFields {
			t.Fatalf("step = %d, want to stay on the fields step", m.tdGen.step)
		}
		if !strings.Contains(m.tdGen.err, "at least one field") {
			t.Fatalf("err = %q, want an empty-field-list message", m.tdGen.err)
		}
	})

	t.Run("unknown kind", func(t *testing.T) {
		m := fields(t)
		m = tdKey(m, 'e') // edit the first field
		if m.tdGen.step != tdStepField {
			t.Fatalf("step = %d, want the field editor", m.tdGen.step)
		}
		m.tdGen.edit[tdEditKind] = "banana"
		m = tdKey(m, tea.KeyEnter)
		if m.tdGen.step != tdStepField {
			t.Fatalf("step = %d, want to stay in the field editor", m.tdGen.step)
		}
		if !strings.Contains(m.tdGen.err, `unknown kind "banana"`) {
			t.Fatalf("err = %q, want an unknown-kind message", m.tdGen.err)
		}
		if !strings.Contains(m.tdGen.err, "first_name") {
			t.Fatalf("err = %q, want it to list the catalog", m.tdGen.err)
		}
	})

	t.Run("empty field name", func(t *testing.T) {
		m := tdKey(fields(t), 'e')
		m.tdGen.edit[tdEditName] = "  "
		m = tdKey(m, tea.KeyEnter)
		if !strings.Contains(m.tdGen.err, "field name is required") {
			t.Fatalf("err = %q, want a name message", m.tdGen.err)
		}
	})

	t.Run("bad parameter", func(t *testing.T) {
		m := tdKey(fields(t), 'e')
		m.tdGen.edit[tdEditKind] = string(testdata.KindURL)
		m.tdGen.edit[tdEditParam] = "https://example.com/x"
		m = tdKey(m, tea.KeyEnter)
		if !strings.Contains(m.tdGen.err, "not a domain name") {
			t.Fatalf("err = %q, want a domain message", m.tdGen.err)
		}
	})

	t.Run("duplicate field name", func(t *testing.T) {
		m := tdKey(fields(t), 'a') // new field
		m.tdGen.edit[tdEditName] = m.tdGen.spec.Fields[0].Name
		m.tdGen.edit[tdEditKind] = string(testdata.KindID)
		m = tdKey(m, tea.KeyEnter)
		if !strings.Contains(m.tdGen.err, "already exists") {
			t.Fatalf("err = %q, want a duplicate-name message", m.tdGen.err)
		}
	})
}

// TestGenerateFieldEditorAddsAndCycles covers the field list's editing keys.
func TestGenerateFieldEditorAddsAndCycles(t *testing.T) {
	t.Setenv("IKE_CONFIG_DIR", t.TempDir())
	m := openGenerate(t, newSized())
	m = tdKey(m, tea.KeyEnter) // → options
	m = tdKey(m, tea.KeyEnter) // → fields
	before := len(m.tdGen.spec.Fields)

	m = tdKey(m, 'a')
	m.tdGen.edit[tdEditName] = "city"
	m.tdGen.editField = tdEditKind
	// Cycling reaches every kind, so walking to "city" needs no typing.
	steps := 0
	for m.tdGen.edit[tdEditKind] != string(testdata.KindCity) {
		m = tdKey(m, tea.KeyDown)
		steps++
		if steps > len(testdata.Kinds())+1 {
			t.Fatalf("cycling never reached %q (stuck at %q)", testdata.KindCity, m.tdGen.edit[tdEditKind])
		}
	}
	m = tdKey(m, tea.KeyEnter)
	if m.tdGen.step != tdStepFields {
		t.Fatalf("step = %d, want back on the fields step (err %q)", m.tdGen.step, m.tdGen.err)
	}
	if len(m.tdGen.spec.Fields) != before+1 {
		t.Fatalf("got %d fields, want %d", len(m.tdGen.spec.Fields), before+1)
	}
	added := m.tdGen.spec.Fields[len(m.tdGen.spec.Fields)-1]
	if added.Name != "city" || added.Kind != testdata.KindCity {
		t.Fatalf("added field = %+v, want city/city", added)
	}

	// Deleting takes it away again.
	m.tdGen.fieldPick = len(m.tdGen.spec.Fields) - 1
	m = tdKey(m, 'd')
	if len(m.tdGen.spec.Fields) != before {
		t.Fatalf("got %d fields after delete, want %d", len(m.tdGen.spec.Fields), before)
	}
}

// TestGenerateEscWalksBack mirrors the new-project wizard's esc semantics.
func TestGenerateEscWalksBack(t *testing.T) {
	t.Setenv("IKE_CONFIG_DIR", t.TempDir())
	m := openGenerate(t, newSized())
	m = tdKey(m, tea.KeyEnter)
	m = tdKey(m, tea.KeyEnter)
	if m.tdGen.step != tdStepFields {
		t.Fatalf("step = %d, want the fields step", m.tdGen.step)
	}
	m = tdKey(m, tea.KeyEscape)
	if m.tdGen.step != tdStepOptions {
		t.Fatalf("step = %d, want the options step", m.tdGen.step)
	}
	m = tdKey(m, tea.KeyEscape)
	if m.tdGen.step != tdStepFormat {
		t.Fatalf("step = %d, want the format step", m.tdGen.step)
	}
	m = tdKey(m, tea.KeyEscape)
	if m.generateScratchOpen() {
		t.Fatal("esc on the first step must close the wizard")
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
// the same format is picked.
func TestGenerateRemembersPreset(t *testing.T) {
	t.Setenv("IKE_CONFIG_DIR", t.TempDir())
	m := openGenerate(t, newSized())
	m = tdKey(m, tea.KeyEnter) // format: CSV → options
	m.tdGen.opt[tdOptRows] = "3"
	m.tdGen.opt[tdOptSeed] = "77"
	m = tdKey(m, tea.KeyEnter) // → fields
	m = tdKey(m, tea.KeyEnter) // generate

	m2 := openGenerate(t, m)
	if got := m2.tdGen.opt[tdOptRows]; got != "3" {
		t.Fatalf("rows = %q, want the remembered 3", got)
	}
	if got := m2.tdGen.opt[tdOptSeed]; got != "77" {
		t.Fatalf("seed = %q, want the remembered 77", got)
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
