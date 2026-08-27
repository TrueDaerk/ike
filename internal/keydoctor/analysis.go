package keydoctor

import (
	"sort"
	"strings"
	"unicode"

	"ike/internal/keymap"
)

// analysis.go is the dead-binding audit (#2161): the doctor's second job.
// Probing answers "which chords does this terminal deliver?" one keypress at
// a time; the audit turns that knowledge on the *active keymap* without
// asking the user to press anything — every bound chord is judged through
// keymap.TerminalEnv.Deliverability, and every chord that cannot arrive here
// gets a conflict-free alternative offered for one keypress of repair.

// maxSuggestions is how many alternatives a finding offers; the report cycles
// through them with tab.
const maxSuggestions = 3

// Finding is one binding that does not (or may not) reach the program in this
// setup, with the reason and the rebind offers.
type Finding struct {
	Binding keymap.Binding
	Class   keymap.Deliverability
	Reason  string
	// Suggestions are deliverable chords that collide with nothing in the
	// keymap, best first. Empty when the keymap leaves no room — the report
	// still lists the finding, honestly, with no offer.
	Suggestions []keymap.Chord
}

// Analyze audits the active binding table for env: one finding per
// non-deliverable chord+context, dead ones first, then by chord. Bindings
// arrive post-conflict-resolution (BindingTable.Bindings), so a chord+context
// pair appears once; duplicates are collapsed defensively anyway.
func Analyze(env keymap.TerminalEnv, bindings []keymap.Binding) []Finding {
	seen := map[string]bool{}
	var out []Finding
	for _, b := range bindings {
		if b.Chord.Len() == 0 {
			continue
		}
		key := b.Chord.String() + "\x00" + string(b.Context)
		if seen[key] {
			continue
		}
		seen[key] = true
		class, reason := env.Deliverability(b.Chord)
		if class == keymap.Live {
			continue
		}
		out = append(out, Finding{Binding: b, Class: class, Reason: reason})
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Class != out[j].Class {
			return out[i].Class > out[j].Class // Dead before AtRisk
		}
		if out[i].Binding.Chord.String() != out[j].Binding.Chord.String() {
			return out[i].Binding.Chord.String() < out[j].Binding.Chord.String()
		}
		return out[i].Binding.Context < out[j].Binding.Context
	})
	// Suggestions are computed in display order, so the dead bindings get
	// first pick, and every offer is claimed: two findings never propose the
	// same chord, which would make the second repair steal the first one's
	// key. A finding that comes up empty under the claims falls back to the
	// unclaimed offer — a shared suggestion beats no suggestion, and the
	// report re-audits after every applied rebind anyway.
	claimed := map[string]bool{}
	for i := range out {
		s := suggest(env, out[i].Binding, bindings, claimed, maxSuggestions)
		if len(s) == 0 {
			s = suggest(env, out[i].Binding, bindings, nil, maxSuggestions)
		}
		for _, c := range s {
			claimed[c.String()] = true
		}
		out[i].Suggestions = s
	}
	return out
}

// Suggest proposes up to limit replacement chords for a binding: deliverable
// in this setup, and free across the whole keymap. "Free" is deliberately
// context-blind — the rebind is written as an unqualified
// keymap.bindings.<chord> override, which claims the chord in every context,
// so a suggestion that is only free in the binding's own pane would silently
// steal another one's key.
func Suggest(env keymap.TerminalEnv, b keymap.Binding, all []keymap.Binding, limit int) []keymap.Chord {
	return suggest(env, b, all, nil, limit)
}

// suggest is Suggest with a claim set: chords another finding in the same
// audit already offers, which this one must not propose again.
func suggest(env keymap.TerminalEnv, b keymap.Binding, all []keymap.Binding, claimed map[string]bool, limit int) []keymap.Chord {
	var out []keymap.Chord
	seen := map[string]bool{b.Chord.String(): true}
	for _, cand := range candidateChords(b) {
		s := cand.String()
		if seen[s] || claimed[s] {
			continue
		}
		seen[s] = true
		if class, _ := env.Deliverability(cand); class != keymap.Live {
			continue
		}
		if occupied(cand, all) {
			continue
		}
		out = append(out, cand)
		if len(out) >= limit {
			break
		}
	}
	return out
}

// occupied reports whether a candidate chord collides with the keymap: an
// identical chord, a chord it would shadow as a prefix ("ctrl+k" when
// "ctrl+k c" exists — the shorter one swallows the sequence's start), or a
// chord that is a prefix of it (the existing binding would fire first).
func occupied(cand keymap.Chord, all []keymap.Binding) bool {
	for _, b := range all {
		if b.Chord.HasPrefix(cand) || cand.HasPrefix(b.Chord) {
			return true
		}
	}
	return false
}

// candidateChords generates replacement chords for a binding, best first:
// same base key under a delivered modifier (the smallest change to muscle
// memory), then mnemonic ctrl chords derived from the command id, then
// two-step sequences under a ctrl prefix — the escape hatch that always has
// room. Candidates are generated, not validated: Suggest drops the ones that
// are undeliverable or taken.
func candidateChords(b keymap.Binding) []keymap.Chord {
	if b.Chord.Len() == 0 {
		return nil
	}
	var out []keymap.Chord
	add := func(mods keymap.Mod, base string) {
		out = append(out, keymap.Chord{Steps: []keymap.Key{{Base: base, Mods: mods}}})
	}
	last := b.Chord.Steps[b.Chord.Len()-1]
	base := last.Base
	// Same key, different modifiers. Function keys keep their number (the
	// F-key row is what the user reaches for); character keys move onto ctrl.
	if isFunctionKey(base) {
		add(keymap.ModShift, base)
		add(keymap.ModCtrl|keymap.ModShift, base)
		add(0, base)
		add(keymap.ModAlt, base)
	} else {
		add(keymap.ModCtrl, base)
		add(keymap.ModCtrl|keymap.ModShift, base)
	}
	// Mnemonic ctrl chords from the command id ("editor.findUsages" → ctrl+f,
	// ctrl+u), so the replacement is still guessable.
	letters := mnemonicLetters(b.Command)
	for _, r := range letters {
		add(keymap.ModCtrl, string(r))
	}
	// Two-step sequences: a ctrl prefix plus a letter of the command — its
	// initials first, then any other letter it contains. The prefixes are
	// ctrl chords no default binds as a whole word, so there is almost always
	// room left even in a saturated keymap.
	letters = append(letters, otherLetters(b.Command, letters)...)
	if len(letters) == 0 {
		letters = []rune{'a'}
	}
	for _, prefix := range []string{"k", "x", "w"} {
		for _, r := range letters {
			out = append(out, keymap.Chord{Steps: []keymap.Key{
				{Base: prefix, Mods: keymap.ModCtrl},
				{Base: string(r)},
			}})
		}
	}
	return out
}

// mnemonicLetters derives the letters worth trying for a command id: the
// initial of every camel-case word of the last segment, then the initial of
// the leading segment ("editor.findUsages" → f, u, e).
func mnemonicLetters(command string) []rune {
	var out []rune
	seen := map[rune]bool{}
	push := func(r rune) {
		r = unicode.ToLower(r)
		if r >= 'a' && r <= 'z' && !seen[r] {
			seen[r] = true
			out = append(out, r)
		}
	}
	segs := strings.Split(command, ".")
	last := segs[len(segs)-1]
	prev := rune(0)
	for i, r := range last {
		if i == 0 || (unicode.IsUpper(r) && !unicode.IsUpper(prev)) {
			push(r)
		}
		prev = r
	}
	if len(segs) > 1 && segs[0] != "" {
		push(rune(segs[0][0]))
	}
	return out
}

// otherLetters returns the remaining distinct letters of a command id, in
// order — the deeper bench the two-step ladder draws from once the initials
// are taken.
func otherLetters(command string, have []rune) []rune {
	seen := map[rune]bool{}
	for _, r := range have {
		seen[r] = true
	}
	var out []rune
	for _, r := range strings.ToLower(command) {
		if r >= 'a' && r <= 'z' && !seen[r] {
			seen[r] = true
			out = append(out, r)
		}
	}
	return out
}

// isFunctionKey reports whether a base key is one of the fN keys. It mirrors
// the keymap package's own (unexported) test; the candidate generator needs
// the same question answered.
func isFunctionKey(base string) bool {
	if len(base) < 2 || base[0] != 'f' {
		return false
	}
	for _, c := range base[1:] {
		if c < '0' || c > '9' {
			return false
		}
	}
	return true
}
