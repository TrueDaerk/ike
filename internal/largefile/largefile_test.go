package largefile

import "testing"

func get(m map[string]string) Getter {
	return func(k string) (string, bool) { v, ok := m[k]; return v, ok }
}

func TestLimitsFromDefaults(t *testing.T) {
	for _, g := range []Getter{nil, get(map[string]string{})} {
		l := LimitsFrom(g)
		if l.MaxBytes != DefaultMaxKB*1024 || l.MaxLines != DefaultMaxLines {
			t.Fatalf("defaults = %+v", l)
		}
	}
}

func TestLimitsFromConfig(t *testing.T) {
	l := LimitsFrom(get(map[string]string{
		"files.large_file_kb":    "2",
		"files.large_file_lines": "10",
	}))
	if l.MaxBytes != 2048 || l.MaxLines != 10 {
		t.Fatalf("limits = %+v", l)
	}
	// Malformed values keep the defaults.
	l = LimitsFrom(get(map[string]string{"files.large_file_kb": "abc"}))
	if l.MaxBytes != DefaultMaxKB*1024 {
		t.Fatalf("malformed kb = %+v", l)
	}
}

func TestExceeded(t *testing.T) {
	l := Limits{MaxBytes: 1024, MaxLines: 100}
	cases := []struct {
		bytes int64
		lines int
		want  bool
	}{
		{1024, 100, false}, // at the threshold: not over
		{1025, 1, true},    // bytes guard
		{1, 101, true},     // line guard
	}
	for _, c := range cases {
		if got := l.Exceeded(c.bytes, c.lines); got != c.want {
			t.Errorf("Exceeded(%d, %d) = %v, want %v", c.bytes, c.lines, got, c.want)
		}
	}
	// A disabled guard (<= 0) never fires.
	if (Limits{MaxBytes: 0, MaxLines: 0}).Exceeded(1 << 40, 1 << 30) {
		t.Fatal("disabled guards flagged a file")
	}
}

func TestOverride(t *testing.T) {
	defer Reset()
	if Forced("a.txt") {
		t.Fatal("fresh path already forced")
	}
	Force("a.txt")
	if !Forced("a.txt") {
		t.Fatal("Force did not stick")
	}
	// Relative and absolute spellings of the same file agree.
	if !Forced("./a.txt") {
		t.Fatal("canonicalization mismatch")
	}
	Reset()
	if Forced("a.txt") {
		t.Fatal("Reset did not clear")
	}
}
