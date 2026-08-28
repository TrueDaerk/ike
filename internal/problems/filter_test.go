package problems

// The Problems pane's dialect of the shared filter syntax (#2156): what its
// schema parses, what each field gates, and that the 'f' quick key is the
// same filter a user could type.

import (
	"strings"
	"testing"

	ilsp "ike/internal/lsp"
)

// filterModel is a pane holding three findings across two files, with the
// display path shortened the way the app shortens it.
func filterModel(t *testing.T) *Model {
	t.Helper()
	s := NewStore()
	s.Set("/proj/internal/app/app.go", []ilsp.Diagnostic{
		{Severity: 1, Message: "undefined: foo", Code: "E42", Source: "gopls"},
		{Severity: 2, Message: "unused variable", Code: "W7", Source: "vet"},
	})
	s.Set("/proj/web/main.ts", []ilsp.Diagnostic{
		{Severity: 1, Message: "cannot find name", Code: "TS2304", Source: "tsserver"},
	})
	m := New(nil)
	m.SetSize(120, 12)
	m.SetDisplayPath(func(p string) string { return strings.TrimPrefix(p, "/proj/") })
	m.SetStore(s)
	return &m
}

// messages lists the diagnostic rows currently visible.
func messages(m *Model) []string {
	var out []string
	for _, r := range m.rows {
		if !r.header && r.rel == nil {
			out = append(out, r.d.Message)
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
	m.Refresh()
}

func TestFilterFields(t *testing.T) {
	for _, tt := range []struct {
		expr string
		want []string
	}{
		{"", []string{"undefined: foo", "unused variable", "cannot find name"}},
		{"severity:warning", []string{"unused variable"}},
		{"sev:error", []string{"undefined: foo", "cannot find name"}},
		// Repeats of one field OR together.
		{"severity:warning severity:error", []string{"undefined: foo", "unused variable", "cannot find name"}},
		{"file:*.ts", []string{"cannot find name"}},
		{"file:internal/app", []string{"undefined: foo", "unused variable"}},
		{"code:TS", []string{"cannot find name"}},
		{"source:vet", []string{"unused variable"}},
		// Free text is the fuzzy gate, over message and code.
		{"undef", []string{"undefined: foo"}},
		// Fields of different names AND together.
		{"sev:error file:*.go", []string{"undefined: foo"}},
		{"sev:error file:*.go nothinghere", nil},
	} {
		t.Run(tt.expr, func(t *testing.T) {
			m := filterModel(t)
			setFilter(t, m, tt.expr)
			got := messages(m)
			if strings.Join(got, "|") != strings.Join(tt.want, "|") {
				t.Fatalf("rows = %v, want %v", got, tt.want)
			}
		})
	}
}

// A file the filter empties loses its header too, so no dangling group stays.
func TestFilterDropsEmptyFileHeaders(t *testing.T) {
	m := filterModel(t)
	setFilter(t, m, "file:*.ts")
	for _, r := range m.rows {
		if r.header && r.path != "/proj/web/main.ts" {
			t.Fatalf("header of a filtered-out file survived: %q", r.path)
		}
	}
	if m.Rows() != 2 {
		t.Fatalf("rows = %d, want header + 1 diagnostic", m.Rows())
	}
}

// The header counts and the empty text follow the filter.
func TestFilterDrivesHeaderCountsAndEmptyText(t *testing.T) {
	m := filterModel(t)
	setFilter(t, m, "severity:warning")
	if v := m.View(); !strings.Contains(v, "0 errors · 1 warning") {
		t.Fatalf("header counts must be the visible ones:\n%s", v)
	}
	setFilter(t, m, "nothing matches this")
	if v := m.View(); !strings.Contains(v, "(no problems match the filter)") {
		t.Fatalf("empty text:\n%s", v)
	}
}

// The schema rejects what it does not know, with advice naming its fields.
func TestFilterRejectsUnknownField(t *testing.T) {
	m := filterModel(t)
	m.filter.SetText("author:me")
	if err := m.filter.Err(); !strings.Contains(err, "unknown qualifier") ||
		!strings.Contains(err, "severity:") {
		t.Fatalf("err = %q", err)
	}
}

// "/" focuses the row and typing lands in the filter rather than in the
// pane's own single-key actions.
func TestSlashFocusesTheFilter(t *testing.T) {
	m := filterModel(t)
	m.Update(key("/"))
	if !m.filter.Active() {
		t.Fatal("/ must focus the filter")
	}
	for _, r := range "sev:warning" {
		m.Update(key(string(r)))
	}
	if got := messages(m); len(got) != 1 || got[0] != "unused variable" {
		t.Fatalf("typing into the filter did not narrow the list: %v", got)
	}
	// 'f' was typed, not treated as the scope toggle.
	if m.FileOnly() {
		t.Fatal("keys typed into the filter must not trigger pane shortcuts")
	}
	m.Update(key("esc"))
	if m.filter.Active() || len(messages(m)) != 3 {
		t.Fatalf("esc must clear and blur: rows=%v", messages(m))
	}
}

// The 'f' quick key is sugar: it writes scope:file into the very same filter.
func TestQuickFileScopeIsAFilterTerm(t *testing.T) {
	m := filterModel(t)
	m.SetActivePath("/proj/web/main.ts")
	m.Update(key("f"))
	if m.filter.Text() != "scope:file" {
		t.Fatalf("f must write the filter, text = %q", m.filter.Text())
	}
	if got := messages(m); len(got) != 1 || got[0] != "cannot find name" {
		t.Fatalf("scope rows = %v", got)
	}
	// Typing the same term by hand is the same filter.
	m2 := filterModel(t)
	m2.SetActivePath("/proj/web/main.ts")
	setFilter(t, m2, "scope:file")
	if !m2.FileOnly() || strings.Join(messages(m2), "|") != strings.Join(messages(m), "|") {
		t.Fatalf("typed scope:file = %v, quick key = %v", messages(m2), messages(m))
	}
	// And it composes with the rest of the expression.
	m.Update(key("/"))
	for _, r := range " cannot" {
		m.Update(key(string(r)))
	}
	if m.filter.Text() != "scope:file cannot" {
		t.Fatalf("text = %q", m.filter.Text())
	}
	if got := messages(m); len(got) != 1 {
		t.Fatalf("rows = %v", got)
	}
}

// The footer advertises the shared focus key.
func TestFooterNamesTheFilterKey(t *testing.T) {
	m := filterModel(t)
	if v := m.View(); !strings.Contains(v, "/ filter") {
		t.Fatalf("footer must name the filter key:\n%s", v)
	}
}
