// Package netlink is the network counterpart of the ike:// deep-link socket
// (#2519): a TCP endpoint other devices can reach, speaking a small
// newline-delimited JSON protocol that triggers the same actions as an
// ike:// URL — switch project, open file:line, show a tool window.
//
// A client has to pair once before it may send commands. Pairing is a
// one-time code IKE shows in a popup: six glyphs, each a card suit (♠ ♥ ♣ ♦)
// in one of four colours (red, black, blue, green), sixteen possibilities per
// position. A wrong guess regenerates the code, and every code expires after
// a short window. A successful pairing hands the client a long-lived token it
// sends with every later request; tokens live hashed in the user state dir.
//
// The package is a pure leaf — no bubbletea, no app imports — so the code
// alphabet, the pairing state machine, the token store and the wire protocol
// are fully unit-testable. It reuses internal/deeplink for the URL grammar
// and hands every accepted link to the app through a callback; the app then
// runs the very same resolution pipeline an OS-delivered click goes through.
package netlink

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/json"
	"fmt"
	"strings"
)

// CodeLength is the number of glyphs in a pairing code.
const CodeLength = 6

// Suit is one of the four card suits a glyph can show.
type Suit struct {
	ID    string `json:"id"`
	Glyph string `json:"glyph"`
	Name  string `json:"name"`
}

// Colour is one of the four colours a glyph can be drawn in. Hex is the
// suggested rendering colour for a client UI; the terminal popup uses the
// same values so both sides show the same thing.
type Colour struct {
	ID   string `json:"id"`
	Hex  string `json:"hex"`
	Name string `json:"name"`
}

// Suits is the fixed suit alphabet, in wire order.
var Suits = []Suit{
	{ID: "spade", Glyph: "♠", Name: "Spade"},
	{ID: "heart", Glyph: "♥", Name: "Heart"},
	{ID: "club", Glyph: "♣", Name: "Club"},
	{ID: "diamond", Glyph: "♦", Name: "Diamond"},
}

// Colours is the fixed colour alphabet, in wire order.
var Colours = []Colour{
	{ID: "red", Hex: "#d62828", Name: "Red"},
	{ID: "black", Hex: "#111111", Name: "Black"},
	{ID: "blue", Hex: "#1d6fd6", Name: "Blue"},
	{ID: "green", Hex: "#2a9d3a", Name: "Green"},
}

// Glyph is one position of a code: a suit in a colour, both by ID.
type Glyph struct {
	Suit   string `json:"suit"`
	Colour string `json:"color"`
}

// Code is a full pairing code.
type Code [CodeLength]Glyph

// Generate draws a fresh code from crypto/rand. Every position is an
// independent uniform pick out of the 16 suit×colour combinations.
func Generate() Code {
	var buf [CodeLength]byte
	if _, err := rand.Read(buf[:]); err != nil {
		panic("netlink: crypto/rand unavailable: " + err.Error())
	}
	var c Code
	for i, b := range buf {
		c[i] = Glyph{Suit: Suits[b&3].ID, Colour: Colours[(b>>2)&3].ID}
	}
	return c
}

// Equal compares two codes in constant time, so a guess leaks no timing
// information about how many leading positions were right.
func (c Code) Equal(o Code) bool {
	return subtle.ConstantTimeCompare([]byte(c.String()), []byte(o.String())) == 1
}

// String renders the code as "spade:red heart:black …" — the canonical
// text form used for comparison and in logs.
func (c Code) String() string {
	parts := make([]string, CodeLength)
	for i, g := range c {
		parts[i] = g.Suit + ":" + g.Colour
	}
	return strings.Join(parts, " ")
}

// suitByID resolves a suit by ID or glyph; ok is false for anything else.
func suitByID(s string) (Suit, bool) {
	s = strings.ToLower(strings.TrimSpace(s))
	for _, x := range Suits {
		if s == x.ID || s == x.Glyph || s == strings.ToLower(x.Name) {
			return x, true
		}
	}
	return Suit{}, false
}

// colourByID resolves a colour by ID or hex; ok is false for anything else.
func colourByID(s string) (Colour, bool) {
	s = strings.ToLower(strings.TrimSpace(s))
	for _, x := range Colours {
		if s == x.ID || s == x.Hex || s == strings.ToLower(x.Name) {
			return x, true
		}
	}
	return Colour{}, false
}

// ParseCode validates a code as it arrived on the wire: exactly CodeLength
// glyphs, every suit and colour from the alphabet (IDs, glyphs and hex values
// are all accepted, so a client may echo back whatever the challenge listed).
func ParseCode(raw []Glyph) (Code, error) {
	if len(raw) != CodeLength {
		return Code{}, fmt.Errorf("a code has exactly %d glyphs, got %d", CodeLength, len(raw))
	}
	var c Code
	for i, g := range raw {
		s, ok := suitByID(g.Suit)
		if !ok {
			return Code{}, fmt.Errorf("glyph %d: unknown suit %q", i+1, g.Suit)
		}
		col, ok := colourByID(g.Colour)
		if !ok {
			return Code{}, fmt.Errorf("glyph %d: unknown color %q", i+1, g.Colour)
		}
		c[i] = Glyph{Suit: s.ID, Colour: col.ID}
	}
	return c, nil
}

// ParseCodeText parses the compact text form "spade:red heart:black …" (any
// whitespace or comma separated, suits may be glyphs) — the shape a human
// types into a minimal client such as `nc`.
func ParseCodeText(text string) (Code, error) {
	fields := strings.FieldsFunc(text, func(r rune) bool { return r == ' ' || r == ',' || r == '\t' })
	raw := make([]Glyph, 0, len(fields))
	for _, f := range fields {
		suit, colour, ok := strings.Cut(f, ":")
		if !ok {
			return Code{}, fmt.Errorf("glyph %q: expected suit:color", f)
		}
		raw = append(raw, Glyph{Suit: suit, Colour: colour})
	}
	return ParseCode(raw)
}

// Alphabet is the challenge's description of the code space, sent to the
// client so it can build an input UI without hard-coding anything.
type Alphabet struct {
	Length  int      `json:"length"`
	Suits   []Suit   `json:"suits"`
	Colours []Colour `json:"colors"`
}

// DefaultAlphabet returns the alphabet every challenge carries.
func DefaultAlphabet() Alphabet {
	return Alphabet{Length: CodeLength, Suits: Suits, Colours: Colours}
}

// MarshalJSON renders a code as its glyph array — the same shape the client
// sends back.
func (c Code) MarshalJSON() ([]byte, error) { return json.Marshal(c[:]) }
