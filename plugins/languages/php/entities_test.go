package langphp

import (
	"testing"

	"ike/internal/escapes"
)

func TestPHPEntitiesInMarkupOnly(t *testing.T) {
	lines := []string{
		`<h1>Fish &amp; Chips</h1>`,
		`<?php`,
		`$s = "&amp;"; // code: stays literal`,
		`?>`,
		`<p>&copy; 2026</p>`,
		`<p>&amp; <?php echo $x; ?> &lt;</p>`,
	}
	got := map[int][]string{}
	for _, s := range entitySpans(lines) {
		if s.Capture == escapes.EntityCapture {
			got[s.Line] = append(got[s.Line], s.Replace)
		}
	}
	if v := got[0]; len(v) != 1 || v[0] != "&" {
		t.Errorf("line 0 decodes %v, want the markup reference", v)
	}
	if v := got[2]; len(v) != 0 {
		t.Errorf("line 2 decodes %v, want nothing inside PHP code", v)
	}
	if v := got[4]; len(v) != 1 || v[0] != "©" {
		t.Errorf("line 4 decodes %v, want the copyright sign", v)
	}
	if v := got[5]; len(v) != 2 || v[0] != "&" || v[1] != "<" {
		t.Errorf("line 5 decodes %v, want both references around the code island", v)
	}
}

// TestPHPSpansWiring: the hook layers the #2345 families — masks, network,
// permission and cron hints, entities — over the existing conceals.
func TestPHPSpansWiring(t *testing.T) {
	lines := []string{
		`$password = "hunter2";`,
		`$cidr = "10.0.0.0/8";`,
		`chmod($f, 0o755);`,
		`$cron = "*/5 * * * *";`,
	}
	want := map[int]string{0: "secret.value", 1: "net.cidr", 2: "perm.mode", 3: "cron.hint"}
	found := map[int]bool{}
	for _, s := range phpSpans(lines) {
		if want[s.Line] == s.Capture {
			found[s.Line] = true
		}
	}
	for li, capture := range want {
		if !found[li] {
			t.Errorf("line %d: no %q span produced", li, capture)
		}
	}
}
