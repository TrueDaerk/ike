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

// equal asserts the line's masks cover exactly want, in order.
func equal(t *testing.T, line string, want ...string) {
	t.Helper()
	got := masked(t, []string{line}, 0)
	if len(got) != len(want) {
		t.Errorf("%s masks %q, want %q", line, got, want)
		return
	}
	for i := range got {
		if got[i] != want[i] {
			t.Errorf("%s masks %q, want %q", line, got, want)
			return
		}
	}
}

// TestMaskLiteralSpans (#1930): the mask covers the secret string literals of
// a suspect assignment and nothing else — a value that holds no literal holds
// no secret, and a literal naming a key stays readable.
func TestMaskLiteralSpans(t *testing.T) {
	cases := []struct {
		name string
		line string
		want []string
	}{
		// Nothing a literal could hide: a lookup, a call, a name.
		{"subscript lookup", `token = item["token"]`, nil},
		{"call", `token = get_token()`, nil},
		{"name", `token = other_var`, nil},
		{"attribute", `self._token = self._token or fetch()`, nil},
		{"config key", `api_key = config["api_key"]`, nil},
		{"dotted key name", `secret = cfg.get("db.token")`, nil},

		// The literal is the value.
		{"plain value", `token = "abc123"`, []string{"abc123"}},
		{"single quotes", `api_key = 'abc123'`, []string{"abc123"}},
		{"attribute target", `self.password = "hunter2"`, []string{"hunter2"}},
		{"annotated target", `    client_secret: str = "abc123"`, []string{"abc123"}},
		{"byte string", `password = b"abc123"`, []string{"abc123"}},
		{"closed triple", `password = """abc123"""`, []string{"abc123"}},
		{"empty value", `password = ""`, nil},

		// A literal inside an expression: the key name stays, the value goes.
		{"env fallback", `PROXY_API_KEY = os.environ.get("PARSER_PROXY_API_KEY", "84791234")`, []string{"84791234"}},
		{"hyphenated fallback", `PROXY_API_KEY = os.environ.get("PARSER_PROXY_API_KEY", "my-secret")`, []string{"my-secret"}},
		{"sole literal is never a name", `password = "token"`, []string{"token"}},
		{"list of values", `TOKENS = ["abc", "def"]`, []string{"abc", "def"}},

		// Comments and quotes.
		{"trailing comment", `self.password = "hunter2"  # the staging one`, []string{"hunter2"}},
		{"hash inside the value", `self.password = "a#b"`, []string{"a#b"}},
		{"escaped quote", `self.password = "he said \"hi\" #1"`, []string{`he said \"hi\" #1`}},
		{"unterminated string", `self.password = "abc`, []string{`"abc`}},

		// Not an assignment at all.
		{"comparison", `if password == "x":`, nil},
		{"augmented", `password += "tail"`, nil},
		{"commented out", `# self.password = "hunter2"`, nil},
		{"empty right-hand side", `self.password = `, nil},
		{"cleared by a public marker", `self.public_key = "ssh-rsa AAAA"`, nil},
		{"call statement", `get_secret("name")`, nil},
		{"no literal, no mask", `self.timeout = 500`, nil},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) { equal(t, c.line, c.want...) })
	}
}

// TestMaskFStringParts (#1930): an f-string masks its literal text and keeps
// the `{...}` interpolations readable — an interpolation is an expression, and
// only literal text can carry the secret.
func TestMaskFStringParts(t *testing.T) {
	equal(t, `password = f"pw-{user}-x"`, "pw-", "-x")
	equal(t, `password = f"{base}"`)
	equal(t, `password = f"a{{b}}c"`, "a{{b}}c")
	equal(t, `token = f"{a[{k}]}tail"`, "tail")
	equal(t, `TOKEN = os.getenv("TOKEN", f"dev-{user}")`, "dev-")
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
		`self.timeout = "500ms"`,
		`self.legacy_token = "visible"`,
		`self.api_token = "abc"`,
		`self.retries = "3"`,
	}
	one(t, lines, 0, `500ms`)
	none(t, lines, 1)
	one(t, lines, 2, `abc`)
	none(t, lines, 3)
}

// TestMaskWinsOverOtherFamilies: overlapping spans resolve first-covering-wins,
// so a masked value must outrank the network-literal hint (#1653) that would
// otherwise draw over the same string.
func TestMaskWinsOverOtherFamilies(t *testing.T) {
	lines := []string{`api_key = "10.0.0.1"`}
	value := len(`api_key = "`)
	for _, s := range pythonSpans(lines) {
		if s.Line != 0 || s.StartCol > value || s.EndCol <= value {
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
