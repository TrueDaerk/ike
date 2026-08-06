package logline

import "testing"

func kinds(pairs []Pair) []PairKind {
	out := make([]PairKind, len(pairs))
	for i, p := range pairs {
		out[i] = p.Kind
	}
	return out
}

// TestScanPairsDockerLogfmt is the #1633 example: a containerd RFC3339Nano
// prefix followed by logrus logfmt output, quoted message included.
func TestScanPairsDockerLogfmt(t *testing.T) {
	line := `2026-08-06T19:06:13.303770698Z time="2026-08-06T19:06:13Z" level=info msg="Session done" Failed=0 notify=no`
	pairs := ScanPairs(line)
	if !Logfmt(pairs) {
		t.Fatal("line must be recognized as logfmt")
	}
	wantKeys := []string{"time=", "level=", "msg=", "Failed=", "notify="}
	if len(pairs) != len(wantKeys) {
		t.Fatalf("got %d pairs, want %d: %+v", len(pairs), len(wantKeys), pairs)
	}
	runes := []rune(line)
	for i, want := range wantKeys {
		if got := string(runes[pairs[i].Key.Start:pairs[i].Key.End]); got != want {
			t.Errorf("pair %d key = %q, want %q", i, got, want)
		}
	}
	wantVals := []string{"2026-08-06T19:06:13Z", "info", "Session done", "0", "no"}
	for i, want := range wantVals {
		if pairs[i].Text != want {
			t.Errorf("pair %d value = %q, want %q", i, pairs[i].Text, want)
		}
		if got := string(runes[pairs[i].Value.Start:pairs[i].Value.End]); got != want {
			t.Errorf("pair %d value range covers %q, want %q", i, got, want)
		}
	}
	want := []PairKind{KindTime, KindLevel, KindMessage, KindPlain, KindPlain}
	for i, k := range kinds(pairs) {
		if k != want[i] {
			t.Errorf("pair %d kind = %v, want %v", i, k, want[i])
		}
	}
}

// TestScanPairsQuotedValueKeepsSpaces: a space inside a quoted value must not
// split the pair, and must not start a bogus key either.
func TestScanPairsQuotedValueKeepsSpaces(t *testing.T) {
	pairs := ScanPairs(`msg="a b=c d" next=1`)
	if len(pairs) != 2 {
		t.Fatalf("got %d pairs, want 2: %+v", len(pairs), pairs)
	}
	if pairs[0].Text != "a b=c d" {
		t.Errorf("quoted value = %q", pairs[0].Text)
	}
	if pairs[1].Text != "1" {
		t.Errorf("second value = %q", pairs[1].Text)
	}
}

// TestScanPairsEscapedQuote: a backslash-escaped quote does not close the run.
func TestScanPairsEscapedQuote(t *testing.T) {
	pairs := ScanPairs(`msg="say \"hi\" now" n=2`)
	if len(pairs) != 2 || pairs[0].Text != `say \"hi\" now` {
		t.Fatalf("pairs = %+v", pairs)
	}
}

// TestScanPairsUnterminatedQuote: a value whose quote never closes runs to the
// end of the line instead of derailing the scan.
func TestScanPairsUnterminatedQuote(t *testing.T) {
	pairs := ScanPairs(`msg="never closed`)
	if len(pairs) != 1 || pairs[0].Text != `"never closed` {
		t.Fatalf("pairs = %+v", pairs)
	}
}

// TestScanPairsNonPairWords: prose words and a bare "=" are skipped.
func TestScanPairsNonPairWords(t *testing.T) {
	if pairs := ScanPairs("nothing to see here"); len(pairs) != 0 {
		t.Errorf("prose yields pairs: %+v", pairs)
	}
	if pairs := ScanPairs("= =x 1=2"); len(pairs) != 0 {
		t.Errorf("invalid keys yield pairs: %+v", pairs)
	}
	if pairs := ScanPairs("key="); len(pairs) != 1 || !pairs[0].Value.Empty() {
		t.Errorf("empty value = %+v", pairs)
	}
}

// TestLogfmtGuard: a lone unknown pair inside a sentence is not enough — two
// pairs, or one recognized key, mark a line as logfmt.
func TestLogfmtGuard(t *testing.T) {
	for _, tc := range []struct {
		line string
		want bool
	}{
		{"solving for x=42 today", false},
		{"x=42 y=7", true},
		{"msg=hello", true},
		{"plain sentence", false},
	} {
		if got := Logfmt(ScanPairs(tc.line)); got != tc.want {
			t.Errorf("Logfmt(%q) = %v, want %v", tc.line, got, tc.want)
		}
	}
}

// TestScanPairsKindCasing: keys are matched case-insensitively.
func TestScanPairsKindCasing(t *testing.T) {
	pairs := ScanPairs(`TIME=1 Msg=hi`)
	if len(pairs) != 2 || pairs[0].Kind != KindTime || pairs[1].Kind != KindMessage {
		t.Fatalf("pairs = %+v", pairs)
	}
	if pairs[0].Key != (Range{0, 5}) {
		t.Errorf("key range = %+v, want the key plus '='", pairs[0].Key)
	}
}
