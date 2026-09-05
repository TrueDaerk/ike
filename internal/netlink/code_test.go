package netlink

import (
	"encoding/json"
	"strings"
	"testing"
)

// TestGenerateUsesWholeAlphabet: codes are CodeLength digits out of 1-9,
// never 0; over many draws every digit shows up.
func TestGenerateUsesWholeAlphabet(t *testing.T) {
	seen := map[byte]bool{}
	for i := 0; i < 300; i++ {
		c := Generate()
		for _, d := range c {
			if !strings.ContainsRune(CodeDigits, rune(d)) {
				t.Fatalf("digit %q outside the alphabet in %s", d, c)
			}
			seen[d] = true
		}
		if len(c.String()) != CodeLength {
			t.Fatalf("code %q has the wrong length", c)
		}
	}
	if len(seen) != len(CodeDigits) {
		t.Fatalf("alphabet not fully used: %v", seen)
	}
}

// TestParseCodeAcceptsDigitsAndGrouping: the wire form is the bare digits;
// the display grouping (spaces, the middle dot) is tolerated on the way in.
func TestParseCodeAcceptsDigitsAndGrouping(t *testing.T) {
	c, err := ParseCode("481936")
	if err != nil || c.String() != "481936" {
		t.Fatalf("got %q err %v", c, err)
	}
	if c.Grouped() != "481 936" {
		t.Fatalf("grouped %q", c.Grouped())
	}
	for _, in := range []string{"481 936", "481·936", " 4 8 1 9 3 6 ", "481-936"} {
		if g, err := ParseCode(in); err != nil || !g.Equal(c) {
			t.Fatalf("%q: got %q err %v", in, g, err)
		}
	}
}

// TestParseCodeRejectsBadInput: wrong length, zero, letters.
func TestParseCodeRejectsBadInput(t *testing.T) {
	if _, err := ParseCode("48193"); err == nil || !strings.Contains(err.Error(), "exactly 6") {
		t.Fatalf("length must be enforced, got %v", err)
	}
	if _, err := ParseCode("4819366"); err == nil || !strings.Contains(err.Error(), "exactly 6") {
		t.Fatalf("length must be enforced, got %v", err)
	}
	if _, err := ParseCode("481930"); err == nil || !strings.Contains(err.Error(), "1-9") {
		t.Fatalf("zero must fail, got %v", err)
	}
	if _, err := ParseCode("48a936"); err == nil {
		t.Fatal("letters must fail")
	}
	if _, err := ParseCode(""); err == nil {
		t.Fatal("empty must fail")
	}
}

// TestCodeEqualAndJSON: Equal compares whole codes; the JSON form is the
// digit string a client sends back.
func TestCodeEqualAndJSON(t *testing.T) {
	a := Generate()
	b := a
	if !a.Equal(b) {
		t.Fatal("identical codes must be equal")
	}
	b[0] = nextDigit(b[0])
	if a.Equal(b) {
		t.Fatal("a one-digit difference must not be equal")
	}
	data, err := json.Marshal(a)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != `"`+a.String()+`"` {
		t.Fatalf("json %s", data)
	}
	var back Code
	if err := json.Unmarshal(data, &back); err != nil || !back.Equal(a) {
		t.Fatalf("round trip lost the code: %v", err)
	}
	if err := json.Unmarshal([]byte(`"480936"`), &back); err == nil {
		t.Fatal("unmarshal must validate the digits")
	}
}

// nextDigit returns a digit different from d.
func nextDigit(d byte) byte {
	if d == '9' {
		return '1'
	}
	return d + 1
}

// TestDefaultAlphabetLists: the challenge alphabet describes the full space.
func TestDefaultAlphabetLists(t *testing.T) {
	a := DefaultAlphabet()
	if a.Digits != "123456789" {
		t.Fatalf("alphabet %+v", a)
	}
	if CodeKind != "pin" {
		t.Fatalf("kind %q", CodeKind)
	}
}
