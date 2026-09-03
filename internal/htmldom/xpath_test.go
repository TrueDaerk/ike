package htmldom

import (
	"strings"
	"testing"
)

// TestXPathNamesTheNode: the HTML tree's absolute path, positionally
// qualified exactly where an element shares its tag with a sibling.
func TestXPathNamesTheNode(t *testing.T) {
	src := "<html><body><div><a href=\"x\">one</a></div><div><a href=\"y\">two</a></div></body></html>"
	d := Parse(src)
	n := d.NodeAt(strings.Index(src, "two"))
	if got := d.XPath(n); got != "/html/body/div[2]/a" {
		t.Fatalf("XPath = %q, want /html/body/div[2]/a", got)
	}
	head := d.NodeAt(strings.Index(src, "one"))
	if got := d.XPath(head); got != "/html/body/div[1]/a" {
		t.Fatalf("XPath = %q, want /html/body/div[1]/a", got)
	}
}

// TestXPathNonElement: text offsets resolve to their enclosing element via
// NodeAt; a nil node answers "".
func TestXPathNil(t *testing.T) {
	d := Parse("<html></html>")
	if got := d.XPath(nil); got != "" {
		t.Fatalf("XPath(nil) = %q", got)
	}
}

// TestXMLXPathAt: the XML scan answers with the innermost enclosing element,
// keeping original tag case and counting same-named siblings.
func TestXMLXPathAt(t *testing.T) {
	src := `<Root>
  <Item id="1"><Name>Ada</Name></Item>
  <Item id="2"><Name>Grace</Name></Item>
  <Meta/>
</Root>`
	for _, tc := range []struct {
		at   string
		want string
	}{
		{"Ada", "/Root/Item/Name"},
		{"Grace", "/Root/Item[2]/Name"},
		{`id="2"`, "/Root/Item[2]"},
		{"<Meta", "/Root/Meta"},
	} {
		off := strings.Index(src, tc.at)
		got, ok := XMLXPathAt(src, off)
		if !ok || got != tc.want {
			t.Errorf("XMLXPathAt(%q) = %q ok=%v, want %q", tc.at, got, ok, tc.want)
		}
	}
}

// TestXMLXPathAtOutside: before the root there is no enclosing element.
func TestXMLXPathAtOutside(t *testing.T) {
	src := "<?xml version=\"1.0\"?>\n<root/>"
	if _, ok := XMLXPathAt(src, 3); ok {
		t.Fatal("the prolog is inside no element")
	}
}

// TestXMLXPathAtHalfEdited: a document broken past the caret still answers
// for the part that scans — the intention gate must not require validity.
func TestXMLXPathAtHalfEdited(t *testing.T) {
	src := "<root><item>here</item><broken <<<"
	got, ok := XMLXPathAt(src, strings.Index(src, "here"))
	if !ok || got != "/root/item" {
		t.Fatalf("XMLXPathAt = %q ok=%v, want /root/item", got, ok)
	}
}

// TestXMLOffset: 0-based line/rune-column coordinates land on the byte, with
// multi-byte runes counted as one column.
func TestXMLOffset(t *testing.T) {
	src := "<a>\n<é x=\"1\"/>\n</a>"
	if off := XMLOffset(src, 1, 1); src[off:off+2] != "é" {
		t.Fatalf("offset %d does not sit on é", off)
	}
	if off := XMLOffset(src, 0, 0); off != 0 {
		t.Fatalf("origin = %d", off)
	}
	if off := XMLOffset(src, 9, 0); off != len(src) {
		t.Fatalf("past-end line clamps to len, got %d", off)
	}
}
