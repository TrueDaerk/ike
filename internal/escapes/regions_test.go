package escapes

import "testing"

func TestUnicodeCSSSpans(t *testing.T) {
	lines := []string{
		`content: "caf\e9 s";`,                // é plus its terminating space
		`.icon::before { content: "\f00c"; }`, // private use: not graphic, stays raw
		`content: "\00e9";`,
		`font-family: "a\\b";`, // identity escape, stays raw
	}
	spans := UnicodeCSSSpans(lines)
	if len(spans) != 2 {
		t.Fatalf("got %d spans, want 2: %+v", len(spans), spans)
	}
	s := spans[0]
	if s.Line != 0 || s.Replace != "é" {
		t.Errorf("span 0 = %+v, want é on line 0", s)
	}
	if got := string([]rune(lines[0])[s.StartCol:s.EndCol]); got != `\e9 ` {
		t.Errorf("span 0 covers %q, want the escape plus its terminating space", got)
	}
	if s := spans[1]; s.Line != 2 || s.Replace != "é" {
		t.Errorf("span 1 = %+v, want é on line 2", s)
	}
}

func TestUnicodeCSSSpansGreedyDigits(t *testing.T) {
	// Six digits maximum, taken greedily: \0000e9a is the six-digit 0000e9
	// followed by a literal a.
	spans := UnicodeCSSSpans([]string{`content: "\0000e9a";`})
	if len(spans) != 1 || spans[0].Replace != "é" {
		t.Fatalf("got %+v, want one é span", spans)
	}
	if got := spans[0].EndCol - spans[0].StartCol; got != 7 {
		t.Errorf("span covers %d runes, want the 7 of \\0000e9", got)
	}
}

func TestUnicodeANSICSpans(t *testing.T) {
	lines := []string{
		`echo $'caf\u00e9' done`,
		`echo 'caf\u00e9'`,   // plain single quotes: raw
		`echo "caf\u00e9"`,   // double quotes: no unicode forms
		`echo $'tab\tx\xe9'`, // \t ordinary, \xe9 decodes
	}
	spans := UnicodeANSICSpans(lines)
	if len(spans) != 2 {
		t.Fatalf("got %d spans, want 2: %+v", len(spans), spans)
	}
	if s := spans[0]; s.Line != 0 || s.Replace != "é" {
		t.Errorf("span 0 = %+v, want é on line 0", s)
	}
	if got := string([]rune(lines[0])[spans[0].StartCol:spans[0].EndCol]); got != `\u00e9` {
		t.Errorf("span 0 covers %q, want the \\u escape", got)
	}
	if s := spans[1]; s.Line != 3 || s.Replace != "é" {
		t.Errorf("span 1 = %+v, want the \\xe9 é on line 3", s)
	}
}
