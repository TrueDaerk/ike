package idcase

import (
	"strings"
	"testing"
)

func TestDetect(t *testing.T) {
	cases := []struct {
		in   string
		want Style
	}{
		{"fooBarBaz", Camel},
		{"foo_bar_baz", Snake},
		{"foo-bar-baz", Kebab},
		{"FooBarBaz", Pascal},
		{"FOO_BAR_BAZ", Screaming},
		{"foo", Camel},
		{"Foo", Pascal},
		{"URL", Screaming},
		{"_private", Camel},   // the affix is stripped, then a lone lowercase word is camel
		{"__dunder__", Camel}, //
		{"Foo_bar", Unknown},  // mixed: names no single style
		{"Foo-Bar", Unknown},
		{"foo_bar-baz", Unknown}, // the two separators never mix
		{"", Unknown},
		{"42", Unknown},
		{"42abc", Unknown},
		{"a - b", Unknown},
		{"foo.bar", Unknown},
		{"foo bar", Unknown},
		{"_", Unknown},
		{"foo__bar", Unknown}, // doubled separator
		{"foo-", Unknown},
		{"-foo", Unknown},
	}
	for _, c := range cases {
		if got := Detect(c.in); got != c.want {
			t.Errorf("Detect(%q) = %v, want %v", c.in, got, c.want)
		}
	}
}

func TestConvertAcrossStyles(t *testing.T) {
	targets := []struct {
		style Style
		want  string
	}{
		{Camel, "fooBarBaz"},
		{Snake, "foo_bar_baz"},
		{Kebab, "foo-bar-baz"},
		{Pascal, "FooBarBaz"},
		{Screaming, "FOO_BAR_BAZ"},
	}
	for _, src := range []string{"fooBarBaz", "foo_bar_baz", "foo-bar-baz", "FooBarBaz", "FOO_BAR_BAZ"} {
		for _, tc := range targets {
			if got := Convert(src, tc.style); got != tc.want {
				t.Errorf("Convert(%q, %v) = %q, want %q", src, tc.style, got, tc.want)
			}
		}
	}
}

func TestCycleRotatesThroughAllFiveAndBack(t *testing.T) {
	want := []string{"foo_bar", "foo-bar", "FooBar", "FOO_BAR", "fooBar"}
	s := "fooBar"
	for i, w := range want {
		out, ok := Cycle(s)
		if !ok {
			t.Fatalf("step %d: Cycle(%q) refused", i, s)
		}
		if out != w {
			t.Fatalf("step %d: Cycle(%q) = %q, want %q", i, s, out, w)
		}
		s = out
	}
}

func TestCycleLeavesNonIdentifiersUntouched(t *testing.T) {
	for _, s := range []string{"42", "a + b", "foo.bar", "Foo_bar", ""} {
		out, ok := Cycle(s)
		if ok {
			t.Errorf("Cycle(%q) claimed a style, got %q", s, out)
		}
		if out != s {
			t.Errorf("Cycle(%q) changed the text to %q", s, out)
		}
	}
}

func TestWordsSplitsAcronymsAndDigits(t *testing.T) {
	cases := []struct {
		in   string
		want []string
	}{
		{"HTTPServer", []string{"HTTP", "Server"}},
		{"parseURL", []string{"parse", "URL"}},
		{"utf8Name", []string{"utf8", "Name"}},
		{"foo_bar-baz", []string{"foo", "bar", "baz"}},
		{"URL", []string{"URL"}},
	}
	for _, c := range cases {
		got := Words(c.in)
		if strings.Join(got, "|") != strings.Join(c.want, "|") {
			t.Errorf("Words(%q) = %v, want %v", c.in, got, c.want)
		}
	}
}

func TestConvertKeepsUnderscoreAffixes(t *testing.T) {
	if got := Convert("_fooBar", Snake); got != "_foo_bar" {
		t.Errorf("Convert(_fooBar, Snake) = %q", got)
	}
	if got := Convert("__init__", Pascal); got != "__Init__" {
		t.Errorf("Convert(__init__, Pascal) = %q", got)
	}
}

func TestAcronymRoundTrip(t *testing.T) {
	// An acronym survives the rotation as one word, though its all-caps
	// spelling is lost — the styles have no way to record it.
	if got := Convert("HTTPServer", Snake); got != "http_server" {
		t.Errorf("Convert(HTTPServer, Snake) = %q", got)
	}
	if got := Convert("http_server", Camel); got != "httpServer" {
		t.Errorf("Convert(http_server, Camel) = %q", got)
	}
}

func TestNextWrapsAndKeepsUnknown(t *testing.T) {
	if got := Next(Screaming); got != Camel {
		t.Errorf("Next(Screaming) = %v", got)
	}
	if got := Next(Unknown); got != Unknown {
		t.Errorf("Next(Unknown) = %v", got)
	}
}
