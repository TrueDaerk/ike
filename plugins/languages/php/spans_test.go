package langphp

import (
	"testing"

	"ike/internal/lang"
	"ike/internal/numhint"
)

// TestPHPConstantSpans (#1701): `const` and `define()` declarations whose
// right-hand side is pure literal arithmetic evaluate and conceal in the unit
// the constant's name carries; identifier arithmetic stays raw.
func TestPHPConstantSpans(t *testing.T) {
	l, ok := lang.ByPath("/p/config.php")
	if !ok || l.Spans == nil {
		t.Fatal("php: no Spans producer registered")
	}
	spans := l.Spans([]string{
		"const MAX_BYTES = 10 * 1024 * 1024;",
		"define('CACHE_TTL', 60 * 60 * 24);",
		"const DERIVED = self::BASE * 2;",
	})
	if len(spans) != 2 {
		t.Fatalf("spans = %+v, want two conceals", spans)
	}
	if spans[0].Capture != numhint.SizeCapture || spans[0].Replace != "10 MiB" {
		t.Errorf("const span = %+v, want a 10 MiB byte-size conceal", spans[0])
	}
	if spans[1].Capture != numhint.DurationCapture || spans[1].Replace != "24h" {
		t.Errorf("define span = %+v, want a 24h duration conceal", spans[1])
	}
}
