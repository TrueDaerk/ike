package keymap

import (
	"fmt"
	"sort"
)

// BindingTable is the effective, platform-normalised binding set: defaults
// overlaid by config overrides, with same-chord+context conflicts resolved at
// build time. It indexes bindings by chord for resolution and prefix tests.
type BindingTable struct {
	bindings    []Binding
	conflicts   []Conflict
	shadows     []Shadow
	diagnostics []string
}

// BuildTable builds the effective table from a default set and a merged override
// map (binding key → command id; "" unbinds). Overrides arrive already merged
// by config precedence (defaults < user < project), so each non-empty entry is
// treated as a user/project layer that replaces matching default chords. Chords
// are normalised for goos once here. Unparseable override keys are skipped as
// diagnostics (the focus_* config stopgap lives in the same map and is ignored).
//
// A key is either a bare chord ("ctrl+s"), which applies in every context the
// defaults bind it in, or a context-qualified chord ("editor.ctrl+s", #1312),
// which touches that one context only — the config spelling behind "keep both,
// resolve by context". Unqualified keys are applied first and qualified ones
// after, so the narrower statement wins whatever the map order was.
func BuildTable(defaults []Binding, overrides map[string]string, goos string) *BindingTable {
	var diags []string
	// Start from normalised defaults.
	bindings := make([]Binding, 0, len(defaults))
	for _, b := range defaults {
		b.Chord = NormalizeChord(b.Chord, goos)
		bindings = append(bindings, b)
	}
	var bare, qualified []string
	for raw := range overrides {
		key, err := ParseOverrideKey(raw)
		if err != nil {
			diags = append(diags, fmt.Sprintf("keymap: ignoring override key %q: %v", raw, err))
			continue
		}
		if key.Qualified {
			qualified = append(qualified, raw)
		} else {
			bare = append(bare, raw)
		}
	}
	sort.Strings(bare)
	sort.Strings(qualified)
	for _, raw := range append(bare, qualified...) {
		key, _ := ParseOverrideKey(raw) // re-parse: the erroring keys are gone
		cmd := overrides[raw]
		chord := NormalizeChord(key.Chord, goos)
		cs := chord.String()
		// An unqualified key matches the chord in any context; a qualified one
		// matches only its own.
		matches := func(b Binding) bool {
			return b.Chord.String() == cs && (!key.Qualified || b.Context == key.Context)
		}
		if cmd == "" {
			// Unbind: drop the matching bindings.
			filtered := bindings[:0:0]
			for _, b := range bindings {
				if !matches(b) {
					filtered = append(filtered, b)
				}
			}
			bindings = filtered
			continue
		}
		// Rebind: replace the command on matching bindings (keeping their
		// context); if none exist, add a new override binding in the key's
		// context — Global for the unqualified form.
		replaced := false
		for i := range bindings {
			if matches(bindings[i]) {
				bindings[i].Command = cmd
				bindings[i].Layer = LayerUser
				replaced = true
			}
		}
		if !replaced {
			bindings = append(bindings, Binding{
				Chord:   chord,
				Command: cmd,
				Context: key.Context,
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
	// Cross-context shadowing (#1875): a pane-scoped binding hiding a Global
	// one with a different command is kept — layering is a feature — but never
	// silently.
	shadows := detectShadows(kept)
	for _, s := range shadows {
		diags = append(diags, s.String())
	}
	return &BindingTable{bindings: kept, conflicts: conflicts, shadows: shadows, diagnostics: diags}
}

// Bindings returns the effective bindings (post conflict resolution).
func (t *BindingTable) Bindings() []Binding { return t.bindings }

// Conflicts returns the detected build-time conflicts.
func (t *BindingTable) Conflicts() []Conflict { return t.conflicts }

// Shadows returns the detected cross-context shadows (#1875): pane-scoped
// bindings hiding a Global binding of a different command on the same chord,
// minus the allowlisted intentional default layerings.
func (t *BindingTable) Shadows() []Shadow { return t.shadows }

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
