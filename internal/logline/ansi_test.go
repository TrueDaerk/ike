package logline

import "testing"

func TestScanSGRColorRun(t *testing.T) {
	// red "err", reset, plain tail
	escapes, runs := ScanSGR("\x1b[31merr\x1b[0m ok")
	if len(escapes) != 2 || escapes[0] != (Escape{0, 5}) || escapes[1] != (Escape{8, 12}) {
		t.Fatalf("escapes = %+v", escapes)
	}
	if len(runs) != 1 || runs[0].Start != 5 || runs[0].End != 8 || runs[0].Style.Fg != "1" {
		t.Fatalf("runs = %+v", runs)
	}
}

func TestScanSGRAttributesAccumulate(t *testing.T) {
	_, runs := ScanSGR("\x1b[1m\x1b[33mwarn\x1b[m")
	if len(runs) != 1 {
		t.Fatalf("runs = %+v", runs)
	}
	if st := runs[0].Style; !st.Bold || st.Fg != "3" {
		t.Errorf("style = %+v", st)
	}
}

func TestScanSGRExtendedColors(t *testing.T) {
	_, runs := ScanSGR("\x1b[38;5;196mx\x1b[0m\x1b[38;2;0;128;255my\x1b[0m")
	if len(runs) != 2 {
		t.Fatalf("runs = %+v", runs)
	}
	if runs[0].Style.Fg != "196" {
		t.Errorf("indexed fg = %q", runs[0].Style.Fg)
	}
	if runs[1].Style.Fg != "#0080ff" {
		t.Errorf("truecolor fg = %q", runs[1].Style.Fg)
	}
}

func TestScanSGRBrightAndBackground(t *testing.T) {
	_, runs := ScanSGR("\x1b[91;42mx\x1b[0m")
	if len(runs) != 1 || runs[0].Style.Fg != "9" || runs[0].Style.Bg != "2" {
		t.Fatalf("runs = %+v", runs)
	}
}

// TestScanSGRNonSGRConcealed: a cursor-movement CSI conceals like any escape
// but changes no style.
func TestScanSGRNonSGRConcealed(t *testing.T) {
	escapes, runs := ScanSGR("\x1b[2Ktext")
	if len(escapes) != 1 || escapes[0] != (Escape{0, 4}) {
		t.Fatalf("escapes = %+v", escapes)
	}
	if len(runs) != 0 {
		t.Errorf("styleless text must produce no runs: %+v", runs)
	}
}

func TestScanSGRUnterminated(t *testing.T) {
	escapes, _ := ScanSGR("tail\x1b[31")
	if len(escapes) != 0 {
		t.Errorf("unterminated sequence must stay raw: %+v", escapes)
	}
}

func TestSpecRoundTrip(t *testing.T) {
	s := SGRStyle{Fg: "9", Bg: "#102030", Bold: true, Underline: true}
	got, ok := ParseSpec(s.Spec())
	if !ok || got != s {
		t.Errorf("round trip = %+v (%v), want %+v", got, ok, s)
	}
	if _, ok := ParseSpec(""); ok {
		t.Error("empty spec must not parse")
	}
	if _, ok := ParseSpec("bogus"); ok {
		t.Error("unknown part must not parse")
	}
}
