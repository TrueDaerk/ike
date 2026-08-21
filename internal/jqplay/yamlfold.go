package jqplay

// yamlfold.go is fold.go's job for a YAML result (#2039): which blocks of the
// rendered output fold, and how many members the collapsed placeholder
// credits them with. JSON folds between delimiters; YAML has none, so the
// structure is the indentation.
//
// Like the JSON scan this reads text *its own encoder wrote* (encodeYAML:
// block style, two-space indent, no flow collections), which is what makes a
// line scan enough. The one shape that indents without nesting is a long
// scalar the emitter wrapped across rows, so a line is only read as a block
// header when it *cannot* be a wrapped scalar: it ends on a colon, it is a
// bare dash, it is a block-scalar indicator, or it is a sequence entry whose
// content is itself a mapping (`- key: value` — a plain scalar may not
// contain `: ` at all, and a quoted one is excluded by its opening quote).

import "strings"

// yamlFolds returns the foldable blocks of a YAML result in pre-order (outer
// before inner, the order the editor's innermost-fold lookup relies on).
func yamlFolds(text string) []Fold {
	lines := strings.Split(text, "\n")
	var out []Fold
	for i, line := range lines {
		op, ok := yamlOpener(line)
		if !ok {
			continue
		}
		indent := yamlIndentOf(line)
		end := yamlBlockEnd(lines, i, indent)
		if end <= i {
			continue // nothing under it after all: an empty or elided value
		}
		fold := Fold{HeaderLine: i, EndLine: end, Unit: op.unit}
		if op.unit == UnitLines {
			fold.Items = end - i
		} else {
			items, unit := yamlMembers(lines, i, end)
			fold.Items = items + op.extra
			if fold.Unit == "" {
				fold.Unit = unit
			}
		}
		out = append(out, fold)
	}
	return out
}

// yamlOpen describes a line that opens a foldable block: the unit its members
// are counted in ("" = the children decide), and how many members already sit
// on the header line itself — one for a `- key: value` entry, whose first key
// shares the row with its dash.
type yamlOpen struct {
	unit  string
	extra int
}

// yamlBlockEnd is the last line belonging to the block opened on line i at
// the given indent: the run of following lines that are blank or indented
// deeper, with trailing blanks given back — a blank line between two mappings
// belongs to neither.
func yamlBlockEnd(lines []string, i, indent int) int {
	end := i
	for j := i + 1; j < len(lines); j++ {
		if strings.TrimSpace(lines[j]) == "" {
			continue // a blank line inside a block does not close it
		}
		if yamlIndentOf(lines[j]) <= indent {
			break
		}
		end = j
	}
	return end
}

// yamlMembers counts the block's *direct* members and names their unit. The
// children are the lines at the block's shallowest indent — not at the first
// child's, which may be a nested block's body sitting above its siblings —
// and a leading dash on the first of them makes the block a sequence.
func yamlMembers(lines []string, i, end int) (items int, unit string) {
	child := -1
	for j := i + 1; j <= end; j++ {
		if strings.TrimSpace(lines[j]) == "" {
			continue
		}
		if in := yamlIndentOf(lines[j]); child < 0 || in < child {
			child = in
		}
	}
	unit = UnitKeys
	for j := i + 1; j <= end; j++ {
		if strings.TrimSpace(lines[j]) == "" || yamlIndentOf(lines[j]) != child {
			continue
		}
		if items == 0 && yamlDash(lines[j][child:]) {
			unit = UnitItems
		}
		items++
	}
	return items, unit
}

// yamlOpener reports whether line opens a foldable block. See the file
// comment for why the four accepted shapes are the only safe ones.
func yamlOpener(line string) (yamlOpen, bool) {
	s := strings.TrimLeft(line, " ")
	dash := yamlDash(s)
	if dash {
		s = strings.TrimLeft(strings.TrimPrefix(s, "-"), " ")
	}
	switch {
	case s == "":
		// A bare `-`: the entry's value is the block below it, and its shape
		// is whatever the children turn out to be.
		return yamlOpen{}, dash
	case yamlBlockScalar(s):
		return yamlOpen{unit: UnitLines}, true
	case !dash:
		// A plain mapping key with nothing after the colon. Anything else on
		// a non-dash row carries its whole value and opens nothing.
		return yamlOpen{}, strings.HasSuffix(s, ":")
	case yamlDash(s):
		// `- - a`: a nested sequence whose first item is on the dash's row.
		return yamlOpen{unit: UnitItems, extra: 1}, true
	case s[0] == '"' || s[0] == '\'':
		return yamlOpen{}, false // a quoted scalar, possibly wrapped
	case strings.HasSuffix(s, ":"), strings.Contains(s, ": "):
		// `- key:` / `- key: value`: a mapping entry whose first key shares
		// the dash's row with it.
		return yamlOpen{unit: UnitKeys, extra: 1}, true
	}
	return yamlOpen{}, false
}

// yamlDash reports whether s starts a sequence entry — a dash that is a
// structural indicator rather than the sign of a negative number.
func yamlDash(s string) bool { return s == "-" || strings.HasPrefix(s, "- ") }

// yamlBlockScalar reports whether s ends on a `|`/`>` block-scalar indicator
// with only its chomping and explicit-indent modifiers after it: `key: |-`,
// `key: >2`, or the bare indicator left by a stripped dash.
func yamlBlockScalar(s string) bool {
	if i := strings.LastIndex(s, ": "); i >= 0 {
		s = strings.TrimSpace(s[i+2:])
	}
	if s == "" || (s[0] != '|' && s[0] != '>') {
		return false
	}
	for _, r := range s[1:] {
		if r != '-' && r != '+' && (r < '0' || r > '9') {
			return false
		}
	}
	return true
}

// yamlIndentOf counts a line's leading spaces. The encoder never emits tabs —
// YAML forbids them as indentation — so spaces are the whole story.
func yamlIndentOf(line string) int {
	n := 0
	for n < len(line) && line[n] == ' ' {
		n++
	}
	return n
}
