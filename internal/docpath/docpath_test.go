package docpath

import (
	"strings"
	"testing"
)

// at scans lines and renders the caret's path in the dotted form. The caret is
// marked in the fixture with a "|" that this helper strips, so a case reads as
// the document it is about.
func at(t *testing.T, langID string, lines ...string) string {
	t.Helper()
	line, col := -1, 0
	src := make(Lines, len(lines))
	for i, l := range lines {
		if j := strings.Index(l, "|"); j >= 0 && line < 0 {
			line, col = i, len([]rune(l[:j]))
			l = strings.Replace(l, "|", "", 1)
		}
		src[i] = l
	}
	if line < 0 {
		t.Fatalf("fixture has no | caret marker")
	}
	return Dotted(At(langID, src, line, col))
}

func eq(t *testing.T, got, want string) {
	t.Helper()
	if got != want {
		t.Fatalf("path = %q, want %q", got, want)
	}
}

// TestJSONNesting (#1660): objects nest by key, arrays by index, and the two
// interleave — the motivating `spec.containers[2].env[0].name` shape.
func TestJSONNesting(t *testing.T) {
	doc := []string{
		`{`,
		`  "spec": {`,
		`    "containers": [`,
		`      {"name": "a"},`,
		`      {"name": "b"},`,
		`      {"env": [{"name": "PO|RT"}]}`,
		`    ]`,
		`  }`,
		`}`,
	}
	eq(t, at(t, "json", doc...), "spec.containers[2].env[0].name")
}

// TestJSONCaretPositions (#1660): the path follows the caret within one line —
// on a key, on its value, and on the closing brace of the object it names.
func TestJSONCaretPositions(t *testing.T) {
	eq(t, at(t, "json", `{"a": {"b|b": 1}}`), "a.bb")
	eq(t, at(t, "json", `{"a": {"bb": |1}}`), "a.bb")
	eq(t, at(t, "json", `{"a": {"bb": 1|}}`), "a.bb")
	// Inside an object that has no key yet the enclosing path is the honest
	// answer; a caret on the key's first cell is already on the key.
	eq(t, at(t, "json", `{"a": {`, `  |`, `}}`), "a")
	eq(t, at(t, "json", `{"a": {|"bb": 1}}`), "a.bb")
	// A caret before any container is at the document root.
	eq(t, at(t, "json", `|{"a": 1}`), "")
}

// TestJSONArrayIndices (#1660): commas advance the index of their own level
// only, so a comma inside a nested object never moves the array.
func TestJSONArrayIndices(t *testing.T) {
	eq(t, at(t, "json", `{"xs": [1, 2, |3]}`), "xs[2]")
	eq(t, at(t, "json", `{"xs": [{"a": 1, "b": |2}, 9]}`), "xs[0].b")
	eq(t, at(t, "json", `[[0, 0], [0, |0]]`), "[1][1]")
}

// TestJSONStringsAndComments (#1660): braces, brackets and commas inside
// strings are data, and JSONC comments are skipped — a `{` in either must not
// open a container.
func TestJSONStringsAndComments(t *testing.T) {
	eq(t, at(t, "json", `{"a": "} , [", "b|": 1}`), "b")
	eq(t, at(t, "json", `{"a": 1, // "x": {`, `  "b|": 2}`), "b")
	eq(t, at(t, "json", `{"a": 1, /* {[ */ "b|": 2}`), "b")
	eq(t, at(t, "json", `{"a": 1, /* {[`, `  still comment */ "b|": 2}`), "b")
	// An escaped quote does not end the key.
	eq(t, at(t, "json", `{"a\"b": {"c|": 1}}`), `a"b.c`)
}

// TestJSONBroken (#1660): a buffer that is unfinished or unbalanced still
// yields the nearest enclosing path, because the scan only ever reads up to
// the caret.
func TestJSONBroken(t *testing.T) {
	// Still being typed: nothing below the caret is closed.
	eq(t, at(t, "json", `{`, `  "a": {`, `    "b": |`), "a.b")
	// A stray closer with no opener pops nothing.
	eq(t, at(t, "json", `}`, `{"a": |1}`), "a")
	// An unterminated string ends at the line, so the lines below keep working.
	eq(t, at(t, "json", `{"a": "oops`, `  "b": |2}`), "a")
}

// TestYAMLBlockNesting (#1660): indentation nests mappings, `- ` items index
// their sequence, and both compose into the k8s manifest path.
func TestYAMLBlockNesting(t *testing.T) {
	doc := []string{
		`spec:`,
		`  template:`,
		`    containers:`,
		`      - name: a`,
		`      - name: b`,
		`        env:`,
		`          - name: PO|RT`,
		`            value: "80"`,
	}
	eq(t, at(t, "yaml", doc...), "spec.template.containers[1].env[0].name")
	doc[6], doc[7] = `          - name: PORT`, `            value: "8|0"`
	eq(t, at(t, "yaml", doc...), "spec.template.containers[1].env[0].value")
}

// TestYAMLSequenceAtParentIndent (#1660): a sequence may share its key's
// column, the other legal spelling — the key must not be popped by its own
// items.
func TestYAMLSequenceAtParentIndent(t *testing.T) {
	doc := []string{
		`on:`,
		`- push`,
		`- pull_re|quest`,
		`jobs:`,
	}
	eq(t, at(t, "yaml", doc...), "on[1]")
	eq(t, at(t, "yaml", `on:`, `- push`, `jobs:`, `  build|:`), "jobs.build")
}

// TestYAMLDedent (#1660): a shallower key closes everything at or below its
// column, including the sequences opened inside it.
func TestYAMLDedent(t *testing.T) {
	doc := []string{
		`a:`,
		`  b:`,
		`    - x`,
		`  c: |1`,
		`d: 2`,
	}
	eq(t, at(t, "yaml", doc...), "a.c")
	doc[3], doc[4] = `  c: 1`, `d: |2`
	eq(t, at(t, "yaml", doc...), "d")
}

// TestYAMLCaretColumn (#1660): within a line the path grows left to right —
// on the dash it is the sequence element, on the key the key.
func TestYAMLCaretColumn(t *testing.T) {
	eq(t, at(t, "yaml", `xs:`, `  -| name: a`), "xs[0]")
	eq(t, at(t, "yaml", `xs:`, `  - na|me: a`), "xs[0].name")
	eq(t, at(t, "yaml", `xs:`, `|  - name: a`), "xs")
	// Compact nested sequences open one level per dash.
	eq(t, at(t, "yaml", `m:`, `  - - a|: 1`), "m[0][0].a")
}

// TestYAMLFlowStyle (#1660): a flow collection is the JSON grammar, including
// unquoted keys and a flow that spans lines.
func TestYAMLFlowStyle(t *testing.T) {
	eq(t, at(t, "yaml", `a: {b: 1, c: |2}`), "a.c")
	eq(t, at(t, "yaml", `a: [1, |2]`), "a[1]")
	eq(t, at(t, "yaml", `a: {b: [{c: |1}]}`), "a.b[0].c")
	eq(t, at(t, "yaml", `- {name: x, port: |80}`), "[0].port")
	eq(t, at(t, "yaml", `a: {b: 1,`, `   c: |2}`), "a.c")
	// Left of the flow the caret is still on the key line itself.
	eq(t, at(t, "yaml", `ab|c: {b: 1}`), "abc")
}

// TestYAMLBlockScalarAndComments (#1660): a block scalar's body is text, and
// `#` comments never contribute structure — a `key:` inside either must not
// deepen the path.
func TestYAMLBlockScalarAndComments(t *testing.T) {
	doc := []string{
		`run: |`,
		`  echo hi`,
		`  not_a_k|ey: 1`,
		`next: 2`,
	}
	eq(t, at(t, "yaml", doc...), "run")
	eq(t, at(t, "yaml", `a:`, `  # b: 1`, `  c: |2`), "a.c")
	eq(t, at(t, "yaml", `a: 1 # b: 2`, `c|: 3`), "c")
	// A `#` glued to a value is data, not a comment.
	eq(t, at(t, "yaml", `a: pass#word`, `c|: 3`), "c")
}

// TestYAMLDocuments (#1660): a path never crosses a `---` boundary.
func TestYAMLDocuments(t *testing.T) {
	doc := []string{
		`kind: Service`,
		`spec:`,
		`  ports:`,
		`    - port: 80`,
		`---`,
		`kind: Deploy|ment`,
	}
	eq(t, at(t, "yaml", doc...), "kind")
}

// TestYAMLAnchors (#1660): anchors and aliases are reported as written — the
// path names a location in *this* document, so a merge key is the `<<` key and
// an alias is never followed (#1629 owns resolution).
func TestYAMLAnchors(t *testing.T) {
	doc := []string{
		`base: &base`,
		`  cpu: 1`,
		`svc:`,
		`  <<: *b|ase`,
		`  mem: 2`,
	}
	eq(t, at(t, "yaml", doc...), "svc.<<")
	// An anchor on a mapping line decorates the node; the key is the key.
	eq(t, at(t, "yaml", `xs:`, `  - &a na|me: x`), "xs[0].name")
	eq(t, at(t, "yaml", `xs:`, `  - &a`, `    na|me: x`), "xs[0].name")
}

// TestYAMLQuotedAndTrickyKeys (#1660): a quoted key keeps its spaces, and a
// colon inside a value does not end the key.
func TestYAMLQuotedAndTrickyKeys(t *testing.T) {
	eq(t, at(t, "yaml", `"my key":`, `  a: |1`), "my key.a")
	eq(t, at(t, "yaml", `url: http://x|/y`), "url")
	eq(t, at(t, "yaml", `time: 12:3|0`), "time")
}

// TestYAMLBroken (#1660): an incomplete buffer degrades to the nearest
// enclosing node instead of losing the path.
func TestYAMLBroken(t *testing.T) {
	// A key still being typed.
	eq(t, at(t, "yaml", `a:`, `  b:`, `    c|`), "a.b")
	// A blank line has no columns of its own, so it keeps the enclosing node.
	eq(t, at(t, "yaml", `a:`, `  b: 1`, `|`), "a.b")
	// A flow left open stays open (there is no way to know it was meant to
	// close), so the path keeps the last complete key of that object rather
	// than reading the broken continuation as a new one.
	eq(t, at(t, "yaml", `a: {b: 1`, `c|: 2`), "a.b")
}

// TestUnknownLanguage (#1660): a language without a scanner has no path, which
// is what hides the status segment.
func TestUnknownLanguage(t *testing.T) {
	if got := At("go", Lines{"package main"}, 0, 3); got != nil {
		t.Fatalf("At(go) = %v, want nil", got)
	}
	if IsLang("go") || !IsLang("json") || !IsLang("yaml") || !IsLang("ansible") {
		t.Fatal("IsLang disagrees with the scanner table")
	}
	if IsYAML("json") || !IsYAML("ansible") {
		t.Fatal("IsYAML disagrees with the scanner table")
	}
}

// TestRendering (#1660): the three copy forms of one path, including the
// quoting each tool needs for a key that is not a bare identifier.
func TestRendering(t *testing.T) {
	steps := []Step{{Key: "spec"}, {Seq: true, Index: 2}, {Key: "my-key"}}
	if got, want := Dotted(steps), "spec[2].my-key"; got != want {
		t.Errorf("Dotted = %q, want %q", got, want)
	}
	if got, want := JQ(steps), `.spec[2]["my-key"]`; got != want {
		t.Errorf("JQ = %q, want %q", got, want)
	}
	if got, want := YQ(steps), `.spec[2]."my-key"`; got != want {
		t.Errorf("YQ = %q, want %q", got, want)
	}
	// A path starting with a sequence index needs the leading identity dot.
	root := []Step{{Seq: true, Index: 0}, {Key: "name"}}
	if got, want := Dotted(root), "[0].name"; got != want {
		t.Errorf("Dotted = %q, want %q", got, want)
	}
	if got, want := JQ(root), ".[0].name"; got != want {
		t.Errorf("JQ = %q, want %q", got, want)
	}
	if got, want := YQ(root), ".[0].name"; got != want {
		t.Errorf("YQ = %q, want %q", got, want)
	}
	// The empty path is the whole document in both tools.
	if got := JQ(nil); got != "." {
		t.Errorf("JQ(nil) = %q, want %q", got, ".")
	}
	if got := YQ(nil); got != "." {
		t.Errorf("YQ(nil) = %q, want %q", got, ".")
	}
	if got := Dotted(nil); got != "" {
		t.Errorf("Dotted(nil) = %q, want %q", got, "")
	}
}

// TestCaretPastEnd (#1660): a caret line beyond the buffer clamps instead of
// panicking — the editor clamps too, but the package is used directly.
func TestCaretPastEnd(t *testing.T) {
	src := Lines{`{"a": {`}
	if got := Dotted(At("json", src, 40, 0)); got != "a" {
		t.Fatalf("path = %q, want %q", got, "a")
	}
	if got := Dotted(At("yaml", Lines{"a:"}, 40, 0)); got != "a" {
		t.Fatalf("path = %q, want %q", got, "a")
	}
}
