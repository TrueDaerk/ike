//go:build cgo

package highlight

import (
	"strings"

	ts "github.com/tree-sitter/go-tree-sitter"

	"ike/internal/lang"
)

// detectFragments parses lines with the host grammar and runs its injection
// query, turning every @fragment.<lang>[.guess] capture into a Fragment. Like
// parse, it re-parses from scratch: fragment detection runs on the LSP sync
// and highlight paths (off the Update goroutine), never per-keystroke render.
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

	var frags []Fragment
	captures := cursor.Captures(query, tree.RootNode(), src)
	for {
		match, idx := captures.Next()
		if match == nil {
			break
		}
		cap := match.Captures[idx]
		langID, guess, ok := fragmentCapture(names[cap.Index])
		if !ok {
			continue
		}
		content := string(src[cap.Node.StartByte():cap.Node.EndByte()])
		if content == "" {
			continue
		}
		if guess && !guessFragment(langID, content) {
			continue
		}
		start, end := cap.Node.StartPosition(), cap.Node.EndPosition()
		frags = append(frags, Fragment{
			Lang:      langID,
			StartLine: int(start.Row),
			StartCol:  conv.runeCol(int(start.Row), int(start.Column)),
			EndLine:   int(end.Row),
			EndCol:    conv.runeCol(int(end.Row), int(end.Column)),
			Lines:     strings.Split(content, "\n"),
		})
	}
	return frags
}
