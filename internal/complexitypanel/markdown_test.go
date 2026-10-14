package complexitypanel

import (
	"strings"
	"testing"
)

// markdown_test.go covers the 'E' export (#2963): the Changes tab's
// verdict, totals and sections, the Project tab's filtered table, the
// group table, and the footer hint.

func TestMarkdownExportChanges(t *testing.T) {
	m := changesPanel(t)
	msg, ok := m.Update(key("E"))().(ExportMarkdownMsg)
	if !ok || msg.Label != "Changes" {
		t.Fatalf("export = %+v", msg)
	}
	want := "**Complexity: MORE COMPLEX**\n\n" +
		"base HEAD → working tree · functions 3→3 · ΣCCN 20→20 (0) · Σcog 17→24 (+7) · excess 0→5 (+5) · more complex · 1 renamed (removed + added): internal/ui/old.go → internal/ui/list.go\n\n" +
		"### Changed (2)\n\n" +
		"| function | file | line | ccn | cog | nloc | cmt% |\n" +
		"| --- | --- | ---: | ---: | ---: | ---: | ---: |\n" +
		"| render\\* | internal/app/app.go | 1 | 10→15 (+5) | 12→20 (+8) | 21→25 | 0→0 (0) |\n" +
		"| tiny\\* | internal/app/app.go | 35 | 4→3 (-1) | 3→3 (0) | 3→3 | 0→0 (0) |\n\n" +
		"### Added (1)\n\n" +
		"| function | file | line | ccn | cog | nloc | cmt% |\n" +
		"| --- | --- | ---: | ---: | ---: | ---: | ---: |\n" +
		"| fresh\\* | internal/app/app.go | 41 | -→2 (+2) | -→1 (+1) | -→5 | -→0 (0) |\n\n" +
		"### Removed (1)\n\n" +
		"| function | file | line | ccn | cog | nloc | cmt% |\n" +
		"| --- | --- | ---: | ---: | ---: | ---: | ---: |\n" +
		"| gone | internal/ui/old.go | 1 | 6→- (-6) | 2→- (-2) | 10→- | 0→- (0) |\n"
	if msg.Markdown != want {
		t.Errorf("markdown =\n%s\nwant\n%s", msg.Markdown, want)
	}
	// The CSV export is unchanged by the Markdown one.
	if exp := m.Update(key("e"))().(ExportMsg); !strings.HasPrefix(exp.CSV, strings.Join(changesCSVHeader, ",")) {
		t.Errorf("csv = %q", exp.CSV)
	}
}

func TestMarkdownExportProjectFollowsFilter(t *testing.T) {
	m := newPanel(t)
	m.filter.SetText("lang:go")
	m.Refresh()
	md := m.Markdown()
	if !strings.HasPrefix(md, "**gen ") || !strings.Contains(md, "| file | function | line | ccn | cog | nloc | params | depth | cmt% |\n| --- | --- | ---: |") {
		t.Fatalf("markdown =\n%s", md)
	}
	if !strings.Contains(md, "| internal/app/app.go | render | 1 | 25 | 30 | 21 | 1 | 2 | 0 |") {
		t.Errorf("first row missing:\n%s", md)
	}
	if n := strings.Count(md, "\n| ") - 2; n != len(m.Rows()) {
		t.Errorf("%d table rows, want the %d visible ones:\n%s", n, len(m.Rows()), md)
	}
	if strings.Contains(md, "▼") {
		t.Errorf("the sort marker leaked into the header:\n%s", md)
	}
}

func TestMarkdownExportGroupAndFooter(t *testing.T) {
	m := groupPanel(t)
	m.CycleGroup()
	md := m.Markdown()
	if !strings.Contains(md, "| file | class | funcs | nloc | Σccn |") || !strings.Contains(md, "| b/client.py | Client | 1 | 26 | 16 |") {
		t.Errorf("group markdown =\n%s", md)
	}
	if !strings.Contains(m.footer(), "E md") {
		t.Errorf("footer lacks the E hint: %q", m.footer())
	}
}
