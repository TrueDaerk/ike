//go:build cgo

package langphp

import (
	"strings"
	"testing"

	"ike/internal/highlight"
)

// TestPHPStringInterpolation guards #1466: interpolated expressions inside
// double-quoted strings and heredocs highlight as code (the old whole-node
// (encapsed_string) / (heredoc_body) captures started before any
// interpolation and won the first-covering lookup), while nowdoc bodies and
// escaped `\$` stay plain string.
func TestPHPStringInterpolation(t *testing.T) {
	highlight.SetRainbow(false)
	defer highlight.SetRainbow(true)
	lines := []string{
		`<?php`,
		`$b = "abc{$x}def";`,
		`$c = "abc{$obj->m()}def";`,
		`$a = "abc${y}def";`,
		`$d = "pre $var mid $arr[key] post $o->prop";`,
		`$e = <<<TXT`,
		`  value: {$z} plain`,
		`TXT;`,
		`$f = <<<'RAW'`,
		`  raw {$x} $y`,
		`RAW;`,
		`$g = "no \$x here";`,
	}
	spans := highlight.Highlight("main.php", lines)
	if len(spans) == 0 {
		t.Fatal("expected spans for PHP source, got none")
	}
	ix := highlight.NewIndex(spans)
	cases := []struct {
		name string
		line int
		word string
		want string
	}{
		// Complex interpolation {$x}
		{"literal before interpolation", 1, "abc", "string"},
		{"open brace", 1, "{", "punctuation.special"},
		{"interpolated variable", 1, "$x", "variable"},
		{"close brace", 1, "}", "punctuation.special"},
		{"literal after interpolation", 1, "def", "string"},
		{"closing quote", 1, `";`, "string"},
		// Complex interpolation with a method call
		{"object variable", 2, "$obj", "variable"},
		{"method name", 2, "m()", "function.method"},
		// Deprecated ${y} form
		{"dollar of dollar-brace", 3, "${", "punctuation.special"},
		{"brace of dollar-brace", 3, "{y", "punctuation.special"},
		{"dollar-brace name", 3, "y}", "variable"},
		{"dollar-brace close", 3, "}", "punctuation.special"},
		// Simple interpolation
		{"simple variable", 4, "$var", "variable"},
		{"literal between", 4, " mid ", "string"},
		{"array variable", 4, "$arr", "variable"},
		{"array index", 4, "key]", "property"},
		{"property object", 4, "$o->", "variable"},
		{"property name", 4, "prop", "property"},
		// Heredoc
		{"heredoc start", 5, "TXT", "string"},
		{"heredoc literal", 6, "value:", "string"},
		{"heredoc open brace", 6, "{", "punctuation.special"},
		{"heredoc variable", 6, "$z", "variable"},
		{"heredoc close brace", 6, "}", "punctuation.special"},
		{"heredoc end", 7, "TXT", "string"},
		// Nowdoc: no interpolation, everything stays string
		{"nowdoc brace", 9, "{", "string"},
		{"nowdoc fake variable", 9, "$x", "string"},
		{"nowdoc bare variable", 9, "$y", "string"},
		// Escaped \$ stays string-colored (its own themable sub-scope)
		{"escaped dollar", 11, `\$x`, "string.escape"},
		{"literal around escape", 11, "no ", "string"},
	}
	for _, c := range cases {
		col := strings.Index(lines[c.line], c.word)
		if col < 0 {
			t.Fatalf("%s: %q not in line %d", c.name, c.word, c.line)
		}
		if got := ix.CaptureAt(c.line, col); got != c.want {
			t.Errorf("%s: CaptureAt(%d,%d) = %q, want %q", c.name, c.line, col, got, c.want)
		}
	}
}
