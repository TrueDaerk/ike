package secret

import (
	"testing"

	"ike/internal/lang"
)

// cover returns the text a span covers.
func cover(lines []string, s lang.Span) string {
	return string([]rune(lines[s.Line])[s.StartCol:s.EndCol])
}

func TestPairSpans(t *testing.T) {
	lines := []string{
		`password = "hunter2"`,
		`host = example.com`,
		`# password = commented`,
		`[credentials]`,
		`api_token: abc123`,
		`MAILTO=ops@example.com`,
		`*/5 * * * * FOO_PASSWORD=x cmd`, // key would hold spaces: no assignment
		`db_password = hunter2 trailing`,
	}
	spans := PairSpans(lines, "=:")
	want := map[int]string{
		0: "hunter2", // quoted: content only, quotes stay
		4: "abc123",
		7: "hunter2 trailing", // bare: to the line end, never cut mid-value
	}
	if len(spans) != len(want) {
		t.Fatalf("got %d spans, want %d: %+v", len(spans), len(want), spans)
	}
	for _, s := range spans {
		if s.Capture != Capture || s.Replace != Mask {
			t.Errorf("span %+v must carry the mask stand-in", s)
		}
		if got := cover(lines, s); got != want[s.Line] {
			t.Errorf("line %d masks %q, want %q", s.Line, got, want[s.Line])
		}
	}
}

func TestPairSpansSeparatorSet(t *testing.T) {
	// With "=" alone a colon line is no assignment — the crontab/TOML shape.
	if spans := PairSpans([]string{`api_token: abc123`}, "="); len(spans) != 0 {
		t.Fatalf("colon must not separate under seps==\"=\", got %+v", spans)
	}
}

func TestAssignSpans(t *testing.T) {
	lines := []string{
		`password := "hunter2"`,                           // Go short declaration
		`const apiKey = "sk_live_x"`,                      // JS const
		`$this->password = "hunter2";`,                    // PHP member
		`var dbPassword string = "hunter2"`,               // Go var with type
		`token = os.Getenv("API_TOKEN")`,                  // name literal stays readable
		`if password == "hunter2" {`,                      // comparison, no mask
		`timeout = 500`,                                   // no literal, nothing to mask
		`secret = "DB_PASSWORD"`,                          // sole literal is the value
		`apiKey = user.Name + "-" + "PROXY_API_KEY_SALT"`, // suspect name literal in expression
	}
	spans := AssignSpans(lines)
	want := map[int][]string{
		0: {"hunter2"},
		1: {"sk_live_x"},
		2: {"hunter2"},
		3: {"hunter2"},
		7: {"DB_PASSWORD"},
		8: {"-"},
	}
	got := map[int][]string{}
	for _, s := range spans {
		if s.Capture != Capture || s.Replace != Mask {
			t.Errorf("span %+v must carry the mask stand-in", s)
		}
		got[s.Line] = append(got[s.Line], cover(lines, s))
	}
	for li, w := range want {
		if len(got[li]) != len(w) {
			t.Errorf("line %d masks %v, want %v", li, got[li], w)
			continue
		}
		for i := range w {
			if got[li][i] != w[i] {
				t.Errorf("line %d span %d masks %q, want %q", li, i, got[li][i], w[i])
			}
		}
	}
	for li := range got {
		if _, ok := want[li]; !ok {
			t.Errorf("line %d must not mask, got %v", li, got[li])
		}
	}
}

func TestAssignSpansAnnotationsAndComments(t *testing.T) {
	lines := []string{
		`const token: string = "abc" // dev only`,
		`private readonly apiKey: Map<string, string> = "x"`,
		`password = "with # inside" # note`,
	}
	spans := AssignSpans(lines)
	want := map[int]string{0: "abc", 1: "x", 2: "with # inside"}
	if len(spans) != len(want) {
		t.Fatalf("got %d spans, want %d: %+v", len(spans), len(want), spans)
	}
	for _, s := range spans {
		if got := cover(lines, s); got != want[s.Line] {
			t.Errorf("line %d masks %q, want %q", s.Line, got, want[s.Line])
		}
	}
}

func TestAssignSpansUserPatterns(t *testing.T) {
	SetKeyPatterns([]string{"*_license"})
	t.Cleanup(func() { SetKeyPatterns(nil) })
	spans := AssignSpans([]string{`acme_license := "AAAA"`, `ACME_LICENSE = "BBBB"`})
	if len(spans) != 2 {
		t.Fatalf("configured patterns must reach the assignment producer, got %+v", spans)
	}
}
