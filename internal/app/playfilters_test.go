package app

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"ike/internal/jqplay"
	"ike/internal/palette"
)

// playfilters_test.go covers the named saved-filter library (#1995): saving from
// the playground into either scope, the picker's name filtering and scope
// badges, inserting a filter into the query line, rename/delete from the
// picker, and that all of it is on disk — the "survives a restart" case, which
// here is a second read of the store the model wrote.

// typeName feeds a name into the open filter-name prompt.
func typeName(m Model, name string) Model {
	for _, r := range name {
		tm, _ := m.Update(keyMsg(r))
		m = tm.(Model)
	}
	return m
}

// pressKey feeds one non-printable key through the app's dispatch.
func pressKey(m Model, code rune) Model {
	tm, cmd := m.Update(tea.KeyPressMsg{Code: code})
	return drainCmd(asModel(tm), cmd)
}

// asModel unwraps the tea.Model an Update returns.
func asModel(tm tea.Model) Model { return tm.(Model) }

// saveFilter runs the whole save flow from an open playground: the ctrl+s
// chord, the typed name, an optional scope toggle, enter.
func saveFilter(t *testing.T, m Model, name string, global bool) Model {
	t.Helper()
	tm, _ := m.Update(tea.KeyPressMsg{Code: 's', Mod: tea.ModCtrl})
	m = asModel(tm)
	if !m.playNamePromptOpen() {
		t.Fatal("ctrl+s must open the save-filter prompt")
	}
	m = typeName(m, name)
	if global {
		m = pressKey(m, tea.KeyTab)
		if m.playName.scope != jqplay.ScopeGlobal {
			t.Fatal("tab must switch the save prompt to the global scope")
		}
	}
	m = pressKey(m, tea.KeyEnter)
	if m.playNamePromptOpen() {
		t.Fatalf("the prompt stayed open: %q", m.playName.err)
	}
	return m
}

// TestJQFilterSavePersistsPerScope is the issue's acceptance case: a filter
// saved from the playground is on disk in the scope it was saved to, and a
// fresh read — what a restarted IDE does — finds it there.
func TestJQFilterSavePersistsPerScope(t *testing.T) {
	m := openJQ(t, playApp(t, `{"hits":{"hits":[{"_source":{"a":1}}]}}`))
	m = setProgram(m, ".hits.hits[] | ._source")
	m = saveFilter(t, m, "es hits", false)
	m = setProgram(m, ".hits.hits | length")
	m = saveFilter(t, m, "es count", true)

	proj := loadPlayFilters(jqplay.DialectJQ, jqplay.ScopeProject)
	if f, ok := proj.Get("es hits"); !ok || f.Program != ".hits.hits[] | ._source" {
		t.Fatalf("project store holds %+v (ok=%v)", f, ok)
	}
	if proj.Has("es count") {
		t.Fatal("the global save must not land in the project store")
	}
	global := loadPlayFilters(jqplay.DialectJQ, jqplay.ScopeGlobal)
	if f, ok := global.Get("es count"); !ok || f.Program != ".hits.hits | length" {
		t.Fatalf("global store holds %+v (ok=%v)", f, ok)
	}
	if global.Has("es hits") {
		t.Fatal("the project save must not land in the global store")
	}
	if playFilterFile(jqplay.DialectJQ, jqplay.ScopeProject) == playFilterFile(jqplay.DialectJQ, jqplay.ScopeGlobal) {
		t.Fatal("the two scopes must never share one file")
	}
}

// TestJQFilterSaveRefusesEmptyProgram: the identity program is the
// playground's default, not something worth a name.
func TestJQFilterSaveRefusesEmptyProgram(t *testing.T) {
	m := openJQ(t, playApp(t, `{"a":1}`))
	m = setProgram(m, ".")
	tm, _ := m.Update(tea.KeyPressMsg{Code: 's', Mod: tea.ModCtrl})
	m = asModel(tm)
	if m.playNamePromptOpen() {
		t.Fatal("the identity program must not open the save prompt")
	}
	if m.play.status == "" {
		t.Fatal("the refusal must say why on the info row")
	}
}

// TestJQFilterSaveOverwriteNeedsSecondEnter: a taken name never silently
// replaces a saved filter — and the second enter is how an edited filter is
// written back under the same name.
func TestJQFilterSaveOverwriteNeedsSecondEnter(t *testing.T) {
	m := openJQ(t, playApp(t, `{"a":1}`))
	m = setProgram(m, ".a")
	m = saveFilter(t, m, "mine", false)

	m = setProgram(m, ".a + 1")
	tm, _ := m.Update(tea.KeyPressMsg{Code: 's', Mod: tea.ModCtrl})
	m = typeName(asModel(tm), "mine")
	m = pressKey(m, tea.KeyEnter)
	if !m.playNamePromptOpen() {
		t.Fatal("a taken name must hold the prompt open for a confirmation")
	}
	if !strings.Contains(m.playName.err, "overwrites") {
		t.Fatalf("the guard message is %q", m.playName.err)
	}
	if f, _ := loadPlayFilters(jqplay.DialectJQ, jqplay.ScopeProject).Get("mine"); f.Program != ".a" {
		t.Fatalf("the first enter must not have written: %q", f.Program)
	}
	m = pressKey(m, tea.KeyEnter)
	if m.playNamePromptOpen() {
		t.Fatal("the second enter must commit the overwrite")
	}
	if f, _ := loadPlayFilters(jqplay.DialectJQ, jqplay.ScopeProject).Get("mine"); f.Program != ".a + 1" {
		t.Fatalf("the edit was not written back: %q", f.Program)
	}
}

// TestJQFilterPickerFiltersAndBadgesScope covers the picker itself: rows are
// fuzzy-matched over the *name*, the program rides along as the detail
// preview, and project and global entries carry different badges.
func TestJQFilterPickerFiltersAndBadgesScope(t *testing.T) {
	m := newSized()
	proj := loadPlayFilters(jqplay.DialectJQ, jqplay.ScopeProject)
	if err := proj.Set("es hits", ".hits.hits[] | ._source"); err != nil {
		t.Fatal(err)
	}
	if !m.savePlayFilters(jqplay.DialectJQ, jqplay.ScopeProject, proj) {
		t.Fatal("saving the project store failed")
	}
	global := loadPlayFilters(jqplay.DialectJQ, jqplay.ScopeGlobal)
	if err := global.Set("errors only", `.[] | select(.level == "error")`); err != nil {
		t.Fatal(err)
	}
	if !m.savePlayFilters(jqplay.DialectJQ, jqplay.ScopeGlobal, global) {
		t.Fatal("saving the global store failed")
	}

	mode := m.playFilters
	mode.Refresh()
	all := mode.Results("", palette.Context{})
	if len(all) != 2 {
		t.Fatalf("got %d rows for the empty query, want both scopes: %+v", len(all), all)
	}
	if all[0].Badge != "project" || all[1].Badge != "global" {
		t.Fatalf("scope badges are %q/%q", all[0].Badge, all[1].Badge)
	}
	if !strings.Contains(all[0].Detail, "._source") {
		t.Fatalf("the row must preview its program, got %q", all[0].Detail)
	}

	hits := mode.Results("hits", palette.Context{})
	if len(hits) != 1 || hits[0].Title != "es hits" {
		t.Fatalf("the name filter matched %+v", hits)
	}
	if none := mode.Results("zzz", palette.Context{}); len(none) != 0 {
		t.Fatalf("a non-matching query returned %+v", none)
	}
	// The program is deliberately not part of the match: a name is what a
	// saved filter is remembered by.
	if byProgram := mode.Results("select", palette.Context{}); len(byProgram) != 0 {
		t.Fatalf("the picker filters by name only, got %+v", byProgram)
	}
}

// TestJQFilterInsertPutsProgramOnQueryLine: picking a filter replaces the
// query line and evaluates it.
func TestJQFilterInsertPutsProgramOnQueryLine(t *testing.T) {
	m := openJQ(t, playApp(t, `{"foo":[{"bar":1},{"bar":4}]}`))
	m = setProgram(m, ".foo")
	m = saveFilter(t, m, "big bars", false)
	m = setProgram(m, ".")

	tm, cmd := m.Update(InsertFilterMsg{Scope: jqplay.ScopeProject, Name: "big bars"})
	m = drainCmd(asModel(tm), cmd)
	if m.play.program.Text != ".foo" {
		t.Fatalf("the query line is %q, want the saved program", m.play.program.Text)
	}
	if m.play.bufFocus {
		t.Fatal("an insert belongs to the query line, which must hold the keyboard")
	}
	if m.play.result.Err != "" || len(m.play.result.Outputs) != 1 {
		t.Fatalf("the inserted filter did not run: err=%q outputs=%v", m.play.result.Err, m.play.result.Outputs)
	}
}

// TestJQFilterInsertOpensPlaygroundWhenClosed: the picker is reachable from
// the palette with no playground up, so picking one has to open it.
func TestJQFilterInsertOpensPlaygroundWhenClosed(t *testing.T) {
	m := playApp(t, `{"foo":1}`)
	lib := loadPlayFilters(jqplay.DialectJQ, jqplay.ScopeGlobal)
	if err := lib.Set("foo", ".foo"); err != nil {
		t.Fatal(err)
	}
	if !m.savePlayFilters(jqplay.DialectJQ, jqplay.ScopeGlobal, lib) {
		t.Fatal("saving the global store failed")
	}
	tm, cmd := m.Update(InsertFilterMsg{Scope: jqplay.ScopeGlobal, Name: "foo"})
	m = drainCmd(asModel(tm), cmd)
	if !m.playOpen() {
		t.Fatal("picking a filter with no playground up must open one")
	}
	if m.play.program.Text != ".foo" {
		t.Fatalf("the query line is %q, want the saved program", m.play.program.Text)
	}
	if m.play.result.Err != "" || m.play.result.Text() != "1" {
		t.Fatalf("the filter did not run: err=%q out=%q", m.play.result.Err, m.play.result.Text())
	}
}

// TestJQFilterRenameFromPicker walks the picker's rename spelling: the entry's
// row opens the prompt prefilled, and the new name is what the store holds.
func TestJQFilterRenameFromPicker(t *testing.T) {
	m := openJQ(t, playApp(t, `{"a":1}`))
	m = setProgram(m, ".a")
	m = saveFilter(t, m, "old name", false)

	tm, _ := m.Update(RenameFilterPromptMsg{Scope: jqplay.ScopeProject, Name: "old name"})
	m = asModel(tm)
	if !m.playNamePromptOpen() || m.playName.input.Text != "old name" {
		t.Fatalf("the rename prompt opened as %+v", m.playName)
	}
	// Clear the prefilled name and type the new one.
	for range "old name" {
		m = pressKey(m, tea.KeyBackspace)
	}
	m = typeName(m, "new name")
	m = pressKey(m, tea.KeyEnter)
	if m.playNamePromptOpen() {
		t.Fatalf("the rename prompt stayed open: %q", m.playName.err)
	}
	lib := loadPlayFilters(jqplay.DialectJQ, jqplay.ScopeProject)
	if lib.Has("old name") {
		t.Fatal("the old name survived the rename")
	}
	if f, ok := lib.Get("new name"); !ok || f.Program != ".a" {
		t.Fatalf("the renamed entry is %+v (ok=%v)", f, ok)
	}
}

// TestJQFilterRenameRefusesTakenName: a rename must never swallow another
// filter — only the deliberate, confirmed save overwrite may replace one.
func TestJQFilterRenameRefusesTakenName(t *testing.T) {
	m := openJQ(t, playApp(t, `{"a":1}`))
	m = setProgram(m, ".a")
	m = saveFilter(t, m, "one", false)
	m = setProgram(m, ".a + 1")
	m = saveFilter(t, m, "two", false)

	tm, _ := m.Update(RenameFilterPromptMsg{Scope: jqplay.ScopeProject, Name: "one"})
	m = asModel(tm)
	for range "one" {
		m = pressKey(m, tea.KeyBackspace)
	}
	m = typeName(m, "two")
	m = pressKey(m, tea.KeyEnter)
	if !m.playNamePromptOpen() {
		t.Fatal("renaming onto a taken name must hold the prompt open")
	}
	lib := loadPlayFilters(jqplay.DialectJQ, jqplay.ScopeProject)
	if f, _ := lib.Get("two"); f.Program != ".a + 1" {
		t.Fatalf("the other filter was clobbered: %q", f.Program)
	}
	if !lib.Has("one") {
		t.Fatal("the refused rename must leave the entry alone")
	}
}

// TestJQFilterDeleteFromPicker: the picker's aux action removes the entry from
// its scope's store, for good.
func TestJQFilterDeleteFromPicker(t *testing.T) {
	m := openJQ(t, playApp(t, `{"a":1}`))
	m = setProgram(m, ".a")
	m = saveFilter(t, m, "gone soon", false)
	m = setProgram(m, ".a + 1")
	m = saveFilter(t, m, "kept", false)

	tm, _ := m.Update(DeleteFilterMsg{Scope: jqplay.ScopeProject, Name: "gone soon"})
	m = asModel(tm)
	lib := loadPlayFilters(jqplay.DialectJQ, jqplay.ScopeProject)
	if lib.Has("gone soon") {
		t.Fatal("the deleted filter is still in the store")
	}
	if !lib.Has("kept") {
		t.Fatal("the delete took the wrong entry")
	}
}

// TestJQFilterPickerEmptyExplains: with nothing saved the command says where
// filters come from instead of opening an empty list.
func TestJQFilterPickerEmptyExplains(t *testing.T) {
	m := newSized()
	tm, _ := m.Update(ShowFiltersMsg{})
	m = asModel(tm)
	if m.palette.IsOpen() {
		t.Fatal("an empty library must not open the picker")
	}
}
