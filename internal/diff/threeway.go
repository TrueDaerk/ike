package diff

// threeway.go is the three-way half of the diff engine (#1478): Compute3
// aligns ours and theirs against their merge base and classifies every
// changed region as ours-only, theirs-only, both-identical or conflicting;
// Merge3 folds that into a merged text where only true conflicts remain as
// diff3-style marker blocks (`<<<<<<<`/`|||||||`/`=======`/`>>>>>>>`), the
// format the editor's inline conflict resolution (#1149) already understands.

import "strings"

// Kind3 classifies one three-way chunk.
type Kind3 int

const (
	// Chunk3Same is a region no side changed.
	Chunk3Same Kind3 = iota
	// Chunk3Ours is a region only ours changed.
	Chunk3Ours
	// Chunk3Theirs is a region only theirs changed.
	Chunk3Theirs
	// Chunk3Both is a region both sides changed to the same text.
	Chunk3Both
	// Chunk3Conflict is a region both sides changed differently.
	Chunk3Conflict
)

// Chunk3 is one aligned region of a three-way comparison.
type Chunk3 struct {
	Kind   Kind3
	Base   []string
	Ours   []string
	Theirs []string
}

// Resolution returns the lines a chunk contributes to the merged result;
// nil (and ok=false) for a conflict, which has no automatic resolution.
func (c Chunk3) Resolution() (lines []string, ok bool) {
	switch c.Kind {
	case Chunk3Same:
		return c.Base, true
	case Chunk3Ours, Chunk3Both:
		return c.Ours, true
	case Chunk3Theirs:
		return c.Theirs, true
	}
	return nil, false
}

// Compute3 aligns ours and theirs against base (the classic diff3 walk over
// the two base-relative edit scripts) and returns the chunk sequence in
// document order. Stable regions — base lines matched unchanged by both
// sides — become Chunk3Same; each maximal unstable region between them is
// classified by comparing its three segments.
func Compute3(base, ours, theirs string) []Chunk3 {
	b := splitLines(base)
	o := splitLines(ours)
	t := splitLines(theirs)
	mo := baseMatches(script(b, o))
	mt := baseMatches(script(b, t))

	var chunks []Chunk3
	bi, oi, ti := 0, 0, 0
	for bi < len(b) || oi < len(o) || ti < len(t) {
		// Find the next base line at or after bi that both sides keep and
		// that lies at or after the current side cursors.
		s := bi
		for s < len(b) {
			so, okO := mo[s]
			st, okT := mt[s]
			if okO && okT && so >= oi && st >= ti {
				break
			}
			s++
		}
		if s >= len(b) {
			// Unstable tail.
			chunks = appendChunk3(chunks, b[bi:], o[oi:], t[ti:])
			break
		}
		if s == bi && mo[s] == oi && mt[s] == ti {
			// Stable run: extend while all three cursors advance in step.
			end := bi
			for end < len(b) {
				so, okO := mo[end]
				st, okT := mt[end]
				if !okO || !okT || so != oi+(end-bi) || st != ti+(end-bi) {
					break
				}
				end++
			}
			n := end - bi
			chunks = append(chunks, Chunk3{Kind: Chunk3Same, Base: b[bi:end], Ours: o[oi : oi+n], Theirs: t[ti : ti+n]})
			bi, oi, ti = end, oi+n, ti+n
			continue
		}
		// Unstable region up to the stable line s.
		chunks = appendChunk3(chunks, b[bi:s], o[oi:mo[s]], t[ti:mt[s]])
		bi, oi, ti = s, mo[s], mt[s]
	}
	return chunks
}

// baseMatches maps each kept base line index to its line index on the other
// side, walking the base→side edit script.
func baseMatches(edits []Edit) map[int]int {
	m := make(map[int]int)
	bi, si := 0, 0
	for _, e := range edits {
		switch e.Op {
		case OpEqual:
			m[bi] = si
			bi++
			si++
		case OpDelete:
			bi++
		case OpInsert:
			si++
		}
	}
	return m
}

// appendChunk3 classifies one unstable region and appends it; an all-empty
// region contributes nothing.
func appendChunk3(chunks []Chunk3, base, ours, theirs []string) []Chunk3 {
	if len(base) == 0 && len(ours) == 0 && len(theirs) == 0 {
		return chunks
	}
	kind := Chunk3Conflict
	switch {
	case eqLines(ours, base) && eqLines(theirs, base):
		kind = Chunk3Same
	case eqLines(ours, base):
		kind = Chunk3Theirs
	case eqLines(theirs, base):
		kind = Chunk3Ours
	case eqLines(ours, theirs):
		kind = Chunk3Both
	}
	return append(chunks, Chunk3{Kind: kind, Base: base, Ours: ours, Theirs: theirs})
}

// eqLines reports whether two line slices are identical.
func eqLines(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// Merge3 builds the merged text: every auto-resolvable chunk contributes its
// resolution, every conflict becomes a diff3-style marker block (base section
// included, so the editor's #1149 machinery shows all three versions). It
// returns the merged text and the number of conflict blocks.
func Merge3(base, ours, theirs string) (merged string, conflicts int) {
	var out []string
	for _, c := range Compute3(base, ours, theirs) {
		if lines, ok := c.Resolution(); ok {
			out = append(out, lines...)
			continue
		}
		conflicts++
		out = append(out, "<<<<<<< ours")
		out = append(out, c.Ours...)
		out = append(out, "||||||| base")
		out = append(out, c.Base...)
		out = append(out, "=======")
		out = append(out, c.Theirs...)
		out = append(out, ">>>>>>> theirs")
	}
	return strings.Join(out, "\n") + "\n", conflicts
}
