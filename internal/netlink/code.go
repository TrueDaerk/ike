// Package netlink is the network counterpart of the ike:// deep-link socket
// (#2519): a TCP endpoint other devices can reach, speaking a small
// newline-delimited JSON protocol that triggers the same actions as an
// ike:// URL — switch project, open file:line, show a tool window.
//
// A client has to pair once before it may send commands. Pairing is a
// one-time PIN IKE shows in a popup: six digits, each drawn from 1 to 9 (no
// zero, so a 3×3 keypad is enough to enter it). A wrong guess regenerates
// the code, and every code expires after a short window. A successful
// pairing hands the client a long-lived token it sends with every later
// request; tokens live hashed in the user state dir.
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

// CodeLength is the number of digits in a pairing code.
const CodeLength = 6

// CodeDigits is the digit alphabet: 1 to 9, never 0, so a client can offer
// a plain 3×3 keypad.
const CodeDigits = "123456789"

// CodeKind names the code shape on the wire. The card-suit glyph code that
// preceded it ("glyph" alphabet with suits and colors) is deprecated and no
// longer issued.
const CodeKind = "pin"

// Code is a full pairing code: CodeLength ASCII digits out of CodeDigits.
type Code [CodeLength]byte

// Generate draws a fresh code from crypto/rand. Every position is an
// independent uniform pick out of the nine digits — rejection sampling keeps
// the distribution flat instead of folding 256 values onto 9.
func Generate() Code {
	var c Code
	var buf [64]byte
	i := 0
	for i < CodeLength {
		if _, err := rand.Read(buf[:]); err != nil {
			panic("netlink: crypto/rand unavailable: " + err.Error())
		}
		for _, b := range buf {
			if i == CodeLength {
				break
			}
			// 252 is the largest multiple of 9 below 256; values at or above
			// it would bias the low digits and are skipped.
			if b >= 252 {
				continue
			}
			c[i] = CodeDigits[b%9]
			i++
		}
	}
	return c
}

// Equal compares two codes in constant time, so a guess leaks no timing
// information about how many leading positions were right.
func (c Code) Equal(o Code) bool {
	return subtle.ConstantTimeCompare(c[:], o[:]) == 1
}

// String renders the code as its six digits, "481936" — the canonical text
// form used for comparison, on the wire and in logs.
func (c Code) String() string { return string(c[:]) }

// Grouped renders the code for a human, "481 936": the grouping is display
// only and never part of the wire form.
func (c Code) Grouped() string {
	s := c.String()
	return s[:CodeLength/2] + " " + s[CodeLength/2:]
}

// ParseCode validates a code as it arrived on the wire: exactly CodeLength
// characters, every one a digit from CodeDigits. Whitespace between digits
// (the display grouping) is tolerated, everything else is refused.
func ParseCode(text string) (Code, error) {
	var c Code
	n := 0
	for _, r := range text {
		switch {
		case r == ' ' || r == '\t' || r == '·' || r == '-':
			continue
		case strings.ContainsRune(CodeDigits, r):
			if n < CodeLength {
				c[n] = byte(r)
			}
			n++
		default:
			return Code{}, fmt.Errorf("a code holds only the digits 1-9, got %q", r)
		}
	}
	if n != CodeLength {
		return Code{}, fmt.Errorf("a code has exactly %d digits, got %d", CodeLength, n)
	}
	return c, nil
}

// Alphabet is the challenge's description of the code space, sent to the
// client so it can build an input UI without hard-coding anything.
type Alphabet struct {
	Digits string `json:"digits"`
}

// DefaultAlphabet returns the alphabet every challenge carries.
func DefaultAlphabet() Alphabet { return Alphabet{Digits: CodeDigits} }

// MarshalJSON renders a code as its digit string — the same shape the client
// sends back in "code".
func (c Code) MarshalJSON() ([]byte, error) { return json.Marshal(c.String()) }

// UnmarshalJSON accepts the digit string form.
func (c *Code) UnmarshalJSON(data []byte) error {
	var s string
	if err := json.Unmarshal(data, &s); err != nil {
		return err
	}
	parsed, err := ParseCode(s)
	if err != nil {
		return err
	}
	*c = parsed
	return nil
}
