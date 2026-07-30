package settings

import (
	"strconv"
	"strings"

	"ike/internal/keymap"
)

// keymap_ranges.go folds numbered binding runs (0460, #1300). Nine lines
// saying "alt+N goes to tab N" carry one fact between them; collapsed into
// `alt+1 … alt+9 · Go to tab 1–9 ▸ 9` they carry the same fact in one line and
// leave the list scannable. The run expands in place with `z`, and a filter
// never folds — search must show exactly what it matched.

// minRange is the shortest run worth folding: two rows fold into one plus a
// caption, which saves nothing.
const minRange = 3

// splitTrailingDigits splits a trailing decimal run off s. ok is false when s
// does not end in a digit, or is only digits.
func splitTrailingDigits(s string) (prefix string, n int, ok bool) {
	i := len(s)
	for i > 0 && s[i-1] >= '0' && s[i-1] <= '9' {
		i--
	}
	if i == len(s) || i == 0 {
		return "", 0, false
	}
	n, err := strconv.Atoi(s[i:])
	if err != nil {
		return "", 0, false
	}
	return s[:i], n, true
}

// rangeKeyOf returns the fold key of a row — the chord and command prefixes
// that a run shares — plus its number. ok is false for rows that cannot be
// part of a numbered run.
func rangeKeyOf(b keymapRow) (key string, n int, ok bool) {
	if b.unbound || b.nobind || b.Chord.Len() != 1 {
		return "", 0, false
	}
	chordPrefix, chordN, ok := splitTrailingDigits(b.Chord.String())
	if !ok {
		return "", 0, false
	}
	cmdPrefix, cmdN, ok := splitTrailingDigits(b.Command)
	if !ok || cmdN != chordN {
		return "", 0, false
	}
	return chordPrefix + "\x00" + cmdPrefix + "\x00" + string(b.Context), chordN, true
}

// foldRanges collapses each run of ≥minRange consecutive numbered bindings
// into a single range row, unless that run is expanded. rows must already be
// in display order; runs are detected on consecutive rows only, so a fold can
// never reorder the list.
func (k *KeymapPage) foldRanges(rows []keymapRow) []keymapRow {
	if k.filter != "" {
		return rows // search shows what it matched, never a fold
	}
	out := make([]keymapRow, 0, len(rows))
	for i := 0; i < len(rows); {
		key, n, ok := rangeKeyOf(rows[i])
		if !ok {
			out = append(out, rows[i])
			i++
			continue
		}
		j, want := i+1, n+1
		for j < len(rows) {
			k2, n2, ok2 := rangeKeyOf(rows[j])
			if !ok2 || k2 != key || n2 != want {
				break
			}
			j, want = j+1, want+1
		}
		run := rows[i:j]
		if len(run) < minRange || k.expanded[key] {
			out = append(out, run...)
			i = j
			continue
		}
		row := run[0]
		row.rangeKey, row.rangeCount = key, len(run)
		row.rangeLast = run[len(run)-1].Chord.String()
		out = append(out, row)
		i = j
	}
	return out
}

// rangeLabel renders a folded run's chord and title: "alt+1 … alt+9" and
// "Go to tab 1–9".
func (b keymapRow) rangeLabel() (chord, title string) {
	first := b.Chord.String()
	chord = first + " … " + b.rangeLast
	title = b.Title
	if prefix, n, ok := splitTrailingDigits(strings.TrimSpace(b.Title)); ok {
		title = prefix + strconv.Itoa(n) + "–" + strconv.Itoa(n+b.rangeCount-1)
	}
	return chord, title
}

// toggleRange expands or collapses the selected run; false when the selection
// is not part of one.
func (k *KeymapPage) toggleRange() bool {
	b, ok := k.current()
	if !ok {
		return false
	}
	key := b.rangeKey
	if key == "" {
		// Inside an expanded run: collapse it again from any of its rows.
		var isRun bool
		if key, _, isRun = rangeKeyOf(b); !isRun || !k.expanded[key] {
			return false
		}
	}
	if k.expanded == nil {
		k.expanded = map[string]bool{}
	}
	k.expanded[key] = !k.expanded[key]
	k.foldGen++ // fold state feeds the row cache (#1396)
	k.sel = clamp(k.sel, 0, len(k.rows())-1)
	return true
}

// rangeBindings lists the individual bindings a folded row stands for, for
// the detail column.
func (k *KeymapPage) rangeBindings(b keymapRow) []keymap.Binding {
	key := b.rangeKey
	if key == "" {
		return nil
	}
	var out []keymap.Binding
	for _, r := range k.unfoldedRows() {
		if k2, _, ok := rangeKeyOf(r); ok && k2 == key {
			out = append(out, r.Binding)
		}
	}
	return out
}
