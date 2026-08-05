package diag

import (
	"strings"
	"testing"
)

func TestMemSummaryShape(t *testing.T) {
	s := MemSummary()
	for _, want := range []string{"heap ", "in use", "goroutines"} {
		if !strings.Contains(s, want) {
			t.Fatalf("summary %q missing %q", s, want)
		}
	}
}

func TestFmtBytes(t *testing.T) {
	cases := map[uint64]string{
		512:     "0 KiB",
		4 << 10: "4 KiB",
		3 << 20: "3.0 MiB",
		2 << 30: "2.0 GiB",
	}
	for in, want := range cases {
		if got := fmtBytes(in); got != want {
			t.Fatalf("fmtBytes(%d) = %q, want %q", in, got, want)
		}
	}
}
