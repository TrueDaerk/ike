package keymap

import (
	"sort"
	"strings"
)

// discover.go is the discoverability layer of Roadmap 0081/40: honest,
// live binding labels for the cheatsheet and the palette's shortcut column,
// plus which-key continuations for a held prefix. Everything reads the
// effective BindingTable — never a static list — through LiveBindings, whose
// pointer long-lived consumers keep across config reloads.

// LiveBindings holds the current binding table behind a stable pointer:
// the palette's command mode and the help sheet are built once but must
// follow every keymap reload.
type LiveBindings struct {
	table *BindingTable
}

// Set installs the freshly built table (called wherever buildKeymap runs).
func (l *LiveBindings) Set(t *BindingTable) { l.table = t }

// Table returns the current table (nil before the first Set).
func (l *LiveBindings) Table() *BindingTable { return l.table }

// Binding implements the help/palette BindingResolver with honest labelling:
//
//	blocked command      → "✗ blocked: <dependency>"
//	delivered chord      → "ctrl+s"
//	fragile only, with a delivered alternative in the table or the leader
//	layer                → "cmd+d ⚠ use <alternative>"
//	fragile only, no alternative → "cmd+d ⚠ terminal-dependent"
func (l *LiveBindings) Binding(id string) (string, bool) {
	if id == "" || l.table == nil {
		return "", false
	}
	if reason, blocked := BlockedReason(id); blocked {
		return "✗ blocked: " + reason, true
	}
	var deliveredChords, fragileChords []string
	for _, b := range l.table.Bindings() {
		if b.Command != id {
			continue
		}
		if Classify(b.Chord) == Delivered {
			deliveredChords = append(deliveredChords, b.Chord.String())
		} else {
			fragileChords = append(fragileChords, b.Chord.String())
		}
	}
	sort.Slice(deliveredChords, func(i, j int) bool {
		return shorterThen(deliveredChords[i], deliveredChords[j])
	})
	sort.Slice(fragileChords, func(i, j int) bool {
		return shorterThen(fragileChords[i], fragileChords[j])
	})
	if len(deliveredChords) > 0 {
		return deliveredChords[0], true
	}
	if len(fragileChords) == 0 {
		return "", false
	}
	label := fragileChords[0] + " ⚠ "
	if alt := leaderAlternative(id); alt != "" {
		return label + "use " + alt, true
	}
	return label + "terminal-dependent", true
}

// shorterThen orders chord labels fewest-steps-first, then short-first, then
// lexically, so single-step delivered chords beat leader sequences in the
// primary slot. Pure string length is not enough for the step rule: "space n"
// is shorter than "shift+f6" yet takes two keystrokes (#18). Steps are counted
// by the separating space in Chord.String's format; key bases never contain
// spaces.
func shorterThen(a, b string) bool {
	if sa, sb := strings.Count(a, " "), strings.Count(b, " "); sa != sb {
		return sa < sb
	}
	if len(a) != len(b) {
		return len(a) < len(b)
	}
	return a < b
}

// leaderAlternative names the leader path for a command covered by the
// mnemonic table ("space d"), "" otherwise.
func leaderAlternative(id string) string {
	for _, m := range leaderMnemonics {
		if m.command == id {
			return DefaultLeader + " " + m.key
		}
	}
	return ""
}

// Continuation is one which-key hint: the next key of a longer chord that
// starts with the held prefix, and what completing it runs.
type Continuation struct {
	Key     string
	Command string
	Title   string
}

// Continuations lists the possible next steps for a held prefix in the
// active context, deduplicated by key and sorted for stable rendering.
func (t *BindingTable) Continuations(prefix Chord, active Context) []Continuation {
	if t == nil || prefix.Len() == 0 {
		return nil
	}
	seen := map[string]bool{}
	var out []Continuation
	for _, b := range t.bindings {
		if b.Chord.Len() <= prefix.Len() || !b.Context.Matches(active) || !b.Chord.HasPrefix(prefix) {
			continue
		}
		next := b.Chord.Steps[prefix.Len()].String()
		if seen[next] {
			continue
		}
		seen[next] = true
		title := b.Title
		if title == "" {
			title = b.Command
		}
		out = append(out, Continuation{Key: next, Command: b.Command, Title: title})
	}
	sort.Slice(out, func(i, j int) bool {
		if ri, rj := keyRank(out[i].Key), keyRank(out[j].Key); ri != rj {
			return ri < rj
		}
		if len(out[i].Key) != len(out[j].Key) {
			return len(out[i].Key) < len(out[j].Key)
		}
		return out[i].Key < out[j].Key
	})
	return out
}

// keyRank orders which-key rows for scanability: letter mnemonics first,
// digits next, everything else (punctuation, modified keys) last.
func keyRank(key string) int {
	if len(key) == 1 {
		switch c := key[0]; {
		case c >= 'a' && c <= 'z':
			return 0
		case c >= '0' && c <= '9':
			return 1
		}
	}
	return 2
}

// PendingContinuations exposes the resolver's held prefix to the which-key
// overlay: the prefix string and its continuations (nil when not pending).
func (r *Resolver) PendingContinuations(active Context) (string, []Continuation) {
	if !r.Pending() {
		return "", nil
	}
	return r.pending.String(), r.table.Continuations(r.pending, active)
}

// FormatContinuations renders which-key rows ("f  Go to file") for the
// overlay, capped to keep the popup small.
func FormatContinuations(conts []Continuation, max int) []string {
	out := make([]string, 0, len(conts))
	for i, c := range conts {
		if i == max {
			out = append(out, "…")
			break
		}
		out = append(out, c.Key+"  "+c.Title)
	}
	return out
}
