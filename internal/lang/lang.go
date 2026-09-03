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
	"regexp"
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

	// UseTabs is the language's indent-style default (#1137): non-nil true
	// means buffers of this language indent with tab characters, non-nil
	// false with spaces; nil means the language has no opinion and the
	// editor's global editor.use_spaces setting applies. Resolution order in
	// the editor: built-in default < editor.use_spaces < UseTabs <
	// .editorconfig — a project's explicit .editorconfig keeps the last word.
	// Set by make (recipes require a literal tab) and go (gofmt output is
	// tab-indented).
	UseTabs *bool

	// IndentAfter lists trimmed-line suffixes that open a block (Roadmap 0260):
	// a line ending with one of them indents the next line one level deeper,
	// e.g. ":" for Python or "{" for brace languages. Empty means the editor
	// falls back to plain copy-indent.
	IndentAfter []string

	// SpaceAfter lists the punctuation runes that get a space inserted after
	// them while typing (#1326), e.g. ':' in JSON so `"key":` becomes
	// `"key": ` as you type. This is the language's typing-convention table:
	// a language opts in by listing its runes, and the editor suppresses the
	// aid inside strings/comments, when a space already follows, and when the
	// user turns editor.typing.space_after_punctuation off. Empty means the
	// language has no such convention.
	SpaceAfter []rune

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

	// Postfix lists the postfix-completion templates of this language (#1913):
	// `expr.if`, `err.nil`, `expr.for` and friends, which rewrite the
	// expression before the dot instead of inserting at the cursor. Empty —
	// the default — means the feature is inert for the language. See
	// postfix.go.
	Postfix []PostfixTemplate

	// PostfixExprNodes lists the Tree-sitter node kinds that count as a
	// postfix-able expression (#1913), e.g. "call_expression" for Go or
	// "call" for Python. The postfix source takes the widest node of one of
	// these kinds ending exactly at the dot, which is what makes
	// `foo(bar).if` wrap the whole call while `x := foo(bar).if` wraps only
	// the call and not the assignment. Empty means the source falls back to
	// its token heuristic even where a grammar exists.
	PostfixExprNodes []string

	// Template is the initial content seeded into newly created files of this
	// language (#170), with ${FILENAME}/${NAME}/${DIR}/${PACKAGE}/${DATE}/${YEAR}
	// substituted — see TemplateFor. Empty means new files start empty. Users
	// override it per language via `[lang.<id>] template` in the config.
	Template string

	// Regions optionally detects embedded-language regions in a buffer of this
	// language (#1303). It exists for hosts whose embedded regions cannot be
	// expressed as a Tree-sitter injection query because the region's language
	// is not derivable from its own syntax: a .http request body is JSON, XML
	// or HTML depending on a *sibling header*. Nil — the normal case — means
	// the host uses injections.scm, or has no embedded regions at all.
	//
	// The detector runs on every highlight pass, so it must be cheap and must
	// not allocate more than the regions it returns. Regions outside the
	// buffer, or naming an unregistered language, are ignored by the consumer.
	Regions func(lines []string) []Region

	// EmbeddedShadow opts this host language into per-language shadow
	// documents for its embedded LSP fragments (#2330): instead of one
	// virtual document per detected region, the LSP manager merges all
	// regions of one embedded language into a single virtual document
	// spanning the whole host buffer, with everything outside the regions
	// blanked (one space per rune, newlines preserved) — VS Code's
	// virtual-document trick. Positions map 1:1 between host and shadow, and
	// all of a host's <script> regions share one scope, matching how a
	// browser executes them. False — the default — keeps the per-region
	// fragment documents (an SQL string in Python is a standalone statement;
	// merging separate strings would produce parse errors).
	EmbeddedShadow bool

	// Test declares how the language's test functions are detected and run
	// (#1150) — gutter run markers and run.testAtCursor. Nil means the
	// language has no test runner. See test.go.
	Test *TestSpec

	// Spans optionally produces Go-computed highlight spans for a buffer of
	// this language (#1585). It exists for structure a Tree-sitter grammar
	// does not expose — the .http grammar captures a request target as one
	// opaque url node, so query-parameter key/value/separator spans and
	// percent-encoding conceals are computed here instead. The spans are
	// overlaid *over* the grammar's (they win where both cover a cell), so a
	// producer must skip regions the grammar styles more specifically (e.g.
	// placeholders). Like Regions, it runs on every highlight pass and must
	// be cheap. Nil — the normal case — means the language has none.
	Spans func(lines []string) []Span

	// Folds optionally produces Go-computed fold ranges for a buffer of this
	// language (#1630). It exists for foldable structure no Tree-sitter
	// grammar provides — the unified diff format has no grammar at all, yet
	// its hunks fold at their @@ headers. Like Spans it rides every highlight
	// pass and must be cheap; ranges must be emitted in pre-order (outer
	// before inner). Nil — the normal case — means the language computes no
	// folds of its own (FoldNodes still applies when a grammar exists).
	Folds func(lines []string) []FoldRange

	// Lint optionally produces Go-computed notes for a buffer of this
	// language (#1623): mistakes a language server would report, for
	// languages that have none. Dotenv's duplicate keys are the first case —
	// an earlier `KEY=` silently loses to a later one in most loaders, which
	// is invisible without a mark. It rides the highlight pass like Spans, so
	// it must be cheap; nil — the normal case — means the language has no
	// linter. See Note.
	Lint func(lines []string) []Note

	// DepManifests optionally names the dependency manifest base names this
	// language owns (#2419) — go.mod, package.json, composer.json,
	// Cargo.toml, requirements.txt, pyproject.toml. The Dependencies tool
	// window only scans manifests some registered language declares, so
	// disabling a language plugin also silences its dependency scan. Nil —
	// the normal case — declares none.
	DepManifests []string
}

// Note is one Go-computed diagnostic (#1623): the half-open rune-column range
// [StartCol, EndCol) on Line, with an LSP severity (1 error, 2 warning,
// 3 information, 4 hint) and the message explaining it. The editor merges
// notes into the same gutter tint and inline underline that language-server
// diagnostics use, so a linted language marks problems without a server.
type Note struct {
	Line     int
	StartCol int
	EndCol   int
	Severity int
	Message  string
}

// Note severities, mirroring the LSP numbering used by internal/lsp.
const (
	NoteError = 1
	NoteWarn  = 2
	NoteInfo  = 3
	NoteHint  = 4
)

// Span is one Go-produced highlight run (#1585), the registry-level twin of
// highlight.Span: the half-open rune-column range [StartCol, EndCol) on Line
// carries a capture name the theme resolves. A non-empty Replace marks the
// range as a conceal-with-stand-in: the editor renders Replace (styled as a
// decoded stand-in) instead of the source runes on lines the caret is not on
// — e.g. "%20" displays as " ". Capture still styles the raw source when it
// is shown.
type Span struct {
	Line     int
	StartCol int
	EndCol   int
	Capture  string
	Replace  string
}

// FoldRange is one Go-computed foldable region (#1630), the registry-level
// twin of highlight.Fold: buffer lines [HeaderLine+1, EndLine] can collapse
// behind HeaderLine. Lines are 0-based and inclusive.
type FoldRange struct {
	HeaderLine int
	EndLine    int
}

// Region is one embedded-language range inside a host buffer, in editor
// coordinates: it covers [StartLine:StartCol, EndLine:EndCol) with 0-based
// lines and columns — the same convention highlight.Fragment uses.
type Region struct {
	Lang      string // language id of the embedded region, e.g. "json"
	StartLine int
	StartCol  int
	EndLine   int
	EndCol    int
}

// RegionAt returns the embedded region covering line (0-based) in a buffer of
// language langID, if the language has a region detector and one covers it
// (#1304): the editor indents a .http request body by the body's own language,
// not by the host's.
func RegionAt(langID string, lines []string, line int) (Region, bool) {
	l, ok := ByID(langID)
	if !ok || l.Regions == nil {
		return Region{}, false
	}
	for _, r := range l.Regions(lines) {
		if line >= r.StartLine && line <= r.EndLine {
			return r, true
		}
	}
	return Region{}, false
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

// templateSuffixes are the outer extensions marking a file as a template over
// its real type (#1595): Jinja2's .j2/.jinja/.jinja2 (Ansible templates) and
// the Go-template .tmpl/.tpl/.gotmpl. When every direct lookup fails and the
// path carries one, ByPath strips it and resolves the remaining name —
// environment.yml.j2 highlights as YAML, nginx.conf.j2 as ini. A file with no
// inner extension (motd.j2) stays plain text.
var templateSuffixes = map[string]bool{
	"j2": true, "jinja": true, "jinja2": true,
	"tmpl": true, "tpl": true, "gotmpl": true,
}

// rotationSuffix matches the trailing extension logrotate (and friends) tack
// onto a rotated file: a plain sequence number ("1", "2", ...) or a date stamp
// ("2026-08-01", "20260801"). When every direct lookup fails and the path
// carries one, ByPath strips it and resolves the remaining name the same way
// it does for templateSuffixes (#1595) — app.log.2026-08-01 highlights as log,
// app.log.1 too. A file with no inner extension (backup.1) stays plain text.
var rotationSuffix = regexp.MustCompile(`^(?:\d+|\d{4}-\d{2}-\d{2}|\d{8})$`)

// ByPath returns the language for a file path: a user-configured association
// (#1365, explicit intent beats detection) wins, then a sniffed per-path
// association (#893), then an exact base name match (e.g. "Dockerfile"), then
// the extension. A path whose extension is a known template suffix (#1595) or
// a rotation suffix (#1745, e.g. logrotate's ".1" or ".2026-08-01") resolves
// as the name with that suffix stripped — the inner extension keeps the last
// word, so a language claiming such a suffix outright still wins.
func ByPath(path string) (Language, bool) {
	if l, ok := ByAssociation(path); ok {
		return l, true
	}
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
	ext := filepath.Ext(path)
	if l, ok := ByExt(ext); ok {
		return l, true
	}
	if templateSuffixes[strings.ToLower(strings.TrimPrefix(ext, "."))] {
		return ByPath(strings.TrimSuffix(path, ext))
	}
	if suffix := strings.TrimPrefix(ext, "."); suffix != "" && rotationSuffix.MatchString(suffix) {
		stripped := strings.TrimSuffix(path, ext)
		if filepath.Ext(stripped) != "" {
			return ByPath(stripped)
		}
	}
	return Language{}, false
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
