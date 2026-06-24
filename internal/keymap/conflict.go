package keymap

import "fmt"

// Conflict records two or more bindings that map the same chord+context to
// different command ids. The table keeps the highest-precedence binding (by
// Layer, then stable order) and surfaces the rest as a diagnostic; it never
// silently shadows.
type Conflict struct {
	Chord   string
	Context Context
	Winner  Binding
	Losers  []Binding
}

// String renders a conflict for a diagnostic line.
func (c Conflict) String() string {
	s := fmt.Sprintf("keymap conflict: %q in context %q resolves to %q (%s)",
		c.Chord, contextLabel(c.Context), c.Winner.Command, c.Winner.Layer)
	for _, l := range c.Losers {
		s += fmt.Sprintf("; shadowed %q (%s)", l.Command, l.Layer)
	}
	return s
}

// detectConflicts groups bindings by chord+context and, where a group binds more
// than one distinct command, keeps the highest-Layer binding and reports the
// clash. Input order is preserved for ties so the result is deterministic.
func detectConflicts(bindings []Binding) (kept []Binding, conflicts []Conflict) {
	type slot struct {
		idx []int // indices into bindings, in input order
	}
	order := []string{}
	groups := map[string]*slot{}
	keyOf := func(b Binding) string {
		return b.Chord.String() + "\x00" + string(b.Context)
	}
	for i, b := range bindings {
		k := keyOf(b)
		g, ok := groups[k]
		if !ok {
			g = &slot{}
			groups[k] = g
			order = append(order, k)
		}
		g.idx = append(g.idx, i)
	}
	for _, k := range order {
		g := groups[k]
		// Pick the winner: highest Layer, earliest on tie.
		win := g.idx[0]
		for _, i := range g.idx[1:] {
			if bindings[i].Layer > bindings[win].Layer {
				win = i
			}
		}
		kept = append(kept, bindings[win])
		// A conflict exists only when some other binding names a different command.
		var losers []Binding
		for _, i := range g.idx {
			if i == win {
				continue
			}
			if bindings[i].Command != bindings[win].Command {
				losers = append(losers, bindings[i])
			}
		}
		if len(losers) > 0 {
			conflicts = append(conflicts, Conflict{
				Chord:   bindings[win].Chord.String(),
				Context: bindings[win].Context,
				Winner:  bindings[win],
				Losers:  losers,
			})
		}
	}
	return kept, conflicts
}

func contextLabel(c Context) string {
	if c == Global {
		return "global"
	}
	return string(c)
}
