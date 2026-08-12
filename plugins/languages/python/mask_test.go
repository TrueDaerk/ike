package langpython

import (
	"testing"

	"ike/internal/highlight"
	"ike/internal/lang"
	"ike/internal/secret"
)

// masked returns the text every mask span on the given line covers.
func masked(t *testing.T, lines []string, line int) []string {
	t.Helper()
	var out []string
	for _, s := range pythonSpans(lines) {
		if s.Line != line || s.Capture != secret.Capture {
			continue
		}
		if s.Replace != secret.Mask {
			t.Errorf("line %d: Replace = %q, want the mask stand-in", line, s.Replace)
		}
		out = append(out, string([]rune(lines[line])[s.StartCol:s.EndCol]))
	}
	return out
}

// one asserts that exactly one mask on the line covers want.
func one(t *testing.T, lines []string, line int, want string) {
	t.Helper()
	got := masked(t, lines, line)
	if len(got) != 1 || got[0] != want {
		t.Errorf("line %d masks %q, want [%q]", line, got, want)
	}
}

// none asserts the line carries no mask at all.
func none(t *testing.T, lines []string, line int) {
	t.Helper()
	if got := masked(t, lines, line); len(got) != 0 {
		t.Errorf("line %d masks %q, want nothing", line, got)
	}
}

// TestMaskSuspectAssignments (#1811): a built-in suspect name masks its value,
// through a plain name, a `self.` attribute and an annotated target.
func TestMaskSuspectAssignments(t *testing.T) {
	lines := []string{
		`self.password = "hunter2"`,
		`API_TOKEN = os.environ["T"]`,
		`    client_secret: str = "abc123"`,
		`cfg.db.private_key = load()`,
	}
	one(t, lines, 0, `"hunter2"`)
	one(t, lines, 1, `os.environ["T"]`)
	one(t, lines, 2, `"abc123"`)
	one(t, lines, 3, `load()`)
}

// TestMaskLeavesHarmlessLinesAlone: a name no pattern finds suspect, a
// comparison, an augmented assignment, a comment and an empty value all stay
// readable.
func TestMaskLeavesHarmlessLinesAlone(t *testing.T) {
	lines := []string{
		`self.timeout = 500`,
		`if password == "x":`,
		`password += tail`,
		`# self.password = "hunter2"`,
		`self.password = `,
		`self.public_key = "ssh-rsa AAAA"`,
		`get_secret("name")`,
	}
	for li := range lines {
		none(t, lines, li)
	}
}

// TestMaskStopsAtComments: a trailing comment stays readable, but a `#` inside
// the value is part of it — cutting there would leak the tail of the secret.
func TestMaskStopsAtComments(t *testing.T) {
	lines := []string{
		`self.password = "hunter2"  # the staging one`,
		`self.password = "a#b"`,
	}
	one(t, lines, 0, `"hunter2"`)
	one(t, lines, 1, `"a#b"`)
}

// TestMaskMultiLineValue: a triple-quoted secret masks on every one of its
// lines — hiding only the first would hide nothing worth hiding — and the code
// after it is read as code again.
func TestMaskMultiLineValue(t *testing.T) {
	lines := []string{
		`PRIVATE_KEY = """`,
		`-----BEGIN RSA PRIVATE KEY-----`,
		`MIIEowIBAAKCAQEA`,
		`"""`,
		`host = "api.example.com"`,
	}
	one(t, lines, 0, `"""`)
	one(t, lines, 1, `-----BEGIN RSA PRIVATE KEY-----`)
	one(t, lines, 2, `MIIEowIBAAKCAQEA`)
	one(t, lines, 3, `"""`)
	none(t, lines, 4)
}

// TestMaskSkipsDocstringBodies: an assignment inside a docstring is prose, not
// code, and the docstring's own name does not make its lines secret.
func TestMaskSkipsDocstringBodies(t *testing.T) {
	lines := []string{
		`DOC = """`,
		`self.password = "hunter2"`,
		`"""`,
	}
	for li := range lines {
		none(t, lines, li)
	}
}

// TestMaskCustomKeyPatterns (#1712): the user's own patterns reach the Python
// producer too — `*timeout*` masks a name the built-in tables never heard of,
// and a `-` entry exempts one they would otherwise mask.
func TestMaskCustomKeyPatterns(t *testing.T) {
	secret.SetKeyPatterns([]string{"*timeout*", "-legacy_token"})
	t.Cleanup(func() { secret.SetKeyPatterns(nil) })

	lines := []string{
		`self.timeout = 500`,
		`self.legacy_token = "visible"`,
		`self.api_token = "abc"`,
		`self.retries = 3`,
	}
	one(t, lines, 0, `500`)
	none(t, lines, 1)
	one(t, lines, 2, `"abc"`)
	none(t, lines, 3)
}

// TestMaskWinsOverOtherFamilies: overlapping spans resolve first-covering-wins,
// so a masked value must outrank the constant conceal (#1701) that would
// otherwise read `500` as a duration.
func TestMaskWinsOverOtherFamilies(t *testing.T) {
	secret.SetKeyPatterns([]string{"*timeout*"})
	t.Cleanup(func() { secret.SetKeyPatterns(nil) })

	lines := []string{"timeout = 5000"}
	for _, s := range pythonSpans(lines) {
		if s.Line != 0 || s.StartCol > 10 || s.EndCol <= 10 {
			continue
		}
		if s.Capture != secret.Capture {
			t.Fatalf("first span covering the value is %q, want %q", s.Capture, secret.Capture)
		}
		return
	}
	t.Fatal("no span covers the value")
}

// TestMaskThroughHighlightPipeline: the mask reaches the editor through the
// registry, not only through the local producer.
func TestMaskThroughHighlightPipeline(t *testing.T) {
	l, ok := lang.ByPath("/p/settings.py")
	if !ok || l.Spans == nil {
		t.Fatal("python: no Spans producer registered")
	}
	lines := []string{`self.password = "hunter2"`}
	for _, s := range highlight.Highlight("/p/settings.py", lines) {
		if s.Line == 0 && s.Capture == secret.Capture && s.Replace == secret.Mask {
			return
		}
	}
	t.Error("the highlight pipeline must carry the mask span for a .py path")
}
