package complexity

import "sort"

// group.go is the group aggregation (#2937): the per-function numbers
// folded per class and per file, each group a Summary over its direct units.
// A nested lambda or inner def is its own unit with no class, so it counts
// toward its file's group but no class's — exactly the units the flat list
// holds, nothing counted twice.

// Level is a grouping level: none (the flat function list), class or file.
type Level int

const (
	// ByNone is the flat function list.
	ByNone Level = iota
	// ByClass groups the method units of a file by Function.Class; free
	// functions and lambdas belong to no class group.
	ByClass
	// ByFile groups every unit of a file.
	ByFile
)

// String is the level's flag and persistence spelling: "", "class", "file".
func (l Level) String() string {
	switch l {
	case ByClass:
		return "class"
	case ByFile:
		return "file"
	}
	return ""
}

// ParseLevel reads a level's String form; "none" and "" are ByNone.
func ParseLevel(s string) (Level, bool) {
	switch s {
	case "", "none":
		return ByNone, true
	case "class":
		return ByClass, true
	case "file":
		return ByFile, true
	}
	return ByNone, false
}

// Group is the aggregate of one class or one file: the same Summary the
// project gets, over the group's units.
type Group struct {
	// File is the path the caller keyed the functions by.
	File string
	// Class is the qualifier of a class group, "" for a file group.
	Class string
	// Line is the 0-based first line of the group's earliest unit — where
	// "open the group" lands.
	Line int
	// DepthMax and ParamsMax are the largest Depth / Params over the
	// group's functions (the depth: / params: filter on group rows).
	DepthMax  int
	ParamsMax int
	Summary
}

// GroupFunctions folds files (functions per path) into the groups of level
// by: one per (file, class) for ByClass, one per file for ByFile; nil for
// ByNone. Each group is Summarize over its units with the thresholds res
// resolves for its file.
// The order is stable: CCN sum descending, then file, then class.
func GroupFunctions(files map[string][]Function, by Level, res Resolver) []Group {
	if by != ByClass && by != ByFile {
		return nil
	}
	type key struct{ file, class string }
	members := map[key][]Function{}
	for path, fns := range files {
		for _, f := range fns {
			k := key{file: path}
			if by == ByClass {
				if f.Class == "" {
					continue
				}
				k.class = f.Class
			}
			members[k] = append(members[k], f)
		}
	}
	out := make([]Group, 0, len(members))
	for k, fns := range members {
		g := Group{File: k.file, Class: k.class, Line: fns[0].Line,
			Summary: Summarize(fns, res(k.file))}
		for _, f := range fns {
			if f.Line < g.Line {
				g.Line = f.Line
			}
			g.DepthMax = max(g.DepthMax, f.Depth)
			g.ParamsMax = max(g.ParamsMax, f.Params)
		}
		out = append(out, g)
	}
	sort.Slice(out, func(i, j int) bool {
		a, b := out[i], out[j]
		if a.CCNSum != b.CCNSum {
			return a.CCNSum > b.CCNSum
		}
		if a.File != b.File {
			return a.File < b.File
		}
		return a.Class < b.Class
	})
	return out
}

// Hot is the hotspot score of a file (#2957, Tornhill's churn ×
// complexity): its commits in the churn window times its excess above the
// thresholds, or times its CCN sum when nothing exceeds — so a file of
// small functions still ranks by how much code it changes how often.
func Hot(commits int, s Summary) int {
	mass := s.Excess
	if mass == 0 {
		mass = s.CCNSum
	}
	return commits * mass
}
