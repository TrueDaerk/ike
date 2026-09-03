package ui

import "testing"

// TestHumanSize covers the byte-count formatter shared by the archive/remote
// browsers' size columns and the image preview's metadata line.
func TestHumanSize(t *testing.T) {
	cases := []struct {
		n    int64
		want string
	}{
		{0, "0 B"},
		{1, "1 B"},
		{1023, "1023 B"},
		{1024, "1.0 KB"},
		{1536, "1.5 KB"},
		{1 << 20, "1.0 MB"},
		{3 * (1 << 20), "3.0 MB"},
		{1 << 30, "1.0 GB"},
		{2 * (1 << 30), "2.0 GB"},
	}
	for _, c := range cases {
		if got := HumanSize(c.n); got != c.want {
			t.Errorf("HumanSize(%d) = %q, want %q", c.n, got, c.want)
		}
	}
}
