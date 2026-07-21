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
				content := string(src[cap.Node.StartByte():cap.Node.EndByte()])
				if guess && !guessFragment(langID, content) {
					continue
				}
				frags = appendFrag(frags, langID, &cap.Node)
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
	return frags
}
