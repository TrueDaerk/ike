package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"ike/internal/palette"
)

// scratch_custom_ext_test.go covers the free extension of the scratch language
// pickers (#2340): the validation both surfaces share, the "Custom…" row that
// no filter may hide, the creation flow behind it in scratch.new, the same
// flow in the manager's language step, and the esc that goes back to the row
// list rather than out of the dialog.

// openCustomExt opens the scratch.new picker's extension prompt the way its
// row does.
func openCustomExt(t *testing.T, m Model) Model {
	t.Helper()
	tm, cmd := m.Update(ShowScratchCustomExtMsg{})
	m = drainCmd(tm.(Model), cmd)
	if !m.scratchCustomExtOpen() {
		t.Fatal("the Custom… row must open the extension prompt")
	}
	return m
}

// typeExt types s into whichever extension prompt is open.
func typeExt(m Model, s string) Model {
	for _, r := range s {
		m = drainKey(m, tea.KeyPressMsg{Code: r, Text: string(r)})
	}
	return m
}

// TestNormalizeScratchExt covers the shared validation: the leading dot is
// optional, and every refusal names its reason instead of correcting the
// input silently.
func TestNormalizeScratchExt(t *testing.T) {
	for _, in := range []string{"tf", ".tf"} {
		got, err := normalizeScratchExt(in)
		if err != nil || got != "tf" {
			t.Errorf("normalizeScratchExt(%q) = %q, %v; want tf, nil", in, got, err)
		}
	}
	// A dot inside is a real extension shape ("scratch-1.d.ts").
	if got, err := normalizeScratchExt("d.ts"); err != nil || got != "d.ts" {
		t.Errorf("normalizeScratchExt(\"d.ts\") = %q, %v; want d.ts, nil", got, err)
	}
	// The case is kept: nothing is silently corrected.
	if got, err := normalizeScratchExt("TF"); err != nil || got != "TF" {
		t.Errorf("normalizeScratchExt(\"TF\") = %q, %v; want TF, nil", got, err)
	}
	for in, want := range map[string]string{
		"":       "required",
		".":      "required",
		"a/b":    "path separators",
		"a\\b":   "path separators",
		"a b":    "spaces",
		"tf ":    "spaces",
		" tf":    "spaces",
		"tf.":    "dot",
		"..tf":   "dot",
		"tf*":    "cannot contain",
		"tf:zip": "cannot contain",
	} {
		got, err := normalizeScratchExt(in)
		if err == nil {
			t.Errorf("normalizeScratchExt(%q) = %q, want a refusal", in, got)
			continue
		}
		if !strings.Contains(err.Error(), want) {
			t.Errorf("normalizeScratchExt(%q) error = %q, want it to mention %q", in, err, want)
		}
	}
}

// TestScratchNewOffersCustomRow covers the picker row itself: it is present
// unfiltered, it survives a query that matches nothing else — which is exactly
// when it is needed — and it carries the prompt message, not a creator
// command.
func TestScratchNewOffersCustomRow(t *testing.T) {
	for _, query := range []string{"", "custom", "zzqq"} {
		items := scratchNewMode{}.Results(query, palette.Context{})
		var found bool
		for _, it := range items {
			if it.Title != scratchCustomTitle {
				continue
			}
			found = true
			if _, ok := it.Msg.(ShowScratchCustomExtMsg); !ok {
				t.Errorf("row msg = %#v, want the extension prompt", it.Msg)
			}
			if _, ok := it.Msg.(palette.RunCommandMsg); ok {
				t.Errorf("the Custom… row must not run a scratch.new command")
			}
		}
		if !found {
			t.Fatalf("query %q must keep the %q row: %v", query, scratchCustomTitle, items)
		}
	}
	// With other rows around it, the row sorts last so it never displaces a
	// language the query actually matched.
	items := scratchNewMode{}.Results("", palette.Context{})
	if got := items[len(items)-1].Title; got != scratchCustomTitle {
		t.Fatalf("last row = %q, want %q", got, scratchCustomTitle)
	}
	// A query nothing else matches leaves it as the single row.
	only := scratchNewMode{}.Results("zzqq", palette.Context{})
	if len(only) != 1 || only[0].Title != scratchCustomTitle {
		t.Fatalf("a query nothing matches must leave only %q: %v", scratchCustomTitle, only)
	}
}

// TestScratchNewCustomExtCreates covers the scratch.new flow: typing "tf"
// creates a .tf scratch and opens it.
func TestScratchNewCustomExtCreates(t *testing.T) {
	t.Setenv("IKE_CONFIG_DIR", t.TempDir())
	m := newSized()
	m = openCustomExt(t, m)
	m = typeExt(m, ".tf")
	m = drainKey(m, tea.KeyPressMsg{Code: tea.KeyEnter})

	if m.scratchCustomExtOpen() {
		t.Fatal("an accepted extension must close the prompt")
	}
	got := m.activeWS().Panes.FocusedInstance().Editor().Path()
	if filepath.Ext(got) != ".tf" {
		t.Fatalf("created scratch = %q, want a .tf file", got)
	}
	if _, err := os.Stat(got); err != nil {
		t.Fatalf("the scratch must exist: %v", err)
	}
}

// TestScratchNewCustomExtIsNoSpecialCase covers the overlap with the curated
// list: typing an extension that has a row of its own produces the very same
// scratch the row does.
func TestScratchNewCustomExtIsNoSpecialCase(t *testing.T) {
	t.Setenv("IKE_CONFIG_DIR", t.TempDir())
	m := newSized()
	m = openCustomExt(t, m)
	m = typeExt(m, "sct")
	m = drainKey(m, tea.KeyPressMsg{Code: tea.KeyEnter})
	typed := m.activeWS().Panes.FocusedInstance().Editor().Path()

	row := findRow(t, "sct")
	m = drainCmd(m, m.RunCommand(row.CmdID))
	viaRow := m.activeWS().Panes.FocusedInstance().Editor().Path()

	if filepath.Ext(typed) != filepath.Ext(viaRow) {
		t.Fatalf("typed %q and row %q must agree on the extension", typed, viaRow)
	}
	if scratchRowTitle(typed) != scratchRowTitle(viaRow) {
		t.Fatalf("typed scratch reads as %q, the row's as %q",
			scratchRowTitle(typed), scratchRowTitle(viaRow))
	}
}

// TestScratchNewCustomExtRejects covers the refusals: the prompt stays open
// with a message and creates nothing.
func TestScratchNewCustomExtRejects(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("IKE_CONFIG_DIR", dir)
	m := newSized()
	for _, in := range []string{"", "a/b", "a b"} {
		m = openCustomExt(t, m)
		m = typeExt(m, in)
		m = drainKey(m, tea.KeyPressMsg{Code: tea.KeyEnter})
		if !m.scratchCustomExtOpen() {
			t.Fatalf("input %q must keep the prompt open", in)
		}
		if m.scratchExtErr == "" {
			t.Fatalf("input %q must be refused with a message", in)
		}
		m.closeScratchCustomExt()
	}
	if entries, _ := os.ReadDir(filepath.Join(dir, "scratches")); len(entries) != 0 {
		t.Fatalf("a refused extension must create nothing, got %v", entries)
	}
}

// TestScratchNewCustomExtEscReturnsToPicker covers the way back: esc leaves
// the prompt for the language list it was opened from, not for the editor.
func TestScratchNewCustomExtEscReturnsToPicker(t *testing.T) {
	t.Setenv("IKE_CONFIG_DIR", t.TempDir())
	m := newSized()
	m = openCustomExt(t, m)
	m = drainKey(m, tea.KeyPressMsg{Code: tea.KeyEscape})

	if m.scratchCustomExtOpen() {
		t.Fatal("esc must leave the prompt")
	}
	if !m.palette.IsOpen() {
		t.Fatal("esc must return to the language picker")
	}
}

// TestScratchManagerCustomExtRenames covers the manager's language step: the
// Custom… row opens a prompt, and the typed extension re-languages the scratch
// through the same store call the rows use.
func TestScratchManagerCustomExtRenames(t *testing.T) {
	t.Setenv("IKE_CONFIG_DIR", t.TempDir())
	m := newSized()
	paths := smSeed(t, "txt")
	m = openManager(t, m)
	m = smSelect(t, m, filepath.Base(paths[0]))
	m = smKey(m, tea.Key{Code: 'l', Mod: tea.ModCtrl})

	// A filter that matches no language keeps the Custom… row: that is when
	// a free extension is wanted.
	m = smTypeAll(m, "zzqq")
	filtered := m.scratchMgr.filteredLangs()
	if len(filtered) != 1 || !filtered[0].custom {
		t.Fatalf("filtered langs = %+v, want only the %q row", filtered, scratchCustomTitle)
	}
	m.scratchMgr.langPick = 0
	m = smKey(m, tea.Key{Code: tea.KeyEnter})
	if m.scratchMgr.step != smStepCustomExt {
		t.Fatalf("step = %d, want the extension prompt", m.scratchMgr.step)
	}

	// The field is seeded with the current extension; replace it.
	for range m.scratchMgr.customInput {
		m = smKey(m, tea.Key{Code: tea.KeyBackspace})
	}
	m = smTypeAll(m, ".tf")
	m = smKey(m, tea.Key{Code: tea.KeyEnter})

	want := strings.TrimSuffix(paths[0], ".txt") + ".tf"
	if _, err := os.Stat(want); err != nil {
		t.Fatalf("re-languaged scratch must exist: %v", err)
	}
	if m.scratchMgr.step != smStepList {
		t.Fatalf("step = %d, want the list after the change", m.scratchMgr.step)
	}
	row, ok := m.scratchMgr.selected()
	if !ok || row.path != want {
		t.Fatalf("row = %+v, want the .tf scratch", row)
	}
	if row.lang != "Plain Text" {
		t.Fatalf("unknown extension must still get a title, got %q", row.lang)
	}
}

// TestScratchManagerCustomExtRejects covers the manager's validation: the
// prompt keeps the step with a message and the scratch keeps its name.
func TestScratchManagerCustomExtRejects(t *testing.T) {
	t.Setenv("IKE_CONFIG_DIR", t.TempDir())
	m := newSized()
	paths := smSeed(t, "txt")
	m = openManager(t, m)
	m = smSelect(t, m, filepath.Base(paths[0]))
	m = smKey(m, tea.Key{Code: 'l', Mod: tea.ModCtrl})

	// The Custom… row is the last one of the unfiltered list.
	filtered := m.scratchMgr.filteredLangs()
	m.scratchMgr.langPick = len(filtered) - 1
	m = smKey(m, tea.Key{Code: tea.KeyEnter})
	if m.scratchMgr.step != smStepCustomExt {
		t.Fatalf("step = %d, want the extension prompt", m.scratchMgr.step)
	}

	for range m.scratchMgr.customInput {
		m = smKey(m, tea.Key{Code: tea.KeyBackspace})
	}
	m = smTypeAll(m, "a/b")
	m = smKey(m, tea.Key{Code: tea.KeyEnter})
	if m.scratchMgr.step != smStepCustomExt || m.scratchMgr.err == "" {
		t.Fatalf("a path separator must be refused in place: step %d, err %q",
			m.scratchMgr.step, m.scratchMgr.err)
	}
	if _, err := os.Stat(paths[0]); err != nil {
		t.Fatalf("the scratch must keep its name: %v", err)
	}
}

// TestScratchManagerCustomExtEscGoesBack covers the step walk: esc from the
// prompt lands in the language list, not outside the dialog.
func TestScratchManagerCustomExtEscGoesBack(t *testing.T) {
	t.Setenv("IKE_CONFIG_DIR", t.TempDir())
	m := newSized()
	paths := smSeed(t, "txt")
	m = openManager(t, m)
	m = smSelect(t, m, filepath.Base(paths[0]))
	m = smKey(m, tea.Key{Code: 'l', Mod: tea.ModCtrl})
	filtered := m.scratchMgr.filteredLangs()
	m.scratchMgr.langPick = len(filtered) - 1
	m = smKey(m, tea.Key{Code: tea.KeyEnter})
	if m.scratchMgr.step != smStepCustomExt {
		t.Fatalf("step = %d, want the extension prompt", m.scratchMgr.step)
	}

	m = smKey(m, tea.Key{Code: tea.KeyEscape})
	if !m.scratchManagerOpen() {
		t.Fatal("esc must stay inside the manager")
	}
	if m.scratchMgr.step != smStepLang {
		t.Fatalf("step = %d, want the language list", m.scratchMgr.step)
	}
}
