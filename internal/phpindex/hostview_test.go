package phpindex

// hostview_test.go covers the host-facing view the LSP bridge's navigation
// fallback consults (#2670): the gates (language, php.trait_index, trait
// body, member access) and the flat member shape the bridge renders from.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"ike/internal/host"

	_ "ike/plugins/languages/php"
)

// traitAView returns a view over the fixture project plus the lines of
// trait A, whose body calls `abc` (declared on the consumer B) and `fromC`
// (declared on the sibling trait C).
func traitAView(t *testing.T) (*HostView, string, []string) {
	t.Helper()
	x, dir := fixture(t, defaultOpts())
	path := filepath.Join(dir, "src", "Traits", "A.php")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return NewHostView(x), path, strings.Split(string(data), "\n")
}

// at resolves the position of needle+within in lines.
func at(t *testing.T, lines []string, needle string, within int) (int, int) {
	t.Helper()
	for i, line := range lines {
		if c := strings.Index(line, needle); c >= 0 {
			return i, len([]rune(line[:c])) + within
		}
	}
	t.Fatalf("%q not found", needle)
	return 0, 0
}

func TestHostViewResolvesConsumerMember(t *testing.T) {
	v, path, lines := traitAView(t)
	line, col := at(t, lines, "$this->abc()", 8)
	got := v.TraitMembersAt(host.TraitDefinition, path, lines, line, col)
	if len(got) != 1 {
		t.Fatalf("members = %+v, want exactly one", got)
	}
	m := got[0]
	if m.Name != "abc" || m.Declaring != `App\Models\B` || m.DeclName != "B" || m.DeclKind != "class" {
		t.Fatalf("member = %+v, want abc on class App\\Models\\B", m)
	}
	if m.Signature != "public function abc(int $times = 1): string" {
		t.Fatalf("signature = %q", m.Signature)
	}
	if m.Doc != "Does the abc thing." {
		t.Fatalf("doc = %q", m.Doc)
	}
	if !strings.HasSuffix(m.Path, filepath.Join("src", "Models", "B.php")) {
		t.Fatalf("path = %q, want B.php", m.Path)
	}
	// The target is the member's *name*, so the jump lands on the identifier.
	if got := strings.Index(readLine(t, m.Path, m.Line), "abc"); got != m.Col {
		t.Fatalf("target col = %d, want %d (the name)", m.Col, got)
	}
}

func TestHostViewResolvesSiblingTraitMember(t *testing.T) {
	v, path, lines := traitAView(t)
	line, col := at(t, lines, "$this->fromC()", 8)
	got := v.TraitMembersAt(host.TraitDefinition, path, lines, line, col)
	if len(got) != 1 || got[0].Declaring != `App\Traits\C` || got[0].DeclKind != "trait" {
		t.Fatalf("members = %+v, want fromC on trait App\\Traits\\C", got)
	}
}

// TestHostViewGates pins every position the view must stay silent at.
func TestHostViewGates(t *testing.T) {
	v, path, lines := traitAView(t)
	abcLine, abcCol := at(t, lines, "$this->abc()", 8)

	t.Run("unknown member", func(t *testing.T) {
		edited := append([]string(nil), lines...)
		edited[abcLine] = strings.Replace(edited[abcLine], "abc", "nope", 1)
		if got := v.TraitMembersAt(host.TraitDefinition, path, edited, abcLine, abcCol); len(got) != 0 {
			t.Fatalf("unknown member resolved to %+v", got)
		}
	})
	t.Run("not a member access", func(t *testing.T) {
		line, col := at(t, lines, "public function run", 4)
		if got := v.TraitMembersAt(host.TraitDefinition, path, lines, line, col); len(got) != 0 {
			t.Fatalf("plain position resolved to %+v", got)
		}
	})
	t.Run("not a php buffer", func(t *testing.T) {
		if got := v.TraitMembersAt(host.TraitDefinition, "notes.txt", lines, abcLine, abcCol); len(got) != 0 {
			t.Fatalf("non-PHP buffer resolved to %+v", got)
		}
	})
	t.Run("not a trait body", func(t *testing.T) {
		classPath := filepath.Join(filepath.Dir(filepath.Dir(path)), "Models", "B.php")
		data, err := os.ReadFile(classPath)
		if err != nil {
			t.Fatal(err)
		}
		bLines := strings.Split(string(data), "\n")
		line, col := at(t, bLines, "public function abc", 20)
		if got := v.TraitMembersAt(host.TraitDefinition, classPath, bLines, line, col); len(got) != 0 {
			t.Fatalf("class body resolved to %+v", got)
		}
	})
	t.Run("nil index", func(t *testing.T) {
		if got := NewHostView(nil).TraitMembersAt(host.TraitDefinition, path, lines, abcLine, abcCol); len(got) != 0 {
			t.Fatalf("nil index resolved to %+v", got)
		}
	})
}

// TestHostViewHonoursSetting guards php.trait_index = false: the index drops
// everything, so the fallback goes inert without the bridge knowing.
func TestHostViewHonoursSetting(t *testing.T) {
	x, dir := fixture(t, defaultOpts())
	path := filepath.Join(dir, "src", "Traits", "A.php")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(string(data), "\n")
	line, col := at(t, lines, "$this->abc()", 8)
	v := NewHostView(x)
	if got := v.TraitMembersAt(host.TraitDefinition, path, lines, line, col); len(got) != 1 {
		t.Fatalf("with the index on: %+v, want one member", got)
	}
	opts := defaultOpts()
	opts.Enabled = false
	x.Reconfigure(opts)
	if got := v.TraitMembersAt(host.TraitDefinition, path, lines, line, col); len(got) != 0 {
		t.Fatalf("with php.trait_index off: %+v, want nothing", got)
	}
}

// TestHostViewTelemetry guards the php.trait.definition / php.trait.hover
// ops: a non-empty answer reports its feature and count, an empty one
// reports nothing.
func TestHostViewTelemetry(t *testing.T) {
	v, path, lines := traitAView(t)
	type call struct {
		op host.TraitLookup
		n  int
	}
	var calls []call
	v.SetTelemetry(func(op host.TraitLookup, n int) { calls = append(calls, call{op, n}) })

	line, col := at(t, lines, "$this->abc()", 8)
	v.TraitMembersAt(host.TraitHover, path, lines, line, col)
	nopeLine, nopeCol := at(t, lines, "public function run", 4)
	v.TraitMembersAt(host.TraitDefinition, path, lines, nopeLine, nopeCol)

	if len(calls) != 1 || calls[0].op != host.TraitHover || calls[0].n != 1 {
		t.Fatalf("telemetry calls = %+v, want one hover call with count 1", calls)
	}
}

func readLine(t *testing.T, path string, line int) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(string(data), "\n")
	if line < 0 || line >= len(lines) {
		t.Fatalf("line %d out of range in %s", line, path)
	}
	return lines[line]
}
