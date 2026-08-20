package consthint

import (
	"testing"

	"ike/internal/lang"
	"ike/internal/numhint"
)

// units_test.go covers the field-unit mapping over code positions (#2008): the
// mapped unit is the base the raw number is read in, and a keyword argument on
// a call line reads exactly like the plain assignment beside it.

// mapUnits installs a field-unit mapping for the duration of one test.
func mapUnits(t *testing.T, entries ...string) {
	t.Helper()
	numhint.SetFieldUnits(entries)
	t.Cleanup(func() { numhint.SetFieldUnits(nil) })
}

// only asserts a one-line buffer produced exactly one span and returns it.
func only(t *testing.T, name string, spans []lang.Span) lang.Span {
	t.Helper()
	if len(spans) != 1 {
		t.Fatalf("%s produced %d spans, want 1: %+v", name, len(spans), spans)
	}
	return spans[0]
}

// TestMappedUnitBaseInCode: with `request_timeout=s` the value 1500 is 1500
// seconds wherever the name carries it — the Python kwarg of a call line, a
// def default, a plain assignment, a CONST_CASE constant, and the Go and PHP
// shapes — never the milliseconds the built-in `timeout` word would give it.
func TestMappedUnitBaseInCode(t *testing.T) {
	mapUnits(t, "request_timeout=s")
	cases := []struct {
		name  string
		spans []lang.Span
	}{
		{"python kwarg", PythonSpans([]string{`elastic.options(max_retries=3, request_timeout=1500).search(q)`})},
		{"python kwarg, multi-line call", PythonSpans([]string{
			"elastic.options(", "    max_retries=3,", "    request_timeout=1500,", ").search(q)",
		})},
		{"python def default", PythonSpans([]string{`def search(request_timeout=1500):`})},
		{"python assignment", PythonSpans([]string{`request_timeout = 1500`})},
		{"python constant", PythonSpans([]string{`REQUEST_TIMEOUT = 1500`})},
		{"python computed constant", PythonSpans([]string{`REQUEST_TIMEOUT = 15 * 100`})},
		{"python computed kwarg", PythonSpans([]string{`f(request_timeout=15 * 100)`})},
		{"go constant", GoSpans([]string{`const requestTimeout = 1500`})},
		{"php constant", PHPSpans([]string{`const REQUEST_TIMEOUT = 1500;`})},
		{"php named argument", PHPSpans([]string{`$c->options(request_timeout: 1500);`})},
		{"php variable", PHPSpans([]string{`$request_timeout = 1500;`})},
	}
	for _, c := range cases {
		s := only(t, c.name, c.spans)
		if s.Capture != numhint.DurationCapture || s.Replace != "25m" {
			t.Errorf("%s = %s %q; want %s \"25m\"", c.name, s.Capture, s.Replace, numhint.DurationCapture)
		}
	}
}

// TestMappedUnitsInKwargs: every mapped base renders the same 1500 its own
// way in a keyword argument, and the plain assignment agrees with it — the two
// shapes go through different producers (#1761's kwarg scan, the assignment
// shapes) and must not read a value differently.
func TestMappedUnitsInKwargs(t *testing.T) {
	cases := []struct {
		unit             string
		capture, replace string
	}{
		{"s", numhint.DurationCapture, "25m"},
		{"ms", numhint.DurationCapture, "1s500ms"},
		{"min", numhint.DurationCapture, "25h"},
		{"bytes", numhint.SizeCapture, "1.5 KiB"},
		{"hex", numhint.RadixCapture, "1500" + numhint.Gap + "= 0x5DC"},
		{"none", "", ""},
	}
	for _, c := range cases {
		mapUnits(t, "request_timeout="+c.unit)
		kwarg := PythonSpans([]string{`f(request_timeout=1500)`})
		assign := PythonSpans([]string{`request_timeout = 1500`})
		if c.capture == "" {
			if len(kwarg) != 0 || len(assign) != 0 {
				t.Errorf("=%s concealed %+v / %+v; want the mapping to silence both", c.unit, kwarg, assign)
			}
			continue
		}
		k := only(t, "kwarg ="+c.unit, kwarg)
		a := only(t, "assignment ="+c.unit, assign)
		if k.Capture != c.capture || k.Replace != c.replace {
			t.Errorf("kwarg =%s = %s %q; want %s %q", c.unit, k.Capture, k.Replace, c.capture, c.replace)
		}
		if a.Capture != k.Capture || a.Replace != k.Replace {
			t.Errorf("=%s: assignment %s %q disagrees with kwarg %s %q",
				c.unit, a.Capture, a.Replace, k.Capture, k.Replace)
		}
	}
}
