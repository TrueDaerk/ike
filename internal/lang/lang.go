// Package lang is the neutral language registry: the single source of truth for
// "what is a language" in IKE. A Language bundles the file extensions that select
// it, an optional Tree-sitter grammar for highlighting, an optional LSP server
// spec, and an optional toolchain detector (Roadmap 0101).
//
// It is a leaf package — pure Go, no CGo, no Tree-sitter import; its only IKE
// dependency is the equally leaf internal/config (template overrides, see
// template.go) — so both the highlight engine (internal/highlight) and the LSP
// subsystem (internal/lsp) can depend on it without a cycle. Per-language plugins (plugins/languages/*)
// populate it from their init() via Register, exactly like registry.Register and
// config.Register elsewhere in the codebase.
package lang

import (
	"path/filepath"
	"sort"
	"strings"
	"sync"
)

// Grammar is an opaque highlighting token. Its concrete type is the compiled
// Tree-sitter grammar built by internal/highlight (behind the cgo build tag); the
// registry only ever stores and hands it back, so this package stays CGo-free. A
// nil Grammar means the language has no syntax highlighting (e.g. a CGO_ENABLED=0
// build, where highlight.NewGrammar returns nil).
type Grammar any

// Language is one registered language: how to recognise its files, plus the
// optional capabilities attached to it. Any of Grammar / Server / Toolchain may be
// nil; a Language with all three nil is legal but inert.
type Language struct {
	ID         string   // stable id, e.g. "python"
	Extensions []string // file extensions without dot, e.g. []string{"py", "pyi"}
	Filenames  []string // optional exact base names, e.g. []string{"Dockerfile"}
	Grammar    Grammar  // highlighting grammar, or nil
	Server     *ServerSpec
	Toolchain  Toolchain

	// ServerLanguage names the language whose LSP server handles this
	// language's documents (#1063): e.g. the "go.mod" language delegates to
	// the "go" server, so go.mod files attach to the very gopls instance
	// (same spec, same root, same process) that serves the module's .go
	// files — while the wire languageId stays this language's own ID
	// ("go.mod", gopls' documented id for the file). Empty means the
	// language uses its own Server spec.
	ServerLanguage string

	// Interpreters lists the interpreter base names that select this language
	// via a shebang line (#893), e.g. []string{"python", "python3"} — the
	// fallback when a file has no extension and no known base name. See
	// ForShebang in shebang.go.
	Interpreters []string

	// Comment syntax for toggling (Roadmap 0120). LineComment is the marker
	// placed before a line ("//", "#"); BlockComment is the open/close pair
	// ("/*", "*/"). Empty strings mean the language has no such syntax.
	LineComment  string
	BlockComment [2]string

	// IndentAfter lists trimmed-line suffixes that open a block (Roadmap 0260):
	// a line ending with one of them indents the next line one level deeper,
	// e.g. ":" for Python or "{" for brace languages. Empty means the editor
	// falls back to plain copy-indent.
	IndentAfter []string

	// ScopeNodes lists the Tree-sitter node kinds that define a sticky-scroll
	// scope (#168): declarations whose first line is pinned at the top of the
	// editor while their body is scrolled through, e.g. "function_declaration"
	// for Go or "class_definition" for Python. Empty means the language has no
	// sticky scopes (the feature is simply inert for it).
	ScopeNodes []string

	// FoldNodes lists the Tree-sitter node kinds that define a foldable
	// region (#144): multi-line nodes whose body can be collapsed behind the
	// header line, e.g. function bodies, blocks, import lists, multi-line
	// comments. Empty means folding falls back to ScopeNodes; both empty
	// means the language has no code folding.
	FoldNodes []string

	// Template is the initial content seeded into newly created files of this
	// language (#170), with ${FILENAME}/${NAME}/${DIR}/${PACKAGE}/${DATE}/${YEAR}
	// substituted — see TemplateFor. Empty means new files start empty. Users
	// override it per language via `[lang.<id>] template` in the config.
	Template string
}

var (
	mu       sync.RWMutex
	byID     = map[string]Language{}
	extIdx   = map[string]string{} // extension (no dot, lower) -> language id
	nameIx   = map[string]string{} // exact base name -> language id
	interpIx = map[string]string{} // shebang interpreter base name -> language id (#893)
	pathIx   = map[string]string{} // exact full path -> language id, from content sniffing (#893)
)

// Register records a language. Re-registering the same ID replaces the prior
// entry (last writer wins), so a user plugin can override a built-in. Safe to call
// from init().
func Register(l Language) {
	mu.Lock()
	defer mu.Unlock()
	byID[l.ID] = l
	for _, e := range l.Extensions {
		extIdx[strings.ToLower(strings.TrimPrefix(e, "."))] = l.ID
	}
	for _, n := range l.Filenames {
		nameIx[n] = l.ID
	}
	for _, i := range l.Interpreters {
		interpIx[i] = l.ID
	}
}

// AssociatePath records that path is language id, overriding what its
// extension or base name would say. The editor calls it when content sniffing
// — the shebang fallback (#893) — resolves a file the static indexes cannot;
// every path-keyed consumer (highlighting, LSP didOpen, statusline) then
// resolves the file through the ordinary ByPath. Re-sniffing on a later open
// simply overwrites the entry.
func AssociatePath(path, id string) {
	mu.Lock()
	defer mu.Unlock()
	pathIx[path] = id
}

// ServerLang returns the id of the language whose server spec handles this
// language's documents: ServerLanguage when set, else the language's own ID.
// The LSP subsystem resolves specs and keys server instances by this id, so a
// delegating language shares its delegate's server process per root (#1063).
func (l Language) ServerLang() string {
	if l.ServerLanguage != "" {
		return l.ServerLanguage
	}
	return l.ID
}

// HasServer reports whether documents of this language get a language server:
// either the language carries its own Server spec, or it delegates via
// ServerLanguage to a language that does.
func (l Language) HasServer() bool {
	if l.Server != nil {
		return true
	}
	if l.ServerLanguage != "" {
		if d, ok := ByID(l.ServerLanguage); ok {
			return d.Server != nil
		}
	}
	return false
}

// ByID returns the language with the given id.
func ByID(id string) (Language, bool) {
	mu.RLock()
	defer mu.RUnlock()
	l, ok := byID[id]
	return l, ok
}

// ByExt returns the language for a file extension (leading dot optional).
func ByExt(ext string) (Language, bool) {
	ext = strings.ToLower(strings.TrimPrefix(ext, "."))
	mu.RLock()
	defer mu.RUnlock()
	if id, ok := extIdx[ext]; ok {
		return byID[id], true
	}
	return Language{}, false
}

// ByPath returns the language for a file path: a sniffed per-path association
// (#893) wins, then an exact base name match (e.g. "Dockerfile"), then the
// extension.
func ByPath(path string) (Language, bool) {
	base := filepath.Base(path)
	mu.RLock()
	if id, ok := pathIx[path]; ok {
		l := byID[id]
		mu.RUnlock()
		return l, true
	}
	if id, ok := nameIx[base]; ok {
		l := byID[id]
		mu.RUnlock()
		return l, true
	}
	mu.RUnlock()
	return ByExt(filepath.Ext(path))
}

// Comments returns the comment syntax for path's language. ok is false when no
// language matches the path or the matched language declares no comment syntax
// at all; callers treat that as "comment toggling unavailable".
func Comments(path string) (line string, block [2]string, ok bool) {
	l, found := ByPath(path)
	if !found {
		return "", [2]string{}, false
	}
	return l.LineComment, l.BlockComment, l.LineComment != "" || l.BlockComment[0] != ""
}

// IndentAfter returns the block-opening line suffixes for path's language
// (Roadmap 0260). ok is false when no language matches the path or the matched
// language declares no indent rules; callers treat that as "plain copy-indent".
func IndentAfter(path string) ([]string, bool) {
	l, found := ByPath(path)
	if !found || len(l.IndentAfter) == 0 {
		return nil, false
	}
	return l.IndentAfter, true
}

// All returns every registered language, sorted by id (stable for tests/listing).
func All() []Language {
	mu.RLock()
	out := make([]Language, 0, len(byID))
	for _, l := range byID {
		out = append(out, l)
	}
	mu.RUnlock()
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}
