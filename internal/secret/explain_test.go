package secret

import "testing"

// explain_test.go covers the verdict provenance (#1998): Explain must walk the
// tables in exactly the order Suspect does and name the rule that decided, so
// the popover's "why" matches the buffer's rendering.

func TestExplainReasons(t *testing.T) {
	SetKeyPatterns(nil)
	cases := []struct {
		key     string
		mask    bool
		reason  Reason
		pattern string
	}{
		{"DB_PASSWORD", true, ReasonStrong, "PASSWORD"},
		{"GITHUB_TOKEN", true, ReasonMarker, "TOKEN"},
		{"STRIPE_KEY", true, ReasonSuffix, "_KEY"},
		{"KEY", true, ReasonExact, "KEY"},
		{"TOKEN_URL", false, ReasonPublicMarker, "TOKEN_URL"},
		{"PORT", false, ReasonNone, ""},
		{"", false, ReasonNone, ""},
	}
	for _, c := range cases {
		v := Explain(c.key)
		if v.Mask != c.mask || v.Reason != c.reason || v.Pattern != c.pattern {
			t.Errorf("Explain(%q) = %+v, want %v/%v/%q", c.key, v, c.mask, c.reason, c.pattern)
		}
		if v.Mask != Suspect(c.key) {
			t.Errorf("Explain(%q) disagrees with Suspect", c.key)
		}
	}
}

func TestExplainUserPatterns(t *testing.T) {
	SetKeyPatterns([]string{"*_license", "-PUBLIC_SECRET"})
	defer SetKeyPatterns(nil)
	if v := Explain("ACME_LICENSE"); !v.Mask || v.Reason != ReasonUserPattern || v.Pattern != "*_license" {
		t.Fatalf("configured pattern verdict = %+v", v)
	}
	if v := Explain("PUBLIC_SECRET"); v.Mask || v.Reason != ReasonUserExempt || v.Pattern != "public_secret" {
		t.Fatalf("exempting pattern verdict = %+v", v)
	}
	// A key no entry covers still falls through to the built-in tables.
	if v := Explain("DB_PASSWORD"); !v.Mask || v.Reason != ReasonStrong {
		t.Fatalf("fallthrough verdict = %+v", v)
	}
}
