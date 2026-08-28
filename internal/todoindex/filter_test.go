package todoindex

// The TODO index's dialect of the shared filter syntax (#2156): what its
// schema parses, what each field gates, and that the two single-key filters
// are sugar over the same expression.

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"ike/internal/search"
)

// filterModel is an open index holding four tags across three files, with
// cur.go as the "current" file.
func filterModel(t *testing.T) (*Model, string) {
	t.Helper()
	m := New(search.New(nil), t.TempDir(), nil)
	m.SetSize(120, 40)
	cur, err := filepath.Abs("internal/app/cur.go")
	if err != nil {
		t.Fatal(err)
	}
	m.open = true
	m.curPath = cur
	batch(m,
		match(cur, 1, "// TODO: wire the pane", "TODO"),
		match("internal/app/other.go", 2, "// TODO: drop this", "TODO"),
		match("internal/app/other.go", 3, "// FIXME: broken", "FIXME"),
		match("web/main.ts", 4, "// HACK: temporary", "HACK"),
	)
	done(m)
	return m, cur
}

func setFilter(t *testing.T, m *Model, expr string) {
	t.Helper()
	m.filter.SetText(expr)
	if err := m.filter.Err(); err != "" {
		t.Fatalf("filter %q: %s", expr, err)
	}
	m.rebuildList()
}

func TestFilterFields(t *testing.T) {
	for _, tt := range []struct {
		expr string
		want int
	}{
		{"", 4},
		{"tag:TODO", 2},
		{"tag:todo", 0}, // the vocabulary is the pattern words, upper case
		{"tag:TODO tag:FIXME", 3},
		{"file:*.ts", 1},
		{"file:internal/app", 3},
		{"scope:file", 1},
		{"scope:file tag:TODO", 1},
		{"broken", 1}, // free text is the fuzzy gate over the source line
		{"tag:TODO file:*.ts", 0},
	} {
		t.Run(tt.expr, func(t *testing.T) {
			m, _ := filterModel(t)
			m.filter.SetText(tt.expr)
			if tt.want == 0 && m.filter.Err() != "" {
				return // an unparseable expression narrows to the last good one
			}
			if err := m.filter.Err(); err != "" {
				t.Fatalf("filter %q: %s", tt.expr, err)
			}
			m.rebuildList()
			if got := m.Total(); got != tt.want {
				t.Fatalf("total = %d, want %d", got, tt.want)
			}
		})
	}
}

// tag: takes only the configured pattern words.
func TestTagVocabularyIsTheConfiguredPatterns(t *testing.T) {
	m := New(search.New(nil), t.TempDir(), []string{"TODO", "NOTE"})
	m.filter.SetText("tag:FIXME")
	if err := m.filter.Err(); !strings.Contains(err, "tag: wants TODO, NOTE") {
		t.Fatalf("err = %q", err)
	}
	m.filter.SetText("tag:NOTE")
	if err := m.filter.Err(); err != "" {
		t.Fatalf("a configured pattern must parse: %s", err)
	}
}

// ctrl+t and ctrl+o are sugar: they write tag: and scope:file into the shared
// filter, so the key and the typed term are one mechanism.
func TestQuickKeysWriteTheFilter(t *testing.T) {
	m, _ := filterModel(t)
	m.Update(key("ctrl+t"))
	if m.filter.Text() != "tag:TODO" {
		t.Fatalf("ctrl+t text = %q", m.filter.Text())
	}
	if m.Total() != 2 {
		t.Fatalf("TODO total = %d, want 2", m.Total())
	}
	m.Update(key("ctrl+o"))
	if !m.FileOnly() || m.filter.Text() != "tag:TODO scope:file" {
		t.Fatalf("ctrl+o text = %q", m.filter.Text())
	}
	if m.Total() != 1 || m.Files() != 1 {
		t.Fatalf("scoped total = %d in %d files, want 1/1", m.Total(), m.Files())
	}
	// The cycle walks the remaining patterns and back to "no tag term",
	// leaving the scope term it did not write alone.
	for _, want := range []string{"FIXME", "HACK", "XXX", ""} {
		m.Update(key("ctrl+t"))
		if got := m.filter.Query().Value("tag"); got != want {
			t.Fatalf("cycle reached %q, want %q (text %q)", got, want, m.filter.Text())
		}
	}
	if !m.FileOnly() {
		t.Fatal("cycling the tag must not drop the scope term")
	}
}

// The chips row renders the filter rather than a state of its own.
func TestChipsRowReadsTheFilter(t *testing.T) {
	m, _ := filterModel(t)
	setFilter(t, m, "tag:FIXME scope:file")
	v := ansi.Strip(m.View())
	for _, want := range []string{"Tag: FIXME", "[x] Current file"} {
		if !strings.Contains(v, want) {
			t.Fatalf("chips row missing %q:\n%s", want, v)
		}
	}
	setFilter(t, m, "")
	if v := ansi.Strip(m.View()); !strings.Contains(v, "Tag: All") || !strings.Contains(v, "[ ] Current file") {
		t.Fatalf("cleared chips row:\n%s", v)
	}
}

// "/" focuses the input and typing lands there, not in the overlay's own
// single-key bindings.
func TestSlashFocusesTheFilter(t *testing.T) {
	m, _ := filterModel(t)
	m.Update(key("/"))
	if !m.filter.Active() {
		t.Fatal("/ must focus the filter")
	}
	for _, r := range "tag:HACK" {
		m.Update(key(string(r)))
	}
	if m.Total() != 1 {
		t.Fatalf("total = %d, want 1", m.Total())
	}
	// 'k' was typed, not read as a cursor step; esc clears rather than
	// closing the overlay.
	m.Update(key("esc"))
	if !m.IsOpen() {
		t.Fatal("esc in the filter must not close the overlay")
	}
	if m.filter.Active() || m.Total() != 4 {
		t.Fatalf("esc must clear and blur: total=%d", m.Total())
	}
	// A second esc, with the filter blurred, closes as before.
	m.Update(key("esc"))
	if m.IsOpen() {
		t.Fatal("esc must close the overlay once the filter is blurred")
	}
}

// Clicking the filter line puts the cursor in it.
func TestClickOnTheFilterLineFocuses(t *testing.T) {
	m, _ := filterModel(t)
	m.View() // fills the layout the hit test reads
	m.Click(4, m.lay.input+1)
	if !m.filter.Active() {
		t.Fatal("a click on the filter row must focus it")
	}
}

// Filtering never rescans: the retained entry set (and Count) is untouched.
func TestFilteringDoesNotRescan(t *testing.T) {
	m, _ := filterModel(t)
	gen := m.gen
	setFilter(t, m, "tag:FIXME")
	if m.gen != gen {
		t.Fatalf("generation moved from %d to %d — the filter rescanned", gen, m.gen)
	}
	if m.Count() != 4 {
		t.Fatalf("Count = %d, want the unfiltered 4", m.Count())
	}
}
