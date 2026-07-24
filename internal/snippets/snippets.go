// Package snippets is the live-template store (#1152): user-configured
// [[snippets]] entries plus a small built-in table, resolved per buffer
// language. Two consumers share it — the editor's insert-mode Tab expansion
// (trigger word before the cursor → body through the LSP snippet placeholder
// engine) and the completion popup, which lists matching templates via the
// local completion engine's Source below.
//
// Resolution order for one trigger: user language-scoped > built-in
// language-scoped > user global > built-in global. A language-scoped entry
// always beats a global one; within a scope the user entry shadows the
// built-in with the same trigger+language. User entries are read live from
// config.Get(), so a config reload applies without any re-wiring.
package snippets

import (
	"context"
	"strings"

	"ike/internal/complete"
	"ike/internal/config"
	"ike/internal/lang"
	ilsp "ike/internal/lsp"
	"ike/internal/lsp/protocol"
)

// SourceName tags the completion batches this package sends; the editor's
// accept path recognises it to re-indent the body to the cursor's line.
const SourceName = "snippets"

// Entry is one resolved template: a trigger word, the LSP-snippet-syntax body
// and the language ID it is scoped to ("" = every buffer).
type Entry struct {
	Trigger  string
	Language string
	Body     string
}

// builtins are the guarded built-in examples (#1152) — a handful per bundled
// language, each overridable by a user entry with the same trigger+language.
var builtins = []Entry{
	{Trigger: "iferr", Language: "go", Body: "if err != nil {\n\t$1\n}"},
	{Trigger: "main", Language: "go", Body: "func main() {\n\t$1\n}"},
	{Trigger: "forr", Language: "go", Body: "for $1, $2 := range $3 {\n\t$4\n}"},
	{Trigger: "main", Language: "python", Body: "if __name__ == \"__main__\":\n\t$1"},
	{Trigger: "def", Language: "python", Body: "def $1($2):\n\t$3"},
	// JS and TS files both resolve to the "typescript" language ID.
	{Trigger: "log", Language: "typescript", Body: "console.log($1)"},
	{Trigger: "fn", Language: "typescript", Body: "function $1($2) {\n\t$3\n}"},
}

// Lookup resolves a trigger word for the buffer at path: the language-scoped
// entry wins over a global one; within a scope a user entry shadows the
// built-in. ok=false when no template matches.
func Lookup(path, trigger string) (body string, ok bool) {
	for _, e := range For(path) {
		if e.Trigger == trigger {
			return e.Body, true
		}
	}
	return "", false
}

// For returns every template available in the buffer at path — language-scoped
// entries first, then globals — deduplicated by trigger under the Lookup
// precedence, so the completion popup offers exactly what Tab would expand.
func For(path string) []Entry {
	id := ""
	if l, ok := lang.ByPath(path); ok {
		id = l.ID
	}
	user := config.Get().Snippets
	seen := map[string]bool{}
	var out []Entry
	add := func(e Entry) {
		if !seen[e.Trigger] {
			seen[e.Trigger] = true
			out = append(out, e)
		}
	}
	// Scope order: language beats global; user beats built-in within a scope.
	scopes := []string{""}
	if id != "" {
		scopes = []string{id, ""}
	}
	for _, scope := range scopes {
		for _, s := range user {
			if s.Language == scope {
				add(Entry{Trigger: s.Trigger, Language: s.Language, Body: s.Body})
			}
		}
		for _, b := range builtins {
			if b.Language == scope {
				add(b)
			}
		}
	}
	return out
}

// Source offers the templates as completion items (#1152) through the local
// completion engine, so they appear alongside LSP items — and open the popup
// in plain buffers with no server at all. Register with complete.Engine.
type Source struct{}

// NewSource returns the completion source.
func NewSource() Source { return Source{} }

// Name implements complete.Source.
func (Source) Name() string { return SourceName }

// Priority implements complete.Source: below symbols, above Emmet and the
// word echo — a deliberately placed template usually beats an incidental match.
func (Source) Priority() int { return ilsp.PrioritySnippets }

// Complete implements complete.Source: every template for the buffer's
// language (plus globals) is returned; the popup's fuzzy prefix filter
// narrows the list as the user types.
func (Source) Complete(_ context.Context, req complete.Request) ([]ilsp.CompletionItem, error) {
	entries := For(req.Path)
	items := make([]ilsp.CompletionItem, 0, len(entries))
	for _, e := range entries {
		items = append(items, ilsp.CompletionItem{
			Label:      e.Trigger,
			FilterText: e.Trigger,
			SortText:   e.Trigger,
			InsertText: e.Body,
			IsSnippet:  true,
			Detail:     "template " + preview(e.Body),
			Kind:       protocol.KindSnippet,
		})
	}
	return items, nil
}

// preview flattens a body to a short single-line popup detail.
func preview(body string) string {
	s := strings.ReplaceAll(body, "\n", " ")
	s = strings.ReplaceAll(s, "\t", "")
	if r := []rune(s); len(r) > 30 {
		s = string(r[:29]) + "…"
	}
	return s
}
