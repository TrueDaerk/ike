package palette

import (
	"os"
	"path/filepath"
	"strings"

	"ike/internal/pathcomplete"
)

// OpenPathDescendMsg is emitted when an open-path directory candidate is
// activated (#999): the root model re-opens the picker with the accepted
// directory as the new query, so enter descends like tab does.
type OpenPathDescendMsg struct{ Query string }

// OpenPathMode is the "Open File…" picker (#999): a filesystem path browser
// over absolute and ~-relative paths, for files outside the workspace (the
// workspace-scoped goto-file stays '@'). Candidates come from the shared
// pathcomplete engine; tab extends the query (Completer), enter on a file
// opens it via the normal OpenFileMsg path, enter on a directory descends.
// It has no user-facing prefix story — the root model opens it locked.
type OpenPathMode struct{}

// NewOpenPathMode builds the open-path mode.
func NewOpenPathMode() *OpenPathMode { return &OpenPathMode{} }

// OpenPathPrefix selects the open-path mode; ';' is unclaimed by the other
// modes ('%': recent, '~': scratch, '*': search-all, '#': projects, …).
const OpenPathPrefix = ';'

// Prefix implements Mode.
func (o *OpenPathMode) Prefix() rune { return OpenPathPrefix }

// Placeholder implements Mode.
func (o *OpenPathMode) Placeholder() string { return "Open file path… (/, ~/; tab completes)" }

// Results implements Mode: filesystem candidates for the typed path prefix.
// An empty query seeds the two roots a path can start from.
func (o *OpenPathMode) Results(query string, cx Context) []Item {
	if strings.TrimSpace(query) == "" {
		return []Item{
			{Title: "~/", Msg: OpenPathDescendMsg{Query: "~/"}},
			{Title: "/", Msg: OpenPathDescendMsg{Query: "/"}},
		}
	}
	res := pathcomplete.Complete(query)
	items := make([]Item, 0, len(res.Candidates)+1)
	exact := false
	for _, c := range res.Candidates {
		if c == query {
			exact = true
		}
		if strings.HasSuffix(c, string(filepath.Separator)) {
			items = append(items, Item{Title: c, Msg: OpenPathDescendMsg{Query: c}})
			continue
		}
		items = append(items, Item{Title: c, Msg: OpenFileMsg{Path: expandedAbs(c)}})
	}
	if !exact && len(items) == 0 {
		// No candidate: offer the raw query so a full typed path still
		// activates; the root model raises the error for a missing file.
		items = append(items, Item{Title: "Open " + query, Msg: OpenFileMsg{Path: expandedAbs(query)}})
	}
	return items
}

// Complete implements Completer: tab extends the query by the longest shared
// candidate prefix, descending on a unique directory match.
func (o *OpenPathMode) Complete(query string) string {
	return pathcomplete.Complete(query).Completed
}

// expandedAbs resolves ~ and makes the path absolute and cleaned.
func expandedAbs(p string) string {
	p = pathcomplete.Expand(p)
	if !filepath.IsAbs(p) {
		if cwd, err := os.Getwd(); err == nil {
			p = filepath.Join(cwd, p)
		}
	}
	return filepath.Clean(p)
}
