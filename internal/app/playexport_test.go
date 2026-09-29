package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"ike/internal/palette"
)

// playExportApp is playApp with the first-start dialog dismissed, so the
// scripted keys reach the playground and its prompt.
func playExportApp(t *testing.T, body string) Model {
	t.Helper()
	m := playApp(t, body)
	if m.onboardingOpen() {
		m = m.closeOnboarding().(Model)
	}
	return m
}

// playExportRowFor returns the picker row for target, failing when it is missing.
func playExportRowFor(t *testing.T, m Model, target string) playExportRow {
	t.Helper()
	for _, r := range m.playExportRows() {
		if r.target == target {
			return r
		}
	}
	t.Fatalf("the export picker has no %s row", target)
	return playExportRow{}
}

// TestPlayExportChordOpensPicker: the default chord reaches the picker from
// the query line, locked to the export mode with every target listed.
func TestPlayExportChordOpensPicker(t *testing.T) {
	m := openJQ(t, playExportApp(t, `[{"a":1}]`))
	m = setProgram(m, ".")
	m = drainKey(m, tea.KeyPressMsg{Code: 'v', Mod: tea.ModCtrl | tea.ModAlt})
	if !m.palette.IsOpen() {
		t.Fatal("ctrl+alt+v must open the export picker")
	}
	if got := len(m.playExport.rows); got != 4 {
		t.Fatalf("the picker must list every target, got %d rows", got)
	}
}

// TestPlayExportGating: CSV / TSV are offered for a list of objects and shown
// unavailable with a reason otherwise; the HTTP row needs an open .http buffer.
func TestPlayExportGating(t *testing.T) {
	m := openJQ(t, playExportApp(t, `{"rows":[{"a":1},{"b":"x"}],"nested":[[1],[2]]}`))
	m = setProgram(m, ".rows")
	for _, target := range []string{playExportSave, playExportCSV, playExportTSV} {
		if r := playExportRowFor(t, m, target); r.reason != "" {
			t.Errorf("%s must be available for a list of objects, reason %q", target, r.reason)
		}
	}
	if r := playExportRowFor(t, m, playExportHTTP); !strings.Contains(r.reason, "no .http buffer") {
		t.Errorf("without an .http buffer the HTTP row must say so, reason %q", r.reason)
	}

	m = setProgram(m, ".nested")
	if r := playExportRowFor(t, m, playExportCSV); !strings.Contains(r.reason, "row 1 is an array") {
		t.Errorf("a list of arrays is no table, reason %q", r.reason)
	}
	m.playExport.rows = m.playExportRows()
	for _, it := range m.playExport.Results("", palette.Context{}) {
		if it.Title == "Copy as CSV" && it.Badge != "unavailable" {
			t.Errorf("a disabled row must be badged unavailable, got %q", it.Badge)
		}
	}

	m = setProgram(m, "empty")
	if r := playExportRowFor(t, m, playExportSave); r.reason == "" {
		t.Error("an empty result has nothing to save")
	}

	// Picking a disabled row repeats its reason instead of acting.
	copied := "untouched"
	prev := clipboardWrite
	clipboardWrite = func(s string) { copied = s }
	t.Cleanup(func() { clipboardWrite = prev })
	tm, _ := m.Update(PlayExportMsg{Target: playExportCSV, Reason: "no rows"})
	m = tm.(Model)
	if copied != "untouched" || !strings.Contains(m.play.status, "no rows") {
		t.Errorf("a disabled pick must explain, status %q clipboard %q", m.play.status, copied)
	}
}

// TestPlayExportCopiesCSV: the CSV row copies the table to the clipboard.
func TestPlayExportCopiesCSV(t *testing.T) {
	copied := ""
	prev := clipboardWrite
	clipboardWrite = func(s string) { copied = s }
	t.Cleanup(func() { clipboardWrite = prev })

	m := openJQ(t, playExportApp(t, `[{"n":"a,b","v":1},{"v":2,"w":[1]}]`))
	m = setProgram(m, ".")
	tm, cmd := m.Update(PlayExportMsg{Target: playExportCSV})
	m = drainCmd(tm.(Model), cmd)
	if want := "n,v,w\n\"a,b\",1,\n,2,[1]\n"; copied != want {
		t.Fatalf("csv = %q, want %q", copied, want)
	}
	tm, cmd = m.Update(PlayExportMsg{Target: playExportTSV})
	drainCmd(tm.(Model), cmd)
	if want := "n\tv\tw\na,b\t1\t\n\t2\t[1]\n"; copied != want {
		t.Fatalf("tsv = %q, want %q", copied, want)
	}
}

// TestPlayExportSaveAsFile: the prompt writes the shown form to the path and
// opens it; an existing file needs a second enter.
func TestPlayExportSaveAsFile(t *testing.T) {
	m := openJQ(t, playExportApp(t, `{"foo":[1,2]}`))
	m = setProgram(m, ".foo")
	tm, _ := m.Update(PlayExportMsg{Target: playExportSave})
	m = tm.(Model)
	if !m.playSaveFileOpen() {
		t.Fatal("the save row must open the path prompt")
	}
	if got := m.playSaveFile.input.Text; !strings.HasSuffix(got, "data-result.json") {
		t.Errorf("the prompt should default next to the source with the dialect's extension, got %q", got)
	}
	path := filepath.Join(t.TempDir(), "out.json")
	if err := os.WriteFile(path, []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}
	m.playSaveFile.input.Set(path)
	m = drainKey(m, tea.KeyPressMsg{Code: tea.KeyEnter})
	if !m.playSaveFileOpen() || !strings.Contains(m.playSaveFile.err, "enter again overwrites") {
		t.Fatalf("an existing file must ask before overwriting, err %q", m.playSaveFile.err)
	}
	if b, _ := os.ReadFile(path); string(b) != "old" {
		t.Fatal("the first enter must not write")
	}
	m = drainKey(m, tea.KeyPressMsg{Code: tea.KeyEnter})
	if m.playSaveFileOpen() {
		t.Fatal("the confirming enter must close the prompt")
	}
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(strings.Fields(string(b)), ""); got != "[1,2]" {
		t.Errorf("saved %q, want the result", got)
	}
	if ed := m.activeEditor(); ed == nil || ed.Path() != path {
		t.Errorf("the saved file must open, active editor %v", ed)
	}
}

// TestPlayExportHTTPBody: with an .http buffer open the result becomes the
// body of the request under its cursor — replacing one, or adding one.
func TestPlayExportHTTPBody(t *testing.T) {
	m := playExportApp(t, `{"id":7}`)
	dir := t.TempDir()
	httpPath := filepath.Join(dir, "api.http")
	src := "### create\nPOST https://x.test/items\nContent-Type: application/json\n\n{\"old\":true}\n\n### ping\nGET https://x.test/ping\n"
	if err := os.WriteFile(httpPath, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	tm, cmd := m.openPath(httpPath, false)
	m = drainCmd(tm.(Model), cmd)
	httpEd := m.activeEditor()
	jsonPath := filepath.Join(dir, "data.json")
	if err := os.WriteFile(jsonPath, []byte(`{"id":7}`), 0o644); err != nil {
		t.Fatal(err)
	}
	tm, cmd = m.openPath(jsonPath, false)
	m = drainCmd(tm.(Model), cmd)
	m = openJQ(t, m)
	m = setProgram(m, ".")

	if r := playExportRowFor(t, m, playExportHTTP); r.reason != "" {
		t.Fatalf("an open .http buffer must enable the HTTP row, reason %q", r.reason)
	}
	// Cursor on the first request: its body is replaced.
	httpEd.SetCursor(1, 0)
	tm, cmd = m.Update(PlayExportMsg{Target: playExportHTTP})
	m = drainCmd(tm.(Model), cmd)
	if got := httpEd.Text(); !strings.Contains(got, "application/json\n\n{\n  \"id\": 7\n}\n\n### ping") || strings.Contains(got, "old") {
		t.Fatalf("the body must be replaced, got:\n%s", got)
	}
	// Cursor on the bodiless request: a blank line and the body are added.
	httpEd.SetCursor(len(strings.Split(httpEd.Text(), "\n"))-2, 0)
	tm, cmd = m.Update(PlayExportMsg{Target: playExportHTTP})
	drainCmd(tm.(Model), cmd)
	if got := httpEd.Text(); !strings.Contains(got, "GET https://x.test/ping\n\n{\n  \"id\": 7\n}") {
		t.Fatalf("the body must be added after the head, got:\n%s", got)
	}

	// Several values are no single body.
	m = setProgram(m, ".id, .id")
	if r := playExportRowFor(t, m, playExportHTTP); !strings.Contains(r.reason, "2 values") {
		t.Errorf("a multi-value result must be refused, reason %q", r.reason)
	}
}
