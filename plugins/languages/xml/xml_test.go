package langxml

import (
	"testing"

	"ike/internal/lang"

	// The .xhtml guard below needs the HTML plugin registered (#855):
	// without it the path resolves to nothing and the assertion is vacuous.
	_ "ike/plugins/languages/web"
)

// TestXMLRegistered guards #1253: .xml and the common XML dialects resolve to
// the xml language, with the <!-- --> block comment, no line comment and no
// LSP server (lemminx needs a JVM — highlighting only).
func TestXMLRegistered(t *testing.T) {
	for _, path := range []string{
		"/p/pom.xml",
		"/p/schema.xsd",
		"/p/transform.xsl",
		"/p/transform.xslt",
		"/p/logo.svg",
		"/p/Info.plist",
		"/p/service.wsdl",
		"/p/App.csproj",
		"/p/Directory.Build.props",
		"/p/Directory.Build.targets",
	} {
		l, ok := lang.ByPath(path)
		if !ok {
			t.Errorf("%s: no language registered", path)
			continue
		}
		if l.ID != "xml" {
			t.Errorf("%s → %s, want xml", path, l.ID)
		}
	}

	// .xhtml stays with the HTML plugin — its own server and grammar handle
	// it better than the generic XML grammar.
	if l, ok := lang.ByPath("/p/page.xhtml"); ok && l.ID == "xml" {
		t.Error("page.xhtml must not resolve to xml")
	}

	l, _ := lang.ByID("xml")
	if l.Server != nil {
		t.Errorf("server = %+v, want none (no bundled XML language server)", l.Server)
	}

	line, block, ok := lang.Comments("/p/pom.xml")
	if !ok {
		t.Fatal("no comment support registered for xml")
	}
	if line != "" {
		t.Errorf("line comment = %q, want none — XML has no line comment form", line)
	}
	if block != [2]string{"<!--", "-->"} {
		t.Errorf("block comment = %v, want <!-- -->", block)
	}
}

// TestXMLEntitySpans (#1620): only the five predefined entities (plus
// numeric references) decode — HTML-only names stay raw.
func TestXMLEntitySpans(t *testing.T) {
	l, ok := lang.ByID("xml")
	if !ok || l.Spans == nil {
		t.Fatal("xml: no Spans producer registered")
	}
	spans := l.Spans([]string{`<a>&amp; &auml; &#x2026;</a>`})
	if len(spans) != 2 || spans[0].Replace != "&" || spans[1].Replace != "\u2026" {
		t.Errorf("spans = %+v, want &amp; and &#x2026; decoded, &auml; raw", spans)
	}
}
