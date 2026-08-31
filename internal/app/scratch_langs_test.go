package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"ike/internal/lang"
	"ike/internal/palette"
)

// scratch_langs_test.go covers the shared scratch offering (#2333): a language
// that owns several dialect extensions contributes a picker row per curated
// dialect, so a .js scratch can be created and switched to.

func init() {
	// The web plugin's TypeScript language, minus everything the scratch
	// surfaces do not look at — the language plugins are not imported by this
	// package, and the alias table is keyed by this id.
	lang.Register(lang.Language{ID: "typescript", Extensions: []string{"ts", "tsx", "js", "jsx", "mjs"}})
}

// findRow returns the offering row with the given extension.
func findRow(t *testing.T, ext string) scratchLangRow {
	t.Helper()
	for _, r := range scratchLangRows() {
		if r.Ext == ext {
			return r
		}
	}
	t.Fatalf("no scratch language row for .%s in %+v", ext, scratchLangRows())
	return scratchLangRow{}
}

// TestScratchLangRowsOfferDialects covers the offering itself: JavaScript is
// reachable although it shares the "typescript" language, the primary row
// survives, rows stay sorted and no extension is offered twice.
func TestScratchLangRowsOfferDialects(t *testing.T) {
	if got := findRow(t, "js"); got.Title != "JavaScript" || got.CmdID != "scratch.new.typescript.js" {
		t.Fatalf("javascript row = %+v, want the JavaScript title and its own command", got)
	}
	if got := findRow(t, "ts"); got.Title != "Typescript" || got.CmdID != "scratch.new.typescript" {
		t.Fatalf("typescript row = %+v, want the unchanged language row", got)
	}
	findRow(t, "jsx")
	findRow(t, "sct") // a plain single-extension language is untouched

	rows := scratchLangRows()
	seen := map[string]bool{}
	for i, r := range rows {
		if seen[r.Ext] {
			t.Fatalf("extension .%s offered twice: %+v", r.Ext, rows)
		}
		seen[r.Ext] = true
		if r.Ext == "txt" {
			t.Fatalf("plain text is pinned by the callers, not offered as a row: %+v", rows)
		}
		if i > 0 && rows[i-1].Title > r.Title {
			t.Fatalf("rows must stay sorted by title: %+v", rows)
		}
	}
}

// TestScratchLangRowsIgnoreUnregisteredAlias covers the table's guard: an
// alias the language does not actually register would create a row whose
// extension nothing resolves, so it is dropped.
func TestScratchLangRowsIgnoreUnregisteredAlias(t *testing.T) {
	lang.Register(lang.Language{ID: "aliastest", Extensions: []string{"alt"}})
	scratchAliasExts["aliastest"] = []scratchLangRow{{Title: "Aliastest Dialect", Ext: "nope"}}
	t.Cleanup(func() { delete(scratchAliasExts, "aliastest") })

	for _, r := range scratchLangRows() {
		if r.Ext == "nope" {
			t.Fatalf("an unregistered alias extension must not be offered: %+v", r)
		}
	}
}

// TestScratchRowTitleNamesDialect covers the metadata column: a .js path reads
// as JavaScript, not as the language id that owns the extension.
func TestScratchRowTitleNamesDialect(t *testing.T) {
	for path, want := range map[string]string{
		"/tmp/scratch-1.js":  "JavaScript",
		"/tmp/scratch-1.TS":  "Typescript",
		"/tmp/scratch-1.txt": "Plain Text",
	} {
		if got := scratchRowTitle(path); got != want {
			t.Errorf("scratchRowTitle(%q) = %q, want %q", path, got, want)
		}
	}
}

// TestScratchNewOffersJavaScript covers the creation flow: the scratch.new
// picker lists JavaScript, its command is registered, and the row is found by
// the language name as well as by its extension.
func TestScratchNewOffersJavaScript(t *testing.T) {
	t.Setenv("IKE_CONFIG_DIR", t.TempDir())
	m := newSized()
	if _, ok := m.reg.Command("scratch.new.typescript.js"); !ok {
		t.Fatal("the JavaScript row needs a registered creator command")
	}

	has := func(items []palette.Item) bool {
		for _, it := range items {
			if it.Title == "JavaScript" {
				if it.Detail != ".js" {
					t.Errorf("detail = %q, want .js", it.Detail)
				}
				if it.Msg != (palette.RunCommandMsg{ID: "scratch.new.typescript.js"}) {
					t.Errorf("row msg = %+v, want the JavaScript creator", it.Msg)
				}
				return true
			}
		}
		return false
	}
	for _, query := range []string{"", "javascript", ".js"} {
		if !has(scratchNewMode{}.Results(query, palette.Context{})) {
			t.Fatalf("query %q must offer the JavaScript row", query)
		}
	}

	// Running the row creates a .js scratch and opens it.
	m = drainCmd(m, m.RunCommand("scratch.new.typescript.js"))
	got := m.activeWS().Panes.FocusedInstance().Editor().Path()
	if filepath.Ext(got) != ".js" {
		t.Fatalf("created scratch = %q, want a .js file", got)
	}
	if _, err := os.Stat(got); err != nil {
		t.Fatalf("the scratch must exist: %v", err)
	}
}

// TestScratchManagerSwitchesToJavaScript covers the manager's language step:
// the JavaScript row is found by ".js", choosing it renames the scratch and
// the list names it JavaScript.
func TestScratchManagerSwitchesToJavaScript(t *testing.T) {
	t.Setenv("IKE_CONFIG_DIR", t.TempDir())
	m := newSized()
	paths := smSeed(t, "txt")
	m = openManager(t, m)
	m = smSelect(t, m, filepath.Base(paths[0]))
	m = smKey(m, tea.Key{Code: 'l', Mod: tea.ModCtrl})
	if m.scratchMgr.step != smStepLang {
		t.Fatalf("step = %d, want the language picker", m.scratchMgr.step)
	}

	// Both the name and the extension find the row.
	for _, query := range []string{"javascript", ".js"} {
		m.scratchMgr.langSearch.Reset()
		m = smTypeAll(m, query)
		var found bool
		for _, l := range m.scratchMgr.filteredLangs() {
			if l.title == "JavaScript" && l.ext == "js" {
				found = true
			}
		}
		if !found {
			t.Fatalf("query %q must find the JavaScript row, got %+v", query, m.scratchMgr.filteredLangs())
		}
	}

	m.scratchMgr.langSearch.Reset()
	m = smTypeAll(m, "javascript")
	m.scratchMgr.langPick = 0
	for i, l := range m.scratchMgr.filteredLangs() {
		if l.title == "JavaScript" {
			m.scratchMgr.langPick = i
		}
	}
	m = smKey(m, tea.Key{Code: tea.KeyEnter})

	want := strings.TrimSuffix(paths[0], ".txt") + ".js"
	if _, err := os.Stat(want); err != nil {
		t.Fatalf("re-languaged scratch must exist: %v", err)
	}
	row, ok := m.scratchMgr.selected()
	if !ok || row.path != want || row.lang != "JavaScript" {
		t.Fatalf("row = %+v, want the .js scratch listed as JavaScript", row)
	}

	// Re-opening the picker preselects the scratch's current dialect, not the
	// language's primary extension.
	m = smKey(m, tea.Key{Code: 'l', Mod: tea.ModCtrl})
	if got := m.scratchMgr.langs[m.scratchMgr.langPick]; got.ext != "js" {
		t.Fatalf("preselected row = %+v, want the .js row", got)
	}
}
