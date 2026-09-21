package phpindex

// hostview_refs_test.go covers the references half of the host view
// (#2671): what an empty server answer inside a trait body gets, what a
// non-empty answer on the consumer gets (in-trait rows only), the gates,
// the file-local highlight variant and the telemetry callback.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"ike/internal/host"
)

// refsView returns a view over the reference fixture plus a file's lines.
func refsView(t *testing.T, rel string) (*HostView, *Index, string, string, []string) {
	t.Helper()
	x, dir := refsProject(t)
	path := filepath.Join(dir, filepath.FromSlash(rel))
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return NewHostView(x), x, dir, path, strings.Split(string(data), "\n")
}

func refKeys(rows []host.TraitReference) map[string]bool {
	out := map[string]bool{}
	for _, r := range rows {
		out[filepath.Base(r.Path)+":"+itoa(r.Line)+":"+itoa(r.Col)] = true
	}
	return out
}

// TestHostViewReferencesInsideTrait: with the server empty, the whole scope
// comes back — the call in A, the declaration in B, the call in C and the
// subclasses — and the telemetry callback sees (0, n).
func TestHostViewReferencesInsideTrait(t *testing.T) {
	v, _, dir, path, lines := refsView(t, "src/Traits/A.php")
	var gotServer, gotIndex int
	v.SetReferencesTelemetry(func(server, index int) { gotServer, gotIndex = server, index })
	line, col := at(t, lines, "$this->abc()", 8)
	rows := v.TraitReferencesAt(host.TraitReferences, path, lines, line, col, 0)
	keys := refKeys(rows)
	for _, s := range []site{
		{"src/Traits/A.php", "$this->abc()", 7, 0},
		{"src/Traits/C.php", "$this->abc()", 7, 0},
		{"src/Models/B.php", "public function abc", 16, 0},
	} {
		p, l, c := s.want(t, dir)
		if !keys[filepath.Base(p)+":"+itoa(l)+":"+itoa(c)] {
			t.Errorf("missing %s %q", s.rel, s.needle)
		}
	}
	for _, r := range rows {
		if filepath.Base(r.Path) == "Z.php" {
			t.Fatalf("unrelated class listed: %+v", r)
		}
		if r.Decl != strings.HasSuffix(r.Path, "B.php") {
			t.Fatalf("Decl flag wrong on %+v", r)
		}
	}
	if gotServer != 0 || gotIndex != len(rows) {
		t.Fatalf("telemetry = (%d, %d), want (0, %d)", gotServer, gotIndex, len(rows))
	}
}

// TestHostViewReferencesConsumerSide: with server hits, only the rows inside
// trait bodies are returned — the server reported the rest itself.
func TestHostViewReferencesConsumerSide(t *testing.T) {
	v, _, _, path, lines := refsView(t, "src/Models/B.php")
	line, col := at(t, lines, "public function abc", 17)
	rows := v.TraitReferencesAt(host.TraitReferences, path, lines, line, col, 2)
	if len(rows) != 2 {
		t.Fatalf("rows = %+v, want the two in-trait calls", rows)
	}
	for _, r := range rows {
		base := filepath.Base(r.Path)
		if (base != "A.php" && base != "C.php") || r.Decl {
			t.Fatalf("row = %+v, want an access inside A or C", r)
		}
	}
}

// TestHostViewReferencesGates pins where the view stays silent.
func TestHostViewReferencesGates(t *testing.T) {
	v, x, _, path, lines := refsView(t, "src/Models/B.php")
	line, col := at(t, lines, "public function abc", 17)
	t.Run("empty server answer outside a trait", func(t *testing.T) {
		if rows := v.TraitReferencesAt(host.TraitReferences, path, lines, line, col, 0); len(rows) != 0 {
			t.Fatalf("class body with an empty server answer must stay silent, got %+v", rows)
		}
	})
	t.Run("not a php buffer", func(t *testing.T) {
		if rows := v.TraitReferencesAt(host.TraitReferences, "notes.txt", lines, line, col, 1); len(rows) != 0 {
			t.Fatalf("non-PHP buffer answered %+v", rows)
		}
	})
	t.Run("nil index", func(t *testing.T) {
		if rows := NewHostView(nil).TraitReferencesAt(host.TraitReferences, path, lines, line, col, 1); len(rows) != 0 {
			t.Fatalf("nil index answered %+v", rows)
		}
	})
	t.Run("php.trait_index off", func(t *testing.T) {
		opts := defaultOpts()
		opts.Enabled = false
		x.Reconfigure(opts)
		defer x.Reconfigure(defaultOpts())
		if rows := v.TraitReferencesAt(host.TraitReferences, path, lines, line, col, 1); len(rows) != 0 {
			t.Fatalf("disabled index answered %+v", rows)
		}
	})
}

// TestHostViewHighlightIsFileLocalAndSilent: the highlight variant answers
// only the current file's rows and records no telemetry.
func TestHostViewHighlightIsFileLocalAndSilent(t *testing.T) {
	v, _, _, path, lines := refsView(t, "src/Traits/A.php")
	called := false
	v.SetReferencesTelemetry(func(server, index int) { called = true })
	line, col := at(t, lines, "$this->abc()", 8)
	rows := v.TraitReferencesAt(host.TraitHighlight, path, lines, line, col, 0)
	if len(rows) != 1 || rows[0].Path != path || rows[0].Line != line {
		t.Fatalf("rows = %+v, want the one call in A.php", rows)
	}
	if called {
		t.Fatal("a highlight lookup must not record references telemetry")
	}
}
