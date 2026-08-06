//go:build cgo

package langxml

import (
	"testing"

	"ike/internal/highlight"
	"ike/internal/lang"
)

// TestXMLGrammar guards the vendored-source cgo wiring: the grammar is
// non-nil under cgo.
func TestXMLGrammar(t *testing.T) {
	l, ok := lang.ByID("xml")
	if !ok || l.Grammar == nil {
		t.Fatal("xml grammar is nil under cgo")
	}
}

// TestXMLHighlighting parses a small document end-to-end: the XML
// declaration, comments, tags, attribute names and attribute values all
// resolve to their captures.
func TestXMLHighlighting(t *testing.T) {
	lines := []string{
		`<?xml version="1.0" encoding="UTF-8"?>`,
		`<!-- the project root -->`,
		`<project name="ike">`,
		`  <dep scope="test"/>`,
		`</project>`,
	}
	spans := highlight.Highlight("/p/pom.xml", lines)
	if len(spans) == 0 {
		t.Fatal("expected spans for XML source, got none")
	}
	ix := highlight.NewIndex(spans)
	if got := ix.CaptureAt(0, 2); got != "keyword" { // xml in <?xml
		t.Errorf(`"xml" declaration: got capture %q, want keyword`, got)
	}
	if got := ix.CaptureAt(0, 6); got != "property" { // version
		t.Errorf("version pseudo-attribute: got capture %q, want property", got)
	}
	if got := ix.CaptureAt(1, 0); got != "comment" {
		t.Errorf("comment: got capture %q, want comment", got)
	}
	if got := ix.CaptureAt(2, 1); got != "tag" { // project
		t.Errorf("start tag: got capture %q, want tag", got)
	}
	if got := ix.CaptureAt(2, 9); got != "attribute" { // name=
		t.Errorf("attribute name: got capture %q, want attribute", got)
	}
	if got := ix.CaptureAt(2, 14); got != "string" { // "ike"
		t.Errorf("attribute value: got capture %q, want string", got)
	}
	if got := ix.CaptureAt(4, 2); got != "tag" { // project in </project>
		t.Errorf("end tag: got capture %q, want tag", got)
	}
}

// TestXMLEntitiesAndCDATA covers the two payload forms the issue calls out:
// entity references (predefined ones as constant.builtin) and CDATA sections
// (fences as punctuation, payload as string).
func TestXMLEntitiesAndCDATA(t *testing.T) {
	lines := []string{
		`<doc>`,
		`  <t>a &amp; b &custom;</t>`,
		`  <s><![CDATA[if (a < b) {}]]></s>`,
		`</doc>`,
	}
	ix := highlight.NewIndex(highlight.Highlight("/p/doc.xml", lines))
	// Since #1620 the entity-decoding span producer overlays predefined
	// entities as escape.entity (it wins over the grammar's constant.builtin)
	// so the revealed raw reference styles like the other decoded escapes.
	if got := ix.CaptureAt(1, 7); got != "escape.entity" { // &amp;
		t.Errorf("&amp;: got capture %q, want escape.entity", got)
	}
	if got := ix.CaptureAt(1, 15); got != "constant" { // &custom;
		t.Errorf("&custom;: got capture %q, want constant", got)
	}
	if got := ix.CaptureAt(2, 5); got != "punctuation" { // <![CDATA[
		t.Errorf("CDATA start: got capture %q, want punctuation", got)
	}
	if got := ix.CaptureAt(2, 14); got != "string" { // payload
		t.Errorf("CDATA payload: got capture %q, want string", got)
	}
}

// TestXMLProcessingInstruction guards the PI path (<?php-style targets are a
// different language; here it is the generic <?target …?> form).
func TestXMLProcessingInstruction(t *testing.T) {
	lines := []string{
		`<?xml-stylesheet href="s.xsl" type="text/xsl"?>`,
		`<doc/>`,
	}
	ix := highlight.NewIndex(highlight.Highlight("/p/doc.xml", lines))
	if got := ix.CaptureAt(0, 2); got != "keyword" { // xml-stylesheet
		t.Errorf("xml-stylesheet: got capture %q, want keyword", got)
	}
	if got := ix.CaptureAt(1, 1); got != "tag" { // <doc/>
		t.Errorf("empty element tag: got capture %q, want tag", got)
	}
}
