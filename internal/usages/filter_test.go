package usages

// The Usages pane's dialect of the shared filter syntax (#2156): what its
// schema parses, what each field gates, and that the filter narrows the rows
// and the totals without losing the result set behind them.

import (
	"strings"
	"testing"

	ilsp "ike/internal/lsp"
)

// filterModel is a pane holding four hits across three files.
func filterModel(t *testing.T) *Model {
	t.Helper()
	m := New(nil)
	m.SetSize(120, 12)
	m.SetDisplayPath(func(p string) string { return strings.TrimPrefix(p, "/proj/") })
	m.Set("Foo", []ilsp.Reference{
		ref("/proj/internal/app/app.go", 3, 1, "return Foo()"),
		ref("/proj/internal/app/app.go", 9, 4, "x := Foo{}"),
		ref("/proj/web/main.ts", 1, 0, "const f = Foo"),
		ref("/proj/docs/readme.md", 2, 0, "call Foo here"),
	}, nil)
	return &m
}

// previews lists the reference rows currently visible.
func previews(m *Model) []string {
	var out []string
	for _, r := range m.rows {
		if !r.header {
			out = append(out, r.ref.Preview)
		}
	}
	return out
}

func setFilter(t *testing.T, m *Model, expr string) {
	t.Helper()
	m.filter.SetText(expr)
	if err := m.filter.Err(); err != "" {
		t.Fatalf("filter %q: %s", expr, err)
	}
	m.rebuild()
}

func TestFilterFields(t *testing.T) {
	for _, tt := range []struct {
		expr string
		want []string
	}{
		{"", []string{"return Foo()", "x := Foo{}", "const f = Foo", "call Foo here"}},
		{"file:*.ts", []string{"const f = Foo"}},
		{"file:internal/app", []string{"return Foo()", "x := Foo{}"}},
		// Repeats of one field OR together.
		{"file:*.ts file:*.md", []string{"const f = Foo", "call Foo here"}},
		{"text:return", []string{"return Foo()"}},
		// Free text is the fuzzy gate over the path and the preview.
		{"const", []string{"const f = Foo"}},
		{"file:*.go x :=", []string{"x := Foo{}"}},
	} {
		t.Run(tt.expr, func(t *testing.T) {
			m := filterModel(t)
			setFilter(t, m, tt.expr)
			got := previews(m)
			if strings.Join(got, "|") != strings.Join(tt.want, "|") {
				t.Fatalf("rows = %v, want %v", got, tt.want)
			}
		})
	}
}

// The title totals and the empty text follow the filter; the unfiltered
// result set stays behind it, so widening again restores every hit.
func TestFilterDrivesTotalsAndIsReversible(t *testing.T) {
	m := filterModel(t)
	setFilter(t, m, "file:internal/app")
	if m.Count() != 2 || m.Files() != 1 {
		t.Fatalf("totals = %d in %d files, want 2 in 1", m.Count(), m.Files())
	}
	if got := m.Title(); !strings.Contains(got, "2 in 1 file") {
		t.Fatalf("title = %q", got)
	}
	setFilter(t, m, "nothing here matches")
	if m.Rows() != 0 {
		t.Fatalf("rows = %d, want none", m.Rows())
	}
	if v := m.View(); !strings.Contains(v, "(no usages match the filter)") {
		t.Fatalf("empty text:\n%s", v)
	}
	setFilter(t, m, "")
	if m.Count() != 4 || m.Files() != 3 {
		t.Fatalf("clearing the filter lost hits: %d in %d files", m.Count(), m.Files())
	}
}

// A file the filter empties loses its header too.
func TestFilterDropsEmptyFileHeaders(t *testing.T) {
	m := filterModel(t)
	setFilter(t, m, "file:*.md")
	if m.Rows() != 2 {
		t.Fatalf("rows = %d, want header + 1 hit", m.Rows())
	}
	if !m.rows[0].header || m.rows[0].path != "/proj/docs/readme.md" {
		t.Fatalf("wrong header survived: %+v", m.rows[0])
	}
}

// "/" focuses the row and typing lands in the filter, not in the pane's own
// single-key actions.
func TestSlashFocusesTheFilter(t *testing.T) {
	m := filterModel(t)
	m.Update(key("/"))
	if !m.filter.Active() {
		t.Fatal("/ must focus the filter")
	}
	for _, r := range "file:*.ts" {
		m.Update(key(string(r)))
	}
	if got := previews(m); len(got) != 1 || got[0] != "const f = Foo" {
		t.Fatalf("typing into the filter did not narrow the list: %v", got)
	}
	m.Update(key("esc"))
	if m.filter.Active() || len(previews(m)) != 4 {
		t.Fatalf("esc must clear and blur: rows=%v", previews(m))
	}
}

// A key the bar does not own still reaches the pane while the filter has
// focus, so the list can be steered mid-typing.
func TestNavigationWorksWhileFiltering(t *testing.T) {
	m := filterModel(t)
	m.Update(key("/"))
	before := m.Cursor()
	m.Update(key("down"))
	if m.Cursor() == before {
		t.Fatal("down must still move the list while the filter is focused")
	}
}

func TestFilterRejectsUnknownField(t *testing.T) {
	m := filterModel(t)
	m.filter.SetText("severity:error")
	if err := m.filter.Err(); !strings.Contains(err, "unknown qualifier") ||
		!strings.Contains(err, "file:") {
		t.Fatalf("err = %q", err)
	}
}

func TestFooterNamesTheFilterKey(t *testing.T) {
	m := filterModel(t)
	if v := m.View(); !strings.Contains(v, "/ filter") {
		t.Fatalf("footer must name the filter key:\n%s", v)
	}
}
