package permhint

import "testing"

// TestDescribe covers the octal→symbolic decoding: the plain three-digit forms,
// the write-style variants (leading zero, 0o prefix) and the four-digit special
// bits — including the capital S/T forms, where the special bit is set without
// the execute bit under it.
func TestDescribe(t *testing.T) {
	cases := []struct{ in, want string }{
		{"755", "rwxr-xr-x"},
		{"644", "rw-r--r--"},
		{"0644", "rw-r--r--"},
		{"0o755", "rwxr-xr-x"},
		{"0O600", "rw-------"},
		{"777", "rwxrwxrwx"},
		{"000", "---------"},
		{"400", "r--------"},
		{"0000", "---------"},
		// Special bits.
		{"4755", "rwsr-xr-x"},
		{"2775", "rwxrwsr-x"},
		{"1777", "rwxrwxrwt"},
		{"6755", "rwsr-sr-x"},
		{"7777", "rwsrwsrwt"},
		// The special bit without the execute bit under it: ls prints the
		// capital form.
		{"4644", "rwSr--r--"},
		{"2644", "rw-r-Sr--"},
		{"1666", "rw-rw-rwT"},
	}
	for _, c := range cases {
		got, ok := Describe(c.in)
		if !ok {
			t.Errorf("Describe(%q): not decoded", c.in)
			continue
		}
		if got != c.want {
			t.Errorf("Describe(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

// TestDescribeRejects: anything that is not three or four octal digits gets no
// hint — a wrong reading is worse than none.
func TestDescribeRejects(t *testing.T) {
	for _, in := range []string{
		"",
		"7",
		"75",
		"75555",
		"0o75555",
		"788",   // 8 is not an octal digit
		"8080",  // a port
		"2024x", // not digits at all
		"0x644", // hex
		"-755",
		"u+x",
		"rwxr-xr-x",
	} {
		if got, ok := Describe(in); ok {
			t.Errorf("Describe(%q) = %q, want no hint", in, got)
		}
	}
}

// TestHasOctalPrefix: the code and YAML contexts only decode a literal written
// as octal, so a decimal `644` never reads as a permission there.
func TestHasOctalPrefix(t *testing.T) {
	for _, in := range []string{"0644", "0755", "0o644", "02775", " 0644"} {
		if !HasOctalPrefix(in) {
			t.Errorf("HasOctalPrefix(%q) = false, want true", in)
		}
	}
	for _, in := range []string{"644", "755", "2775", "0", "07", ""} {
		if HasOctalPrefix(in) {
			t.Errorf("HasOctalPrefix(%q) = true, want false", in)
		}
	}
}
