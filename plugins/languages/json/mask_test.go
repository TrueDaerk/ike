package langjson

import (
	"testing"

	"ike/internal/lang"
	"ike/internal/secret"
)

// masked returns the source text the mask spans of one line cover.
func masked(spans []lang.Span, lines []string, line int) []string {
	var out []string
	for _, s := range spans {
		if s.Line == line && s.Capture == secret.Capture {
			out = append(out, string([]rune(lines[line])[s.StartCol:s.EndCol]))
		}
	}
	return out
}

func TestMaskSuspectMembers(t *testing.T) {
	lines := []string{
		`{`,
		`  "password": "hunter2",`,
		`  "STRIPE_SECRET_KEY": "sk_live_abc123",`,
		`  "api_key": "abc",`,
		`  "host": "api.example.com",`,
		`  "public_key": "ssh-rsa AAAA",`,
		`  "token_url": "https://example.com/oauth"`,
		`}`,
	}
	spans := jsonSpans(lines)

	for _, c := range []struct {
		line int
		want string
	}{
		{1, "hunter2"},
		{2, "sk_live_abc123"},
		{3, "abc"},
	} {
		got := masked(spans, lines, c.line)
		if len(got) != 1 || got[0] != c.want {
			t.Errorf("line %d masks %q, want [%q]", c.line, got, c.want)
		}
	}
	for _, li := range []int{4, 5, 6} {
		if got := masked(spans, lines, li); len(got) != 0 {
			t.Errorf("line %d masks %q, want nothing", li, got)
		}
	}

	for _, s := range spans {
		if s.Capture == secret.Capture && s.Replace != secret.Mask {
			t.Errorf("Replace = %q, want the mask stand-in", s.Replace)
		}
	}
}

// TestMaskQuotesStayVisible: only the string's content is covered, so the
// member still reads as a string in the render.
func TestMaskQuotesStayVisible(t *testing.T) {
	lines := []string{`{"password": "xyz"}`}
	spans := jsonSpans(lines)
	var got []lang.Span
	for _, s := range spans {
		if s.Capture == secret.Capture {
			got = append(got, s)
		}
	}
	if len(got) != 1 {
		t.Fatalf("got %d mask spans, want 1", len(got))
	}
	runes := []rune(lines[0])
	if runes[got[0].StartCol-1] != '"' || runes[got[0].EndCol] != '"' {
		t.Errorf("mask covers %d..%d, want the quotes left out", got[0].StartCol, got[0].EndCol)
	}
}

// TestMaskNestedAndNonString: only the key directly in front of a value
// decides, and a value that is not a string has nothing to stand in for.
func TestMaskNestedAndNonString(t *testing.T) {
	lines := []string{
		`{"secrets": {"name": "prod", "value": "s3cr3t"}, "password": 1234, "auth": true}`,
		`{"password": {"kind": "bearer"}}`,
		`{"tokens": ["aaa", "bbb"]}`,
	}
	spans := jsonSpans(lines)
	for li := range lines {
		if got := masked(spans, lines, li); len(got) != 0 {
			t.Errorf("line %d masks %q, want nothing", li, got)
		}
	}
}

// TestMaskEmptyValue: a mask over an empty value would only read as a value
// that is not there.
func TestMaskEmptyValue(t *testing.T) {
	lines := []string{`{"password": ""}`}
	if got := masked(jsonSpans(lines), lines, 0); len(got) != 0 {
		t.Errorf("an empty value masks %q, want nothing", got)
	}
}

// TestMaskWrappedMember: a member broken over two lines must not leave the
// value in the clear.
func TestMaskWrappedMember(t *testing.T) {
	lines := []string{
		`{`,
		`  "password":`,
		`    "hunter2",`,
		`  "host":`,
		`    "example.com"`,
		`}`,
	}
	spans := jsonSpans(lines)
	if got := masked(spans, lines, 2); len(got) != 1 || got[0] != "hunter2" {
		t.Errorf("wrapped value masks %q, want [\"hunter2\"]", got)
	}
	if got := masked(spans, lines, 4); len(got) != 0 {
		t.Errorf("wrapped harmless value masks %q, want nothing", got)
	}
}

// TestMaskEscapedQuotes: a `\"` inside a value does not end the string, so the
// whole credential stays covered and the text after it is not mistaken for a
// member.
func TestMaskEscapedQuotes(t *testing.T) {
	lines := []string{`{"password": "a\"b", "host": "x"}`}
	spans := jsonSpans(lines)
	if got := masked(spans, lines, 0); len(got) != 1 || got[0] != `a\"b` {
		t.Errorf("masks %q, want the whole escaped value", got)
	}
}

// TestMaskCustomKeyPatterns: the user's own patterns (#1712) reach the JSON
// producer — a key the built-in tables never heard of masks, and a `-` entry
// exempts one they would otherwise mask.
func TestMaskCustomKeyPatterns(t *testing.T) {
	secret.SetKeyPatterns([]string{"*_LICENSE", "-PUBLIC_TOKEN"})
	t.Cleanup(func() { secret.SetKeyPatterns(nil) })

	lines := []string{
		`{"acme_license": "AAAA-BBBB", "PUBLIC_TOKEN": "pk_live", "api_token": "abc"}`,
	}
	got := masked(jsonSpans(lines), lines, 0)
	want := []string{"AAAA-BBBB", "abc"}
	if len(got) != len(want) {
		t.Fatalf("masks %q, want %q", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("mask %d covers %q, want %q", i, got[i], want[i])
		}
	}
}

// TestMaskUnterminatedString: an invalid buffer (the state a file is in while
// a credential is being pasted) must not panic or mask past the line.
func TestMaskUnterminatedString(t *testing.T) {
	lines := []string{`{"password": "hunter`, `"host": "x"`}
	spans := jsonSpans(lines)
	if got := masked(spans, lines, 0); len(got) != 0 {
		t.Errorf("unterminated value masks %q, want nothing", got)
	}
	if got := masked(spans, lines, 1); len(got) != 0 {
		t.Errorf("line after an unterminated string masks %q, want nothing", got)
	}
}
