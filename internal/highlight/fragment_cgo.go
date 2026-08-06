//go:build cgo

package highlight

import (
	"strings"

	ts "github.com/tree-sitter/go-tree-sitter"

	"ike/internal/lang"
)

// detectFragments parses lines with the host grammar and runs its injection
// query, turning every @fragment.<lang>[.guess] capture — and every
// @fragment.language / @fragment.content pair (#880) — into a Fragment. Like
// parse, it re-parses from scratch: fragment detection runs on the LSP sync
// and highlight paths (off the Update goroutine), never per-keystroke render.
// Iteration is match-based (not capture-based) so a dynamic pair's language
// tag and content node arrive together.
// guessKey identifies a group of .guess captures: same capture name under the
// same parent node (#1625).
type guessKey struct {
	name   string
	parent uintptr
}

// guessGroup collects one guessKey's captured nodes; the content heuristic
// runs once over the joined parts.
type guessGroup struct {
	langID string
	nodes  []ts.Node
	parts  []string
}

func detectFragments(g lang.Grammar, lines []string) []Fragment {
	gi, ok := g.(*grammarImpl)
	if !ok {
		return nil
	}
	tsLang, query, ok := gi.compiledInjections()
	if !ok {
		return nil
	}

	src := []byte(strings.Join(lines, "\n"))
	parser := ts.NewParser()
	defer parser.Close()
	if err := parser.SetLanguage(tsLang); err != nil {
		return nil
	}
	tree := parser.Parse(src, nil)
	if tree == nil {
		return nil
	}
	defer tree.Close()

	conv := newColMapper(lines)
	cursor := ts.NewQueryCursor()
	defer cursor.Close()
	names := query.CaptureNames()

	appendFrag := func(frags []Fragment, langID string, node *ts.Node) []Fragment {
		content := string(src[node.StartByte():node.EndByte()])
		if content == "" {
			return frags
		}
		start, end := node.StartPosition(), node.EndPosition()
		return append(frags, Fragment{
			Lang:      langID,
			StartLine: int(start.Row),
			StartCol:  conv.runeCol(int(start.Row), int(start.Column)),
			EndLine:   int(end.Row),
			EndCol:    conv.runeCol(int(end.Row), int(end.Column)),
			Lines:     strings.Split(content, "\n"),
		})
	}

	// Guessed captures group per capture name and parent node: a template
	// literal's text chunks around ${…} substitutions are separate
	// string_fragment nodes under one template_string (#1625), and the
	// heuristic must judge the joined text — no single chunk of
	// `<ul>${items}</ul>` looks like HTML on its own. On a hit every node of
	// the group becomes a fragment. Grouping by parent (not by match) is
	// deliberate: tree-sitter reports each chunk as its own match.
	var guesses []*guessGroup
	groupIx := map[guessKey]*guessGroup{}
	var frags []Fragment
	matches := cursor.Matches(query, tree.RootNode(), src)
	for {
		match := matches.Next()
		if match == nil {
			break
		}
		// Dynamic pair: the pattern captures the language tag and the content
		// region separately; both belong to this one match.
		var langTag string
		var contents []ts.Node
		for _, cap := range match.Captures {
			switch name := names[cap.Index]; name {
			case "fragment.language":
				langTag = string(src[cap.Node.StartByte():cap.Node.EndByte()])
			case "fragment.content":
				contents = append(contents, cap.Node)
			default:
				langID, guess, ok := fragmentCapture(name)
				if !ok {
					continue
				}
				if !guess {
					frags = appendFrag(frags, langID, &cap.Node)
					continue
				}
				key := guessKey{name: name}
				if p := cap.Node.Parent(); p != nil {
					key.parent = p.Id()
				}
				g := groupIx[key]
				if g == nil {
					g = &guessGroup{langID: langID}
					groupIx[key] = g
					guesses = append(guesses, g)
				}
				g.nodes = append(g.nodes, cap.Node)
				g.parts = append(g.parts, string(src[cap.Node.StartByte():cap.Node.EndByte()]))
			}
		}
		if langTag != "" && len(contents) > 0 {
			if id, ok := resolveFragmentLang(langTag); ok {
				for i := range contents {
					frags = appendFrag(frags, id, &contents[i])
				}
			}
		}
	}
	for _, g := range guesses {
		if !guessFragment(g.langID, strings.Join(g.parts, "\n")) {
			continue
		}
		for i := range g.nodes {
			frags = appendFrag(frags, g.langID, &g.nodes[i])
		}
	}
	return frags
}
