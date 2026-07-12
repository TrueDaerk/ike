package market

import "testing"

func TestParseVersion(t *testing.T) {
	cases := []struct {
		in   string
		want Version
		ok   bool
	}{
		{"1.2.3", Version{1, 2, 3}, true},
		{"v1.2.3", Version{1, 2, 3}, true},
		{"0.0.0", Version{0, 0, 0}, true},
		{"10.20.30", Version{10, 20, 30}, true},
		{"1.2", Version{}, false},
		{"1.2.3.4", Version{}, false},
		{"1.2.x", Version{}, false},
		{"1.02.3", Version{}, false}, // leading zero
		{"1.-2.3", Version{}, false},
		{"", Version{}, false},
		{"1.2.3-beta", Version{}, false},
	}
	for _, c := range cases {
		got, err := ParseVersion(c.in)
		if c.ok != (err == nil) {
			t.Errorf("ParseVersion(%q) err=%v, want ok=%v", c.in, err, c.ok)
			continue
		}
		if c.ok && got != c.want {
			t.Errorf("ParseVersion(%q) = %v, want %v", c.in, got, c.want)
		}
	}
}

func TestVersionCompare(t *testing.T) {
	cases := []struct {
		a, b string
		want int
	}{
		{"1.0.0", "1.0.0", 0},
		{"1.0.1", "1.0.0", 1},
		{"1.0.0", "1.0.1", -1},
		{"1.1.0", "1.0.9", 1},
		{"2.0.0", "1.9.9", 1},
		{"0.9.9", "1.0.0", -1},
	}
	for _, c := range cases {
		a, _ := ParseVersion(c.a)
		b, _ := ParseVersion(c.b)
		if got := a.Compare(b); got != c.want {
			t.Errorf("Compare(%s, %s) = %d, want %d", c.a, c.b, got, c.want)
		}
	}
}

func TestVersionString(t *testing.T) {
	v, _ := ParseVersion("v1.2.3")
	if v.String() != "1.2.3" {
		t.Errorf("String() = %q, want %q", v.String(), "1.2.3")
	}
}
