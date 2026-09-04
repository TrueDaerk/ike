package netlink

import (
	"encoding/json"
	"strings"
	"testing"
)

// TestGenerateUsesWholeAlphabet: codes are CodeLength glyphs drawn from the
// suit×colour alphabet; over many draws every suit and colour shows up.
func TestGenerateUsesWholeAlphabet(t *testing.T) {
	suits, colours := map[string]bool{}, map[string]bool{}
	for i := 0; i < 200; i++ {
		c := Generate()
		for _, g := range c {
			if _, ok := suitByID(g.Suit); !ok {
				t.Fatalf("unknown suit %q", g.Suit)
			}
			if _, ok := colourByID(g.Colour); !ok {
				t.Fatalf("unknown colour %q", g.Colour)
			}
			suits[g.Suit], colours[g.Colour] = true, true
		}
	}
	if len(suits) != len(Suits) || len(colours) != len(Colours) {
		t.Fatalf("alphabet not fully used: suits=%v colours=%v", suits, colours)
	}
}

// TestParseCodeAcceptsIDsGlyphsAndHex: a client may echo back IDs, glyphs
// or hex values from the alphabet; everything normalises to IDs.
func TestParseCodeAcceptsIDsGlyphsAndHex(t *testing.T) {
	raw := []Glyph{
		{"spade", "red"}, {"♥", "#111111"}, {"Club", "Blue"},
		{"♦", "green"}, {"spade", "black"}, {"heart", "red"},
	}
	c, err := ParseCode(raw)
	if err != nil {
		t.Fatal(err)
	}
	want := "spade:red heart:black club:blue diamond:green spade:black heart:red"
	if c.String() != want {
		t.Fatalf("got %q, want %q", c.String(), want)
	}
	if c2, err := ParseCodeText("spade:red, ♥:black club:blue ♦:green spade:black heart:red"); err != nil || !c2.Equal(c) {
		t.Fatalf("text form mismatch: %v %q", err, c2.String())
	}
}

// TestParseCodeRejectsBadInput: wrong length, unknown suit or colour.
func TestParseCodeRejectsBadInput(t *testing.T) {
	if _, err := ParseCode(make([]Glyph, 5)); err == nil || !strings.Contains(err.Error(), "exactly 6") {
		t.Fatalf("length must be enforced, got %v", err)
	}
	raw := []Glyph{{"spade", "red"}, {"spade", "red"}, {"spade", "red"}, {"spade", "red"}, {"spade", "red"}, {"joker", "red"}}
	if _, err := ParseCode(raw); err == nil || !strings.Contains(err.Error(), "suit") {
		t.Fatalf("unknown suit must fail, got %v", err)
	}
	raw[5] = Glyph{"spade", "pink"}
	if _, err := ParseCode(raw); err == nil || !strings.Contains(err.Error(), "color") {
		t.Fatalf("unknown colour must fail, got %v", err)
	}
	if _, err := ParseCodeText("spade red"); err == nil {
		t.Fatal("text form without colon must fail")
	}
}

// TestCodeEqualAndJSON: Equal compares whole codes; the JSON form is the
// glyph array a client sends back.
func TestCodeEqualAndJSON(t *testing.T) {
	a := Generate()
	b := a
	if !a.Equal(b) {
		t.Fatal("identical codes must be equal")
	}
	b[0].Colour = nextColour(b[0].Colour)
	if a.Equal(b) {
		t.Fatal("a one-glyph difference must not be equal")
	}
	data, err := json.Marshal(a)
	if err != nil {
		t.Fatal(err)
	}
	var back []Glyph
	if err := json.Unmarshal(data, &back); err != nil {
		t.Fatal(err)
	}
	c, err := ParseCode(back)
	if err != nil || !c.Equal(a) {
		t.Fatalf("round trip lost the code: %v", err)
	}
}

// nextColour returns a colour different from id.
func nextColour(id string) string {
	for _, c := range Colours {
		if c.ID != id {
			return c.ID
		}
	}
	return id
}

// TestDefaultAlphabetLists: the challenge alphabet describes the full space.
func TestDefaultAlphabetLists(t *testing.T) {
	a := DefaultAlphabet()
	if a.Length != CodeLength || len(a.Suits) != 4 || len(a.Colours) != 4 {
		t.Fatalf("alphabet %+v", a)
	}
	for _, s := range a.Suits {
		if s.Glyph == "" || s.ID == "" {
			t.Fatalf("suit without glyph/id: %+v", s)
		}
	}
	for _, c := range a.Colours {
		if !strings.HasPrefix(c.Hex, "#") {
			t.Fatalf("colour without hex: %+v", c)
		}
	}
}
