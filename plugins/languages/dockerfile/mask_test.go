package langdockerfile

import (
	"testing"

	"ike/internal/lang"
	"ike/internal/secret"
)

func maskTexts(lines []string, spans []lang.Span) map[int][]string {
	out := map[int][]string{}
	for _, s := range spans {
		if s.Capture == secret.Capture {
			out[s.Line] = append(out[s.Line], string([]rune(lines[s.Line])[s.StartCol:s.EndCol]))
		}
	}
	return out
}

func TestDockerfileMask(t *testing.T) {
	lines := []string{
		`ENV DB_PASSWORD=hunter2 APP_ENV=prod`,
		`ARG API_TOKEN="abc123"`,
		`env STRIPE_SECRET_KEY sk_live with spaces`, // legacy form, case-folded
		`ENV HOST=example.com`,
		`RUN echo DB_PASSWORD=leaked`, // not an ENV/ARG line
	}
	got := maskTexts(lines, dockerfileSpans(lines))
	want := map[int]string{0: "hunter2", 1: "abc123", 2: "sk_live with spaces"}
	for li, w := range want {
		if v := got[li]; len(v) != 1 || v[0] != w {
			t.Errorf("line %d masks %v, want %q", li, v, w)
		}
	}
	for _, li := range []int{3, 4} {
		if v := got[li]; len(v) != 0 {
			t.Errorf("line %d masks %v, want nothing", li, v)
		}
	}
}
