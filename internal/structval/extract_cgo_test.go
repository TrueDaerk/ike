//go:build cgo

package structval_test

// extract_cgo_test.go drives the structural-value extraction (#2499) through
// the real Tree-sitter grammars: the language plugins are blank-imported, so
// the chain comes from highlight.SyntaxChainAt exactly as it does in the
// editor and the table below reads as "caret here, clipboard that". The test
// lives in the external test package because internal/highlight imports
// internal/structval — the dependency only runs the other way for tests.

import (
	"strings"
	"testing"

	"ike/internal/highlight"
	"ike/internal/structval"

	_ "ike/plugins/languages/go"
	_ "ike/plugins/languages/json"
	_ "ike/plugins/languages/toml"
	_ "ike/plugins/languages/web"
	_ "ike/plugins/languages/xml"
	_ "ike/plugins/languages/yaml"
)

// caret marks the cursor position in a fixture; splitCaret removes it and
// returns the rune coordinates it stood at.
const caret = '‸'

func splitCaret(t *testing.T, lines []string) ([]string, int, int) {
	t.Helper()
	out := make([]string, len(lines))
	copy(out, lines)
	for i, l := range out {
		if col := strings.IndexRune(l, caret); col >= 0 {
			out[i] = strings.Replace(l, string(caret), "", 1)
			return out, i, len([]rune(l[:col]))
		}
	}
	t.Fatalf("fixture has no %q caret marker", string(caret))
	return nil, 0, 0
}

type extractCase struct {
	name   string
	path   string
	langID string
	lines  []string
	// inner and outer are what gy and gY copy. An empty inner means gy has
	// nothing to take; want=false means neither command finds anything.
	inner, outer string
	want         bool
}

func TestExtractJSON(t *testing.T) {
	doc := []string{
		`{`,
		`  "body": "{\"a\":1}\n",`,
		`  "list": [1, {"k": 2}],`,
		`  "nested": {`,
		`    "deep": true`,
		`  }`,
		`}`,
	}
	runExtract(t, []extractCase{
		{
			name: "string value decoded", path: "t.json", langID: "json",
			lines: withCaret(doc, 1, 15),
			inner: "{\"a\":1}\n", outer: `"body": "{\"a\":1}\n"`, want: true,
		},
		{
			name: "caret on the key copies its value", path: "t.json", langID: "json",
			lines: withCaret(doc, 1, 4),
			inner: "{\"a\":1}\n", outer: `"body": "{\"a\":1}\n"`, want: true,
		},
		{
			name: "array element", path: "t.json", langID: "json",
			lines: withCaret(doc, 2, 11),
			inner: "1", outer: "1", want: true,
		},
		{
			name: "object element keeps its buffer text", path: "t.json", langID: "json",
			lines: withCaret(doc, 2, 14),
			inner: `{"k": 2}`, outer: `{"k": 2}`, want: true,
		},
		{
			name: "multi-line object value", path: "t.json", langID: "json",
			lines: withCaret(doc, 3, 4),
			inner: "{\n    \"deep\": true\n  }",
			outer: "\"nested\": {\n    \"deep\": true\n  }", want: true,
		},
		{
			name: "non-string value copied as written", path: "t.json", langID: "json",
			lines: withCaret(doc, 4, 13),
			inner: "true", outer: `"deep": true`, want: true,
		},
		{
			name: "outside any value", path: "t.json", langID: "json",
			lines: withCaret(doc, 6, 0),
			want:  false,
		},
	})
}

func TestExtractYAML(t *testing.T) {
	doc := []string{
		"db:",
		"  host: x",
		"  port: 5",
		"msg: |",
		"  line1",
		"  line2",
		"folded: >-",
		"  one",
		"  two",
		`q: "a\nb"`,
		"seq:",
		"  - one",
		"  - two",
		"",
	}
	runExtract(t, []extractCase{
		{
			name: "nested mapping is dedented back into a document", path: "t.yaml", langID: "yaml",
			lines: withCaret(doc, 0, 0),
			inner: "host: x\nport: 5", outer: "db:\n  host: x\n  port: 5", want: true,
		},
		{
			name: "literal block scalar keeps its newlines", path: "t.yaml", langID: "yaml",
			lines: withCaret(doc, 4, 3),
			inner: "line1\nline2\n", outer: "msg: |\n  line1\n  line2", want: true,
		},
		{
			name: "folded block scalar folds", path: "t.yaml", langID: "yaml",
			lines: withCaret(doc, 7, 3),
			inner: "one two", outer: "folded: >-\n  one\n  two", want: true,
		},
		{
			name: "quoted scalar loses its quotes", path: "t.yaml", langID: "yaml",
			lines: withCaret(doc, 9, 5),
			inner: "a\nb", outer: `q: "a\nb"`, want: true,
		},
		{
			name: "plain scalar", path: "t.yaml", langID: "yaml",
			lines: withCaret(doc, 1, 8),
			inner: "x", outer: "host: x", want: true,
		},
		{
			name: "sequence item", path: "t.yaml", langID: "yaml",
			lines: withCaret(doc, 11, 5),
			inner: "one", outer: "- one", want: true,
		},
		{
			name: "sequence under a key", path: "t.yaml", langID: "yaml",
			lines: withCaret(doc, 10, 1),
			inner: "- one\n- two\n", outer: "seq:\n  - one\n  - two\n", want: true,
		},
	})
}

func TestExtractMarkup(t *testing.T) {
	runExtract(t, []extractCase{
		{
			name: "html element inner markup", path: "t.html", langID: "html",
			lines: []string{`<div class="c"><p>Hel‸lo <b>x</b></p><br/></div>`},
			inner: "Hello <b>x</b>", outer: "<p>Hello <b>x</b></p>", want: true,
		},
		{
			name: "html innermost element wins", path: "t.html", langID: "html",
			lines: []string{`<div class="c"><p>Hello <b>‸x</b></p><br/></div>`},
			inner: "x", outer: "<b>x</b>", want: true,
		},
		{
			name: "html attribute value", path: "t.html", langID: "html",
			lines: []string{`<div cla‸ss="c"><p>Hello</p></div>`},
			inner: "c", outer: `class="c"`, want: true,
		},
		{
			name: "html void element has no inner content", path: "t.html", langID: "html",
			lines: []string{`<div><b‸r/></div>`},
			inner: "", outer: "<br/>", want: true,
		},
		{
			name: "xml element inner markup", path: "t.xml", langID: "xml",
			lines: []string{`<root a="x&amp;y"><p>Hel‸lo <b>x</b></p></root>`},
			inner: "Hello <b>x</b>", outer: "<p>Hello <b>x</b></p>", want: true,
		},
		{
			name: "xml attribute resolves entities", path: "t.xml", langID: "xml",
			lines: []string{`<root a="x&am‸p;y"><p>Hello</p></root>`},
			inner: "x&y", outer: `a="x&amp;y"`, want: true,
		},
	})
}

func TestExtractTOML(t *testing.T) {
	doc := []string{
		`[t]`,
		`key = "a\nb"`,
		`lit = 'raw\n'`,
		"ml = \"\"\"",
		"x",
		"y\"\"\"",
		`arr = ["one", 2]`,
		`n = 42`,
	}
	runExtract(t, []extractCase{
		{
			name: "basic string decoded", path: "t.toml", langID: "toml",
			lines: withCaret(doc, 1, 8),
			inner: "a\nb", outer: `key = "a\nb"`, want: true,
		},
		{
			name: "caret on the key copies its value", path: "t.toml", langID: "toml",
			lines: withCaret(doc, 1, 1),
			inner: "a\nb", outer: `key = "a\nb"`, want: true,
		},
		{
			name: "literal string keeps its backslashes", path: "t.toml", langID: "toml",
			lines: withCaret(doc, 2, 8),
			inner: `raw\n`, outer: `lit = 'raw\n'`, want: true,
		},
		{
			name: "multi-line string", path: "t.toml", langID: "toml",
			lines: withCaret(doc, 4, 0),
			inner: "x\ny", outer: "ml = \"\"\"\nx\ny\"\"\"", want: true,
		},
		{
			name: "array element", path: "t.toml", langID: "toml",
			lines: withCaret(doc, 6, 8),
			inner: "one", outer: `"one"`, want: true,
		},
		{
			name: "number copied as written", path: "t.toml", langID: "toml",
			lines: withCaret(doc, 7, 5),
			inner: "42", outer: "n = 42", want: true,
		},
	})
}

func TestExtractStringLiteralFallback(t *testing.T) {
	runExtract(t, []extractCase{
		{
			name: "go interpreted literal decoded", path: "t.go", langID: "go",
			lines: []string{"package main", "", `var s = "a\nb‸"`},
			inner: "a\nb", outer: `"a\nb"`, want: true,
		},
		{
			name: "go raw literal keeps its backslashes", path: "t.go", langID: "go",
			lines: []string{"package main", "", "var r = `ra‸w\\n`"},
			inner: `raw\n`, outer: "`raw\\n`", want: true,
		},
		{
			name: "go outside any literal", path: "t.go", langID: "go",
			lines: []string{"package m‸ain", "", `var s = "a"`},
			want:  false,
		},
	})
}

func TestExtractNoGrammar(t *testing.T) {
	lines := []string{"just some text"}
	chain := highlight.SyntaxChainAt("notes.unknownext", lines, 0, 3)
	if chain != nil {
		t.Fatalf("chain for a grammar-less path = %+v, want nil", chain)
	}
	if _, ok := structval.Extract("", strings.Join(lines, "\n"), chain); ok {
		t.Error("Extract on an empty chain should decline")
	}
}

// withCaret returns doc with the caret marker inserted at (line, col).
func withCaret(doc []string, line, col int) []string {
	out := make([]string, len(doc))
	copy(out, doc)
	r := []rune(out[line])
	out[line] = string(r[:col]) + string(caret) + string(r[col:])
	return out
}

func runExtract(t *testing.T, cases []extractCase) {
	t.Helper()
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			lines, line, col := splitCaret(t, tc.lines)
			chain := highlight.SyntaxChainAt(tc.path, lines, line, col)
			got, ok := structval.Extract(tc.langID, strings.Join(lines, "\n"), chain)
			if ok != tc.want {
				t.Fatalf("Extract ok = %v, want %v (value %+v)", ok, tc.want, got)
			}
			if !tc.want {
				return
			}
			if got.Inner != tc.inner {
				t.Errorf("inner (gy) = %q, want %q", got.Inner, tc.inner)
			}
			if got.Outer != tc.outer {
				t.Errorf("outer (gY) = %q, want %q", got.Outer, tc.outer)
			}
		})
	}
}
