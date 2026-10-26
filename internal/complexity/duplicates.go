package complexity

// duplicates.go is the duplicate-code detector (Roadmap 0560, #3044): the
// type-1 / type-2 clone groups of a project — exact copies and copies with
// renamed identifiers or changed literals — over the normalized token
// streams tokens.go builds. The detector is pure and runs on a snapshot:
// the app calls View.Duplicates off the Update goroutine whenever the index
// moved (the walk re-tokenizes a saved file alone; the matching is a pass
// over the streams already held).
//
// Algorithm, per language:
//
//  1. Every window of minTokens consecutive tokens inside one segment is
//     fingerprinted with a polynomial rolling hash; the (hash, position)
//     pairs are sorted, so equal windows sit in one run.
//  2. A run of two or more windows is a seed. A seed whose members all have
//     the same left neighbour window, and whose left run holds exactly the
//     same members, is skipped: the run one token to the left grows into
//     the same group. Members overlapping an earlier member of the same
//     segment are dropped, so a periodic block never matches itself.
//  3. The seed grows left and right while every member's next token agrees
//     and no member runs into a segment edge or into another member — the
//     maximal range of that member set.
//  4. Groups with the same occurrences collapse; a group whose every
//     occurrence lies inside an occurrence of a group with at least as many
//     occurrences is nested in it and dropped.
//
// Groups come back by tokens × occurrences descending.

import (
	"sort"
	"strconv"
	"strings"
)

// DefaultDuplicatesMinTokens is the minimum clone size the setting
// complexity.duplicates_min_tokens falls back to.
const DefaultDuplicatesMinTokens = 50

// Occurrence is one copy of a clone: where it sits and which tokens of its
// file's stream it spans.
type Occurrence struct {
	Path    string `json:"path"`
	Line    int    `json:"line"`    // 0-based first line
	EndLine int    `json:"endLine"` // 0-based last line
	Start   int    `json:"start"`   // first token index in the file's Tokens
	End     int    `json:"end"`     // one past the last token index
}

// Lines is the occurrence's line count.
func (o Occurrence) Lines() int { return o.EndLine - o.Line + 1 }

// CloneGroup is one set of copies of the same normalized code.
type CloneGroup struct {
	Lang   string `json:"lang"`
	Tokens int    `json:"tokens"` // the clone's length in tokens
	// Occurrences are the copies, sorted by path then line; always at
	// least two.
	Occurrences []Occurrence `json:"occurrences"`
}

// Score ranks groups: tokens × occurrences.
func (g CloneGroup) Score() int { return g.Tokens * len(g.Occurrences) }

// Lines is the longest occurrence's line count.
func (g CloneGroup) Lines() int {
	n := 0
	for _, o := range g.Occurrences {
		n = max(n, o.Lines())
	}
	return n
}

// Files counts the distinct files the group spans.
func (g CloneGroup) Files() int {
	seen := map[string]bool{}
	for _, o := range g.Occurrences {
		seen[o.Path] = true
	}
	return len(seen)
}

// Key identifies the group across detections: its occurrences' paths and
// first lines.
func (g CloneGroup) Key() string {
	var b strings.Builder
	for _, o := range g.Occurrences {
		b.WriteString(o.Path)
		b.WriteByte(':')
		b.WriteString(strconv.Itoa(o.Line))
		b.WriteByte('\x00')
	}
	return b.String()
}

// Duplicates finds the clone groups of the snapshot's token streams with
// at least minTokens tokens (a non-positive minTokens means the default).
func (v View) Duplicates(minTokens int) []CloneGroup {
	return Duplicates(v.Tokens, minTokens)
}

// Duplicates finds the clone groups of at least minTokens tokens among
// the files' streams, per language, sorted by Score descending.
func Duplicates(files map[string]Tokens, minTokens int) []CloneGroup {
	if minTokens <= 0 {
		minTokens = DefaultDuplicatesMinTokens
	}
	byLang := map[string][]string{}
	for p, t := range files {
		if t.Len() >= minTokens {
			byLang[t.Lang] = append(byLang[t.Lang], p)
		}
	}
	var out []CloneGroup
	for lang, paths := range byLang {
		sort.Strings(paths)
		d := &detector{k: minTokens, lang: lang}
		for _, p := range paths {
			d.add(p, files[p])
		}
		out = append(out, d.run()...)
	}
	sortGroups(out)
	return out
}

// sortGroups orders groups by score, then tokens, then first occurrence.
func sortGroups(gs []CloneGroup) {
	sort.Slice(gs, func(i, j int) bool {
		a, b := gs[i], gs[j]
		if a.Score() != b.Score() {
			return a.Score() > b.Score()
		}
		if a.Tokens != b.Tokens {
			return a.Tokens > b.Tokens
		}
		if a.Occurrences[0].Path != b.Occurrences[0].Path {
			return a.Occurrences[0].Path < b.Occurrences[0].Path
		}
		return a.Occurrences[0].Line < b.Occurrences[0].Line
	})
}

// The rolling hash base: odd, so B^n never vanishes modulo 2^64.
const hashBase uint64 = 1_000_003

// detector holds one language's streams, concatenated into one global
// position space so a window is one integer.
type detector struct {
	k    int
	lang string
	// The concatenated stream: tok the hashes, line the lines, and per
	// global position its file (index into files) and segment (into segs).
	tok  []uint32
	line []int32
	seg  []int32
	// files are the paths; segs the segment bounds in global positions.
	files []string
	segs  []span
	// fileOf maps a segment to its file; base is the file's first global
	// position (Occurrence.Start is relative to the file).
	fileOf []int32
	base   []int
}

type span struct{ lo, hi int }

// window is one fingerprinted window at global position pos.
type window struct {
	h   uint64
	pos int32
}

// add appends t's segments to the global stream.
func (d *detector) add(path string, t Tokens) {
	fi := len(d.files)
	d.files = append(d.files, path)
	d.base = append(d.base, len(d.tok))
	for i := range t.Seg {
		lo, hi := int(t.Seg[i]), t.segEnd(i)
		if hi-lo < d.k {
			continue
		}
		si := int32(len(d.segs))
		d.segs = append(d.segs, span{len(d.tok), len(d.tok) + hi - lo})
		d.fileOf = append(d.fileOf, int32(fi))
		d.tok = append(d.tok, t.Hash[lo:hi]...)
		d.line = append(d.line, t.Line[lo:hi]...)
		for j := lo; j < hi; j++ {
			d.seg = append(d.seg, si)
		}
	}
}

// run detects the groups of the stream.
func (d *detector) run() []CloneGroup {
	k := d.k
	if len(d.tok) < 2*k {
		return nil
	}
	// B^(k-1), the weight of a window's first token.
	top := uint64(1)
	for i := 1; i < k; i++ {
		top *= hashBase
	}
	// wh[pos] is the hash of the window starting at pos, 0 past a
	// segment's last window (a hash is never 0 in practice; a collision
	// there only costs a skipped seed).
	wh := make([]uint64, len(d.tok))
	wins := make([]window, 0, len(d.tok))
	for _, s := range d.segs {
		var h uint64
		for i := s.lo; i < s.lo+k; i++ {
			h = h*hashBase + uint64(d.tok[i])
		}
		for pos := s.lo; pos+k <= s.hi; pos++ {
			wh[pos] = h
			wins = append(wins, window{h, int32(pos)})
			if pos+k < s.hi {
				h = (h-uint64(d.tok[pos])*top)*hashBase + uint64(d.tok[pos+k])
			}
		}
	}
	sort.Slice(wins, func(i, j int) bool {
		if wins[i].h != wins[j].h {
			return wins[i].h < wins[j].h
		}
		return wins[i].pos < wins[j].pos
	})
	// counts holds the run length of every repeated hash, for the
	// left-neighbour skip.
	counts := map[uint64]int{}
	for i := 0; i < len(wins); {
		j := i + 1
		for j < len(wins) && wins[j].h == wins[i].h {
			j++
		}
		if j-i >= 2 {
			counts[wins[i].h] = j - i
		}
		i = j
	}
	var groups []CloneGroup
	seen := map[string]bool{}
	for i := 0; i < len(wins); {
		j := i + 1
		for j < len(wins) && wins[j].h == wins[i].h {
			j++
		}
		if j-i >= 2 {
			if g, ok := d.seed(wins[i:j], wh, counts); ok {
				key := g.Key() + strconv.Itoa(g.Tokens)
				if !seen[key] {
					seen[key] = true
					groups = append(groups, g)
				}
			}
		}
		i = j
	}
	return dropNested(groups)
}

// seed turns one run of equal windows into its maximal group, or reports
// false when the run is covered by its left neighbour or holds fewer than
// two non-overlapping members.
func (d *detector) seed(run []window, wh []uint64, counts map[uint64]int) (CloneGroup, bool) {
	k := d.k
	// The members: verified against the first (a 64-bit collision is
	// unlikely, a wrong group would be worse), non-overlapping within a
	// segment.
	members := make([]int, 0, len(run))
	first := int(run[0].pos)
	for _, w := range run {
		pos := int(w.pos)
		if pos != first && !d.equal(first, pos, k) {
			continue
		}
		if n := len(members); n > 0 && d.seg[members[n-1]] == d.seg[pos] && pos < members[n-1]+k {
			continue
		}
		members = append(members, pos)
	}
	if len(members) < 2 {
		return CloneGroup{}, false
	}
	// Covered by the left neighbour: every member has one, all left
	// windows are equal and their run has exactly these members.
	if left, ok := d.leftHash(members, wh); ok && counts[left] == len(members) {
		return CloneGroup{}, false
	}
	// Grow: left while every member's previous token agrees, right while
	// every member's next does; never past a segment edge or into the next
	// member of the same segment.
	length := k
	maxLen := int(^uint(0) >> 1)
	for i := 1; i < len(members); i++ {
		if d.seg[members[i]] == d.seg[members[i-1]] {
			maxLen = min(maxLen, members[i]-members[i-1])
		}
	}
	for length < maxLen && d.canGrowLeft(members) {
		for i := range members {
			members[i]--
		}
		length++
	}
	for length < maxLen && d.canGrowRight(members, length) {
		length++
	}
	g := CloneGroup{Lang: d.lang, Tokens: length}
	for _, pos := range members {
		fi := d.fileOf[d.seg[pos]]
		g.Occurrences = append(g.Occurrences, Occurrence{
			Path:    d.files[fi],
			Line:    int(d.line[pos]),
			EndLine: int(d.line[pos+length-1]),
			Start:   pos - d.base[fi],
			End:     pos + length - d.base[fi],
		})
	}
	sort.Slice(g.Occurrences, func(i, j int) bool {
		if g.Occurrences[i].Path != g.Occurrences[j].Path {
			return g.Occurrences[i].Path < g.Occurrences[j].Path
		}
		return g.Occurrences[i].Line < g.Occurrences[j].Line
	})
	return g, true
}

// equal reports whether the n tokens at a and b agree.
func (d *detector) equal(a, b, n int) bool {
	for i := 0; i < n; i++ {
		if d.tok[a+i] != d.tok[b+i] {
			return false
		}
	}
	return true
}

// leftHash is the common hash of every member's left-neighbour window,
// false when a member has none or they differ.
func (d *detector) leftHash(members []int, wh []uint64) (uint64, bool) {
	var h uint64
	for i, pos := range members {
		if pos == 0 || d.seg[pos-1] != d.seg[pos] {
			return 0, false
		}
		if i == 0 {
			h = wh[pos-1]
		} else if wh[pos-1] != h {
			return 0, false
		}
	}
	return h, true
}

// canGrowLeft reports whether every member has a previous token in its
// segment and they all agree.
func (d *detector) canGrowLeft(members []int) bool {
	for _, pos := range members {
		if pos == 0 || d.seg[pos-1] != d.seg[pos] {
			return false
		}
	}
	first := d.tok[members[0]-1]
	for _, pos := range members[1:] {
		if d.tok[pos-1] != first {
			return false
		}
	}
	return true
}

// canGrowRight reports whether every member of length has a next token in
// its segment and they all agree.
func (d *detector) canGrowRight(members []int, length int) bool {
	for _, pos := range members {
		if end := pos + length; end >= len(d.tok) || d.seg[end] != d.seg[pos] {
			return false
		}
	}
	first := d.tok[members[0]+length]
	for _, pos := range members[1:] {
		if d.tok[pos+length] != first {
			return false
		}
	}
	return true
}

// dropNested removes every group whose occurrences all lie inside
// occurrences of longer groups: the repeated rows of a table that is itself
// copied, the inner block of a copied function — they read as noise next
// to the group that spans them. A group with one occurrence outside every
// longer group's stays: it is a clone in its own right.
func dropNested(groups []CloneGroup) []CloneGroup {
	type ref struct{ group, occ int }
	byFile := map[string][]ref{}
	for gi, g := range groups {
		for oi, o := range g.Occurrences {
			byFile[o.Path] = append(byFile[o.Path], ref{gi, oi})
		}
	}
	covered := func(gi int, o Occurrence) bool {
		for _, r := range byFile[o.Path] {
			h := groups[r.group]
			if r.group != gi && h.Tokens > groups[gi].Tokens && contains(h.Occurrences[r.occ], o) {
				return true
			}
		}
		return false
	}
	out := make([]CloneGroup, 0, len(groups))
	for gi, g := range groups {
		nested := true
		for _, o := range g.Occurrences {
			if !covered(gi, o) {
				nested = false
				break
			}
		}
		if !nested {
			out = append(out, g)
		}
	}
	return out
}

// contains reports whether occurrence outer spans inner (same file).
func contains(outer, inner Occurrence) bool {
	return outer.Path == inner.Path && outer.Start <= inner.Start && inner.End <= outer.End
}
