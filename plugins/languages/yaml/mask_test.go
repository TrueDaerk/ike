package langyaml

import (
	"testing"

	"ike/internal/lang"
	"ike/internal/secret"
)

// masksOf collects the mask spans per line, as covered text.
func masksOf(lines []string, spans []lang.Span) map[int][]string {
	out := map[int][]string{}
	for _, s := range spans {
		if s.Capture == secret.Capture {
			out[s.Line] = append(out[s.Line], string([]rune(lines[s.Line])[s.StartCol:s.EndCol]))
		}
	}
	return out
}

func TestMaskSuspectKeys(t *testing.T) {
	lines := []string{
		`db_password: hunter2`,
		`- api_token: "abc123"  # staging`,
		`host: example.com`,
		`password: |`, // block scalar: no inline value to cover
		`  hunter2`,
		`# password: commented`,
	}
	got := masksOf(lines, yamlSpans(lines))
	if v := got[0]; len(v) != 1 || v[0] != "hunter2" {
		t.Errorf("line 0 masks %v, want the bare value", v)
	}
	if v := got[1]; len(v) != 1 || v[0] != "abc123" {
		t.Errorf("line 1 masks %v, want the quoted content without quotes", v)
	}
	for _, li := range []int{2, 3, 4, 5} {
		if v := got[li]; len(v) != 0 {
			t.Errorf("line %d masks %v, want nothing", li, v)
		}
	}
}

func TestMaskStringDataBlock(t *testing.T) {
	lines := []string{
		`apiVersion: v1`,
		`kind: Secret`,
		`stringData:`,
		`  username: alice`, // key harmless, block decides
		`  password: hunter2`,
		`data:`,
		`  other: x`,
		`---`,
		`kind: ConfigMap`,
		`stringData:`,
		`  username: bob`, // not a Secret document
	}
	got := masksOf(lines, yamlSpans(lines))
	if v := got[3]; len(v) != 1 || v[0] != "alice" {
		t.Errorf("line 3 masks %v, want every stringData value", v)
	}
	if v := got[4]; len(v) != 1 || v[0] != "hunter2" {
		t.Errorf("line 4 masks %v, want the password value", v)
	}
	for _, li := range []int{6, 10} {
		if v := got[li]; len(v) != 0 {
			t.Errorf("line %d masks %v, want nothing", li, v)
		}
	}
}

// TestMaskPrecedesBase64Decode: in a Secret's data: block the mask span must
// come before the base64 stand-in, so with masking on the credential renders
// masked, never decoded.
func TestMaskPrecedesBase64Decode(t *testing.T) {
	lines := []string{
		`kind: Secret`,
		`data:`,
		`  password: c2VjcmV0dmFsdWU=`,
	}
	for _, s := range yamlSpans(lines) {
		if s.Line != 2 || s.EndCol <= 12 {
			continue
		}
		if s.Capture != secret.Capture {
			t.Fatalf("first span covering the value is %q, want the mask", s.Capture)
		}
		return
	}
	t.Fatal("no span covers the data value")
}
