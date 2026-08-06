package highlight

import "testing"

// regexCaptureAt reads the winning capture at (line, col) through the same
// first-covering-wins index lookup the editor uses.
func regexCaptureAt(spans []Span, line, col int) string {
	return NewIndex(spans).CaptureAt(line, col)
}

func TestRegexSpansCharClass(t *testing.T) {
	// [a-z]+ — the class body colours as regex.class, the quantifier after it.
	spans := RegexSpans([]string{`[a-z]+`})
	for col := 0; col < 5; col++ {
		if got := regexCaptureAt(spans, 0, col); got != "regex.class" {
			t.Errorf("col %d = %q, want regex.class", col, got)
		}
	}
	if got := regexCaptureAt(spans, 0, 5); got != "regex.quantifier" {
		t.Errorf("quantifier = %q, want regex.quantifier", got)
	}
}

func TestRegexSpansClassEscapeWins(t *testing.T) {
	// Escapes inside a class emit before the class span, so they win.
	spans := RegexSpans([]string{`[\d.]`})
	if got := regexCaptureAt(spans, 0, 1); got != "regex.class" {
		t.Errorf(`\d in class = %q, want regex.class`, got)
	}
	if got := regexCaptureAt(spans, 0, 3); got != "regex.class" {
		t.Errorf("dot in class = %q, want regex.class", got)
	}
	// \b inside a class is backspace, not an anchor.
	spans = RegexSpans([]string{`[\b]`})
	if got := regexCaptureAt(spans, 0, 1); got != "regex.escape" {
		t.Errorf(`\b in class = %q, want regex.escape`, got)
	}
}

func TestRegexSpansLiteralBracketMember(t *testing.T) {
	// []] — a ] directly after [ is a literal member, the class stays open
	// until the second ].
	spans := RegexSpans([]string{`[]a]x`})
	if got := regexCaptureAt(spans, 0, 3); got != "regex.class" {
		t.Errorf("closing ] = %q, want regex.class", got)
	}
	if got := regexCaptureAt(spans, 0, 4); got != "" {
		t.Errorf("literal x = %q, want no capture", got)
	}
}

func TestRegexSpansEscapes(t *testing.T) {
	spans := RegexSpans([]string{`\d\.\b\p{L}`})
	cases := []struct {
		col  int
		want string
	}{
		{0, "regex.class"},  // \d shorthand class
		{2, "regex.escape"}, // \. plain escape
		{4, "regex.anchor"}, // \b word boundary
		{6, "regex.class"},  // \p{L} unicode class
		{9, "regex.class"},  // …including the brace body
	}
	for _, c := range cases {
		if got := regexCaptureAt(spans, 0, c.col); got != c.want {
			t.Errorf("col %d = %q, want %q", c.col, got, c.want)
		}
	}
}

func TestRegexSpansQuantifiersAnchorsAlternation(t *testing.T) {
	spans := RegexSpans([]string{`^ab*?|cd{2,5}$`})
	cases := []struct {
		col  int
		want string
	}{
		{0, "regex.anchor"},      // ^
		{1, ""},                  // literal a
		{3, "regex.quantifier"},  // *
		{4, "regex.quantifier"},  // lazy ?
		{5, "regex.alternation"}, // |
		{8, "regex.quantifier"},  // {
		{10, "regex.quantifier"}, // ,
		{12, "regex.quantifier"}, // }
		{13, "regex.anchor"},     // $
	}
	for _, c := range cases {
		if got := regexCaptureAt(spans, 0, c.col); got != c.want {
			t.Errorf("col %d = %q, want %q", c.col, got, c.want)
		}
	}
}

func TestRegexSpansInvalidCountIsLiteral(t *testing.T) {
	spans := RegexSpans([]string{`a{x}`})
	for col := 1; col < 4; col++ {
		if got := regexCaptureAt(spans, 0, col); got != "" {
			t.Errorf("col %d = %q, want no capture", col, got)
		}
	}
}

func TestRegexSpansGroupPairColors(t *testing.T) {
	// (a(b)c) — outer pair depth 0, inner pair depth 1; open and close of the
	// same group share a rainbow slot.
	spans := RegexSpans([]string{`(a(b)c)`})
	outer, inner := RainbowCapture(0), RainbowCapture(1)
	if got := regexCaptureAt(spans, 0, 0); got != outer {
		t.Errorf("outer open = %q, want %q", got, outer)
	}
	if got := regexCaptureAt(spans, 0, 6); got != outer {
		t.Errorf("outer close = %q, want %q", got, outer)
	}
	if got := regexCaptureAt(spans, 0, 2); got != inner {
		t.Errorf("inner open = %q, want %q", got, inner)
	}
	if got := regexCaptureAt(spans, 0, 4); got != inner {
		t.Errorf("inner close = %q, want %q", got, inner)
	}
}

func TestRegexSpansGroupFlatWhenRainbowOff(t *testing.T) {
	SetRainbow(false)
	defer SetRainbow(true)
	spans := RegexSpans([]string{`(a)`})
	if got := regexCaptureAt(spans, 0, 0); got != "regex.group" {
		t.Errorf("open = %q, want regex.group", got)
	}
	if got := regexCaptureAt(spans, 0, 2); got != "regex.group" {
		t.Errorf("close = %q, want regex.group", got)
	}
}

func TestRegexSpansGroupModifiers(t *testing.T) {
	// Non-capturing and lookahead openers colour whole with the group's slot.
	spans := RegexSpans([]string{`(?:a)(?!b)`})
	slot := RainbowCapture(0)
	for _, col := range []int{0, 1, 2, 4, 5, 6, 7, 9} {
		if got := regexCaptureAt(spans, 0, col); got != slot {
			t.Errorf("col %d = %q, want %q", col, got, slot)
		}
	}
}

func TestRegexSpansNamedGroup(t *testing.T) {
	spans := RegexSpans([]string{`(?P<year>\d{4})`})
	slot := RainbowCapture(0)
	if got := regexCaptureAt(spans, 0, 0); got != slot {
		t.Errorf("opener = %q, want %q", got, slot)
	}
	for col := 4; col < 8; col++ {
		if got := regexCaptureAt(spans, 0, col); got != "regex.group.name" {
			t.Errorf("name col %d = %q, want regex.group.name", col, got)
		}
	}
	if got := regexCaptureAt(spans, 0, 8); got != slot {
		t.Errorf("name closer > = %q, want %q", got, slot)
	}
	if got := regexCaptureAt(spans, 0, 14); got != slot {
		t.Errorf("group close = %q, want %q", got, slot)
	}
}

func TestRegexSpansInlineFlags(t *testing.T) {
	spans := RegexSpans([]string{`(?im-s:a)`})
	slot := RainbowCapture(0)
	if got := regexCaptureAt(spans, 0, 1); got != slot {
		t.Errorf("(? = %q, want %q", got, slot)
	}
	for col := 2; col < 6; col++ {
		if got := regexCaptureAt(spans, 0, col); got != "regex.flags" {
			t.Errorf("flag col %d = %q, want regex.flags", col, got)
		}
	}
	if got := regexCaptureAt(spans, 0, 6); got != slot {
		t.Errorf(": = %q, want %q", got, slot)
	}
}

func TestRegexSpansCommentGroup(t *testing.T) {
	spans := RegexSpans([]string{`(?#note)a`})
	for col := 0; col < 8; col++ {
		if got := regexCaptureAt(spans, 0, col); got != "regex.comment" {
			t.Errorf("col %d = %q, want regex.comment", col, got)
		}
	}
	if got := regexCaptureAt(spans, 0, 8); got != "" {
		t.Errorf("literal after comment = %q, want no capture", got)
	}
}

func TestRegexSpansUnmatchedCloseIsLiteral(t *testing.T) {
	spans := RegexSpans([]string{`a)b`})
	if got := regexCaptureAt(spans, 0, 1); got != "" {
		t.Errorf("unmatched ) = %q, want no capture", got)
	}
}

func TestRegexSpansMultilineClass(t *testing.T) {
	// A class spanning lines (raw-string regex) emits one span per line.
	spans := RegexSpans([]string{`[ab`, `cd]e`})
	if got := regexCaptureAt(spans, 0, 2); got != "regex.class" {
		t.Errorf("line 0 = %q, want regex.class", got)
	}
	if got := regexCaptureAt(spans, 1, 0); got != "regex.class" {
		t.Errorf("line 1 = %q, want regex.class", got)
	}
	if got := regexCaptureAt(spans, 1, 3); got != "" {
		t.Errorf("after class = %q, want no capture", got)
	}
}

func TestRegexThemeDerivation(t *testing.T) {
	th := NewTheme(nil, nil)
	for capture := range regexSources {
		if _, ok := th.Style(capture); !ok {
			t.Errorf("capture %q resolves no style", capture)
		}
	}
}
