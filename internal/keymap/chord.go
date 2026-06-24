package keymap

import "strings"

// Chord is an ordered sequence of key steps. It models all three binding shapes
// uniformly: a single key (esc), a modified key (cmd+t), and a multi-step
// JetBrains chord (cmd+k cmd+c).
type Chord struct {
	Steps []Key
}

// Len reports the number of steps in the chord.
func (c Chord) Len() int { return len(c.Steps) }

// String returns the canonical space-joined form ("cmd+k cmd+c").
func (c Chord) String() string {
	parts := make([]string, len(c.Steps))
	for i, k := range c.Steps {
		parts[i] = k.String()
	}
	return strings.Join(parts, " ")
}

// Equal reports whether two chords are identical step-for-step.
func (c Chord) Equal(o Chord) bool {
	if len(c.Steps) != len(o.Steps) {
		return false
	}
	for i := range c.Steps {
		if c.Steps[i] != o.Steps[i] {
			return false
		}
	}
	return true
}

// HasPrefix reports whether p is a prefix of c (p shorter or equal, every step
// matching). A chord is a prefix of itself. The resolver uses this to decide
// whether a partial sequence is still building toward a longer chord.
func (c Chord) HasPrefix(p Chord) bool {
	if len(p.Steps) > len(c.Steps) {
		return false
	}
	for i := range p.Steps {
		if c.Steps[i] != p.Steps[i] {
			return false
		}
	}
	return true
}
