package finder

import (
	"testing"

	"ike/internal/search"
)

// TestPrefillSelectsSelectionText guards #2165: opening with an active
// selection prefills the query and marks it selected, so the first typed
// character replaces it — no extra keys.
func TestPrefillSelectsSelectionText(t *testing.T) {
	m := New(search.New(nil))
	m.SetSize(100, 30)
	m.OpenPrefilled(t.TempDir(), "needle")

	if m.query != "needle" || !m.preselect {
		t.Fatalf("prefill should seed and select the query, query=%q preselect=%v", m.query, m.preselect)
	}
	if !m.scanning {
		t.Fatal("a prefilled query must start its scan immediately")
	}
	typeText(m, "x")
	if m.query != "x" {
		t.Fatalf("typing over the prefill should replace it, got %q", m.query)
	}
}

// TestPrefillOverridesRememberedQuery: the selection wins over the query the
// finder remembered from the previous search (#277 + #2165).
func TestPrefillOverridesRememberedQuery(t *testing.T) {
	m := opened(t)
	typeText(m, "old")
	m.Update(key("esc"))

	m.OpenPrefilled(t.TempDir(), "fresh")
	if m.query != "fresh" || !m.preselect {
		t.Fatalf("prefill must override the remembered query, query=%q preselect=%v", m.query, m.preselect)
	}
}

// TestPrefillRejectsMultiLine pins the documented multi-line rule: a
// selection spanning lines prefills nothing, matching the editor's "/" search
// (#2063) and the HTTP viewer's (#2122). A single trailing newline — a
// linewise selection of one line — is not a span and still prefills.
func TestPrefillRejectsMultiLine(t *testing.T) {
	for _, sel := range []string{"first line\nsecond line\nthird", "a\nb", "a\r\nb"} {
		m := opened(t)
		typeText(m, "remembered")
		m.Update(key("esc"))

		m.OpenPrefilled(t.TempDir(), sel)
		if m.query != "remembered" {
			t.Fatalf("multi-line selection %q must not prefill, query=%q", sel, m.query)
		}
	}

	for _, sel := range []string{"one line\n", "one line\r\n"} {
		m := New(search.New(nil))
		m.SetSize(100, 30)
		m.OpenPrefilled(t.TempDir(), sel)
		if m.query != "one line" {
			t.Fatalf("linewise one-line selection %q should prefill %q, got %q", sel, "one line", m.query)
		}
	}
}

// TestPrefillRegexModeEscapesMetacharacters: in regex mode the selected text
// is escaped so it is found literally.
func TestPrefillRegexModeEscapesMetacharacters(t *testing.T) {
	m := opened(t)
	m.regex = true
	m.Update(key("esc"))

	m.OpenPrefilled(t.TempDir(), "foo(bar).*")
	if want := `foo\(bar\)\.\*`; m.query != want {
		t.Fatalf("regex-mode prefill should be escaped to %q, got %q", want, m.query)
	}
	if m.lastQuery.Pattern != `foo\(bar\)\.\*` || !m.lastQuery.Regex {
		t.Fatalf("scan should run the escaped pattern in regex mode, got %+v", m.lastQuery)
	}

	// Literal mode inserts verbatim.
	m.Update(key("esc"))
	m.regex = false
	m.OpenPrefilled(t.TempDir(), "foo(bar).*")
	if m.query != "foo(bar).*" {
		t.Fatalf("literal-mode prefill should be verbatim, got %q", m.query)
	}
}

// TestPrefillEmptySelectionKeepsBehavior: no selection (or a blank one) leaves
// the remembered query untouched — the pre-#2165 behavior.
func TestPrefillEmptySelectionKeepsBehavior(t *testing.T) {
	for _, sel := range []string{"", "   ", "\n\nsecond"} {
		m := opened(t)
		typeText(m, "remembered")
		m.Update(key("esc"))

		m.OpenPrefilled(t.TempDir(), sel)
		if m.query != "remembered" {
			t.Fatalf("selection %q should not prefill, query=%q", sel, m.query)
		}
	}
}

// TestOpenReplacePrefilled: replace-in-path prefills on the same terms and
// stays in replace mode.
func TestOpenReplacePrefilled(t *testing.T) {
	m := New(search.New(nil))
	m.SetSize(100, 30)
	m.OpenReplacePrefilled(t.TempDir(), "needle")
	if m.query != "needle" || !m.replaceMode || !m.preselect {
		t.Fatalf("replace prefill: query=%q replaceMode=%v preselect=%v", m.query, m.replaceMode, m.preselect)
	}
}
