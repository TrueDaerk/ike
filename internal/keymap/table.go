package keymap

import "fmt"

// BindingTable is the effective, platform-normalised binding set: defaults
// overlaid by config overrides, with same-chord+context conflicts resolved at
// build time. It indexes bindings by chord for resolution and prefix tests.
type BindingTable struct {
	bindings    []Binding
	conflicts   []Conflict
	diagnostics []string
}

// BuildTable builds the effective table from a default set and a merged override
// map (chord string → command id; "" unbinds). Overrides arrive already merged
// by config precedence (defaults < user < project), so each non-empty entry is
// treated as a user/project layer that replaces matching default chords. Chords
// are normalised for goos once here. Unparseable override keys are skipped as
// diagnostics (the focus_* config stopgap lives in the same map and is ignored).
func BuildTable(defaults []Binding, overrides map[string]string, goos string) *BindingTable {
	var diags []string
	// Start from normalised defaults.
	bindings := make([]Binding, 0, len(defaults))
	for _, b := range defaults {
		b.Chord = NormalizeChord(b.Chord, goos)
		bindings = append(bindings, b)
	}
	for raw, cmd := range overrides {
		chord, err := ParseChord(raw)
		if err != nil {
			diags = append(diags, fmt.Sprintf("keymap: ignoring override key %q: %v", raw, err))
			continue
		}
		chord = NormalizeChord(chord, goos)
		cs := chord.String()
		if cmd == "" {
			// Unbind: drop every binding with this chord, any context.
			filtered := bindings[:0:0]
			for _, b := range bindings {
				if b.Chord.String() != cs {
					filtered = append(filtered, b)
				}
			}
			bindings = filtered
			continue
		}
		// Rebind: replace the command on matching default chords (keeping their
		// context); if none exist, add a new Global override binding.
		replaced := false
		for i := range bindings {
			if bindings[i].Chord.String() == cs {
				bindings[i].Command = cmd
				bindings[i].Layer = LayerUser
				replaced = true
			}
		}
		if !replaced {
			bindings = append(bindings, Binding{
				Chord:   chord,
				Command: cmd,
				Context: Global,
				Title:   cmd,
				Owner:   "user",
				Layer:   LayerUser,
			})
		}
	}
	kept, conflicts := detectConflicts(bindings)
	for _, c := range conflicts {
		diags = append(diags, c.String())
	}
	return &BindingTable{bindings: kept, conflicts: conflicts, diagnostics: diags}
}

// Bindings returns the effective bindings (post conflict resolution).
func (t *BindingTable) Bindings() []Binding { return t.bindings }

// Conflicts returns the detected build-time conflicts.
func (t *BindingTable) Conflicts() []Conflict { return t.conflicts }

// Diagnostics returns non-fatal messages (ignored overrides, conflicts).
func (t *BindingTable) Diagnostics() []string { return t.diagnostics }

// Lookup returns the binding whose chord equals c and is active in the focus
// context active, preferring the most specific (pane-scoped over Global). It
// reports ok=false when no binding's chord matches in context.
func (t *BindingTable) Lookup(c Chord, active Context) (Binding, bool) {
	cs := c.String()
	var best Binding
	found := false
	for _, b := range t.bindings {
		if b.Chord.String() != cs || !b.Context.Matches(active) {
			continue
		}
		if !found || b.Context.MoreSpecific(best.Context) {
			best, found = b, true
		}
	}
	return best, found
}

// IsPrefix reports whether c is a strict prefix of some longer binding chord
// active in context — i.e. the resolver should keep waiting for more steps.
func (t *BindingTable) IsPrefix(c Chord, active Context) bool {
	for _, b := range t.bindings {
		if b.Chord.Len() > c.Len() && b.Context.Matches(active) && b.Chord.HasPrefix(c) {
			return true
		}
	}
	return false
}
