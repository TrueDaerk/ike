package settings

import (
	"strings"
	"testing"
)

// number_units_test.go covers the editor.number_hint_units element check
// (#2008): the form has to reject what the installer would silently skip.

func TestNumberUnitValidate(t *testing.T) {
	for _, ok := range []string{"request_timeout=s", "*_bytes=bytes", "created_at=timestamp-s", "session_id=none"} {
		if msg := numberUnitValidate(nil, ok); msg != "" {
			t.Errorf("numberUnitValidate(%q) = %q; want it accepted", ok, msg)
		}
	}
	for _, bad := range []string{"", "  ", "request_timeout", "request_timeout: s", "=bytes", "ttl=parsecs"} {
		if msg := numberUnitValidate(nil, bad); msg == "" {
			t.Errorf("numberUnitValidate(%q) accepted an entry the mapping would skip", bad)
		}
	}
}

// The hints name the shape before the "=" is typed and the matching units
// after it, so the vocabulary never has to be recalled from the description.
func TestNumberUnitHints(t *testing.T) {
	if h := numberUnitHints(nil, "request_timeout"); len(h) != 1 || !strings.Contains(h[0], "pattern=unit") {
		t.Errorf("numberUnitHints(\"request_timeout\") = %v; want the entry shape", h)
	}
	h := numberUnitHints(nil, "created_at=time")
	if len(h) != 2 || h[0] != "timestamp-s" || h[1] != "timestamp-ms" {
		t.Errorf("numberUnitHints(\"created_at=time\") = %v; want the timestamp units", h)
	}
	if h := numberUnitHints(nil, "ttl=parsecs"); len(h) < 10 {
		t.Errorf("numberUnitHints with an unknown unit = %v; want the whole vocabulary", h)
	}
}
