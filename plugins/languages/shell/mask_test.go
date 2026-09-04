package langshell

import (
	"testing"

	"ike/internal/escapes"
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

func TestShellMaskAssignments(t *testing.T) {
	lines := []string{
		`export DB_PASSWORD=hunter2`,
		`API_TOKEN="abc 123" ./run.sh`,
		`declare -x STRIPE_SECRET_KEY=sk_live_x`,
		`HOST=example.com`,
		`echo DB_PASSWORD=leaked`, // not an assignment position
		`# DB_PASSWORD=commented`,
	}
	got := maskTexts(lines, shellSpans(lines))
	want := map[int]string{0: "hunter2", 1: "abc 123", 2: "sk_live_x"}
	for li, w := range want {
		if v := got[li]; len(v) != 1 || v[0] != w {
			t.Errorf("line %d masks %v, want %q", li, v, w)
		}
	}
	for _, li := range []int{3, 4, 5} {
		if v := got[li]; len(v) != 0 {
			t.Errorf("line %d masks %v, want nothing", li, v)
		}
	}
}

// TestShellSpansCarryAllFamilies: the hook layers masks, permission hints and
// ANSI-C unicode decoding (#2345) into one producer.
func TestShellSpansCarryAllFamilies(t *testing.T) {
	lines := []string{
		`chmod 755 /tmp/f`,
		`echo $'caf\u00e9'`,
	}
	var perm, uni bool
	for _, s := range shellSpans(lines) {
		switch {
		case s.Line == 0 && s.Capture == "perm.mode":
			perm = true
		case s.Line == 1 && s.Capture == escapes.UnicodeCapture:
			uni = true
		}
	}
	if !perm || !uni {
		t.Fatalf("perm=%v unicode=%v, want both families produced", perm, uni)
	}
}
