package permhint

import "testing"

// codecalls_test.go covers the PHP and JS/TS mode-API producers (#2345); the
// shared codeSpans machinery is exercised by the Go/Python cases in
// spans_test.go.

func TestPHPSpans(t *testing.T) {
	lines := []string{
		`chmod($path, 0755);`,
		`mkdir($dir, 0o750, true);`,
		`umask(0022);`,       // not a mode API
		`chmod($path, 493);`, // bare decimal: no octal marker, stays raw
	}
	spans := PHPSpans(lines)
	if len(spans) != 2 {
		t.Fatalf("got %d spans, want 2: %+v", len(spans), spans)
	}
	if spans[0].Line != 0 || spans[1].Line != 1 {
		t.Errorf("spans on lines %d/%d, want 0/1", spans[0].Line, spans[1].Line)
	}
}

func TestScriptSpans(t *testing.T) {
	lines := []string{
		`fs.chmod("/tmp/f", 0o755, cb);`,
		`await fs.promises.mkdir(dir, { mode: 0o750 });`,
		`fs.chmodSync(p, 0o644);`,
		`fs.open(p, "r");`, // not a mode API
	}
	spans := ScriptSpans(lines)
	if len(spans) != 3 {
		t.Fatalf("got %d spans, want 3: %+v", len(spans), spans)
	}
	for i, want := range []int{0, 1, 2} {
		if spans[i].Line != want {
			t.Errorf("span %d on line %d, want %d", i, spans[i].Line, want)
		}
	}
}
