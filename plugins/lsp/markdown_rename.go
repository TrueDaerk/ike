package lsp

import (
	"path/filepath"

	"ike/internal/lang"
	ilsp "ike/internal/lsp"
	"ike/internal/lsp/manager"
)

// markdown_rename.go completes a Markdown heading rename (#2025).
//
// The *anchors* need no help: marksman resolves same-document references
// itself, so renaming `## Old Heading` returns edits rewriting every
// `](#old-heading)` in the file alongside the heading — verified against
// marksman before building anything here, exactly as the issue asked. What the
// server leaves behind is the link *title*: a table of contents built from
// `- [Old Heading](#old-heading)` keeps reading "Old Heading" while already
// pointing at `#new-heading`. Since IKE conceals link destinations, that made
// a heading rename look like it had updated nothing at all.
//
// So this file adds one narrow completion: a link whose *own* destination the
// server just rewrote, and whose title still spells the old heading exactly,
// is retitled with the new heading. The server's edits are the reference
// resolution — no anchor slugification is reimplemented here, and a title the
// author deliberately worded differently ("see [the section below](#…)") is
// left alone.

// mergeHeadingTitleEdits appends the link-title edits to the renamed file's
// own slice of the WorkspaceEdit. Everything else passes through untouched:
// other files, other languages, and a rename with no known old name (a server
// without prepareRename) are none of this file's business.
func (b *bridge) mergeHeadingTitleEdits(files []manager.FileEdits, path, oldName, newName string) []manager.FileEdits {
	if oldName == "" || oldName == newName || !isMarkdownPath(path) {
		return files
	}
	mgr := b.manager()
	if mgr == nil {
		return files
	}
	// The renamed file is the one under the caret, so it is always an open
	// document; its synced lines are what the server's edits address.
	lines, open := mgr.DocLines(path)
	if !open {
		return files
	}
	for i, f := range files {
		if filepath.Clean(f.Path) != filepath.Clean(path) {
			continue
		}
		files[i].Edits = append(f.Edits, headingLinkTitleEdits(lines, f.Edits, oldName, newName)...)
	}
	return files
}

// isMarkdownPath reports whether path is a Markdown document, through the
// language registry so the extension list stays in one place.
func isMarkdownPath(path string) bool {
	l, ok := lang.ByPath(path)
	return ok && l.ID == "markdown"
}

// headingLinkTitleEdits returns one extra edit per anchor edit that sits in an
// inline link's destination whose title is exactly oldName: the title span
// becomes newName. The returned edits never overlap the input ones (a title
// lies left of its destination), so they can simply be appended — the shared
// bottom-up application order handles the rest.
func headingLinkTitleEdits(lines []string, edits []ilsp.FormatEdit, oldName, newName string) []ilsp.FormatEdit {
	var out []ilsp.FormatEdit
	for _, e := range edits {
		if e.StartLine != e.EndLine || e.StartLine < 0 || e.StartLine >= len(lines) {
			continue
		}
		runes := []rune(lines[e.StartLine])
		if e.StartCol > len(runes) {
			continue
		}
		start, end, ok := linkTitleSpan(runes, e.StartCol)
		if !ok || string(runes[start:end]) != oldName {
			continue
		}
		out = append(out, ilsp.FormatEdit{
			StartLine: e.StartLine, StartCol: start,
			EndLine: e.StartLine, EndCol: end,
			Text: newName,
		})
	}
	return out
}

// linkTitleSpan locates the `[title]` of the inline link whose destination
// carries the anchor starting at col. It walks left from the '#' through the
// destination to the `](`, then to the matching '[' — the shapes that qualify
// are `[title](#anchor)` and `[title](file.md#anchor)`, nothing else.
func linkTitleSpan(runes []rune, col int) (start, end int, ok bool) {
	if col < 1 || runes[col-1] != '#' {
		return 0, 0, false // not an anchor reference
	}
	i := col - 2
	for ; i >= 0; i-- {
		switch runes[i] {
		case '(':
			// The destination opener: a link only if a title closes on it.
			if i >= 1 && runes[i-1] == ']' {
				end = i - 1
				for j := end - 1; j >= 0; j-- {
					if runes[j] == '[' {
						return j + 1, end, end > j+1
					}
				}
			}
			return 0, 0, false
		case ')', '[', ']':
			return 0, 0, false // not inside a link destination
		}
	}
	return 0, 0, false
}
