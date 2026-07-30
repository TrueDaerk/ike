package format

import (
	"context"
	"reflect"
	"strings"
	"testing"
)

func noopFormat(ctx context.Context, req Request) (Result, error) { return Result{}, nil }

func provider(name string, tier Tier, langs ...string) Provider {
	return Provider{Name: name, Tier: tier, Languages: langs, Format: noopFormat}
}

// TestResolveTierOrder: the chain resolves override → external → LSP →
// built-in regardless of registration order (#1400).
func TestResolveTierOrder(t *testing.T) {
	ResetForTest()
	t.Cleanup(ResetForTest)
	Register(provider("builtin", TierBuiltin, "sql"))
	Register(provider("lsp", TierLSP))
	Register(provider("external", TierExternal, "sql"))
	Register(provider("override", TierOverride, "sql"))

	p, ok := Resolve("sql", "x.sql")
	if !ok || p.Name != "override" {
		t.Fatalf("want override, got %q ok=%v", p.Name, ok)
	}
}

// TestResolveSkipsUnavailable: an unavailable provider (missing binary, no
// capable server) falls through to the next tier.
func TestResolveSkipsUnavailable(t *testing.T) {
	ResetForTest()
	t.Cleanup(ResetForTest)
	ext := provider("external", TierExternal, "python")
	ext.Available = func(string) bool { return false }
	Register(ext)
	Register(provider("builtin", TierBuiltin, "python"))

	p, ok := Resolve("python", "x.py")
	if !ok || p.Name != "builtin" {
		t.Fatalf("want builtin, got %q ok=%v", p.Name, ok)
	}
}

// TestResolveLanguageFilter: a provider only serves its declared languages;
// an empty Languages list serves all.
func TestResolveLanguageFilter(t *testing.T) {
	ResetForTest()
	t.Cleanup(ResetForTest)
	Register(provider("sql-only", TierBuiltin, "sql"))

	if _, ok := Resolve("xml", "x.xml"); ok {
		t.Fatal("sql-only provider must not serve xml")
	}
	if !Has("sql", "x.sql") {
		t.Fatal("sql provider missing")
	}
}

// TestResolveRangeFallback: when the whole-file winner has no range support,
// reformat-selection falls back to the next range-capable provider (#1401).
func TestResolveRangeFallback(t *testing.T) {
	ResetForTest()
	t.Cleanup(ResetForTest)
	Register(provider("external", TierExternal, "go")) // no FormatRange
	lsp := provider("lsp", TierLSP, "go")
	lsp.FormatRange = func(ctx context.Context, req Request, start, end Pos) (Result, error) {
		return Result{}, nil
	}
	Register(lsp)

	p, ok, wholeOnly := ResolveRange("go", "x.go")
	if !ok || wholeOnly || p.Name != "lsp" {
		t.Fatalf("want lsp fallback, got %q ok=%v wholeOnly=%v", p.Name, ok, wholeOnly)
	}
}

// TestResolveRangeWholeFileOnly: providers exist but none does ranges — the
// caller reports that only whole-file reformat is available.
func TestResolveRangeWholeFileOnly(t *testing.T) {
	ResetForTest()
	t.Cleanup(ResetForTest)
	Register(provider("external", TierExternal, "go"))

	_, ok, wholeOnly := ResolveRange("go", "x.go")
	if ok || !wholeOnly {
		t.Fatalf("want wholeFileOnly, got ok=%v wholeOnly=%v", ok, wholeOnly)
	}
	// No provider at all: both false.
	_, ok, wholeOnly = ResolveRange("xml", "x.xml")
	if ok || wholeOnly {
		t.Fatalf("want neither, got ok=%v wholeOnly=%v", ok, wholeOnly)
	}
}

// TestRangeAvailableGate: RangeAvailable=false disables a set FormatRange
// (the LSP provider's per-server capability gate).
func TestRangeAvailableGate(t *testing.T) {
	ResetForTest()
	t.Cleanup(ResetForTest)
	p := provider("lsp", TierLSP, "go")
	p.FormatRange = func(ctx context.Context, req Request, start, end Pos) (Result, error) {
		return Result{}, nil
	}
	p.RangeAvailable = func(string) bool { return false }
	Register(p)

	_, ok, wholeOnly := ResolveRange("go", "x.go")
	if ok || !wholeOnly {
		t.Fatalf("want wholeFileOnly, got ok=%v wholeOnly=%v", ok, wholeOnly)
	}
}

// TestDisplayName: NameFor overrides Name per path when it answers.
func TestDisplayName(t *testing.T) {
	p := Provider{Name: "lsp", NameFor: func(path string) string {
		if strings.HasSuffix(path, ".go") {
			return "gopls"
		}
		return ""
	}}
	if got := p.DisplayName("a.go"); got != "gopls" {
		t.Fatalf("got %q", got)
	}
	if got := p.DisplayName("a.py"); got != "lsp" {
		t.Fatalf("got %q", got)
	}
}

// applyEdits replays edits over lines the way the editor does (single edit
// from diffLines), for round-trip checks.
func applyEdits(lines []string, edits []Edit) []string {
	text := strings.Join(lines, "\n")
	for i := len(edits) - 1; i >= 0; i-- {
		e := edits[i]
		start := posOffset(lines, e.StartLine, e.StartCol)
		end := posOffset(lines, e.EndLine, e.EndCol)
		text = text[:start] + e.Text + text[end:]
	}
	return strings.Split(text, "\n")
}

func posOffset(lines []string, line, col int) int {
	off := 0
	for i := 0; i < line; i++ {
		off += len(lines[i]) + 1
	}
	return off + len(string([]rune(lines[line])[:col]))
}

// TestEditsForResultDiff: text results become a minimal middle-replace edit
// that reproduces the target text.
func TestEditsForResultDiff(t *testing.T) {
	cases := []struct {
		name     string
		old, new []string
	}{
		{"middle change", []string{"a", "b", "c", "d"}, []string{"a", "B", "c", "d"}},
		{"identical", []string{"a", "b"}, []string{"a", "b"}},
		{"delete line", []string{"a", "b", "c"}, []string{"a", "c"}},
		{"insert line", []string{"a", "c"}, []string{"a", "b", "c"}},
		{"append at end", []string{"a", "b"}, []string{"a", "b", "c"}},
		{"trailing newline added", []string{"a", "b"}, []string{"a", "b", ""}},
		{"trailing newline removed", []string{"a", "b", ""}, []string{"a", "b"}},
		{"full rewrite", []string{"x", "y"}, []string{"p", "q", "r"}},
		{"empty to content", []string{""}, []string{"a", "b"}},
		{"content to empty", []string{"a", "b"}, []string{""}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			text := strings.Join(c.new, "\n")
			edits := EditsForResult(c.old, TextResult(text))
			if reflect.DeepEqual(c.old, c.new) {
				if edits != nil {
					t.Fatalf("identical text must yield nil edits, got %+v", edits)
				}
				return
			}
			if got := applyEdits(c.old, edits); !reflect.DeepEqual(got, c.new) {
				t.Fatalf("edits %+v: got %q want %q", edits, got, c.new)
			}
		})
	}
}

// TestEditsForResultPassthrough: providers returning edits are untouched.
func TestEditsForResultPassthrough(t *testing.T) {
	in := []Edit{{StartLine: 1, EndLine: 1, Text: "x"}}
	if got := EditsForResult([]string{"a"}, Result{Edits: in}); !reflect.DeepEqual(got, in) {
		t.Fatalf("got %+v", got)
	}
}
