package langxml

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"ike/internal/format"
)

func xopts() xmlOptions { return xmlOptions{Indent: "    ", TabWidth: 4, MaxWidth: 80} }

// TestXMLGolden pins the layout with golden files (nested docs, long
// attribute lists, CDATA, comments, xml:space, SVG and plist samples) and
// asserts idempotency on every one (#1404).
func TestXMLGolden(t *testing.T) {
	inputs, err := filepath.Glob(filepath.Join("testdata", "*.xml"))
	if err != nil || len(inputs) == 0 {
		t.Fatalf("no golden inputs: %v", err)
	}
	for _, in := range inputs {
		name := strings.TrimSuffix(filepath.Base(in), ".xml")
		t.Run(name, func(t *testing.T) {
			src, err := os.ReadFile(in)
			if err != nil {
				t.Fatal(err)
			}
			want, err := os.ReadFile(filepath.Join("testdata", name+".golden"))
			if err != nil {
				t.Fatal(err)
			}
			got, err := formatXML(string(src), xopts())
			if err != nil {
				t.Fatalf("format: %v", err)
			}
			if got != string(want) {
				t.Fatalf("golden mismatch\n--- got ---\n%s--- want ---\n%s", got, want)
			}
			again, err := formatXML(got, xopts())
			if err != nil || again != got {
				t.Fatalf("not idempotent (err=%v)\n--- second ---\n%s", err, again)
			}
		})
	}
}

// TestXMLMalformedUntouched: structural breakage refuses to format.
func TestXMLMalformedUntouched(t *testing.T) {
	for _, src := range []string{
		"<a><b></a>",
		"<a>",
		"<a attr='x></a>",
		"<a><!-- open</a>",
		"text only, no root <",
	} {
		if _, err := formatXML(src, xopts()); err == nil {
			t.Fatalf("malformed %q must refuse", src)
		}
	}
}

// TestXMLSelfClosingKept: <x/> never becomes <x></x> and vice versa.
func TestXMLSelfClosingKept(t *testing.T) {
	got, err := formatXML("<r><a/><b></b></r>", xopts())
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got, "<a/>") || !strings.Contains(got, "<b></b>") {
		t.Fatalf("tag forms must survive:\n%s", got)
	}
}

// TestXMLAttrWrapWidth: attributes stay on one line under the max width and
// wrap aligned under the first beyond it.
func TestXMLAttrWrapWidth(t *testing.T) {
	src := `<e one="1" two="2" three="3"/>`
	wide, err := formatXML(src, xmlOptions{Indent: "  ", TabWidth: 2, MaxWidth: 100})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Count(strings.TrimSpace(wide), "\n") != 0 {
		t.Fatalf("must stay on one line under the width:\n%s", wide)
	}
	narrow, err := formatXML(src, xmlOptions{Indent: "  ", TabWidth: 2, MaxWidth: 20})
	if err != nil {
		t.Fatal(err)
	}
	want := "<e one=\"1\"\n   two=\"2\"\n   three=\"3\"/>\n"
	if narrow != want {
		t.Fatalf("got %q want %q", narrow, want)
	}
	// zero width: never wrap
	off, _ := formatXML(src, xmlOptions{Indent: "  ", TabWidth: 2, MaxWidth: 0})
	if strings.Count(strings.TrimSpace(off), "\n") != 0 {
		t.Fatalf("MaxWidth 0 must never wrap:\n%s", off)
	}
}

// TestXMLIndentStyle: tabs vs spaces follow the options.
func TestXMLIndentStyle(t *testing.T) {
	src := "<r><a>1</a></r>"
	tabs, _ := formatXML(src, xmlOptions{Indent: "\t", TabWidth: 4})
	if !strings.Contains(tabs, "\n\t<a>1</a>\n") {
		t.Fatalf("tab indent expected: %q", tabs)
	}
}

// TestXMLFormatRange: only the subtrees overlapping the selection reformat.
func TestXMLFormatRange(t *testing.T) {
	src := "<root>\n  <a><b>1</b></a>\n  <c    x=\"1\">2</c>\n  <d>3</d>\n</root>"
	got, err := formatRangeXML(src, 2, 2, xopts())
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(got, "\n")
	if lines[1] != "  <a><b>1</b></a>" || lines[3] != "  <d>3</d>" {
		t.Fatalf("unselected siblings must stay untouched:\n%s", got)
	}
	if lines[2] != `    <c x="1">2</c>` {
		t.Fatalf("selected subtree must format:\n%s", got)
	}
}

// TestXMLRangeIdempotent: range-formatting the formatted span again is a
// no-op.
func TestXMLRangeIdempotent(t *testing.T) {
	src := "<root>\n  <a><b>1</b></a>\n  <c    x=\"1\">2</c>\n</root>"
	once, err := formatRangeXML(src, 2, 2, xopts())
	if err != nil {
		t.Fatal(err)
	}
	twice, err := formatRangeXML(once, 2, 2, xopts())
	if err != nil || twice != once {
		t.Fatalf("not idempotent (err=%v)\n--- once ---\n%s\n--- twice ---\n%s", err, once, twice)
	}
}

// TestXMLProviderRegistered: the registry resolves the built-in for xml and
// [format.xml] builtin=false disables it.
func TestXMLProviderRegistered(t *testing.T) {
	p, ok := format.Resolve("xml", "x.svg")
	if !ok || p.Name != "built-in" {
		t.Fatalf("built-in xml provider expected, got %q ok=%v", p.Name, ok)
	}
	res, err := p.Format(context.Background(), format.Request{
		Path: "x.xml", Language: "xml",
		Lines:   []string{"<r><a>1</a></r>"},
		Options: format.Options{TabWidth: 2, UseSpaces: true},
	})
	if err != nil || res.Text == nil || !strings.Contains(*res.Text, "\n  <a>1</a>\n") {
		t.Fatalf("err=%v text=%v", err, res.Text)
	}
	format.SetBuiltinEnabled(func(langID string) bool { return langID != "xml" })
	t.Cleanup(func() { format.SetBuiltinEnabled(nil) })
	if _, ok := format.Resolve("xml", "x.xml"); ok {
		t.Fatal("builtin=false must disable the xml formatter")
	}
}
