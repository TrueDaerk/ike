package epochtime

import (
	"testing"
	"time"
)

// unit_test.go covers the reading the explain popover reports (#1998): the
// digit-count heuristic behind a decoded timestamp, seconds or milliseconds,
// and nothing at all for a run the range guard rejects.

func TestUnit(t *testing.T) {
	if u, ok := Unit("1722945600"); !ok || u != time.Second {
		t.Fatalf("seconds run = %v/%v", u, ok)
	}
	if u, ok := Unit("1722945600123"); !ok || u != time.Millisecond {
		t.Fatalf("millis run = %v/%v", u, ok)
	}
	for _, digits := range []string{"12345", "0", "0172294560", "999999999999999999"} {
		if _, ok := Unit(digits); ok {
			t.Fatalf("%q reported a timestamp unit", digits)
		}
	}
}
